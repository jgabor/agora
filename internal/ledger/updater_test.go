package ledger

import (
	"errors"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/synthesis"
	"github.com/jgabor/agora/internal/types"
)

type stubRunner struct {
	content  string
	metadata *types.RunMetadata
	err      error
	calls    int
	captured types.AgentConfig
	env      map[string]any
}

func (s *stubRunner) Run(ag types.AgentConfig, envelope map[string]any) (string, *types.RunMetadata, error) {
	s.calls++
	s.captured = ag
	s.env = envelope
	if s.err != nil {
		return "", nil, s.err
	}
	return s.content, s.metadata, nil
}

func sampleRecords() []types.TurnRecord {
	return []types.TurnRecord{
		{Turn: -1, AgentID: "moderator", Content: "Deliberate the validation contract."},
		{Turn: 0, AgentID: "agent-a", Content: "Validate at parse time with strict loading."},
		{Turn: 1, AgentID: "agent-b", Content: "Validate at load time with warn-continue."},
	}
}

func TestUpdaterProducesTypedLedger(t *testing.T) {
	const fullLedgerJSON = `{
  "round": 1,
  "positions": [
    {"agent_id": "agent-a", "text": "validate at parse time", "turn": 0},
    {"agent_id": "agent-b", "text": "validate at load time", "turn": 1}
  ],
  "agreements": [
    {"text": "config validation is needed", "endorsers": ["agent-a", "agent-b"]}
  ],
  "cruxes": [
    {"topic": "where validation runs", "views": [{"agent_id": "agent-a", "stance": "parse time"}, {"agent_id": "agent-b", "stance": "load time"}], "raised_at": 0}
  ],
  "draft": {"status": "draft", "text": "strict parse with fall-back load"}
}`
	stub := &stubRunner{content: fullLedgerJSON}
	updater := NewUpdater(stub)

	ledger, err := updater.Update(sampleRecords(), "validation contract", "meta-model")
	if err != nil {
		t.Fatalf("Update: unexpected error: %v", err)
	}
	if ledger == nil {
		t.Fatal("Update: ledger is nil")
	}
	if err := ledger.Validate(); err != nil {
		t.Fatalf("ledger.Validate: %v", err)
	}
	if ledger.Round != 1 {
		t.Errorf("Round: got %d, want 1", ledger.Round)
	}
	if len(ledger.Positions) != 2 {
		t.Fatalf("Positions: got %d, want 2", len(ledger.Positions))
	}
	if ledger.Positions[0].AgentID != "agent-a" || ledger.Positions[1].AgentID != "agent-b" {
		t.Errorf("Positions.AgentID order: got %+v", ledger.Positions)
	}
	if ledger.Draft.Status != types.DraftStatusDraft || ledger.Draft.Text != "strict parse with fall-back load" {
		t.Errorf("Draft: got %+v", ledger.Draft)
	}
	if ledger.UpdatedAt == 0 {
		t.Error("UpdatedAt: got 0, want current epoch time set by Update after parsing")
	}
	if stub.captured.ID != "ledger-updater" {
		t.Errorf("agent.ID: got %q, want ledger-updater", stub.captured.ID)
	}
	if stub.captured.Model != "meta-model" {
		t.Errorf("agent.Model: got %q, want meta-model", stub.captured.Model)
	}
	if !strings.HasPrefix(stub.captured.SystemPrompt, agent.ReadOnlyHint) {
		t.Errorf("prompt missing read-only hint: %q", stub.captured.SystemPrompt)
	}
	if !strings.Contains(stub.captured.SystemPrompt, `"round"`) || !strings.Contains(stub.captured.SystemPrompt, `"positions"`) {
		t.Errorf("prompt missing typed-ledger shape: %q", stub.captured.SystemPrompt)
	}
	if strings.Contains(stub.captured.SystemPrompt, "key_arguments") {
		t.Errorf("prompt leaked synthesis essay-shape contract: %q", stub.captured.SystemPrompt)
	}
	if stub.env["topic"] != "validation contract" {
		t.Errorf("envelope topic: got %v, want validation contract", stub.env["topic"])
	}
}

func TestUpdaterSurfacesAgreements(t *testing.T) {
	const ledgerJSON = `{
  "round": 2,
  "positions": [
    {"agent_id": "agent-a", "text": "shared evidence", "turn": 2},
    {"agent_id": "agent-b", "text": "shared evidence plus examples", "turn": 3}
  ],
  "agreements": [
    {"text": "both agents accept the shared evidence base", "endorsers": ["agent-a", "agent-b"]}
  ],
  "cruxes": [],
  "draft": {"status": "none"}
}`
	stub := &stubRunner{content: ledgerJSON}
	updater := NewUpdater(stub)

	ledger, err := updater.Update(sampleRecords(), "shared evidence base", "meta-model")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(ledger.Agreements) == 0 {
		t.Fatal("Agreements: empty, want non-empty when transcript surfaced agreement points")
	}
	if !strings.Contains(ledger.Agreements[0].Text, "shared evidence base") {
		t.Errorf("Agreements[0].Text: got %q", ledger.Agreements[0].Text)
	}
	if len(ledger.Agreements[0].Endorsers) < 2 {
		t.Errorf("Agreements[0].Endorsers: got %v, want at least 2", ledger.Agreements[0].Endorsers)
	}
}

