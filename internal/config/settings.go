package config

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// Settings holds global Agora user preferences from settings.yaml.
type Settings struct {
	DefaultModel         string `yaml:"default_model,omitempty"`
	DefaultAutoLevel     string `yaml:"default_auto_level,omitempty"`
	DefaultTopology      string `yaml:"default_topology,omitempty"`
	DefaultOutputDir     string `yaml:"default_output_dir,omitempty"`
	DefaultLedgerEnabled *bool  `yaml:"default_ledger_enabled,omitempty"`
	ResearchMaxSources   int    `yaml:"research_max_sources,omitempty"`
	ContextMaxBytes      int64  `yaml:"context_max_bytes,omitempty"`
	ContextMaxDepth      int    `yaml:"context_max_depth,omitempty"`
}

// LoadDefaultSettings loads settings.yaml from the default global config path.
func LoadDefaultSettings() (Settings, error) {
	path, err := SettingsPath()
	if err != nil {
		return Settings{}, err
	}
	return LoadSettings(path)
}

// LoadSettings loads a settings.yaml file. Missing files return zero settings.
func LoadSettings(path string) (Settings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return Settings{}, nil
		}
		return Settings{}, fmt.Errorf("reading settings file: %w", err)
	}

	var settings Settings
	if err := yaml.Unmarshal(data, &settings); err != nil {
		return Settings{}, fmt.Errorf("parsing settings YAML: %w", err)
	}
	return settings, nil
}

// SaveDefaultSettings writes settings.yaml to the default global config path.
func SaveDefaultSettings(settings Settings) error {
	path, err := SettingsPath()
	if err != nil {
		return err
	}
	return SaveSettings(path, settings)
}

// SaveSettings writes a settings.yaml file, creating the parent directory.
func SaveSettings(path string, settings Settings) error {
	data, err := yaml.Marshal(&settings)
	if err != nil {
		return fmt.Errorf("marshaling settings YAML: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating settings directory: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("writing settings file: %w", err)
	}
	return nil
}
