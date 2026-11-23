package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
)

const (
	fakeOpenCodeEnv         = "AGORA_TEST_FAKE_OPENCODE"
	fakeOpenCodeScenarioEnv = "AGORA_TEST_FAKE_OPENCODE_SCENARIO"
)

// init turns this test binary into a deterministic local opencode replacement
// only when the integration test invokes it through its temporary PATH.
func init() {
	if os.Getenv(fakeOpenCodeEnv) != "1" {
		return
	}

	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintln(os.Stderr, "read fake opencode input:", err)
		os.Exit(2)
	}
	response, err := fakeOpenCodeResponse(string(input), os.Getenv(fakeOpenCodeScenarioEnv))
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake opencode response:", err)
		os.Exit(2)
	}
	event, err := json.Marshal(map[string]any{
		"type": "text",
		"part": map[string]string{"text": response},
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal fake opencode event:", err)
		os.Exit(2)
	}
	_, _ = os.Stdout.Write(append(event, '\n'))
	os.Exit(0)
}

func TestBuiltCLIAuthenticatesConsensusAndPreservesTerminalOutcomes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake opencode uses a temporary executable symlink")
	}

	root, dir, binary, baseEnv := buildCLIWithFakeOpenCode(t)
	version := runBuiltCLI(t, root, os.Environ(), binary, "--version")
	if !strings.Contains(version, "agora version ") {
		t.Fatalf("built CLI version output: %q", version)
	}
	configPath := writeBuiltCLIConfig(t, dir, "")
	for _, tc := range []builtCLITerminalExpectation{
		{
			name:            "authenticated consensus",
			scenario:        "consensus",
			maxTurns:        "8",
			wantKind:        types.OutcomeConsensus,
			wantVersion:     3,
			wantProposal:    "Authenticated current proposal v3",
			wantReason:      "consensus (proposal v3)",
			wantUnresolved:  []string{},
			wantEndorsement: true,
		},
		{
			name:           "cap no consensus with unresolved objection",
			scenario:       "no-consensus",
			maxTurns:       "6",
			wantKind:       types.OutcomeNoConsensus,
			wantVersion:    1,
			wantProposal:   "Contested current proposal v1",
			wantReason:     "max_turns (6)",
			wantUnresolved: []string{"safety-objection"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			outputPath := filepath.Join(dir, tc.scenario+".jsonl")
			env := append([]string{}, baseEnv...)
			env = append(env, fakeOpenCodeScenarioEnv+"="+tc.scenario)
			runBuiltCLI(t, root, env, binary,
				"run", "--config", configPath, "--topic", "Verify terminal persistence", "--max-turns", tc.maxTurns,
				"--time", "30", "--output", outputPath, "--quiet")

			records, err := transcript.LoadFileStrict(outputPath)
			if err != nil {
				t.Fatalf("strictly load built-CLI transcript: %v", err)
			}
			assertBuiltCLIPersistedTerminal(t, records, tc)

			shown := showBuiltCLITerminal(t, root, env, binary, outputPath)
			assertBuiltCLITerminalState(t, shown.State, tc)
			if shown.ResumeAction != transcript.CompatibilityActionPreserveTerminal {
				t.Fatalf("show resume action: got %q, want %q", shown.ResumeAction, transcript.CompatibilityActionPreserveTerminal)
			}

			resumedPath := filepath.Join(dir, tc.scenario+"-resumed.jsonl")
			runBuiltCLI(t, root, env, binary,
				"resume", outputPath, "--config", configPath, "--topic", "Verify terminal resume", "--max-turns", "1",
				"--time", "30", "--output", resumedPath, "--quiet")
			resumed := showBuiltCLITerminal(t, root, env, binary, resumedPath)
			if resumed.ResumeAction != transcript.CompatibilityActionPreserveTerminal {
				t.Fatalf("resumed show action: got %q, want %q", resumed.ResumeAction, transcript.CompatibilityActionPreserveTerminal)
			}
			if !reflect.DeepEqual(resumed.State, shown.State) {
				t.Fatalf("resume changed typed terminal state:\nsource=%#v\nresumed=%#v", shown.State, resumed.State)
			}
		})
	}
}