func TestUpdaterSurfacesCruxes(t *testing.T) {
	const ledgerJSON = `{
  "round": 2,
  "positions": [
    {"agent_id": "agent-a", "text": "position A", "turn": 2},
    {"agent_id": "agent-b", "text": "position B", "turn": 3}
  ],
  "agreements": [],
  "cruxes": [
    {"topic": "exception handling between parse and load", "views": [{"agent_id": "agent-a", "stance": "fail fast at parse"}, {"agent_id": "agent-b", "stance": "defer to load"}], "raised_at": 1}
  ],
  "draft": {"status": "none"}
}`
	stub := &stubRunner{content: ledgerJSON}
	updater := NewUpdater(stub)

	ledger, err := updater.Update(sampleRecords(), "exception handling", "meta-model")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(ledger.Cruxes) == 0 {
		t.Fatal("Cruxes: empty, want non-empty when transcript surfaced unaddressed objections")
	}
	if !strings.Contains(ledger.Cruxes[0].Topic, "exception handling") {
		t.Errorf("Cruxes[0].Topic: got %q", ledger.Cruxes[0].Topic)
	}
	if len(ledger.Cruxes[0].Views) < 2 {
		t.Errorf("Cruxes[0].Views: got %v, want at least 2", ledger.Cruxes[0].Views)
	}
}

func TestUpdaterRejectsMalformedOutput(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{
			name:    "non-JSON text",
			content: "the ledger is most curious and not parseable",
			want:    "parsing ledger JSON",
		},
		{
			name:    "position missing agent_id",
			content: `{"round":1,"positions":[{"text":"missing agent_id"}]}`,
			want:    "agent_id must be non-empty",
		},
		{
			name:    "draft with status but no text",
			content: `{"round":1,"draft":{"status":"draft"}}`,
			want:    "draft with status",
		},
		{
			name:    "negative round",
			content: `{"round":-1}`,
			want:    "ledger round must be >= 0",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stub := &stubRunner{content: tc.content}
			updater := NewUpdater(stub)

			ledger, err := updater.Update(sampleRecords(), "validation contract", "meta-model")
			if err == nil {
				t.Fatalf("Update: expected error containing %q, got nil ledger %+v", tc.want, ledger)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error: got %q, want containing %q", err, tc.want)
			}
			if ledger != nil {
				t.Errorf("ledger: expected nil on malformed output, got %+v", ledger)
			}
		})
	}
}

func TestUpdaterDryRunIsDeterministic(t *testing.T) {
	stub := &stubRunner{err: errors.New("dry-run must not call the runner")}
	updater := NewUpdater(stub)
	records := sampleRecords()

	first := updater.UpdateDryRun(records, "validation contract")
	second := updater.UpdateDryRun(records, "validation contract")

	if first == nil || second == nil {
		t.Fatal("UpdateDryRun: ledger is nil")
	}
	if err := first.Validate(); err != nil {
		t.Fatalf("first.UpdateDryRun.Validate: %v", err)
	}
	if stub.calls != 0 {
		t.Fatalf("runner calls: got %d, want 0 for dry-run", stub.calls)
	}
	if first.Round != 0 || second.Round != 0 {
		t.Errorf("Round: got first=%d second=%d, want 0 (orchestrator stamps authoritative round; UpdateDryRun leaves it unstamped)", first.Round, second.Round)
	}
	if len(first.Positions) != 2 || len(second.Positions) != 2 {
		t.Fatalf("Positions: got first=%d second=%d, want 2 agent positions", len(first.Positions), len(second.Positions))
	}
	if first.Positions[0].AgentID != "agent-a" || first.Positions[1].AgentID != "agent-b" {
		t.Errorf("first agent order: got %+v, want [agent-a, agent-b]", first.Positions)
	}
	if first.Draft.Status != types.DraftStatusNone || second.Draft.Status != types.DraftStatusNone {
		t.Errorf("Draft.Status: got first=%q second=%q, want none", first.Draft.Status, second.Draft.Status)
	}
	if first.UpdatedAt != second.UpdatedAt {
		t.Errorf("UpdatedAt: got first=%v second=%v, want equal for determinism", first.UpdatedAt, second.UpdatedAt)
	}
}

func TestSynthesisPathRetainsEssayShapeContract(t *testing.T) {
	const essayShapeJSON = `{
  "key_arguments": ["arg1", "arg2"],
  "points_of_agreement": ["point1"],
  "unresolved_tensions": ["tension1"],
  "recommended_decision": "ship as drafted",
  "confidence": "high"
}`
	stub := &stubRunner{content: essayShapeJSON}
	result := synthesis.Synthesize(stub, sampleRecords(), "validation contract", "meta-model")

	if result == nil {
		t.Fatal("Synthesize: result is nil")
	}
	if _, ok := result["key_arguments"]; !ok {
		t.Errorf("synthesis output: missing key_arguments (essay-shape contract)")
	}
	if _, ok := result["positions"]; ok {
		t.Errorf("synthesis output: unexpected positions key (ledger-shape leak)")
	}
	if _, ok := result["recommended_decision"]; !ok {
		t.Errorf("synthesis output: missing recommended_decision (essay-shape contract)")
	}
	if _, ok := result["round"]; ok {
		t.Errorf("synthesis output: unexpected round key (ledger-shape leak)")
	}
}
