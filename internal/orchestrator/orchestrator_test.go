package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/evidence"
	"github.com/jgabor/agora/internal/ledger"
	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

// mockRunner is a Runner whose Run method returns canned responses.
type mockRunner struct {
	content  string
	metadata *types.RunMetadata
	err      error
	onRun    func()
	agent    types.AgentConfig
	envelope map[string]any
}

func (m *mockRunner) Run(agent types.AgentConfig, envelope map[string]any) (string, *types.RunMetadata, error) {
	m.agent = agent
	m.envelope = envelope
	if m.onRun != nil {
		m.onRun()
	}
	if m.err != nil {
		return "", nil, m.err
	}
	return m.content, m.metadata, nil
}

type mockEvidenceCollector struct {
	request types.EvidenceRequest
	bundle  *types.EvidenceBundle
	err     error
	onRun   func()
}

func (m *mockEvidenceCollector) Collect(request types.EvidenceRequest) (*types.EvidenceBundle, error) {
	m.request = request
	if m.onRun != nil {
		m.onRun()
	}
	return m.bundle, m.err
}

func newTestState(cfg *types.DeliberationConfig) *types.DeliberationState {
	return &types.DeliberationState{
		Config:    cfg,
		Topic:     "test topic",
		Window:    2,
		MaxTurns:  10,
		TimeLimit: 30,
		Running:   true,
		StartTime: float64(time.Now().UnixNano()) / 1e9,
	}
}

func newTestAgents(n int) []types.AgentConfig {
	agents := make([]types.AgentConfig, n)
	for i := range n {
		agents[i] = types.AgentConfig{
			ID:           fmt.Sprintf("agent-%d", i),
			Model:        "test-model",
			SystemPrompt: "You are a test agent.",
		}
	}
	return agents
}

func TestEmitSeed(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_transcript.jsonl")
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(2)})
	o := NewOrchestrator(state, tm, &mockRunner{})

	o.emitSeed()

	records := tm.Records()
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	r := records[0]
	if r.AgentID != "moderator" {
		t.Errorf("expected moderator, got %s", r.AgentID)
	}
	if r.Turn != -1 {
		t.Errorf("expected turn -1, got %d", r.Turn)
	}
	if r.Content == "" {
		t.Error("expected non-empty content")
	}
}

func TestRunAttemptsResearchBeforeDeliberation(t *testing.T) {
	order := []string{}
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(1)})
	state.MaxTurns = 1
	state.Evidence = types.EvidenceRequest{ResearchEnabled: true, ContextPaths: []string{"README.md"}}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	runner := &mockRunner{
		content: "agent response",
		onRun: func() {
			order = append(order, "turn")
		},
	}
	collector := &mockEvidenceCollector{
		bundle: &types.EvidenceBundle{
			Summary:          "research summary",
			SourceReferences: []types.SourceReference{{Title: "source", URL: "https://example.com"}},
		},
		onRun: func() {
			order = append(order, "research")
		},
	}

	o := NewOrchestrator(state, tm, runner)
	o.SetEvidenceCollector(collector)
	o.Run()

	if len(order) < 2 || order[0] != "research" || order[1] != "turn" {
		t.Fatalf("execution order: got %#v, want research before first turn", order)
	}
	if !collector.request.ResearchEnabled {
		t.Fatal("collector request ResearchEnabled: got false, want true")
	}
	if len(collector.request.ContextPaths) != 1 || collector.request.ContextPaths[0] != "README.md" {
		t.Fatalf("collector ContextPaths: got %#v, want README.md", collector.request.ContextPaths)
	}
	if collector.request.Topic != "test topic" {
		t.Fatalf("collector Topic: got %q, want test topic", collector.request.Topic)
	}
	if collector.request.ResearchModel != "test-model" {
		t.Fatalf("collector ResearchModel: got %q, want test-model", collector.request.ResearchModel)
	}
}

func TestRunStopsActivityBeforeTurnCallback(t *testing.T) {
	order := []string{}
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(1)})
	state.MaxTurns = 1
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	runner := &mockRunner{
		content: "agent response",
		onRun: func() {
			order = append(order, "runner")
		},
	}
	o := NewOrchestrator(state, tm, runner)
	o.OnActivity(func(phase string) func() {
		order = append(order, "start "+phase)
		return func() { order = append(order, "stop "+phase) }
	})
	o.OnTurn(func(record types.TurnRecord, turn int, maxTurns int) {
		order = append(order, "turn")
	})

	o.Run()

	want := []string{"start Generation: agent-0", "runner", "stop Generation: agent-0", "turn"}
	if !reflect.DeepEqual(order, want) {
		t.Fatalf("activity order: got %#v, want %#v", order, want)
	}
}

func TestRunHaltsWhenResearchProducesNoReferences(t *testing.T) {
	turnCalled := false
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(1)})
	state.Evidence = types.EvidenceRequest{ResearchEnabled: true}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	runner := &mockRunner{onRun: func() { turnCalled = true }}
	collector := &mockEvidenceCollector{bundle: &types.EvidenceBundle{Summary: "empty"}}

	o := NewOrchestrator(state, tm, runner)
	o.SetEvidenceCollector(collector)
	o.Run()

	if turnCalled {
		t.Fatal("runner was called despite failed research evidence")
	}
	if !strings.Contains(state.HaltedBy, "no source references") {
		t.Fatalf("HaltedBy: got %q, want no source references", state.HaltedBy)
	}
}

func TestRunWritesAuditableEvidenceReferencesToTranscript(t *testing.T) {
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(1)})
	state.MaxTurns = 1
	state.Evidence = types.EvidenceRequest{ResearchEnabled: true}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	runner := &mockRunner{content: "agent response"}
	collector := &mockEvidenceCollector{bundle: &types.EvidenceBundle{
		Summary: "research summary",
		SourceReferences: []types.SourceReference{{
			Title:       "source title",
			URL:         "https://example.com/source",
			Query:       "audit query",
			RetrievedAt: "2026-05-05T00:00:00Z",
		}},
	}}

	o := NewOrchestrator(state, tm, runner)
	o.SetEvidenceCollector(collector)
	o.Run()

	records := tm.Records()
	if len(records) == 0 || records[0].Evidence == nil {
		t.Fatalf("first transcript record: got %#v, want evidence bundle", records)
	}
	refs := records[0].Evidence.SourceReferences
	if len(refs) != 1 || refs[0].URL == "" || refs[0].Query == "" || refs[0].RetrievedAt == "" {
		t.Fatalf("evidence references: got %#v, want url/query/retrieved_at metadata", refs)
	}
	for _, record := range records {
		if record.AgentID != "moderator" && record.Evidence != nil {
			t.Fatalf("agent record contains evidence unexpectedly: %#v", record)
		}
	}
}

