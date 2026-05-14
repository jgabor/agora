package output

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/list"
	"charm.land/lipgloss/v2/table"
	"charm.land/lipgloss/v2/tree"
	"github.com/jgabor/agora/internal/types"
)

func plainOutput() bool {
	term, hasTerm := os.LookupEnv("TERM")
	return os.Getenv("NO_COLOR") != "" || os.Getenv("CI") != "" || !hasTerm || term == "" || term == "dumb"
}

func richOutput() bool {
	return !plainOutput() && stdoutIsTerminal()
}

func mutedStyle() lipgloss.Style {
	if plainOutput() {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Faint(true)
}

func statusStyle(color string) lipgloss.Style {
	if plainOutput() {
		return lipgloss.NewStyle()
	}
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))
}

func labelValue(label, value string) string {
	if value == "" {
		value = "?"
	}
	return fmt.Sprintf("%s %s", label, value)
}

func statusLabel(label, symbol, color string) string {
	plain := fmt.Sprintf("[%s]", label)
	if plainOutput() {
		return plain
	}
	return statusStyle(color).Render(fmt.Sprintf("%s %s", symbol, label))
}

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

// --- rendering primitives ---

func outputWidth() int {
	if width, ok := detectedTerminalWidth(); ok {
		return clampOutputWidth(width)
	}
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
	if width > 150 {
		return 150
	}
	return width
}

func splitWidths(total, gap int, leftRatio float64) (int, int) {
	available := total - gap
	if available < 2 {
		return total, 0
	}
	left := int(float64(available) * leftRatio)
	if left < 20 {
		left = 20
	}
	right := available - left
	if right < 20 {
		right = 20
		left = available - right
	}
	return left, right
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
		sb.WriteString(sectionBlock(label, lines, width))
		sb.WriteString("\n")
	}
}

func sectionBlock(label string, lines []string, width int) string {
	var sb strings.Builder
	if !plainOutput() {
		label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")).Render("▍ " + label)
	}
	sb.WriteString(label)
	sb.WriteString("\n")
	if len(lines) == 0 {
		sb.WriteString("  (none)")
		return sb.String()
	}
	for lineIndex, line := range lines {
		if lineIndex > 0 {
			sb.WriteString("\n")
		}
		for wrappedIndex, wrapped := range wrapText(line, width-2) {
			if wrappedIndex > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString("  ")
			sb.WriteString(wrapped)
		}
	}
	return sb.String()
}

func sectionTitle(title, color string) string {
	if plainOutput() {
		return title
	}
	style := lipgloss.NewStyle().Bold(true)
	style = style.Foreground(lipgloss.Color(color))
	return style.Render(title)
}

func drawPanel(content, title string, borderColor string) string {
	const panelWidth = 76
	return theaterPanel(title, renderTextBlock(content, panelWidth-4), panelWidth, normalizeColor(borderColor, "4"))
}

func wrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}
	block := renderTextBlock(text, maxWidth)
	if block == "" {
		return []string{""}
	}
	return strings.Split(block, "\n")
}

func visualLen(s string) int {
	return lipgloss.Width(s)
}

func renderTextBlock(text string, width int) string {
	if width <= 0 {
		return strings.TrimRight(text, "\n")
	}
	style := lipgloss.NewStyle().MaxWidth(width)
	var out []string
	for _, paragraph := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		paragraph = strings.TrimRight(paragraph, " \t")
		if strings.TrimSpace(paragraph) == "" {
			out = append(out, "")
			continue
		}
		wrapped := lipgloss.Wrap(paragraph, width, " ")
		for _, line := range strings.Split(strings.TrimRight(style.Render(wrapped), "\n"), "\n") {
			out = append(out, strings.TrimRight(line, " "))
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

func normalizeColor(color string, fallback string) string {
	if color == "" || strings.HasPrefix(color, "\x1b") {
		return fallback
	}
	return color
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

func richMetricLine(label, value, color string) string {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Width(11)
	return labelStyle.Render(label) + " " + value
}

// --- table rendering ---

func drawTable(title string, headers []string, rows [][]string, aligns []string) string {
	return drawTableAtWidth(title, headers, rows, aligns, 0, "6")
}

func drawTableAtWidth(title string, headers []string, rows [][]string, aligns []string, width int, color string) string {
	if len(headers) == 0 {
		return ""
	}
	normalizedRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		normalized := make([]string, len(headers))
		copy(normalized, row)
		normalizedRows = append(normalizedRows, normalized)
	}

	t := table.New().
		Headers(headers...).
		Rows(normalizedRows...).
		Wrap(true)
	if width > 0 {
		t.Width(width)
	}
	cellStyle := func(row, col int) lipgloss.Style {
		style := lipgloss.NewStyle().Padding(0, 1)
		if col < len(aligns) && aligns[col] == "right" {
			style = style.AlignHorizontal(lipgloss.Right)
		}
		if plainOutput() {
			return style
		}
		if row == table.HeaderRow {
			return style.Bold(true).Foreground(lipgloss.Color("6"))
		}
		if row%2 == 1 {
			return style.Faint(true)
		}
		return style
	}
	if plainOutput() {
		t.Border(lipgloss.ASCIIBorder()).StyleFunc(cellStyle)
	} else {
		borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		t.Border(lipgloss.RoundedBorder()).
			BorderStyle(borderStyle).
			StyleFunc(cellStyle)
	}

	rendered := strings.TrimRight(t.Render(), "\n")
	if title == "" {
		return rendered
	}
	return sectionTitle(title, color) + "\n" + rendered
}

func drawStructuredTable(title string, headers []string, rows [][]string, aligns []string, width int, color string) string {
	width = clampOutputWidth(width)
	contentWidth := width - 4
	raw := drawTableAtWidth("", headers, rows, aligns, contentWidth, color)
	if plainOutput() {
		return sectionTitle(title, color) + "\n" + raw
	}
	return sectionTitle(title, color) + "\n" + raw
}

// --- metric bars ---

func boundedMetricValue(value, bound float64, valueText, boundText string) string {
	if bound <= 0 {
		return valueText
	}
	percent := boundedPercent(value, bound)
	return fmt.Sprintf("%s/%s (%d%%) %s", valueText, boundText, percent, metricBar(percent))
}

func boundedPercent(value, bound float64) int {
	if bound <= 0 || value <= 0 {
		return 0
	}
	percent := int((value/bound)*100 + 0.5)
	if percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}
	return percent
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
	if plainOutput() {
		return "[" + strings.Repeat("#", filled) + strings.Repeat("-", width-filled) + "]"
	}

	filledStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(progressColor(percent)))
	emptyStyle := lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("8"))
	return filledStyle.Render(strings.Repeat("●", filled)) + emptyStyle.Render(strings.Repeat("○", width-filled))
}

