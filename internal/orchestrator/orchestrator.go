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
	"github.com/jgabor/agora/internal/ledger"
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

	numAgents      int
	sharedEvidence *types.EvidenceBundle
	evidenceSent   map[string]bool
	currentLedger  *types.DebateLedger
	ledgerUpdater  *ledger.Updater
}

// NewOrchestrator creates a new Orchestrator.
func NewOrchestrator(state *types.DeliberationState, tm *transcript.TranscriptManager, runner agent.Runner) *Orchestrator {
	if state != nil && state.Control != nil && state.Config != nil && state.Control.Phase != types.PhaseTerminal && !hasPersistedControlState(tm.Records()) {
		// A fresh run establishes its requirements before writing the first
		// typed control snapshot. Resumed controls already carry the persisted
		// run contract and must not be rehydrated from mutable resume inputs.
		state.Control.Convergence.RequiredEndorsements = state.Config.ConsensusThreshold
		state.Control.Convergence.MinimumRounds = state.Config.EffectiveMinRounds()
		state.Control.Convergence.RunContractVersion = types.RunContractVersion
		state.Control.Convergence.RequiredDeliverableItems = state.Config.RequiredDeliverableItems
	}
	return &Orchestrator{
		state:          state,
		transcript:     tm,
		runner:         runner,
		numAgents:      len(state.Config.Agents),
		evidenceSent:   make(map[string]bool),
		sharedEvidence: state.SharedEvidence,
	}
}

func hasPersistedControlState(records []types.TurnRecord) bool {
	for _, record := range records {
		if record.Control != nil {
			return true
		}
	}
	return false
}

// SetEvidenceCollector registers a pre-deliberation evidence collector.
func (o *Orchestrator) SetEvidenceCollector(collector evidence.Collector) {
	o.evidence = collector
}

// SetLedgerUpdater registers a mid-deliberation ledger updater. When nil no
// per-round ledger update fires; the orchestrator remains otherwise functional
// so tests that don't exercise the ledger stay independent. When set, the
// updater fires once per completed agent round (after the last agent in the
// round completes its turn), gated by ledgerEnabled(LedgerUpdateEnabled) and
// o.state.Running so a mid-round interrupt never produces a partial ledger.
func (o *Orchestrator) SetLedgerUpdater(u *ledger.Updater) {
	o.ledgerUpdater = u
}

// SetCurrentLedger sets the most recent ledger injected into each agent turn.
func (o *Orchestrator) SetCurrentLedger(ledger *types.DebateLedger) {
	o.currentLedger = ledger
}

