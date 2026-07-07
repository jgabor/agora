package types

import (
	"encoding/json"
	"reflect"
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
		{Turn: -1, AgentID: "moderator", Content: "seed", Elapsed: 0.1},
		{Turn: 0, AgentID: "a", Content: "x", Tokens: TokenUsage{Total: &total100}, Cost: &cost001, Elapsed: 1.0},
		{Turn: 1, AgentID: "b", Content: "x", Tokens: TokenUsage{Total: &total200}, Cost: &cost002, Elapsed: 1.5},
		{Turn: 2, AgentID: "a", Content: "x", Tokens: TokenUsage{Total: &total50}, Cost: &cost0005, Consensus: true, ConsensusStatement: "ok", Elapsed: 0.8},
	}

	stats := ComputeStats(records)

	if stats.TotalTurns != 3 {
		t.Errorf("total_turns: got %d, want 3 agent turns", stats.TotalTurns)
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
// TurnRecord JSON wire format
// ---------------------------------------------------------------------------

func TestTurnRecordJSONMarshalUsesCanonicalKeys(t *testing.T) {
	expectedKeys := []string{
		"turn", "agent_id", "model", "timestamp", "content",
		"tokens", "cost", "consensus", "consensus_statement", "elapsed",
	}

	total := 100
	model := "m"
	cost := 0.001
	record := TurnRecord{
		Turn:               0,
		AgentID:            "test",
		Model:              &model,
		Timestamp:          1.0,
		Content:            "hello",
		Tokens:             TokenUsage{Total: &total},
		Cost:               &cost,
		Consensus:          true,
		ConsensusStatement: "agreed",
		Elapsed:            1.0,
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

	tokens, ok := m["tokens"].(map[string]any)
	if !ok {
		t.Fatal("tokens is not an object")
	}
	if _, ok := tokens["total"]; !ok {
		t.Error("missing tokens.total")
	}
}

// ---------------------------------------------------------------------------
// DebateLedger JSON round-trip
// ---------------------------------------------------------------------------

func TestDebateLedgerJSONRoundTrip(t *testing.T) {
	t.Run("populated ledger round-trips exactly", func(t *testing.T) {
		original := &DebateLedger{
			Round:     2,
			UpdatedAt: 1715000000.0,
			Positions: []AgentPosition{
				{AgentID: "skeptic", Text: "The cost model is unbounded", Turn: 3},
				{AgentID: "optimist", Text: "Caps already bound it", Turn: 4},
			},
			Agreements: []AgreementPoint{
				{Text: "Caps exist", Endorsers: []string{"skeptic", "optimist"}},
			},
			Cruxes: []OpenCrux{
				{
					Topic:    "Cap enforcement",
					RaisedAt: 3,
					Views: []PositionalView{
						{AgentID: "skeptic", Stance: "soft caps let runs abort early"},
						{AgentID: "optimist", Stance: "soft caps are the right default"},
					},
				},
			},
			Draft: DraftProposal{
				Status:    DraftStatusDraft,
				Text:      "Adopt auto-level fallback caps with soft overflow.",
				Proposer:  "optimist",
				Endorsers: []string{"optimist", "skeptic"},
			},
		}

		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}

		if !strings.Contains(string(data), `"draft":{"status":"draft"`) {
			t.Errorf("draft status should always serialize, got %s", data)
		}

		var decoded DebateLedger
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}

		if !reflect.DeepEqual(*original, decoded) {
			t.Errorf("round-trip mismatch\noriginal: %+v\ndecoded:  %+v", *original, decoded)
		}
	})

	t.Run("malformed draft status fails validation after unmarshal", func(t *testing.T) {
		raw := `{"round":1,"positions":[],"agreements":[],"cruxes":[],"draft":{"status":"bogus"},"updated_at":1.0}`
		var decoded DebateLedger
		if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
			t.Fatalf("unmarshal should succeed (status is a free string): %v", err)
		}
		err := decoded.Validate()
		if err == nil {
			t.Fatal("expected validation error for bogus draft status, got nil")
		}
		if !strings.Contains(err.Error(), "draft status") {
			t.Errorf("error should mention 'draft status', got %q", err.Error())
		}
	})

	t.Run("no-draft ledger round-trips with explicit status", func(t *testing.T) {
		original := NewDebateLedger(0, 1.0)
		data, err := json.Marshal(original)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		if !strings.Contains(string(data), `"draft":{"status":"none"}`) {
			t.Errorf("no-draft should serialize as status:none, got %s", data)
		}
		var decoded DebateLedger
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if decoded.Draft.Status != DraftStatusNone {
			t.Errorf("round-trip no-draft status: got %q, want %q", decoded.Draft.Status, DraftStatusNone)
		}
	})
}

