package output

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"
	"github.com/jgabor/agora/internal/types"
)

// --- config preview ---

// ConfigPreview displays a preview of an auto-generated configuration
// before deliberation begins, including topology, agents, and level caps.
func (o *OutputManager) ConfigPreview(cfg *types.DeliberationConfig, level types.AutoLevel, caps types.LevelCaps) {
	o.registerCast(cfg)
	fmt.Println()
	fmt.Println(drawAutoConfigPanel(cfg, level, caps))
}

func drawAutoConfigPanel(cfg *types.DeliberationConfig, level types.AutoLevel, caps types.LevelCaps) string {
	return drawAutoConfigPanelAtWidth(cfg, level, caps, outputWidth())
}

func drawAutoConfigPanelAtWidth(cfg *types.DeliberationConfig, level types.AutoLevel, caps types.LevelCaps, width int) string {
	width = clampOutputWidth(width)
	contentWidth := width - 4

	shapeTitle := "Cast Preview"
	shapeLines := []string{
		fmt.Sprintf("Topology: %s", string(cfg.Topology)),
		fmt.Sprintf("Consensus threshold: %d", cfg.ConsensusThreshold),
	}
	if !plainOutput() {
		shapeTitle = "Run Shape"
		agreementTarget := "none"
		if cfg.ConsensusThreshold > 0 {
			agreementTarget = fmt.Sprintf("%d agents", cfg.ConsensusThreshold)
		}
		shapeLines = []string{
			fmt.Sprintf("Topology: %s", string(cfg.Topology)),
			fmt.Sprintf("Agreement target: %s", agreementTarget),
		}
	}
	capLines := []string{fmt.Sprintf("Level caps: %s", string(level))}
	if caps.MaxAgents == 0 {
		capLines = append(capLines, "Max agents: unlimited", "Max turns: unlimited", "Time limit: none")
	} else {
		capLines = append(capLines,
			fmt.Sprintf("Max agents: %d", caps.MaxAgents),
			fmt.Sprintf("Max turns: %d", caps.MaxTurns),
			fmt.Sprintf("Time limit: %ds", caps.TimeLimit),
		)
	}
	limitsTitle := "Run Bounds"
	if !plainOutput() {
		limitsTitle = "Limits"
		capLines = []string{fmt.Sprintf("Auto level: %s", string(level))}
		if caps.MaxAgents == 0 {
			capLines = append(capLines, "Agents: no cap", "Turns: no cap", "Time: no cap")
		} else {
			capLines = append(capLines,
				fmt.Sprintf("Agents: %d max", caps.MaxAgents),
				fmt.Sprintf("Turns: %d max", caps.MaxTurns),
				fmt.Sprintf("Time: %ds max", caps.TimeLimit),
			)
		}
	}
	agentLines := make([]string, 0, len(cfg.Agents))
	if plainOutput() {
		for i, a := range cfg.Agents {
			agentLines = append(agentLines, agentCastLine(i, a, true))
		}
	} else {
		agentLines = append(agentLines, agentCastTree(cfg.Agents, true, contentWidth))
	}
	agentsTitle := "Agents"
	if !plainOutput() {
		agentsTitle = "Cast"
	}

	if !plainOutput() {
		leftWidth, rightWidth := splitWidths(contentWidth, 2, 0.5)
		top := lipgloss.JoinHorizontal(
			lipgloss.Top,
			lipgloss.NewStyle().Width(leftWidth).Render(sectionBlock(shapeTitle, shapeLines, leftWidth)),
			lipgloss.NewStyle().Width(2).Render(""),
			lipgloss.NewStyle().Width(rightWidth).Render(sectionBlock(limitsTitle, capLines, rightWidth)),
		)
		body := lipgloss.JoinVertical(lipgloss.Left, top, "", sectionBlock(agentsTitle, agentLines, contentWidth))
		return theaterPanel("Generated Config", body, width, "6")
	}

	var sb strings.Builder
	writeSection := sectionWriter(&sb, contentWidth)
	writeSection(shapeTitle, shapeLines)
	writeSection(limitsTitle, capLines)
	writeSection(agentsTitle, agentLines)

	return theaterPanel("Generated Config", sb.String(), width, "6")
}

