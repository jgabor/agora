package output

import (
	"fmt"
	"strings"

	"charm.land/glamour/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/list"
	"charm.land/lipgloss/v2/table"
	"charm.land/lipgloss/v2/tree"
	"github.com/jgabor/agora/internal/cast"
	"github.com/jgabor/agora/internal/types"
)

// RichRenderer implements Renderer with rich (lipgloss/glamour) output.
type RichRenderer struct{}

func (r *RichRenderer) IsRich() bool { return true }

func (r *RichRenderer) Width() int { return outputWidth() }

func (r *RichRenderer) Styled(text, color string) string {
	return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Render(text)
}

func (r *RichRenderer) Muted(text string) string {
	return lipgloss.NewStyle().Faint(true).Render(text)
}

func (r *RichRenderer) StatusLabel(label, symbol, color string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(color)).
		Render(fmt.Sprintf("%s %s", symbol, label))
}

func (r *RichRenderer) MetricBar(percent int) string {
	const barWidth = 10
	filled := (percent*barWidth + 50) / 100
	if filled < 0 {
		filled = 0
	}
	if filled > barWidth {
		filled = barWidth
	}
	filledStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(progressColor(percent)))
	emptyStyle := lipgloss.NewStyle().
		Faint(true).
		Foreground(lipgloss.Color("8"))
	return filledStyle.Render(strings.Repeat("●", filled)) +
		emptyStyle.Render(strings.Repeat("○", barWidth-filled))
}

func (r *RichRenderer) Panel(title, body string, width int, borderColor string) string {
	width = clampOutputWidth(width)
	border := lipgloss.RoundedBorder()
	style := lipgloss.NewStyle().
		Width(width-2).
		Border(border).
		Padding(0, 1).
		BorderForeground(lipgloss.Color(borderColor))
	title = lipgloss.NewStyle().Bold(true).Render(title)
	return style.Render(title + "\n" + strings.TrimRight(body, "\n"))
}

func (r *RichRenderer) SectionBlock(label string, lines []string, width int) string {
	var sb strings.Builder
	label = lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("5")).
		Render("▍ " + label)
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

func (r *RichRenderer) SectionTitle(title, color string) string {
	return lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color(color)).
		Render(title)
}

func (r *RichRenderer) Table(title string, headers []string, rows [][]string, aligns []string, width int, color string) string {
	if len(headers) == 0 {
		return ""
	}

	width = clampOutputWidth(width)
	// Leave 4-char margin for terminal edge
	tableWidth := width
	if tableWidth > 4 {
		tableWidth = width - 4
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
	if tableWidth > 0 {
		t.Width(tableWidth)
	}

	cellStyle := func(row, col int) lipgloss.Style {
		style := lipgloss.NewStyle().Padding(0, 1)
		if col < len(aligns) && aligns[col] == "right" {
			style = style.AlignHorizontal(lipgloss.Right)
		}
		if row == table.HeaderRow {
			return style.Bold(true).Foreground(lipgloss.Color("6"))
		}
		if row%2 == 1 {
			return style.Faint(true)
		}
		return style
	}

	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	t.Border(lipgloss.RoundedBorder()).
		BorderStyle(borderStyle).
		StyleFunc(cellStyle)

	rendered := strings.TrimRight(t.Render(), "\n")
	if title == "" {
		return rendered
	}
	return r.SectionTitle(title, color) + "\n" + rendered
}

func (r *RichRenderer) ListSection(title string, items []string, width int, color string) string {
	width = clampOutputWidth(width)
	contentWidth := width - 4
	listItems := make([]any, 0, len(items))
	for _, item := range items {
		listItems = append(listItems, renderTextBlock(item, contentWidth-8))
	}
	rendered := list.New().
		Enumerator(list.Roman).
		Items(listItems...).
		EnumeratorStyle(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color))).
		ItemStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("7"))).
		IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8")))
	return r.Panel(title, rendered.String(), width, color)
}

func (r *RichRenderer) ProseSection(title, content string, width int, color string) string {
	width = clampOutputWidth(width)
	bodyWidth := width - 4
	body := strings.TrimRight(content, "\n")
	if markdownLike(body) {
		if rend, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(bodyWidth),
		); err == nil {
			if rendered, err := rend.Render(body); err == nil {
				body = strings.TrimRight(rendered, "\n")
			}
		}
	}
	body = renderTextBlock(body, bodyWidth)
	return r.Panel(title, body, width, color)
}

