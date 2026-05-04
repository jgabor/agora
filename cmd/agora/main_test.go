package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/cobra"
)

func TestApplyDefaultModelFromSettingsUsesSettingsWhenFlagOmitted(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	writeSettings(t, `default_model: "gpt-4"`)
	model := "opencode-go/deepseek-v4-flash"
	cmd := modelCommand(&model)

	if err := applyDefaultModelFromSettings(cmd, &model); err != nil {
		t.Fatalf("applyDefaultModelFromSettings: %v", err)
	}
	if model != "gpt-4" {
		t.Fatalf("model: got %q, want %q", model, "gpt-4")
	}
}

func TestApplyDefaultModelFromSettingsKeepsExplicitModelFlag(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	writeSettings(t, `default_model: "gpt-4"`)
	model := "opencode-go/deepseek-v4-flash"
	cmd := modelCommand(&model)
	if err := cmd.Flags().Set("model", "o1"); err != nil {
		t.Fatalf("set model flag: %v", err)
	}

	if err := applyDefaultModelFromSettings(cmd, &model); err != nil {
		t.Fatalf("applyDefaultModelFromSettings: %v", err)
	}
	if model != "o1" {
		t.Fatalf("model: got %q, want explicit flag value %q", model, "o1")
	}
}

func TestApplyDefaultModelFromSettingsReturnsInvalidSettingsError(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	writeSettings(t, "default_model: [\n")
	model := "opencode-go/deepseek-v4-flash"
	cmd := modelCommand(&model)

	if err := applyDefaultModelFromSettings(cmd, &model); err == nil {
		t.Fatal("expected invalid settings error")
	}
}

func TestApplySettingsDefaultsUsesDefaultAutoWhenFlagsOmitted(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	writeSettings(t, `default_auto_level: "normal"`)
	model := "opencode-go/deepseek-v4-flash"
	auto := ""
	cmd := settingsCommand(&model, &auto)

	if err := applySettingsDefaults(cmd, &model, &auto); err != nil {
		t.Fatalf("applySettingsDefaults: %v", err)
	}
	if auto != "normal" {
		t.Fatalf("auto: got %q, want %q", auto, "normal")
	}
}

func TestApplySettingsDefaultsKeepsExplicitAutoFlag(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	writeSettings(t, `default_auto_level: "normal"`)
	model := "opencode-go/deepseek-v4-flash"
	auto := ""
	cmd := settingsCommand(&model, &auto)
	if err := cmd.Flags().Set("auto", "quick"); err != nil {
		t.Fatalf("set auto flag: %v", err)
	}

	if err := applySettingsDefaults(cmd, &model, &auto); err != nil {
		t.Fatalf("applySettingsDefaults: %v", err)
	}
	if auto != "quick" {
		t.Fatalf("auto: got %q, want explicit flag value %q", auto, "quick")
	}
}

func TestApplySettingsDefaultsKeepsExplicitConfigOverDefaultAuto(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	writeSettings(t, `default_auto_level: "normal"`)
	model := "opencode-go/deepseek-v4-flash"
	auto := ""
	cmd := settingsCommand(&model, &auto)
	if err := cmd.Flags().Set("config", "example.yaml"); err != nil {
		t.Fatalf("set config flag: %v", err)
	}

	if err := applySettingsDefaults(cmd, &model, &auto); err != nil {
		t.Fatalf("applySettingsDefaults: %v", err)
	}
	if auto != "" {
		t.Fatalf("auto: got %q, want settings ignored because --config is explicit", auto)
	}
}

func writeSettings(t *testing.T, content string) {
	t.Helper()

	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	settingsDir := filepath.Join(cfgHome, "agora")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.yaml"), []byte(content), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}
}

func modelCommand(model *string) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.Flags().StringVarP(model, "model", "M", *model, "Model")
	return cmd
}

func settingsCommand(model, auto *string) *cobra.Command {
	cmd := modelCommand(model)
	cmd.Flags().StringVar(auto, "auto", "", "Auto")
	cmd.Flags().String("config", "", "Config")
	return cmd
}