func agentCastLine(index int, agent types.AgentConfig, includeContext bool) string {
	member := types.CastMemberForAgent(index, agent)
	if !plainOutput() {
		return richAgentCastLine(member, agent, includeContext)
	}
	line := fmt.Sprintf("AGENT %s", castDisplay(castBadge(member), member))
	if member.ProviderModel != "" {
		line += fmt.Sprintf(" MODEL %s", member.ProviderModel)
	}
	if member.Color != "" {
		line += fmt.Sprintf(" COLOR %s", member.Color)
	}
	if includeContext {
		context := firstPromptLine(agent.SystemPrompt)
		if context != "" {
			line += fmt.Sprintf(" CONTEXT %s", context)
		}
	}
	return line
}

func richAgentCastLine(member types.CastMember, agent types.AgentConfig, includeContext bool) string {
	accent := member.Color
	if accent == "" {
		accent = agentAccent(agent.ID)
	}
	badge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(accent)).Render("● " + strings.Trim(castBadge(member), "[]"))
	parts := []string{badge}
	if member.Name != "" {
		parts = append(parts, lipgloss.NewStyle().Bold(true).Render(member.Name))
	}
	if member.Persona != "" {
		parts = append(parts, mutedStyle().Render(member.Persona))
	}

	lines := []string{strings.Join(parts, "  ")}
	metadata := make([]string, 0, 3)
	if member.ProviderModel != "" {
		metadata = append(metadata, "model "+member.ProviderModel)
	}
	if member.Color != "" {
		metadata = append(metadata, "color "+member.Color)
	}
	if includeContext {
		context := firstPromptLine(agent.SystemPrompt)
		if context != "" {
			metadata = append(metadata, "context "+context)
		}
	}
	if len(metadata) > 0 {
		lines = append(lines, mutedStyle().Render(strings.Join(metadata, " · ")))
	}
	return strings.Join(lines, "\n")
}

func agentCastTree(agents []types.AgentConfig, includeContext bool, width int) string {
	root := tree.Root("ensemble").
		Enumerator(tree.RoundedEnumerator).
		RootStyle(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))).
		EnumeratorStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		Width(width)

	for i, agent := range agents {
		member := types.CastMemberForAgent(i, agent)
		accent := member.Color
		if accent == "" {
			accent = agentAccent(agent.ID)
		}

		heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(accent)).Render(strings.Trim(castBadge(member), "[]"))
		if member.Name != "" {
			heading += " " + lipgloss.NewStyle().Bold(true).Render(member.Name)
		}
		if member.Persona != "" {
			heading += " " + mutedStyle().Render(member.Persona)
		}

		agentNode := tree.Root(heading).
			RootStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(accent))).
			Enumerator(tree.RoundedEnumerator).
			EnumeratorStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(accent))).
			IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8")))

		if member.ProviderModel != "" {
			agentNode.Child(mutedStyle().Render("model " + member.ProviderModel))
		}
		if member.Color != "" {
			agentNode.Child(mutedStyle().Render("color " + member.Color))
		}
		if includeContext {
			if context := firstPromptLine(agent.SystemPrompt); context != "" {
				agentNode.Child(mutedStyle().Render("context ") + renderInlineText(context, width-8))
			}
		}

		root.Child(agentNode)
	}
	return root.String()
}

func renderInlineText(text string, width int) string {
	if width <= 0 {
		return strings.TrimSpace(text)
	}
	return strings.ReplaceAll(renderTextBlock(text, width), "\n", " ")
}

func firstPromptLine(prompt string) string {
	if idx := strings.Index(prompt, "\n"); idx >= 0 {
		prompt = prompt[:idx]
	}
	return strings.TrimSpace(prompt)
}

// --- deliberation header ---

// DeliberationHeader prints the deliberation start banner.
func (o *OutputManager) DeliberationHeader(state *types.DeliberationState) {
	o.state = state
	o.totalCost = 0
	o.consensusStreak = 0
	o.registerCast(state.Config)
	fmt.Println()
	fmt.Println(drawDeliberationHeaderAtWidth(state, outputWidth()))
	fmt.Println()
}

