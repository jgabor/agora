// Package output renders terminal output for deliberation progress.
package output

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	xterm "github.com/charmbracelet/x/term"
	"github.com/jgabor/agora/internal/cast"
	"github.com/jgabor/agora/internal/types"
)

var stdoutIsTerminal = func() bool {
	info, err := os.Stdout.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

var detectedTerminalWidth = func() (int, bool) {
	for _, file := range []*os.File{os.Stdout, os.Stderr, os.Stdin} {
		if file == nil || !xterm.IsTerminal(file.Fd()) {
			continue
		}
		width, _, err := xterm.GetSize(file.Fd())
		if err == nil && width > 0 {
			return width, true
		}
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return 0, false
	}
	defer func() { _ = tty.Close() }()
	width, _, err := xterm.GetSize(tty.Fd())
	if err == nil && width > 0 {
		return width, true
	}
	return 0, false
}

// OutputMode controls how much live turn output is rendered.
type OutputMode int

const (
	OutputQuiet OutputMode = iota
	OutputNormal
	OutputVerbose
)

// StatsDict is a standalone statistics dictionary used by PrintStats.
type StatsDict = map[string]any

// OutputManager manages terminal output for deliberation progress.
type OutputManager struct {
	mode            OutputMode
	renderer        Renderer
	cast            *cast.Cast
	agentIdentities map[string]*types.AgentIdentity
	state           *types.DeliberationState
	totalCost       float64
	consensusStreak int
}

// NewOutputManager creates a new OutputManager.
func NewOutputManager(verbose bool) *OutputManager {
	if verbose {
		return NewOutputManagerWithMode(OutputVerbose)
	}
	return NewOutputManagerWithMode(OutputQuiet)
}

// NewOutputManagerWithMode creates a new OutputManager with explicit output semantics.
func NewOutputManagerWithMode(mode OutputMode) *OutputManager {
	return &OutputManager{
		mode:     mode,
		renderer: detectRenderer(mode),
	}
}

func (o *OutputManager) registerCast(cfg *types.DeliberationConfig) {
	o.cast = cast.New(cfg.Agents)
	o.agentIdentities = make(map[string]*types.AgentIdentity, len(cfg.Agents))
	for _, agent := range cfg.Agents {
		o.agentIdentities[agent.ID] = agent.Identity
	}
}

func (o *OutputManager) registerCastMembers(members []types.CastMember, cfg *types.DeliberationConfig) {
	o.cast = cast.FromMetadata(&types.TranscriptMetadata{Cast: members})
	if cfg == nil {
		return
	}
	o.agentIdentities = make(map[string]*types.AgentIdentity, len(cfg.Agents))
	for _, agent := range cfg.Agents {
		o.agentIdentities[agent.ID] = agent.Identity
	}
}

func castBadge(member types.CastMember) string {
	return fmt.Sprintf("[A%d %s]", member.ID, member.Persona)
}

func (o *OutputManager) agentBadgeFor(id string) string {
	if o != nil && o.cast != nil {
		return o.cast.Badge(id)
	}
	return unknownAgentBadge(id)
}

func (o *OutputManager) agentDisplayFor(id string) string {
	if o != nil && o.cast != nil {
		member := o.cast.Profile(id)
		return castDisplay(o.agentBadgeFor(id), member)
	}
	return agentDisplay(o.agentBadgeFor(id), o.agentIdentityFor(id))
}

func (o *OutputManager) agentColorFor(id string) string {
	if o != nil && o.cast != nil {
		return o.cast.Color(id)
	}
	return agentAccent(id)
}

func (o *OutputManager) agentIdentityFor(id string) *types.AgentIdentity {
	if o != nil && o.agentIdentities != nil {
		return o.agentIdentities[id]
	}
	return nil
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

// Activity starts feedback for a long-running operation and returns a cleanup function.
func (o *OutputManager) Activity(activity string) func() {
	activity = strings.TrimSpace(activity)
	if activity == "" {
		activity = "Working"
	}
	label := fmt.Sprintf("Working: %s", activity)
	if !o.renderer.IsRich() || !stdoutIsTerminal() {
		fmt.Printf("[INFO] %s\n", label)
		return func() {}
	}

	done := make(chan struct{})
	stopped := make(chan struct{})
	var once sync.Once
	go func() {
		defer close(stopped)
		frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
		ticker := time.NewTicker(120 * time.Millisecond)
		defer ticker.Stop()
		idx := 0
		for {
			fmt.Printf("\r%s %s", o.renderer.Styled(frames[idx%len(frames)], "6"), label)
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

// Info prints an informational message.
func (o *OutputManager) Info(message string) {
	fmt.Printf("%s %s\n", o.renderer.StatusLabel("INFO", "i", "4"), message)
}

// Error prints an error message.
func (o *OutputManager) Error(message string) {
	fmt.Printf("%s %s\n", o.renderer.StatusLabel("ERROR", "✗", "1"), message)
}

// Success prints a success message.
func (o *OutputManager) Success(message string) {
	fmt.Printf("%s %s\n", o.renderer.StatusLabel("SUCCESS", "✓", "2"), message)
}

// Delimiter prints a horizontal rule.
func (o *OutputManager) Delimiter() {
	var line string
	if o.renderer.IsRich() {
		line = o.renderer.Muted(strings.Repeat("─", 60))
	} else {
		line = strings.Repeat("-", 60)
	}
	fmt.Println(line)
}
