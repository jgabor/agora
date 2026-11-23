package main

import (
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"

	"github.com/jgabor/agora/internal/session"
	"github.com/jgabor/agora/internal/types"
)

const (
	runReportDocumentType    = "agora.run.report"
	runReportDocumentVersion = 1
)

// runReportOutput is the single document `run` and `resume` print on stdout
// for a structured format. It carries the latest state per agent, not every
// record; `agora show` remains the full transcript view.
type runReportOutput struct {
	DocumentType          string                   `json:"document_type"`
	DocumentSchemaVersion int                      `json:"document_schema_version"`
	Topic                 string                   `json:"topic"`
	TranscriptPath        string                   `json:"transcript_path"`
	Failure               *string                  `json:"failure"`
	Outcome               runReportOutcome         `json:"outcome"`
	Scoring               bool                     `json:"scoring"`
	Agents                []runReportAgent         `json:"agents"`
	CanonicalProposal     *types.CanonicalProposal `json:"canonical_proposal"`
	UnresolvedObjections  []types.Objection        `json:"unresolved_objections"`
	DissentingAgentIDs    []string                 `json:"dissenting_agent_ids"`
	EvidenceGapClaimIDs   []string                 `json:"evidence_gap_claim_ids"`
	Synthesis             map[string]any           `json:"synthesis"`
	Totals                runReportTotals          `json:"totals"`

	terminalState *types.TerminalState
}

type runReportOutcome struct {
	Kind            types.TerminalOutcomeKind `json:"kind"`
	HaltReason      string                    `json:"halt_reason"`
	ProposalVersion int                       `json:"proposal_version"`
}

type runReportAgent struct {
	ID       string            `json:"id"`
	Model    string            `json:"model"`
	Score    *int              `json:"score"`
	Vote     *types.VoteChoice `json:"vote"`
	Position string            `json:"position"`
	Turns    int               `json:"turns"`
	Tokens   int               `json:"tokens"`
	Cost     float64           `json:"cost"`
}

type runReportTotals struct {
	Turns           int     `json:"turns"`
	Tokens          int     `json:"tokens"`
	Cost            float64 `json:"cost"`
	DurationSeconds float64 `json:"duration_seconds"`
}

