// Clip-share keeps a history of the images copied on this Mac, in a menu
// bar panel: sending one copies it over ssh to a remote machine and puts
// its path there on the clipboard, to paste into a CLI running on it.
package main

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const maxShots = 100

// item is a shot with its thumbnail, decoded once off the main thread.
type item struct {
	shot
	thumb *ui.Bitmap
}

// app is the state the panel shows. All of it is read and changed on the
// main thread: workers publish through update.
type app struct {
	dir     string
	history *history
	sender  sender
	update  func(func()) // Window.Update
	copy    func(string) // mygo.Clipboard.WriteText
	hide    func()       // hides the panel
	quit    func()       // mygo.App.Quit
	now     func() time.Time

	items   []item
	list    ui.ListState
	hosts   []string
	host    string
	sending map[string]bool   // shot IDs being sent
	sent    map[string]string // host+"\x00"+ID → remote path
	status  string
	failed  bool
}

func newApp(dir string, hosts []string) *app {
	a := &app{
		dir:     dir,
		history: &history{dir: dir, max: maxShots},
		sender:  sender{run: exec.CommandContext},
		update:  func(fn func()) { fn() },
		copy:    func(string) {},
		hide:    func() {},
		quit:    func() {},
		now:     time.Now,
		hosts:   hosts,
		host:    loadSettings(dir).Host,
		sending: map[string]bool{},
		sent:    map[string]string{},
	}
	if !slices.Contains(hosts, a.host) {
		a.host = ""
		if len(hosts) > 0 {
			a.host = hosts[0]
		}
	}
	return a
}

func loadItem(s shot) item {
	it := item{shot: s}
	if data, err := os.ReadFile(s.Thumb); err == nil {
		it.thumb, _ = ui.DecodeBitmap(data)
	}
	return it
}

// load reads the stored history. It does file I/O: call it off the main
// thread.
func (a *app) load() {
	shots, err := a.history.list()
	items := make([]item, len(shots))
	for i, s := range shots {
		items[i] = loadItem(s)
	}
	a.update(func() {
		a.items = items
		if err != nil {
			a.setStatus("读取历史失败："+err.Error(), true)
		}
	})
}

// addImage stores an image the watcher found. It runs on the watcher's
// goroutine.
func (a *app) addImage(data []byte) {
	s, _, err := a.history.add(data, a.now())
	if err != nil {
		a.update(func() { a.setStatus("保存图片失败："+err.Error(), true) })
		return
	}
	it := loadItem(s)
	a.update(func() {
		a.items = slices.DeleteFunc(a.items, func(o item) bool { return o.ID == s.ID })
		a.items = slices.Insert(a.items, 0, it)
		if len(a.items) > maxShots {
			a.items = a.items[:maxShots]
		}
	})
}

func (a *app) setStatus(msg string, failed bool) { a.status, a.failed = msg, failed }

func sentKey(host, id string) string { return host + "\x00" + id }

// send copies an image to the chosen host. It is a click handler.
func (a *app) send(it item) {
	host := a.host
	if host == "" {
		a.setStatus("先在右上角选择远端主机（来自 ~/.ssh/config）", true)
		return
	}
	if a.sending[it.ID] {
		return
	}
	a.sending[it.ID] = true
	a.setStatus("正在发送到 "+host+"…", false)
	go func() {
		path, err := a.sender.send(context.Background(), host, it.shot)
		if err == nil {
			a.copy(path)
		}
		a.update(func() {
			delete(a.sending, it.ID)
			if err != nil {
				a.setStatus("发送失败："+err.Error(), true)
				return
			}
			a.sent[sentKey(host, it.ID)] = path
			a.setStatus("已复制远端路径："+path, false)
		})
	}()
}

func (a *app) clearHistory() {
	if err := a.history.clear(); err != nil {
		a.setStatus("清空失败："+err.Error(), true)
		return
	}
	a.items = nil
	a.setStatus("历史已清空", false)
}

func (a *app) chooseHost() {
	if err := saveSettings(a.dir, settings{Host: a.host}); err != nil {
		a.setStatus("保存设置失败："+err.Error(), true)
	}
}

func (a *app) view(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		a.hide()
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, a.quit)
	// Times such as "3 分钟前" go stale while the panel stays open.
	c.After(time.Minute)
	t := c.Theme()
	now := a.now()
	ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, "Clip Share").FontSize(17).Bold()
				ui.Text(c, "发送图片，远端路径写入剪贴板").FontSize(11).TextColor(t.TextMuted)
			})
			if len(a.hosts) == 0 {
				ui.Text(c, "~/.ssh/config 里没有主机").FontSize(11).TextColor(t.Danger)
			} else {
				ui.Select(c.Key("host"), &a.host, a.hosts).Label("远端主机").Width(150).OnChange(a.chooseHost)
			}
		})
		ui.Divider(c)
		a.list.Label = func(i int) string { return "图片 " + a.items[i].ID }
		ui.List(c.Key("shots"), &a.list, len(a.items)).Grow(1).Gap(4).
			ItemKey(func(i int) any { return a.items[i].ID }).
			Rows(func(row ui.ListRow) { a.row(row.Context, a.items[row.Index], now) }).
			Children(func() {
				if len(a.items) == 0 {
					ui.Column(c).Grow(1).Center().Gap(6).Padding(24).Children(func() {
						ui.Text(c, "还没有图片").FontSize(14).Bold()
						ui.Text(c, "截图或复制图片后，会自动出现在这里").FontSize(12).TextColor(t.TextMuted)
					})
				}
			})
		ui.Divider(c)
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			status, color := a.status, t.TextMuted
			if status == "" {
				status = fmt.Sprintf("%d 张图片，仅保存在本机", len(a.items))
			}
			if a.failed {
				color = t.Danger
			}
			ui.Text(c, status).Grow(1).FontSize(11).TextColor(color).MaxLines(2).Ellipsis("…")
			ui.Button(c, "清空").Disabled(len(a.items) == 0).OnClick(a.clearHistory)
			ui.Button(c, "退出").OnClick(a.quit)
		})
	})
}

