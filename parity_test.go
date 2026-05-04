package agora_test

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/jgabor/agora/internal/types"
)

func TestCrossVersionParity_PythonDryRunTranscript(t *testing.T) {
	data, err := os.ReadFile("testdata/python-dry-run.jsonl")
	if err != nil {
		t.Skipf("python testdata not available: %v", err)
	}

	var records []types.TurnRecord
	lines := splitJSONLLines(string(data))
	if len(lines) == 0 {
		t.Fatal("no records in python transcript")
	}

	for _, line := range lines {
		var rec types.TurnRecord
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("failed to unmarshal python record: %v", err)
		}
		records = append(records, rec)
	}

	if len(records) != 6 {
		t.Fatalf("expected 6 records, got %d", len(records))
	}

	seed := records[0]
	if seed.Turn != -1 || seed.AgentID != "orchestrator" {
		t.Errorf("invalid seed record: turn=%d agent=%s", seed.Turn, seed.AgentID)
	}

	expectedAgents := []string{"strategist", "domain_expert", "skeptic", "optimist", "user_advocate"}
	for i, expected := range expectedAgents {
		rec := records[i+1]
		if rec.AgentID != expected {
			t.Errorf("turn %d: expected agent %s, got %s", rec.Turn, expected, rec.AgentID)
		}
		if rec.Turn != i {
			t.Errorf("turn %d: expected turn %d, got %d", i, rec.Turn, rec.Turn)
		}
	}

	t.Logf("parity verified: 6 records, correct agent sequence, python transcript loads cleanly")
}

func splitJSONLLines(data string) []string {
	var lines []string
	current := ""
	for _, ch := range data {
		current += string(ch)
		if ch == '\n' {
			if current != "\n" {
				lines = append(lines, current[:len(current)-1])
			}
			current = ""
		}
	}
	if current != "" {
		lines = append(lines, current)
	}
	return lines
}
