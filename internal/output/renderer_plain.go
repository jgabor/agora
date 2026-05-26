package output

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// PlainRenderer implements Renderer with plain-text output (no ANSI, no Unicode borders).
type PlainRenderer struct{}

func (r *PlainRenderer) IsRich() bool { return false }

func (r *PlainRenderer) Width() int { return outputWidth() }

func (r *PlainRenderer) Muted(text string) string { return text }

func (r *PlainRenderer) Styled(text, color string) string { return text }

func (r *PlainRenderer) StatusLabel(label, symbol, color string) string {
	return fmt.Sprintf("[%s]", label)
}

func (r *PlainRenderer) MetricBar(percent int) string {
	const barWidth = 10
	filled := (percent*barWidth + 50) / 100
	if filled < 0 {
		filled = 0
	}
	if filled > barWidth {
		filled = barWidth
	}
	return "[" + strings.Repeat("#", filled) + strings.Repeat("-", barWidth-filled) + "]"
}

// Panel renders a plain ASCII-bordered panel.
func (r *PlainRenderer) Panel(title, body string, width int, borderColor string) string {
	width = clampOutputWidth(width)
	innerWidth := width - 4
	if innerWidth < 1 {
		innerWidth = 1
	}

	var sb strings.Builder

	// Top border: "+- title ----+"
	sb.WriteString("+- ")
	runeLen := utf8.RuneCountInString(title)
	if runeLen > innerWidth {
		title = truncatePlain(title, innerWidth)
		runeLen = innerWidth
	}
	sb.WriteString(title)
	if runeLen+3 < width {
		rhs := width - 4 - runeLen
		if rhs < 1 {
			rhs = 1
		}
		sb.WriteString(" ")
		sb.WriteString(strings.Repeat("-", rhs))
		sb.WriteString("-+\n")
	} else {
		sb.WriteString(" -+\n")
	}

	// Body lines
	body = strings.TrimRight(body, "\n")
	for _, line := range strings.Split(body, "\n") {
		pw := utf8.RuneCountInString(line)
		if pw > innerWidth {
			line = truncatePlain(line, innerWidth)
			pw = innerWidth
		}
		sb.WriteString("| ")
		sb.WriteString(line)
		if pw < innerWidth {
			sb.WriteString(strings.Repeat(" ", innerWidth-pw))
		}
		sb.WriteString(" |\n")
	}

	// Bottom border
	sb.WriteString("+-")
	sb.WriteString(strings.Repeat("-", width-3))
	sb.WriteString("-+")

	return sb.String()
}

// SectionBlock renders a labeled section with indented lines.
func (r *PlainRenderer) SectionBlock(label string, lines []string, width int) string {
	var sb strings.Builder
	sb.WriteString(label)
	sb.WriteString("\n")
	if len(lines) == 0 {
		sb.WriteString("  (none)")
		return sb.String()
	}
	for i, line := range lines {
		if i > 0 {
			sb.WriteString("\n")
		}
		for _, wrapped := range plainWrapText(line, width-2) {
			sb.WriteString("  ")
			sb.WriteString(wrapped)
			sb.WriteString("\n")
		}
	}
	return sb.String()
}

// plainWrapText wraps text at maxWidth, respecting word boundaries.
// Named to avoid collision with wrapText in render.go.
func plainWrapText(text string, maxWidth int) []string {
	if maxWidth <= 0 {
		return []string{text}
	}
	var lines []string
	for _, paragraph := range strings.Split(text, "\n") {
		paragraph = strings.TrimRight(paragraph, " \t")
		if paragraph == "" {
			lines = append(lines, "")
			continue
		}
		lines = append(lines, plainWrapParagraph(paragraph, maxWidth)...)
	}
	return lines
}

func plainWrapParagraph(text string, maxWidth int) []string {
	var lines []string
	words := strings.Fields(text)
	if len(words) == 0 {
		return lines
	}
	current := words[0]
	for _, word := range words[1:] {
		candidate := current + " " + word
		if utf8.RuneCountInString(candidate) > maxWidth {
			lines = append(lines, current)
			current = word
		} else {
			current = candidate
		}
	}
	lines = append(lines, current)
	return lines
}

func truncatePlain(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	if maxLen <= 0 {
		return ""
	}
	const ellipsis = "…"
	ellipsisLen := utf8.RuneCountInString(ellipsis)
	if maxLen <= ellipsisLen {
		return ellipsis
	}
	targetLen := maxLen - ellipsisLen
	var trimmed strings.Builder
	count := 0
	for _, r := range s {
		if count >= targetLen {
			break
		}
		trimmed.WriteRune(r)
		count++
	}
	trimmed.WriteString(ellipsis)
	return trimmed.String()
}

// SectionTitle returns the title unchanged in plain mode.
func (r *PlainRenderer) SectionTitle(title, color string) string {
	return title
}

