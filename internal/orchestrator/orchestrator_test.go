package orchestrator

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

// mockRunner is a Runner whose Run method returns canned responses.
type mockRunner struct {
	content  string
	metadata map[string]any
	err      error
}

func (m *mockRunner) Run(agent types.AgentConfig, envelope map[string]any) (string, map[string]any, error) {
	if m.err != nil {
		return "", nil, m.err
	}
	return m.content, m.metadata, nil
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
		result := o.Synthesize()

		if result == nil {
			t.Fatal("expected non-nil result with 3 records")
		}
		if result["confidence"] != "medium" {
			t.Errorf("expected confidence=medium, got %v", result["confidence"])
		}
	})
}
