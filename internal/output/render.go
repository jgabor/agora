package output

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"
	"github.com/jgabor/agora/internal/cast"
	"github.com/jgabor/agora/internal/types"
)

func labelValue(label, value string) string {
	if value == "" {
		value = "?"
	}
	return fmt.Sprintf("%s %s", label, value)
}

func agentAccent(agentID string) string {
	var c cast.Cast
	return c.FallbackColor(agentID)
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

func sectionWriter(r Renderer, sb *strings.Builder, width int) func(string, []string) {
	return func(label string, lines []string) {
		if sb.Len() > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(sectionBlock(r, label, lines, width))
		sb.WriteString("\n")
	}
}

func sectionBlock(r Renderer, label string, lines []string, width int) string {
	var sb strings.Builder
	if r.IsRich() {
		label = r.Styled("▍ "+label, "5")
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

func drawPanel(content, title string, borderColor string) string {
	r := detectRenderer(OutputNormal)
	const panelWidth = 76
	return r.Panel(title, renderTextBlock(content, panelWidth-4), panelWidth, normalizeColor(borderColor, "4"))
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

// --- table rendering ---

func drawTable(r Renderer, title string, headers []string, rows [][]string, aligns []string) string {
	return r.Table(title, headers, rows, aligns, 0, "6")
}

// --- metric bars ---

func boundedMetricValue(r Renderer, value, bound float64, valueText, boundText string) string {
	if bound <= 0 {
		return valueText
	}
	percent := boundedPercent(value, bound)
	return fmt.Sprintf("%s/%s (%d%%) %s", valueText, boundText, percent, r.MetricBar(percent))
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

func boundedIntMetricValue(r Renderer, value, bound int) string {
	return boundedMetricValue(r, float64(value), float64(bound), fmt.Sprintf("%d", value), fmt.Sprintf("%d", bound))
}

func boundedSecondsMetricValue(r Renderer, value float64, bound int) string {
	return boundedMetricValue(r, value, float64(bound), fmt.Sprintf("%.1fs", value), fmt.Sprintf("%ds", bound))
}

func boundedCostMetricValue(r Renderer, value, bound float64) string {
	return boundedMetricValue(r, value, bound, fmt.Sprintf("$%.6f", value), fmt.Sprintf("$%.2f", bound))
}

// --- public helpers ---

// RenderTable renders a terminal-width-aware table for human-facing commands.
func RenderTable(title string, headers []string, rows [][]string, aligns []string, color string) string {
	r := detectRenderer(OutputNormal)
	return r.Table(title, headers, rows, aligns, outputWidth(), color)
}

// RenderStatus renders a compact status panel for human-facing command results.
func RenderStatus(title string, rows [][]string, color string) string {
	r := detectRenderer(OutputNormal)
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
		if !r.IsRich() {
			lines = append(lines, fmt.Sprintf("%s: %s", label, value))
			continue
		}
		lines = append(lines, r.Styled(label, color)+" "+value)
	}
	return r.Panel(title, renderTextBlock(strings.Join(lines, "\n"), contentWidth), width, color)
}

// RenderConfigSummary renders a validated config as a rich tree when styling is available.
func RenderConfigSummary(cfg *types.DeliberationConfig) string {
	if cfg == nil {
		return ""
	}
	r := detectRenderer(OutputNormal)

	if !r.IsRich() {
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
		return r.Table("Configuration Valid", []string{"Field", "Value"}, rows, []string{"", ""}, outputWidth(), "2")
	}

	agents := tree.Root(fmt.Sprintf("Agents (%d)", len(cfg.Agents))).
		RootStyle(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))).
		Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("6"))).
		IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8")))
	c := cast.New(cfg.Agents)
	for _, agent := range cfg.Agents {
		member := c.Profile(agent.ID)
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
		shape.Child(fmt.Sprintf("agreement target %d", cfg.ConsensusThreshold))
	}
	if cfg.SynthesisModel != nil {
		shape.Child("synthesis " + *cfg.SynthesisModel)
	}

	root := tree.New().
		Enumerator(tree.RoundedEnumerator).
		EnumeratorStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("2"))).
		IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		Child(shape, agents).
		Width(outputWidth() - 4)

	return r.Panel("Configuration Valid", root.String(), outputWidth(), "2")
}