func (a *app) row(c *ui.Context, it item, now time.Time) {
	t := c.Theme()
	ui.Row(c).Gap(10).Padding(6, 4).AlignItems(ui.Center).Children(func() {
		if it.thumb != nil {
			ui.Image(c, it.thumb).Size(84, 56).Fit(ui.Cover).Radius(6)
		} else {
			ui.Column(c).Size(84, 56).Radius(6).Background(t.Surface)
		}
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, ago(it.Taken, now)).FontSize(13)
			detail := fmt.Sprintf("%d × %d", it.W, it.H)
			if a.sent[sentKey(a.host, it.ID)] != "" {
				detail += " · 已发送"
			}
			ui.Text(c, detail).FontSize(11).TextColor(t.TextMuted)
		})
		label := "发送"
		if a.sending[it.ID] {
			label = "发送中…"
		}
		ui.PrimaryButton(c, label).Label("发送 " + it.ID).
			Disabled(a.sending[it.ID] || a.host == "").
			OnClick(func() { a.send(it) })
	})
}

// ago says when an image was copied, the way a clipboard history does.
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return "刚刚"
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	case t.YearDay() == now.YearDay() && t.Year() == now.Year():
		return t.Format("今天 15:04")
	default:
		return t.Format("01-02 15:04")
	}
}

// showPanel places the panel below the tray icon, kept on its display.
func showPanel(win *mygo.Window, tray *mygo.Tray) {
	anchor := tray.Bounds()
	if anchor.Width == 0 || anchor.Height == 0 {
		win.Center()
	} else {
		area := mygo.Screen.DisplayNearestPoint(mygo.Point{
			X: anchor.X + anchor.Width/2,
			Y: anchor.Y + anchor.Height/2,
		}).WorkArea
		b := win.Bounds()
		x := anchor.X + (anchor.Width-b.Width)/2
		y := anchor.Y + anchor.Height + 6
		if y+b.Height > area.Y+area.Height {
			y = anchor.Y - b.Height - 6
		}
		x = max(area.X, min(x, area.X+max(0, area.Width-b.Width)))
		y = max(area.Y, min(y, area.Y+max(0, area.Height-b.Height)))
		win.SetPosition(x, y)
	}
	win.Show()
}

// trayIcon draws a clipboard, black on transparent for macOS to tint.
func trayIcon() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	ink := color.NRGBA{R: 79, G: 124, B: 255, A: 255}
	if runtime.GOOS == "darwin" {
		ink = color.NRGBA{A: 255}
	}
	for _, r := range []image.Rectangle{
		image.Rect(6, 5, 26, 8), image.Rect(6, 26, 26, 29), // board
		image.Rect(6, 8, 9, 26), image.Rect(23, 8, 26, 26),
		image.Rect(12, 2, 20, 9),                               // clip
		image.Rect(12, 14, 20, 16), image.Rect(12, 19, 18, 21), // lines
	} {
		draw.Draw(img, r, image.NewUniform(ink), image.Point{}, draw.Src)
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func main() {
	mygo.App.SetName("Clip Share")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	var quitting bool
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
	mygo.App.WhenReady(func() {
		dir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			log.Fatal(err)
		}
		home, _ := os.UserHomeDir()
		a := newApp(dir, sshHosts(filepath.Join(home, ".ssh", "config")))
		tray, err := mygo.NewTray(mygo.TrayOptions{
			ID:             "clip-share",
			Icon:           trayIcon(),
			IconIsTemplate: true,
			ToolTip:        "Clip Share — 点击查看剪贴板图片",
		})
		if err != nil {
			log.Fatal("tray: ", err)
		}
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:             "Clip Share",
			Width:             380,
			Height:            520,
			Hidden:            true,
			Frameless:         true,
			DisableResize:     true,
			DisableMinimize:   true,
			DisableMaximize:   true,
			DisableFullScreen: true,
			AlwaysOnTop:       true,
			SkipTaskbar:       true,
			Content:           ui.View(a.view),
		})
		a.update = win.Update
		a.copy = mygo.Clipboard.WriteText
		a.hide = win.Hide
		a.quit = mygo.App.Quit
		win.OnBlur(win.Hide)
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				win.Hide()
			}
		})
		toggle := func() {
			if win.IsVisible() {
				win.Hide()
			} else {
				showPanel(win, tray)
			}
		}
		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: "打开 Clip Share", Click: func(*mygo.MenuItem, *mygo.Window) { showPanel(win, tray) }},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		})
		if runtime.GOOS == "linux" {
			tray.SetMenu(menu)
		} else {
			tray.OnClick(toggle)
			tray.OnRightClick(func() {
				win.Hide()
				tray.SetMenu(menu)
				defer tray.SetMenu(nil)
				tray.PopUpMenu()
			})
		}
		ctx, stop := context.WithCancel(context.Background())
		mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
			stop()
			tray.Destroy()
		})
		go func() {
			a.load()
			w := &watcher{
				clip:     mygo.Clipboard,
				changes:  clipboardChangeCount(),
				interval: 700 * time.Millisecond,
				onImage:  a.addImage,
			}
			w.run(ctx)
		}()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
