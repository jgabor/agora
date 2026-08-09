package orchestrator

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/ledger"
	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

func TestModerationAppliesEachValidatedAction(t *testing.T) {
	tests := []struct {
		name          string
		prepare       func(*types.DeliberationControlState)
		stagnant      bool
		kind          types.ModeratorActionKind
		wantDirective types.DirectiveKind
	}{
		{
			name:          "direct response",
			kind:          types.ModeratorActionDirectResponse,
			wantDirective: types.DirectiveRespond,
		},
		{
			name: "verification",
			prepare: func(state *types.DeliberationControlState) {
				state.Phase = types.PhaseDrafting
				addCurrentProposal(state)
				state.Claims = []types.ClaimEvidence{{
					ID: "claim-1", AgentID: "alpha", ProposalVersion: 1,
					Kind: types.ClaimFact, Decisive: true, Status: types.EvidenceUnverified,
				}}
			},
			kind:          types.ModeratorActionRequestEvidence,
			wantDirective: types.DirectiveVerify,
		},
		{
			name: "revision",
			prepare: func(state *types.DeliberationControlState) {
				state.Phase = types.PhaseDrafting
				addCurrentProposal(state)
				state.Objections = []types.Objection{{ID: "obj-1", AgentID: "beta", ProposalVersion: 1, Summary: "needs revision"}}
			},
			kind:          types.ModeratorActionRequestRevision,
			wantDirective: types.DirectiveReviseProposal,
		},
		{
			name: "vote",
			prepare: func(state *types.DeliberationControlState) {
				state.Phase = types.PhaseVoting
				addCurrentProposal(state)
			},
			kind:          types.ModeratorActionCallVote,
			wantDirective: types.DirectiveVote,
		},
		{
			name:          "no consensus request",
			stagnant:      true,
			kind:          types.ModeratorActionRequestNoConsensus,
			wantDirective: types.DirectiveNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := moderationState(types.PhaseRebuttal, tt.stagnant)
			if tt.prepare != nil {
				tt.prepare(state)
			}
			debate := moderationLedger(len(state.Contributions) / len(state.AgentIDs))
			observation, ok := moderationObservationForRound(state, debate, debate.Round)
			if !ok {
				t.Fatal("expected moderation observation")
			}
			action := moderationOption(t, state, observation, tt.kind)
			before := cloneModerationState(t, state)

			content, err := json.Marshal(action)
			if err != nil {
				t.Fatalf("marshal moderator action: %v", err)
			}
			runner := &mockRunner{content: string(content)}
			next, err := runModeration(runner, "test", "topic", state, debate, observation)
			if err != nil {
				t.Fatalf("run moderation: %v", err)
			}
			if runner.agent.ID != "moderator" {
				t.Fatalf("moderator call used agent %q", runner.agent.ID)
			}
			if !reflect.DeepEqual(state, before) {
				t.Fatalf("moderation mutated prior state:\n got %#v\nwant %#v", state, before)
			}
			if next.ModeratorAction.Kind != tt.kind || next.ModeratorAction.Phase != state.Phase || next.ModeratorAction.Trigger != observation.Trigger {
				t.Fatalf("moderator action: %#v", next.ModeratorAction)
			}
			if next.Directive.Kind != tt.wantDirective {
				t.Fatalf("directive: got %#v, want kind %q", next.Directive, tt.wantDirective)
			}
			if next.Convergence.StagnantRounds != observation.StagnantRounds || next.Convergence.LastModeratedRound != observation.Round {
				t.Fatalf("persisted moderation signals: %#v, observation %#v", next.Convergence, observation)
			}
			if next.Phase == types.PhaseTerminal || next.Outcome.Kind != types.OutcomePending {
				t.Fatalf("moderation created a terminal outcome: phase=%s outcome=%#v", next.Phase, next.Outcome)
			}
			if err := types.ValidateDeliberationTransition(state, next); err != nil {
				t.Fatalf("moderation transition: %v", err)
			}
		})
	}
}

