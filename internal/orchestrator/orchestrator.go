// Package orchestrator runs the closed-loop multi-agent deliberation.
package orchestrator

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/evidence"
	"github.com/jgabor/agora/internal/synthesis"
	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

// TurnFunc is called after each agent turn completes.
type TurnFunc func(record types.TurnRecord, turn int, maxTurns int)

// EvidenceFunc is called after shared evidence is collected.
type EvidenceFunc func(evidence types.EvidenceBundle)

// ActivityFunc is called when a long-running phase starts and returns cleanup.
type ActivityFunc func(phase string) func()

// Orchestrator orchestrates multi-agent deliberation.
type Orchestrator struct {
	state      *types.DeliberationState
	transcript *transcript.TranscriptManager
	runner     agent.Runner
	evidence   evidence.Collector
	onTurn     TurnFunc
	onEvidence EvidenceFunc
	onActivity ActivityFunc

	numAgents       int
	consensusStreak int
	sharedEvidence  *types.EvidenceBundle
	evidenceSent    map[string]bool
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
func (o *Orchestrator) SetEvidenceCollector(collector evidence.Collector) {
	o.evidence = collector
}

// OnTurn registers a callback invoked after each agent turn.
func (o *Orchestrator) OnTurn(fn TurnFunc) {
	o.onTurn = fn
}

// OnEvidence registers a callback invoked after pre-deliberation evidence collection.
func (o *Orchestrator) OnEvidence(fn EvidenceFunc) {
	o.onEvidence = fn
}

// OnActivity registers a callback invoked around long-running phases.
func (o *Orchestrator) OnActivity(fn ActivityFunc) {
	o.onActivity = fn
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
		o.consensusStreak = transcript.ConsecutiveConsensusCount(o.transcript.Records())

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

	stop := o.activity("Research")
	bundle, err := o.evidence.Collect(request)
	stop()
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
	auditEvidence := *bundle
	auditEvidence.ContextDocuments = nil
	_ = o.transcript.Append(types.TurnRecord{
		Turn:      -2,
		AgentID:   "orchestrator",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Content:   bundle.Summary,
		Evidence:  &auditEvidence,
	})
	if o.onEvidence != nil {
		o.onEvidence(auditEvidence)
	}
	return true
}

// Synthesize runs the final synthesis agent after deliberation completes.
func (o *Orchestrator) Synthesize() map[string]any {
	if len(o.transcript.Records()) <= 1 {
		return nil
	}
	// Skip synthesis in dry-run mode — there is no real LLM response to summarize.
	if ar, ok := o.runner.(*agent.AgentRunner); ok && ar.IsDryRun() {
		return nil
	}
	stop := o.activity("Synthesis")
	defer stop()
	result := synthesis.Synthesize(o.runner, o.transcript.Records(), o.state.Topic, o.synthesizeModel())

	content, _ := json.Marshal(result)
	_ = o.transcript.Append(types.TurnRecord{
		AgentID:   "synthesizer",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Content:   string(content),
	})
	_ = o.transcript.WriteAll()

	return result
}

// synthesizeModel returns the model to use for synthesis (explicit override or first agent's model).
func (o *Orchestrator) synthesizeModel() string {
	if o.state.Config.SynthesisModel != nil {
		return *o.state.Config.SynthesisModel
	}
	return o.state.Config.Agents[0].Model
}

func (o *Orchestrator) activity(phase string) func() {
	if o.onActivity == nil {
		return func() {}
	}
	stop := o.onActivity(phase)
	if stop == nil {
		return func() {}
	}
	return stop
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

	if o.state.Budget != nil && transcript.TotalCost(o.transcript.Records()) >= *o.state.Budget {
		o.state.Running = false
		o.state.HaltedBy = fmt.Sprintf("budget_exceeded ($%.2f)", *o.state.Budget)
		return
	}
}

func (o *Orchestrator) executeTurn(ag types.AgentConfig) (types.TurnRecord, bool) {
	turnStart := float64(time.Now().UnixNano()) / 1e9

	history := transcript.HistoryForAgent(
		o.transcript.Records(),
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

	stop := o.activity(fmt.Sprintf("Generation: %s", ag.ID))
	content, meta, err := o.runner.Run(agent.WithReadOnlyAgentPrompt(ag), envelope)
	stop()
	if err != nil {
		return types.TurnRecord{}, false
	}

	cleanedContent, hasConsensus, consensusStmt := agent.ExtractConsensus(content)

	var tokens types.TokenUsage
	var cost *float64
	if meta != nil {
		tokens = meta.Tokens
		cost = meta.Cost
	}

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
