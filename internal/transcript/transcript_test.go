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

func authenticatedActiveConsensus() *types.DeliberationControlState {
	state := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
	state.Phase = types.PhaseVoting
	state.CurrentProposalVersion = 1
	state.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "proposal"}}
	state.Convergence.RequiredEndorsements = 2
	state.Convergence.MinimumRounds = 1
	state.Contributions = []types.AgentContribution{
		{AgentID: "alpha", Turn: 0, Position: "alpha position", ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone}},
		{AgentID: "beta", Turn: 1, Position: "beta position", ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone}},
	}
	state.Votes = []types.ProposalVote{
		{AgentID: "alpha", ProposalVersion: 1, Choice: types.VoteEndorse},
		{AgentID: "beta", ProposalVersion: 1, Choice: types.VoteEndorse},
	}
	return state
}

func authenticatedTerminalConsensus() *types.DeliberationControlState {
	state := authenticatedActiveConsensus()
	state.Phase = types.PhaseTerminal
	state.Convergence.CurrentEndorsements = 2
	state.Outcome = types.TerminalOutcome{Kind: types.OutcomeConsensus, ProposalVersion: 1, DissentingAgentIDs: []string{}, UnresolvedObjectionIDs: []string{}, EvidenceGapClaimIDs: []string{}}
	return state
}

func terminalConsensusFromActive(t *testing.T, active *types.DeliberationControlState) *types.DeliberationControlState {
	t.Helper()
	data, err := json.Marshal(active)
	if err != nil {
		t.Fatalf("marshal active control: %v", err)
	}
	var terminal types.DeliberationControlState
	if err := json.Unmarshal(data, &terminal); err != nil {
		t.Fatalf("unmarshal terminal control: %v", err)
	}
	terminal.Phase = types.PhaseTerminal
	terminal.Directive = types.TurnDirective{Kind: types.DirectiveNone}
	terminal.Convergence.CurrentEndorsements = len(terminal.AgentIDs)
	terminal.Outcome = types.TerminalOutcome{
		Kind:                   types.OutcomeConsensus,
		ProposalVersion:        terminal.CurrentProposalVersion,
		DissentingAgentIDs:     []string{},
		UnresolvedObjectionIDs: []string{},
		EvidenceGapClaimIDs:    []string{},
	}
	return &terminal
}

type historicalControlOptions struct {
	protocolVersion      string
	includeContributions bool
	terminal             bool
	terminalRequirements bool
}

func historicalControlLine(t *testing.T, options historicalControlOptions) string {
	t.Helper()
	proposal := "1. An agent must verify claims.\n2. An agent must preserve evidence.\n3. An agent must record dissent."
	convergence := map[string]any{
		"current_endorsements": 0, "required_endorsements": 1,
		"unresolved_objections": 0, "evidence_gaps": 0, "stagnant_rounds": 0, "ready_to_vote": false,
	}
	control := map[string]any{
		"protocol_version":         options.protocolVersion,
		"phase":                    "voting",
		"agent_ids":                []string{"alpha"},
		"source_reference_count":   0,
		"current_proposal_version": 1,
		"proposals": []any{map[string]any{
			"version": 1, "author_id": "alpha", "content": proposal, "supersedes": 0,
		}},
		"objections":   []any{},
		"dispositions": []any{},
		"votes": []any{map[string]any{
			"agent_id": "alpha", "proposal_version": 1, "choice": "endorse",
		}},
		"claims": []any{},
		"moderator_action": map[string]any{
			"kind": "none", "objection_ids": []any{}, "claim_ids": []any{},
		},
		"convergence": convergence,
		"outcome": map[string]any{
			"kind": "pending", "dissenting_agent_ids": []any{}, "unresolved_objection_ids": []any{}, "evidence_gap_claim_ids": []any{},
		},
	}
	if options.protocolVersion == types.DeliberationProtocolVersion {
		control["directive"] = map[string]any{"kind": "none"}
		control["contributions"] = []any{}
		if options.includeContributions {
			control["contributions"] = []any{map[string]any{
				"agent_id": "alpha", "turn": 0, "position": "historical position",
				"responses": []any{}, "concessions": []any{}, "proposal_action": map[string]any{"kind": "none"}, "objections": []any{}, "claims": []any{},
			}}
		}
	}
	if options.terminal {
		control["phase"] = "terminal"
		convergence["current_endorsements"] = 1
		control["outcome"] = map[string]any{
			"kind": "consensus", "proposal_version": 1, "dissenting_agent_ids": []any{}, "unresolved_objection_ids": []any{}, "evidence_gap_claim_ids": []any{},
		}
		if options.terminalRequirements {
			convergence["minimum_rounds"] = 1
			convergence["required_deliverable_items"] = 3
		}
	}
	record := map[string]any{
		"turn": 0, "agent_id": "alpha", "model": "test/model", "timestamp": 1, "content": "historical typed control", "tokens": map[string]any{},
		"consensus": false, "consensus_statement": "", "elapsed": 0, "control": control,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal historical control: %v", err)
	}
	if !options.terminalRequirements && (strings.Contains(string(data), "minimum_rounds") || strings.Contains(string(data), "required_deliverable_items")) {
		t.Fatalf("historical control unexpectedly contains current requirements: %s", data)
	}
	return string(data)
}

