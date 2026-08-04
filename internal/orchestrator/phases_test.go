package orchestrator

import (
	"reflect"
	"testing"

	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

func TestPrepareNextTurnRequiresEachPhaseWorkThreshold(t *testing.T) {
	contributions := func(count int) []types.AgentContribution {
		result := make([]types.AgentContribution, count)
		for i := range result {
			result[i] = types.AgentContribution{AgentID: []string{"alpha", "beta"}[i%2]}
		}
		return result
	}
	tests := []struct {
		name      string
		phase     types.DeliberationPhase
		failCount int
		passCount int
		proposal  bool
		want      types.DeliberationPhase
	}{
		{name: "opening", phase: types.PhaseOpening, failCount: 1, passCount: 2, want: types.PhaseRebuttal},
		{name: "rebuttal", phase: types.PhaseRebuttal, failCount: 3, passCount: 4, want: types.PhaseDrafting},
		{name: "drafting", phase: types.PhaseDrafting, failCount: 5, passCount: 6, proposal: true, want: types.PhaseVoting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
			failed.Phase = tt.phase
			failed.Contributions = contributions(tt.failCount)
			if tt.proposal {
				failed.CurrentProposalVersion = 1
				failed.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "draft"}}
			}
			prepareNextTurn(failed, nil, false)
			if failed.Phase != tt.phase {
				t.Fatalf("phase advanced before criteria: got %s", failed.Phase)
			}

			passed := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
			passed.Phase = tt.phase
			passed.Contributions = contributions(tt.passCount)
			if tt.proposal {
				passed.CurrentProposalVersion = 1
				passed.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "draft"}}
			}
			prepareNextTurn(passed, nil, false)
			if passed.Phase != tt.want {
				t.Fatalf("phase after criteria: got %s, want %s", passed.Phase, tt.want)
			}
		})
	}
}

func TestRunGivesEveryAgentOneIndependentOpening(t *testing.T) {
	agents := []types.AgentConfig{{ID: "alpha", Model: "test"}, {ID: "beta", Model: "test"}, {ID: "gamma", Model: "test"}}
	state := newTestState(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: agents})
	state.MaxTurns = 3
	state.Control = types.NewDeliberationControlState([]string{"alpha", "beta", "gamma"}, 0)
	response := mockResponse{content: `{"position":"opening","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`}
	runner := &recordingRunner{responses: []mockResponse{response}}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/opening.jsonl")

	NewOrchestrator(state, tm, runner).Run()

	got := make([]string, len(runner.agents))
	for i := range runner.agents {
		got[i] = runner.agents[i].ID
		if history := runner.envelopes[i]["history"].([]map[string]string); len(history) != 0 {
			t.Fatalf("opening %d saw prior speakers: %#v", i, history)
		}
		if directive := runner.envelopes[i]["directive"].(types.TurnDirective); directive.Kind != types.DirectiveNone {
			t.Fatalf("opening %d received state-driven directive: %#v", i, directive)
		}
	}
	if want := []string{"alpha", "beta", "gamma"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("opening speakers: got %v, want %v", got, want)
	}
	if state.Control.Phase != types.PhaseRebuttal {
		t.Fatalf("phase after complete opening: got %s", state.Control.Phase)
	}
}

