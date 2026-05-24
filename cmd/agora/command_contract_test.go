package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var commandContract = struct {
	formats     []string
	commands    map[string][]string
	settings    []string
	schema      int
	readmeMarks int
}{
	formats: []string{formatText, formatJSON, formatMarkdown},
	commands: map[string][]string{
		"agora config":      {},
		"agora config get":  {"all", "format"},
		"agora config init": {"force"},
		"agora config set":  {},
		"agora list":        {"format", "verbose"},
		"agora metadata":    {"format"},
		"agora prime":       {"format"},
		"agora run":         {"auto", "budget", "config", "context", "dry-run", "full-context", "max-turns", "model", "no-research", "output", "quiet", "research", "synthesize", "time", "topic", "verbose", "window", "yes"},
		"agora resume":      {"auto", "budget", "config", "context", "dry-run", "file", "full-context", "max-turns", "model", "no-research", "output", "quiet", "research", "synthesize", "time", "topic", "verbose", "window", "yes"},
		"agora stats":       {"format"},
		"agora show":        {"format"},
		"agora validate":    {"format"},
	},
	settings:    []string{"default_auto_level", "default_model", "default_output_dir", "default_topology", "context_max_bytes", "context_max_depth", "research_max_sources"},
	schema:      1,
	readmeMarks: 11,
}

func TestCommandContractMatchesLiveCLIBehavior(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	writeSettings(t, "")

	if !reflect.DeepEqual(validFormats, commandContract.formats) {
		t.Fatalf("valid formats drifted: got %v, want %v", validFormats, commandContract.formats)
	}
	if schemaVersion != commandContract.schema {
		t.Fatalf("schema version drifted: got %d, want %d", schemaVersion, commandContract.schema)
	}

	metadata := metadataCommandData()
	if got := intFromAny(metadata["schema_version"]); got != commandContract.schema {
		t.Fatalf("metadata schema version: got %d, want %d", got, commandContract.schema)
	}
	formats, ok := metadata["formats"].(map[string]any)
	if !ok {
		t.Fatalf("metadata formats missing: %#v", metadata["formats"])
	}
	if formats["default"] != formatText || !reflect.DeepEqual(anyStringSlice(formats["values"]), commandContract.formats) {
		t.Fatalf("metadata formats drifted: %#v", formats)
	}

	live := liveContractCommands()
	if got, want := sortedKeys(live), sortedKeys(commandContract.commands); !reflect.DeepEqual(got, want) {
		t.Fatalf("live command tree drifted:\ngot  %v\nwant %v", got, want)
	}
	for name, wantFlags := range commandContract.commands {
		cmd, ok := live[name]
		if !ok {
			t.Fatalf("live command %q missing", name)
		}
		if got := sortedFlagNames(cmd); !reflect.DeepEqual(got, wantFlags) {
			t.Fatalf("%s flags drifted:\ngot  %v\nwant %v", name, got, wantFlags)
		}
	}

	metadataCommands := metadataCommandMap(t, metadata["commands"])
	if got, want := sortedKeys(metadataCommands), sortedKeys(commandContract.commands); !reflect.DeepEqual(got, want) {
		t.Fatalf("metadata commands drifted:\ngot  %v\nwant %v", got, want)
	}
	for name, wantFlags := range commandContract.commands {
		if got := metadataFlagNames(metadataCommands[name]); !reflect.DeepEqual(got, wantFlags) {
			t.Fatalf("%s metadata flags drifted:\ngot  %v\nwant %v", name, got, wantFlags)
		}
	}

	settings := metadataSettings(t, metadata["settings_keys"])
	if got, want := sortedKeys(settings), sortedStrings(commandContract.settings); !reflect.DeepEqual(got, want) {
		t.Fatalf("settings key contract drifted:\ngot  %v\nwant %v", got, want)
	}
	assertEnumValues(t, settings["default_auto_level"], []string{"quick", "normal", "deep", "yolo"})
	assertEnumValues(t, settings["default_topology"], []string{"ring", "star", "mesh"})

	enums, ok := metadata["commands"].([]map[string]any)
	if !ok || len(enums) == 0 {
		t.Fatalf("metadata commands have unexpected shape: %#v", metadata["commands"])
	}
	for _, name := range []string{"agora list", "agora stats", "agora show", "agora validate", "agora config get", "agora metadata", "agora prime"} {
		flag := findMetadataFlag(t, metadataCommands[name], "format")
		assertEnumValues(t, flag, commandContract.formats)
	}
	assertEnumValues(t, findMetadataFlag(t, metadataCommands["agora run"], "auto"), []string{"quick", "normal", "deep", "yolo"})
	assertEnumValues(t, findMetadataFlag(t, metadataCommands["agora resume"], "auto"), []string{"quick", "normal", "deep", "yolo"})

	prime, err := primeCommandData()
	if err != nil {
		t.Fatalf("prime data: %v", err)
	}
	for _, key := range []string{"schema_version", "commands", "flags", "defaults", "enum_values", "settings_keys", "settings", "transcript_metadata", "context_boundary"} {
		if _, ok := prime[key]; !ok {
			t.Fatalf("prime metadata missing %q: %#v", key, prime)
		}
	}
	if got := intFromAny(prime["schema_version"]); got != commandContract.schema {
		t.Fatalf("prime schema version: got %d, want %d", got, commandContract.schema)
	}
}

