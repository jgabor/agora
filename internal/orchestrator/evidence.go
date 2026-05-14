package orchestrator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/config"
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
	refs            []types.SourceReference
	documents       []types.ContextDocument
	maxSources      int
	maxBytes        int64
	maxDepth        int
	totalBytes      int64
	ignoredGit      int
	fileCapHit      bool
	byteCapHit      bool
	depthCapHit     bool
	skippedFileCap  int
	skippedByteCap  int
	skippedDepthCap int
	truncatedFiles  int
}

func (r *contextResolver) add(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("resolving context %q: %w", path, err)
	}
	if !info.IsDir() {
		return r.addFile(path, info)
	}
	if shouldAlwaysSkipContextDir(info.Name()) {
		return nil
	}

	root := filepath.Clean(path)
	ignore := newGitignoreMatcher(root)
	return filepath.WalkDir(root, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("resolving context %q: %w", current, walkErr)
		}
		info, err := entry.Info()
		if err != nil {
			return fmt.Errorf("resolving context %q: %w", current, err)
		}
		if entry.IsDir() && shouldAlwaysSkipContextDir(entry.Name()) {
			if current != root {
				return filepath.SkipDir
			}
			return nil
		}
		if current != root && ignore.ignored(current, entry.IsDir()) {
			r.ignoredGit++
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		depth := contextDepth(root, current)
		if depth > r.maxDepth {
			r.depthCapHit = true
			r.skippedDepthCap++
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		return r.addFile(current, info)
	})
}

func (r *contextResolver) addFile(path string, info os.FileInfo) error {
	if !info.Mode().IsRegular() || shouldSkipContextFile(info.Name()) {
		return nil
	}
	if len(r.refs) >= r.maxSources {
		r.fileCapHit = true
		r.skippedFileCap++
		return nil
	}
	remaining := r.maxBytes - r.totalBytes
	if remaining <= 0 {
		r.byteCapHit = true
		r.skippedByteCap++
		return nil
	}
	if !isTextFile(path, info) {
		return nil
	}
	text, truncated, err := readContextText(path, remaining)
	if err != nil {
		return fmt.Errorf("resolving context %q: %w", path, err)
	}
	if strings.Contains(text, "\x00") || !utf8.ValidString(text) {
		return nil
	}
	actualBytes := int64(len([]byte(text)))
	if actualBytes == 0 && info.Size() > 0 {
		r.byteCapHit = true
		r.skippedByteCap++
		return nil
	}
	if truncated {
		r.byteCapHit = true
		r.truncatedFiles++
	}
	r.totalBytes += actualBytes
	r.refs = append(r.refs, types.SourceReference{Title: path, Path: path})
	r.documents = append(r.documents, types.ContextDocument{Path: path, Content: text})
	return nil
}

func readContextText(path string, byteLimit int64) (string, bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", false, err
	}
	if info.Size() <= byteLimit {
		content, err := os.ReadFile(path)
		return string(content), false, err
	}
	if byteLimit <= 0 {
		return "", true, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return "", true, err
	}
	defer func() { _ = file.Close() }()
	content := make([]byte, int(byteLimit))
	n, err := file.Read(content)
	if err != nil && n == 0 {
		return "", true, err
	}
	content = validUTF8Prefix(content[:n])
	return string(content), true, nil
}

func validUTF8Prefix(content []byte) []byte {
	for len(content) > 0 && !utf8.Valid(content) {
		content = content[:len(content)-1]
	}
	return content
}

func contextDepth(root, path string) int {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." {
		return 0
	}
	return len(strings.Split(rel, string(os.PathSeparator)))
}

func (r *contextResolver) summary(requestedPaths int) string {
	lines := []string{
		fmt.Sprintf("Local text context summary: included %d file(s), delivered %d byte(s), requested path(s): %d.", len(r.refs), r.totalBytes, requestedPaths),
		fmt.Sprintf("Local context caps: max_files=%d, max_bytes=%d, max_depth=%d.", r.maxSources, r.maxBytes, r.maxDepth),
	}
	if r.ignoredGit > 0 {
		lines = append(lines, fmt.Sprintf("Ignored %d path(s) matched by .gitignore.", r.ignoredGit))
	}
	if r.fileCapHit {
		lines = append(lines, fmt.Sprintf("WARNING: local context file cap reached at %d file(s); omitted %d additional candidate file(s).", r.maxSources, r.skippedFileCap))
	}
	if r.byteCapHit {
		lines = append(lines, fmt.Sprintf("WARNING: local context byte cap reached at %d byte(s); truncated %d file(s) and omitted %d additional candidate file(s).", r.maxBytes, r.truncatedFiles, r.skippedByteCap))
	}
	if r.depthCapHit {
		lines = append(lines, fmt.Sprintf("WARNING: local context depth cap reached at depth %d; skipped %d deeper path(s).", r.maxDepth, r.skippedDepthCap))
	}
	return strings.Join(lines, "\n")
}

