package transcript

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jgabor/agora/internal/types"
)

// newTestTranscript creates a new TranscriptManager backed by a temp file.
func newTestTranscript(t *testing.T) *TranscriptManager {
	t.Helper()
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	return NewTranscriptManager(path)
}

// helper to create a types.TurnRecord with minimal fields for testing.
func mkRecord(turn int, agentID, content string, consensus bool, consensusStmt string) types.TurnRecord {
	model := "test-model"
	return types.TurnRecord{
		Turn:               turn,
		AgentID:            agentID,
		Model:              &model,
		Timestamp:          float64(time.Now().Unix()),
		Content:            content,
		Consensus:          consensus,
		ConsensusStatement: consensusStmt,
		Elapsed:            1.0,
	}
}

// mkRecordWithCost creates a types.TurnRecord with cost and tokens.
func mkRecordWithCost(turn int, agentID, content string, cost float64, tokens int) types.TurnRecord {
	model := "test-model"
	return types.TurnRecord{
		Turn:      turn,
		AgentID:   agentID,
		Model:     &model,
		Timestamp: float64(time.Now().Unix()),
		Content:   content,
		Tokens:    types.TokenUsage{Total: &tokens},
		Cost:      &cost,
		Elapsed:   1.0,
	}
}

// ---------------------------------------------------------------------------
// Append and load cycle
// ---------------------------------------------------------------------------

func TestTranscriptAppendAndLoad(t *testing.T) {
	tm := newTestTranscript(t)
	tm.SetMetadata(types.NewTranscriptMetadata(&types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "agent1", Model: "test-model"}}}))

	record := mkRecord(0, "agent1", "hello", false, "")
	if err := tm.Append(record); err != nil {
		t.Fatalf("append: %v", err)
	}

	if len(tm.Records()) != 1 {
		t.Fatalf("records: got %d, want 1", len(tm.Records()))
	}
	if tm.Records()[0].AgentID != "agent1" {
		t.Errorf("agent_id: got %q, want %q", tm.Records()[0].AgentID, "agent1")
	}

	// Load from the same path.
	tm2 := NewTranscriptManager(tm.path)
	loaded, err := tm2.LoadExisting()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("loaded: got %d, want 1", len(loaded))
	}
	if loaded[0].AgentID != "agent1" {
		t.Errorf("loaded agent_id: got %q, want %q", loaded[0].AgentID, "agent1")
	}
	if tm2.Metadata() == nil || len(tm2.Metadata().Cast) != 1 {
		t.Fatalf("loaded metadata: %#v, want one cast member", tm2.Metadata())
	}
	member := tm2.Metadata().Cast[0]
	if member.ID != 1 || member.Name != "Solon" || member.Persona != "agent1" || member.ProviderModel != "test-model" || member.Color != "6" {
		t.Fatalf("cast member: %#v", member)
	}
}

