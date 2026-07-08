package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Config holds global Agora user preferences from config.yaml.
type Config struct {
	DefaultModel         string `yaml:"default_model,omitempty"`
	DefaultAutoLevel     string `yaml:"default_auto_level,omitempty"`
	DefaultTopology      string `yaml:"default_topology,omitempty"`
	DefaultOutputDir     string `yaml:"default_output_dir,omitempty"`
	DefaultLedgerEnabled *bool  `yaml:"default_ledger_enabled,omitempty"`
	ResearchMaxSources   int    `yaml:"research_max_sources,omitempty"`
	ContextMaxBytes      int64  `yaml:"context_max_bytes,omitempty"`
	ContextMaxDepth      int    `yaml:"context_max_depth,omitempty"`
}

// LoadDefaultGlobalConfig loads config.yaml from the default global config path.
func LoadDefaultGlobalConfig() (Config, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return Config{}, err
	}
	return LoadGlobalConfig(path)
}

// LoadGlobalConfig loads a config.yaml file. Missing files return zero config.
func LoadGlobalConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("reading config file: %w", err)
	}

	var gconf Config
	if err := yaml.Unmarshal(data, &gconf); err != nil {
		return Config{}, fmt.Errorf("parsing config YAML: %w", err)
	}
	return gconf, nil
}

// SaveDefaultGlobalConfig writes config.yaml to the default global config path.
func SaveDefaultGlobalConfig(gconf Config) error {
	path, err := GlobalConfigPath()
	if err != nil {
		return err
	}
	return SaveGlobalConfig(path, gconf)
}

// SaveGlobalConfig writes a config.yaml file, creating the parent directory.
func SaveGlobalConfig(path string, gconf Config) error {
	data, err := yaml.Marshal(&gconf)
	if err != nil {
		return fmt.Errorf("marshaling config YAML: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}
	return nil
}
