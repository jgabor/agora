package output

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/jgabor/agora/internal/types"
)

func TestDrawPanelWrapsAndPadsContent(t *testing.T) {
	got := drawPanel("Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu.", "Topic", ansiBlue)

	assertContains(t, got, ansiBlue+"╭")
	assertContains(t, got, ansiBold+" Topic "+ansiReset)
	assertContains(t, got, "Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda")
	assertContains(t, got, "mu.")
	assertContains(t, got, ansiBlue+"╰"+strings.Repeat("─", 74)+"╯"+ansiReset)

	for _, line := range strings.Split(got, "\n") {
		if strings.Contains(line, "│") && visualLen(line) != 76 {
			t.Fatalf("content line visual width: got %d, want 76 in %q", visualLen(line), line)
		}
	}
}

func TestDrawPanelPlainModeUsesASCIIBorder(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")

	got := drawPanel("Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu.", "Topic", ansiBlue)

	assertContains(t, got, "+")
	assertContains(t, got, strings.Repeat("-", 74))
	assertContains(t, got, "| Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda")
	assertNoANSI(t, got)
	assertNoUnicodeBox(t, got)
}

func TestDrawTableAlignsAndPadsCells(t *testing.T) {
	got := drawTable("Stats", []string{"Agent", "Tokens"}, [][]string{
		{"a", "7"},
		{"long-agent", "123"},
	}, []string{"", "right"})

	assertContains(t, got, ansiBold+"Stats"+ansiReset)
	assertContains(t, got, ansiCyan+"Agent      │ Tokens"+ansiReset)
	assertContains(t, got, "a          │      7")
	assertContains(t, got, "long-agent │    123")

	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("drawTable line count: got %d lines in %q, want title, separators, header, and two rows", len(lines), got)
	}
	for _, line := range lines[1:] {
		if visualLen(line) != 19 {
			t.Fatalf("drawTable visual width: got %d, want 19 in %q", visualLen(line), line)
		}
	}
}

func TestWrapTextPreservesParagraphBreaks(t *testing.T) {
	got := wrapText("alpha beta gamma\n\ndelta epsilon", 11)
	want := []string{"alpha beta", "gamma", "", "delta", "epsilon"}

	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("wrapText: got %#v, want %#v", got, want)
	}
}

func TestVisualLenIgnoresAnsiEscapes(t *testing.T) {
	got := visualLen(ansiBold + "ok" + ansiReset + " ✓")
	want := utf8.RuneCountInString("ok ✓")
	if got != want {
		t.Fatalf("visualLen: got %d, want %d", got, want)
	}
}

func TestConfigPreviewPrintsGeneratedConfigPanel(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	cfg := &types.DeliberationConfig{
		Topology:           types.TopologyRing,
		ConsensusThreshold: 2,
		Agents: []types.AgentConfig{
			{ID: "skeptic", SystemPrompt: "Challenge weak claims.\nIgnore this second line."},
		},
	}
	caps := types.LevelCaps{MaxAgents: 2, MaxTurns: 4, TimeLimit: 60}

	got := captureOutput(t, func() {
		NewOutputManager(false).ConfigPreview(cfg, types.AutoQuick, caps)
	})

	assertContains(t, got, "Generated Config")
	assertContains(t, got, "Topology: ring")
	assertContains(t, got, "Level caps: quick")
	assertContains(t, got, "AGENT [A1 skeptic]")
	assertContains(t, got, "Challenge weak claims.")
	assertNoANSI(t, got)
	assertNoUnicodeBox(t, got)
	if strings.Contains(got, "Ignore this second line") {
		t.Fatal("ConfigPreview should only render the first system prompt line")
	}
}

