package output

import (
	"fmt"
)

// SynthesizeHeader prints the synthesis section header.
func (o *OutputManager) SynthesizeHeader() {
	fmt.Println()
	fmt.Println(o.renderer.SectionTitle("Synthesis", "6"))
}

// SynthesisResult displays the synthesis result.
func (o *OutputManager) SynthesisResult(result map[string]any) {
	fmt.Println()
	width := outputWidth()

	if rec, ok := result["recommended_decision"]; ok {
		if s, ok := rec.(string); ok && s != "" {
			fmt.Println(o.renderer.ProseSection("Recommended Decision", s, width, "2"))
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
					items[i] = "[CONSENSUS] " + s
				}
			}
			fmt.Println(o.renderer.ListSection("Points of Agreement", items, width, "2"))
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
}
