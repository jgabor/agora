package orchestrator

import (
	"encoding/json"
	"fmt"
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
	response := mockResponse{content: `{"position":"opening"}`}
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
	if state.Control.Phase != types.PhaseTerminal || state.Control.Outcome.Kind != types.OutcomeNoConsensus {
		t.Fatalf("cap terminal after complete opening: phase=%s outcome=%#v", state.Control.Phase, state.Control.Outcome)
	}
}

func TestOpeningEnvelopeHidesPriorModelWork(t *testing.T) {
	agents := []types.AgentConfig{{ID: "alpha", Model: "test"}, {ID: "beta", Model: "test"}, {ID: "gamma", Model: "test"}}
	state := newTestState(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: agents})
	state.MaxTurns = 3
	state.Control = types.NewDeliberationControlState([]string{"alpha", "beta", "gamma"}, 0)
	runner := &recordingRunner{responses: []mockResponse{
		{content: `{"position":"independent opening alpha"}`},
		{content: `{"position":"independent opening beta"}`},
		{content: `{"position":"independent opening gamma"}`},
	}}
	priorLedger := types.NewDebateLedger(1, 1)
	priorLedger.Positions = []types.AgentPosition{{AgentID: "alpha", Text: "prior ledger position", Turn: 0}}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/opening-safe.jsonl")
	o := NewOrchestrator(state, tm, runner)
	o.SetCurrentLedger(priorLedger)
	o.Run()

	if state.Failure != nil || len(state.Control.Proposals) != 0 || len(state.Control.Objections) != 0 || len(state.Control.Contributions) != 3 {
		t.Fatalf("opening state: failure=%v proposals=%d objections=%d contributions=%d", state.Failure, len(state.Control.Proposals), len(state.Control.Objections), len(state.Control.Contributions))
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
		contract, ok := envelope["contribution_contract"].(map[string]any)
		if !ok || contract["mode"] != "position_only" {
			t.Fatalf("opening %d contribution contract: %#v", i, envelope["contribution_contract"])
		}
		if required, ok := contract["required"].([]string); !ok || !reflect.DeepEqual(required, []string{"position"}) {
			t.Fatalf("opening %d required fields: %#v", i, contract["required"])
		}
		for _, field := range []string{"proposal_action_kinds", "vote_choices", "verification_outcomes"} {
			if _, present := contract[field]; present {
				t.Fatalf("opening %d contract advertises %q: %#v", i, field, contract)
			}
		}
		payload, err := json.Marshal(envelope)
		if err != nil {
			t.Fatalf("marshal opening %d envelope: %v", i, err)
		}
		for _, priorWork := range []string{"independent opening alpha", "independent opening beta", "prior ledger position"} {
			if strings.Contains(string(payload), priorWork) {
				t.Fatalf("opening %d envelope contains prior model work %q: %s", i, priorWork, payload)
			}
		}
		for _, forbidden := range []string{`"proposal_action"`, `"proposal_action_kinds"`, `"vote_choices"`, `"verification_outcomes"`} {
			if strings.Contains(string(payload), forbidden) {
				t.Fatalf("opening %d envelope contains non-opening action schema %q: %s", i, forbidden, payload)
			}
		}
	}
}

