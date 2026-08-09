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

func consensusCandidate() *DeliberationControlState {
	state := NewDeliberationControlState([]string{"alpha", "beta"}, 2)
	state.Phase = PhaseVoting
	state.CurrentProposalVersion = 1
	state.Proposals = []CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "proposal one"}}
	state.Convergence.RequiredEndorsements = 2
	state.Convergence.MinimumRounds = 1
	state.Convergence.RequiredDeliverableItems = 0
	for turn := 0; turn < 2; turn++ {
		for _, agentID := range state.AgentIDs {
			state.Contributions = append(state.Contributions, AgentContribution{
				AgentID: agentID, Turn: len(state.Contributions), Position: "position",
				ProposalAction: ContributionProposalAction{Kind: ProposalActionNone},
			})
		}
	}
	state.Votes = []ProposalVote{
		{AgentID: "alpha", ProposalVersion: 1, Choice: VoteEndorse},
		{AgentID: "beta", ProposalVersion: 1, Choice: VoteEndorse},
	}
	return state
}

func authenticatedConsensusTerminal() *DeliberationControlState {
	state := consensusCandidate()
	state.Phase = PhaseTerminal
	state.Convergence.MinimumRounds = 1
	state.Convergence.RequiredDeliverableItems = 0
	state.Convergence.CurrentEndorsements = 2
	state.Outcome = TerminalOutcome{
		Kind:                   OutcomeConsensus,
		ProposalVersion:        1,
		DissentingAgentIDs:     []string{},
		UnresolvedObjectionIDs: []string{},
		EvidenceGapClaimIDs:    []string{},
	}
	return state
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

func TestEvaluateConsensusRequiresTypedHaltGates(t *testing.T) {
	tests := []struct {
		name        string
		mutate      func(*DeliberationControlState)
		deliverable bool
		ready       bool
	}{
		{name: "all gates pass", deliverable: true, ready: true},
		{
			name: "current proposal required",
			mutate: func(state *DeliberationControlState) {
				state.CurrentProposalVersion = 0
				state.Proposals = nil
			},
		},
		{
			name: "unique current endorsements reach threshold",
			mutate: func(state *DeliberationControlState) {
				state.Votes = state.Votes[:1]
			},
		},
		{
			name: "stale split endorsement does not count",
			mutate: func(state *DeliberationControlState) {
				state.Proposals = append(state.Proposals, CanonicalProposal{Version: 2, AuthorID: "beta", Content: "proposal two", Supersedes: 1})
				state.CurrentProposalVersion = 2
				state.Votes[1].ProposalVersion = 2
			},
		},
		{
			name: "minimum rounds required",
			mutate: func(state *DeliberationControlState) {
				state.Contributions = state.Contributions[:2]
			},
		},
		{
			name: "deliverable required",
			mutate: func(state *DeliberationControlState) {
				// The gate is supplied independently of typed control state.
			},
		},
		{name: "deliverable present", deliverable: true, ready: true},
		{
			name: "objection disposition required",
			mutate: func(state *DeliberationControlState) {
				state.Objections = append(state.Objections, Objection{ID: "obj-1", AgentID: "alpha", ProposalVersion: 1, Summary: "challenge"})
			},
		},
		{
			name: "evidence gap blocks halt",
			mutate: func(state *DeliberationControlState) {
				state.Claims = append(state.Claims, ClaimEvidence{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: ClaimFact, Decisive: true, Status: EvidenceUnverified})
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := consensusCandidate()
			if tt.mutate != nil {
				tt.mutate(state)
			}
			got := state.EvaluateConsensus(2, tt.deliverable)
			if got.Ready != tt.ready {
				t.Fatalf("ready=%v, want %v; evaluation=%+v", got.Ready, tt.ready, got)
			}
		})
	}

	resolved := consensusCandidate()
	resolved.Objections = []Objection{{ID: "obj-1", AgentID: "alpha", ProposalVersion: 1, Summary: "challenge"}}
	resolved.Dispositions = []ObjectionDisposition{{ObjectionID: "obj-1", AgentID: "beta", Status: DispositionResolved, Rationale: "addressed"}}
	if got := resolved.EvaluateConsensus(2, true); !got.Ready {
		t.Fatalf("resolved objection should pass: %+v", got)
	}

	verified := consensusCandidate()
	verified.Claims = []ClaimEvidence{{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: ClaimFact, Decisive: true, Status: EvidenceVerified, SourceRefs: []int{0}}}
	if got := verified.EvaluateConsensus(2, true); !got.Ready {
		t.Fatalf("verified evidence should pass: %+v", got)
	}

	duplicate := consensusCandidate()
	duplicate.Votes = append(duplicate.Votes, ProposalVote{AgentID: "alpha", ProposalVersion: 1, Choice: VoteEndorse})
	if got := duplicate.EvaluateConsensus(2, true); got.Ready {
		t.Fatalf("duplicate current vote advanced consensus: %+v", got)
	}
}

func TestEvaluateConsensusIgnoresAgreementProseWithoutVotes(t *testing.T) {
	state := consensusCandidate()
	state.Votes = nil
	state.Contributions[0].Position = "I agree with the proposal"
	state.Contributions[1].Position = "I agree with the proposal"
	got := state.EvaluateConsensus(2, true)
	if got.Ready {
		t.Fatalf("agreement prose advanced consensus: %+v", got)
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

func TestClaimEvidenceTransitionsRequireBoundedSourcesAndTypedOutcomes(t *testing.T) {
	previous := protocolStateWithProposal()
	previous.Claims = []ClaimEvidence{{
		ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: ClaimFact,
		Decisive: true, Status: EvidenceUnverified, SourceRefs: []int{},
	}}
	if err := previous.Validate(); err != nil {
		t.Fatalf("unverified claim should validate without a source: %v", err)
	}

	verified := cloneControlState(t, previous)
	verified.Claims[0].Status = EvidenceVerified
	verified.Claims[0].SourceRefs = []int{0}
	if err := ValidateDeliberationTransition(previous, verified); err != nil {
		t.Fatalf("verified claim transition: %v", err)
	}

	for _, status := range []ClaimEvidenceStatus{EvidenceConflicting, EvidenceUnsupported, EvidenceVerificationFailed} {
		next := cloneControlState(t, previous)
		next.Claims[0].Status = status
		if status == EvidenceConflicting {
			next.Claims[0].SourceRefs = []int{1}
		}
		if err := ValidateDeliberationTransition(previous, next); err != nil {
			t.Fatalf("%s claim transition: %v", status, err)
		}
	}

	invalidVerified := cloneControlState(t, previous)
	invalidVerified.Claims[0].Status = EvidenceVerified
	if err := invalidVerified.Validate(); err == nil || !strings.Contains(err.Error(), "requires at least one source") {
		t.Fatalf("verified claim without source accepted: %v", err)
	}

	invalidRegression := cloneControlState(t, verified)
	invalidRegression.Claims[0].Status = EvidenceUnsupported
	if err := ValidateDeliberationTransition(verified, invalidRegression); err == nil || !strings.Contains(err.Error(), "status transition") {
		t.Fatalf("verification regression accepted: %v", err)
	}

	invalidRemoval := cloneControlState(t, verified)
	invalidRemoval.Claims[0].SourceRefs = []int{}
	if err := ValidateDeliberationTransition(previous, invalidRemoval); err == nil || !strings.Contains(err.Error(), "requires at least one source") {
		t.Fatalf("source removal accepted: %v", err)
	}
}

func TestValidateTerminalConsensusRequiresAuthenticTypedState(t *testing.T) {
	unauthenticated := protocolStateWithProposal()
	unauthenticated.Phase = PhaseTerminal
	unauthenticated.Convergence.RequiredEndorsements = 2
	unauthenticated.Convergence.MinimumRounds = 1
	unauthenticated.Outcome = TerminalOutcome{Kind: OutcomeConsensus, ProposalVersion: 1, DissentingAgentIDs: []string{}, UnresolvedObjectionIDs: []string{}, EvidenceGapClaimIDs: []string{}}
	if err := unauthenticated.Validate(); err == nil {
		t.Fatalf("zero-vote terminal consensus accepted: %v", err)
	}

	previous := authenticatedConsensusTerminal()
	if err := previous.Validate(); err != nil {
		t.Fatalf("authenticated terminal state: %v", err)
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
			name: "change terminal reason",
			mutate: func(next *DeliberationControlState) {
				next.Outcome.Reason = "different reason"
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

func TestValidateDeliberationTransitionBindsTerminalConsensusToRunContract(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*DeliberationControlState)
		mutate  func(*DeliberationControlState)
		wantErr bool
	}{
		{
			name: "matching requirements",
			prepare: func(state *DeliberationControlState) {
				state.Convergence.MinimumRounds = 2
				state.Convergence.RequiredDeliverableItems = 3
				state.Proposals[0].Content = "1. An agent must verify claims.\n2. An agent must preserve evidence.\n3. An agent must record dissent."
			},
		},
		{
			name: "lowered endorsement threshold",
			prepare: func(state *DeliberationControlState) {
				state.Convergence.RequiredEndorsements = 3
			},
			mutate: func(state *DeliberationControlState) {
				state.Convergence.RequiredEndorsements = 2
			},
			wantErr: true,
		},
		{
			name: "lowered minimum rounds",
			prepare: func(state *DeliberationControlState) {
				state.Convergence.MinimumRounds = 3
			},
			mutate: func(state *DeliberationControlState) {
				state.Convergence.MinimumRounds = 2
			},
			wantErr: true,
		},
		{
			name: "lowered deliverable requirement",
			prepare: func(state *DeliberationControlState) {
				state.Convergence.RequiredDeliverableItems = 3
			},
			mutate: func(state *DeliberationControlState) {
				state.Convergence.RequiredDeliverableItems = 0
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			previous := consensusCandidate()
			tt.prepare(previous)
			if err := previous.Validate(); err != nil {
				t.Fatalf("active run contract: %v", err)
			}

			next := cloneControlState(t, previous)
			next.Phase = PhaseTerminal
			next.Directive = TurnDirective{Kind: DirectiveNone}
			next.Convergence.CurrentEndorsements = 2
			next.Outcome = TerminalOutcome{
				Kind:                   OutcomeConsensus,
				ProposalVersion:        1,
				DissentingAgentIDs:     []string{},
				UnresolvedObjectionIDs: []string{},
				EvidenceGapClaimIDs:    []string{},
			}
			if tt.mutate != nil {
				tt.mutate(next)
			}
			if err := next.Validate(); err != nil {
				t.Fatalf("self-consistent terminal snapshot: %v", err)
			}

			err := ValidateDeliberationTransition(previous, next)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "run consensus requirements are immutable") {
					t.Fatalf("terminal requirement mutation error: got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("matching terminal requirements: %v", err)
			}
		})
	}
}

func TestValidateDeliberationTransitionRequiresVersionedPreContractBoundary(t *testing.T) {
	previous := consensusCandidate()
	previous.Convergence.MinimumRounds = 0
	previous.Proposals[0].Content = "1. An agent must verify claims.\n2. An agent must preserve evidence.\n3. An agent must record dissent."
	if err := previous.Validate(); err != nil {
		t.Fatalf("pre-contract active state: %v", err)
	}

	boundary := cloneControlState(t, previous)
	boundary.Convergence.RunContractVersion = RunContractVersion
	boundary.Convergence.MinimumRounds = 2
	boundary.Convergence.RequiredDeliverableItems = 3
	if err := ValidateDeliberationTransition(previous, boundary); err != nil {
		t.Fatalf("versioned pre-contract boundary: %v", err)
	}
	for _, tt := range []struct {
		name   string
		mutate func(*DeliberationControlState)
	}{
		{
			name: "changes directive",
			mutate: func(state *DeliberationControlState) {
				state.Directive = TurnDirective{Kind: DirectiveVote, TargetAgentID: "alpha", ProposalVersion: 1}
			},
		},
		{
			name: "changes moderator action",
			mutate: func(state *DeliberationControlState) {
				state.ModeratorAction = ModeratorAction{Kind: ModeratorActionContinue}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			invalid := cloneControlState(t, previous)
			invalid.Convergence.RunContractVersion = RunContractVersion
			invalid.Convergence.MinimumRounds = 2
			invalid.Convergence.RequiredDeliverableItems = 3
			tt.mutate(invalid)
			if err := invalid.Validate(); err != nil {
				t.Fatalf("self-consistent boundary mutation: %v", err)
			}
			if err := ValidateDeliberationTransition(previous, invalid); err == nil || !strings.Contains(err.Error(), "pre-contract active state requires a versioned run contract boundary") {
				t.Fatalf("boundary history mutation error: got %v", err)
			}
		})
	}

	terminal := cloneControlState(t, boundary)
	terminal.Phase = PhaseTerminal
	terminal.Directive = TurnDirective{Kind: DirectiveNone}
	terminal.Convergence.CurrentEndorsements = 2
	terminal.Outcome = TerminalOutcome{
		Kind:                   OutcomeConsensus,
		ProposalVersion:        1,
		DissentingAgentIDs:     []string{},
		UnresolvedObjectionIDs: []string{},
		EvidenceGapClaimIDs:    []string{},
	}
	if err := ValidateDeliberationTransition(boundary, terminal); err != nil {
		t.Fatalf("terminal after versioned boundary: %v", err)
	}

	retroactive := cloneControlState(t, previous)
	retroactive.Phase = PhaseTerminal
	retroactive.Convergence.RunContractVersion = RunContractVersion
	retroactive.Convergence.MinimumRounds = 2
	retroactive.Convergence.RequiredDeliverableItems = 3
	retroactive.Convergence.CurrentEndorsements = 2
	retroactive.Outcome = terminal.Outcome
	if err := ValidateDeliberationTransition(previous, retroactive); err == nil || !strings.Contains(err.Error(), "pre-contract active state cannot authenticate terminal consensus") {
		t.Fatalf("retroactive terminal error: got %v", err)
	}
}

func TestValidateTerminalConsensusRejectsStaleSplitAndUnmetGates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*DeliberationControlState)
	}{
		{
			name: "absent votes",
			mutate: func(state *DeliberationControlState) {
				state.Votes = nil
				state.Convergence.CurrentEndorsements = 0
				state.Outcome.DissentingAgentIDs = []string{"alpha", "beta"}
			},
		},
		{
			name: "stale convergence witness",
			mutate: func(state *DeliberationControlState) {
				state.Convergence.CurrentEndorsements = 0
			},
		},
		{
			name: "stale votes",
			mutate: func(state *DeliberationControlState) {
				state.Proposals = append(state.Proposals, CanonicalProposal{Version: 2, AuthorID: "beta", Content: "proposal two", Supersedes: 1})
				state.CurrentProposalVersion = 2
				state.Votes[0].ProposalVersion = 1
				state.Votes[1].ProposalVersion = 1
				state.Convergence.CurrentEndorsements = 0
				state.Outcome.ProposalVersion = 2
				state.Outcome.DissentingAgentIDs = []string{"alpha", "beta"}
			},
		},
		{
			name: "split current versions",
			mutate: func(state *DeliberationControlState) {
				state.Proposals = append(state.Proposals, CanonicalProposal{Version: 2, AuthorID: "beta", Content: "proposal two", Supersedes: 1})
				state.CurrentProposalVersion = 2
				state.Votes[1].ProposalVersion = 2
				state.Convergence.CurrentEndorsements = 1
				state.Outcome.ProposalVersion = 2
				state.Outcome.DissentingAgentIDs = []string{"alpha"}
			},
		},
		{
			name: "minimum rounds unmet",
			mutate: func(state *DeliberationControlState) {
				state.Convergence.MinimumRounds = 3
			},
		},
		{
			name: "canonical deliverable unmet",
			mutate: func(state *DeliberationControlState) {
				state.Convergence.RequiredDeliverableItems = 3
			},
		},
		{
			name: "objection unresolved",
			mutate: func(state *DeliberationControlState) {
				state.Objections = append(state.Objections, Objection{ID: "obj-1", AgentID: "alpha", ProposalVersion: 1, Summary: "challenge"})
				state.Convergence.UnresolvedObjections = 1
				state.Outcome.UnresolvedObjectionIDs = []string{"obj-1"}
			},
		},
		{
			name: "evidence gap unresolved",
			mutate: func(state *DeliberationControlState) {
				state.Claims = append(state.Claims, ClaimEvidence{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: ClaimFact, Decisive: true, Status: EvidenceUnverified})
				state.Convergence.EvidenceGaps = 1
				state.Outcome.EvidenceGapClaimIDs = []string{"claim-1"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := authenticatedConsensusTerminal()
			tt.mutate(state)
			if err := state.Validate(); err == nil {
				t.Fatalf("unauthenticated terminal state accepted: %v", err)
			}
		})
	}
}

func TestValidateDeliberationTransitionRejectsUnauthenticatedConsensus(t *testing.T) {
	previous := authenticatedConsensusTerminal()
	previous.Phase = PhaseVoting
	previous.Outcome = TerminalOutcome{Kind: OutcomePending, DissentingAgentIDs: []string{}, UnresolvedObjectionIDs: []string{}, EvidenceGapClaimIDs: []string{}}
	if err := previous.Validate(); err != nil {
		t.Fatalf("previous active state: %v", err)
	}
	next := cloneControlState(t, previous)
	next.Phase = PhaseTerminal
	next.Votes = nil
	next.Convergence.CurrentEndorsements = 0
	next.Outcome = TerminalOutcome{Kind: OutcomeConsensus, ProposalVersion: 1, DissentingAgentIDs: []string{"alpha", "beta"}, UnresolvedObjectionIDs: []string{}, EvidenceGapClaimIDs: []string{}}
	if err := ValidateDeliberationTransition(previous, next); err == nil {
		t.Fatal("transition accepted consensus terminal state without current votes")
	}
}
