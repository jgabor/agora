package types

import (
	"fmt"
	"reflect"
)

// DeliberationProtocolVersion identifies the typed deliberation control
// protocol written by this release.
const DeliberationProtocolVersion = "agora.deliberation.v1"

// DeliberationPhase identifies the current stage of a deliberation. Phase
// advancement remains an orchestrator concern; this package only validates
// protocol state and lifecycle invariants.
type DeliberationPhase string

const (
	PhaseOpening  DeliberationPhase = "opening"
	PhaseRebuttal DeliberationPhase = "rebuttal"
	PhaseDrafting DeliberationPhase = "drafting"
	PhaseVoting   DeliberationPhase = "voting"
	PhaseTerminal DeliberationPhase = "terminal"
)

// CanonicalProposal is one immutable version of the proposal under debate.
type CanonicalProposal struct {
	Version    int    `yaml:"version" json:"version"`
	AuthorID   string `yaml:"author_id" json:"author_id"`
	Content    string `yaml:"content" json:"content"`
	Supersedes int    `yaml:"supersedes" json:"supersedes"`
}

// Objection records a durable challenge to a proposal or claim.
type Objection struct {
	ID              string `yaml:"id" json:"id"`
	AgentID         string `yaml:"agent_id" json:"agent_id"`
	ProposalVersion int    `yaml:"proposal_version" json:"proposal_version"`
	ClaimID         string `yaml:"claim_id,omitempty" json:"claim_id,omitempty"`
	Summary         string `yaml:"summary" json:"summary"`
}

// ObjectionDispositionStatus identifies how an objection was handled.
type ObjectionDispositionStatus string

const (
	DispositionResolved  ObjectionDispositionStatus = "resolved"
	DispositionSustained ObjectionDispositionStatus = "sustained"
	DispositionWithdrawn ObjectionDispositionStatus = "withdrawn"
)

// ObjectionDisposition records a model-produced resolution decision. Agora
// validates and stores it without inventing the decision or its rationale.
type ObjectionDisposition struct {
	ObjectionID string                     `yaml:"objection_id" json:"objection_id"`
	AgentID     string                     `yaml:"agent_id" json:"agent_id"`
	Status      ObjectionDispositionStatus `yaml:"status" json:"status"`
	Rationale   string                     `yaml:"rationale" json:"rationale"`
}

// VoteChoice is an agent's position on one proposal version.
type VoteChoice string

const (
	VoteEndorse VoteChoice = "endorse"
	VoteReject  VoteChoice = "reject"
	VoteAbstain VoteChoice = "abstain"
)

// ProposalVote is durable vote history. A vote counts only while its
// ProposalVersion equals CurrentProposalVersion.
type ProposalVote struct {
	AgentID         string     `yaml:"agent_id" json:"agent_id"`
	ProposalVersion int        `yaml:"proposal_version" json:"proposal_version"`
	Choice          VoteChoice `yaml:"choice" json:"choice"`
}

// ClaimKind distinguishes facts from non-factual reasoning in the ledger.
type ClaimKind string

const (
	ClaimFact           ClaimKind = "fact"
	ClaimInference      ClaimKind = "inference"
	ClaimAssumption     ClaimKind = "assumption"
	ClaimRecommendation ClaimKind = "recommendation"
)

// ClaimEvidenceStatus is the durable verification state of a claim.
type ClaimEvidenceStatus string

const (
	EvidenceUnverified         ClaimEvidenceStatus = "unverified"
	EvidenceVerified           ClaimEvidenceStatus = "verified"
	EvidenceConflicting        ClaimEvidenceStatus = "conflicting"
	EvidenceUnsupported        ClaimEvidenceStatus = "unsupported"
	EvidenceVerificationFailed ClaimEvidenceStatus = "verification_failed"
)

// ClaimEvidence records a decisive claim and references to the bounded shared
// evidence bundle. It never stores retrieved source content.
type ClaimEvidence struct {
	ID              string              `yaml:"id" json:"id"`
	AgentID         string              `yaml:"agent_id" json:"agent_id"`
	ProposalVersion int                 `yaml:"proposal_version" json:"proposal_version"`
	Kind            ClaimKind           `yaml:"kind" json:"kind"`
	Decisive        bool                `yaml:"decisive" json:"decisive"`
	Status          ClaimEvidenceStatus `yaml:"status" json:"status"`
	SourceRefs      []int               `yaml:"source_refs" json:"source_refs"`
}