// Table renders an ASCII table with borders.
func (r *PlainRenderer) Table(title string, headers []string, rows [][]string, aligns []string, width int, color string) string {
	width = clampOutputWidth(width)
	if len(headers) == 0 {
		return ""
	}

	// Compute column widths
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = utf8.RuneCountInString(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(colWidths) {
				if cw := utf8.RuneCountInString(cell); cw > colWidths[i] {
					colWidths[i] = cw
				}
			}
		}
	}

	// Ensure table fits within width
	tableWidth := 1 // left border
	for _, cw := range colWidths {
		tableWidth += cw + 3 // space + content + space + border
	}
	if tableWidth > width {
		// Crude shrink: reduce proportionally
		excess := tableWidth - width
		for i := len(colWidths) - 1; i >= 0 && excess > 0; i-- {
			shrink := colWidths[i] - 3
			if shrink > excess {
				shrink = excess
			}
			colWidths[i] -= shrink
			excess -= shrink
			if colWidths[i] < 3 {
				colWidths[i] = 3
			}
		}
		tableWidth = 1
		for _, cw := range colWidths {
			tableWidth += cw + 3
		}
	}

	var sb strings.Builder

	// Optional title
	if title != "" {
		sb.WriteString(r.SectionTitle(title, color))
		sb.WriteString("\n")
	}

	// Separator line
	writeTableSep(&sb, colWidths)

	// Header row
	writeTableRow(&sb, headers, colWidths, aligns)

	// Header separator
	writeTableSep(&sb, colWidths)

	// Data rows
	for _, row := range rows {
		writeTableRow(&sb, row, colWidths, aligns)
	}

	// Bottom separator
	writeTableSep(&sb, colWidths)

	return sb.String()
}

func writeTableSep(sb *strings.Builder, colWidths []int) {
	sb.WriteString("+")
	for _, w := range colWidths {
		sb.WriteString(strings.Repeat("-", w+2))
		sb.WriteString("+")
	}
	sb.WriteString("\n")
}

func writeTableRow(sb *strings.Builder, cells []string, colWidths []int, aligns []string) {
	sb.WriteString("|")
	for i, w := range colWidths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		cellLen := utf8.RuneCountInString(cell)
		if cellLen > w {
			cell = truncatePlain(cell, w)
			cellLen = w
		}
		padding := w - cellLen
		if i < len(aligns) && aligns[i] == "right" {
			sb.WriteString(strings.Repeat(" ", padding+1))
			sb.WriteString(cell)
			sb.WriteString(" ")
		} else {
			sb.WriteString(" ")
			sb.WriteString(cell)
			sb.WriteString(strings.Repeat(" ", padding+1))
		}
		sb.WriteString("|")
	}
	sb.WriteString("\n")
}

// ListSection renders a title and roman-enumerated items.
func (r *PlainRenderer) ListSection(title string, items []string, width int, color string) string {
	width = clampOutputWidth(width)
	contentWidth := width - 4

	var sb strings.Builder
	sb.WriteString(r.SectionTitle(title, color))
	sb.WriteString("\n")

	for i, item := range items {
		if i > 0 {
			sb.WriteString("\n")
		}
		prefix := toRoman(i+1) + ". "
		prefixWidth := utf8.RuneCountInString(prefix)
		wrapWidth := contentWidth - 8
		if wrapWidth < 10 {
			wrapWidth = 10
		}
		wrapped := plainWrapText(item, wrapWidth)
		for j, line := range wrapped {
			if j == 0 {
				sb.WriteString("  ")
				sb.WriteString(prefix)
				sb.WriteString(line)
			} else {
				sb.WriteString("  ")
				sb.WriteString(strings.Repeat(" ", prefixWidth))
				sb.WriteString(line)
			}
			if j < len(wrapped)-1 {
				sb.WriteString("\n")
			}
		}
	}
	return sb.String()
}

func toRoman(n int) string {
	type pair struct {
		val int
		rom string
	}
	table := []pair{
		{100, "c"},
		{90, "xc"},
		{50, "l"},
		{40, "xl"},
		{10, "x"},
		{9, "ix"},
		{5, "v"},
		{4, "iv"},
		{1, "i"},
	}
	var sb strings.Builder
	for _, p := range table {
		for n >= p.val {
			sb.WriteString(p.rom)
			n -= p.val
		}
	}
	return sb.String()
}

// ProseSection renders a title followed by wrapped prose content.
func (r *PlainRenderer) ProseSection(title, content string, width int, color string) string {
	width = clampOutputWidth(width)
	bodyWidth := width - 4
	if bodyWidth < 1 {
		bodyWidth = 1
	}

	var sb strings.Builder
	sb.WriteString(r.SectionTitle(title, color))
	sb.WriteString("\n")

	body := strings.TrimRight(content, "\n")
	// Plain mode: no glamour, just plain text wrapping
	wrapped := plainWrapText(body, bodyWidth)
	for i, line := range wrapped {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(line)
	}
	return sb.String()
}

// VerboseBody renders verbose agent response content with pipe-prefixed lines.
func (r *PlainRenderer) VerboseBody(content string, width int, borderColor string) string {
	width = clampOutputWidth(width)
	bodyWidth := width - 10
	if bodyWidth < 1 {
		bodyWidth = 1
	}

	var sb strings.Builder
	sb.WriteString(r.Muted("AGENT CONTENT"))
	sb.WriteString("\n")

	body := strings.TrimRight(content, "\n")
	// No glamour in plain mode
	for _, line := range strings.Split(body, "\n") {
		if line == "" {
			sb.WriteString("  |\n")
			continue
		}
		for _, wrapped := range plainWrapText(line, bodyWidth) {
			sb.WriteString("  | ")
			sb.WriteString(wrapped)
			sb.WriteString("\n")
		}
	}
	sb.WriteString("\n")
	return sb.String()
}