// SetSharedEvidence hydrates the authoritative references persisted with a
// resumed transcript. The bundle is references-only when loaded from disk;
// fresh runs still receive the bounded runtime bundle collected before the
// first turn.
func (o *Orchestrator) SetSharedEvidence(bundle *types.EvidenceBundle) {
	o.sharedEvidence = bundle
	if o.state != nil {
		o.state.SharedEvidence = bundle
	}
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
	if o.state.Control != nil && o.state.Control.Phase == types.PhaseTerminal {
		if err := o.state.Control.Validate(); err != nil {
			o.fail("control_error:", err)
			return types.ComputeStats(o.transcript.Records())
		}
		o.state.Running = false
		o.state.HaltedBy = terminalHaltReason(o.state.Control.Outcome)
		return types.ComputeStats(o.transcript.Records())
	}

	o.setupSignalHandler()

	if len(o.transcript.Records()) == 0 {
		if !o.collectEvidence() {
			if err := o.transcript.WriteAll(); err != nil {
				o.fail("error:", err)
			}
			return types.ComputeStats(o.transcript.Records())
		}
		o.emitSeed()
		if o.state.Failure != nil {
			return types.ComputeStats(o.transcript.Records())
		}
	}
	prepareNextTurn(o.state.Control, o.currentLedger, true)

	for o.state.Running && (o.state.MaxTurns <= 0 || o.state.Turn < o.state.MaxTurns) {
		o.checkTerminationConditions()
		if !o.state.Running {
			break
		}

		ag := o.nextAgent()

		turnRecord, ok := o.executeTurn(ag)
		if !ok {
			o.state.Turn++
			continue
		}
		if err := o.transcript.Append(turnRecord); err != nil {
			o.fail("error:", err)
			break
		}
		if turnRecord.Control != nil {
			o.state.Control = turnRecord.Control
		}
		if o.onTurn != nil {
			o.onTurn(turnRecord, o.state.Turn, o.state.MaxTurns)
		}

		o.updateLedgerIfRoundComplete()
		o.moderateAfterRound()
		if !o.state.Running {
			break
		}

		o.state.Turn++
		o.checkConsensusCondition()
	}

	if o.state.Running && o.state.MaxTurns > 0 && o.state.Turn >= o.state.MaxTurns {
		o.haltNoConsensus(fmt.Sprintf("max_turns (%d)", o.state.MaxTurns))
	}

	if err := o.transcript.WriteAll(); err != nil {
		o.fail("error:", err)
	}

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
		o.fail("evidence_error:", fmt.Errorf("evidence collector unavailable"))
		return false
	}

	stop := o.activity("Evidence")
	bundle, err := o.evidence.Collect(request)
	stop()
	if err != nil {
		o.fail("evidence_error:", err)
		return false
	}
	if bundle == nil || len(bundle.SourceReferences) == 0 {
		o.fail("evidence_error:", fmt.Errorf("no source references produced"))
		return false
	}
	if o.state.Control != nil {
		o.state.Control.SourceReferenceCount = len(bundle.SourceReferences)
	}
	o.sharedEvidence = bundle
	o.state.SharedEvidence = bundle
	auditEvidence := *bundle
	auditEvidence.ContextDocuments = nil
	if err := o.transcript.Append(types.TurnRecord{
		Turn:      -2,
		AgentID:   "moderator",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Content:   bundle.Summary,
		Evidence:  &auditEvidence,
	}); err != nil {
		o.fail("error:", err)
		return false
	}
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
	if err := o.transcript.Append(types.TurnRecord{
		AgentID:   "synthesizer",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Content:   string(content),
	}); err != nil {
		o.fail("error:", err)
		return result
	}
	if err := o.transcript.WriteAll(); err != nil {
		o.fail("error:", err)
	}

	return result
}

// synthesizeModel returns the model to use for synthesis (explicit override or first agent's model).
func (o *Orchestrator) synthesizeModel() string {
	return o.state.Config.EffectiveMetaModel()
}

// isDryRun reports whether the runner is in a simulated dry-run mode. Matches
// the Synthesize path's *agent.AgentRunner type assertion.
func (o *Orchestrator) isDryRun() bool {
	if ar, ok := o.runner.(*agent.AgentRunner); ok && ar.IsDryRun() {
		return true
	}
	return false
}

// updateLedgerIfRoundComplete fires the ledger updater once per completed
// agent round (when o.state.Turn is the last agent in the round). The first
// agent of the next round sees the freshly set currentLedger in its envelope.
// When no updater is set, the disable flag is active, the round has not
// completed, or the run was interrupted mid-round, the call is a no-op so the
// prior ledger (if any) is preserved and no partial mid-round update is
// produced. Dry-run mode routes through UpdateDryRun to avoid model calls;
// real mode calls Update and logs (without halting) on failure so failed
// updates are non-fatal and the next round retries using the prior ledger.
func (o *Orchestrator) updateLedgerIfRoundComplete() {
	if o.ledgerUpdater == nil {
		return
	}
	if !ledgerEnabled(o.state.LedgerUpdateEnabled) {
		return
	}
	if !o.state.Running {
		return
	}
	if (o.state.Turn+1)%o.numAgents != 0 {
		return
	}

	round := (o.state.Turn + 1) / o.numAgents

	stop := o.activity("Ledger Update")
	defer stop()

	if o.isDryRun() {
		ledger := o.ledgerUpdater.UpdateDryRun(o.transcript.Records(), o.state.Topic)
		ledger.Round = round
		o.SetCurrentLedger(ledger)
		persistLedgerRecord(o.transcript, ledger)
		return
	}

	ledger, err := o.ledgerUpdater.Update(o.transcript.Records(), o.state.Topic, o.synthesizeModel())
	if err != nil {
		fmt.Fprintf(os.Stderr, "ledger update: %v\n", err)
		return
	}
	ledger.Round = round
	o.SetCurrentLedger(ledger)
	persistLedgerRecord(o.transcript, ledger)
}

