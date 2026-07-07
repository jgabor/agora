// Package types defines domain types for the Agora deliberation system.
//
// The debate ledger (DebateLedger, LedgerRecord, and related types) captures
// the compact per-round state each agent receives in its per-turn envelope:
// every active agent's most recent stated position, the points agents have
// agreed on, the cruxes currently open, and the current draft proposal (or an
// explicit no-draft status). Ledger snapshots persist as typed transcript
// records (LedgerRecord) using Turn = LedgerSentinelTurn (-3) and
// AgentID = LedgerAgentID ("ledger"), the next sentinel beyond -1 (seed) and
// -2 (evidence). Per Decision 1, the orchestrator treats the ledger as opaque
// DATA injected into the envelope and must not compose prompts from ledger
// contents; per Decision 6, transcripts persist references-only typed state,
// not full prose.
package types

import (
	"fmt"
	"strings"
)

// Topology represents the interaction topology between agents.
type Topology string

const (
	TopologyRing Topology = "ring"
	TopologyStar Topology = "star"
	TopologyMesh Topology = "mesh"
)

var validTopologies = []Topology{TopologyRing, TopologyStar, TopologyMesh}

// AutoLevel represents the auto-configuration level for deliberation.
type AutoLevel string

const (
	AutoOff    AutoLevel = "off"
	AutoQuick  AutoLevel = "quick"
	AutoNormal AutoLevel = "normal"
	AutoDeep   AutoLevel = "deep"
	AutoYOLO   AutoLevel = "yolo"
)

var validAutoLevels = []AutoLevel{AutoOff, AutoQuick, AutoNormal, AutoDeep, AutoYOLO}

// ParseAutoLevel parses a string into an AutoLevel.
func ParseAutoLevel(s string) (AutoLevel, error) {
	normalized := strings.ToLower(s)
	switch AutoLevel(normalized) {
	case AutoOff, AutoQuick, AutoNormal, AutoDeep, AutoYOLO:
		return AutoLevel(normalized), nil
	default:
		valid := make([]string, len(validAutoLevels))
		for i, l := range validAutoLevels {
			valid[i] = string(l)
		}
		return "", fmt.Errorf("unknown auto level '%s'. Expected one of: %s", s, strings.Join(valid, ", "))
	}
}

// LevelCaps holds hard-coded caps for an auto-configuration level.
type LevelCaps struct {
	MaxAgents int
	MaxTurns  int
	TimeLimit int
}

// CapsForLevel returns the hard-coded LevelCaps for the given AutoLevel.
// A value of 0 means unlimited.
func CapsForLevel(level AutoLevel) LevelCaps {
	switch level {
	case AutoQuick:
		return LevelCaps{MaxAgents: 2, MaxTurns: 4, TimeLimit: 60}
	case AutoNormal:
		return LevelCaps{MaxAgents: 4, MaxTurns: 10, TimeLimit: 300}
	case AutoDeep:
		return LevelCaps{MaxAgents: 8, MaxTurns: 20, TimeLimit: 900}
	case AutoYOLO:
		return LevelCaps{MaxAgents: 0, MaxTurns: 0, TimeLimit: 0}
	case AutoOff:
		return LevelCaps{}
	default:
		return LevelCaps{}
	}
}

// ParseTopology parses a string into a Topology.
func ParseTopology(s string) (Topology, error) {
	normalized := strings.ToLower(strings.ReplaceAll(s, "-", "_"))
	switch Topology(normalized) {
	case TopologyRing, TopologyStar, TopologyMesh:
		return Topology(normalized), nil
	default:
		valid := make([]string, len(validTopologies))
		for i, t := range validTopologies {
			valid[i] = string(t)
		}
		return "", fmt.Errorf("unknown topology '%s'. Expected one of: %s", s, strings.Join(valid, ", "))
	}
}

// AgentIdentity holds optional non-avatar display metadata for an agent.
// The canonical visible identity remains AgentConfig.ID.
type AgentIdentity struct {
	DisplayName string `yaml:"display_name,omitempty" json:"display_name,omitempty"`
	Role        string `yaml:"role,omitempty" json:"role,omitempty"`
	Affiliation string `yaml:"affiliation,omitempty" json:"affiliation,omitempty"`
}

