package synthesis

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/types"
)

// mockRunner is a Runner whose Run method returns canned responses.
type mockRunner struct {
	content  string
	metadata *types.RunMetadata
	err      error
	agent    types.AgentConfig
	envelope map[string]any
}

func (m *mockRunner) Run(ag types.AgentConfig, envelope map[string]any) (string, *types.RunMetadata, error) {
	m.agent = ag
	m.envelope = envelope
	if m.err != nil {
		return "", nil, m.err
	}
	return m.content, m.metadata, nil
}

func TestFormatTranscript(t *testing.T) {
	records := []types.TurnRecord{
		{Turn: -1, AgentID: "moderator", Content: "Begin topic: test"},
		{Turn: 0, AgentID: "agent-0", Content: "I think X is correct."},
		{Turn: 1, AgentID: "agent-1", Content: "I disagree because Y."},
	}

	se := &synthesisEngine{}
	result := se.formatTranscript(records)
	expected := "[Turn -1] moderator: Begin topic: test\n[Turn 0] agent-0: I think X is correct.\n[Turn 1] agent-1: I disagree because Y."
	if result != expected {
		t.Errorf("expected:\n%s\n\ngot:\n%s", expected, result)
	}
}

func TestSynthesize(t *testing.T) {
	records := []types.TurnRecord{
		{Turn: -1, AgentID: "moderator", Content: "seed"},
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
		if !strings.Contains(result["recommended_decision"].(string), "Synthesis could not") {
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

func TestSynthesizeAttachesCanonicalClaimEvidence(t *testing.T) {
	control := types.NewDeliberationControlState([]string{"alpha"}, 1)
	control.Claims = []types.ClaimEvidence{
		{ID: "fact-1", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimFact, Decisive: true, Status: types.EvidenceVerified, SourceRefs: []int{0}},
		{ID: "assumption-1", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimAssumption, Status: types.EvidenceUnverified, SourceRefs: []int{}},
	}
	records := []types.TurnRecord{{Turn: 0, AgentID: "alpha", Content: "proposal", Control: control}}
	mock := &mockRunner{content: `{"confidence":"high","recommended_decision":"ship","claims":[{"id":"invented","status":"verified","source_refs":[99]}]}`}

	result := Synthesize(mock, records, "topic", "model")
	claims, ok := result["claims"].([]types.ClaimEvidence)
	if !ok || len(claims) != 2 {
		t.Fatalf("canonical claims: got %#v", result["claims"])
	}
	if claims[0].ID != "fact-1" || claims[0].Status != types.EvidenceVerified || claims[1].ID != "assumption-1" {
		t.Fatalf("synthesis retained non-canonical claims: %#v", claims)
	}
}
