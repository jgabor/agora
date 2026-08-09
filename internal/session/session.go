// Package session runs the deliberation pipeline from resolved config through
// orchestrator execution, for fresh runs and resumed transcripts.
package session

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/cast"
	"github.com/jgabor/agora/internal/evidence"
	"github.com/jgabor/agora/internal/ledger"
	"github.com/jgabor/agora/internal/orchestrator"
	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

// AutoCaps configures auto-level cap application. ExplicitTime and
// ExplicitMaxTurns reflect whether the caller overrode CLI defaults.
type AutoCaps struct {
	Caps             types.LevelCaps
	ExplicitTime     bool
	ExplicitMaxTurns bool
}

// RunRequest configures a fresh deliberation session.
type RunRequest struct {
	Topic        string
	Config       *types.DeliberationConfig
	Workdir      string
	OutputPath   string
	Window       int
	MaxTurns     int
	TimeLimit    int
	Budget       *float64
	FullContext  bool
	DryRun       bool
	Evidence     types.EvidenceRequest
	Ledger       *bool
	Synthesize   bool
	Auto         *AutoCaps
	TranscriptID int
}

// ResumeRequest configures continuing from an existing transcript.
type ResumeRequest struct {
	RunRequest
	SourceRecords []types.TurnRecord
}

// Hooks wires orchestrator lifecycle callbacks and the deliberation header.
type Hooks struct {
	OnTurn     orchestrator.TurnFunc
	OnEvidence orchestrator.EvidenceFunc
	OnActivity orchestrator.ActivityFunc
	OnHeader   func(*types.DeliberationState)
}

// Result reports deliberation outcomes after a session completes.
type Result struct {
	Stats      types.DeliberationStats
	Records    []types.TurnRecord
	OutputPath string
	HaltedBy   string
	Failure    error
	Synthesis  map[string]any
	State      *types.DeliberationState
}

// Run executes a fresh deliberation session.
func Run(req RunRequest, hooks Hooks) (Result, error) {
	state := buildState(req, 0)
	if req.Auto != nil {
		ApplyAutoCaps(state, *req.Auto, 0)
	}

	tm, err := prepareFreshTranscript(req)
	if err != nil {
		return Result{}, err
	}

	return execute(state, tm, req.Workdir, req.OutputPath, req.DryRun, req.Synthesize, req.Evidence, hooks)
}

// Resume continues deliberation from existing transcript records.
func Resume(req ResumeRequest, hooks Hooks) (Result, error) {
	if len(req.SourceRecords) == 0 {
		return Result{}, fmt.Errorf("no existing transcript found — use 'agora run' to start")
	}
	normalizedRecords, protocol, err := normalizeResumeRecords(req.SourceRecords)
	if err != nil {
		return Result{}, err
	}
	req.SourceRecords = normalizedRecords

	existingTurns := countAgentTurns(req.SourceRecords)
	state := buildState(req.RunRequest, existingTurns)
	var contractBoundary *types.DeliberationControlState
	if control, err := lastControlFromRecords(req.SourceRecords); err != nil {
		return Result{}, err
	} else if control != nil {
		state.Control = control
		if protocol.PreContractActive {
			contractBoundary, err = establishResumedRunContract(control, state)
			if err != nil {
				return Result{}, err
			}
			state.Control = contractBoundary
		}
	} else if protocol.PreContractActive {
		return Result{}, fmt.Errorf("loading source transcript: pre-contract active state is missing its control snapshot")
	}
	state.Evidence = types.EvidenceRequest{}
	if req.Auto != nil {
		ApplyAutoCaps(state, *req.Auto, existingTurns)
	}

	tm, err := prepareResumeTranscript(req)
	if err != nil {
		return Result{}, err
	}
	if contractBoundary != nil {
		if err := appendResumedRunContractBoundary(tm, contractBoundary); err != nil {
			return Result{}, err
		}
	}

	return execute(state, tm, req.Workdir, req.OutputPath, req.DryRun, req.Synthesize, types.EvidenceRequest{}, hooks)
}

