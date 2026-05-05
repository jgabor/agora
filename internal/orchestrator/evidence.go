package orchestrator

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/types"
)

const researchQuerySystemPrompt = `Derive focused web research queries for an Agora deliberation.

Return only JSON in this exact shape:
{"queries":["query 1","query 2"]}

Rules:
- infer queries only from the supplied topic
- produce no more than max_queries queries
- keep each query concise and suitable for web search
- do not fetch web pages or provide evidence`

const webResearchSystemPrompt = `Collect bounded web evidence for an Agora deliberation.

Use OpenCode's web search and fetch tools to search the supplied queries and fetch useful source pages.

Return only JSON in this exact shape:
{"summary":"concise evidence summary","sources":[{"title":"source title","url":"https://example.com/page","query":"query that found this source"}]}

Rules:
- use only the supplied queries
- produce no more than max_sources total sources
- include only sources found through web search/fetch
- include enough title/url/query metadata for audit
- do not include full source text`

// PolicyEvidenceCollector resolves safe local text context and bounded web evidence.
type PolicyEvidenceCollector struct {
	runner agent.Runner
}

func NewPolicyEvidenceCollector(runner agent.Runner) PolicyEvidenceCollector {
	return PolicyEvidenceCollector{runner: runner}
}

func (c PolicyEvidenceCollector) Collect(request types.EvidenceRequest) (*types.EvidenceBundle, error) {
	bundle := &types.EvidenceBundle{}
	if request.ResearchEnabled {
		queries, err := c.deriveResearchQueries(request)
		if err != nil {
			return nil, err
		}
		webBundle, err := c.collectWebEvidence(request, queries)
		if err != nil {
			return nil, err
		}
		bundle.Summary = webBundle.Summary
		bundle.SourceReferences = append(bundle.SourceReferences, webBundle.SourceReferences...)
	}

	resolver := contextResolver{
		maxSources: positiveIntOrDefault(request.MaxSources, config.DefaultContextMaxSources),
		maxBytes:   positiveInt64OrDefault(request.MaxBytes, config.DefaultContextMaxBytes),
		maxDepth:   positiveIntOrDefault(request.MaxDepth, config.DefaultContextMaxDepth),
	}
	for _, path := range request.ContextPaths {
		if err := resolver.add(path); err != nil {
			return nil, err
		}
	}
	if len(resolver.refs) > 0 {
		if bundle.Summary != "" {
			bundle.Summary += "\n\n"
		}
		bundle.Summary += "Local text context was resolved for pre-deliberation evidence."
		bundle.SourceReferences = append(bundle.SourceReferences, resolver.refs...)
	}
	if bundle.Summary == "" {
		bundle.Summary = "No pre-deliberation evidence sources were resolved."
	}
	return bundle, nil
}

func (c PolicyEvidenceCollector) collectWebEvidence(request types.EvidenceRequest, queries []string) (*types.EvidenceBundle, error) {
	maxSources := positiveIntOrDefault(request.MaxSources, config.DefaultContextMaxSources)
	if c.runner == nil {
		return nil, fmt.Errorf("web research collection: runner unavailable")
	}

	ag := types.AgentConfig{
		ID:           "web-research-collector",
		Model:        request.ResearchModel,
		SystemPrompt: webResearchSystemPrompt,
	}
	envelope := map[string]any{
		"topic":       request.Topic,
		"queries":     queries,
		"max_sources": maxSources,
	}
	content, _, err := c.runner.Run(ag, envelope)
	if err != nil {
		return nil, fmt.Errorf("web research collection: %w", err)
	}

	bundle, err := parseWebEvidence(content, queries, maxSources, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("web research collection: %w", err)
	}
	return &types.EvidenceBundle{
		Summary:          bundle.Summary,
		SourceReferences: bundle.SourceReferences,
	}, nil
}