// ProposalActionKind identifies how a contribution changes the canonical
// proposal. "none" is explicit so incomplete model output cannot be mistaken
// for a proposal endorsement.
type ProposalActionKind string

const (
	ProposalActionNone   ProposalActionKind = "none"
	ProposalActionCreate ProposalActionKind = "create"
	ProposalActionRevise ProposalActionKind = "revise"
)

// ContributionProposalAction is the proposal mutation requested by one agent
// turn. Supersedes is required for revisions and zero otherwise.
type ContributionProposalAction struct {
	Kind       ProposalActionKind `yaml:"kind" json:"kind"`
	Content    string             `yaml:"content,omitempty" json:"content,omitempty"`
	Supersedes int                `yaml:"supersedes,omitempty" json:"supersedes,omitempty"`
}

// ContributionResponse records an agent's response to an existing objection.
// A response changes objection state only when it includes a disposition.
type ContributionResponse struct {
	ObjectionID string                     `yaml:"objection_id" json:"objection_id"`
	Response    string                     `yaml:"response" json:"response"`
	Disposition ObjectionDispositionStatus `yaml:"disposition,omitempty" json:"disposition,omitempty"`
	Rationale   string                     `yaml:"rationale,omitempty" json:"rationale,omitempty"`
}

// AgentContribution is the accepted structured output of one agent turn. The
// processor, not the model, binds AgentID and Turn. Nested protocol records are
// likewise stamped with that identity before they enter canonical history.
type AgentContribution struct {
	AgentID        string                     `yaml:"agent_id" json:"agent_id"`
	Turn           int                        `yaml:"turn" json:"turn"`
	Position       string                     `yaml:"position" json:"position"`
	Responses      []ContributionResponse     `yaml:"responses" json:"responses"`
	Concessions    []string                   `yaml:"concessions" json:"concessions"`
	ProposalAction ContributionProposalAction `yaml:"proposal_action" json:"proposal_action"`
	Objections     []Objection                `yaml:"objections" json:"objections"`
	Vote           *ProposalVote              `yaml:"vote,omitempty" json:"vote,omitempty"`
	Claims         []ClaimEvidence            `yaml:"claims" json:"claims"`
}

// DirectiveKind identifies the required work for the next directed turn.
// DirectiveNone is used while an opening speaker only needs to state an
// independent position or when the current state has no more specific work.
type DirectiveKind string

const (
	DirectiveNone           DirectiveKind = "none"
	DirectiveRespond        DirectiveKind = "respond"
	DirectiveVerify         DirectiveKind = "verify"
	DirectiveReviseProposal DirectiveKind = "revise_proposal"
	DirectiveVote           DirectiveKind = "vote"
)

// TurnDirective is the outstanding state-driven instruction for one agent.
// References identify existing debate state; the orchestrator does not invent
// the response, verification result, proposal text, or vote.
type TurnDirective struct {
	Kind            DirectiveKind `yaml:"kind" json:"kind"`
	TargetAgentID   string        `yaml:"target_agent_id,omitempty" json:"target_agent_id,omitempty"`
	Crux            string        `yaml:"crux,omitempty" json:"crux,omitempty"`
	ObjectionID     string        `yaml:"objection_id,omitempty" json:"objection_id,omitempty"`
	ClaimID         string        `yaml:"claim_id,omitempty" json:"claim_id,omitempty"`
	ProposalVersion int           `yaml:"proposal_version,omitempty" json:"proposal_version,omitempty"`
}

// ModeratorActionKind identifies the latest typed moderator decision.
type ModeratorActionKind string

const (
	ModeratorActionNone            ModeratorActionKind = "none"
	ModeratorActionContinue        ModeratorActionKind = "continue"
	ModeratorActionRequestRevision ModeratorActionKind = "request_revision"
	ModeratorActionRequestEvidence ModeratorActionKind = "request_evidence"
	ModeratorActionCallVote        ModeratorActionKind = "call_vote"
)

// ModeratorAction records the latest moderator decision and its references.
type ModeratorAction struct {
	Kind            ModeratorActionKind `yaml:"kind" json:"kind"`
	TargetAgentID   string              `yaml:"target_agent_id,omitempty" json:"target_agent_id,omitempty"`
	ProposalVersion int                 `yaml:"proposal_version,omitempty" json:"proposal_version,omitempty"`
	ObjectionIDs    []string            `yaml:"objection_ids" json:"objection_ids"`
	ClaimIDs        []string            `yaml:"claim_ids" json:"claim_ids"`
}

