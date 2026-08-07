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
		`"directive"`, `"moderator_action"`, `"convergence"`, `"outcome"`,
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

func TestValidateDeliberationTransitionRequiresOrderedPhases(t *testing.T) {
	tests := []struct {
		name    string
		from    DeliberationPhase
		to      DeliberationPhase
		invalid DeliberationPhase
	}{
		{name: "opening to rebuttal", from: PhaseOpening, to: PhaseRebuttal, invalid: PhaseDrafting},
		{name: "rebuttal to drafting", from: PhaseRebuttal, to: PhaseDrafting, invalid: PhaseVoting},
		{name: "drafting to voting", from: PhaseDrafting, to: PhaseVoting, invalid: PhaseOpening},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := NewDeliberationControlState([]string{"alpha"}, 0)
			previous.Phase = tt.from
			for turn := 0; turn < phaseWorkContributions(tt.from); turn++ {
				previous.Contributions = append(previous.Contributions, AgentContribution{
					AgentID: "alpha", Turn: turn, Position: "position",
					ProposalAction: ContributionProposalAction{Kind: ProposalActionNone},
				})
			}
			if tt.from == PhaseDrafting {
				previous.CurrentProposalVersion = 1
				previous.Proposals = append(previous.Proposals, CanonicalProposal{
					Version: 1, AuthorID: "alpha", Content: "proposal",
				})
			}
			next := cloneControlState(t, previous)
			next.Phase = tt.to
			if err := ValidateDeliberationTransition(previous, next); err != nil {
				t.Fatalf("valid phase transition: %v", err)
			}
			next.Phase = tt.invalid
			if next.Phase == PhaseOpening && len(next.Contributions) > 1 {
				next.Contributions = next.Contributions[:1]
			}
			if err := ValidateDeliberationTransition(previous, next); err == nil || !strings.Contains(err.Error(), "invalid phase transition") {
				t.Fatalf("illegal phase transition error: %v", err)
			}
		})
	}
}

func phaseWorkContributions(phase DeliberationPhase) int {
	switch phase {
	case PhaseOpening:
		return 1
	case PhaseRebuttal:
		return 2
	case PhaseDrafting:
		return 3
	default:
		return 0
	}
}

func TestValidateDeliberationTransitionRequiresPhaseWork(t *testing.T) {
	tests := []struct {
		name  string
		from  DeliberationPhase
		to    DeliberationPhase
		setup func(*DeliberationControlState)
	}{
		{
			name: "opening lacks one active agent",
			from: PhaseOpening,
			to:   PhaseRebuttal,
			setup: func(state *DeliberationControlState) {
				state.AgentIDs = []string{"alpha", "beta"}
				state.Contributions = []AgentContribution{{
					AgentID: "alpha", Turn: 0, Position: "position",
					ProposalAction: ContributionProposalAction{Kind: ProposalActionNone},
				}}
			},
		},
		{
			name: "rebuttal lacks the second contribution per agent",
			from: PhaseRebuttal,
			to:   PhaseDrafting,
			setup: func(state *DeliberationControlState) {
				state.AgentIDs = []string{"alpha", "beta"}
				state.Contributions = []AgentContribution{
					{AgentID: "alpha", Turn: 0, Position: "position", ProposalAction: ContributionProposalAction{Kind: ProposalActionNone}},
					{AgentID: "beta", Turn: 1, Position: "position", ProposalAction: ContributionProposalAction{Kind: ProposalActionNone}},
					{AgentID: "alpha", Turn: 2, Position: "position", ProposalAction: ContributionProposalAction{Kind: ProposalActionNone}},
				}
			},
		},
		{
			name: "drafting lacks a proposal",
			from: PhaseDrafting,
			to:   PhaseVoting,
			setup: func(state *DeliberationControlState) {
				state.AgentIDs = []string{"alpha", "beta"}
				for turn := 0; turn < 6; turn++ {
					state.Contributions = append(state.Contributions, AgentContribution{
						AgentID: state.AgentIDs[turn%2], Turn: turn, Position: "position",
						ProposalAction: ContributionProposalAction{Kind: ProposalActionNone},
					})
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := NewDeliberationControlState([]string{"alpha", "beta"}, 0)
			previous.Phase = tt.from
			tt.setup(previous)
			next := cloneControlState(t, previous)
			next.Phase = tt.to
			if err := ValidateDeliberationTransition(previous, next); err == nil || !strings.Contains(err.Error(), "phase") {
				t.Fatalf("missing phase-work error: %v", err)
			}
		})
	}
}

func TestValidateDeliberationTransitionRequiresBalancedPhaseWork(t *testing.T) {
	makeState := func(phase DeliberationPhase, alpha, beta int, proposal bool) *DeliberationControlState {
		state := NewDeliberationControlState([]string{"alpha", "beta"}, 0)
		state.Phase = phase
		for turn := 0; turn < alpha; turn++ {
			state.Contributions = append(state.Contributions, AgentContribution{
				AgentID: "alpha", Turn: len(state.Contributions), Position: "position",
				ProposalAction: ContributionProposalAction{Kind: ProposalActionNone},
			})
		}
		for turn := 0; turn < beta; turn++ {
			state.Contributions = append(state.Contributions, AgentContribution{
				AgentID: "beta", Turn: len(state.Contributions), Position: "position",
				ProposalAction: ContributionProposalAction{Kind: ProposalActionNone},
			})
		}
		if proposal {
			state.CurrentProposalVersion = 1
			state.Proposals = []CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "proposal"}}
		}
		return state
	}
	tests := []struct {
		name      string
		from      DeliberationPhase
		to        DeliberationPhase
		alpha     int
		beta      int
		proposal  bool
		wantError bool
	}{
		{name: "rebuttal balanced pass", from: PhaseRebuttal, to: PhaseDrafting, alpha: 2, beta: 2},
		{name: "rebuttal imbalanced fail", from: PhaseRebuttal, to: PhaseDrafting, alpha: 3, beta: 1, wantError: true},
		{name: "drafting balanced pass", from: PhaseDrafting, to: PhaseVoting, alpha: 3, beta: 3, proposal: true},
		{name: "drafting imbalanced fail", from: PhaseDrafting, to: PhaseVoting, alpha: 5, beta: 1, proposal: true, wantError: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := makeState(tt.from, tt.alpha, tt.beta, tt.proposal)
			next := cloneControlState(t, previous)
			next.Phase = tt.to
			err := ValidateDeliberationTransition(previous, next)
			if tt.wantError {
				if err == nil || !strings.Contains(err.Error(), "phase") {
					t.Fatalf("imbalanced transition error: %v", err)
				}
			} else if err != nil {
				t.Fatalf("balanced transition: %v", err)
			}
		})
	}
}