func drawDeliberationHeaderAtWidth(state *types.DeliberationState, width int) string {
	width = clampOutputWidth(width)
	contentWidth := width - 4
	topicLines := []string{state.Topic}

	cast := make([]string, 0, len(state.Config.Agents))
	if plainOutput() {
		for i, a := range state.Config.Agents {
			cast = append(cast, agentCastLine(i, a, true))
		}
	}

	settings := []string{
		fmt.Sprintf("Topology: %s", string(state.Config.Topology)),
		fmt.Sprintf("Time limit: %ds", state.TimeLimit),
		fmt.Sprintf("Max turns: %d", state.MaxTurns),
		fmt.Sprintf("Window: %d", state.Window),
	}
	if !plainOutput() {
		settings = []string{
			fmt.Sprintf("Topology: %s", string(state.Config.Topology)),
			fmt.Sprintf("Time: %ds max", state.TimeLimit),
			fmt.Sprintf("Turns: %d max", state.MaxTurns),
			fmt.Sprintf("Context window: %d prior messages", state.Window),
		}
	}
	if state.Budget != nil {
		budgetLine := fmt.Sprintf("Budget: $%.2f", *state.Budget)
		if !plainOutput() {
			budgetLine = fmt.Sprintf("Budget: $%.2f max", *state.Budget)
		}
		settings = append(settings, budgetLine)
	}
	if state.Config.ConsensusThreshold > 0 {
		agreementLine := fmt.Sprintf("Consensus threshold: %d", state.Config.ConsensusThreshold)
		if !plainOutput() {
			agreementLine = fmt.Sprintf("Agreement target: %d agents", state.Config.ConsensusThreshold)
		}
		settings = append(settings, agreementLine)
	}
	settingsTitle := "Run Settings"
	if !plainOutput() {
		settingsTitle = "Limits"
	}

	if !plainOutput() {
		castWidth, settingsWidth := splitWidths(contentWidth, 2, 0.62)
		cast = append(cast, agentCastTree(state.Config.Agents, true, castWidth))
		castBlock := lipgloss.NewStyle().Width(castWidth).Render(sectionBlock("Cast", cast, castWidth))
		settingsBlock := lipgloss.NewStyle().Width(settingsWidth).Render(sectionBlock(settingsTitle, settings, settingsWidth))
		body := lipgloss.JoinVertical(
			lipgloss.Left,
			sectionBlock("Topic", topicLines, contentWidth),
			"",
			lipgloss.JoinHorizontal(lipgloss.Top, castBlock, lipgloss.NewStyle().Width(2).Render(""), settingsBlock),
		)
		return theaterPanel("Deliberation Start", body, width, "4")
	}

	var sb strings.Builder
	writeSection := sectionWriter(&sb, contentWidth)
	writeSection("Topic", topicLines)
	writeSection("Cast", cast)
	writeSection(settingsTitle, settings)

	return theaterPanel("Deliberation Start", sb.String(), width, "4")
}

// --- turn progress ---

// TurnProgress prints progress for a single turn.
func (o *OutputManager) TurnProgress(record types.TurnRecord, turn int, maxTurns int) {
	o.renderTurnProgress(os.Stdout, record, turn, maxTurns)
}