// ConvergenceSignals captures computed control inputs without deciding how the
// orchestrator advances or halts.
type ConvergenceSignals struct {
	CurrentEndorsements  int  `yaml:"current_endorsements" json:"current_endorsements"`
	RequiredEndorsements int  `yaml:"required_endorsements" json:"required_endorsements"`
	UnresolvedObjections int  `yaml:"unresolved_objections" json:"unresolved_objections"`
	EvidenceGaps         int  `yaml:"evidence_gaps" json:"evidence_gaps"`
	StagnantRounds       int  `yaml:"stagnant_rounds" json:"stagnant_rounds"`
	ReadyToVote          bool `yaml:"ready_to_vote" json:"ready_to_vote"`
}

// TerminalOutcomeKind distinguishes an active deliberation from its two typed
// terminal outcomes.
type TerminalOutcomeKind string

const (
	OutcomePending     TerminalOutcomeKind = "pending"
	OutcomeConsensus   TerminalOutcomeKind = "consensus"
	OutcomeNoConsensus TerminalOutcomeKind = "no_consensus"
)

// TerminalOutcome records the authoritative result and all unresolved
// references needed by later synthesis and inspection.
type TerminalOutcome struct {
	Kind                   TerminalOutcomeKind `yaml:"kind" json:"kind"`
	ProposalVersion        int                 `yaml:"proposal_version,omitempty" json:"proposal_version,omitempty"`
	Reason                 string              `yaml:"reason,omitempty" json:"reason,omitempty"`
	DissentingAgentIDs     []string            `yaml:"dissenting_agent_ids" json:"dissenting_agent_ids"`
	UnresolvedObjectionIDs []string            `yaml:"unresolved_objection_ids" json:"unresolved_objection_ids"`
	EvidenceGapClaimIDs    []string            `yaml:"evidence_gap_claim_ids" json:"evidence_gap_claim_ids"`
}

// DeliberationControlState is the versioned typed control plane for one
// deliberation. It contains model-produced debate decisions and computed
// control signals; validation must pass before an orchestrator relies on it.
type DeliberationControlState struct {
	ProtocolVersion        string                 `yaml:"protocol_version" json:"protocol_version"`
	Phase                  DeliberationPhase      `yaml:"phase" json:"phase"`
	AgentIDs               []string               `yaml:"agent_ids" json:"agent_ids"`
	SourceReferenceCount   int                    `yaml:"source_reference_count" json:"source_reference_count"`
	CurrentProposalVersion int                    `yaml:"current_proposal_version" json:"current_proposal_version"`
	Proposals              []CanonicalProposal    `yaml:"proposals" json:"proposals"`
	Objections             []Objection            `yaml:"objections" json:"objections"`
	Dispositions           []ObjectionDisposition `yaml:"dispositions" json:"dispositions"`
	Votes                  []ProposalVote         `yaml:"votes" json:"votes"`
	Claims                 []ClaimEvidence        `yaml:"claims" json:"claims"`
	Contributions          []AgentContribution    `yaml:"contributions" json:"contributions"`
	Directive              TurnDirective          `yaml:"directive" json:"directive"`
	ModeratorAction        ModeratorAction        `yaml:"moderator_action" json:"moderator_action"`
	Convergence            ConvergenceSignals     `yaml:"convergence" json:"convergence"`
	Outcome                TerminalOutcome        `yaml:"outcome" json:"outcome"`
}

// NewDeliberationControlState returns an explicit initial protocol state.
func NewDeliberationControlState(agentIDs []string, sourceReferenceCount int) *DeliberationControlState {
	return &DeliberationControlState{
		ProtocolVersion:      DeliberationProtocolVersion,
		Phase:                PhaseOpening,
		AgentIDs:             append([]string(nil), agentIDs...),
		SourceReferenceCount: sourceReferenceCount,
		Proposals:            []CanonicalProposal{},
		Objections:           []Objection{},
		Dispositions:         []ObjectionDisposition{},
		Votes:                []ProposalVote{},
		Claims:               []ClaimEvidence{},
		Contributions:        []AgentContribution{},
		Directive:            TurnDirective{Kind: DirectiveNone},
		ModeratorAction: ModeratorAction{
			Kind:         ModeratorActionNone,
			ObjectionIDs: []string{},
			ClaimIDs:     []string{},
		},
		Outcome: TerminalOutcome{
			Kind:                   OutcomePending,
			DissentingAgentIDs:     []string{},
			UnresolvedObjectionIDs: []string{},
			EvidenceGapClaimIDs:    []string{},
		},
	}
}

