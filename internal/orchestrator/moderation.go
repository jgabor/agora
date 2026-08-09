package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/llmutil"
	"github.com/jgabor/agora/internal/types"
)

const stagnantRoundThreshold = types.StagnationRoundsForNoConsensus

// moderatorActionRequest is the complete, model-facing action shape. The
// orchestrator binds Trigger after parsing so a moderator cannot claim a
// different reason for its invocation.
type moderatorActionRequest struct {
	Kind            types.ModeratorActionKind `json:"kind"`
	TargetAgentID   string                    `json:"target_agent_id"`
	Crux            string                    `json:"crux"`
	ProposalVersion int                       `json:"proposal_version"`
	ObjectionIDs    []string                  `json:"objection_ids"`
	ClaimIDs        []string                  `json:"claim_ids"`
}

// moderationObservation is computed only from accepted control state and the
// completed-round ledger. It is the authority for the moderator's bounded
// choice set; model output cannot add a target or reference of its own.
type moderationObservation struct {
	Round                  int
	Trigger                types.ModerationTrigger
	LeadingCrux            string
	PreferredAgentIDs      []string
	StagnantRounds         int
	CurrentEndorsements    int
	UnresolvedObjectionIDs []string
	EvidenceGapClaimIDs    []string
	ReadyToVote            bool
}

// moderateAfterRound makes at most one moderator call after a completed agent
// round. Failed calls and invalid replies are deliberately non-fatal: they do
// not replace the outstanding validated directive or control state.
func (o *Orchestrator) moderateAfterRound() {
	if o == nil || o.state == nil || o.state.Control == nil || o.numAgents == 0 || !o.state.Running {
		return
	}
	if (o.state.Turn+1)%o.numAgents != 0 {
		return
	}
	round := (o.state.Turn + 1) / o.numAgents
	observation, ok := moderationObservationForRound(o.state.Control, o.currentLedger, round)
	if !ok {
		return
	}

	stop := o.activity("Moderation")
	next, err := runModeration(o.runner, o.synthesizeModel(), o.state.Topic, o.state.Control, o.currentLedger, observation)
	stop()
	if err != nil {
		fmt.Fprintf(os.Stderr, "moderation: %v\n", err)
		return
	}
	if err := o.transcript.Append(types.TurnRecord{
		Turn:      -1,
		AgentID:   "moderator",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Control:   next,
	}); err != nil {
		o.fail("error:", err)
		return
	}
	o.state.Control = next
}

// moderationObservationForRound returns an observation only for a complete,
// post-opening round with state that admits at least one validated action.
func moderationObservationForRound(state *types.DeliberationControlState, debate *types.DebateLedger, round int) (moderationObservation, bool) {
	if state == nil || len(state.AgentIDs) == 0 || state.Phase == types.PhaseOpening || state.Phase == types.PhaseTerminal || round <= state.Convergence.LastModeratedRound {
		return moderationObservation{}, false
	}
	if debate == nil || debate.Round != round {
		return moderationObservation{}, false
	}
	if len(state.Contributions) != round*len(state.AgentIDs) {
		return moderationObservation{}, false
	}

	observation := moderationObservation{Round: round}
	observation.LeadingCrux, observation.PreferredAgentIDs = leadingCrux(state, debate)
	for _, objection := range state.UnresolvedObjections() {
		observation.UnresolvedObjectionIDs = append(observation.UnresolvedObjectionIDs, objection.ID)
	}
	for _, claim := range state.EvidenceGaps() {
		observation.EvidenceGapClaimIDs = append(observation.EvidenceGapClaimIDs, claim.ID)
	}
	for _, vote := range state.CurrentVotes() {
		if vote.Choice == types.VoteEndorse {
			observation.CurrentEndorsements++
		}
	}
	observation.ReadyToVote = state.Phase == types.PhaseVoting && state.CurrentProposalVersion > 0 &&
		len(observation.UnresolvedObjectionIDs) == 0 && len(observation.EvidenceGapClaimIDs) == 0 && fairVoteTarget(state) != ""
	if hasUnresolvedModerationWork(state, observation) {
		observation.StagnantRounds = observedStagnantRounds(state)
	}
	observation.Trigger = types.ModerationTriggerRoundBoundary
	if observation.StagnantRounds >= stagnantRoundThreshold {
		observation.Trigger = types.ModerationTriggerStagnation
	}
	if len(moderationOptions(state, observation)) == 0 {
		return moderationObservation{}, false
	}
	return observation, true
}

