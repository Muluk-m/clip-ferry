package main

import (
	"fmt"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"clip-ferry/locales"
)

func keepName(days int) string {
	if days == 0 {
		return locales.KeepForever()
	}
	return locales.KeepDays(days)
}

func edgeName(edge int) string {
	if edge < 0 {
		return locales.SizeOriginal()
	}
	return fmt.Sprintf("%d px", edge)
}

// choose shows a Select of values by their names and reports the one
// chosen.
func choose[T comparable](c *ui.Context, value T, values []T, name func(T) string) (T, bool) {
	names := make([]string, len(values))
	for i, v := range values {
		names[i] = name(v)
	}
	shown := name(value)
	if ui.Select(c, &shown, names).Width(180).Changed() {
		if i := slices.Index(names, shown); i >= 0 {
			return values[i], true
		}
	}
	return value, false
}

// settingsView is the settings window, a macOS form: labels on the right
// of their column, controls on the left of theirs.
func (a *app) settingsView(c *ui.Context) {
	c.OnShortcut(ui.Cmd, ui.KeyQ, a.quit)
	t := c.Theme()
	ui.Column(c).Fill().Padding(24, 28).Gap(16).Children(func() {
		ui.Form(c, func() {
			ui.Fieldset(c, locales.SectionHosts(), func() {
				gone := ""
				for _, h := range a.opts.Hosts {
					ui.Field(c, "", func() {
						ui.Row(c.Key(h)).Width(300).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Text(c, h).Grow(1).SingleLine().Ellipsis("…")
							ui.Toolbar(c, func() {
								if ui.Button(c, "").Label(locales.RemoveHost(h)).Children(func() {
									ui.Icon(c, iconX).FontSize(12)
								}).Clicked() {
									gone = h
								}
							})
						})
					})
				}
				if gone != "" {
					a.removeHost(gone)
				}
				ui.Field(c, locales.NewHost(), func() {
					ui.Row(c).Width(300).Gap(8).Children(func() {
						if ui.TextInput(c.Key("new-host"), &a.newHost).Grow(1).Placeholder(locales.HostPlaceholder()).Submitted() {
							a.addHost()
						}
						if ui.Button(c, locales.Add()).Disabled(strings.TrimSpace(a.newHost) == "").Clicked() {
							a.addHost()
						}
					})
				}).Description(locales.HostsHint()).Error(a.hostErr)
			})
			ui.Fieldset(c, locales.SectionSending(), func() {
				ui.Field(c, locales.RemoteFolder(), func() {
					if ui.TextInput(c.Key("dir"), &a.opts.RemoteDir).Width(240).Placeholder(defaultSettings().RemoteDir).Changed() {
						a.save()
					}
				}).Description(locales.RemoteFolderHint())
				ui.Field(c, locales.Keep(), func() {
					if v, ok := choose(c.Key("keep"), a.opts.KeepDays, keepOptions, keepName); ok {
						a.opts.KeepDays = v
						a.save()
					}
				})
				ui.Field(c, locales.ImageSize(), func() {
					if v, ok := choose(c.Key("edge"), a.opts.MaxEdge, edgeOptions, edgeName); ok {
						a.opts.MaxEdge = v
						a.save()
					}
				}).Description(locales.SizeHint())
				ui.Field(c, locales.AfterSend(), func() {
					if ui.Checkbox(c.Key("hide"), &a.opts.AutoHide, locales.ClosePanel()).Changed() {
						a.save()
					}
				})
			})
			ui.Fieldset(c, locales.SectionGeneral(), func() {
				ui.Field(c, locales.Shortcut(), func() {
					if k, ok := choose(c.Key("key"), a.opts.shortcut(), shortcuts, func(k shortcut) string { return k.label }); ok {
						a.chooseShortcut(k)
					}
				})
				ui.Field(c, locales.Language(), func() {
					if v, ok := choose(c.Key("lang"), a.opts.Language, languages, languageName); ok {
						a.chooseLanguage(v)
					}
				})
				ui.Field(c, locales.OpenAtLogin(), func() {
					on := a.atLogin
					if ui.Switch(c.Key("login"), &on).Label(locales.OpenAtLogin()).Changed() {
						a.setLogin(on)
					}
				})
			})
		})
		if a.failed {
			ui.Text(c, a.status).FontSize(11).TextColor(t.Danger)
		}
	})
}
