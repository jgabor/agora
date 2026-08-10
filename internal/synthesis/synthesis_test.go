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

func TestSynthesizeSuppliesCanonicalTerminalState(t *testing.T) {
	tests := []struct {
		name      string
		kind      types.TerminalOutcomeKind
		wantScope string
		votes     []types.ProposalVote
		dissents  []string
	}{
		{
			name:      "consensus",
			kind:      types.OutcomeConsensus,
			wantScope: "group_consensus",
			votes: []types.ProposalVote{
				{AgentID: "alpha", ProposalVersion: 1, Choice: types.VoteEndorse},
				{AgentID: "beta", ProposalVersion: 1, Choice: types.VoteEndorse},
			},
		},
		{
			name:      "no consensus",
			kind:      types.OutcomeNoConsensus,
			wantScope: "independent",
			votes: []types.ProposalVote{
				{AgentID: "alpha", ProposalVersion: 1, Choice: types.VoteEndorse},
				{AgentID: "beta", ProposalVersion: 1, Choice: types.VoteReject},
			},
			dissents: []string{"beta"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			control := types.NewDeliberationControlState([]string{"alpha", "beta"}, 1)
			control.Phase = types.PhaseTerminal
			control.CurrentProposalVersion = 1
			control.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "canonical proposal"}}
			control.Objections = []types.Objection{{ID: "obj-1", AgentID: "beta", ProposalVersion: 1, Summary: "needs evidence"}}
			control.Dispositions = []types.ObjectionDisposition{{ObjectionID: "obj-1", AgentID: "alpha", Status: types.DispositionSustained, Rationale: "not resolved"}}
			control.Claims = []types.ClaimEvidence{{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimFact, Decisive: true, Status: types.EvidenceUnverified}}
			control.Votes = tt.votes
			control.Convergence = types.ConvergenceSignals{RunContractVersion: types.RunContractVersion, CurrentEndorsements: 1, RequiredEndorsements: 2, MinimumRounds: 1, UnresolvedObjections: 1, EvidenceGaps: 1}
			if tt.kind == types.OutcomeConsensus {
				control.Convergence.CurrentEndorsements = 2
				control.Convergence.UnresolvedObjections = 0
				control.Convergence.EvidenceGaps = 0
				control.Objections = nil
				control.Dispositions = nil
				control.Claims = nil
			}
			control.Outcome = types.TerminalOutcome{
				Kind:                   tt.kind,
				ProposalVersion:        1,
				Reason:                 "max_turns (4)",
				DissentingAgentIDs:     tt.dissents,
				UnresolvedObjectionIDs: []string{"obj-1"},
				EvidenceGapClaimIDs:    []string{"claim-1"},
			}
			if tt.kind == types.OutcomeConsensus {
				control.Outcome.UnresolvedObjectionIDs = []string{}
				control.Outcome.EvidenceGapClaimIDs = []string{}
			}
			records := []types.TurnRecord{
				{Turn: -2, AgentID: "moderator", Evidence: &types.EvidenceBundle{Summary: "trusted evidence", SourceReferences: []types.SourceReference{{Title: "spec", URL: "https://example.test/spec"}}}},
				{Turn: -1, AgentID: "moderator", Control: control},
			}
			mock := &mockRunner{content: `{"key_arguments":[],"points_of_agreement":["model observation"],"unresolved_tensions":[],"recommended_decision":"ship independently","confidence":"medium"}`}

			result := Synthesize(mock, records, "topic", "model")
			state, ok := mock.envelope["terminal_state"].(*types.TerminalState)
			if !ok || state == nil {
				t.Fatalf("terminal_state envelope: %#v", mock.envelope["terminal_state"])
			}
			if state.CanonicalProposal == nil || state.CanonicalProposal.Content != "canonical proposal" || len(state.CurrentVotes) != 2 {
				t.Fatalf("proposal and current votes: %#v", state)
			}
			if len(state.Objections) != len(control.Objections) || len(state.Dispositions) != len(control.Dispositions) || len(state.Claims) != len(control.Claims) {
				t.Fatalf("objections, dispositions, or claims missing: %#v", state)
			}
			if state.Evidence == nil || len(state.Evidence.SourceReferences) != 1 || state.HaltReason != "max_turns (4)" {
				t.Fatalf("evidence or halt reason missing: %#v", state)
			}
			if got := result["recommendation_scope"]; got != tt.wantScope {
				t.Fatalf("recommendation_scope: got %v, want %s", got, tt.wantScope)
			}
			if tt.kind == types.OutcomeNoConsensus && result["model_recommendation_non_authoritative"] != true {
				t.Fatalf("no-consensus recommendation authority: %#v", result)
			}
			resultState, ok := result["terminal_state"].(*types.TerminalState)
			if !ok || resultState.Outcome.Kind != tt.kind || len(resultState.DissentingAgentIDs) != len(tt.dissents) {
				t.Fatalf("result terminal state: %#v", result["terminal_state"])
			}
		})
	}
}
