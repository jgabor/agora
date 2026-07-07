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

	"github.com/jgabor/agora/internal/cast"
	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/evidence"
	"github.com/jgabor/agora/internal/output"
	"github.com/jgabor/agora/internal/session"
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
		"settings",
		"path",
		"default_model",
		"opencode-",
		"go/deepseek-",
		"flash",
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

func TestConfigGetAllFormattedOutputsIncludeMetadata(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("XDG_CONFIG_HOME path behavior is Linux-specific")
	}
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	secret := "sk-test-secret-token-123"
	writeSettings(t, `default_model: "`+secret+`"
default_auto_level: "quick"
`)

	jsonOut, err := executeConfigCommand(t, "get", "--all", "--format", "json")
	if err != nil {
		t.Fatalf("config get --all json: %v", err)
	}
	assertJSONNoANSI(t, jsonOut)
	for _, want := range []string{"\"settings\"", "\"default_model\"", "\"type\"", "\"source\"", "\"effective_value_policy\"", "\"allowed_values\"", "[redacted]", "quick"} {
		assertStringContains(t, jsonOut, want)
	}
	assertStringNotContains(t, jsonOut, secret)

	markdownOut, err := executeConfigCommand(t, "get", "--all", "--format", "markdown")
	if err != nil {
		t.Fatalf("config get --all markdown: %v", err)
	}
	for _, want := range []string{"# Global Settings", "## Effective Settings", "`default_auto_level`", "source `set`", "type `enum`", "allowed `quick`, `normal`, `deep`, `yolo`", "[redacted]"} {
		assertStringContains(t, markdownOut, want)
	}
	assertStringNotContains(t, markdownOut, secret)
	assertStringNotContains(t, markdownOut, "\x1b[")
}

func TestConfigGetAllFormattedRejectsInvalidFormat(t *testing.T) {
	_, err := executeConfigCommand(t, "get", "--all", "--format", "xml")
	if err == nil {
		t.Fatal("expected invalid format error")
	}
	for _, want := range []string{"text", "json", "markdown"} {
		assertStringContains(t, err.Error(), want)
	}
}

func TestFormatContractAcceptedFormats(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "default_output_dir: \""+dir+"\"")
	configPath := filepath.Join(dir, "config.yaml")
	writeValidConfig(t, configPath)
	transcriptPath := filepath.Join(dir, "20260504-143022-topic.jsonl")
	model := "test/model"
	if err := os.WriteFile(transcriptPath, []byte(transcriptContent(t,
		types.TurnRecord{Turn: 0, AgentID: "analyst", Model: &model, Timestamp: 1, Content: "answer", Tokens: types.TokenUsage{Total: intPtr(3)}},
		types.TurnRecord{Turn: 1, AgentID: "critic", Model: &model, Timestamp: 2, Content: "second answer", Tokens: types.TokenUsage{Total: intPtr(7)}, Consensus: true, ConsensusStatement: "We agree"},
	)), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	tests := []struct {
		name    string
		format  string
		run     func(*cobra.Command) error
		assert  func(string)
		cleanup func()
	}{
		{
			name:   "list json",
			format: formatJSON,
			run: func(cmd *cobra.Command) error {
				old := listFormat
				listFormat = formatJSON
				t.Cleanup(func() { listFormat = old })
				return listCmd.RunE(cmd, nil)
			},
			assert: func(out string) { assertJSONNoANSI(t, out) },
		},
		{
			name:   "stats json",
			format: formatJSON,
			run: func(cmd *cobra.Command) error {
				old := statsFormat
				statsFormat = formatJSON
				t.Cleanup(func() { statsFormat = old })
				return statsCmd.RunE(cmd, []string{transcriptPath})
			},
			assert: func(out string) { assertJSONNoANSI(t, out) },
		},
		{
			name:   "show markdown",
			format: formatMarkdown,
			run: func(cmd *cobra.Command) error {
				old := showFormat
				showFormat = formatMarkdown
				t.Cleanup(func() { showFormat = old })
				return showCmd.RunE(cmd, []string{transcriptPath})
			},
			assert: func(out string) {
				assertStringContains(t, out, "# Transcript")
				assertStringContains(t, out, "Records")
				assertStringContains(t, out, "answer")
				assertStringContains(t, out, "critic")
				assertStringContains(t, out, "We agree")
				assertStringNotContains(t, out, "\x1b[")
			},
		},
		{
			name:   "stats markdown",
			format: formatMarkdown,
			run: func(cmd *cobra.Command) error {
				old := statsFormat
				statsFormat = formatMarkdown
				t.Cleanup(func() { statsFormat = old })
				return statsCmd.RunE(cmd, []string{transcriptPath})
			},
			assert: func(out string) {
				assertStringContains(t, out, "# Transcript Statistics")
				assertStringContains(t, out, "Total turns")
				assertStringContains(t, out, "Agent analyst")
				assertStringContains(t, out, "Agent critic")
				assertStringContains(t, out, "Consensus 1")
				assertStringContains(t, out, "We agree")
				assertStringNotContains(t, out, "\x1b[")
			},
		},
		{
			name:   "validate json",
			format: formatJSON,
			run: func(cmd *cobra.Command) error {
				old := validateFormatValue
				validateFormatValue = formatJSON
				t.Cleanup(func() { validateFormatValue = old })
				return validateCmd.RunE(cmd, []string{configPath})
			},
			assert: func(out string) { assertJSONNoANSI(t, out) },
		},
		{
			name:   "config all markdown",
			format: formatMarkdown,
			run: func(cmd *cobra.Command) error {
				out, err := executeConfigCommand(t, "get", "--all", "--format", "markdown")
				_, _ = cmd.OutOrStdout().Write([]byte(out))
				return err
			},
			assert: func(out string) {
				assertStringContains(t, out, "# Global Settings")
				assertStringContains(t, out, "default_model")
				assertStringNotContains(t, out, "\x1b[")
			},
		},
		{
			name:   "metadata json",
			format: formatJSON,
			run: func(cmd *cobra.Command) error {
				old := metadataFormat
				metadataFormat = formatJSON
				t.Cleanup(func() { metadataFormat = old })
				return metadataCmd.RunE(cmd, nil)
			},
			assert: func(out string) { assertJSONNoANSI(t, out) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := &cobra.Command{}
			var out bytes.Buffer
			cmd.SetOut(&out)
			if err := tt.run(cmd); err != nil {
				t.Fatalf("run %s: %v", tt.name, err)
			}
			tt.assert(out.String())
		})
	}
}

func TestFormatContractDefaultMatchesTextOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "default_output_dir: \""+dir+"\"")
	transcriptPath := filepath.Join(dir, "20260504-143022-topic.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcriptLine("analyst", "default text answer", 3)), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	runShow := func(format string) string {
		old := showFormat
		showFormat = format
		defer func() { showFormat = old }()
		cmd := &cobra.Command{}
		var out bytes.Buffer
		cmd.SetOut(&out)
		if err := showCmd.RunE(cmd, []string{transcriptPath}); err != nil {
			t.Fatalf("show command: %v", err)
		}
		return out.String()
	}
	if defaultOut, textOut := runShow(formatText), runShow(formatText); defaultOut != textOut {
		t.Fatalf("show default output differs from --format text\ndefault:\n%s\ntext:\n%s", defaultOut, textOut)
	}

	defaultConfig, err := executeConfigCommand(t, "get", "--all")
	if err != nil {
		t.Fatalf("config get --all default: %v", err)
	}
	textConfig, err := executeConfigCommand(t, "get", "--all", "--format", "text")
	if err != nil {
		t.Fatalf("config get --all text: %v", err)
	}
	if defaultConfig != textConfig {
		t.Fatalf("config default output differs from --format text\ndefault:\n%s\ntext:\n%s", defaultConfig, textConfig)
	}
}

func TestPrimeCommandDefaultAndTextOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	writeSettings(t, `default_model: "test-secret-safe-model"`)

	defaultOut := runPrimeCommand(t, formatText)
	textOut := runPrimeCommand(t, formatText)
	if defaultOut != textOut {
		t.Fatalf("prime default output differs from --format text\ndefault:\n%s\ntext:\n%s", defaultOut, textOut)
	}
	for _, want := range []string{"Agora Prime", "CLI operating context", "not deliberation evidence", "--context PATH", "agora run", "default_model", "Transcript metadata"} {
		assertStringContains(t, defaultOut, want)
	}
}

func TestPrimeCommandJSONOutputIncludesContractSections(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	writeSettings(t, `default_auto_level: "quick"`)

	out := runPrimeCommand(t, formatJSON)
	assertJSONNoANSI(t, out)
	var envelope struct {
		SchemaVersion int            `json:"schema_version"`
		Command       string         `json:"command"`
		Data          map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("decode prime json: %v", err)
	}
	if envelope.Command != "prime" || envelope.SchemaVersion != schemaVersion {
		t.Fatalf("prime envelope: %#v", envelope)
	}
	for _, key := range []string{"commands", "flags", "defaults", "enum_values", "settings_keys", "settings", "transcript_metadata", "context_boundary"} {
		if _, ok := envelope.Data[key]; !ok {
			t.Fatalf("prime json missing %q: %#v", key, envelope.Data)
		}
	}
	assertStringContains(t, out, "agora prime")
	assertStringContains(t, out, "--context PATH")
	assertStringContains(t, out, "[redacted]")
}

func TestPrimeCommandMarkdownOutputIncludesAgentBriefing(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	writeSettings(t, "")

	out := runPrimeCommand(t, formatMarkdown)
	assertStringContains(t, out, "# Agora Prime")
	assertStringContains(t, out, "## Commands")
	assertStringContains(t, out, "## Flags, Defaults, And Enum Values")
	assertStringContains(t, out, "## Settings")
	assertStringContains(t, out, "## Transcript Metadata")
	assertStringContains(t, out, "`agora prime`")
	assertStringContains(t, out, "`--context PATH`")
	assertStringContains(t, out, "`text`, `json`, `markdown`")
	assertStringNotContains(t, out, "\x1b[")
}

func TestPrimeCommandRedactsSecretLookingSettingsValues(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	secret := "sk-test-secret-token-123"
	writeSettings(t, `default_model: "`+secret+`"`)

	for _, format := range []string{formatText, formatJSON, formatMarkdown} {
		out := runPrimeCommand(t, format)
		assertStringNotContains(t, out, secret)
		assertStringContains(t, out, "[redacted]")
	}
}

func TestPrimeCommandRejectsInvalidFormat(t *testing.T) {
	old := primeFormat
	primeFormat = "xml"
	defer func() { primeFormat = old }()

	err := primeCmd.RunE(&cobra.Command{}, nil)
	if err == nil {
		t.Fatal("expected invalid format error")
	}
	for _, want := range []string{"text", "json", "markdown"} {
		assertStringContains(t, err.Error(), want)
	}
}

