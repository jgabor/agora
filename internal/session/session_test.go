package session_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestResumeContinuesPartialPositionOnlyOpening(t *testing.T) {
	dir := t.TempDir()
	outputPath := dir + "/partial-opening-resume.jsonl"
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents: []types.AgentConfig{
			{ID: "alpha", Model: model},
			{ID: "beta", Model: model},
			{ID: "gamma", Model: model},
		},
	}
	control := types.NewDeliberationControlState([]string{"alpha", "beta", "gamma"}, 0)
	control.Contributions = []types.AgentContribution{{
		AgentID: "alpha", Turn: 0, Position: "alpha independent opening",
		ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone},
	}}
	if err := control.Validate(); err != nil {
		t.Fatalf("partial opening control: %v", err)
	}
	beforeJSON, err := json.Marshal(control)
	if err != nil {
		t.Fatalf("marshal partial opening control: %v", err)
	}
	var before types.DeliberationControlState
	if err := json.Unmarshal(beforeJSON, &before); err != nil {
		t.Fatalf("unmarshal partial opening control: %v", err)
	}
	source := []types.TurnRecord{{
		Turn: 0, AgentID: "alpha", Model: &model, Content: "alpha independent opening", Control: control,
	}}

	result, err := session.Resume(session.ResumeRequest{
		RunRequest: session.RunRequest{
			Topic: "resume partial opening", Config: cfg, OutputPath: outputPath,
			MaxTurns: 2, TimeLimit: 60, DryRun: true,
		},
		SourceRecords: source,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if result.Failure != nil {
		t.Fatalf("resume failure: %v", result.Failure)
	}
	if !reflect.DeepEqual(source[0].Control, &before) {
		t.Fatalf("resume mutated partial source control: got %#v, want %#v", source[0].Control, before)
	}

	gotAgents := make([]string, 0, 3)
	var betaControl, gammaControl *types.DeliberationControlState
	for i := range result.Records {
		record := result.Records[i]
		if record.Turn < 0 {
			continue
		}
		gotAgents = append(gotAgents, record.AgentID)
		switch record.AgentID {
		case "beta":
			betaControl = record.Control
		case "gamma":
			gammaControl = record.Control
		}
	}
	if want := []string{"alpha", "beta", "gamma"}; !reflect.DeepEqual(gotAgents, want) {
		t.Fatalf("resumed opening schedule: got %v, want %v", gotAgents, want)
	}
	if betaControl == nil || betaControl.Phase != types.PhaseOpening {
		t.Fatalf("beta did not remain in opening: %#v", betaControl)
	}
	if gammaControl == nil || gammaControl.Phase != types.PhaseRebuttal {
		t.Fatalf("gamma did not complete opening into rebuttal: %#v", gammaControl)
	}
	if gammaControl.CurrentProposalVersion != 0 || len(gammaControl.Proposals) != 0 || len(gammaControl.Contributions) != 3 {
		t.Fatalf("resumed opening canonical state: %#v", gammaControl)
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

func TestResumeEstablishesContractForHistoricalTypedActiveSnapshots(t *testing.T) {
	tests := []struct {
		name                 string
		protocolVersion      string
		includeContributions bool
	}{
		{name: "typed v1", protocolVersion: types.LegacyDeliberationProtocolVersion},
		{name: "early typed v2", protocolVersion: types.DeliberationProtocolVersion, includeContributions: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			source := historicalTypedActiveSource(t, tt.protocolVersion, tt.includeContributions)
			source[0].Transcript = &types.TranscriptMetadata{
				SchemaVersion: 1,
				Config:        &types.DeliberationConfig{ConsensusThreshold: 99, MinRounds: 99},
			}
			cfg := &types.DeliberationConfig{
				Topology:           types.TopologyRing,
				ConsensusThreshold: 1,
				MinRounds:          1,
				Agents:             []types.AgentConfig{{ID: "alpha", Model: "test/model"}},
			}
			outputPath := filepath.Join(t.TempDir(), "resume.jsonl")
			result, err := session.Resume(session.ResumeRequest{
				RunRequest: session.RunRequest{
					Topic:  "The final output must contain exactly three laws",
					Config: cfg, OutputPath: outputPath, MaxTurns: 1, TimeLimit: 60, DryRun: true,
				},
				SourceRecords: source,
			}, session.Hooks{})
			if err != nil {
				t.Fatalf("Resume: %v", err)
			}
			if result.Failure != nil || result.State.Control.Outcome.Kind != types.OutcomeConsensus {
				t.Fatalf("historical active resume did not reach consensus: failure=%v control=%#v", result.Failure, result.State.Control)
			}

			boundary, terminal := -1, -1
			var boundaryControl *types.DeliberationControlState
			for i, record := range result.Records {
				if record.Control == nil {
					continue
				}
				if record.Control.Phase == types.PhaseTerminal {
					terminal = i
					continue
				}
				if boundary < 0 && record.AgentID == "moderator" && record.Turn == -1 &&
					record.Control.Convergence.RequiredEndorsements == 1 &&
					record.Control.Convergence.MinimumRounds == 1 &&
					record.Control.Convergence.RequiredDeliverableItems == 3 &&
					record.Control.Convergence.RunContractVersion == types.RunContractVersion {
					boundary = i
					boundaryControl = record.Control
				}
			}
			if boundary < 0 || terminal < 0 || boundary >= terminal {
				t.Fatalf("normalized active contract must precede terminal outcome: boundary=%d terminal=%d records=%#v", boundary, terminal, result.Records)
			}
			if tt.protocolVersion == types.LegacyDeliberationProtocolVersion {
				if result.Records[0].Control.Contributions != nil {
					t.Fatalf("historical source contributions changed: %#v", result.Records[0].Control.Contributions)
				}
				if boundaryControl.Contributions == nil {
					t.Fatalf("run contract boundary must normalize nil historical contributions")
				}
			}
			persisted, err := transcript.LoadFileStrict(outputPath)
			if err != nil {
				t.Fatalf("normalized resumed transcript must strictly load: %v", err)
			}
			info, err := transcript.ProtocolFromRecords(persisted)
			if err != nil || info.PreContractActive {
				t.Fatalf("normalized resumed transcript must retain an established contract: info=%#v err=%v", info, err)
			}
		})
	}
}

func historicalTypedActiveSource(t *testing.T, protocolVersion string, includeContributions bool) []types.TurnRecord {
	t.Helper()
	proposal := "1. An agent must verify claims.\n2. An agent must preserve evidence.\n3. An agent must record dissent."
	control := map[string]any{
		"protocol_version":         protocolVersion,
		"phase":                    "voting",
		"agent_ids":                []string{"alpha"},
		"source_reference_count":   0,
		"current_proposal_version": 1,
		"proposals": []any{map[string]any{
			"version": 1, "author_id": "alpha", "content": proposal, "supersedes": 0,
		}},
		"objections":   []any{},
		"dispositions": []any{},
		"votes": []any{map[string]any{
			"agent_id": "alpha", "proposal_version": 1, "choice": "endorse",
		}},
		"claims": []any{},
		"moderator_action": map[string]any{
			"kind": "none", "objection_ids": []any{}, "claim_ids": []any{},
		},
		"convergence": map[string]any{
			"current_endorsements": 0, "required_endorsements": 1,
			"unresolved_objections": 0, "evidence_gaps": 0, "stagnant_rounds": 0, "ready_to_vote": false,
		},
		"outcome": map[string]any{
			"kind": "pending", "dissenting_agent_ids": []any{}, "unresolved_objection_ids": []any{}, "evidence_gap_claim_ids": []any{},
		},
	}
	if protocolVersion == types.DeliberationProtocolVersion {
		control["directive"] = map[string]any{"kind": "none"}
		control["contributions"] = []any{}
		if includeContributions {
			control["contributions"] = []any{map[string]any{
				"agent_id": "alpha", "turn": 0, "position": "historical position",
				"responses": []any{}, "concessions": []any{}, "proposal_action": map[string]any{"kind": "none"}, "objections": []any{}, "claims": []any{},
			}}
		}
	}
	record := map[string]any{
		"turn": 0, "agent_id": "alpha", "model": "test/model", "timestamp": 1, "content": "historical typed active", "tokens": map[string]any{},
		"consensus": false, "consensus_statement": "", "elapsed": 0, "control": control,
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatalf("marshal historical record: %v", err)
	}
	if strings.Contains(string(data), "minimum_rounds") || strings.Contains(string(data), "required_deliverable_items") {
		t.Fatalf("historical fixture unexpectedly contains current requirement fields: %s", data)
	}
	path := filepath.Join(t.TempDir(), "historical.jsonl")
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		t.Fatalf("write historical fixture: %v", err)
	}
	records, err := transcript.LoadFileLenient(path, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("load historical fixture: %v", err)
	}
	return records
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
	if len(result.Records) < 3 || result.Records[1].AgentID != "moderator" || result.Records[1].Control == nil ||
		result.Records[1].Control.Convergence.RunContractVersion != types.RunContractVersion || result.Records[2].AgentID != "beta" {
		t.Fatalf("resumed directed turn: %#v", result.Records)
	}
	if result.State.Control.Phase != types.PhaseTerminal || result.State.Control.Outcome.Kind != types.OutcomeNoConsensus {
		t.Fatalf("resumed terminal state: phase=%s outcome=%#v", result.State.Control.Phase, result.State.Control.Outcome)
	}
	if source[0].Control.Phase != types.PhaseVoting || !reflect.DeepEqual(source[0].Control.Directive, wantDirective) {
		t.Fatalf("resume mutated source control: %#v", source[0].Control)
	}
}

func TestResumePreservesTypedTerminalOutcome(t *testing.T) {
	model := "test/model"
	cfg := &types.DeliberationConfig{
		Topology: types.TopologyRing,
		Agents:   []types.AgentConfig{{ID: "alpha", Model: model}},
	}
	control := types.NewDeliberationControlState([]string{"alpha"}, 0)
	control.Phase = types.PhaseTerminal
	control.CurrentProposalVersion = 1
	control.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "candidate"}}
	control.Outcome = types.TerminalOutcome{
		Kind:                   types.OutcomeNoConsensus,
		ProposalVersion:        1,
		Reason:                 "budget_exceeded ($0.01)",
		DissentingAgentIDs:     []string{"alpha"},
		UnresolvedObjectionIDs: []string{},
		EvidenceGapClaimIDs:    []string{},
	}
	if err := control.Validate(); err != nil {
		t.Fatalf("terminal source control: %v", err)
	}
	source := []types.TurnRecord{{Turn: -1, AgentID: "moderator", Model: &model, Control: control}}
	result, err := session.Resume(session.ResumeRequest{
		RunRequest: session.RunRequest{
			Topic: "terminal resume", Config: cfg, OutputPath: t.TempDir() + "/resume.jsonl",
			MaxTurns: 1, TimeLimit: 60, DryRun: true,
		},
		SourceRecords: source,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(result.Records) != 1 || result.State.Control.Phase != types.PhaseTerminal ||
		!reflect.DeepEqual(result.State.Control.Outcome, control.Outcome) || result.HaltedBy != "budget_exceeded ($0.01)" {
		t.Fatalf("terminal resume changed outcome: records=%#v state=%#v halted=%q", result.Records, result.State.Control, result.HaltedBy)
	}
}

func TestResumeRejectsUnauthenticatedTerminalConsensus(t *testing.T) {
	model := "test/model"
	cfg := &types.DeliberationConfig{Topology: types.TopologyRing, Agents: []types.AgentConfig{{ID: "alpha", Model: model}}}
	control := authenticatedSessionConsensusControl()
	source := []types.TurnRecord{{Turn: -1, AgentID: "moderator", Control: control}}
	if _, err := session.Resume(session.ResumeRequest{
		RunRequest:    session.RunRequest{Topic: "invalid terminal resume", Config: cfg, OutputPath: t.TempDir() + "/resume.jsonl", MaxTurns: 1, TimeLimit: 60, DryRun: true},
		SourceRecords: source,
	}, session.Hooks{}); err == nil || !strings.Contains(err.Error(), "terminal consensus has no established run contract") {
		t.Fatalf("terminal-first consensus resume error: got %v", err)
	}
}

func authenticatedSessionConsensusActiveControl() *types.DeliberationControlState {
	control := types.NewDeliberationControlState([]string{"alpha", "beta"}, 0)
	control.Phase = types.PhaseVoting
	control.CurrentProposalVersion = 1
	control.Proposals = []types.CanonicalProposal{{Version: 1, AuthorID: "alpha", Content: "candidate"}}
	control.Convergence.RequiredEndorsements = 2
	control.Convergence.MinimumRounds = 1
	control.Contributions = []types.AgentContribution{
		{AgentID: "alpha", Turn: 0, Position: "alpha position", ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone}},
		{AgentID: "beta", Turn: 1, Position: "beta position", ProposalAction: types.ContributionProposalAction{Kind: types.ProposalActionNone}},
	}
	control.Votes = []types.ProposalVote{
		{AgentID: "alpha", ProposalVersion: 1, Choice: types.VoteEndorse},
		{AgentID: "beta", ProposalVersion: 1, Choice: types.VoteEndorse},
	}
	return control
}

