// Package agent executes LLM agent turns via opencode subprocess.
package agent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/jgabor/agora/internal/types"
)

// Runner is the interface for executing agent turns.
type Runner interface {
	Run(agent types.AgentConfig, envelope map[string]any) (string, map[string]any, error)
}

// AgentRunner executes agent turns via the opencode subprocess.
type AgentRunner struct {
	dryRun bool
}

// NewAgentRunner creates a new AgentRunner.
func NewAgentRunner(dryRun bool) *AgentRunner {
	return &AgentRunner{dryRun: dryRun}
}

// Run executes a single agent turn via opencode subprocess.
// Returns text content, metadata (tokens, cost), and any error.
func (r *AgentRunner) Run(agent types.AgentConfig, envelope map[string]any) (string, map[string]any, error) {
	if r.dryRun {
		return r.dryRunResponse(agent, envelope)
	}

	envJSON, err := json.Marshal(envelope)
	if err != nil {
		return "", nil, fmt.Errorf("marshaling envelope: %w", err)
	}
	payload := agent.SystemPrompt + "\n\n" + string(envJSON)

	if _, err := exec.LookPath("opencode"); err != nil {
		return "", nil, fmt.Errorf("opencode not found in PATH: %w", err)
	}

	cmd := exec.Command(
		"opencode", "run",
		"--model", agent.Model,
		"--format", "json",
		"--dangerously-skip-permissions",
	)
	cmd.Stdin = strings.NewReader(payload)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		if execErr, ok := err.(*exec.Error); ok {
			return "", nil, fmt.Errorf("opencode execution error: %w", execErr)
		}
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderrStr := strings.TrimSpace(stderr.String())
			stdoutStr := strings.TrimSpace(stdout.String())
			detail := stderrStr
			if detail == "" {
				detail = stdoutStr
			}
			return "", nil, fmt.Errorf("opencode run failed (exit %d): %s", exitErr.ExitCode(), detail)
		}
		return "", nil, fmt.Errorf("opencode run error: %w", err)
	}

	textParts, metadata, err := parseOpenCodeOutput(stdout.String())
	if err != nil {
		return "", nil, err
	}

	content := strings.TrimSpace(strings.Join(textParts, ""))
	if content == "" {
		return "", nil, fmt.Errorf("agent produced empty text response")
	}

	return content, metadata, nil
}

func parseOpenCodeOutput(output string) ([]string, map[string]any, error) {
	var textParts []string
	metadata := map[string]any{
		"tokens": map[string]any{},
		"cost":   (*float64)(nil),
	}

	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string)

		switch eventType {
		case "text":
			part, _ := event["part"].(map[string]any)
			if part != nil {
				if text, ok := part["text"].(string); ok {
					textParts = append(textParts, text)
				}
			}
		case "error":
			errMsg := openCodeErrorMessage(event)
			return nil, nil, fmt.Errorf("opencode run error: %s", errMsg)
		case "step_finish":
			part, _ := event["part"].(map[string]any)
			if part != nil {
				if tokens, ok := part["tokens"]; ok {
					metadata["tokens"] = convertTokens(tokens)
				}
				if costVal, ok := part["cost"]; ok && costVal != nil {
					if cost, ok := costVal.(float64); ok {
						c := cost
						metadata["cost"] = &c
					}
				}
			}
		}
	}

	return textParts, metadata, nil
}

func openCodeErrorMessage(event map[string]any) string {
	if errMsg := formatOpenCodeErrorValue(event["error"]); errMsg != "" {
		return errMsg
	}
	return formatOpenCodeErrorValue(event)
}

func formatOpenCodeErrorValue(value any) string {
	switch v := value.(type) {
	case nil:
		return ""
	case string:
		return normalizeOpenCodeErrorMessage(v)
	case map[string]any:
		name, _ := v["name"].(string)
		detail := formatOpenCodeErrorValue(v["data"])
		if detail == "" {
			detail = formatOpenCodeErrorValue(v["message"])
		}
		if detail != "" {
			if name != "" {
				return name + ": " + detail
			}
			return detail
		}
		if name != "" {
			return name
		}
		if b, err := json.Marshal(v); err == nil {
			return string(b)
		}
	}
	return "unknown opencode error"
}

func normalizeOpenCodeErrorMessage(message string) string {
	message = strings.TrimSpace(message)
	if providerMsg := providerValidationMessage(message); providerMsg != "" {
		return providerMsg
	}
	return message
}

