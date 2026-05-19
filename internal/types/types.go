// Package types defines domain types for the Agora deliberation system.
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
	SynthesisModel     *string       `yaml:"synthesis_model,omitempty" json:"synthesis_model,omitempty"`
	ResearchEnabled    bool          `yaml:"research" json:"research"`
	ContextPaths       []string      `yaml:"context,omitempty" json:"context,omitempty"`
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
	if cfg.SynthesisModel != nil {
		model := *cfg.SynthesisModel
		clone.SynthesisModel = &model
	}
	return &clone
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
	Tokens             TokenUsage          `yaml:"tokens" json:"tokens"`
	Cost               *float64            `yaml:"cost,omitempty" json:"cost,omitempty"`
	Consensus          bool                `yaml:"consensus" json:"consensus"`
	ConsensusStatement string              `yaml:"consensus_statement" json:"consensus_statement"`
	Elapsed            float64             `yaml:"elapsed" json:"elapsed"`
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

	Turn      int
	StartTime float64
	Running   bool
	HaltedBy  string
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
		TotalTurns: len(records),
		PerAgent:   make(map[string]AgentTurnStats),
	}

	var totalDuration float64
	var durationCount int

	for _, r := range records {
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
