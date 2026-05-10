package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/output"
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
		"Global Settings",
		"settings.yaml",
		"default_model",
		"opencode-go/deepseek-",
		"v4-flash",
		"default_auto_level",
		"(unset)",
		"default_topology",
		"ring",
		"research_max_sources",
		"20",
		"context_max_bytes",
		"1048576",
		"context_max_depth",
		"5",
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
	if !strings.Contains(out, "Config Initialized") || !strings.Contains(out, "Path") {
		t.Fatalf("config init output missing status/path %q:\n%s", settingsPath, out)
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

func TestRunEvidenceOverridesAddsAutoDefaults(t *testing.T) {
	cmd := &cobra.Command{}
	runContext = nil
	cmd.Flags().Bool("research", false, "Research")
	cmd.Flags().Bool("no-research", false, "No research")
	cmd.Flags().StringArrayVar(&runContext, "context", nil, "Context")

	overrides := runEvidenceOverrides(cmd, true, types.AutoNormal)

	want := config.EvidenceDefaultsForAutoLevel(types.AutoNormal)
	if overrides.Defaults != want {
		t.Fatalf("Defaults: got %+v, want %+v", overrides.Defaults, want)
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

func TestListCommandShowsFilenameOnlyWhenVerbose(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	t.Setenv("COLUMNS", "120")
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	filename := "20260504-143022-my-topic.jsonl"
	if err := os.WriteFile(filepath.Join(store, filename), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	oldVerbose := listVerbose
	t.Cleanup(func() { listVerbose = oldVerbose })

	listVerbose = false
	normal := runListCommand(t)
	assertStringContains(t, normal, "my-topic")
	assertStringNotContains(t, normal, filename)

	listVerbose = true
	verbose := runListCommand(t)
	assertStringContains(t, verbose, "my-topic")
	assertStringContains(t, verbose, filename)
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

func TestResolveResumeSourceExactSlugWinsBeforeNewerPrefixMatch(t *testing.T) {
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	if err := os.WriteFile(filepath.Join(store, "20260504-143022-my-topic.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write exact transcript: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store, "20260504-150000-my-topic-again.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write prefix transcript: %v", err)
	}

	got, err := resolveResumeSource("", []string{"my-topic"})
	if err != nil {
		t.Fatalf("resolveResumeSource: %v", err)
	}
	want := filepath.Join(store, "20260504-143022-my-topic.jsonl")
	if got != want {
		t.Fatalf("source: got %q, want %q", got, want)
	}
}

func TestResolveResumeSourcePrefixWinsBeforeNewerSubstringMatch(t *testing.T) {
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	if err := os.WriteFile(filepath.Join(store, "20260504-143022-topic-alpha.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write prefix transcript: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store, "20260504-150000-my-topic-again.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write substring transcript: %v", err)
	}

	got, err := resolveResumeSource("", []string{"topic"})
	if err != nil {
		t.Fatalf("resolveResumeSource: %v", err)
	}
	want := filepath.Join(store, "20260504-143022-topic-alpha.jsonl")
	if got != want {
		t.Fatalf("source: got %q, want %q", got, want)
	}
}

func TestResolveResumeSourceNewestWithinMatchTier(t *testing.T) {
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	if err := os.WriteFile(filepath.Join(store, "20260504-143022-my-topic.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write old transcript: %v", err)
	}
	if err := os.WriteFile(filepath.Join(store, "20260504-150000-my-topic.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write new transcript: %v", err)
	}

	got, err := resolveResumeSource("", []string{"my-topic"})
	if err != nil {
		t.Fatalf("resolveResumeSource: %v", err)
	}
	want := filepath.Join(store, "20260504-150000-my-topic.jsonl")
	if got != want {
		t.Fatalf("source: got %q, want %q", got, want)
	}
}

func TestResolveResumeSourceMissingPathLikeInputReportsPath(t *testing.T) {
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	if err := os.WriteFile(filepath.Join(store, "20260504-143022-missing.jsonl.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write slug transcript: %v", err)
	}

	_, err := resolveResumeSource("", []string{"missing.jsonl"})
	if err == nil || !strings.Contains(err.Error(), "transcript path not found: missing.jsonl") {
		t.Fatalf("error: got %v, want missing transcript path", err)
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

func TestResolveConfigArtifactExistingPathWins(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "demo.yaml")
	if err := os.WriteFile(path, []byte("agents: []\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := resolveConfigArtifact(path, []string{filepath.Join(dir, "examples")})
	if err != nil {
		t.Fatalf("resolveConfigArtifact: %v", err)
	}
	if got != path {
		t.Fatalf("config path: got %q, want %q", got, path)
	}
}

func TestResolveConfigArtifactFindsStemInSearchRoots(t *testing.T) {
	dir := t.TempDir()
	examples := filepath.Join(dir, "examples")
	if err := os.Mkdir(examples, 0o755); err != nil {
		t.Fatalf("mkdir examples: %v", err)
	}
	want := filepath.Join(examples, "quick.yaml")
	if err := os.WriteFile(want, []byte("agents: []\n"), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := resolveConfigArtifact("quick", []string{dir, examples})
	if err != nil {
		t.Fatalf("resolveConfigArtifact: %v", err)
	}
	if got != want {
		t.Fatalf("config path: got %q, want %q", got, want)
	}
}

func TestResolveConfigArtifactMissingPathLikeInputReportsPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "missing.yaml"), []byte("agents: []\n"), 0o644); err != nil {
		t.Fatalf("write slug config: %v", err)
	}

	_, err := resolveConfigArtifact("missing.yml", []string{dir})
	if err == nil || !strings.Contains(err.Error(), "config path not found: missing.yml") {
		t.Fatalf("error: got %v, want missing config path", err)
	}
}

func TestResolveConfigArtifactReportsCollision(t *testing.T) {
	dir := t.TempDir()
	examples := filepath.Join(dir, "examples")
	if err := os.Mkdir(examples, 0o755); err != nil {
		t.Fatalf("mkdir examples: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "quick.yaml"), []byte("agents: []\n"), 0o644); err != nil {
		t.Fatalf("write cwd config: %v", err)
	}
	if err := os.WriteFile(filepath.Join(examples, "quick.yml"), []byte("agents: []\n"), 0o644); err != nil {
		t.Fatalf("write examples config: %v", err)
	}

	_, err := resolveConfigArtifact("quick", []string{dir, examples})
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("error: got %v, want ambiguous config slug", err)
	}
}

func TestRunCommandResolvesConfigExistingPath(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	if err := os.Mkdir(filepath.Join(dir, "examples"), 0o755); err != nil {
		t.Fatalf("mkdir examples: %v", err)
	}
	path := filepath.Join(dir, "demo.yaml")
	writeValidConfig(t, path)
	writeInvalidConfig(t, filepath.Join(dir, "examples", "demo.yaml"))
	t.Chdir(dir)

	restore := configureRunGlobals(path, filepath.Join(dir, "run.jsonl"))
	defer restore()

	cmd := artifactCommand(t, filepath.Join(dir, "run.jsonl"))
	if err := runCmd.RunE(cmd, nil); err != nil {
		t.Fatalf("run command: %v", err)
	}
}

func TestRunCommandReportsMissingPathLikeConfig(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	writeValidConfig(t, filepath.Join(dir, "missing.yaml"))
	t.Chdir(dir)

	restore := configureRunGlobals("missing.yml", filepath.Join(dir, "run.jsonl"))
	defer restore()

	cmd := artifactCommand(t, filepath.Join(dir, "run.jsonl"))
	err := runCmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "config path not found: missing.yml") {
		t.Fatalf("error: got %v, want missing config path", err)
	}
}

func TestRunCommandDefaultShowsResponseAndQuietHidesIt(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "")
	configPath := filepath.Join(dir, "config.yaml")
	writeValidConfig(t, configPath)
	t.Chdir(dir)

	restore := configureRunGlobals(configPath, filepath.Join(dir, "run-normal.jsonl"))
	normal := captureStdout(t, func() {
		if err := runCmd.RunE(artifactCommand(t, filepath.Join(dir, "run-normal.jsonl")), nil); err != nil {
			t.Fatalf("run normal: %v", err)
		}
	})
	restore()

	restore = configureRunGlobals(configPath, filepath.Join(dir, "run-quiet.jsonl"))
	runQuiet = true
	quiet := captureStdout(t, func() {
		if err := runCmd.RunE(artifactCommand(t, filepath.Join(dir, "run-quiet.jsonl")), nil); err != nil {
			t.Fatalf("run quiet: %v", err)
		}
	})
	restore()

	assertStringContains(t, normal, "AGENT CONTENT")
	assertStringContains(t, normal, "[DRY RUN] Agent 'a' responds")
	assertStringContains(t, quiet, "Deliberation complete")
	assertStringNotContains(t, quiet, "AGENT CONTENT")
	assertStringNotContains(t, quiet, "[DRY RUN] Agent 'a' responds")
}

func TestRunCommandPersistsTranscriptCastMetadata(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "")
	configPath := filepath.Join(dir, "config.yaml")
	writeValidConfig(t, configPath)
	t.Chdir(dir)

	outputPath := filepath.Join(dir, "run.jsonl")
	restore := configureRunGlobals(configPath, outputPath)
	defer restore()

	if err := runCmd.RunE(artifactCommand(t, outputPath), nil); err != nil {
		t.Fatalf("run command: %v", err)
	}
	records, err := loadTranscriptFile(outputPath)
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	if len(records) == 0 || records[0].Transcript == nil {
		t.Fatalf("first record transcript metadata missing: %#v", records)
	}
	metadata := records[0].Transcript
	if metadata.SchemaVersion != 1 || metadata.Config == nil || len(metadata.Cast) != 1 {
		t.Fatalf("metadata: %#v", metadata)
	}
	member := metadata.Cast[0]
	if member.ID != 1 || member.Name != "Solon" || member.Persona != "a" || member.ProviderModel != "test/model" || member.Color != "6" {
		t.Fatalf("cast member: %#v", member)
	}
	if len(records) > 1 && records[1].Transcript != nil {
		t.Fatalf("transcript metadata should only be embedded on first record: %#v", records[1].Transcript)
	}
}

func TestValidateCommandResolvesConfigSlug(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	if err := os.Mkdir(filepath.Join(dir, "examples"), 0o755); err != nil {
		t.Fatalf("mkdir examples: %v", err)
	}
	writeValidConfig(t, filepath.Join(dir, "examples", "quick.yaml"))
	t.Chdir(dir)

	if err := validateCmd.RunE(&cobra.Command{}, []string{"quick"}); err != nil {
		t.Fatalf("validate command: %v", err)
	}
}

func TestValidateCommandKeepsExplicitPathCompatibility(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	if err := os.Mkdir(filepath.Join(dir, "examples"), 0o755); err != nil {
		t.Fatalf("mkdir examples: %v", err)
	}
	path := filepath.Join(dir, "demo.yaml")
	writeValidConfig(t, path)
	writeInvalidConfig(t, filepath.Join(dir, "examples", "demo.yaml"))
	t.Chdir(dir)

	if err := validateCmd.RunE(&cobra.Command{}, []string{path}); err != nil {
		t.Fatalf("validate command: %v", err)
	}
}

func TestValidateCommandReportsAmbiguousConfigSlug(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	examples := filepath.Join(dir, "examples")
	if err := os.Mkdir(examples, 0o755); err != nil {
		t.Fatalf("mkdir examples: %v", err)
	}
	writeValidConfig(t, filepath.Join(dir, "quick.yaml"))
	writeValidConfig(t, filepath.Join(examples, "quick.yml"))
	t.Chdir(dir)

	err := validateCmd.RunE(&cobra.Command{}, []string{"quick"})
	if err == nil {
		t.Fatal("expected ambiguous config slug error")
	}
	assertStringContains(t, err.Error(), "config slug \"quick\" is ambiguous")
	assertStringContains(t, err.Error(), "quick.yaml")
	assertStringContains(t, err.Error(), filepath.Join("examples", "quick.yml"))
}

func TestValidateCommandReportsMissingPathLikeConfig(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	writeValidConfig(t, filepath.Join(dir, "missing.yaml"))
	t.Chdir(dir)

	err := validateCmd.RunE(&cobra.Command{}, []string{"missing.yml"})
	if err == nil || !strings.Contains(err.Error(), "config path not found: missing.yml") {
		t.Fatalf("error: got %v, want missing config path", err)
	}
}

func TestValidateCommandUsageIncludesConfigOrSlug(t *testing.T) {
	if validateCmd.Use != "validate CONFIG|SLUG" {
		t.Fatalf("validate usage: got %q, want CONFIG|SLUG", validateCmd.Use)
	}
}

func TestResumeCommandResolvesConfigExistingPath(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "demo.yaml")
	writeValidConfig(t, path)
	source := filepath.Join(dir, "source.jsonl")
	writeResumeTranscript(t, source)
	t.Chdir(dir)

	restore := configureResumeGlobals(path, source, filepath.Join(dir, "resume.jsonl"))
	defer restore()

	cmd := artifactCommand(t, filepath.Join(dir, "resume.jsonl"))
	if err := resumeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf("resume command: %v", err)
	}
}

func TestResumeCommandReportsMissingPathLikeConfig(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	writeValidConfig(t, filepath.Join(dir, "missing.yaml"))
	source := filepath.Join(dir, "source.jsonl")
	writeResumeTranscript(t, source)
	t.Chdir(dir)

	restore := configureResumeGlobals("missing.yml", source, filepath.Join(dir, "resume.jsonl"))
	defer restore()

	cmd := artifactCommand(t, filepath.Join(dir, "resume.jsonl"))
	err := resumeCmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "config path not found: missing.yml") {
		t.Fatalf("error: got %v, want missing config path", err)
	}
}

func TestResumeCommandDefaultShowsResponseAndVerboseAddsDiagnostics(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "")
	configPath := filepath.Join(dir, "config.yaml")
	writeValidConfig(t, configPath)
	source := filepath.Join(dir, "source.jsonl")
	writeResumeTranscript(t, source)
	t.Chdir(dir)

	restore := configureResumeGlobals(configPath, source, filepath.Join(dir, "resume-normal.jsonl"))
	resumeMaxTurns = 1
	normal := captureStdout(t, func() {
		if err := resumeCmd.RunE(artifactCommand(t, filepath.Join(dir, "resume-normal.jsonl")), nil); err != nil {
			t.Fatalf("resume normal: %v", err)
		}
	})
	restore()

	restore = configureResumeGlobals(configPath, source, filepath.Join(dir, "resume-verbose.jsonl"))
	resumeMaxTurns = 1
	resumeVerbose = true
	verbose := captureStdout(t, func() {
		if err := resumeCmd.RunE(artifactCommand(t, filepath.Join(dir, "resume-verbose.jsonl")), nil); err != nil {
			t.Fatalf("resume verbose: %v", err)
		}
	})
	restore()

	assertStringContains(t, normal, "AGENT CONTENT")
	assertStringContains(t, normal, "[DRY RUN] Agent 'a' responds")
	assertStringNotContains(t, normal, "DIAGNOSTICS")
	assertStringContains(t, verbose, "AGENT CONTENT")
	assertStringContains(t, verbose, "DIAGNOSTICS")
	assertStringContains(t, verbose, "INPUT_TOKENS 50")
	assertStringContains(t, verbose, "OUTPUT_TOKENS 50")
}

func TestStatsCommandResolvesTranscriptSlug(t *testing.T) {
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	if err := os.WriteFile(filepath.Join(store, "20260504-143022-topic.jsonl"), []byte(transcriptLine("a", "stats", 7)), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	if err := statsCmd.RunE(&cobra.Command{}, []string{"topic"}); err != nil {
		t.Fatalf("stats command: %v", err)
	}
}

func TestStatsCommandReportsMalformedTranscriptRecord(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLine("a", "ok", 1)+"not-json\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	err := statsCmd.RunE(&cobra.Command{}, []string{path})
	if err == nil || !strings.Contains(err.Error(), "malformed transcript record") {
		t.Fatalf("error: got %v, want malformed transcript record", err)
	}
}

func TestShowCommandResolvesTranscriptSlug(t *testing.T) {
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	path := filepath.Join(store, "20260504-143022-topic.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLine("analyst", "slug answer", 3)), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	out, err := executeShowCommand(t, "topic")
	if err != nil {
		t.Fatalf("show command: %v", err)
	}
	for _, want := range []string{"TURN 1/1", "AGENT [A1 analyst]", "NAME Solon", "PERSONA analyst", "AGENT CONTENT", "slug answer"} {
		if !strings.Contains(out, want) {
			t.Fatalf("show output missing %q:\n%s", want, out)
		}
	}
}

func TestShowCommandAcceptsTranscriptPath(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "path.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLine("path-agent", "path answer", 3)), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	out, err := executeShowCommand(t, path)
	if err != nil {
		t.Fatalf("show command: %v", err)
	}
	if !strings.Contains(out, "AGENT [A1 path-agent]") || !strings.Contains(out, "path answer") {
		t.Fatalf("show output missing path transcript content:\n%s", out)
	}
}

func TestShowCommandReportsMalformedTranscriptRecord(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(path, []byte(transcriptLine("a", "ok", 1)+"not-json\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	_, err := executeShowCommand(t, path)
	if err == nil || !strings.Contains(err.Error(), "malformed transcript record") {
		t.Fatalf("error: got %v, want malformed transcript record", err)
	}
}

func TestShowCommandRendersReadableTurnsInRecordOrder(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "show.jsonl")
	model := "test/model"
	metadata := types.NewTranscriptMetadata(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: []types.AgentConfig{
		{ID: "analyst", Model: model},
		{ID: "critic", Model: model},
	}})
	content := transcriptContent(t,
		types.TurnRecord{Turn: -1, AgentID: "orchestrator", Timestamp: 1, Transcript: metadata, Evidence: &types.EvidenceBundle{Summary: "Two sources found", SourceReferences: []types.SourceReference{{Title: "Spec", URL: "https://example.test/spec"}, {Path: "README.md"}}}},
		types.TurnRecord{Turn: 0, AgentID: "analyst", Model: &model, Timestamp: 2, Content: "First answer"},
		types.TurnRecord{Turn: 1, AgentID: "critic", Model: &model, Timestamp: 3, Content: "Fallback answer", Consensus: true, ConsensusStatement: "We agree"},
	)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	out, err := executeShowCommand(t, path)
	if err != nil {
		t.Fatalf("show command: %v", err)
	}
	for _, want := range []string{
		"Transcript Evidence",
		"RECORD 1",
		"TURN -1",
		"AGENT orchestrator",
		"Evidence Summary",
		"Two sources found",
		"1. Spec (https://example.test/spec)",
		"2. README.md (README.md)",
		"TURN 1/2 (50%)",
		"AGENT [A1 analyst]",
		"NAME Solon",
		"PERSONA analyst",
		"MODEL test/model",
		"AGENT CONTENT",
		"First answer",
		"TURN 2/2 (100%)",
		"AGENT [A2 critic]",
		"NAME Aspasia",
		"PERSONA critic",
		"Fallback answer",
		"[CONSENSUS] We agree",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("show output missing %q:\n%s", want, out)
		}
	}
	if strings.Index(out, "Transcript Evidence") > strings.Index(out, "TURN 1/2") || strings.Index(out, "TURN 1/2") > strings.Index(out, "TURN 2/2") {
		t.Fatalf("show output changed record order:\n%s", out)
	}
}

func TestShowCommandReportsEmptyTranscript(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(path, []byte("\n\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	_, err := executeShowCommand(t, path)
	if err == nil || !strings.Contains(err.Error(), "transcript empty") {
		t.Fatalf("error: got %v, want transcript empty", err)
	}
}

func TestResumeCommandResolvesTranscriptSlugToNewestMatch(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	if err := os.Mkdir(store, 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}
	writeSettings(t, "default_output_dir: \""+store+"\"")
	writeValidConfig(t, filepath.Join(dir, "config.yaml"))
	oldPath := filepath.Join(store, "20260504-143022-topic.jsonl")
	newPath := filepath.Join(store, "20260504-150000-topic.jsonl")
	if err := os.WriteFile(oldPath, []byte(transcriptLine("a", "old", 1)), 0o644); err != nil {
		t.Fatalf("write old transcript: %v", err)
	}
	if err := os.WriteFile(newPath, []byte(transcriptLine("a", "new", 1)), 0o644); err != nil {
		t.Fatalf("write new transcript: %v", err)
	}
	t.Chdir(dir)

	outputPath := filepath.Join(dir, "resume.jsonl")
	restore := configureResumeGlobals(filepath.Join(dir, "config.yaml"), "", outputPath)
	defer restore()

	cmd := artifactCommand(t, outputPath)
	if err := resumeCmd.RunE(cmd, []string{"topic"}); err != nil {
		t.Fatalf("resume command: %v", err)
	}
	records, err := loadTranscriptFile(outputPath)
	if err != nil {
		t.Fatalf("load output transcript: %v", err)
	}
	if len(records) != 1 || records[0].Content != "new" {
		t.Fatalf("copied records: got %#v, want newest transcript content", records)
	}
}

func TestResumeCommandReportsMalformedTranscriptRecord(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	writeValidConfig(t, filepath.Join(dir, "config.yaml"))
	source := filepath.Join(dir, "source.jsonl")
	if err := os.WriteFile(source, []byte(transcriptLine("a", "ok", 1)+"not-json\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	t.Chdir(dir)

	restore := configureResumeGlobals(filepath.Join(dir, "config.yaml"), source, filepath.Join(dir, "resume.jsonl"))
	defer restore()

	cmd := artifactCommand(t, filepath.Join(dir, "resume.jsonl"))
	err := resumeCmd.RunE(cmd, nil)
	if err == nil || !strings.Contains(err.Error(), "malformed transcript record") {
		t.Fatalf("error: got %v, want malformed transcript record", err)
	}
}

func TestLiveOutputModeMapsRunAndResumeFlagSemantics(t *testing.T) {
	if got := liveOutputMode(false, false); got != output.OutputNormal {
		t.Fatalf("default mode: got %v, want normal", got)
	}
	if got := liveOutputMode(true, false); got != output.OutputQuiet {
		t.Fatalf("quiet mode: got %v, want quiet", got)
	}
	if got := liveOutputMode(false, true); got != output.OutputVerbose {
		t.Fatalf("verbose mode: got %v, want verbose", got)
	}
}

func TestRunCommandRejectsQuietVerboseConflict(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	restore := configureRunGlobals("", filepath.Join(dir, "run.jsonl"))
	defer restore()
	runAuto = "quick"
	runQuiet = true
	runVerbose = true

	err := runCmd.PreRunE(outputModeCommand(t), nil)
	if err == nil || !strings.Contains(err.Error(), "cannot use --quiet and --verbose together") {
		t.Fatalf("error: got %v, want quiet/verbose conflict", err)
	}
}

func TestResumeCommandRejectsQuietVerboseConflict(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	restore := configureResumeGlobals("", filepath.Join(dir, "source.jsonl"), filepath.Join(dir, "resume.jsonl"))
	defer restore()
	resumeAuto = "quick"
	resumeQuiet = true
	resumeVerbose = true

	err := resumeCmd.PreRunE(outputModeCommand(t), []string{filepath.Join(dir, "source.jsonl")})
	if err == nil || !strings.Contains(err.Error(), "cannot use --quiet and --verbose together") {
		t.Fatalf("error: got %v, want quiet/verbose conflict", err)
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

func writeValidConfig(t *testing.T, path string) {
	t.Helper()
	content := `topology: ring
agents:
  - id: a
    model: test/model
    system_prompt: test
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write valid config: %v", err)
	}
}

func writeInvalidConfig(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte("agents: []\n"), 0o644); err != nil {
		t.Fatalf("write invalid config: %v", err)
	}
}

func writeResumeTranscript(t *testing.T, path string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(transcriptLine("a", "done", 0)), 0o644); err != nil {
		t.Fatalf("write resume transcript: %v", err)
	}
}

func transcriptLine(agentID, content string, tokens int) string {
	model := "test/model"
	record := types.TurnRecord{
		Turn:      0,
		AgentID:   agentID,
		Model:     &model,
		Timestamp: 1,
		Content:   content,
		Tokens:    types.TokenUsage{Total: &tokens},
		Elapsed:   0,
	}
	if agentID != "" {
		record.Transcript = types.NewTranscriptMetadata(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: []types.AgentConfig{{ID: agentID, Model: model}}})
	}
	data, err := json.Marshal(record)
	if err != nil {
		panic(fmt.Sprintf("marshal transcript record: %v", err))
	}
	return string(data) + "\n"
}

func transcriptContent(t *testing.T, records ...types.TurnRecord) string {
	t.Helper()
	var b strings.Builder
	for _, record := range records {
		data, err := json.Marshal(record)
		if err != nil {
			t.Fatalf("marshal transcript record: %v", err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}

func executeShowCommand(t *testing.T, arg string) (string, error) {
	t.Helper()
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := showCmd.RunE(cmd, []string{arg})
	return out.String(), err
}

func runListCommand(t *testing.T) string {
	t.Helper()
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := listCmd.RunE(cmd, nil); err != nil {
		t.Fatalf("list command: %v", err)
	}
	return out.String()
}

func artifactCommand(t *testing.T, outputPath string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{}
	cmd.Flags().String("output", "", "Output")
	cmd.Flags().Bool("research", false, "Research")
	cmd.Flags().Bool("no-research", false, "No research")
	cmd.Flags().StringArray("context", nil, "Context")
	cmd.Flags().Int("time", 60, "Time")
	cmd.Flags().Int("max-turns", 1, "Max turns")
	if err := cmd.Flags().Set("output", outputPath); err != nil {
		t.Fatalf("set output: %v", err)
	}
	return cmd
}

func outputModeCommand(t *testing.T) *cobra.Command {
	t.Helper()
	model := defaultModel
	auto := ""
	cmd := artifactCommand(t, filepath.Join(t.TempDir(), "out.jsonl"))
	cmd.Flags().StringVarP(&model, "model", "M", model, "Model")
	cmd.Flags().StringVar(&auto, "auto", "", "Auto")
	cmd.Flags().String("config", "", "Config")
	cmd.Flags().Float64("budget", 0, "Budget")
	return cmd
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(&buf, r)
		done <- err
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	os.Stdout = old
	if err := <-done; err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}
	return buf.String()
}

func assertStringContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Fatalf("expected output to contain %q\noutput: %s", substr, s)
	}
}

func assertStringNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Fatalf("expected output not to contain %q\noutput: %s", substr, s)
	}
}

func configureRunGlobals(configPath, outputPath string) func() {
	oldConfig, oldTopic, oldTimeLimit, oldWindow, oldMaxTurns, oldOutput := runConfig, runTopic, runTimeLimit, runWindow, runMaxTurns, runOutput
	oldVerbose, oldQuiet, oldBudget, oldBudgetFlag, oldSynthesize, oldFullContext := runVerbose, runQuiet, runBudget, runBudgetFlag, runSynthesize, runFullContext
	oldDryRun, oldAuto, oldModel, oldYes, oldResearch, oldNoResearch, oldContext := runDryRun, runAuto, runModel, runYes, runResearch, runNoResearch, runContext

	runConfig = configPath
	runTopic = "artifact resolution"
	runTimeLimit = 60
	runWindow = 2
	runMaxTurns = 1
	runOutput = outputPath
	runVerbose = false
	runQuiet = false
	runBudget = 0
	runBudgetFlag = false
	runSynthesize = false
	runFullContext = false
	runDryRun = true
	runAuto = ""
	runModel = defaultModel
	runYes = true
	runResearch = false
	runNoResearch = false
	runContext = nil

	return func() {
		runConfig, runTopic, runTimeLimit, runWindow, runMaxTurns, runOutput = oldConfig, oldTopic, oldTimeLimit, oldWindow, oldMaxTurns, oldOutput
		runVerbose, runQuiet, runBudget, runBudgetFlag, runSynthesize, runFullContext = oldVerbose, oldQuiet, oldBudget, oldBudgetFlag, oldSynthesize, oldFullContext
		runDryRun, runAuto, runModel, runYes, runResearch, runNoResearch, runContext = oldDryRun, oldAuto, oldModel, oldYes, oldResearch, oldNoResearch, oldContext
	}
}

func configureResumeGlobals(configPath, sourcePath, outputPath string) func() {
	oldConfig, oldTopic, oldTimeLimit, oldWindow, oldMaxTurns, oldOutput := resumeConfig, resumeTopic, resumeTimeLimit, resumeWindow, resumeMaxTurns, resumeOutput
	oldVerbose, oldQuiet, oldBudget, oldBudgetFlag, oldFullContext, oldDryRun := resumeVerbose, resumeQuiet, resumeBudget, resumeBudgetFlag, resumeFullContext, resumeDryRun
	oldAuto, oldModel, oldYes, oldFile, oldResearch, oldNoResearch, oldContext := resumeAuto, resumeModel, resumeYes, resumeFile, resumeResearch, resumeNoResearch, resumeContext

	resumeConfig = configPath
	resumeTopic = "artifact resolution"
	resumeTimeLimit = 60
	resumeWindow = 2
	resumeMaxTurns = 0
	resumeOutput = outputPath
	resumeVerbose = false
	resumeQuiet = false
	resumeBudget = 0
	resumeBudgetFlag = false
	resumeFullContext = false
	resumeDryRun = true
	resumeAuto = ""
	resumeModel = defaultModel
	resumeYes = true
	resumeFile = sourcePath
	resumeResearch = false
	resumeNoResearch = false
	resumeContext = nil

	return func() {
		resumeConfig, resumeTopic, resumeTimeLimit, resumeWindow, resumeMaxTurns, resumeOutput = oldConfig, oldTopic, oldTimeLimit, oldWindow, oldMaxTurns, oldOutput
		resumeVerbose, resumeQuiet, resumeBudget, resumeBudgetFlag, resumeFullContext, resumeDryRun = oldVerbose, oldQuiet, oldBudget, oldBudgetFlag, oldFullContext, oldDryRun
		resumeAuto, resumeModel, resumeYes, resumeFile, resumeResearch, resumeNoResearch, resumeContext = oldAuto, oldModel, oldYes, oldFile, oldResearch, oldNoResearch, oldContext
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