// IsCurrentVote reports whether vote can count for the canonical proposal.
func (s *DeliberationControlState) IsCurrentVote(v ProposalVote) bool {
	return s != nil && s.CurrentProposalVersion > 0 && v.ProposalVersion == s.CurrentProposalVersion
}

// CurrentVotes returns the votes that can count for the canonical proposal.
func (s *DeliberationControlState) CurrentVotes() []ProposalVote {
	var current []ProposalVote
	if s == nil {
		return current
	}
	for _, vote := range s.Votes {
		if s.IsCurrentVote(vote) {
			current = append(current, vote)
		}
	}
	return current
}

// UnresolvedObjections returns objections without a resolving or withdrawing
// disposition. Sustained objections remain unresolved.
func (s *DeliberationControlState) UnresolvedObjections() []Objection {
	closed := make(map[string]bool)
	for _, disposition := range s.Dispositions {
		if disposition.Status == DispositionResolved || disposition.Status == DispositionWithdrawn {
			closed[disposition.ObjectionID] = true
		}
	}
	var unresolved []Objection
	for _, objection := range s.Objections {
		if !closed[objection.ID] {
			unresolved = append(unresolved, objection)
		}
	}
	return unresolved
}

// EvidenceGaps returns decisive claims that are not verified.
func (s *DeliberationControlState) EvidenceGaps() []ClaimEvidence {
	var gaps []ClaimEvidence
	for _, claim := range s.Claims {
		if claim.Decisive && claim.Status != EvidenceVerified {
			gaps = append(gaps, claim)
		}
	}
	return gaps
}