func providerValidationMessage(message string) string {
	const valuePrefix = "Value: "
	start := strings.Index(message, valuePrefix)
	if start == -1 {
		return ""
	}

	value := message[start+len(valuePrefix):]
	if end := strings.Index(value, ".\nError message:"); end != -1 {
		value = value[:end]
	}
	value = strings.TrimSpace(value)

	var providerErr struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"request_id"`
	}
	if err := json.Unmarshal([]byte(value), &providerErr); err != nil || providerErr.Message == "" {
		return ""
	}

	parts := make([]string, 0, 3)
	if providerErr.Code != "" {
		parts = append(parts, providerErr.Code)
	}
	parts = append(parts, providerErr.Message)
	if providerErr.RequestID != "" {
		parts = append(parts, "request_id: "+providerErr.RequestID)
	}
	return strings.Join(parts, " | ")
}

func convertTokens(tokens any) map[string]any {
	tokenMap, ok := tokens.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	converted := make(map[string]any, len(tokenMap))
	for k, v := range tokenMap {
		if f, ok := v.(float64); ok {
			converted[k] = int(f)
		} else {
			converted[k] = v
		}
	}
	return converted
}

func (r *AgentRunner) dryRunResponse(agent types.AgentConfig, envelope map[string]any) (string, map[string]any, error) {
	if agent.ID == "research-query-planner" {
		return dryRunResearchQueries(envelope)
	}
	if agent.ID == "web-research-collector" {
		return dryRunWebResearch(envelope)
	}

	topic := "unknown topic"
	if t, ok := envelope["topic"]; ok {
		if s, ok := t.(string); ok {
			topic = s
		}
	}

	total := 100
	input := 50
	output := 50
	cost := 0.001

	return fmt.Sprintf("[DRY RUN] Agent '%s' responds to: %s", agent.ID, topic),
		map[string]any{
			"tokens": map[string]any{
				"total":  total,
				"input":  input,
				"output": output,
			},
			"cost": &cost,
		}, nil
}

func dryRunResearchQueries(envelope map[string]any) (string, map[string]any, error) {
	topic := "dry-run topic"
	if value, ok := envelope["topic"].(string); ok && strings.TrimSpace(value) != "" {
		topic = strings.TrimSpace(value)
	}
	maxQueries := intEnvelopeValue(envelope, "max_queries", 1)
	queries := []string{fmt.Sprintf("%s research evidence", topic)}
	if maxQueries > 1 {
		queries = append(queries, fmt.Sprintf("%s current sources", topic))
	}
	if len(queries) > maxQueries {
		queries = queries[:maxQueries]
	}
	payload, err := json.Marshal(map[string][]string{"queries": queries})
	if err != nil {
		return "", nil, err
	}
	return string(payload), dryRunMetadata(), nil
}

func dryRunWebResearch(envelope map[string]any) (string, map[string]any, error) {
	queries, _ := envelope["queries"].([]string)
	maxSources := intEnvelopeValue(envelope, "max_sources", 1)
	if maxSources < len(queries) {
		queries = queries[:maxSources]
	}
	sources := make([]map[string]string, 0, len(queries))
	for i, query := range queries {
		sources = append(sources, map[string]string{
			"title": fmt.Sprintf("Dry-run research source %d", i+1),
			"url":   fmt.Sprintf("https://example.com/agora-dry-run-research-%d", i+1),
			"query": query,
		})
	}
	payload, err := json.Marshal(map[string]any{
		"summary": "Dry-run web research planned deterministic source references without live web tool calls.",
		"sources": sources,
	})
	if err != nil {
		return "", nil, err
	}
	return string(payload), dryRunMetadata(), nil
}

func intEnvelopeValue(envelope map[string]any, key string, fallback int) int {
	if value, ok := envelope[key].(int); ok && value > 0 {
		return value
	}
	return fallback
}

func dryRunMetadata() map[string]any {
	total := 100
	input := 50
	output := 50
	cost := 0.001
	return map[string]any{
		"tokens": map[string]any{
			"total":  total,
			"input":  input,
			"output": output,
		},
		"cost": &cost,
	}
}

var consensusPattern = regexp.MustCompile(`(?si)\[CONSENSUS\s*:\s*(.*?)\]`)

// ExtractConsensus extracts a [CONSENSUS: <statement>] marker from an agent response.
// Returns the cleaned text, whether consensus was found, and the statement.
func ExtractConsensus(content string) (cleaned string, hasConsensus bool, statement string) {
	loc := consensusPattern.FindStringSubmatchIndex(content)
	if loc == nil {
		return content, false, ""
	}

	consensusStatement := strings.TrimSpace(content[loc[2]:loc[3]])

	cleanedText := consensusPattern.ReplaceAllString(content, "")
	cleanedText = strings.TrimSpace(cleanedText)

	return cleanedText, true, consensusStatement
}