func TestRunRecoversFromOpeningProposalActions(t *testing.T) {
	agents := []types.AgentConfig{{ID: "alpha", Model: "test"}, {ID: "beta", Model: "test"}, {ID: "gamma", Model: "test"}}
	state := newTestState(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: agents})
	state.MaxTurns = len(agents)
	state.Control = types.NewDeliberationControlState([]string{"alpha", "beta", "gamma"}, 0)
	runner := &recordingRunner{responses: []mockResponse{
		{content: `{"position":"alpha proposes immediately","responses":[],"concessions":[],"proposal_action":{"kind":"create","content":"alpha proposal"},"objections":[],"vote":null,"claims":[]}`},
		{content: `{"position":"beta proposes immediately","responses":[],"concessions":[],"proposal_action":{"kind":"create","content":"beta proposal"},"objections":[],"vote":null,"claims":[]}`},
		{content: `{"position":"gamma proposes immediately","responses":[],"concessions":[],"proposal_action":{"kind":"create","content":"gamma proposal"},"objections":[],"vote":null,"claims":[]}`},
	}}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/opening-recovery.jsonl")

	NewOrchestrator(state, tm, runner).Run()

	gotAgents := make([]string, len(runner.agents))
	for i := range runner.agents {
		gotAgents[i] = runner.agents[i].ID
		if phase := envelopeControlPhase(t, runner.envelopes[i]); phase != types.PhaseOpening {
			t.Fatalf("opening %d phase: got %s, want opening", i, phase)
		}
	}
	if want := []string{"alpha", "beta", "gamma"}; !reflect.DeepEqual(gotAgents, want) {
		t.Fatalf("opening schedule: got %v, want %v", gotAgents, want)
	}
	if state.Failure != nil || state.HaltedBy != "max_turns (3)" {
		t.Fatalf("opening recovery halted incorrectly: failure=%v halted=%q", state.Failure, state.HaltedBy)
	}
	if state.Control.CurrentProposalVersion != 0 || len(state.Control.Proposals) != 0 || len(state.Control.Objections) != 0 || len(state.Control.Votes) != 0 || len(state.Control.Claims) != 0 {
		t.Fatalf("opening proposal action mutated canonical state: %#v", state.Control)
	}
	if len(state.Control.Contributions) != len(agents) {
		t.Fatalf("opening contributions: got %d, want %d", len(state.Control.Contributions), len(agents))
	}
	seen := map[string]int{}
	for _, contribution := range state.Control.Contributions {
		seen[contribution.AgentID]++
		if contribution.ProposalAction.Kind != types.ProposalActionNone {
			t.Fatalf("opening retained proposal action: %#v", contribution)
		}
	}
	for _, agentID := range []string{"alpha", "beta", "gamma"} {
		if seen[agentID] != 1 {
			t.Fatalf("opening count for %s: got %d, want 1", agentID, seen[agentID])
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
	state := newTestState(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: agents, ConsensusThreshold: 2})
	state.MaxTurns = 8
	state.Control = types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
	responses := []mockResponse{
		{content: `{"position":"independent opening alpha"}`},
		{content: `{"position":"independent opening beta"}`},
		{content: `{"position":"create proposal after opening","responses":[],"concessions":[],"proposal_action":{"kind":"create","content":"proposal one"},"objections":[{"id":"obj-1","proposal_version":1,"summary":"challenge"}],"vote":null,"claims":[]}`},
		{content: `{"position":"answer objection","responses":[{"objection_id":"obj-1","response":"addressed","disposition":"resolved","rationale":"the proposal covers the challenge"}],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`},
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
		{Kind: types.DirectiveNone},
		{Kind: types.DirectiveRespond, TargetAgentID: "beta", ObjectionID: "obj-1"},
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
	if state.Control.Phase != types.PhaseTerminal || state.Control.Outcome.Kind != types.OutcomeConsensus ||
		state.Control.Outcome.ProposalVersion != 3 || state.Control.Directive.Kind != types.DirectiveNone {
		t.Fatalf("final consensus state: phase=%s directive=%#v outcome=%#v", state.Control.Phase, state.Control.Directive, state.Control.Outcome)
	}
	if len(state.Control.Proposals) != 3 || len(state.Control.CurrentVotes()) != 2 {
		t.Fatalf("final canonical work: proposals=%d votes=%d", len(state.Control.Proposals), len(state.Control.CurrentVotes()))
	}
	if info, err := transcript.ProtocolFromRecords(tm.Records()); err != nil || info.Legacy {
		t.Fatalf("phase transcript protocol: info=%+v err=%v", info, err)
	}
}

func typedProposalDeliverableResponses(finalContent string) []mockResponse {
	return []mockResponse{
		{content: `{"position":"independent opening alpha"}`},
		{content: `{"position":"independent opening beta"}`},
		{content: `{"position":"create proposal after opening","responses":[],"concessions":[],"proposal_action":{"kind":"create","content":"proposal one"},"objections":[],"vote":null,"claims":[]}`},
		{content: `{"position":"rebuttal follow-up","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`},
		{content: `{"position":"draft revision one","responses":[],"concessions":[],"proposal_action":{"kind":"revise","content":"proposal two","supersedes":1},"objections":[],"vote":null,"claims":[]}`},
		{content: fmt.Sprintf(`{"position":"draft revision two","responses":[],"concessions":[],"proposal_action":{"kind":"revise","content":%q,"supersedes":2},"objections":[],"vote":null,"claims":[]}`, finalContent)},
		{content: `{"position":"vote one","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":{"proposal_version":3,"choice":"endorse"},"claims":[]}`},
		{content: `{"position":"vote two","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":{"proposal_version":3,"choice":"endorse"},"claims":[]}`},
	}
}

func TestCanonicalProposalContentControlsTypedConsensus(t *testing.T) {
	artifact := "1. An agent must verify claims.\n2. An agent must preserve evidence.\n3. An agent must record dissent."
	tests := []struct {
		name        string
		finalText   string
		wantOutcome types.TerminalOutcomeKind
	}{
		{name: "canonical artifact satisfies gate", finalText: artifact, wantOutcome: types.OutcomeConsensus},
		{name: "missing canonical artifact blocks gate", finalText: "proposal three without the artifact", wantOutcome: types.OutcomeNoConsensus},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			agents := []types.AgentConfig{{ID: "alpha", Model: "test"}, {ID: "beta", Model: "test"}}
			state := newTestState(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: agents, ConsensusThreshold: 2})
			state.MaxTurns = 8
			state.Control = types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
			state.DeliverableGate = &types.DeliverableGate{MinItems: 3}
			path := t.TempDir() + "/canonical-deliverable.jsonl"
			tm := transcript.NewTranscriptManager(path)
			runner := &recordingRunner{responses: typedProposalDeliverableResponses(tt.finalText)}
			NewOrchestrator(state, tm, runner).Run()
			if state.Control.Outcome.Kind != tt.wantOutcome {
				t.Fatalf("outcome=%#v, want %s; halted=%q failure=%v", state.Control.Outcome, tt.wantOutcome, state.HaltedBy, state.Failure)
			}
			if tt.wantOutcome == types.OutcomeConsensus && state.Control.Outcome.ProposalVersion != 3 {
				t.Fatalf("consensus proposal version=%d, want 3", state.Control.Outcome.ProposalVersion)
			}
			if loaded, err := transcript.LoadFileStrict(path); err != nil || loaded[len(loaded)-1].Control.Outcome.Kind != tt.wantOutcome {
				t.Fatalf("persisted canonical deliverable outcome: records=%#v err=%v", loaded, err)
			}
		})
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