// ApplyAutoCaps applies auto-level defaults to state. Explicit CLI limits win.
func ApplyAutoCaps(state *types.DeliberationState, caps AutoCaps, existingTurns int) {
	if !caps.ExplicitTime {
		state.TimeLimit = caps.Caps.TimeLimit
	}
	if caps.ExplicitMaxTurns {
		return
	}
	if caps.Caps.MaxTurns == 0 {
		state.MaxTurns = 0
		return
	}
	state.MaxTurns = existingTurns + caps.Caps.MaxTurns
}

func buildState(req RunRequest, existingTurns int) *types.DeliberationState {
	maxTurns := req.MaxTurns
	turn := 0
	if existingTurns > 0 {
		maxTurns = existingTurns + req.MaxTurns
		turn = existingTurns
	}
	agentIDs := make([]string, len(req.Config.Agents))
	for i := range req.Config.Agents {
		agentIDs[i] = req.Config.Agents[i].ID
	}
	control := types.NewDeliberationControlState(agentIDs, 0)
	control.Convergence.RequiredEndorsements = req.Config.ConsensusThreshold
	return &types.DeliberationState{
		Config:              req.Config,
		Control:             control,
		Topic:               req.Topic,
		Window:              req.Window,
		MaxTurns:            maxTurns,
		TimeLimit:           req.TimeLimit,
		Budget:              req.Budget,
		FullContext:         req.FullContext,
		Turn:                turn,
		Evidence:            req.Evidence,
		DeliverableGate:     orchestrator.ParseDeliverableGate(req.Topic),
		LedgerUpdateEnabled: req.Ledger,
	}
}

func prepareFreshTranscript(req RunRequest) (*transcript.TranscriptManager, error) {
	tm := transcript.NewTranscriptManager(req.OutputPath)
	c := cast.New(req.Config.Agents)
	meta := types.NewTranscriptMetadata(req.Config, c.Members())
	meta.ID = req.TranscriptID
	if meta.ID == 0 {
		meta.ID = newTranscriptID()
	}
	tm.SetMetadata(meta)
	return tm, nil
}

func prepareResumeTranscript(req ResumeRequest) (*transcript.TranscriptManager, error) {
	tm := transcript.NewTranscriptManager(req.OutputPath)
	meta := metadataFromRecords(req.SourceRecords)
	if meta == nil {
		c := cast.New(req.Config.Agents)
		meta = types.NewTranscriptMetadata(req.Config, c.Members())
		if req.TranscriptID != 0 {
			meta.ID = req.TranscriptID
		} else {
			meta.ID = newTranscriptID()
		}
	}
	tm.SetMetadata(meta)

	if _, err := tm.LoadExisting(); err != nil {
		return nil, fmt.Errorf("loading existing output transcript: %w", err)
	}

	for _, record := range req.SourceRecords {
		if err := tm.Append(record); err != nil {
			return nil, fmt.Errorf("copying records: %w", err)
		}
	}
	return tm, nil
}

func execute(
	state *types.DeliberationState,
	tm *transcript.TranscriptManager,
	workdir string,
	outputPath string,
	dryRun bool,
	synthesize bool,
	evidenceReq types.EvidenceRequest,
	hooks Hooks,
) (Result, error) {
	runner := agent.NewAgentRunnerAt(dryRun, workdir)
	orch := orchestrator.NewOrchestrator(state, tm, runner)
	orch.SetLedgerUpdater(ledger.NewUpdater(runner))
	if seed := lastLedgerFromRecords(tm.Records()); seed != nil {
		orch.SetCurrentLedger(seed)
	}
	if evidenceEnabled(evidenceReq) {
		orch.SetEvidenceCollector(evidence.NewPolicyCollector(runner))
	}
	persistedEvidence, err := transcript.EvidenceFromRecords(tm.Records())
	if err != nil {
		return Result{}, fmt.Errorf("loading persisted evidence: %w", err)
	}
	orch.SetSharedEvidence(persistedEvidence)
	orch.OnEvidence(hooks.OnEvidence)
	orch.OnTurn(hooks.OnTurn)
	orch.OnActivity(hooks.OnActivity)

	if hooks.OnHeader != nil {
		hooks.OnHeader(state)
	}

	stats := orch.Run()

	var synthesis map[string]any
	if synthesize && state.Failure == nil {
		synthesis = orch.Synthesize()
	}

	return Result{
		Stats:      stats,
		Records:    tm.Records(),
		OutputPath: outputPath,
		HaltedBy:   state.HaltedBy,
		Failure:    state.Failure,
		Synthesis:  synthesis,
		State:      state,
	}, nil
}

