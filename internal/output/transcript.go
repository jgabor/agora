package output

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

// RenderTranscript displays a stored transcript with the same turn styling used
// while a deliberation is running.
func RenderTranscript(w io.Writer, records []types.TurnRecord) {
	compatibility, err := transcript.CompatibilityFromRecords(records)
	if err != nil {
		compatibility = transcript.CompatibilityState{}
	}
	RenderTranscriptWithCompatibility(w, records, compatibility)
}

// RenderTranscriptWithCompatibility displays a stored transcript with its
// loader-authenticated compatibility action.
func RenderTranscriptWithCompatibility(w io.Writer, records []types.TurnRecord, compatibility transcript.CompatibilityState) {
	NewOutputManagerWithMode(OutputNormal).RenderTranscriptWithCompatibility(w, records, compatibility)
}

// RenderTranscript displays a stored transcript with this output manager's mode.
func (o *OutputManager) RenderTranscript(w io.Writer, records []types.TurnRecord) {
	compatibility, err := transcript.CompatibilityFromRecords(records)
	if err != nil {
		compatibility = transcript.CompatibilityState{}
	}
	o.RenderTranscriptWithCompatibility(w, records, compatibility)
}

// RenderTranscriptWithCompatibility renders with the protocol classification
// returned by the strict loader, preserving v1 migration provenance.
func (o *OutputManager) RenderTranscriptWithCompatibility(w io.Writer, records []types.TurnRecord, compatibility transcript.CompatibilityState) {
	if metadata := transcriptMetadata(records); metadata != nil {
		o.registerCastMembers(metadata.Cast, metadata.Config)
	}
	terminalState := types.TerminalStateFromRecords(records)
	maxTurns := transcriptMaxTurn(records)
	fallbackTurn := 0
	for i, record := range records {
		if i > 0 {
			writeLine(w)
		}

		record.AgentID = transcriptAgentID(record.AgentID)
		if transcriptLedgerRecord(record) {
			writeLine(w, o.renderTranscriptLedger(record, i+1))
			continue
		}
		if transcriptEventRecord(record) {
			writeLine(w, o.renderTranscriptEvent(record, i+1, terminalState))
			continue
		}
		if record.AgentID == "synthesizer" {
			writeLine(w, o.renderTranscriptSynthesis(record, i+1, terminalState))
			continue
		}

		displayTurn := record.Turn
		if displayTurn < 0 {
			displayTurn = fallbackTurn
		}
		o.renderTurnProgress(w, record, displayTurn, maxTurns, terminalState)
		fallbackTurn++
		if record.Evidence != nil {
			writeLine(w)
			writeLine(w, renderTranscriptEvidence(o.renderer, record.Evidence, o.agentColorFor(record.AgentID)))
		}
	}
	if terminalState != nil {
		if legacyConsensus := types.LegacyConsensusDataFromRecords(records); legacyConsensus != nil {
			writeLine(w)
			writeLine(w, o.renderer.SectionBlock("Legacy consensus compatibility (non-authoritative)", legacyConsensusLines(legacyConsensus), outputWidth()))
		}
	}
	if compatibility.ResumeAction != "" {
		writeLine(w)
		writeLine(w, o.renderer.SectionBlock("Protocol compatibility", protocolCompatibilityLines(compatibility), outputWidth()))
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
		if transcriptEventRecord(record) || transcriptLedgerRecord(record) {
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
	return strings.TrimSpace(record.AgentID) == "moderator" && record.Turn < 0
}

func transcriptLedgerRecord(record types.TurnRecord) bool {
	return record.Ledger != nil || strings.TrimSpace(record.AgentID) == types.LedgerAgentID
}

func (o *OutputManager) renderTranscriptLedger(record types.TurnRecord, index int) string {
	r := o.renderer
	width := outputWidth()
	round := 0
	positions := 0
	agreements := 0
	cruxes := 0
	draftStatus := string(types.DraftStatusNone)
	if record.Ledger != nil {
		round = record.Ledger.Round
		positions = len(record.Ledger.Positions)
		agreements = len(record.Ledger.Agreements)
		cruxes = len(record.Ledger.Cruxes)
		draftStatus = string(record.Ledger.Draft.Status)
		if draftStatus == "" {
			draftStatus = string(types.DraftStatusNone)
		}
	}
	metadata := []string{
		fmt.Sprintf("RECORD %d", index),
		fmt.Sprintf("TURN %d", record.Turn),
		fmt.Sprintf("AGENT %s", transcriptAgentID(record.AgentID)),
	}
	summary := fmt.Sprintf("Ledger · Round %d: positions %d, agreements %d, cruxes %d, draft %s",
		round, positions, agreements, cruxes, draftStatus)
	body := strings.Join([]string{strings.Join(metadata, " · "), summary}, "\n")
	return r.Panel("Ledger Snapshot", body, width, o.agentColorFor(record.AgentID))
}

func (o *OutputManager) renderTranscriptSynthesis(record types.TurnRecord, index int, terminalState *types.TerminalState) string {
	var result map[string]any
	if err := json.Unmarshal([]byte(record.Content), &result); err != nil {
		return o.renderTranscriptEvent(record, index, terminalState)
	}

	r := o.renderer
	width := outputWidth()
	var sections []string
	labels := LabelsForTerminalState(terminalState)

	if rec, ok := result["recommended_decision"]; ok {
		if s, ok := rec.(string); ok && s != "" {
			sections = append(sections, r.ProseSection(labels.Recommendation, ModelRecommendationProse(terminalState, s), width, "2"))
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
					items[i] = labels.AgreementMark + " " + s
				}
			}
			sections = append(sections, r.ListSection(labels.Agreements, items, width, "2"))
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
	if claims := ClaimEvidenceLines(result["claims"]); len(claims) > 0 {
		sections = append(sections, r.ListSection("Claim Evidence", claims, width, "4"))
	}

	body := strings.Join(sections, "\n")
	title := "Synthesis"
	return r.Panel(title, body, width, "6")
}

func (o *OutputManager) renderTranscriptEvent(record types.TurnRecord, index int, terminalState *types.TerminalState) string {
	r := o.renderer
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
	if record.Control != nil && record.Control.Phase == types.PhaseTerminal {
		outcome := record.Control.Outcome
		lines := []string{
			fmt.Sprintf("kind: %s", outcome.Kind),
			fmt.Sprintf("proposal version: %d", outcome.ProposalVersion),
		}
		if outcome.Reason != "" {
			lines = append(lines, "reason: "+outcome.Reason)
		}
		lines = append(lines,
			"dissenting agents: "+strings.Join(outcome.DissentingAgentIDs, ", "),
			"unresolved objections: "+strings.Join(outcome.UnresolvedObjectionIDs, ", "),
			"evidence gaps: "+strings.Join(outcome.EvidenceGapClaimIDs, ", "),
		)
		writeSection("Terminal outcome", lines)
		if terminalState != nil {
			writeSection("Terminal state", terminalStateLines(terminalState))
		}
	}
	if record.Consensus || record.ConsensusIgnored {
		if terminalState != nil {
			writeSection("Legacy consensus marker (non-authoritative)", []string{legacyConsensusMarkerText(record)})
		} else if record.Consensus {
			statement := strings.TrimSpace(record.ConsensusStatement)
			if statement == "" {
				statement = "This turn agrees with the emerging decision."
			}
			writeSection("Consensus", []string{statement})
		}
	}

	title := "Transcript Event"
	if record.Evidence != nil {
		title = "Transcript Evidence"
	}
	return r.Panel(title, sb.String(), width, o.agentColorFor(record.AgentID))
}

func terminalStateLines(state *types.TerminalState) []string {
	if state == nil {
		return nil
	}
	lines := []string{
		"phase: " + string(state.Phase),
		fmt.Sprintf("proposal version: %d", state.ProposalVersion),
		"halt reason: " + state.HaltReason,
		"dissenting agents: " + strings.Join(state.DissentingAgentIDs, ", "),
		"evidence gaps: " + strings.Join(state.EvidenceGapClaimIDs, ", "),
		fmt.Sprintf("convergence: endorsements %d/%d; minimum rounds %d; unresolved objections %d; evidence gaps %d",
			state.Convergence.CurrentEndorsements,
			state.Convergence.RequiredEndorsements,
			state.Convergence.MinimumRounds,
			state.Convergence.UnresolvedObjections,
			state.Convergence.EvidenceGaps,
		),
	}
	if state.CanonicalProposal != nil {
		lines = append(lines, fmt.Sprintf("canonical proposal: v%d by %s: %s", state.CanonicalProposal.Version, state.CanonicalProposal.AuthorID, state.CanonicalProposal.Content))
	}
	if len(state.CurrentVotes) > 0 {
		votes := make([]string, 0, len(state.CurrentVotes))
		for _, vote := range state.CurrentVotes {
			votes = append(votes, fmt.Sprintf("%s=%s", vote.AgentID, vote.Choice))
		}
		lines = append(lines, "current votes: "+strings.Join(votes, ", "))
	}
	if len(state.Objections) > 0 {
		objections := make([]string, 0, len(state.Objections))
		for _, objection := range state.Objections {
			objections = append(objections, objection.ID)
		}
		lines = append(lines, "objections: "+strings.Join(objections, ", "))
	}
	if len(state.Dispositions) > 0 {
		dispositions := make([]string, 0, len(state.Dispositions))
		for _, disposition := range state.Dispositions {
			dispositions = append(dispositions, fmt.Sprintf("%s=%s", disposition.ObjectionID, disposition.Status))
		}
		lines = append(lines, "dispositions: "+strings.Join(dispositions, ", "))
	}
	if state.Evidence != nil {
		lines = append(lines, fmt.Sprintf("evidence: %s (%d source references)", state.Evidence.Summary, len(state.Evidence.SourceReferences)))
	}
	return lines
}

func protocolCompatibilityLines(compatibility transcript.CompatibilityState) []string {
	return []string{
		"resume action: " + compatibility.ResumeAction,
		fmt.Sprintf("legacy: %t", compatibility.Legacy),
		fmt.Sprintf("pre-contract active: %t", compatibility.PreContractActive),
		fmt.Sprintf("terminal: %t", compatibility.Terminal),
	}
}

func legacyConsensusLines(data *types.LegacyConsensusData) []string {
	if data == nil {
		return nil
	}
	lines := []string{
		fmt.Sprintf("markers: %d", len(data.Markers)),
		fmt.Sprintf("trailing streak: %d", data.TrailingStreak),
	}
	for _, marker := range data.Markers {
		status := "marker"
		if marker.Ignored {
			status = "ignored marker"
		}
		line := fmt.Sprintf("turn %d [%s]: %s", marker.Turn, marker.AgentID, status)
		if marker.Statement != "" {
			line += ": " + marker.Statement
		}
		lines = append(lines, line)
	}
	return lines
}
