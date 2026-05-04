package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Settings holds global Agora user preferences from settings.yaml.
type Settings struct {
	DefaultModel     string `yaml:"default_model"`
	DefaultAutoLevel string `yaml:"default_auto_level"`
	DefaultTopology  string `yaml:"default_topology"`
	DefaultOutputDir string `yaml:"default_output_dir"`
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
