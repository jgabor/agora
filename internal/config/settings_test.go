package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fakePathEnv(vars map[string]string, home string) pathEnv {
	return pathEnv{
		lookupEnv: func(key string) (string, bool) {
			v, ok := vars[key]
			return v, ok
		},
		userHome: func() (string, error) {
			return home, nil
		},
	}
}

func TestLinuxXDGConfigHomeSettingsPath(t *testing.T) {
	env := fakePathEnv(map[string]string{"XDG_CONFIG_HOME": "/tmp/cfg"}, "/home/tester")
	dir, err := configDirFor("linux", env)
	if err != nil {
		t.Fatalf("configDirFor: %v", err)
	}

	got := filepath.Join(dir, "settings.yaml")
	want := filepath.Join("/tmp/cfg", "agora", "settings.yaml")
	if got != want {
		t.Fatalf("settings path: got %q, want %q", got, want)
	}
}

func TestLinuxXDGDataHomeTranscriptStoreDir(t *testing.T) {
	env := fakePathEnv(map[string]string{"XDG_DATA_HOME": "/tmp/data"}, "/home/tester")
	dir, err := dataDirFor("linux", env)
	if err != nil {
		t.Fatalf("dataDirFor: %v", err)
	}

	got := filepath.Join(dir, "transcripts")
	want := filepath.Join("/tmp/data", "agora", "transcripts")
	if got != want {
		t.Fatalf("transcript store dir: got %q, want %q", got, want)
	}
}

func TestMacOSConfigDirUsesApplicationSupport(t *testing.T) {
	env := fakePathEnv(nil, "/Users/tester")
	dir, err := configDirFor("darwin", env)
	if err != nil {
		t.Fatalf("configDirFor: %v", err)
	}

	want := filepath.Join("/Users/tester", "Library", "Application Support", "agora")
	if dir != want {
		t.Fatalf("macOS config dir: got %q, want %q", dir, want)
	}
}

func TestLoadDefaultSettingsUsesXDGConfigHomeOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME public path behavior is Linux-specific")
	}

	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	settingsDir := filepath.Join(cfgHome, "agora")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.yaml"), []byte(`default_model: "gpt-4"`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	settings, err := LoadDefaultSettings()
	if err != nil {
		t.Fatalf("LoadDefaultSettings: %v", err)
	}
	if settings.DefaultModel != "gpt-4" {
		t.Fatalf("DefaultModel: got %q, want %q", settings.DefaultModel, "gpt-4")
	}
}

func TestTranscriptStoreDirUsesXDGDataHomeOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_DATA_HOME public path behavior is Linux-specific")
	}

	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)
	dir, err := TranscriptStoreDir()
	if err != nil {
		t.Fatalf("TranscriptStoreDir: %v", err)
	}

	want := filepath.Join(dataHome, "agora", "transcripts")
	if dir != want {
		t.Fatalf("TranscriptStoreDir: got %q, want %q", dir, want)
	}
}

func TestLoadSettingsValidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte(`default_model: "gpt-4"`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.DefaultModel != "gpt-4" {
		t.Fatalf("DefaultModel: got %q, want %q", settings.DefaultModel, "gpt-4")
	}
}

func TestLoadSettingsMissingFileReturnsZeroValue(t *testing.T) {
	settings, err := LoadSettings(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadSettings missing file: %v", err)
	}
	if settings != (Settings{}) {
		t.Fatalf("settings: got %#v, want zero value", settings)
	}
}

func TestLoadSettingsInvalidYAMLReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.yaml")
	if err := os.WriteFile(path, []byte("default_model: [\n"), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	if _, err := LoadSettings(path); err == nil {
		t.Fatal("expected invalid YAML error")
	}
}

func TestSaveSettingsCreatesParentAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "settings.yaml")

	if err := SaveSettings(path, Settings{DefaultAutoLevel: "quick", ContextMaxDepth: 3}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	settings, err := LoadSettings(path)
	if err != nil {
		t.Fatalf("LoadSettings: %v", err)
	}
	if settings.DefaultAutoLevel != "quick" || settings.ContextMaxDepth != 3 {
		t.Fatalf("settings: got %#v", settings)
	}
}
