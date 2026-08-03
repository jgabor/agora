package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jgabor/agora/internal/types"
	"github.com/spf13/cobra"
)

func resolveWorkdir(cmd *cobra.Command, flags runFlagValues, cfg *types.DeliberationConfig, configPath string) (string, error) {
	launchDir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("resolving launch directory: %w", err)
	}

	path := launchDir
	base := launchDir
	if cfg != nil && cfg.Workdir != "" {
		path = cfg.Workdir
		if configPath != "" {
			absoluteConfig, err := filepath.Abs(configPath)
			if err != nil {
				return "", fmt.Errorf("resolving config path: %w", err)
			}
			base = filepath.Dir(absoluteConfig)
		}
	}
	if cmd.Flags().Changed("workdir") {
		path = flags.Workdir
		base = launchDir
	}
	if path == "" {
		return "", fmt.Errorf("workdir must not be empty")
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolving workdir %q: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("resolving workdir %q: %w", path, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir %q is not a directory", path)
	}
	return filepath.Clean(path), nil
}

func resolveContextPaths(paths []string, workdir string) []string {
	resolved := make([]string, len(paths))
	for i, path := range paths {
		if filepath.IsAbs(path) {
			resolved[i] = filepath.Clean(path)
			continue
		}
		resolved[i] = filepath.Join(workdir, path)
	}
	return resolved
}