func (o *OutputManager) renderTurnProgress(w io.Writer, record types.TurnRecord, turn int, maxTurns int) {
	elapsed := fmt.Sprintf("%.1fs", record.Elapsed)
	tokensTotal := "?"
	if record.Tokens.Total != nil {
		tokensTotal = fmt.Sprintf("%d", *record.Tokens.Total)
	}
	if record.Cost != nil {
		o.totalCost += *record.Cost
	}
	costValue := "?"
	if record.Cost != nil {
		costValue = fmt.Sprintf("$%.6f", *record.Cost)
	}
	if o.state != nil && o.state.Budget != nil {
		costValue = boundedCostMetricValue(o.totalCost, *o.state.Budget)
	}
	if record.Consensus {
		o.consensusStreak++
	} else {
		o.consensusStreak = 0
	}

	agentDisplay := labelValue("AGENT", o.agentDisplayFor(record.AgentID))
	if !plainOutput() {
		agentDisplay = lipgloss.NewStyle().Foreground(lipgloss.Color(o.agentColorFor(record.AgentID))).Bold(true).Render(agentDisplay)
	}
	modelDisplay := labelValue("MODEL", "?")
	if record.Model != nil {
		modelDisplay = labelValue("MODEL", *record.Model)
	}
	metadata := strings.Join([]string{
		agentDisplay,
		modelDisplay,
		labelValue("ELAPSED", elapsed),
		labelValue("TOKENS", tokensTotal),
		labelValue("COST", costValue),
	}, " | ")
	if o.state != nil && o.state.StartTime > 0 && o.state.TimeLimit > 0 {
		elapsedTotal := float64(time.Now().UnixNano())/1e9 - o.state.StartTime
		metadata += " | " + labelValue("TIME", boundedSecondsMetricValue(elapsedTotal, o.state.TimeLimit))
	}
	if o.state != nil && o.state.Config != nil && o.state.Config.ConsensusThreshold > 0 {
		metadata += " | " + labelValue("CONSENSUS", boundedIntMetricValue(o.consensusStreak, o.state.Config.ConsensusThreshold))
	}

	turnValue := fmt.Sprintf("%d", turn+1)
	if maxTurns > 0 {
		turnValue = boundedIntMetricValue(turn+1, maxTurns)
	}
	if !plainOutput() {
		writeLine(w, o.renderTurnCard(record, turn, maxTurns, elapsed, tokensTotal, costValue))
		if o.mode == OutputVerbose {
			writeText(w, o.renderTurnDiagnostics(record, costValue))
		}
		if o.mode != OutputQuiet && record.Content != "" {
			writeLine(w)
			writeText(w, renderVerboseBody(record.Content, outputWidth(), o.agentColorFor(record.AgentID)))
		}
		return
	}
	writeFormat(w, "TURN %s | %s\n", turnValue, metadata)

	if record.Consensus {
		label := "[CONSENSUS]"
		if !plainOutput() {
			label = statusStyle("2").Render("✓ CONSENSUS")
		}
		writeFormat(w, "  %s %s\n", label, record.ConsensusStatement)
	}

	if o.mode == OutputVerbose {
		writeText(w, o.renderTurnDiagnostics(record, costValue))
	}

	if o.mode != OutputQuiet && record.Content != "" {
		writeLine(w)
		writeText(w, renderVerboseBody(record.Content, outputWidth(), o.agentColorFor(record.AgentID)))
	}
}

func (o *OutputManager) renderTurnDiagnostics(record types.TurnRecord, costValue string) string {
	parts := []string{"DIAGNOSTICS"}
	if record.Tokens.Input != nil {
		parts = append(parts, labelValue("INPUT_TOKENS", fmt.Sprintf("%d", *record.Tokens.Input)))
	}
	if record.Tokens.Output != nil {
		parts = append(parts, labelValue("OUTPUT_TOKENS", fmt.Sprintf("%d", *record.Tokens.Output)))
	}
	if record.Tokens.Reasoning != nil {
		parts = append(parts, labelValue("REASONING_TOKENS", fmt.Sprintf("%d", *record.Tokens.Reasoning)))
	}
	parts = append(parts, labelValue("CUMULATIVE_COST", costValue))
	if o.state != nil && o.state.Config != nil && o.state.Config.ConsensusThreshold > 0 {
		parts = append(parts, labelValue("CONSENSUS_STREAK", boundedIntMetricValue(o.consensusStreak, o.state.Config.ConsensusThreshold)))
	}
	return "  " + strings.Join(parts, " | ") + "\n"
}

