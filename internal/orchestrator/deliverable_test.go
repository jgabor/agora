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

func TestDeliverablePresentForStateUsesCanonicalProposalContent(t *testing.T) {
	gate := &types.DeliverableGate{MinItems: 3}
	records := []types.TurnRecord{{AgentID: "architect", Content: "I agree with the proposal."}}
	canonical := types.NewDeliberationControlState([]string{"architect"}, 0)
	canonical.CurrentProposalVersion = 1
	canonical.Proposals = []types.CanonicalProposal{{
		Version: 1, AuthorID: "architect",
		Content: "1. An agent must verify claims.\n2. An agent must preserve evidence.\n3. An agent must record dissent.",
	}}
	if !DeliverablePresentForState(records, canonical, gate) {
		t.Fatal("canonical proposal artifact should satisfy the deliverable gate")
	}

	missing := types.NewDeliberationControlState([]string{"architect"}, 0)
	missing.CurrentProposalVersion = 1
	missing.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "architect", Content: "proposal without the required artifact"}}
	if DeliverablePresentForState(records, missing, gate) {
		t.Fatal("ordinary agreement prose should not satisfy an absent canonical artifact")
	}
}