func evidenceEnabled(req types.EvidenceRequest) bool {
	return req.ResearchEnabled || len(req.ContextPaths) > 0
}

// lastLedgerFromRecords returns a deep-cloned copy of the most recent ledger
// snapshot carried by a transcript record, or nil when no record carries one.
// Resume uses it to seed the orchestrator's currentLedger so a resumed
// deliberation starts with continuity from the prior ledger without making an
// extra model call.
func lastLedgerFromRecords(records []types.TurnRecord) *types.DebateLedger {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Ledger != nil {
			return types.CloneDebateLedger(records[i].Ledger)
		}
	}
	return nil
}

func lastControlFromRecords(records []types.TurnRecord) (*types.DeliberationControlState, error) {
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].Control != nil {
			return cloneControlState(records[i].Control)
		}
	}
	return nil, nil
}

func cloneControlState(control *types.DeliberationControlState) (*types.DeliberationControlState, error) {
	data, err := json.Marshal(control)
	if err != nil {
		return nil, fmt.Errorf("cloning resumed control state: %w", err)
	}
	var clone types.DeliberationControlState
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, fmt.Errorf("cloning resumed control state: %w", err)
	}
	return &clone, nil
}

func establishResumedRunContract(previous *types.DeliberationControlState, state *types.DeliberationState) (*types.DeliberationControlState, error) {
	if previous == nil || !previous.IsPreContractActive() || state == nil || state.Config == nil {
		return nil, fmt.Errorf("pre-contract run contract requires an active control state and resume configuration")
	}
	boundary, err := cloneControlState(previous)
	if err != nil {
		return nil, err
	}
	if boundary.Contributions == nil {
		boundary.Contributions = []types.AgentContribution{}
	}
	if boundary.Directive.Kind == "" {
		boundary.Directive = types.TurnDirective{Kind: types.DirectiveNone}
	}
	boundary.Convergence.RunContractVersion = types.RunContractVersion
	boundary.Convergence.RequiredEndorsements = state.Config.ConsensusThreshold
	boundary.Convergence.MinimumRounds = state.Config.EffectiveMinRounds()
	if state.DeliverableGate != nil {
		boundary.Convergence.RequiredDeliverableItems = state.DeliverableGate.MinItems
	} else {
		boundary.Convergence.RequiredDeliverableItems = 0
	}
	if err := types.ValidateDeliberationTransition(previous, boundary); err != nil {
		return nil, fmt.Errorf("establishing pre-contract run contract: %w", err)
	}
	return boundary, nil
}

func appendResumedRunContractBoundary(tm *transcript.TranscriptManager, control *types.DeliberationControlState) error {
	snapshot, err := cloneControlState(control)
	if err != nil {
		return err
	}
	if err := tm.Append(types.TurnRecord{
		Turn:      -1,
		AgentID:   "moderator",
		Timestamp: float64(time.Now().UnixNano()) / 1e9,
		Control:   snapshot,
	}); err != nil {
		return fmt.Errorf("persisting pre-contract run contract boundary: %w", err)
	}
	return nil
}

func normalizeResumeRecords(records []types.TurnRecord) ([]types.TurnRecord, transcript.ProtocolInfo, error) {
	data, err := json.Marshal(records)
	if err != nil {
		return nil, transcript.ProtocolInfo{}, fmt.Errorf("cloning resume records: %w", err)
	}
	var clone []types.TurnRecord
	if err := json.Unmarshal(data, &clone); err != nil {
		return nil, transcript.ProtocolInfo{}, fmt.Errorf("cloning resume records: %w", err)
	}
	info, err := transcript.ProtocolFromRecords(clone)
	if err != nil {
		return nil, transcript.ProtocolInfo{}, fmt.Errorf("loading source transcript: %w", err)
	}
	return clone, info, nil
}

func countAgentTurns(records []types.TurnRecord) int {
	return types.AgentTurnCount(records)
}

func metadataFromRecords(records []types.TurnRecord) *types.TranscriptMetadata {
	for _, record := range records {
		if record.Transcript != nil {
			return record.Transcript
		}
	}
	return nil
}

func newTranscriptID() int {
	return int(time.Now().UnixMilli())*1000 + rand.Intn(1000)
}
