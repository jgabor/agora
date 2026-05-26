package cast

import (
	"fmt"
	"strings"

	"github.com/jgabor/agora/internal/types"
)

var castNames = []string{
	"Solon",
	"Aspasia",
	"Pericles",
	"Socrates",
	"Plato",
	"Themistocles",
	"Demosthenes",
	"Phidias",
}

var castColors = []string{
	"6",
	"4",
	"2",
	"1",
	"3",
	"5",
	"12",
	"9",
}

// Cast manages the "theatre" of agent identities.
type Cast struct {
	members []types.CastMember
	byID    map[string]types.CastMember
}

// New creates a cast from agent configurations.
func New(agents []types.AgentConfig) *Cast {
	c := &Cast{
		byID: make(map[string]types.CastMember),
	}
	for i, agent := range agents {
		member := c.memberForAgent(i, agent)
		c.members = append(c.members, member)
		c.byID[agent.ID] = member
	}
	return c
}

// FromMetadata reconstructs a cast from transcript metadata.
func FromMetadata(meta *types.TranscriptMetadata) *Cast {
	c := &Cast{
		byID: make(map[string]types.CastMember),
	}
	if meta == nil {
		return c
	}
	for _, member := range meta.Cast {
		c.members = append(c.members, member)
		c.byID[member.Persona] = member
	}
	return c
}

// Members returns all cast members in order.
func (c *Cast) Members() []types.CastMember {
	return c.members
}

// Profile returns the full display profile (CastMember) for an agent.
func (c *Cast) Profile(agentID string) types.CastMember {
	if member, ok := c.byID[agentID]; ok {
		return member
	}
	// Fallback for unknown agents (e.g. from a resumed transcript not in config)
	return types.CastMember{
		ID:      0,
		Persona: agentID,
		Name:    "Unknown",
		Color:   c.FallbackColor(agentID),
	}
}

// Badge returns the formatted identity badge, e.g., "[A1 strategist]".
func (c *Cast) Badge(agentID string) string {
	member := c.Profile(agentID)
	if member.ID == 0 {
		return fmt.Sprintf("[A? %s]", agentID)
	}
	return fmt.Sprintf("[A%d %s]", member.ID, agentID)
}

// Color returns the ANSI color slot for the agent.
func (c *Cast) Color(agentID string) string {
	return c.Profile(agentID).Color
}

func (c *Cast) memberForAgent(index int, agent types.AgentConfig) types.CastMember {
	name := castNames[index%len(castNames)]
	return types.CastMember{
		ID:            index + 1,
		Name:          name,
		Persona:       agent.ID,
		ProviderModel: agent.Model,
		Color:         castColors[index%len(castColors)],
	}
}

// FallbackColor returns an ANSI color for agents not in the cast.
func (c *Cast) FallbackColor(agentID string) string {
	normalized := strings.ReplaceAll(agentID, "-", "_")
	switch normalized {
	case "moderator", "synthesizer":
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
