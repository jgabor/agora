package output

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/jgabor/agora/internal/cast"
	"github.com/jgabor/agora/internal/types"
)

func TestDrawPanelWrapsAndPadsContent(t *testing.T) {
	got := drawPanel("Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu.", "Topic", "4")

	assertContains(t, got, "╭")
	assertContains(t, got, "Topic")
	assertContains(t, got, "Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu.")
	assertContains(t, got, "╰")

	lines := strings.Split(got, "\n")
	wantWidth := visualLen(lines[0])
	for _, line := range lines {
		if strings.Contains(line, "│") && visualLen(line) != wantWidth {
			t.Fatalf("content line visual width: got %d, want %d in %q", visualLen(line), wantWidth, line)
		}
	}
}

func TestDrawPanelPlainModeUsesASCIIBorder(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")

	got := drawPanel("Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu.", "Topic", "4")

	assertContains(t, got, "+- Topic")
	assertContains(t, got, "| Alpha beta gamma delta epsilon zeta eta theta iota kappa lambda mu.")
	assertNoANSI(t, got)
	assertNoUnicodeBox(t, got)
}

func TestDrawTableAlignsAndPadsCells(t *testing.T) {
	got := drawTable(&PlainRenderer{}, "Stats", []string{"Agent", "Tokens"}, [][]string{
		{"a", "7"},
		{"long-agent", "123"},
	}, []string{"", "right"})

	assertContains(t, got, "Stats")
	assertContains(t, got, "Agent")
	assertContains(t, got, "Tokens")
	assertContains(t, got, "a")
	assertContains(t, got, "7")
	assertContains(t, got, "long-agent")
	assertContains(t, got, "123")

	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("drawTable line count: got %d lines in %q, want title, border, header, separator, rows, and border", len(lines), got)
	}
	width := visualLen(lines[1])
	for _, line := range lines[2:] {
		if visualLen(line) != width {
			t.Fatalf("drawTable visual width: got %d, want %d in %q", visualLen(line), width, line)
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
	got := visualLen("\x1b[1mok\x1b[0m ✓")
	want := utf8.RuneCountInString("ok ✓")
	if got != want {
		t.Fatalf("visualLen: got %d, want %d", got, want)
	}
}

func TestOutputWidthUsesDetectedTerminalWidthBeforeColumns(t *testing.T) {
	old := detectedTerminalWidth
	detectedTerminalWidth = func() (int, bool) { return 155, true }
	t.Cleanup(func() { detectedTerminalWidth = old })
	t.Setenv("COLUMNS", "90")

	if got := outputWidth(); got != 150 {
		t.Fatalf("outputWidth: got %d, want detected terminal width capped at 150", got)
	}
}

func TestOutputWidthFallsBackToColumns(t *testing.T) {
	old := detectedTerminalWidth
	detectedTerminalWidth = func() (int, bool) { return 0, false }
	t.Cleanup(func() { detectedTerminalWidth = old })
	t.Setenv("COLUMNS", "120")

	if got := outputWidth(); got != 120 {
		t.Fatalf("outputWidth: got %d, want COLUMNS fallback", got)
	}
}

func TestOutputWidthFallsBackToDefault(t *testing.T) {
	old := detectedTerminalWidth
	detectedTerminalWidth = func() (int, bool) { return 0, false }
	t.Cleanup(func() { detectedTerminalWidth = old })
	t.Setenv("COLUMNS", "")

	if got := outputWidth(); got != 76 {
		t.Fatalf("outputWidth: got %d, want default width", got)
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

func TestConfigPreviewRendersGeneratedCastIdentityWithoutReplacingAgentID(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents: []types.AgentConfig{
			{
				ID:    "strategist",
				Model: "opencode/test",
				Identity: &types.AgentIdentity{
					DisplayName: "Mina",
					Role:        "Planner",
					Affiliation: "Core Team",
				},
			},
		},
	}

	got := captureOutput(t, func() {
		NewOutputManager(false).ConfigPreview(cfg, types.AutoQuick, types.LevelCaps{})
	})

	assertContains(t, got, "AGENT [A1 strategist] NAME Solon PERSONA strategist")
	assertContains(t, got, "opencode/test")
	assertContains(t, got, "COLOR 6")
	assertNoANSI(t, got)
	assertNoUnicodeBox(t, got)
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
	assertContains(t, got, "AGENT [A1 optimist]")
	assertContains(t, got, "NAME Solon")
	assertContains(t, got, "PERSONA optimist")
	assertContains(t, got, "MODEL")
	assertContains(t, got, "opencode/test")
	assertContains(t, got, "COLOR 6")
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

	assertContains(t, got, "TURN 2/5 (40%) [####------]")
	assertContains(t, got, "AGENT [A2 skeptic]")
	assertNotContains(t, got, "[A? skeptic]")
	assertContains(t, got, "MODEL opencode/test")
	assertContains(t, got, "ELAPSED 3.4s")
	assertContains(t, got, "TOKENS 42")
	assertContains(t, got, "COST $0.012345")
	assertContains(t, got, "[CONSENSUS] Ship the small fix.")
}

func TestTurnProgressShowsBoundedMetricsWithoutMisleadingUnboundedBars(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	tokens := 42
	cost := 0.25
	budget := 1.0
	record := types.TurnRecord{
		AgentID:   "skeptic",
		Elapsed:   3.4,
		Tokens:    types.TokenUsage{Total: &tokens},
		Cost:      &cost,
		Consensus: true,
	}

	got := captureOutput(t, func() {
		manager := NewOutputManager(false)
		manager.state = &types.DeliberationState{
			StartTime: float64(time.Now().UnixNano())/1e9 - 10,
			TimeLimit: 60,
			Budget:    &budget,
			Config:    &types.DeliberationConfig{ConsensusThreshold: 2},
		}
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "skeptic"}}})
		manager.TurnProgress(record, 1, 5)
	})

	assertContains(t, got, "TURN 2/5 (40%) [####------]")
	assertContains(t, got, "COST $0.250000/$1.00 (25%) [###-------]")
	assertContains(t, got, "TIME ")
	assertContains(t, got, "/60s (")
	assertContains(t, got, "CONSENSUS 1/2 (50%) [#####-----]")
	assertContains(t, got, "TOKENS 42")
	assertNotContains(t, got, "TOKENS 42/")
	assertNoANSI(t, got)
	assertNoUnicodeBox(t, got)
}

