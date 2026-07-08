package main

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/types"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	formatText     = "text"
	formatJSON     = "json"
	formatMarkdown = "markdown"
	schemaVersion  = 1
)

var validFormats = []string{formatText, formatJSON, formatMarkdown}

func addFormatFlag(cmd *cobra.Command, target *string) {
	*target = formatText
	cmd.Flags().StringVar(target, "format", formatText, "output format (text, json, markdown)")
}

func validateFormat(format string) error {
	switch format {
	case formatText, formatJSON, formatMarkdown:
		return nil
	default:
		return fmt.Errorf("invalid format %q; valid values: %s", format, strings.Join(validFormats, ", "))
	}
}

func writeJSON(w io.Writer, command string, data any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{
		"schema_version": schemaVersion,
		"command":        command,
		"data":           data,
	})
}

func writeMarkdown(w io.Writer, title string, rows [][]string) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", title)
	for _, row := range rows {
		if len(row) < 2 {
			continue
		}
		fmt.Fprintf(&sb, "- **%s:** %s\n", row[0], row[1])
	}
	_, err := fmt.Fprint(w, sb.String())
	return err
}

func redactedSettingValue(key, value string) string {
	lower := strings.ToLower(key)
	secretWords := []string{"secret", "token", "password", "credential", "api_key", "apikey", "private_key"}
	for _, word := range secretWords {
		if strings.Contains(lower, word) {
			if value == "" || value == "(unset)" {
				return value
			}
			return "[redacted]"
		}
	}
	if secretLikeSettingValue(value) {
		return "[redacted]"
	}
	return value
}

func secretLikeSettingValue(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed == "(unset)" {
		return false
	}
	lower := strings.ToLower(trimmed)
	for _, prefix := range []string{"sk-", "pk-", "ghp_", "github_pat_", "xoxb-", "xoxp-", "ya29."} {
		if strings.HasPrefix(lower, prefix) && len(trimmed) >= 16 {
			return true
		}
	}
	if strings.HasPrefix(trimmed, "AKIA") && len(trimmed) >= 20 {
		return true
	}
	if strings.Contains(lower, "-----begin") && strings.Contains(lower, "private key-----") {
		return true
	}
	if !containsAny(lower, []string{"secret", "token", "password", "credential", "api_key", "apikey", "private_key"}) {
		return false
	}
	return len(trimmed) >= 16 && containsDigit(trimmed) && strings.ContainsAny(trimmed, "-_.")
}

