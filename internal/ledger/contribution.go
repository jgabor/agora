package ledger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jgabor/agora/internal/llmutil"
	"github.com/jgabor/agora/internal/types"
)

// ContributionContract is included in each typed turn envelope. AgentID and
// turn are intentionally absent: the orchestrator binds both from execution
// context instead of trusting model output.
var ContributionContract = map[string]any{
	"instruction": "Return only one JSON object matching this contribution contract.",
	"required": []string{
		"position", "responses", "concessions", "proposal_action", "objections", "vote", "claims",
	},
	"proposal_action_kinds": []string{"none", "create", "revise"},
	"vote_choices":          []string{"endorse", "reject", "abstain"},
	"claim_kinds":           []string{"fact", "inference", "assumption", "recommendation"},
	"shape": map[string]any{
		"position":    "non-empty string",
		"responses":   []any{map[string]any{"objection_id": "string", "response": "string", "disposition": "resolved|sustained|withdrawn (optional)", "rationale": "required with disposition"}},
		"concessions": []string{},
		"proposal_action": map[string]any{
			"kind": "none|create|revise", "content": "required for create/revise", "supersedes": "current version for revise",
		},
		"objections": []any{map[string]any{"id": "string", "proposal_version": "current version", "claim_id": "optional", "summary": "string"}},
		"vote":       map[string]any{"proposal_version": "current version", "choice": "endorse|reject|abstain; null when not voting"},
		"claims":     []any{map[string]any{"id": "string", "proposal_version": "current version", "kind": "claim kind", "decisive": false, "status": "omit for a new claim; verification outcome for an existing claim", "source_refs": []int{}}},
	},
	"verification_outcomes": []string{"verified", "conflicting", "unsupported", "verification_failed"},
}

// OpeningContributionContract limits an independent opening to a position.
// Proposal and other canonical work begins only after every active agent has
// had that independent opportunity.
var OpeningContributionContract = map[string]any{
	"mode":        "position_only",
	"instruction": "Return only one JSON object with your independent opening position.",
	"required":    []string{"position"},
	"shape": map[string]any{
		"position": "non-empty string",
	},
}

// ContributionContractForPhase returns the action schema applicable to phase.
func ContributionContractForPhase(phase types.DeliberationPhase) map[string]any {
	if phase == types.PhaseOpening {
		return OpeningContributionContract
	}
	return ContributionContract
}

type contributionPayload struct {
	Position       string                           `json:"position"`
	Responses      []types.ContributionResponse     `json:"responses"`
	Concessions    []string                         `json:"concessions"`
	ProposalAction types.ContributionProposalAction `json:"proposal_action"`
	Objections     []contributionObjection          `json:"objections"`
	Vote           *contributionVote                `json:"vote"`
	Claims         []contributionClaim              `json:"claims"`
}

type contributionObjection struct {
	ID              string `json:"id"`
	ProposalVersion int    `json:"proposal_version"`
	ClaimID         string `json:"claim_id,omitempty"`
	Summary         string `json:"summary"`
}

type contributionVote struct {
	ProposalVersion int              `json:"proposal_version"`
	Choice          types.VoteChoice `json:"choice"`
}

type contributionClaim struct {
	ID              string                     `json:"id"`
	ProposalVersion int                        `json:"proposal_version"`
	Kind            types.ClaimKind            `json:"kind"`
	Decisive        *bool                      `json:"decisive"`
	Status          *types.ClaimEvidenceStatus `json:"status"`
	SourceRefs      []int                      `json:"source_refs"`
}

// ProcessContribution parses the phase-appropriate authoritative portion of
// one agent's structured output and atomically advances a cloned control
// state. Any error returns nil and leaves current untouched, so malformed
// output cannot endorse a proposal or dispose an objection.
func ProcessContribution(current *types.DeliberationControlState, agentID string, turn int, output string) (*types.DeliberationControlState, error) {
	payload, err := parseContribution(output, current != nil && current.Phase == types.PhaseOpening)
	if err != nil {
		return nil, fmt.Errorf("contribution from %q at turn %d: %w", agentID, turn, err)
	}
	next, err := applyContribution(current, agentID, turn, payload)
	if err != nil {
		return nil, fmt.Errorf("contribution from %q at turn %d: %w", agentID, turn, err)
	}
	return next, nil
}