func TestTranscriptAppendWritesMetadataOnFirstRecordOnly(t *testing.T) {
	tm := newTestTranscript(t)
	tm.SetMetadata(types.NewTranscriptMetadata(&types.DeliberationConfig{Agents: []types.AgentConfig{
		{ID: "alpha", Model: "openai/gpt-5.5"},
		{ID: "beta", Model: "anthropic/claude"},
	}}))

	if err := tm.Append(mkRecord(0, "alpha", "hello", false, "")); err != nil {
		t.Fatalf("append first: %v", err)
	}
	if err := tm.Append(mkRecord(1, "beta", "reply", false, "")); err != nil {
		t.Fatalf("append second: %v", err)
	}

	loaded, err := LoadFileStrict(tm.path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if loaded[0].Transcript == nil || len(loaded[0].Transcript.Cast) != 2 || loaded[0].Transcript.Config == nil {
		t.Fatalf("first record metadata: %#v", loaded[0].Transcript)
	}
	if loaded[0].Transcript.Cast[1].ID != 2 || loaded[0].Transcript.Cast[1].Name != "Aspasia" || loaded[0].Transcript.Cast[1].Persona != "beta" {
		t.Fatalf("second cast member: %#v", loaded[0].Transcript.Cast[1])
	}
	if loaded[1].Transcript != nil {
		t.Fatalf("second record should not duplicate transcript metadata: %#v", loaded[1].Transcript)
	}
}

func TestTranscriptMultipleAppends(t *testing.T) {
	tm := newTestTranscript(t)

	// Write orchestrator seed then agent turns.
	if err := tm.Append(mkRecord(-1, "orchestrator", "seed", false, "")); err != nil {
		t.Fatalf("append seed: %v", err)
	}
	if err := tm.Append(mkRecord(0, "a", "turn 0", false, "")); err != nil {
		t.Fatalf("append turn 0: %v", err)
	}
	if err := tm.Append(mkRecord(1, "b", "turn 1", true, "agreed")); err != nil {
		t.Fatalf("append turn 1: %v", err)
	}

	if len(tm.Records()) != 3 {
		t.Fatalf("records: got %d, want 3", len(tm.Records()))
	}

	// Reload and verify.
	tm2 := NewTranscriptManager(tm.path)
	loaded, err := tm2.LoadExisting()
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded: got %d, want 3", len(loaded))
	}
	if loaded[2].AgentID != "b" {
		t.Errorf("last agent: got %q, want %q", loaded[2].AgentID, "b")
	}
}

func TestTranscriptLoadNonexistent(t *testing.T) {
	tm := NewTranscriptManager("/nonexistent/path/transcript.jsonl")
	records, err := tm.LoadExisting()
	if err != nil {
		t.Fatalf("load nonexistent should not error: %v", err)
	}
	if records != nil {
		t.Errorf("expected nil records for nonexistent file, got %d", len(records))
	}
}

func TestLoadFileStrictRejectsMalformedNonBlankRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	content := `{"turn":0,"agent_id":"a","timestamp":1,"content":"ok","tokens":{},"consensus":false,"consensus_statement":"","elapsed":0}` + "\n\nnot-json\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	_, err := LoadFileStrict(path)
	if err == nil || !strings.Contains(err.Error(), "malformed transcript record") || !strings.Contains(err.Error(), ":3:") {
		t.Fatalf("error: got %v, want malformed transcript record at line 3", err)
	}
}

// ---------------------------------------------------------------------------
// HistoryForAgent — ring topology (predecessor-only)
// ---------------------------------------------------------------------------

func TestHistoryRingTopology(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "orchestrator", "seed", false, ""),
		mkRecord(0, "a", "msg from a", false, ""),
		mkRecord(1, "b", "msg from b", false, ""),
	}

	history := HistoryForAgent(records, "a", 5, types.TopologyRing, 2, 0)
	if len(history) != 1 {
		t.Fatalf("turn 0 history: got %d, want 1", len(history))
	}
	if history[0]["agent_id"] != "orchestrator" {
		t.Errorf("turn 0: expected orchestrator, got %q", history[0]["agent_id"])
	}

	history = HistoryForAgent(records, "b", 5, types.TopologyRing, 2, 1)
	if len(history) != 1 {
		t.Fatalf("turn 1 history: got %d, want 1", len(history))
	}
	if history[0]["agent_id"] != "a" {
		t.Errorf("turn 1: expected a, got %q", history[0]["agent_id"])
	}

	history = HistoryForAgent(records, "a", 5, types.TopologyRing, 2, 2)
	if len(history) != 1 {
		t.Fatalf("turn 2 history: got %d, want 1", len(history))
	}
	if history[0]["agent_id"] != "b" {
		t.Errorf("turn 2: expected b, got %q", history[0]["agent_id"])
	}
}

