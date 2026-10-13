package main

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// Settings is the small set of inputs restored on the next launch so the
// program carries the last used address list and parameters (缓存).
type Settings struct {
	Targets     string `json:"targets"`
	RunMode     int    `json:"run_mode"`
	Count       int    `json:"count"`
	Duration    int    `json:"duration"`
	DurUnit     int    `json:"duration_unit"`
	IntervalMS  int    `json:"interval_ms"`
	TimeoutMS   int    `json:"timeout_ms"`
	Concurrency int    `json:"concurrency"`
	ThresholdMS int    `json:"threshold_ms"`
}

// settingsPath returns %AppData%\PingTool\settings.json on Windows (the
// platform-specific user config directory elsewhere).
func settingsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "PingTool", "settings.json"), nil
}

func readSettings() (*Settings, error) {
	p, err := settingsPath()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var s Settings
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

func writeSettings(s *Settings) error {
	p, err := settingsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, b, 0o644)
}
