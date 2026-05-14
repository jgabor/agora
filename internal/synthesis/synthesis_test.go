package synthesis

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/types"
)

// mockRunner is a Runner whose Run method returns canned responses.
type mockRunner struct {
	content  string
	metadata map[string]any
	err      error
	agent    types.AgentConfig
	envelope map[string]any
}

func (m *mockRunner) Run(ag types.AgentConfig, envelope map[string]any) (string, map[string]any, error) {
	m.agent = ag
	m.envelope = envelope
	if m.err != nil {
		return "", nil, m.err
	}
	return m.content, m.metadata, nil
}

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]any
		wantErr bool
	}{
		{
			name:  "json code block",
			input: "```json\n{\"key_arguments\": [\"arg1\"]}\n```",
			want:  map[string]any{"key_arguments": []any{"arg1"}},
		},
		{
			name:  "plain code block",
			input: "```\n{\"key_arguments\": [\"arg1\"]}\n```",
			want:  map[string]any{"key_arguments": []any{"arg1"}},
		},
		{
			name:  "raw json",
			input: `{"key_arguments": ["arg1"]}`,
			want:  map[string]any{"key_arguments": []any{"arg1"}},
		},
		{
			name:    "no json found",
			input:   "just text without json",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "malformed json",
			input:   `{"key_arguments": broken}`,
			wantErr: true,
		},
		{
			name:  "multiple blocks picks first",
			input: "```json\n{\"first\": true}\n```\n```json\n{\"second\": true}\n```",
			want:  map[string]any{"first": true},
		},
		{
			name:  "json in code block with text",
			input: "Here is the result:\n```json\n{\"confidence\": \"high\"}\n```\nDone.",
			want:  map[string]any{"confidence": "high"},
		},
	}

	se := &synthesisEngine{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := se.extractJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestFormatTranscript(t *testing.T) {
	records := []types.TurnRecord{
		{Turn: -1, AgentID: "orchestrator", Content: "Begin topic: test"},
		{Turn: 0, AgentID: "agent-0", Content: "I think X is correct."},
		{Turn: 1, AgentID: "agent-1", Content: "I disagree because Y."},
	}

	se := &synthesisEngine{}
	result := se.formatTranscript(records)
	expected := "[Turn -1] orchestrator: Begin topic: test\n[Turn 0] agent-0: I think X is correct.\n[Turn 1] agent-1: I disagree because Y."
	if result != expected {
		t.Errorf("expected:\n%s\n\ngot:\n%s", expected, result)
	}
}

func TestSynthesize(t *testing.T) {
	records := []types.TurnRecord{
		{Turn: -1, AgentID: "orchestrator", Content: "seed"},
		{Turn: 0, AgentID: "agent-0", Content: "proposal"},
		{Turn: 1, AgentID: "agent-1", Content: "critique"},
	}

	t.Run("successful synthesis", func(t *testing.T) {
		mock := &mockRunner{
			content: "```json\n{\"key_arguments\":[\"arg1\",\"arg2\"],\"points_of_agreement\":[\"point1\"],\"unresolved_tensions\":[\"tension1\"],\"recommended_decision\":\"go with option A\",\"confidence\":\"high\"}\n```",
		}
		result := Synthesize(mock, records, "test topic", "test-model")

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result["confidence"] != "high" {
			t.Errorf("expected confidence=high, got %v", result["confidence"])
		}
		if result["recommended_decision"] != "go with option A" {
			t.Errorf("expected recommended_decision, got %v", result["recommended_decision"])
		}
		if !strings.HasPrefix(mock.agent.SystemPrompt, agent.ReadOnlyHint) {
			t.Fatalf("synthesis prompt = %q, want read-only hint", mock.agent.SystemPrompt)
		}
	})

	t.Run("runner error", func(t *testing.T) {
		mock := &mockRunner{err: fmt.Errorf("LLM unavailable")}
		result := Synthesize(mock, records, "test topic", "test-model")

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result["confidence"] != "low" {
			t.Errorf("expected confidence=low on error, got %v", result["confidence"])
		}
		if !strings.Contains(result["recommended_decision"].(string), "Synthesis failed") {
			t.Errorf("expected error message in recommendation, got %v", result["recommended_decision"])
		}
	})

	t.Run("invalid json response", func(t *testing.T) {
		mock := &mockRunner{
			content: "This is not valid JSON and has no code block.",
		}
		result := Synthesize(mock, records, "test topic", "test-model")

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result["confidence"] != "low" {
			t.Errorf("expected confidence=low on invalid JSON, got %v", result["confidence"])
		}
	})

	t.Run("uses specified model", func(t *testing.T) {
		mock := &mockRunner{
			content: "```json\n{\"confidence\":\"high\",\"recommended_decision\":\"use gpt-4\",\"key_arguments\":[],\"points_of_agreement\":[],\"unresolved_tensions\":[]}\n```",
		}
		result := Synthesize(mock, records, "test topic", "gpt-4")

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if mock.agent.Model != "gpt-4" {
			t.Errorf("expected model=gpt-4, got %q", mock.agent.Model)
		}
	})
}