// runReportData builds the report from a finished session. Outcome fields come
// from the persisted terminal state; a session that failed before reaching one
// reports a pending outcome with the failure text.
func runReportData(result session.Result, topic string) runReportOutput {
	terminalState := types.TerminalStateFromRecords(result.Records)
	report := runReportOutput{
		DocumentType:          runReportDocumentType,
		DocumentSchemaVersion: runReportDocumentVersion,
		Topic:                 topic,
		TranscriptPath:        result.OutputPath,
		Outcome:               runReportOutcome{Kind: types.OutcomePending, HaltReason: result.HaltedBy},
		Agents:                []runReportAgent{},
		UnresolvedObjections:  []types.Objection{},
		DissentingAgentIDs:    []string{},
		EvidenceGapClaimIDs:   []string{},
		Totals: runReportTotals{
			Turns:  result.Stats.TotalTurns,
			Tokens: result.Stats.TotalTokens,
			Cost:   result.Stats.TotalCost,
		},
		terminalState: terminalState,
	}
	if result.Failure != nil {
		failure := result.Failure.Error()
		report.Failure = &failure
	}

	var control *types.DeliberationControlState
	var config *types.DeliberationConfig
	if result.State != nil {
		control = result.State.Control
		config = result.State.Config
		if result.State.StartTime > 0 {
			report.Totals.DurationSeconds = float64(time.Now().UnixNano())/1e9 - result.State.StartTime
		}
	}
	votes := map[string]types.VoteChoice{}
	if terminalState != nil {
		control = terminalState.Control
		report.Outcome = runReportOutcome{
			Kind:            terminalState.Outcome.Kind,
			HaltReason:      terminalState.HaltReason,
			ProposalVersion: terminalState.Outcome.ProposalVersion,
		}
		report.CanonicalProposal = terminalState.CanonicalProposal
		report.DissentingAgentIDs = append(report.DissentingAgentIDs, terminalState.DissentingAgentIDs...)
		report.EvidenceGapClaimIDs = append(report.EvidenceGapClaimIDs, terminalState.EvidenceGapClaimIDs...)
		unresolved := make(map[string]bool, len(terminalState.Outcome.UnresolvedObjectionIDs))
		for _, id := range terminalState.Outcome.UnresolvedObjectionIDs {
			unresolved[id] = true
		}
		for _, objection := range terminalState.Objections {
			if unresolved[objection.ID] {
				report.UnresolvedObjections = append(report.UnresolvedObjections, objection)
			}
		}
		for _, vote := range terminalState.CurrentVotes {
			votes[vote.AgentID] = vote.Choice
		}
	}

	scores := map[string]*int{}
	if control != nil {
		// Later contributions overwrite earlier ones, so each agent reports the
		// score from its most recent turn, including a null one.
		for _, contribution := range control.Contributions {
			scores[contribution.AgentID] = contribution.Score
		}
	}
	positions := map[string]string{}
	for _, record := range result.Records {
		if !types.IsInternalAgent(record.AgentID) {
			positions[record.AgentID] = record.Content
		}
	}
	if config != nil {
		report.Scoring = config.Scoring
		for _, agent := range config.Agents {
			stats := result.Stats.PerAgent[agent.ID]
			entry := runReportAgent{
				ID: agent.ID, Model: agent.Model, Score: scores[agent.ID], Position: positions[agent.ID],
				Turns: stats.Turns, Tokens: stats.Tokens, Cost: stats.Cost,
			}
			if choice, ok := votes[agent.ID]; ok {
				entry.Vote = &choice
			}
			report.Agents = append(report.Agents, entry)
		}
	}

	if result.Synthesis != nil {
		report.Synthesis = make(map[string]any, len(result.Synthesis))
		for key, value := range result.Synthesis {
			// The embedded terminal state repeats the full control history.
			if key != "terminal_state" {
				report.Synthesis[key] = value
			}
		}
	}
	return report
}

// runReportContract describes the report for `agora prime`. Field names are
// read from the report types so the contract cannot drift from the document.
func runReportContract() map[string]any {
	return map[string]any{
		"document_type":           runReportDocumentType,
		"document_schema_version": runReportDocumentVersion,
		"commands":                []string{"agora run", "agora resume"},
		"formats":                 []string{formatJSON, formatMarkdown},
		"streams":                 "stdout carries exactly one report; live progress, prompts, and status lines go to stderr. A command that fails before the session starts prints no report.",
		"exit_status":             "0 for both consensus and no_consensus; non-zero when the session fails. Test outcome.kind to gate on consensus.",
		"fields":                  jsonFieldNames(reflect.TypeOf(runReportOutput{})),
		"agent_fields":            jsonFieldNames(reflect.TypeOf(runReportAgent{})),
	}
}

func jsonFieldNames(t reflect.Type) []string {
	names := make([]string, 0, t.NumField())
	for i := 0; i < t.NumField(); i++ {
		name, _, _ := strings.Cut(t.Field(i).Tag.Get("json"), ",")
		if name != "" && name != "-" {
			names = append(names, name)
		}
	}
	return names
}

func writeRunReport(w io.Writer, format, command string, report runReportOutput) error {
	if format == formatJSON {
		return writeJSON(w, command, report)
	}
	return writeString(w, runReportMarkdown(report))
}

