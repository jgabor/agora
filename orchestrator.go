package kumbaja

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// SYNTHESIS_SYSTEM_PROMPT is the system prompt used to instruct the synthesis
// agent to produce a structured JSON summary of a deliberation.
const SYNTHESIS_SYSTEM_PROMPT = `You are a deliberation synthesis agent. Your job is to read the full transcript
of a multi-agent deliberation and produce a structured summary.

Your output must be valid JSON with this exact structure:
{
  "key_arguments": ["argument 1", "argument 2", ...],
  "points_of_agreement": ["agreement 1", ...],
  "unresolved_tensions": ["tension 1", ...],
  "recommended_decision": "...",
  "confidence": "high|medium|low"
}

Be concise but thorough. Capture the essential insights from the deliberation.
`

// DeliberationState tracks the runtime state of an ongoing deliberation.
type DeliberationState struct {
	Config      *DeliberationConfig
	Topic       string
	Window      int
	MaxTurns    int
	TimeLimit   int
	Budget      *float64
	FullContext bool

	Turn      int
	StartTime float64
	Running   bool
	HaltedBy  string
}

// Orchestrator orchestrates multi-agent deliberation.
type Orchestrator struct {
	state      *DeliberationState
	transcript *TranscriptManager
	runner     *AgentRunner

	numAgents       int
	consensusStreak int
}

// NewOrchestrator creates a new Orchestrator with the given state, transcript, and runner.
func NewOrchestrator(state *DeliberationState, transcript *TranscriptManager, runner *AgentRunner) *Orchestrator {
	return &Orchestrator{
		state:      state,
		transcript: transcript,
		runner:     runner,
		numAgents:  len(state.Config.Agents),
	}
}

// Run executes the full deliberation loop. It returns the computed stats.
func (o *Orchestrator) Run() DeliberationStats {
	o.state.Running = true
	o.state.StartTime = float64(time.Now().UnixNano()) / 1e9

	o.setupSignalHandler()

	// Seed message (unless resuming from an existing transcript).
	if len(o.transcript.Records()) == 0 {
		o.emitSeed()
	}

	for o.state.Running && o.state.Turn < o.state.MaxTurns {
		o.checkTerminationConditions()
		if !o.state.Running {
			break
		}

		agentIdx := o.state.Turn % o.numAgents
		agent := o.state.Config.Agents[agentIdx]

		turnRecord := o.executeTurn(agent)
		if err := o.transcript.Append(turnRecord); err != nil {
			o.state.Running = false
			o.state.HaltedBy = fmt.Sprintf("error: %v", err)
			break
		}
		o.consensusStreak = o.transcript.ConsecutiveConsensusCount()

		o.state.Turn++
	}

	// If loop exited cleanly (not halted), it reached max_turns.
	if o.state.Running && o.state.Turn >= o.state.MaxTurns {
		o.state.HaltedBy = fmt.Sprintf("max_turns (%d)", o.state.MaxTurns)
	}

	// Save the final transcript to disk.
	_ = o.transcript.WriteAll()

	// If interrupted, exit gracefully.
	if o.state.HaltedBy == "user_interrupt" {
		os.Exit(130)
	}

	return ComputeStats(o.transcript.Records())
}

// Synthesize runs the final synthesis agent after deliberation completes.
// Returns the parsed JSON summary, or nil if the transcript is too short.
func (o *Orchestrator) Synthesize() map[string]any {
	if len(o.transcript.Records()) <= 1 {
		return nil
	}

	engine := NewSynthesisEngine(o.runner)
	return engine.Synthesize(o.transcript.Records(), o.state.Topic, o.state.Config)
}

// emitSeed writes the orchestrator seed message as the first record.
func (o *Orchestrator) emitSeed() {
	seed := TurnRecord{
		Turn:      -1,
		AgentID:   "orchestrator",
		Model:     nil,
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Content:   fmt.Sprintf("Begin deliberating on the following topic: %s", o.state.Topic),
		Elapsed:   0.0,
	}
	_ = o.transcript.Append(seed)
}

