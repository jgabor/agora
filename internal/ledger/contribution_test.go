package ledger

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/types"
)

func contributionJSON(t *testing.T, fields map[string]any) string {
	t.Helper()
	base := map[string]any{
		"position":        "position",
		"responses":       []any{},
		"concessions":     []any{},
		"proposal_action": map[string]any{"kind": "none"},
		"objections":      []any{},
		"vote":            nil,
		"claims":          []any{},
	}
	for key, value := range fields {
		base[key] = value
	}
	data, err := json.Marshal(base)
	if err != nil {
		t.Fatalf("marshal contribution: %v", err)
	}
	return string(data)
}

func stateWithOpenProposal(t *testing.T) *types.DeliberationControlState {
	t.Helper()
	state := types.NewDeliberationControlState([]string{"alpha", "beta"}, 2)
	next, err := ProcessContribution(state, "alpha", 0, contributionJSON(t, map[string]any{
		"position": "create proposal one",
		"proposal_action": map[string]any{
			"kind": "create", "content": "proposal one",
		},
		"claims": []any{
			map[string]any{"id": "claim-1", "proposal_version": 1, "kind": "fact", "decisive": true, "source_refs": []int{0}},
		},
		"objections": []any{
			map[string]any{"id": "obj-1", "proposal_version": 1, "claim_id": "claim-1", "summary": "verify claim one"},
			map[string]any{"id": "obj-2", "proposal_version": 1, "summary": "retain compatibility"},
		},
		"vote": map[string]any{"proposal_version": 1, "choice": "endorse"},
	}))
	if err != nil {
		t.Fatalf("seed contribution: %v", err)
	}
	return next
}

func TestProcessContributionBindsEveryFamilyAndRevisesAtomically(t *testing.T) {
	state := stateWithOpenProposal(t)
	next, err := ProcessContribution(state, "beta", 1, contributionJSON(t, map[string]any{
		"position":    "revise while preserving the compatibility concern",
		"concessions": []string{"strict parsing needs a migration note"},
		"responses": []any{
			map[string]any{"objection_id": "obj-1", "response": "the revision narrows the factual claim", "disposition": "resolved", "rationale": "claim no longer controls the revision"},
		},
		"proposal_action": map[string]any{"kind": "revise", "content": "proposal two", "supersedes": 1},
		"claims": []any{
			map[string]any{"id": "claim-2", "proposal_version": 2, "kind": "inference", "decisive": true, "source_refs": []int{}},
		},
		"objections": []any{
			map[string]any{"id": "obj-3", "proposal_version": 2, "claim_id": "claim-2", "summary": "test the inference"},
		},
		"vote": map[string]any{"proposal_version": 2, "choice": "endorse"},
	}))
	if err != nil {
		t.Fatalf("ProcessContribution: %v", err)
	}
	if len(next.Contributions) != 2 {
		t.Fatalf("contributions: got %d, want 2", len(next.Contributions))
	}
	got := next.Contributions[1]
	if got.AgentID != "beta" || got.Turn != 1 || got.Position == "" || len(got.Responses) != 1 || len(got.Concessions) != 1 || len(got.Objections) != 1 || got.Vote == nil || len(got.Claims) != 1 {
		t.Fatalf("bound contribution: %+v", got)
	}
	if got.Objections[0].AgentID != "beta" || got.Vote.AgentID != "beta" || got.Claims[0].AgentID != "beta" {
		t.Fatalf("nested records are not bound to beta: %+v", got)
	}
	if next.CurrentProposalVersion != 2 || next.Proposals[1].AuthorID != "beta" {
		t.Fatalf("proposal revision: %+v", next.Proposals)
	}
	if next.IsCurrentVote(next.Votes[0]) || !next.IsCurrentVote(next.Votes[1]) {
		t.Fatalf("vote currency: %+v", next.Votes)
	}
	unresolved := next.UnresolvedObjections()
	if len(unresolved) != 2 || unresolved[0].ID != "obj-2" || unresolved[1].ID != "obj-3" {
		t.Fatalf("unresolved objections did not survive revision: %+v", unresolved)
	}
	if gaps := next.EvidenceGaps(); len(gaps) != 2 || gaps[0].ID != "claim-1" || gaps[1].ID != "claim-2" {
		t.Fatalf("evidence gaps did not survive revision: %+v", gaps)
	}
	if next.Convergence.CurrentEndorsements != 1 || next.Convergence.UnresolvedObjections != 2 || next.Convergence.EvidenceGaps != 2 {
		t.Fatalf("convergence signals: %+v", next.Convergence)
	}
	if !reflect.DeepEqual(state.CurrentVotes(), []types.ProposalVote{{AgentID: "alpha", ProposalVersion: 1, Choice: types.VoteEndorse}}) {
		t.Fatalf("input state mutated: %+v", state)
	}
}