func TestTurnProgressRendersRegisteredIdentityAsPlainLabels(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	record := types.TurnRecord{AgentID: "strategist", Elapsed: 0.2}

	got := captureOutput(t, func() {
		manager := NewOutputManager(false)
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{{
			ID: "strategist",
			Identity: &types.AgentIdentity{
				DisplayName: "Mina",
				Role:        "Planner",
			},
		}}})
		manager.TurnProgress(record, 0, 1)
	})

	assertContains(t, got, "AGENT [A1 strategist] NAME Solon PERSONA strategist")
	assertContains(t, got, "ELAPSED 0.2s")
	assertNoANSI(t, got)
}

func TestTurnProgressRichModeUsesPanelBubblesAndReadableMetrics(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	model := "opencode/test"
	tokens := 42
	cost := 0.25
	budget := 1.0
	record := types.TurnRecord{
		AgentID:            "strategist",
		Model:              &model,
		Elapsed:            1.2,
		Tokens:             types.TokenUsage{Total: &tokens},
		Cost:               &cost,
		Consensus:          true,
		ConsensusStatement: "Ship the small fix.",
		Content:            "# Decision\n\nKeep metadata readable.",
	}

	got := captureOutput(t, func() {
		manager := NewOutputManager(true)
		manager.state = &types.DeliberationState{
			StartTime: float64(time.Now().UnixNano())/1e9 - 10,
			TimeLimit: 60,
			Budget:    &budget,
			Config:    &types.DeliberationConfig{ConsensusThreshold: 2},
		}
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{{
			ID: "strategist",
			Identity: &types.AgentIdentity{
				DisplayName: "Mina",
				Role:        "Planner",
			},
		}}})
		manager.TurnProgress(record, 1, 5)
	})

	assertContains(t, got, "╭")
	assertContains(t, got, "Turn 2 of 5")
	assertContains(t, got, "●")
	assertContains(t, got, "○")
	assertContains(t, got, "Agent")
	assertContains(t, got, "NAME Solon PERSONA strategist")
	assertContains(t, got, "Agreement")
	assertContains(t, got, "Agent Response")
	assertContains(t, got, "Keep metadata")
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

