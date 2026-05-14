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
