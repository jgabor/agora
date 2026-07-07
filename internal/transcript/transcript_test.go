package transcript

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jgabor/agora/internal/cast"
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
	cfg := &types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "agent1", Model: "test-model"}}}
	tm.SetMetadata(types.NewTranscriptMetadata(cfg, cast.New(cfg.Agents).Members()))

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
	cfg := &types.DeliberationConfig{Agents: []types.AgentConfig{
		{ID: "alpha", Model: "openai/gpt-5.5"},
		{ID: "beta", Model: "anthropic/claude"},
	}}
	tm.SetMetadata(types.NewTranscriptMetadata(cfg, cast.New(cfg.Agents).Members()))

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

	// Write moderator seed then agent turns.
	if err := tm.Append(mkRecord(-1, "moderator", "seed", false, "")); err != nil {
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
		mkRecord(-1, "moderator", "seed", false, ""),
		mkRecord(0, "a", "msg from a", false, ""),
		mkRecord(1, "b", "msg from b", false, ""),
	}

	history := HistoryForAgent(records, "a", 5, types.TopologyRing, 2, 0)
	if len(history) != 1 {
		t.Fatalf("turn 0 history: got %d, want 1", len(history))
	}
	if history[0]["agent_id"] != "moderator" {
		t.Errorf("turn 0: expected moderator, got %q", history[0]["agent_id"])
	}

	history = HistoryForAgent(records, "b", 5, types.TopologyRing, 2, 1)
	if len(history) != 1 {
		t.Fatalf("turn 1 history: got %d, want 1", len(history))
	}
	if history[0]["agent_id"] != "a" {
		t.Errorf("turn 1: expected a, got %q", history[0]["agent_id"])
	}

	history = HistoryForAgent(records, "a", 5, types.TopologyRing, 2, 2)
	if len(history) != 2 {
		t.Fatalf("turn 2 history: got %d, want 2 (predecessor + self-history)", len(history))
	}
	if history[0]["agent_id"] != "b" {
		t.Errorf("turn 2 first: expected predecessor b, got %q", history[0]["agent_id"])
	}
	if history[1]["agent_id"] != "a" || history[1]["content"] != "msg from a" {
		t.Errorf("turn 2 self-history: got %q=%q, want a/msg from a", history[1]["agent_id"], history[1]["content"])
	}
}

func TestHistoryRingTopologyWindow(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "moderator", "seed", false, ""),
		mkRecord(0, "a", "a-0", false, ""),
		mkRecord(1, "b", "b-0", false, ""),
		mkRecord(2, "c", "c-0", false, ""),
		mkRecord(3, "a", "a-1", false, ""),
		mkRecord(4, "b", "b-1", false, ""),
		mkRecord(5, "c", "c-1", false, ""),
	}

	history := HistoryForAgent(records, "a", 2, types.TopologyRing, 3, 6)
	if len(history) != 3 {
		t.Fatalf("history len: got %d, want 3 (2 predecessor + 1 self-history)", len(history))
	}
	if history[0]["agent_id"] != "c" || history[0]["content"] != "c-0" {
		t.Errorf("first: got %q=%q, want c/c-0", history[0]["agent_id"], history[0]["content"])
	}
	if history[1]["agent_id"] != "c" || history[1]["content"] != "c-1" {
		t.Errorf("second: got %q=%q, want c/c-1", history[1]["agent_id"], history[1]["content"])
	}
	if history[2]["agent_id"] != "a" || history[2]["content"] != "a-1" {
		t.Errorf("self-history: got %q=%q, want a/a-1", history[2]["agent_id"], history[2]["content"])
	}
}

func TestHistoryStarTopology(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "moderator", "seed", false, ""),
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
	if !agents["moderator"] || !agents["a"] || !agents["b"] {
		t.Errorf("star history missing agents: %v", agents)
	}
}

func TestHistoryMeshTopology(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "moderator", "seed", false, ""),
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
		mkRecord(-1, "moderator", "seed", false, ""),
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

func TestSelfHistoryAppendedAcrossAllTopologies(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "moderator", "seed", false, ""),
		mkRecord(0, "analyst", "first position", false, ""),
		mkRecord(1, "critic", "reply", false, ""),
		mkRecord(2, "analyst", "revised position", false, ""),
		mkRecord(3, "critic", "second reply", false, ""),
	}

	for _, tc := range []struct {
		name      string
		topology  types.Topology
		numAgents int
		turn      int
	}{
		{"ring", types.TopologyRing, 2, 4},
		{"star", types.TopologyStar, 2, 4},
		{"mesh", types.TopologyMesh, 2, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history := HistoryForAgent(records, "analyst", 2, tc.topology, tc.numAgents, tc.turn)
			var ownEntry map[string]string
			for _, h := range history {
				if h["agent_id"] == "analyst" {
					ownEntry = h
					break
				}
			}
			if ownEntry == nil {
				t.Fatalf("analyst own turn missing from %s history: %#v", tc.name, history)
			}
			if ownEntry["content"] != "revised position" {
				t.Errorf("own turn content: got %q, want %q (most recent only)", ownEntry["content"], "revised position")
			}
		})
	}
}