// ---------------------------------------------------------------------------
// DebateLedger no-draft report
// ---------------------------------------------------------------------------

func TestDebateLedgerNoDraftReport(t *testing.T) {
	t.Run("no-draft ledger reports no endorsable proposal", func(t *testing.T) {
		l := NewDebateLedger(1, 1715000000.0)
		if l.HasEndorsableProposal() {
			t.Errorf("no-draft ledger should not report an endorsable proposal")
		}
		if l.Draft.Status != DraftStatusNone {
			t.Errorf("draft status: got %q, want %q", l.Draft.Status, DraftStatusNone)
		}
	})

	t.Run("incomplete draft does not report endorsable", func(t *testing.T) {
		l := &DebateLedger{
			Round: 1,
			Draft: DraftProposal{Status: DraftStatusDraft, Text: ""},
		}
		if l.HasEndorsableProposal() {
			t.Errorf("draft with status but no text should not be endorsable")
		}
	})

	t.Run("nil ledger receiver reports no endorsable proposal", func(t *testing.T) {
		var l *DebateLedger
		if l.HasEndorsableProposal() {
			t.Errorf("nil ledger should not report an endorsable proposal")
		}
	})
}

func TestDraftProposalHasEndorsableProposal(t *testing.T) {
	tests := []struct {
		name string
		d    *DraftProposal
		want bool
	}{
		{"none status", &DraftProposal{Status: DraftStatusNone}, false},
		{"empty status lenient as none", &DraftProposal{Status: ""}, false},
		{"draft with text", &DraftProposal{Status: DraftStatusDraft, Text: "t"}, true},
		{"final with text", &DraftProposal{Status: DraftStatusFinal, Text: "t"}, true},
		{"draft without text", &DraftProposal{Status: DraftStatusDraft, Text: ""}, false},
		{"final without text", &DraftProposal{Status: DraftStatusFinal, Text: ""}, false},
		{"nil receiver", nil, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.HasEndorsableProposal(); got != tt.want {
				t.Errorf("HasEndorsableProposal() = %v, want %v", got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// DebateLedger empty state
// ---------------------------------------------------------------------------

func TestDebateLedgerEmptyState(t *testing.T) {
	t.Run("empty ledger from constructor is valid", func(t *testing.T) {
		l := NewDebateLedger(0, 0.0)
		if err := l.Validate(); err != nil {
			t.Errorf("empty ledger should validate, got %v", err)
		}
		if l.HasEndorsableProposal() {
			t.Errorf("empty ledger should not report endorsable proposal")
		}
		if len(l.Positions) != 0 || len(l.Agreements) != 0 || len(l.Cruxes) != 0 {
			t.Errorf("empty ledger should have empty slices")
		}
	})

	t.Run("empty ledger with negative round fails validation", func(t *testing.T) {
		l := &DebateLedger{
			Round: -5,
			Draft: DraftProposal{Status: DraftStatusNone},
		}
		err := l.Validate()
		if err == nil {
			t.Fatal("expected error for negative round")
		}
		if !strings.Contains(err.Error(), "round") {
			t.Errorf("error should mention 'round', got %q", err.Error())
		}
	})

	t.Run("nil ledger fails validation", func(t *testing.T) {
		var l *DebateLedger
		err := l.Validate()
		if err == nil {
			t.Fatal("expected error for nil ledger")
		}
		if !strings.Contains(err.Error(), "nil") {
			t.Errorf("error should mention 'nil', got %q", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// DebateLedger Validate (table-driven)
// ---------------------------------------------------------------------------

func TestDebateLedgerValidate(t *testing.T) {
	tests := []struct {
		name    string
		ledger  *DebateLedger
		wantErr string
	}{
		{
			name:    "valid populated ledger",
			ledger:  &DebateLedger{Round: 1, Draft: DraftProposal{Status: DraftStatusNone}},
			wantErr: "",
		},
		{
			name: "valid with positions and agreements",
			ledger: &DebateLedger{
				Round: 1,
				Positions: []AgentPosition{
					{AgentID: "a", Text: "pa", Turn: 1},
					{AgentID: "b", Text: "pb", Turn: 2},
				},
				Agreements: []AgreementPoint{{Text: "agree", Endorsers: []string{"a", "b"}}},
				Draft:      DraftProposal{Status: DraftStatusNone},
			},
			wantErr: "",
		},
		{
			name: "duplicate position agent_id",
			ledger: &DebateLedger{
				Round: 1,
				Positions: []AgentPosition{
					{AgentID: "a", Text: "pa", Turn: 1},
					{AgentID: "a", Text: "pa2", Turn: 2},
				},
				Draft: DraftProposal{Status: DraftStatusNone},
			},
			wantErr: "duplicate agent_id",
		},
		{
			name: "empty position agent_id",
			ledger: &DebateLedger{
				Round:     1,
				Positions: []AgentPosition{{AgentID: "", Text: "x", Turn: 1}},
				Draft:     DraftProposal{Status: DraftStatusNone},
			},
			wantErr: "agent_id must be non-empty",
		},
		{
			name: "empty agreement text",
			ledger: &DebateLedger{
				Round:      1,
				Agreements: []AgreementPoint{{Text: "", Endorsers: []string{"a"}}},
				Draft:      DraftProposal{Status: DraftStatusNone},
			},
			wantErr: "text must be non-empty",
		},
		{
			name: "empty crux topic",
			ledger: &DebateLedger{
				Round:  1,
				Cruxes: []OpenCrux{{Topic: "", RaisedAt: 1}},
				Draft:  DraftProposal{Status: DraftStatusNone},
			},
			wantErr: "topic must be non-empty",
		},
		{
			name: "bogus draft status",
			ledger: &DebateLedger{
				Round: 1,
				Draft: DraftProposal{Status: DraftStatus("bogus"), Text: "x"},
			},
			wantErr: "draft status",
		},
		{
			name: "draft status without text",
			ledger: &DebateLedger{
				Round: 1,
				Draft: DraftProposal{Status: DraftStatusDraft, Text: ""},
			},
			wantErr: "must have non-empty text",
		},
		{
			name: "none draft with no text is valid",
			ledger: &DebateLedger{
				Round: 1,
				Draft: DraftProposal{Status: DraftStatusNone, Text: ""},
			},
			wantErr: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.ledger.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Errorf("error %q should contain %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// CloneDebateLedger deep-copy semantics
// ---------------------------------------------------------------------------

func TestCloneDebateLedger(t *testing.T) {
	t.Run("deep copy does not alias slices", func(t *testing.T) {
		original := &DebateLedger{
			Round: 1,
			Positions: []AgentPosition{
				{AgentID: "a", Text: "t1", Turn: 1},
			},
			Agreements: []AgreementPoint{
				{Text: "x", Endorsers: []string{"a", "b"}},
			},
			Cruxes: []OpenCrux{
				{Topic: "c", Views: []PositionalView{{AgentID: "a", Stance: "s"}}, RaisedAt: 1},
			},
			Draft: DraftProposal{Status: DraftStatusDraft, Text: "d", Endorsers: []string{"a"}},
		}
		clone := CloneDebateLedger(original)
		if clone == original {
			t.Fatal("clone should not equal original pointer")
		}

		clone.Positions[0].Text = "mutated"
		clone.Agreements[0].Endorsers[0] = "z"
		clone.Cruxes[0].Views[0].Stance = "mutated"
		clone.Draft.Endorsers[0] = "z"

		if original.Positions[0].Text != "t1" {
			t.Errorf("original aliasing detected in Positions")
		}
		if original.Agreements[0].Endorsers[0] != "a" {
			t.Errorf("original aliasing detected in Agreements Endorsers")
		}
		if original.Cruxes[0].Views[0].Stance != "s" {
			t.Errorf("original aliasing detected in Cruxes Views")
		}
		if original.Draft.Endorsers[0] != "a" {
			t.Errorf("original aliasing detected in Draft Endorsers")
		}
	})

	t.Run("nil returns nil", func(t *testing.T) {
		if got := CloneDebateLedger(nil); got != nil {
			t.Errorf("CloneDebateLedger(nil) should return nil, got %+v", got)
		}
	})

	t.Run("clone of empty ledger is valid and equal", func(t *testing.T) {
		original := NewDebateLedger(0, 1.0)
		clone := CloneDebateLedger(original)
		if !reflect.DeepEqual(original, clone) {
			t.Errorf("empty clone should equal original\norig: %+v\nclone: %+v", original, clone)
		}
		if err := clone.Validate(); err != nil {
			t.Errorf("cloned empty ledger should validate: %v", err)
		}
	})
}

// ---------------------------------------------------------------------------
// LedgerRecord persistence (malformed-persistence unit)
// ---------------------------------------------------------------------------

func TestLedgerRecordPersistence(t *testing.T) {
	t.Run("well-formed ledger record round-trips and validates", func(t *testing.T) {
		ledger := &DebateLedger{
			Round:     1,
			UpdatedAt: 1715000010.0,
			Positions: []AgentPosition{
				{AgentID: "a", Text: "pos-a", Turn: 1},
			},
			Agreements: []AgreementPoint{
				{Text: "agree-1", Endorsers: []string{"a", "b"}},
			},
			Cruxes: []OpenCrux{
				{Topic: "crux-1", RaisedAt: 1, Views: []PositionalView{{AgentID: "a", Stance: "s1"}}},
			},
			Draft: DraftProposal{Status: DraftStatusNone},
		}
		rec := NewLedgerRecord(ledger, 1715000010.0)

		if err := rec.Validate(); err != nil {
			t.Fatalf("record should validate: %v", err)
		}

		data, err := json.Marshal(rec)
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		var decoded LedgerRecord
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if !reflect.DeepEqual(*rec, decoded) {
			t.Errorf("round-trip mismatch\noriginal: %+v\ndecoded:  %+v", *rec, decoded)
		}
		if decoded.Turn != LedgerSentinelTurn {
			t.Errorf("sentinel turn: got %d, want %d", decoded.Turn, LedgerSentinelTurn)
		}
		if decoded.AgentID != LedgerAgentID {
			t.Errorf("sentinel agent_id: got %q, want %q", decoded.AgentID, LedgerAgentID)
		}
	})

	t.Run("wrong-sentinel turn fails validation", func(t *testing.T) {
		rec := &LedgerRecord{
			Turn:      0,
			AgentID:   LedgerAgentID,
			Timestamp: 1.0,
			Ledger:    DebateLedger{Round: 0, Draft: DraftProposal{Status: DraftStatusNone}},
		}
		err := rec.Validate()
		if err == nil {
			t.Fatal("expected error for wrong sentinel turn")
		}
		if !strings.Contains(err.Error(), "turn must be") {
			t.Errorf("error should mention sentinel turn, got %q", err.Error())
		}
	})

	t.Run("wrong agent_id fails validation", func(t *testing.T) {
		rec := &LedgerRecord{
			Turn:      LedgerSentinelTurn,
			AgentID:   "not-ledger",
			Timestamp: 1.0,
			Ledger:    DebateLedger{Round: 0, Draft: DraftProposal{Status: DraftStatusNone}},
		}
		err := rec.Validate()
		if err == nil {
			t.Fatal("expected error for wrong agent_id")
		}
		if !strings.Contains(err.Error(), "agent_id must be") {
			t.Errorf("error should mention agent_id, got %q", err.Error())
		}
	})

	t.Run("invalid inner ledger fails record validation", func(t *testing.T) {
		rec := &LedgerRecord{
			Turn:      LedgerSentinelTurn,
			AgentID:   LedgerAgentID,
			Timestamp: 1.0,
			Ledger: DebateLedger{
				Round: -1,
				Draft: DraftProposal{Status: DraftStatusNone},
			},
		}
		err := rec.Validate()
		if err == nil {
			t.Fatal("expected error from invalid inner ledger")
		}
		if !strings.Contains(err.Error(), "ledger record:") {
			t.Errorf("error should wrap inner ledger error, got %q", err.Error())
		}
	})

	t.Run("nil record fails validation", func(t *testing.T) {
		var r *LedgerRecord
		err := r.Validate()
		if err == nil {
			t.Fatal("expected error for nil record")
		}
		if !strings.Contains(err.Error(), "nil") {
			t.Errorf("error should mention nil, got %q", err.Error())
		}
	})
}

// ---------------------------------------------------------------------------
// NewLedgerRecord sentinel and clone behavior
// ---------------------------------------------------------------------------

func TestNewLedgerRecord(t *testing.T) {
	t.Run("nil ledger produces valid empty record", func(t *testing.T) {
		rec := NewLedgerRecord(nil, 1.0)
		if rec.Turn != LedgerSentinelTurn {
			t.Errorf("turn: got %d, want %d", rec.Turn, LedgerSentinelTurn)
		}
		if rec.AgentID != LedgerAgentID {
			t.Errorf("agent_id: got %q, want %q", rec.AgentID, LedgerAgentID)
		}
		if err := rec.Validate(); err != nil {
			t.Errorf("nil-ledger record should validate: %v", err)
		}
		if rec.Ledger.HasEndorsableProposal() {
			t.Errorf("empty ledger should not report endorsable proposal")
		}
		if rec.Ledger.Draft.Status != DraftStatusNone {
			t.Errorf("nil-ledger record should have explicit no-draft status, got %q", rec.Ledger.Draft.Status)
		}
	})

	t.Run("record does not alias source ledger", func(t *testing.T) {
		source := &DebateLedger{
			Round:     1,
			Positions: []AgentPosition{{AgentID: "a", Text: "orig", Turn: 1}},
		}
		rec := NewLedgerRecord(source, 1.0)
		rec.Ledger.Positions[0].Text = "mutated"
		if source.Positions[0].Text != "orig" {
			t.Errorf("source ledger aliased through NewLedgerRecord")
		}
	})
}

// ---------------------------------------------------------------------------
// CloneLedgerRecord deep-copy semantics
// ---------------------------------------------------------------------------

func TestCloneLedgerRecord(t *testing.T) {
	t.Run("clone does not alias inner ledger slices", func(t *testing.T) {
		rec := &LedgerRecord{
			Turn:      LedgerSentinelTurn,
			AgentID:   LedgerAgentID,
			Timestamp: 1.0,
			Ledger: DebateLedger{
				Round:     1,
				Positions: []AgentPosition{{AgentID: "a", Text: "orig", Turn: 1}},
				Draft:     DraftProposal{Status: DraftStatusDraft, Text: "d", Endorsers: []string{"a"}},
			},
		}
		clone := CloneLedgerRecord(rec)
		clone.Ledger.Positions[0].Text = "mutated"
		clone.Ledger.Draft.Endorsers[0] = "z"
		if rec.Ledger.Positions[0].Text != "orig" {
			t.Errorf("original record aliased through CloneLedgerRecord Positions")
		}
		if rec.Ledger.Draft.Endorsers[0] != "a" {
			t.Errorf("original record aliased through CloneLedgerRecord Draft Endorsers")
		}
	})

	t.Run("nil returns nil", func(t *testing.T) {
		if got := CloneLedgerRecord(nil); got != nil {
			t.Errorf("CloneLedgerRecord(nil) should return nil, got %+v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Ledger sentinel constants
// ---------------------------------------------------------------------------

func TestLedgerSentinels(t *testing.T) {
	if LedgerSentinelTurn != -3 {
		t.Errorf("LedgerSentinelTurn: got %d, want -3 (next after -1 seed, -2 evidence)", LedgerSentinelTurn)
	}
	if LedgerAgentID != "ledger" {
		t.Errorf("LedgerAgentID: got %q, want %q", LedgerAgentID, "ledger")
	}
}
