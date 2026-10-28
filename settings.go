package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// settings are what the app remembers across launches.
type settings struct {
	Host      string   `json:"host"`
	Hosts     []string `json:"hosts"` // added by hand, beside ~/.ssh/config's
	RemoteDir string   `json:"remoteDir"`
	KeepDays  int      `json:"keepDays"`
	// MaxEdge is the long edge images are sent at; -1 sends originals.
	MaxEdge  int    `json:"maxEdge"`
	AutoSync bool   `json:"autoSync"` // send new clipboard images to the selected host
	AutoHide bool   `json:"autoHide"` // close the panel once the path is copied
	Shortcut string `json:"shortcut"` // one of shortcuts
	Language string `json:"language"` // "" follows the system
}

// shortcut is a key that opens the panel, as mygo.GlobalShortcut names it
// and as macOS menus show it.
type shortcut struct{ accel, label string }

// shortcuts are the keys to choose from. Raycast and Spotlight leave them
// free, and Finder's Cmd+Alt+V moves files.
var shortcuts = []shortcut{
	{"Ctrl+Alt+V", "⌃⌥V"},
	{"Ctrl+Shift+V", "⌃⇧V"},
	{"Alt+Shift+V", "⌥⇧V"},
	{"Ctrl+Alt+Space", "⌃⌥Space"},
}

// keepOptions and edgeOptions are the retention and image sizes the
// settings offer.
var (
	keepOptions = []int{1, 7, 30, 0}
	edgeOptions = []int{claudeMaxEdge, 2560, -1}
)

func (s settings) shortcut() shortcut {
	for _, k := range shortcuts {
		if k.accel == s.Shortcut {
			return k
		}
	}
	return shortcuts[0]
}

func defaultSettings() settings {
	return settings{RemoteDir: "~/.cache/ferry", KeepDays: 7, MaxEdge: claudeMaxEdge, AutoHide: true, Shortcut: shortcuts[0].accel}
}

func (s settings) sendOptions() sendOptions {
	o := sendOptions{dir: s.RemoteDir, keepDays: s.KeepDays, maxEdge: max(s.MaxEdge, 0)}
	if o.dir == "" {
		o.dir = defaultSettings().RemoteDir
	}
	return o
}

func settingsPath(dir string) string { return filepath.Join(dir, "settings.json") }

func loadSettings(dir string) settings {
	s := defaultSettings()
	if data, err := os.ReadFile(settingsPath(dir)); err == nil {
		_ = json.Unmarshal(data, &s)
	}
	return s
}

func saveSettings(dir string, s settings) error {
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(settingsPath(dir), data, 0o600)
}
