package kumbaja

import (
	"fmt"
	"strings"
	"time"
)

// ANSI escape codes for terminal styling.
const (
	ansiReset    = "\033[0m"
	ansiBold     = "\033[1m"
	ansiDim      = "\033[2m"
	ansiBlack    = "\033[30m"
	ansiRed      = "\033[31m"
	ansiGreen    = "\033[32m"
	ansiYellow   = "\033[33m"
	ansiBlue     = "\033[34m"
	ansiMagenta  = "\033[35m"
	ansiCyan     = "\033[36m"
	ansiWhite    = "\033[37m"
	ansiGray     = "\033[90m" // bright black
	ansiBRed     = "\033[91m" // bright red
	ansiBGreen   = "\033[92m"
	ansiBYellow  = "\033[93m"
	ansiBBlue    = "\033[94m"
	ansiBMagenta = "\033[95m"
	ansiBCyan    = "\033[96m"
	ansiBWhite   = "\033[97m"
)

// agentColorCode maps normalized agent IDs to ANSI color codes.
var agentColorCode = map[string]string{
	"orchestrator":  ansiCyan,
	"strategist":    ansiBlue,
	"domain_expert": ansiGreen,
	"skeptic":       ansiRed,
	"optimist":      ansiYellow,
	"user_advocate": ansiMagenta,
	"implementer":   ansiGray,
	"risk_officer":  ansiBRed,
	"synthesizer":   ansiBCyan,
}

// colorCode returns the ANSI color code for an agent ID, normalizing hyphens.
// Unknown agent IDs default to white.
func colorCode(agentID string) string {
	normalized := strings.ReplaceAll(agentID, "-", "_")
	if c, ok := agentColorCode[normalized]; ok {
		return c
	}
	return ansiWhite
}

// StatsDict is a standalone statistics dictionary used by PrintStats.
type StatsDict = map[string]any

// OutputManager manages terminal output for deliberation progress.
type OutputManager struct {
	verbose bool
}

// NewOutputManager creates a new OutputManager.
func NewOutputManager(verbose bool) *OutputManager {
	return &OutputManager{verbose: verbose}
}

// DeliberationHeader prints the deliberation start banner: topic panel, agent
// list, and settings line with styled text.
func (o *OutputManager) DeliberationHeader(state *DeliberationState) {
	fmt.Println()

	// Topic panel.
	fmt.Println(drawPanel(state.Topic, "Topic", ansiBlue))

	// Agent list — each agent ID colored by role.
	var agentParts []string
	for _, a := range state.Config.Agents {
		c := colorCode(a.ID)
		agentParts = append(agentParts, c+a.ID+ansiReset)
	}
	fmt.Printf("%sAgents:%s %s\n", ansiBold, ansiReset, strings.Join(agentParts, ", "))

	// Settings line.
	var settings string
	settings += fmt.Sprintf("topology=%s | ", string(state.Config.Topology))
	settings += fmt.Sprintf("time=%ds | ", state.TimeLimit)
	settings += fmt.Sprintf("max_turns=%d | ", state.MaxTurns)
	settings += fmt.Sprintf("window=%d", state.Window)
	if state.Budget != nil {
		settings += fmt.Sprintf(" | budget=$%.2f", *state.Budget)
	}
	if state.Config.ConsensusThreshold > 0 {
		settings += fmt.Sprintf(" | consensus_threshold=%d", state.Config.ConsensusThreshold)
	}
	fmt.Printf("%sSettings:%s %s\n", ansiBold, ansiReset, settings)

	fmt.Println()
}

// TurnProgress prints progress for a single turn: counter, agent name
// (colored), model, elapsed time, tokens, and cost. If the turn has a
// consensus marker, it prints a [CONSENSUS] line. In verbose mode it also
// prints the agent's content.
func (o *OutputManager) TurnProgress(record TurnRecord, turn int, maxTurns int) {
	elapsed := fmt.Sprintf("%.1fs", record.Elapsed)
	tokensTotal := "?"
	if record.Tokens.Total != nil {
		tokensTotal = fmt.Sprintf("%d", *record.Tokens.Total)
	}
	costStr := "?"
	if record.Cost != nil {
		costStr = fmt.Sprintf("$%.6f", *record.Cost)
	}

	c := colorCode(record.AgentID)
	agentDisplay := fmt.Sprintf("%s%s%s", c, record.AgentID, ansiReset)
	modelDisplay := ""
	if record.Model != nil {
		modelDisplay = fmt.Sprintf("%s%s%s", ansiDim, *record.Model, ansiReset)
	}

	fmt.Printf("[%d/%d] %s %s %s· %s · %stok · %s%s\n",
		turn+1, maxTurns, agentDisplay, modelDisplay,
		ansiDim, elapsed, tokensTotal, costStr, ansiReset)

	if record.Consensus {
		fmt.Printf("  %s✓ CONSENSUS:%s %s\n", ansiGreen+ansiBold, ansiReset, record.ConsensusStatement)
	}

	if o.verbose && record.Content != "" {
		fmt.Println()
		for _, line := range strings.Split(record.Content, "\n") {
			fmt.Printf("  %s│%s %s\n", ansiDim, ansiReset, line)
		}
		fmt.Println()
	}
}