func TestModerationRejectsIllegalActionsAtomically(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*types.DeliberationControlState)
		kind    types.ModeratorActionKind
		mutate  func(*moderatorActionRequest)
	}{
		{
			name: "direct response selects an invented crux",
			kind: types.ModeratorActionDirectResponse,
			mutate: func(action *moderatorActionRequest) {
				action.Crux = "invented crux"
			},
		},
		{
			name: "verification selects an unknown claim",
			prepare: func(state *types.DeliberationControlState) {
				state.Phase = types.PhaseDrafting
				addCurrentProposal(state)
				state.Claims = []types.ClaimEvidence{{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimFact, Decisive: true, Status: types.EvidenceUnverified}}
			},
			kind: types.ModeratorActionRequestEvidence,
			mutate: func(action *moderatorActionRequest) {
				action.ClaimIDs = []string{"unknown"}
			},
		},
		{
			name: "revision omits its unresolved objection",
			prepare: func(state *types.DeliberationControlState) {
				state.Phase = types.PhaseDrafting
				addCurrentProposal(state)
				state.Objections = []types.Objection{{ID: "obj-1", AgentID: "beta", ProposalVersion: 1, Summary: "needs revision"}}
			},
			kind: types.ModeratorActionRequestRevision,
			mutate: func(action *moderatorActionRequest) {
				action.ObjectionIDs = []string{}
			},
		},
		{
			name: "vote bypasses the fair target",
			prepare: func(state *types.DeliberationControlState) {
				state.Phase = types.PhaseVoting
				addCurrentProposal(state)
			},
			kind: types.ModeratorActionCallVote,
			mutate: func(action *moderatorActionRequest) {
				if action.TargetAgentID == "alpha" {
					action.TargetAgentID = "beta"
				} else {
					action.TargetAgentID = "alpha"
				}
			},
		},
		{
			name:   "no consensus is premature",
			kind:   types.ModeratorActionRequestNoConsensus,
			mutate: func(action *moderatorActionRequest) {},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := moderationState(types.PhaseRebuttal, false)
			if tt.prepare != nil {
				tt.prepare(state)
			}
			debate := moderationLedger(len(state.Contributions) / len(state.AgentIDs))
			observation, ok := moderationObservationForRound(state, debate, debate.Round)
			if !ok {
				t.Fatal("expected moderation observation")
			}
			action := moderatorActionRequest{Kind: tt.kind, Crux: observation.LeadingCrux, ObjectionIDs: []string{}, ClaimIDs: []string{}}
			if tt.kind != types.ModeratorActionRequestNoConsensus {
				action = moderationOption(t, state, observation, tt.kind)
			}
			tt.mutate(&action)
			before := cloneModerationState(t, state)

			next, err := applyModeratorAction(state, observation, action)
			if err == nil || next != nil {
				t.Fatalf("illegal action accepted: next=%#v err=%v", next, err)
			}
			if !reflect.DeepEqual(state, before) {
				t.Fatalf("illegal action mutated state:\n got %#v\nwant %#v", state, before)
			}
		})
	}
}

func TestModerationTriggersAtRoundBoundaryAndStagnation(t *testing.T) {
	boundary := moderationState(types.PhaseRebuttal, false)
	boundaryLedger := moderationLedger(2)
	observation, ok := moderationObservationForRound(boundary, boundaryLedger, 2)
	if !ok || observation.Trigger != types.ModerationTriggerRoundBoundary || observation.StagnantRounds != 0 {
		t.Fatalf("round-boundary observation: ok=%v observation=%#v", ok, observation)
	}

	stagnant := moderationState(types.PhaseRebuttal, true)
	stagnantLedger := moderationLedger(3)
	observation, ok = moderationObservationForRound(stagnant, stagnantLedger, 3)
	if !ok || observation.Trigger != types.ModerationTriggerStagnation || observation.StagnantRounds != 2 {
		t.Fatalf("stagnation observation: ok=%v observation=%#v", ok, observation)
	}

	controls := []struct {
		name   string
		state  *types.DeliberationControlState
		ledger *types.DebateLedger
		round  int
	}{
		{name: "opening", state: types.NewDeliberationControlState([]string{"alpha", "beta"}, 0), ledger: moderationLedger(0), round: 0},
		{name: "incomplete round", state: incompleteModerationState(), ledger: moderationLedger(1), round: 1},
		{name: "no unresolved state", state: moderationState(types.PhaseRebuttal, false), ledger: types.NewDebateLedger(2, 0), round: 2},
	}
	for _, tt := range controls {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := moderationObservationForRound(tt.state, tt.ledger, tt.round); ok {
				t.Fatalf("unexpected moderation observation: %#v", got)
			}
		})
	}
}

