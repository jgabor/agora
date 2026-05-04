package kumbaja

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// AgentRunner executes agent turns via the opencode subprocess.
type AgentRunner struct {
	dryRun bool
}

// NewAgentRunner creates a new AgentRunner.
// When dryRun is true, Run returns a canned response without invoking opencode.
func NewAgentRunner(dryRun bool) *AgentRunner {
	return &AgentRunner{dryRun: dryRun}
}

// Run executes a single agent turn. It returns the text content produced by the
// agent, a metadata map containing "tokens" (map[string]any with int values) and
// "cost" (*float64), and any error encountered.
//
// In normal mode, Run invokes opencode as a subprocess with the agent's system
// prompt and envelope JSON piped to stdin. It parses the JSON event stream line
// by line, accumulating text events and extracting token/cost metadata from
// step_finish events.
//
// In dry-run mode, Run returns a placeholder response without invoking opencode.
func (r *AgentRunner) Run(agent AgentConfig, envelope map[string]any) (string, map[string]any, error) {
	if r.dryRun {
		return r.dryRunResponse(agent, envelope)
	}

	// Build the payload: system_prompt + two newlines + JSON envelope.
	// This matches the Python behavior exactly.
	envJSON, err := json.Marshal(envelope)
	if err != nil {
		return "", nil, fmt.Errorf("marshaling envelope: %w", err)
	}
	payload := agent.SystemPrompt + "\n\n" + string(envJSON)

	// Verify opencode is available with a clear diagnostic.
	if _, err := exec.LookPath("opencode"); err != nil {
		return "", nil, fmt.Errorf("opencode not found in PATH: %w", err)
	}

	cmd := exec.Command(
		"opencode",
		"run",
		"--model", agent.Model,
		"--format", "json",
		"--dangerously-skip-permissions",
	)
	cmd.Stdin = strings.NewReader(payload)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Distinguish between "binary not found at start time" (exec.Error)
		// and non-zero exit (exec.ExitError).
		if execErr, ok := err.(*exec.Error); ok {
			// The binary was found by LookPath but disappeared before execution,
			// or some other system-level error occurred.
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

	// Parse the JSON event stream from stdout.
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

// parseOpenCodeOutput parses the opencode JSON event stream line by line.
// It accumulates text from "text" events, extracts tokens and cost from
// "step_finish" events, and raises an error on "error" events.
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
			continue // skip non-JSON lines, matching Python behavior
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
			errMsg, _ := event["error"].(string)
			if errMsg == "" {
				errMsg = fmt.Sprintf("%v", event)
			}
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

// convertTokens converts JSON number values (float64 after unmarshaling) to int
// for the tokens map, matching Python's integer token representation.
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

// dryRunResponse returns a canned placeholder response without invoking opencode.
func (r *AgentRunner) dryRunResponse(agent AgentConfig, envelope map[string]any) (string, map[string]any, error) {
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

// consensusPattern matches [CONSENSUS: <statement>] case-insensitively.
// The (?s) flag makes '.' match newlines (Go equivalent of Python re.DOTALL).
// The (?i) flag makes matching case-insensitive (equivalent to re.IGNORECASE).
var consensusPattern = regexp.MustCompile(`(?si)\[CONSENSUS\s*:\s*(.*?)\]`)

// ExtractConsensus extracts a consensus marker from an agent response.
//
// Consensus is signaled with the syntax: [CONSENSUS: <statement>]
// The marker is case-insensitive and the statement may span multiple lines.
//
// Returns:
//   - cleaned: the response text with the consensus marker removed
//   - hasConsensus: true if a consensus marker was found
//   - statement: the extracted consensus statement (empty if no marker found)
func ExtractConsensus(content string) (cleaned string, hasConsensus bool, statement string) {
	loc := consensusPattern.FindStringSubmatchIndex(content)
	if loc == nil {
		return content, false, ""
	}

	// loc[2:4] are the start/end indices of the first capture group.
	consensusStatement := strings.TrimSpace(content[loc[2]:loc[3]])

	// Remove all consensus markers from the content.
	cleanedText := consensusPattern.ReplaceAllString(content, "")
	cleanedText = strings.TrimSpace(cleanedText)

	return cleanedText, true, consensusStatement
}
