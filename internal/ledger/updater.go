// Package ledger produces a typed DebateLedger from the running deliberation
// transcript by delegating to the meta-model.
//
// The ledger updater is deliberately separate from the post-hoc synthesis
// agent in internal/synthesis. The synthesis agent owns an essay-shape output
// contract ({key_arguments, points_of_agreement, unresolved_tensions,
// recommended_decision, confidence}) written once after deliberation
// completes. The ledger updater owns the typed-state-shape contract
// ({round, positions[], agreements[], cruxes[], draft{status,text}}) produced
// mid-deliberation after each completed round. Per Decision 1, the updater is
// a producer of typed data only: the prompt it builds asks the meta-model for
// a typed summary, and the orchestrator treats the result as opaque DATA
// injected into the per-turn envelope rather than composing downstream
// prompts from ledger contents.
package ledger

import (
	"fmt"
	"strings"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/llmutil"
	"github.com/jgabor/agora/internal/types"
)

// DefaultSystemPrompt instructs the meta-model to return structured JSON in
// the typed debate-ledger shape. The contract differs from the synthesis
// agent's essay-shape output: this prompt asks for a compact per-round state
// (positions, agreements, cruxes, draft), not a post-hoc narrative summary.
const DefaultSystemPrompt = `You are a mid-deliberation ledger updater. Your job is to read the transcript of a multi-agent deliberation round and produce the compact per-round debate state.

Your output must be valid JSON with this exact structure:
{
  "round": <integer>,
  "positions": [{"agent_id": "...", "text": "...", "turn": <integer>}],
  "agreements": [{"text": "...", "endorsers": ["agent-id", ...]}],
  "cruxes": [{"topic": "...", "views": [{"agent_id": "...", "stance": "..."}], "raised_at": <integer>}],
  "draft": {"status": "none|draft|final", "text": "..."}
}

Rules:
- positions: every active agent's most recently stated position, with the turn it was stated at
- agreements: points two or more agents have explicitly agreed on; list each endorser
- cruxes: unresolved disagreements currently in play, with each agent's stance
- draft: the current draft proposal, or {"status":"none"} when no draft exists
- when status is "none", omit text; when status is "draft" or "final", text is required
- omit fields you have no evidence for rather than inventing them`

const ledgerUpdaterAgentID = "ledger-updater"

// Updater produces a typed DebateLedger from the running deliberation
// transcript. It is a producer of typed state only; the orchestrator treats
// its output as opaque DATA injected into per-turn envelopes and must not
// compose prompts from ledger contents (Decision 1, firm).
type Updater struct {
	runner agent.Runner
}

// NewUpdater creates a new Updater that delegates summarization to the given
// runner. Mirrors evidence.NewPolicyCollector: the constructor takes a
// runner, methods return typed values.
func NewUpdater(runner agent.Runner) *Updater {
	return &Updater{runner: runner}
}

// Update consumes the transcript of a completed deliberation round and
// returns a typed ledger holding every active agent's most recent position,
// the points of agreement, the open cruxes, and either a current draft
// proposal or an explicit no-draft status. The model parameter is passed
// through so callers can resolve the meta-model via EffectiveMetaModel().
// On any model-output parse or validation failure the updater returns a
// structured error wrapping the underlying cause; it never silently produces
// an empty or partial ledger.
func (u *Updater) Update(records []types.TurnRecord, topic, model string) (*types.DebateLedger, error) {
	if strings.TrimSpace(topic) == "" {
		return nil, fmt.Errorf("ledger update: topic is required")
	}
	if strings.TrimSpace(model) == "" {
		return nil, fmt.Errorf("ledger update: meta-model is required")
	}
	if u == nil || u.runner == nil {
		return nil, fmt.Errorf("ledger update: runner unavailable")
	}

	envelope := map[string]any{
		"topic":       topic,
		"transcript":  formatTranscript(records),
		"total_turns": nonModeratorTurnCount(records),
	}

	ag := types.AgentConfig{
		ID:           ledgerUpdaterAgentID,
		Model:        model,
		SystemPrompt: DefaultSystemPrompt,
	}

	content, _, err := u.runner.Run(agent.WithReadOnlyAgentPrompt(ag), envelope)
	if err != nil {
		return nil, fmt.Errorf("ledger update: %w", err)
	}

	ledger, err := parseLedger(content)
	if err != nil {
		return nil, fmt.Errorf("ledger update: %w", err)
	}
	ledger.UpdatedAt = float64(time.Now().Unix())
	return ledger, nil
}

// UpdateDryRun returns a deterministic placeholder ledger without making
// model calls. Matches agent.AgentRunner.dryRunResponse convention: the
// returned ledger is valid against DebateLedger.Validate(), exercises the
// typed-state shape, and stays stable across repeated calls on the same
// records so downstream envelope injection is uniform across live and
// dry-run deliberations. Positions are derived deterministically from the
// most recent record per active agent; UpdatedAt is left at zero for
// determinism (callers stamp it on persist if needed).
func (u *Updater) UpdateDryRun(records []types.TurnRecord, topic string) *types.DebateLedger {
	return &types.DebateLedger{
		Round:     lastRound(records),
		Positions: latestPositions(records),
		Draft:     types.DraftProposal{Status: types.DraftStatusNone},
	}
}

// parseLedger parses the meta-model's response into a typed DebateLedger and
// validates it against DebateLedger.Validate(). It returns a structured
// error on llmutil.ExtractJSON failure or Validate() failure, including
// required-field-missing cases caught by Validate; it never silently
// produces an empty or partial ledger.
func parseLedger(content string) (*types.DebateLedger, error) {
	var ledger types.DebateLedger
	if err := llmutil.ExtractJSON(content, &ledger); err != nil {
		return nil, fmt.Errorf("parsing ledger JSON: %w", err)
	}
	if err := ledger.Validate(); err != nil {
		return nil, fmt.Errorf("validating ledger: %w", err)
	}
	return &ledger, nil
}

// formatTranscript renders TurnRecords into the flat text handed to the
// meta-model. Matches the synthesis agent's transcript shape so a single
// envelope convention carries across both the mid-deliberation updater and
// the post-hoc synthesizer.
func formatTranscript(records []types.TurnRecord) string {
	lines := make([]string, 0, len(records))
	for _, r := range records {
		lines = append(lines, fmt.Sprintf("[Turn %d] %s: %s", r.Turn, r.AgentID, r.Content))
	}
	return strings.Join(lines, "\n")
}

func nonModeratorTurnCount(records []types.TurnRecord) int {
	count := 0
	for _, r := range records {
		if r.AgentID != "moderator" {
			count++
		}
	}
	return count
}

func lastRound(records []types.TurnRecord) int {
	max := 0
	for _, r := range records {
		if r.Turn > max {
			max = r.Turn
		}
	}
	return max
}

func latestPositions(records []types.TurnRecord) []types.AgentPosition {
	latest := make(map[string]types.AgentPosition)
	order := make([]string, 0)
	for _, r := range records {
		if r.AgentID == "" || types.IsInternalAgent(r.AgentID) || r.Content == "" {
			continue
		}
		if _, seen := latest[r.AgentID]; !seen {
			order = append(order, r.AgentID)
		}
		latest[r.AgentID] = types.AgentPosition{
			AgentID: r.AgentID,
			Text:    r.Content,
			Turn:    r.Turn,
		}
	}
	positions := make([]types.AgentPosition, 0, len(order))
	for _, id := range order {
		positions = append(positions, latest[id])
	}
	return positions
}
