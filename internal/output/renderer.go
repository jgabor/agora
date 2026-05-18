package output

// Renderer is the interface that wraps all rendering primitives.
// Implementations provide either rich (lipgloss/glamour) or plain text output.
type Renderer interface {
	// IsRich reports whether this renderer produces rich (ANSI/Unicode) output.
	IsRich() bool

	// Width returns the output width in columns.
	Width() int

	// Panel renders a bordered panel with a title and body.
	Panel(title, body string, width int, borderColor string) string

	// SectionBlock renders a labeled section with a list of lines.
	SectionBlock(label string, lines []string, width int) string

	// SectionTitle renders a styled section title.
	SectionTitle(title, color string) string

	// Table renders a titled table with headers, rows, and column alignments.
	Table(title string, headers []string, rows [][]string, aligns []string, width int, color string) string

	// ListSection renders a titled list with enumerated items.
	ListSection(title string, items []string, width int, color string) string

	// ProseSection renders a titled prose block with Markdown support.
	ProseSection(title, content string, width int, color string) string

	// VerboseBody renders verbose agent response content.
	VerboseBody(content string, width int, borderColor string) string

	// MetricBar renders a progress bar.
	MetricBar(percent int) string

	// StatusLabel renders a semantic status label (e.g., [INFO]).
	StatusLabel(label, symbol, color string) string

	// Styled renders text with the given color/style.
	Styled(text, color string) string

	// Muted renders text in a muted style.
	Muted(text string) string
}
