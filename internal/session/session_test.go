package session_test

import (
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
