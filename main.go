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

	"clip-share/locales"
)

const maxShots = 100

// summonKey opens the panel from anywhere. Raycast and Spotlight leave it
// free, and Finder's Cmd+Alt+V moves files.
const (
	summonKey   = "Ctrl+Alt+V"
	summonLabel = "⌃⌥V"
)

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
	update  func(func())     // Window.Update
	copy    func(string)     // mygo.Clipboard.WriteText
	hide    func()           // hides the panel
	quit    func()           // mygo.App.Quit
	login   func(bool) error // mygo.App.SetOpenAtLogin
	now     func() time.Time

	items   []item
	list    ui.ListState
	hosts   []string
	host    string
	sending map[string]float64 // shot IDs being sent → share uploaded
	sent    map[string]string  // host+"\x00"+ID → remote path
	status  string
	failed  bool
	atLogin bool
	opts    settings
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
		login:   func(bool) error { return nil },
		now:     time.Now,
		hosts:   hosts,
		opts:    loadSettings(dir),
		sending: map[string]float64{},
		sent:    map[string]string{},
	}
	a.host = a.opts.Host
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
			a.setStatus(locales.StatusLoadFailed(err.Error()), true)
		}
	})
}

// addImage stores an image the watcher found. It runs on the watcher's
// goroutine.
func (a *app) addImage(data []byte) {
	s, _, err := a.history.add(data, a.now())
	if err != nil {
		a.update(func() { a.setStatus(locales.StatusSaveFailed(err.Error()), true) })
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
		a.setStatus(locales.StatusPickHost(), true)
		return
	}
	if _, busy := a.sending[it.ID]; busy {
		return
	}
	a.sending[it.ID] = 0
	a.setStatus(locales.StatusSendingTo(host), false)
	opts := a.opts.sendOptions()
	go func() {
		path, err := a.sender.send(context.Background(), host, it.shot, opts, func(done, total int64) {
			a.update(func() {
				if _, busy := a.sending[it.ID]; busy {
					a.sending[it.ID] = float64(done) / float64(max(total, 1))
				}
			})
		})
		if err == nil {
			a.copy(path)
		}
		a.update(func() {
			delete(a.sending, it.ID)
			if err != nil {
				a.setStatus(locales.StatusSendFailed(err.Error()), true)
				return
			}
			a.sent[sentKey(host, it.ID)] = path
			a.setStatus(locales.StatusCopied(path), false)
		})
	}()
}

// warm connects to the chosen host while the user picks an image.
func (a *app) warm() {
	if host := a.host; host != "" {
		go a.sender.warm(context.Background(), host)
	}
}

func (a *app) clearHistory() {
	if err := a.history.clear(); err != nil {
		a.setStatus(locales.StatusClearFailed(err.Error()), true)
		return
	}
	a.items = nil
	a.setStatus(locales.StatusCleared(), false)
}

func (a *app) toggleLogin() {
	if err := a.login(a.atLogin); err != nil {
		a.atLogin = !a.atLogin
		a.setStatus(locales.StatusLoginFailed(err.Error()), true)
	}
}

func (a *app) chooseHost() {
	a.opts.Host = a.host
	if err := saveSettings(a.dir, a.opts); err != nil {
		a.setStatus(locales.StatusSettingsFailed(err.Error()), true)
	}
	a.warm()
}

func (a *app) view(c *ui.Context) {
	if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(ui.Cmd, ui.KeyW) {
		a.hide()
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, a.quit)
	// Times such as "3 min ago" go stale while the panel stays open.
	c.After(time.Minute)
	t := c.Theme()
	now := a.now()
	ui.Column(c).Fill().Padding(16).Gap(12).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(2).Children(func() {
				ui.Text(c, "Clip Share").FontSize(17).Bold()
				ui.Text(c, locales.AppSubtitle(summonLabel)).FontSize(11).TextColor(t.TextMuted)
			})
			if len(a.hosts) == 0 {
				ui.Text(c, locales.NoHosts()).FontSize(11).TextColor(t.Danger)
			} else {
				ui.Select(c.Key("host"), &a.host, a.hosts).Label(locales.HostLabel()).Width(150).OnChange(a.chooseHost)
			}
		})
		ui.Divider(c)
		a.list.Label = func(i int) string { return a.items[i].ID }
		ui.List(c.Key("shots"), &a.list, len(a.items)).Grow(1).Gap(4).
			ItemKey(func(i int) any { return a.items[i].ID }).
			Rows(func(row ui.ListRow) { a.row(row.Context, a.items[row.Index], now) }).
			Children(func() {
				if len(a.items) == 0 {
					ui.Column(c).Grow(1).Center().Gap(6).Padding(24).Children(func() {
						ui.Text(c, locales.EmptyTitle()).FontSize(14).Bold()
						ui.Text(c, locales.EmptyHint()).FontSize(12).TextColor(t.TextMuted)
					})
				}
			})
		ui.Divider(c)
		status, color := a.status, t.TextMuted
		if status == "" {
			status = locales.ImageCount(len(a.items))
		}
		if a.failed {
			color = t.Danger
		}
		ui.Text(c, status).FontSize(11).TextColor(color).MaxLines(2).Ellipsis("…")
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Checkbox(c.Key("login"), &a.atLogin, locales.OpenAtLogin()).OnChange(a.toggleLogin)
			ui.Spacer(c)
			ui.Button(c, locales.Clear()).Disabled(len(a.items) == 0).OnClick(a.clearHistory)
			ui.Button(c, locales.Quit()).OnClick(a.quit)
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
				detail += " · " + locales.SentTag()
			}
			ui.Text(c, detail).FontSize(11).TextColor(t.TextMuted)
		})
		label := locales.Send()
		progress, busy := a.sending[it.ID]
		if busy {
			label = locales.SendingPercent(int(progress * 100))
		}
		ui.PrimaryButton(c, label).Label(locales.SendImage(it.ID)).
			Disabled(busy || a.host == "").
			OnClick(func() { a.send(it) })
	})
}