// Validate checks all identities, references, vote uniqueness, and terminal
// invariants before the state can control a turn or halt.
func (s *DeliberationControlState) Validate() error {
	if s == nil {
		return fmt.Errorf("control state is nil")
	}
	if s.ProtocolVersion != DeliberationProtocolVersion {
		return fmt.Errorf("unsupported protocol_version %q", s.ProtocolVersion)
	}
	if !validPhase(s.Phase) {
		return fmt.Errorf("invalid phase %q", s.Phase)
	}
	if s.SourceReferenceCount < 0 {
		return fmt.Errorf("source_reference_count must be >= 0")
	}
	agents := make(map[string]bool, len(s.AgentIDs))
	for _, id := range s.AgentIDs {
		if id == "" {
			return fmt.Errorf("agent identity must be non-empty")
		}
		if agents[id] {
			return fmt.Errorf("duplicate agent identity %q", id)
		}
		agents[id] = true
	}
	turns := make(map[int]bool, len(s.Contributions))
	for i, contribution := range s.Contributions {
		if !agents[contribution.AgentID] {
			return fmt.Errorf("contribution %d references unknown agent %q", i, contribution.AgentID)
		}
		if contribution.Turn < 0 || turns[contribution.Turn] {
			return fmt.Errorf("duplicate or invalid contribution turn %d", contribution.Turn)
		}
		turns[contribution.Turn] = true
		if contribution.Position == "" {
			return fmt.Errorf("contribution at turn %d position must be non-empty", contribution.Turn)
		}
		if !validProposalAction(contribution.ProposalAction.Kind) {
			return fmt.Errorf("contribution at turn %d has invalid proposal action %q", contribution.Turn, contribution.ProposalAction.Kind)
		}
	}
	proposals := make(map[int]CanonicalProposal, len(s.Proposals))
	for _, proposal := range s.Proposals {
		if proposal.Version <= 0 || proposals[proposal.Version].Version != 0 {
			return fmt.Errorf("duplicate or invalid proposal version %d", proposal.Version)
		}
		if !agents[proposal.AuthorID] {
			return fmt.Errorf("proposal version %d references unknown agent %q", proposal.Version, proposal.AuthorID)
		}
		if proposal.Content == "" {
			return fmt.Errorf("proposal version %d content must be non-empty", proposal.Version)
		}
		if proposal.Version == 1 && proposal.Supersedes != 0 || proposal.Version > 1 && proposal.Supersedes != proposal.Version-1 {
			return fmt.Errorf("proposal version %d has invalid supersedes reference %d", proposal.Version, proposal.Supersedes)
		}
		proposals[proposal.Version] = proposal
	}
	if s.CurrentProposalVersion == 0 {
		if len(proposals) != 0 {
			return fmt.Errorf("current_proposal_version is 0 with proposal history")
		}
	} else {
		if _, ok := proposals[s.CurrentProposalVersion]; !ok {
			return fmt.Errorf("current proposal version %d is unknown", s.CurrentProposalVersion)
		}
		for version := 1; version <= s.CurrentProposalVersion; version++ {
			if _, ok := proposals[version]; !ok {
				return fmt.Errorf("proposal history missing version %d", version)
			}
		}
		if len(proposals) != s.CurrentProposalVersion {
			return fmt.Errorf("proposal history extends beyond current version %d", s.CurrentProposalVersion)
		}
	}

	claims := make(map[string]bool, len(s.Claims))
	for _, claim := range s.Claims {
		if claim.ID == "" || claims[claim.ID] {
			return fmt.Errorf("duplicate or empty claim id %q", claim.ID)
		}
		claims[claim.ID] = true
		if !agents[claim.AgentID] {
			return fmt.Errorf("claim %q references unknown agent %q", claim.ID, claim.AgentID)
		}
		if _, ok := proposals[claim.ProposalVersion]; !ok {
			return fmt.Errorf("claim %q references unknown proposal version %d", claim.ID, claim.ProposalVersion)
		}
		if !validClaimKind(claim.Kind) || !validEvidenceStatus(claim.Status) {
			return fmt.Errorf("claim %q has invalid kind or evidence status", claim.ID)
		}
		seenRefs := make(map[int]bool)
		for _, ref := range claim.SourceRefs {
			if ref < 0 || ref >= s.SourceReferenceCount {
				return fmt.Errorf("claim %q references unknown source %d", claim.ID, ref)
			}
			if seenRefs[ref] {
				return fmt.Errorf("claim %q has duplicate source reference %d", claim.ID, ref)
			}
			seenRefs[ref] = true
		}
	}

	objections := make(map[string]bool, len(s.Objections))
	for _, objection := range s.Objections {
		if objection.ID == "" || objections[objection.ID] {
			return fmt.Errorf("duplicate or empty objection id %q", objection.ID)
		}
		objections[objection.ID] = true
		if !agents[objection.AgentID] {
			return fmt.Errorf("objection %q references unknown agent %q", objection.ID, objection.AgentID)
		}
		if _, ok := proposals[objection.ProposalVersion]; !ok {
			return fmt.Errorf("objection %q references unknown proposal version %d", objection.ID, objection.ProposalVersion)
		}
		if objection.ClaimID != "" && !claims[objection.ClaimID] {
			return fmt.Errorf("objection %q references unknown claim %q", objection.ID, objection.ClaimID)
		}
	}

	dispositions := make(map[string]bool, len(s.Dispositions))
	for _, disposition := range s.Dispositions {
		if !objections[disposition.ObjectionID] {
			return fmt.Errorf("disposition references unknown objection %q", disposition.ObjectionID)
		}
		if dispositions[disposition.ObjectionID] {
			return fmt.Errorf("duplicate disposition for objection %q", disposition.ObjectionID)
		}
		dispositions[disposition.ObjectionID] = true
		if !agents[disposition.AgentID] {
			return fmt.Errorf("disposition for %q references unknown agent %q", disposition.ObjectionID, disposition.AgentID)
		}
		if !validDisposition(disposition.Status) {
			return fmt.Errorf("disposition for %q has invalid status %q", disposition.ObjectionID, disposition.Status)
		}
	}

	currentVoters := make(map[string]bool)
	votes := make(map[string]bool)
	for _, vote := range s.Votes {
		if !agents[vote.AgentID] {
			return fmt.Errorf("vote references unknown agent %q", vote.AgentID)
		}
		if _, ok := proposals[vote.ProposalVersion]; !ok {
			return fmt.Errorf("vote by %q references unknown proposal version %d", vote.AgentID, vote.ProposalVersion)
		}
		if !validVote(vote.Choice) {
			return fmt.Errorf("vote by %q has invalid choice %q", vote.AgentID, vote.Choice)
		}
		key := fmt.Sprintf("%s\x00%d", vote.AgentID, vote.ProposalVersion)
		if votes[key] {
			if vote.ProposalVersion == s.CurrentProposalVersion {
				return fmt.Errorf("duplicate current vote by agent %q", vote.AgentID)
			}
			return fmt.Errorf("duplicate vote by agent %q for proposal version %d", vote.AgentID, vote.ProposalVersion)
		}
		votes[key] = true
		if s.IsCurrentVote(vote) {
			if currentVoters[vote.AgentID] {
				return fmt.Errorf("duplicate current vote by agent %q", vote.AgentID)
			}
			currentVoters[vote.AgentID] = true
		}
	}
	if err := s.validateContributions(objections); err != nil {
		return err
	}
	if err := s.validateDirective(agents, proposals, objections, claims); err != nil {
		return err
	}

	if err := s.validateAction(agents, proposals, objections, claims); err != nil {
		return err
	}
	if err := s.validateOutcome(agents, proposals, objections, claims); err != nil {
		return err
	}
	if s.Convergence.CurrentEndorsements < 0 || s.Convergence.RequiredEndorsements < 0 || s.Convergence.UnresolvedObjections < 0 || s.Convergence.EvidenceGaps < 0 || s.Convergence.StagnantRounds < 0 {
		return fmt.Errorf("convergence signal counts must be >= 0")
	}
	return nil
}