func parseWebEvidence(content string, queries []string, maxSources int, retrievedAt string) (*types.EvidenceBundle, error) {
	var payload struct {
		Summary string `json:"summary"`
		Sources []struct {
			Title string `json:"title"`
			URL   string `json:"url"`
			Query string `json:"query"`
		} `json:"sources"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &payload); err != nil {
		return nil, fmt.Errorf("parse failure reading web evidence: %w", err)
	}

	allowedQueries := make(map[string]bool, len(queries))
	for _, query := range queries {
		allowedQueries[query] = true
	}
	seenURLs := make(map[string]bool)
	refs := make([]types.SourceReference, 0, min(len(payload.Sources), maxSources))
	for _, source := range payload.Sources {
		title := strings.TrimSpace(source.Title)
		url := strings.TrimSpace(source.URL)
		query := strings.TrimSpace(source.Query)
		if url == "" || query == "" || !allowedQueries[query] || seenURLs[url] {
			continue
		}
		if title == "" {
			title = url
		}
		seenURLs[url] = true
		refs = append(refs, types.SourceReference{Title: title, URL: url, Query: query, RetrievedAt: retrievedAt})
		if len(refs) == maxSources {
			break
		}
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("no web source references produced")
	}
	summary := strings.TrimSpace(payload.Summary)
	if summary == "" {
		summary = "Web research completed with source references."
	}
	return &types.EvidenceBundle{Summary: summary, SourceReferences: refs}, nil
}

func (c PolicyEvidenceCollector) deriveResearchQueries(request types.EvidenceRequest) ([]string, error) {
	maxQueries := positiveIntOrDefault(request.MaxSources, config.DefaultContextMaxSources)
	if strings.TrimSpace(request.Topic) == "" {
		return nil, fmt.Errorf("research query generation: topic is required")
	}
	if strings.TrimSpace(request.ResearchModel) == "" {
		return nil, fmt.Errorf("research query generation: research model is required")
	}
	if c.runner == nil {
		return nil, fmt.Errorf("research query generation: runner unavailable")
	}

	ag := types.AgentConfig{
		ID:           "research-query-planner",
		Model:        request.ResearchModel,
		SystemPrompt: researchQuerySystemPrompt,
	}
	envelope := map[string]any{
		"topic":       request.Topic,
		"max_queries": maxQueries,
	}
	content, _, err := c.runner.Run(ag, envelope)
	if err != nil {
		return nil, fmt.Errorf("research query generation: %w", err)
	}

	queries, err := parseResearchQueries(content, maxQueries)
	if err != nil {
		return nil, fmt.Errorf("research query generation: %w", err)
	}
	return queries, nil
}

func parseResearchQueries(content string, maxQueries int) ([]string, error) {
	var payload struct {
		Queries []string `json:"queries"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &payload); err != nil {
		return nil, fmt.Errorf("parsing queries: %w", err)
	}

	seen := make(map[string]bool)
	queries := make([]string, 0, min(len(payload.Queries), maxQueries))
	for _, query := range payload.Queries {
		query = strings.TrimSpace(query)
		if query == "" || seen[query] {
			continue
		}
		seen[query] = true
		queries = append(queries, query)
		if len(queries) == maxQueries {
			break
		}
	}
	if len(queries) == 0 {
		return nil, fmt.Errorf("no research queries produced")
	}
	return queries, nil
}

func positiveIntOrDefault(value, fallback int) int {
	if value > 0 {
		return value
	}
	return fallback
}

func positiveInt64OrDefault(value, fallback int64) int64 {
	if value > 0 {
		return value
	}
	return fallback
}

type contextResolver struct {
	refs       []types.SourceReference
	maxSources int
	maxBytes   int64
	maxDepth   int
	totalBytes int64
}

func (r *contextResolver) add(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("resolving context %q: %w", path, err)
	}
	if shouldSkipContextPath(info) {
		return nil
	}
	if !info.IsDir() {
		return r.addFile(path, info)
	}

	root := filepath.Clean(path)
	return filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("resolving context %q: %w", current, walkErr)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("resolving context %q: %w", current, err)
		}
		if shouldSkipContextPath(info) {
			if entry.IsDir() && current != root {
				return filepath.SkipDir
			}
			return nil
		}
		depth := contextDepth(root, current)
		if depth > r.maxDepth {
			return fmt.Errorf("bounded-context error: context directory %q exceeds max depth %d at %q", root, r.maxDepth, current)
		}
		if entry.IsDir() {
			return nil
		}
		return r.addFile(current, info)
	})
}

func (r *contextResolver) addFile(path string, info os.FileInfo) error {
	if !info.Mode().IsRegular() || shouldSkipContextPath(info) || !isTextFile(path, info) {
		return nil
	}
	if len(r.refs)+1 > r.maxSources {
		return fmt.Errorf("bounded-context error: context exceeds max files %d at %q", r.maxSources, path)
	}
	if r.totalBytes+info.Size() > r.maxBytes {
		return fmt.Errorf("bounded-context error: context exceeds max bytes %d at %q", r.maxBytes, path)
	}
	r.totalBytes += info.Size()
	r.refs = append(r.refs, types.SourceReference{Title: path, Path: path})
	return nil
}

func contextDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(os.PathSeparator)))
}

func shouldSkipContextPath(info os.FileInfo) bool {
	name := strings.ToLower(info.Name())
	if info.IsDir() {
		switch name {
		case ".git", ".hg", ".svn":
			return true
		}
		return false
	}
	if strings.HasPrefix(name, ".env") || name == "id_rsa" || name == "id_ed25519" {
		return true
	}
	if strings.HasSuffix(name, ".pem") || strings.HasSuffix(name, ".key") {
		return true
	}
	secretWords := []string{"credential", "secret", "token", "password", "private_key"}
	for _, word := range secretWords {
		if strings.Contains(name, word) {
			return true
		}
	}
	return false
}

func isTextFile(path string, info os.FileInfo) bool {
	if info.Size() == 0 {
		return true
	}
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = file.Close() }()

	buf := make([]byte, 8000)
	n, err := file.Read(buf)
	if err != nil && n == 0 {
		return false
	}
	sample := buf[:n]
	if strings.Contains(string(sample), "\x00") {
		return false
	}
	return utf8.Valid(sample)
}