// checkTerminationConditions checks all termination conditions in order:
// time limit, consensus threshold, budget. If any condition is met,
// it sets running to false and records the halt reason.
func (o *Orchestrator) checkTerminationConditions() {
	elapsed := float64(time.Now().UnixNano())/1e9 - o.state.StartTime

	// Time limit.
	if o.state.TimeLimit > 0 && elapsed >= float64(o.state.TimeLimit) {
		o.state.Running = false
		o.state.HaltedBy = fmt.Sprintf("time_limit (%ds)", o.state.TimeLimit)
		return
	}

	// Consensus threshold.
	if o.state.Config.ConsensusThreshold > 0 &&
		o.consensusStreak >= o.state.Config.ConsensusThreshold {
		o.state.Running = false
		o.state.HaltedBy = fmt.Sprintf("consensus (%d consecutive agreements)", o.consensusStreak)
		return
	}

	// Budget.
	if o.state.Budget != nil && o.transcript.TotalCost() >= *o.state.Budget {
		o.state.Running = false
		o.state.HaltedBy = fmt.Sprintf("budget_exceeded ($%.2f)", *o.state.Budget)
		return
	}
}

// executeTurn runs a single agent turn and returns the resulting TurnRecord.
func (o *Orchestrator) executeTurn(agent AgentConfig) TurnRecord {
	turnStart := float64(time.Now().UnixNano()) / 1e9

	// Build history envelope.
	history := o.transcript.HistoryForAgent(
		agent.ID,
		o.state.Window,
		o.state.Config.Topology,
		o.numAgents,
		o.state.Turn,
	)

	envelope := map[string]any{
		"topic":   o.state.Topic,
		"history": history,
	}

	if o.state.FullContext {
		// Override history to include last K messages from ANY agent.
		records := o.transcript.Records()
		start := len(records) - o.state.Window
		if start < 0 {
			start = 0
		}
		fullHistory := make([]map[string]string, 0, len(records)-start)
		for _, r := range records[start:] {
			fullHistory = append(fullHistory, map[string]string{
				"agent_id": r.AgentID,
				"content":  r.Content,
			})
		}
		envelope["history"] = fullHistory
	}

	content, metadata, err := o.runner.Run(agent, envelope)
	// If runner returned an error, record it as the agent's content.
	if err != nil {
		o.state.Running = false
		o.state.HaltedBy = fmt.Sprintf("error: %v", err)
		turnDuration := float64(time.Now().UnixNano())/1e9 - turnStart
		return TurnRecord{
			Turn:      o.state.Turn,
			AgentID:   agent.ID,
			Model:     &agent.Model,
			Timestamp: float64(time.Now().UnixNano()) / 1e9,
			Content:   fmt.Sprintf("[ERROR] %v", err),
			Elapsed:   turnDuration,
		}
	}

	// Extract consensus.
	cleanedContent, hasConsensus, consensusStmt := ExtractConsensus(content)

	// Extract tokens from metadata.
	tokenMap, _ := metadata["tokens"].(map[string]any)
	tokens := TokenUsage{}
	if tokenMap != nil {
		if v, ok := tokenMap["total"]; ok {
			if iv, ok := v.(int); ok {
				tokens.Total = &iv
			}
		}
		if v, ok := tokenMap["input"]; ok {
			if iv, ok := v.(int); ok {
				tokens.Input = &iv
			}
		}
		if v, ok := tokenMap["output"]; ok {
			if iv, ok := v.(int); ok {
				tokens.Output = &iv
			}
		}
		if v, ok := tokenMap["reasoning"]; ok {
			if iv, ok := v.(int); ok {
				tokens.Reasoning = &iv
			}
		}
	}

	// Extract cost from metadata.
	var cost *float64
	if costVal, ok := metadata["cost"]; ok && costVal != nil {
		if c, ok := costVal.(*float64); ok {
			cost = c
		}
	}

	turnDuration := float64(time.Now().UnixNano())/1e9 - turnStart

	return TurnRecord{
		Turn:               o.state.Turn,
		AgentID:            agent.ID,
		Model:              &agent.Model,
		Timestamp:          float64(time.Now().UnixNano()) / 1e9,
		Content:            cleanedContent,
		Tokens:             tokens,
		Cost:               cost,
		Consensus:          hasConsensus,
		ConsensusStatement: consensusStmt,
		Elapsed:            turnDuration,
	}
}

