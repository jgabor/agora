package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jgabor/agora/internal/types"
)

// RenderTranscript displays a stored transcript with the same turn styling used
// while a deliberation is running.
func RenderTranscript(w io.Writer, records []types.TurnRecord) {
	NewOutputManagerWithMode(OutputNormal).RenderTranscript(w, records)
}

// RenderTranscript displays a stored transcript with this output manager's mode.
func (o *OutputManager) RenderTranscript(w io.Writer, records []types.TurnRecord) {
	if metadata := transcriptMetadata(records); metadata != nil {
		o.registerCastMembers(metadata.Cast, metadata.Config)
	}
	maxTurns := transcriptMaxTurn(records)
	fallbackTurn := 0
	for i, record := range records {
		if i > 0 {
			writeLine(w)
		}

		record.AgentID = transcriptAgentID(record.AgentID)
		if transcriptEventRecord(record) {
			writeLine(w, renderTranscriptEvent(o.renderer, record, i+1))
			continue
		}
		if record.AgentID == "synthesizer" {
			writeLine(w, renderTranscriptSynthesis(o.renderer, record, i+1))
			continue
		}

		displayTurn := record.Turn
		if displayTurn < 0 {
			displayTurn = fallbackTurn
		}
		o.renderTurnProgress(w, record, displayTurn, maxTurns)
		fallbackTurn++
		if record.Evidence != nil {
			writeLine(w)
			writeLine(w, renderTranscriptEvidence(o.renderer, record.Evidence, o.agentColorFor(record.AgentID)))
		}
	}
}

func transcriptMetadata(records []types.TurnRecord) *types.TranscriptMetadata {
	for _, record := range records {
		if record.Transcript != nil {
			return record.Transcript
		}
	}
	return nil
}

func transcriptMaxTurn(records []types.TurnRecord) int {
	maxTurn := -1
	count := 0
	for _, record := range records {
		if transcriptEventRecord(record) {
			continue
		}
		count++
		if record.Turn > maxTurn {
			maxTurn = record.Turn
		}
	}
	if maxTurn >= 0 {
		return maxTurn + 1
	}
	return count
}

func transcriptAgentID(agentID string) string {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "unknown"
	}
	return agentID
}

func transcriptEventRecord(record types.TurnRecord) bool {
	return strings.TrimSpace(record.AgentID) == "orchestrator" && record.Turn < 0
}

func renderTranscriptSynthesis(r Renderer, record types.TurnRecord, index int) string {
	var result map[string]any
	if err := json.Unmarshal([]byte(record.Content), &result); err != nil {
		return renderTranscriptEvent(r, record, index)
	}

	width := outputWidth()
	var sections []string

	if rec, ok := result["recommended_decision"]; ok {
		if s, ok := rec.(string); ok && s != "" {
			sections = append(sections, r.ProseSection("Recommended Decision", s, width, "2"))
		}
	}

	if c, ok := result["confidence"]; ok {
		if s, ok := c.(string); ok {
			sections = append(sections, r.Table("Synthesis Confidence", []string{"Metric", "Value"}, [][]string{{"Confidence", s}}, []string{"", ""}, width, "6"))
		}
	}

	if args, ok := result["key_arguments"]; ok {
		if list, ok := args.([]any); ok && len(list) > 0 {
			items := make([]string, len(list))
			for i, v := range list {
				if s, ok := v.(string); ok {
					items[i] = "* " + s
				}
			}
			sections = append(sections, r.ListSection("Key Arguments", items, width, "6"))
		}
	}

	if agrs, ok := result["points_of_agreement"]; ok {
		if list, ok := agrs.([]any); ok && len(list) > 0 {
			items := make([]string, len(list))
			for i, v := range list {
				if s, ok := v.(string); ok {
					items[i] = "[CONSENSUS] " + s
				}
			}
			sections = append(sections, r.ListSection("Points of Agreement", items, width, "2"))
		}
	}

	if tens, ok := result["unresolved_tensions"]; ok {
		if list, ok := tens.([]any); ok && len(list) > 0 {
			items := make([]string, len(list))
			for i, v := range list {
				if s, ok := v.(string); ok {
					items[i] = "[WARNING] " + s
				}
			}
			sections = append(sections, r.ListSection("Unresolved Tensions", items, width, "3"))
		}
	}

	body := strings.Join(sections, "\n")
	title := "Synthesis"
	return r.Panel(title, body, width, "6")
}

func renderTranscriptEvent(r Renderer, record types.TurnRecord, index int) string {
	width := outputWidth()
	contentWidth := width - 4
	var sb strings.Builder
	writeSection := sectionWriter(r, &sb, contentWidth)

	metadata := []string{
		fmt.Sprintf("RECORD %d", index),
		fmt.Sprintf("TURN %d", record.Turn),
		fmt.Sprintf("AGENT %s", transcriptAgentID(record.AgentID)),
	}
	if record.Model != nil && strings.TrimSpace(*record.Model) != "" {
		metadata = append(metadata, fmt.Sprintf("MODEL %s", strings.TrimSpace(*record.Model)))
	}
	writeSection("Record", metadata)

	if content := strings.TrimSpace(record.Content); content != "" {
		writeSection("Content", strings.Split(content, "\n"))
	}
	if record.Evidence != nil {
		writeTranscriptEvidenceSections(writeSection, record.Evidence)
	}
	if record.Consensus {
		statement := strings.TrimSpace(record.ConsensusStatement)
		if statement == "" {
			statement = "This turn agrees with the emerging decision."
		}
		writeSection("Consensus", []string{statement})
	}

	title := "Transcript Event"
	if record.Evidence != nil {
		title = "Transcript Evidence"
	}
	return r.Panel(title, sb.String(), width, agentAccent(record.AgentID))
}