func normalizedHistoricalBoundaryLine(t *testing.T) string {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal([]byte(historicalControlLine(t, historicalControlOptions{
		protocolVersion: types.DeliberationProtocolVersion, includeContributions: true,
	})), &record); err != nil {
		t.Fatalf("decode historical boundary: %v", err)
	}
	control := record["control"].(map[string]any)
	convergence := control["convergence"].(map[string]any)
	convergence["run_contract_version"] = types.RunContractVersion
	convergence["minimum_rounds"] = 1
	convergence["required_deliverable_items"] = 3
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal normalized boundary: %v", err)
	}
	return string(data)
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

func TestLoadExistingIdentifiesLegacyTranscriptWithoutTypedConsensus(t *testing.T) {
	tm := newTestTranscript(t)
	legacy := mkRecord(0, "alpha", "[CONSENSUS: legacy marker]", true, "legacy marker")
	if err := os.WriteFile(tm.path, []byte(marshalLine(t, legacy)+"\n"), 0o644); err != nil {
		t.Fatalf("write legacy transcript: %v", err)
	}
	records, err := tm.LoadExisting()
	if err != nil {
		t.Fatalf("load legacy transcript: %v", err)
	}
	if !tm.Protocol().Legacy || tm.Protocol().Version != "" {
		t.Fatalf("protocol classification: %#v, want legacy", tm.Protocol())
	}
	if len(records) != 1 || !records[0].Consensus || records[0].Content == "" {
		t.Fatalf("legacy record was not preserved: %#v", records)
	}
}

func TestLoadFileStrictValidatesTypedProtocol(t *testing.T) {
	validPath := filepath.Join(t.TempDir(), "typed.jsonl")
	state := types.NewDeliberationControlState([]string{"alpha"}, 0)
	record := mkRecord(0, "alpha", "opening", false, "")
	record.Control = state
	if err := os.WriteFile(validPath, []byte(marshalLine(t, record)+"\n"), 0o644); err != nil {
		t.Fatalf("write typed transcript: %v", err)
	}
	records, err := LoadFileStrict(validPath)
	if err != nil {
		t.Fatalf("load typed transcript: %v", err)
	}
	info, err := ProtocolFromRecords(records)
	if err != nil {
		t.Fatalf("classify typed transcript: %v", err)
	}
	if info.Legacy || info.Version != types.DeliberationProtocolVersion {
		t.Fatalf("typed protocol classification: %#v", info)
	}

	invalidPath := filepath.Join(t.TempDir(), "invalid-typed.jsonl")
	invalid := mkRecord(0, "alpha", "opening", false, "")
	invalid.Control = types.NewDeliberationControlState([]string{"alpha", "alpha"}, 0)
	if err := os.WriteFile(invalidPath, []byte(marshalLine(t, invalid)+"\n"), 0o644); err != nil {
		t.Fatalf("write invalid typed transcript: %v", err)
	}
	if _, err := LoadFileStrict(invalidPath); err == nil || !strings.Contains(err.Error(), "invalid control state") {
		t.Fatalf("invalid typed protocol error: got %v", err)
	}
}