func leadingCrux(state *types.DeliberationControlState, debate *types.DebateLedger) (string, []string) {
	if debate == nil {
		return "", nil
	}
	active := make(map[string]bool, len(state.AgentIDs))
	for _, agentID := range state.AgentIDs {
		active[agentID] = true
	}
	for _, crux := range debate.Cruxes {
		if strings.TrimSpace(crux.Topic) == "" {
			continue
		}
		seen := make(map[string]bool, len(crux.Views))
		preferred := make([]string, 0, len(crux.Views))
		for _, view := range crux.Views {
			if active[view.AgentID] && !seen[view.AgentID] {
				preferred = append(preferred, view.AgentID)
				seen[view.AgentID] = true
			}
		}
		return crux.Topic, preferred
	}
	return "", nil
}

func hasUnresolvedModerationWork(state *types.DeliberationControlState, observation moderationObservation) bool {
	return observation.LeadingCrux != "" || len(observation.UnresolvedObjectionIDs) > 0 || len(observation.EvidenceGapClaimIDs) > 0 ||
		(state.Phase == types.PhaseVoting && state.CurrentProposalVersion > 0 && fairVoteTarget(state) != "")
}

func observedStagnantRounds(state *types.DeliberationControlState) int {
	if !repeatedRoundPositions(state) {
		return 0
	}
	return state.Convergence.StagnantRounds + 1
}

func repeatedRoundPositions(state *types.DeliberationControlState) bool {
	if state == nil || len(state.AgentIDs) == 0 {
		return false
	}
	count := len(state.AgentIDs)
	if len(state.Contributions) < 2*count || len(state.Contributions)%count != 0 {
		return false
	}
	previous := state.Contributions[len(state.Contributions)-2*count : len(state.Contributions)-count]
	current := state.Contributions[len(state.Contributions)-count:]
	if roundHasProgress(current) {
		return false
	}
	previousPositions, ok := roundPositions(state.AgentIDs, previous)
	if !ok {
		return false
	}
	currentPositions, ok := roundPositions(state.AgentIDs, current)
	if !ok {
		return false
	}
	for _, agentID := range state.AgentIDs {
		if currentPositions[agentID] != previousPositions[agentID] {
			return false
		}
	}
	return true
}

func roundPositions(agentIDs []string, contributions []types.AgentContribution) (map[string]string, bool) {
	positions := make(map[string]string, len(agentIDs))
	for _, contribution := range contributions {
		if _, exists := positions[contribution.AgentID]; exists {
			return nil, false
		}
		positions[contribution.AgentID] = strings.TrimSpace(contribution.Position)
	}
	if len(positions) != len(agentIDs) {
		return nil, false
	}
	for _, agentID := range agentIDs {
		if _, exists := positions[agentID]; !exists {
			return nil, false
		}
	}
	return positions, true
}

func roundHasProgress(contributions []types.AgentContribution) bool {
	for _, contribution := range contributions {
		if contribution.ProposalAction.Kind != types.ProposalActionNone || len(contribution.Responses) > 0 ||
			len(contribution.Concessions) > 0 || len(contribution.Objections) > 0 || contribution.Vote != nil {
			return true
		}
		for _, claim := range contribution.Claims {
			if claim.Status != types.EvidenceUnverified {
				return true
			}
		}
	}
	return false
}

