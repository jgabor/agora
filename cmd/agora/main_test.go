package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

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

func TestResolveTranscriptOutputKeepsExplicitOutput(t *testing.T) {
	model := "opencode-go/deepseek-v4-flash"
	auto := ""
	cmd := settingsCommand(&model, &auto)
	cmd.Flags().String("output", "", "Output")
	if err := cmd.Flags().Set("output", "custom.jsonl"); err != nil {
		t.Fatalf("set output flag: %v", err)
	}

	got, err := resolveTranscriptOutput(cmd, "custom.jsonl", "My Topic")
	if err != nil {
		t.Fatalf("resolveTranscriptOutput: %v", err)
	}
	if got != "custom.jsonl" {
		t.Fatalf("output: got %q, want %q", got, "custom.jsonl")
	}
}

func TestResearchOverridesCapturesRepeatedContext(t *testing.T) {
	cmd := &cobra.Command{}
	runContext = nil
	cmd.Flags().Bool("research", false, "Research")
	cmd.Flags().Bool("no-research", false, "No research")
	cmd.Flags().StringArrayVar(&runContext, "context", nil, "Context")
	if err := cmd.Flags().Set("context", "README.md"); err != nil {
		t.Fatalf("set first context: %v", err)
	}
	if err := cmd.Flags().Set("context", "examples"); err != nil {
		t.Fatalf("set second context: %v", err)
	}

	overrides := researchOverrides(cmd)
	if !overrides.ContextSet {
		t.Fatal("ContextSet: got false, want true")
	}
	want := []string{"README.md", "examples"}
	if len(overrides.ContextPaths) != len(want) || overrides.ContextPaths[0] != want[0] || overrides.ContextPaths[1] != want[1] {
		t.Fatalf("ContextPaths: got %#v, want %#v", overrides.ContextPaths, want)
	}
}

func TestResearchOverridesNoResearchDisablesConfigResearch(t *testing.T) {
	cmd := &cobra.Command{}
	runContext = nil
	cmd.Flags().Bool("research", false, "Research")
	cmd.Flags().Bool("no-research", false, "No research")
	cmd.Flags().StringArrayVar(&runContext, "context", nil, "Context")
	if err := cmd.Flags().Set("no-research", "true"); err != nil {
		t.Fatalf("set no-research: %v", err)
	}

	overrides := researchOverrides(cmd)
	if overrides.Research == nil {
		t.Fatal("Research override: got nil, want false pointer")
	}
	if *overrides.Research {
		t.Fatal("Research override: got true, want false")
	}
}

func TestResumeEvidenceRequestChangedRejectsResearchContextFlags(t *testing.T) {
	tests := []struct {
		name string
		flag string
		val  string
	}{
		{name: "research", flag: "research", val: "true"},
		{name: "no research", flag: "no-research", val: "true"},
		{name: "context", flag: "context", val: "README.md"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var contextPaths []string
			cmd := &cobra.Command{}
			cmd.Flags().Bool("research", false, "Research")
			cmd.Flags().Bool("no-research", false, "No research")
			cmd.Flags().StringArrayVar(&contextPaths, "context", nil, "Context")
			if err := cmd.Flags().Set(tt.flag, tt.val); err != nil {
				t.Fatalf("set %s: %v", tt.flag, err)
			}
			if !resumeEvidenceRequestChanged(cmd) {
				t.Fatalf("resumeEvidenceRequestChanged: got false, want true for --%s", tt.flag)
			}
		})
	}
}

func TestParseTranscriptFilename(t *testing.T) {
	entry, ok := parseTranscriptFilename("20260504-143022-my-topic.jsonl")
	if !ok {
		t.Fatal("expected transcript filename to parse")
	}
	if entry.date != time.Date(2026, 5, 4, 14, 30, 22, 0, time.UTC) {
		t.Fatalf("date: got %s", entry.date)
	}
	if entry.slug != "my-topic" {
		t.Fatalf("slug: got %q, want %q", entry.slug, "my-topic")
	}
}

func TestListTranscriptEntriesIgnoresNonJSONL(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "20260504-143022-my-topic.jsonl"), []byte("{}\n{}\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignore"), 0o644); err != nil {
		t.Fatalf("write notes: %v", err)
	}

	entries, err := listTranscriptEntries(dir)
	if err != nil {
		t.Fatalf("listTranscriptEntries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("entries: got %d, want 1", len(entries))
	}
	if entries[0].filename != "20260504-143022-my-topic.jsonl" || entries[0].turns != 2 {
		t.Fatalf("entry: got %#v", entries[0])
	}
}

func TestResolveResumeSourceFileFlag(t *testing.T) {
	got, err := resolveResumeSource("./my.jsonl", nil)
	if err != nil {
		t.Fatalf("resolveResumeSource: %v", err)
	}
	if got != "./my.jsonl" {
		t.Fatalf("source: got %q, want %q", got, "./my.jsonl")
	}
}

func TestResolveResumeSourceExistingPathWinsOverSlug(t *testing.T) {
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	if err := os.WriteFile(filepath.Join(store, "20260504-143022-my-topic.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write store transcript: %v", err)
	}

	cwdFile := filepath.Join(t.TempDir(), "my-topic")
	if err := os.WriteFile(cwdFile, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write cwd transcript: %v", err)
	}

	got, err := resolveResumeSource("", []string{cwdFile})
	if err != nil {
		t.Fatalf("resolveResumeSource: %v", err)
	}
	if got != cwdFile {
		t.Fatalf("source: got %q, want existing path %q", got, cwdFile)
	}
}

func TestResolveResumeSourceFindsLatestSlugMatch(t *testing.T) {
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	if err := os.WriteFile(filepath.Join(store, "20260504-143022-my-topic.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write old transcript: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store, "20260504-150000-my-topic-again.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write new transcript: %v", err)
	}

	got, err := resolveResumeSource("", []string{"my-topic"})
	if err != nil {
		t.Fatalf("resolveResumeSource: %v", err)
	}
	want := filepath.Join(store, "20260504-150000-my-topic-again.jsonl")
	if got != want {
		t.Fatalf("source: got %q, want %q", got, want)
	}
}

func TestResolveResumeSourceNoSlugMatch(t *testing.T) {
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")

	_, err := resolveResumeSource("", []string{"nonexistent"})
	if err == nil {
		t.Fatal("expected no matching transcript error")
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
