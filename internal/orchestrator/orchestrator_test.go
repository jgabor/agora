package orchestrator

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

// mockRunner is a Runner whose Run method returns canned responses.
type mockRunner struct {
	content  string
	metadata map[string]any
	err      error
	onRun    func()
	agent    types.AgentConfig
	envelope map[string]any
}

func (m *mockRunner) Run(agent types.AgentConfig, envelope map[string]any) (string, map[string]any, error) {
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
	if r.AgentID != "orchestrator" {
		t.Errorf("expected orchestrator, got %s", r.AgentID)
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
		if record.AgentID != "orchestrator" && record.Evidence != nil {
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
	o.SetEvidenceCollector(NewPolicyEvidenceCollector(runner))
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
	o.SetEvidenceCollector(NewPolicyEvidenceCollector(runner))
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
		AgentID: "orchestrator",
		Content: "prior research summary",
		Evidence: &types.EvidenceBundle{SourceReferences: []types.SourceReference{{
			Title: "prior source",
			URL:   "https://example.com/prior",
		}}},
	}); err != nil {
		t.Fatalf("append prior evidence: %v", err)
	}
	if err := tm.Append(types.TurnRecord{Turn: -1, AgentID: "orchestrator", Content: "seed"}); err != nil {
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
		AgentID: "orchestrator",
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
		metadata: map[string]any{
			"tokens": map[string]any{
				"total":  total,
				"input":  input,
				"output": output,
			},
			"cost": &cost,
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
	if !strings.HasPrefix(mock.agent.SystemPrompt, agent.ReadOnlyFilesystemInstruction) {
		t.Fatalf("runner prompt = %q, want read-only guard", mock.agent.SystemPrompt)
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

func TestTurnTokens(t *testing.T) {
	total := 42
	input := 20
	output := 18
	reasoning := 4

	tests := []struct {
		name     string
		metadata map[string]any
		want     types.TokenUsage
	}{
		{
			name: "all token fields",
			metadata: map[string]any{
				"tokens": map[string]any{
					"total":     total,
					"input":     input,
					"output":    output,
					"reasoning": reasoning,
				},
			},
			want: types.TokenUsage{Total: &total, Input: &input, Output: &output, Reasoning: &reasoning},
		},
		{
			name:     "missing tokens",
			metadata: map[string]any{},
			want:     types.TokenUsage{},
		},
		{
			name: "non-int token ignored",
			metadata: map[string]any{
				"tokens": map[string]any{"total": 42.0},
			},
			want: types.TokenUsage{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := turnTokens(tt.metadata); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("turnTokens() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestTurnCost(t *testing.T) {
	cost := 0.005
	tests := []struct {
		name     string
		metadata map[string]any
		want     *float64
	}{
		{name: "float pointer", metadata: map[string]any{"cost": &cost}, want: &cost},
		{name: "missing cost", metadata: map[string]any{}, want: nil},
		{name: "float value ignored", metadata: map[string]any{"cost": cost}, want: nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := turnCost(tt.metadata)
			if tt.want == nil {
				if got != nil {
					t.Errorf("turnCost() = %v, want nil", *got)
				}
				return
			}
			if got == nil || *got != *tt.want {
				t.Errorf("turnCost() = %v, want %v", got, tt.want)
			}
		})
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
		{content: "second turn succeeds", metadata: map[string]any{}},
		{content: "third turn succeeds", metadata: map[string]any{}},
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
	if stats.TotalTurns != 3 {
		t.Errorf("expected seed plus 2 successful records, got %d", stats.TotalTurns)
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
	metadata map[string]any
	err      error
}

func (s *seqMockRunner) Run(agent types.AgentConfig, envelope map[string]any) (string, map[string]any, error) {
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

	noConsensus := mockResponse{content: "I disagree.", metadata: map[string]any{}}
	withConsensus := mockResponse{content: "[CONSENSUS: we agree] Agreed.", metadata: map[string]any{}}

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
			{content: "No consensus here.", metadata: map[string]any{}},
		},
	}

	o := NewOrchestrator(state, tm, runner)
	stats := o.Run()

	if state.HaltedBy != "max_turns (10)" {
		t.Errorf("expected halt reason 'max_turns (10)', got %q", state.HaltedBy)
	}
	// 10 agent turns + 1 seed record = 11 total records.
	if stats.TotalTurns != 11 {
		t.Errorf("expected 11 total records (10 agent + 1 seed), got %d", stats.TotalTurns)
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
		responses[i] = mockResponse{content: "Still going.", metadata: map[string]any{}}
	}
	responses[14] = mockResponse{content: "[CONSENSUS: done] Done.", metadata: map[string]any{}}

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

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    map[string]any
		wantErr bool
	}{
		{
			name:  "json code block",
			input: "```json\n{\"key_arguments\": [\"arg1\"]}\n```",
			want:  map[string]any{"key_arguments": []any{"arg1"}},
		},
		{
			name:  "plain code block",
			input: "```\n{\"key_arguments\": [\"arg1\"]}\n```",
			want:  map[string]any{"key_arguments": []any{"arg1"}},
		},
		{
			name:  "raw json",
			input: `{"key_arguments": ["arg1"]}`,
			want:  map[string]any{"key_arguments": []any{"arg1"}},
		},
		{
			name:    "no json found",
			input:   "just text without json",
			wantErr: true,
		},
		{
			name:    "empty input",
			input:   "",
			wantErr: true,
		},
		{
			name:    "malformed json",
			input:   `{"key_arguments": broken}`,
			wantErr: true,
		},
		{
			name:  "multiple blocks picks first",
			input: "```json\n{\"first\": true}\n```\n```json\n{\"second\": true}\n```",
			want:  map[string]any{"first": true},
		},
		{
			name:  "json in code block with text",
			input: "Here is the result:\n```json\n{\"confidence\": \"high\"}\n```\nDone.",
			want:  map[string]any{"confidence": "high"},
		},
	}

	se := &SynthesisEngine{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := se.extractJSON(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestFormatTranscript(t *testing.T) {
	records := []types.TurnRecord{
		{Turn: -1, AgentID: "orchestrator", Content: "Begin topic: test"},
		{Turn: 0, AgentID: "agent-0", Content: "I think X is correct."},
		{Turn: 1, AgentID: "agent-1", Content: "I disagree because Y."},
	}

	se := &SynthesisEngine{}
	result := se.formatTranscript(records)
	expected := "[Turn -1] orchestrator: Begin topic: test\n[Turn 0] agent-0: I think X is correct.\n[Turn 1] agent-1: I disagree because Y."
	if result != expected {
		t.Errorf("expected:\n%s\n\ngot:\n%s", expected, result)
	}
}

func TestSynthesisEngineSynthesize(t *testing.T) {
	records := []types.TurnRecord{
		{Turn: -1, AgentID: "orchestrator", Content: "seed"},
		{Turn: 0, AgentID: "agent-0", Content: "proposal"},
		{Turn: 1, AgentID: "agent-1", Content: "critique"},
	}

	cfg := &types.DeliberationConfig{
		Agents: newTestAgents(2),
	}

	t.Run("successful synthesis", func(t *testing.T) {
		mock := &mockRunner{
			content: "```json\n{\"key_arguments\":[\"arg1\",\"arg2\"],\"points_of_agreement\":[\"point1\"],\"unresolved_tensions\":[\"tension1\"],\"recommended_decision\":\"go with option A\",\"confidence\":\"high\"}\n```",
		}
		engine := NewSynthesisEngine(mock)
		result := engine.Synthesize(records, "test topic", cfg)

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result["confidence"] != "high" {
			t.Errorf("expected confidence=high, got %v", result["confidence"])
		}
		if result["recommended_decision"] != "go with option A" {
			t.Errorf("expected recommended_decision, got %v", result["recommended_decision"])
		}
		if !strings.HasPrefix(mock.agent.SystemPrompt, agent.ReadOnlyFilesystemInstruction) {
			t.Fatalf("synthesis prompt = %q, want read-only guard", mock.agent.SystemPrompt)
		}
	})

	t.Run("runner error", func(t *testing.T) {
		mock := &mockRunner{err: fmt.Errorf("LLM unavailable")}
		engine := NewSynthesisEngine(mock)
		result := engine.Synthesize(records, "test topic", cfg)

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result["confidence"] != "low" {
			t.Errorf("expected confidence=low on error, got %v", result["confidence"])
		}
		if !strings.Contains(result["recommended_decision"].(string), "Synthesis failed") {
			t.Errorf("expected error message in recommendation, got %v", result["recommended_decision"])
		}
	})

	t.Run("invalid json response", func(t *testing.T) {
		mock := &mockRunner{
			content: "This is not valid JSON and has no code block.",
		}
		engine := NewSynthesisEngine(mock)
		result := engine.Synthesize(records, "test topic", cfg)

		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result["confidence"] != "low" {
			t.Errorf("expected confidence=low on invalid JSON, got %v", result["confidence"])
		}
	})

	t.Run("uses synthesis model when configured", func(t *testing.T) {
		model := "gpt-4"
		cfgWithSynth := &types.DeliberationConfig{
			Agents:         newTestAgents(2),
			SynthesisModel: &model,
		}
		mock := &mockRunner{
			content: "```json\n{\"confidence\":\"high\",\"recommended_decision\":\"use gpt-4\",\"key_arguments\":[],\"points_of_agreement\":[],\"unresolved_tensions\":[]}\n```",
		}
		engine := NewSynthesisEngine(mock)
		result := engine.Synthesize(records, "test topic", cfgWithSynth)

		if result == nil {
			t.Fatal("expected non-nil result")
		}
	})
}

func TestOrchestratorSynthesize(t *testing.T) {
	t.Run("returns nil with one record or fewer", func(t *testing.T) {
		tm := transcript.NewTranscriptManager("/tmp/test_synth.jsonl")
		_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "orchestrator", Content: "seed"})

		state := newTestState(&types.DeliberationConfig{Agents: newTestAgents(2)})
		o := NewOrchestrator(state, tm, &mockRunner{})
		result := o.Synthesize()
		if result != nil {
			t.Error("expected nil with 1 record")
		}
	})

	t.Run("returns synthesis with multiple records", func(t *testing.T) {
		tm := transcript.NewTranscriptManager("/tmp/test_synth.jsonl")
		_ = tm.Append(types.TurnRecord{Turn: -1, AgentID: "orchestrator", Content: "seed"})
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