func TestProcessContributionRejectsEachInvalidFamilyWithoutMutation(t *testing.T) {
	state := stateWithOpenProposal(t)
	tests := []struct {
		name   string
		fields map[string]any
		want   string
	}{
		{name: "position", fields: map[string]any{"position": " "}, want: "position must be non-empty"},
		{name: "responses", fields: map[string]any{"responses": []any{map[string]any{"objection_id": "missing", "response": "answer"}}}, want: "unknown objection"},
		{name: "concessions", fields: map[string]any{"concessions": []string{""}}, want: "concessions must be non-empty"},
		{name: "proposal action", fields: map[string]any{"proposal_action": map[string]any{"kind": "revise", "content": "v2", "supersedes": 9}}, want: "must supersede current version 1"},
		{name: "objections", fields: map[string]any{"objections": []any{map[string]any{"id": "obj-x", "proposal_version": 9, "summary": "wrong proposal"}}}, want: "must reference current proposal version 1"},
		{name: "vote", fields: map[string]any{"vote": map[string]any{"proposal_version": 9, "choice": "endorse"}}, want: "vote must reference current proposal version 1"},
		{name: "claims", fields: map[string]any{"claims": []any{map[string]any{"id": "claim-x", "proposal_version": 9, "kind": "fact", "decisive": true, "source_refs": []int{}}}}, want: "must reference current proposal version 1"},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			before, err := cloneControlState(state)
			if err != nil {
				t.Fatal(err)
			}
			next, err := ProcessContribution(state, "beta", i+1, contributionJSON(t, tc.fields))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error: got %v, want containing %q", err, tc.want)
			}
			if next != nil {
				t.Fatalf("next state: got %+v, want nil", next)
			}
			if !reflect.DeepEqual(state, before) {
				t.Fatal("rejected contribution mutated authoritative state")
			}
		})
	}
}

func TestProcessContributionMalformedCannotEndorseOrResolve(t *testing.T) {
	state := stateWithOpenProposal(t)
	before, err := cloneControlState(state)
	if err != nil {
		t.Fatal(err)
	}
	malformed := `{"position":"looks complete","responses":[{"objection_id":"obj-1","response":"done","disposition":"resolved","rationale":"claimed"}],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":{"proposal_version":1,"choice":"endorse"}}`
	next, err := ProcessContribution(state, "beta", 1, malformed)
	if err == nil || !strings.Contains(err.Error(), `missing "claims"`) {
		t.Fatalf("malformed error: %v", err)
	}
	if next != nil || !reflect.DeepEqual(state, before) {
		t.Fatal("malformed output created authoritative state")
	}
	if len(state.Dispositions) != 0 || len(state.Votes) != 1 {
		t.Fatalf("malformed output resolved or endorsed: dispositions=%+v votes=%+v", state.Dispositions, state.Votes)
	}
}

func TestProcessContributionIsDeterministicForIdenticalInput(t *testing.T) {
	output := contributionJSON(t, map[string]any{
		"position":        "create deterministic proposal",
		"proposal_action": map[string]any{"kind": "create", "content": "same proposal"},
		"vote":            map[string]any{"proposal_version": 1, "choice": "endorse"},
	})
	first, err := ProcessContribution(types.NewDeliberationControlState([]string{"alpha"}, 0), "alpha", 0, output)
	if err != nil {
		t.Fatal(err)
	}
	second, err := ProcessContribution(types.NewDeliberationControlState([]string{"alpha"}, 0), "alpha", 0, output)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("identical input produced different state:\nfirst=%+v\nsecond=%+v", first, second)
	}
}

func TestProcessContributionRejectsUnknownAndTrailingFields(t *testing.T) {
	state := types.NewDeliberationControlState([]string{"alpha"}, 0)
	for _, output := range []string{
		`{"position":"p","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[],"endorse":true}`,
		`{"position":"p","responses":[],"concessions":[],"proposal_action":{"kind":"none"},"objections":[],"vote":null,"claims":[]} {}`,
	} {
		if next, err := ProcessContribution(state, "alpha", 0, output); err == nil || next != nil {
			t.Fatalf("strict parse accepted %q: next=%+v err=%v", output, next, err)
		}
	}
}