func TestHistoricalPreContractActiveSnapshotsRemainReadable(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		withWork bool
	}{
		{name: "typed v1", version: types.LegacyDeliberationProtocolVersion},
		{name: "early typed v2", version: types.DeliberationProtocolVersion, withWork: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "historical-active.jsonl")
			line := historicalControlLine(t, historicalControlOptions{
				protocolVersion: tt.version, includeContributions: tt.withWork,
			})
			if err := os.WriteFile(path, []byte(line+"\n"), 0o644); err != nil {
				t.Fatalf("write historical active transcript: %v", err)
			}
			loaded, err := LoadFileStrict(path)
			if err != nil {
				t.Fatalf("strict load historical active transcript: %v", err)
			}
			info, err := ProtocolFromRecords(loaded)
			if err != nil || !info.PreContractActive || info.Legacy || info.Version != types.DeliberationProtocolVersion {
				t.Fatalf("historical active protocol info: info=%#v err=%v", info, err)
			}
			if loaded[0].Control.Convergence.MinimumRounds != 0 || loaded[0].Control.Convergence.RequiredDeliverableItems != 0 {
				t.Fatalf("historical requirements were fabricated during load: %#v", loaded[0].Control.Convergence)
			}
		})
	}
}

func TestHistoricalPreContractTerminalConsensusRejects(t *testing.T) {
	tests := []struct {
		name     string
		version  string
		withWork bool
	}{
		{name: "typed v1", version: types.LegacyDeliberationProtocolVersion},
		{name: "early typed v2", version: types.DeliberationProtocolVersion, withWork: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "historical-terminal.jsonl")
			active := historicalControlLine(t, historicalControlOptions{
				protocolVersion: tt.version, includeContributions: tt.withWork,
			})
			terminal := historicalControlLine(t, historicalControlOptions{
				protocolVersion: tt.version, includeContributions: tt.withWork, terminal: true, terminalRequirements: tt.version == types.DeliberationProtocolVersion,
			})
			if err := os.WriteFile(path, []byte(active+"\n"+terminal+"\n"), 0o644); err != nil {
				t.Fatalf("write historical terminal transcript: %v", err)
			}
			if _, err := LoadFileStrict(path); err == nil || !strings.Contains(err.Error(), terminalConsensusMissingRunContract) {
				t.Fatalf("strict historical terminal error: got %v", err)
			}
			if _, err := LoadFileLenient(path, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), terminalConsensusMissingRunContract) {
				t.Fatalf("lenient historical terminal error: got %v", err)
			}
		})
	}
}

func TestTerminalFirstHistoricalAndCurrentConsensusRejects(t *testing.T) {
	tests := []struct {
		name    string
		options historicalControlOptions
	}{
		{
			name:    "typed v1 pre-contract",
			options: historicalControlOptions{protocolVersion: types.LegacyDeliberationProtocolVersion, terminal: true},
		},
		{
			name:    "early typed v2 pre-contract",
			options: historicalControlOptions{protocolVersion: types.DeliberationProtocolVersion, includeContributions: true, terminal: true},
		},
		{
			name:    "current typed v2",
			options: historicalControlOptions{protocolVersion: types.DeliberationProtocolVersion, includeContributions: true, terminal: true, terminalRequirements: true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "terminal-first.jsonl")
			if err := os.WriteFile(path, []byte(historicalControlLine(t, tt.options)+"\n"), 0o644); err != nil {
				t.Fatalf("write terminal-first transcript: %v", err)
			}
			if _, err := LoadFileStrict(path); err == nil || !strings.Contains(err.Error(), terminalConsensusMissingRunContract) {
				t.Fatalf("strict terminal-first error: got %v", err)
			}
			if _, err := LoadFileLenient(path, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), terminalConsensusMissingRunContract) {
				t.Fatalf("lenient terminal-first error: got %v", err)
			}
		})
	}
}

