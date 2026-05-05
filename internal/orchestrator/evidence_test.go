package orchestrator

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/jgabor/agora/internal/types"
)

func TestPolicyEvidenceCollectorReferencesReadableTextFile(t *testing.T) {
	path := writeContextFile(t, t.TempDir(), "notes.md", "useful context\n")

	bundle, err := (PolicyEvidenceCollector{}).Collect(types.EvidenceRequest{ContextPaths: []string{path}})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(bundle.SourceReferences) != 1 || bundle.SourceReferences[0].Path != path {
		t.Fatalf("SourceReferences: got %#v, want %q", bundle.SourceReferences, path)
	}
}

func TestPolicyEvidenceCollectorResolvesDirectoryTextAndSkipsUnsafeFiles(t *testing.T) {
	dir := t.TempDir()
	keep := writeContextFile(t, dir, "docs/keep.txt", "safe text\n")
	alsoKeep := writeContextFile(t, dir, "notes.md", "safe note\n")
	writeContextFile(t, dir, "binary.dat", string([]byte{0x00, 0x01, 0x02}))
	writeContextFile(t, dir, ".git/config", "[remote]\n")
	writeContextFile(t, dir, ".env", "TOKEN=secret\n")
	writeContextFile(t, dir, "api-token.txt", "secret\n")

	bundle, err := (PolicyEvidenceCollector{}).Collect(types.EvidenceRequest{
		ContextPaths: []string{dir},
		MaxSources:   10,
		MaxBytes:     1024,
		MaxDepth:     3,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	got := referencePaths(bundle.SourceReferences)
	assertContainsPath(t, got, keep)
	assertContainsPath(t, got, alsoKeep)
	assertNotContainsSubstring(t, got, "binary.dat")
	assertNotContainsSubstring(t, got, ".git")
	assertNotContainsSubstring(t, got, ".env")
	assertNotContainsSubstring(t, got, "api-token.txt")
}

func TestPolicyEvidenceCollectorReportsBoundedContextErrors(t *testing.T) {
	t.Run("file cap", func(t *testing.T) {
		dir := t.TempDir()
		writeContextFile(t, dir, "one.txt", "one")
		writeContextFile(t, dir, "two.txt", "two")
		_, err := (PolicyEvidenceCollector{}).Collect(types.EvidenceRequest{ContextPaths: []string{dir}, MaxSources: 1, MaxBytes: 1024, MaxDepth: 3})
		assertBoundedContextError(t, err, "max files 1")
	})

	t.Run("byte cap", func(t *testing.T) {
		dir := t.TempDir()
		writeContextFile(t, dir, "large.txt", "1234567890")
		_, err := (PolicyEvidenceCollector{}).Collect(types.EvidenceRequest{ContextPaths: []string{dir}, MaxSources: 10, MaxBytes: 5, MaxDepth: 3})
		assertBoundedContextError(t, err, "max bytes 5")
	})

	t.Run("depth cap", func(t *testing.T) {
		dir := t.TempDir()
		writeContextFile(t, dir, "a/b/deep.txt", "deep")
		_, err := (PolicyEvidenceCollector{}).Collect(types.EvidenceRequest{ContextPaths: []string{dir}, MaxSources: 10, MaxBytes: 1024, MaxDepth: 1})
		assertBoundedContextError(t, err, "max depth 1")
	})
}

func TestPolicyEvidenceCollectorDerivesBoundedResearchQueries(t *testing.T) {
	runner := &mockRunner{content: `{"queries":["agora deliberation research", "agora evidence contract", "agora query caps"]}`}
	collector := NewPolicyEvidenceCollector(runner)

	queries, err := collector.deriveResearchQueries(types.EvidenceRequest{
		ResearchEnabled: true,
		Topic:           "How should Agora bound web research?",
		ResearchModel:   "selected-deliberation-model",
		MaxSources:      2,
	})
	if err != nil {
		t.Fatalf("deriveResearchQueries: %v", err)
	}
	if len(queries) != 2 {
		t.Fatalf("queries: got %#v, want exactly 2 due to cap", queries)
	}
	if runner.agent.Model != "selected-deliberation-model" {
		t.Fatalf("research model: got %q, want selected deliberation model", runner.agent.Model)
	}
	if runner.envelope["topic"] != "How should Agora bound web research?" {
		t.Fatalf("topic envelope: got %#v", runner.envelope["topic"])
	}
	if runner.envelope["max_queries"] != 2 {
		t.Fatalf("max_queries envelope: got %#v, want 2", runner.envelope["max_queries"])
	}
}

func TestPolicyEvidenceCollectorRejectsResearchQueryFailures(t *testing.T) {
	tests := []struct {
		name    string
		request types.EvidenceRequest
		runner  *mockRunner
		want    string
	}{
		{
			name:    "runner failure",
			request: types.EvidenceRequest{ResearchEnabled: true, Topic: "topic", ResearchModel: "model", MaxSources: 3},
			runner:  &mockRunner{err: fmt.Errorf("model unavailable")},
			want:    "model unavailable",
		},
		{
			name:    "empty queries",
			request: types.EvidenceRequest{ResearchEnabled: true, Topic: "topic", ResearchModel: "model", MaxSources: 3},
			runner:  &mockRunner{content: `{"queries":[]}`},
			want:    "no research queries produced",
		},
		{
			name:    "missing topic",
			request: types.EvidenceRequest{ResearchEnabled: true, ResearchModel: "model", MaxSources: 3},
			runner:  &mockRunner{content: `{"queries":["unused"]}`},
			want:    "topic is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			collector := NewPolicyEvidenceCollector(tt.runner)
			_, err := collector.deriveResearchQueries(tt.request)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error: got %v, want containing %q", err, tt.want)
			}
		})
	}
}

