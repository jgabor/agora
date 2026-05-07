// Package transcript manages the deliberation transcript as a JSONL file.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/jgabor/agora/internal/types"
)

// TranscriptManager manages the deliberation transcript as a JSONL file.
type TranscriptManager struct {
	path     string
	metadata *types.TranscriptMetadata
	records  []types.TurnRecord
	written  int
}

// NewTranscriptManager creates a new TranscriptManager for the given file path.
func NewTranscriptManager(path string) *TranscriptManager {
	return &TranscriptManager{path: path}
}

// Records returns the in-memory slice of all turn records.
func (tm *TranscriptManager) Records() []types.TurnRecord {
	return tm.records
}

// SetMetadata stores the run setup metadata written with the first record.
func (tm *TranscriptManager) SetMetadata(metadata *types.TranscriptMetadata) {
	tm.metadata = metadata
}

// Metadata returns the transcript metadata loaded or assigned for this file.
func (tm *TranscriptManager) Metadata() *types.TranscriptMetadata {
	return tm.metadata
}

// LoadExisting loads an existing JSONL transcript file into memory.
func (tm *TranscriptManager) LoadExisting() ([]types.TurnRecord, error) {
	if _, err := os.Stat(tm.path); os.IsNotExist(err) {
		return nil, nil
	}

	loaded, err := LoadFileStrict(tm.path)
	if err != nil {
		return nil, err
	}
	tm.records = loaded
	tm.written = len(loaded)
	tm.metadata = metadataFromRecords(loaded)
	return loaded, nil
}

// LoadFileStrict loads a JSONL transcript and rejects malformed non-blank records.
func LoadFileStrict(path string) ([]types.TurnRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening transcript file: %w", err)
	}
	defer func() { _ = f.Close() }()

	var loaded []types.TurnRecord
	scanner := bufio.NewScanner(f)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		var r types.TurnRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			return nil, fmt.Errorf("malformed transcript record %s:%d: %w", path, lineNumber, err)
		}
		loaded = append(loaded, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading transcript file: %w", err)
	}
	return loaded, nil
}

// Append appends a single record and writes all unwritten records to disk.
func (tm *TranscriptManager) Append(record types.TurnRecord) error {
	if len(tm.records) == 0 && tm.metadata != nil {
		record.Transcript = tm.metadata
	}
	tm.records = append(tm.records, record)

	if err := os.MkdirAll(filepath.Dir(tm.path), 0o755); err != nil {
		return fmt.Errorf("creating transcript directory: %w", err)
	}

	f, err := os.OpenFile(tm.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("opening transcript file for append: %w", err)
	}
	defer func() { _ = f.Close() }()

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
	if len(tm.records) > 0 && tm.metadata != nil {
		tm.records[0].Transcript = tm.metadata
	}
	if err := os.MkdirAll(filepath.Dir(tm.path), 0o755); err != nil {
		return fmt.Errorf("creating transcript directory: %w", err)
	}

	f, err := os.Create(tm.path)
	if err != nil {
		return fmt.Errorf("creating transcript file: %w", err)
	}
	defer func() { _ = f.Close() }()

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

func metadataFromRecords(records []types.TurnRecord) *types.TranscriptMetadata {
	for _, record := range records {
		if record.Transcript != nil {
			return record.Transcript
		}
	}
	return nil
}

// HistoryForAgent builds the history envelope for the next agent turn.
func (tm *TranscriptManager) HistoryForAgent(agentID string, window int, topology types.Topology, numAgents int, turn int) []map[string]string {
	switch topology {
	case types.TopologyStar, types.TopologyMesh:
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
		var predecessorID string
		if turn == 0 {
			predecessorID = "orchestrator"
		} else {
			predecessorIdx := (turn - 1) % numAgents
			agentOrder := tm.inferAgentOrder(numAgents)
			if predecessorIdx < len(agentOrder) {
				predecessorID = agentOrder[predecessorIdx]
			} else {
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
		for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
			history[i], history[j] = history[j], history[i]
		}
		return history
	}
}

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

// ConsecutiveConsensusCount counts consecutive trailing records with consensus=true.
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

// TotalCost returns the sum of all cost values across records.
func (tm *TranscriptManager) TotalCost() float64 {
	var total float64
	for _, r := range tm.records {
		if r.Cost != nil {
			total += *r.Cost
		}
	}
	return total
}

// TotalTokens returns the sum of all total token counts across records.
func (tm *TranscriptManager) TotalTokens() int {
	total := 0
	for _, r := range tm.records {
		total += types.IntVal(r.Tokens.Total)
	}
	return total
}
