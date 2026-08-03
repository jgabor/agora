package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/types"
	"github.com/spf13/cobra"
)

func workdirCommand(t *testing.T, value string, changed bool) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("workdir", "", "Workdir")
	if changed {
		if err := cmd.Flags().Set("workdir", value); err != nil {
			t.Fatalf("set workdir: %v", err)
		}
	}
	return cmd
}

func TestResolveWorkdirUsesConfigDirectoryAsRelativeBase(t *testing.T) {
	root := t.TempDir()
	configDir := filepath.Join(root, "config")
	projectDir := filepath.Join(root, "project")
	launchDir := filepath.Join(root, "launch")
	for _, dir := range []string{configDir, projectDir, launchDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
	}
	t.Chdir(launchDir)

	got, err := resolveWorkdir(workdirCommand(t, "", false), runFlagValues{}, &types.DeliberationConfig{Workdir: "../project"}, filepath.Join(configDir, "panel.yaml"))
	if err != nil {
		t.Fatalf("resolveWorkdir: %v", err)
	}
	if got != projectDir {
		t.Fatalf("workdir: got %q, want %q", got, projectDir)
	}
}

func TestResolveWorkdirCLIOverridesConfigFromLaunchDirectory(t *testing.T) {
	root := t.TempDir()
	launchDir := filepath.Join(root, "launch")
	projectDir := filepath.Join(launchDir, "project")
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	t.Chdir(launchDir)

	flags := runFlagValues{Workdir: "project"}
	got, err := resolveWorkdir(workdirCommand(t, "project", true), flags, &types.DeliberationConfig{Workdir: "missing"}, filepath.Join(root, "panel.yaml"))
	if err != nil {
		t.Fatalf("resolveWorkdir: %v", err)
	}
	if got != projectDir {
		t.Fatalf("workdir: got %q, want %q", got, projectDir)
	}
}

func TestResolveWorkdirRejectsMissingDirectory(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	_, err := resolveWorkdir(workdirCommand(t, "missing", true), runFlagValues{Workdir: "missing"}, nil, "")
	if err == nil || !strings.Contains(err.Error(), "resolving workdir") {
		t.Fatalf("error: got %v, want missing workdir", err)
	}
}

func TestResolveContextPathsUsesWorkdir(t *testing.T) {
	workdir := filepath.Join(t.TempDir(), "project")
	absolute := filepath.Join(t.TempDir(), "absolute.md")
	got := resolveContextPaths([]string{"README.md", absolute}, workdir)
	wantRelative := filepath.Join(workdir, "README.md")
	if len(got) != 2 || got[0] != wantRelative || got[1] != absolute {
		t.Fatalf("paths: got %#v, want [%q %q]", got, wantRelative, absolute)
	}
}

func TestConfigArtifactRootsIncludeXDGAndExcludeGlobalConfig(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	agoraDir := filepath.Join(xdg, "agora")
	if err := os.Mkdir(agoraDir, 0o755); err != nil {
		t.Fatalf("mkdir agora config: %v", err)
	}
	panel := filepath.Join(agoraDir, "panel.yaml")
	if err := os.WriteFile(panel, []byte("agents: []\n"), 0o644); err != nil {
		t.Fatalf("write panel: %v", err)
	}
	if err := os.WriteFile(filepath.Join(agoraDir, "config.yaml"), []byte("default_model: test\n"), 0o644); err != nil {
		t.Fatalf("write global config: %v", err)
	}

	got, err := resolveConfigArtifact("panel", configArtifactRoots())
	if err != nil {
		t.Fatalf("resolve panel: %v", err)
	}
	if got != panel {
		t.Fatalf("panel: got %q, want %q", got, panel)
	}
	if _, err := resolveConfigArtifact("config", configArtifactRoots()); err == nil {
		t.Fatal("global config.yaml must not resolve as a deliberation config")
	}
}
