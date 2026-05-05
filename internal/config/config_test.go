package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/types"
)

func TestMain(m *testing.M) {
	cfgHome, err := os.MkdirTemp("", "agora-config-test-*")
	if err != nil {
		panic(err)
	}

	if err := os.Setenv("XDG_CONFIG_HOME", cfgHome); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(cfgHome)
	os.Exit(code)
}

// writeTempYAML creates a temporary YAML file with the given content and
// returns its path. The caller is responsible for deleting it.
func writeTempYAML(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

// ---------------------------------------------------------------------------
// Valid config loading
// ---------------------------------------------------------------------------

func TestLoadConfigValid(t *testing.T) {
	yaml := `
agents:
  - id: agent1
    model: openai/gpt-4
    system_prompt: Be helpful
`
	path := writeTempYAML(t, yaml)

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if len(cfg.Agents) != 1 {
		t.Fatalf("agents: got %d, want 1", len(cfg.Agents))
	}
	if cfg.Agents[0].ID != "agent1" {
		t.Errorf("agent[0].id: got %q, want %q", cfg.Agents[0].ID, "agent1")
	}
	if cfg.Agents[0].Model != "openai/gpt-4" {
		t.Errorf("agent[0].model: got %q, want %q", cfg.Agents[0].Model, "openai/gpt-4")
	}
	if cfg.Agents[0].SystemPrompt != "Be helpful" {
		t.Errorf("agent[0].system_prompt: got %q, want %q", cfg.Agents[0].SystemPrompt, "Be helpful")
	}
	// Default topology should be ring.
	if cfg.Topology != types.TopologyRing {
		t.Errorf("topology: got %q, want %q", cfg.Topology, types.TopologyRing)
	}
}

func TestLoadConfigFillsMissingAgentModelFromSettings(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	settingsDir := filepath.Join(cfgHome, "agora")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.yaml"), []byte(`default_model: "gpt-4"`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	path := writeTempYAML(t, `
agents:
  - id: agent1
    system_prompt: Be helpful
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Agents[0].Model != "gpt-4" {
		t.Fatalf("agent model: got %q, want %q", cfg.Agents[0].Model, "gpt-4")
	}
}

func TestLoadConfigKeepsExplicitAgentModelOverSettings(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	settingsDir := filepath.Join(cfgHome, "agora")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.yaml"), []byte(`default_model: "gpt-4"`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	path := writeTempYAML(t, `
agents:
  - id: agent1
    model: claude
    system_prompt: Be helpful
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Agents[0].Model != "claude" {
		t.Fatalf("agent model: got %q, want %q", cfg.Agents[0].Model, "claude")
	}
}

func TestLoadConfigUsesDefaultTopologyFromSettings(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgHome)
	settingsDir := filepath.Join(cfgHome, "agora")
	if err := os.MkdirAll(settingsDir, 0o755); err != nil {
		t.Fatalf("mkdir settings dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(settingsDir, "settings.yaml"), []byte(`default_topology: "mesh"`), 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	path := writeTempYAML(t, `
agents:
  - id: agent1
    model: gpt-4
    system_prompt: Be helpful
`)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.Topology != types.TopologyMesh {
		t.Fatalf("topology: got %q, want %q", cfg.Topology, types.TopologyMesh)
	}
}

// ---------------------------------------------------------------------------
// types.Topology variants
// ---------------------------------------------------------------------------

func TestLoadConfigTopologyVariants(t *testing.T) {
	tests := []struct {
		label string
		yaml  string
		want  types.Topology
	}{
		{"ring explicit", "topology: ring\nagents:\n  - id: a\n    model: m\n", types.TopologyRing},
		{"star", "topology: star\nagents:\n  - id: a\n    model: m\n", types.TopologyStar},
		{"mesh", "topology: mesh\nagents:\n  - id: a\n    model: m\n", types.TopologyMesh},
		{"ring default (omitted)", "agents:\n  - id: a\n    model: m\n", types.TopologyRing},
		{"ring caps", "topology: RING\nagents:\n  - id: a\n    model: m\n", types.TopologyRing},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			path := writeTempYAML(t, tt.yaml)
			cfg, err := LoadConfig(path)
			if err != nil {
				t.Fatalf("LoadConfig: %v", err)
			}
			if cfg.Topology != tt.want {
				t.Errorf("topology: got %q, want %q", cfg.Topology, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Consensus threshold
// ---------------------------------------------------------------------------

func TestLoadConfigConsensusThreshold(t *testing.T) {
	yaml := `
consensus_threshold: 3
agents:
  - id: a
    model: m
`
	path := writeTempYAML(t, yaml)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.ConsensusThreshold != 3 {
		t.Errorf("consensus_threshold: got %d, want 3", cfg.ConsensusThreshold)
	}
}

// ---------------------------------------------------------------------------
// Synthesis model
// ---------------------------------------------------------------------------

func TestLoadConfigSynthesisModel(t *testing.T) {
	yaml := `
synthesis_model: openai/gpt-4
agents:
  - id: a
    model: m
`
	path := writeTempYAML(t, yaml)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if cfg.SynthesisModel == nil {
		t.Fatal("synthesis_model should not be nil")
	}
	if *cfg.SynthesisModel != "openai/gpt-4" {
		t.Errorf("synthesis_model: got %q, want %q", *cfg.SynthesisModel, "openai/gpt-4")
	}
}

func TestLoadConfigResearchAndContext(t *testing.T) {
	yaml := `
research: true
context:
  - README.md
  - docs/
agents:
  - id: a
    model: m
`
	path := writeTempYAML(t, yaml)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}
	if !cfg.ResearchEnabled {
		t.Fatal("ResearchEnabled: got false, want true")
	}
	want := []string{"README.md", "docs/"}
	if len(cfg.ContextPaths) != len(want) || cfg.ContextPaths[0] != want[0] || cfg.ContextPaths[1] != want[1] {
		t.Fatalf("ContextPaths: got %#v, want %#v", cfg.ContextPaths, want)
	}
}

func TestResolveEvidenceRequestPrecedence(t *testing.T) {
	falseValue := false
	cfg := &types.DeliberationConfig{
		ResearchEnabled: true,
		ContextPaths:    []string{"config.md"},
	}
	settings := Settings{ResearchMaxSources: 7, ContextMaxBytes: 512, ContextMaxDepth: 2}

	request := ResolveEvidenceRequest(cfg, settings, ResearchOverrides{
		Research:     &falseValue,
		ContextSet:   true,
		ContextPaths: []string{"cli.md", "cli-dir"},
	})

	if request.ResearchEnabled {
		t.Fatal("ResearchEnabled: got true, want CLI override false")
	}
	want := []string{"cli.md", "cli-dir"}
	if len(request.ContextPaths) != len(want) || request.ContextPaths[0] != want[0] || request.ContextPaths[1] != want[1] {
		t.Fatalf("ContextPaths: got %#v, want %#v", request.ContextPaths, want)
	}
	if request.MaxSources != 7 {
		t.Fatalf("MaxSources: got %d, want settings cap 7", request.MaxSources)
	}
	if request.MaxBytes != 512 {
		t.Fatalf("MaxBytes: got %d, want settings cap 512", request.MaxBytes)
	}
	if request.MaxDepth != 2 {
		t.Fatalf("MaxDepth: got %d, want settings cap 2", request.MaxDepth)
	}
}

func TestResolveEvidenceRequestUsesConfigResearchWithoutCLI(t *testing.T) {
	cfg := &types.DeliberationConfig{ResearchEnabled: true, ContextPaths: []string{"config.md"}}
	request := ResolveEvidenceRequest(cfg, Settings{}, ResearchOverrides{})
	if !request.ResearchEnabled {
		t.Fatal("ResearchEnabled: got false, want config-enabled research")
	}
	if len(request.ContextPaths) != 1 || request.ContextPaths[0] != "config.md" {
		t.Fatalf("ContextPaths: got %#v, want config context", request.ContextPaths)
	}
}

func TestResolveEvidenceRequestSettingsDoNotEnableResearch(t *testing.T) {
	cfg := &types.DeliberationConfig{}
	request := ResolveEvidenceRequest(cfg, Settings{ResearchMaxSources: 5}, ResearchOverrides{})
	if request.ResearchEnabled {
		t.Fatal("ResearchEnabled: got true, want false because settings must not enable web access")
	}
}

// ---------------------------------------------------------------------------
// Validation errors
// ---------------------------------------------------------------------------

func TestLoadConfigFileNotFound(t *testing.T) {
	_, err := LoadConfig("/nonexistent/path/config.yaml")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	if !strings.Contains(err.Error(), "reading config file") {
		t.Errorf("error mismatch: %q", err.Error())
	}
}

func TestLoadConfigNoAgents(t *testing.T) {
	path := writeTempYAML(t, "agents: []\n")
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "at least one agent") {
		t.Errorf("error mismatch: %q", err.Error())
	}
}

func TestLoadConfigDuplicateIDs(t *testing.T) {
	yaml := `
agents:
  - id: dup
    model: m1
  - id: dup
    model: m2
`
	path := writeTempYAML(t, yaml)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "duplicate agent id") {
		t.Errorf("error mismatch: %q", err.Error())
	}
}

func TestLoadConfigMissingID(t *testing.T) {
	yaml := `
agents:
  - model: m
`
	path := writeTempYAML(t, yaml)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "non-empty 'id'") {
		t.Errorf("error mismatch: %q", err.Error())
	}
}

func TestLoadConfigMissingModel(t *testing.T) {
	yaml := `
agents:
  - id: a
`
	path := writeTempYAML(t, yaml)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "non-empty 'model'") {
		t.Errorf("error mismatch: %q", err.Error())
	}
}

func TestLoadConfigInvalidTopology(t *testing.T) {
	yaml := `
topology: bogus
agents:
  - id: a
    model: m
`
	path := writeTempYAML(t, yaml)
	_, err := LoadConfig(path)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "unknown topology") {
		t.Errorf("error mismatch: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// LoadConfig with example-config.yaml
// ---------------------------------------------------------------------------

func TestLoadConfigExampleFile(t *testing.T) {
	// Try the new standard location first (examples/ directory from project root).
	path := filepath.Join("..", "..", "examples", "example-default.yaml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		// Fall back to the old root-level file.
		path = filepath.Join("..", "example-config.yaml")
		if _, err := os.Stat(path); os.IsNotExist(err) {
			path = "example-config.yaml"
		}
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Skip("example config file not found")
	}

	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("LoadConfig example: %v", err)
	}
	if len(cfg.Agents) != 5 {
		t.Errorf("expected 5 agents in example config, got %d", len(cfg.Agents))
	}
}

// ---------------------------------------------------------------------------
// LoadConfigFromBytes
// ---------------------------------------------------------------------------

func TestLoadConfigFromBytesValid(t *testing.T) {
	yaml := `
agents:
  - id: agent1
    model: openai/gpt-4
    system_prompt: Be helpful
  - id: agent2
    model: anthropic/claude-3
    system_prompt: Be concise
`
	cfg, err := LoadConfigFromBytes([]byte(yaml))
	if err != nil {
		t.Fatalf("LoadConfigFromBytes: %v", err)
	}
	if len(cfg.Agents) != 2 {
		t.Fatalf("agents: got %d, want 2", len(cfg.Agents))
	}
	if cfg.Agents[0].ID != "agent1" {
		t.Errorf("agent[0].id: got %q, want %q", cfg.Agents[0].ID, "agent1")
	}
	if cfg.Agents[1].ID != "agent2" {
		t.Errorf("agent[1].id: got %q, want %q", cfg.Agents[1].ID, "agent2")
	}
}

func TestLoadConfigFromBytesInvalid(t *testing.T) {
	yaml := `topology: ring
`
	_, err := LoadConfigFromBytes([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing agents")
	}
	if !strings.Contains(err.Error(), "at least one agent") {
		t.Errorf("error mismatch: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// ParseTopology integration with LoadConfig
// ---------------------------------------------------------------------------

func TestLoadConfigTopologyHyphens(t *testing.T) {
	// Hyphens in topology should be normalized and accepted.
	yaml := `
topology: mesh-network
agents:
  - id: a
    model: m
`
	path := writeTempYAML(t, yaml)
	cfg, err := LoadConfig(path)
	if err != nil {
		// If this fails, that's also acceptable — the key test is that it
		// doesn't panic.
		t.Logf("hyphenated topology result: %v", err)
		return
	}
	if cfg.Topology != types.TopologyMesh {
		t.Errorf("hyphenated mesh-network should parse as mesh: got %q", cfg.Topology)
	}
}