func (o *OutputManager) renderTurnCard(record types.TurnRecord, turn int, maxTurns int, elapsed, tokensTotal, costValue string) string {
	width := outputWidth()
	accent := o.agentColorFor(record.AgentID)
	title := fmt.Sprintf("Turn %d", turn+1)
	if maxTurns > 0 {
		title = fmt.Sprintf("Turn %d of %d", turn+1, maxTurns)
	}

	model := "?"
	if record.Model != nil {
		model = *record.Model
	}

	var lines []string
	agent := statusStyle(accent).Render(o.agentDisplayFor(record.AgentID))
	lines = append(lines, richMetricLine("Agent", agent, accent))
	lines = append(lines, richMetricLine("Model", model, "7"))
	lines = append(lines, "")
	if maxTurns > 0 {
		percent := boundedPercent(float64(turn+1), float64(maxTurns))
		lines = append(lines, richMetricLine("Run", fmt.Sprintf("%d/%d (%d%%) %s", turn+1, maxTurns, percent, metricBar(percent)), "6"))
	} else {
		lines = append(lines, richMetricLine("Run", fmt.Sprintf("%d", turn+1), "6"))
	}
	lines = append(lines, richMetricLine("Elapsed", elapsed, "7"))
	lines = append(lines, richMetricLine("Tokens", tokensTotal, "7"))
	lines = append(lines, richMetricLine("Cost", costValue, "7"))
	if o.state != nil && o.state.StartTime > 0 && o.state.TimeLimit > 0 {
		elapsedTotal := float64(time.Now().UnixNano())/1e9 - o.state.StartTime
		lines = append(lines, richMetricLine("Time limit", boundedSecondsMetricValue(elapsedTotal, o.state.TimeLimit), "3"))
	}
	if o.state != nil && o.state.Config != nil && o.state.Config.ConsensusThreshold > 0 {
		lines = append(lines, richMetricLine("Agreement", boundedIntMetricValue(o.consensusStreak, o.state.Config.ConsensusThreshold), "2"))
	}
	if record.Consensus {
		statement := strings.TrimSpace(record.ConsensusStatement)
		if statement == "" {
			statement = "This turn agrees with the emerging decision."
		}
		lines = append(lines, "", statusStyle("2").Render("✓ Agreement"), statement)
	}

	return theaterPanel(title, strings.Join(lines, "\n"), width, accent)
}

func renderVerboseBody(content string, width int, borderColor string) string {
	width = clampOutputWidth(width)
	bodyWidth := width - 4
	var sb strings.Builder

	body := strings.TrimRight(content, "\n")
	if !plainOutput() && markdownLike(body) {
		if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(bodyWidth)); err == nil {
			if rendered, err := r.Render(body); err == nil {
				body = strings.TrimRight(rendered, "\n")
			}
		}
	}

	if !plainOutput() {
		for _, line := range strings.Split(body, "\n") {
			if line == "" {
				sb.WriteString("\n")
				continue
			}
			for _, wrapped := range wrapText(line, bodyWidth) {
				sb.WriteString(wrapped)
				sb.WriteString("\n")
			}
		}
		return theaterPanel("Agent Response", sb.String(), width, borderColor) + "\n"
	}

	sb.WriteString(mutedStyle().Render("AGENT CONTENT"))
	sb.WriteString("\n")
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			sb.WriteString("  |\n")
			continue
		}
		for _, wrapped := range wrapText(line, bodyWidth) {
			sb.WriteString("  | ")
			sb.WriteString(wrapped)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
	return sb.String()
}

func writeText(w io.Writer, text string) {
	_, _ = fmt.Fprint(w, text)
}

func writeLine(w io.Writer, args ...any) {
	_, _ = fmt.Fprintln(w, args...)
}

func writeFormat(w io.Writer, format string, args ...any) {
	_, _ = fmt.Fprintf(w, format, args...)
}

// --- final stats ---

// FinalStats prints final deliberation statistics.
func (o *OutputManager) FinalStats(records []types.TurnRecord, state *types.DeliberationState) {
	stats := types.ComputeStats(records)
	duration := float64(time.Now().UnixNano())/1e9 - state.StartTime

	actualTurns := 0
	for _, r := range records {
		if r.AgentID != "orchestrator" {
			actualTurns++
		}
	}

	fmt.Println()
	rows := [][]string{
		{"Turns completed", finalTurnsValue(actualTurns, state.MaxTurns)},
		{"Duration", finalDurationValue(duration, state.TimeLimit)},
		{"Total tokens", fmt.Sprintf("%d", stats.TotalTokens)},
		{"Total cost", finalCostValue(stats.TotalCost, state.Budget)},
	}
	if state.Config != nil && state.Config.ConsensusThreshold > 0 {
		rows = append(rows, []string{"Consensus streak", boundedIntMetricValue(finalConsensusStreak(records), state.Config.ConsensusThreshold)})
	}
	rows = append(rows, []string{"Halted by", state.HaltedBy})
	fmt.Println(drawStructuredTable("Deliberation Summary", []string{"Metric", "Value"}, rows, []string{"", ""}, outputWidth(), "6"))

	if len(stats.PerAgent) > 0 {
		fmt.Println()
		fmt.Println(drawStructuredTable("Per-Agent Stats", []string{"Agent", "Turns", "Tokens", "Cost"}, finalAgentRows(stats.PerAgent, state.Config), []string{"", "right", "right", "right"}, outputWidth(), "4"))
	}
}