func TestModerationSelectsLeadingCruxSpeakersFairly(t *testing.T) {
	state := types.NewDeliberationControlState([]string{"alpha", "beta", "gamma"}, 0)
	state.Phase = types.PhaseRebuttal
	for _, agentID := range state.AgentIDs {
		state.Contributions = append(state.Contributions, types.AgentContribution{
			AgentID: agentID, Turn: len(state.Contributions), Position: agentID + " opening",
			ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone},
		})
	}
	preferred := []string{"beta", "gamma"}
	if got := fairModeratorTarget(state, preferred); got != "beta" {
		t.Fatalf("leading crux target: got %q, want beta", got)
	}

	selected := map[string]int{}
	for range 9 {
		target := fairModeratorTarget(state, preferred)
		selected[target]++
		state.Contributions = append(state.Contributions, types.AgentContribution{
			AgentID: target, Turn: len(state.Contributions), Position: target + " response",
			ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone},
		})
	}
	for _, agentID := range state.AgentIDs {
		if selected[agentID] == 0 {
			t.Fatalf("starved agent %q: selections=%v", agentID, selected)
		}
	}
	if selected["alpha"] != 3 || selected["beta"] != 3 || selected["gamma"] != 3 {
		t.Fatalf("unfair repeated targets: %v", selected)
	}
}

func TestModerationOutputFailurePreservesState(t *testing.T) {
	state := moderationState(types.PhaseRebuttal, false)
	debate := moderationLedger(2)
	observation, ok := moderationObservationForRound(state, debate, 2)
	if !ok {
		t.Fatal("expected moderation observation")
	}
	before := cloneModerationState(t, state)

	for _, tt := range []struct {
		name   string
		runner agent.Runner
	}{
		{name: "invalid JSON", runner: &mockRunner{content: `{`}},
		{name: "terminal outcome field", runner: &mockRunner{content: `{"kind":"direct_response","target_agent_id":"alpha","crux":"rollback threshold","proposal_version":0,"objection_ids":[],"claim_ids":[],"outcome":{"kind":"no_consensus"}}`}},
		{name: "runner failure", runner: &mockRunner{err: errors.New("moderator unavailable")}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			next, err := runModeration(tt.runner, "test", "topic", state, debate, observation)
			if err == nil || next != nil {
				t.Fatalf("failed moderation returned next=%#v err=%v", next, err)
			}
			if !reflect.DeepEqual(state, before) {
				t.Fatalf("failed moderation mutated state:\n got %#v\nwant %#v", state, before)
			}
		})
	}
}

func TestModeratorNoConsensusRequestDefersTerminalOutcome(t *testing.T) {
	control := moderationState(types.PhaseRebuttal, true)
	debate := moderationLedger(3)
	observation, ok := moderationObservationForRound(control, debate, 3)
	if !ok || observation.Trigger != types.ModerationTriggerStagnation {
		t.Fatalf("stagnation observation: ok=%v observation=%#v", ok, observation)
	}
	action := moderationOption(t, control, observation, types.ModeratorActionRequestNoConsensus)
	content, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("marshal moderator action: %v", err)
	}
	state := newTestState(&types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "alpha", Model: "test"}, {ID: "beta", Model: "test"}}})
	state.Control = control
	state.Turn = 5
	state.MaxTurns = 10
	tm := transcript.NewTranscriptManager(t.TempDir() + "/no-consensus-request.jsonl")
	o := NewOrchestrator(state, tm, &mockRunner{content: string(content)})
	o.SetCurrentLedger(debate)

	o.moderateAfterRound()
	if state.Control.Phase == types.PhaseTerminal || state.Control.Outcome.Kind != types.OutcomePending || len(tm.Records()) != 1 {
		t.Fatalf("moderator request wrote an outcome: phase=%s outcome=%#v records=%#v", state.Control.Phase, state.Control.Outcome, tm.Records())
	}
	if tm.Records()[0].Control == nil || tm.Records()[0].Control.ModeratorAction.Kind != types.ModeratorActionRequestNoConsensus || tm.Records()[0].Control.Outcome.Kind != types.OutcomePending {
		t.Fatalf("pending moderator request snapshot: %#v", tm.Records()[0])
	}

	o.haltNoConsensus("test_cap")
	if state.Control.Phase != types.PhaseTerminal || state.Control.Outcome.Kind != types.OutcomeNoConsensus || len(tm.Records()) != 2 {
		t.Fatalf("terminal evaluator result: phase=%s outcome=%#v records=%#v", state.Control.Phase, state.Control.Outcome, tm.Records())
	}
}