func TestSelfHistoryDeduplicatedAgainstPredecessorWindow(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "moderator", "seed", false, ""),
		mkRecord(0, "a", "a-0", false, ""),
		mkRecord(1, "b", "b-0", false, ""),
		mkRecord(2, "a", "a-1", false, ""),
		mkRecord(3, "b", "b-1", false, ""),
	}

	history := HistoryForAgent(records, "a", 10, types.TopologyStar, 2, 4)
	ownCount := 0
	for _, h := range history {
		if h["agent_id"] == "a" && h["content"] == "a-1" {
			ownCount++
		}
	}
	if ownCount != 1 {
		t.Fatalf("self-history dedup: got %d own-turn entries, want 1 (history: %#v)", ownCount, history)
	}
}

func TestSelfHistoryMostRecentOnly(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "moderator", "seed", false, ""),
		mkRecord(0, "a", "oldest own turn", false, ""),
		mkRecord(1, "b", "b-0", false, ""),
		mkRecord(2, "a", "middle own turn", false, ""),
		mkRecord(3, "b", "b-1", false, ""),
		mkRecord(4, "a", "most recent own turn", false, ""),
		mkRecord(5, "b", "b-2", false, ""),
	}

	history := HistoryForAgent(records, "a", 5, types.TopologyRing, 2, 6)
	ownCount := 0
	ownContent := ""
	for _, h := range history {
		if h["agent_id"] == "a" {
			ownCount++
			ownContent = h["content"]
		}
	}
	if ownCount != 1 {
		t.Fatalf("self-history count: got %d own-turn entries, want 1 (history: %#v)", ownCount, history)
	}
	if ownContent != "most recent own turn" {
		t.Errorf("self-history content: got %q, want %q", ownContent, "most recent own turn")
	}
}

func TestSelfHistoryAbsentWhenNoPriorOwnTurn(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-1, "moderator", "seed", false, ""),
		mkRecord(0, "a", "first turn", false, ""),
	}

	history := HistoryForAgent(records, "b", 5, types.TopologyRing, 2, 1)
	for _, h := range history {
		if h["agent_id"] == "b" {
			t.Fatalf("unexpected self-history for b with no prior turns: %#v", history)
		}
	}
}

func TestConsecutiveAgentConsensusCount(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(0, "a", "x", true, "ok"),
		mkRecord(1, "b", "x", true, "ok"),
		mkRecord(2, "synthesizer", "{}", false, ""),
	}
	if n := ConsecutiveAgentConsensusCount(records); n != 2 {
		t.Errorf("with trailing synthesizer: got %d, want 2", n)
	}
}