func TestHistoryRingTopologyWindow(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "orchestrator", "seed", false, ""),
		mkRecord(0, "a", "a-0", false, ""),
		mkRecord(1, "b", "b-0", false, ""),
		mkRecord(2, "c", "c-0", false, ""),
		mkRecord(3, "a", "a-1", false, ""),
		mkRecord(4, "b", "b-1", false, ""),
		mkRecord(5, "c", "c-1", false, ""),
	}

	history := HistoryForAgent(records, "a", 2, types.TopologyRing, 3, 6)
	if len(history) != 2 {
		t.Fatalf("history len: got %d, want 2", len(history))
	}
	if history[0]["agent_id"] != "c" || history[0]["content"] != "c-0" {
		t.Errorf("first: got %q=%q, want c/c-0", history[0]["agent_id"], history[0]["content"])
	}
	if history[1]["agent_id"] != "c" || history[1]["content"] != "c-1" {
		t.Errorf("second: got %q=%q, want c/c-1", history[1]["agent_id"], history[1]["content"])
	}
}

func TestHistoryStarTopology(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "orchestrator", "seed", false, ""),
		mkRecord(0, "a", "msg a", false, ""),
		mkRecord(1, "b", "msg b", false, ""),
	}

	history := HistoryForAgent(records, "c", 3, types.TopologyStar, 2, 2)
	if len(history) != 3 {
		t.Fatalf("star history: got %d, want 3", len(history))
	}
	agents := map[string]bool{}
	for _, h := range history {
		agents[h["agent_id"]] = true
	}
	if !agents["orchestrator"] || !agents["a"] || !agents["b"] {
		t.Errorf("star history missing agents: %v", agents)
	}
}

func TestHistoryMeshTopology(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "orchestrator", "seed", false, ""),
		mkRecord(0, "x", "x-msg", false, ""),
		mkRecord(1, "y", "y-msg", false, ""),
	}

	history := HistoryForAgent(records, "z", 2, types.TopologyMesh, 2, 2)
	if len(history) != 2 {
		t.Fatalf("mesh history: got %d, want 2", len(history))
	}
}

func TestHistoryWindowLargerThanRecords(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "orchestrator", "seed", false, ""),
		mkRecord(0, "a", "msg a", false, ""),
	}

	history := HistoryForAgent(records, "b", 10, types.TopologyStar, 2, 1)
	if len(history) != 2 {
		t.Errorf("window overflow: got %d, want 2", len(history))
	}
}

func TestHistoryRingEmptyTurn0(t *testing.T) {
	var records []types.TurnRecord

	history := HistoryForAgent(records, "a", 5, types.TopologyRing, 2, 0)
	if len(history) != 0 {
		t.Errorf("empty history for no records: got %d, want 0", len(history))
	}
}

func TestConsecutiveConsensusCount(t *testing.T) {
	var records []types.TurnRecord
	if n := ConsecutiveConsensusCount(records); n != 0 {
		t.Errorf("empty: got %d, want 0", n)
	}

	records = append(records, mkRecord(0, "a", "x", true, "ok"))
	records = append(records, mkRecord(1, "b", "x", true, "ok"))
	if n := ConsecutiveConsensusCount(records); n != 2 {
		t.Errorf("two cons: got %d, want 2", n)
	}

	records = append(records, mkRecord(2, "c", "x", false, ""))
	if n := ConsecutiveConsensusCount(records); n != 0 {
		t.Errorf("after non-cons: got %d, want 0", n)
	}

	records = append(records, mkRecord(3, "a", "x", true, "ok"))
	if n := ConsecutiveConsensusCount(records); n != 1 {
		t.Errorf("single cons: got %d, want 1", n)
	}
}

func TestTotalCost(t *testing.T) {
	records := []types.TurnRecord{
		mkRecordWithCost(0, "a", "x", 0.001, 100),
		mkRecordWithCost(1, "b", "x", 0.002, 200),
		mkRecord(-1, "orchestrator", "seed", false, ""),
	}

	if c := TotalCost(records); c != 0.003 {
		t.Errorf("total cost: got %f, want 0.003", c)
	}
	if tok := TotalTokens(records); tok != 300 {
		t.Errorf("total tokens: got %d, want 300", tok)
	}
}