func (r *RichRenderer) VerboseBody(content string, width int, borderColor string) string {
	width = clampOutputWidth(width)
	bodyWidth := width - 4
	var sb strings.Builder

	body := strings.TrimRight(content, "\n")
	if markdownLike(body) {
		if rend, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle("dark"),
			glamour.WithWordWrap(bodyWidth),
		); err == nil {
			if rendered, err := rend.Render(body); err == nil {
				body = strings.TrimRight(rendered, "\n")
			}
		}
	}

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
	return r.Panel("Agent Response", sb.String(), width, borderColor) + "\n"
}

// richMetricLine renders a bold-colored label followed by a value; used in turn cards.
func richMetricLine(label, value, color string) string {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Width(11)
	return labelStyle.Render(label) + " " + value
}

// progressColor maps a percentage to a terminal color code.
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

// agentCastTree renders a nested tree of agents with metadata; used in config panels.
func agentCastTree(r Renderer, agents []types.AgentConfig, c *cast.Cast, includeContext bool, width int) string {
	root := tree.Root("ensemble").
		Enumerator(tree.RoundedEnumerator).
		RootStyle(lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5"))).
		EnumeratorStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8"))).
		Width(width)

	for _, agent := range agents {
		member := c.Profile(agent.ID)
		accent := member.Color

		heading := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(accent)).Render(strings.Trim(c.Badge(agent.ID), "[]"))
		if member.Name != "" {
			heading += " " + lipgloss.NewStyle().Bold(true).Render(member.Name)
		}
		if member.Persona != "" {
			heading += " " + r.Muted(member.Persona)
		}

		agentNode := tree.Root(heading).
			RootStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(accent))).
			Enumerator(tree.RoundedEnumerator).
			EnumeratorStyle(lipgloss.NewStyle().Foreground(lipgloss.Color(accent))).
			IndenterStyle(lipgloss.NewStyle().Foreground(lipgloss.Color("8")))

		if member.ProviderModel != "" {
			agentNode.Child(r.Muted("model " + member.ProviderModel))
		}
		if member.Color != "" {
			agentNode.Child(r.Muted("color " + member.Color))
		}
		if includeContext {
			if context := firstPromptLine(agent.SystemPrompt); context != "" {
				agentNode.Child(r.Muted("context ") + renderInlineText(context, width-8))
			}
		}

		root.Child(agentNode)
	}
	return root.String()
}

// richAutoConfigPanelAtWidth renders the auto-config panel with side-by-side layout.
func richAutoConfigPanelAtWidth(r Renderer, width, contentWidth int,
	shapeTitle, limitsTitle, agentsTitle string,
	shapeLines, capLines, agentLines []string,
) string {
	leftWidth, rightWidth := splitWidths(contentWidth, 2, 0.5)
	top := lipgloss.JoinHorizontal(
		lipgloss.Top,
		lipgloss.NewStyle().Width(leftWidth).Render(sectionBlock(r, shapeTitle, shapeLines, leftWidth)),
		lipgloss.NewStyle().Width(2).Render(""),
		lipgloss.NewStyle().Width(rightWidth).Render(sectionBlock(r, limitsTitle, capLines, rightWidth)),
	)
	body := lipgloss.JoinVertical(lipgloss.Left, top, "", sectionBlock(r, agentsTitle, agentLines, contentWidth))
	return r.Panel("Generated Config", body, width, "6")
}

// richDeliberationHeaderAtWidth renders the deliberation header with side-by-side layout.
func richDeliberationHeaderAtWidth(r Renderer, width, contentWidth int,
	topicLines, castLines []string, settings []string,
	settingsTitle string, agents []types.AgentConfig, c *cast.Cast,
) string {
	castWidth, settingsWidth := splitWidths(contentWidth, 2, 0.62)
	castLines = append(castLines, agentCastTree(r, agents, c, true, castWidth))
	castBlock := lipgloss.NewStyle().Width(castWidth).Render(sectionBlock(r, "Cast", castLines, castWidth))
	settingsBlock := lipgloss.NewStyle().Width(settingsWidth).Render(sectionBlock(r, settingsTitle, settings, settingsWidth))
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		sectionBlock(r, "Topic", topicLines, contentWidth),
		"",
		lipgloss.JoinHorizontal(lipgloss.Top, castBlock, lipgloss.NewStyle().Width(2).Render(""), settingsBlock),
	)
	return r.Panel("Deliberation Start", body, width, "4")
}