func TestRunDeliversEvidenceToEachAgentFirstTurnOnly(t *testing.T) {
	bundle := &types.EvidenceBundle{
		Summary: "shared evidence summary",
		SourceReferences: []types.SourceReference{{
			Title:       "source title",
			URL:         "https://example.com/source",
			Query:       "audit query",
			RetrievedAt: "2026-05-05T00:00:00Z",
		}},
	}
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(2), Topology: types.TopologyRing})
	state.Topic = "What would the best programming language be to implement this tool?"
	state.MaxTurns = 4
	state.Evidence = types.EvidenceRequest{ResearchEnabled: true}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	collector := &mockEvidenceCollector{bundle: bundle}

	o := NewOrchestrator(state, tm, runner)
	o.SetEvidenceCollector(collector)
	o.Run()

	if len(runner.envelopes) != 4 {
		t.Fatalf("runner envelopes: got %d, want 4", len(runner.envelopes))
	}
	for i, envelope := range runner.envelopes {
		got, ok := envelope["evidence"].(*types.EvidenceBundle)
		if i < 2 {
			if !ok || got != bundle {
				t.Fatalf("turn %d evidence: got %#v, want shared bundle pointer", i, envelope["evidence"])
			}
			continue
		}
		if ok || envelope["evidence"] != nil {
			t.Fatalf("turn %d evidence: got %#v, want omitted after first agent turn", i, envelope["evidence"])
		}
	}
}

func TestRunTranscriptEvidenceExcludesFullSourceContent(t *testing.T) {
	fullSourceContent := "full source body that must not be serialized"
	path := writeContextFile(t, t.TempDir(), "README.md", fullSourceContent)
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(1), Topology: types.TopologyRing})
	state.MaxTurns = 1
	state.Evidence = types.EvidenceRequest{ContextPaths: []string{path}}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	runner := &mockRunner{content: "agent response"}

	o := NewOrchestrator(state, tm, runner)
	o.SetEvidenceCollector(evidence.NewPolicyCollector(runner))
	o.Run()

	evidence, ok := runner.envelope["evidence"].(*types.EvidenceBundle)
	if !ok || len(evidence.ContextDocuments) != 1 || evidence.ContextDocuments[0].Content != fullSourceContent {
		t.Fatalf("agent evidence context: got %#v, want delivered local text", runner.envelope["evidence"])
	}

	data, err := json.Marshal(tm.Records())
	if err != nil {
		t.Fatalf("marshal records: %v", err)
	}
	transcriptJSON := string(data)
	if strings.Contains(transcriptJSON, fullSourceContent) || strings.Contains(transcriptJSON, "body") || strings.Contains(transcriptJSON, "snippet") {
		t.Fatalf("transcript contains full source content fields unexpectedly: %s", transcriptJSON)
	}
	if !strings.Contains(transcriptJSON, "source_references") || !strings.Contains(transcriptJSON, "README.md") {
		t.Fatalf("transcript JSON: got %s, want source references", transcriptJSON)
	}
}

func TestRunWithoutEvidencePreservesDeliberationEnvelopeAndTranscript(t *testing.T) {
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(1), Topology: types.TopologyRing})
	state.MaxTurns = 1
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}

	o := NewOrchestrator(state, tm, runner)
	o.Run()

	if len(runner.envelopes) != 1 {
		t.Fatalf("runner envelopes: got %d, want 1", len(runner.envelopes))
	}
	if _, ok := runner.envelopes[0]["evidence"]; ok {
		t.Fatalf("unexpected evidence envelope without research/context: %#v", runner.envelopes[0])
	}
	records := tm.Records()
	if len(records) != 2 || records[0].Turn != -1 || records[0].Evidence != nil || records[1].Evidence != nil {
		t.Fatalf("records: got %#v, want seed plus one agent record without evidence", records)
	}
}

func TestRunHaltsWhenResearchQueryGenerationProducesNoQueries(t *testing.T) {
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(1)})
	state.Evidence = types.EvidenceRequest{ResearchEnabled: true, MaxSources: 3}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	var runner *mockRunner
	calls := []string{}
	runner = &mockRunner{
		content: `{"queries":[]}`,
		onRun: func() {
			calls = append(calls, runner.agent.ID)
		},
	}

	o := NewOrchestrator(state, tm, runner)
	o.SetEvidenceCollector(evidence.NewPolicyCollector(runner))
	stats := o.Run()

	if len(calls) != 1 || calls[0] != "research-query-planner" {
		t.Fatalf("runner calls: got %#v, want only research query generation", calls)
	}
	if stats.TotalTurns != 0 {
		t.Fatalf("stats.TotalTurns: got %d, want 0 before deliberation starts", stats.TotalTurns)
	}
	if !strings.Contains(state.HaltedBy, "no research queries produced") {
		t.Fatalf("HaltedBy: got %q, want no research queries produced", state.HaltedBy)
	}
}

func TestRunHaltsWhenContextProducesNoReferences(t *testing.T) {
	turnCalled := false
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(1)})
	state.Evidence = types.EvidenceRequest{ContextPaths: []string{"empty-dir"}}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	runner := &mockRunner{onRun: func() { turnCalled = true }}
	collector := &mockEvidenceCollector{bundle: &types.EvidenceBundle{Summary: "empty"}}

	o := NewOrchestrator(state, tm, runner)
	o.SetEvidenceCollector(collector)
	o.Run()

	if turnCalled {
		t.Fatal("runner was called despite empty context evidence")
	}
	if !strings.Contains(state.HaltedBy, "no source references") {
		t.Fatalf("HaltedBy: got %q, want no source references", state.HaltedBy)
	}
}

func TestRunResumeWithPriorEvidenceDoesNotCollectAgain(t *testing.T) {
	state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(1)})
	state.MaxTurns = 2
	state.Turn = 1
	state.Evidence = types.EvidenceRequest{ResearchEnabled: true, ContextPaths: []string{"README.md"}}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
	if err := tm.Append(types.TurnRecord{
		Turn:    -2,
		AgentID: "moderator",
		Content: "prior research summary",
		Evidence: &types.EvidenceBundle{SourceReferences: []types.SourceReference{{
			Title: "prior source",
			URL:   "https://example.com/prior",
		}}},
	}); err != nil {
		t.Fatalf("append prior evidence: %v", err)
	}
	if err := tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"}); err != nil {
		t.Fatalf("append seed: %v", err)
	}
	if err := tm.Append(types.TurnRecord{Turn: 0, AgentID: "agent-0", Content: "prior turn"}); err != nil {
		t.Fatalf("append prior turn: %v", err)
	}
	collectorCalled := false

	o := NewOrchestrator(state, tm, &mockRunner{content: "resumed turn"})
	o.SetEvidenceCollector(&mockEvidenceCollector{onRun: func() { collectorCalled = true }})
	o.Run()

	if collectorCalled {
		t.Fatal("evidence collector was called while resuming a transcript with prior records")
	}
	if len(tm.Records()) != 4 || tm.Records()[3].Content != "resumed turn" {
		t.Fatalf("records: got %#v, want prior records plus one resumed agent turn", tm.Records())
	}
}