func parseContribution(output string, opening bool) (contributionPayload, error) {
	if opening {
		return parseOpeningContribution(output)
	}
	cleaned := llmutil.StripCodeFences(output)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cleaned), &fields); err != nil {
		return contributionPayload{}, fmt.Errorf("parsing JSON: %w", err)
	}
	for _, field := range []string{"position", "responses", "concessions", "proposal_action", "objections", "vote", "claims"} {
		if _, ok := fields[field]; !ok {
			return contributionPayload{}, fmt.Errorf("incomplete output: missing %q", field)
		}
	}
	for _, field := range []string{"position", "responses", "concessions", "proposal_action", "objections", "claims"} {
		if bytes.Equal(bytes.TrimSpace(fields[field]), []byte("null")) {
			return contributionPayload{}, fmt.Errorf("incomplete output: %q must not be null", field)
		}
	}

	var payload contributionPayload
	decoder := json.NewDecoder(bytes.NewBufferString(cleaned))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&payload); err != nil {
		return contributionPayload{}, fmt.Errorf("parsing JSON: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return contributionPayload{}, err
	}
	return payload, nil
}

// parseOpeningContribution treats position as the sole authoritative opening
// field. Older or nonconforming model output can include generic contribution
// fields, but those fields are deliberately discarded so a proposal action
// cannot mutate canonical state or interrupt the opening schedule.
func parseOpeningContribution(output string) (contributionPayload, error) {
	cleaned := llmutil.StripCodeFences(output)
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(cleaned), &fields); err != nil {
		return contributionPayload{}, fmt.Errorf("parsing JSON: %w", err)
	}
	position, ok := fields["position"]
	if !ok {
		return contributionPayload{}, fmt.Errorf("incomplete output: missing %q", "position")
	}
	if bytes.Equal(bytes.TrimSpace(position), []byte("null")) {
		return contributionPayload{}, fmt.Errorf("incomplete output: %q must not be null", "position")
	}
	var value string
	if err := json.Unmarshal(position, &value); err != nil {
		return contributionPayload{}, fmt.Errorf("parsing JSON: %w", err)
	}
	return contributionPayload{
		Position:       value,
		Responses:      []types.ContributionResponse{},
		Concessions:    []string{},
		ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone},
		Objections:     []contributionObjection{},
		Claims:         []contributionClaim{},
	}, nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err == io.EOF {
		return nil
	} else if err != nil {
		return fmt.Errorf("parsing JSON: %w", err)
	}
	return fmt.Errorf("parsing JSON: multiple values are not allowed")
}

func applyContribution(current *types.DeliberationControlState, agentID string, turn int, payload contributionPayload) (*types.DeliberationControlState, error) {
	if err := current.Validate(); err != nil {
		return nil, fmt.Errorf("current control state: %w", err)
	}
	if current.Phase == types.PhaseTerminal {
		return nil, fmt.Errorf("terminal control state is immutable")
	}
	if !contains(current.AgentIDs, agentID) {
		return nil, fmt.Errorf("unknown agent")
	}
	if current.Directive.Kind != "" && current.Directive.Kind != types.DirectiveNone && current.Directive.TargetAgentID != agentID {
		return nil, fmt.Errorf("turn is directed to agent %q", current.Directive.TargetAgentID)
	}
	if current.Phase == types.PhaseOpening {
		for _, prior := range current.Contributions {
			if prior.AgentID == agentID {
				return nil, fmt.Errorf("opening phase permits one contribution per agent")
			}
		}
	}
	if turn < 0 {
		return nil, fmt.Errorf("turn must be >= 0")
	}
	for _, prior := range current.Contributions {
		if prior.Turn == turn {
			return nil, fmt.Errorf("turn already has an accepted contribution")
		}
	}
	if strings.TrimSpace(payload.Position) == "" {
		return nil, fmt.Errorf("position must be non-empty")
	}

	next, err := cloneControlState(current)
	if err != nil {
		return nil, err
	}
	contribution := types.AgentContribution{
		AgentID: agentID, Turn: turn, Position: payload.Position,
		Responses:      append(make([]types.ContributionResponse, 0, len(payload.Responses)), payload.Responses...),
		Concessions:    append(make([]string, 0, len(payload.Concessions)), payload.Concessions...),
		ProposalAction: payload.ProposalAction,
		Objections:     []types.Objection{}, Claims: []types.ClaimEvidence{},
	}

	if err := applyProposalAction(next, agentID, payload.ProposalAction); err != nil {
		return nil, err
	}
	if err := applyClaims(next, &contribution, agentID, payload.Claims); err != nil {
		return nil, err
	}
	if err := applyObjections(next, &contribution, agentID, payload.Objections); err != nil {
		return nil, err
	}
	if err := applyResponses(next, agentID, payload.Responses); err != nil {
		return nil, err
	}
	if err := applyVote(next, &contribution, agentID, payload.Vote); err != nil {
		return nil, err
	}
	if err := validateConcessions(payload.Concessions); err != nil {
		return nil, err
	}
	if current.Directive.Kind == types.DirectiveReviseProposal && next.CurrentProposalVersion > current.Directive.ProposalVersion {
		next.Directive = types.TurnDirective{Kind: types.DirectiveNone}
	}

	next.Contributions = append(next.Contributions, contribution)
	recomputeContributionSignals(next)
	if err := types.ValidateDeliberationTransition(current, next); err != nil {
		return nil, err
	}
	return next, nil
}

