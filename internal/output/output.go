// Package output renders terminal output for deliberation progress.
package output

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"github.com/jgabor/agora/internal/types"
)

var stdoutIsTerminal = func() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

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
	ansiGray     = "\033[90m"
	ansiBRed     = "\033[91m"
	ansiBGreen   = "\033[92m"
	ansiBYellow  = "\033[93m"
	ansiBBlue    = "\033[94m"
	ansiBMagenta = "\033[95m"
	ansiBCyan    = "\033[96m"
	ansiBWhite   = "\033[97m"
)

func agentAccent(agentID string) string {
	normalized := strings.ReplaceAll(agentID, "-", "_")
	switch normalized {
	case "orchestrator", "synthesizer":
		return "6"
	case "strategist":
		return "4"
	case "domain_expert":
		return "2"
	case "skeptic", "risk_officer":
		return "1"
	case "optimist":
		return "3"
	case "user_advocate":
		return "5"
	case "implementer":
		return "8"
	default:
		return "7"
	}
}

// StatsDict is a standalone statistics dictionary used by PrintStats.
type StatsDict = map[string]any

// OutputManager manages terminal output for deliberation progress.
type OutputManager struct {
	verbose         bool
	agentBadges     map[string]string
	agentIdentities map[string]*types.AgentIdentity
	state           *types.DeliberationState
	totalCost       float64
	consensusStreak int
}

// NewOutputManager creates a new OutputManager.
func NewOutputManager(verbose bool) *OutputManager {
	return &OutputManager{verbose: verbose}
}

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
	var sb strings.Builder
	writeSection := sectionWriter(&sb, contentWidth)

	writeSection("Cast Preview", []string{
		fmt.Sprintf("Topology: %s", string(cfg.Topology)),
		fmt.Sprintf("Consensus threshold: %d", cfg.ConsensusThreshold),
	})

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
	writeSection("Run Bounds", capLines)

	agentLines := make([]string, 0, len(cfg.Agents))
	for i, a := range cfg.Agents {
		agentLines = append(agentLines, agentCastLine(i, a, true))
	}
	writeSection("Agents", agentLines)

	return theaterPanel("Generated Config", sb.String(), width, "6")
}

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
	var sb strings.Builder
	writeSection := sectionWriter(&sb, contentWidth)

	writeSection("Topic", []string{state.Topic})

	cast := make([]string, 0, len(state.Config.Agents))
	for i, a := range state.Config.Agents {
		cast = append(cast, agentCastLine(i, a, true))
	}
	writeSection("Cast", cast)

	settings := []string{
		fmt.Sprintf("Topology: %s", string(state.Config.Topology)),
		fmt.Sprintf("Time limit: %ds", state.TimeLimit),
		fmt.Sprintf("Max turns: %d", state.MaxTurns),
		fmt.Sprintf("Window: %d", state.Window),
	}
	if state.Budget != nil {
		settings = append(settings, fmt.Sprintf("Budget: $%.2f", *state.Budget))
	}
	if state.Config.ConsensusThreshold > 0 {
		settings = append(settings, fmt.Sprintf("Consensus threshold: %d", state.Config.ConsensusThreshold))
	}
	writeSection("Run Settings", settings)

	return theaterPanel("Deliberation Start", sb.String(), width, "4")
}

func outputWidth() int {
	if raw := os.Getenv("COLUMNS"); raw != "" {
		if width, err := strconv.Atoi(raw); err == nil {
			return clampOutputWidth(width)
		}
	}
	return 76
}

func clampOutputWidth(width int) int {
	if width < 40 {
		return 40
	}
	if width > 120 {
		return 120
	}
	return width
}

func theaterPanel(title, body string, width int, borderColor string) string {
	border := lipgloss.RoundedBorder()
	if plainOutput() {
		border = lipgloss.ASCIIBorder()
	}
	style := lipgloss.NewStyle().
		Width(width-2).
		Border(border).
		Padding(0, 1)
	if !plainOutput() {
		style = style.BorderForeground(lipgloss.Color(borderColor))
		title = lipgloss.NewStyle().Bold(true).Render(title)
	}

	return style.Render(title + "\n" + strings.TrimRight(body, "\n"))
}

func sectionWriter(sb *strings.Builder, width int) func(string, []string) {
	return func(label string, lines []string) {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		if !plainOutput() {
			label = lipgloss.NewStyle().Bold(true).Render(label)
		}
		sb.WriteString(label)
		sb.WriteString("\n")
		if len(lines) == 0 {
			sb.WriteString("  (none)\n")
			return
		}
		for _, line := range lines {
			for _, wrapped := range wrapText(line, width-2) {
				sb.WriteString("  ")
				sb.WriteString(wrapped)
				sb.WriteString("\n")
			}
		}
	}
}

