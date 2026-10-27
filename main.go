// Ferry keeps a history of the images copied on this Mac, in a menu bar
// panel: sending one copies it over ssh to a remote machine and puts its
// path there on the clipboard, to paste into a CLI running on it.
package main

import (
	"context"
	_ "embed"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"clip-ferry/locales"
)

const maxShots = 100

//go:embed resources/tray.png
var trayPNG []byte

// item is a shot with its thumbnail, decoded once off the main thread.
type item struct {
	shot
	thumb *ui.Bitmap
}

// app is the state the panel and the settings show. All of it is read and
// changed on the main thread: workers publish through update.
type app struct {
	dir     string
	history *history
	sender  sender
	update  func(func())     // Window.Update
	copy    func(string)     // mygo.Clipboard.WriteText
	close   func()           // closes the panel and gives the keyboard back
	quit    func()           // mygo.App.Quit
	login   func(bool) error // mygo.App.SetOpenAtLogin
	rebind  func(old, new shortcut) error
	locale  func(string) // mygo.App.SetLocale
	prefs   func()       // opens the settings
	now     func() time.Time

	items   []item
	sel     int // the row Enter sends
	list    ui.ListState
	hosts   []string // ~/.ssh/config's; allHosts adds those added by hand
	host    string
	newHost string // typed in the settings
	hostErr string
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
		close:   func() {},
		quit:    func() {},
		login:   func(bool) error { return nil },
		rebind:  func(shortcut, shortcut) error { return nil },
		locale:  func(string) {},
		prefs:   func() {},
		now:     time.Now,
		hosts:   hosts,
		opts:    loadSettings(dir),
		sending: map[string]float64{},
		sent:    map[string]string{},
	}
	a.host = a.opts.Host
	a.keepHost()
	return a
}

// allHosts are the hosts of ~/.ssh/config, then those added by hand.
func (a *app) allHosts() []string {
	all := slices.Clone(a.hosts)
	for _, h := range a.opts.Hosts {
		if !slices.Contains(all, h) {
			all = append(all, h)
		}
	}
	return all
}

// keepHost falls back to the first host when the chosen one is gone.
func (a *app) keepHost() {
	all := a.allHosts()
	if !slices.Contains(all, a.host) {
		a.host = ""
		if len(all) > 0 {
			a.host = all[0]
		}
	}
}

// addHost adds the host typed in the settings, and chooses it.
func (a *app) addHost() {
	h, ok := parseHost(a.newHost)
	switch {
	case !ok:
		a.hostErr = locales.ErrHostInvalid()
	case slices.Contains(a.allHosts(), h):
		a.hostErr = locales.ErrHostExists(h)
	default:
		a.opts.Hosts = append(a.opts.Hosts, h)
		a.newHost, a.hostErr = "", ""
		a.chooseHost(h)
	}
}

func (a *app) removeHost(h string) {
	a.opts.Hosts = slices.DeleteFunc(a.opts.Hosts, func(o string) bool { return o == h })
	a.keepHost()
	a.save()
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

// addImage stores an image the watcher found and selects it. It runs on
// the watcher's goroutine.
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
		a.sel = 0
	})
}

func (a *app) setStatus(msg string, failed bool) { a.status, a.failed = msg, failed }

func sentKey(host, id string) string { return host + "\x00" + id }

// opened resets the panel for a new look: the newest image selected, the
// last outcome kept only if it failed.
func (a *app) opened() {
	a.sel = 0
	a.list.ScrollIntoView(0)
	if !a.failed && len(a.sending) == 0 {
		a.status = ""
	}
	a.warm()
}

// send copies an image to the chosen host. It runs on the main thread.
func (a *app) send(it item) {
	host := a.host
	if host == "" {
		a.setStatus(locales.ChooseHost(), true)
		return
	}
	if _, busy := a.sending[it.ID]; busy {
		return
	}
	a.sending[it.ID] = 0
	a.setStatus(locales.StatusSending(0), false)
	opts := a.opts.sendOptions()
	go func() {
		path, err := a.sender.send(context.Background(), host, it.shot, opts, func(done, total int64) {
			a.update(func() {
				if _, busy := a.sending[it.ID]; busy {
					p := float64(done) / float64(max(total, 1))
					a.sending[it.ID] = p
					a.setStatus(locales.StatusSending(int(p*100)), false)
				}
			})
		})
		if err == nil {
			a.copy(path)
		}
		a.update(func() {
			delete(a.sending, it.ID)
			if err != nil {
				a.setStatus(sendFailure(host, err), true)
				return
			}
			a.sent[sentKey(host, it.ID)] = path
			a.setStatus(locales.StatusCopied(), false)
			if a.opts.AutoHide {
				a.close()
			}
		})
	}()
}