func TestCheckTerminationConditions(t *testing.T) {
	tests := []struct {
		name             string
		timeLimit        int
		elapsedOffset    float64
		consensusStreak  int
		consensusThresh  int
		turn             int
		budget           *float64
		transcriptCost   float64
		expectHalted     bool
		expectHaltReason string
	}{
		{
			name:            "no termination conditions met",
			timeLimit:       30,
			elapsedOffset:   5,
			consensusStreak: 1,
			consensusThresh: 3,
			expectHalted:    false,
		},
		{
			name:             "time limit exceeded",
			timeLimit:        30,
			elapsedOffset:    35,
			consensusStreak:  1,
			consensusThresh:  3,
			expectHalted:     true,
			expectHaltReason: "time_limit (30s)",
		},
		{
			name:             "consensus threshold reached",
			timeLimit:        30,
			elapsedOffset:    5,
			consensusStreak:  3,
			consensusThresh:  3,
			turn:             2,
			expectHalted:     true,
			expectHaltReason: "consensus (3 consecutive agreements)",
		},
		{
			name:             "budget exceeded",
			timeLimit:        30,
			elapsedOffset:    5,
			consensusStreak:  1,
			consensusThresh:  3,
			budget:           floatPtr(0.01),
			transcriptCost:   0.02,
			expectHalted:     true,
			expectHaltReason: "budget_exceeded ($0.01)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tm := transcript.NewTranscriptManager("/tmp/test_transcript.jsonl")
			cfg := &types.DeliberationConfig{
				Agents:             newTestAgents(2),
				ConsensusThreshold: tt.consensusThresh,
			}
			state := newTestState(cfg)
			state.TimeLimit = tt.timeLimit
			state.Budget = tt.budget
			if tt.turn > 0 {
				state.Turn = tt.turn
			}

			// Simulate elapsed time by back-dating StartTime.
			state.StartTime = float64(time.Now().UnixNano())/1e9 - tt.elapsedOffset

			o := NewOrchestrator(state, tm, &mockRunner{})
			o.consensusStreak = tt.consensusStreak

			// Seed transcript with records that accrue cost.
			if tt.transcriptCost > 0 {
				cost := tt.transcriptCost
				_ = tm.Append(types.TurnRecord{
					Turn:    0,
					AgentID: "agent-0",
					Content: "ok",
					Cost:    &cost,
				})
			}

			o.checkTerminationConditions()

			if o.state.Running != !tt.expectHalted {
				t.Errorf("expected Running=%v, got %v", !tt.expectHalted, o.state.Running)
			}
			if tt.expectHalted && o.state.HaltedBy != tt.expectHaltReason {
				t.Errorf("expected HaltedBy=%q, got %q", tt.expectHaltReason, o.state.HaltedBy)
			}
		})
	}
}

func TestExecuteTurn(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_transcript.jsonl")
	_ = tm.Append(types.TurnRecord{
		Turn:    -1,
		AgentID: "moderator",
		Content: "seed message",
	})

	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := newTestState(cfg)

	total := 42
	input := 20
	output := 22
	cost := 0.005
	mock := &mockRunner{
		content: "[CONSENSUS: we agree] This is the agent response.",
		metadata: &types.RunMetadata{
			Tokens: types.TokenUsage{
				Total:  &total,
				Input:  &input,
				Output: &output,
			},
			Cost: &cost,
		},
	}

	o := NewOrchestrator(state, tm, mock)
	record, ok := o.executeTurn(cfg.Agents[0])
	if !ok {
		t.Fatal("expected turn record")
	}

	if record.AgentID != "agent-0" {
		t.Errorf("expected agent-0, got %s", record.AgentID)
	}
	if record.Turn != 0 {
		t.Errorf("expected turn 0, got %d", record.Turn)
	}
	if !record.Consensus {
		t.Error("expected consensus=true")
	}
	if record.ConsensusStatement != "we agree" {
		t.Errorf("expected 'we agree', got %q", record.ConsensusStatement)
	}
	if record.Content == "[CONSENSUS: we agree] This is the agent response." {
		t.Error("consensus marker should have been stripped from content")
	}
	if record.Tokens.Total == nil || *record.Tokens.Total != 42 {
		t.Errorf("expected total tokens=42, got %v", record.Tokens.Total)
	}
	if record.Cost == nil || *record.Cost != 0.005 {
		t.Errorf("expected cost=0.005, got %v", record.Cost)
	}
	if record.Elapsed <= 0 {
		t.Error("expected positive elapsed time")
	}
	if !strings.HasPrefix(mock.agent.SystemPrompt, agent.ReadOnlyHint) {
		t.Fatalf("runner prompt = %q, want read-only hint", mock.agent.SystemPrompt)
	}
}

func TestExecuteTurnRunnerError(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_transcript.jsonl")
	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := newTestState(cfg)

	mock := &mockRunner{
		err: fmt.Errorf("opencode crashed"),
	}

	o := NewOrchestrator(state, tm, mock)
	record, ok := o.executeTurn(cfg.Agents[0])

	if ok {
		t.Fatalf("expected runner error to skip record, got %#v", record)
	}
	if !o.state.Running {
		t.Error("expected Running=true after skipped runner error")
	}
	if o.state.HaltedBy != "" {
		t.Errorf("expected no halt reason, got %q", o.state.HaltedBy)
	}
}

func TestRunSkipsRunnerErrorAndContinues(t *testing.T) {
	dir := t.TempDir()
	tm := transcript.NewTranscriptManager(dir + "/transcript.jsonl")
	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := &types.DeliberationState{
		Config:    cfg,
		Topic:     "test topic",
		Window:    2,
		MaxTurns:  3,
		TimeLimit: 0,
		Running:   true,
	}
	runner := &seqMockRunner{responses: []mockResponse{
		{err: fmt.Errorf("malformed llm response")},
		{content: "second turn succeeds", metadata: &types.RunMetadata{}},
		{content: "third turn succeeds", metadata: &types.RunMetadata{}},
	}}

	o := NewOrchestrator(state, tm, runner)
	stats := o.Run()

	if state.HaltedBy != "max_turns (3)" {
		t.Errorf("expected max_turns halt after continuing, got %q", state.HaltedBy)
	}
	if state.Turn != 3 {
		t.Errorf("expected Turn=3 after skipped error, got %d", state.Turn)
	}
	if runner.callCount != 3 {
		t.Errorf("expected 3 runner calls, got %d", runner.callCount)
	}
	if stats.TotalTurns != 2 {
		t.Errorf("expected 2 successful agent records, got %d", stats.TotalTurns)
	}
	for _, record := range tm.Records() {
		if strings.Contains(record.Content, "[ERROR]") || strings.Contains(record.Content, "malformed llm response") {
			t.Errorf("skipped runner error leaked into transcript record: %#v", record)
		}
	}
}

// seqMockRunner returns a sequence of canned responses, one per call.
// After the sequence is exhausted it repeats the last entry.
type seqMockRunner struct {
	responses []mockResponse
	callCount int
}

type mockResponse struct {
	content  string
	metadata *types.RunMetadata
	err      error
}

func (s *seqMockRunner) Run(agent types.AgentConfig, envelope map[string]any) (string, *types.RunMetadata, error) {
	idx := s.callCount
	if idx >= len(s.responses) {
		idx = len(s.responses) - 1
	}
	s.callCount++
	r := s.responses[idx]
	if r.err != nil {
		return "", nil, r.err
	}
	return r.content, r.metadata, nil
}

