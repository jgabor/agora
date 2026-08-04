package types

import (
	"encoding/json"
	"strings"
	"testing"
)

func protocolStateWithProposal() *DeliberationControlState {
	state := NewDeliberationControlState([]string{"alpha", "beta"}, 2)
	state.CurrentProposalVersion = 1
	state.Proposals = append(state.Proposals, CanonicalProposal{
		Version: 1, AuthorID: "alpha", Content: "proposal one",
	})
	return state
}

func cloneControlState(t *testing.T, state *DeliberationControlState) *DeliberationControlState {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal control state: %v", err)
	}
	var clone DeliberationControlState
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatalf("unmarshal control state: %v", err)
	}
	return &clone
}

func TestNewDeliberationControlState(t *testing.T) {
	state := NewDeliberationControlState([]string{"alpha", "beta"}, 3)
	if err := state.Validate(); err != nil {
		t.Fatalf("initial state should validate: %v", err)
	}
	if state.ProtocolVersion != DeliberationProtocolVersion || state.Phase != PhaseOpening || state.CurrentProposalVersion != 0 {
		t.Fatalf("initial protocol identity: %#v", state)
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, field := range []string{
		`"protocol_version"`, `"phase"`, `"current_proposal_version"`,
		`"objections"`, `"dispositions"`, `"votes"`, `"claims"`,
		`"moderator_action"`, `"convergence"`, `"outcome"`,
	} {
		if !strings.Contains(string(data), field) {
			t.Errorf("initial state JSON missing %s: %s", field, data)
		}
	}

	invalid := NewDeliberationControlState([]string{"alpha", "alpha"}, 0)
	if err := invalid.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate agent identity") {
		t.Fatalf("duplicate identity error: got %v", err)
	}
}

func TestDeliberationControlStateRejectsInvalidReferences(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DeliberationControlState)
		want   string
	}{
		{
			name: "unknown agent",
			mutate: func(state *DeliberationControlState) {
				state.Votes = append(state.Votes, ProposalVote{AgentID: "gamma", ProposalVersion: 1, Choice: VoteEndorse})
			},
			want: "unknown agent",
		},
		{
			name: "unknown proposal",
			mutate: func(state *DeliberationControlState) {
				state.Votes = append(state.Votes, ProposalVote{AgentID: "alpha", ProposalVersion: 2, Choice: VoteEndorse})
			},
			want: "unknown proposal",
		},
		{
			name: "unknown objection",
			mutate: func(state *DeliberationControlState) {
				state.Dispositions = append(state.Dispositions, ObjectionDisposition{ObjectionID: "missing", AgentID: "alpha", Status: DispositionResolved})
			},
			want: "unknown objection",
		},
		{
			name: "unknown evidence source",
			mutate: func(state *DeliberationControlState) {
				state.Claims = append(state.Claims, ClaimEvidence{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: ClaimFact, Decisive: true, Status: EvidenceUnverified, SourceRefs: []int{2}})
			},
			want: "unknown source",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := protocolStateWithProposal()
			tt.mutate(state)
			if err := state.Validate(); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("validation error: got %v, want %q", err, tt.want)
			}
		})
	}
}

func TestDeliberationControlStateRejectsDuplicateCurrentVote(t *testing.T) {
	state := protocolStateWithProposal()
	state.Votes = []ProposalVote{
		{AgentID: "alpha", ProposalVersion: 1, Choice: VoteEndorse},
		{AgentID: "alpha", ProposalVersion: 1, Choice: VoteReject},
	}
	if err := state.Validate(); err == nil || !strings.Contains(err.Error(), "duplicate current vote") {
		t.Fatalf("duplicate current vote error: got %v", err)
	}
}

func TestProposalRevisionStalesVotesAndPreservesOpenState(t *testing.T) {
	previous := protocolStateWithProposal()
	previous.Objections = []Objection{{ID: "obj-1", AgentID: "beta", ProposalVersion: 1, ClaimID: "claim-1", Summary: "needs support"}}
	previous.Claims = []ClaimEvidence{{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: ClaimFact, Decisive: true, Status: EvidenceUnverified, SourceRefs: []int{0}}}
	previous.Votes = []ProposalVote{{AgentID: "alpha", ProposalVersion: 1, Choice: VoteEndorse}}
	if err := previous.Validate(); err != nil {
		t.Fatalf("previous state: %v", err)
	}

	next := cloneControlState(t, previous)
	next.CurrentProposalVersion = 2
	next.Proposals = append(next.Proposals, CanonicalProposal{Version: 2, AuthorID: "beta", Content: "proposal two", Supersedes: 1})
	next.Votes = append(next.Votes, ProposalVote{AgentID: "beta", ProposalVersion: 2, Choice: VoteEndorse})
	if err := ValidateDeliberationTransition(previous, next); err != nil {
		t.Fatalf("valid revision transition: %v", err)
	}
	if got := next.CurrentVotes(); len(got) != 1 || got[0].AgentID != "beta" {
		t.Fatalf("current votes: got %#v, want only beta's v2 vote", got)
	}
	if next.IsCurrentVote(previous.Votes[0]) {
		t.Fatal("v1 vote should be stale after v2 becomes current")
	}
	if len(next.UnresolvedObjections()) != 1 || len(next.EvidenceGaps()) != 1 {
		t.Fatalf("open state was not preserved: objections=%#v gaps=%#v", next.UnresolvedObjections(), next.EvidenceGaps())
	}

	invalid := cloneControlState(t, next)
	invalid.Objections = nil
	if err := ValidateDeliberationTransition(previous, invalid); err == nil || !strings.Contains(err.Error(), "history cannot be removed") {
		t.Fatalf("removed history transition error: got %v", err)
	}
}

func TestValidateDeliberationTransitionTerminalStateIsImmutable(t *testing.T) {
	previous := protocolStateWithProposal()
	previous.Phase = PhaseTerminal
	previous.Outcome = TerminalOutcome{Kind: OutcomeConsensus, ProposalVersion: 1, DissentingAgentIDs: []string{}, UnresolvedObjectionIDs: []string{}, EvidenceGapClaimIDs: []string{}}
	if err := previous.Validate(); err != nil {
		t.Fatalf("terminal previous state: %v", err)
	}

	exactRepeat := cloneControlState(t, previous)
	if err := ValidateDeliberationTransition(previous, exactRepeat); err != nil {
		t.Fatalf("exact terminal snapshot repeat: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*DeliberationControlState)
	}{
		{
			name: "append valid vote",
			mutate: func(next *DeliberationControlState) {
				next.Votes = append(next.Votes, ProposalVote{AgentID: "alpha", ProposalVersion: 1, Choice: VoteEndorse})
			},
		},
		{
			name: "change convergence signal",
			mutate: func(next *DeliberationControlState) {
				next.Convergence.StagnantRounds++
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			next := cloneControlState(t, previous)
			tt.mutate(next)
			if err := next.Validate(); err != nil {
				t.Fatalf("mutated state should remain individually valid: %v", err)
			}
			if err := ValidateDeliberationTransition(previous, next); err == nil || !strings.Contains(err.Error(), "terminal control state is immutable") {
				t.Fatalf("terminal mutation error: got %v", err)
			}
		})
	}
}
