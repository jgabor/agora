package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/types"
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

func TestConfigSetGetRoundTrip(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)

	if _, err := executeConfigCommand(t, "set", "default_auto_level", "quick"); err != nil {
		t.Fatalf("config set: %v", err)
	}
	out, err := executeConfigCommand(t, "get", "default_auto_level")
	if err != nil {
		t.Fatalf("config get: %v", err)
	}
	if out != "quick\n" {
		t.Fatalf("config get output: got %q, want quick", out)
	}

	settings, err := config.LoadDefaultSettings()
	if err != nil {
		t.Fatalf("LoadDefaultSettings: %v", err)
	}
	if settings.DefaultAutoLevel != "quick" {
		t.Fatalf("DefaultAutoLevel: got %q, want quick", settings.DefaultAutoLevel)
	}
}

func TestConfigGetAllShowsDefaults(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	out, err := executeConfigCommand(t, "get", "--all")
	if err != nil {
		t.Fatalf("config get --all: %v", err)
	}
	for _, want := range []string{
		"path: " + filepath.Join(cfgHome, "agora", "settings.yaml"),
		"default_model:         " + defaultModel + " (default)",
		"default_auto_level:    (unset)",
		"default_topology:      ring (default)",
		"research_max_sources:  20 (default)",
		"context_max_bytes:     1048576 (default)",
		"context_max_depth:     5 (default)",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("config get --all missing %q:\n%s", want, out)
		}
	}
}

func TestConfigSetRejectsInvalidAutoLevel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	_, err := executeConfigCommand(t, "set", "default_auto_level", "off")
	if err == nil {
		t.Fatal("expected invalid auto level error")
	}
	if !strings.Contains(err.Error(), "quick, normal, deep, yolo") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigInitCreatesDefaultSettings(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	dataHome := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dataHome)

	out, err := executeConfigCommand(t, "init")
	if err != nil {
		t.Fatalf("config init: %v", err)
	}
	settingsPath := filepath.Join(cfgHome, "agora", "settings.yaml")
	if !strings.Contains(out, settingsPath) {
		t.Fatalf("config init output missing path %q:\n%s", settingsPath, out)
	}

	settings, err := config.LoadDefaultSettings()
	if err != nil {
		t.Fatalf("LoadDefaultSettings: %v", err)
	}
	wantOutputDir := filepath.Join(dataHome, "agora", "transcripts")
	if settings.DefaultModel != defaultModel || settings.DefaultTopology != "ring" || settings.DefaultOutputDir != wantOutputDir {
		t.Fatalf("default settings: got %#v", settings)
	}
	if settings.DefaultAutoLevel != "" {
		t.Fatalf("DefaultAutoLevel: got %q, want unset", settings.DefaultAutoLevel)
	}
	if settings.ResearchMaxSources != 20 || settings.ContextMaxBytes != 1048576 || settings.ContextMaxDepth != 5 {
		t.Fatalf("evidence defaults: got %#v", settings)
	}
}

func TestConfigInitRefusesExistingSettings(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := executeConfigCommand(t, "init"); err != nil {
		t.Fatalf("config init: %v", err)
	}
	_, err := executeConfigCommand(t, "init")
	if err == nil {
		t.Fatal("expected existing settings error")
	}
	if !strings.Contains(err.Error(), "settings already exists") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestConfigInitForceOverwritesExistingSettings(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}

	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if _, err := executeConfigCommand(t, "set", "default_model", "custom-model"); err != nil {
		t.Fatalf("config set: %v", err)
	}
	if _, err := executeConfigCommand(t, "init", "--force"); err != nil {
		t.Fatalf("config init --force: %v", err)
	}

	settings, err := config.LoadDefaultSettings()
	if err != nil {
		t.Fatalf("LoadDefaultSettings: %v", err)
	}
	if settings.DefaultModel != defaultModel {
		t.Fatalf("DefaultModel: got %q, want %q", settings.DefaultModel, defaultModel)
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

func TestApplyAutoLevelCapsKeepsExplicitRunLimits(t *testing.T) {
	cmd := autoCapsCommand(t)
	if err := cmd.Flags().Set("time", "1200"); err != nil {
		t.Fatalf("set time: %v", err)
	}
	if err := cmd.Flags().Set("max-turns", "50"); err != nil {
		t.Fatalf("set max-turns: %v", err)
	}
	state := &types.DeliberationState{TimeLimit: 1200, MaxTurns: 50}

	applyAutoLevelCaps(cmd, state, types.CapsForLevel(types.AutoDeep), 0)

	if state.TimeLimit != 1200 || state.MaxTurns != 50 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want explicit time=1200 maxTurns=50", state.TimeLimit, state.MaxTurns)
	}
}

func TestApplyAutoLevelCapsAppliesRunDefaults(t *testing.T) {
	cmd := autoCapsCommand(t)
	state := &types.DeliberationState{TimeLimit: 60, MaxTurns: 10}

	applyAutoLevelCaps(cmd, state, types.CapsForLevel(types.AutoDeep), 0)

	if state.TimeLimit != 900 || state.MaxTurns != 20 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want deep defaults time=900 maxTurns=20", state.TimeLimit, state.MaxTurns)
	}
}

func TestApplyAutoLevelCapsKeepsExplicitResumeLimits(t *testing.T) {
	cmd := autoCapsCommand(t)
	if err := cmd.Flags().Set("time", "1200"); err != nil {
		t.Fatalf("set time: %v", err)
	}
	if err := cmd.Flags().Set("max-turns", "50"); err != nil {
		t.Fatalf("set max-turns: %v", err)
	}
	state := &types.DeliberationState{TimeLimit: 1200, MaxTurns: 57}

	applyAutoLevelCaps(cmd, state, types.CapsForLevel(types.AutoDeep), 7)

	if state.TimeLimit != 1200 || state.MaxTurns != 57 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want explicit resume time=1200 maxTurns=57", state.TimeLimit, state.MaxTurns)
	}
}

func TestApplyAutoLevelCapsAddsResumeDefaultsToExistingTurns(t *testing.T) {
	cmd := autoCapsCommand(t)
	state := &types.DeliberationState{TimeLimit: 60, MaxTurns: 17}

	applyAutoLevelCaps(cmd, state, types.CapsForLevel(types.AutoDeep), 7)

	if state.TimeLimit != 900 || state.MaxTurns != 27 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want deep resume defaults time=900 maxTurns=27", state.TimeLimit, state.MaxTurns)
	}
}

func TestApplyAutoLevelCapsPreservesYOLOUnlimitedDefault(t *testing.T) {
	cmd := autoCapsCommand(t)
	state := &types.DeliberationState{TimeLimit: 60, MaxTurns: 17}

	applyAutoLevelCaps(cmd, state, types.CapsForLevel(types.AutoYOLO), 7)

	if state.TimeLimit != 0 || state.MaxTurns != 0 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want yolo unlimited defaults", state.TimeLimit, state.MaxTurns)
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

func executeConfigCommand(t *testing.T, args ...string) (string, error) {
	t.Helper()

	cmd := newConfigCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return out.String(), err
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

func autoCapsCommand(t *testing.T) *cobra.Command {
	t.Helper()

	cmd := &cobra.Command{}
	cmd.Flags().Int("time", 60, "Time")
	cmd.Flags().Int("max-turns", 10, "Max turns")
	return cmd
}