func TestDeliberationHeaderPlainModeHasNoAnsi(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	state := &types.DeliberationState{
		Topic:     "Plain terminal review",
		TimeLimit: 30,
		MaxTurns:  2,
		Window:    1,
		Config: &types.DeliberationConfig{
			Topology:           types.TopologyRing,
			ConsensusThreshold: 2,
			Agents: []types.AgentConfig{
				{ID: "optimist", Model: "opencode/test"},
				{ID: "skeptic", Model: "opencode/test"},
			},
		},
	}

	got := captureOutput(t, func() {
		NewOutputManager(false).DeliberationHeader(state)
	})

	assertContains(t, got, "Deliberation Start")
	assertContains(t, got, "Topic")
	assertContains(t, got, "Cast")
	assertContains(t, got, "AGENT [A1 optimist] MODEL opencode/test")
	assertContains(t, got, "Run Settings")
	assertContains(t, got, "Consensus threshold: 2")
	assertNoANSI(t, got)
	assertNoUnicodeBox(t, got)
}

func TestTurnProgressUsesCastBadgeForConfiguredAgent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	model := "opencode/test"
	tokens := 42
	cost := 0.012345
	record := types.TurnRecord{
		AgentID:            "skeptic",
		Model:              &model,
		Elapsed:            3.4,
		Tokens:             types.TokenUsage{Total: &tokens},
		Cost:               &cost,
		Consensus:          true,
		ConsensusStatement: "Ship the small fix.",
	}

	got := captureOutput(t, func() {
		manager := NewOutputManager(false)
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{
			{ID: "optimist"},
			{ID: "skeptic"},
		}})
		manager.TurnProgress(record, 1, 5)
	})

	assertContains(t, got, "TURN 2/5")
	assertContains(t, got, "AGENT [A2 skeptic]")
	assertNotContains(t, got, "[A? skeptic]")
	assertContains(t, got, "MODEL opencode/test")
	assertContains(t, got, "ELAPSED 3.4s")
	assertContains(t, got, "TOKENS 42")
	assertContains(t, got, "COST $0.012345")
	assertContains(t, got, "[CONSENSUS] Ship the small fix.")
}

func TestTurnProgressFallsBackForUnknownAgent(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	record := types.TurnRecord{AgentID: "resumed-agent", Elapsed: 0.1}

	got := captureOutput(t, func() {
		manager := NewOutputManager(false)
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "skeptic"}}})
		manager.TurnProgress(record, 0, 1)
	})

	assertContains(t, got, "AGENT [A? resumed-agent]")
}

func TestVerboseTurnContentSeparatesMetadataFromBody(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	model := "opencode/test"
	record := types.TurnRecord{
		AgentID: "strategist",
		Model:   &model,
		Elapsed: 1.2,
		Content: "# Decision\n\nThis is a long agent response with prose that should remain readable and separate from metadata.",
	}

	got := captureOutput(t, func() {
		manager := NewOutputManager(true)
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "strategist"}}})
		manager.TurnProgress(record, 0, 1)
	})

	assertContains(t, got, "TURN 1/1 | AGENT [A1 strategist] | MODEL opencode/test | ELAPSED 1.2s")
	assertContains(t, got, "AGENT CONTENT")
	assertContains(t, got, "  | # Decision")
	assertContains(t, got, "  | This is a long agent response")
	if strings.Contains(got, "AGENT CONTENT |") {
		t.Fatal("verbose prose should start after a metadata/body separator")
	}
}

func TestPrintStatsPrintsSummaryAndAgentTables(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	stats := StatsDict{
		"total_turns":               3,
		"total_tokens":              100,
		"total_cost":                0.25,
		"avg_turn_duration_seconds": 1.5,
		"per_agent": map[string]any{
			"skeptic": map[string]any{"turns": 2, "tokens": 70, "cost": 0.2},
		},
		"consensus_events": []any{
			map[string]any{"turn": 2, "agent_id": "skeptic", "statement": "ship it"},
		},
	}

	got := captureOutput(t, func() {
		NewOutputManager(false).PrintStats(stats)
	})

	assertContains(t, got, "Transcript Statistics")
	assertContains(t, got, "Total turns")
	assertContains(t, got, "$0.250000")
	assertContains(t, got, "Per-Agent Stats")
	assertContains(t, got, "skeptic")
	assertContains(t, got, "Consensus Events:")
	assertContains(t, got, "Turn 2 [skeptic]: ship it")
}