func authenticatedSessionConsensusControl() *types.DeliberationControlState {
	control := authenticatedSessionConsensusActiveControl()
	control.Phase = types.PhaseTerminal
	control.Convergence.CurrentEndorsements = 2
	control.Outcome = types.TerminalOutcome{Kind: types.OutcomeConsensus, ProposalVersion: 1, DissentingAgentIDs: []string{}, UnresolvedObjectionIDs: []string{}, EvidenceGapClaimIDs: []string{}}
	return control
}

func terminalSessionConsensusFromActive(t *testing.T, active *types.DeliberationControlState) *types.DeliberationControlState {
	t.Helper()
	data, err := json.Marshal(active)
	if err != nil {
		t.Fatalf("marshal active control: %v", err)
	}
	var terminal types.DeliberationControlState
	if err := json.Unmarshal(data, &terminal); err != nil {
		t.Fatalf("unmarshal terminal control: %v", err)
	}
	terminal.Phase = types.PhaseTerminal
	terminal.Directive = types.TurnDirective{Kind: types.DirectiveNone}
	terminal.Convergence.CurrentEndorsements = len(terminal.AgentIDs)
	terminal.Outcome = types.TerminalOutcome{
		Kind:                   types.OutcomeConsensus,
		ProposalVersion:        terminal.CurrentProposalVersion,
		DissentingAgentIDs:     []string{},
		UnresolvedObjectionIDs: []string{},
		EvidenceGapClaimIDs:    []string{},
	}
	return &terminal
}