// AgentConfig holds configuration for a single deliberation agent.
type AgentConfig struct {
	ID           string         `yaml:"id" json:"id"`
	Model        string         `yaml:"model" json:"model"`
	SystemPrompt string         `yaml:"system_prompt" json:"system_prompt"`
	Identity     *AgentIdentity `yaml:"identity,omitempty" json:"identity,omitempty"`
}

// CastMember is the durable display identity assigned to an agent for a run.
type CastMember struct {
	ID            int    `yaml:"id" json:"id"`
	Name          string `yaml:"name" json:"name"`
	Persona       string `yaml:"persona" json:"persona"`
	ProviderModel string `yaml:"provider_model" json:"provider_model"`
	Color         string `yaml:"color" json:"color"`
}

// TranscriptMetadata is written into the first transcript record so replay can
// render the original cast without reloading the config file.
type TranscriptMetadata struct {
	ID            int                 `yaml:"id,omitempty" json:"id,omitempty"`
	SchemaVersion int                 `yaml:"schema_version" json:"schema_version"`
	Cast          []CastMember        `yaml:"cast" json:"cast"`
	Config        *DeliberationConfig `yaml:"config" json:"config"`
}

// Validate checks that the agent has a non-empty id and model.
func (a *AgentConfig) Validate() error {
	if a.ID == "" {
		return fmt.Errorf("agent must have a non-empty 'id'")
	}
	if a.Model == "" {
		return fmt.Errorf("agent '%s' must have a non-empty 'model'", a.ID)
	}
	return nil
}

// DeliberationConfig holds the top-level deliberation configuration.
type DeliberationConfig struct {
	Agents             []AgentConfig `yaml:"agents" json:"agents"`
	Topology           Topology      `yaml:"topology" json:"topology"`
	ConsensusThreshold int           `yaml:"consensus_threshold" json:"consensus_threshold"`
	MinRounds          int           `yaml:"min_rounds" json:"min_rounds"`
	SynthesisModel     *string       `yaml:"synthesis_model,omitempty" json:"synthesis_model,omitempty"`
	MetaModel          *string       `yaml:"meta_model,omitempty" json:"meta_model,omitempty"`
	ResearchEnabled    bool          `yaml:"research" json:"research"`
	Ledger             *bool         `yaml:"ledger,omitempty" json:"ledger,omitempty"`
	ContextPaths       []string      `yaml:"context,omitempty" json:"context,omitempty"`
}

// EffectiveMinRounds returns the minimum full agent rounds before consensus halt.
// Zero means one round (default).
func (c *DeliberationConfig) EffectiveMinRounds() int {
	if c == nil || c.MinRounds <= 0 {
		return 1
	}
	return c.MinRounds
}

// NewTranscriptMetadata captures the run setup needed to replay a transcript.
func NewTranscriptMetadata(cfg *DeliberationConfig, cast []CastMember) *TranscriptMetadata {
	return &TranscriptMetadata{
		SchemaVersion: 1,
		Cast:          cast,
		Config:        CloneDeliberationConfig(cfg),
	}
}

// CloneDeliberationConfig returns a deep copy suitable for transcript metadata.
func CloneDeliberationConfig(cfg *DeliberationConfig) *DeliberationConfig {
	if cfg == nil {
		return nil
	}
	clone := *cfg
	clone.Agents = append([]AgentConfig(nil), cfg.Agents...)
	for i := range clone.Agents {
		if cfg.Agents[i].Identity != nil {
			identity := *cfg.Agents[i].Identity
			clone.Agents[i].Identity = &identity
		}
	}
	clone.ContextPaths = append([]string(nil), cfg.ContextPaths...)
	if cfg.Ledger != nil {
		v := *cfg.Ledger
		clone.Ledger = &v
	}
	if cfg.SynthesisModel != nil {
		model := *cfg.SynthesisModel
		clone.SynthesisModel = &model
	}
	if cfg.MetaModel != nil {
		model := *cfg.MetaModel
		clone.MetaModel = &model
	}
	return &clone
}

// EffectiveMetaModel returns the effective model for meta-tasks (convergence,
// moderation, synthesis). Fallback: meta_model → synthesis_model → agent[0].model → "".
func (c *DeliberationConfig) EffectiveMetaModel() string {
	if c != nil && c.MetaModel != nil && *c.MetaModel != "" {
		return *c.MetaModel
	}
	if c != nil && c.SynthesisModel != nil && *c.SynthesisModel != "" {
		return *c.SynthesisModel
	}
	if c != nil && len(c.Agents) > 0 && c.Agents[0].Model != "" {
		return c.Agents[0].Model
	}
	return ""
}