// persistLedgerRecord appends a typed ledger snapshot as a TurnRecord using the
// LedgerSentinelTurn (-3) / LedgerAgentID ("ledger") sentinel convention, the
// next sentinel beyond -2 (evidence). A nil ledger is a no-op so a failed or
// empty update never writes a malformed record. The persisted Ledger is a deep
// clone so later mutations of o.currentLedger cannot retroactively alter the
// transcript snapshot.
func persistLedgerRecord(tm *transcript.TranscriptManager, ledger *types.DebateLedger) {
	if ledger == nil {
		return
	}
	clone := types.CloneDebateLedger(ledger)
	if clone == nil {
		return
	}
	_ = tm.Append(types.TurnRecord{
		Turn:      types.LedgerSentinelTurn,
		AgentID:   types.LedgerAgentID,
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Ledger:    clone,
	})
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
		AgentID:   "moderator",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Content:   fmt.Sprintf("Begin deliberating on the following topic: %s", o.state.Topic),
		Control:   o.state.Control,
	}
	if err := o.transcript.Append(seed); err != nil {
		o.fail("error:", err)
	}
}

func (o *Orchestrator) checkTerminationConditions() {
	elapsed := float64(time.Now().UnixNano())/1e9 - o.state.StartTime

	if o.state.TimeLimit > 0 && elapsed >= float64(o.state.TimeLimit) {
		o.haltNoConsensus(fmt.Sprintf("time_limit (%ds)", o.state.TimeLimit))
		return
	}

	if o.checkConsensusCondition() {
		return
	}

	if o.state.Budget != nil && transcript.TotalCost(o.transcript.Records()) >= *o.state.Budget {
		o.haltNoConsensus(fmt.Sprintf("budget_exceeded ($%.2f)", *o.state.Budget))
		return
	}
}

func (o *Orchestrator) checkConsensusCondition() bool {
	if o.state.Control == nil || o.state.Control.Convergence.RequiredEndorsements <= 0 {
		return false
	}
	evaluation := o.state.Control.EvaluateConsensus(
		o.state.Control.Convergence.MinimumRounds,
		o.state.Control.DeliverablePresent(o.state.Control.Convergence.RequiredDeliverableItems),
	)
	if !evaluation.Ready {
		return false
	}
	o.haltConsensus(evaluation)
	return true
}

func (o *Orchestrator) haltConsensus(evaluation types.ConsensusEvaluation) {
	reason := fmt.Sprintf("consensus (proposal v%d)", evaluation.ProposalVersion)
	o.state.FinalConsensusStreak = len(evaluation.EndorsementAgentIDs)
	o.recordTerminal(types.OutcomeConsensus, evaluation.ProposalVersion, reason, evaluation)
}

func (o *Orchestrator) haltNoConsensus(reason string) {
	evaluation := types.ConsensusEvaluation{}
	if o.state.Control != nil {
		evaluation = o.state.Control.EvaluateConsensus(
			o.state.Control.Convergence.MinimumRounds,
			o.state.Control.DeliverablePresent(o.state.Control.Convergence.RequiredDeliverableItems),
		)
	}
	o.recordTerminal(types.OutcomeNoConsensus, evaluation.ProposalVersion, reason, evaluation)
}

