// Package config loads YAML configuration for Agora deliberation.
package config

import (
	"fmt"
	"os"

	"github.com/jgabor/agora/internal/types"
	"gopkg.in/yaml.v3"
)

// rawConfig mirrors the YAML structure for unmarshaling.
type rawConfig struct {
	Topology           string              `yaml:"topology"`
	Agents             []types.AgentConfig `yaml:"agents"`
	ConsensusThreshold int                 `yaml:"consensus_threshold"`
	SynthesisModel     *string             `yaml:"synthesis_model"`
}

// LoadConfig loads and validates a deliberation configuration from a YAML file.
func LoadConfig(path string) (*types.DeliberationConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}
	return LoadConfigFromBytes(data)
}

// LoadConfigFromBytes parses and validates a deliberation configuration from raw YAML bytes.
func LoadConfigFromBytes(data []byte) (*types.DeliberationConfig, error) {
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	topology := types.TopologyRing
	if raw.Topology != "" {
		t, err := types.ParseTopology(raw.Topology)
		if err != nil {
			return nil, err
		}
		topology = t
	}

	if len(raw.Agents) == 0 {
		return nil, fmt.Errorf("configuration must contain at least one agent")
	}

	cfg := &types.DeliberationConfig{
		Agents:             raw.Agents,
		Topology:           topology,
		ConsensusThreshold: raw.ConsensusThreshold,
		SynthesisModel:     raw.SynthesisModel,
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
