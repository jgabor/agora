package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/types"
)

// ---------------------------------------------------------------------------
// ExtractConsensus — positive cases (semantic parity with Python)
// ---------------------------------------------------------------------------

func TestExtractConsensusPresent(t *testing.T) {
	content := "System A is superior [CONSENSUS: System A wins]"
	cleaned, hasConsensus, statement, _ := ExtractConsensus(content)

	if !hasConsensus {
		t.Error("expected hasConsensus=true")
	}
	if statement != "System A wins" {
		t.Errorf("statement: got %q, want %q", statement, "System A wins")
	}
	if strings.Contains(cleaned, "CONSENSUS") {
		t.Errorf("cleaned should not contain CONSENSUS: %q", cleaned)
	}
	if !strings.Contains(cleaned, "System A is superior") {
		t.Errorf("cleaned should keep original text: %q", cleaned)
	}
}

func TestExtractConsensusMissing(t *testing.T) {
	content := "No consensus here."
	_, hasConsensus, _, _ := ExtractConsensus(content)

	if hasConsensus {
		t.Error("expected hasConsensus=false")
	}
}

// ---------------------------------------------------------------------------
// ExtractConsensus — semantic parity: case-insensitive
// ---------------------------------------------------------------------------

func TestExtractConsensusCaseInsensitive(t *testing.T) {
	tests := []struct {
		label   string
		content string
		want    string
	}{
		{"lowercase", "[consensus: we agree]", "we agree"},
		{"mixed case", "[CoNsEnSuS: mixed case]", "mixed case"},
		{"uppercase bracketed", "[CONSENSUS: UPPER]", "UPPER"},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, hasConsensus, statement, _ := ExtractConsensus(tt.content)
			if !hasConsensus {
				t.Error("expected hasConsensus=true")
			}
			if statement != tt.want {
				t.Errorf("statement: got %q, want %q", statement, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractConsensus — semantic parity: multiline
// ---------------------------------------------------------------------------

func TestExtractConsensusMultiline(t *testing.T) {
	tests := []struct {
		label   string
		content string
		want    string
	}{
		{
			"newline in content",
			"Line 1\n[CONSENSUS: option B is correct]\nLine 3",
			"option B is correct",
		},
		{
			"newline in statement",
			"[CONSENSUS:\nmultiline\nstatement]",
			"multiline\nstatement",
		},
		{
			"newline in statement with leading line",
			"prefix [CONSENSUS:\nline one\nline two] suffix",
			"line one\nline two",
		},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			cleaned, hasConsensus, statement, _ := ExtractConsensus(tt.content)
			if !hasConsensus {
				t.Error("expected hasConsensus=true")
			}
			if statement != tt.want {
				t.Errorf("statement: got %q, want %q", statement, tt.want)
			}
			if strings.Contains(cleaned, "CONSENSUS") {
				t.Errorf("cleaned should not contain marker: %q", cleaned)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractConsensus — semantic parity: whitespace variants
// ---------------------------------------------------------------------------

func TestExtractConsensusWhitespaceVariants(t *testing.T) {
	tests := []struct {
		label   string
		content string
		want    string
	}{
		{"tight", "[CONSENSUS:tight]", "tight"},
		{"spaces around colon", "[CONSENSUS   :   spaced  ]", "spaced"},
		{"extra trailing spaces", "[consensus:  we   agree  ]", "we   agree"},
		{"tab before colon", "[CONSENSUS\t:\ttabbed]", "tabbed"},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, hasConsensus, statement, _ := ExtractConsensus(tt.content)
			if !hasConsensus {
				t.Errorf("expected hasConsensus=true for %q", tt.content)
			}
			if statement != tt.want {
				t.Errorf("statement: got %q, want %q", statement, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractConsensus — negative cases (no false positives)
// ---------------------------------------------------------------------------

func TestExtractConsensusFalsePositives(t *testing.T) {
	tests := []struct {
		label   string
		content string
	}{
		{"no brackets", "CONSENSUS: we agree"},
		{"only opening bracket", "[CONSENSUS: incomplete"},
		{"only closing bracket", "CONSENSUS: incomplete]"},
		{"partial match", "something [CONSENSUS like this"},
		{"empty", ""},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, hasConsensus, _, _ := ExtractConsensus(tt.content)
			if hasConsensus {
				t.Errorf("expected hasConsensus=false for %q", tt.content)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// ExtractConsensus — multiple markers
// ---------------------------------------------------------------------------

func TestExtractConsensusMultipleMarkers(t *testing.T) {
	content := "[CONSENSUS: first] text [CONSENSUS: second]"
	cleaned, hasConsensus, _, _ := ExtractConsensus(content)
	if !hasConsensus {
		t.Error("expected hasConsensus=true")
	}
	// All markers should be stripped.
	if strings.Contains(cleaned, "CONSENSUS") {
		t.Errorf("cleaned should not contain any CONSENSUS marker: %q", cleaned)
	}
}

// ---------------------------------------------------------------------------
// ExtractConsensus — semantic parity: regex edge cases (Python re.DOTALL/I)
// ---------------------------------------------------------------------------

func TestExtractConsensusRegexEdgeCases(t *testing.T) {
	// These cases must match Python's re.DOTALL (s flag) + re.IGNORECASE (i flag).
	tests := []struct {
		label   string
		content string
		has     bool
		want    string
	}{
		{
			"multiline blank line after colon (trimmed)",
			"[CONSENSUS:\n\nblank]",
			true,
			"blank",
		},
		{
			"CRLF newlines",
			"[consensus:\r\nwindows\r\nstyle]",
			true,
			"windows\r\nstyle",
		},
		{
			"colon with no space then newline",
			"[CONSENSUS:\nno space before newline]",
			true,
			"no space before newline",
		},
		{
			"embedded [brackets] in statement (non-greedy stops at first ])",
			"[CONSENSUS: use [option A] please]",
			true,
			"use [option A",
		},
	}

	for _, tt := range tests {
		t.Run(tt.label, func(t *testing.T) {
			_, hasConsensus, statement, _ := ExtractConsensus(tt.content)
			if hasConsensus != tt.has {
				t.Errorf("hasConsensus: got %v, want %v", hasConsensus, tt.has)
			}
			if hasConsensus && statement != tt.want {
				t.Errorf("statement: got %q, want %q", statement, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Dry-run mode
// ---------------------------------------------------------------------------

func TestAgentRunnerDryRun(t *testing.T) {
	runner := NewAgentRunner(true)
	agent := types.AgentConfig{
		ID:           "test_agent",
		Model:        "test-model",
		SystemPrompt: "You are a test.",
	}

	envelope := map[string]any{
		"topic":   "test topic",
		"history": []map[string]string{},
	}

	content, metadata, err := runner.Run(agent, envelope)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}

	if !strings.Contains(content, "[DRY RUN]") {
		t.Errorf("dry run response missing marker: %q", content)
	}
	if !strings.Contains(content, "test_agent") {
		t.Errorf("dry run missing agent id: %q", content)
	}
	if !strings.Contains(content, "test topic") {
		t.Errorf("dry run missing topic: %q", content)
	}

	// Check tokens.
	if metadata.Tokens.Total == nil || *metadata.Tokens.Total != 100 {
		t.Errorf("total tokens: got %v, want 100", metadata.Tokens.Total)
	}
	if metadata.Tokens.Input == nil || *metadata.Tokens.Input != 50 {
		t.Errorf("input tokens: got %v, want 50", metadata.Tokens.Input)
	}
	if metadata.Tokens.Output == nil || *metadata.Tokens.Output != 50 {
		t.Errorf("output tokens: got %v, want 50", metadata.Tokens.Output)
	}

	// Check cost.
	if metadata.Cost == nil {
		t.Error("cost missing from metadata")
	}
}

func TestAgentRunnerDryRunNoTopic(t *testing.T) {
	runner := NewAgentRunner(true)
	agent := types.AgentConfig{
		ID:           "test_agent",
		Model:        "test-model",
		SystemPrompt: "You are a test.",
	}

	envelope := map[string]any{}

	content, _, err := runner.Run(agent, envelope)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}

	// Should use "unknown topic" fallback.
	if !strings.Contains(content, "unknown topic") {
		t.Errorf("dry run without topic: %q", content)
	}
}

func TestAgentRunnerDryRunResearchAgentsReturnStructuredJSON(t *testing.T) {
	runner := NewAgentRunner(true)

	queryContent, _, err := runner.Run(types.AgentConfig{ID: "research-query-planner", Model: "model"}, map[string]any{
		"topic":       "What would the best programming language be to implement this tool?",
		"max_queries": 2,
	})
	if err != nil {
		t.Fatalf("research query dry run: %v", err)
	}
	var queryPayload struct {
		Queries []string `json:"queries"`
	}
	if err := json.Unmarshal([]byte(queryContent), &queryPayload); err != nil {
		t.Fatalf("query JSON: %v; content=%q", err, queryContent)
	}
	if len(queryPayload.Queries) != 2 {
		t.Fatalf("queries: got %#v, want 2 deterministic queries", queryPayload.Queries)
	}

	webContent, _, err := runner.Run(types.AgentConfig{ID: "web-research-collector", Model: "model"}, map[string]any{
		"queries":     queryPayload.Queries,
		"max_sources": 1,
	})
	if err != nil {
		t.Fatalf("web research dry run: %v", err)
	}
	var webPayload struct {
		Summary string `json:"summary"`
		Sources []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
			Query string `json:"query"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(webContent), &webPayload); err != nil {
		t.Fatalf("web JSON: %v; content=%q", err, webContent)
	}
	if len(webPayload.Sources) != 1 || webPayload.Sources[0].URL == "" || webPayload.Sources[0].Query != queryPayload.Queries[0] {
		t.Fatalf("sources: got %#v, want one deterministic source for first query", webPayload.Sources)
	}
}

func TestWithReadOnlySystemPromptAddsGuardOnce(t *testing.T) {
	prompt := WithReadOnlySystemPrompt("You are a test agent.")
	if !strings.HasPrefix(prompt, ReadOnlyHint) {
		t.Fatalf("prompt = %q, want read-only hint prefix", prompt)
	}
	guarded := WithReadOnlySystemPrompt(prompt)
	if strings.Count(guarded, ReadOnlyHint) != 1 {
		t.Fatalf("guarded prompt contains read-only hint %d times, want 1", strings.Count(guarded, ReadOnlyHint))
	}
}

func TestPayloadForAgentIncludesReadOnlyGuard(t *testing.T) {
	payload, err := payloadForAgent(types.AgentConfig{ID: "test", Model: "m", SystemPrompt: "Role prompt."}, map[string]any{"topic": "test"})
	if err != nil {
		t.Fatalf("payloadForAgent: %v", err)
	}
	if !strings.HasPrefix(payload, ReadOnlyHint) {
		t.Fatalf("payload = %q, want read-only hint prefix", payload)
	}
	if !strings.Contains(payload, `"topic":"test"`) {
		t.Fatalf("payload = %q, want marshaled envelope", payload)
	}
}

func TestOpencodeRunArgsDoNotAutoApprovePermissions(t *testing.T) {
	args := strings.Join(opencodeRunArgs("model"), " ")
	if strings.Contains(args, "dangerously-skip-permissions") {
		t.Fatalf("opencode args %q must not auto-approve permissions", args)
	}
}

func TestApplyReadOnlyPromptGuardUpdatesConfiguredCast(t *testing.T) {
	cfg := &types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "a", SystemPrompt: "Role A"}, {ID: "b"}}}
	ApplyReadOnlyPromptGuard(cfg)
	for _, ag := range cfg.Agents {
		if !strings.HasPrefix(ag.SystemPrompt, ReadOnlyHint) {
			t.Fatalf("agent %s prompt = %q, want read-only hint", ag.ID, ag.SystemPrompt)
		}
	}
}

// ---------------------------------------------------------------------------
// Agent runner — error propagation: missing binary
// ---------------------------------------------------------------------------

func TestAgentRunnerMissingBinary(t *testing.T) {
	runner := NewAgentRunner(false)
	agent := types.AgentConfig{
		ID:           "test",
		Model:        "m",
		SystemPrompt: "prompt",
	}
	envelope := map[string]any{"topic": "test"}

	_, _, err := runner.Run(agent, envelope)
	if err == nil {
		t.Fatal("expected error for missing opencode binary")
	}
	if !strings.Contains(err.Error(), "opencode") {
		t.Errorf("error should mention opencode: %q", err.Error())
	}
}

// ---------------------------------------------------------------------------
// Agent runner — error propagation: empty output
// (We avoid calling opencode; instead test parseOpenCodeOutput directly)
// ---------------------------------------------------------------------------

func TestAgentRunnerDryRunEmptyEnvelope(t *testing.T) {
	// When envelope has no 'topic', dry-run still succeeds.
	runner := NewAgentRunner(true)
	agent := types.AgentConfig{ID: "x", Model: "m"}

	content, _, err := runner.Run(agent, map[string]any{})
	if err != nil {
		t.Fatalf("empty envelope: %v", err)
	}
	if content == "" {
		t.Error("dry-run should produce non-empty content")
	}
}

// ---------------------------------------------------------------------------
// parseOpenCodeOutput
// ---------------------------------------------------------------------------

func TestParseOpenCodeOutputEmpty(t *testing.T) {
	textParts, metadata, err := parseOpenCodeOutput("")
	if err != nil {
		t.Errorf("empty output: %v", err)
	}
	if len(textParts) != 0 {
		t.Errorf("expected empty text parts, got %d", len(textParts))
	}
	if metadata == nil {
		t.Error("metadata should not be nil")
	}
}

func TestParseOpenCodeOutputTextEvents(t *testing.T) {
	output := `{"type":"text","part":{"text":"Hello "}}
{"type":"text","part":{"text":"world"}}`

	textParts, _, err := parseOpenCodeOutput(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	combined := strings.Join(textParts, "")
	if combined != "Hello world" {
		t.Errorf("text: got %q, want %q", combined, "Hello world")
	}
}

func TestParseOpenCodeOutputStepFinish(t *testing.T) {
	output := `{"type":"text","part":{"text":"response"}}
{"type":"step_finish","part":{"tokens":{"total":150.0,"input":100.0,"output":50.0},"cost":0.002}}`

	_, metadata, err := parseOpenCodeOutput(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	if metadata.Tokens.Total == nil || *metadata.Tokens.Total != 150 {
		t.Errorf("total: got %v, want 150", metadata.Tokens.Total)
	}
	if metadata.Tokens.Input == nil || *metadata.Tokens.Input != 100 {
		t.Errorf("input: got %v, want 100", metadata.Tokens.Input)
	}
	if metadata.Tokens.Output == nil || *metadata.Tokens.Output != 50 {
		t.Errorf("output: got %v, want 50", metadata.Tokens.Output)
	}

	if metadata.Cost == nil || *metadata.Cost != 0.002 {
		t.Errorf("cost: got %v, want 0.002", metadata.Cost)
	}
}

func TestParseOpenCodeOutputErrorEvent(t *testing.T) {
	output := `{"type":"text","part":{"text":"partial"}}
{"type":"error","error":"api rate limit exceeded"}`

	_, _, err := parseOpenCodeOutput(output)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "api rate limit exceeded") {
		t.Errorf("error message: %q", err.Error())
	}
}

func TestParseOpenCodeOutputErrorEventNoMessage(t *testing.T) {
	output := `{"type":"error"}`

	_, _, err := parseOpenCodeOutput(output)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "opencode run error") {
		t.Errorf("error message: %q", err.Error())
	}
}

func TestParseOpenCodeOutputStructuredProviderError(t *testing.T) {
	output := `{"type":"error","error":{"data":{"message":"Type validation failed: Value: {\"code\":\"InvalidParameter\",\"message\":\"<400> InternalError.Algo.DataInspectionFailed: Output data may contain inappropriate content.\",\"request_id\":\"59d9759f-b2a2-4350-ab9a-8478a3d39d40\"}.\nError message: [{\"code\":\"invalid_union\"}]"},"name":"UnknownError"},"sessionID":"ses_206d17a06ffedPGNrxSgSplhLO"}`

	_, _, err := parseOpenCodeOutput(output)
	if err == nil {
		t.Fatal("expected error")
	}
	message := err.Error()
	for _, want := range []string{
		"UnknownError",
		"InvalidParameter",
		"Output data may contain inappropriate content",
		"request_id: 59d9759f-b2a2-4350-ab9a-8478a3d39d40",
	} {
		if !strings.Contains(message, want) {
			t.Errorf("error message %q missing %q", message, want)
		}
	}
	if strings.Contains(message, "map[") || strings.Contains(message, "invalid_union") {
		t.Errorf("error message should not expose raw validation payload: %q", message)
	}
}

func TestParseOpenCodeOutputNonJSONLines(t *testing.T) {
	output := `not json at all
{"type":"text","part":{"text":"real"}}
also not json`

	textParts, _, err := parseOpenCodeOutput(output)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(textParts) != 1 || textParts[0] != "real" {
		t.Errorf("text parts: got %v, want [real]", textParts)
	}
}

func TestParseOpenCodeOutputWhitespaceOnly(t *testing.T) {
	output := "\n  \n\t\n"
	textParts, _, err := parseOpenCodeOutput(output)
	if err != nil {
		t.Fatalf("parse whitespace: %v", err)
	}
	if len(textParts) != 0 {
		t.Errorf("expected no text parts, got %d", len(textParts))
	}
}

// ---------------------------------------------------------------------------
// Semantic parity: JSON marshal for error propagation
// ---------------------------------------------------------------------------

func TestEnvelopeMarshaling(t *testing.T) {
	// Verify that the envelope structure used by the agent runner serializes
	// to valid JSON with the expected keys.
	envelope := map[string]any{
		"topic": "test topic",
		"history": []map[string]string{
			{"agent_id": "a", "content": "msg1"},
			{"agent_id": "b", "content": "msg2"},
		},
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal envelope: %v", err)
	}

	if decoded["topic"] != "test topic" {
		t.Errorf("topic: got %v", decoded["topic"])
	}
	history, ok := decoded["history"].([]any)
	if !ok || len(history) != 2 {
		t.Fatalf("history: got %v (len=%d)", decoded["history"], len(history))
	}
	item0, _ := history[0].(map[string]any)
	if item0["agent_id"] != "a" || item0["content"] != "msg1" {
		t.Errorf("history[0]: %v", item0)
	}
}

// ---------------------------------------------------------------------------
// convertTokens
// ---------------------------------------------------------------------------

func TestConvertTokens(t *testing.T) {
	input := map[string]any{
		"total":     float64(150),
		"input":     float64(100),
		"output":    float64(50),
		"reasoning": float64(30),
	}

	converted := convertTokens(input)

	if converted.Total == nil || *converted.Total != 150 {
		t.Errorf("total: got %v, want 150", converted.Total)
	}
	if converted.Input == nil || *converted.Input != 100 {
		t.Errorf("input: got %v, want 100", converted.Input)
	}
	if converted.Reasoning == nil || *converted.Reasoning != 30 {
		t.Errorf("reasoning: got %v, want 30", converted.Reasoning)
	}
}

func TestConvertTokensNil(t *testing.T) {
	converted := convertTokens(nil)
	if converted.Total != nil || converted.Input != nil || converted.Output != nil || converted.Reasoning != nil {
		t.Errorf("nil input should return zero TokenUsage, got %#v", converted)
	}
}

// ---------------------------------------------------------------------------
// Dry-run specific fields (cost pointer behavior)
// ---------------------------------------------------------------------------

func TestDryRunCostIsNonNull(t *testing.T) {
	runner := NewAgentRunner(true)
	agent := types.AgentConfig{ID: "x", Model: "m"}
	envelope := map[string]any{"topic": "test"}

	_, metadata, err := runner.Run(agent, envelope)
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}

	if metadata.Cost == nil {
		t.Error("cost should be non-nil *float64 in dry-run metadata")
	}
}

// ---------------------------------------------------------------------------
// parseOpenCodeOutput: partial JSON streams (missing fields)
// ---------------------------------------------------------------------------

func TestParseOpenCodeOutputPartialJSON(t *testing.T) {
	// text event without 'part' field.
	output := `{"type":"text"}
{"type":"step_finish"}`

	textParts, metadata, err := parseOpenCodeOutput(output)
	if err != nil {
		t.Fatalf("parse partial: %v", err)
	}
	if len(textParts) != 0 {
		t.Errorf("expected 0 text parts, got %d", len(textParts))
	}
	// Should still return non-nil metadata with zero-valued fields.
	if metadata == nil {
		t.Error("metadata should not be nil")
	}
}

// ---------------------------------------------------------------------------
// Semantic parity: multiline consensus with leading/trailing whitespace
// (Python re.DOTALL + re.IGNORECASE behavior)
// ---------------------------------------------------------------------------

func TestExtractConsensusMultilineWhitespace(t *testing.T) {
	content := "  [CONSENSUS:  \n  multi  \n  line  ]  "
	_, hasConsensus, statement, _ := ExtractConsensus(content)
	if !hasConsensus {
		t.Fatal("expected hasConsensus=true")
	}
	// Python's re.findall with trim: "multi  \n  line"
	// Go's strings.TrimSpace trims leading/trailing whitespace from the capture.
	want := "multi  \n  line"
	if statement != want {
		t.Errorf("statement: got %q, want %q", statement, want)
	}
}

func TestExtractConsensusRejectsContradictoryStatement(t *testing.T) {
	content := "[CONSENSUS: I do not agree with the current integration because it weakens the mandate.]\nI find a critical tension."
	_, hasConsensus, _, ignored := ExtractConsensus(content)
	if hasConsensus {
		t.Fatal("expected contradictory consensus to be rejected")
	}
	if !ignored {
		t.Fatal("expected ignored=true when marker expresses disagreement")
	}
}

func TestExtractConsensusRejectsBodyDisagreementWithoutEndorsement(t *testing.T) {
	content := "[CONSENSUS: The discussion was productive.]\nI find a critical tension in the current proposals."
	_, hasConsensus, _, ignored := ExtractConsensus(content)
	if hasConsensus {
		t.Fatal("expected body disagreement without endorsement to reject consensus")
	}
	if !ignored {
		t.Fatal("expected ignored=true")
	}
}

// ---------------------------------------------------------------------------
// WriteReadOnlyConfig
// ---------------------------------------------------------------------------

func TestWriteReadOnlyConfigWritesAndCleansUp(t *testing.T) {
	dir := t.TempDir()

	cleanup, err := WriteReadOnlyConfig(dir)
	if err != nil {
		t.Fatalf("WriteReadOnlyConfig: %v", err)
	}

	configPath := filepath.Join(dir, "opencode.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), `"edit": "deny"`) {
		t.Errorf("config should deny edit, got: %s", string(data))
	}
	if !strings.Contains(string(data), `"read": "allow"`) {
		t.Errorf("config should allow read, got: %s", string(data))
	}

	cleanup()

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Errorf("config should be removed after cleanup, got stat err: %v", err)
	}
}

func TestWriteReadOnlyConfigRespectsExistingConfig(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "opencode.json"), []byte(`{"existing": true}`), 0o644); err != nil {
		t.Fatalf("writing existing config: %v", err)
	}

	cleanup, err := WriteReadOnlyConfig(dir)
	if err != nil {
		t.Fatalf("WriteReadOnlyConfig: %v", err)
	}
	cleanup()

	data, err := os.ReadFile(filepath.Join(dir, "opencode.json"))
	if err != nil {
		t.Fatalf("reading config: %v", err)
	}
	if !strings.Contains(string(data), `"existing": true`) {
		t.Errorf("existing config should be preserved, got: %s", string(data))
	}
}