func (s *DeliberationControlState) validateContributions(objectionIDs map[string]bool) error {
	actionVersions := make(map[int]bool)
	for _, contribution := range s.Contributions {
		for _, concession := range contribution.Concessions {
			if concession == "" {
				return fmt.Errorf("contribution at turn %d has an empty concession", contribution.Turn)
			}
		}
		for _, response := range contribution.Responses {
			if !objectionIDs[response.ObjectionID] || response.Response == "" {
				return fmt.Errorf("contribution at turn %d has an invalid objection response", contribution.Turn)
			}
			if response.Disposition == "" && response.Rationale != "" || response.Disposition != "" && (!validDisposition(response.Disposition) || response.Rationale == "") {
				return fmt.Errorf("contribution at turn %d has an invalid objection disposition", contribution.Turn)
			}
			if response.Disposition != "" && !containsDisposition(s.Dispositions, ObjectionDisposition{ObjectionID: response.ObjectionID, AgentID: contribution.AgentID, Status: response.Disposition, Rationale: response.Rationale}) {
				return fmt.Errorf("contribution at turn %d disposition is not bound to canonical state", contribution.Turn)
			}
		}
		action := contribution.ProposalAction
		switch action.Kind {
		case ProposalActionNone:
			if action.Content != "" || action.Supersedes != 0 {
				return fmt.Errorf("contribution at turn %d has invalid none proposal action", contribution.Turn)
			}
		case ProposalActionCreate, ProposalActionRevise:
			version := 1
			if action.Kind == ProposalActionRevise {
				version = action.Supersedes + 1
			}
			if actionVersions[version] || !containsProposal(s.Proposals, CanonicalProposal{Version: version, AuthorID: contribution.AgentID, Content: action.Content, Supersedes: action.Supersedes}) {
				return fmt.Errorf("contribution at turn %d proposal action is not bound to canonical state", contribution.Turn)
			}
			actionVersions[version] = true
		}
		for _, objection := range contribution.Objections {
			if objection.AgentID != contribution.AgentID || !containsObjection(s.Objections, objection) {
				return fmt.Errorf("contribution at turn %d objection is not bound to its agent", contribution.Turn)
			}
		}
		if contribution.Vote != nil && (contribution.Vote.AgentID != contribution.AgentID || !containsVote(s.Votes, *contribution.Vote)) {
			return fmt.Errorf("contribution at turn %d vote is not bound to its agent", contribution.Turn)
		}
		for _, claim := range contribution.Claims {
			if claim.AgentID != contribution.AgentID || !containsClaim(s.Claims, claim) {
				return fmt.Errorf("contribution at turn %d claim is not bound to its agent", contribution.Turn)
			}
		}
	}
	return nil
}

func containsProposal(values []CanonicalProposal, target CanonicalProposal) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, target) {
			return true
		}
	}
	return false
}

func containsObjection(values []Objection, target Objection) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, target) {
			return true
		}
	}
	return false
}

func containsDisposition(values []ObjectionDisposition, target ObjectionDisposition) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, target) {
			return true
		}
	}
	return false
}

func containsVote(values []ProposalVote, target ProposalVote) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, target) {
			return true
		}
	}
	return false
}

func containsClaim(values []ClaimEvidence, target ClaimEvidence) bool {
	for _, value := range values {
		if reflect.DeepEqual(value, target) {
			return true
		}
	}
	return false
}

