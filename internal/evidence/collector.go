package evidence

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/types"
)

var codeFenceRe = regexp.MustCompile("(?s)```(?:ya?ml|json)?\\s*\n(.*?)```")

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

// PolicyCollector resolves safe local text context and bounded web evidence.
type PolicyCollector struct {
	runner agent.Runner
}

// NewPolicyCollector creates a new PolicyCollector.
func NewPolicyCollector(runner agent.Runner) PolicyCollector {
	return PolicyCollector{runner: runner}
}

// Collect resolves and collects pre-deliberation evidence.
func (c PolicyCollector) Collect(request types.EvidenceRequest) (*types.EvidenceBundle, error) {
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
		maxSources: positiveOrDefault(request.MaxSources, DefaultMaxSources),
		maxBytes:   positiveInt64OrDefault(request.MaxBytes, DefaultMaxBytes),
		maxDepth:   positiveOrDefault(request.MaxDepth, DefaultMaxDepth),
	}
	for _, path := range request.ContextPaths {
		if err := resolver.add(path); err != nil {
			return nil, err
		}
	}
	if len(request.ContextPaths) > 0 {
		if bundle.Summary != "" {
			bundle.Summary += "\n\n"
		}
		bundle.Summary += resolver.summary(len(request.ContextPaths))
	}
	if len(resolver.refs) > 0 {
		bundle.SourceReferences = append(bundle.SourceReferences, resolver.refs...)
		bundle.ContextDocuments = append(bundle.ContextDocuments, resolver.documents...)
	}
	if bundle.Summary == "" {
		bundle.Summary = "No pre-deliberation evidence sources were resolved."
	}
	return bundle, nil
}

func (c PolicyCollector) collectWebEvidence(request types.EvidenceRequest, queries []string) (*types.EvidenceBundle, error) {
	maxSources := positiveOrDefault(request.MaxSources, DefaultMaxSources)
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
	content, _, err := c.runner.Run(agent.WithReadOnlyAgentPrompt(ag), envelope)
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
	cleaned := stripCodeFences(strings.TrimSpace(content))
	if err := decodeFirstJSONObject(cleaned, &payload); err != nil {
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

func stripCodeFences(s string) string {
	locs := codeFenceRe.FindStringSubmatch(s)
	if len(locs) >= 2 {
		return strings.TrimSpace(locs[1])
	}
	return strings.TrimSpace(s)
}

func decodeFirstJSONObject(content string, payload any) error {
	cleaned := []byte(strings.TrimSpace(content))
	start := bytes.IndexByte(cleaned, '{')
	if start < 0 {
		return fmt.Errorf("no JSON object found")
	}
	decoder := json.NewDecoder(bytes.NewReader(cleaned[start:]))
	return decoder.Decode(payload)
}

func (c PolicyCollector) deriveResearchQueries(request types.EvidenceRequest) ([]string, error) {
	maxQueries := positiveOrDefault(request.MaxSources, DefaultMaxSources)
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
	content, _, err := c.runner.Run(agent.WithReadOnlyAgentPrompt(ag), envelope)
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
	cleaned := stripCodeFences(strings.TrimSpace(content))
	if err := decodeFirstJSONObject(cleaned, &payload); err != nil {
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
