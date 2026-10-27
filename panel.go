package main

import (
	"fmt"
	"slices"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"clip-ferry/locales"
)

// Lucide icons (lucide.dev, ISC), drawn in the color of the text.
var (
	iconMore  = icon(`<circle cx="12" cy="12" r="1"/><circle cx="19" cy="12" r="1"/><circle cx="5" cy="12" r="1"/>`)
	iconDown  = icon(`<path d="m6 9 6 6 6-6"/>`)
	iconImage = icon(`<rect width="18" height="18" x="3" y="3" rx="2" ry="2"/><circle cx="9" cy="9" r="2"/><path d="m21 15-3.086-3.086a2 2 0 0 0-2.828 0L6 21"/>`)
	iconCheck = icon(`<path d="M20 6 9 17l-5-5"/>`)
	iconX     = icon(`<path d="M18 6 6 18"/><path d="m6 6 12 12"/>`)
)

func icon(body string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` + body + `</svg>`))
}

var digitKeys = []ui.Key{ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9}

// languages are the ones the app is written in, "" for the system's.
var languages = []string{"", "en", "zh-Hans"}

func languageName(tag string) string {
	if tag == "" {
		return locales.FollowSystem()
	}
	return i18n.Autonym(tag)
}

func (a *app) keys(c *ui.Context) {
	switch {
	case c.Shortcut(0, ui.KeyEscape), c.Shortcut(ui.Cmd, ui.KeyW):
		a.close()
	case c.Shortcut(0, ui.KeyUp):
		a.move(-1)
	case c.Shortcut(0, ui.KeyDown):
		a.move(1)
	case c.Shortcut(0, ui.KeyEnter):
		if a.sel < len(a.items) {
			a.send(a.items[a.sel])
		}
	case c.Shortcut(0, ui.KeyBackspace), c.Shortcut(0, ui.KeyDelete):
		if a.sel < len(a.items) {
			a.remove(a.items[a.sel].ID)
		}
	case c.Shortcut(ui.Cmd, ui.KeyComma):
		a.prefs()
	}
	for i, k := range digitKeys {
		if c.Shortcut(0, k) && i < len(a.items) {
			a.sel = i
			a.send(a.items[i])
		}
	}
	c.OnShortcut(ui.Cmd, ui.KeyQ, a.quit)
}

// view is the panel: over the popover's material on macOS, on a surface
// of its own elsewhere.
func (a *app) view(c *ui.Context) {
	a.keys(c)
	// Times such as "3 min ago" go stale while the panel stays open.
	c.After(time.Minute)
	t := c.Theme()
	c.Root().Background(ui.Transparent)
	box := ui.Column(c).Fill()
	if !c.Vibrancy() {
		box.Background(t.Background).Radius(12).Border(1, t.Border)
	}
	box.Children(func() {
		a.header(c)
		a.shots(c)
		a.footer(c)
	})
}

func (a *app) header(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Padding(10, 10, 6, 14).Gap(4).AlignItems(ui.Center).Children(func() {
		ui.Text(c, "Ferry").FontSize(13).FontWeight(600).Grow(1)
		ui.Toolbar(c, func() {
			host := a.host
			if host == "" {
				host = locales.NoHosts()
			}
			ui.Button(c.Key("host"), "").Label(locales.HostLabel()).Children(func() {
				dot := t.Success
				if a.host == "" {
					dot = t.TextMuted
				}
				ui.Box(c).Size(6, 6).Radius(3).Background(dot)
				ui.Text(c, host).FontSize(12).SingleLine()
				ui.Icon(c, iconDown).FontSize(12).TextColor(t.TextMuted)
			}).Menu(a.hostMenu)
			ui.Button(c.Key("more"), "").Label(locales.More()).Children(func() {
				ui.Icon(c, iconMore).FontSize(15)
			}).Menu(a.moreMenu)
		})
	})
}

// hostMenu tells where the hosts come from: ~/.ssh/config, or the
// settings, where more are added.
func (a *app) hostMenu(m *ui.Menu) {
	group := func(title string, hosts []string) {
		if len(hosts) == 0 {
			return
		}
		m.Item(title).Disabled(true)
		for _, h := range hosts {
			if m.Item(h).Checked(h == a.host).Chosen() {
				a.chooseHost(h)
			}
		}
		m.Separator()
	}
	group(locales.HostsFromConfig(), a.hosts)
	added := slices.DeleteFunc(slices.Clone(a.opts.Hosts), func(h string) bool { return slices.Contains(a.hosts, h) })
	group(locales.HostsAdded(), added)
	if m.Item(locales.AddHost()).Chosen() {
		a.prefs()
	}
}

func (a *app) moreMenu(m *ui.Menu) {
	if m.Item(locales.MenuSettings()).Shortcut(ui.Cmd, ui.KeyComma).Chosen() {
		a.prefs()
	}
	if m.Item(locales.OpenAtLogin()).Checked(a.atLogin).Chosen() {
		a.setLogin(!a.atLogin)
	}
	m.Submenu(locales.Language(), func(m *ui.Menu) {
		for _, tag := range languages {
			if m.Item(languageName(tag)).Checked(tag == a.opts.Language).Chosen() {
				a.chooseLanguage(tag)
			}
		}
	})
	m.Separator()
	if m.Item(locales.ClearHistory()).Disabled(len(a.items) == 0).Chosen() {
		a.clearHistory()
	}
	if m.Item(locales.Quit()).Shortcut(ui.Cmd, ui.KeyQ).Chosen() {
		a.quit()
	}
}

func (a *app) shots(c *ui.Context) {
	t := c.Theme()
	now := a.now()
	if len(a.items) == 0 {
		ui.Column(c).Grow(1).Center().Gap(10).Children(func() {
			ui.Icon(c, iconImage).FontSize(26).TextColor(t.TextMuted.Alpha(0.6))
			ui.Text(c, locales.EmptyHint()).FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	a.list.Label = func(i int) string { return a.items[i].ID }
	ui.List(c.Key("shots"), &a.list, len(a.items)).Grow(1).Gap(2).Padding(2, 8).
		ItemKey(func(i int) any { return a.items[i].ID }).
		Rows(func(row ui.ListRow) { a.row(row.Context, row.Index, now) })
}

func (a *app) row(c *ui.Context, i int, now time.Time) {
	t := c.Theme()
	it := a.items[i]
	progress, busy := a.sending[it.ID]
	sent := a.sent[sentKey(a.host, it.ID)] != ""
	r := ui.Row(c).Padding(6, 8).Gap(10).Radius(8).AlignItems(ui.Center).Label(locales.SendImage(it.ID))
	switch {
	case i == a.sel:
		r.Background(t.Accent.Alpha(0.16))
	case r.Hovered():
		r.Background(t.Text.Alpha(0.06))
	}
	if r.Clicked() {
		a.sel = i
	}
	if r.DoubleClicked() {
		a.send(it)
	}
	r.ContextMenu(func(m *ui.Menu) {
		if m.Item(locales.Send()).Disabled(busy || a.host == "").Chosen() {
			a.send(it)
		}
		if m.Item(locales.Delete()).Chosen() {
			a.remove(it.ID)
		}
	})
	r.Children(func() {
		key := ""
		if i < len(digitKeys) {
			key = fmt.Sprint(i + 1)
		}
		ui.Text(c, key).FontSize(11).TextColor(t.TextMuted).FontFeatures("tnum").Width(8)
		if it.thumb != nil {
			ui.Image(c, it.thumb).Size(56, 38).Fit(ui.Cover).Radius(5).Border(1, t.Border)
		} else {
			ui.Box(c).Size(56, 38).Radius(5).Background(t.Surface)
		}
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Text(c, ago(it.Taken, now)).FontSize(13).FontWeight(500)
			switch {
			case busy:
				ui.Box(c).Height(3).Radius(2).Background(t.Text.Alpha(0.1)).Children(func() {
					ui.Box(c).Height(3).Radius(2).WidthPercent(float32(progress * 100)).Background(t.Accent)
				})
			case sent:
				ui.Row(c).Gap(3).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, iconCheck).FontSize(11).TextColor(t.Success)
					ui.Text(c, locales.SentTag()).FontSize(11).TextColor(t.Success)
				})
			default:
				ui.Text(c, fmt.Sprintf("%d × %d", it.W, it.H)).FontSize(11).TextColor(t.TextMuted).FontFeatures("tnum")
			}
		})
		switch {
		case busy:
			ui.Text(c, locales.Percent(int(progress*100))).FontSize(11).TextColor(t.TextMuted).FontFeatures("tnum")
		case i == a.sel || r.Hovered():
			ui.PrimaryButton(c.Key("send"), locales.Send()).FontSize(12).
				Disabled(a.host == "").OnClick(func() { a.sel = i; a.send(it) })
		}
	})
}

func (a *app) footer(c *ui.Context) {
	t := c.Theme()
	ui.Divider(c).Margin(0, 14)
	ui.Row(c).Padding(8, 14, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		status, color := a.status, t.TextMuted
		if status == "" && len(a.items) > 0 {
			status = locales.Hint()
		}
		if a.failed {
			color = t.Danger
		}
		ui.Text(c, status).Grow(1).FontSize(11).TextColor(color).MaxLines(1).Ellipsis("…").Tooltip(status)
		ui.Text(c, a.opts.shortcut().label).FontSize(11).TextColor(t.TextMuted).
			Tooltip(locales.Shortcut())
	})
}