// ValidateDeliberationTransition checks immutable history and monotonic
// lifecycle rules without choosing the next phase or creating debate content.
func ValidateDeliberationTransition(previous, next *DeliberationControlState) error {
	if err := previous.Validate(); err != nil {
		return fmt.Errorf("previous control state: %w", err)
	}
	if err := next.Validate(); err != nil {
		return fmt.Errorf("next control state: %w", err)
	}
	if !equalStrings(previous.AgentIDs, next.AgentIDs) || previous.SourceReferenceCount != next.SourceReferenceCount {
		return fmt.Errorf("agent identities and source reference bounds are immutable")
	}
	if previous.Phase == PhaseTerminal {
		if reflect.DeepEqual(previous, next) {
			return nil
		}
		return fmt.Errorf("terminal control state is immutable")
	}
	if !validPhaseTransition(previous.Phase, next.Phase) {
		return fmt.Errorf("invalid phase transition from %q to %q", previous.Phase, next.Phase)
	}
	if next.CurrentProposalVersion < previous.CurrentProposalVersion || next.CurrentProposalVersion > previous.CurrentProposalVersion+1 {
		return fmt.Errorf("invalid proposal lifecycle transition from version %d to %d", previous.CurrentProposalVersion, next.CurrentProposalVersion)
	}
	if len(next.Proposals) < len(previous.Proposals) || len(next.Objections) < len(previous.Objections) || len(next.Dispositions) < len(previous.Dispositions) || len(next.Votes) < len(previous.Votes) || len(next.Claims) < len(previous.Claims) || len(next.Contributions) < len(previous.Contributions) {
		return fmt.Errorf("protocol history cannot be removed")
	}
	if !reflect.DeepEqual(previous.Proposals, next.Proposals[:len(previous.Proposals)]) ||
		!reflect.DeepEqual(previous.Objections, next.Objections[:len(previous.Objections)]) ||
		!reflect.DeepEqual(previous.Dispositions, next.Dispositions[:len(previous.Dispositions)]) ||
		!reflect.DeepEqual(previous.Votes, next.Votes[:len(previous.Votes)]) ||
		!reflect.DeepEqual(previous.Claims, next.Claims[:len(previous.Claims)]) ||
		!reflect.DeepEqual(previous.Contributions, next.Contributions[:len(previous.Contributions)]) {
		return fmt.Errorf("protocol history is immutable")
	}
	if previous.Outcome.Kind != OutcomePending && !reflect.DeepEqual(previous.Outcome, next.Outcome) {
		return fmt.Errorf("terminal outcome is immutable")
	}
	return nil
}

func (s *DeliberationControlState) validateDirective(agents map[string]bool, proposals map[int]CanonicalProposal, objections, claims map[string]bool) error {
	directive := s.Directive
	if directive.Kind == "" { // Backward-compatible typed snapshots predate directives.
		directive.Kind = DirectiveNone
	}
	if directive.Kind == DirectiveNone {
		if directive.TargetAgentID != "" || directive.Crux != "" || directive.ObjectionID != "" || directive.ClaimID != "" || directive.ProposalVersion != 0 {
			return fmt.Errorf("none directive cannot include a target or references")
		}
		return nil
	}
	if !agents[directive.TargetAgentID] {
		return fmt.Errorf("directive references unknown target agent %q", directive.TargetAgentID)
	}
	switch directive.Kind {
	case DirectiveRespond:
		if (directive.Crux == "") == (directive.ObjectionID == "") || directive.ClaimID != "" || directive.ProposalVersion != 0 {
			return fmt.Errorf("respond directive requires exactly one crux or objection reference")
		}
		if directive.ObjectionID != "" && !objections[directive.ObjectionID] {
			return fmt.Errorf("directive references unknown objection %q", directive.ObjectionID)
		}
	case DirectiveVerify:
		if !claims[directive.ClaimID] || directive.Crux != "" || directive.ObjectionID != "" || directive.ProposalVersion != 0 {
			return fmt.Errorf("verify directive requires one known claim reference")
		}
	case DirectiveReviseProposal:
		if _, ok := proposals[directive.ProposalVersion]; !ok || directive.ProposalVersion != s.CurrentProposalVersion || directive.Crux != "" || directive.ObjectionID != "" || directive.ClaimID != "" {
			return fmt.Errorf("revise_proposal directive requires the current proposal version")
		}
	case DirectiveVote:
		if _, ok := proposals[directive.ProposalVersion]; !ok || directive.ProposalVersion != s.CurrentProposalVersion || directive.Crux != "" || directive.ObjectionID != "" || directive.ClaimID != "" {
			return fmt.Errorf("vote directive requires the current proposal version")
		}
	default:
		return fmt.Errorf("invalid directive kind %q", directive.Kind)
	}
	return nil
}