// buildCLIWithFakeOpenCode builds the checkout CLI and returns an environment
// whose PATH resolves opencode to this test binary's deterministic responder.
func buildCLIWithFakeOpenCode(t *testing.T) (root, dir, binary string, env []string) {
	t.Helper()
	root = builtCLIRepositoryRoot(t)
	dir = t.TempDir()
	binary = filepath.Join(dir, "agora")
	build := exec.Command("go", "build", "-o", binary, "./cmd/agora")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build checkout CLI: %v\n%s", err, output)
	}

	fakeDir := filepath.Join(dir, "bin")
	if err := os.Mkdir(fakeDir, 0o755); err != nil {
		t.Fatalf("create fake PATH directory: %v", err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatalf("test executable: %v", err)
	}
	if err := os.Symlink(testBinary, filepath.Join(fakeDir, "opencode")); err != nil {
		t.Fatalf("link fake opencode: %v", err)
	}
	return root, dir, binary, builtCLIEnvironment(fakeDir, filepath.Join(dir, "config-home"), filepath.Join(dir, "data-home"))
}

// writeBuiltCLIConfig writes the two-agent integration config. extra holds
// additional top-level YAML lines.
func writeBuiltCLIConfig(t *testing.T, dir, extra string) string {
	t.Helper()
	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`topology: ring
consensus_threshold: 2
min_rounds: 3
ledger: true
`+extra+`agents:
  - id: alpha
    model: fake/non-provider
  - id: beta
    model: fake/non-provider
`), 0o644); err != nil {
		t.Fatalf("write integration config: %v", err)
	}
	return configPath
}

type builtCLITerminalExpectation struct {
	name            string
	scenario        string
	maxTurns        string
	wantKind        types.TerminalOutcomeKind
	wantVersion     int
	wantProposal    string
	wantReason      string
	wantUnresolved  []string
	wantEndorsement bool
}

type builtCLIShowTerminal struct {
	State        *types.TerminalState
	ResumeAction string
}

