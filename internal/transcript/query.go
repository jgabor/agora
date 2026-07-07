package transcript

import "github.com/jgabor/agora/internal/types"

// IsInternalAgent reports whether agentID is orchestrator/system, not deliberation cast.
func IsInternalAgent(agentID string) bool {
	return types.IsInternalAgent(agentID)
}

// HistoryForAgent builds the history envelope for the next agent turn
// from the given records using the specified topology and window. For all
// topologies, the agent's own immediately preceding turn is appended at most
// once, deduplicated against the predecessor-history window.
func HistoryForAgent(records []types.TurnRecord, agentID string, window int, topology types.Topology, numAgents int, turn int) []map[string]string {
	history := historyForTopology(records, agentID, window, topology, numAgents, turn)
	return appendSelfHistory(history, records, agentID, turn)
}

func historyForTopology(records []types.TurnRecord, agentID string, window int, topology types.Topology, numAgents int, turn int) []map[string]string {
	switch topology {
	case types.TopologyStar, types.TopologyMesh:
		start := len(records) - window
		if start < 0 {
			start = 0
		}
		var history []map[string]string
		for _, r := range records[start:] {
			history = append(history, map[string]string{
				"agent_id": r.AgentID,
				"content":  r.Content,
			})
		}
		return history

	default:
		var predecessorID string
		if turn == 0 {
			predecessorID = "moderator"
		} else {
			predecessorIdx := (turn - 1) % numAgents
			agentOrder := inferAgentOrder(records, numAgents)
			if predecessorIdx < len(agentOrder) {
				predecessorID = agentOrder[predecessorIdx]
			} else {
				return nil
			}
		}

		var history []map[string]string
		for i := len(records) - 1; i >= 0; i-- {
			if records[i].AgentID == predecessorID {
				history = append(history, map[string]string{
					"agent_id": records[i].AgentID,
					"content":  records[i].Content,
				})
			}
			if len(history) >= window {
				break
			}
		}
		for i, j := 0, len(history)-1; i < j; i, j = i+1, j-1 {
			history[i], history[j] = history[j], history[i]
		}
		return history
	}
}

func appendSelfHistory(history []map[string]string, records []types.TurnRecord, agentID string, turn int) []map[string]string {
	for i := len(records) - 1; i >= 0; i-- {
		r := records[i]
		if r.Turn >= turn || r.Turn < 0 {
			continue
		}
		if r.AgentID != agentID {
			continue
		}
		for _, h := range history {
			if h["agent_id"] == r.AgentID && h["content"] == r.Content {
				return history
			}
		}
		return append(history, map[string]string{
			"agent_id": r.AgentID,
			"content":  r.Content,
		})
	}
	return history
}

func inferAgentOrder(records []types.TurnRecord, numAgents int) []string {
	var seen []string
	for _, r := range records {
		if IsInternalAgent(r.AgentID) {
			continue
		}
		found := false
		for _, s := range seen {
			if s == r.AgentID {
				found = true
				break
			}
		}
		if !found {
			seen = append(seen, r.AgentID)
		}
		if len(seen) >= numAgents {
			break
		}
	}
	return seen
}

// ConsecutiveConsensusCount counts consecutive trailing records with consensus=true.
func ConsecutiveConsensusCount(records []types.TurnRecord) int {
	count := 0
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Consensus {
			count++
		} else {
			break
		}
	}
	return count
}

// AgentTurnCount returns deliberation agent turns, excluding internal system agents.
func AgentTurnCount(records []types.TurnRecord) int {
	return types.AgentTurnCount(records)
}

// ConsecutiveAgentConsensusCount counts trailing consensus=true records from
// deliberation agents only, skipping internal agents such as synthesizer.
func ConsecutiveAgentConsensusCount(records []types.TurnRecord) int {
	count := 0
	for i := len(records) - 1; i >= 0; i-- {
		if IsInternalAgent(records[i].AgentID) {
			continue
		}
		if records[i].Consensus {
			count++
		} else {
			break
		}
	}
	return count
}

// TotalCost returns the sum of all cost values across records.
func TotalCost(records []types.TurnRecord) float64 {
	var total float64
	for _, r := range records {
		if r.Cost != nil {
			total += *r.Cost
		}
	}
	return total
}

// TotalTokens returns the sum of all total token counts across records.
func TotalTokens(records []types.TurnRecord) int {
	total := 0
	for _, r := range records {
		total += types.IntVal(r.Tokens.Total)
	}
	return total
}