func TestPreContractBoundaryAfterTerminalCannotAuthenticateConsensus(t *testing.T) {
	path := filepath.Join(t.TempDir(), "retroactive-boundary.jsonl")
	active := historicalControlLine(t, historicalControlOptions{
		protocolVersion: types.DeliberationProtocolVersion, includeContributions: true,
	})
	terminal := historicalControlLine(t, historicalControlOptions{
		protocolVersion: types.DeliberationProtocolVersion, includeContributions: true, terminal: true, terminalRequirements: true,
	})
	if err := os.WriteFile(path, []byte(active+"\n"+terminal+"\n"+normalizedHistoricalBoundaryLine(t)+"\n"), 0o644); err != nil {
		t.Fatalf("write retroactive boundary transcript: %v", err)
	}
	if _, err := LoadFileStrict(path); err == nil || !strings.Contains(err.Error(), terminalConsensusMissingRunContract) || !strings.Contains(err.Error(), "record 1") {
		t.Fatalf("retroactive boundary strict error: got %v", err)
	}
	if _, err := LoadFileLenient(path, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), terminalConsensusMissingRunContract) || !strings.Contains(err.Error(), "record 1") {
		t.Fatalf("retroactive boundary lenient error: got %v", err)
	}
}

func TestTypedSourceBoundsRequirePersistedEvidence(t *testing.T) {
	phantomPath := filepath.Join(t.TempDir(), "phantom.jsonl")
	phantom := types.NewDeliberationControlState([]string{"alpha"}, 1)
	phantomRecord := mkRecord(0, "alpha", "phantom source", false, "")
	phantomRecord.Control = phantom
	if err := os.WriteFile(phantomPath, []byte(marshalLine(t, phantomRecord)+"\n"), 0o644); err != nil {
		t.Fatalf("write phantom transcript: %v", err)
	}
	if _, err := LoadFileStrict(phantomPath); err == nil || !strings.Contains(err.Error(), "persisted evidence supplies 0") {
		t.Fatalf("phantom source accepted by strict loader: %v", err)
	}
	if _, err := LoadFileLenient(phantomPath, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "persisted evidence supplies 0") {
		t.Fatalf("phantom source accepted by resume loader: %v", err)
	}

	boundPath := filepath.Join(t.TempDir(), "bound.jsonl")
	evidenceRecord := mkRecord(-2, "moderator", "one supplied source", false, "")
	evidenceRecord.Evidence = &types.EvidenceBundle{SourceReferences: []types.SourceReference{{Title: "source"}}}
	bound := types.NewDeliberationControlState([]string{"alpha"}, 1)
	boundRecord := mkRecord(0, "alpha", "bound source", false, "")
	boundRecord.Control = bound
	content := marshalLine(t, evidenceRecord) + "\n" + marshalLine(t, boundRecord) + "\n"
	if err := os.WriteFile(boundPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write bound transcript: %v", err)
	}
	loaded, err := LoadFileStrict(boundPath)
	if err != nil {
		t.Fatalf("bound source rejected: %v", err)
	}
	persisted, err := EvidenceFromRecords(loaded)
	if err != nil || persisted == nil || len(persisted.SourceReferences) != 1 {
		t.Fatalf("persisted evidence: got=%#v err=%v", persisted, err)
	}
}

