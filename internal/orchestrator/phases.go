package orchestrator

import "github.com/jgabor/agora/internal/types"

// prepareNextTurn advances at most one required phase and records the
// deterministic outstanding directive for the next selected agent.
func prepareNextTurn(state *types.DeliberationControlState, debate *types.DebateLedger, preserveOutstanding bool) {
	if state == nil || state.Phase == types.PhaseTerminal || len(state.AgentIDs) == 0 {
		return
	}
	if preserveOutstanding && state.Directive.Kind != "" && state.Directive.Kind != types.DirectiveNone {
		return
	}

	switch state.Phase {
	case types.PhaseOpening:
		if openingComplete(state) {
			state.Phase = types.PhaseRebuttal
		}
	case types.PhaseRebuttal:
		if state.PhaseWorkComplete(2) {
			state.Phase = types.PhaseDrafting
		}
	case types.PhaseDrafting:
		if state.PhaseWorkComplete(3) && state.CurrentProposalVersion > 0 {
			state.Phase = types.PhaseVoting
		}
	}

	target := nextScheduledAgent(state)
	state.Directive = types.TurnDirective{Kind: types.DirectiveNone}
	switch state.Phase {
	case types.PhaseRebuttal:
		if objections := state.UnresolvedObjections(); len(objections) > 0 {
			state.Directive = types.TurnDirective{Kind: types.DirectiveRespond, TargetAgentID: target, ObjectionID: objections[0].ID}
		} else if debate != nil && len(debate.Cruxes) > 0 {
			_, preferred := leadingCrux(state, debate)
			target = fairModeratorTarget(state, preferred)
			state.Directive = types.TurnDirective{Kind: types.DirectiveRespond, TargetAgentID: target, Crux: debate.Cruxes[0].Topic}
		}
	case types.PhaseDrafting:
		if claimID := firstEvidenceGap(state); claimID != "" {
			state.Directive = types.TurnDirective{Kind: types.DirectiveVerify, TargetAgentID: target, ClaimID: claimID}
		} else if objections := state.UnresolvedObjections(); len(objections) > 0 {
			state.Directive = types.TurnDirective{Kind: types.DirectiveRespond, TargetAgentID: target, ObjectionID: objections[0].ID}
		} else if state.CurrentProposalVersion > 0 {
			state.Directive = types.TurnDirective{Kind: types.DirectiveReviseProposal, TargetAgentID: target, ProposalVersion: state.CurrentProposalVersion}
		}
	case types.PhaseVoting:
		if target = fairVoteTarget(state); target != "" {
			state.Directive = types.TurnDirective{Kind: types.DirectiveVote, TargetAgentID: target, ProposalVersion: state.CurrentProposalVersion}
		}
	}
}

func directiveFulfilled(directive types.TurnDirective, state *types.DeliberationControlState) bool {
	if directive.Kind == "" || directive.Kind == types.DirectiveNone {
		return true
	}
	switch directive.Kind {
	case types.DirectiveRespond:
		if len(state.Contributions) == 0 {
			return false
		}
		latest := state.Contributions[len(state.Contributions)-1]
		if latest.AgentID != directive.TargetAgentID {
			return false
		}
		if directive.Crux != "" {
			return true
		}
		for _, response := range latest.Responses {
			if response.ObjectionID == directive.ObjectionID {
				return true
			}
		}
	case types.DirectiveVerify:
		for _, claim := range state.Claims {
			if claim.ID == directive.ClaimID && claim.Status != types.EvidenceUnverified {
				return true
			}
		}
	case types.DirectiveReviseProposal:
		return state.CurrentProposalVersion > directive.ProposalVersion
	case types.DirectiveVote:
		for _, vote := range state.CurrentVotes() {
			if vote.AgentID == directive.TargetAgentID {
				return true
			}
		}
	}
	return false
}

func openingComplete(state *types.DeliberationControlState) bool {
	return state.PhaseWorkComplete(1)
}

func nextScheduledAgent(state *types.DeliberationControlState) string {
	if state.Phase == types.PhaseOpening {
		seen := make(map[string]bool, len(state.AgentIDs))
		for _, contribution := range state.Contributions {
			seen[contribution.AgentID] = true
		}
		for _, id := range state.AgentIDs {
			if !seen[id] {
				return id
			}
		}
	}
	return fairModeratorTarget(state, nil)
}

func firstEvidenceGap(state *types.DeliberationControlState) string {
	for _, claim := range state.Claims {
		if claim.Decisive && claim.Status == types.EvidenceUnverified {
			return claim.ID
		}
	}
	return ""
}
