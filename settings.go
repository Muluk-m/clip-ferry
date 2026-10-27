package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// settings are what the app remembers across launches.
type settings struct {
	Host      string `json:"host"`
	RemoteDir string `json:"remoteDir"`
	KeepDays  int    `json:"keepDays"`
	// MaxEdge is the long edge images are sent at; -1 sends originals.
	MaxEdge  int  `json:"maxEdge"`
	AutoHide bool `json:"autoHide"`
}

func defaultSettings() settings {
	return settings{RemoteDir: "~/.cache/clip-share", KeepDays: 7, MaxEdge: claudeMaxEdge, AutoHide: true}
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
