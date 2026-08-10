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

	root := builtCLIRepositoryRoot(t)
	dir := t.TempDir()
	binary := filepath.Join(dir, "agora")
	build := exec.Command("go", "build", "-o", binary, "./cmd/agora")
	build.Dir = root
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build checkout CLI: %v\n%s", err, output)
	}
	version := runBuiltCLI(t, root, os.Environ(), binary, "--version")
	if !strings.Contains(version, "agora version ") {
		t.Fatalf("built CLI version output: %q", version)
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

	configPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(configPath, []byte(`topology: ring
consensus_threshold: 2
min_rounds: 3
ledger: true
agents:
  - id: alpha
    model: fake/non-provider
  - id: beta
    model: fake/non-provider
`), 0o644); err != nil {
		t.Fatalf("write integration config: %v", err)
	}

	baseEnv := builtCLIEnvironment(fakeDir, filepath.Join(dir, "config-home"), filepath.Join(dir, "data-home"))
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
	if _, ok := envelope["contribution_contract"]; !ok {
		return "", fmt.Errorf("unexpected fake opencode envelope")
	}
	return fakeContributionResponse(envelope, scenario)
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