// setupSignalHandler sets up signal handling for SIGINT and SIGTERM.
// When a signal is received, it sets running=false, saves the partial
// transcript, and records the halt reason as "user_interrupt".
func (o *Orchestrator) setupSignalHandler() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		o.state.Running = false
		o.state.HaltedBy = "user_interrupt"
		_ = o.transcript.WriteAll()
	}()
}

// SynthesisEngine generates a final synthesis from a deliberation transcript.
type SynthesisEngine struct {
	runner *AgentRunner
}

// NewSynthesisEngine creates a new SynthesisEngine with the given runner.
func NewSynthesisEngine(runner *AgentRunner) *SynthesisEngine {
	return &SynthesisEngine{runner: runner}
}

// Synthesize runs a synthesis agent to summarize the deliberation.
// Returns the parsed JSON summary.
func (se *SynthesisEngine) Synthesize(
	records []TurnRecord,
	topic string,
	config *DeliberationConfig,
) map[string]any {
	transcriptText := se.formatTranscript(records)

	// Count non-orchestrator turns.
	totalTurns := 0
	for _, r := range records {
		if r.AgentID != "orchestrator" {
			totalTurns++
		}
	}

	envelope := map[string]any{
		"topic":       topic,
		"transcript":  transcriptText,
		"num_agents":  len(config.Agents),
		"total_turns": totalTurns,
	}

	// Determine synthesis model.
	model := config.Agents[0].Model
	if config.SynthesisModel != nil {
		model = *config.SynthesisModel
	}

	synthAgent := AgentConfig{
		ID:           "synthesizer",
		Model:        model,
		SystemPrompt: SYNTHESIS_SYSTEM_PROMPT,
	}

	content, _, err := se.runner.Run(synthAgent, envelope)
	if err != nil {
		return map[string]any{
			"key_arguments":        []any{},
			"points_of_agreement":  []any{},
			"unresolved_tensions":  []any{},
			"recommended_decision": fmt.Sprintf("Synthesis failed: %v", err),
			"confidence":           "low",
		}
	}

	parsed, err := se.extractJSON(content)
	if err != nil {
		return map[string]any{
			"key_arguments":        []any{},
			"points_of_agreement":  []any{},
			"unresolved_tensions":  []any{},
			"recommended_decision": fmt.Sprintf("Synthesis failed: %v", err),
			"confidence":           "low",
		}
	}

	return parsed
}

// formatTranscript formats the transcript records into a text representation.
func (se *SynthesisEngine) formatTranscript(records []TurnRecord) string {
	var lines []string
	for _, r := range records {
		lines = append(lines, fmt.Sprintf("[Turn %d] %s: %s", r.Turn, r.AgentID, r.Content))
	}
	return strings.Join(lines, "\n")
}

// jsonBlockPattern matches JSON code blocks in markdown, optionally prefixed with "json".
var jsonBlockPattern = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

// extractJSON extracts a JSON object from a potentially markdown-wrapped response.
func (se *SynthesisEngine) extractJSON(content string) (map[string]any, error) {
	// Look for JSON in code blocks first.
	if m := jsonBlockPattern.FindStringSubmatch(content); m != nil {
		content = m[1]
	}

	// Find the first { and last }.
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("no JSON object found in synthesis response")
	}
	content = content[start : end+1]

	var result map[string]any
	if err := json.Unmarshal([]byte(content), &result); err != nil {
		return nil, fmt.Errorf("parsing synthesis JSON: %w", err)
	}
	return result, nil
}
