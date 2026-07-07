package session

import (
	"testing"

	"github.com/jgabor/agora/internal/types"
)

func TestLastLedgerFromRecords(t *testing.T) {
	t.Run("pass_returns_most_recent_ledger_clone", func(t *testing.T) {
		older := types.NewDebateLedger(1, 1.0)
		older.Positions = []types.AgentPosition{{AgentID: "a", Text: "pos", Turn: 0}}
		latest := types.NewDebateLedger(2, 2.0)
		latest.Positions = []types.AgentPosition{{AgentID: "a", Text: "pos2", Turn: 1}}
		records := []types.TurnRecord{
			{Turn: 0, AgentID: "a", Content: "turn 0"},
			{Turn: types.LedgerSentinelTurn, AgentID: types.LedgerAgentID, Ledger: older},
			{Turn: 1, AgentID: "b", Content: "turn 1"},
			{Turn: types.LedgerSentinelTurn, AgentID: types.LedgerAgentID, Ledger: latest},
		}

		seed := lastLedgerFromRecords(records)
		if seed == nil {
			t.Fatal("seed: got nil, want the most recent ledger")
		}
		if seed.Round != 2 {
			t.Fatalf("seed round: got %d, want 2 (most recent)", seed.Round)
		}
		seed.Positions[0].Text = "mutated"
		if latest.Positions[0].Text == "mutated" {
			t.Fatal("seed must be a deep clone, not aliased to the source record ledger")
		}
	})

	t.Run("fail_legacy_records_return_nil", func(t *testing.T) {
		records := []types.TurnRecord{
			{Turn: -1, AgentID: "moderator", Content: "seed"},
			{Turn: 0, AgentID: "a", Content: "turn 0"},
		}
		if seed := lastLedgerFromRecords(records); seed != nil {
			t.Fatalf("legacy records without a ledger should return nil, got %+v", seed)
		}
	})

	t.Run("fail_nil_records_return_nil", func(t *testing.T) {
		if seed := lastLedgerFromRecords(nil); seed != nil {
			t.Fatalf("nil records should return nil, got %+v", seed)
		}
	})
}
