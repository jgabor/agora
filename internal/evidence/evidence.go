// Package evidence gathers shared pre-deliberation evidence (web research and local context).
package evidence

import "github.com/jgabor/agora/internal/types"

// Collector gathers shared evidence before the first deliberation turn.
type Collector interface {
	Collect(request types.EvidenceRequest) (*types.EvidenceBundle, error)
}

// Default caps used when config.yaml leaves evidence caps unset.
const (
	DefaultMaxSources = 20
	DefaultMaxBytes   = 1 << 20
	DefaultMaxDepth   = 5
)