func progressColor(percent int) string {
	switch {
	case percent >= 80:
		return "2"
	case percent >= 50:
		return "3"
	default:
		return "6"
	}
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

// --- list sections ---

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
	items := make([]any, 0, len(lines))
	for _, line := range lines {
		items = append(items, renderTextBlock(line, contentWidth-8))
	}
	rendered := list.New().
		Enumerator(list.Roman).
		Items(items...)
	if !plainOutput() {
		rendered = rendered.
			EnumeratorStyle(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))).
			ItemStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("7"))).
			IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8")))
		return theaterPanel(title, rendered.String(), width, color)
	}
	return sectionTitle(title, color) + "\n" + rendered.String()
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

	body = renderTextBlock(body, bodyWidth)
	if !plainOutput() {
		return theaterPanel(title, body, width, color)
	}
	return sectionTitle(title, color) + "\n" + body
}

// --- public helpers ---

// RenderTable renders a terminal-width-aware table for human-facing commands.
func RenderTable(title string, headers []string, rows [][]string, aligns []string, color string) string {
	return drawStructuredTable(title, headers, rows, aligns, outputWidth(), color)
}

// RenderStatus renders a compact status panel for human-facing command results.
func RenderStatus(title string, rows [][]string, color string) string {
	width := outputWidth()
	contentWidth := clampOutputWidth(width) - 4
	lines := make([]string, 0, len(rows))
	for _, row := range rows {
		if len(row) == 0 {
			continue
		}
		label := row[0]
		value := ""
		if len(row) > 1 {
			value = row[1]
		}
		if plainOutput() {
			lines = append(lines, fmt.Sprintf("%s: %s", label, value))
			continue
		}
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(label)+" "+value)
	}
	return theaterPanel(title, renderTextBlock(strings.Join(lines, "\n"), contentWidth), width, color)
}

// RenderConfigSummary renders a validated config as a rich tree when styling is available.
func RenderConfigSummary(cfg *types.DeliberationConfig) string {
	if cfg == nil {
		return ""
	}
	if plainOutput() {
		rows := [][]string{
			{"Topology", string(cfg.Topology)},
			{"Agents", fmt.Sprintf("%d", len(cfg.Agents))},
		}
		if cfg.ConsensusThreshold > 0 {
			rows = append(rows, []string{"Consensus threshold", fmt.Sprintf("%d", cfg.ConsensusThreshold)})
		}
		if cfg.SynthesisModel != nil {
			rows = append(rows, []string{"Synthesis model", *cfg.SynthesisModel})
		}
		return drawStructuredTable("Configuration Valid", []string{"Field", "Value"}, rows, []string{"", ""}, outputWidth(), "2")
	}

	agents := tree.Root(fmt.Sprintf("Agents (%d)", len(cfg.Agents))).
		RootStyle(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))).
		Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("6"))).
		IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8")))
	for i, agent := range cfg.Agents {
		member := types.CastMemberForAgent(i, agent)
		label := fmt.Sprintf("%s %s", strings.Trim(castBadge(member), "[]"), agent.ID)
		if agent.Model != "" {
			label += " · " + agent.Model
		}
		agents.Child(label)
	}

	shape := tree.Root("Run Shape").
		RootStyle(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))).
		Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("5"))).
		IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		Child("topology " + string(cfg.Topology))
	if cfg.ConsensusThreshold > 0 {
		shape.Child(fmt.Sprintf("agreement target %d agents", cfg.ConsensusThreshold))
	}
	if cfg.SynthesisModel != nil {
		shape.Child("synthesis " + *cfg.SynthesisModel)
	}

	root := tree.Root("valid configuration").
		RootStyle(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("2"))).
		Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("2"))).
		IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		Child(shape, agents).
		Width(outputWidth() - 4)

	return theaterPanel("Configuration Valid", root.String(), outputWidth(), "2")
}