func agentBadge(index int, id string) string {
	return fmt.Sprintf("[A%d %s]", index+1, id)
}

func unknownAgentBadge(id string) string {
	return fmt.Sprintf("[A? %s]", id)
}

func agentDisplay(badge string, identity *types.AgentIdentity) string {
	parts := []string{badge}
	if identity == nil {
		return badge
	}
	if identity.DisplayName != "" {
		parts = append(parts, labelValue("NAME", identity.DisplayName))
	}
	if identity.Role != "" {
		parts = append(parts, labelValue("ROLE", identity.Role))
	}
	if identity.Affiliation != "" {
		parts = append(parts, labelValue("AFFILIATION", identity.Affiliation))
	}
	return strings.Join(parts, " ")
}

func agentCastLine(index int, agent types.AgentConfig, includeContext bool) string {
	line := fmt.Sprintf("AGENT %s", agentDisplay(agentBadge(index, agent.ID), agent.Identity))
	if agent.Model != "" {
		line += fmt.Sprintf(" MODEL %s", agent.Model)
	}
	if includeContext {
		context := firstPromptLine(agent.SystemPrompt)
		if context != "" {
			line += fmt.Sprintf(" CONTEXT %s", context)
		}
	}
	return line
}

func firstPromptLine(prompt string) string {
	if idx := strings.Index(prompt, "\n"); idx >= 0 {
		prompt = prompt[:idx]
	}
	return strings.TrimSpace(prompt)
}

func (o *OutputManager) registerCast(cfg *types.DeliberationConfig) {
	if cfg == nil || len(cfg.Agents) == 0 {
		return
	}
	o.agentBadges = make(map[string]string, len(cfg.Agents))
	o.agentIdentities = make(map[string]*types.AgentIdentity, len(cfg.Agents))
	for i, agent := range cfg.Agents {
		o.agentBadges[agent.ID] = agentBadge(i, agent.ID)
		o.agentIdentities[agent.ID] = agent.Identity
	}
}

func (o *OutputManager) agentBadgeFor(id string) string {
	if o != nil && o.agentBadges != nil {
		if badge, ok := o.agentBadges[id]; ok {
			return badge
		}
	}
	return unknownAgentBadge(id)
}

func (o *OutputManager) agentIdentityFor(id string) *types.AgentIdentity {
	if o != nil && o.agentIdentities != nil {
		return o.agentIdentities[id]
	}
	return nil
}

func plainOutput() bool {
	term, hasTerm := os.LookupEnv("TERM")
	return os.Getenv("NO_COLOR") != "" || os.Getenv("CI") != "" || !hasTerm || term == "" || term == "dumb"
}

func richOutput() bool {
	return !plainOutput() && stdoutIsTerminal()
}

// Activity starts feedback for a long-running phase and returns a cleanup function.
func (o *OutputManager) Activity(phase string) func() {
	phase = strings.TrimSpace(phase)
	if phase == "" {
		phase = "Working"
	}
	label := fmt.Sprintf("PHASE %s", phase)
	if !richOutput() {
		fmt.Printf("[INFO] %s\n", label)
		return func() {}
	}

	done := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(stopped)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		style := statusStyle("6")
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		idx := 0
		for {
			fmt.Printf("\r%s %s", style.Render(frames[idx%len(frames)]), label)
			idx++
			select {
			case <-done:
				fmt.Print("\r\033[2K")
				return
			case <-ticker.C:
			}
		}
	}()

	return func() {
		once.Do(func() {
			close(done)
			<-stopped
		})
	}
}

func statusStyle(color string) lipgloss.Style {
	if plainOutput() {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))
}