func containsAny(value string, needles []string) bool {
	for _, needle := range needles {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func containsDigit(value string) bool {
	for _, r := range value {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

type configValue struct {
	Group                string   `json:"group"`
	Key                  string   `json:"key"`
	Type                 string   `json:"type"`
	Value                string   `json:"value"`
	Source               string   `json:"source"`
	Default              string   `json:"default"`
	EffectiveValuePolicy string   `json:"effective_value_policy"`
	AllowedValues        []string `json:"allowed_values,omitempty"`
	Description          string   `json:"description"`
}

func effectiveConfigRows(gconf config.Config) ([]configValue, string, error) {
	path, err := config.GlobalConfigPath()
	if err != nil {
		return nil, "", err
	}
	rows := make([]configValue, 0, len(configKeyDefs))
	for _, def := range configKeyDefs {
		value, explicit, err := effectiveConfigValue(def, gconf)
		if err != nil {
			return nil, "", err
		}
		defaultValue, err := settingDefaultValue(def)
		if err != nil {
			return nil, "", err
		}
		source := "set"
		if !explicit {
			if value == "" {
				value = "(unset)"
				source = "unset"
			} else {
				source = "default"
			}
		}
		rows = append(rows, configValue{
			Group:                def.Group,
			Key:                  def.Key,
			Type:                 def.Type,
			Value:                redactedSettingValue(def.Key, value),
			Source:               source,
			Default:              redactedSettingValue(def.Key, defaultValue),
			EffectiveValuePolicy: configValuePolicy(def),
			AllowedValues:        append([]string(nil), def.Allowed...),
			Description:          def.Description,
		})
	}
	return rows, path, nil
}

func configAllData(gconf config.Config) (map[string]any, error) {
	values, path, err := effectiveConfigRows(gconf)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"path":      path,
		"config":    values,
		"redaction": "secret-like config keys and values are reported as [redacted] when set",
		"source_values": []string{
			"set: explicitly configured in config.yaml",
			"default: effective value supplied by Agora defaults",
			"unset: no configured value and no default",
		},
	}, nil
}

func writeConfigMarkdown(w io.Writer, data map[string]any) error {
	values, _ := data["config"].([]configValue)
	var sb strings.Builder
	fmt.Fprintln(&sb, "# Global Config")
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "- **Path:** `%v`\n", data["path"])
	fmt.Fprintf(&sb, "- **Redaction:** %v\n", data["redaction"])
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## Effective Config")
	for _, value := range values {
		line := fmt.Sprintf("- `%s`: `%s` (source `%s`; type `%s`; policy: %s; default `%s`", value.Key, value.Value, value.Source, value.Type, value.EffectiveValuePolicy, value.Default)
		if len(value.AllowedValues) > 0 {
			line += fmt.Sprintf("; allowed `%s`", strings.Join(value.AllowedValues, "`, `"))
		}
		line += fmt.Sprintf(") - %s", value.Description)
		fmt.Fprintln(&sb, line)
	}
	_, err := fmt.Fprint(w, sb.String())
	return err
}

type transcriptEntryOutput struct {
	ID       int    `json:"id"`
	Date     string `json:"date"`
	Turns    int    `json:"turns"`
	Slug     string `json:"slug"`
	Filename string `json:"filename"`
}

type transcriptListOutput struct {
	StorePath       string                  `json:"store_path"`
	TranscriptCount int                     `json:"transcript_count"`
	Empty           bool                    `json:"empty"`
	Transcripts     []transcriptEntryOutput `json:"transcripts"`
}

func transcriptEntriesOutput(entries []transcriptEntry) []transcriptEntryOutput {
	out := make([]transcriptEntryOutput, 0, len(entries))
	for _, entry := range entries {
		out = append(out, transcriptEntryOutput{
			ID:       entry.id,
			Date:     entry.date.Format("2006-01-02 15:04:05"),
			Turns:    entry.turns,
			Slug:     entry.slug,
			Filename: entry.filename,
		})
	}
	return out
}

func transcriptListData(dir string, entries []transcriptEntry) transcriptListOutput {
	entryData := transcriptEntriesOutput(entries)
	return transcriptListOutput{
		StorePath:       dir,
		TranscriptCount: len(entryData),
		Empty:           len(entryData) == 0,
		Transcripts:     entryData,
	}
}

func writeTranscriptListMarkdown(w io.Writer, data transcriptListOutput) error {
	var sb strings.Builder
	fmt.Fprintln(&sb, "# Managed Transcripts")
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "- **Store path:** `%s`\n", data.StorePath)
	fmt.Fprintf(&sb, "- **Transcript count:** %d\n", data.TranscriptCount)
	fmt.Fprintf(&sb, "- **Empty:** %t\n", data.Empty)
	if len(data.Transcripts) == 0 {
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "No transcripts found.")
		_, err := fmt.Fprint(w, sb.String())
		return err
	}
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "| # | Date | Turns | Slug | Filename |")
	fmt.Fprintln(&sb, "| ---: | --- | ---: | --- | --- |")
	for i, entry := range data.Transcripts {
		fmt.Fprintf(&sb, "| %d | %s | %d | %s | `%s` |\n", i+1, entry.Date, entry.Turns, entry.Slug, entry.Filename)
	}
	_, err := fmt.Fprint(w, sb.String())
	return err
}