func (o *Orchestrator) recordTerminal(kind types.TerminalOutcomeKind, proposalVersion int, reason string, evaluation types.ConsensusEvaluation) {
	if o.state.Control == nil || o.state.Control.Phase == types.PhaseTerminal {
		o.state.Running = false
		o.state.HaltedBy = reason
		return
	}

	terminal, err := cloneControlState(o.state.Control)
	if err != nil {
		o.fail("control_error:", err)
		return
	}
	terminal.Phase = types.PhaseTerminal
	terminal.Directive = types.TurnDirective{Kind: types.DirectiveNone}
	// The cloned control state carries the established run requirements. Do not
	// replace them with config or topic values while making the terminal record.
	terminal.Convergence.CurrentEndorsements = len(evaluation.EndorsementAgentIDs)
	terminal.Convergence.UnresolvedObjections = len(evaluation.UnresolvedObjectionIDs)
	terminal.Convergence.EvidenceGaps = len(evaluation.EvidenceGapClaimIDs)
	terminal.Outcome = types.TerminalOutcome{
		Kind:                   kind,
		ProposalVersion:        proposalVersion,
		Reason:                 reason,
		DissentingAgentIDs:     append([]string{}, evaluation.DissentingAgentIDs...),
		UnresolvedObjectionIDs: append([]string{}, evaluation.UnresolvedObjectionIDs...),
		EvidenceGapClaimIDs:    append([]string{}, evaluation.EvidenceGapClaimIDs...),
	}
	if err := terminal.Validate(); err != nil {
		o.fail("control_error:", err)
		return
	}
	if err := types.ValidateDeliberationTransition(o.state.Control, terminal); err != nil {
		o.fail("control_error:", err)
		return
	}
	if err := o.transcript.Append(types.TurnRecord{
		Turn:      -1,
		AgentID:   "moderator",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Control:   terminal,
	}); err != nil {
		o.fail("error:", err)
		return
	}
	o.state.Control = terminal
	o.state.Running = false
	o.state.HaltedBy = reason
}

func cloneControlState(state *types.DeliberationControlState) (*types.DeliberationControlState, error) {
	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("cloning terminal control state: %w", err)
	}
	var clone types.DeliberationControlState
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, fmt.Errorf("cloning terminal control state: %w", err)
	}
	return &clone, nil
}