func TestFinalStatsPreservesSummaryAndPerAgentMetrics(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	tokensA := 30
	tokensB := 40
	costA := 0.1
	costB := 0.2
	state := &types.DeliberationState{
		Config: &types.DeliberationConfig{Agents: []types.AgentConfig{
			{ID: "strategist"},
			{ID: "skeptic"},
		}},
		StartTime: 1,
		HaltedBy:  "max_turns",
	}
	records := []types.TurnRecord{
		{Turn: 0, AgentID: "orchestrator", Tokens: types.TokenUsage{Total: &tokensA}, Cost: &costA, Elapsed: 0.5},
		{Turn: 1, AgentID: "skeptic", Tokens: types.TokenUsage{Total: &tokensB}, Cost: &costB, Elapsed: 1.5},
	}

	got := captureOutput(t, func() {
		NewOutputManager(false).FinalStats(records, state)
	})

	assertContains(t, got, "Deliberation Summary")
	assertContains(t, got, "Turns completed")
	assertContains(t, got, "1")
	assertContains(t, got, "Total tokens")
	assertContains(t, got, "70")
	assertContains(t, got, "Total cost")
	assertContains(t, got, "$0.300000")
	assertContains(t, got, "Halted by")
	assertContains(t, got, "max_turns")
	assertContains(t, got, "Per-Agent Stats")
	assertContains(t, got, "[A2 skeptic]")
	assertContains(t, got, "$0.200000")
	assertContains(t, got, "[A? orchestrator]")
}

func TestSynthesisResultPreservesAllModelFields(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	result := map[string]any{
		"recommended_decision": "Proceed with the smallest safe option.",
		"confidence":           "medium",
		"key_arguments":        []any{"It limits blast radius.", "It remains reversible."},
		"points_of_agreement":  []any{"Everyone accepts the core constraint."},
		"unresolved_tensions":  []any{"Cost versus speed remains unresolved."},
	}

	got := captureOutput(t, func() {
		manager := NewOutputManager(false)
		manager.SynthesizeHeader()
		manager.SynthesisResult(result)
	})

	assertContains(t, got, "Synthesis")
	assertContains(t, got, "Recommended Decision")
	assertContains(t, got, "Proceed with the smallest safe option.")
	assertContains(t, got, "Synthesis Confidence")
	assertContains(t, got, "Confidence")
	assertContains(t, got, "medium")
	assertContains(t, got, "Key Arguments")
	assertContains(t, got, "It limits blast radius.")
	assertContains(t, got, "It remains reversible.")
	assertContains(t, got, "Points of Agreement")
	assertContains(t, got, "[CONSENSUS] Everyone accepts the core constraint.")
	assertContains(t, got, "Unresolved Tensions")
	assertContains(t, got, "[WARNING] Cost versus speed remains unresolved.")
}

func TestSimpleStatusMethods(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	manager := NewOutputManager(false)
	got := captureOutput(t, func() {
		manager.Info("thinking")
		manager.Success("done")
		manager.Error("failed")
		manager.Delimiter()
	})

	assertContains(t, got, "[INFO] thinking")
	assertContains(t, got, "[SUCCESS] done")
	assertContains(t, got, "[ERROR] failed")
	assertContains(t, got, strings.Repeat("-", 60))
}

func captureOutput(t *testing.T, fn func()) string {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	done := make(chan error, 1)
	go func() {
		_, err := io.Copy(&buf, r)
		done <- err
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	os.Stdout = old
	if err := <-done; err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close stdout reader: %v", err)
	}

	return buf.String()
}

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Fatalf("expected output to contain %q\noutput: %q", substr, s)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Fatalf("expected output not to contain %q\noutput: %q", substr, s)
	}
}

func assertNoANSI(t *testing.T, s string) {
	t.Helper()
	if strings.Contains(s, "\x1b[") {
		t.Fatalf("expected output to contain no ANSI escapes\noutput: %q", s)
	}
}

func assertNoUnicodeBox(t *testing.T, s string) {
	t.Helper()
	for _, r := range "╭╮╰╯─│┼" {
		if strings.ContainsRune(s, r) {
			t.Fatalf("expected output to contain no Unicode box drawing rune %q\noutput: %q", r, s)
		}
	}
}