func TestPrepareNextTurnBuildsAndPreservesStateDrivenDirectives(t *testing.T) {
	base := types.NewDeliberationControlState([]string{"alpha", "beta"}, 1)
	base.CurrentProposalVersion = 1
	base.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "draft"}}
	base.Objections = []types.Objection{{ID: "obj-1", AgentID: "alpha", ProposalVersion: 1, Summary: "challenge"}}
	base.Claims = []types.ClaimEvidence{{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimFact, Decisive: true, Status: types.EvidenceUnverified, SourceRefs: []int{0}}}

	tests := []struct {
		name  string
		phase types.DeliberationPhase
		edit  func(*types.DeliberationControlState)
		want  types.DirectiveKind
	}{
		{name: "response", phase: types.PhaseRebuttal, want: types.DirectiveRespond},
		{name: "verification", phase: types.PhaseDrafting, want: types.DirectiveVerify},
		{name: "proposal revision", phase: types.PhaseDrafting, edit: func(s *types.DeliberationControlState) { s.Claims = nil; s.Objections = nil }, want: types.DirectiveReviseProposal},
		{name: "vote", phase: types.PhaseVoting, edit: func(s *types.DeliberationControlState) { s.Claims = nil; s.Objections = nil }, want: types.DirectiveVote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := *base
			state.Proposals = append([]types.CanonicalProposal(nil), base.Proposals...)
			state.Objections = append([]types.Objection(nil), base.Objections...)
			state.Claims = append([]types.ClaimEvidence(nil), base.Claims...)
			state.Phase = tt.phase
			if tt.edit != nil {
				tt.edit(&state)
			}
			prepareNextTurn(&state, nil, false)
			if state.Directive.Kind != tt.want || state.Directive.TargetAgentID == "" {
				t.Fatalf("directive: got %#v, want kind %s with target", state.Directive, tt.want)
			}
			persisted := state.Directive
			prepareNextTurn(&state, nil, true)
			if state.Directive != persisted {
				t.Fatalf("outstanding directive changed on resume: got %#v, want %#v", state.Directive, persisted)
			}
		})
	}
}

func TestPrepareNextTurnDirectsResponseToLedgerCrux(t *testing.T) {
	state := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
	state.Phase = types.PhaseRebuttal
	debate := types.NewDebateLedger(1, 1)
	debate.Cruxes = []types.OpenCrux{{Topic: "rollback threshold", RaisedAt: 1}}

	prepareNextTurn(state, debate, false)

	if state.Directive.Kind != types.DirectiveRespond || state.Directive.Crux != "rollback threshold" || state.Directive.ObjectionID != "" {
		t.Fatalf("crux directive: %#v", state.Directive)
	}
}

func TestExecuteTurnDeliversEachDirectiveToTarget(t *testing.T) {
	base := types.NewDeliberationControlState([]string{"alpha", "beta"}, 1)
	base.CurrentProposalVersion = 1
	base.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "draft"}}
	base.Objections = []types.Objection{{ID: "obj-1", AgentID: "alpha", ProposalVersion: 1, Summary: "challenge"}}
	base.Claims = []types.ClaimEvidence{{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimFact, Decisive: true, Status: types.EvidenceUnverified, SourceRefs: []int{0}}}
	directives := []types.TurnDirective{
		{Kind: types.DirectiveRespond, TargetAgentID: "beta", ObjectionID: "obj-1"},
		{Kind: types.DirectiveVerify, TargetAgentID: "beta", ClaimID: "claim-1"},
		{Kind: types.DirectiveReviseProposal, TargetAgentID: "beta", ProposalVersion: 1},
		{Kind: types.DirectiveVote, TargetAgentID: "beta", ProposalVersion: 1},
	}
	response := `{"position":"directed attempt","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`
	for _, directive := range directives {
		t.Run(string(directive.Kind), func(t *testing.T) {
			control := *base
			control.Proposals = append([]types.CanonicalProposal(nil), base.Proposals...)
			control.Objections = append([]types.Objection(nil), base.Objections...)
			control.Claims = append([]types.ClaimEvidence(nil), base.Claims...)
			control.Directive = directive
			state := newTestState(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: []types.AgentConfig{{ID: "alpha", Model: "test"}, {ID: "beta", Model: "test"}}})
			state.Control = &control
			runner := &mockRunner{content: response}
			o := NewOrchestrator(state, transcript.NewTranscriptManager(t.TempDir()+"/directed.jsonl"), runner)

			record, ok := o.executeTurn(o.nextAgent())
			if !ok || record.AgentID != "beta" {
				t.Fatalf("directed turn: ok=%v record=%#v failure=%v", ok, record, state.Failure)
			}
			if got := runner.envelope["directive"].(types.TurnDirective); got != directive {
				t.Fatalf("delivered directive: got %#v, want %#v", got, directive)
			}
		})
	}
}