func terminalHaltReason(outcome types.TerminalOutcome) string {
	if outcome.Reason != "" {
		return outcome.Reason
	}
	if outcome.Kind == types.OutcomeConsensus {
		return fmt.Sprintf("consensus (proposal v%d)", outcome.ProposalVersion)
	}
	if outcome.Kind == types.OutcomeNoConsensus {
		return "no_consensus"
	}
	return ""
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
	opening := o.state.Control != nil && o.state.Control.Phase == types.PhaseOpening
	if opening {
		history = nil
	}

	envelope := map[string]any{
		"topic":            o.state.Topic,
		"history":          history,
		"agent_id":         ag.ID,
		"cast_roster":      o.buildCastRoster(),
		"turn":             o.state.Turn,
		"round":            o.state.Turn/o.numAgents + 1,
		"remaining_budget": o.buildRemainingBudget(turnStart),
		"halting_rule":     o.buildHaltingRule(),
	}
	if o.state.Control != nil {
		controlState := any(o.state.Control)
		if opening {
			controlState = openingControlStateView(o.state.Control)
		}
		envelope["control_state"] = controlState
		envelope["directive"] = o.state.Control.Directive
		envelope["contribution_contract"] = ledger.ContributionContractForPhase(o.state.Control.Phase)
	}
	verifyTurn := o.state.Control != nil && o.state.Control.Directive.Kind == types.DirectiveVerify
	if o.sharedEvidence != nil && (!o.evidenceSent[ag.ID] || verifyTurn) {
		if verifyTurn {
			envelope["evidence"] = referencesOnlyEvidence(o.sharedEvidence)
		} else {
			envelope["evidence"] = o.sharedEvidence
		}
		o.evidenceSent[ag.ID] = true
	}
	if !opening && o.currentLedger != nil && ledgerEnabled(o.state.LedgerUpdateEnabled) {
		envelope["ledger"] = o.currentLedger
	}

	if o.state.FullContext && !opening {
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

	var cleanedContent string
	var nextControl *types.DeliberationControlState
	if o.state.Control != nil {
		var err error
		nextControl, err = ledger.ProcessContribution(o.state.Control, ag.ID, o.state.Turn, content)
		if err != nil {
			o.fail("contribution_error:", err)
			return types.TurnRecord{}, false
		}
		prepareNextTurn(nextControl, o.currentLedger, !directiveFulfilled(o.state.Control.Directive, nextControl))
		if err := types.ValidateDeliberationTransition(o.state.Control, nextControl); err != nil {
			o.fail("control_error:", err)
			return types.TurnRecord{}, false
		}
		cleanedContent = nextControl.Contributions[len(nextControl.Contributions)-1].Position
	} else {
		// Existing legacy records retain their persisted display fields. New
		// execution never parses response prose into a consensus signal.
		cleanedContent = content
	}

	var tokens types.TokenUsage
	var cost *float64
	if meta != nil {
		tokens = meta.Tokens
		cost = meta.Cost
	}

	return types.TurnRecord{
		Turn:      o.state.Turn,
		AgentID:   ag.ID,
		Model:     &ag.Model,
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Content:   cleanedContent,
		Control:   nextControl,
		Tokens:    tokens,
		Cost:      cost,
		Elapsed:   float64(time.Now().UnixNano())/1e9 - turnStart,
	}, true
}

func openingControlStateView(state *types.DeliberationControlState) map[string]any {
	return map[string]any{
		"protocol_version":       state.ProtocolVersion,
		"phase":                  state.Phase,
		"agent_ids":              append([]string(nil), state.AgentIDs...),
		"source_reference_count": state.SourceReferenceCount,
	}
}

func (o *Orchestrator) nextAgent() types.AgentConfig {
	target := ""
	if o.state.Control != nil {
		target = o.state.Control.Directive.TargetAgentID
		if target == "" {
			target = nextScheduledAgent(o.state.Control)
		}
	}
	if target != "" {
		for _, candidate := range o.state.Config.Agents {
			if candidate.ID == target {
				return candidate
			}
		}
	}
	return o.state.Config.Agents[o.state.Turn%o.numAgents]
}

func (o *Orchestrator) fail(prefix string, err error) {
	o.state.Running = false
	o.state.Failure = err
	o.state.HaltedBy = fmt.Sprintf("%s %v", prefix, err)
}

func (o *Orchestrator) buildCastRoster() []map[string]string {
	agents := o.state.Config.Agents
	roster := make([]map[string]string, 0, len(agents))
	for _, a := range agents {
		name := a.ID
		if a.Identity != nil && a.Identity.DisplayName != "" {
			name = a.Identity.DisplayName
		}
		roster = append(roster, map[string]string{
			"id":   a.ID,
			"name": name,
		})
	}
	return roster
}

func (o *Orchestrator) buildRemainingBudget(now float64) map[string]any {
	budget := map[string]any{}
	if o.state.MaxTurns > 0 {
		turnsRemaining := o.state.MaxTurns - o.state.Turn
		budget["turns_remaining"] = turnsRemaining
		budget["rounds_remaining"] = turnsRemaining / o.numAgents
	}
	if o.state.TimeLimit > 0 {
		remaining := float64(o.state.TimeLimit) - (now - o.state.StartTime)
		budget["time_remaining_seconds"] = remaining
	}
	if o.state.MaxTurns == 0 && o.state.TimeLimit == 0 {
		budget["uncapped"] = true
	}
	if o.state.Budget != nil && *o.state.Budget > 0 {
		accumulated := transcript.TotalCost(o.transcript.Records())
		budget["budget_remaining"] = *o.state.Budget - accumulated
	}
	return budget
}

func (o *Orchestrator) buildHaltingRule() map[string]any {
	rule := map[string]any{
		"consensus_threshold": o.state.Config.ConsensusThreshold,
		"min_rounds":          o.state.Config.EffectiveMinRounds(),
		"max_turns":           o.state.MaxTurns,
		"time_limit_seconds":  o.state.TimeLimit,
	}
	if o.state.Budget != nil {
		rule["budget_cap"] = *o.state.Budget
	} else {
		rule["budget_cap"] = float64(0)
	}
	if o.state.Control != nil && o.state.Control.Convergence.RequiredDeliverableItems > 0 {
		rule["deliverable_gate"] = map[string]any{
			"min_items": o.state.Control.Convergence.RequiredDeliverableItems,
		}
	}
	return rule
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

func ledgerEnabled(v *bool) bool {
	if v == nil {
		return true
	}
	return *v
}

func referencesOnlyEvidence(bundle *types.EvidenceBundle) *types.EvidenceBundle {
	if bundle == nil {
		return nil
	}
	return &types.EvidenceBundle{
		Summary:          bundle.Summary,
		SourceReferences: append([]types.SourceReference{}, bundle.SourceReferences...),
	}
}