// FinalStats prints final deliberation statistics: a summary table and a
// per-agent metrics table.
func (o *OutputManager) FinalStats(records []TurnRecord, state *DeliberationState) {
	stats := ComputeStats(records)
	duration := float64(time.Now().UnixNano())/1e9 - state.StartTime

	// Count actual turns (exclude orchestrator seed).
	actualTurns := 0
	for _, r := range records {
		if r.AgentID != "orchestrator" {
			actualTurns++
		}
	}

	fmt.Println()
	fmt.Println(drawTable("Deliberation Summary", []string{"Metric", "Value"}, [][]string{
		{"Turns completed", fmt.Sprintf("%d", actualTurns)},
		{"Duration", fmt.Sprintf("%.1fs", duration)},
		{"Total tokens", fmt.Sprintf("%d", stats.TotalTokens)},
		{"Total cost", fmt.Sprintf("$%.6f", stats.TotalCost)},
		{"Halted by", state.HaltedBy},
	}, []string{"", ""}))

	if len(stats.PerAgent) > 0 {
		fmt.Println()
		// Build per-agent rows.
		var rows [][]string
		for agentID, s := range stats.PerAgent {
			rows = append(rows, []string{
				agentID,
				fmt.Sprintf("%d", s.Turns),
				fmt.Sprintf("%d", s.Tokens),
				fmt.Sprintf("$%.6f", s.Cost),
			})
		}
		fmt.Println(drawTable("Per-Agent Stats", []string{"Agent", "Turns", "Tokens", "Cost"}, rows, []string{"", "right", "right", "right"}))
	}
}

// PrintStats displays standalone statistics (from the stats command) without
// requiring a live deliberation state.
func (o *OutputManager) PrintStats(stats StatsDict) {
	fmt.Println()

	var rows [][]string
	rows = append(rows, []string{"Total turns", fmt.Sprintf("%v", stats["total_turns"])})
	rows = append(rows, []string{"Total tokens", fmt.Sprintf("%v", stats["total_tokens"])})
	if cost, ok := stats["total_cost"]; ok {
		switch v := cost.(type) {
		case float64:
			rows = append(rows, []string{"Total cost", fmt.Sprintf("$%.6f", v)})
		default:
			rows = append(rows, []string{"Total cost", fmt.Sprintf("$%v", cost)})
		}
	}
	rows = append(rows, []string{"Avg turn duration", fmt.Sprintf("%vs", stats["avg_turn_duration_seconds"])})

	fmt.Println(drawTable("Transcript Statistics", []string{"Metric", "Value"}, rows, []string{"", ""}))

	// Per-agent table.
	if perAgent, ok := stats["per_agent"]; ok {
		if pa, ok := perAgent.(map[string]any); ok && len(pa) > 0 {
			fmt.Println()
			var agentRows [][]string
			for agentID, s := range pa {
				if sm, ok := s.(map[string]any); ok {
					agentRows = append(agentRows, []string{
						agentID,
						fmt.Sprintf("%v", sm["turns"]),
						fmt.Sprintf("%v", sm["tokens"]),
						fmt.Sprintf("$%v", sm["cost"]),
					})
				}
			}
			fmt.Println(drawTable("Per-Agent Stats", []string{"Agent", "Turns", "Tokens", "Cost"}, agentRows, []string{"", "right", "right", "right"}))
		}
	}

	// Consensus events.
	if ce, ok := stats["consensus_events"]; ok {
		if events, ok := ce.([]any); ok && len(events) > 0 {
			fmt.Println()
			fmt.Printf("%sConsensus Events:%s\n", ansiBold, ansiReset)
			for _, evt := range events {
				if em, ok := evt.(map[string]any); ok {
					fmt.Printf("  Turn %v [%v]: %v\n", em["turn"], em["agent_id"], em["statement"])
				}
			}
		}
	}
}