func TestRunMaxTurnsZeroConsensusHalt(t *testing.T) {
	dir := t.TempDir()
	tm := transcript.NewTranscriptManager(dir + "/transcript.jsonl")

	// 2 agents, consensus threshold 3.
	// First 4 turns: no consensus. Then 5 turns with consensus markers.
	// The loop should NOT stop at a turn count (MaxTurns=0) but should halt
	// once consensus streak hits threshold.
	agents := newTestAgents(2)
	cfg := &types.DeliberationConfig{
		Agents:             agents,
		ConsensusThreshold: 5,
	}
	state := &types.DeliberationState{
		Config:    cfg,
		Topic:     "test topic",
		Window:    2,
		MaxTurns:  0, // unlimited
		TimeLimit: 0, // unlimited
		Running:   true,
	}

	noConsensus := mockResponse{content: "I disagree.", metadata: &types.RunMetadata{}}
	withConsensus := mockResponse{content: "[CONSENSUS: we agree] Agreed.", metadata: &types.RunMetadata{}}

	runner := &seqMockRunner{
		responses: []mockResponse{
			noConsensus, noConsensus, noConsensus, noConsensus, // turns 0-3
			withConsensus, withConsensus, withConsensus, withConsensus, withConsensus, // turns 4-8
		},
	}

	o := NewOrchestrator(state, tm, runner)
	stats := o.Run()

	if state.HaltedBy == "" {
		t.Fatal("expected a halt reason, got none")
	}
	if !strings.Contains(state.HaltedBy, "consensus") {
		t.Errorf("expected halt reason containing 'consensus', got %q", state.HaltedBy)
	}
	// Should have run more than 4 turns (the non-consensus phase).
	if stats.TotalTurns <= 4 {
		t.Errorf("expected more than 4 turns with MaxTurns=0, got %d", stats.TotalTurns)
	}
	// Should NOT have stopped due to max_turns.
	if strings.Contains(state.HaltedBy, "max_turns") {
		t.Errorf("should not halt with max_turns when MaxTurns=0, got %q", state.HaltedBy)
	}
}

func TestRunMaxTurnsTenBackwardCompat(t *testing.T) {
	dir := t.TempDir()
	tm := transcript.NewTranscriptManager(dir + "/transcript.jsonl")

	agents := newTestAgents(2)
	cfg := &types.DeliberationConfig{Agents: agents}
	state := &types.DeliberationState{
		Config:    cfg,
		Topic:     "test topic",
		Window:    2,
		MaxTurns:  10,
		TimeLimit: 0,
		Running:   true,
	}

	// All turns return no consensus, so only the turn cap stops it.
	runner := &seqMockRunner{
		responses: []mockResponse{
			{content: "No consensus here.", metadata: &types.RunMetadata{}},
		},
	}

	o := NewOrchestrator(state, tm, runner)
	stats := o.Run()

	if state.HaltedBy != "max_turns (10)" {
		t.Errorf("expected halt reason 'max_turns (10)', got %q", state.HaltedBy)
	}
	if stats.TotalTurns != 10 {
		t.Errorf("expected 10 agent turns, got %d", stats.TotalTurns)
	}
	// Turn counter should be exactly 10.
	if state.Turn != 10 {
		t.Errorf("expected Turn=10, got %d", state.Turn)
	}
}

func TestRunMaxTurnsZeroDoesNotHaltAtTurnCount(t *testing.T) {
	dir := t.TempDir()
	tm := transcript.NewTranscriptManager(dir + "/transcript.jsonl")

	agents := newTestAgents(2)
	cfg := &types.DeliberationConfig{Agents: agents} // no consensus threshold
	state := &types.DeliberationState{
		Config:    cfg,
		Topic:     "test topic",
		Window:    2,
		MaxTurns:  0, // unlimited
		TimeLimit: 0, // unlimited
		Running:   true,
	}

	// Run 15 turns without consensus, then consensus to stop the loop.
	// If MaxTurns=0 were NOT working, the loop would stop at turn 0
	// because 0 < 0 is false. This test proves we go past turn 0.
	cfg.ConsensusThreshold = 1
	responses := make([]mockResponse, 15)
	for i := range 14 {
		responses[i] = mockResponse{content: "Still going.", metadata: &types.RunMetadata{}}
	}
	responses[14] = mockResponse{content: "[CONSENSUS: done] Done.", metadata: &types.RunMetadata{}}

	runner := &seqMockRunner{responses: responses}

	o := NewOrchestrator(state, tm, runner)
	_ = o.Run()

	if state.Turn < 14 {
		t.Errorf("expected at least 14 turns with MaxTurns=0, got %d", state.Turn)
	}
	if !strings.Contains(state.HaltedBy, "consensus") {
		t.Errorf("expected halt reason containing 'consensus', got %q", state.HaltedBy)
	}
	// Must NOT have halted due to max_turns.
	if strings.Contains(state.HaltedBy, "max_turns") {
		t.Errorf("should not halt with max_turns when MaxTurns=0, got %q", state.HaltedBy)
	}
}

func floatPtr(f float64) *float64 {
	return &f
}

// recordingRunner records agents and envelopes across calls.
type recordingRunner struct {
	responses []mockResponse
	callCount int
	agents    []types.AgentConfig
	envelopes []map[string]any
}

func (r *recordingRunner) Run(agent types.AgentConfig, envelope map[string]any) (string, *types.RunMetadata, error) {
	r.agents = append(r.agents, agent)
	r.envelopes = append(r.envelopes, envelope)
	idx := r.callCount
	if idx >= len(r.responses) {
		idx = len(r.responses) - 1
	}
	r.callCount++
	response := r.responses[idx]
	if response.err != nil {
		return "", nil, response.err
	}
	return response.content, response.metadata, nil
}

func writeContextFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func TestOrchestratorSynthesize(t *testing.T) {
	t.Run("returns nil with one record or fewer", func(t *testing.T) {
		tm := transcript.NewTranscriptManager("/tmp/test_synth.jsonl")
		_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

		state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(2)})
		o := NewOrchestrator(state, tm, &mockRunner{})
		result := o.Synthesize()
		if result != nil {
			t.Error("expected nil with 1 record")
		}
	})

	t.Run("returns synthesis with multiple records", func(t *testing.T) {
		tm := transcript.NewTranscriptManager("/tmp/test_synth.jsonl")
		_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})
		_ = tm.Append(types.TurnRecord{Turn: 0, AgentID: "agent-0", Content: "proposal"})
		_ = tm.Append(types.TurnRecord{Turn: 1, AgentID: "agent-1", Content: "critique"})

		state := newTestState(&types.DeliberationConfig{
			Agents: newTestAgents(2),
		})
		mock := &mockRunner{
			content: "```json\n{\"key_arguments\":[\"arg1\"],\"points_of_agreement\":[],\"unresolved_tensions\":[],\"recommended_decision\":\"proceed\",\"confidence\":\"medium\"}\n```",
		}
		o := NewOrchestrator(state, tm, mock)
		order := []string{}
		o.OnActivity(func(phase string) func() {
			order = append(order, "start "+phase)
			return func() { order = append(order, "stop "+phase) }
		})
		result := o.Synthesize()

		if result == nil {
			t.Fatal("expected non-nil result with 3 records")
		}
		if result["confidence"] != "medium" {
			t.Errorf("expected confidence=medium, got %v", result["confidence"])
		}
		if !reflect.DeepEqual(order, []string{"start Synthesis", "stop Synthesis"}) {
			t.Fatalf("synthesis activity order: got %#v", order)
		}
	})
}