func TestLegacyTypedV1IsExplicitlyMigratedWithoutPhantomSources(t *testing.T) {
	legacyState := func() *types.DeliberationControlState {
		state := types.NewDeliberationControlState([]string{"alpha"}, 1)
		state.ProtocolVersion = types.LegacyDeliberationProtocolVersion
		state.CurrentProposalVersion = 1
		state.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "legacy proposal"}}
		state.Claims = []types.ClaimEvidence{{
			ID: "legacy-claim", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimFact,
			Decisive: true, Status: types.EvidenceVerified, SourceRefs: []int{0},
		}}
		return state
	}

	raw := []types.TurnRecord{{Turn: 0, AgentID: "alpha", Content: "legacy", Control: legacyState()}}
	info, err := ProtocolFromRecords(raw)
	if err != nil {
		t.Fatalf("legacy protocol migration: %v", err)
	}
	if info.Legacy || info.Version != types.DeliberationProtocolVersion || info.MigratedFrom != types.LegacyDeliberationProtocolVersion {
		t.Fatalf("legacy migration info: %#v", info)
	}
	if got := raw[0].Control; got.SourceReferenceCount != 0 || got.Claims[0].Status != types.EvidenceUnverified || len(got.Claims[0].SourceRefs) != 0 {
		t.Fatalf("unsafe legacy source preservation: %#v", got)
	}

	path := filepath.Join(t.TempDir(), "legacy-typed.jsonl")
	legacyRecord := types.TurnRecord{Turn: 0, AgentID: "alpha", Content: "legacy", Control: legacyState()}
	if err := os.WriteFile(path, []byte(marshalLine(t, legacyRecord)+"\n"), 0o644); err != nil {
		t.Fatalf("write legacy transcript: %v", err)
	}
	loaded, err := LoadFileStrict(path)
	if err != nil {
		t.Fatalf("legacy typed transcript should remain readable: %v", err)
	}
	if loaded[0].Control.ProtocolVersion != types.DeliberationProtocolVersion || loaded[0].Control.SourceReferenceCount != 0 {
		t.Fatalf("loaded legacy normalization: %#v", loaded[0].Control)
	}
}

func TestTypedV1TerminalConsensusUsesMigratedActiveRunContract(t *testing.T) {
	active := authenticatedActiveConsensus()
	active.ProtocolVersion = types.LegacyDeliberationProtocolVersion
	terminal := terminalConsensusFromActive(t, active)
	terminal.ProtocolVersion = types.LegacyDeliberationProtocolVersion
	path := filepath.Join(t.TempDir(), "typed-v1-terminal.jsonl")
	content := marshalLine(t, mkControlRecord(active)) + "\n" + marshalLine(t, mkControlRecord(terminal)) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write typed v1 transcript: %v", err)
	}

	loaded, err := LoadFileStrict(path)
	if err != nil {
		t.Fatalf("typed v1 terminal load: %v", err)
	}
	if len(loaded) != 2 || loaded[0].Control.ProtocolVersion != types.DeliberationProtocolVersion || loaded[1].Control.Outcome.Kind != types.OutcomeConsensus {
		t.Fatalf("migrated typed v1 terminal: %#v", loaded)
	}
}

func TestTranscriptLoadersRejectMutationAfterTerminalState(t *testing.T) {
	active := types.NewDeliberationControlState([]string{"alpha"}, 0)
	active.Phase = types.PhaseVoting
	active.CurrentProposalVersion = 1
	active.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "final proposal"}}
	active.Convergence.RequiredEndorsements = 1
	active.Convergence.MinimumRounds = 1
	active.Contributions = []types.AgentContribution{{AgentID: "alpha", Turn: 0, Position: "terminal", ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone}}}
	active.Votes = []types.ProposalVote{{AgentID: "alpha", ProposalVersion: 1, Choice: types.VoteEndorse}}
	terminal := terminalConsensusFromActive(t, active)
	if err := terminal.Validate(); err != nil {
		t.Fatalf("terminal state: %v", err)
	}

	mutated := *terminal
	mutated.Outcome.Reason = "post-terminal mutation"
	first := mkRecord(0, "alpha", "active", false, "")
	first.Control = active
	second := mkRecord(1, "alpha", "terminal", false, "")
	second.Control = terminal
	third := mkRecord(2, "alpha", "post-terminal vote", false, "")
	third.Control = &mutated
	path := filepath.Join(t.TempDir(), "post-terminal-mutation.jsonl")
	content := marshalLine(t, first) + "\n" + marshalLine(t, second) + "\n" + marshalLine(t, third) + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	if _, err := LoadFileStrict(path); err == nil || !strings.Contains(err.Error(), "terminal control state is immutable") {
		t.Fatalf("strict loader error: got %v", err)
	}
	if _, err := LoadFileLenient(path, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "terminal control state is immutable") {
		t.Fatalf("lenient loader error: got %v", err)
	}
}