func TestCastIdentityConsistentAcrossPreviewHeaderAndTurns(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	model := "opencode/test"
	cfg := &types.DeliberationConfig{
		Topology:           types.TopologyRing,
		ConsensusThreshold: 2,
		Agents: []types.AgentConfig{
			{
				ID:           "strategist",
				Model:        model,
				SystemPrompt: "Plan the smallest safe path.\nSecond line stays hidden.",
				Identity: &types.AgentIdentity{
					DisplayName: "Mina",
					Role:        "Planner",
					Affiliation: "Core Team",
				},
			},
			{ID: "skeptic", Model: model, SystemPrompt: "Challenge weak claims."},
		},
	}
	state := &types.DeliberationState{Topic: "Identity review", Config: cfg, TimeLimit: 30, MaxTurns: 2, Window: 1}
	record := types.TurnRecord{AgentID: "strategist", Model: &model, Elapsed: 0.3}
	unknown := types.TurnRecord{AgentID: "legacy-agent", Elapsed: 0.1}

	manager := NewOutputManager(false)
	preview := captureOutput(t, func() {
		manager.ConfigPreview(cfg, types.AutoQuick, types.LevelCaps{MaxAgents: 2, MaxTurns: 4, TimeLimit: 60})
	})
	header := captureOutput(t, func() { manager.DeliberationHeader(state) })
	turn := captureOutput(t, func() { manager.TurnProgress(record, 0, 2) })
	fallback := captureOutput(t, func() { manager.TurnProgress(unknown, 1, 2) })

	for name, output := range map[string]string{"preview": preview, "header": header, "turn": turn} {
		assertContains(t, output, "AGENT [A1 strategist]")
		assertContains(t, output, "NAME Solon")
		assertContains(t, output, "PERSONA strategist")
		assertContains(t, output, "MODEL")
		assertContains(t, output, "opencode/test")
		assertNoANSI(t, output)
		assertNoUnicodeBox(t, output)
		if strings.Contains(output, "Second line stays hidden") {
			t.Fatalf("%s should only render the first system prompt line", name)
		}
	}
	assertContains(t, preview, "CONTEXT Plan the smallest safe path.")
	assertContains(t, header, "CONTEXT Plan the smallest safe path.")
	assertOrder(t, preview, "[A1 strategist]", "[A2 skeptic]")
	assertOrder(t, header, "[A1 strategist]", "[A2 skeptic]")
	assertContains(t, fallback, "AGENT [A? legacy-agent]")
	assertNotContains(t, fallback, "[A2 legacy-agent]")
}

func TestVerboseTurnContentSeparatesMetadataFromBody(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	model := "opencode/test"
	inputTokens := 10
	outputTokens := 20
	reasoningTokens := 3
	cost := 0.01
	record := types.TurnRecord{
		AgentID: "strategist",
		Model:   &model,
		Elapsed: 1.2,
		Tokens:  types.TokenUsage{Input: &inputTokens, Output: &outputTokens, Reasoning: &reasoningTokens},
		Cost:    &cost,
		Content: "# Decision\n\nThis is a long agent response with prose that should remain readable and separate from metadata.",
	}

	got := captureOutput(t, func() {
		manager := NewOutputManagerWithMode(OutputVerbose)
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "strategist"}}})
		manager.TurnProgress(record, 0, 1)
	})

	assertContains(t, got, "TURN 1/1 (100%) [##########] | AGENT [A1 strategist] NAME Solon PERSONA strategist | MODEL opencode/test | ELAPSED 1.2s")
	assertContains(t, got, "DIAGNOSTICS | INPUT_TOKENS 10 | OUTPUT_TOKENS 20 | REASONING_TOKENS 3 | CUMULATIVE_COST $0.010000")
	assertContains(t, got, "AGENT CONTENT")
	assertContains(t, got, "  | # Decision")
	assertContains(t, got, "  | This is a long agent response")
	if strings.Contains(got, "AGENT CONTENT |") {
		t.Fatal("verbose prose should start after a metadata/body separator")
	}
}

func TestNormalTurnProgressIncludesResponseBodyWithoutVerboseDiagnostics(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	record := types.TurnRecord{AgentID: "strategist", Elapsed: 0.2, Content: "Default output shows the response."}

	got := captureOutput(t, func() {
		manager := NewOutputManagerWithMode(OutputNormal)
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "strategist"}}})
		manager.TurnProgress(record, 0, 1)
	})

	assertContains(t, got, "TURN 1/1 (100%) [##########] | AGENT [A1 strategist] NAME Solon PERSONA strategist")
	assertContains(t, got, "AGENT CONTENT")
	assertContains(t, got, "Default output shows the response.")
	assertNotContains(t, got, "DIAGNOSTICS")
}