func TestExecuteTurnInjectsLedgerWhenSet(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_ledger.jsonl")
	_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := newTestState(cfg)

	ledger := types.NewDebateLedger(1, 100.0)
	ledger.Positions = []types.AgentPosition{
		{AgentID: "agent-0", Text: "position 0", Turn: 0},
		{AgentID: "agent-1", Text: "position 1", Turn: 1},
	}

	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	o := NewOrchestrator(state, tm, runner)
	o.SetCurrentLedger(ledger)

	_, ok := o.executeTurn(cfg.Agents[0])
	if !ok {
		t.Fatal("expected successful turn execution")
	}

	if len(runner.envelopes) != 1 {
		t.Fatalf("envelopes: got %d, want 1", len(runner.envelopes))
	}
	injected, ok := runner.envelopes[0]["ledger"].(*types.DebateLedger)
	if !ok || injected == nil {
		t.Fatalf("envelope ledger: got %#v, want injected *DebateLedger", runner.envelopes[0]["ledger"])
	}
	if injected.Round != 1 || len(injected.Positions) != 2 {
		t.Fatalf("ledger content: got round=%d positions=%d, want round=1 positions=2", injected.Round, len(injected.Positions))
	}
}

func TestExecuteTurnOmitsLedgerWhenDisabled(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_ledger_disabled.jsonl")
	_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := newTestState(cfg)
	disabled := false
	state.LedgerUpdateEnabled = &disabled

	ledger := types.NewDebateLedger(1, 100.0)

	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	o := NewOrchestrator(state, tm, runner)
	o.SetCurrentLedger(ledger)

	_, ok := o.executeTurn(cfg.Agents[0])
	if !ok {
		t.Fatal("expected successful turn execution")
	}

	if _, hasLedger := runner.envelopes[0]["ledger"]; hasLedger {
		t.Fatalf("envelope should not contain ledger when disabled: %#v", runner.envelopes[0])
	}
}

func TestExecuteTurnOmitsLedgerWhenNil(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_ledger_nil.jsonl")
	_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := newTestState(cfg)

	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	o := NewOrchestrator(state, tm, runner)

	_, ok := o.executeTurn(cfg.Agents[0])
	if !ok {
		t.Fatal("expected successful turn execution")
	}

	if _, hasLedger := runner.envelopes[0]["ledger"]; hasLedger {
		t.Fatalf("envelope should not contain ledger when currentLedger is nil: %#v", runner.envelopes[0])
	}
}

func TestExecuteTurnInjectsLedgerWhenEnabledExplicit(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_ledger_explicit.jsonl")
	_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := newTestState(cfg)
	enabled := true
	state.LedgerUpdateEnabled = &enabled

	ledger := types.NewDebateLedger(0, 50.0)

	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	o := NewOrchestrator(state, tm, runner)
	o.SetCurrentLedger(ledger)

	_, ok := o.executeTurn(cfg.Agents[0])
	if !ok {
		t.Fatal("expected successful turn execution")
	}

	if _, hasLedger := runner.envelopes[0]["ledger"]; !hasLedger {
		t.Fatalf("envelope should contain ledger when explicitly enabled: %#v", runner.envelopes[0])
	}
}

type dryRunCaptureRunner struct {
	inner     *agent.AgentRunner
	envelopes []map[string]any
}

func (d *dryRunCaptureRunner) Run(ag types.AgentConfig, envelope map[string]any) (string, *types.RunMetadata, error) {
	d.envelopes = append(d.envelopes, envelope)
	return d.inner.Run(ag, envelope)
}

func newLedgerTestState(cfg *types.DeliberationConfig, maxTurns int, ledgerEnabled *bool) *types.DeliberationState {
	return &types.DeliberationState{
		Config:              cfg,
		Topic:               "test topic",
		Window:              2,
		MaxTurns:            maxTurns,
		TimeLimit:           0,
		Running:             true,
		StartTime:           float64(time.Now().UnixNano()) / 1e9,
		LedgerUpdateEnabled: ledgerEnabled,
	}
}

func twoAgentLedgerJSON(t *testing.T, round int) string {
	t.Helper()
	return fmt.Sprintf(`{"round":%d,"positions":[{"agent_id":"agent-0","text":"agent response","turn":0},{"agent_id":"agent-1","text":"agent response","turn":1}],"agreements":[],"cruxes":[],"draft":{"status":"none"}}`, round)
}

func threeAgentLedgerJSON(t *testing.T, round int) string {
	t.Helper()
	return fmt.Sprintf(`{"round":%d,"positions":[{"agent_id":"agent-0","text":"agent response","turn":0},{"agent_id":"agent-1","text":"agent response","turn":1},{"agent_id":"agent-2","text":"agent response","turn":2}],"agreements":[],"cruxes":[],"draft":{"status":"none"}}`, round)
}

func countLedgerUpdaterCalls(agents []types.AgentConfig) int {
	calls := 0
	for _, ag := range agents {
		if ag.ID == "ledger-updater" {
			calls++
		}
	}
	return calls
}

func TestLedgerRoundBoundaryUpdate(t *testing.T) {
	t.Run("pass", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
		state := newLedgerTestState(cfg, 4, nil)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		ledgerJSON := twoAgentLedgerJSON(t, 1)
		runner := &recordingRunner{responses: []mockResponse{
			{content: "agent 0 turn 0"},
			{content: "agent 1 turn 1"},
			{content: ledgerJSON},
			{content: "agent 0 turn 2"},
			{content: "agent 1 turn 3"},
			{content: ledgerJSON},
		}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if state.HaltedBy != "max_turns (4)" {
			t.Fatalf("HaltedBy: got %q, want max_turns (4)", state.HaltedBy)
		}
		if o.currentLedger == nil || o.currentLedger.Round != 2 {
			t.Fatalf("currentLedger: got %+v, want Round=2 after second round-boundary update (MaxTurns=4, 2 agents = 2 full rounds)", o.currentLedger)
		}
		if len(runner.envelopes) < 4 {
			t.Fatalf("runner envelopes: got %d, want at least 4", len(runner.envelopes))
		}
		injected, ok := runner.envelopes[3]["ledger"].(*types.DebateLedger)
		if !ok || injected == nil {
			t.Fatalf("envelope[3] ledger: got %#v, want injected *DebateLedger before next-round turn", runner.envelopes[3]["ledger"])
		}
		if injected.Round != 1 {
			t.Errorf("envelope[3] ledger.Round: got %d, want 1", injected.Round)
		}
	})

	t.Run("fail_updater_returns_error_skips_set_and_runs_to_completion", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
		state := newLedgerTestState(cfg, 4, nil)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		runner := &recordingRunner{responses: []mockResponse{
			{content: "agent 0 turn 0"},
			{content: "agent 1 turn 1"},
			{content: "garbage not json"},
			{content: "agent 0 turn 2"},
			{content: "agent 1 turn 3"},
			{content: "garbage not json"},
		}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if state.HaltedBy != "max_turns (4)" {
			t.Fatalf("HaltedBy: got %q, want max_turns (4); failed ledger update must not halt", state.HaltedBy)
		}
		if len(runner.envelopes) != 6 {
			t.Fatalf("runner calls: got %d, want 6 (4 turns + 2 failed updates)", len(runner.envelopes))
		}
		for i, env := range runner.envelopes {
			if _, has := env["ledger"]; has {
				t.Fatalf("envelope[%d] should not contain ledger when Update failed", i)
			}
		}
		if o.currentLedger != nil {
			t.Fatalf("currentLedger: got %+v, want nil after failed updates", o.currentLedger)
		}
	})
}

