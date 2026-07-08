// Package config loads YAML configuration for Agora deliberation.
package config

import (
	"fmt"
	"os"

	"github.com/jgabor/agora/internal/types"
	"gopkg.in/yaml.v3"
)

const defaultMinRounds = 3

// rawConfig mirrors the YAML structure for unmarshaling.
type rawConfig struct {
	Topology           string              `yaml:"topology"`
	Agents             []types.AgentConfig `yaml:"agents"`
	ConsensusThreshold int                 `yaml:"consensus_threshold"`
	MinRounds          int                 `yaml:"min_rounds"`
	SynthesisModel     *string             `yaml:"synthesis_model"`
	Research           *bool               `yaml:"research"`
	Ledger             *bool               `yaml:"ledger"`
	Context            []string            `yaml:"context"`
}

// LoadConfig loads and validates a deliberation configuration from a YAML file.
func LoadConfig(path string) (*types.DeliberationConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	gconf, err := LoadDefaultGlobalConfig()
	if err != nil {
		return nil, err
	}
	return loadConfigFromBytes(data, gconf, true)
}

// LoadConfigFromBytes parses and validates a deliberation configuration from raw YAML bytes.
func LoadConfigFromBytes(data []byte) (*types.DeliberationConfig, error) {
	return loadConfigFromBytes(data, Config{}, false)
}

func loadConfigFromBytes(data []byte, gconf Config, applyNonAutoDefaults bool) (*types.DeliberationConfig, error) {
	var raw rawConfig
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	topology := types.TopologyRing
	topologySource := raw.Topology
	if topologySource == "" {
		topologySource = gconf.DefaultTopology
	}
	if topologySource != "" {
		t, err := types.ParseTopology(topologySource)
		if err != nil {
			return nil, err
		}
		topology = t
	}

	if len(raw.Agents) == 0 {
		return nil, fmt.Errorf("configuration must contain at least one agent")
	}

	agents := raw.Agents
	for i := range agents {
		if agents[i].Model == "" && gconf.DefaultModel != "" {
			agents[i].Model = gconf.DefaultModel
		}
	}

	cfg := &types.DeliberationConfig{
		Agents:             agents,
		Topology:           topology,
		ConsensusThreshold: raw.ConsensusThreshold,
		MinRounds:          raw.MinRounds,
		SynthesisModel:     raw.SynthesisModel,
		Ledger:             raw.Ledger,
		ContextPaths:       append([]string(nil), raw.Context...),
	}
	if raw.Research != nil {
		cfg.ResearchEnabled = *raw.Research
	}

	if applyNonAutoDefaults {
		if cfg.ConsensusThreshold == 0 {
			cfg.ConsensusThreshold = len(cfg.Agents)
		}
		if cfg.MinRounds == 0 {
			cfg.MinRounds = defaultMinRounds
		}
	}

	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}
