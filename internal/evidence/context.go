package evidence

import (
	"fmt"
	"io/fs"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/jgabor/agora/internal/types"
)

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
