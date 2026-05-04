package kumbaja

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// TranscriptManager manages the deliberation transcript as a JSONL file.
type TranscriptManager struct {
	path    string
	records []TurnRecord
	written int
}

// NewTranscriptManager creates a new TranscriptManager for the given file path.
func NewTranscriptManager(path string) *TranscriptManager {
	return &TranscriptManager{path: path}
}

// Records returns the in-memory slice of all loaded/appended turn records.
func (tm *TranscriptManager) Records() []TurnRecord {
	return tm.records
}

// LoadExisting loads an existing JSONL transcript file into memory.
// Returns all loaded records or an empty slice if the file does not exist.
func (tm *TranscriptManager) LoadExisting() ([]TurnRecord, error) {
	if _, err := os.Stat(tm.path); os.IsNotExist(err) {
		return nil, nil
	}

	f, err := os.Open(tm.path)
	if err != nil {
		return nil, fmt.Errorf("opening transcript file: %w", err)
	}
	defer f.Close()

	var loaded []TurnRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var r TurnRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue // skip corrupted lines silently, matching Python behavior
		}
		loaded = append(loaded, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading transcript file: %w", err)
	}

	tm.records = loaded
	tm.written = len(loaded)
	return loaded, nil
}

// Append appends a single record and writes all unwritten records to disk.
func (tm *TranscriptManager) Append(record TurnRecord) error {
	tm.records = append(tm.records, record)

	// Ensure the parent directory exists.
	if err := os.MkdirAll(filepath.Dir(tm.path), 0o755); err != nil {
		return fmt.Errorf("creating transcript directory: %w", err)
	}

	f, err := os.OpenFile(tm.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening transcript file for append: %w", err)
	}
	defer f.Close()

	for i := tm.written; i < len(tm.records); i++ {
		data, err := json.Marshal(tm.records[i])
		if err != nil {
			return fmt.Errorf("marshaling turn record: %w", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("writing turn record: %w", err)
		}
	}
	tm.written = len(tm.records)
	return nil
}

// WriteAll rewrites the entire transcript file from memory.
func (tm *TranscriptManager) WriteAll() error {
	if err := os.MkdirAll(filepath.Dir(tm.path), 0o755); err != nil {
		return fmt.Errorf("creating transcript directory: %w", err)
	}

	f, err := os.Create(tm.path)
	if err != nil {
		return fmt.Errorf("creating transcript file: %w", err)
	}
	defer f.Close()

	for _, r := range tm.records {
		data, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("marshaling turn record: %w", err)
		}
		if _, err := f.Write(append(data, '\n')); err != nil {
			return fmt.Errorf("writing turn record: %w", err)
		}
	}
	tm.written = len(tm.records)
	return nil
}

// HistoryForAgent builds the history envelope for the next agent turn.
//
// For star and mesh topologies, returns the last K messages from any agent.
// For ring topology, returns the last K messages from the predecessor agent only.
// At turn 0, the predecessor is "orchestrator".
// Otherwise, the predecessor is determined by (turn-1) % numAgents index
// using the agent order inferred from the first non-orchestrator turns.
func (tm *TranscriptManager) HistoryForAgent(agentID string, window int, topology Topology, numAgents int, turn int) []map[string]string {
	switch topology {
	case TopologyStar, TopologyMesh:
		// Star and mesh: last K messages from any agent.
		start := len(tm.records) - window
		if start < 0 {
			start = 0
		}
		var history []map[string]string
		for _, r := range tm.records[start:] {
			history = append(history, map[string]string{
				"agent_id": r.AgentID,
				"content":  r.Content,
			})
		}
		return history

	default:
		// Ring: last K messages from the predecessor agent.
		var predecessorID string
		if turn == 0 {
			predecessorID = "orchestrator"
		} else {
			predecessorIdx := (turn - 1) % numAgents
			agentOrder := tm.inferAgentOrder(numAgents)
			if predecessorIdx < len(agentOrder) {
				predecessorID = agentOrder[predecessorIdx]
			} else {
				// Fallback: not enough agents inferred yet, return empty.
				return nil
			}
		}

		var history []map[string]string
		for i := len(tm.records) - 1; i >= 0; i-- {
			if tm.records[i].AgentID == predecessorID {
				history = append(history, map[string]string{
					"agent_id": tm.records[i].AgentID,
					"content":  tm.records[i].Content,
				})
			}
			if len(history) >= window {
				break
			}
		}
		// Reverse to maintain chronological order.
		for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
			history[i], history[j] = history[j], history[i]
		}
		return history
	}
}

// inferAgentOrder infers the agent participation order from the first N
// non-orchestrator turns in the transcript.
func (tm *TranscriptManager) inferAgentOrder(numAgents int) []string {
	var seen []string
	for _, r := range tm.records {
		if r.AgentID == "orchestrator" {
			continue
		}
		found := false
		for _, s := range seen {
			if s == r.AgentID {
				found = true
				break
			}
		}
		if !found {
			seen = append(seen, r.AgentID)
		}
		if len(seen) >= numAgents {
			break
		}
	}
	return seen
}

// ConsecutiveConsensusCount counts how many consecutive trailing records
// have consensus=true.
func (tm *TranscriptManager) ConsecutiveConsensusCount() int {
	count := 0
	for i := len(tm.records) - 1; i >= 0; i-- {
		if tm.records[i].Consensus {
			count++
		} else {
			break
		}
	}
	return count
}

// TotalCost returns the sum of all non-nil cost values across records.
func (tm *TranscriptManager) TotalCost() float64 {
	var total float64
	for _, r := range tm.records {
		if r.Cost != nil {
			total += *r.Cost
		}
	}
	return total
}

// TotalTokens returns the sum of all non-nil total token counts across records.
func (tm *TranscriptManager) TotalTokens() int {
	total := 0
	for _, r := range tm.records {
		total += intVal(r.Tokens.Total)
	}
	return total
}
