package output

import (
	"encoding/json"
	"fmt"

	"github.com/jgabor/agora/internal/types"
)

// SynthesisLabels defines the user-facing labels implied by the canonical
// terminal outcome. Model prose never chooses these labels.
type SynthesisLabels struct {
	Recommendation string
	Agreements     string
	AgreementMark  string
}

// LabelsForTerminalState returns labels that cannot present a no-consensus
// recommendation as a group decision.
func LabelsForTerminalState(state *types.TerminalState) SynthesisLabels {
	if state == nil {
		return SynthesisLabels{
			Recommendation: "Synthesis recommendation",
			Agreements:     "Model-reported points of agreement",
			AgreementMark:  "[UNVERIFIED]",
		}
	}
	switch state.Outcome.Kind {
	case types.OutcomeConsensus:
		return SynthesisLabels{
			Recommendation: "Consensus decision",
			Agreements:     "Points of agreement",
			AgreementMark:  "[CONSENSUS]",
		}
	case types.OutcomeNoConsensus:
		return SynthesisLabels{
			Recommendation: "Quoted independent analysis (non-authoritative; not group consensus)",
			Agreements:     "Model-reported points of agreement (not group consensus)",
			AgreementMark:  "[NOT CONSENSUS]",
		}
	default:
		return SynthesisLabels{
			Recommendation: "Synthesis recommendation",
			Agreements:     "Model-reported points of agreement",
			AgreementMark:  "[UNVERIFIED]",
		}
	}
}

// ModelRecommendationProse quotes a model recommendation when the typed
// terminal outcome is no-consensus, so model prose cannot acquire outcome
// authority from its presentation.
func ModelRecommendationProse(state *types.TerminalState, prose string) string {
	if state != nil && state.Outcome.Kind == types.OutcomeNoConsensus {
		return fmt.Sprintf("%q", prose)
	}
	return prose
}

func terminalStateFromSynthesis(result map[string]any) *types.TerminalState {
	if result == nil {
		return nil
	}
	value, ok := result["terminal_state"]
	if !ok || value == nil {
		return nil
	}
	switch state := value.(type) {
	case *types.TerminalState:
		return state
	case types.TerminalState:
		return &state
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var state types.TerminalState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil
	}
	return &state
}

// SynthesizeHeader prints the synthesis section header.
func (o *OutputManager) SynthesizeHeader() {
	fmt.Println()
	fmt.Println(o.renderer.SectionTitle("Synthesis", "6"))
}

// SynthesisResult displays the synthesis result.
func (o *OutputManager) SynthesisResult(result map[string]any) {
	fmt.Println()
	width := outputWidth()
	labels := LabelsForTerminalState(terminalStateFromSynthesis(result))

	if rec, ok := result["recommended_decision"]; ok {
		if s, ok := rec.(string); ok && s != "" {
			fmt.Println(o.renderer.ProseSection(labels.Recommendation, ModelRecommendationProse(terminalStateFromSynthesis(result), s), width, "2"))
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
	fmt.Println(o.renderer.Table("Synthesis Confidence", []string{"Metric", "Value"}, [][]string{{"Confidence", confidence}}, []string{"", ""}, width, confColor))

	if args, ok := result["key_arguments"]; ok {
		if list, ok := args.([]any); ok && len(list) > 0 {
			fmt.Println()
			items := make([]string, len(list))
			for i, v := range list {
				if s, ok := v.(string); ok {
					items[i] = "* " + s
				}
			}
			fmt.Println(o.renderer.ListSection("Key Arguments", items, width, "6"))
		}
	}

	if agrs, ok := result["points_of_agreement"]; ok {
		if list, ok := agrs.([]any); ok && len(list) > 0 {
			fmt.Println()
			items := make([]string, len(list))
			for i, v := range list {
				if s, ok := v.(string); ok {
					items[i] = labels.AgreementMark + " " + s
				}
			}
			fmt.Println(o.renderer.ListSection(labels.Agreements, items, width, "2"))
		}
	}

	if tens, ok := result["unresolved_tensions"]; ok {
		if list, ok := tens.([]any); ok && len(list) > 0 {
			fmt.Println()
			items := make([]string, len(list))
			for i, v := range list {
				if s, ok := v.(string); ok {
					items[i] = "[WARNING] " + s
				}
			}
			fmt.Println(o.renderer.ListSection("Unresolved Tensions", items, width, "3"))
		}
	}

	if claims := ClaimEvidenceLines(result["claims"]); len(claims) > 0 {
		fmt.Println()
		fmt.Println(o.renderer.ListSection("Claim Evidence", claims, width, "4"))
	}
}