func shouldAlwaysSkipContextDir(name string) bool {
	lower := strings.ToLower(name)
	switch lower {
	case ".git", ".hg", ".svn":
		return true
	}
	return false
}

func shouldSkipContextFile(name string) bool {
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, ".env") || lower == "id_rsa" || lower == "id_ed25519" {
		return true
	}
	if strings.HasSuffix(lower, ".pem") || strings.HasSuffix(lower, ".key") {
		return true
	}
	secretWords := []string{"credential", "secret", "token", "password", "private_key"}
	for _, word := range secretWords {
		if strings.Contains(lower, word) {
			return true
		}
	}
	return false
}

type gitignoreMatcher struct {
	root  string
	cache map[string][]gitignoreRule
}

type gitignoreRule struct {
	base     string
	pattern  string
	negated  bool
	dirOnly  bool
	anchored bool
	hasSlash bool
}

func newGitignoreMatcher(contextRoot string) *gitignoreMatcher {
	root := gitRootForContext(contextRoot)
	matcher := &gitignoreMatcher{root: root, cache: make(map[string][]gitignoreRule)}
	matcher.cache[filepath.Dir(root)] = nil
	return matcher
}

func gitRootForContext(contextRoot string) string {
	path := filepath.Clean(contextRoot)
	for {
		if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return filepath.Clean(contextRoot)
		}
		path = parent
	}
}

func (m *gitignoreMatcher) ignored(path string, isDir bool) bool {
	if m == nil {
		return false
	}
	rules := m.rulesFor(filepath.Dir(path))
	ignored := false
	for _, rule := range rules {
		if rule.matches(path, isDir) {
			ignored = !rule.negated
		}
	}
	return ignored
}

func (m *gitignoreMatcher) rulesFor(dir string) []gitignoreRule {
	dir = filepath.Clean(dir)
	if rules, ok := m.cache[dir]; ok {
		return rules
	}
	parent := filepath.Dir(dir)
	if !pathWithin(parent, m.root) && parent != filepath.Dir(m.root) {
		m.cache[dir] = nil
		return nil
	}
	rules := append([]gitignoreRule(nil), m.rulesFor(parent)...)
	rules = append(rules, parseGitignoreFile(dir)...)
	m.cache[dir] = rules
	return rules
}

func parseGitignoreFile(dir string) []gitignoreRule {
	content, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		return nil
	}
	lines := strings.Split(string(content), "\n")
	rules := make([]gitignoreRule, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		rule := gitignoreRule{base: dir}
		if strings.HasPrefix(line, "!") {
			rule.negated = true
			line = strings.TrimSpace(strings.TrimPrefix(line, "!"))
		}
		if strings.HasPrefix(line, "/") {
			rule.anchored = true
			line = strings.TrimPrefix(line, "/")
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		line = filepath.ToSlash(filepath.Clean(line))
		if line == "." || line == "" {
			continue
		}
		rule.pattern = line
		rule.hasSlash = strings.Contains(line, "/")
		rules = append(rules, rule)
	}
	return rules
}

func (r gitignoreRule) matches(path string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}
	rel, err := filepath.Rel(r.base, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return false
	}
	rel = filepath.ToSlash(rel)
	if !r.anchored && !r.hasSlash {
		return matchGitignorePattern(r.pattern, pathpkg.Base(rel))
	}
	return matchGitignorePattern(r.pattern, rel)
}

func matchGitignorePattern(pattern, target string) bool {
	pattern = strings.TrimPrefix(filepath.ToSlash(pattern), "/")
	target = filepath.ToSlash(target)
	if strings.Contains(pattern, "**") {
		return matchGitignoreSegments(strings.Split(pattern, "/"), strings.Split(target, "/"))
	}
	matched, err := pathpkg.Match(pattern, target)
	return err == nil && matched
}

func matchGitignoreSegments(pattern, target []string) bool {
	if len(pattern) == 0 {
		return len(target) == 0
	}
	if pattern[0] == "**" {
		if len(pattern) == 1 {
			return true
		}
		for i := 0; i <= len(target); i++ {
			if matchGitignoreSegments(pattern[1:], target[i:]) {
				return true
			}
		}
		return false
	}
	if len(target) == 0 {
		return false
	}
	matched, err := pathpkg.Match(pattern[0], target[0])
	return err == nil && matched && matchGitignoreSegments(pattern[1:], target[1:])
}

func pathWithin(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
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
