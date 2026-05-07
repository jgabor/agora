// Package output renders terminal output for deliberation progress.
package output

import (
	"fmt"
	"io"
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
	mode            OutputMode
	agentBadges     map[string]string
	agentIdentities map[string]*types.AgentIdentity
	castMembers     map[string]types.CastMember
	state           *types.DeliberationState
	totalCost       float64
	consensusStreak int
}

// OutputMode controls how much live turn output is rendered.
type OutputMode int

const (
	OutputQuiet OutputMode = iota
	OutputNormal
	OutputVerbose
)

// NewOutputManager creates a new OutputManager.
func NewOutputManager(verbose bool) *OutputManager {
	if verbose {
		return NewOutputManagerWithMode(OutputVerbose)
	}
	return NewOutputManagerWithMode(OutputQuiet)
}

// NewOutputManagerWithMode creates a new OutputManager with explicit output semantics.
func NewOutputManagerWithMode(mode OutputMode) *OutputManager {
	return &OutputManager{mode: mode}
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
	writeSection(shapeTitle, shapeLines)

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
	writeSection(limitsTitle, capLines)

	agentLines := make([]string, 0, len(cfg.Agents))
	for i, a := range cfg.Agents {
		agentLines = append(agentLines, agentCastLine(i, a, true))
	}
	agentsTitle := "Agents"
	if !plainOutput() {
		agentsTitle = "Cast"
	}
	writeSection(agentsTitle, agentLines)

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
	writeSection(settingsTitle, settings)

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
			label = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("5")).Render("▍ " + label)
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