func (s *DeliberationControlState) validateAction(agents map[string]bool, proposals map[int]CanonicalProposal, objections, claims map[string]bool) error {
	action := s.ModeratorAction
	if !validModeratorAction(action.Kind) {
		return fmt.Errorf("invalid moderator action %q", action.Kind)
	}
	if action.TargetAgentID != "" && !agents[action.TargetAgentID] {
		return fmt.Errorf("moderator action references unknown agent %q", action.TargetAgentID)
	}
	if action.ProposalVersion != 0 {
		if _, ok := proposals[action.ProposalVersion]; !ok {
			return fmt.Errorf("moderator action references unknown proposal version %d", action.ProposalVersion)
		}
	}
	for _, id := range action.ObjectionIDs {
		if !objections[id] {
			return fmt.Errorf("moderator action references unknown objection %q", id)
		}
	}
	for _, id := range action.ClaimIDs {
		if !claims[id] {
			return fmt.Errorf("moderator action references unknown claim %q", id)
		}
	}
	return nil
}

func (s *DeliberationControlState) validateOutcome(agents map[string]bool, proposals map[int]CanonicalProposal, objections, claims map[string]bool) error {
	outcome := s.Outcome
	if outcome.Kind != OutcomePending && outcome.Kind != OutcomeConsensus && outcome.Kind != OutcomeNoConsensus {
		return fmt.Errorf("invalid terminal outcome %q", outcome.Kind)
	}
	if s.Phase == PhaseTerminal && outcome.Kind == OutcomePending {
		return fmt.Errorf("terminal phase requires a terminal outcome")
	}
	if s.Phase != PhaseTerminal && outcome.Kind != OutcomePending {
		return fmt.Errorf("terminal outcome requires terminal phase")
	}
	if outcome.ProposalVersion != 0 {
		if _, ok := proposals[outcome.ProposalVersion]; !ok {
			return fmt.Errorf("terminal outcome references unknown proposal version %d", outcome.ProposalVersion)
		}
	}
	for _, id := range outcome.DissentingAgentIDs {
		if !agents[id] {
			return fmt.Errorf("terminal outcome references unknown dissenting agent %q", id)
		}
	}
	for _, id := range outcome.UnresolvedObjectionIDs {
		if !objections[id] {
			return fmt.Errorf("terminal outcome references unknown objection %q", id)
		}
	}
	for _, id := range outcome.EvidenceGapClaimIDs {
		if !claims[id] {
			return fmt.Errorf("terminal outcome references unknown claim %q", id)
		}
	}
	return nil
}

func validPhase(v DeliberationPhase) bool {
	return v == PhaseOpening || v == PhaseRebuttal || v == PhaseDrafting || v == PhaseVoting || v == PhaseTerminal
}

func validPhaseTransition(previous, next DeliberationPhase) bool {
	if previous == next || next == PhaseTerminal {
		return true
	}
	return previous == PhaseOpening && next == PhaseRebuttal ||
		previous == PhaseRebuttal && next == PhaseDrafting ||
		previous == PhaseDrafting && next == PhaseVoting
}

func validDisposition(v ObjectionDispositionStatus) bool {
	return v == DispositionResolved || v == DispositionSustained || v == DispositionWithdrawn
}
func validVote(v VoteChoice) bool { return v == VoteEndorse || v == VoteReject || v == VoteAbstain }
func validClaimKind(v ClaimKind) bool {
	return v == ClaimFact || v == ClaimInference || v == ClaimAssumption || v == ClaimRecommendation
}

func validEvidenceStatus(v ClaimEvidenceStatus) bool {
	return v == EvidenceUnverified || v == EvidenceVerified || v == EvidenceConflicting || v == EvidenceUnsupported || v == EvidenceVerificationFailed
}

func validProposalAction(v ProposalActionKind) bool {
	return v == ProposalActionNone || v == ProposalActionCreate || v == ProposalActionRevise
}

func validModeratorAction(v ModeratorActionKind) bool {
	return v == ModeratorActionNone || v == ModeratorActionContinue || v == ModeratorActionRequestRevision || v == ModeratorActionRequestEvidence || v == ModeratorActionCallVote
}

func equalStrings(a, b []string) bool {
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
