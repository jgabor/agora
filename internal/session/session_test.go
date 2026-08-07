package session_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/jgabor/agora/internal/cast"
	"github.com/jgabor/agora/internal/session"
	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

func TestApplyAutoCapsKeepsExplicitRunLimits(t *testing.T) {
	state := &types.DeliberationState{TimeLimit: 1200, MaxTurns: 50}
	caps := session.AutoCaps{
		Caps:             types.CapsForLevel(types.AutoDeep),
		ExplicitTime:     true,
		ExplicitMaxTurns: true,
	}

	session.ApplyAutoCaps(state, caps, 0)

	if state.TimeLimit != 1200 || state.MaxTurns != 50 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want explicit time=1200 maxTurns=50", state.TimeLimit, state.MaxTurns)
	}
}

func TestApplyAutoCapsAppliesRunDefaults(t *testing.T) {
	state := &types.DeliberationState{TimeLimit: 60, MaxTurns: 10}
	caps := session.AutoCaps{Caps: types.CapsForLevel(types.AutoDeep)}

	session.ApplyAutoCaps(state, caps, 0)

	if state.TimeLimit != 900 || state.MaxTurns != 20 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want deep defaults time=900 maxTurns=20", state.TimeLimit, state.MaxTurns)
	}
}

func TestApplyAutoCapsKeepsExplicitResumeLimits(t *testing.T) {
	state := &types.DeliberationState{TimeLimit: 1200, MaxTurns: 57}
	caps := session.AutoCaps{
		Caps:             types.CapsForLevel(types.AutoDeep),
		ExplicitTime:     true,
		ExplicitMaxTurns: true,
	}

	session.ApplyAutoCaps(state, caps, 7)

	if state.TimeLimit != 1200 || state.MaxTurns != 57 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want explicit resume time=1200 maxTurns=57", state.TimeLimit, state.MaxTurns)
	}
}

func TestApplyAutoCapsAddsResumeDefaultsToExistingTurns(t *testing.T) {
	state := &types.DeliberationState{TimeLimit: 60, MaxTurns: 17}
	caps := session.AutoCaps{Caps: types.CapsForLevel(types.AutoDeep)}

	session.ApplyAutoCaps(state, caps, 7)

	if state.TimeLimit != 900 || state.MaxTurns != 27 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want deep resume defaults time=900 maxTurns=27", state.TimeLimit, state.MaxTurns)
	}
}

func TestApplyAutoCapsPreservesYOLOUnlimitedDefault(t *testing.T) {
	state := &types.DeliberationState{TimeLimit: 60, MaxTurns: 17}
	caps := session.AutoCaps{Caps: types.CapsForLevel(types.AutoYOLO)}

	session.ApplyAutoCaps(state, caps, 7)

	if state.TimeLimit != 0 || state.MaxTurns != 0 {
		t.Fatalf("state limits: got time=%d maxTurns=%d, want yolo unlimited defaults", state.TimeLimit, state.MaxTurns)
	}
}

