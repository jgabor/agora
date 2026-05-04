package kumbaja

import (
	"fmt"
	"testing"
	"time"
)

// mockRunner is a Runner whose Run method returns canned responses.
type mockRunner struct {
	content  string
	metadata map[string]any
	err      error
}

func (m *mockRunner) Run(agent AgentConfig, envelope map[string]any) (string, map[string]any, error) {
	if m.err != nil {
		return "", nil, m.err
	}
	return m.content, m.metadata, nil
}

func newTestState(cfg *DeliberationConfig) *DeliberationState {
	return &DeliberationState{
		Config:    cfg,
		Topic:     "test topic",
		Window:    2,
		MaxTurns:  10,
		TimeLimit: 30,
		Running:   true,
		StartTime: float64(time.Now().UnixNano()) / 1e9,
	}
}

func newTestAgents(n int) []AgentConfig {
	agents := make([]AgentConfig, n)
	for i := range n {
		agents[i] = AgentConfig{
			ID:           fmt.Sprintf("agent-%d", i),
			Model:        "test-model",
			SystemPrompt: "You are a test agent.",
		}
	}
	return agents
}

func TestEmitSeed(t *testing.T) {
	tm := NewTranscriptManager("/tmp/test_transcript.jsonl")
	state := newTestState(&DeliberationConfig{Agents: newTestAgents(2)})
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
			tm := NewTranscriptManager("/tmp/test_transcript.jsonl")
			cfg := &DeliberationConfig{
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
				_ = tm.Append(TurnRecord{
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
	tm := NewTranscriptManager("/tmp/test_transcript.jsonl")
	_ = tm.Append(TurnRecord{
		Turn:    -1,
		AgentID: "orchestrator",
		Content: "seed message",
	})

	cfg := &DeliberationConfig{Agents: newTestAgents(2)}
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
	record := o.executeTurn(cfg.Agents[0])

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
	tm := NewTranscriptManager("/tmp/test_transcript.jsonl")
	cfg := &DeliberationConfig{Agents: newTestAgents(2)}
	state := newTestState(cfg)

	mock := &mockRunner{
		err: fmt.Errorf("opencode crashed"),
	}

	o := NewOrchestrator(state, tm, mock)
	record := o.executeTurn(cfg.Agents[0])

	if o.state.Running {
		t.Error("expected Running=false after runner error")
	}
	if o.state.HaltedBy != "error: opencode crashed" {
		t.Errorf("expected halt reason, got %q", o.state.HaltedBy)
	}
	if record.Content != "[ERROR] opencode crashed" {
		t.Errorf("expected error content, got %q", record.Content)
	}
}

func floatPtr(f float64) *float64 {
	return &f
}