type commandErrorOutput struct {
	OK      bool   `json:"ok"`
	Input   string `json:"input,omitempty"`
	Path    string `json:"path,omitempty"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func commandErrorData(input, path, code string, err error) commandErrorOutput {
	return commandErrorOutput{OK: false, Input: input, Path: path, Code: code, Message: err.Error()}
}

func writeCommandErrorMarkdown(w io.Writer, title string, data commandErrorOutput) error {
	rows := [][]string{{"OK", fmt.Sprint(data.OK)}, {"Code", data.Code}, {"Message", data.Message}}
	if data.Input != "" {
		rows = append(rows, []string{"Input", data.Input})
	}
	if data.Path != "" {
		rows = append(rows, []string{"Path", data.Path})
	}
	return writeMarkdown(w, title, rows)
}

func writeFormattedCommandError(cmd *cobra.Command, format, command, title string, data commandErrorOutput) {
	switch format {
	case formatJSON:
		_ = writeJSON(cmd.OutOrStdout(), command, data)
	case formatMarkdown:
		_ = writeCommandErrorMarkdown(cmd.OutOrStdout(), title, data)
	}
}

type transcriptShowOutput struct {
	DocumentType          string                   `json:"document_type"`
	DocumentSchemaVersion int                      `json:"document_schema_version"`
	Path                  string                   `json:"path"`
	RecordCount           int                      `json:"record_count"`
	Agents                []string                 `json:"agents"`
	Metadata              transcriptMetadataOutput `json:"metadata,omitempty"`
	Records               []transcriptRecordOutput `json:"records"`
	EvidenceBoundary      string                   `json:"evidence_boundary"`
}

type transcriptMetadataOutput struct {
	Available          bool               `json:"available"`
	SchemaVersion      int                `json:"schema_version,omitempty"`
	Cast               []types.CastMember `json:"cast,omitempty"`
	Topology           types.Topology     `json:"topology,omitempty"`
	ConsensusThreshold int                `json:"consensus_threshold,omitempty"`
	MinRounds          int                `json:"min_rounds,omitempty"`
	SynthesisModel     *string            `json:"synthesis_model,omitempty"`
	Research           bool               `json:"research"`
	ContextPaths       []string           `json:"context_paths,omitempty"`
}

type transcriptRecordOutput struct {
	RecordIndex        int                       `json:"record_index"`
	Turn               int                       `json:"turn"`
	AgentID            string                    `json:"agent_id"`
	Model              string                    `json:"model,omitempty"`
	Timestamp          float64                   `json:"timestamp"`
	Content            string                    `json:"content,omitempty"`
	Evidence           *transcriptEvidenceOutput `json:"evidence,omitempty"`
	Consensus          bool                      `json:"consensus"`
	ConsensusStatement string                    `json:"consensus_statement,omitempty"`
	CastMember         *types.CastMember         `json:"cast_member,omitempty"`
}

type transcriptEvidenceOutput struct {
	Summary                  string                  `json:"summary,omitempty"`
	SourceReferences         []types.SourceReference `json:"source_references,omitempty"`
	ContextDocumentRefs      []string                `json:"context_document_refs,omitempty"`
	FullSourceContentOmitted bool                    `json:"full_source_content_omitted"`
}

func transcriptShowData(path string, records []types.TurnRecord) transcriptShowOutput {
	metadata := transcriptMetadataFromRecords(records)
	return transcriptShowOutput{
		DocumentType:          "agora.transcript.show",
		DocumentSchemaVersion: 1,
		Path:                  path,
		RecordCount:           len(records),
		Agents:                transcriptAgents(records),
		Metadata:              transcriptMetadataData(metadata),
		Records:               transcriptRecordData(records, metadata),
		EvidenceBoundary:      "Evidence source contents are not included; output preserves transcript references and summaries only.",
	}
}

func transcriptMetadataData(metadata *types.TranscriptMetadata) transcriptMetadataOutput {
	if metadata == nil {
		return transcriptMetadataOutput{Available: false}
	}
	out := transcriptMetadataOutput{Available: true, SchemaVersion: metadata.SchemaVersion, Cast: append([]types.CastMember(nil), metadata.Cast...)}
	if metadata.Config != nil {
		out.Topology = metadata.Config.Topology
		out.ConsensusThreshold = metadata.Config.ConsensusThreshold
		out.MinRounds = metadata.Config.MinRounds
		out.SynthesisModel = metadata.Config.SynthesisModel
		out.Research = metadata.Config.ResearchEnabled
		out.ContextPaths = append([]string(nil), metadata.Config.ContextPaths...)
	}
	return out
}

func transcriptRecordData(records []types.TurnRecord, metadata *types.TranscriptMetadata) []transcriptRecordOutput {
	castByAgent := map[string]types.CastMember{}
	if metadata != nil {
		for _, member := range metadata.Cast {
			castByAgent[member.Persona] = member
		}
	}
	out := make([]transcriptRecordOutput, 0, len(records))
	for i, record := range records {
		entry := transcriptRecordOutput{
			RecordIndex:        i + 1,
			Turn:               record.Turn,
			AgentID:            strings.TrimSpace(record.AgentID),
			Timestamp:          record.Timestamp,
			Content:            strings.TrimSpace(record.Content),
			Consensus:          record.Consensus,
			ConsensusStatement: strings.TrimSpace(record.ConsensusStatement),
		}
		if entry.AgentID == "" {
			entry.AgentID = "unknown"
		}
		if record.Model != nil {
			entry.Model = strings.TrimSpace(*record.Model)
		}
		if evidence := transcriptEvidenceData(record.Evidence); evidence != nil {
			entry.Evidence = evidence
		}
		if member, ok := castByAgent[entry.AgentID]; ok {
			memberCopy := member
			entry.CastMember = &memberCopy
		}
		out = append(out, entry)
	}
	return out
}

func transcriptEvidenceData(evidence *types.EvidenceBundle) *transcriptEvidenceOutput {
	if evidence == nil {
		return nil
	}
	out := &transcriptEvidenceOutput{
		Summary:                  strings.TrimSpace(evidence.Summary),
		SourceReferences:         append([]types.SourceReference(nil), evidence.SourceReferences...),
		FullSourceContentOmitted: true,
	}
	for _, doc := range evidence.ContextDocuments {
		if path := strings.TrimSpace(doc.Path); path != "" {
			out.ContextDocumentRefs = append(out.ContextDocumentRefs, path)
		}
	}
	return out
}

func statsMarkdownRows(stats map[string]any) [][]string {
	rows := [][]string{
		{"Total turns", fmt.Sprint(stats["total_turns"])},
		{"Total tokens", fmt.Sprint(stats["total_tokens"])},
		{"Total cost", fmt.Sprint(stats["total_cost"])},
		{"Average turn duration", fmt.Sprint(stats["avg_turn_duration_seconds"])},
	}

	if perAgent, ok := stats["per_agent"].(map[string]any); ok {
		agentIDs := make([]string, 0, len(perAgent))
		for agentID := range perAgent {
			agentIDs = append(agentIDs, agentID)
		}
		sort.Strings(agentIDs)
		for _, agentID := range agentIDs {
			if agentStats, ok := perAgent[agentID].(map[string]any); ok {
				rows = append(rows, []string{fmt.Sprintf("Agent %s", agentID), fmt.Sprintf("turns=%v; tokens=%v; cost=%v", agentStats["turns"], agentStats["tokens"], agentStats["cost"])})
			}
		}
	}

	if events, ok := stats["consensus_events"].([]any); ok {
		rows = append(rows, []string{"Consensus events", fmt.Sprintf("%d", len(events))})
		for i, event := range events {
			if eventMap, ok := event.(map[string]any); ok {
				rows = append(rows, []string{fmt.Sprintf("Consensus %d", i+1), fmt.Sprintf("turn=%v; agent=%v; statement=%v", eventMap["turn"], eventMap["agent_id"], eventMap["statement"])})
			}
		}
	}

	return rows
}

func writeTranscriptMarkdown(w io.Writer, data transcriptShowOutput) error {
	var sb strings.Builder
	fmt.Fprintln(&sb, "# Transcript")
	fmt.Fprintln(&sb)
	fmt.Fprintf(&sb, "- **Document type:** `%s`\n", data.DocumentType)
	fmt.Fprintf(&sb, "- **Document schema version:** %d\n", data.DocumentSchemaVersion)
	fmt.Fprintf(&sb, "- **Path:** `%s`\n", data.Path)
	fmt.Fprintf(&sb, "- **Records:** %d\n", data.RecordCount)
	fmt.Fprintf(&sb, "- **Agents:** %s\n", strings.Join(data.Agents, ", "))
	fmt.Fprintf(&sb, "- **Evidence boundary:** %s\n", data.EvidenceBoundary)
	if data.Metadata.Available {
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "## Cast Metadata")
		fmt.Fprintf(&sb, "- **Transcript metadata schema:** %d\n", data.Metadata.SchemaVersion)
		if data.Metadata.Topology != "" {
			fmt.Fprintf(&sb, "- **Topology:** %s\n", data.Metadata.Topology)
		}
		fmt.Fprintf(&sb, "- **Consensus threshold:** %d\n", data.Metadata.ConsensusThreshold)
		for _, member := range data.Metadata.Cast {
			fmt.Fprintf(&sb, "- **A%d %s:** persona `%s`, model `%s`, color `%s`\n", member.ID, member.Name, member.Persona, member.ProviderModel, member.Color)
		}
	}
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## Records")
	for _, record := range data.Records {
		if record.AgentID == "synthesizer" {
			writeTranscriptSynthesisMarkdown(&sb, record)
			continue
		}
		fmt.Fprintf(&sb, "\n### Record %d\n\n", record.RecordIndex)
		fmt.Fprintf(&sb, "- **Turn:** %d\n", record.Turn)
		fmt.Fprintf(&sb, "- **Agent:** %s\n", record.AgentID)
		if record.CastMember != nil {
			fmt.Fprintf(&sb, "- **Cast:** A%d %s (persona `%s`)\n", record.CastMember.ID, record.CastMember.Name, record.CastMember.Persona)
		}
		if record.Model != "" {
			fmt.Fprintf(&sb, "- **Model:** %s\n", record.Model)
		}
		fmt.Fprintf(&sb, "- **Timestamp:** %v\n", record.Timestamp)
		if record.Content != "" {
			fmt.Fprintf(&sb, "- **Content:** %s\n", record.Content)
		}
		fmt.Fprintf(&sb, "- **Consensus:** %t\n", record.Consensus)
		if record.ConsensusStatement != "" {
			fmt.Fprintf(&sb, "- **Consensus statement:** %s\n", record.ConsensusStatement)
		}
		if record.Evidence != nil {
			fmt.Fprintln(&sb, "- **Evidence full source content omitted:** true")
			if record.Evidence.Summary != "" {
				fmt.Fprintf(&sb, "- **Evidence summary:** %s\n", record.Evidence.Summary)
			}
			for i, source := range record.Evidence.SourceReferences {
				fmt.Fprintf(&sb, "- **Evidence source %d:** %s\n", i+1, transcriptEvidenceReferenceLine(source))
			}
			for i, path := range record.Evidence.ContextDocumentRefs {
				fmt.Fprintf(&sb, "- **Context reference %d:** `%s`\n", i+1, path)
			}
		}
	}
	_, err := fmt.Fprint(w, sb.String())
	return err
}

func writeTranscriptSynthesisMarkdown(sb *strings.Builder, record transcriptRecordOutput) {
	var result map[string]any
	if err := json.Unmarshal([]byte(record.Content), &result); err != nil {
		fmt.Fprintf(sb, "- **Synthesis:** %s\n", record.Content)
		return
	}
	fmt.Fprintf(sb, "\n### Synthesis\n\n")
	fmt.Fprintf(sb, "- **Record:** %d\n", record.RecordIndex)
	if rec, ok := result["recommended_decision"]; ok {
		if s, ok := rec.(string); ok && s != "" {
			fmt.Fprintf(sb, "- **Recommended Decision:** %s\n", s)
		}
	}
	if c, ok := result["confidence"]; ok {
		if s, ok := c.(string); ok {
			fmt.Fprintf(sb, "- **Confidence:** %s\n", s)
		}
	}
	if args, ok := result["key_arguments"]; ok {
		if list, ok := args.([]any); ok && len(list) > 0 {
			fmt.Fprintln(sb)
			fmt.Fprintln(sb, "### Key Arguments")
			for _, arg := range list {
				fmt.Fprintf(sb, "- %s\n", fmt.Sprint(arg))
			}
		}
	}
	if agrs, ok := result["points_of_agreement"]; ok {
		if list, ok := agrs.([]any); ok && len(list) > 0 {
			fmt.Fprintln(sb)
			fmt.Fprintln(sb, "### Points of Agreement")
			for _, arg := range list {
				fmt.Fprintf(sb, "- %s\n", fmt.Sprint(arg))
			}
		}
	}
	if tens, ok := result["unresolved_tensions"]; ok {
		if list, ok := tens.([]any); ok && len(list) > 0 {
			fmt.Fprintln(sb)
			fmt.Fprintln(sb, "### Unresolved Tensions")
			for _, arg := range list {
				fmt.Fprintf(sb, "- %s\n", fmt.Sprint(arg))
			}
		}
	}
}

func transcriptMetadataFromRecords(records []types.TurnRecord) *types.TranscriptMetadata {
	for _, record := range records {
		if record.Transcript != nil {
			return record.Transcript
		}
	}
	return nil
}

func transcriptEvidenceReferenceLine(source types.SourceReference) string {
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
	refs := []string{}
	if source.URL != "" {
		refs = append(refs, source.URL)
	}
	if source.Path != "" {
		refs = append(refs, source.Path)
	}
	if source.Query != "" {
		refs = append(refs, "query: "+source.Query)
	}
	if len(refs) == 0 {
		return label
	}
	return fmt.Sprintf("%s (%s)", label, strings.Join(refs, "; "))
}

func configSummaryData(cfg *types.DeliberationConfig) map[string]any {
	return map[string]any{
		"valid":               true,
		"topology":            cfg.Topology,
		"agents":              cfg.Agents,
		"agent_count":         len(cfg.Agents),
		"consensus_threshold": cfg.ConsensusThreshold,
		"min_rounds":          cfg.MinRounds,
		"synthesis_model":     cfg.SynthesisModel,
		"research":            cfg.ResearchEnabled,
		"context":             cfg.ContextPaths,
	}
}

func configMarkdownRows(cfg *types.DeliberationConfig) [][]string {
	rows := [][]string{{"Valid", "true"}, {"Topology", string(cfg.Topology)}, {"Agents", fmt.Sprintf("%d", len(cfg.Agents))}}
	if cfg.ConsensusThreshold > 0 {
		rows = append(rows, []string{"Consensus threshold", fmt.Sprintf("%d", cfg.ConsensusThreshold)})
	}
	if cfg.MinRounds > 0 {
		rows = append(rows, []string{"Min rounds", fmt.Sprintf("%d", cfg.MinRounds)})
	}
	if cfg.SynthesisModel != nil {
		rows = append(rows, []string{"Synthesis model", *cfg.SynthesisModel})
	}
	return rows
}

type validationResult struct {
	Valid       bool             `json:"valid"`
	Input       string           `json:"input"`
	Path        string           `json:"path,omitempty"`
	Summary     map[string]any   `json:"summary,omitempty"`
	Error       *validationError `json:"error,omitempty"`
	Corrections []string         `json:"corrections,omitempty"`
}

type validationError struct {
	Stage   string   `json:"stage"`
	Message string   `json:"message"`
	Details []string `json:"details,omitempty"`
}

func validationSuccess(input, path string, cfg *types.DeliberationConfig) validationResult {
	return validationResult{Valid: true, Input: input, Path: path, Summary: configSummaryData(cfg)}
}

func validationFailure(input, path string, err error) validationResult {
	message := err.Error()
	return validationResult{
		Valid: false,
		Input: input,
		Path:  path,
		Error: &validationError{Stage: validationErrorStage(message), Message: message, Details: validationErrorDetails(message)},
		Corrections: []string{
			"Ensure the YAML parses successfully.",
			"Provide at least one agent with non-empty id and model fields, or configure default_model for omitted models.",
			"Use one of the supported topologies: ring, star, mesh.",
			"Set consensus_threshold to zero or a positive integer.",
		},
	}
}

func validationErrorStage(message string) string {
	switch {
	case strings.Contains(message, "config path not found") || strings.Contains(message, "no config found") || strings.Contains(message, "ambiguous") || strings.Contains(message, "reading config search directory"):
		return "resolve"
	case strings.Contains(message, "reading config file"):
		return "read"
	case strings.Contains(message, "parsing config YAML"):
		return "parse"
	default:
		return "validate"
	}
}

func validationErrorDetails(message string) []string {
	details := []string{}
	if strings.Contains(message, "topology") {
		details = append(details, "allowed topology values: ring, star, mesh")
	}
	if strings.Contains(message, "agent") {
		details = append(details, "agents must include id and model unless default_model supplies missing models")
	}
	if strings.Contains(message, "consensus_threshold") {
		details = append(details, "consensus_threshold must be >= 0")
	}
	return details
}

func writeValidationMarkdown(w io.Writer, title string, result validationResult) error {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", title)
	fmt.Fprintf(&sb, "- **Valid:** %t\n", result.Valid)
	fmt.Fprintf(&sb, "- **Input:** `%s`\n", result.Input)
	if result.Path != "" {
		fmt.Fprintf(&sb, "- **Path:** `%s`\n", result.Path)
	}
	if result.Valid {
		rows := configMarkdownRows(&types.DeliberationConfig{
			Topology:           types.Topology(fmt.Sprint(result.Summary["topology"])),
			ConsensusThreshold: intFromAny(result.Summary["consensus_threshold"]),
			SynthesisModel:     stringPtrFromAny(result.Summary["synthesis_model"]),
		})
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "## Summary")
		for _, row := range rows {
			if row[0] == "Agents" {
				fmt.Fprintf(&sb, "- **Agents:** %v\n", result.Summary["agent_count"])
				continue
			}
			fmt.Fprintf(&sb, "- **%s:** %s\n", row[0], row[1])
		}
		return writeString(w, sb.String())
	}
	if result.Error != nil {
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "## Error")
		fmt.Fprintf(&sb, "- **Stage:** `%s`\n", result.Error.Stage)
		fmt.Fprintf(&sb, "- **Message:** %s\n", result.Error.Message)
		for _, detail := range result.Error.Details {
			fmt.Fprintf(&sb, "- **Detail:** %s\n", detail)
		}
	}
	if len(result.Corrections) > 0 {
		fmt.Fprintln(&sb)
		fmt.Fprintln(&sb, "## Self-Correction")
		for _, correction := range result.Corrections {
			fmt.Fprintf(&sb, "- %s\n", correction)
		}
	}
	return writeString(w, sb.String())
}

func writeString(w io.Writer, s string) error {
	_, err := fmt.Fprint(w, s)
	return err
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case float64:
		return int(v)
	default:
		return 0
	}
}

func stringPtrFromAny(value any) *string {
	s, ok := value.(*string)
	if ok {
		return s
	}
	if value == nil {
		return nil
	}
	text := fmt.Sprint(value)
	if text == "" || text == "<nil>" {
		return nil
	}
	return &text
}

func transcriptAgents(records []types.TurnRecord) []string {
	seen := map[string]bool{}
	agents := []string{}
	for _, record := range records {
		id := strings.TrimSpace(record.AgentID)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		agents = append(agents, id)
	}
	return agents
}

func metadataCommandData() map[string]any {
	return map[string]any{
		"schema_version": schemaVersion,
		"formats": map[string]any{
			"default": formatText,
			"values":  validFormats,
		},
		"commands":            commandMetadata(),
		"config_keys":         configKeyMetadata(),
		"transcript_metadata": transcriptMetadataContract(),
	}
}

func commandMetadata() []map[string]any {
	commands := liveCommandSurface(rootCmd)
	out := make([]map[string]any, 0, len(commands))
	for _, item := range commands {
		cmd := item.cmd
		if cmd == nil {
			continue
		}
		flags := []map[string]any{}
		cmd.Flags().VisitAll(func(flag *pflag.Flag) {
			entry := map[string]any{
				"name":        flag.Name,
				"default":     flag.DefValue,
				"description": flag.Usage,
			}
			if flag.Name == "format" {
				entry["enum_values"] = validFormats
			}
			if flag.Name == "auto" {
				entry["enum_values"] = []string{"quick", "normal", "deep", "yolo"}
			}
			flags = append(flags, entry)
		})
		out = append(out, map[string]any{"name": item.name, "use": cmd.Use, "short": cmd.Short, "flags": flags})
	}
	return out
}

type commandSurfaceEntry struct {
	name string
	cmd  *cobra.Command
}

func liveCommandSurface(root *cobra.Command) []commandSurfaceEntry {
	var out []commandSurfaceEntry
	var walk func(prefix string, cmd *cobra.Command)
	walk = func(prefix string, cmd *cobra.Command) {
		children := cmd.Commands()
		sort.Slice(children, func(i, j int) bool { return children[i].Name() < children[j].Name() })
		for _, child := range children {
			if child.Hidden || !child.IsAvailableCommand() {
				continue
			}
			name := strings.TrimSpace(prefix + " " + child.Name())
			out = append(out, commandSurfaceEntry{name: name, cmd: child})
			walk(name, child)
		}
	}
	walk(root.Name(), root)
	return out
}

func commandFlags(commands []map[string]any) []map[string]any {
	var flags []map[string]any
	for _, command := range commands {
		commandName, _ := command["name"].(string)
		items, _ := command["flags"].([]map[string]any)
		for _, item := range items {
			flag := map[string]any{"command": commandName}
			for key, value := range item {
				flag[key] = value
			}
			flags = append(flags, flag)
		}
	}
	return flags
}

func defaultMetadata(commands []map[string]any) map[string]any {
	defaults := map[string]any{"format": formatText}
	for _, command := range commands {
		commandName, _ := command["name"].(string)
		items, _ := command["flags"].([]map[string]any)
		for _, item := range items {
			name, _ := item["name"].(string)
			if name == "" {
				continue
			}
			defaults[commandName+" --"+name] = item["default"]
		}
	}
	return defaults
}

func enumMetadata(commands []map[string]any) map[string]any {
	enums := map[string]any{"format": validFormats}
	for _, command := range commands {
		commandName, _ := command["name"].(string)
		items, _ := command["flags"].([]map[string]any)
		for _, item := range items {
			values, ok := item["enum_values"]
			if !ok {
				continue
			}
			name, _ := item["name"].(string)
			enums[commandName+" --"+name] = values
		}
	}
	return enums
}

func primeCommandData() (map[string]any, error) {
	gconf, err := config.LoadDefaultGlobalConfig()
	if err != nil {
		return nil, fmt.Errorf("loading config: %w", err)
	}
	configValues, cfgPath, err := effectiveConfigRows(gconf)
	if err != nil {
		return nil, err
	}
	commands := commandMetadata()
	return map[string]any{
		"schema_version": schemaVersion,
		"purpose":        "Agora-provided CLI operating context for agents. This is not deliberation evidence.",
		"context_boundary": map[string]any{
			"prime":                "Use `agora prime` to inspect Agora's command surface, defaults, config, and transcript metadata before operating the CLI.",
			"deliberation_context": "Use `--context PATH` on `agora run` to provide user-owned local text evidence to the deliberation. It is repeatable and does not change CLI operating context.",
		},
		"commands":            commands,
		"flags":               commandFlags(commands),
		"defaults":            defaultMetadata(commands),
		"enum_values":         enumMetadata(commands),
		"config_keys":         configKeyMetadata(),
		"config":              map[string]any{"path": cfgPath, "values": configValues, "redaction": "secret-like config keys and values are reported as [redacted] when set"},
		"transcript_metadata": transcriptMetadataContract(),
	}, nil
}

var primeFormat string

var primeCmd = &cobra.Command{
	Use:   "prime",
	Short: "Print Agora CLI operating context for agents",
	Long:  "Print Agora-provided CLI operating context for agents. This is separate from --context PATH, which supplies user-provided local text evidence to a deliberation.",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateFormat(primeFormat); err != nil {
			return err
		}
		data, err := primeCommandData()
		if err != nil {
			return err
		}
		switch primeFormat {
		case formatJSON:
			return writeJSON(cmd.OutOrStdout(), "prime", data)
		case formatMarkdown:
			return writePrimeMarkdown(cmd.OutOrStdout(), data)
		default:
			return writePrimeText(cmd.OutOrStdout(), data)
		}
	},
}

func init() {
	addFormatFlag(primeCmd, &primeFormat)
}

func writePrimeText(w io.Writer, data map[string]any) error {
	commands, _ := data["commands"].([]map[string]any)
	gconf, _ := data["config"].(map[string]any)
	configValues, _ := gconf["values"].([]configValue)
	var sb strings.Builder
	fmt.Fprintln(&sb, "Agora Prime")
	fmt.Fprintln(&sb, "Purpose: Agora-provided CLI operating context for agents; not deliberation evidence.")
	fmt.Fprintln(&sb, "Use --context PATH only with run to provide user-owned local text evidence to deliberation.")
	fmt.Fprintf(&sb, "Formats: %s (default %s)\n", strings.Join(validFormats, ", "), formatText)
	fmt.Fprintf(&sb, "Commands: %s\n", commandNames(commands))
	fmt.Fprintf(&sb, "Config: %d keys at %v; secret-like keys and values render as [redacted]\n", len(configKeyDefs), gconf["path"])
	for _, value := range configValues {
		fmt.Fprintf(&sb, "- %s: %s (%s)\n", value.Key, value.Value, value.Source)
	}
	fmt.Fprintln(&sb, "Transcript metadata: first JSONL record may include transcript.schema_version, cast, and config.")
	_, err := fmt.Fprint(w, sb.String())
	return err
}

func writePrimeMarkdown(w io.Writer, data map[string]any) error {
	commands, _ := data["commands"].([]map[string]any)
	flags, _ := data["flags"].([]map[string]any)
	configKeys, _ := data["config_keys"].([]map[string]any)
	gconf, _ := data["config"].(map[string]any)
	configValues, _ := gconf["values"].([]configValue)
	var sb strings.Builder
	fmt.Fprintln(&sb, "# Agora Prime")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "Agora-provided CLI operating context for agents. This is not deliberation evidence.")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## Context Boundary")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "- `agora prime`: inspect commands, flags, defaults, enum values, config, and transcript metadata before operating Agora.")
	fmt.Fprintln(&sb, "- `--context PATH`: provide user-owned local text evidence to `agora run`; repeatable and separate from CLI operating context.")
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## Formats")
	fmt.Fprintf(&sb, "- Default: `%s`\n", formatText)
	fmt.Fprintf(&sb, "- Values: `%s`\n", strings.Join(validFormats, "`, `"))
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## Commands")
	for _, command := range commands {
		fmt.Fprintf(&sb, "- `%s`: %s\n", command["name"], command["short"])
	}
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## Flags, Defaults, And Enum Values")
	for _, flag := range flags {
		line := fmt.Sprintf("- `%s --%s`: default `%v`; %s", flag["command"], flag["name"], flag["default"], flag["description"])
		if values, ok := flag["enum_values"]; ok {
			line += fmt.Sprintf("; values `%s`", strings.Join(anyStringSlice(values), "`, `"))
		}
		fmt.Fprintln(&sb, line)
	}
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## Config")
	fmt.Fprintf(&sb, "- Path: `%v`\n", gconf["path"])
	fmt.Fprintln(&sb, "- Redaction: secret-like config keys and values are reported as `[redacted]` when set.")
	for _, key := range configKeys {
		line := fmt.Sprintf("- `%s`: default `%v`; %s", key["key"], key["default"], key["description"])
		if values, ok := key["enum_values"]; ok {
			line += fmt.Sprintf("; values `%s`", strings.Join(anyStringSlice(values), "`, `"))
		}
		fmt.Fprintln(&sb, line)
	}
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## Effective Config")
	for _, value := range configValues {
		fmt.Fprintf(&sb, "- `%s`: `%s` (%s)\n", value.Key, value.Value, value.Source)
	}
	fmt.Fprintln(&sb)
	fmt.Fprintln(&sb, "## Transcript Metadata")
	fmt.Fprintln(&sb, "- Storage: JSONL turn records; transcript metadata appears on the first record `transcript` field.")
	fmt.Fprintln(&sb, "- Fields: `schema_version`, `cast`, `config`.")
	_, err := fmt.Fprint(w, sb.String())
	return err
}

func commandNames(commands []map[string]any) string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		if name, ok := command["name"].(string); ok {
			names = append(names, name)
		}
	}
	return strings.Join(names, ", ")
}

func anyStringSlice(value any) []string {
	switch v := value.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			out = append(out, fmt.Sprint(item))
		}
		return out
	default:
		return nil
	}
}

func configKeyMetadata() []map[string]any {
	out := make([]map[string]any, 0, len(configKeyDefs))
	for _, def := range configKeyDefs {
		defaultValue, _ := settingDefaultValue(def)
		entry := map[string]any{
			"group":                  def.Group,
			"key":                    def.Key,
			"type":                   def.Type,
			"description":            def.Description,
			"default":                redactedSettingValue(def.Key, defaultValue),
			"effective_value_policy": configValuePolicy(def),
		}
		if len(def.Allowed) > 0 {
			entry["enum_values"] = append([]string(nil), def.Allowed...)
		}
		out = append(out, entry)
	}
	return out
}

func settingDefaultValue(def configKeyDef) (string, error) {
	if def.DefaultFunc != nil {
		return def.DefaultFunc()
	}
	return def.DefaultValue, nil
}

func configValuePolicy(def configKeyDef) string {
	if def.DefaultFunc != nil {
		return "when unset, Agora computes a runtime default"
	}
	if def.DefaultValue != "" {
		return "when unset, Agora uses the documented default"
	}
	return "unset means no default is applied"
}

func transcriptMetadataContract() map[string]any {
	return map[string]any{
		"storage_schema": "jsonl turn records; transcript metadata appears on the first record transcript field",
		"fields": []map[string]any{
			{"name": "schema_version", "type": "integer", "default": 1},
			{"name": "cast", "type": "array", "items": []string{"id", "name", "persona", "provider_model", "color"}},
			{"name": "config", "type": "object", "fields": []string{"agents", "topology", "consensus_threshold", "min_rounds", "synthesis_model", "research", "context"}},
		},
	}
}

var metadataFormat string

var metadataCmd = &cobra.Command{
	Use:   "metadata",
	Short: "Inspect Agora CLI metadata",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateFormat(metadataFormat); err != nil {
			return err
		}
		data := metadataCommandData()
		switch metadataFormat {
		case formatJSON:
			return writeJSON(cmd.OutOrStdout(), "metadata", data)
		case formatMarkdown:
			return writeMarkdown(cmd.OutOrStdout(), "Agora Metadata", metadataMarkdownRows(data))
		default:
			return writeMarkdown(cmd.OutOrStdout(), "Agora Metadata", metadataMarkdownRows(data))
		}
	},
}

func init() {
	addFormatFlag(metadataCmd, &metadataFormat)
}

func metadataMarkdownRows(data map[string]any) [][]string {
	return [][]string{
		{"Schema version", fmt.Sprint(data["schema_version"])},
		{"Default format", formatText},
		{"Valid formats", strings.Join(validFormats, ", ")},
		{"Supported inspection commands", "list, stats, show, validate, config get --all, metadata"},
		{"Config keys", fmt.Sprintf("%d", len(configKeyDefs))},
		{"Transcript metadata", "schema_version, cast, config"},
	}
}