func TestRunCreatesVersionedDeliberationControlState(t *testing.T) {
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents:   []types.AgentConfig{{ID: "alpha", Model: "test/model"}},
	}
	result, err := session.Run(session.RunRequest{
		Topic:      "protocol substrate",
		Config:     cfg,
		OutputPath: t.TempDir() + "/run.jsonl",
		MaxTurns:   1,
		TimeLimit:  60,
		DryRun:     true,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.State.Control == nil {
		t.Fatal("run state is missing deliberation control state")
	}
	if err := result.State.Control.Validate(); err != nil {
		t.Fatalf("run control state: %v", err)
	}
	if result.State.Control.ProtocolVersion != types.DeliberationProtocolVersion {
		t.Fatalf("protocol version: got %q", result.State.Control.ProtocolVersion)
	}
	if len(result.State.Control.Contributions) != 1 {
		t.Fatalf("contributions: got %d, want one accepted dry-run contribution", len(result.State.Control.Contributions))
	}
	contribution := result.State.Control.Contributions[0]
	if contribution.AgentID != "alpha" || contribution.Turn != 0 || contribution.Position == "" {
		t.Fatalf("bound dry-run contribution: %+v", contribution)
	}
	if info, err := transcript.ProtocolFromRecords(result.Records); err != nil || info.Legacy || info.Version != types.DeliberationProtocolVersion {
		t.Fatalf("typed transcript protocol: info=%+v err=%v", info, err)
	}
}

func TestResumePreservesSourceMetadata(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "/resume.jsonl"
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents:   []types.AgentConfig{{ID: "a", Model: model}},
	}
	sourceMeta := types.NewTranscriptMetadata(cfg, cast.New(cfg.Agents).Members())
	sourceMeta.ID = 4242

	req := session.ResumeRequest{
		RunRequest: session.RunRequest{
			Topic:      "resume topic",
			Config:     cfg,
			OutputPath: outputPath,
			Window:     2,
			MaxTurns:   1,
			TimeLimit:  60,
			DryRun:     true,
		},
		SourceRecords: []types.TurnRecord{{
			Turn:       0,
			AgentID:    "a",
			Model:      &model,
			Content:    "prior",
			Transcript: sourceMeta,
		}},
	}

	result, err := session.Resume(req, session.Hooks{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.Stats.TotalTurns < 2 {
		t.Fatalf("total turns: got %d, want at least 2 (prior + resumed)", result.Stats.TotalTurns)
	}

	records := readTranscript(t, outputPath)
	if len(records) == 0 || records[0].Transcript == nil {
		t.Fatal("expected preserved transcript metadata on first record")
	}
	if records[0].Transcript.ID != 4242 {
		t.Fatalf("metadata ID: got %d, want preserved ID 4242", records[0].Transcript.ID)
	}
}

func TestResumeHydratesPersistedEvidenceReferences(t *testing.T) {
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents:   []types.AgentConfig{{ID: "alpha", Model: model}},
	}
	control := types.NewDeliberationControlState([]string{"alpha"}, 1)
	control.Phase = types.PhaseVoting
	control.CurrentProposalVersion = 1
	control.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "proposal"}}
	if err := control.Validate(); err != nil {
		t.Fatalf("source control: %v", err)
	}
	source := []types.TurnRecord{
		{Turn: -2, AgentID: "moderator", Content: "persisted evidence", Evidence: &types.EvidenceBundle{
			Summary: "one source", SourceReferences: []types.SourceReference{{Title: "source", URL: "https://example.test/source"}},
		}},
		{Turn: 0, AgentID: "alpha", Model: &model, Content: "prior", Control: control},
	}

	result, err := session.Resume(session.ResumeRequest{
		RunRequest: session.RunRequest{
			Topic: "resume evidence", Config: cfg, OutputPath: t.TempDir() + "/resume.jsonl",
			MaxTurns: 1, TimeLimit: 60, DryRun: true,
		},
		SourceRecords: source,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.State.SharedEvidence == nil || len(result.State.SharedEvidence.SourceReferences) != 1 || result.State.SharedEvidence.ContextDocuments != nil {
		t.Fatalf("resumed evidence hydration: %#v", result.State.SharedEvidence)
	}
}

func TestResumeMigratesPersistedTypedV1BeforeExecution(t *testing.T) {
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents:   []types.AgentConfig{{ID: "alpha", Model: model}},
	}
	legacy := types.NewDeliberationControlState([]string{"alpha"}, 1)
	legacy.ProtocolVersion = types.LegacyDeliberationProtocolVersion
	legacy.Phase = types.PhaseVoting
	legacy.CurrentProposalVersion = 1
	legacy.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "legacy proposal"}}
	legacy.Claims = []types.ClaimEvidence{{
		ID: "legacy-claim", AgentID: "alpha", ProposalVersion: 1, Kind: types.ClaimFact,
		Decisive: true, Status: types.EvidenceVerified, SourceRefs: []int{0},
	}}
	rawPath := filepath.Join(t.TempDir(), "legacy-v1.jsonl")
	raw := types.TurnRecord{Turn: 0, AgentID: "alpha", Model: &model, Content: "legacy", Control: legacy}
	data, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshal legacy record: %v", err)
	}
	if err := os.WriteFile(rawPath, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write legacy transcript: %v", err)
	}
	source, err := transcript.LoadFileLenient(rawPath, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("load legacy transcript for resume: %v", err)
	}

	result, err := session.Resume(session.ResumeRequest{
		RunRequest: session.RunRequest{
			Topic: "legacy v1 resume", Config: cfg, OutputPath: t.TempDir() + "/resume.jsonl",
			MaxTurns: 1, TimeLimit: 60, DryRun: true,
		},
		SourceRecords: source,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("legacy resume: %v", err)
	}
	if result.State.Control.ProtocolVersion != types.DeliberationProtocolVersion || result.State.Control.SourceReferenceCount != 0 {
		t.Fatalf("legacy resume control: %#v", result.State.Control)
	}
}