// Validate checks the full configuration for correctness.
func (c *DeliberationConfig) Validate() error {
	if len(c.Agents) == 0 {
		return fmt.Errorf("configuration must contain at least one agent")
	}
	seenIDs := make(map[string]bool)
	for i := range c.Agents {
		if err := c.Agents[i].Validate(); err != nil {
			return fmt.Errorf("agent %d: %w", i, err)
		}
		if seenIDs[c.Agents[i].ID] {
			return fmt.Errorf("duplicate agent id: '%s'", c.Agents[i].ID)
		}
		seenIDs[c.Agents[i].ID] = true
	}
	if c.ConsensusThreshold < 0 {
		return fmt.Errorf("consensus_threshold must be >= 0")
	}
	if c.MinRounds < 0 {
		return fmt.Errorf("min_rounds must be >= 0")
	}
	switch c.Topology {
	case TopologyRing, TopologyStar, TopologyMesh:
	case "":
	default:
		valid := make([]string, len(validTopologies))
		for i, t := range validTopologies {
			valid[i] = string(t)
		}
		return fmt.Errorf("unknown topology '%s'. Expected one of: %s", c.Topology, strings.Join(valid, ", "))
	}
	return nil
}

// TokenUsage holds token usage metadata from a model call.
type TokenUsage struct {
	Total     *int `yaml:"total,omitempty" json:"total,omitempty"`
	Input     *int `yaml:"input,omitempty" json:"input,omitempty"`
	Output    *int `yaml:"output,omitempty" json:"output,omitempty"`
	Reasoning *int `yaml:"reasoning,omitempty" json:"reasoning,omitempty"`
}

// RunMetadata holds the typed metadata returned from a single agent run.
// The Runner interface returns a pointer to this struct; nil means no metadata
// (e.g. dry-run mode).
type RunMetadata struct {
	Tokens TokenUsage `json:"tokens"`
	Cost   *float64   `json:"cost,omitempty"`
}

// TurnRecord represents a single turn in the deliberation transcript.
type TurnRecord struct {
	Turn               int                 `yaml:"turn" json:"turn"`
	AgentID            string              `yaml:"agent_id" json:"agent_id"`
	Model              *string             `yaml:"model,omitempty" json:"model,omitempty"`
	Transcript         *TranscriptMetadata `yaml:"transcript,omitempty" json:"transcript,omitempty"`
	Timestamp          float64             `yaml:"timestamp" json:"timestamp"`
	Content            string              `yaml:"content" json:"content"`
	Evidence           *EvidenceBundle     `yaml:"evidence,omitempty" json:"evidence,omitempty"`
	Ledger             *DebateLedger       `yaml:"ledger,omitempty" json:"ledger,omitempty"`
	Tokens             TokenUsage          `yaml:"tokens" json:"tokens"`
	Cost               *float64            `yaml:"cost,omitempty" json:"cost,omitempty"`
	Consensus          bool                `yaml:"consensus" json:"consensus"`
	ConsensusStatement string              `yaml:"consensus_statement" json:"consensus_statement"`
	ConsensusIgnored   bool                `yaml:"consensus_ignored,omitempty" json:"consensus_ignored,omitempty"`
	Elapsed            float64             `yaml:"elapsed" json:"elapsed"`
}

// DeliverableGate describes a topic-required artifact before consensus halt.
type DeliverableGate struct {
	MinItems int `yaml:"min_items" json:"min_items"`
}

// IsInternalAgent reports whether agentID is orchestrator/system, not deliberation cast.
func IsInternalAgent(agentID string) bool {
	return agentID == "moderator" || agentID == "synthesizer" || agentID == LedgerAgentID
}

// AgentTurnCount returns deliberation agent turns, excluding internal system agents.
func AgentTurnCount(records []TurnRecord) int {
	count := 0
	for _, r := range records {
		if !IsInternalAgent(r.AgentID) {
			count++
		}
	}
	return count
}

// DeliberationStats holds statistics computed from deliberation records.
type DeliberationStats struct {
	Records         []TurnRecord              `json:"records"`
	TotalTurns      int                       `json:"total_turns"`
	TotalTokens     int                       `json:"total_tokens"`
	TotalCost       float64                   `json:"total_cost"`
	AvgTurnDuration float64                   `json:"avg_turn_duration_seconds"`
	PerAgent        map[string]AgentTurnStats `json:"per_agent"`
	ConsensusEvents []ConsensusEvent          `json:"consensus_events"`
}

