// Package transcript manages the deliberation transcript as a JSONL file.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
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
// Ledger sentinel records (Turn == LedgerSentinelTurn or AgentID == LedgerAgentID)
// must carry a non-nil, valid DebateLedger, mirroring how malformed agent and
// evidence records already fail loading under the strict contract used by show.
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
		if err := validateLedgerSentinel(r); err != nil {
			return nil, fmt.Errorf("malformed transcript record %s:%d: %w", path, lineNumber, err)
		}
		loaded = append(loaded, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading transcript file: %w", err)
	}
	return loaded, nil
}

// LoadFileLenient loads a JSONL transcript for resume: malformed ledger sentinel
// records emit a warning to w and are skipped so resume continues with
// best-effort state. Records that fail JSON parsing — including non-ledger
// malformed records — still fail, matching the existing resume contract.
func LoadFileLenient(path string, w io.Writer) ([]types.TurnRecord, error) {
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
			if looksLikeLedgerRecord(line) {
				warnf(w, "warning: %s:%d: malformed ledger record: %v (skipping ledger record)\n", path, lineNumber, err)
				continue
			}
			return nil, fmt.Errorf("malformed transcript record %s:%d: %w", path, lineNumber, err)
		}
		if err := validateLedgerSentinel(r); err != nil {
			warnf(w, "warning: %s:%d: %v (skipping ledger record)\n", path, lineNumber, err)
			continue
		}
		loaded = append(loaded, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("reading transcript file: %w", err)
	}
	return loaded, nil
}

// warnf writes a formatted warning line to w, ignoring write errors. Resume
// treats ledger-record warnings as best-effort diagnostics; a failure to write
// the warning (e.g. a closed sink) must not fail the load.
func warnf(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// looksLikeLedgerRecord reports whether a JSON line advertises ledger-sentinel
// intent, used to decide warn-and-skip (resume) versus fail (strict) when the
// line itself fails to parse as a TurnRecord. It probes only the sentinel
// fields so a truncated ledger line is treated as a malformed ledger record.
func looksLikeLedgerRecord(line string) bool {
	probe := struct {
		Turn    *int   `json:"turn"`
		AgentID string `json:"agent_id"`
	}{}
	if err := json.Unmarshal([]byte(line), &probe); err != nil {
		return false
	}
	if probe.AgentID == types.LedgerAgentID {
		return true
	}
	if probe.Turn != nil && *probe.Turn == types.LedgerSentinelTurn {
		return true
	}
	return false
}

// validateLedgerSentinel reports whether a record claiming ledger-sentinel intent
// carries a valid DebateLedger payload. A record is a valid ledger record only
// when Turn == LedgerSentinelTurn AND AgentID == LedgerAgentID AND Ledger is a
// non-nil, validated DebateLedger. Any record that advertises ledger intent
// (either sentinel value present) without satisfying all three is malformed.
func validateLedgerSentinel(r types.TurnRecord) error {
	isLedgerTurn := r.Turn == types.LedgerSentinelTurn
	isLedgerAgent := r.AgentID == types.LedgerAgentID
	if !isLedgerTurn && !isLedgerAgent {
		return nil
	}
	if !isLedgerTurn || !isLedgerAgent {
		return fmt.Errorf("ledger record requires turn=%d and agent_id=%q, got turn=%d agent_id=%q",
			types.LedgerSentinelTurn, types.LedgerAgentID, r.Turn, r.AgentID)
	}
	if r.Ledger == nil {
		return fmt.Errorf("ledger record missing '%s' field", "ledger")
	}
	if err := r.Ledger.Validate(); err != nil {
		return fmt.Errorf("ledger record: %w", err)
	}
	return nil
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
