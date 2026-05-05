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

func TestDrawTableAlignsAndPadsCells(t *testing.T) {
	got := drawTable("Stats", []string{"Agent", "Tokens"}, [][]string{
		{"a", "7"},
		{"long-agent", "123"},
	}, []string{"", "right"})

	want := ansiBold + "Stats" + ansiReset + "\n" +
		ansiDim + "───────────┼───────" + ansiReset + "\n" +
		ansiCyan + "Agent      │ Tokens" + ansiReset + "\n" +
		ansiDim + "───────────┼───────" + ansiReset + "\n" +
		"a          │      7\n" +
		"long-agent │    123\n"

	if got != want {
		t.Fatalf("drawTable mismatch\n got: %q\nwant: %q", got, want)
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
	assertContains(t, got, "Level: quick (max 2 agents, 4 turns, 60s)")
	assertContains(t, got, "1. skeptic — Challenge weak claims.")
	if strings.Contains(got, "Ignore this second line") {
		t.Fatal("ConfigPreview should only render the first system prompt line")
	}
}

func TestPrintStatsPrintsSummaryAndAgentTables(t *testing.T) {
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

func TestSimpleStatusMethods(t *testing.T) {
	manager := NewOutputManager(false)
	got := captureOutput(t, func() {
		manager.Info("thinking")
		manager.Success("done")
		manager.Error("failed")
		manager.Delimiter()
	})

	assertContains(t, got, "thinking")
	assertContains(t, got, "done")
	assertContains(t, got, "failed")
	assertContains(t, got, strings.Repeat("─", 60))
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