func TestPolicyEvidenceCollectorHaltsResearchBeforeWebCollectionWhenQueriesFail(t *testing.T) {
	collector := NewPolicyEvidenceCollector(&mockRunner{content: `{"queries":[]}`})

	_, err := collector.Collect(types.EvidenceRequest{
		ResearchEnabled: true,
		Topic:           "topic",
		ResearchModel:   "model",
		MaxSources:      3,
	})
	if err == nil || !strings.Contains(err.Error(), "no research queries produced") {
		t.Fatalf("Collect error: got %v, want no research queries produced", err)
	}
}

func TestPolicyEvidenceCollectorCollectsWebEvidenceReferences(t *testing.T) {
	runner := &recordingRunner{responses: []mockResponse{
		{content: `{"queries":["agora web evidence","agora source audit"]}`},
		{content: `{"summary":"web evidence summary","sources":[{"title":"Agora evidence","url":"https://example.com/evidence","query":"agora web evidence"},{"title":"Audit trail","url":"https://example.com/audit","query":"agora source audit"}]}`},
	}}
	collector := NewPolicyEvidenceCollector(runner)

	bundle, err := collector.Collect(types.EvidenceRequest{
		ResearchEnabled: true,
		Topic:           "How should Agora collect web evidence?",
		ResearchModel:   "model",
		MaxSources:      2,
	})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if len(runner.agents) != 2 || runner.agents[0].ID != "research-query-planner" || runner.agents[1].ID != "web-research-collector" {
		t.Fatalf("runner agents: got %#v, want query planner then web collector", runner.agents)
	}
	if got := runner.envelopes[1]["queries"]; !reflect.DeepEqual(got, []string{"agora web evidence", "agora source audit"}) {
		t.Fatalf("web queries envelope: got %#v", got)
	}
	if len(bundle.SourceReferences) != 2 {
		t.Fatalf("SourceReferences: got %#v, want 2", bundle.SourceReferences)
	}
	ref := bundle.SourceReferences[0]
	if ref.Title != "Agora evidence" || ref.URL != "https://example.com/evidence" || ref.Query != "agora web evidence" || ref.RetrievedAt == "" {
		t.Fatalf("reference metadata: got %#v, want title/url/query/retrieved_at", ref)
	}
}

func TestPolicyEvidenceCollectorRejectsWebCollectionFailures(t *testing.T) {
	tests := []struct {
		name        string
		second      mockResponse
		want        string
		wantRunCall int
	}{
		{name: "tools unavailable", second: mockResponse{err: fmt.Errorf("websearch tool unavailable")}, want: "websearch tool unavailable", wantRunCall: 2},
		{name: "malformed response", second: mockResponse{content: `not-json`}, want: "parse failure reading web evidence", wantRunCall: 2},
		{name: "zero references", second: mockResponse{content: `{"summary":"none","sources":[]}`}, want: "no web source references produced", wantRunCall: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runner := &recordingRunner{responses: []mockResponse{
				{content: `{"queries":["agora evidence"]}`},
				tt.second,
			}}
			collector := NewPolicyEvidenceCollector(runner)

			_, err := collector.Collect(types.EvidenceRequest{ResearchEnabled: true, Topic: "topic", ResearchModel: "model", MaxSources: 2})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Collect error: got %v, want containing %q", err, tt.want)
			}
			if runner.callCount != tt.wantRunCall {
				t.Fatalf("runner calls: got %d, want %d", runner.callCount, tt.wantRunCall)
			}
		})
	}
}

func TestParseWebEvidenceRequiresAuditableSources(t *testing.T) {
	bundle, err := parseWebEvidence(`{"summary":"summary","sources":[{"title":"ignored","url":"https://bad.example","query":"other"},{"title":"kept","url":"https://example.com","query":"allowed"}]}`, []string{"allowed"}, 1, "2026-05-05T00:00:00Z")
	if err != nil {
		t.Fatalf("parseWebEvidence: %v", err)
	}
	if len(bundle.SourceReferences) != 1 || bundle.SourceReferences[0].Query != "allowed" || bundle.SourceReferences[0].RetrievedAt == "" {
		t.Fatalf("SourceReferences: got %#v, want one auditable allowed-query reference", bundle.SourceReferences)
	}
}

