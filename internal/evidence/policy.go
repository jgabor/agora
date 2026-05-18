package evidence

import (
	"github.com/jgabor/agora/internal/types"
)

// Defaults captures fallback caps used when settings.yaml leaves evidence caps unset.
type Defaults struct {
	MaxSources int
	MaxBytes   int64
	MaxDepth   int
}

// Overrides captures CLI-level evidence policy choices.
type Overrides struct {
	Research     *bool
	ContextSet   bool
	ContextPaths []string
	Defaults     Defaults
}

// DefaultsForAutoLevel returns larger evidence fallback caps for broader auto runs.
func DefaultsForAutoLevel(level types.AutoLevel) Defaults {
	switch level {
	case types.AutoNormal:
		return Defaults{MaxSources: 40, MaxBytes: 4 << 20, MaxDepth: 6}
	case types.AutoDeep:
		return Defaults{MaxSources: 300, MaxBytes: 16 << 20, MaxDepth: 8}
	case types.AutoYOLO:
		return Defaults{MaxSources: 1000, MaxBytes: 64 << 20, MaxDepth: 12}
	default:
		return defaultDefaults()
	}
}

// ResolveRequest applies CLI/config evidence choices plus settings/default caps.
// Settings caps are injected as individual primitives rather than importing the
// config package, keeping policy resolution and collection self-contained.
func ResolveRequest(cfg *types.DeliberationConfig, researchMaxSources int, contextMaxBytes int64, contextMaxDepth int, overrides Overrides) types.EvidenceRequest {
	defaults := defaultsOrBase(overrides.Defaults)
	request := types.EvidenceRequest{
		ResearchEnabled: cfg.ResearchEnabled,
		ContextPaths:    append([]string(nil), cfg.ContextPaths...),
		MaxSources:      positiveOrDefault(researchMaxSources, defaults.MaxSources),
		MaxBytes:        positiveInt64OrDefault(contextMaxBytes, defaults.MaxBytes),
		MaxDepth:        positiveOrDefault(contextMaxDepth, defaults.MaxDepth),
	}

	if overrides.Research != nil {
		request.ResearchEnabled = *overrides.Research
	}
	if overrides.ContextSet {
		request.ContextPaths = append([]string(nil), overrides.ContextPaths...)
	}

	return request
}

func defaultDefaults() Defaults {
	return Defaults{MaxSources: DefaultMaxSources, MaxBytes: DefaultMaxBytes, MaxDepth: DefaultMaxDepth}
}

func defaultsOrBase(d Defaults) Defaults {
	base := defaultDefaults()
	if d.MaxSources <= 0 {
		d.MaxSources = base.MaxSources
	}
	if d.MaxBytes <= 0 {
		d.MaxBytes = base.MaxBytes
	}
	if d.MaxDepth <= 0 {
		d.MaxDepth = base.MaxDepth
	}
	return d
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
