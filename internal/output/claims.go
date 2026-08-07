package output

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jgabor/agora/internal/types"
)

// ClaimEvidenceLines renders canonical claim metadata with both its reasoning
// kind and evidence status visible. The value may be a typed slice from a live
// synthesis result or a JSON-decoded slice from a persisted transcript.
func ClaimEvidenceLines(value any) []string {
	claims := claimEvidenceValues(value)
	lines := make([]string, 0, len(claims))
	for _, claim := range claims {
		kind := strings.ToUpper(string(claim.Kind))
		if kind == "" {
			kind = "UNKNOWN"
		}
		status := strings.ToUpper(string(claim.Status))
		if status == "" {
			status = string(types.EvidenceUnverified)
			status = strings.ToUpper(status)
		}
		status = strings.ReplaceAll(status, "_", " ")
		refs := "none supplied"
		if len(claim.SourceRefs) > 0 {
			values := make([]string, len(claim.SourceRefs))
			for i, ref := range claim.SourceRefs {
				values[i] = fmt.Sprintf("#%d", ref+1)
			}
			refs = strings.Join(values, ", ")
		}
		decisive := "non-decisive"
		if claim.Decisive {
			decisive = "decisive"
		}
		lines = append(lines, fmt.Sprintf("[%s] [%s] %s (proposal v%d; %s; source refs: %s)", kind, status, claim.ID, claim.ProposalVersion, decisive, refs))
	}
	return lines
}

func claimEvidenceValues(value any) []types.ClaimEvidence {
	if value == nil {
		return nil
	}
	if claims, ok := value.([]types.ClaimEvidence); ok {
		return claims
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var claims []types.ClaimEvidence
	if err := json.Unmarshal(data, &claims); err != nil {
		return nil
	}
	return claims
}