func TestRunModeratesOnceAfterOpeningAndDoesNotWriteTerminalOutcome(t *testing.T) {
	agents := []types.AgentConfig{{ID: "alpha", Model: "test"}, {ID: "beta", Model: "test"}}
	state := newTestState(&types.DeliberationConfig{Topology: types.TopologyRing, Agents: agents})
	state.MaxTurns = 3
	state.Control = types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
	ledgerJSON := `{"round":1,"positions":[{"agent_id":"alpha","text":"alpha opening","turn":0},{"agent_id":"beta","text":"beta opening","turn":1}],"agreements":[],"cruxes":[{"topic":"rollback threshold","views":[{"agent_id":"alpha","stance":"strict"},{"agent_id":"beta","stance":"flexible"}],"raised_at":1}],"draft":{"status":"none"}}`
	moderatorJSON := `{"kind":"direct_response","target_agent_id":"alpha","crux":"rollback threshold","proposal_version":0,"objection_ids":[],"claim_ids":[]}`
	runner := &recordingRunner{responses: []mockResponse{
		{content: `{"position":"alpha opening"}`},
		{content: `{"position":"beta opening"}`},
		{content: ledgerJSON},
		{content: moderatorJSON},
		{content: `{"position":"alpha response","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]}`},
	}}
	path := t.TempDir() + "/moderation.jsonl"
	tm := transcript.NewTranscriptManager(path)
	o := NewOrchestrator(state, tm, runner)
	o.SetLedgerUpdater(ledger.NewUpdater(runner))
	o.Run()

	moderatorCalls := 0
	for _, called := range runner.agents {
		if called.ID == "moderator" {
			moderatorCalls++
		}
	}
	if moderatorCalls != 1 {
		t.Fatalf("moderator calls: got %d, want one", moderatorCalls)
	}
	var actionSnapshot *types.DeliberationControlState
	for _, record := range tm.Records() {
		if record.Control != nil && record.Control.ModeratorAction.Kind == types.ModeratorActionDirectResponse {
			actionSnapshot = record.Control
			break
		}
	}
	if actionSnapshot == nil || actionSnapshot.Directive.Kind != types.DirectiveRespond || actionSnapshot.Outcome.Kind != types.OutcomePending {
		t.Fatalf("moderation snapshot: %#v", actionSnapshot)
	}
	if state.Control.Outcome.Kind != types.OutcomeNoConsensus {
		t.Fatalf("only the cap evaluator should create the terminal outcome: %#v", state.Control.Outcome)
	}
	loaded, err := transcript.LoadFileStrict(path)
	if err != nil {
		t.Fatalf("load persisted moderation transcript: %v", err)
	}
	if len(loaded) != len(tm.Records()) {
		t.Fatalf("persisted moderation records: got %d, want %d", len(loaded), len(tm.Records()))
	}
}

func moderationState(phase types.DeliberationPhase, stagnant bool) *types.DeliberationControlState {
	state := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
	state.Phase = phase
	rounds := 2
	if stagnant {
		rounds = 3
		state.Convergence.StagnantRounds = 1
	}
	for round := range rounds {
		for _, agentID := range state.AgentIDs {
			position := agentID + " current"
			if !stagnant {
				position += string(rune('a' + round))
			}
			state.Contributions = append(state.Contributions, types.AgentContribution{
				AgentID: agentID, Turn: len(state.Contributions), Position: position,
				ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone},
			})
		}
	}
	return state
}

func incompleteModerationState() *types.DeliberationControlState {
	state := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
	state.Phase = types.PhaseRebuttal
	state.Contributions = append(state.Contributions, types.AgentContribution{AgentID: "alpha", Turn: 0, Position: "alpha", ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone}})
	return state
}

func moderationLedger(round int) *types.DebateLedger {
	return &types.DebateLedger{
		Round: round,
		Cruxes: []types.OpenCrux{{
			Topic:    "rollback threshold",
			Views:    []types.PositionalView{{AgentID: "alpha", Stance: "strict"}, {AgentID: "beta", Stance: "flexible"}},
			RaisedAt: 1,
		}},
		Draft: types.DraftProposal{Status: types.DraftStatusNone},
	}
}

func addCurrentProposal(state *types.DeliberationControlState) {
	state.CurrentProposalVersion = 1
	state.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "proposal"}}
}

func moderationOption(t *testing.T, state *types.DeliberationControlState, observation moderationObservation, kind types.ModeratorActionKind) moderatorActionRequest {
	t.Helper()
	for _, action := range moderationOptions(state, observation) {
		if action.Kind == kind {
			return action
		}
	}
	t.Fatalf("no %q moderation option in %#v", kind, moderationOptions(state, observation))
	return moderatorActionRequest{}
}

func cloneModerationState(t *testing.T, state *types.DeliberationControlState) *types.DeliberationControlState {
	t.Helper()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatalf("marshal state: %v", err)
	}
	var clone types.DeliberationControlState
	if err := json.Unmarshal(data, &clone); err != nil {
		t.Fatalf("unmarshal state: %v", err)
	}
	return &clone
}