func TestPolicyEvidenceCollectorDeterministicSmokeVariations(t *testing.T) {
	const topic = "What would the best programming language be to implement this tool?"
	const model = "opencode/nemotron-3-super-free"

	t.Run("online research only", func(t *testing.T) {
		runner := &recordingRunner{responses: []mockResponse{
			{content: `{"queries":["best programming language implement CLI tool"]}`},
			{content: `{"summary":"web summary","sources":[{"title":"Language comparison","url":"https://example.com/languages","query":"best programming language implement CLI tool"}]}`},
		}}
		bundle, err := NewPolicyEvidenceCollector(runner).Collect(types.EvidenceRequest{
			ResearchEnabled: true,
			Topic:           topic,
			ResearchModel:   model,
			MaxSources:      2,
		})
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if len(runner.agents) != 2 || runner.agents[0].Model != model || runner.agents[1].Model != model {
			t.Fatalf("runner agents: got %#v, want query and web research with requested model", runner.agents)
		}
		if runner.envelopes[0]["topic"] != topic || runner.envelopes[1]["topic"] != topic {
			t.Fatalf("topic envelopes: got %#v", runner.envelopes)
		}
		if len(bundle.SourceReferences) != 1 || bundle.SourceReferences[0].URL == "" || bundle.SourceReferences[0].Path != "" {
			t.Fatalf("SourceReferences: got %#v, want web-only reference", bundle.SourceReferences)
		}
	})

	t.Run("local file only", func(t *testing.T) {
		path := writeContextFile(t, t.TempDir(), "README.md", "local context for language choice\n")
		runner := &recordingRunner{}
		bundle, err := NewPolicyEvidenceCollector(runner).Collect(types.EvidenceRequest{
			Topic:         topic,
			ResearchModel: model,
			ContextPaths:  []string{path},
			MaxSources:    2,
		})
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if runner.callCount != 0 {
			t.Fatalf("runner calls: got %d, want 0 for local-only context", runner.callCount)
		}
		if len(bundle.SourceReferences) != 1 || bundle.SourceReferences[0].Path != path || bundle.SourceReferences[0].URL != "" {
			t.Fatalf("SourceReferences: got %#v, want local-only reference", bundle.SourceReferences)
		}
	})

	t.Run("online research and local file", func(t *testing.T) {
		path := writeContextFile(t, t.TempDir(), "README.md", "local context for language choice\n")
		runner := &recordingRunner{responses: []mockResponse{
			{content: `{"queries":["best programming language implement CLI tool"]}`},
			{content: `{"summary":"web summary","sources":[{"title":"Language comparison","url":"https://example.com/languages","query":"best programming language implement CLI tool"}]}`},
		}}
		bundle, err := NewPolicyEvidenceCollector(runner).Collect(types.EvidenceRequest{
			ResearchEnabled: true,
			Topic:           topic,
			ResearchModel:   model,
			ContextPaths:    []string{path},
			MaxSources:      3,
		})
		if err != nil {
			t.Fatalf("Collect: %v", err)
		}
		if len(bundle.SourceReferences) != 2 {
			t.Fatalf("SourceReferences: got %#v, want web plus local references", bundle.SourceReferences)
		}
		if bundle.SourceReferences[0].URL == "" || bundle.SourceReferences[1].Path != path {
			t.Fatalf("SourceReferences: got %#v, want web reference followed by local reference", bundle.SourceReferences)
		}
		if !strings.Contains(bundle.Summary, "web summary") || !strings.Contains(bundle.Summary, "Local text context") {
			t.Fatalf("Summary: got %q, want web and local context summary", bundle.Summary)
		}
	})
}

type recordingRunner struct {
	responses []mockResponse
	callCount int
	agents    []types.AgentConfig
	envelopes []map[string]any
}

func (r *recordingRunner) Run(agent types.AgentConfig, envelope map[string]any) (string, map[string]any, error) {
	r.agents = append(r.agents, agent)
	r.envelopes = append(r.envelopes, envelope)
	idx := r.callCount
	if idx >= len(r.responses) {
		idx = len(r.responses) - 1
	}
	r.callCount++
	response := r.responses[idx]
	if response.err != nil {
		return "", nil, response.err
	}
	return response.content, response.metadata, nil
}

func writeContextFile(t *testing.T, root, rel, content string) string {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func referencePaths(refs []types.SourceReference) []string {
	paths := make([]string, 0, len(refs))
	for _, ref := range refs {
		paths = append(paths, ref.Path)
	}
	return paths
}

func assertContainsPath(t *testing.T, paths []string, want string) {
	t.Helper()
	for _, path := range paths {
		if path == want {
			return
		}
	}
	t.Fatalf("paths %v do not contain %q", paths, want)
}

func assertNotContainsSubstring(t *testing.T, paths []string, substring string) {
	t.Helper()
	for _, path := range paths {
		if strings.Contains(path, substring) {
			t.Fatalf("paths %v contain unsafe substring %q", paths, substring)
		}
	}
}

func assertBoundedContextError(t *testing.T, err error, detail string) {
	t.Helper()
	if err == nil {
		t.Fatal("expected bounded-context error")
	}
	if !strings.Contains(err.Error(), "bounded-context error") || !strings.Contains(err.Error(), detail) {
		t.Fatalf("error: got %q, want bounded-context error containing %q", err, detail)
	}
}