func TestLoadRejectsUnauthenticatedPersistedConsensus(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*types.DeliberationControlState, *types.DeliberationControlState)
	}{
		{
			name: "absent votes",
			mutate: func(_ *types.DeliberationControlState, state *types.DeliberationControlState) {
				state.Votes = nil
				state.Convergence.CurrentEndorsements = 0
				state.Outcome.DissentingAgentIDs = []string{"alpha", "beta"}
			},
		},
		{
			name: "stale votes",
			mutate: func(_ *types.DeliberationControlState, state *types.DeliberationControlState) {
				state.Proposals = append(state.Proposals, types.CanonicalProposal{Version: 2, AuthorID: "beta", Content: "proposal two", Supersedes: 1})
				state.CurrentProposalVersion = 2
				state.Convergence.CurrentEndorsements = 0
				state.Outcome.ProposalVersion = 2
				state.Outcome.DissentingAgentIDs = []string{"alpha", "beta"}
			},
		},
		{
			name: "split current versions",
			mutate: func(_ *types.DeliberationControlState, state *types.DeliberationControlState) {
				state.Proposals = append(state.Proposals, types.CanonicalProposal{Version: 2, AuthorID: "beta", Content: "proposal two", Supersedes: 1})
				state.CurrentProposalVersion = 2
				state.Votes[1].ProposalVersion = 2
				state.Convergence.CurrentEndorsements = 1
				state.Outcome.ProposalVersion = 2
				state.Outcome.DissentingAgentIDs = []string{"alpha"}
			},
		},
		{
			name: "minimum rounds unmet",
			mutate: func(active, state *types.DeliberationControlState) {
				active.Convergence.MinimumRounds = 2
				state.Convergence.MinimumRounds = 2
			},
		},
		{
			name: "deliverable unmet",
			mutate: func(active, state *types.DeliberationControlState) {
				active.Convergence.RequiredDeliverableItems = 3
				state.Convergence.RequiredDeliverableItems = 3
			},
		},
		{
			name: "objection unresolved",
			mutate: func(_ *types.DeliberationControlState, state *types.DeliberationControlState) {
				state.Objections = append(state.Objections, types.Objection{ID: "obj-1", AgentID: "alpha", ProposalVersion: 1, Summary: "challenge"})
				state.Convergence.UnresolvedObjections = 1
				state.Outcome.UnresolvedObjectionIDs = []string{"obj-1"}
			},
		},
		{
			name: "evidence gap unresolved",
			mutate: func(_ *types.DeliberationControlState, state *types.DeliberationControlState) {
				state.Claims = append(state.Claims, types.ClaimEvidence{ID: "claim-1", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimFact, Decisive: true, Status: types.EvidenceUnverified})
				state.Convergence.EvidenceGaps = 1
				state.Outcome.EvidenceGapClaimIDs = []string{"claim-1"}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active := authenticatedActiveConsensus()
			state := terminalConsensusFromActive(t, active)
			tt.mutate(active, state)
			path := filepath.Join(t.TempDir(), "invalid-terminal.jsonl")
			content := marshalLine(t, mkControlRecord(active)) + "\n" + marshalLine(t, mkControlRecord(state)) + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write transcript: %v", err)
			}
			if _, err := LoadFileStrict(path); err == nil {
				t.Fatal("unauthenticated persisted consensus was accepted")
			}
		})
	}

	validPath := filepath.Join(t.TempDir(), "valid-terminal.jsonl")
	validActive := authenticatedActiveConsensus()
	validTerminal := terminalConsensusFromActive(t, validActive)
	if err := os.WriteFile(validPath, []byte(marshalLine(t, mkControlRecord(validActive))+"\n"+marshalLine(t, mkControlRecord(validTerminal))+"\n"), 0o644); err != nil {
		t.Fatalf("write valid transcript: %v", err)
	}
	if loaded, err := LoadFileStrict(validPath); err != nil || len(loaded) != 2 || loaded[1].Control.Outcome.Kind != types.OutcomeConsensus {
		t.Fatalf("valid authenticated consensus load: records=%#v err=%v", loaded, err)
	}
}