func TestDeliberationControlStateValidatesEachDirectiveKind(t *testing.T) {
	base := protocolStateWithProposal()
	base.Claims = []ClaimEvidence{{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: ClaimFact, Decisive: true, Status: EvidenceUnverified, SourceRefs: []int{}}}
	base.Objections = []Objection{{ID: "objection-1", AgentID: "alpha", ProposalVersion: 1, Summary: "challenge"}}
	tests := []struct {
		name    string
		phase   DeliberationPhase
		valid   TurnDirective
		invalid TurnDirective
	}{
		{
			name:    "response",
			phase:   PhaseRebuttal,
			valid:   TurnDirective{Kind: DirectiveRespond, TargetAgentID: "beta", ObjectionID: "objection-1"},
			invalid: TurnDirective{Kind: DirectiveRespond, TargetAgentID: "beta", ObjectionID: "missing"},
		},
		{
			name:    "verification",
			phase:   PhaseDrafting,
			valid:   TurnDirective{Kind: DirectiveVerify, TargetAgentID: "beta", ClaimID: "claim-1"},
			invalid: TurnDirective{Kind: DirectiveVerify, TargetAgentID: "beta", ClaimID: "missing"},
		},
		{
			name:    "proposal revision",
			phase:   PhaseDrafting,
			valid:   TurnDirective{Kind: DirectiveReviseProposal, TargetAgentID: "beta", ProposalVersion: 1},
			invalid: TurnDirective{Kind: DirectiveReviseProposal, TargetAgentID: "beta", ProposalVersion: 2},
		},
		{
			name:    "vote",
			phase:   PhaseVoting,
			valid:   TurnDirective{Kind: DirectiveVote, TargetAgentID: "beta", ProposalVersion: 1},
			invalid: TurnDirective{Kind: DirectiveVote, TargetAgentID: "missing", ProposalVersion: 1},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			valid := cloneControlState(t, base)
			valid.Phase = tt.phase
			valid.Directive = tt.valid
			if err := valid.Validate(); err != nil {
				t.Fatalf("valid directive: %v", err)
			}
			invalid := cloneControlState(t, base)
			invalid.Phase = tt.phase
			invalid.Directive = tt.invalid
			if err := invalid.Validate(); err == nil {
				t.Fatal("invalid directive unexpectedly validated")
			}
		})
	}
}

func TestDeliberationControlStateRejectsDirectiveOutsidePhase(t *testing.T) {
	base := protocolStateWithProposal()
	base.Claims = []ClaimEvidence{{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: ClaimFact, Status: EvidenceUnverified}}
	base.Objections = []Objection{{ID: "objection-1", AgentID: "alpha", ProposalVersion: 1, Summary: "challenge"}}
	tests := []TurnDirective{
		{Kind: DirectiveRespond, TargetAgentID: "beta", ObjectionID: "objection-1"},
		{Kind: DirectiveVerify, TargetAgentID: "beta", ClaimID: "claim-1"},
		{Kind: DirectiveReviseProposal, TargetAgentID: "beta", ProposalVersion: 1},
		{Kind: DirectiveVote, TargetAgentID: "beta", ProposalVersion: 1},
	}
	for _, directive := range tests {
		t.Run(string(directive.Kind), func(t *testing.T) {
			state := cloneControlState(t, base)
			state.Directive = directive
			if err := state.Validate(); err == nil || !strings.Contains(err.Error(), "not valid in phase") {
				t.Fatalf("outside-phase directive error: %v", err)
			}
		})
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