func TestQuietTurnProgressSuppressesResponseBody(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	record := types.TurnRecord{AgentID: "strategist", Elapsed: 0.2, Content: "Quiet output hides this response."}

	got := captureOutput(t, func() {
		manager := NewOutputManagerWithMode(OutputQuiet)
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "strategist"}}})
		manager.TurnProgress(record, 0, 1)
	})

	assertContains(t, got, "TURN 1/1 (100%) [##########] | AGENT [A1 strategist] NAME Solon PERSONA strategist")
	assertNotContains(t, got, "AGENT CONTENT")
	assertNotContains(t, got, "Quiet output hides this response.")
}

func TestRenderTranscriptUsesRunStylePlainOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	model := "opencode/test"
	cfg := &types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "strategist", Model: model}}}
	metadata := types.NewTranscriptMetadata(cfg, cast.New(cfg.Agents).Members())
	records := []types.TurnRecord{
		{
			Turn:       -2,
			AgentID:    "orchestrator",
			Content:    "Evidence gathered.",
			Transcript: metadata,
			Evidence: &types.EvidenceBundle{
				Summary:          "Two sources found",
				SourceReferences: []types.SourceReference{{Title: "Spec", URL: "https://example.test/spec"}},
			},
		},
		{Turn: 0, AgentID: "strategist", Model: &model, Content: "Stored response."},
	}

	var out bytes.Buffer
	RenderTranscript(&out, records)
	got := out.String()

	assertContains(t, got, "Transcript Evidence")
	assertContains(t, got, "RECORD 1")
	assertContains(t, got, "Evidence Summary")
	assertContains(t, got, "1. Spec (https://example.test/spec)")
	assertContains(t, got, "TURN 1/1 (100%) [##########] | AGENT [A1 strategist] NAME Solon PERSONA strategist | MODEL opencode/test")
	assertContains(t, got, "AGENT CONTENT")
	assertContains(t, got, "Stored response.")
	assertNoANSI(t, got)
	assertNoUnicodeBox(t, got)
}

func TestRenderTranscriptUsesRunStyleRichOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	cfg := &types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "strategist", Model: "test/model"}}}
	metadata := types.NewTranscriptMetadata(cfg, cast.New(cfg.Agents).Members())
	records := []types.TurnRecord{{Turn: 0, AgentID: "strategist", Transcript: metadata, Content: "Stored rich response."}}

	var out bytes.Buffer
	RenderTranscript(&out, records)
	got := out.String()

	assertContains(t, got, "Turn 1 of 1")
	assertContains(t, got, "Agent Response")
	assertContains(t, got, "Stored rich response.")
	if !strings.Contains(got, "╭") || !strings.Contains(got, "╰") {
		t.Fatalf("rich transcript output should use run-style rounded panels:\n%s", got)
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
	assertContains(t, got, "Completed: all planned turns finished")
	assertContains(t, got, "Per-Agent Stats")
	assertContains(t, got, "[A2 skeptic]")
	assertContains(t, got, "$0.2") // Robust to truncation in narrow tables
	assertContains(t, got, "[A? orchestrator]")
}