func TestFormatContractInvalidFormats(t *testing.T) {
	tests := []struct {
		name string
		run  func() error
	}{
		{name: "list", run: func() error {
			old := listFormat
			listFormat = "xml"
			defer func() { listFormat = old }()
			return listCmd.RunE(&cobra.Command{}, nil)
		}},
		{name: "stats", run: func() error {
			old := statsFormat
			statsFormat = "xml"
			defer func() { statsFormat = old }()
			return statsCmd.RunE(&cobra.Command{}, []string{"missing"})
		}},
		{name: "show", run: func() error {
			old := showFormat
			showFormat = "xml"
			defer func() { showFormat = old }()
			return showCmd.RunE(&cobra.Command{}, []string{"missing"})
		}},
		{name: "validate", run: func() error {
			old := validateFormatValue
			validateFormatValue = "xml"
			defer func() { validateFormatValue = old }()
			return validateCmd.RunE(&cobra.Command{}, []string{"missing"})
		}},
		{name: "config get all", run: func() error { _, err := executeConfigCommand(t, "get", "--all", "--format", "xml"); return err }},
		{name: "metadata", run: func() error {
			old := metadataFormat
			metadataFormat = "xml"
			defer func() { metadataFormat = old }()
			return metadataCmd.RunE(&cobra.Command{}, nil)
		}},
		{name: "prime", run: func() error {
			old := primeFormat
			primeFormat = "xml"
			defer func() { primeFormat = old }()
			return primeCmd.RunE(&cobra.Command{}, nil)
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.run()
			if err == nil {
				t.Fatal("expected invalid format error")
			}
			for _, want := range []string{"text", "json", "markdown"} {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("invalid format error missing %q: %v", want, err)
				}
			}
		})
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
	runFlags.Context = nil
	cmd.Flags().Bool("research", false, "Research")
	cmd.Flags().Bool("no-research", false, "No research")
	cmd.Flags().StringArrayVar(&runFlags.Context, "context", nil, "Context")
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
	runFlags.Context = nil
	cmd.Flags().Bool("research", false, "Research")
	cmd.Flags().Bool("no-research", false, "No research")
	cmd.Flags().StringArrayVar(&runFlags.Context, "context", nil, "Context")
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
	runFlags.Context = nil
	cmd.Flags().Bool("research", false, "Research")
	cmd.Flags().Bool("no-research", false, "No research")
	cmd.Flags().StringArrayVar(&runFlags.Context, "context", nil, "Context")

	overrides := runEvidenceOverrides(cmd, true, types.AutoNormal)

	want := evidence.DefaultsForAutoLevel(types.AutoNormal)
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

func TestResolveLedgerPolicyCLIDisablesOverConfigEnable(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-ledger", false, "No ledger")
	if err := cmd.Flags().Set("no-ledger", "true"); err != nil {
		t.Fatalf("set no-ledger: %v", err)
	}
	cfg := &types.DeliberationConfig{}
	enabled := true
	cfg.Ledger = &enabled

	resolved := resolveLedgerPolicy(cmd, cfg, config.Settings{})
	if resolved == nil {
		t.Fatal("resolved: got nil, want false pointer (CLI override)")
	}
	if *resolved {
		t.Fatal("resolved: got true, want false (CLI --no-ledger wins over config-enabled)")
	}
}

func TestResolveLedgerPolicyConfigDisablesOverSettingsEnable(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-ledger", false, "No ledger")
	cfg := &types.DeliberationConfig{}
	disabled := false
	cfg.Ledger = &disabled
	settingsEnabled := true
	settings := config.Settings{DefaultLedgerEnabled: &settingsEnabled}

	resolved := resolveLedgerPolicy(cmd, cfg, settings)
	if resolved == nil {
		t.Fatal("resolved: got nil, want false pointer (config override)")
	}
	if *resolved {
		t.Fatal("resolved: got true, want false (config-disabled wins over settings-enabled)")
	}
}

func TestResolveLedgerPolicySettingsDisablesOverDefaultEnable(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-ledger", false, "No ledger")
	cfg := &types.DeliberationConfig{}
	settingsDisabled := false
	settings := config.Settings{DefaultLedgerEnabled: &settingsDisabled}

	resolved := resolveLedgerPolicy(cmd, cfg, settings)
	if resolved == nil {
		t.Fatal("resolved: got nil, want false pointer (settings override)")
	}
	if *resolved {
		t.Fatal("resolved: got true, want false (settings-disabled wins over default-enabled)")
	}
}

func TestResolveLedgerPolicyAllUnsetReturnsNil(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-ledger", false, "No ledger")
	cfg := &types.DeliberationConfig{}

	resolved := resolveLedgerPolicy(cmd, cfg, config.Settings{})
	if resolved != nil {
		t.Fatalf("resolved: got %v, want nil (enabled by default when all layers unset)", *resolved)
	}
}

func TestResolveLedgerPolicyCLIDisablesConfigAndSettingsEnabled(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().Bool("no-ledger", false, "No ledger")
	if err := cmd.Flags().Set("no-ledger", "true"); err != nil {
		t.Fatalf("set no-ledger: %v", err)
	}
	cfg := &types.DeliberationConfig{}
	configEnabled := true
	cfg.Ledger = &configEnabled
	settingsEnabled := true
	settings := config.Settings{DefaultLedgerEnabled: &settingsEnabled}

	resolved := resolveLedgerPolicy(cmd, cfg, settings)
	if resolved == nil {
		t.Fatal("resolved: got nil, want &false (CLI --no-ledger must win triple-over-config-and-settings)")
	}
	if *resolved {
		t.Fatal("resolved: got true, want false (CLI --no-ledger wins over config-enable AND settings-enable simultaneously per three-layer precedence)")
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
	assertStringContains(t, verbose, "20260504-143022-my-topi") // Robust to truncation in narrow table
}

func TestListCommandFormattedOutputReportsStoreFacts(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")
	filename := "20260504-143022-my-topic.jsonl"
	if err := os.WriteFile(filepath.Join(store, filename), []byte(transcriptLine("a", "listed", 5)+transcriptLine("b", "again", 7)), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	jsonOut := executeListCommand(t, formatJSON)
	assertJSONNoANSI(t, jsonOut)
	var doc struct {
		Data transcriptListOutput `json:"data"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("decode list json: %v", err)
	}
	if doc.Data.StorePath != store || doc.Data.TranscriptCount != 1 || doc.Data.Empty || len(doc.Data.Transcripts) != 1 {
		t.Fatalf("list json data: got %#v", doc.Data)
	}
	if got := doc.Data.Transcripts[0]; got.Slug != "my-topic" || got.Turns != 2 || got.Filename != filename {
		t.Fatalf("list transcript: got %#v", got)
	}

	markdownOut := executeListCommand(t, formatMarkdown)
	assertStringContains(t, markdownOut, "# Managed Transcripts")
	assertStringContains(t, markdownOut, "Transcript count:** 1")
	assertStringContains(t, markdownOut, "my-topic")
	assertStringContains(t, markdownOut, filename)
	assertStringNotContains(t, markdownOut, "\x1b[")
}

func TestListCommandFormattedOutputReportsEmptyStore(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	store := t.TempDir()
	writeSettings(t, "default_output_dir: \""+store+"\"")

	jsonOut := executeListCommand(t, formatJSON)
	assertJSONNoANSI(t, jsonOut)
	var doc struct {
		Data transcriptListOutput `json:"data"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("decode list json: %v", err)
	}
	if doc.Data.StorePath != store || doc.Data.TranscriptCount != 0 || !doc.Data.Empty || len(doc.Data.Transcripts) != 0 {
		t.Fatalf("empty list json data: got %#v", doc.Data)
	}

	markdownOut := executeListCommand(t, formatMarkdown)
	assertStringContains(t, markdownOut, "Transcript count:** 0")
	assertStringContains(t, markdownOut, "Empty:** true")
	assertStringContains(t, markdownOut, "No transcripts found.")
	assertStringNotContains(t, markdownOut, "\x1b[")
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
	runFlags.Quiet = true
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

func TestValidateFormattedOutputsReportSummary(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "valid.yaml")
	writeValidConfig(t, path)

	jsonOut, err := runValidateCommand(t, formatJSON, path)
	if err != nil {
		t.Fatalf("validate json: %v", err)
	}
	assertJSONNoANSI(t, jsonOut)
	for _, want := range []string{"\"valid\": true", "\"summary\"", "\"topology\": \"ring\"", "\"agent_count\": 1"} {
		assertStringContains(t, jsonOut, want)
	}

	markdownOut, err := runValidateCommand(t, formatMarkdown, path)
	if err != nil {
		t.Fatalf("validate markdown: %v", err)
	}
	for _, want := range []string{"# Configuration Valid", "**Valid:** true", "**Topology:** ring", "**Agents:** 1"} {
		assertStringContains(t, markdownOut, want)
	}
	assertStringNotContains(t, markdownOut, "\x1b[")
}

func TestValidateFormattedOutputsReportStructuredFailure(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "invalid.yaml")
	writeInvalidConfig(t, path)

	jsonOut, err := runValidateCommand(t, formatJSON, path)
	if err == nil {
		t.Fatal("expected validate json failure")
	}
	assertJSONNoANSI(t, jsonOut)
	for _, want := range []string{"\"valid\": false", "\"stage\": \"validate\"", "configuration must contain at least one agent", "\"corrections\""} {
		assertStringContains(t, jsonOut, want)
	}

	markdownOut, err := runValidateCommand(t, formatMarkdown, path)
	if err == nil {
		t.Fatal("expected validate markdown failure")
	}
	for _, want := range []string{"# Configuration Invalid", "**Valid:** false", "**Stage:** `validate`", "configuration must contain at least one agent", "## Self-Correction"} {
		assertStringContains(t, markdownOut, want)
	}
	assertStringNotContains(t, markdownOut, "\x1b[")
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
	resumeFlags.MaxTurns = 1
	normal := captureStdout(t, func() {
		if err := resumeCmd.RunE(artifactCommand(t, filepath.Join(dir, "resume-normal.jsonl")), nil); err != nil {
			t.Fatalf("resume normal: %v", err)
		}
	})
	restore()

	restore = configureResumeGlobals(configPath, source, filepath.Join(dir, "resume-verbose.jsonl"))
	resumeFlags.MaxTurns = 1
	resumeFlags.Verbose = true
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

func TestStatsCommandFormattedOutputReportsTranscriptFacts(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "stats.jsonl")
	model := "test/model"
	if err := os.WriteFile(path, []byte(transcriptContent(t,
		types.TurnRecord{Turn: 0, AgentID: "analyst", Model: &model, Timestamp: 1, Content: "answer", Tokens: types.TokenUsage{Total: intPtr(3)}},
		types.TurnRecord{Turn: 1, AgentID: "critic", Model: &model, Timestamp: 2, Content: "second", Tokens: types.TokenUsage{Total: intPtr(7)}, Consensus: true, ConsensusStatement: "We agree"},
	)), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	jsonOut, err := executeStatsCommand(t, formatJSON, path)
	if err != nil {
		t.Fatalf("stats json: %v", err)
	}
	assertJSONNoANSI(t, jsonOut)
	var doc struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("decode stats json: %v", err)
	}
	if doc.Data["total_turns"] != float64(2) || doc.Data["total_tokens"] != float64(10) {
		t.Fatalf("stats json data: got %#v", doc.Data)
	}
	perAgent, ok := doc.Data["per_agent"].(map[string]any)
	if !ok || len(perAgent) != 2 || perAgent["analyst"] == nil || perAgent["critic"] == nil {
		t.Fatalf("stats per-agent data: got %#v", doc.Data["per_agent"])
	}

	markdownOut, err := executeStatsCommand(t, formatMarkdown, path)
	if err != nil {
		t.Fatalf("stats markdown: %v", err)
	}
	assertStringContains(t, markdownOut, "# Transcript Statistics")
	assertStringContains(t, markdownOut, "Total turns")
	assertStringContains(t, markdownOut, "Agent analyst")
	assertStringContains(t, markdownOut, "Agent critic")
	assertStringContains(t, markdownOut, "We agree")
	assertStringNotContains(t, markdownOut, "\x1b[")
}

func TestStatsCommandFormattedOutputReportsMachineUsableErrors(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(path, []byte("not-json\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	jsonOut, err := executeStatsCommand(t, formatJSON, path)
	if err == nil || !strings.Contains(err.Error(), "malformed transcript record") {
		t.Fatalf("stats json error: got %v, want malformed transcript record", err)
	}
	assertJSONNoANSI(t, jsonOut)
	var doc struct {
		Data commandErrorOutput `json:"data"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("decode stats error json: %v", err)
	}
	if doc.Data.OK || doc.Data.Code != "transcript_load_failed" || !strings.Contains(doc.Data.Message, "malformed transcript record") {
		t.Fatalf("stats error json data: got %#v", doc.Data)
	}

	markdownOut, err := executeStatsCommand(t, formatMarkdown, filepath.Join(dir, "missing.jsonl"))
	if err == nil || !strings.Contains(err.Error(), "loading transcript") {
		t.Fatalf("stats markdown error: got %v, want loading transcript", err)
	}
	assertStringContains(t, markdownOut, "# Transcript Statistics Error")
	assertStringContains(t, markdownOut, "transcript_load_failed")
	assertStringContains(t, markdownOut, "loading transcript")
	assertStringNotContains(t, markdownOut, "\x1b[")
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
	for _, want := range []string{"TURN 1/1", "AGENT [A1 analyst] Solon analyst", "AGENT CONTENT", "slug answer"} {
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

const malformedLedgerRecordLine = `{"turn": -3, "agent_id": "ledger", "timestamp": 1.0, "content": "", "tokens": {}, "consensus": false, "consensus_statement": "", "elapsed": 0}`

func TestShowCommandRejectsMalformedLedgerRecord(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "bad-ledger.jsonl")
	content := transcriptLine("a", "ok", 1) + malformedLedgerRecordLine + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	_, err := executeShowCommand(t, path)
	if err == nil || !strings.Contains(err.Error(), "malformed transcript record") || !strings.Contains(err.Error(), "ledger") {
		t.Fatalf("error: got %v, want malformed transcript record mentioning ledger", err)
	}

	if _, err := executeStatsCommand(t, formatText, path); err == nil || !strings.Contains(err.Error(), "malformed transcript record") {
		t.Fatalf("stats error: got %v, want malformed transcript record for malformed ledger", err)
	}
}

func TestResumeCommandWarnsAndContinuesOnMalformedLedgerRecord(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	writeValidConfig(t, filepath.Join(dir, "config.yaml"))
	source := filepath.Join(dir, "source.jsonl")
	content := transcriptLine("a", "ok", 1) + malformedLedgerRecordLine + "\n"
	if err := os.WriteFile(source, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	t.Chdir(dir)

	outputPath := filepath.Join(dir, "resume.jsonl")
	restore := configureResumeGlobals(filepath.Join(dir, "config.yaml"), source, outputPath)
	defer restore()

	cmd := artifactCommand(t, outputPath)
	if err := resumeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf("resume should warn-and-continue on a malformed ledger record, got: %v", err)
	}

	records, err := loadTranscriptFile(outputPath)
	if err != nil {
		t.Fatalf("load resumed transcript: %v", err)
	}
	for i, r := range records {
		if r.AgentID == types.LedgerAgentID || r.Turn == types.LedgerSentinelTurn {
			t.Fatalf("malformed ledger record should have been dropped on resume; found at index %d: %#v", i, r)
		}
	}
	if len(records) == 0 {
		t.Fatal("resumed transcript should retain the well-formed source records")
	}
}

func TestShowCommandRendersReadableTurnsInRecordOrder(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "show.jsonl")
	model := "test/model"
	cfg := &types.DeliberationConfig{Topology: types.TopologyRing, Agents: []types.AgentConfig{
		{ID: "analyst", Model: model},
		{ID: "critic", Model: model},
	}}
	metadata := types.NewTranscriptMetadata(cfg, cast.New(cfg.Agents).Members())
	content := transcriptContent(t,
		types.TurnRecord{Turn: -1, AgentID: "moderator", Timestamp: 1, Transcript: metadata, Evidence: &types.EvidenceBundle{Summary: "Two sources found", SourceReferences: []types.SourceReference{{Title: "Spec", URL: "https://example.test/spec"}, {Path: "README.md"}}}},
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
		"AGENT moderator",
		"Evidence Summary",
		"Two sources found",
		"1. Spec (https://example.test/spec)",
		"2. README.md (README.md)",
		"TURN 1/2 (50%)",
		"AGENT [A1 analyst] Solon analyst",
		"MODEL test/model",
		"AGENT CONTENT",
		"First answer",
		"TURN 2/2 (100%)",
		"AGENT [A2 critic] Aspasia critic",
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

func TestShowCommandFormattedOutputIsInspectionDocument(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	path := filepath.Join(dir, "show.jsonl")
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology:           types.TopologyRing,
		ConsensusThreshold: 2,
		Agents: []types.AgentConfig{
			{ID: "analyst", Model: model},
			{ID: "critic", Model: model},
		},
		ContextPaths: []string{"secret-notes.md"},
	}
	metadata := types.NewTranscriptMetadata(cfg, cast.New(cfg.Agents).Members())
	content := transcriptContent(t,
		types.TurnRecord{Turn: -1, AgentID: "moderator", Timestamp: 1, Transcript: metadata, Evidence: &types.EvidenceBundle{
			Summary:          "Two sources found",
			SourceReferences: []types.SourceReference{{Title: "Spec", URL: "https://example.test/spec"}, {Path: "README.md"}},
			ContextDocuments: []types.ContextDocument{{Path: "secret-notes.md", Content: "full source text must not be exported"}},
		}},
		types.TurnRecord{Turn: 0, AgentID: "analyst", Model: &model, Timestamp: 2, Content: "First answer"},
		types.TurnRecord{Turn: 1, AgentID: "critic", Model: &model, Timestamp: 3, Content: "Second answer", Consensus: true, ConsensusStatement: "We agree"},
	)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	jsonOut, err := executeShowCommandFormat(t, formatJSON, path)
	if err != nil {
		t.Fatalf("show json: %v", err)
	}
	assertJSONNoANSI(t, jsonOut)
	assertStringNotContains(t, jsonOut, "full source text must not be exported")
	assertStringNotContains(t, jsonOut, "context_documents")
	assertStringNotContains(t, jsonOut, "\"tokens\"")
	assertStringNotContains(t, jsonOut, "\"elapsed\"")
	var doc struct {
		SchemaVersion int                  `json:"schema_version"`
		Command       string               `json:"command"`
		Data          transcriptShowOutput `json:"data"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("decode show json: %v", err)
	}
	if doc.SchemaVersion != schemaVersion || doc.Command != "show" || doc.Data.DocumentType != "agora.transcript.show" || doc.Data.DocumentSchemaVersion != 1 {
		t.Fatalf("show json document metadata: got %#v", doc)
	}
	if doc.Data.RecordCount != 3 || len(doc.Data.Records) != 3 || doc.Data.Records[0].AgentID != "moderator" || doc.Data.Records[1].AgentID != "analyst" || doc.Data.Records[2].AgentID != "critic" {
		t.Fatalf("show json record order: got %#v", doc.Data.Records)
	}
	if !doc.Data.Metadata.Available || len(doc.Data.Metadata.Cast) != 2 || doc.Data.Records[1].CastMember == nil || doc.Data.Records[1].CastMember.Name != "Solon" {
		t.Fatalf("show json cast metadata: got %#v", doc.Data)
	}
	if doc.Data.Records[0].Evidence == nil || !doc.Data.Records[0].Evidence.FullSourceContentOmitted || len(doc.Data.Records[0].Evidence.SourceReferences) != 2 || len(doc.Data.Records[0].Evidence.ContextDocumentRefs) != 1 {
		t.Fatalf("show json evidence boundary: got %#v", doc.Data.Records[0].Evidence)
	}
	if !doc.Data.Records[2].Consensus || doc.Data.Records[2].ConsensusStatement != "We agree" {
		t.Fatalf("show json consensus: got %#v", doc.Data.Records[2])
	}

	markdownOut, err := executeShowCommandFormat(t, formatMarkdown, path)
	if err != nil {
		t.Fatalf("show markdown: %v", err)
	}
	assertStringContains(t, markdownOut, "# Transcript")
	assertStringContains(t, markdownOut, "Document schema version")
	assertStringContains(t, markdownOut, "Cast Metadata")
	assertStringContains(t, markdownOut, "A1 Solon")
	assertStringContains(t, markdownOut, "Evidence summary")
	assertStringContains(t, markdownOut, "Spec (https://example.test/spec)")
	assertStringContains(t, markdownOut, "Evidence full source content omitted")
	assertStringContains(t, markdownOut, "Consensus statement")
	assertStringContains(t, markdownOut, "We agree")
	assertStringNotContains(t, markdownOut, "full source text must not be exported")
	assertStringNotContains(t, markdownOut, "\x1b[")
	if strings.Index(markdownOut, "Record 1") > strings.Index(markdownOut, "Record 2") || strings.Index(markdownOut, "Record 2") > strings.Index(markdownOut, "Record 3") {
		t.Fatalf("show markdown changed record order:\n%s", markdownOut)
	}
}

func TestShowCommandFormattedOutputReportsClearFailures(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	badPath := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(badPath, []byte(transcriptLine("a", "partial answer", 1)+"not-json\n"), 0o644); err != nil {
		t.Fatalf("write malformed transcript: %v", err)
	}
	emptyPath := filepath.Join(dir, "empty.jsonl")
	if err := os.WriteFile(emptyPath, []byte("\n"), 0o644); err != nil {
		t.Fatalf("write empty transcript: %v", err)
	}

	jsonOut, err := executeShowCommandFormat(t, formatJSON, badPath)
	if err == nil || !strings.Contains(err.Error(), "malformed transcript record") {
		t.Fatalf("show json malformed error: got %v, want malformed transcript record", err)
	}
	assertJSONNoANSI(t, jsonOut)
	assertStringNotContains(t, jsonOut, "partial answer")
	var doc struct {
		Data commandErrorOutput `json:"data"`
	}
	if err := json.Unmarshal([]byte(jsonOut), &doc); err != nil {
		t.Fatalf("decode show error json: %v", err)
	}
	if doc.Data.OK || doc.Data.Code != "transcript_load_failed" || !strings.Contains(doc.Data.Message, "malformed transcript record") {
		t.Fatalf("show error json data: got %#v", doc.Data)
	}

	markdownOut, err := executeShowCommandFormat(t, formatMarkdown, filepath.Join(dir, "missing.jsonl"))
	if err == nil || !strings.Contains(err.Error(), "loading transcript") {
		t.Fatalf("show markdown missing error: got %v, want loading transcript", err)
	}
	assertStringContains(t, markdownOut, "# Transcript Error")
	assertStringContains(t, markdownOut, "transcript_load_failed")
	assertStringNotContains(t, markdownOut, "\x1b[")

	emptyOut, err := executeShowCommandFormat(t, formatJSON, emptyPath)
	if err == nil || !strings.Contains(err.Error(), "transcript empty") {
		t.Fatalf("show json empty error: got %v, want transcript empty", err)
	}
	assertStringContains(t, emptyOut, "transcript_empty")
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
	runFlags.Auto = "quick"
	runFlags.Quiet = true
	runFlags.Verbose = true

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
	resumeFlags.Auto = "quick"
	resumeFlags.Quiet = true
	resumeFlags.Verbose = true

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
		cfg := &types.DeliberationConfig{Topology: types.TopologyRing, Agents: []types.AgentConfig{{ID: agentID, Model: model}}}
		record.Transcript = types.NewTranscriptMetadata(cfg, cast.New(cfg.Agents).Members())
	}
	data, err := json.Marshal(record)
	if err != nil {
		panic(fmt.Sprintf("marshal transcript record: %v", err))
	}
	return string(data) + "\n"
}

func intPtr(value int) *int {
	return &value
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

func executeShowCommandFormat(t *testing.T, format, arg string) (string, error) {
	t.Helper()
	old := showFormat
	showFormat = format
	t.Cleanup(func() { showFormat = old })
	return executeShowCommand(t, arg)
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

func executeListCommand(t *testing.T, format string) string {
	t.Helper()
	old := listFormat
	listFormat = format
	t.Cleanup(func() { listFormat = old })
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := listCmd.RunE(cmd, nil); err != nil {
		t.Fatalf("list command: %v", err)
	}
	return out.String()
}

func executeStatsCommand(t *testing.T, format, arg string) (string, error) {
	t.Helper()
	old := statsFormat
	statsFormat = format
	t.Cleanup(func() { statsFormat = old })
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := statsCmd.RunE(cmd, []string{arg})
	return out.String(), err
}

func runPrimeCommand(t *testing.T, format string) string {
	t.Helper()
	old := primeFormat
	primeFormat = format
	t.Cleanup(func() { primeFormat = old })
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := primeCmd.RunE(cmd, nil); err != nil {
		t.Fatalf("prime command: %v", err)
	}
	return out.String()
}

func runValidateCommand(t *testing.T, format, arg string) (string, error) {
	t.Helper()
	old := validateFormatValue
	validateFormatValue = format
	t.Cleanup(func() { validateFormatValue = old })
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	err := validateCmd.RunE(cmd, []string{arg})
	return out.String(), err
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

func assertJSONNoANSI(t *testing.T, out string) {
	t.Helper()
	if !json.Valid([]byte(out)) {
		t.Fatalf("output is not valid JSON:\n%s", out)
	}
	assertStringContains(t, out, "\"schema_version\"")
	assertStringNotContains(t, out, "\x1b[")
}

func configureRunGlobals(configPath, outputPath string) func() {
	oldConfig, oldTopic, oldTimeLimit, oldWindow, oldMaxTurns, oldOutput := runFlags.Config, runFlags.Topic, runFlags.TimeLimit, runFlags.Window, runFlags.MaxTurns, runFlags.Output
	oldVerbose, oldQuiet, oldBudget, oldBudgetSet, oldSynthesize, oldFullContext := runFlags.Verbose, runFlags.Quiet, runFlags.Budget, runFlags.BudgetSet, runSynthesize, runFlags.FullContext
	oldDryRun, oldAuto, oldModel, oldYes, oldResearch, oldNoResearch, oldNoLedger, oldContext := runFlags.DryRun, runFlags.Auto, runFlags.Model, runFlags.Yes, runFlags.Research, runFlags.NoResearch, runFlags.NoLedger, runFlags.Context

	runFlags.Config = configPath
	runFlags.Topic = "artifact resolution"
	runFlags.TimeLimit = 60
	runFlags.Window = 2
	runFlags.MaxTurns = 1
	runFlags.Output = outputPath
	runFlags.Verbose = false
	runFlags.Quiet = false
	runFlags.Budget = 0
	runFlags.BudgetSet = false
	runSynthesize = false
	runFlags.FullContext = false
	runFlags.DryRun = true
	runFlags.Auto = ""
	runFlags.Model = defaultModel
	runFlags.Yes = true
	runFlags.Research = false
	runFlags.NoResearch = false
	runFlags.NoLedger = false
	runFlags.Context = nil

	return func() {
		runFlags.Config, runFlags.Topic, runFlags.TimeLimit, runFlags.Window, runFlags.MaxTurns, runFlags.Output = oldConfig, oldTopic, oldTimeLimit, oldWindow, oldMaxTurns, oldOutput
		runFlags.Verbose, runFlags.Quiet, runFlags.Budget, runFlags.BudgetSet, runSynthesize, runFlags.FullContext = oldVerbose, oldQuiet, oldBudget, oldBudgetSet, oldSynthesize, oldFullContext
		runFlags.DryRun, runFlags.Auto, runFlags.Model, runFlags.Yes, runFlags.Research, runFlags.NoResearch, runFlags.NoLedger, runFlags.Context = oldDryRun, oldAuto, oldModel, oldYes, oldResearch, oldNoResearch, oldNoLedger, oldContext
	}
}

func configureResumeGlobals(configPath, sourcePath, outputPath string) func() {
	oldConfig, oldTopic, oldTimeLimit, oldWindow, oldMaxTurns, oldOutput := resumeFlags.Config, resumeFlags.Topic, resumeFlags.TimeLimit, resumeFlags.Window, resumeFlags.MaxTurns, resumeFlags.Output
	oldVerbose, oldQuiet, oldBudget, oldBudgetSet, oldFullContext, oldDryRun := resumeFlags.Verbose, resumeFlags.Quiet, resumeFlags.Budget, resumeFlags.BudgetSet, resumeFlags.FullContext, resumeFlags.DryRun
	oldAuto, oldModel, oldYes, oldFile, oldResearch, oldNoResearch, oldNoLedger, oldContext := resumeFlags.Auto, resumeFlags.Model, resumeFlags.Yes, resumeFile, resumeFlags.Research, resumeFlags.NoResearch, resumeFlags.NoLedger, resumeFlags.Context

	resumeFlags.Config = configPath
	resumeFlags.Topic = "artifact resolution"
	resumeFlags.TimeLimit = 60
	resumeFlags.Window = 2
	resumeFlags.MaxTurns = 0
	resumeFlags.Output = outputPath
	resumeFlags.Verbose = false
	resumeFlags.Quiet = false
	resumeFlags.Budget = 0
	resumeFlags.BudgetSet = false
	resumeFlags.FullContext = false
	resumeFlags.DryRun = true
	resumeFlags.Auto = ""
	resumeFlags.Model = defaultModel
	resumeFlags.Yes = true
	resumeFile = sourcePath
	resumeFlags.Research = false
	resumeFlags.NoResearch = false
	resumeFlags.NoLedger = false
	resumeFlags.Context = nil

	return func() {
		resumeFlags.Config, resumeFlags.Topic, resumeFlags.TimeLimit, resumeFlags.Window, resumeFlags.MaxTurns, resumeFlags.Output = oldConfig, oldTopic, oldTimeLimit, oldWindow, oldMaxTurns, oldOutput
		resumeFlags.Verbose, resumeFlags.Quiet, resumeFlags.Budget, resumeFlags.BudgetSet, resumeFlags.FullContext, resumeFlags.DryRun = oldVerbose, oldQuiet, oldBudget, oldBudgetSet, oldFullContext, oldDryRun
		resumeFlags.Auto, resumeFlags.Model, resumeFlags.Yes, resumeFile, resumeFlags.Research, resumeFlags.NoResearch, resumeFlags.NoLedger, resumeFlags.Context = oldAuto, oldModel, oldYes, oldFile, oldResearch, oldNoResearch, oldNoLedger, oldContext
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

// --- requireAutoApprovalForNonTTY tests ---------------------------

func TestRequireAutoApprovalNonTTYYesSkips(t *testing.T) {
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = old })

	err := requireAutoApprovalForNonTTY(true, false)
	if err != nil {
		t.Fatalf("expected nil for yes=true, got: %v", err)
	}
}

func TestRequireAutoApprovalNonTTYDryRunSkips(t *testing.T) {
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = old })

	err := requireAutoApprovalForNonTTY(false, true)
	if err != nil {
		t.Fatalf("expected nil for dryRun=true, got: %v", err)
	}
}

func TestRequireAutoApprovalNonTTYBothBypass(t *testing.T) {
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = old })

	err := requireAutoApprovalForNonTTY(true, true)
	if err != nil {
		t.Fatalf("expected nil for both yes=true dryRun=true, got: %v", err)
	}
}

func TestRequireAutoApprovalTTYInteractiveSkips(t *testing.T) {
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdinIsTerminal = old })

	err := requireAutoApprovalForNonTTY(false, false)
	if err != nil {
		t.Fatalf("expected nil for TTY stdin, got: %v", err)
	}
}