// moderationOptions returns only complete, state-derived requests. A model
// may select one option but cannot alter its target or references.
func moderationOptions(state *types.DeliberationControlState, observation moderationObservation) []moderatorActionRequest {
	actions := make([]moderatorActionRequest, 0, 5)
	if observation.StagnantRounds >= stagnantRoundThreshold {
		actions = append(actions, moderatorActionRequest{
			Kind:            types.ModeratorActionRequestNoConsensus,
			Crux:            observation.LeadingCrux,
			ProposalVersion: state.CurrentProposalVersion,
			ObjectionIDs:    append([]string{}, observation.UnresolvedObjectionIDs...),
			ClaimIDs:        append([]string{}, observation.EvidenceGapClaimIDs...),
		})
	}
	if observation.LeadingCrux != "" && (state.Phase == types.PhaseRebuttal || state.Phase == types.PhaseDrafting) {
		if target := fairModeratorTarget(state, observation.PreferredAgentIDs); target != "" {
			actions = append(actions, moderatorActionRequest{
				Kind: types.ModeratorActionDirectResponse, TargetAgentID: target, Crux: observation.LeadingCrux,
				ObjectionIDs: []string{}, ClaimIDs: []string{},
			})
		}
	}
	if state.Phase == types.PhaseDrafting && len(observation.EvidenceGapClaimIDs) > 0 {
		if target := fairModeratorTarget(state, nil); target != "" {
			actions = append(actions, moderatorActionRequest{
				Kind: types.ModeratorActionRequestEvidence, TargetAgentID: target, Crux: observation.LeadingCrux,
				ObjectionIDs: []string{}, ClaimIDs: []string{observation.EvidenceGapClaimIDs[0]},
			})
		}
	}
	if state.Phase == types.PhaseDrafting && state.CurrentProposalVersion > 0 && len(observation.UnresolvedObjectionIDs) > 0 {
		if target := fairModeratorTarget(state, nil); target != "" {
			actions = append(actions, moderatorActionRequest{
				Kind: types.ModeratorActionRequestRevision, TargetAgentID: target, Crux: observation.LeadingCrux,
				ProposalVersion: state.CurrentProposalVersion, ObjectionIDs: []string{observation.UnresolvedObjectionIDs[0]}, ClaimIDs: []string{},
			})
		}
	}
	if observation.ReadyToVote {
		actions = append(actions, moderatorActionRequest{
			Kind: types.ModeratorActionCallVote, TargetAgentID: fairVoteTarget(state), Crux: observation.LeadingCrux,
			ProposalVersion: state.CurrentProposalVersion, ObjectionIDs: []string{}, ClaimIDs: []string{},
		})
	}
	return actions
}

// fairModeratorTarget prefers participants in the leading crux only while
// they are tied for least work. That keeps the unresolved state relevant
// without allowing any active agent to be selected indefinitely late.
func fairModeratorTarget(state *types.DeliberationControlState, preferred []string) string {
	if state == nil {
		return ""
	}
	return fairTarget(state, state.AgentIDs, preferred)
}

func fairVoteTarget(state *types.DeliberationControlState) string {
	if state == nil {
		return ""
	}
	voted := make(map[string]bool, len(state.CurrentVotes()))
	for _, vote := range state.CurrentVotes() {
		voted[vote.AgentID] = true
	}
	eligible := make([]string, 0, len(state.AgentIDs))
	for _, agentID := range state.AgentIDs {
		if !voted[agentID] {
			eligible = append(eligible, agentID)
		}
	}
	return fairTarget(state, eligible, nil)
}