func TestFinalStatsUsesSameBoundedMetricPresentationAsTurnProgress(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	tokensA := 30
	tokensB := 40
	costA := 0.25
	costB := 0.25
	budget := 1.0
	state := &types.DeliberationState{
		Config: &types.DeliberationConfig{
			ConsensusThreshold: 2,
			Agents:             []types.AgentConfig{{ID: "optimist"}, {ID: "skeptic"}},
		},
		StartTime: float64(time.Now().UnixNano())/1e9 - 30,
		MaxTurns:  4,
		TimeLimit: 60,
		Budget:    &budget,
		HaltedBy:  "consensus (2 consecutive agreements)",
	}
	records := []types.TurnRecord{
		{Turn: 0, AgentID: "optimist", Tokens: types.TokenUsage{Total: &tokensA}, Cost: &costA, Elapsed: 1, Consensus: true},
		{Turn: 1, AgentID: "skeptic", Tokens: types.TokenUsage{Total: &tokensB}, Cost: &costB, Elapsed: 1, Consensus: true},
	}

	got := captureOutput(t, func() {
		NewOutputManager(false).FinalStats(records, state)
	})

	assertContains(t, got, "Turns completed")
	assertContains(t, got, "2/4 (50%) [#####-----]")
	assertContains(t, got, "Duration")
	assertContains(t, got, "/60s (")
	assertContains(t, got, "Total cost")
	assertContains(t, got, "$0.500000/$1.00 (50%) [#####-----]")
	assertContains(t, got, "Consensus streak")
	assertContains(t, got, "2/2 (100%) [##########]")
	assertContains(t, got, "Total tokens")
	assertContains(t, got, "70")
	assertNotContains(t, got, "Total tokens | 70/")
	assertNoANSI(t, got)
	assertNoUnicodeBox(t, got)
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

func TestActivityPlainModeEmitsReadableStatusWithoutSpinnerArtifacts(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")

	got := captureOutput(t, func() {
		stop := NewOutputManager(false).Activity("Research")
		stop()
		fmt.Println("final output")
	})

	assertContains(t, got, "[INFO] Working: Research")
	assertContains(t, got, "final output")
	assertNoANSI(t, got)
	if strings.Contains(got, "\r") || strings.ContainsAny(got, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("plain activity should not contain spinner artifacts\noutput: %q", got)
	}
}

func TestActivityRedirectedStdoutRichTermEmitsPlainStatus(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	old := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return false }
	t.Cleanup(func() { stdoutIsTerminal = old })

	got := captureOutput(t, func() {
		stop := NewOutputManager(false).Activity("Generation: optimist")
		stop()
		fmt.Println("final output")
	})

	assertContains(t, got, "[INFO] Working: Generation: optimist")
	assertContains(t, got, "final output")
	assertNoANSI(t, got)
	if strings.Contains(got, "\r") || strings.ContainsAny(got, "⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏") {
		t.Fatalf("redirected activity should not contain spinner artifacts\noutput: %q", got)
	}
}

func TestActivityRichModeCleansLineBeforeFinalOutput(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	old := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdoutIsTerminal = old })

	got := captureOutput(t, func() {
		stop := NewOutputManager(false).Activity("Generation: strategist")
		stop()
		fmt.Println("TURN 1/1 | AGENT [A1 strategist]")
	})

	assertContains(t, got, "Working: Generation: strategist")
	assertContains(t, got, "\r\x1b[2KTURN 1/1 | AGENT [A1 strategist]\n")
}

func TestActivityStopsBeforeVerboseTurnContent(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CI", "")
	t.Setenv("TERM", "xterm-256color")
	old := stdoutIsTerminal
	stdoutIsTerminal = func() bool { return true }
	t.Cleanup(func() { stdoutIsTerminal = old })
	model := "opencode/test"
	record := types.TurnRecord{AgentID: "strategist", Model: &model, Elapsed: 0.4, Content: "# Decision\n\nKeep metadata readable."}

	got := captureOutput(t, func() {
		manager := NewOutputManager(true)
		manager.registerCast(&types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "strategist"}}})
		stop := manager.Activity("Generation: strategist")
		stop()
		manager.TurnProgress(record, 0, 1)
	})

	assertContains(t, got, "\r\x1b[2K")
	assertContains(t, got, "Turn 1 of 1")
	assertContains(t, got, "[A1 strategist]")
	assertContains(t, got, "Agent Response")
	assertContains(t, got, "Keep metadata")
	assertContains(t, got, "readable.")
	assertOrder(t, got, "\x1b[2K", "Agent Response")
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

func assertOrder(t *testing.T, s, first, second string) {
	t.Helper()
	firstIndex := strings.Index(s, first)
	secondIndex := strings.Index(s, second)
	if firstIndex < 0 || secondIndex < 0 || firstIndex >= secondIndex {
		t.Fatalf("expected %q before %q\noutput: %q", first, second, s)
	}
}

// --- Renderer contract tests ---