func TestTotalTokensWithNil(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(0, "a", "x", false, ""),
	}

	if tok := TotalTokens(records); tok != 0 {
		t.Errorf("total tokens with nil: got %d, want 0", tok)
	}
}

// ---------------------------------------------------------------------------
// Python JSONL compatibility — load a Python-produced transcript
// ---------------------------------------------------------------------------

func TestLoadPythonProducedJSONL(t *testing.T) {
	// participants.jsonl is a Python-produced transcript in the project root.
	paths := []string{
		filepath.Join("..", "participants.jsonl"),
		"participants.jsonl",
		filepath.Join("..", "..", "participants.jsonl"),
	}
	found := false
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			found = true
			tm := NewTranscriptManager(p)
			records, err := tm.LoadExisting()
			if err != nil {
				t.Fatalf("load python JSONL %q: %v", p, err)
			}
			if len(records) == 0 {
				t.Fatalf("expected records in python JSONL %q", p)
			}
			t.Logf("loaded %d records from Python-produced %q", len(records), p)

			// Verify key fields are populated correctly.
			first := records[0]
			if first.AgentID != "orchestrator" {
				t.Errorf("first agent_id: got %q, want orchestrator", first.AgentID)
			}
			if first.Turn != -1 {
				t.Errorf("first turn: got %d, want -1", first.Turn)
			}

			// Verify a non-orchestrator record has model populated.
			for _, r := range records {
				if r.AgentID != "orchestrator" && r.Model != nil {
					t.Logf("model field present: %s -> %s", r.AgentID, *r.Model)
					break
				}
			}

			break
		}
	}
	if !found {
		t.Skip("participants.jsonl not found")
	}
}

// ---------------------------------------------------------------------------
// JSON marshal parity — Go-encoded record must be parseable as Python-like JSON
// ---------------------------------------------------------------------------

func TestGoMarshaledRecordJSONKeysForPython(t *testing.T) {
	model := "openai/gpt-4"
	total := 100
	cost := 0.001
	record := types.TurnRecord{
		Turn:               0,
		AgentID:            "test_agent",
		Model:              &model,
		Timestamp:          1.0,
		Content:            "hello",
		Tokens:             types.TokenUsage{Total: &total},
		Cost:               &cost,
		Consensus:          true,
		ConsensusStatement: "agreed",
		Elapsed:            2.5,
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Python expects these snake_case keys.
	if _, ok := m["agent_id"]; !ok {
		t.Error("missing agent_id")
	}
	if _, ok := m["consensus_statement"]; !ok {
		t.Error("missing consensus_statement")
	}
	// Verify tokens is an object, not a scalar.
	if tokens, ok := m["tokens"].(map[string]any); !ok {
		t.Error("tokens is not an object")
	} else {
		if _, ok := tokens["total"]; !ok {
			t.Error("missing tokens.total")
		}
	}
}

// ---------------------------------------------------------------------------
// WriteAll
// ---------------------------------------------------------------------------

func TestTranscriptWriteAll(t *testing.T) {
	tm := newTestTranscript(t)

	_ = tm.Append(mkRecord(-1, "orchestrator", "seed", false, ""))
	_ = tm.Append(mkRecord(0, "a", "msg", false, ""))

	// WriteAll should succeed.
	if err := tm.WriteAll(); err != nil {
		t.Fatalf("WriteAll: %v", err)
	}

	// Should be reloadable from disk.
	tm2 := NewTranscriptManager(tm.path)
	loaded, err := tm2.LoadExisting()
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if len(loaded) != 2 {
		t.Errorf("reload: got %d, want 2", len(loaded))
	}
}