func TestRunDryRunProducesTurns(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "/run.jsonl"
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents: []types.AgentConfig{
			{ID: "a", Model: model},
			{ID: "b", Model: model},
		},
	}

	result, err := session.Run(session.RunRequest{
		Topic:      "dry topic",
		Config:     cfg,
		OutputPath: outputPath,
		Window:     2,
		MaxTurns:   2,
		TimeLimit:  60,
		DryRun:     true,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Stats.TotalTurns < 2 {
		t.Fatalf("total turns: got %d, want at least 2", result.Stats.TotalTurns)
	}
}

func TestResumeEmptySourceFails(t *testing.T) {
	_, err := session.Resume(session.ResumeRequest{
		RunRequest: session.RunRequest{
			Topic:      "topic",
			Config:     &types.DeliberationConfig{Agents: []types.AgentConfig{{ID: "a", Model: "m"}}},
			OutputPath: t.TempDir() + "/out.jsonl",
		},
	}, session.Hooks{})
	if err == nil {
		t.Fatal("expected error for empty source records")
	}
}

func readTranscript(t *testing.T, path string) []types.TurnRecord {
	t.Helper()
	records, err := transcript.LoadFileStrict(path)
	if err != nil {
		t.Fatalf("load transcript: %v", err)
	}
	return records
}

func TestResumeSeedsLedgerFromSourceAndContinues(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "/resume.jsonl"
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents:   []types.AgentConfig{{ID: "a", Model: model}, {ID: "b", Model: model}},
	}
	sourceLedger := types.NewDebateLedger(1, 1715000000.0)
	sourceLedger.Positions = []types.AgentPosition{
		{AgentID: "a", Text: "prior position a", Turn: 0},
		{AgentID: "b", Text: "prior position b", Turn: 1},
	}
	sourceRecords := []types.TurnRecord{
		{Turn: -1, AgentID: "moderator", Content: "seed"},
		{Turn: 0, AgentID: "a", Model: &model, Content: "prior turn a"},
		{Turn: 1, AgentID: "b", Model: &model, Content: "prior turn b"},
		{Turn: types.LedgerSentinelTurn, AgentID: types.LedgerAgentID, Timestamp: 1715000005.0, Ledger: sourceLedger},
	}

	result, err := session.Resume(session.ResumeRequest{
		RunRequest: session.RunRequest{
			Topic:      "resume topic",
			Config:     cfg,
			OutputPath: outputPath,
			Window:     2,
			MaxTurns:   2,
			TimeLimit:  60,
			DryRun:     true,
		},
		SourceRecords: sourceRecords,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.Stats.TotalTurns < 4 {
		t.Fatalf("total turns: got %d, want at least 4 (2 prior + 2 resumed)", result.Stats.TotalTurns)
	}

	loaded := readTranscript(t, outputPath)
	var sourceLedgerCopied, resumedLedger *types.TurnRecord
	for i := range loaded {
		if loaded[i].AgentID == types.LedgerAgentID {
			if sourceLedgerCopied == nil {
				sourceLedgerCopied = &loaded[i]
			} else {
				resumedLedger = &loaded[i]
			}
		}
	}
	if sourceLedgerCopied == nil {
		t.Fatal("source ledger record should be copied into the resumed transcript")
	}
	if sourceLedgerCopied.Ledger == nil || sourceLedgerCopied.Ledger.Round != 1 {
		t.Fatalf("copied source ledger: %#v, want round=1", sourceLedgerCopied.Ledger)
	}
	if resumedLedger == nil {
		t.Fatal("resumed run should persist a fresh ledger after its first completed round, proving the seed fed continuity")
	}
	if resumedLedger.Ledger == nil {
		t.Fatalf("resumed ledger record missing payload: %#v", resumedLedger)
	}
}

func TestResumeLegacySourceNoLedgerInjection(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "/resume.jsonl"
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents:   []types.AgentConfig{{ID: "a", Model: model}, {ID: "b", Model: model}},
	}
	disabled := false
	sourceRecords := []types.TurnRecord{
		{Turn: -1, AgentID: "moderator", Content: "legacy seed"},
		{Turn: 0, AgentID: "a", Model: &model, Content: "legacy turn a"},
		{Turn: 1, AgentID: "b", Model: &model, Content: "legacy turn b"},
	}

	_, err := session.Resume(session.ResumeRequest{
		RunRequest: session.RunRequest{
			Topic:      "legacy resume topic",
			Config:     cfg,
			OutputPath: outputPath,
			Window:     2,
			MaxTurns:   2,
			TimeLimit:  60,
			DryRun:     true,
			Ledger:     &disabled,
		},
		SourceRecords: sourceRecords,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("legacy resume should not fail: %v", err)
	}

	loaded := readTranscript(t, outputPath)
	for i, r := range loaded {
		if r.AgentID == types.LedgerAgentID || r.Turn == types.LedgerSentinelTurn || r.Ledger != nil {
			t.Fatalf("legacy resume must not inject ledger records; found at index %d: %#v", i, r)
		}
	}
}

func TestResumePreservesPhaseAndOutstandingDirective(t *testing.T) {
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents:   []types.AgentConfig{{ID: "alpha", Model: model}, {ID: "beta", Model: model}},
	}
	control := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
	control.Phase = types.PhaseVoting
	control.CurrentProposalVersion = 1
	control.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "candidate"}}
	control.Directive = types.TurnDirective{Kind: types.DirectiveVote, TargetAgentID: "beta", ProposalVersion: 1}
	if err := control.Validate(); err != nil {
		t.Fatalf("source control: %v", err)
	}
	source := []types.TurnRecord{{Turn: 0, AgentID: "alpha", Model: &model, Content: "prior", Control: control}}
	wantDirective := control.Directive

	result, err := session.Resume(session.ResumeRequest{
		RunRequest: session.RunRequest{
			Topic: "resume phase state", Config: cfg, OutputPath: t.TempDir() + "/resume.jsonl",
			MaxTurns: 1, TimeLimit: 60, DryRun: true,
		},
		SourceRecords: source,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(result.Records) < 2 || result.Records[1].AgentID != "beta" {
		t.Fatalf("resumed directed turn: %#v", result.Records)
	}
	if result.State.Control.Phase != types.PhaseVoting {
		t.Fatalf("resumed phase: got %s, want voting", result.State.Control.Phase)
	}
	if source[0].Control.Phase != types.PhaseVoting || !reflect.DeepEqual(source[0].Control.Directive, wantDirective) {
		t.Fatalf("resume mutated source control: %#v", source[0].Control)
	}
}
