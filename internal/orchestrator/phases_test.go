package orchestrator

import (
	"encoding/json"
	"reflect"
	"strings"
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

func TestPrepareNextTurnRejectsEqualTotalImbalancedPhaseWork(t *testing.T) {
	contributions := func(alpha, beta int) []types.AgentContribution {
		result := make([]types.AgentContribution, 0, alpha+beta)
		for i := 0; i < alpha; i++ {
			result = append(result, types.AgentContribution{AgentID: "alpha", Turn: len(result), Position: "position"})
		}
		for i := 0; i < beta; i++ {
			result = append(result, types.AgentContribution{AgentID: "beta", Turn: len(result), Position: "position"})
		}
		return result
	}
	tests := []struct {
		name     string
		phase    types.DeliberationPhase
		alpha    int
		beta     int
		proposal bool
		want     types.DeliberationPhase
	}{
		{name: "rebuttal balanced pass", phase: types.PhaseRebuttal, alpha: 2, beta: 2, want: types.PhaseDrafting},
		{name: "rebuttal imbalanced fail", phase: types.PhaseRebuttal, alpha: 3, beta: 1, want: types.PhaseRebuttal},
		{name: "drafting balanced pass", phase: types.PhaseDrafting, alpha: 3, beta: 3, proposal: true, want: types.PhaseVoting},
		{name: "drafting imbalanced fail", phase: types.PhaseDrafting, alpha: 5, beta: 1, proposal: true, want: types.PhaseDrafting},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
			state.Phase = tt.phase
			state.Contributions = contributions(tt.alpha, tt.beta)
			if tt.proposal {
				state.CurrentProposalVersion = 1
				state.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "draft"}}
			}
			prepareNextTurn(state, nil, false)
			if state.Phase != tt.want {
				t.Fatalf("phase: got %s, want %s", state.Phase, tt.want)
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

func TestOpeningEnvelopeHidesPriorModelWork(t *testing.T) {
	agents := []types.AgentConfig{{ID: "alpha", Model: "test"}, {ID: "beta", Model: "test"}, {ID: "gamma", Model: "test"}}
	state := newTestState(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: agents})
	state.MaxTurns = 3
	state.Control = types.NewDeliberationControlState([]string{"alpha", "beta", "gamma"}, 0)
	runner := &recordingRunner{responses: []mockResponse{
		{content: `{"position":"opening proposal","responses":[],"concessions":[],"proposal_action":{"kind":"create","content":"proposal one"},"objections":[{"id":"obj-1","proposal_version":1,"summary":"prior objection"}],"vote":null,"claims":[]}`},
		{content: `{"position":"independent opening","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`},
	}}
	priorLedger := types.NewDebateLedger(1, 1)
	priorLedger.Positions = []types.AgentPosition{{AgentID: "alpha", Text: "prior ledger position", Turn: 0}}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/opening-safe.jsonl")
	o := NewOrchestrator(state, tm, runner)
	o.SetCurrentLedger(priorLedger)
	o.Run()

	if len(state.Control.Proposals) != 1 || len(state.Control.Objections) != 1 || len(state.Control.Contributions) != 3 {
		t.Fatalf("authoritative opening state was not retained: proposals=%d objections=%d contributions=%d", len(state.Control.Proposals), len(state.Control.Objections), len(state.Control.Contributions))
	}
	wantControl := map[string]any{
		"protocol_version":       types.DeliberationProtocolVersion,
		"phase":                  types.PhaseOpening,
		"agent_ids":              []string{"alpha", "beta", "gamma"},
		"source_reference_count": 0,
	}
	for i := 1; i < len(runner.envelopes); i++ {
		envelope := runner.envelopes[i]
		control, ok := envelope["control_state"].(map[string]any)
		if !ok || !reflect.DeepEqual(control, wantControl) {
			t.Fatalf("opening %d control view: got %#v, want %#v", i, envelope["control_state"], wantControl)
		}
		if _, ok := envelope["ledger"]; ok {
			t.Fatalf("opening %d received prior ledger: %#v", i, envelope["ledger"])
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal opening %d envelope: %v", i, err)
		}
		for _, priorWork := range []string{"opening proposal", "proposal one", "obj-1", "prior objection", "prior ledger position"} {
			if strings.Contains(string(payload), priorWork) {
				t.Fatalf("opening %d envelope contains prior model work %q: %s", i, priorWork, payload)
			}
		}
	}
}

func TestPrepareNextTurnKeepsImbalancedDirectedWorkInPhase(t *testing.T) {
	t.Run("rebuttal response", func(t *testing.T) {
		state := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
		state.Phase = types.PhaseRebuttal
		state.Contributions = []types.AgentContribution{
			{AgentID: "alpha", Turn: 0, Position: "opening"},
			{AgentID: "beta", Turn: 1, Position: "opening"},
			{AgentID: "alpha", Turn: 2, Position: "attempt"},
		}
		state.Objections = []types.Objection{{ID: "obj-1", AgentID: "alpha", ProposalVersion: 0, Summary: "challenge"}}
		state.Directive = types.TurnDirective{Kind: types.DirectiveRespond, TargetAgentID: "alpha", ObjectionID: "obj-1"}
		prepareNextTurn(state, nil, true)
		if state.Phase != types.PhaseRebuttal || state.Directive.TargetAgentID != "alpha" {
			t.Fatalf("preserved rebuttal directive: phase=%s directive=%#v", state.Phase, state.Directive)
		}
		state.Contributions = append(state.Contributions, types.AgentContribution{
			AgentID: "alpha", Turn: 3, Position: "answer",
			Responses: []types.ContributionResponse{{ObjectionID: "obj-1", Response: "addressed"}},
		})
		prepareNextTurn(state, nil, !directiveFulfilled(state.Directive, state))
		if state.Phase != types.PhaseRebuttal {
			t.Fatalf("imbalanced rebuttal advanced after response: got %s", state.Phase)
		}
	})

	t.Run("draft revision", func(t *testing.T) {
		state := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
		state.Phase = types.PhaseDrafting
		state.CurrentProposalVersion = 1
		state.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "draft one"}}
		state.Contributions = []types.AgentContribution{
			{AgentID: "alpha", Turn: 0, Position: "work"},
			{AgentID: "alpha", Turn: 1, Position: "work"},
			{AgentID: "alpha", Turn: 2, Position: "work"},
			{AgentID: "alpha", Turn: 3, Position: "work"},
			{AgentID: "beta", Turn: 4, Position: "work"},
		}
		state.Directive = types.TurnDirective{Kind: types.DirectiveReviseProposal, TargetAgentID: "alpha", ProposalVersion: 1}
		prepareNextTurn(state, nil, true)
		state.Proposals = append(state.Proposals, types.CanonicalProposal{Version: 2, AuthorID: "alpha", Content: "draft two", Supersedes: 1})
		state.CurrentProposalVersion = 2
		state.Contributions = append(state.Contributions, types.AgentContribution{AgentID: "alpha", Turn: 5, Position: "revised"})
		prepareNextTurn(state, nil, !directiveFulfilled(state.Directive, state))
		if state.Phase != types.PhaseDrafting {
			t.Fatalf("imbalanced drafting advanced after revision: got %s", state.Phase)
		}
	})
}

