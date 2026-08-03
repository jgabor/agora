package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/types"
)

type artifactMatchTier int

const (
	artifactMatchExact artifactMatchTier = iota
	artifactMatchPrefix
	artifactMatchSubstring
)

func resolveTranscriptArtifact(input, dir string) (string, error) {
	if existingPath(input) {
		return input, nil
	}
	if pathLike(input) {
		return "", fmt.Errorf("transcript path not found: %s", input)
	}

	entries, err := listTranscriptEntries(dir)
	if err != nil {
		return "", err
	}

	if id, err := strconv.Atoi(input); err == nil && id >= 1 && id <= len(entries) {
		entry := entries[id-1]
		return filepath.Join(dir, entry.filename), nil
	}

	for _, tier := range []artifactMatchTier{artifactMatchExact, artifactMatchPrefix, artifactMatchSubstring} {
		for _, entry := range entries {
			if transcriptEntryMatches(entry, input, tier) {
				return filepath.Join(dir, entry.filename), nil
			}
		}
	}
	return "", fmt.Errorf("no matching transcript found for slug %q", input)
}

func transcriptEntryMatches(entry transcriptEntry, input string, tier artifactMatchTier) bool {
	switch tier {
	case artifactMatchExact:
		return entry.slug == input
	case artifactMatchPrefix:
		return strings.HasPrefix(entry.slug, input)
	case artifactMatchSubstring:
		return strings.Contains(entry.slug, input)
	default:
		return false
	}
}

func resolveConfigArtifact(input string, roots []string) (string, error) {
	if existingPath(input) {
		return input, nil
	}
	if pathLike(input) {
		return "", fmt.Errorf("config path not found: %s", input)
	}

	matches, err := configSlugMatches(input, roots)
	if err != nil {
		return "", err
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("config slug %q is ambiguous; candidates: %s", input, strings.Join(matches, ", "))
	}
	return "", fmt.Errorf("no config found for slug %q in %s", input, strings.Join(roots, ", "))
}

func loadConfigArtifact(input string) (*types.DeliberationConfig, string, error) {
	path, err := resolveConfigArtifact(input, configArtifactRoots())
	if err != nil {
		return nil, "", err
	}
	cfg, err := config.LoadConfig(path)
	return cfg, path, err
}

func configArtifactRoots() []string {
	roots := []string{".", "examples"}
	if dir, err := config.ConfigDir(); err == nil {
		roots = appendUniquePath(roots, dir)
	}
	return roots
}

func configSlugMatches(slug string, roots []string) ([]string, error) {
	var matches []string
	seen := make(map[string]bool)
	var globalConfig string
	if path, err := config.GlobalConfigPath(); err == nil {
		globalConfig, _ = filepath.Abs(path)
	}
	for _, root := range roots {
		files, err := os.ReadDir(root)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("reading config search directory %s: %w", root, err)
		}
		for _, file := range files {
			if file.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(file.Name()))
			if ext != ".yaml" && ext != ".yml" {
				continue
			}
			if strings.TrimSuffix(file.Name(), filepath.Ext(file.Name())) == slug {
				candidate := filepath.Join(root, file.Name())
				absolute, err := filepath.Abs(candidate)
				if err != nil {
					return nil, fmt.Errorf("resolving config candidate %s: %w", candidate, err)
				}
				if (globalConfig != "" && absolute == globalConfig) || seen[absolute] {
					continue
				}
				seen[absolute] = true
				matches = append(matches, candidate)
			}
		}
	}
	sort.Strings(matches)
	return matches, nil
}

func appendUniquePath(paths []string, candidate string) []string {
	want, err := filepath.Abs(candidate)
	if err != nil {
		return append(paths, candidate)
	}
	for _, path := range paths {
		got, err := filepath.Abs(path)
		if err == nil && got == want {
			return paths
		}
	}
	return append(paths, candidate)
}

func existingPath(input string) bool {
	if input == "" {
		return false
	}
	_, err := os.Stat(input)
	return err == nil
}

func pathLike(input string) bool {
	return filepath.IsAbs(input) || strings.ContainsRune(input, os.PathSeparator) || strings.ContainsRune(input, '/') || filepath.Ext(input) != ""
}