func applyProposalAction(state *types.DeliberationControlState, agentID string, action types.ContributionProposalAction) error {
	switch action.Kind {
	case types.ProposalActionNone:
		if action.Content != "" || action.Supersedes != 0 {
			return fmt.Errorf("proposal action none cannot include content or supersedes")
		}
	case types.ProposalActionCreate:
		if state.CurrentProposalVersion != 0 {
			return fmt.Errorf("cannot create a proposal when version %d is current", state.CurrentProposalVersion)
		}
		if strings.TrimSpace(action.Content) == "" || action.Supersedes != 0 {
			return fmt.Errorf("create proposal action requires content and supersedes 0")
		}
		state.CurrentProposalVersion = 1
		state.Proposals = append(state.Proposals, types.CanonicalProposal{Version: 1, AuthorID: agentID, Content: action.Content})
	case types.ProposalActionRevise:
		if state.CurrentProposalVersion == 0 || action.Supersedes != state.CurrentProposalVersion || strings.TrimSpace(action.Content) == "" {
			return fmt.Errorf("revise proposal action must supersede current version %d with non-empty content", state.CurrentProposalVersion)
		}
		version := state.CurrentProposalVersion + 1
		state.Proposals = append(state.Proposals, types.CanonicalProposal{Version: version, AuthorID: agentID, Content: action.Content, Supersedes: action.Supersedes})
		state.CurrentProposalVersion = version
	default:
		return fmt.Errorf("invalid proposal action %q", action.Kind)
	}
	return nil
}

