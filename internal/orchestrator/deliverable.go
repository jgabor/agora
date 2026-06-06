package orchestrator

import (
	"regexp"
	"strings"

	"github.com/jgabor/agora/internal/types"
)

var (
	deliverableLawLine = regexp.MustCompile(`(?im)^\s*\d+\.\s+An agent must\b`)
	wordThree          = regexp.MustCompile(`(?i)\bthree\b`)
	wordDigitThree     = regexp.MustCompile(`\b3\b`)
)

// ParseDeliverableGate returns a gate when the topic requires a numbered final artifact.
func ParseDeliverableGate(topic string) *types.DeliverableGate {
	lower := strings.ToLower(topic)
	requires := strings.Contains(lower, "output must") ||
		strings.Contains(lower, "final output") ||
		strings.Contains(lower, "exactly three") ||
		strings.Contains(lower, "exactly 3") ||
		strings.Contains(lower, "no more than three") ||
		strings.Contains(lower, "no more than 3")
	if !requires {
		return nil
	}

	minItems := 3
	if wordThree.MatchString(topic) || strings.Contains(lower, "three laws") {
		minItems = 3
	} else if wordDigitThree.MatchString(topic) {
		minItems = 3
	}

	return &types.DeliverableGate{MinItems: minItems}
}

// DeliverablePresent reports whether any agent turn contains the required artifact.
func DeliverablePresent(records []types.TurnRecord, gate *types.DeliverableGate) bool {
	if gate == nil || gate.MinItems <= 0 {
		return true
	}
	for _, r := range records {
		if types.IsInternalAgent(r.AgentID) {
			continue
		}
		if deliverableItemCount(r.Content) >= gate.MinItems {
			return true
		}
	}
	return false
}

func deliverableItemCount(content string) int {
	return len(deliverableLawLine.FindAllString(content, -1))
}