func runReportMarkdown(report runReportOutput) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# Agora run report\n\n")
	fmt.Fprintf(&sb, "- **Document type:** `%s`\n", report.DocumentType)
	fmt.Fprintf(&sb, "- **Document schema version:** %d\n", report.DocumentSchemaVersion)
	fmt.Fprintf(&sb, "- **Topic:** %s\n", report.Topic)
	fmt.Fprintf(&sb, "- **Outcome:** %s\n", report.Outcome.Kind)
	fmt.Fprintf(&sb, "- **Halt reason:** %s\n", report.Outcome.HaltReason)
	if report.Failure != nil {
		fmt.Fprintf(&sb, "- **Failure:** %s\n", *report.Failure)
	}
	fmt.Fprintf(&sb, "- **Transcript:** `%s`\n", report.TranscriptPath)
	fmt.Fprintf(&sb, "- **Turns:** %d\n", report.Totals.Turns)
	fmt.Fprintf(&sb, "- **Tokens:** %d\n", report.Totals.Tokens)
	fmt.Fprintf(&sb, "- **Cost:** $%.6f\n", report.Totals.Cost)
	fmt.Fprintf(&sb, "- **Duration:** %.1fs\n", report.Totals.DurationSeconds)
	if len(report.DissentingAgentIDs) > 0 {
		fmt.Fprintf(&sb, "- **Dissenting agents:** %s\n", strings.Join(report.DissentingAgentIDs, ", "))
	}
	if len(report.EvidenceGapClaimIDs) > 0 {
		fmt.Fprintf(&sb, "- **Evidence gaps:** %s\n", strings.Join(report.EvidenceGapClaimIDs, ", "))
	}

	if len(report.Agents) > 0 {
		fmt.Fprintf(&sb, "\n## Agents\n\n")
		header, rule := "| Agent | Model |", "| --- | --- |"
		if report.Scoring {
			header, rule = header+" Score |", rule+" --- |"
		}
		fmt.Fprintf(&sb, "%s Vote | Turns | Tokens | Cost |\n", header)
		fmt.Fprintf(&sb, "%s --- | ---: | ---: | ---: |\n", rule)
		for _, agent := range report.Agents {
			fmt.Fprintf(&sb, "| %s | `%s` |", markdownCell(agent.ID), agent.Model)
			if report.Scoring {
				fmt.Fprintf(&sb, " %s |", scoreText(agent.Score))
			}
			vote := "no vote"
			if agent.Vote != nil {
				vote = string(*agent.Vote)
			}
			fmt.Fprintf(&sb, " %s | %d | %d | $%.6f |\n", vote, agent.Turns, agent.Tokens, agent.Cost)
		}
	}

	if report.CanonicalProposal != nil {
		fmt.Fprintf(&sb, "\n## Canonical proposal\n\n")
		fmt.Fprintf(&sb, "Version %d by %s:\n\n%s\n", report.CanonicalProposal.Version, report.CanonicalProposal.AuthorID, report.CanonicalProposal.Content)
	}
	if len(report.UnresolvedObjections) > 0 {
		fmt.Fprintf(&sb, "\n## Unresolved objections\n\n")
		for _, objection := range report.UnresolvedObjections {
			fmt.Fprintf(&sb, "- `%s` (%s): %s\n", objection.ID, objection.AgentID, objection.Summary)
		}
	}

	wrotePositions := false
	for _, agent := range report.Agents {
		if agent.Position == "" {
			continue
		}
		if !wrotePositions {
			fmt.Fprintf(&sb, "\n## Latest positions\n")
			wrotePositions = true
		}
		fmt.Fprintf(&sb, "\n### %s\n\n%s\n", agent.ID, agent.Position)
	}

	if report.Synthesis != nil {
		fmt.Fprintf(&sb, "\n## Synthesis\n\n")
		writeSynthesisBodyMarkdown(&sb, report.Synthesis, report.terminalState)
	}
	return sb.String()
}

// scoreText renders a nullable score. A missing score is never shown as zero.
func scoreText(score *int) string {
	if score == nil {
		return "no score"
	}
	return fmt.Sprintf("%d/%d", *score, types.MaxContributionScore)
}

func markdownCell(value string) string {
	return strings.ReplaceAll(value, "|", `\|`)
}