// SynthesizeHeader prints the synthesis section header.
func (o *OutputManager) SynthesizeHeader() {
	fmt.Println()
	fmt.Printf("%sSynthesis:%s\n", ansiBold, ansiReset)
}

// SynthesisResult displays the synthesis result with a recommendation panel,
// confidence level, key arguments, points of agreement, and unresolved
// tensions — each section using appropriate styling.
func (o *OutputManager) SynthesisResult(result map[string]any) {
	fmt.Println()

	if rec, ok := result["recommended_decision"]; ok {
		if s, ok := rec.(string); ok && s != "" {
			fmt.Println(drawPanel(s, "Recommended Decision", ansiGreen))
		}
	}

	confidence := "?"
	if c, ok := result["confidence"]; ok {
		if s, ok := c.(string); ok {
			confidence = s
		}
	}
	confColor := ansiWhite
	switch confidence {
	case "high":
		confColor = ansiGreen
	case "medium":
		confColor = ansiYellow
	case "low":
		confColor = ansiRed
	}
	fmt.Printf("%sConfidence:%s %s%s%s\n", ansiBold, ansiReset, confColor, confidence, ansiReset)

	if args, ok := result["key_arguments"]; ok {
		if list, ok := args.([]any); ok && len(list) > 0 {
			fmt.Println()
			fmt.Printf("%sKey Arguments:%s\n", ansiBold, ansiReset)
			for _, arg := range list {
				if s, ok := arg.(string); ok {
					fmt.Printf("  %s•%s %s\n", ansiDim, ansiReset, s)
				}
			}
		}
	}

	if agrs, ok := result["points_of_agreement"]; ok {
		if list, ok := agrs.([]any); ok && len(list) > 0 {
			fmt.Println()
			fmt.Printf("%sPoints of Agreement:%s\n", ansiGreen+ansiBold, ansiReset)
			for _, pt := range list {
				if s, ok := pt.(string); ok {
					fmt.Printf("  %s✓%s %s\n", ansiGreen, ansiReset, s)
				}
			}
		}
	}

	if tens, ok := result["unresolved_tensions"]; ok {
		if list, ok := tens.([]any); ok && len(list) > 0 {
			fmt.Println()
			fmt.Printf("%sUnresolved Tensions:%s\n", ansiYellow+ansiBold, ansiReset)
			for _, t := range list {
				if s, ok := t.(string); ok {
					fmt.Printf("  %s⚡%s %s\n", ansiYellow, ansiReset, s)
				}
			}
		}
	}
}

// Info prints an informational message.
func (o *OutputManager) Info(message string) {
	fmt.Printf("%sℹ%s %s\n", ansiBlue+ansiBold, ansiReset, message)
}

// Error prints an error message.
func (o *OutputManager) Error(message string) {
	fmt.Printf("%s✗%s %s\n", ansiRed+ansiBold, ansiReset, message)
}

// Success prints a success message.
func (o *OutputManager) Success(message string) {
	fmt.Printf("%s✓%s %s\n", ansiGreen+ansiBold, ansiReset, message)
}

// Delimiter prints a horizontal rule.
func (o *OutputManager) Delimiter() {
	fmt.Printf("%s%s%s\n", ansiDim, strings.Repeat("─", 60), ansiReset)
}