// AgentTurnStats holds per-agent aggregated statistics.
type AgentTurnStats struct {
	Turns  int     `json:"turns"`
	Tokens int     `json:"tokens"`
	Cost   float64 `json:"cost"`
}

// ConsensusEvent records a consensus occurrence.
type ConsensusEvent struct {
	Turn      int    `json:"turn"`
	AgentID   string `json:"agent_id"`
	Statement string `json:"statement"`
}

// DeliberationState tracks the runtime state of an ongoing deliberation.
type DeliberationState struct {
	Config      *DeliberationConfig
	Topic       string
	Window      int
	MaxTurns    int
	TimeLimit   int
	Budget      *float64
	FullContext bool
	Evidence    EvidenceRequest

	Turn                 int
	StartTime            float64
	Running              bool
	HaltedBy             string
	FinalConsensusStreak int
	DeliverableGate      *DeliverableGate
	LedgerUpdateEnabled  *bool
}

// EvidenceRequest captures the resolved pre-deliberation evidence policy.
type EvidenceRequest struct {
	ResearchEnabled bool
	Topic           string
	ResearchModel   string
	ContextPaths    []string
	MaxSources      int
	MaxBytes        int64
	MaxDepth        int
}

// SourceReference identifies an evidence source without storing full source content.
type SourceReference struct {
	Title       string `yaml:"title" json:"title"`
	URL         string `yaml:"url,omitempty" json:"url,omitempty"`
	Path        string `yaml:"path,omitempty" json:"path,omitempty"`
	Query       string `yaml:"query,omitempty" json:"query,omitempty"`
	RetrievedAt string `yaml:"retrieved_at,omitempty" json:"retrieved_at,omitempty"`
}

// ContextDocument carries bounded local text to agents. Transcript records must omit it.
type ContextDocument struct {
	Path    string `yaml:"path" json:"path"`
	Content string `yaml:"content" json:"content"`
}

// EvidenceBundle is the shared evidence result produced before deliberation.
type EvidenceBundle struct {
	Summary          string            `yaml:"summary" json:"summary"`
	SourceReferences []SourceReference `yaml:"source_references" json:"source_references"`
	ContextDocuments []ContextDocument `yaml:"context_documents,omitempty" json:"context_documents,omitempty"`
}

// ComputeStats computes DeliberationStats from a slice of TurnRecords.
func ComputeStats(records []TurnRecord) DeliberationStats {
	stats := DeliberationStats{
		Records:    records,
		TotalTurns: AgentTurnCount(records),
		PerAgent:   make(map[string]AgentTurnStats),
	}

	var totalDuration float64
	var durationCount int

	for _, r := range records {
		if IsInternalAgent(r.AgentID) {
			continue
		}
		stats.TotalTokens += IntVal(r.Tokens.Total)
		if r.Cost != nil {
			stats.TotalCost += *r.Cost
		}
		if r.Elapsed > 0 {
			totalDuration += r.Elapsed
			durationCount++
		}

		as := stats.PerAgent[r.AgentID]
		as.Turns++
		as.Tokens += IntVal(r.Tokens.Total)
		if r.Cost != nil {
			as.Cost += *r.Cost
		}
		stats.PerAgent[r.AgentID] = as

		if r.Consensus {
			stats.ConsensusEvents = append(stats.ConsensusEvents, ConsensusEvent{
				Turn:      r.Turn,
				AgentID:   r.AgentID,
				Statement: r.ConsensusStatement,
			})
		}
	}

	if durationCount > 0 {
		stats.AvgTurnDuration = totalDuration / float64(durationCount)
	}

	return stats
}

