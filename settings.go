package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// settings are what the app remembers across launches.
type settings struct {
	Host string `json:"host"`
}

func settingsPath(dir string) string { return filepath.Join(dir, "settings.json") }

func loadSettings(dir string) settings {
	var s settings
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
