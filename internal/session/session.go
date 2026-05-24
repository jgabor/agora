// Package session runs the deliberation pipeline from resolved config through
// orchestrator execution, for fresh runs and resumed transcripts.
package session

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/cast"
	"github.com/jgabor/agora/internal/evidence"
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
	OutputPath   string
	Window       int
	MaxTurns     int
	TimeLimit    int
	Budget       *float64
	FullContext  bool
	DryRun       bool
	Evidence     types.EvidenceRequest
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

	return execute(state, tm, req.OutputPath, req.DryRun, req.Synthesize, req.Evidence, hooks)
}

// Resume continues deliberation from existing transcript records.
func Resume(req ResumeRequest, hooks Hooks) (Result, error) {
	if len(req.SourceRecords) == 0 {
		return Result{}, fmt.Errorf("no existing transcript found — use 'agora run' to start")
	}

	existingTurns := countAgentTurns(req.SourceRecords)
	state := buildState(req.RunRequest, existingTurns)
	state.Evidence = types.EvidenceRequest{}
	if req.Auto != nil {
		ApplyAutoCaps(state, *req.Auto, existingTurns)
	}

	tm, err := prepareResumeTranscript(req)
	if err != nil {
		return Result{}, err
	}

	return execute(state, tm, req.OutputPath, req.DryRun, req.Synthesize, types.EvidenceRequest{}, hooks)
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
	return &types.DeliberationState{
		Config:      req.Config,
		Topic:       req.Topic,
		Window:      req.Window,
		MaxTurns:    maxTurns,
		TimeLimit:   req.TimeLimit,
		Budget:      req.Budget,
		FullContext: req.FullContext,
		Turn:        turn,
		Evidence:    req.Evidence,
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
	outputPath string,
	dryRun bool,
	synthesize bool,
	evidenceReq types.EvidenceRequest,
	hooks Hooks,
) (Result, error) {
	runner := agent.NewAgentRunner(dryRun)
	orch := orchestrator.NewOrchestrator(state, tm, runner)
	if evidenceEnabled(evidenceReq) {
		orch.SetEvidenceCollector(evidence.NewPolicyCollector(runner))
	}
	orch.OnEvidence(hooks.OnEvidence)
	orch.OnTurn(hooks.OnTurn)
	orch.OnActivity(hooks.OnActivity)

	if hooks.OnHeader != nil {
		hooks.OnHeader(state)
	}

	stats := orch.Run()

	var synthesis map[string]any
	if synthesize {
		synthesis = orch.Synthesize()
	}

	return Result{
		Stats:      stats,
		Records:    tm.Records(),
		OutputPath: outputPath,
		HaltedBy:   state.HaltedBy,
		Synthesis:  synthesis,
		State:      state,
	}, nil
}

func evidenceEnabled(req types.EvidenceRequest) bool {
	return req.ResearchEnabled || len(req.ContextPaths) > 0
}

func countAgentTurns(records []types.TurnRecord) int {
	count := 0
	for _, record := range records {
		if record.AgentID != "orchestrator" {
			count++
		}
	}
	return count
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