func TestEnumRejectionsExposeValidValues(t *testing.T) {
	checks := []struct {
		name string
		err  error
		want []string
	}{
		{name: "format", err: validateFormat("xml"), want: commandContract.formats},
		{name: "auto level", err: func() error { _, err := types.ParseAutoLevel("maximum"); return err }(), want: []string{"quick", "normal", "deep", "yolo"}},
		{name: "topology", err: func() error { _, err := types.ParseTopology("line"); return err }(), want: []string{"ring", "star", "mesh"}},
	}
	for _, check := range checks {
		t.Run(check.name, func(t *testing.T) {
			if check.err == nil {
				t.Fatal("expected rejection")
			}
			for _, want := range check.want {
				assertStringContains(t, check.err.Error(), want)
			}
		})
	}
}

func TestReadmeContractCheckedExamplesMatchCommandSurface(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatalf("read README: %v", err)
	}
	markers := readmeContractMarkers(string(data))
	if len(markers) != commandContract.readmeMarks {
		t.Fatalf("README contract markers: got %d (%v), want %d", len(markers), markers, commandContract.readmeMarks)
	}
	commands := readmeContractCommands()
	for _, marker := range markers {
		assertReadmeCommandSurface(t, commands, marker)
	}
}

func TestCommandContractExpectedUseAndFailurePaths(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("TERM", "dumb")
	dir := t.TempDir()
	writeSettings(t, "default_output_dir: \""+dir+"\"")
	configPath := filepath.Join(dir, "config.yaml")
	writeValidConfig(t, configPath)
	badConfigPath := filepath.Join(dir, "bad.yaml")
	writeInvalidConfig(t, badConfigPath)
	transcriptPath := filepath.Join(dir, "20260504-143022-topic.jsonl")
	if err := os.WriteFile(transcriptPath, []byte(transcriptLine("analyst", "contract", 3)), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}
	malformedTranscript := filepath.Join(dir, "bad.jsonl")
	if err := os.WriteFile(malformedTranscript, []byte("{not json}\n"), 0o644); err != nil {
		t.Fatalf("write malformed transcript: %v", err)
	}

	tests := []struct {
		name    string
		success func() error
		failure func() error
	}{
		{name: "list", success: func() error { executeListCommand(t, formatJSON); return nil }, failure: invalidListFormat},
		{name: "stats", success: func() error { _, err := executeStatsCommand(t, formatJSON, transcriptPath); return err }, failure: func() error {
			_, err := executeStatsCommand(t, formatJSON, filepath.Join(dir, "missing.jsonl"))
			return err
		}},
		{name: "show", success: func() error { _, err := executeShowCommandFormat(t, formatJSON, transcriptPath); return err }, failure: func() error { _, err := executeShowCommandFormat(t, formatJSON, malformedTranscript); return err }},
		{name: "validate", success: func() error { _, err := runValidateCommand(t, formatJSON, configPath); return err }, failure: func() error { _, err := runValidateCommand(t, formatJSON, badConfigPath); return err }},
		{name: "config init", success: func() error {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			_, err := executeConfigCommand(t, "init")
			return err
		}, failure: func() error { _, err := executeConfigCommand(t, "init"); return err }},
		{name: "config get --all", success: func() error { _, err := executeConfigCommand(t, "get", "--all", "--format", "json"); return err }, failure: func() error { _, err := executeConfigCommand(t, "get", "--all", "--format", "xml"); return err }},
		{name: "config set", success: func() error { _, err := executeConfigCommand(t, "set", "default_auto_level", "quick"); return err }, failure: func() error { _, err := executeConfigCommand(t, "set", "default_auto_level", "maximum"); return err }},
		{name: "metadata", success: func() error { return runMetadata(formatJSON) }, failure: func() error { return runMetadata("xml") }},
		{name: "prime", success: func() error { runPrimeCommand(t, formatJSON); return nil }, failure: invalidPrimeFormat},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.success(); err != nil {
				t.Fatalf("expected-use path failed: %v", err)
			}
			if err := tt.failure(); err == nil {
				t.Fatal("failure path unexpectedly succeeded")
			}
		})
	}
}

