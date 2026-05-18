package output

import (
	"fmt"
	"os"
	"strings"

	"github.com/jgabor/agora/internal/types"
)

// EvidenceSummary prints the pre-deliberation evidence summary and source list.
func (o *OutputManager) EvidenceSummary(evidence types.EvidenceBundle) {
	writeLine(os.Stdout, renderTranscriptEvidence(o.renderer, &evidence, "6"))
}

func renderTranscriptEvidence(r Renderer, evidence *types.EvidenceBundle, color string) string {
	width := outputWidth()
	contentWidth := width - 4
	var sb strings.Builder
	writeSection := sectionWriter(r, &sb, contentWidth)
	writeTranscriptEvidenceSections(writeSection, evidence)
	return r.Panel("Transcript Evidence", sb.String(), width, color)
}

func writeTranscriptEvidenceSections(writeSection func(string, []string), evidence *types.EvidenceBundle) {
	if evidence == nil {
		return
	}
	if summary := strings.TrimSpace(evidence.Summary); summary != "" {
		writeSection("Evidence Summary", strings.Split(summary, "\n"))
	}
	if len(evidence.SourceReferences) == 0 {
		return
	}
	sources := make([]string, 0, len(evidence.SourceReferences))
	for i, source := range evidence.SourceReferences {
		sources = append(sources, transcriptEvidenceSourceLine(i+1, source))
	}
	writeSection("Evidence Sources", sources)
}

func transcriptEvidenceSourceLine(index int, source types.SourceReference) string {
	label := strings.TrimSpace(source.Title)
	if label == "" {
		label = strings.TrimSpace(source.URL)
	}
	if label == "" {
		label = strings.TrimSpace(source.Path)
	}
	if label == "" {
		label = "source"
	}

	var refs []string
	if source.URL != "" {
		refs = append(refs, source.URL)
	}
	if source.Path != "" {
		refs = append(refs, source.Path)
	}
	if source.Query != "" {
		refs = append(refs, "query: "+source.Query)
	}
	line := fmt.Sprintf("%d. %s", index, label)
	if len(refs) > 0 {
		line += " (" + strings.Join(refs, "; ") + ")"
	}
	return line
}