func TestPlainRendererContract(t *testing.T) {
	r := &PlainRenderer{}
	width := 76

	// IsRich
	if r.IsRich() {
		t.Fatal("PlainRenderer.IsRich should be false")
	}

	// Panel
	p := r.Panel("Title", "Body content", width, "4")
	assertContains(t, p, "Title")
	assertContains(t, p, "Body content")
	assertNoANSI(t, p)
	assertNoUnicodeBox(t, p)

	// SectionBlock
	sb := r.SectionBlock("Label", []string{"line1", "line2"}, width)
	assertContains(t, sb, "Label")
	assertContains(t, sb, "line1")
	assertNoANSI(t, sb)

	// SectionTitle
	st := r.SectionTitle("Important", "2")
	assertContains(t, st, "Important")
	assertNoANSI(t, st)

	// Table
	tbl := r.Table("Stats", []string{"A", "B"}, [][]string{{"x", "1"}, {"y", "2"}}, []string{"", "right"}, width, "6")
	assertContains(t, tbl, "Stats")
	assertContains(t, tbl, "A")
	assertContains(t, tbl, "x")
	assertContains(t, tbl, "2")
	assertNoANSI(t, tbl)

	// ListSection
	ls := r.ListSection("Items", []string{"first", "second"}, width, "6")
	assertContains(t, ls, "Items")
	assertContains(t, ls, "first")
	assertContains(t, ls, "second")
	assertNoANSI(t, ls)

	// ProseSection
	ps := r.ProseSection("Prose", "Some content here.", width, "2")
	assertContains(t, ps, "Prose")
	assertContains(t, ps, "Some content here.")
	assertNoANSI(t, ps)

	// VerboseBody
	vb := r.VerboseBody("Verbose output content.", width, "4")
	assertContains(t, vb, "AGENT CONTENT")
	assertContains(t, vb, "Verbose output content.")
	assertNoANSI(t, vb)

	// MetricBar
	mb := r.MetricBar(50)
	assertNoANSI(t, mb)

	// StatusLabel
	sl := r.StatusLabel("OK", "✓", "2")
	assertContains(t, sl, "OK")
	assertNoANSI(t, sl)

	// Styled
	styled := r.Styled("text", "2")
	if styled != "text" {
		t.Fatalf("PlainRenderer.Styled should return plain text, got %q", styled)
	}

	// Muted
	muted := r.Muted("dim")
	if muted != "dim" {
		t.Fatalf("PlainRenderer.Muted should return plain text, got %q", muted)
	}

	// Width
	if r.Width() <= 0 {
		t.Fatalf("PlainRenderer.Width should be positive, got %d", r.Width())
	}
}

func TestRichRendererContract(t *testing.T) {
	r := &RichRenderer{}
	width := 76

	// IsRich
	if !r.IsRich() {
		t.Fatal("RichRenderer.IsRich should be true")
	}

	// Panel - should include Unicode box borders
	p := r.Panel("Title", "Body content", width, "4")
	assertContains(t, p, "╭")
	assertContains(t, p, "╰")
	assertContains(t, p, "Title")
	assertContains(t, p, "Body content")

	// SectionBlock
	sb := r.SectionBlock("Label", []string{"line1", "line2"}, width)
	assertContains(t, sb, "Label")
	assertContains(t, sb, "line1")

	// SectionTitle
	st := r.SectionTitle("Important", "2")
	assertContains(t, st, "Important")

	// Table
	tbl := r.Table("Stats", []string{"A", "B"}, [][]string{{"x", "1"}}, []string{"", "right"}, width, "6")
	assertContains(t, tbl, "Stats")
	assertContains(t, tbl, "A")
	assertContains(t, tbl, "x")

	// ListSection
	ls := r.ListSection("Items", []string{"first", "second"}, width, "6")
	assertContains(t, ls, "Items")
	assertContains(t, ls, "first")

	// ProseSection
	ps := r.ProseSection("Prose", "Some prose content.", width, "2")
	assertContains(t, ps, "Prose")
	assertContains(t, ps, "Some prose content.")

	// VerboseBody
	vb := r.VerboseBody("Verbose output content.", width, "4")
	assertContains(t, vb, "Agent Response")
	assertContains(t, vb, "Verbose output content.")

	// MetricBar
	mb := r.MetricBar(50)
	if mb == "" {
		t.Fatal("RichRenderer.MetricBar should not be empty")
	}

	// StatusLabel
	sl := r.StatusLabel("OK", "✓", "2")
	assertContains(t, sl, "OK")

	// Styled
	styled := r.Styled("text", "2")
	assertContains(t, styled, "text")

	// Muted
	muted := r.Muted("dim")
	assertContains(t, muted, "dim")

	// Width
	if r.Width() <= 0 {
		t.Fatalf("RichRenderer.Width should be positive, got %d", r.Width())
	}
}