func TestLedgerRound0Empty(t *testing.T) {
	t.Run("pass_two_agents_one_turn_no_update", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
		state := newLedgerTestState(cfg, 1, nil)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if o.currentLedger != nil {
			t.Fatalf("currentLedger: got %+v, want nil before first round completes", o.currentLedger)
		}
		if len(runner.envelopes) != 1 {
			t.Fatalf("runner envelopes: got %d, want 1", len(runner.envelopes))
		}
		if _, has := runner.envelopes[0]["ledger"]; has {
			t.Fatalf("envelope[0] should not contain ledger before first round completes: %#v", runner.envelopes[0])
		}
	})

	t.Run("fail_four_agents_three_turns_no_round_complete_no_update", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(4)}
		state := newLedgerTestState(cfg, 3, nil)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if o.currentLedger != nil {
			t.Fatalf("currentLedger: got %+v, want nil when round never completes", o.currentLedger)
		}
		for i, env := range runner.envelopes {
			if _, has := env["ledger"]; has {
				t.Fatalf("envelope[%d] should not contain ledger when round never completes", i)
			}
		}
		if got := countLedgerUpdaterCalls(runner.agents); got != 0 {
			t.Fatalf("ledger-updater calls: got %d, want 0 when round never completes", got)
		}
	})
}

func TestLedgerMidRoundInterrupt(t *testing.T) {
	t.Run("pass_halt_mid_round_2_keeps_round_1_ledger_no_partial", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(3)}
		state := newLedgerTestState(cfg, 5, nil)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		ledgerJSON := threeAgentLedgerJSON(t, 1)
		runner := &recordingRunner{responses: []mockResponse{
			{content: "agent 0 turn 0"},
			{content: "agent 1 turn 1"},
			{content: "agent 2 turn 2"},
			{content: ledgerJSON},
			{content: "agent 0 turn 3"},
			{content: "agent 1 turn 4"},
		}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if state.HaltedBy != "max_turns (5)" {
			t.Fatalf("HaltedBy: got %q, want max_turns (5)", state.HaltedBy)
		}
		if o.currentLedger == nil {
			t.Fatal("currentLedger: got nil, want non-nil after round 1 completed before mid-round-2 halt")
		}
		if o.currentLedger.Round != 1 {
			t.Errorf("currentLedger.Round: got %d, want 1 (only round 1 completed; no partial mid-round-2 update)", o.currentLedger.Round)
		}
		if got := countLedgerUpdaterCalls(runner.agents); got != 1 {
			t.Fatalf("ledger-updater calls: got %d, want 1 (no partial mid-round-2 update)", got)
		}
	})

	t.Run("fail_max_turns_below_round_size_no_update", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(3)}
		state := newLedgerTestState(cfg, 2, nil)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if o.currentLedger != nil {
			t.Fatalf("currentLedger: got %+v, want nil when MaxTurns<round-size", o.currentLedger)
		}
		if got := countLedgerUpdaterCalls(runner.agents); got != 0 {
			t.Fatalf("ledger-updater calls: got %d, want 0 when round never completes", got)
		}
	})
}

func TestLedgerDryRunPlaceholder(t *testing.T) {
	t.Run("pass_real_dry_run_runner_takes_update_dry_run_path", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
		state := newLedgerTestState(cfg, 2, nil)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		runner := agent.NewAgentRunner(true)
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if state.HaltedBy != "max_turns (2)" {
			t.Fatalf("HaltedBy: got %q, want max_turns (2)", state.HaltedBy)
		}
		if o.currentLedger == nil {
			t.Fatal("currentLedger: got nil, want non-nil (dry-run path should be detected via *agent.AgentRunner.IsDryRun)")
		}
		if o.currentLedger.Round != 1 {
			t.Errorf("Round: got %d, want 1 (orchestrator stamps authoritative round after UpdateDryRun returns)", o.currentLedger.Round)
		}
		if len(o.currentLedger.Positions) != 2 {
			t.Fatalf("Positions: got %d, want 2 (UpdateDryRun derives positions from latest record per active agent)", len(o.currentLedger.Positions))
		}
		if o.currentLedger.UpdatedAt != 0 {
			t.Errorf("UpdatedAt: got %v, want 0 (UpdateDryRun leaves UpdatedAt at zero for determinism)", o.currentLedger.UpdatedAt)
		}
	})

	t.Run("fail_non_dry_run_runner_takes_real_update_path_runner_invoked", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
		state := newLedgerTestState(cfg, 4, nil)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		ledgerJSON := twoAgentLedgerJSON(t, 1)
		runner := &recordingRunner{responses: []mockResponse{
			{content: "agent 0 turn 0"},
			{content: "agent 1 turn 1"},
			{content: ledgerJSON},
			{content: "agent 0 turn 2"},
			{content: "agent 1 turn 3"},
			{content: ledgerJSON},
		}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if o.currentLedger == nil {
			t.Fatal("currentLedger: got nil, want non-nil (real-path Update should set the ledger on JSON parse success)")
		}
		if got := countLedgerUpdaterCalls(runner.agents); got != 2 {
			t.Fatalf("ledger-updater calls: got %d, want 2 (real path calls Update once per completed round)", got)
		}
		if o.currentLedger.UpdatedAt == 0 {
			t.Errorf("UpdatedAt: got 0, want non-zero (Update stamps time.Now().Unix() after parsing)")
		}
	})
}

func TestLedgerDisableFlagSkipsUpdate(t *testing.T) {
	t.Run("pass_disabled_suppresses_updater_call_and_envelope_inject", func(t *testing.T) {
		disabled := false
		cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
		state := newLedgerTestState(cfg, 4, &disabled)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if state.HaltedBy != "max_turns (4)" {
			t.Fatalf("HaltedBy: got %q, want max_turns (4)", state.HaltedBy)
		}
		if o.currentLedger != nil {
			t.Fatalf("currentLedger: got %+v, want nil when --no-ledger active", o.currentLedger)
		}
		if got := countLedgerUpdaterCalls(runner.agents); got != 0 {
			t.Fatalf("ledger-updater calls: got %d, want 0 when --no-ledger active", got)
		}
		for i, env := range runner.envelopes {
			if _, has := env["ledger"]; has {
				t.Fatalf("envelope[%d] should not contain ledger when --no-ledger active", i)
			}
		}
	})

	t.Run("fail_default_enabled_triggers_updater_call_post_round", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
		state := newLedgerTestState(cfg, 4, nil)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		ledgerJSON := twoAgentLedgerJSON(t, 1)
		runner := &recordingRunner{responses: []mockResponse{
			{content: "agent 0 turn 0"},
			{content: "agent 1 turn 1"},
			{content: ledgerJSON},
			{content: "agent 0 turn 2"},
			{content: "agent 1 turn 3"},
			{content: ledgerJSON},
		}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		if o.currentLedger == nil {
			t.Fatal("currentLedger: got nil, want non-nil when ledger enabled (default)")
		}
		if got := countLedgerUpdaterCalls(runner.agents); got == 0 {
			t.Fatal("ledger-updater calls: got 0, want ≥1 when ledger enabled (default)")
		}
	})
}