func TestRunAdvancesThroughPhasesWithStateDrivenWork(t *testing.T) {
	agents := []types.AgentConfig{{ID: "alpha", Model: "test"}, {ID: "beta", Model: "test"}}
	state := newTestState(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: agents})
	state.MaxTurns = 8
	state.Control = types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
	responses := []mockResponse{
		{content: `{"position":"opening proposal","responses":[],"concessions":[],"proposal_action":{"kind":"create","content":"proposal one"},"objections":[{"id":"obj-1","proposal_version":1,"summary":"challenge"}],"vote":null,"claims":[]}`},
		{content: `{"position":"independent opening","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`},
		{content: `{"position":"answer objection","responses":[{"objection_id":"obj-1","response":"addressed","disposition":"resolved","rationale":"the proposal covers the challenge"}],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`},
		{content: `{"position":"rebuttal follow-up","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`},
		{content: `{"position":"draft revision one","responses":[],"concessions":[],"proposal_action":{"kind":"revise","content":"proposal two","supersedes":1},"objections":[],"vote":null,"claims":[]}`},
		{content: `{"position":"draft revision two","responses":[],"concessions":[],"proposal_action":{"kind":"revise","content":"proposal three","supersedes":2},"objections":[],"vote":null,"claims":[]}`},
		{content: `{"position":"vote one","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":{"proposal_version":3,"choice":"endorse"},"claims":[]}`},
		{content: `{"position":"vote two","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":{"proposal_version":3,"choice":"endorse"},"claims":[]}`},
	}
	runner := &recordingRunner{responses: responses}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/phases.jsonl")

	NewOrchestrator(state, tm, runner).Run()

	wantPhases := []types.DeliberationPhase{
		types.PhaseOpening, types.PhaseOpening,
		types.PhaseRebuttal, types.PhaseRebuttal,
		types.PhaseDrafting, types.PhaseDrafting,
		types.PhaseVoting, types.PhaseVoting,
	}
	wantDirectives := []types.TurnDirective{
		{Kind: types.DirectiveNone},
		{Kind: types.DirectiveNone},
		{Kind: types.DirectiveRespond, TargetAgentID: "alpha", ObjectionID: "obj-1"},
		{Kind: types.DirectiveNone},
		{Kind: types.DirectiveReviseProposal, TargetAgentID: "alpha", ProposalVersion: 1},
		{Kind: types.DirectiveReviseProposal, TargetAgentID: "beta", ProposalVersion: 2},
		{Kind: types.DirectiveVote, TargetAgentID: "alpha", ProposalVersion: 3},
		{Kind: types.DirectiveVote, TargetAgentID: "beta", ProposalVersion: 3},
	}
	if len(runner.envelopes) != len(wantPhases) {
		t.Fatalf("turns: got %d, want %d, halted=%q failure=%v", len(runner.envelopes), len(wantPhases), state.HaltedBy, state.Failure)
	}
	for i := range wantPhases {
		if phase := envelopeControlPhase(t, runner.envelopes[i]); phase != wantPhases[i] {
			t.Fatalf("turn %d phase: got %s, want %s", i, phase, wantPhases[i])
		}
		if directive := runner.envelopes[i]["directive"].(types.TurnDirective); directive != wantDirectives[i] {
			t.Fatalf("turn %d directive: got %#v, want %#v", i, directive, wantDirectives[i])
		}
	}
	if state.Control.Phase != types.PhaseVoting || state.Control.Directive.Kind != types.DirectiveNone {
		t.Fatalf("final phase/directive: got %s/%#v", state.Control.Phase, state.Control.Directive)
	}
	if len(state.Control.Proposals) != 3 || len(state.Control.CurrentVotes()) != 2 {
		t.Fatalf("final canonical work: proposals=%d votes=%d", len(state.Control.Proposals), len(state.Control.CurrentVotes()))
	}
	if info, err := transcript.ProtocolFromRecords(tm.Records()); err != nil || info.Legacy {
		t.Fatalf("phase transcript protocol: info=%+v err=%v", info, err)
	}
}

func envelopeControlPhase(t *testing.T, envelope map[string]any) types.DeliberationPhase {
	t.Helper()
	switch control := envelope["control_state"].(type) {
	case *types.DeliberationControlState:
		return control.Phase
	case map[string]any:
		phase, ok := control["phase"].(types.DeliberationPhase)
		if ok {
			return phase
		}
	}
	t.Fatalf("unexpected control view: %#v", envelope["control_state"])
	return ""
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
			switch directive.Kind {
			case types.DirectiveRespond:
				control.Phase = types.PhaseRebuttal
			case types.DirectiveVerify, types.DirectiveReviseProposal:
				control.Phase = types.PhaseDrafting
			case types.DirectiveVote:
				control.Phase = types.PhaseVoting
			}
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
