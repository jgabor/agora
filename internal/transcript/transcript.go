// Package transcript manages the deliberation transcript as a JSONL file.
package transcript

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/jgabor/agora/internal/types"
)

// TranscriptManager manages the deliberation transcript as a JSONL file.
type TranscriptManager struct {
	path     string
	metadata *types.TranscriptMetadata
	records  []types.TurnRecord
	written  int
	protocol ProtocolInfo
}

// ProtocolInfo classifies a loaded transcript without treating legacy
// free-text consensus fields as typed protocol state.
type ProtocolInfo struct {
	Version      string
	Legacy       bool
	MigratedFrom string
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

// Protocol returns the protocol classification from the last successful load.
func (tm *TranscriptManager) Protocol() ProtocolInfo {
	return tm.protocol
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
	tm.protocol, err = ProtocolFromRecords(loaded)
	if err != nil {
		return nil, err
	}
	return loaded, nil
}

// ProtocolFromRecords classifies transcripts without a typed control state as
// legacy. Legacy consensus fields remain readable but do not establish typed
// consensus. Typed control snapshots are validated in order. Typed v1
// snapshots are explicitly migrated to v2 before validation; the migration
// trusts only persisted evidence references and downgrades claims whose old
// source references cannot be proven.
func ProtocolFromRecords(records []types.TurnRecord) (ProtocolInfo, error) {
	version, typed, err := typedProtocolVersion(records)
	if err != nil {
		return ProtocolInfo{}, err
	}
	if !typed {
		return ProtocolInfo{Legacy: true}, nil
	}

	persistedEvidence, err := EvidenceFromRecords(records)
	if err != nil {
		return ProtocolInfo{}, err
	}
	migratedFrom := ""
	if version == types.LegacyDeliberationProtocolVersion {
		migrateLegacyControls(records, persistedEvidence)
		migratedFrom = version
		version = types.DeliberationProtocolVersion
	} else if version != types.DeliberationProtocolVersion {
		return ProtocolInfo{}, fmt.Errorf("unsupported deliberation protocol version %q", version)
	}
	if err := validatePersistedSourceAuthority(records, persistedEvidence); err != nil {
		return ProtocolInfo{}, err
	}

	var previous *types.DeliberationControlState
	for i := range records {
		control := records[i].Control
		if control == nil {
			continue
		}
		if previous == nil {
			if err := control.Validate(); err != nil {
				return ProtocolInfo{}, fmt.Errorf("invalid control state at record %d: %w", i, err)
			}
		} else if err := types.ValidateDeliberationTransition(previous, control); err != nil {
			return ProtocolInfo{}, fmt.Errorf("invalid control state at record %d: %w", i, err)
		}
		previous = control
	}
	return ProtocolInfo{Version: version, MigratedFrom: migratedFrom}, nil
}

// EvidenceFromRecords returns the persisted, references-only evidence bundle
// used by a typed transcript. Context documents are intentionally excluded so
// resume and verification turns cannot rehydrate source content from disk.
// Multiple evidence records must agree on their source references.
func EvidenceFromRecords(records []types.TurnRecord) (*types.EvidenceBundle, error) {
	var result *types.EvidenceBundle
	for i, record := range records {
		if record.Evidence == nil {
			continue
		}
		references := append([]types.SourceReference{}, record.Evidence.SourceReferences...)
		if result != nil && !reflect.DeepEqual(result.SourceReferences, references) {
			return nil, fmt.Errorf("persisted evidence source references disagree at record %d", i)
		}
		if result == nil {
			result = &types.EvidenceBundle{
				Summary:          record.Evidence.Summary,
				SourceReferences: references,
			}
		}
	}
	return result, nil
}

func typedProtocolVersion(records []types.TurnRecord) (string, bool, error) {
	version := ""
	for _, record := range records {
		if record.Control == nil {
			continue
		}
		if version == "" {
			version = record.Control.ProtocolVersion
			continue
		}
		if record.Control.ProtocolVersion != version {
			return "", false, fmt.Errorf("mixed typed deliberation protocol versions %q and %q", version, record.Control.ProtocolVersion)
		}
	}
	return version, version != "", nil
}

func migrateLegacyControls(records []types.TurnRecord, evidence *types.EvidenceBundle) {
	sourceCount := 0
	if evidence != nil {
		sourceCount = len(evidence.SourceReferences)
	}
	for _, record := range records {
		if record.Control == nil {
			continue
		}
		control := record.Control
		control.ProtocolVersion = types.DeliberationProtocolVersion
		control.SourceReferenceCount = sourceCount
		for i := range control.Claims {
			claim := &control.Claims[i]
			if !sourceRefsWithinBound(claim.SourceRefs, sourceCount) {
				claim.SourceRefs = []int{}
				if claim.Status == types.EvidenceVerified || claim.Status == types.EvidenceConflicting {
					claim.Status = types.EvidenceUnverified
				}
			}
			if (claim.Status == types.EvidenceVerified || claim.Status == types.EvidenceConflicting) && len(claim.SourceRefs) == 0 {
				claim.Status = types.EvidenceUnverified
			}
		}
	}
}

func validatePersistedSourceAuthority(records []types.TurnRecord, evidence *types.EvidenceBundle) error {
	sourceCount := 0
	if evidence != nil {
		sourceCount = len(evidence.SourceReferences)
	}
	for i, record := range records {
		if record.Control == nil {
			continue
		}
		if record.Control.SourceReferenceCount != sourceCount {
			return fmt.Errorf("typed control state at record %d declares %d source references but persisted evidence supplies %d", i, record.Control.SourceReferenceCount, sourceCount)
		}
	}
	return nil
}

func sourceRefsWithinBound(refs []int, sourceCount int) bool {
	seen := make(map[int]bool, len(refs))
	for _, ref := range refs {
		if ref < 0 || ref >= sourceCount || seen[ref] {
			return false
		}
		seen[ref] = true
	}
	return true
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
	if _, err := ProtocolFromRecords(loaded); err != nil {
		return nil, fmt.Errorf("malformed transcript protocol %s: %w", path, err)
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
	if _, err := ProtocolFromRecords(loaded); err != nil {
		return nil, fmt.Errorf("malformed transcript protocol %s: %w", path, err)
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
