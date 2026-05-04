package autogen

import (
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/types"
)

// ---------------------------------------------------------------------------
// Mock Runner
// ---------------------------------------------------------------------------

// mockRunner is a test double for agent.Runner that returns a canned response.
type mockRunner struct {
	response string
	err      error
}

func (m *mockRunner) Run(_ types.AgentConfig, _ map[string]any) (string, map[string]any, error) {
	if m.err != nil {
		return "", nil, m.err
	}
	return m.response, nil, nil
}

// capturingRunner records the AgentConfig it receives so tests can inspect
// the system prompt, while still returning a canned response.
type capturingRunner struct {
	response     string
	lastAgent    types.AgentConfig
	lastEnvelope map[string]any
}

func (c *capturingRunner) Run(agent types.AgentConfig, envelope map[string]any) (string, map[string]any, error) {
	c.lastAgent = agent
	c.lastEnvelope = envelope
	return c.response, nil, nil
}

// ---------------------------------------------------------------------------
// Test: valid config at Quick level
// ---------------------------------------------------------------------------

func TestGenerateConfigValid(t *testing.T) {
	yaml := `
topology: mesh
consensus_threshold: 0
agents:
  - id: advocate
    model: openai/gpt-4
    system_prompt: You argue in favor.
  - id: skeptic
    model: openai/gpt-4
    system_prompt: You argue against.
`
	runner := &mockRunner{response: yaml}

	cfg, err := GenerateConfig("Is microservices worth it?", types.AutoQuick, "openai/gpt-4", runner)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	if len(cfg.Agents) > 2 {
		t.Errorf("agents: got %d, want ≤2 (Quick cap)", len(cfg.Agents))
	}
	if len(cfg.Agents) != 2 {
		t.Errorf("agents: got %d, want 2", len(cfg.Agents))
	}
	if cfg.Topology != types.TopologyMesh {
		t.Errorf("topology: got %q, want %q", cfg.Topology, types.TopologyMesh)
	}
}

// ---------------------------------------------------------------------------
// Test: invalid YAML
// ---------------------------------------------------------------------------

func TestGenerateConfigInvalidYAML(t *testing.T) {
	runner := &mockRunner{response: "this is not yaml at all {{{{"}

	_, err := GenerateConfig("Is microservices worth it?", types.AutoQuick, "openai/gpt-4", runner)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
	// Error should mention "auto config generation failed", not a raw parse error.
	if !strings.Contains(err.Error(), "auto config generation failed") {
		t.Errorf("error should mention 'auto config generation failed', got: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Test: exceeds caps (5 agents for Quick, max 2)
// ---------------------------------------------------------------------------

func TestGenerateConfigExceedsCaps(t *testing.T) {
	yaml := `
topology: mesh
consensus_threshold: 0
agents:
  - id: a1
    model: openai/gpt-4
    system_prompt: Role 1
  - id: a2
    model: openai/gpt-4
    system_prompt: Role 2
  - id: a3
    model: openai/gpt-4
    system_prompt: Role 3
  - id: a4
    model: openai/gpt-4
    system_prompt: Role 4
  - id: a5
    model: openai/gpt-4
    system_prompt: Role 5
`
	runner := &mockRunner{response: yaml}

	_, err := GenerateConfig("Is microservices worth it?", types.AutoQuick, "openai/gpt-4", runner)
	if err == nil {
		t.Fatal("expected error for exceeding level caps")
	}
	if !strings.Contains(err.Error(), "auto config generation failed") {
		t.Errorf("error should mention 'auto config generation failed', got: %q", err.Error())
	}
	if !strings.Contains(err.Error(), "exceeds level cap") {
		t.Errorf("error should mention exceeding level cap, got: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Test: YOLO level — no agent count constraint in prompt
// ---------------------------------------------------------------------------

func TestGenerateConfigYOLO(t *testing.T) {
	yaml := `
topology: ring
consensus_threshold: 0
agents:
  - id: visionary
    model: openai/gpt-4
    system_prompt: Think big.
  - id: pragmatist
    model: openai/gpt-4
    system_prompt: Stay grounded.
  - id: critic
    model: openai/gpt-4
    system_prompt: Find flaws.
`
	runner := &capturingRunner{response: yaml}

	cfg, err := GenerateConfig("What is consciousness?", types.AutoYOLO, "openai/gpt-4", runner)
	if err != nil {
		t.Fatalf("GenerateConfig: %v", err)
	}

	if len(cfg.Agents) != 3 {
		t.Errorf("agents: got %d, want 3", len(cfg.Agents))
	}

	// Verify system prompt does NOT contain agent count constraint.
	if strings.Contains(runner.lastAgent.SystemPrompt, "Maximum") {
		t.Errorf("YOLO system prompt should not contain agent count constraint, got:\n%s", runner.lastAgent.SystemPrompt)
	}
	if strings.Contains(runner.lastAgent.SystemPrompt, "Maximum 0 agents") {
		t.Errorf("YOLO system prompt should not contain 'Maximum 0 agents', got:\n%s", runner.lastAgent.SystemPrompt)
	}
}

// ---------------------------------------------------------------------------
// Test: code fence stripping
// ---------------------------------------------------------------------------

func TestStripCodeFences(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "yaml fence",
			input: "```yaml\ntopology: ring\n```\n",
			want:  "topology: ring",
		},
		{
			name:  "yml fence",
			input: "```yml\ntopology: ring\n```\n",
			want:  "topology: ring",
		},
		{
			name:  "bare fence",
			input: "```\ntopology: ring\n```\n",
			want:  "topology: ring",
		},
		{
			name:  "no fence",
			input: "topology: ring\n",
			want:  "topology: ring",
		},
		{
			name:  "fence with preamble",
			input: "Here is the config:\n```yaml\ntopology: ring\n```\n",
			want:  "topology: ring",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripCodeFences(tt.input)
			if got != tt.want {
				t.Errorf("stripCodeFences(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Test: system prompt includes agent count for non-YOLO levels
// ---------------------------------------------------------------------------

func TestBuildSystemPromptIncludesCaps(t *testing.T) {
	caps := types.CapsForLevel(types.AutoQuick)
	prompt := buildSystemPrompt("test topic", types.AutoQuick, "openai/gpt-4", caps)

	if !strings.Contains(prompt, "Maximum 2 agents") {
		t.Errorf("Quick prompt should contain 'Maximum 2 agents', got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "openai/gpt-4") {
		t.Errorf("prompt should contain model name, got:\n%s", prompt)
	}
	if !strings.Contains(prompt, "test topic") {
		t.Errorf("prompt should contain topic, got:\n%s", prompt)
	}
}

func TestBuildSystemPromptYOLONoCaps(t *testing.T) {
	caps := types.CapsForLevel(types.AutoYOLO)
	prompt := buildSystemPrompt("test topic", types.AutoYOLO, "openai/gpt-4", caps)

	if strings.Contains(prompt, "Maximum") {
		t.Errorf("YOLO prompt should NOT contain 'Maximum' constraint, got:\n%s", prompt)
	}
}
