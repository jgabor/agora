// Package synthesis produces a structured summary from a deliberation transcript.
package synthesis

import (
	"fmt"
	"strings"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/llmutil"
	"github.com/jgabor/agora/internal/types"
)

// DefaultSystemPrompt instructs the synthesis model to return structured JSON.
const DefaultSystemPrompt = `You are a deliberation synthesis agent. Your job is to read the full transcript
of a multi-agent deliberation and produce a structured summary.

Your output must be valid JSON with this exact structure:
{
  "key_arguments": ["argument 1", "argument 2", ...],
  "points_of_agreement": ["agreement 1", ...],
  "unresolved_tensions": ["tension 1", ...],
  "recommended_decision": "...",
  "confidence": "high|medium|low"
}

Be concise but thorough. Capture the essential insights from the deliberation.
`

// Synthesize runs a synthesis agent to summarize the deliberation.
func Synthesize(runner agent.Runner, records []types.TurnRecord, topic, model string) map[string]any {
	engine := synthesisEngine{runner: runner}
	return engine.synthesize(records, topic, model)
}

type synthesisEngine struct {
	runner agent.Runner
}

func (se *synthesisEngine) synthesize(records []types.TurnRecord, topic, model string) map[string]any {
	transcriptText := se.formatTranscript(records)

	totalTurns := 0
	for _, r := range records {
		if r.AgentID != "moderator" {
			totalTurns++
		}
	}

	envelope := map[string]any{
		"topic":       topic,
		"transcript":  transcriptText,
		"total_turns": totalTurns,
	}

	synthAgent := types.AgentConfig{
		ID:           "synthesizer",
		Model:        model,
		SystemPrompt: DefaultSystemPrompt,
	}

	content, _, err := se.runner.Run(agent.WithReadOnlyAgentPrompt(synthAgent), envelope)
	if err != nil {
		return map[string]any{
			"key_arguments":        []any{},
			"points_of_agreement":  []any{},
			"unresolved_tensions":  []any{},
			"recommended_decision": "Synthesis could not run: the model was unable to produce a response.",
			"confidence":           "low",
		}
	}

	var parsed map[string]any
	if err := llmutil.ExtractJSON(content, &parsed); err != nil {
		return map[string]any{
			"key_arguments":        []any{},
			"points_of_agreement":  []any{},
			"unresolved_tensions":  []any{},
			"recommended_decision": "Synthesis could not produce a structured summary. The model response is available in the transcript.",
			"confidence":           "low",
		}
	}

	return parsed
}

func (se *synthesisEngine) formatTranscript(records []types.TurnRecord) string {
	var lines []string
	for _, r := range records {
		lines = append(lines, fmt.Sprintf("[Turn %d] %s: %s", r.Turn, r.AgentID, r.Content))
	}
	return strings.Join(lines, "\n")
}