func liveContractCommands() map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	for _, item := range liveCommandSurface(rootCmd) {
		out[item.name] = item.cmd
	}
	return out
}

func readmeContractCommands() map[string]*cobra.Command {
	out := map[string]*cobra.Command{}
	for name, cmd := range liveContractCommands() {
		out[strings.TrimPrefix(name, "agora ")] = cmd
	}
	return out
}

func sortedFlagNames(cmd *cobra.Command) []string {
	var names []string
	cmd.Flags().VisitAll(func(flag *pflag.Flag) {
		names = append(names, flag.Name)
	})
	return sortedStrings(names)
}

func metadataCommandMap(t *testing.T, value any) map[string]map[string]any {
	t.Helper()
	commands, ok := value.([]map[string]any)
	if !ok {
		t.Fatalf("metadata commands have unexpected shape: %#v", value)
	}
	out := map[string]map[string]any{}
	for _, command := range commands {
		name, _ := command["name"].(string)
		out[name] = command
	}
	return out
}

func metadataFlagNames(command map[string]any) []string {
	flags, _ := command["flags"].([]map[string]any)
	names := make([]string, 0, len(flags))
	for _, flag := range flags {
		if name, ok := flag["name"].(string); ok {
			names = append(names, name)
		}
	}
	return sortedStrings(names)
}

func findMetadataFlag(t *testing.T, command map[string]any, name string) map[string]any {
	t.Helper()
	flags, _ := command["flags"].([]map[string]any)
	for _, flag := range flags {
		if flag["name"] == name {
			return flag
		}
	}
	t.Fatalf("metadata flag %q missing from %#v", name, command)
	return nil
}

func metadataSettings(t *testing.T, value any) map[string]map[string]any {
	t.Helper()
	settings, ok := value.([]map[string]any)
	if !ok {
		t.Fatalf("metadata settings have unexpected shape: %#v", value)
	}
	out := map[string]map[string]any{}
	for _, setting := range settings {
		key, _ := setting["key"].(string)
		out[key] = setting
	}
	return out
}

func assertEnumValues(t *testing.T, item map[string]any, want []string) {
	t.Helper()
	got := anyStringSlice(item["enum_values"])
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("enum values drifted for %#v: got %v, want %v", item, got, want)
	}
}

func readmeContractMarkers(readme string) []string {
	var markers []string
	for _, line := range strings.Split(readme, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "<!-- agora-contract:") || !strings.HasSuffix(line, "-->") {
			continue
		}
		marker := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "<!-- agora-contract:"), "-->"))
		if marker != "" {
			markers = append(markers, marker)
		}
	}
	return markers
}

func assertReadmeCommandSurface(t *testing.T, commands map[string]*cobra.Command, marker string) {
	t.Helper()
	fields := strings.Fields(marker)
	if len(fields) < 2 {
		t.Fatalf("invalid README contract marker %q", marker)
	}
	bin := strings.TrimPrefix(fields[0], "./")
	if bin != "agora" {
		t.Fatalf("README marker %q does not start with agora", marker)
	}
	path, cmd, rest := resolveReadmeCommand(commands, fields[1:])
	if cmd == nil {
		t.Fatalf("README marker %q references unsupported command path %q", marker, path)
	}
	for _, token := range rest {
		if !strings.HasPrefix(token, "--") {
			continue
		}
		name := strings.TrimPrefix(strings.SplitN(token, "=", 2)[0], "--")
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("README marker %q references unsupported flag --%s", marker, name)
		}
	}
}

func resolveReadmeCommand(commands map[string]*cobra.Command, fields []string) (string, *cobra.Command, []string) {
	for n := min(3, len(fields)); n >= 1; n-- {
		path := strings.Join(fields[:n], " ")
		if cmd := commands[path]; cmd != nil {
			return path, cmd, fields[n:]
		}
	}
	return fields[0], nil, fields[1:]
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return sortedStrings(keys)
}

func sortedStrings(values []string) []string {
	out := append([]string(nil), values...)
	sort.Strings(out)
	if out == nil {
		return []string{}
	}
	return out
}

func invalidListFormat() error {
	old := listFormat
	listFormat = "xml"
	defer func() { listFormat = old }()
	return listCmd.RunE(&cobra.Command{}, nil)
}

func invalidPrimeFormat() error {
	old := primeFormat
	primeFormat = "xml"
	defer func() { primeFormat = old }()
	return primeCmd.RunE(&cobra.Command{}, nil)
}

func runMetadata(format string) error {
	old := metadataFormat
	metadataFormat = format
	defer func() { metadataFormat = old }()
	cmd := &cobra.Command{}
	var out bytes.Buffer
	cmd.SetOut(&out)
	return metadataCmd.RunE(cmd, nil)
}