// ago says when an image was copied, the way a clipboard history does.
func ago(t, now time.Time) string {
	d := now.Sub(t)
	switch {
	case d < time.Minute:
		return locales.JustNow()
	case d < time.Hour:
		return locales.MinutesAgo(int(d.Minutes()))
	case t.YearDay() == now.YearDay() && t.Year() == now.Year():
		return locales.TodayAt(t)
	default:
		return locales.DateAt(t)
	}
}

// showPanel places the panel below the tray icon. A crowded menu bar can
// push the icon off the screen, behind the notch or past the edge: the
// panel then shows on the display of the pointer, a third of the way down,
// as launchers do.
func showPanel(win *mygo.Window, tray *mygo.Tray, onShow func()) {
	anchor := tray.Bounds()
	center := mygo.Point{X: anchor.X + anchor.Width/2, Y: anchor.Y + anchor.Height/2}
	d := mygo.Screen.DisplayNearestPoint(center)
	b := win.Bounds()
	area := d.WorkArea
	var x, y int
	if anchor.Width > 0 && anchor.Height > 0 && contains(d.Bounds, center) {
		x = anchor.X + (anchor.Width-b.Width)/2
		y = anchor.Y + anchor.Height + 6
		if y+b.Height > area.Y+area.Height {
			y = anchor.Y - b.Height - 6
		}
	} else {
		area = mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
		x = area.X + (area.Width-b.Width)/2
		y = area.Y + (area.Height-b.Height)/3
	}
	x = max(area.X, min(x, area.X+max(0, area.Width-b.Width)))
	y = max(area.Y, min(y, area.Y+max(0, area.Height-b.Height)))
	win.SetPosition(x, y)
	win.Show()
	onShow()
}

func contains(r mygo.Rectangle, p mygo.Point) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
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
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running app shows its panel
	}
	mygo.App.SetName("Clip Share")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {})
	var quitting bool
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
	mygo.App.WhenReady(func() {
		dir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			log.Fatal(err)
		}
		home, _ := os.UserHomeDir()
		a := newApp(dir, sshHosts(filepath.Join(home, ".ssh", "config")))
		placeTrayRight("clip-share", 420)
		tray, err := mygo.NewTray(mygo.TrayOptions{
			ID:             "clip-share",
			Icon:           trayIcon(),
			IconIsTemplate: true,
			ToolTip:        locales.TrayTooltip(summonLabel),
		})
		if err != nil {
			log.Fatal("tray: ", err)
		}
		// A panel takes the keyboard without activating the app, so the
		// terminal keeps its place in front and has the keyboard back once
		// the panel hides.
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:         "Clip Share",
			Panel:         true,
			Width:         380,
			Height:        520,
			Hidden:        true,
			Frameless:     true,
			DisableResize: true,
			Content:       ui.View(a.view),
		})
		a.update = win.Update
		a.copy = mygo.Clipboard.WriteText
		a.hide = win.Hide
		a.quit = mygo.App.Quit
		a.login = mygo.App.SetOpenAtLogin
		a.atLogin = mygo.App.OpenAtLogin()
		win.OnBlur(win.Hide)
		win.OnClose(func(e *mygo.CloseEvent) {
			// Escape and Cmd+W hide the panel; quitting closes it.
			if !quitting {
				e.PreventDefault()
				win.Hide()
			}
		})
		show := func() { showPanel(win, tray, a.warm) }
		toggle := func() {
			if win.IsVisible() {
				win.Hide()
			} else {
				show()
			}
		}
		if err := mygo.GlobalShortcut.Register(summonKey, toggle); err != nil {
			a.setStatus(locales.StatusShortcutTaken(summonLabel, err.Error()), true)
		}
		// Opening the app again, from Spotlight or Launchpad, shows the
		// panel: the way in when the menu bar has no room for the icon.
		mygo.App.OnSecondInstance(func([]string, string) { show() })
		mygo.App.OnActivate(func(bool) { show() })
		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: locales.MenuOpen(), Click: func(*mygo.MenuItem, *mygo.Window) { show() }},
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
			mygo.GlobalShortcut.UnregisterAll()
			tray.Destroy()
		})
		if !mygo.App.WasOpenedAtLogin() {
			show() // launched by hand: show where it lives
		}
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
