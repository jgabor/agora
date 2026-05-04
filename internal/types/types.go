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

// AgentConfig holds configuration for a single deliberation agent.
type AgentConfig struct {
	ID           string `yaml:"id" json:"id"`
	Model        string `yaml:"model" json:"model"`
	SystemPrompt string `yaml:"system_prompt" json:"system_prompt"`
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

// TurnRecord represents a single turn in the deliberation transcript.
type TurnRecord struct {
	Turn               int        `yaml:"turn" json:"turn"`
	AgentID            string     `yaml:"agent_id" json:"agent_id"`
	Model              *string    `yaml:"model,omitempty" json:"model,omitempty"`
	Timestamp          float64    `yaml:"timestamp" json:"timestamp"`
	Content            string     `yaml:"content" json:"content"`
	Tokens             TokenUsage `yaml:"tokens" json:"tokens"`
	Cost               *float64   `yaml:"cost,omitempty" json:"cost,omitempty"`
	Consensus          bool       `yaml:"consensus" json:"consensus"`
	ConsensusStatement string     `yaml:"consensus_statement" json:"consensus_statement"`
	Elapsed            float64    `yaml:"elapsed" json:"elapsed"`
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

	Turn      int
	StartTime float64
	Running   bool
	HaltedBy  string
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