func applyClaims(state *types.DeliberationControlState, contribution *types.AgentContribution, agentID string, claims []contributionClaim) error {
	known := make(map[string]int, len(state.Claims)+len(claims))
	for i, claim := range state.Claims {
		known[claim.ID] = i
	}
	seen := make(map[string]bool, len(claims))
	verificationDirective := state.Directive.Kind == types.DirectiveVerify
	verificationMatched := false
	for _, claim := range claims {
		if claim.ID == "" || seen[claim.ID] {
			return fmt.Errorf("claim id must be non-empty and unique")
		}
		seen[claim.ID] = true

		if index, exists := known[claim.ID]; exists {
			if !verificationDirective || state.Directive.ClaimID != claim.ID {
				return fmt.Errorf("claim %q is already in the ledger; verification requires its active directive", claim.ID)
			}
			if claim.Status == nil || !validVerificationOutcome(*claim.Status) {
				return fmt.Errorf("claim %q verification requires a typed outcome", claim.ID)
			}
			prior := state.Claims[index]
			if claim.ProposalVersion != 0 && claim.ProposalVersion != prior.ProposalVersion {
				return fmt.Errorf("claim %q verification changes its proposal version", claim.ID)
			}
			if claim.Kind != "" && claim.Kind != prior.Kind {
				return fmt.Errorf("claim %q verification changes its kind", claim.ID)
			}
			if claim.Decisive != nil && *claim.Decisive != prior.Decisive {
				return fmt.Errorf("claim %q verification changes its decisive flag", claim.ID)
			}
			refs := make([]int, 0, len(prior.SourceRefs))
			refs = append(refs, prior.SourceRefs...)
			if claim.SourceRefs != nil {
				var err error
				refs, err = mergeSourceRefs(state.SourceReferenceCount, refs, claim.SourceRefs)
				if err != nil {
					return fmt.Errorf("claim %q verification: %w", claim.ID, err)
				}
			}
			if (*claim.Status == types.EvidenceVerified || *claim.Status == types.EvidenceConflicting) && len(refs) == 0 {
				return fmt.Errorf("claim %q outcome %q requires a supplied source reference", claim.ID, *claim.Status)
			}
			if prior.Status != types.EvidenceUnverified {
				return fmt.Errorf("claim %q already has verification outcome %q", claim.ID, prior.Status)
			}
			updated := prior
			updated.Status = *claim.Status
			updated.SourceRefs = refs
			state.Claims[index] = updated
			verification := updated
			verification.AgentID = agentID
			contribution.Claims = append(contribution.Claims, verification)
			verificationMatched = true
			continue
		}

		if state.CurrentProposalVersion == 0 || claim.ProposalVersion != state.CurrentProposalVersion {
			return fmt.Errorf("claim %q must reference current proposal version %d", claim.ID, state.CurrentProposalVersion)
		}
		if !validClaimKind(claim.Kind) {
			return fmt.Errorf("claim %q has invalid kind %q", claim.ID, claim.Kind)
		}
		if claim.Decisive == nil {
			return fmt.Errorf("claim %q decisive is required", claim.ID)
		}
		if claim.SourceRefs == nil {
			return fmt.Errorf("claim %q source_refs is required", claim.ID)
		}
		if err := validateSourceRefs(state.SourceReferenceCount, claim.SourceRefs); err != nil {
			return fmt.Errorf("claim %q: %w", claim.ID, err)
		}
		status := types.EvidenceUnverified
		if claim.Status != nil {
			if *claim.Status != types.EvidenceUnverified {
				return fmt.Errorf("new claim %q must enter as explicitly unverified", claim.ID)
			}
			status = *claim.Status
		}
		record := types.ClaimEvidence{ID: claim.ID, AgentID: agentID, ProposalVersion: claim.ProposalVersion, Kind: claim.Kind, Decisive: *claim.Decisive, Status: status, SourceRefs: append(make([]int, 0, len(claim.SourceRefs)), claim.SourceRefs...)}
		state.Claims = append(state.Claims, record)
		contribution.Claims = append(contribution.Claims, record)
		known[claim.ID] = len(state.Claims) - 1
	}
	if verificationDirective && !verificationMatched {
		return fmt.Errorf("verification directive for claim %q requires a typed outcome", state.Directive.ClaimID)
	}
	return nil
}

func validateSourceRefs(sourceReferenceCount int, refs []int) error {
	seen := make(map[int]bool, len(refs))
	for _, ref := range refs {
		if ref < 0 || ref >= sourceReferenceCount {
			return fmt.Errorf("references unknown source %d", ref)
		}
		if seen[ref] {
			return fmt.Errorf("has duplicate source reference %d", ref)
		}
		seen[ref] = true
	}
	return nil
}

func mergeSourceRefs(sourceReferenceCount int, existing, additional []int) ([]int, error) {
	if err := validateSourceRefs(sourceReferenceCount, additional); err != nil {
		return nil, err
	}
	refs := make([]int, 0, len(existing)+len(additional))
	refs = append(refs, existing...)
	seen := make(map[int]bool, len(refs)+len(additional))
	for _, ref := range refs {
		seen[ref] = true
	}
	for _, ref := range additional {
		if !seen[ref] {
			refs = append(refs, ref)
			seen[ref] = true
		}
	}
	return refs, nil
}

func applyObjections(state *types.DeliberationControlState, contribution *types.AgentContribution, agentID string, objections []contributionObjection) error {
	known := make(map[string]bool, len(state.Objections)+len(objections))
	for _, objection := range state.Objections {
		known[objection.ID] = true
	}
	for _, objection := range objections {
		if objection.ID == "" || known[objection.ID] || strings.TrimSpace(objection.Summary) == "" {
			return fmt.Errorf("objection id and summary must be non-empty and id must be unique")
		}
		if state.CurrentProposalVersion == 0 || objection.ProposalVersion != state.CurrentProposalVersion {
			return fmt.Errorf("objection %q must reference current proposal version %d", objection.ID, state.CurrentProposalVersion)
		}
		if objection.ClaimID != "" && !hasClaim(state.Claims, objection.ClaimID) {
			return fmt.Errorf("objection %q references unknown claim %q", objection.ID, objection.ClaimID)
		}
		record := types.Objection{ID: objection.ID, AgentID: agentID, ProposalVersion: objection.ProposalVersion, ClaimID: objection.ClaimID, Summary: objection.Summary}
		state.Objections = append(state.Objections, record)
		contribution.Objections = append(contribution.Objections, record)
		known[objection.ID] = true
	}
	return nil
}