func castDisplay(badge string, member types.CastMember) string {
	parts := []string{badge}
	if member.Name != "" {
		parts = append(parts, labelValue("NAME", member.Name))
	}
	if member.Persona != "" {
		parts = append(parts, labelValue("PERSONA", member.Persona))
	}
	return strings.Join(parts, " ")
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

func firstPromptLine(prompt string) string {
	if idx := strings.Index(prompt, "\n"); idx >= 0 {
		prompt = prompt[:idx]
	}
	return strings.TrimSpace(prompt)
}

func (o *OutputManager) registerCast(cfg *types.DeliberationConfig) {
	o.registerCastMembers(types.BuildCast(cfg), cfg)
}

func (o *OutputManager) registerCastMembers(cast []types.CastMember, cfg *types.DeliberationConfig) {
	if len(cast) == 0 {
		return
	}
	o.agentBadges = make(map[string]string, len(cast))
	o.agentIdentities = make(map[string]*types.AgentIdentity, len(cast))
	o.castMembers = make(map[string]types.CastMember, len(cast))
	for _, member := range cast {
		o.agentBadges[member.Persona] = castBadge(member)
		o.castMembers[member.Persona] = member
	}
	if cfg == nil {
		return
	}
	for _, agent := range cfg.Agents {
		o.agentIdentities[agent.ID] = agent.Identity
	}
}

func castBadge(member types.CastMember) string {
	return fmt.Sprintf("[A%d %s]", member.ID, member.Persona)
}

func (o *OutputManager) agentBadgeFor(id string) string {
	if o != nil && o.agentBadges != nil {
		if badge, ok := o.agentBadges[id]; ok {
			return badge
		}
	}
	return unknownAgentBadge(id)
}

func (o *OutputManager) agentDisplayFor(id string) string {
	if o != nil && o.castMembers != nil {
		if member, ok := o.castMembers[id]; ok {
			return castDisplay(o.agentBadgeFor(id), member)
		}
	}
	return agentDisplay(o.agentBadgeFor(id), o.agentIdentityFor(id))
}

func (o *OutputManager) agentColorFor(id string) string {
	if o != nil && o.castMembers != nil {
		if member, ok := o.castMembers[id]; ok && member.Color != "" {
			return member.Color
		}
	}
	return agentAccent(id)
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

// Activity starts feedback for a long-running operation and returns a cleanup function.
func (o *OutputManager) Activity(activity string) func() {
	activity = strings.TrimSpace(activity)
	if activity == "" {
		activity = "Working"
	}
	label := fmt.Sprintf("Working: %s", activity)
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

// RenderTranscript displays a stored transcript with the same turn styling used
// while a deliberation is running.
func RenderTranscript(w io.Writer, records []types.TurnRecord) {
	NewOutputManagerWithMode(OutputNormal).RenderTranscript(w, records)
}

// RenderTranscript displays a stored transcript with this output manager's mode.
func (o *OutputManager) RenderTranscript(w io.Writer, records []types.TurnRecord) {
	if metadata := transcriptMetadata(records); metadata != nil {
		o.registerCastMembers(metadata.Cast, metadata.Config)
	}
	maxTurns := transcriptMaxTurn(records)
	fallbackTurn := 0
	for i, record := range records {
		if i > 0 {
			writeLine(w)
		}

		record.AgentID = transcriptAgentID(record.AgentID)
		if transcriptEventRecord(record) {
			writeLine(w, renderTranscriptEvent(record, i+1))
			continue
		}

		displayTurn := record.Turn
		if displayTurn < 0 {
			displayTurn = fallbackTurn
		}
		o.renderTurnProgress(w, record, displayTurn, maxTurns)
		fallbackTurn++
		if record.Evidence != nil {
			writeLine(w)
			writeLine(w, renderTranscriptEvidence(record.Evidence, o.agentColorFor(record.AgentID)))
		}
	}
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

func transcriptMetadata(records []types.TurnRecord) *types.TranscriptMetadata {
	for _, record := range records {
		if record.Transcript != nil {
			return record.Transcript
		}
	}
	return nil
}

func transcriptMaxTurn(records []types.TurnRecord) int {
	maxTurn := -1
	count := 0
	for _, record := range records {
		if transcriptEventRecord(record) {
			continue
		}
		count++
		if record.Turn > maxTurn {
			maxTurn = record.Turn
		}
	}
	if maxTurn >= 0 {
		return maxTurn + 1
	}
	return count
}

func transcriptAgentID(agentID string) string {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		return "unknown"
	}
	return agentID
}

func transcriptEventRecord(record types.TurnRecord) bool {
	return strings.TrimSpace(record.AgentID) == "orchestrator" && record.Turn < 0
}

func renderTranscriptEvent(record types.TurnRecord, index int) string {
	width := outputWidth()
	contentWidth := width - 4
	var sb strings.Builder
	writeSection := sectionWriter(&sb, contentWidth)

	metadata := []string{
		fmt.Sprintf("RECORD %d", index),
		fmt.Sprintf("TURN %d", record.Turn),
		fmt.Sprintf("AGENT %s", transcriptAgentID(record.AgentID)),
	}
	if record.Model != nil && strings.TrimSpace(*record.Model) != "" {
		metadata = append(metadata, fmt.Sprintf("MODEL %s", strings.TrimSpace(*record.Model)))
	}
	writeSection("Record", metadata)

	if content := strings.TrimSpace(record.Content); content != "" {
		writeSection("Content", strings.Split(content, "\n"))
	}
	if record.Evidence != nil {
		writeTranscriptEvidenceSections(writeSection, record.Evidence)
	}
	if record.Consensus {
		statement := strings.TrimSpace(record.ConsensusStatement)
		if statement == "" {
			statement = "This turn agrees with the emerging decision."
		}
		writeSection("Consensus", []string{statement})
	}

	title := "Transcript Event"
	if record.Evidence != nil {
		title = "Transcript Evidence"
	}
	return theaterPanel(title, sb.String(), width, agentAccent(record.AgentID))
}

func renderTranscriptEvidence(evidence *types.EvidenceBundle, color string) string {
	width := outputWidth()
	contentWidth := width - 4
	var sb strings.Builder
	writeSection := sectionWriter(&sb, contentWidth)
	writeTranscriptEvidenceSections(writeSection, evidence)
	return theaterPanel("Transcript Evidence", sb.String(), width, color)
}

func writeTranscriptEvidenceSections(writeSection func(string, []string), evidence *types.EvidenceBundle) {
	if evidence == nil {
		return
	}
	if summary := strings.TrimSpace(evidence.Summary); summary != "" {
		writeSection("Evidence Summary", strings.Split(summary, "\n"))
	}
	if len(evidence.SourceReferences) == 0 {
		return
	}
	sources := make([]string, 0, len(evidence.SourceReferences))
	for i, source := range evidence.SourceReferences {
		sources = append(sources, transcriptEvidenceSourceLine(i+1, source))
	}
	writeSection("Evidence Sources", sources)
}

func transcriptEvidenceSourceLine(index int, source types.SourceReference) string {
	label := strings.TrimSpace(source.Title)
	if label == "" {
		label = strings.TrimSpace(source.URL)
	}
	if label == "" {
		label = strings.TrimSpace(source.Path)
	}
	if label == "" {
		label = "source"
	}

	var refs []string
	if source.URL != "" {
		refs = append(refs, source.URL)
	}
	if source.Path != "" {
		refs = append(refs, source.Path)
	}
	if source.Query != "" {
		refs = append(refs, "query: "+source.Query)
	}
	line := fmt.Sprintf("%d. %s", index, label)
	if len(refs) > 0 {
		line += " (" + strings.Join(refs, "; ") + ")"
	}
	return line
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

func richMetricLine(label, value, color string) string {
	labelStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(color)).Width(11)
	return labelStyle.Render(label) + " " + value
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
	if plainOutput() {
		raw = stripKnownANSI(raw)
	}

	var sb strings.Builder
	if plainOutput() {
		sb.WriteString(sectionTitle(title, color))
		sb.WriteString("\n")
	}
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
	if !plainOutput() {
		return theaterPanel(title, sb.String(), width, color)
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
