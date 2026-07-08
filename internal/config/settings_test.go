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

func TestLinuxXDGConfigHomeGlobalConfigPath(t *testing.T) {
	env := fakePathEnv(map[string]string{"XDG_CONFIG_HOME": "/tmp/cfg"}, "/home/tester")
	dir, err := configDirFor("linux", env)
	if err != nil {
		t.Fatalf("configDirFor: %v", err)
	}

	got := filepath.Join(dir, "config.yaml")
	want := filepath.Join("/tmp/cfg", "agora", "config.yaml")
	if got != want {
		t.Fatalf("config path: got %q, want %q", got, want)
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

func TestLoadDefaultGlobalConfigUsesXDGConfigHomeOnLinux(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME public path behavior is Linux-specific")
	}

	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	cfgDir := filepath.Join(cfgHome, "agora")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatalf("mkdir gconf dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "config.yaml"), []byte(`default_model: "gpt-4"`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	gconf, err := LoadDefaultGlobalConfig()
	if err != nil {
		t.Fatalf("LoadDefaultGlobalConfig: %v", err)
	}
	if gconf.DefaultModel != "gpt-4" {
		t.Fatalf("DefaultModel: got %q, want %q", gconf.DefaultModel, "gpt-4")
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

func TestLoadGlobalConfigValidYAML(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(`default_model: "gpt-4"`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	gconf, err := LoadGlobalConfig(path)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if gconf.DefaultModel != "gpt-4" {
		t.Fatalf("DefaultModel: got %q, want %q", gconf.DefaultModel, "gpt-4")
	}
}

func TestLoadGlobalConfigMissingFileReturnsZeroValue(t *testing.T) {
	gconf, err := LoadGlobalConfig(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("LoadGlobalConfig missing file: %v", err)
	}
	if gconf != (Config{}) {
		t.Fatalf("config: got %#v, want zero value", gconf)
	}
}

func TestLoadGlobalConfigInvalidYAMLReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte("default_model: [\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	if _, err := LoadGlobalConfig(path); err == nil {
		t.Fatal("expected invalid YAML error")
	}
}

func TestSaveGlobalConfigCreatesParentAndRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.yaml")

	if err := SaveGlobalConfig(path, Config{DefaultAutoLevel: "quick", ContextMaxDepth: 3}); err != nil {
		t.Fatalf("SaveGlobalConfig: %v", err)
	}

	gconf, err := LoadGlobalConfig(path)
	if err != nil {
		t.Fatalf("LoadGlobalConfig: %v", err)
	}
	if gconf.DefaultAutoLevel != "quick" || gconf.ContextMaxDepth != 3 {
		t.Fatalf("config: got %#v", gconf)
	}
}