// sendFailure names what went wrong in a few words: ssh's own message
// only when it is not one of the usual two.
func sendFailure(host string, err error) string {
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"could not resolve", "connection refused", "timed out", "no route", "unreachable", "host is down", "connection closed", "connection reset"} {
		if strings.Contains(msg, s) {
			return locales.StatusCantReach(host)
		}
	}
	if strings.Contains(msg, "permission denied") {
		return locales.StatusLoginRefused(host)
	}
	return locales.StatusSendFailed(err.Error())
}

// warm connects to the chosen host while the user picks an image.
func (a *app) warm() {
	if host := a.host; host != "" {
		go a.sender.warm(context.Background(), host)
	}
}

func (a *app) move(by int) {
	if len(a.items) == 0 {
		return
	}
	a.sel = max(0, min(a.sel+by, len(a.items)-1))
	a.list.ScrollIntoView(a.sel)
}

func (a *app) remove(id string) {
	i := slices.IndexFunc(a.items, func(it item) bool { return it.ID == id })
	if i < 0 {
		return
	}
	a.history.remove(a.items[i].shot)
	a.items = slices.Delete(a.items, i, i+1)
	a.sel = max(0, min(a.sel, len(a.items)-1))
}

func (a *app) clearHistory() {
	if err := a.history.clear(); err != nil {
		a.setStatus(locales.StatusDeleteFailed(err.Error()), true)
		return
	}
	a.items, a.sel = nil, 0
	a.setStatus(locales.StatusCleared(), false)
}

func (a *app) setLogin(on bool) {
	if err := a.login(on); err != nil {
		a.setStatus(locales.StatusLoginFailed(), true)
		return
	}
	a.atLogin = on
}

func (a *app) chooseHost(host string) {
	a.host = host
	a.save()
	a.warm()
}

func (a *app) chooseShortcut(k shortcut) {
	old := a.opts.shortcut()
	if k == old {
		return
	}
	if err := a.rebind(old, k); err != nil {
		a.setStatus(locales.StatusShortcutTaken(k.label), true)
		return
	}
	a.opts.Shortcut = k.accel
	a.save()
}

func (a *app) chooseLanguage(tag string) {
	a.opts.Language = tag
	a.locale(tag)
	a.save()
}