func TestSynthesisUnresolvedAfterConsensus(t *testing.T) {
	result := session.Result{
		HaltedBy:   "consensus (3 consecutive agreements)",
		OutputPath: "/tmp/transcript.jsonl",
		Synthesis: map[string]any{
			"confidence": "high",
			"unresolved_tensions": []any{
				"The internal friction in Law 2 between aggressive compute and minimalist code.",
			},
		},
	}
	if !synthesisUnresolvedAfterConsensus(result) {
		t.Fatal("expected unresolved synthesis after consensus halt")
	}

	quiet := session.Result{
		HaltedBy: "consensus (3 consecutive agreements)",
		Synthesis: map[string]any{
			"confidence": "high",
			"unresolved_tensions": []any{
				"None identified; the linguistic architect provided a finalized version.",
			},
		},
	}
	if synthesisUnresolvedAfterConsensus(quiet) {
		t.Fatal("expected no warning when tensions are none identified")
	}

	noneSemicolon := session.Result{
		HaltedBy: "consensus (3 consecutive agreements)",
		Synthesis: map[string]any{
			"confidence": "high",
			"unresolved_tensions": []any{
				"None; the deliberation reached a unanimous consensus on the final wording.",
			},
		},
	}
	if synthesisUnresolvedAfterConsensus(noneSemicolon) {
		t.Fatal("expected no warning when tension item begins with none")
	}
}