// drawPanel renders a bordered panel with a centered title at the top and
// the content inside. The border uses the given ANSI color.
func drawPanel(content, title string, borderColor string) string {
	const panelWidth = 76

	var sb strings.Builder

	contentLines := wrapText(content, panelWidth-4) // 2 padding + 2 borders

	// Top border with title embedded.
	sb.WriteString(borderColor)
	sb.WriteString("╭")
	titleStr := " " + title + " "
	if len(titleStr) < panelWidth-2 {
		// Center the title.
		remaining := panelWidth - 2 - len(titleStr)
		left := remaining / 2
		right := remaining - left
		sb.WriteString(strings.Repeat("─", left))
		sb.WriteString(ansiReset)
		sb.WriteString(ansiBold)
		sb.WriteString(titleStr)
		sb.WriteString(ansiReset)
		sb.WriteString(borderColor)
		sb.WriteString(strings.Repeat("─", right))
	} else {
		sb.WriteString(strings.Repeat("─", panelWidth-2))
	}
	sb.WriteString("╮")
	sb.WriteString(ansiReset)
	sb.WriteString("\n")

	// Content area.
	for _, line := range contentLines {
		sb.WriteString(borderColor)
		sb.WriteString("│")
		sb.WriteString(ansiReset)
		sb.WriteString(" ")
		sb.WriteString(line)
		// Pad to panelWidth-4 visual chars.
		padLen := panelWidth - 4 - visualLen(line)
		if padLen > 0 {
			sb.WriteString(strings.Repeat(" ", padLen))
		}
		sb.WriteString(" ")
		sb.WriteString(borderColor)
		sb.WriteString("│")
		sb.WriteString(ansiReset)
		sb.WriteString("\n")
	}

	// Bottom border.
	sb.WriteString(borderColor)
	sb.WriteString("╰")
	sb.WriteString(strings.Repeat("─", panelWidth-2))
	sb.WriteString("╯")
	sb.WriteString(ansiReset)

	return sb.String()
}

// wrapText wraps text to fit within maxWidth, splitting at word boundaries
// when possible but breaking mid-word if necessary.
func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}

	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		paragraph = strings.TrimSpace(paragraph)
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}

		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}

		currentLine := words[0]
		for _, word := range words[1:] {
			if visualLen(currentLine)+1+visualLen(word) <= maxWidth {
				currentLine += " " + word
			} else {
				lines = append(lines, currentLine)
				currentLine = word
			}
		}
		lines = append(lines, currentLine)
	}
	return lines
}

// visualLen approximates the visible character width of a string, ignoring
// ANSI escape sequences.
func visualLen(s string) int {
	n := 0
	inEscape := false
	for i := 0; i < len(s); i++ {
		if inEscape {
			if s[i] == 'm' {
				inEscape = false
			}
			continue
		}
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			inEscape = true
			i++ // skip the '['
			continue
		}
		n++
	}
	return n
}

// drawTable renders a formatted table with a title, headers, and rows.
// Column alignments can be "left", "right", or "" (defaults to left).
func drawTable(title string, headers []string, rows [][]string, aligns []string) string {
	if len(headers) == 0 {
		return ""
	}

	// Determine column widths from headers and data.
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = visualLen(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i >= len(colWidths) {
				break
			}
			if l := visualLen(cell); l > colWidths[i] {
				colWidths[i] = l
			}
		}
	}

	// Minimum column width of 4 chars.
	for i := range colWidths {
		if colWidths[i] < 4 {
			colWidths[i] = 4
		}
	}

	var sb strings.Builder

	// Title.
	if title != "" {
		sb.WriteString(ansiBold)
		sb.WriteString(title)
		sb.WriteString(ansiReset)
		sb.WriteString("\n")
	}

	// Separator line.
	sep := ""
	for i, w := range colWidths {
		if i > 0 {
			sep += "─┼─"
		}
		sep += strings.Repeat("─", w)
	}
	if sep != "" {
		sb.WriteString(ansiDim)
		sb.WriteString(sep)
		sb.WriteString(ansiReset)
		sb.WriteString("\n")
	}

	// Header row.
	sb.WriteString(ansiCyan)
	for i, h := range headers {
		if i > 0 {
			sb.WriteString(" │ ")
		}
		sb.WriteString(padCell(h, colWidths[i], "left"))
	}
	sb.WriteString(ansiReset)
	sb.WriteString("\n")

	// Separator after header.
	sb.WriteString(ansiDim)
	sb.WriteString(sep)
	sb.WriteString(ansiReset)
	sb.WriteString("\n")

	// Data rows.
	for _, row := range rows {
		for i, cell := range row {
			if i > 0 {
				sb.WriteString(" │ ")
			}
			if i >= len(colWidths) {
				break
			}
			align := "left"
			if i < len(aligns) && aligns[i] != "" {
				align = aligns[i]
			}
			sb.WriteString(padCell(cell, colWidths[i], align))
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

// padCell pads a string to the given width, aligning left or right.
func padCell(s string, width int, align string) string {
	vLen := visualLen(s)
	if vLen >= width {
		return s
	}
	pad := strings.Repeat(" ", width-vLen)
	if align == "right" {
		return pad + s
	}
	return s + pad
}