func fairTarget(state *types.DeliberationControlState, eligible, preferred []string) string {
	if len(eligible) == 0 {
		return ""
	}
	counts := make(map[string]int, len(state.AgentIDs))
	lastTurn := make(map[string]int, len(state.AgentIDs))
	for _, agentID := range state.AgentIDs {
		lastTurn[agentID] = -1
	}
	for _, contribution := range state.Contributions {
		counts[contribution.AgentID]++
		lastTurn[contribution.AgentID] = contribution.Turn
	}
	minimum := -1
	for _, agentID := range eligible {
		if minimum == -1 || counts[agentID] < minimum {
			minimum = counts[agentID]
		}
	}
	candidates := make([]string, 0, len(eligible))
	for _, agentID := range eligible {
		if counts[agentID] == minimum {
			candidates = append(candidates, agentID)
		}
	}
	preferredSet := make(map[string]bool, len(preferred))
	for _, agentID := range preferred {
		preferredSet[agentID] = true
	}
	if len(preferredSet) > 0 {
		preferredCandidates := make([]string, 0, len(candidates))
		for _, agentID := range candidates {
			if preferredSet[agentID] {
				preferredCandidates = append(preferredCandidates, agentID)
			}
		}
		if len(preferredCandidates) > 0 {
			candidates = preferredCandidates
		}
	}
	target := candidates[0]
	for _, agentID := range candidates[1:] {
		if lastTurn[agentID] < lastTurn[target] {
			target = agentID
		}
	}
	return target
}

func runModeration(runner agent.Runner, model, topic string, current *types.DeliberationControlState, debate *types.DebateLedger, observation moderationObservation) (*types.DeliberationControlState, error) {
	if runner == nil {
		return nil, fmt.Errorf("moderator runner unavailable")
	}
	if model == "" {
		return nil, fmt.Errorf("moderator model is required")
	}
	options := moderationOptions(current, observation)
	if len(options) == 0 {
		return nil, fmt.Errorf("no validated moderator action is available")
	}
	actions := make([]map[string]any, 0, len(options))
	for _, option := range options {
		actions = append(actions, moderatorActionMap(option))
	}
	envelope := map[string]any{
		"topic":         topic,
		"control_state": current,
		"ledger":        types.CloneDebateLedger(debate),
		"moderation_contract": map[string]any{
			"instruction":     "Return exactly one action object from actions. Do not add fields or terminal outcomes.",
			"trigger":         observation.Trigger,
			"round":           observation.Round,
			"leading_crux":    observation.LeadingCrux,
			"stagnant_rounds": observation.StagnantRounds,
			"actions":         actions,
		},
	}
	content, _, err := runner.Run(agent.WithReadOnlyAgentPrompt(agent.ModeratorConfig(model)), envelope)
	if err != nil {
		return nil, fmt.Errorf("moderator call: %w", err)
	}
	action, err := parseModeratorAction(content)
	if err != nil {
		return nil, err
	}
	return applyModeratorAction(current, observation, action)
}

func moderatorActionMap(action moderatorActionRequest) map[string]any {
	return map[string]any{
		"kind":             action.Kind,
		"target_agent_id":  action.TargetAgentID,
		"crux":             action.Crux,
		"proposal_version": action.ProposalVersion,
		"objection_ids":    action.ObjectionIDs,
		"claim_ids":        action.ClaimIDs,
	}
}

func parseModeratorAction(content string) (moderatorActionRequest, error) {
	cleaned := llmutil.StripCodeFences(content)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cleaned), &fields); err != nil {
		return moderatorActionRequest{}, fmt.Errorf("parsing moderator JSON: %w", err)
	}
	for _, field := range []string{"kind", "target_agent_id", "crux", "proposal_version", "objection_ids", "claim_ids"} {
		if _, ok := fields[field]; !ok {
			return moderatorActionRequest{}, fmt.Errorf("incomplete moderator action: missing %q", field)
		}
	}
	decoder := json.NewDecoder(bytes.NewBufferString(cleaned))
	decoder.DisallowUnknownFields()
	var action moderatorActionRequest
	if err := decoder.Decode(&action); err != nil {
		return moderatorActionRequest{}, fmt.Errorf("parsing moderator JSON: %w", err)
	}
	if err := ensureModeratorJSONEnd(decoder); err != nil {
		return moderatorActionRequest{}, err
	}
	if action.ObjectionIDs == nil || action.ClaimIDs == nil {
		return moderatorActionRequest{}, fmt.Errorf("incomplete moderator action: references must be arrays")
	}
	return action, nil
}

func ensureModeratorJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("parsing moderator JSON: %w", err)
	}
	return fmt.Errorf("parsing moderator JSON: multiple values are not allowed")
}

func applyModeratorAction(current *types.DeliberationControlState, observation moderationObservation, request moderatorActionRequest) (*types.DeliberationControlState, error) {
	if current == nil {
		return nil, fmt.Errorf("moderator control state is nil")
	}
	if err := current.Validate(); err != nil {
		return nil, fmt.Errorf("current moderator control state: %w", err)
	}
	if current.Phase == types.PhaseTerminal {
		return nil, fmt.Errorf("terminal control state is immutable")
	}
	if observation.Round <= current.Convergence.LastModeratedRound {
		return nil, fmt.Errorf("moderator round %d is already recorded", observation.Round)
	}
	matched := false
	for _, option := range moderationOptions(current, observation) {
		if sameModeratorActionRequest(option, request) {
			matched = true
			break
		}
	}
	if !matched {
		return nil, fmt.Errorf("invalid moderator action %q for current state", request.Kind)
	}

	next, err := cloneControlState(current)
	if err != nil {
		return nil, err
	}
	next.ModeratorAction = types.ModeratorAction{
		Kind:            request.Kind,
		Phase:           current.Phase,
		Trigger:         observation.Trigger,
		TargetAgentID:   request.TargetAgentID,
		Crux:            request.Crux,
		ProposalVersion: request.ProposalVersion,
		ObjectionIDs:    append([]string{}, request.ObjectionIDs...),
		ClaimIDs:        append([]string{}, request.ClaimIDs...),
	}
	next.Directive = directiveForModeratorAction(request)
	next.Convergence.CurrentEndorsements = observation.CurrentEndorsements
	next.Convergence.UnresolvedObjections = len(observation.UnresolvedObjectionIDs)
	next.Convergence.EvidenceGaps = len(observation.EvidenceGapClaimIDs)
	next.Convergence.StagnantRounds = observation.StagnantRounds
	next.Convergence.ReadyToVote = observation.ReadyToVote
	next.Convergence.LastModeratedRound = observation.Round
	if err := next.Validate(); err != nil {
		return nil, fmt.Errorf("validating moderator action: %w", err)
	}
	if err := types.ValidateDeliberationTransition(current, next); err != nil {
		return nil, fmt.Errorf("validating moderator transition: %w", err)
	}
	return next, nil
}

func sameModeratorActionRequest(a, b moderatorActionRequest) bool {
	return a.Kind == b.Kind && a.TargetAgentID == b.TargetAgentID && a.Crux == b.Crux && a.ProposalVersion == b.ProposalVersion &&
		equalModeratorStrings(a.ObjectionIDs, b.ObjectionIDs) && equalModeratorStrings(a.ClaimIDs, b.ClaimIDs)
}

func equalModeratorStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func directiveForModeratorAction(action moderatorActionRequest) types.TurnDirective {
	switch action.Kind {
	case types.ModeratorActionDirectResponse:
		return types.TurnDirective{Kind: types.DirectiveRespond, TargetAgentID: action.TargetAgentID, Crux: action.Crux}
	case types.ModeratorActionRequestEvidence:
		return types.TurnDirective{Kind: types.DirectiveVerify, TargetAgentID: action.TargetAgentID, ClaimID: action.ClaimIDs[0]}
	case types.ModeratorActionRequestRevision:
		return types.TurnDirective{Kind: types.DirectiveReviseProposal, TargetAgentID: action.TargetAgentID, ProposalVersion: action.ProposalVersion}
	case types.ModeratorActionCallVote:
		return types.TurnDirective{Kind: types.DirectiveVote, TargetAgentID: action.TargetAgentID, ProposalVersion: action.ProposalVersion}
	default:
		return types.TurnDirective{Kind: types.DirectiveNone}
	}
}