func TestAgentTurnCount(t *testing.T) {
	records := []types.TurnRecord{
		mkRecord(-2, "moderator", "evidence", false, ""),
		mkRecord(-1, "moderator", "seed", false, ""),
		mkRecord(0, "a", "x", false, ""),
		mkRecord(1, "synthesizer", "{}", false, ""),
	}
	if n := AgentTurnCount(records); n != 1 {
		t.Errorf("agent turns: got %d, want 1", n)
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
		mkRecord(-1, "moderator", "seed", false, ""),
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
// Legacy transcript JSONL compatibility
// ---------------------------------------------------------------------------

func TestLoadLegacyTranscriptJSONL(t *testing.T) {
	path := filepath.Join("testdata", "legacy-deliberation.jsonl")
	tm := NewTranscriptManager(path)
	records, err := tm.LoadExisting()
	if err != nil {
		t.Fatalf("load legacy JSONL %q: %v", path, err)
	}
	if len(records) != 3 {
		t.Fatalf("record count: got %d, want 3", len(records))
	}

	first := records[0]
	if first.AgentID != "moderator" {
		t.Errorf("first agent_id: got %q, want moderator", first.AgentID)
	}
	if first.Turn != -1 {
		t.Errorf("first turn: got %d, want -1", first.Turn)
	}

	var sawModel bool
	for _, r := range records {
		if r.AgentID != "moderator" && r.Model != nil && *r.Model != "" {
			sawModel = true
			break
		}
	}
	if !sawModel {
		t.Fatal("expected at least one non-moderator record with model populated")
	}
}

// ---------------------------------------------------------------------------
// WriteAll
// ---------------------------------------------------------------------------

func TestTranscriptWriteAll(t *testing.T) {
	tm := newTestTranscript(t)

	_ = tm.Append(mkRecord(-1, "moderator", "seed", false, ""))
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

// ---------------------------------------------------------------------------
// Ledger record loading (strict + lenient)
// ---------------------------------------------------------------------------

func validLedgerRecordLine(t *testing.T, round int) string {
	t.Helper()
	l := types.NewDebateLedger(round, 1715000005.0)
	l.Positions = []types.AgentPosition{
		{AgentID: "skeptic", Text: "position", Turn: 0},
		{AgentID: "optimist", Text: "position", Turn: 1},
	}
	return marshalLine(t, types.TurnRecord{
		Turn:      types.LedgerSentinelTurn,
		AgentID:   types.LedgerAgentID,
		Timestamp: 1715000005.0,
		Ledger:    l,
	})
}

func marshalLine(t *testing.T, rec types.TurnRecord) string {
	t.Helper()
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal record: %v", err)
	}
	return string(data)
}

func TestLoadFileStrictRejectsMalformedLedgerRecord(t *testing.T) {
	cases := []struct {
		name string
		line string
	}{
		{
			name: "ledger_sentinel_missing_ledger_field",
			line: `{"turn": -3, "agent_id": "ledger", "timestamp": 1.0, "content": "", "tokens": {}, "consensus": false, "consensus_statement": "", "elapsed": 0}`,
		},
		{
			name: "ledger_sentinel_wrong_agent_id",
			line: `{"turn": -3, "agent_id": "moderator", "timestamp": 1.0, "content": "", "tokens": {}, "consensus": false, "consensus_statement": "", "elapsed": 0}`,
		},
		{
			name: "ledger_sentinel_invalid_ledger_round",
			line: `{"turn": -3, "agent_id": "ledger", "timestamp": 1.0, "content": "", "tokens": {}, "consensus": false, "consensus_statement": "", "elapsed": 0, "ledger": {"round": -1, "positions": [], "agreements": [], "cruxes": [], "draft": {"status": "none"}, "updated_at": 0}}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name+"/strict_fail", func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "transcript.jsonl")
			content := validLedgerRecordLine(t, 0) + "\n" + tc.line + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write transcript: %v", err)
			}
			_, err := LoadFileStrict(path)
			if err == nil || !strings.Contains(err.Error(), "malformed transcript record") || !strings.Contains(err.Error(), "ledger") {
				t.Fatalf("error: got %v, want malformed transcript record mentioning ledger", err)
			}
		})
	}
}

func TestLoadFileStrictLoadsValidLedgerRecordInOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	content := marshalLine(t, mkRecord(-1, "moderator", "seed", false, "")) + "\n" +
		marshalLine(t, mkRecord(0, "skeptic", "turn 0", false, "")) + "\n" +
		validLedgerRecordLine(t, 1) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	loaded, err := LoadFileStrict(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("record count: got %d, want 3", len(loaded))
	}
	if loaded[2].Turn != types.LedgerSentinelTurn || loaded[2].AgentID != types.LedgerAgentID {
		t.Fatalf("ledger record not at expected index 2: %#v", loaded[2])
	}
	if loaded[2].Ledger == nil || loaded[2].Ledger.Round != 1 {
		t.Fatalf("ledger payload: got %#v, want round=1", loaded[2].Ledger)
	}
	if loaded[1].AgentID == types.LedgerAgentID {
		t.Fatalf("agent turn should not be misclassified as ledger: %#v", loaded[1])
	}
}

func TestLoadFileLenientWarnsOnMalformedLedgerRecord(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	malformedLedger := `{"turn": -3, "agent_id": "ledger", "timestamp": 1.0, "content": "", "tokens": {}, "consensus": false, "consensus_statement": "", "elapsed": 0}`
	content := validLedgerRecordLine(t, 0) + "\n" + malformedLedger + "\n" + marshalLine(t, mkRecord(0, "skeptic", "after", false, "")) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	var warn bytes.Buffer
	loaded, err := LoadFileLenient(path, &warn)
	if err != nil {
		t.Fatalf("lenient load: got error %v, want warn-and-continue", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded records: got %d, want 2 (valid ledger + agent turn, malformed skipped)", len(loaded))
	}
	if loaded[0].AgentID != types.LedgerAgentID || loaded[1].AgentID != "skeptic" {
		t.Fatalf("loaded order: got %#v, want ledger then agent", loaded)
	}
	warning := warn.String()
	if !strings.Contains(warning, "warning") || !strings.Contains(warning, "ledger") {
		t.Fatalf("warning: got %q, want a ledger-record warning", warning)
	}
}

func TestLoadFileLenientFailsOnNonLedgerMalformed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "transcript.jsonl")
	content := marshalLine(t, mkRecord(0, "skeptic", "ok", false, "")) + "\nnot-json\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	var warn bytes.Buffer
	_, err := LoadFileLenient(path, &warn)
	if err == nil || !strings.Contains(err.Error(), "malformed transcript record") {
		t.Fatalf("error: got %v, want malformed transcript record (resume must still fail non-ledger JSON errors)", err)
	}
}

func TestLoadFileLenientLegacyTranscriptNoWarnings(t *testing.T) {
	path := filepath.Join("testdata", "legacy-deliberation.jsonl")
	var warn bytes.Buffer
	loaded, err := LoadFileLenient(path, &warn)
	if err != nil {
		t.Fatalf("lenient load legacy JSONL %q: %v", path, err)
	}
	if len(loaded) != 3 {
		t.Fatalf("record count: got %d, want 3", len(loaded))
	}
	for _, r := range loaded {
		if r.Ledger != nil || r.AgentID == types.LedgerAgentID {
			t.Fatalf("legacy transcript should contain no ledger records: %#v", r)
		}
	}
	if warn.Len() > 0 {
		t.Fatalf("legacy transcript should load without warnings: %q", warn.String())
	}
}