func finalTurnsValue(value int, bound int) string {
	if bound <= 0 {
		return fmt.Sprintf("%d", value)
	}
	return boundedIntMetricValue(value, bound)
}

func finalDurationValue(value float64, bound int) string {
	if bound <= 0 {
		return fmt.Sprintf("%.1fs", value)
	}
	return boundedSecondsMetricValue(value, bound)
}

func finalCostValue(value float64, bound *float64) string {
	if bound == nil {
		return fmt.Sprintf("$%.6f", value)
	}
	return boundedCostMetricValue(value, *bound)
}

func finalConsensusStreak(records []types.TurnRecord) int {
	streak := 0
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Consensus {
			streak++
			continue
		}
		if records[i].AgentID != "orchestrator" {
			break
		}
	}
	return streak
}

func finalAgentRows(perAgent map[string]types.AgentTurnStats, cfg *types.DeliberationConfig) [][]string {
	rows := make([][]string, 0, len(perAgent))
	seen := make(map[string]bool, len(perAgent))
	if cfg != nil {
		for i, agent := range cfg.Agents {
			if s, ok := perAgent[agent.ID]; ok {
				member := types.CastMemberForAgent(i, agent)
				rows = append(rows, agentStatsRow(castDisplay(castBadge(member), member), s))
				seen[agent.ID] = true
			}
		}
	}

	var unknown []string
	for agentID := range perAgent {
		if !seen[agentID] {
			unknown = append(unknown, agentID)
		}
	}
	sort.Strings(unknown)
	for _, agentID := range unknown {
		rows = append(rows, agentStatsRow(unknownAgentBadge(agentID), perAgent[agentID]))
	}
	return rows
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func agentStatsRow(label string, s types.AgentTurnStats) []string {
	return []string{label, fmt.Sprintf("%d", s.Turns), fmt.Sprintf("%d", s.Tokens), fmt.Sprintf("$%.6f", s.Cost)}
}

// PrintStats displays standalone statistics without requiring a live deliberation state.
func (o *OutputManager) PrintStats(stats map[string]any) {
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

	fmt.Println(drawStructuredTable("Transcript Statistics", []string{"Metric", "Value"}, rows, []string{"", ""}, outputWidth(), "6"))

	if perAgent, ok := stats["per_agent"]; ok {
		if pa, ok := perAgent.(map[string]any); ok && len(pa) > 0 {
			fmt.Println()
			agentIDs := sortedKeys(pa)
			agentRows := make([][]string, 0, len(agentIDs))
			for _, agentID := range agentIDs {
				s := pa[agentID]
				if sm, ok := s.(map[string]any); ok {
					agentRows = append(agentRows, []string{
						unknownAgentBadge(agentID),
						fmt.Sprintf("%v", sm["turns"]),
						fmt.Sprintf("%v", sm["tokens"]),
						fmt.Sprintf("$%v", sm["cost"]),
					})
				}
			}
			fmt.Println(drawStructuredTable("Per-Agent Stats", []string{"Agent", "Turns", "Tokens", "Cost"}, agentRows, []string{"", "right", "right", "right"}, outputWidth(), "4"))
		}
	}

	if ce, ok := stats["consensus_events"]; ok {
		if events, ok := ce.([]any); ok && len(events) > 0 {
			fmt.Println()
			fmt.Println(renderConsensusEvents(events, outputWidth()))
		}
	}
}
