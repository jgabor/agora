package output

import (
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
			writeLine(w, renderTranscriptEvent(record, i+1))
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
			writeLine(w, renderTranscriptEvidence(record.Evidence, o.agentColorFor(record.AgentID)))
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

func renderTranscriptEvent(record types.TurnRecord, index int) string {
	width := outputWidth()
	contentWidth := width - 4
	var sb strings.Builder
	writeSection := sectionWriter(&sb, contentWidth)

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
	return theaterPanel(title, sb.String(), width, agentAccent(record.AgentID))
}
