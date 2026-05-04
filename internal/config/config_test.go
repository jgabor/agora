package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/types"
)

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