func (a *app) save() {
	a.opts.Host = a.host
	if err := saveSettings(a.dir, a.opts); err != nil {
		a.setStatus(locales.StatusSettingsFailed(err.Error()), true)
	}
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

// trayVisible reports whether the tray icon shows on a display. A crowded
// menu bar pushes icons behind the notch or past the edge, where a popover
// would hang from nothing.
func trayVisible(tray *mygo.Tray) bool {
	b := tray.Bounds()
	if b.Width <= 0 || b.Height <= 0 {
		return false
	}
	center := mygo.Point{X: b.X + b.Width/2, Y: b.Y + b.Height/2}
	return contains(mygo.Screen.DisplayNearestPoint(center).Bounds, center)
}

func contains(r mygo.Rectangle, p mygo.Point) bool {
	return p.X >= r.X && p.X < r.X+r.Width && p.Y >= r.Y && p.Y < r.Y+r.Height
}

// centerPanel shows the panel on the display of the pointer, a third of
// the way down, as launchers do.
func centerPanel(win *mygo.Window) {
	area := mygo.Screen.DisplayNearestPoint(mygo.Screen.CursorScreenPoint()).WorkArea
	b := win.Bounds()
	win.SetPosition(area.X+(area.Width-b.Width)/2, area.Y+(area.Height-b.Height)/3)
	win.Show()
}

// moveOldData moves the history kept under the app's old name. MyGo makes
// the new folder before the app starts, so it moves what is in it.
func moveOldData(dir string) {
	old := filepath.Join(filepath.Dir(dir), "Clip Share")
	for _, name := range []string{"images", "thumbs", "settings.json"} {
		if _, err := os.Stat(filepath.Join(dir, name)); errors.Is(err, os.ErrNotExist) {
			_ = os.Rename(filepath.Join(old, name), filepath.Join(dir, name))
		}
	}
	_ = os.Remove(old) // once empty
}

const (
	panelWidth  = 340
	panelHeight = 420
)

func main() {
	if !mygo.App.RequestSingleInstanceLock() {
		return // the running app shows its panel
	}
	mygo.App.SetName("Ferry")
	mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	mygo.App.OnWindowAllClosed(func() {}) // the panel closes; the app stays
	var quitting bool
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { quitting = true })
	mygo.App.WhenReady(func() {
		dir, err := mygo.App.Path(mygo.PathUserData)
		if err != nil {
			log.Fatal(err)
		}
		moveOldData(dir)
		home, _ := os.UserHomeDir()
		a := newApp(dir, sshHosts(filepath.Join(home, ".ssh", "config")))
		placeTrayRight("ferry", 420)
		tray, err := mygo.NewTray(mygo.TrayOptions{
			ID:             "ferry",
			Icon:           trayPNG,
			IconIsTemplate: true,
			ToolTip:        locales.TrayTooltip(a.opts.shortcut().label),
		})
		if err != nil {
			log.Fatal("tray: ", err)
		}

		// The popover hangs from the tray icon. Where the icon does not
		// show, the same view opens in a panel in the middle of the
		// screen, which takes the keyboard without activating the app.
		var popover, settingsWin *mygo.Window
		panel := mygo.NewWindow(mygo.WindowOptions{
			Title:         "Ferry",
			Panel:         true,
			Width:         panelWidth,
			Height:        panelHeight,
			Hidden:        true,
			Frameless:     true,
			DisableResize: true,
			Transparent:   true,
			Vibrancy:      mygo.VibrancyPopover,
			CornerRadius:  12,
			Content:       ui.View(a.view),
		})
		panel.OnBlur(panel.Hide)
		panel.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				panel.Hide()
			}
		})
		a.update = func(fn func()) {
			panel.Update(func() {
				fn()
				for _, w := range []*mygo.Window{popover, settingsWin} {
					if w != nil {
						w.Invalidate()
					}
				}
			})
		}
		open := func() bool { return popover != nil || panel.IsVisible() }
		var place func(tries int)
		// place opens the popover once the icon has its place in the menu
		// bar, which it lacks for a moment after the app starts.
		place = func(tries int) {
			if open() {
				return
			}
			if !trayVisible(tray) {
				if tries > 0 {
					time.AfterFunc(100*time.Millisecond, func() { panel.Update(func() { place(tries - 1) }) })
				} else {
					centerPanel(panel)
				}
				return
			}
			popover = mygo.NewFlyout(mygo.FlyoutOptions{
				Tray:      tray,
				Placement: mygo.PlacementBottom,
				Width:     panelWidth,
				Height:    panelHeight,
				Focusable: true,
				Shadow:    true,
				Popover:   true,
				Content:   ui.View(a.view),
			})
			popover.OnClosed(func() { popover = nil })
		}
		show := func() {
			if open() {
				return
			}
			a.opened()
			place(10)
		}
		a.close = func() {
			activated := popover != nil
			if popover != nil {
				popover.Close()
			}
			panel.Hide()
			// The popover activated the app to take the keyboard: hiding
			// the app gives it back to the terminal.
			if activated && settingsWin == nil {
				mygo.App.Hide()
			}
		}
		toggle := func() {
			if open() {
				a.close()
			} else {
				show()
			}
		}
		a.copy = mygo.Clipboard.WriteText
		a.quit = mygo.App.Quit
		a.login = mygo.App.SetOpenAtLogin
		a.atLogin = mygo.App.OpenAtLogin()
		a.locale = mygo.App.SetLocale
		a.rebind = func(old, k shortcut) error {
			mygo.GlobalShortcut.Unregister(old.accel)
			if err := mygo.GlobalShortcut.Register(k.accel, toggle); err != nil {
				_ = mygo.GlobalShortcut.Register(old.accel, toggle)
				return err
			}
			tray.SetToolTip(locales.TrayTooltip(k.label))
			return nil
		}
		a.prefs = func() {
			if popover != nil {
				popover.Close()
			}
			panel.Hide()
			if settingsWin != nil {
				settingsWin.Focus()
				return
			}
			settingsWin = mygo.NewWindow(mygo.WindowOptions{
				Title:         locales.SettingsTitle(),
				Width:         480,
				Height:        620,
				DisableResize: true,
				Content:       ui.View(a.settingsView),
			})
			settingsWin.OnClosed(func() { settingsWin = nil })
			settingsWin.Center()
			mygo.App.Focus()
		}
		if err := mygo.GlobalShortcut.Register(a.opts.shortcut().accel, toggle); err != nil {
			a.setStatus(locales.StatusShortcutTaken(a.opts.shortcut().label), true)
		}
		// Opening the app again, from Spotlight or Launchpad, shows the
		// panel: the way in when the menu bar has no room for the icon.
		mygo.App.OnSecondInstance(func([]string, string) { show() })
		menu := mygo.NewMenu([]*mygo.MenuItem{
			{Label: locales.MenuOpen(), Click: func(*mygo.MenuItem, *mygo.Window) { show() }},
			{Label: locales.MenuSettings(), Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { a.prefs() }},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		})
		if runtime.GOOS == "linux" {
			tray.SetMenu(menu)
		} else {
			tray.OnClick(toggle)
			tray.OnRightClick(func() {
				a.close()
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