func TestPrepareNextTurnUsesLeadingCruxForFairSpeakerSelection(t *testing.T) {
	state := types.NewDeliberationControlState([]string{"alpha", "beta", "gamma"}, 0)
	state.Phase = types.PhaseRebuttal
	for _, agentID := range state.AgentIDs {
		state.Contributions = append(state.Contributions, types.AgentContribution{
			AgentID: agentID, Turn: len(state.Contributions), Position: agentID + " opening",
			ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone},
		})
	}
	debate := types.NewDebateLedger(1, 1)
	debate.Cruxes = []types.OpenCrux{{
		Topic: "rollback threshold",
		Views: []types.PositionalView{
			{AgentID: "beta", Stance: "strict"},
			{AgentID: "gamma", Stance: "flexible"},
		},
		RaisedAt: 1,
	}}

	prepareNextTurn(state, debate, false)

	if state.Directive.Kind != types.DirectiveRespond || state.Directive.Crux != "rollback threshold" || state.Directive.TargetAgentID != "beta" {
		t.Fatalf("state-derived crux directive: %#v", state.Directive)
	}
}

func TestVerificationDirectiveNeedsAnOutcome(t *testing.T) {
	for _, status := range []types.ClaimEvidenceStatus{
		types.EvidenceUnverified,
		types.EvidenceVerified,
		types.EvidenceConflicting,
		types.EvidenceUnsupported,
		types.EvidenceVerificationFailed,
	} {
		t.Run(string(status), func(t *testing.T) {
			state := types.NewDeliberationControlState([]string{"alpha", "beta"}, 1)
			state.Phase = types.PhaseDrafting
			state.CurrentProposalVersion = 1
			state.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "proposal"}}
			state.Claims = []types.ClaimEvidence{{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimFact, Decisive: true, Status: status, SourceRefs: []int{0}}}
			directive := types.TurnDirective{Kind: types.DirectiveVerify, TargetAgentID: "beta", ClaimID: "claim-1"}
			if got := directiveFulfilled(directive, state); got != (status != types.EvidenceUnverified) {
				t.Fatalf("directiveFulfilled(%q) = %v", status, got)
			}
		})
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
			response := `{"position":"directed attempt","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`
			if directive.Kind == types.DirectiveVerify {
				response = `{"position":"directed verification","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[{"id":"claim-1","status":"verified","source_refs":[0]}]}`
			}
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