func TestLedgerPersistedToTranscript(t *testing.T) {
	t.Run("pass_round_complete_appends_typed_ledger_record", func(t *testing.T) {
		cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
		state := newLedgerTestState(cfg, 2, nil)
		path := t.TempDir() + "/transcript.jsonl"
		tm := transcript.NewTranscriptManager(path)
		ledgerJSON := twoAgentLedgerJSON(t, 1)
		runner := &recordingRunner{responses: []mockResponse{
			{content: "agent 0 turn 0"},
			{content: "agent 1 turn 1"},
			{content: ledgerJSON},
		}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		records := tm.Records()
		var ledgerRec *types.TurnRecord
		var ledgerIdx int
		for i := range records {
			if records[i].AgentID == types.LedgerAgentID {
				ledgerRec = &records[i]
				ledgerIdx = i
				break
			}
		}
		if ledgerRec == nil {
			t.Fatalf("no ledger record appended; records: %#v", records)
		}
		if ledgerRec.Turn != types.LedgerSentinelTurn {
			t.Errorf("ledger record turn: got %d, want %d", ledgerRec.Turn, types.LedgerSentinelTurn)
		}
		if ledgerRec.Ledger == nil {
			t.Fatalf("ledger record missing Ledger payload: %#v", ledgerRec)
		}
		if ledgerRec.Ledger.Round != 1 || len(ledgerRec.Ledger.Positions) != 2 {
			t.Errorf("persisted ledger content: round=%d positions=%d, want round=1 positions=2", ledgerRec.Ledger.Round, len(ledgerRec.Ledger.Positions))
		}
		if err := ledgerRec.Ledger.Validate(); err != nil {
			t.Errorf("persisted ledger invalid: %v", err)
		}
		if ledgerRec.Content != "" {
			t.Errorf("ledger record content not empty: %q (persisted shape must be typed, not essay)", ledgerRec.Content)
		}
		var lastAgentTurnIdx int
		for i := len(records) - 1; i >= 0; i-- {
			if !types.IsInternalAgent(records[i].AgentID) {
				lastAgentTurnIdx = i
				break
			}
		}
		if ledgerIdx <= lastAgentTurnIdx {
			t.Fatalf("ledger record at index %d should follow last agent turn at index %d", ledgerIdx, lastAgentTurnIdx)
		}
		loaded, err := transcript.LoadFileStrict(path)
		if err != nil {
			t.Fatalf("strict reload of persisted ledger record: %v", err)
		}
		if len(loaded) != len(records) {
			t.Fatalf("strict reload: got %d records, want %d", len(loaded), len(records))
		}
	})

	t.Run("fail_disabled_appends_no_ledger_record", func(t *testing.T) {
		disabled := false
		cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
		state := newLedgerTestState(cfg, 2, &disabled)
		tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")
		runner := &recordingRunner{responses: []mockResponse{
			{content: "agent 0 turn 0"},
			{content: "agent 1 turn 1"},
		}}
		o := NewOrchestrator(state, tm, runner)
		o.SetLedgerUpdater(ledger.NewUpdater(runner))
		o.Run()

		for i, r := range tm.Records() {
			if r.AgentID == types.LedgerAgentID || r.Turn == types.LedgerSentinelTurn {
				t.Fatalf("disabled ledger must not persist a record; found at index %d: %#v", i, r)
			}
			if r.Ledger != nil {
				t.Fatalf("disabled ledger must not attach a Ledger payload; found at index %d: %#v", i, r)
			}
		}
		if o.currentLedger != nil {
			t.Fatalf("currentLedger: got %+v, want nil when ledger disabled", o.currentLedger)
		}
	})
}

func TestExecuteTurnSituationalFields(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_envelope_fields.jsonl")
	_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

	cfg := &types.DeliberationConfig{
		Agents:             newTestAgents(2),
		ConsensusThreshold: 3,
		MinRounds:          2,
	}
	state := newTestState(cfg)
	state.Turn = 2

	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	o := NewOrchestrator(state, tm, runner)

	_, ok := o.executeTurn(cfg.Agents[0])
	if !ok {
		t.Fatal("expected successful turn execution")
	}

	env := runner.envelopes[0]

	if agentID, ok := env["agent_id"].(string); !ok || agentID != "agent-0" {
		t.Errorf("agent_id: got %v, want \"agent-0\"", env["agent_id"])
	}

	if turn, ok := env["turn"].(int); !ok || turn != 2 {
		t.Errorf("turn: got %v, want 2", env["turn"])
	}

	if round, ok := env["round"].(int); !ok || round != 2 {
		t.Errorf("round: got %v, want 2", env["round"])
	}

	roster, ok := env["cast_roster"].([]map[string]string)
	if !ok || len(roster) != 2 {
		t.Fatalf("cast_roster: got %v, want 2 entries", env["cast_roster"])
	}
	for i, entry := range roster {
		expectedID := fmt.Sprintf("agent-%d", i)
		if entry["id"] != expectedID {
			t.Errorf("cast_roster[%d] id: got %q, want %q", i, entry["id"], expectedID)
		}
		if entry["name"] != expectedID {
			t.Errorf("cast_roster[%d] name: got %q, want %q (ID as fallback)", i, entry["name"], expectedID)
		}
	}

	budget, ok := env["remaining_budget"].(map[string]any)
	if !ok {
		t.Fatalf("remaining_budget: got %T, want map[string]any", env["remaining_budget"])
	}
	turnsRemaining, ok := budget["turns_remaining"].(int)
	if !ok || turnsRemaining != 8 {
		t.Errorf("turns_remaining: got %v, want 8", budget["turns_remaining"])
	}
	roundsRemaining, ok := budget["rounds_remaining"].(int)
	if !ok || roundsRemaining != 4 {
		t.Errorf("rounds_remaining: got %v, want 4", budget["rounds_remaining"])
	}
	if _, hasTime := budget["time_remaining_seconds"]; !hasTime {
		t.Error("expected time_remaining_seconds in remaining_budget when TimeLimit > 0")
	}
	if _, hasUncapped := budget["uncapped"]; hasUncapped {
		t.Error("uncapped should not be present when MaxTurns > 0 or TimeLimit > 0")
	}

	rule, ok := env["halting_rule"].(map[string]any)
	if !ok {
		t.Fatalf("halting_rule: got %T, want map[string]any", env["halting_rule"])
	}
	if ct, ok := rule["consensus_threshold"].(int); !ok || ct != 3 {
		t.Errorf("consensus_threshold: got %v, want 3", rule["consensus_threshold"])
	}
	if mr, ok := rule["min_rounds"].(int); !ok || mr != 2 {
		t.Errorf("min_rounds: got %v, want 2", rule["min_rounds"])
	}
	if mt, ok := rule["max_turns"].(int); !ok || mt != 10 {
		t.Errorf("max_turns: got %v, want 10", rule["max_turns"])
	}
	if tl, ok := rule["time_limit_seconds"].(int); !ok || tl != 30 {
		t.Errorf("time_limit_seconds: got %v, want 30", rule["time_limit_seconds"])
	}
	if _, hasBudgetCap := rule["budget_cap"]; !hasBudgetCap {
		t.Error("expected budget_cap in halting_rule")
	}
	if _, hasGate := rule["deliverable_gate"]; hasGate {
		t.Error("deliverable_gate should not be present when gate is nil")
	}

	if _, ok := env["topic"].(string); !ok {
		t.Error("existing key 'topic' should still be present")
	}
	if _, ok := env["history"]; !ok {
		t.Error("existing key 'history' should still be present")
	}
}

func TestRemainingBudgetTimeOnly(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_budget_time.jsonl")
	_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

	cfg := &types.DeliberationConfig{Agents: newTestAgents(1)}
	state := newTestState(cfg)
	state.MaxTurns = 0
	state.TimeLimit = 60

	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	o := NewOrchestrator(state, tm, runner)

	_, ok := o.executeTurn(cfg.Agents[0])
	if !ok {
		t.Fatal("expected successful turn execution")
	}

	budget := runner.envelopes[0]["remaining_budget"].(map[string]any)
	if _, hasTurns := budget["turns_remaining"]; hasTurns {
		t.Error("turns_remaining should not be present when MaxTurns == 0")
	}
	if _, hasRounds := budget["rounds_remaining"]; hasRounds {
		t.Error("rounds_remaining should not be present when MaxTurns == 0")
	}
	timeRemaining, ok := budget["time_remaining_seconds"].(float64)
	if !ok || timeRemaining <= 0 || timeRemaining > 60 {
		t.Errorf("time_remaining_seconds: got %v, want (0, 60]", budget["time_remaining_seconds"])
	}
	if _, hasUncapped := budget["uncapped"]; hasUncapped {
		t.Error("uncapped should not be present when TimeLimit > 0")
	}
}

func TestRemainingBudgetUncapped(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_budget_uncapped.jsonl")
	_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

	cfg := &types.DeliberationConfig{Agents: newTestAgents(1)}
	state := newTestState(cfg)
	state.MaxTurns = 0
	state.TimeLimit = 0

	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	o := NewOrchestrator(state, tm, runner)

	_, ok := o.executeTurn(cfg.Agents[0])
	if !ok {
		t.Fatal("expected successful turn execution")
	}

	budget := runner.envelopes[0]["remaining_budget"].(map[string]any)
	uncapped, ok := budget["uncapped"].(bool)
	if !ok || !uncapped {
		t.Errorf("uncapped: got %v, want true", budget["uncapped"])
	}
	if _, hasTurns := budget["turns_remaining"]; hasTurns {
		t.Error("turns_remaining should not be present when uncapped")
	}
	if _, hasRounds := budget["rounds_remaining"]; hasRounds {
		t.Error("rounds_remaining should not be present when uncapped")
	}
	if _, hasTime := budget["time_remaining_seconds"]; hasTime {
		t.Error("time_remaining_seconds should not be present when uncapped")
	}
}

func TestRemainingBudgetWithBudgetCap(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_budget_cap.jsonl")
	_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

	priorCost := 0.002
	_ = tm.Append(types.TurnRecord{
		Turn:    0,
		AgentID: "agent-0",
		Content: "prior turn",
		Cost:    &priorCost,
	})

	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := newTestState(cfg)
	state.Turn = 1

	budgetCap := 0.05
	state.Budget = &budgetCap

	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	o := NewOrchestrator(state, tm, runner)

	_, ok := o.executeTurn(cfg.Agents[1])
	if !ok {
		t.Fatal("expected successful turn execution")
	}

	budget := runner.envelopes[0]["remaining_budget"].(map[string]any)
	budgetRemaining, ok := budget["budget_remaining"].(float64)
	if !ok {
		t.Fatalf("budget_remaining: got %T, want float64", budget["budget_remaining"])
	}
	expected := 0.05 - 0.002
	if budgetRemaining < expected-1e-9 || budgetRemaining > expected+1e-9 {
		t.Errorf("budget_remaining: got %f, want %f", budgetRemaining, expected)
	}
}

func TestHaltingRuleDeliverableGate(t *testing.T) {
	tm := transcript.NewTranscriptManager("/tmp/test_deliverable_gate.jsonl")
	_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "moderator", Content: "seed"})

	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := newTestState(cfg)
	state.Topic = "The output must contain exactly three laws"
	state.DeliverableGate = ParseDeliverableGate(state.Topic)

	if state.DeliverableGate == nil || state.DeliverableGate.MinItems != 3 {
		t.Fatalf("setup: expected DeliverableGate with MinItems=3, got %+v", state.DeliverableGate)
	}

	runner := &recordingRunner{responses: []mockResponse{{content: "agent response"}}}
	o := NewOrchestrator(state, tm, runner)

	_, ok := o.executeTurn(cfg.Agents[0])
	if !ok {
		t.Fatal("expected successful turn execution")
	}

	rule := runner.envelopes[0]["halting_rule"].(map[string]any)
	gate, ok := rule["deliverable_gate"].(map[string]any)
	if !ok {
		t.Fatalf("deliverable_gate: got %T, want map[string]any", rule["deliverable_gate"])
	}
	if minItems, ok := gate["min_items"].(int); !ok || minItems != 3 {
		t.Errorf("min_items: got %v, want 3", gate["min_items"])
	}
}