func showBuiltCLITerminal(t *testing.T, root string, env []string, binary, path string) builtCLIShowTerminal {
	t.Helper()
	output := runBuiltCLI(t, root, env, binary, "show", path, "--format", "json")
	var document struct {
		Data struct {
			TerminalState *types.TerminalState `json:"terminal_state"`
			Compatibility struct {
				ResumeAction string `json:"resume_action"`
			} `json:"compatibility"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatalf("decode built CLI show JSON: %v\n%s", err, output)
	}
	return builtCLIShowTerminal{State: document.Data.TerminalState, ResumeAction: document.Data.Compatibility.ResumeAction}
}

func assertBuiltCLIPersistedTerminal(t *testing.T, records []types.TurnRecord, want builtCLITerminalExpectation) {
	t.Helper()
	terminalCount := 0
	ledgerCount := 0
	activeContract := false
	for _, record := range records {
		if record.Ledger != nil {
			ledgerCount++
		}
		if record.Control == nil {
			continue
		}
		if record.Control.Phase == types.PhaseTerminal {
			terminalCount++
			continue
		}
		if record.Control.Convergence.RunContractVersion == types.RunContractVersion &&
			record.Control.Convergence.RequiredEndorsements == 2 &&
			record.Control.Convergence.MinimumRounds == 3 {
			activeContract = true
		}
	}
	if terminalCount != 1 {
		t.Fatalf("terminal records: got %d, want 1", terminalCount)
	}
	if ledgerCount == 0 {
		t.Fatal("built CLI persisted no ledger snapshots")
	}
	if !activeContract {
		t.Fatal("built CLI persisted no active authenticated run contract")
	}
}

func assertBuiltCLITerminalState(t *testing.T, state *types.TerminalState, want builtCLITerminalExpectation) {
	t.Helper()
	if state == nil || state.Phase != types.PhaseTerminal || state.Outcome.Kind != want.wantKind ||
		state.ProposalVersion != want.wantVersion || state.CanonicalProposal == nil ||
		state.CanonicalProposal.Content != want.wantProposal || state.HaltReason != want.wantReason {
		t.Fatalf("terminal state: got %#v", state)
	}
	if state.Convergence.RunContractVersion != types.RunContractVersion ||
		state.Convergence.RequiredEndorsements != 2 || state.Convergence.MinimumRounds != 3 {
		t.Fatalf("terminal run contract: %#v", state.Convergence)
	}
	if !reflect.DeepEqual(state.Outcome.UnresolvedObjectionIDs, want.wantUnresolved) {
		t.Fatalf("unresolved objections: got %v, want %v", state.Outcome.UnresolvedObjectionIDs, want.wantUnresolved)
	}
	if want.wantEndorsement {
		if len(state.CurrentVotes) != 2 || state.CurrentVotes[0].Choice != types.VoteEndorse || state.CurrentVotes[1].Choice != types.VoteEndorse ||
			state.CurrentVotes[0].AgentID == state.CurrentVotes[1].AgentID {
			t.Fatalf("unique consensus votes: %#v", state.CurrentVotes)
		}
		return
	}
	if !reflect.DeepEqual(state.DissentingAgentIDs, []string{"alpha", "beta"}) || len(state.Objections) != 1 || state.Objections[0].ID != "safety-objection" {
		t.Fatalf("no-consensus evidence: %#v", state)
	}
}

func builtCLIRepositoryRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate integration test source")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func builtCLIEnvironment(fakeDir, configHome, dataHome string) []string {
	overrides := map[string]string{
		fakeOpenCodeEnv:   "1",
		"NO_COLOR":        "1",
		"TERM":            "dumb",
		"PATH":            fakeDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"XDG_CONFIG_HOME": configHome,
		"XDG_DATA_HOME":   dataHome,
	}
	env := make([]string, 0, len(os.Environ())+len(overrides))
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		if _, overridden := overrides[key]; !overridden {
			env = append(env, entry)
		}
	}
	for _, key := range []string{fakeOpenCodeEnv, "NO_COLOR", "TERM", "PATH", "XDG_CONFIG_HOME", "XDG_DATA_HOME"} {
		env = append(env, key+"="+overrides[key])
	}
	return env
}

func runBuiltCLI(t *testing.T, root string, env []string, binary string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = root
	cmd.Env = env
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("built CLI %s: %v\n%s", strings.Join(args, " "), err, output)
	}
	return string(output)
}

// runBuiltCLIStreams runs the built CLI and returns stdout and stderr apart,
// so tests can assert that a structured format keeps stdout to one document.
func runBuiltCLIStreams(t *testing.T, root string, env []string, binary string, args ...string) (string, string) {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = root
	cmd.Env = env
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("built CLI %s: %v\nstdout: %s\nstderr: %s", strings.Join(args, " "), err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String()
}

func fakeOpenCodeResponse(input, scenario string) (string, error) {
	envelope, err := fakeOpenCodeEnvelope(input)
	if err != nil {
		return "", err
	}
	if _, ok := envelope["moderation_contract"]; ok {
		return fakeModeratorResponse(envelope)
	}
	if strings.Contains(input, "mid-deliberation ledger updater") {
		return `{"round":0,"positions":[],"agreements":[],"cruxes":[],"draft":{"status":"none"}}`, nil
	}
	if _, ok := envelope["total_turns"]; ok {
		return `{"key_arguments":["fake argument"],"points_of_agreement":[],"unresolved_tensions":["fake tension"],"recommended_decision":"fake recommendation","confidence":"medium"}`, nil
	}
	if _, ok := envelope["contribution_contract"]; !ok {
		return "", fmt.Errorf("unexpected fake opencode envelope")
	}
	response, err := fakeContributionResponse(envelope, scenario)
	if err != nil {
		return "", err
	}
	return fakeScoredContribution(envelope, response)
}

// fakeScoredContribution adds a score only when the contract requires one.
// alpha rates 7 in its opening and 9 afterwards; beta always declines, so the
// report must carry a latest score and a null score side by side.
func fakeScoredContribution(envelope map[string]any, response string) (string, error) {
	contract, _ := envelope["contribution_contract"].(map[string]any)
	required, _ := contract["required"].([]any)
	scored := false
	for _, field := range required {
		scored = scored || field == "score"
	}
	if !scored {
		return response, nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(response), &payload); err != nil {
		return "", err
	}
	payload["score"] = nil
	if envelope["agent_id"] == "alpha" {
		payload["score"] = 9
		if control, _ := envelope["control_state"].(map[string]any); control["phase"] == string(types.PhaseOpening) {
			payload["score"] = 7
		}
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func fakeOpenCodeEnvelope(input string) (map[string]any, error) {
	separator := strings.LastIndex(input, "\n\n")
	if separator < 0 {
		return nil, fmt.Errorf("missing prompt envelope separator")
	}
	var envelope map[string]any
	if err := json.Unmarshal([]byte(input[separator+2:]), &envelope); err != nil {
		return nil, fmt.Errorf("decode prompt envelope: %w", err)
	}
	return envelope, nil
}

func fakeModeratorResponse(envelope map[string]any) (string, error) {
	contract, ok := envelope["moderation_contract"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid moderation contract")
	}
	actions, ok := contract["actions"].([]any)
	if !ok || len(actions) == 0 {
		return "", fmt.Errorf("moderation contract has no actions")
	}
	data, err := json.Marshal(actions[0])
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func fakeContributionResponse(envelope map[string]any, scenario string) (string, error) {
	control, ok := envelope["control_state"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("missing control state")
	}
	phase, _ := control["phase"].(string)
	agentID, _ := envelope["agent_id"].(string)
	if phase == string(types.PhaseOpening) {
		return fakeContribution(fmt.Sprintf("%s independent opening", agentID), map[string]any{"kind": "none"}, nil, nil)
	}

	proposalVersion := fakeInteger(control["current_proposal_version"])
	switch scenario {
	case "consensus":
		switch phase {
		case string(types.PhaseRebuttal):
			if proposalVersion == 0 {
				return fakeContribution("create an authenticated proposal", map[string]any{"kind": "create", "content": "Authenticated current proposal v1"}, nil, nil)
			}
		case string(types.PhaseDrafting):
			directive, _ := envelope["directive"].(map[string]any)
			if directive["kind"] == string(types.DirectiveReviseProposal) {
				previous := fakeInteger(directive["proposal_version"])
				return fakeContribution("revise the authenticated proposal", map[string]any{
					"kind": "revise", "supersedes": previous, "content": fmt.Sprintf("Authenticated current proposal v%d", previous+1),
				}, nil, nil)
			}
		case string(types.PhaseVoting):
			return fakeContribution("endorse the authenticated proposal", map[string]any{"kind": "none"}, nil, map[string]any{
				"proposal_version": proposalVersion, "choice": "endorse",
			})
		}
	case "no-consensus":
		if phase == string(types.PhaseRebuttal) {
			if proposalVersion == 0 {
				return fakeContribution("create a contested proposal", map[string]any{"kind": "create", "content": "Contested current proposal v1"}, nil, nil)
			}
			if objections, _ := control["objections"].([]any); len(objections) == 0 {
				return fakeContribution("raise an unresolved safety objection", map[string]any{"kind": "none"}, []map[string]any{{
					"id": "safety-objection", "proposal_version": proposalVersion, "summary": "safety evidence remains unresolved",
				}}, nil)
			}
		}
	default:
		return "", fmt.Errorf("unknown fake opencode scenario %q", scenario)
	}
	return fakeContribution("retain the current state", map[string]any{"kind": "none"}, nil, nil)
}

func fakeContribution(position string, action map[string]any, objections []map[string]any, vote any) (string, error) {
	objectionValues := make([]any, 0, len(objections))
	for _, objection := range objections {
		objectionValues = append(objectionValues, objection)
	}
	payload := map[string]any{
		"position":        position,
		"responses":       []any{},
		"concessions":     []any{},
		"proposal_action": action,
		"objections":      objectionValues,
		"vote":            vote,
		"claims":          []any{},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func fakeInteger(value any) int {
	if number, ok := value.(float64); ok {
		return int(number)
	}
	return 0
}

type builtCLIRunReport struct {
	Command string `json:"command"`
	Data    struct {
		DocumentType   string  `json:"document_type"`
		TranscriptPath string  `json:"transcript_path"`
		Failure        *string `json:"failure"`
		Scoring        bool    `json:"scoring"`
		Outcome        struct {
			Kind       types.TerminalOutcomeKind `json:"kind"`
			HaltReason string                    `json:"halt_reason"`
		} `json:"outcome"`
		Agents []struct {
			ID       string  `json:"id"`
			Model    string  `json:"model"`
			Score    *int    `json:"score"`
			Vote     *string `json:"vote"`
			Position string  `json:"position"`
			Turns    int     `json:"turns"`
		} `json:"agents"`
		UnresolvedObjections []types.Objection `json:"unresolved_objections"`
		Synthesis            map[string]any    `json:"synthesis"`
		Totals               struct {
			Turns int `json:"turns"`
		} `json:"totals"`
	} `json:"data"`
}

func decodeBuiltCLIRunReport(t *testing.T, stdout string) builtCLIRunReport {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(stdout))
	var report builtCLIRunReport
	if err := decoder.Decode(&report); err != nil {
		t.Fatalf("decode run report: %v\n%s", err, stdout)
	}
	if decoder.More() {
		t.Fatalf("stdout carries more than one document:\n%s", stdout)
	}
	assertStringNotContains(t, stdout, "\x1b[")
	return report
}

func TestBuiltCLIRunReportKeepsStdoutToOneDocumentWithScores(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake opencode uses a temporary executable symlink")
	}
	root, dir, binary, baseEnv := buildCLIWithFakeOpenCode(t)
	configPath := writeBuiltCLIConfig(t, dir, "scoring: true\n")
	noConsensusEnv := append(append([]string{}, baseEnv...), fakeOpenCodeScenarioEnv+"=no-consensus")
	consensusEnv := append(append([]string{}, baseEnv...), fakeOpenCodeScenarioEnv+"=consensus")

	outputPath := filepath.Join(dir, "report.jsonl")
	stdout, stderr := runBuiltCLIStreams(t, root, noConsensusEnv, binary,
		"run", "--config", configPath, "--topic", "Report a scored review", "--max-turns", "6",
		"--time", "30", "--output", outputPath, "--synthesize", "--format", "json")
	report := decodeBuiltCLIRunReport(t, stdout)

	if report.Command != "run" || report.Data.DocumentType != "agora.run.report" {
		t.Fatalf("report identity: command=%q document_type=%q", report.Command, report.Data.DocumentType)
	}
	if report.Data.TranscriptPath != outputPath || report.Data.Failure != nil || !report.Data.Scoring {
		t.Fatalf("report header: path=%q failure=%v scoring=%v", report.Data.TranscriptPath, report.Data.Failure, report.Data.Scoring)
	}
	if report.Data.Outcome.Kind != types.OutcomeNoConsensus || report.Data.Outcome.HaltReason != "max_turns (6)" || report.Data.Totals.Turns != 6 {
		t.Fatalf("report outcome: %#v turns=%d", report.Data.Outcome, report.Data.Totals.Turns)
	}
	if len(report.Data.UnresolvedObjections) != 1 || report.Data.UnresolvedObjections[0].ID != "safety-objection" ||
		report.Data.UnresolvedObjections[0].Summary != "safety evidence remains unresolved" {
		t.Fatalf("report unresolved objections: %#v", report.Data.UnresolvedObjections)
	}
	if len(report.Data.Agents) != 2 || report.Data.Agents[0].ID != "alpha" || report.Data.Agents[1].ID != "beta" {
		t.Fatalf("report agents: %#v", report.Data.Agents)
	}
	alpha, beta := report.Data.Agents[0], report.Data.Agents[1]
	if alpha.Score == nil || *alpha.Score != 9 || beta.Score != nil {
		t.Fatalf("report scores: alpha=%v beta=%v, want latest 9 and null", alpha.Score, beta.Score)
	}
	if alpha.Model != "fake/non-provider" || alpha.Turns+beta.Turns != 6 || alpha.Position == "" || beta.Position == "" || alpha.Vote != nil {
		t.Fatalf("report agent detail: alpha=%#v beta=%#v", alpha, beta)
	}
	if report.Data.Synthesis["recommended_decision"] != "fake recommendation" {
		t.Fatalf("report synthesis: %#v", report.Data.Synthesis)
	}
	if _, embedded := report.Data.Synthesis["terminal_state"]; embedded {
		t.Fatalf("report synthesis repeats the terminal state: %#v", report.Data.Synthesis)
	}
	for _, live := range []string{"Deliberation complete", "[SUCCESS] Transcript:", "AGENT CONTENT", "Synthesis"} {
		assertStringContains(t, stderr, live)
	}
	for _, live := range []string{"Deliberation complete", "[SUCCESS]", "[INFO]", "AGENT CONTENT", "TURN "} {
		assertStringNotContains(t, stdout, live)
	}

	// The scores are read back from the persisted transcript, not only from
	// the in-memory result.
	records, err := transcript.LoadFileStrict(outputPath)
	if err != nil {
		t.Fatalf("strictly load scored transcript: %v", err)
	}
	persisted := map[string][]any{}
	for _, contribution := range types.TerminalStateFromRecords(records).Control.Contributions {
		var score any
		if contribution.Score != nil {
			score = *contribution.Score
		}
		persisted[contribution.AgentID] = append(persisted[contribution.AgentID], score)
	}
	if want := map[string][]any{"alpha": {7, 9, 9, 9}, "beta": {nil, nil}}; !reflect.DeepEqual(persisted, want) {
		t.Fatalf("persisted scores: got %#v, want %#v", persisted, want)
	}

	markdown, markdownErr := runBuiltCLIStreams(t, root, noConsensusEnv, binary,
		"run", "--config", configPath, "--topic", "Report a scored review", "--max-turns", "6",
		"--time", "30", "--output", filepath.Join(dir, "report-markdown.jsonl"), "--synthesize", "--quiet", "--format", "markdown")
	if !strings.HasPrefix(markdown, "# Agora run report\n") {
		t.Fatalf("markdown report must start the document:\n%s", markdown)
	}
	for _, want := range []string{
		"- **Outcome:** no_consensus", "- **Halt reason:** max_turns (6)",
		"| Agent | Model | Score | Vote | Turns | Tokens | Cost |",
		"| alpha | `fake/non-provider` | 9/10 | no vote |", "| beta | `fake/non-provider` | no score | no vote |",
		"## Unresolved objections", "`safety-objection` (", "## Latest positions", "## Synthesis", "fake recommendation",
	} {
		assertStringContains(t, markdown, want)
	}
	assertStringNotContains(t, markdown, "\x1b[")
	assertStringNotContains(t, markdown, "Deliberation complete")
	assertStringContains(t, markdownErr, "Deliberation complete")

	consensusPath := filepath.Join(dir, "report-consensus.jsonl")
	stdout, _ = runBuiltCLIStreams(t, root, consensusEnv, binary,
		"run", "--config", configPath, "--topic", "Report a consensus", "--max-turns", "8",
		"--time", "30", "--output", consensusPath, "--quiet", "--format", "json")
	consensus := decodeBuiltCLIRunReport(t, stdout)
	if consensus.Data.Outcome.Kind != types.OutcomeConsensus || consensus.Data.Synthesis != nil {
		t.Fatalf("consensus report: outcome=%#v synthesis=%#v", consensus.Data.Outcome, consensus.Data.Synthesis)
	}
	for _, agent := range consensus.Data.Agents {
		if agent.Vote == nil || *agent.Vote != string(types.VoteEndorse) {
			t.Fatalf("consensus report vote for %s: %v", agent.ID, agent.Vote)
		}
	}

	stdout, stderr = runBuiltCLIStreams(t, root, consensusEnv, binary,
		"resume", consensusPath, "--config", configPath, "--topic", "Report a resume", "--max-turns", "1",
		"--time", "30", "--output", filepath.Join(dir, "report-resumed.jsonl"), "--quiet", "--format", "json")
	resumed := decodeBuiltCLIRunReport(t, stdout)
	if resumed.Command != "resume" || resumed.Data.Outcome.Kind != types.OutcomeConsensus {
		t.Fatalf("resume report: command=%q outcome=%#v", resumed.Command, resumed.Data.Outcome)
	}
	assertStringContains(t, stderr, "Resumed deliberation complete")
}