func TestResumePreservesAuthenticatedTerminalConsensus(t *testing.T) {
	model := "test/model"
	cfg := &types.DeliberationConfig{Topology: types.TopologyRing, Agents: []types.AgentConfig{{ID: "alpha", Model: model}, {ID: "beta", Model: model}}}
	active := authenticatedSessionConsensusActiveControl()
	control := terminalSessionConsensusFromActive(t, active)
	if err := control.Validate(); err != nil {
		t.Fatalf("authenticated source control: %v", err)
	}
	source := []types.TurnRecord{{Turn: -1, AgentID: "moderator", Control: active}, {Turn: -1, AgentID: "moderator", Control: control}}
	result, err := session.Resume(session.ResumeRequest{
		RunRequest:    session.RunRequest{Topic: "valid terminal resume", Config: cfg, OutputPath: t.TempDir() + "/resume.jsonl", MaxTurns: 1, TimeLimit: 60, DryRun: true},
		SourceRecords: source,
	}, session.Hooks{})
	if err != nil {
		t.Fatalf("Resume: %v", err)
	}
	if len(result.Records) != 2 || result.State.Control.Outcome.Kind != types.OutcomeConsensus || result.State.Control.Outcome.ProposalVersion != 1 {
		t.Fatalf("authenticated consensus changed at resume: records=%#v outcome=%#v", result.Records, result.State.Control.Outcome)
	}
}

