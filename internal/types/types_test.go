package types

import (
	"encoding/json"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// TurnRecord JSON round-trips
// ---------------------------------------------------------------------------

func TestTurnRecordJSONRoundTrip(t *testing.T) {
	total := 100
	input := 60
	output := 40
	cost := 0.001
	model := "openai/gpt-4"

	original := TurnRecord{
		Turn:               0,
		AgentID:            "skeptic",
		Model:              &model,
		Timestamp:          1715000000.0,
		Content:            "Hello world",
		Tokens:             TokenUsage{Total: &total, Input: &input, Output: &output},
		Cost:               &cost,
		Consensus:          true,
		ConsensusStatement: "We agree",
		Elapsed:            2.5,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TurnRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Turn != original.Turn {
		t.Errorf("turn: got %d, want %d", decoded.Turn, original.Turn)
	}
	if decoded.AgentID != original.AgentID {
		t.Errorf("agent_id: got %q, want %q", decoded.AgentID, original.AgentID)
	}
	if decoded.Content != original.Content {
		t.Errorf("content: got %q, want %q", decoded.Content, original.Content)
	}
	if decoded.Consensus != original.Consensus {
		t.Errorf("consensus: got %v, want %v", decoded.Consensus, original.Consensus)
	}
	if decoded.ConsensusStatement != original.ConsensusStatement {
		t.Errorf("consensus_statement: got %q, want %q", decoded.ConsensusStatement, original.ConsensusStatement)
	}
	if decoded.Elapsed != original.Elapsed {
		t.Errorf("elapsed: got %f, want %f", decoded.Elapsed, original.Elapsed)
	}
	if decoded.Model == nil || *decoded.Model != model {
		t.Errorf("model: got %v, want %q", decoded.Model, model)
	}

	if decoded.Tokens.Total == nil || *decoded.Tokens.Total != total {
		t.Errorf("tokens.total: got %v, want %d", decoded.Tokens.Total, total)
	}
	if decoded.Tokens.Input == nil || *decoded.Tokens.Input != input {
		t.Errorf("tokens.input: got %v, want %d", decoded.Tokens.Input, input)
	}
	if decoded.Tokens.Output == nil || *decoded.Tokens.Output != output {
		t.Errorf("tokens.output: got %v, want %d", decoded.Tokens.Output, output)
	}
	if decoded.Cost == nil || *decoded.Cost != cost {
		t.Errorf("cost: got %v, want %f", decoded.Cost, cost)
	}
}

// TestTurnRecordJSONRoundTripMinimal tests that records with nil/zero fields
// survive a marshal/unmarshal cycle without panicking or injecting unwanted values.
func TestTurnRecordJSONRoundTripMinimal(t *testing.T) {
	original := TurnRecord{
		Turn:      1,
		AgentID:   "test",
		Timestamp: 1715000010.0,
		Content:   "minimal",
		Elapsed:   0.0,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded TurnRecord
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if decoded.Turn != original.Turn {
		t.Errorf("turn: got %d, want %d", decoded.Turn, original.Turn)
	}
	if decoded.AgentID != original.AgentID {
		t.Errorf("agent_id mismatch")
	}
	// Model should remain nil (not zero-value string).
	if decoded.Model != nil {
		t.Errorf("model should be nil, got %v", decoded.Model)
	}
	// Consensus should remain false (not true).
	if decoded.Consensus != false {
		t.Errorf("consensus should be false, got %v", decoded.Consensus)
	}
}

// ---------------------------------------------------------------------------
// TokenUsage marshaling (nil vs zero)
// ---------------------------------------------------------------------------

func TestTokenUsageNilVsZero(t *testing.T) {
	t.Run("nil fields omitted", func(t *testing.T) {
		tu := TokenUsage{}
		data, err := json.Marshal(tu)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// With omitempty, nil *int fields should be omitted.
		str := string(data)
		if strings.Contains(str, `"total"`) {
			t.Errorf("unexpected 'total' in %s", str)
		}
		if strings.Contains(str, `"input"`) {
			t.Errorf("unexpected 'input' in %s", str)
		}
	})

	t.Run("zero values present", func(t *testing.T) {
		zero := 0
		tu := TokenUsage{Total: &zero}
		data, err := json.Marshal(tu)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		// With omitempty on a pointer to 0, Go's omitempty treats pointer-to-zero
		// as non-empty (because the pointer itself is non-nil), so it IS included.
		// This is the behavior we want for semantic parity.
		if !strings.Contains(string(data), `"total"`) {
			t.Errorf("zero-valued *int should be present in JSON: %s", data)
		}
	})

	t.Run("non-zero values present", func(t *testing.T) {
		n := 42
		tu := TokenUsage{Total: &n}
		data, err := json.Marshal(tu)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(data), `"total":42`) {
			t.Errorf("expected total:42 in %s", data)
		}
	})
}

// ---------------------------------------------------------------------------
// Topology parsing
// ---------------------------------------------------------------------------

func TestParseTopology(t *testing.T) {
	tests := []struct {
		input   string
		want    Topology
		wantErr bool
	}{
		{"ring", TopologyRing, false},
		{"RING", TopologyRing, false},
		{"Ring", TopologyRing, false},
		{"star", TopologyStar, false},
		{"STAR", TopologyStar, false},
		{"mesh", TopologyMesh, false},
		{"MESH", TopologyMesh, false},
		{"mesh-network", "", true}, // hyphens not valid after normalization, "mesh_network" is unknown
		{"invalid", "", true},
		{"", "", true},
		{"random", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseTopology(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for %q: %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// AgentConfig validation
// ---------------------------------------------------------------------------

func TestAgentConfigValidate(t *testing.T) {
	t.Run("valid agent", func(t *testing.T) {
		a := AgentConfig{ID: "agent1", Model: "openai/gpt-4"}
		if err := a.Validate(); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("empty id", func(t *testing.T) {
		a := AgentConfig{ID: "", Model: "m"}
		err := a.Validate()
		if err == nil {
			t.Fatal("expected error for empty id")
		}
		if !strings.Contains(err.Error(), "non-empty 'id'") {
			t.Errorf("error should mention 'id', got %q", err.Error())
		}
	})

	t.Run("empty model", func(t *testing.T) {
		a := AgentConfig{ID: "a", Model: ""}
		err := a.Validate()
		if err == nil {
			t.Fatal("expected error for empty model")
		}
		if !strings.Contains(err.Error(), "non-empty 'model'") {
			t.Errorf("error should mention 'model', got %q", err.Error())
		}
	})

	t.Run("both empty", func(t *testing.T) {
		a := AgentConfig{ID: "", Model: ""}
		err := a.Validate()
		if err == nil {
			t.Fatal("expected error for both empty")
		}
		// First check should be for id.
		if !strings.Contains(err.Error(), "non-empty 'id'") {
			t.Errorf("error should mention 'id', got %q", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// DeliberationConfig validation
// ---------------------------------------------------------------------------

func TestDeliberationConfigValidate(t *testing.T) {
	t.Run("valid config", func(t *testing.T) {
		cfg := &DeliberationConfig{
			Agents:   []AgentConfig{{ID: "a", Model: "m"}},
			Topology: TopologyRing,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("unexpected error: %v", err)
		}
	})

	t.Run("no agents", func(t *testing.T) {
		cfg := &DeliberationConfig{
			Agents:   []AgentConfig{},
			Topology: TopologyRing,
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "at least one agent") {
			t.Errorf("error mismatch: %q", err.Error())
		}
	})

	t.Run("duplicate ids", func(t *testing.T) {
		cfg := &DeliberationConfig{
			Agents: []AgentConfig{
				{ID: "dup", Model: "m1"},
				{ID: "dup", Model: "m2"},
			},
			Topology: TopologyRing,
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "duplicate agent id") {
			t.Errorf("error mismatch: %q", err.Error())
		}
	})

	t.Run("missing id", func(t *testing.T) {
		cfg := &DeliberationConfig{
			Agents:   []AgentConfig{{ID: "", Model: "m"}},
			Topology: TopologyRing,
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "non-empty 'id'") {
			t.Errorf("error mismatch: %q", err.Error())
		}
	})

	t.Run("missing model", func(t *testing.T) {
		cfg := &DeliberationConfig{
			Agents:   []AgentConfig{{ID: "a", Model: ""}},
			Topology: TopologyRing,
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "non-empty 'model'") {
			t.Errorf("error mismatch: %q", err.Error())
		}
	})

	t.Run("negative consensus threshold", func(t *testing.T) {
		cfg := &DeliberationConfig{
			Agents:             []AgentConfig{{ID: "a", Model: "m"}},
			Topology:           TopologyRing,
			ConsensusThreshold: -1,
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "consensus_threshold") {
			t.Errorf("error mismatch: %q", err.Error())
		}
	})

	t.Run("invalid topology", func(t *testing.T) {
		cfg := &DeliberationConfig{
			Agents:   []AgentConfig{{ID: "a", Model: "m"}},
			Topology: Topology("bogus"),
		}
		err := cfg.Validate()
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "unknown topology") {
			t.Errorf("error mismatch: %q", err.Error())
		}
	})

	t.Run("empty topology defaults ok during loading (but not Validate)", func(t *testing.T) {
		// Empty topology passes Validate (it gets defaulted during LoadConfig).
		// This ensures we don't reject it preemptively.
		cfg := &DeliberationConfig{
			Agents:   []AgentConfig{{ID: "a", Model: "m"}},
			Topology: Topology(""),
		}
		err := cfg.Validate()
		if err != nil {
			t.Errorf("empty topology should pass Validate (defaulted in LoadConfig): %v", err)
		}
	})

	t.Run("zero consensus threshold is ok", func(t *testing.T) {
		cfg := &DeliberationConfig{
			Agents:             []AgentConfig{{ID: "a", Model: "m"}},
			Topology:           TopologyRing,
			ConsensusThreshold: 0,
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("zero consensus_threshold should be ok: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// ComputeStats
// ---------------------------------------------------------------------------

func TestComputeStats(t *testing.T) {
	total100 := 100
	total200 := 200
	total50 := 50
	cost001 := 0.001
	cost002 := 0.002
	cost0005 := 0.0005

	records := []TurnRecord{
		{Turn: -1, AgentID: "orchestrator", Content: "seed", Elapsed: 0.1},
		{Turn: 0, AgentID: "a", Content: "x", Tokens: TokenUsage{Total: &total100}, Cost: &cost001, Elapsed: 1.0},
		{Turn: 1, AgentID: "b", Content: "x", Tokens: TokenUsage{Total: &total200}, Cost: &cost002, Elapsed: 1.5},
		{Turn: 2, AgentID: "a", Content: "x", Tokens: TokenUsage{Total: &total50}, Cost: &cost0005, Consensus: true, ConsensusStatement: "ok", Elapsed: 0.8},
	}

	stats := ComputeStats(records)

	if stats.TotalTurns != 4 {
		t.Errorf("total_turns: got %d, want 4", stats.TotalTurns)
	}
	if stats.TotalTokens != 350 {
		t.Errorf("total_tokens: got %d, want 350", stats.TotalTokens)
	}
	if stats.TotalCost != 0.0035 {
		t.Errorf("total_cost: got %f, want 0.0035", stats.TotalCost)
	}

	// Average turn duration should be based on non-zero elapsed values.
	if stats.AvgTurnDuration <= 0 {
		t.Errorf("avg_turn_duration should be > 0, got %f", stats.AvgTurnDuration)
	}

	if len(stats.ConsensusEvents) != 1 {
		t.Errorf("consensus events: got %d, want 1", len(stats.ConsensusEvents))
	}
	if len(stats.ConsensusEvents) > 0 && stats.ConsensusEvents[0].Statement != "ok" {
		t.Errorf("consensus statement: got %q, want %q", stats.ConsensusEvents[0].Statement, "ok")
	}

	// Per-agent stats.
	if pa, ok := stats.PerAgent["a"]; ok {
		if pa.Turns != 2 {
			t.Errorf("agent a turns: got %d, want 2", pa.Turns)
		}
		if pa.Tokens != 150 {
			t.Errorf("agent a tokens: got %d, want 150", pa.Tokens)
		}
	} else {
		t.Error("missing per-agent stats for 'a'")
	}

	if pa, ok := stats.PerAgent["b"]; ok {
		if pa.Turns != 1 {
			t.Errorf("agent b turns: got %d, want 1", pa.Turns)
		}
	} else {
		t.Error("missing per-agent stats for 'b'")
	}
}

// ---------------------------------------------------------------------------
// AutoLevel parsing
// ---------------------------------------------------------------------------

func TestParseAutoLevel(t *testing.T) {
	tests := []struct {
		input   string
		want    AutoLevel
		wantErr bool
	}{
		{"quick", AutoQuick, false},
		{"normal", AutoNormal, false},
		{"deep", AutoDeep, false},
		{"yolo", AutoYOLO, false},
		{"off", AutoOff, false},
		{"QUICK", AutoQuick, false},
		{"Quick", AutoQuick, false},
		{"turbo", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseAutoLevel(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Errorf("expected error for %q, got nil", tt.input)
				}
				if err != nil && !strings.Contains(err.Error(), "off") {
					t.Errorf("error should list valid levels, got %q", err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for %q: %v", tt.input, err)
				}
				if got != tt.want {
					t.Errorf("got %q, want %q", got, tt.want)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// LevelCaps per auto level
// ---------------------------------------------------------------------------

func TestCapsForLevel(t *testing.T) {
	tests := []struct {
		level AutoLevel
		want  LevelCaps
	}{
		{AutoQuick, LevelCaps{MaxAgents: 2, MaxTurns: 4, TimeLimit: 60}},
		{AutoNormal, LevelCaps{MaxAgents: 4, MaxTurns: 10, TimeLimit: 300}},
		{AutoDeep, LevelCaps{MaxAgents: 8, MaxTurns: 20, TimeLimit: 900}},
		{AutoYOLO, LevelCaps{MaxAgents: 0, MaxTurns: 0, TimeLimit: 0}},
		{AutoOff, LevelCaps{MaxAgents: 0, MaxTurns: 0, TimeLimit: 0}},
	}

	for _, tt := range tests {
		t.Run(string(tt.level), func(t *testing.T) {
			got := CapsForLevel(tt.level)
			if got != tt.want {
				t.Errorf("CapsForLevel(%q) = %+v, want %+v", tt.level, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// IntVal helper
// ---------------------------------------------------------------------------

func TestIntVal(t *testing.T) {
	if got := IntVal(nil); got != 0 {
		t.Errorf("IntVal(nil) = %d, want 0", got)
	}
	n := 42
	if got := IntVal(&n); got != 42 {
		t.Errorf("IntVal(&42) = %d, want 42", got)
	}
}

// ---------------------------------------------------------------------------
// JSON marshal parity — Go-marshaled TurnRecord key names must match Python
// ---------------------------------------------------------------------------

func TestTurnRecordJSONKeysMatchPython(t *testing.T) {
	// These are the JSON key names expected by the Python side based on
	// participants.jsonl and the Python TurnRecord model.
	expectedKeys := []string{
		"turn", "agent_id", "model", "timestamp", "content",
		"tokens", "cost", "consensus", "consensus_statement", "elapsed",
	}

	model := "m"
	cost := 0.001
	record := TurnRecord{
		Turn:      0,
		AgentID:   "test",
		Model:     &model,
		Timestamp: 1.0,
		Content:   "hello",
		Cost:      &cost,
		Elapsed:   1.0,
	}

	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	for _, key := range expectedKeys {
		if _, ok := m[key]; !ok {
			t.Errorf("missing key %q in JSON: %s", key, data)
		}
	}
}
