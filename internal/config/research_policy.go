package config

import "github.com/jgabor/agora/internal/types"

const (
	DefaultContextMaxSources = 20
	DefaultContextMaxBytes   = 1 << 20
	DefaultContextMaxDepth   = 5
)

// EvidenceDefaults captures fallback caps used when settings.yaml leaves evidence caps unset.
type EvidenceDefaults struct {
	MaxSources int
	MaxBytes   int64
	MaxDepth   int
}

// ResearchOverrides captures CLI-level evidence policy choices.
type ResearchOverrides struct {
	Research     *bool
	ContextSet   bool
	ContextPaths []string
	Defaults     EvidenceDefaults
}

// EvidenceDefaultsForAutoLevel returns larger evidence fallback caps for broader auto runs.
func EvidenceDefaultsForAutoLevel(level types.AutoLevel) EvidenceDefaults {
	switch level {
	case types.AutoNormal:
		return EvidenceDefaults{MaxSources: 40, MaxBytes: 4 << 20, MaxDepth: 6}
	case types.AutoDeep:
		return EvidenceDefaults{MaxSources: 300, MaxBytes: 16 << 20, MaxDepth: 8}
	case types.AutoYOLO:
		return EvidenceDefaults{MaxSources: 1000, MaxBytes: 64 << 20, MaxDepth: 12}
	default:
		return defaultEvidenceDefaults()
	}
}

// ResolveEvidenceRequest applies CLI/config evidence choices plus settings/default caps.
func ResolveEvidenceRequest(cfg *types.DeliberationConfig, settings Settings, overrides ResearchOverrides) types.EvidenceRequest {
	defaults := evidenceDefaultsOrBase(overrides.Defaults)
	request := types.EvidenceRequest{
		ResearchEnabled: cfg.ResearchEnabled,
		ContextPaths:    append([]string(nil), cfg.ContextPaths...),
		MaxSources:      positiveOrDefault(settings.ResearchMaxSources, defaults.MaxSources),
		MaxBytes:        positiveInt64OrDefault(settings.ContextMaxBytes, defaults.MaxBytes),
		MaxDepth:        positiveOrDefault(settings.ContextMaxDepth, defaults.MaxDepth),
	}

	if overrides.Research != nil {
		request.ResearchEnabled = *overrides.Research
	}
	if overrides.ContextSet {
		request.ContextPaths = append([]string(nil), overrides.ContextPaths...)
	}

	return request
}

func defaultEvidenceDefaults() EvidenceDefaults {
	return EvidenceDefaults{MaxSources: DefaultContextMaxSources, MaxBytes: DefaultContextMaxBytes, MaxDepth: DefaultContextMaxDepth}
}

func evidenceDefaultsOrBase(defaults EvidenceDefaults) EvidenceDefaults {
	base := defaultEvidenceDefaults()
	if defaults.MaxSources <= 0 {
		defaults.MaxSources = base.MaxSources
	}
	if defaults.MaxBytes <= 0 {
		defaults.MaxBytes = base.MaxBytes
	}
	if defaults.MaxDepth <= 0 {
		defaults.MaxDepth = base.MaxDepth
	}
	return defaults
}

func positiveOrDefault(value, fallback int) int {
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
