package output

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/jgabor/agora/internal/types"
)

// --- config preview ---

// ConfigPreview displays a preview of an auto-generated configuration
// before deliberation begins, including topology, agents, and level caps.
func (o *OutputManager) ConfigPreview(cfg *types.DeliberationConfig, level types.AutoLevel, caps types.LevelCaps) {
	o.registerCast(cfg)
	fmt.Println()
	fmt.Println(drawAutoConfigPanel(o.renderer, cfg, level, caps))
}

func drawAutoConfigPanel(r Renderer, cfg *types.DeliberationConfig, level types.AutoLevel, caps types.LevelCaps) string {
	return drawAutoConfigPanelAtWidth(r, cfg, level, caps, outputWidth())
}

func drawAutoConfigPanelAtWidth(r Renderer, cfg *types.DeliberationConfig, level types.AutoLevel, caps types.LevelCaps, width int) string {
	width = clampOutputWidth(width)
	contentWidth := width - 4

	shapeTitle := "Cast Preview"
	shapeLines := []string{
		fmt.Sprintf("Topology: %s", string(cfg.Topology)),
		fmt.Sprintf("Consensus threshold: %d", cfg.ConsensusThreshold),
	}
	if r.IsRich() {
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
	if r.IsRich() {
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
	if !r.IsRich() {
		for i, a := range cfg.Agents {
			agentLines = append(agentLines, agentCastLine(r, i, a, true))
		}
	} else {
		agentLines = append(agentLines, agentCastTree(r, cfg.Agents, true, contentWidth))
	}
	agentsTitle := "Agents"
	if r.IsRich() {
		agentsTitle = "Cast"
	}

	if r.IsRich() {
		return richAutoConfigPanelAtWidth(r, width, contentWidth, shapeTitle, limitsTitle, agentsTitle, shapeLines, capLines, agentLines)
	}

	var sb strings.Builder
	writeSection := sectionWriter(r, &sb, contentWidth)
	writeSection(shapeTitle, shapeLines)
	writeSection(limitsTitle, capLines)
	writeSection(agentsTitle, agentLines)

	return r.Panel("Generated Config", sb.String(), width, "6")
}

func agentCastLine(r Renderer, index int, agent types.AgentConfig, includeContext bool) string {
	member := types.CastMemberForAgent(index, agent)
	if r.IsRich() {
		return richAgentCastLine(r, member, agent, includeContext)
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

func richAgentCastLine(r Renderer, member types.CastMember, agent types.AgentConfig, includeContext bool) string {
	accent := member.Color
	if accent == "" {
		accent = agentAccent(agent.ID)
	}
	badge := r.Styled("● "+strings.Trim(castBadge(member), "[]"), accent)
	parts := []string{badge}
	if member.Name != "" {
		parts = append(parts, r.Styled(member.Name, accent))
	}
	if member.Persona != "" {
		parts = append(parts, r.Muted(member.Persona))
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
		lines = append(lines, r.Muted(strings.Join(metadata, " · ")))
	}
	return strings.Join(lines, "\n")
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
	fmt.Println(drawDeliberationHeaderAtWidth(o.renderer, state, outputWidth()))
	fmt.Println()
}

func drawDeliberationHeaderAtWidth(r Renderer, state *types.DeliberationState, width int) string {
	width = clampOutputWidth(width)
	contentWidth := width - 4
	topicLines := []string{state.Topic}

	cast := make([]string, 0, len(state.Config.Agents))
	if !r.IsRich() {
		for i, a := range state.Config.Agents {
			cast = append(cast, agentCastLine(r, i, a, true))
		}
	}

	settings := []string{
		fmt.Sprintf("Topology: %s", string(state.Config.Topology)),
		fmt.Sprintf("Time limit: %ds", state.TimeLimit),
		fmt.Sprintf("Max turns: %d", state.MaxTurns),
		fmt.Sprintf("Window: %d", state.Window),
	}
	if r.IsRich() {
		settings = []string{
			fmt.Sprintf("Topology: %s", string(state.Config.Topology)),
			fmt.Sprintf("Time: %ds max", state.TimeLimit),
			fmt.Sprintf("Turns: %d max", state.MaxTurns),
			fmt.Sprintf("Context window: %d prior messages", state.Window),
		}
	}
	if state.Budget != nil {
		budgetLine := fmt.Sprintf("Budget: $%.2f", *state.Budget)
		if r.IsRich() {
			budgetLine = fmt.Sprintf("Budget: $%.2f max", *state.Budget)
		}
		settings = append(settings, budgetLine)
	}
	if state.Config.ConsensusThreshold > 0 {
		agreementLine := fmt.Sprintf("Consensus threshold: %d", state.Config.ConsensusThreshold)
		if r.IsRich() {
			agreementLine = fmt.Sprintf("Agreement target: %d agents", state.Config.ConsensusThreshold)
		}
		settings = append(settings, agreementLine)
	}
	settingsTitle := "Run Settings"
	if r.IsRich() {
		settingsTitle = "Limits"
	}

	if r.IsRich() {
		return richDeliberationHeaderAtWidth(r, width, contentWidth, topicLines, cast, settings, settingsTitle, state.Config.Agents)
	}

	var sb strings.Builder
	writeSection := sectionWriter(r, &sb, contentWidth)
	writeSection("Topic", topicLines)
	writeSection("Cast", cast)
	writeSection(settingsTitle, settings)

	return r.Panel("Deliberation Start", sb.String(), width, "4")
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
		costValue = boundedCostMetricValue(o.renderer, o.totalCost, *o.state.Budget)
	}
	if record.Consensus {
		o.consensusStreak++
	} else {
		o.consensusStreak = 0
	}

	agentDisplay := labelValue("AGENT", o.agentDisplayFor(record.AgentID))
	if o.renderer.IsRich() {
		agentDisplay = o.renderer.Styled(agentDisplay, o.agentColorFor(record.AgentID))
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
		metadata += " | " + labelValue("TIME", boundedSecondsMetricValue(o.renderer, elapsedTotal, o.state.TimeLimit))
	}
	if o.state != nil && o.state.Config != nil && o.state.Config.ConsensusThreshold > 0 {
		metadata += " | " + labelValue("CONSENSUS", boundedIntMetricValue(o.renderer, o.consensusStreak, o.state.Config.ConsensusThreshold))
	}

	turnValue := fmt.Sprintf("%d", turn+1)
	if maxTurns > 0 {
		turnValue = boundedIntMetricValue(o.renderer, turn+1, maxTurns)
	}
	if o.renderer.IsRich() {
		writeLine(w, o.renderTurnCard(record, turn, maxTurns, elapsed, tokensTotal, costValue))
		if o.mode == OutputVerbose {
			writeText(w, o.renderTurnDiagnostics(record, costValue))
		}
		if o.mode != OutputQuiet && record.Content != "" {
			writeLine(w)
			writeText(w, o.renderer.VerboseBody(record.Content, outputWidth(), o.agentColorFor(record.AgentID)))
		}
		return
	}
	writeFormat(w, "TURN %s | %s\n", turnValue, metadata)

	if record.Consensus {
		label := o.renderer.Styled("✓ CONSENSUS", "2")
		if !o.renderer.IsRich() {
			label = "[CONSENSUS]"
		}
		writeFormat(w, "  %s %s\n", label, record.ConsensusStatement)
	}

	if o.mode == OutputVerbose {
		writeText(w, o.renderTurnDiagnostics(record, costValue))
	}

	if o.mode != OutputQuiet && record.Content != "" {
		writeLine(w)
		writeText(w, o.renderer.VerboseBody(record.Content, outputWidth(), o.agentColorFor(record.AgentID)))
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
		parts = append(parts, labelValue("CONSENSUS_STREAK", boundedIntMetricValue(o.renderer, o.consensusStreak, o.state.Config.ConsensusThreshold)))
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
	agent := o.renderer.Styled(o.agentDisplayFor(record.AgentID), accent)
	lines = append(lines, richMetricLine("Agent", agent, accent))
	lines = append(lines, richMetricLine("Model", model, "7"))
	lines = append(lines, "")
	if maxTurns > 0 {
		percent := boundedPercent(float64(turn+1), float64(maxTurns))
		lines = append(lines, richMetricLine("Run", fmt.Sprintf("%d/%d (%d%%) %s", turn+1, maxTurns, percent, o.renderer.MetricBar(percent)), "6"))
	} else {
		lines = append(lines, richMetricLine("Run", fmt.Sprintf("%d", turn+1), "6"))
	}
	lines = append(lines, richMetricLine("Elapsed", elapsed, "7"))
	lines = append(lines, richMetricLine("Tokens", tokensTotal, "7"))
	lines = append(lines, richMetricLine("Cost", costValue, "7"))
	if o.state != nil && o.state.StartTime > 0 && o.state.TimeLimit > 0 {
		elapsedTotal := float64(time.Now().UnixNano())/1e9 - o.state.StartTime
		lines = append(lines, richMetricLine("Time limit", boundedSecondsMetricValue(o.renderer, elapsedTotal, o.state.TimeLimit), "3"))
	}
	if o.state != nil && o.state.Config != nil && o.state.Config.ConsensusThreshold > 0 {
		lines = append(lines, richMetricLine("Agreement", boundedIntMetricValue(o.renderer, o.consensusStreak, o.state.Config.ConsensusThreshold), "2"))
	}
	if record.Consensus {
		statement := strings.TrimSpace(record.ConsensusStatement)
		if statement == "" {
			statement = "This turn agrees with the emerging decision."
		}
		lines = append(lines, "", o.renderer.Styled("✓ Agreement", "2"), statement)
	}

	return o.renderer.Panel(title, strings.Join(lines, "\n"), width, accent)
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
		{"Turns completed", finalTurnsValue(o.renderer, actualTurns, state.MaxTurns)},
		{"Duration", finalDurationValue(o.renderer, duration, state.TimeLimit)},
		{"Total tokens", fmt.Sprintf("%d", stats.TotalTokens)},
		{"Total cost", finalCostValue(o.renderer, stats.TotalCost, state.Budget)},
	}
	if state.Config != nil && state.Config.ConsensusThreshold > 0 {
		rows = append(rows, []string{"Consensus streak", boundedIntMetricValue(o.renderer, finalConsensusStreak(records), state.Config.ConsensusThreshold)})
	}
	rows = append(rows, []string{"Halted by", state.HaltedBy})
	fmt.Println(o.renderer.Table("Deliberation Summary", []string{"Metric", "Value"}, rows, []string{"", ""}, outputWidth(), "6"))

	if len(stats.PerAgent) > 0 {
		fmt.Println()
		fmt.Println(o.renderer.Table("Per-Agent Stats", []string{"Agent", "Turns", "Tokens", "Cost"}, finalAgentRows(stats.PerAgent, state.Config), []string{"", "right", "right", "right"}, outputWidth(), "4"))
	}
}

func finalTurnsValue(r Renderer, value int, bound int) string {
	if bound <= 0 {
		return fmt.Sprintf("%d", value)
	}
	return boundedIntMetricValue(r, value, bound)
}

func finalDurationValue(r Renderer, value float64, bound int) string {
	if bound <= 0 {
		return fmt.Sprintf("%.1fs", value)
	}
	return boundedSecondsMetricValue(r, value, bound)
}

func finalCostValue(r Renderer, value float64, bound *float64) string {
	if bound == nil {
		return fmt.Sprintf("$%.6f", value)
	}
	return boundedCostMetricValue(r, value, *bound)
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

	fmt.Println(o.renderer.Table("Transcript Statistics", []string{"Metric", "Value"}, rows, []string{"", ""}, outputWidth(), "6"))

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
			fmt.Println(o.renderer.Table("Per-Agent Stats", []string{"Agent", "Turns", "Tokens", "Cost"}, agentRows, []string{"", "right", "right", "right"}, outputWidth(), "4"))
		}
	}

	if ce, ok := stats["consensus_events"]; ok {
		if events, ok := ce.([]any); ok && len(events) > 0 {
			fmt.Println()
			lines := make([]string, 0, len(events))
			for _, evt := range events {
				if em, ok := evt.(map[string]any); ok {
					lines = append(lines, fmt.Sprintf("[CONSENSUS] Turn %v [%v]: %v", em["turn"], em["agent_id"], em["statement"]))
				}
			}
			fmt.Println(o.renderer.ListSection("Consensus Events:", lines, outputWidth(), "2"))
		}
	}
}
