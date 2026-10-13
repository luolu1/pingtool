package main

import (
	"runtime"
	"testing"
)

func TestSettingsRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG_CONFIG_HOME is not used by os.UserConfigDir on Windows")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	in := &Settings{
		Targets:     "8.8.8.8\nwww.baidu.com:443 # 注释",
		RunMode:     2,
		Count:       50,
		Duration:    10,
		DurUnit:     1,
		IntervalMS:  1000,
		TimeoutMS:   2000,
		Concurrency: 100,
		ThresholdMS: 0,
	}
	if err := writeSettings(in); err != nil {
		t.Fatalf("writeSettings: %v", err)
	}
	out, err := readSettings()
	if err != nil {
		t.Fatalf("readSettings: %v", err)
	}
	if *out != *in {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", out, in)
	}
}

func TestReadSettingsMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("XDG_CONFIG_HOME is not used by os.UserConfigDir on Windows")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := readSettings(); err == nil {
		t.Fatal("expected an error when no settings file exists")
	}
}