func mutedStyle() lipgloss.Style {
	if plainOutput() {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Faint(true)
}

func labelValue(label, value string) string {
	if value == "" {
		value = "?"
	}
	return fmt.Sprintf("%s %s", label, value)
}

func boundedMetricValue(value, bound float64, valueText, boundText string) string {
	if bound <= 0 {
		return valueText
	}
	percent := 0
	if value > 0 {
		percent = int((value/bound)*100 + 0.5)
	}
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	return fmt.Sprintf("%s/%s (%d%%) %s", valueText, boundText, percent, metricBar(percent))
}

func metricBar(percent int) string {
	const width = 10
	filled := (percent*width + 50) / 100
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
}

func boundedIntMetricValue(value, bound int) string {
	return boundedMetricValue(float64(value), float64(bound), fmt.Sprintf("%d", value), fmt.Sprintf("%d", bound))
}

func boundedSecondsMetricValue(value float64, bound int) string {
	return boundedMetricValue(value, float64(bound), fmt.Sprintf("%.1fs", value), fmt.Sprintf("%ds", bound))
}

func boundedCostMetricValue(value, bound float64) string {
	return boundedMetricValue(value, bound, fmt.Sprintf("$%.6f", value), fmt.Sprintf("$%.2f", bound))
}

// TurnProgress prints progress for a single turn.
func (o *OutputManager) TurnProgress(record types.TurnRecord, turn int, maxTurns int) {
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

	agentDisplay := labelValue("AGENT", agentDisplay(o.agentBadgeFor(record.AgentID), o.agentIdentityFor(record.AgentID)))
	if !plainOutput() {
		agentDisplay = lipgloss.NewStyle().Foreground(lipgloss.Color(agentAccent(record.AgentID))).Bold(true).Render(agentDisplay)
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
	fmt.Printf("TURN %s | %s\n", turnValue, metadata)

	if record.Consensus {
		label := "[CONSENSUS]"
		if !plainOutput() {
			label = statusStyle("2").Render("✓ CONSENSUS")
		}
		fmt.Printf("  %s %s\n", label, record.ConsensusStatement)
	}

	if o.verbose && record.Content != "" {
		fmt.Println()
		fmt.Print(renderVerboseBody(record.Content, outputWidth()))
	}
}

func renderVerboseBody(content string, width int) string {
	width = clampOutputWidth(width)
	bodyWidth := width - 4
	var sb strings.Builder
	sb.WriteString(mutedStyle().Render("AGENT CONTENT"))
	sb.WriteString("\n")

	body := strings.TrimRight(content, "\n")
	if !plainOutput() && markdownLike(body) {
		if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(bodyWidth)); err == nil {
			if rendered, err := r.Render(body); err == nil {
				body = strings.TrimRight(rendered, "\n")
			}
		}
	}

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

func markdownLike(content string) bool {
	markers := []string{"# ", "## ", "- ", "* ", "```", "> ", "**", "["}
	for _, marker := range markers {
		if strings.Contains(content, marker) {
			return true
		}
	}
	return false
}

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

// PrintStats displays standalone statistics without requiring a live deliberation state.
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

// SynthesizeHeader prints the synthesis section header.
func (o *OutputManager) SynthesizeHeader() {
	fmt.Println()
	fmt.Println(sectionTitle("Synthesis", "6"))
}

// SynthesisResult displays the synthesis result.
func (o *OutputManager) SynthesisResult(result map[string]any) {
	fmt.Println()
	width := outputWidth()

	if rec, ok := result["recommended_decision"]; ok {
		if s, ok := rec.(string); ok && s != "" {
			fmt.Println(renderProseSection("Recommended Decision", s, width, "2"))
		}
	}

	confidence := "?"
	if c, ok := result["confidence"]; ok {
		if s, ok := c.(string); ok {
			confidence = s
		}
	}
	confColor := "7"
	switch confidence {
	case "high":
		confColor = "2"
	case "medium":
		confColor = "3"
	case "low":
		confColor = "1"
	}
	fmt.Println(drawStructuredTable("Synthesis Confidence", []string{"Metric", "Value"}, [][]string{{"Confidence", confidence}}, []string{"", ""}, width, confColor))

	if args, ok := result["key_arguments"]; ok {
		if list, ok := args.([]any); ok && len(list) > 0 {
			fmt.Println()
			fmt.Println(renderListSection("Key Arguments", list, width, "6", "*"))
		}
	}

	if agrs, ok := result["points_of_agreement"]; ok {
		if list, ok := agrs.([]any); ok && len(list) > 0 {
			fmt.Println()
			fmt.Println(renderListSection("Points of Agreement", list, width, "2", "[CONSENSUS]"))
		}
	}

	if tens, ok := result["unresolved_tensions"]; ok {
		if list, ok := tens.([]any); ok && len(list) > 0 {
			fmt.Println()
			fmt.Println(renderListSection("Unresolved Tensions", list, width, "3", "[WARNING]"))
		}
	}
}

// Info prints an informational message.
func (o *OutputManager) Info(message string) {
	fmt.Printf("%s %s\n", statusLabel("INFO", "i", "4"), message)
}

// Error prints an error message.
func (o *OutputManager) Error(message string) {
	fmt.Printf("%s %s\n", statusLabel("ERROR", "✗", "1"), message)
}

// Success prints a success message.
func (o *OutputManager) Success(message string) {
	fmt.Printf("%s %s\n", statusLabel("SUCCESS", "✓", "2"), message)
}

// Delimiter prints a horizontal rule.
func (o *OutputManager) Delimiter() {
	line := strings.Repeat("-", 60)
	if !plainOutput() {
		line = mutedStyle().Render(strings.Repeat("─", 60))
	}
	fmt.Println(line)
}

func statusLabel(label, symbol, color string) string {
	plain := fmt.Sprintf("[%s]", label)
	if plainOutput() {
		return plain
	}
	return statusStyle(color).Render(fmt.Sprintf("%s %s", symbol, label))
}

func drawPanel(content, title string, borderColor string) string {
	const panelWidth = 76
	var sb strings.Builder
	plain := plainOutput()
	borderTopLeft, borderTopRight := "╭", "╮"
	borderBottomLeft, borderBottomRight := "╰", "╯"
	horizontal, vertical := "─", "│"
	if plain {
		borderTopLeft, borderTopRight = "+", "+"
		borderBottomLeft, borderBottomRight = "+", "+"
		horizontal, vertical = "-", "|"
		borderColor = ""
	}

	contentLines := wrapText(content, panelWidth-4)

	sb.WriteString(borderColor)
	sb.WriteString(borderTopLeft)
	titleStr := " " + title + " "
	if len(titleStr) < panelWidth-2 {
		remaining := panelWidth - 2 - len(titleStr)
		left := remaining / 2
		right := remaining - left
		sb.WriteString(strings.Repeat(horizontal, left))
		if !plain {
			sb.WriteString(ansiReset)
			sb.WriteString(ansiBold)
		}
		sb.WriteString(titleStr)
		if !plain {
			sb.WriteString(ansiReset)
		}
		sb.WriteString(borderColor)
		sb.WriteString(strings.Repeat(horizontal, right))
	} else {
		sb.WriteString(strings.Repeat(horizontal, panelWidth-2))
	}
	sb.WriteString(borderTopRight)
	if !plain {
		sb.WriteString(ansiReset)
	}
	sb.WriteString("\n")

	for _, line := range contentLines {
		sb.WriteString(borderColor)
		sb.WriteString(vertical)
		if !plain {
			sb.WriteString(ansiReset)
		}
		sb.WriteString(" ")
		sb.WriteString(line)
		padLen := panelWidth - 4 - visualLen(line)
		if padLen > 0 {
			sb.WriteString(strings.Repeat(" ", padLen))
		}
		sb.WriteString(" ")
		sb.WriteString(borderColor)
		sb.WriteString(vertical)
		if !plain {
			sb.WriteString(ansiReset)
		}
		sb.WriteString("\n")
	}

	sb.WriteString(borderColor)
	sb.WriteString(borderBottomLeft)
	sb.WriteString(strings.Repeat(horizontal, panelWidth-2))
	sb.WriteString(borderBottomRight)
	if !plain {
		sb.WriteString(ansiReset)
	}

	return sb.String()
}

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

func visualLen(s string) int {
	n := 0
	inEscape := false
	for i := 0; i < len(s); {
		if inEscape {
			if s[i] == 'm' {
				inEscape = false
			}
			i++
			continue
		}
		if s[i] == '\033' && i+1 < len(s) && s[i+1] == '[' {
			inEscape = true
			i += 2
			continue
		}
		_, size := utf8.DecodeRuneInString(s[i:])
		n++
		i += size
	}
	return n
}

func finalAgentRows(perAgent map[string]types.AgentTurnStats, cfg *types.DeliberationConfig) [][]string {
	rows := make([][]string, 0, len(perAgent))
	seen := make(map[string]bool, len(perAgent))
	if cfg != nil {
		for i, agent := range cfg.Agents {
			if s, ok := perAgent[agent.ID]; ok {
				rows = append(rows, agentStatsRow(agentDisplay(agentBadge(i, agent.ID), agent.Identity), s))
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

func agentStatsRow(label string, s types.AgentTurnStats) []string {
	return []string{label, fmt.Sprintf("%d", s.Turns), fmt.Sprintf("%d", s.Tokens), fmt.Sprintf("$%.6f", s.Cost)}
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sectionTitle(title, color string) string {
	if plainOutput() {
		return title
	}
	style := lipgloss.NewStyle().Bold(true)
	style = style.Foreground(lipgloss.Color(color))
	return style.Render(title)
}

func drawStructuredTable(title string, headers []string, rows [][]string, aligns []string, width int, color string) string {
	width = clampOutputWidth(width)
	contentWidth := width - 4
	raw := drawTable("", headers, rows, aligns)
	raw = stripKnownANSI(raw)

	var sb strings.Builder
	sb.WriteString(sectionTitle(title, color))
	sb.WriteString("\n")
	for _, line := range strings.Split(strings.TrimRight(raw, "\n"), "\n") {
		if plainOutput() {
			line = asciiTableLine(line)
		}
		if line == "" {
			sb.WriteString("\n")
			continue
		}
		if visualLen(line) <= contentWidth {
			sb.WriteString(line)
			sb.WriteString("\n")
		} else {
			for _, wrapped := range wrapText(line, contentWidth) {
				sb.WriteString(wrapped)
				sb.WriteString("\n")
			}
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func asciiTableLine(s string) string {
	s = strings.ReplaceAll(s, "─", "-")
	s = strings.ReplaceAll(s, "┼", "+")
	s = strings.ReplaceAll(s, "│", "|")
	return s
}

func stripKnownANSI(s string) string {
	for _, code := range []string{ansiReset, ansiBold, ansiDim, ansiBlack, ansiRed, ansiGreen, ansiYellow, ansiBlue, ansiMagenta, ansiCyan, ansiWhite, ansiGray, ansiBRed, ansiBGreen, ansiBYellow, ansiBBlue, ansiBMagenta, ansiBCyan, ansiBWhite} {
		s = strings.ReplaceAll(s, code, "")
	}
	return s
}

func renderConsensusEvents(events []any, width int) string {
	width = clampOutputWidth(width)
	var lines []string
	for _, evt := range events {
		if em, ok := evt.(map[string]any); ok {
			lines = append(lines, fmt.Sprintf("[CONSENSUS] Turn %v [%v]: %v", em["turn"], em["agent_id"], em["statement"]))
		}
	}
	return renderListLines("Consensus Events:", lines, width, "2")
}

func renderListSection(title string, list []any, width int, color string, marker string) string {
	lines := make([]string, 0, len(list))
	for _, item := range list {
		if s, ok := item.(string); ok {
			lines = append(lines, marker+" "+s)
		}
	}
	return renderListLines(title, lines, width, color)
}

func renderListLines(title string, lines []string, width int, color string) string {
	width = clampOutputWidth(width)
	contentWidth := width - 4
	var sb strings.Builder
	sb.WriteString(sectionTitle(title, color))
	sb.WriteString("\n")
	for _, line := range lines {
		for _, wrapped := range wrapText(line, contentWidth-2) {
			sb.WriteString("  ")
			sb.WriteString(wrapped)
			sb.WriteString("\n")
		}
	}
	return strings.TrimRight(sb.String(), "\n")
}

func renderProseSection(title, content string, width int, color string) string {
	width = clampOutputWidth(width)
	bodyWidth := width - 4
	body := strings.TrimRight(content, "\n")
	if !plainOutput() && markdownLike(body) {
		if r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(bodyWidth)); err == nil {
			if rendered, err := r.Render(body); err == nil {
				body = strings.TrimRight(rendered, "\n")
			}
		}
	}

	var sb strings.Builder
	sb.WriteString(sectionTitle(title, color))
	sb.WriteString("\n")
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
	return strings.TrimRight(sb.String(), "\n")
}

func drawTable(title string, headers []string, rows [][]string, aligns []string) string {
	if len(headers) == 0 {
		return ""
	}

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

	for i := range colWidths {
		if colWidths[i] < 4 {
			colWidths[i] = 4
		}
	}

	var sb strings.Builder

	if title != "" {
		sb.WriteString(ansiBold)
		sb.WriteString(title)
		sb.WriteString(ansiReset)
		sb.WriteString("\n")
	}

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

	sb.WriteString(ansiCyan)
	for i, h := range headers {
		if i > 0 {
			sb.WriteString(" │ ")
		}
		sb.WriteString(padCell(h, colWidths[i], "left"))
	}
	sb.WriteString(ansiReset)
	sb.WriteString("\n")

	sb.WriteString(ansiDim)
	sb.WriteString(sep)
	sb.WriteString(ansiReset)
	sb.WriteString("\n")

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