// IntVal safely dereferences an *int, returning 0 for nil.
func IntVal(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

// LedgerSentinelTurn is the transcript turn number reserved for ledger records.
// The convention reserves negative turn numbers for orchestrator-system
// records: -1 for seed, -2 for evidence, and -3 for ledger snapshots persisted
// after each completed deliberation round.
const LedgerSentinelTurn = -3

// LedgerAgentID is the agent_id sentinel written into transcript records that
// carry a ledger snapshot, distinguishing them from agent turns, seed (-1), and
// evidence (-2) records.
const LedgerAgentID = "ledger"

// DraftStatus enumerates the lifecycle states of the current draft proposal.
type DraftStatus string

const (
	DraftStatusNone  DraftStatus = "none"
	DraftStatusDraft DraftStatus = "draft"
	DraftStatusFinal DraftStatus = "final"
)

// AgentPosition captures an active agent's most recently stated position.
type AgentPosition struct {
	AgentID string `yaml:"agent_id" json:"agent_id"`
	Text    string `yaml:"text" json:"text"`
	Turn    int    `yaml:"turn" json:"turn"`
}

// AgreementPoint names a point two or more agents have agreed on.
type AgreementPoint struct {
	Text      string   `yaml:"text" json:"text"`
	Endorsers []string `yaml:"endorsers" json:"endorsers"`
}

// PositionalView captures one agent's stance on a crux.
type PositionalView struct {
	AgentID string `yaml:"agent_id" json:"agent_id"`
	Stance  string `yaml:"stance" json:"stance"`
}

// OpenCrux describes an unresolved disagreement currently in play.
type OpenCrux struct {
	Topic    string           `yaml:"topic" json:"topic"`
	Views    []PositionalView `yaml:"views" json:"views"`
	RaisedAt int              `yaml:"raised_at" json:"raised_at"`
}

// DraftProposal is the current draft proposal, or an explicit no-draft status.
// Status == DraftStatusNone (or "") is the explicit no-draft signal; the field
// is always serialized so consumers can distinguish "no draft" from a missing
// or stale value.
type DraftProposal struct {
	Status    DraftStatus `yaml:"status" json:"status"`
	Text      string      `yaml:"text,omitempty" json:"text,omitempty"`
	Proposer  string      `yaml:"proposer,omitempty" json:"proposer,omitempty"`
	Endorsers []string    `yaml:"endorsers,omitempty" json:"endorsers,omitempty"`
}

// HasEndorsableProposal reports whether the draft is in a state a consumer may
// treat as halt-permittable. A no-draft or incomplete draft returns false.
func (d *DraftProposal) HasEndorsableProposal() bool {
	if d == nil {
		return false
	}
	if d.Status == DraftStatusNone || d.Status == "" {
		return false
	}
	return d.Text != ""
}

// DebateLedger is the compact per-round debate state injected into each
// agent's per-turn envelope. It captures every active agent's most recent
// stated position, the points agents have agreed on, the cruxes currently
// open, and the current draft proposal (or an explicit no-draft status).
// Per Decision 1, the orchestrator treats the ledger as opaque DATA injected
// into the envelope and must not compose prompts from ledger contents;
// per Decision 6, transcripts persist references-only typed state, not full
// prose.
type DebateLedger struct {
	Round      int              `yaml:"round" json:"round"`
	Positions  []AgentPosition  `yaml:"positions" json:"positions"`
	Agreements []AgreementPoint `yaml:"agreements" json:"agreements"`
	Cruxes     []OpenCrux       `yaml:"cruxes" json:"cruxes"`
	Draft      DraftProposal    `yaml:"draft" json:"draft"`
	UpdatedAt  float64          `yaml:"updated_at" json:"updated_at"`
}

// NewDebateLedger returns a valid empty ledger suitable as an initial state.
// The draft is set to DraftStatusNone so the no-draft status is explicit.
func NewDebateLedger(round int, updatedAt float64) *DebateLedger {
	return &DebateLedger{
		Round:     round,
		Draft:     DraftProposal{Status: DraftStatusNone},
		UpdatedAt: updatedAt,
	}
}

// HasEndorsableProposal reports whether this ledger's current draft permits
// halting. Returns false when no draft exists (the explicit no-draft state).
func (l *DebateLedger) HasEndorsableProposal() bool {
	if l == nil {
		return false
	}
	return l.Draft.HasEndorsableProposal()
}

// CloneDebateLedger returns a deep copy of the ledger, suitable for persisting
// into a transcript record without aliasing caller-owned slices.
func CloneDebateLedger(l *DebateLedger) *DebateLedger {
	if l == nil {
		return nil
	}
	clone := *l
	clone.Positions = append([]AgentPosition(nil), l.Positions...)
	clone.Agreements = append([]AgreementPoint(nil), l.Agreements...)
	for i := range clone.Agreements {
		clone.Agreements[i].Endorsers = append([]string(nil), l.Agreements[i].Endorsers...)
	}
	clone.Cruxes = append([]OpenCrux(nil), l.Cruxes...)
	for i := range clone.Cruxes {
		clone.Cruxes[i].Views = append([]PositionalView(nil), l.Cruxes[i].Views...)
	}
	clone.Draft.Endorsers = append([]string(nil), l.Draft.Endorsers...)
	return &clone
}

// Validate checks the ledger for internal consistency. An empty ledger (no
// positions, agreements, or cruxes) is a valid initial state and does not
// produce an error.
func (l *DebateLedger) Validate() error {
	if l == nil {
		return fmt.Errorf("ledger is nil")
	}
	if l.Round < 0 {
		return fmt.Errorf("ledger round must be >= 0, got %d", l.Round)
	}
	seenPositions := make(map[string]bool)
	for i, p := range l.Positions {
		if p.AgentID == "" {
			return fmt.Errorf("position %d: agent_id must be non-empty", i)
		}
		if seenPositions[p.AgentID] {
			return fmt.Errorf("position %d: duplicate agent_id '%s'", i, p.AgentID)
		}
		seenPositions[p.AgentID] = true
	}
	for i, a := range l.Agreements {
		if a.Text == "" {
			return fmt.Errorf("agreement %d: text must be non-empty", i)
		}
	}
	for i, c := range l.Cruxes {
		if c.Topic == "" {
			return fmt.Errorf("crux %d: topic must be non-empty", i)
		}
	}
	switch l.Draft.Status {
	case DraftStatusNone, DraftStatusDraft, DraftStatusFinal, "":
	default:
		return fmt.Errorf("draft status '%s' is not one of: none, draft, final", l.Draft.Status)
	}
	if l.Draft.Status != DraftStatusNone && l.Draft.Status != "" && l.Draft.Text == "" {
		return fmt.Errorf("draft with status '%s' must have non-empty text", l.Draft.Status)
	}
	return nil
}

// LedgerRecord is the typed transcript record that persists a ledger snapshot.
// It is a sibling of TurnRecord: it carries a DebateLedger rather than agent
// prose, so show and resume can replay typed ledger state without re-parsing
// essays. The orchestrator writes one LedgerRecord per completed round using
// Turn = LedgerSentinelTurn (-3) and AgentID = LedgerAgentID ("ledger"), the
// next sentinel beyond -1 (seed) and -2 (evidence).
type LedgerRecord struct {
	Turn      int          `yaml:"turn" json:"turn"`
	AgentID   string       `yaml:"agent_id" json:"agent_id"`
	Timestamp float64      `yaml:"timestamp" json:"timestamp"`
	Ledger    DebateLedger `yaml:"ledger" json:"ledger"`
}

// NewLedgerRecord constructs a transcript record carrying a deep clone of the
// given ledger snapshot, using the LedgerSentinelTurn (-3) and LedgerAgentID
// ("ledger") convention. A nil ledger is replaced with a valid empty ledger.
func NewLedgerRecord(l *DebateLedger, timestamp float64) *LedgerRecord {
	ledger := CloneDebateLedger(l)
	if ledger == nil {
		ledger = NewDebateLedger(0, timestamp)
	}
	return &LedgerRecord{
		Turn:      LedgerSentinelTurn,
		AgentID:   LedgerAgentID,
		Timestamp: timestamp,
		Ledger:    *ledger,
	}
}

// Validate checks the ledger record for sentinel and content consistency.
func (r *LedgerRecord) Validate() error {
	if r == nil {
		return fmt.Errorf("ledger record is nil")
	}
	if r.Turn != LedgerSentinelTurn {
		return fmt.Errorf("ledger record turn must be %d (LedgerSentinelTurn), got %d", LedgerSentinelTurn, r.Turn)
	}
	if r.AgentID != LedgerAgentID {
		return fmt.Errorf("ledger record agent_id must be %q, got %q", LedgerAgentID, r.AgentID)
	}
	if err := r.Ledger.Validate(); err != nil {
		return fmt.Errorf("ledger record: %w", err)
	}
	return nil
}

// CloneLedgerRecord returns a deep copy of the ledger record.
func CloneLedgerRecord(r *LedgerRecord) *LedgerRecord {
	if r == nil {
		return nil
	}
	ledger := CloneDebateLedger(&r.Ledger)
	return &LedgerRecord{
		Turn:      r.Turn,
		AgentID:   r.AgentID,
		Timestamp: r.Timestamp,
		Ledger:    *ledger,
	}
}
