package config

import "github.com/jgabor/agora/internal/types"

const (
	DefaultContextMaxSources = 20
	DefaultContextMaxBytes   = 1 << 20
	DefaultContextMaxDepth   = 5
)

// ResearchOverrides captures CLI-level evidence policy choices.
type ResearchOverrides struct {
	Research     *bool
	ContextSet   bool
	ContextPaths []string
}

// ResolveEvidenceRequest applies precedence: CLI flags, then config, then settings.
func ResolveEvidenceRequest(cfg *types.DeliberationConfig, settings Settings, overrides ResearchOverrides) types.EvidenceRequest {
	request := types.EvidenceRequest{
		ResearchEnabled: cfg.ResearchEnabled,
		ContextPaths:    append([]string(nil), cfg.ContextPaths...),
		MaxSources:      positiveOrDefault(settings.ResearchMaxSources, DefaultContextMaxSources),
		MaxBytes:        positiveInt64OrDefault(settings.ContextMaxBytes, DefaultContextMaxBytes),
		MaxDepth:        positiveOrDefault(settings.ContextMaxDepth, DefaultContextMaxDepth),
	}

	if overrides.Research != nil {
		request.ResearchEnabled = *overrides.Research
	}
	if overrides.ContextSet {
		request.ContextPaths = append([]string(nil), overrides.ContextPaths...)
	}

	return request
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