func TestRequireAutoApprovalNonTTYAcceptsEnvYes(t *testing.T) {
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = old })

	t.Setenv("AGORA_YES", "1")
	if err := requireAutoApprovalForNonTTY(false, false); err != nil {
		t.Fatalf("expected nil for AGORA_YES=1, got: %v", err)
	}
}

func TestRequireAutoApprovalNonTTYNoYesDryRunErrors(t *testing.T) {
	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = old })

	err := requireAutoApprovalForNonTTY(false, false)
	if err == nil {
		t.Fatal("expected error for non-TTY without --yes or --dry-run")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--yes") {
		t.Fatalf("error should mention --yes, got: %s", msg)
	}
	if !strings.Contains(msg, "--dry-run") {
		t.Fatalf("error should mention --dry-run, got: %s", msg)
	}
}

func TestRunCommandAutoNonTTYRequiresYesOrDryRun(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	t.Chdir(dir)

	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = old })
	restore := configureRunGlobals("", filepath.Join(dir, "run.jsonl"))
	defer restore()

	runFlags.Auto = "quick"
	runFlags.Topic = "test topic"
	runFlags.Yes = false
	runFlags.DryRun = false

	cmd := artifactCommand(t, filepath.Join(dir, "run.jsonl"))
	err := runCmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for run auto non-TTY without --yes or --dry-run")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--yes") {
		t.Fatalf("error should mention --yes, got: %s", msg)
	}
	if !strings.Contains(msg, "--dry-run") {
		t.Fatalf("error should mention --dry-run, got: %s", msg)
	}
}

func TestResumeCommandAutoNonTTYRequiresYesOrDryRun(t *testing.T) {
	dir := t.TempDir()
	writeSettings(t, "")
	t.Chdir(dir)

	old := stdinIsTerminal
	stdinIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdinIsTerminal = old })
	restore := configureResumeGlobals("", filepath.Join(dir, "source.jsonl"), filepath.Join(dir, "resume.jsonl"))
	defer restore()

	resumeFlags.Auto = "quick"
	resumeFlags.Topic = "test topic"
	resumeFlags.Yes = false
	resumeFlags.DryRun = false

	cmd := artifactCommand(t, filepath.Join(dir, "resume.jsonl"))
	err := resumeCmd.RunE(cmd, nil)
	if err == nil {
		t.Fatal("expected error for resume auto non-TTY without --yes or --dry-run")
	}
	msg := err.Error()
	if !strings.Contains(msg, "--yes") {
		t.Fatalf("error should mention --yes, got: %s", msg)
	}
	if !strings.Contains(msg, "--dry-run") {
		t.Fatalf("error should mention --dry-run, got: %s", msg)
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