func TestEnvelopeDryRunSituationalFields(t *testing.T) {
	cfg := &types.DeliberationConfig{Agents: newTestAgents(2)}
	state := &types.DeliberationState{
		Config:    cfg,
		Topic:     "dry-run test",
		Window:    2,
		MaxTurns:  4,
		TimeLimit: 0,
		Running:   true,
		StartTime: float64(time.Now().UnixNano()) / 1e9,
	}
	tm := transcript.NewTranscriptManager(t.TempDir() + "/transcript.jsonl")

	capture := &dryRunCaptureRunner{inner: agent.NewAgentRunner(true)}
	o := NewOrchestrator(state, tm, capture)
	o.Run()

	if len(capture.envelopes) != 4 {
		t.Fatalf("envelopes: got %d, want 4", len(capture.envelopes))
	}
	for i, env := range capture.envelopes {
		if _, ok := env["agent_id"].(string); !ok {
			t.Errorf("envelope[%d] agent_id: missing", i)
		}
		if turn, ok := env["turn"].(int); !ok {
			t.Errorf("envelope[%d] turn: missing", i)
		} else if turn != i {
			t.Errorf("envelope[%d] turn: got %d, want %d", i, turn, i)
		}
		if _, ok := env["round"].(int); !ok {
			t.Errorf("envelope[%d] round: missing", i)
		}
		if _, ok := env["cast_roster"].([]map[string]string); !ok {
			t.Errorf("envelope[%d] cast_roster: wrong type %T", i, env["cast_roster"])
		}
		budget, ok := env["remaining_budget"].(map[string]any)
		if !ok {
			t.Errorf("envelope[%d] remaining_budget: missing", i)
		} else if _, hasTurns := budget["turns_remaining"]; !hasTurns {
			t.Errorf("envelope[%d] turns_remaining: missing", i)
		} else if _, hasRounds := budget["rounds_remaining"]; !hasRounds {
			t.Errorf("envelope[%d] rounds_remaining: missing", i)
		}
		if _, ok := env["halting_rule"].(map[string]any); !ok {
			t.Errorf("envelope[%d] halting_rule: missing", i)
		}
	}
}