func applyResponses(state *types.DeliberationControlState, agentID string, responses []types.ContributionResponse) error {
	seen := make(map[string]bool, len(responses))
	for _, response := range responses {
		if response.ObjectionID == "" || strings.TrimSpace(response.Response) == "" || seen[response.ObjectionID] {
			return fmt.Errorf("response objection_id and response must be non-empty and unique")
		}
		seen[response.ObjectionID] = true
		if !hasObjection(state.Objections, response.ObjectionID) {
			return fmt.Errorf("response references unknown objection %q", response.ObjectionID)
		}
		if response.Disposition == "" {
			if response.Rationale != "" {
				return fmt.Errorf("response rationale requires a disposition")
			}
			continue
		}
		if !validDisposition(response.Disposition) || strings.TrimSpace(response.Rationale) == "" {
			return fmt.Errorf("response disposition for %q is invalid or missing rationale", response.ObjectionID)
		}
		if hasDisposition(state.Dispositions, response.ObjectionID) {
			return fmt.Errorf("objection %q already has a disposition", response.ObjectionID)
		}
		state.Dispositions = append(state.Dispositions, types.ObjectionDisposition{ObjectionID: response.ObjectionID, AgentID: agentID, Status: response.Disposition, Rationale: response.Rationale})
	}
	return nil
}

func applyVote(state *types.DeliberationControlState, contribution *types.AgentContribution, agentID string, vote *contributionVote) error {
	if vote == nil {
		return nil
	}
	if state.CurrentProposalVersion == 0 || vote.ProposalVersion != state.CurrentProposalVersion {
		return fmt.Errorf("vote must reference current proposal version %d", state.CurrentProposalVersion)
	}
	if vote.Choice != types.VoteEndorse && vote.Choice != types.VoteReject && vote.Choice != types.VoteAbstain {
		return fmt.Errorf("invalid vote choice %q", vote.Choice)
	}
	for _, prior := range state.Votes {
		if prior.AgentID == agentID && prior.ProposalVersion == vote.ProposalVersion {
			return fmt.Errorf("agent already voted on proposal version %d", vote.ProposalVersion)
		}
	}
	record := types.ProposalVote{AgentID: agentID, ProposalVersion: vote.ProposalVersion, Choice: vote.Choice}
	state.Votes = append(state.Votes, record)
	contribution.Vote = &record
	return nil
}

func validateConcessions(concessions []string) error {
	seen := make(map[string]bool, len(concessions))
	for _, concession := range concessions {
		if strings.TrimSpace(concession) == "" || seen[concession] {
			return fmt.Errorf("concessions must be non-empty and unique")
		}
		seen[concession] = true
	}
	return nil
}

func recomputeContributionSignals(state *types.DeliberationControlState) {
	endorsements := 0
	for _, vote := range state.CurrentVotes() {
		if vote.Choice == types.VoteEndorse {
			endorsements++
		}
	}
	state.Convergence.CurrentEndorsements = endorsements
	state.Convergence.UnresolvedObjections = len(state.UnresolvedObjections())
	state.Convergence.EvidenceGaps = len(state.EvidenceGaps())
}

func cloneControlState(state *types.DeliberationControlState) (*types.DeliberationControlState, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("cloning control state: %w", err)
	}
	var clone types.DeliberationControlState
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, fmt.Errorf("cloning control state: %w", err)
	}
	return &clone, nil
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func hasClaim(claims []types.ClaimEvidence, id string) bool {
	for _, claim := range claims {
		if claim.ID == id {
			return true
		}
	}
	return false
}

func hasObjection(objections []types.Objection, id string) bool {
	for _, objection := range objections {
		if objection.ID == id {
			return true
		}
	}
	return false
}

func hasDisposition(dispositions []types.ObjectionDisposition, id string) bool {
	for _, disposition := range dispositions {
		if disposition.ObjectionID == id {
			return true
		}
	}
	return false
}

func validClaimKind(kind types.ClaimKind) bool {
	return kind == types.ClaimFact || kind == types.ClaimInference || kind == types.ClaimAssumption || kind == types.ClaimRecommendation
}

func validVerificationOutcome(status types.ClaimEvidenceStatus) bool {
	return status == types.EvidenceVerified || status == types.EvidenceConflicting || status == types.EvidenceUnsupported || status == types.EvidenceVerificationFailed
}

func validDisposition(status types.ObjectionDispositionStatus) bool {
	return status == types.DispositionResolved || status == types.DispositionSustained || status == types.DispositionWithdrawn
}
