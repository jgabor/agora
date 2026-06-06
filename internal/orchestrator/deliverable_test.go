package orchestrator

import (
	"testing"

	"github.com/jgabor/agora/internal/types"
)

func TestParseDeliverableGate(t *testing.T) {
	topic := "Output must be exactly three laws in Asimovian form."
	gate := ParseDeliverableGate(topic)
	if gate == nil || gate.MinItems != 3 {
		t.Fatalf("gate: got %#v, want min_items=3", gate)
	}
}

func TestDeliverablePresent(t *testing.T) {
	gate := &types.DeliverableGate{MinItems: 3}
	records := []types.TurnRecord{
		{AgentID: "moderator", Content: "seed"},
		{AgentID: "architect", Content: "1. An agent must not drift.\n2. An agent must spend compute.\n3. An agent must pursue parallelism."},
	}
	if !DeliverablePresent(records, gate) {
		t.Fatal("expected deliverable present")
	}
}

func TestDeliverableAbsent(t *testing.T) {
	gate := &types.DeliverableGate{MinItems: 3}
	records := []types.TurnRecord{
		{AgentID: "architect", Content: "We should refine the laws later."},
	}
	if DeliverablePresent(records, gate) {
		t.Fatal("expected deliverable absent")
	}
}