func TestLoadersBindTerminalConsensusRequirementsToActiveContract(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(*types.DeliberationControlState)
		mutate  func(*types.DeliberationControlState)
		wantErr bool
	}{
		{
			name: "matching requirements",
			prepare: func(state *types.DeliberationControlState) {
				state.Convergence.MinimumRounds = 1
				state.Convergence.RequiredDeliverableItems = 3
				state.Proposals[0].Content = "1. An agent must verify claims.\n2. An agent must preserve evidence.\n3. An agent must record dissent."
			},
		},
		{
			name: "lowered endorsement threshold",
			prepare: func(state *types.DeliberationControlState) {
				state.Convergence.RequiredEndorsements = 3
			},
			mutate: func(state *types.DeliberationControlState) {
				state.Convergence.RequiredEndorsements = 2
			},
			wantErr: true,
		},
		{
			name: "lowered minimum rounds",
			prepare: func(state *types.DeliberationControlState) {
				state.Convergence.MinimumRounds = 2
			},
			mutate: func(state *types.DeliberationControlState) {
				state.Convergence.MinimumRounds = 1
			},
			wantErr: true,
		},
		{
			name: "lowered deliverable requirement",
			prepare: func(state *types.DeliberationControlState) {
				state.Convergence.RequiredDeliverableItems = 3
			},
			mutate: func(state *types.DeliberationControlState) {
				state.Convergence.RequiredDeliverableItems = 0
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active := authenticatedActiveConsensus()
			tt.prepare(active)
			terminal := terminalConsensusFromActive(t, active)
			if tt.mutate != nil {
				tt.mutate(terminal)
			}
			path := filepath.Join(t.TempDir(), "requirements.jsonl")
			content := marshalLine(t, mkControlRecord(active)) + "\n" + marshalLine(t, mkControlRecord(terminal)) + "\n"
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				t.Fatalf("write transcript: %v", err)
			}

			strict, strictErr := LoadFileStrict(path)
			var warnings bytes.Buffer
			lenient, lenientErr := LoadFileLenient(path, &warnings)
			if tt.wantErr {
				for _, err := range []error{strictErr, lenientErr} {
					if err == nil || !strings.Contains(err.Error(), "run consensus requirements are immutable") {
						t.Fatalf("requirement mutation error: got %v", err)
					}
				}
				return
			}
			if strictErr != nil || len(strict) != 2 || lenientErr != nil || len(lenient) != 2 || warnings.Len() != 0 {
				t.Fatalf("authenticated terminal load: strict=%#v strictErr=%v lenient=%#v lenientErr=%v warnings=%q", strict, strictErr, lenient, lenientErr, warnings.String())
			}
		})
	}
}

func TestLoadersRejectTerminalFirstTypedConsensusWithoutRunContract(t *testing.T) {
	for _, tt := range []struct {
		name         string
		version      string
		replayConfig bool
	}{
		{name: "v2", version: types.DeliberationProtocolVersion},
		{name: "v1 migration input", version: types.LegacyDeliberationProtocolVersion},
		{name: "v2 replay metadata", version: types.DeliberationProtocolVersion, replayConfig: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			terminal := authenticatedTerminalConsensus()
			terminal.ProtocolVersion = tt.version
			if tt.version == types.DeliberationProtocolVersion {
				if err := terminal.Validate(); err != nil {
					t.Fatalf("self-consistent terminal state: %v", err)
				}
			}
			record := mkControlRecord(terminal)
			if tt.replayConfig {
				record.Transcript = &types.TranscriptMetadata{
					SchemaVersion: 1,
					Config:        &types.DeliberationConfig{ConsensusThreshold: 2, MinRounds: 1},
				}
			}
			path := filepath.Join(t.TempDir(), "terminal-first.jsonl")
			if err := os.WriteFile(path, []byte(marshalLine(t, record)+"\n"), 0o644); err != nil {
				t.Fatalf("write terminal-first transcript: %v", err)
			}

			if _, err := LoadFileStrict(path); err == nil || !strings.Contains(err.Error(), terminalConsensusMissingRunContract) {
				t.Fatalf("strict terminal-first error: got %v", err)
			}
			if _, err := LoadFileLenient(path, &bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), terminalConsensusMissingRunContract) {
				t.Fatalf("lenient terminal-first error: got %v", err)
			}
		})
	}
}

func mkControlRecord(control *types.DeliberationControlState) types.TurnRecord {
	record := mkRecord(-1, "moderator", "", false, "")
	record.Control = control
	return record
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
