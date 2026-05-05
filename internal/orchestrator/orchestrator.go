// Package orchestrator runs the closed-loop multi-agent deliberation.
package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

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

// TurnFunc is called after each agent turn completes.
type TurnFunc func(record types.TurnRecord, turn int, maxTurns int)

// Orchestrator orchestrates multi-agent deliberation.
type Orchestrator struct {
	state      *types.DeliberationState
	transcript *transcript.TranscriptManager
	runner     agent.Runner
	evidence   EvidenceCollector
	onTurn     TurnFunc

	numAgents       int
	consensusStreak int
	sharedEvidence  *types.EvidenceBundle
	evidenceSent    map[string]bool
}

// EvidenceCollector gathers shared evidence before the first deliberation turn.
type EvidenceCollector interface {
	Collect(request types.EvidenceRequest) (*types.EvidenceBundle, error)
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(state *types.DeliberationState, tm *transcript.TranscriptManager, runner agent.Runner) *Orchestrator {
	return &Orchestrator{
		state:        state,
		transcript:   tm,
		runner:       runner,
		numAgents:    len(state.Config.Agents),
		evidenceSent: make(map[string]bool),
	}
}

// SetEvidenceCollector registers a pre-deliberation evidence collector.
func (o *Orchestrator) SetEvidenceCollector(collector EvidenceCollector) {
	o.evidence = collector
}

// OnTurn registers a callback invoked after each agent turn.
func (o *Orchestrator) OnTurn(fn TurnFunc) {
	o.onTurn = fn
}

// Run executes the full deliberation loop.
func (o *Orchestrator) Run() types.DeliberationStats {
	o.state.Running = true
	o.state.StartTime = float64(time.Now().UnixNano()) / 1e9

	o.setupSignalHandler()

	if len(o.transcript.Records()) == 0 {
		if !o.collectEvidence() {
			_ = o.transcript.WriteAll()
			return types.ComputeStats(o.transcript.Records())
		}
		o.emitSeed()
	}

	for o.state.Running && (o.state.MaxTurns <= 0 || o.state.Turn < o.state.MaxTurns) {
		o.checkTerminationConditions()
		if !o.state.Running {
			break
		}

		agentIdx := o.state.Turn % o.numAgents
		ag := o.state.Config.Agents[agentIdx]

		turnRecord, ok := o.executeTurn(ag)
		if !ok {
			o.state.Turn++
			continue
		}
		if err := o.transcript.Append(turnRecord); err != nil {
			o.state.Running = false
			o.state.HaltedBy = fmt.Sprintf("error: %v", err)
			break
		}
		o.consensusStreak = o.transcript.ConsecutiveConsensusCount()

		if o.onTurn != nil {
			o.onTurn(turnRecord, o.state.Turn, o.state.MaxTurns)
		}

		o.state.Turn++
	}

	if o.state.Running && o.state.MaxTurns > 0 && o.state.Turn >= o.state.MaxTurns {
		o.state.HaltedBy = fmt.Sprintf("max_turns (%d)", o.state.MaxTurns)
	}

	_ = o.transcript.WriteAll()

	if o.state.HaltedBy == "user_interrupt" {
		os.Exit(130)
	}

	return types.ComputeStats(o.transcript.Records())
}

func (o *Orchestrator) collectEvidence() bool {
	request := o.state.Evidence
	if !request.ResearchEnabled && len(request.ContextPaths) == 0 {
		return true
	}
	request.Topic = o.state.Topic
	if request.ResearchModel == "" && len(o.state.Config.Agents) > 0 {
		request.ResearchModel = o.state.Config.Agents[0].Model
	}
	if o.evidence == nil {
		o.state.Running = false
		o.state.HaltedBy = "research_error: evidence collector unavailable"
		return false
	}

	bundle, err := o.evidence.Collect(request)
	if err != nil {
		o.state.Running = false
		o.state.HaltedBy = fmt.Sprintf("research_error: %v", err)
		return false
	}
	if bundle == nil || len(bundle.SourceReferences) == 0 {
		o.state.Running = false
		o.state.HaltedBy = "research_error: no source references produced"
		return false
	}
	o.sharedEvidence = bundle
	_ = o.transcript.Append(types.TurnRecord{
		Turn:      -2,
		AgentID:   "orchestrator",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Content:   bundle.Summary,
		Evidence:  bundle,
	})
	return true
}

// Synthesize runs the final synthesis agent after deliberation completes.
func (o *Orchestrator) Synthesize() map[string]any {
	if len(o.transcript.Records()) <= 1 {
		return nil
	}

	engine := NewSynthesisEngine(o.runner)
	return engine.Synthesize(o.transcript.Records(), o.state.Topic, o.state.Config)
}

func (o *Orchestrator) emitSeed() {
	seed := types.TurnRecord{
		Turn:      -1,
		AgentID:   "orchestrator",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Content:   fmt.Sprintf("Begin deliberating on the following topic: %s", o.state.Topic),
	}
	_ = o.transcript.Append(seed)
}

func (o *Orchestrator) checkTerminationConditions() {
	elapsed := float64(time.Now().UnixNano())/1e9 - o.state.StartTime

	if o.state.TimeLimit > 0 && elapsed >= float64(o.state.TimeLimit) {
		o.state.Running = false
		o.state.HaltedBy = fmt.Sprintf("time_limit (%ds)", o.state.TimeLimit)
		return
	}

	if o.state.Config.ConsensusThreshold > 0 &&
		o.consensusStreak >= o.state.Config.ConsensusThreshold {
		o.state.Running = false
		o.state.HaltedBy = fmt.Sprintf("consensus (%d consecutive agreements)", o.consensusStreak)
		return
	}

	if o.state.Budget != nil && o.transcript.TotalCost() >= *o.state.Budget {
		o.state.Running = false
		o.state.HaltedBy = fmt.Sprintf("budget_exceeded ($%.2f)", *o.state.Budget)
		return
	}
}

func (o *Orchestrator) executeTurn(ag types.AgentConfig) (types.TurnRecord, bool) {
	turnStart := float64(time.Now().UnixNano()) / 1e9

	history := o.transcript.HistoryForAgent(
		ag.ID,
		o.state.Window,
		o.state.Config.Topology,
		o.numAgents,
		o.state.Turn,
	)

	envelope := map[string]any{
		"topic":   o.state.Topic,
		"history": history,
	}
	if o.sharedEvidence != nil && !o.evidenceSent[ag.ID] {
		envelope["evidence"] = o.sharedEvidence
		o.evidenceSent[ag.ID] = true
	}

	if o.state.FullContext {
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

	content, metadata, err := o.runner.Run(ag, envelope)
	if err != nil {
		return types.TurnRecord{}, false
	}

	cleanedContent, hasConsensus, consensusStmt := agent.ExtractConsensus(content)

	tokens := turnTokens(metadata)
	cost := turnCost(metadata)

	return types.TurnRecord{
		Turn:               o.state.Turn,
		AgentID:            ag.ID,
		Model:              &ag.Model,
		Timestamp:          float64(time.Now().UnixNano()) / 1e9,
		Content:            cleanedContent,
		Tokens:             tokens,
		Cost:               cost,
		Consensus:          hasConsensus,
		ConsensusStatement: consensusStmt,
		Elapsed:            float64(time.Now().UnixNano())/1e9 - turnStart,
	}, true
}

func turnTokens(metadata map[string]any) types.TokenUsage {
	tokenMap, _ := metadata["tokens"].(map[string]any)
	if tokenMap == nil {
		return types.TokenUsage{}
	}

	return types.TokenUsage{
		Total:     intMetadata(tokenMap, "total"),
		Input:     intMetadata(tokenMap, "input"),
		Output:    intMetadata(tokenMap, "output"),
		Reasoning: intMetadata(tokenMap, "reasoning"),
	}
}

func intMetadata(metadata map[string]any, key string) *int {
	iv, ok := metadata[key].(int)
	if !ok {
		return nil
	}
	return &iv
}

func turnCost(metadata map[string]any) *float64 {
	cost, _ := metadata["cost"].(*float64)
	return cost
}

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
	runner agent.Runner
}

// NewSynthesisEngine creates a new SynthesisEngine.
func NewSynthesisEngine(runner agent.Runner) *SynthesisEngine {
	return &SynthesisEngine{runner: runner}
}

// Synthesize runs a synthesis agent to summarize the deliberation.
func (se *SynthesisEngine) Synthesize(
	records []types.TurnRecord,
	topic string,
	config *types.DeliberationConfig,
) map[string]any {
	transcriptText := se.formatTranscript(records)

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

	model := config.Agents[0].Model
	if config.SynthesisModel != nil {
		model = *config.SynthesisModel
	}

	synthAgent := types.AgentConfig{
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

func (se *SynthesisEngine) formatTranscript(records []types.TurnRecord) string {
	var lines []string
	for _, r := range records {
		lines = append(lines, fmt.Sprintf("[Turn %d] %s: %s", r.Turn, r.AgentID, r.Content))
	}
	return strings.Join(lines, "\n")
}

var jsonBlockPattern = regexp.MustCompile("(?s)```(?:json)?\\s*(\\{.*?\\})\\s*```")

func (se *SynthesisEngine) extractJSON(content string) (map[string]any, error) {
	if m := jsonBlockPattern.FindStringSubmatch(content); m != nil {
		content = m[1]
	}

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
