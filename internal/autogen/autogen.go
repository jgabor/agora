// Package autogen generates deliberation configurations via an LLM meta-call.
package autogen

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/types"
)

// codeFenceRe matches markdown code fences wrapping YAML content.
var codeFenceRe = regexp.MustCompile("(?s)```(?:ya?ml)?\\s*\n(.*?)```")

// GenerateConfig creates a DeliberationConfig by asking an LLM to design a
// panel of agents for the given topic, constrained by the level's caps.
func GenerateConfig(topic string, level types.AutoLevel, model string, runner agent.Runner) (*types.DeliberationConfig, error) {
	caps := types.CapsForLevel(level)
	systemPrompt := buildSystemPrompt(topic, level, model, caps)

	designer := types.AgentConfig{
		ID:           "config_designer",
		Model:        model,
		SystemPrompt: systemPrompt,
	}

	envelope := map[string]any{
		"topic": topic,
		"level": string(level),
	}

	resp, _, err := runner.Run(designer, envelope)
	if err != nil {
		return nil, fmt.Errorf("auto config generation failed: LLM call error: %w", err)
	}

	yamlBody := stripCodeFences(resp)

	cfg, err := config.LoadConfigFromBytes([]byte(yamlBody))
	if err != nil {
		return nil, fmt.Errorf("auto config generation failed: %w", err)
	}

	if err := validateCaps(cfg, caps); err != nil {
		return nil, fmt.Errorf("auto config generation failed: %w", err)
	}

	return cfg, nil
}

// buildSystemPrompt constructs the system prompt for the config designer agent.
func buildSystemPrompt(topic string, level types.AutoLevel, model string, caps types.LevelCaps) string {
	var b strings.Builder

	b.WriteString("You are a deliberation configuration designer. Given a topic, design a panel of agents to deliberate on it.\n\n")
	b.WriteString("Return ONLY valid YAML with this structure:\n")
	b.WriteString("topology: <ring|star|mesh>\n")
	b.WriteString("consensus_threshold: <number>\n")
	b.WriteString("agents:\n")
	b.WriteString("  - id: <lowercase_with_underscores>\n")
	b.WriteString("    model: <model from context>\n")
	b.WriteString("    system_prompt: |\n")
	b.WriteString("      <2-4 sentence role description>\n\n")
	b.WriteString("Constraints:\n")

	if caps.MaxAgents > 0 {
		fmt.Fprintf(&b, "- Maximum %d agents\n", caps.MaxAgents)
	}

	b.WriteString("- Agent IDs must be unique, lowercase with underscores\n")
	b.WriteString("- System prompts should be 2-4 sentences each, describing a distinct perspective or role\n")
	b.WriteString("- Choose a topology that creates meaningful adversarial tension\n")
	b.WriteString("- Set consensus_threshold to 0 unless the topic demands convergence\n\n")
	fmt.Fprintf(&b, "The model to use for all agents: %s\n", model)
	fmt.Fprintf(&b, "Topic: %s\n", topic)

	return b.String()
}

// stripCodeFences removes markdown code fences from the LLM response,
// returning the inner content if fences are present, or the original text
// otherwise.
func stripCodeFences(s string) string {
	locs := codeFenceRe.FindStringSubmatch(s)
	if len(locs) >= 2 {
		return strings.TrimSpace(locs[1])
	}
	return strings.TrimSpace(s)
}

// validateCaps checks that the generated config respects level caps.
func validateCaps(cfg *types.DeliberationConfig, caps types.LevelCaps) error {
	if caps.MaxAgents > 0 && len(cfg.Agents) > caps.MaxAgents {
		return fmt.Errorf("generated %d agents exceeds level cap of %d", len(cfg.Agents), caps.MaxAgents)
	}
	return nil
}