func TestResumeRejectsUnauthenticatedTerminalConsensusGates(t *testing.T) {
	model := "test/model"
	cfg := &types.DeliberationConfig{Topology: types.TopologyRing, Agents: []types.AgentConfig{{ID: "alpha", Model: model}, {ID: "beta", Model: model}}}
	tests := []struct {
		name    string
		prepare func(*types.DeliberationControlState)
		mutate  func(*types.DeliberationControlState)
		want    string
	}{
		{
			name: "lowered endorsement threshold",
			prepare: func(state *types.DeliberationControlState) {
				state.Convergence.RequiredEndorsements = 3
			},
			mutate: func(state *types.DeliberationControlState) {
				state.Convergence.RequiredEndorsements = 2
			},
			want: "run consensus requirements are immutable",
		},
		{
			name: "lowered minimum rounds",
			prepare: func(state *types.DeliberationControlState) {
				state.Convergence.MinimumRounds = 2
			},
			mutate: func(state *types.DeliberationControlState) {
				state.Convergence.MinimumRounds = 1
			},
			want: "run consensus requirements are immutable",
		},
		{
			name: "lowered deliverable requirement",
			prepare: func(state *types.DeliberationControlState) {
				state.Convergence.RequiredDeliverableItems = 3
			},
			mutate: func(state *types.DeliberationControlState) {
				state.Convergence.RequiredDeliverableItems = 0
			},
			want: "run consensus requirements are immutable",
		},
		{
			name: "absent votes",
			mutate: func(state *types.DeliberationControlState) {
				state.Votes = nil
				state.Convergence.CurrentEndorsements = 0
				state.Outcome.DissentingAgentIDs = []string{"alpha", "beta"}
			},
		},
		{
			name: "stale votes",
			mutate: func(state *types.DeliberationControlState) {
				state.Proposals = append(state.Proposals, types.CanonicalProposal{Version: 2, AuthorID: "beta", Content: "proposal two", Supersedes: 1})
				state.CurrentProposalVersion = 2
				state.Convergence.CurrentEndorsements = 0
				state.Outcome.ProposalVersion = 2
				state.Outcome.DissentingAgentIDs = []string{"alpha", "beta"}
			},
		},
		{
			name: "split current versions",
			mutate: func(state *types.DeliberationControlState) {
				state.Proposals = append(state.Proposals, types.CanonicalProposal{Version: 2, AuthorID: "beta", Content: "proposal two", Supersedes: 1})
				state.CurrentProposalVersion = 2
				state.Votes[1].ProposalVersion = 2
				state.Convergence.CurrentEndorsements = 1
				state.Outcome.ProposalVersion = 2
				state.Outcome.DissentingAgentIDs = []string{"alpha"}
			},
		},
		{
			name: "minimum rounds unmet",
			prepare: func(state *types.DeliberationControlState) {
				state.Convergence.MinimumRounds = 2
			},
		},
		{
			name: "deliverable unmet",
			prepare: func(state *types.DeliberationControlState) {
				state.Convergence.RequiredDeliverableItems = 3
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active := authenticatedSessionConsensusActiveControl()
			if tt.prepare != nil {
				tt.prepare(active)
			}
			control := terminalSessionConsensusFromActive(t, active)
			if tt.mutate != nil {
				tt.mutate(control)
			}
			_, err := session.Resume(session.ResumeRequest{
				RunRequest:    session.RunRequest{Topic: "invalid terminal resume", Config: cfg, OutputPath: t.TempDir() + "/resume.jsonl", MaxTurns: 1, TimeLimit: 60, DryRun: true},
				SourceRecords: []types.TurnRecord{{Turn: -1, AgentID: "moderator", Control: active}, {Turn: -1, AgentID: "moderator", Control: control}},
			}, session.Hooks{})
			if err == nil {
				t.Fatal("resume accepted unauthenticated terminal consensus")
			}
			if tt.want != "" && !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("resume mutation error: got %v, want %q", err, tt.want)
			}
		})
	}
}
