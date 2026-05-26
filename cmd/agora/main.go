package main

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/autogen"
	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/evidence"
	"github.com/jgabor/agora/internal/output"
	"github.com/jgabor/agora/internal/session"
	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
	"github.com/spf13/cobra"
)

const defaultModel = "opencode-go/deepseek-v4-flash"

var version = "0.3.0"

func main() {
	rootCmd.SetUsageTemplate(rootCmd.UsageTemplate() + "\n\nAuthor:\n  Jonathan Gabor (https://jgabor.se)\n\nSource:\n  https://github.com/jgabor/agora\n")
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:     "agora",
	Short:   "Agora — Closed-loop multi-agent deliberation system",
	Version: version,
}

func init() {
	rootCmd.AddCommand(runCmd, statsCmd, validateCmd, resumeCmd, showCmd, listCmd, configCmd, metadataCmd, primeCmd)
}

// --- run ----------------------------------------------------------

type runFlagValues struct {
	Config      string
	Topic       string
	TimeLimit   int
	Window      int
	MaxTurns    int
	Output      string
	Verbose     bool
	Quiet       bool
	Budget      float64
	BudgetSet   bool // true when --budget was explicitly passed
	FullContext bool
	DryRun      bool
	Auto        string
	Model       string
	Yes         bool
	Research    bool
	NoResearch  bool
	Context     []string // local text context paths (repeatable)
}

var (
	runFlags = runFlagValues{
		TimeLimit: 60,
		Window:    2,
		MaxTurns:  10,
		Model:     defaultModel,
	}
	resumeFlags = runFlagValues{
		TimeLimit: 60,
		Window:    2,
		MaxTurns:  10,
		Model:     defaultModel,
	}
	runSynthesize bool
	resumeFile    string
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a multi-agent deliberation",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg *types.DeliberationConfig
		var autoLevel types.AutoLevel
		var levelCaps types.LevelCaps
		autoMode := runFlags.Auto != ""
		outputPath, err := resolveTranscriptOutput(cmd, runFlags.Output, runFlags.Topic)
		if err != nil {
			return err
		}

		if autoMode {
			level, err := types.ParseAutoLevel(runFlags.Auto)
			if err != nil {
				return err
			}
			autoLevel = level
			levelCaps = types.CapsForLevel(level)
			if cmd.Flags().Changed("time") {
				levelCaps.TimeLimit = runFlags.TimeLimit
			}
			if cmd.Flags().Changed("max-turns") {
				levelCaps.MaxTurns = runFlags.MaxTurns
			}
			outMgr := output.NewOutputManagerWithMode(liveOutputMode(runFlags.Quiet, runFlags.Verbose))

			if runFlags.DryRun {
				cfg, err = autogen.GenerateDryRunConfig(runFlags.Topic, level, runFlags.Model)
				if err != nil {
					return fmt.Errorf("auto config generation: %w", err)
				}
			} else {
				runner := agent.NewAgentRunner(false)
				stop := outMgr.Activity("Config generation")
				cfg, err = autogen.GenerateConfig(runFlags.Topic, level, runFlags.Model, runner)
				stop()
				if err != nil {
					return fmt.Errorf("auto config generation: %w", err)
				}
			}

			outMgr.ConfigPreview(cfg, level, levelCaps)

			if err := requireAutoApprovalForNonTTY(runFlags.Yes, runFlags.DryRun); err != nil {
				return err
			}

			if !runFlags.Yes {
				if !confirmProceed() {
					fmt.Println(output.RenderStatus("Deliberation", [][]string{{"Status", "Aborted"}}, "3"))
					return nil
				}
			}
		} else {
			var err error
			cfg, err = loadConfigArtifact(runFlags.Config)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
		}
		agent.ApplyReadOnlyPromptGuard(cfg)

		// Enforce read-only at the tool-execution layer by writing a minimal
		// opencode.json with permission denies. Falls back gracefully if the
		// directory already contains a config or the write fails.
		readOnlyCleanup, _ := agent.WriteReadOnlyConfig(".")
		defer readOnlyCleanup()

		var budget *float64
		if runFlags.BudgetSet {
			budget = &runFlags.Budget
		}

		settings, err := config.LoadDefaultSettings()
		if err != nil {
			return err
		}
		evidenceOverrides := runEvidenceOverrides(cmd, autoMode, autoLevel)
		evidenceRequest := evidence.ResolveRequest(cfg, settings.ResearchMaxSources, settings.ContextMaxBytes, settings.ContextMaxDepth, evidenceOverrides)

		outMgr := output.NewOutputManagerWithMode(liveOutputMode(runFlags.Quiet, runFlags.Verbose))
		req := session.RunRequest{
			Topic:        runFlags.Topic,
			Config:       cfg,
			OutputPath:   outputPath,
			Window:       runFlags.Window,
			MaxTurns:     runFlags.MaxTurns,
			TimeLimit:    runFlags.TimeLimit,
			Budget:       budget,
			FullContext:  runFlags.FullContext,
			DryRun:       runFlags.DryRun,
			Evidence:     evidenceRequest,
			Synthesize:   runSynthesize || autoMode,
			TranscriptID: generateTranscriptID(),
		}
		if autoMode {
			req.Auto = sessionAutoCaps(cmd, levelCaps)
		}

		result, err := session.Run(req, sessionHooks(outMgr))
		if err != nil {
			return err
		}
		printSessionResult(outMgr, result, fmt.Sprintf("Deliberation complete (%d turns)", result.Stats.TotalTurns))

		return nil
	},
}

func init() {
	sharedRunFlags(runCmd, "run")
	runCmd.Flags().BoolVar(&runSynthesize, "synthesize", false, "Run final synthesis agent after deliberation")

	_ = runCmd.MarkFlagRequired("topic")

	runCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		runFlags.BudgetSet = cmd.Flags().Changed("budget")
		if runFlags.Quiet && runFlags.Verbose {
			return fmt.Errorf("cannot use --quiet and --verbose together")
		}
		if cmd.Flags().Changed("research") && cmd.Flags().Changed("no-research") {
			return fmt.Errorf("cannot use --research and --no-research together")
		}
		if err := applySettingsDefaults(cmd, &runFlags.Model, &runFlags.Auto); err != nil {
			return err
		}

		autoSet := runFlags.Auto != ""
		configSet := runFlags.Config != ""

		if autoSet && configSet {
			return fmt.Errorf("cannot use --auto and --config together")
		}

		if autoSet {
			level, err := types.ParseAutoLevel(runFlags.Auto)
			if err != nil {
				return err
			}
			if level == types.AutoOff {
				return fmt.Errorf("--auto off is not a valid mode; omit --auto to run with a config file")
			}
			if runFlags.Topic == "" {
				return fmt.Errorf("--topic is required with --auto")
			}
		} else {
			// When --auto is not set, --config is required
			if err := cmd.MarkFlagRequired("config"); err != nil {
				return err
			}
		}

		return nil
	}
}

func researchOverrides(cmd *cobra.Command) evidence.Overrides {
	var research *bool
	if cmd.Flags().Changed("research") {
		enabled := true
		research = &enabled
	}
	if cmd.Flags().Changed("no-research") {
		enabled := false
		research = &enabled
	}

	return evidence.Overrides{
		Research:     research,
		ContextSet:   cmd.Flags().Changed("context"),
		ContextPaths: append([]string(nil), runFlags.Context...),
	}
}

func runEvidenceOverrides(cmd *cobra.Command, autoMode bool, level types.AutoLevel) evidence.Overrides {
	overrides := researchOverrides(cmd)
	if autoMode {
		overrides.Defaults = evidence.DefaultsForAutoLevel(level)
	}
	return overrides
}

func resumeEvidenceRequestChanged(cmd *cobra.Command) bool {
	return cmd.Flags().Changed("research") || cmd.Flags().Changed("no-research") || cmd.Flags().Changed("context")
}

func sessionAutoCaps(cmd *cobra.Command, caps types.LevelCaps) *session.AutoCaps {
	return &session.AutoCaps{
		Caps:             caps,
		ExplicitTime:     cmd.Flags().Changed("time"),
		ExplicitMaxTurns: cmd.Flags().Changed("max-turns"),
	}
}

// isSynthesisError reports whether a synthesis result indicates a failure.
func isSynthesisError(result map[string]any) bool {
	if rec, ok := result["recommended_decision"]; ok {
		if s, ok := rec.(string); ok {
			return strings.HasPrefix(s, "Synthesis could not")
		}
	}
	return false
}

func sessionHooks(outMgr *output.OutputManager) session.Hooks {
	return session.Hooks{
		OnTurn:     outMgr.TurnProgress,
		OnEvidence: outMgr.EvidenceSummary,
		OnActivity: outMgr.Activity,
		OnHeader:   outMgr.DeliberationHeader,
	}
}

func printSessionResult(outMgr *output.OutputManager, result session.Result, completeMsg string) {
	outMgr.FinalStats(result.Records, result.State)
	if result.Synthesis != nil {
		outMgr.SynthesizeHeader()
		outMgr.SynthesisResult(result.Synthesis)
		if isSynthesisError(result.Synthesis) {
			outMgr.Info("Synthesis could not produce a structured result — see transcript for raw model output")
		} else {
			outMgr.Success("Synthesis complete")
		}
	}
	outMgr.Success(completeMsg)
	outMgr.Success(fmt.Sprintf("Transcript: %s", result.OutputPath))
	outMgr.Success(outMgr.HaltedByDisplay(result.HaltedBy))
}

// --- stats --------------------------------------------------------

var statsFormat string

var statsCmd = &cobra.Command{
	Use:   "stats TRANSCRIPT",
	Short: "Print statistics from a deliberation transcript",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateFormat(statsFormat); err != nil {
			return err
		}
		path, err := resolveTranscriptSource(args[0])
		if err != nil {
			loadErr := fmt.Errorf("loading transcript: %w", err)
			writeFormattedCommandError(cmd, statsFormat, "stats", "Transcript Statistics Error", commandErrorData(args[0], path, "transcript_load_failed", loadErr))
			return loadErr
		}
		records, err := loadTranscriptFile(path)
		if err != nil {
			loadErr := fmt.Errorf("loading transcript: %w", err)
			writeFormattedCommandError(cmd, statsFormat, "stats", "Transcript Statistics Error", commandErrorData(args[0], path, "transcript_load_failed", loadErr))
			return loadErr
		}
		if len(records) == 0 {
			emptyErr := fmt.Errorf("transcript empty or invalid: %s", path)
			writeFormattedCommandError(cmd, statsFormat, "stats", "Transcript Statistics Error", commandErrorData(args[0], path, "transcript_empty_or_invalid", emptyErr))
			return emptyErr
		}

		stats := types.ComputeStats(records)
		statsData := statsToDict(stats)
		switch statsFormat {
		case formatJSON:
			return writeJSON(cmd.OutOrStdout(), "stats", statsData)
		case formatMarkdown:
			return writeMarkdown(cmd.OutOrStdout(), "Transcript Statistics", statsMarkdownRows(statsData))
		}
		outMgr := output.NewOutputManager(false)
		outMgr.PrintStats(statsData)

		return nil
	},
}

func init() {
	addFormatFlag(statsCmd, &statsFormat)
}

// --- show ---------------------------------------------------------

var showFormat string

var showCmd = &cobra.Command{
	Use:   "show TRANSCRIPT|SLUG",
	Short: "Show a deliberation transcript",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateFormat(showFormat); err != nil {
			return err
		}
		path, err := resolveTranscriptSource(args[0])
		if err != nil {
			if showFormat == formatJSON || showFormat == formatMarkdown {
				writeFormattedCommandError(cmd, showFormat, "show", "Transcript Error", commandErrorData(args[0], path, "transcript_load_failed", err))
			}
			return fmt.Errorf("loading transcript: %w", err)
		}
		records, err := loadTranscriptFile(path)
		if err != nil {
			if showFormat == formatJSON || showFormat == formatMarkdown {
				writeFormattedCommandError(cmd, showFormat, "show", "Transcript Error", commandErrorData(args[0], path, "transcript_load_failed", err))
			}
			return fmt.Errorf("loading transcript: %w", err)
		}
		if len(records) == 0 {
			err := fmt.Errorf("transcript empty: %s", path)
			if showFormat == formatJSON || showFormat == formatMarkdown {
				writeFormattedCommandError(cmd, showFormat, "show", "Transcript Error", commandErrorData(args[0], path, "transcript_empty", err))
			}
			return err
		}

		showData := transcriptShowData(path, records)
		switch showFormat {
		case formatJSON:
			return writeJSON(cmd.OutOrStdout(), "show", showData)
		case formatMarkdown:
			return writeTranscriptMarkdown(cmd.OutOrStdout(), showData)
		}

		output.RenderTranscript(cmd.OutOrStdout(), records)
		return nil
	},
}

func init() {
	addFormatFlag(showCmd, &showFormat)
}

// --- validate -----------------------------------------------------

var validateFormatValue string

var validateCmd = &cobra.Command{
	Use:   "validate CONFIG|SLUG",
	Short: "Validate a configuration file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateFormat(validateFormatValue); err != nil {
			return err
		}
		path, err := resolveConfigArtifact(args[0], configArtifactRoots())
		if err != nil {
			if validateFormatValue == formatJSON || validateFormatValue == formatMarkdown {
				result := validationFailure(args[0], path, err)
				if validateFormatValue == formatJSON {
					_ = writeJSON(cmd.OutOrStdout(), "validate", result)
				} else {
					_ = writeValidationMarkdown(cmd.OutOrStdout(), "Configuration Invalid", result)
				}
			}
			return fmt.Errorf("ERROR: %w", err)
		}
		cfg, err := config.LoadConfig(path)
		if err != nil {
			if validateFormatValue == formatJSON || validateFormatValue == formatMarkdown {
				result := validationFailure(args[0], path, err)
				if validateFormatValue == formatJSON {
					_ = writeJSON(cmd.OutOrStdout(), "validate", result)
				} else {
					_ = writeValidationMarkdown(cmd.OutOrStdout(), "Configuration Invalid", result)
				}
			}
			return fmt.Errorf("ERROR: %w", err)
		}
		result := validationSuccess(args[0], path, cfg)

		switch validateFormatValue {
		case formatJSON:
			return writeJSON(cmd.OutOrStdout(), "validate", result)
		case formatMarkdown:
			return writeValidationMarkdown(cmd.OutOrStdout(), "Configuration Valid", result)
		}

		_, err = fmt.Fprintln(cmd.OutOrStdout(), output.RenderConfigSummary(cfg))
		return err
	},
}

func init() {
	addFormatFlag(validateCmd, &validateFormatValue)
}

// --- list ---------------------------------------------------------

type transcriptEntry struct {
	id       int
	date     time.Time
	slug     string
	filename string
	turns    int
}

var (
	listVerbose bool
	listFormat  string
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List managed transcripts",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := validateFormat(listFormat); err != nil {
			return err
		}
		settings, err := config.LoadDefaultSettings()
		if err != nil {
			return fmt.Errorf("loading settings: %w", err)
		}
		dir := settings.DefaultOutputDir
		if dir == "" {
			dir, err = config.TranscriptStoreDir()
			if err != nil {
				return err
			}
		}

		entries, err := listTranscriptEntries(dir)
		if err != nil {
			return err
		}
		listData := transcriptListData(dir, entries)
		switch listFormat {
		case formatJSON:
			return writeJSON(cmd.OutOrStdout(), "list", listData)
		case formatMarkdown:
			return writeTranscriptListMarkdown(cmd.OutOrStdout(), listData)
		}

		if len(entries) == 0 {
			_, err = fmt.Fprintln(cmd.OutOrStdout(), output.RenderStatus("Managed Transcripts", [][]string{{"Status", "No transcripts found"}}, "6"))
			return err
		}

		headers := []string{"#", "Date", "Turns", "Slug"}
		aligns := []string{"right", "", "right", ""}
		if listVerbose {
			headers = []string{"#", "Date", "Turns", "Transcript"}
		}

		rows := make([][]string, 0, len(entries))
		for i, entry := range entries {
			row := []string{
				fmt.Sprintf("%d", i+1),
				entry.date.Format("2006-01-02 15:04:05"),
				fmt.Sprintf("%d", entry.turns),
				entry.slug,
			}
			if listVerbose {
				row[3] = entry.slug + "\n" + entry.filename
			}
			rows = append(rows, row)
		}
		_, err = fmt.Fprintln(cmd.OutOrStdout(), output.RenderTable("Managed Transcripts", headers, rows, aligns, "6"))
		return err
	},
}

func init() {
	listCmd.Flags().BoolVarP(&listVerbose, "verbose", "v", false, "Show transcript filenames")
	addFormatFlag(listCmd, &listFormat)
}

// --- resume -------------------------------------------------------

var resumeCmd = &cobra.Command{
	Use:   "resume [TRANSCRIPT|SLUG]",
	Short: "Continue deliberation from an existing transcript",
	Args: func(cmd *cobra.Command, args []string) error {
		if resumeFile != "" {
			if len(args) > 1 {
				return fmt.Errorf("accepts at most 1 arg when --file is set")
			}
			return nil
		}
		if len(args) != 1 {
			return fmt.Errorf("requires a transcript path or slug")
		}
		return nil
	},
	PreRunE: func(cmd *cobra.Command, args []string) error {
		resumeFlags.BudgetSet = cmd.Flags().Changed("budget")
		if resumeFlags.Quiet && resumeFlags.Verbose {
			return fmt.Errorf("cannot use --quiet and --verbose together")
		}
		if resumeEvidenceRequestChanged(cmd) {
			return fmt.Errorf("resume cannot change research or context evidence; existing transcript evidence is reused")
		}
		if err := applySettingsDefaults(cmd, &resumeFlags.Model, &resumeFlags.Auto); err != nil {
			return err
		}

		autoSet := resumeFlags.Auto != ""
		configSet := resumeFlags.Config != ""

		if autoSet && configSet {
			return fmt.Errorf("cannot use --auto and --config together")
		}

		if autoSet {
			level, err := types.ParseAutoLevel(resumeFlags.Auto)
			if err != nil {
				return err
			}
			if level == types.AutoOff {
				return fmt.Errorf("--auto off is not a valid mode; omit --auto to run with a config file")
			}
			if resumeFlags.Topic == "" {
				return fmt.Errorf("--topic is required with --auto")
			}
		} else {
			if err := cmd.MarkFlagRequired("config"); err != nil {
				return err
			}
		}

		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg *types.DeliberationConfig
		var levelCaps types.LevelCaps
		autoMode := resumeFlags.Auto != ""
		outputPath, err := resolveTranscriptOutput(cmd, resumeFlags.Output, resumeFlags.Topic)
		if err != nil {
			return err
		}
		sourcePath, err := resolveResumeSource(resumeFile, args)
		if err != nil {
			return err
		}

		if autoMode {
			level, err := types.ParseAutoLevel(resumeFlags.Auto)
			if err != nil {
				return err
			}
			levelCaps = types.CapsForLevel(level)
			if cmd.Flags().Changed("time") {
				levelCaps.TimeLimit = resumeFlags.TimeLimit
			}
			if cmd.Flags().Changed("max-turns") {
				levelCaps.MaxTurns = resumeFlags.MaxTurns
			}
			outMgr := output.NewOutputManagerWithMode(liveOutputMode(resumeFlags.Quiet, resumeFlags.Verbose))

			if resumeFlags.DryRun {
				cfg, err = autogen.GenerateDryRunConfig(resumeFlags.Topic, level, resumeFlags.Model)
				if err != nil {
					return fmt.Errorf("auto config generation: %w", err)
				}
			} else {
				runner := agent.NewAgentRunner(false)
				stop := outMgr.Activity("Config generation")
				cfg, err = autogen.GenerateConfig(resumeFlags.Topic, level, resumeFlags.Model, runner)
				stop()
				if err != nil {
					return fmt.Errorf("auto config generation: %w", err)
				}
			}

			outMgr.ConfigPreview(cfg, level, levelCaps)

			if err := requireAutoApprovalForNonTTY(resumeFlags.Yes, resumeFlags.DryRun); err != nil {
				return err
			}

			if !resumeFlags.Yes {
				if !confirmProceed() {
					fmt.Println(output.RenderStatus("Deliberation", [][]string{{"Status", "Aborted"}}, "3"))
					return nil
				}
			}
		} else {
			cfg, err = loadConfigArtifact(resumeFlags.Config)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
		}
		agent.ApplyReadOnlyPromptGuard(cfg)

		// Enforce read-only at the tool-execution layer (see run command).
		readOnlyCleanup, _ := agent.WriteReadOnlyConfig(".")
		defer readOnlyCleanup()

		sourceRecords, err := loadTranscriptFile(sourcePath)
		if err != nil {
			return fmt.Errorf("loading source transcript: %w", err)
		}

		var budget *float64
		if resumeFlags.BudgetSet {
			budget = &resumeFlags.Budget
		}

		outMgr := output.NewOutputManagerWithMode(liveOutputMode(resumeFlags.Quiet, resumeFlags.Verbose))
		req := session.ResumeRequest{
			RunRequest: session.RunRequest{
				Topic:       resumeFlags.Topic,
				Config:      cfg,
				OutputPath:  outputPath,
				Window:      resumeFlags.Window,
				MaxTurns:    resumeFlags.MaxTurns,
				TimeLimit:   resumeFlags.TimeLimit,
				Budget:      budget,
				FullContext: resumeFlags.FullContext,
				DryRun:      resumeFlags.DryRun,
				Synthesize:  runSynthesize || autoMode,
			},
			SourceRecords: sourceRecords,
		}
		if autoMode {
			req.Auto = sessionAutoCaps(cmd, levelCaps)
		}

		result, err := session.Resume(req, sessionHooks(outMgr))
		if err != nil {
			return err
		}
		printSessionResult(outMgr, result, fmt.Sprintf("Resumed deliberation complete (%d total turns)", result.Stats.TotalTurns))

		return nil
	},
}

func init() {
	sharedRunFlags(resumeCmd, "resume")
	resumeCmd.Flags().StringVar(&resumeFile, "file", "", "Transcript file path to resume")
	resumeCmd.Flags().BoolVar(&runSynthesize, "synthesize", false, "Run final synthesis agent after deliberation")

	_ = resumeCmd.MarkFlagRequired("topic")
}

// --- helpers ------------------------------------------------------

// sharedRunFlags registers the common flags shared by run and resume
// commands. The prefix distinguishes the backing variable set.
func sharedRunFlags(cmd *cobra.Command, prefix string) {
	var v *runFlagValues
	switch prefix {
	case "run":
		v = &runFlags
	case "resume":
		v = &resumeFlags
	default:
		return
	}

	timeDesc := "Time limit in seconds"
	maxTurnsDesc := "Maximum total turns"
	outputDesc := "Path to write the JSONL transcript"
	budgetDesc := "Cost cap in dollars"
	researchDesc := "Enable topic-inferred web research before deliberation"
	noResearchDesc := "Disable config-enabled web research for this run"
	contextDesc := "Local text context path to include before deliberation (repeatable)"

	if prefix == "resume" {
		timeDesc = "Additional time limit in seconds"
		maxTurnsDesc = "Additional max turns"
		outputDesc = "Path to write the updated JSONL transcript"
		budgetDesc = "Remaining cost budget"
		researchDesc = "Rejected on resume: evidence is reused from the transcript"
		noResearchDesc = "Rejected on resume: evidence is reused from the transcript"
		contextDesc = "Rejected on resume: evidence is reused from the transcript"
	}

	cmd.Flags().StringVarP(&v.Config, "config", "c", "", "Path to YAML agent configuration file")
	cmd.Flags().StringVarP(&v.Topic, "topic", "t", "", "Topic or goal for deliberation")
	cmd.Flags().IntVarP(&v.TimeLimit, "time", "T", 60, timeDesc)
	windowDesc := "Number of predecessor messages each agent sees"
	if prefix == "resume" {
		windowDesc = "Window size"
	}
	cmd.Flags().IntVarP(&v.Window, "window", "w", 2, windowDesc)
	cmd.Flags().IntVarP(&v.MaxTurns, "max-turns", "m", 10, maxTurnsDesc)
	cmd.Flags().StringVarP(&v.Output, "output", "o", "", outputDesc)
	cmd.Flags().BoolVarP(&v.Verbose, "verbose", "v", false, "Print response bodies plus additional live diagnostics")
	cmd.Flags().BoolVarP(&v.Quiet, "quiet", "q", false, "Suppress live response bodies and show metadata/progress only")
	cmd.Flags().Float64Var(&v.Budget, "budget", 0, budgetDesc)
	cmd.Flags().BoolVar(&v.FullContext, "full-context", false, "Show last K messages from ALL agents")
	cmd.Flags().BoolVar(&v.DryRun, "dry-run", false, "Run with simulated agent responses")
	cmd.Flags().StringVar(&v.Auto, "auto", "", "Auto-generate agent config (off, quick, normal, deep, yolo)")
	cmd.Flags().StringVarP(&v.Model, "model", "M", "opencode-go/deepseek-v4-flash", "Model to use for auto config generation and deliberation agents")
	cmd.Flags().BoolVar(&v.Yes, "yes", false, "Skip preview confirmation prompt")
	cmd.Flags().BoolVar(&v.Research, "research", false, researchDesc)
	cmd.Flags().BoolVar(&v.NoResearch, "no-research", false, noResearchDesc)
	cmd.Flags().StringArrayVar(&v.Context, "context", nil, contextDesc)
}

func liveOutputMode(quiet, verbose bool) output.OutputMode {
	if quiet {
		return output.OutputQuiet
	}
	if verbose {
		return output.OutputVerbose
	}
	return output.OutputNormal
}

func applyDefaultModelFromSettings(cmd *cobra.Command, model *string) error {
	return applySettingsDefaults(cmd, model, nil)
}

func applySettingsDefaults(cmd *cobra.Command, model *string, autoLevel *string) error {
	settings, err := config.LoadDefaultSettings()
	if err != nil {
		return fmt.Errorf("loading settings: %w", err)
	}

	if !cmd.Flags().Changed("model") && settings.DefaultModel != "" {
		*model = settings.DefaultModel
	}
	if autoLevel != nil && !cmd.Flags().Changed("auto") && !cmd.Flags().Changed("config") && settings.DefaultAutoLevel != "" {
		*autoLevel = settings.DefaultAutoLevel
	}
	return nil
}

func resolveTranscriptOutput(cmd *cobra.Command, currentOutput, topic string) (string, error) {
	if cmd.Flags().Changed("output") {
		return currentOutput, nil
	}

	settings, err := config.LoadDefaultSettings()
	if err != nil {
		return "", fmt.Errorf("loading settings: %w", err)
	}
	return config.TranscriptOutputPath(topic, settings, time.Now())
}

func generateTranscriptID() int {
	return int(time.Now().UnixMilli())*1000 + rand.Intn(1000)
}

func listTranscriptEntries(dir string) ([]transcriptEntry, error) {
	files, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading transcript store: %w", err)
	}

	var entries []transcriptEntry
	for _, file := range files {
		if file.IsDir() {
			continue
		}
		entry, ok := parseTranscriptFilename(file.Name())
		if !ok {
			continue
		}
		records, err := loadTranscriptFile(filepath.Join(dir, file.Name()))
		if err == nil {
			entry.turns = len(records)
			if meta := transcriptMetadataFromRecords(records); meta != nil && meta.ID > 0 {
				entry.id = meta.ID
			}
		}
		entries = append(entries, entry)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].date.Equal(entries[j].date) {
			return entries[i].filename > entries[j].filename
		}
		return entries[i].date.After(entries[j].date)
	})
	nextID := 1
	for i := range entries {
		if entries[i].id == 0 {
			entries[i].id = nextID
		}
		nextID++
	}
	return entries, nil
}

func parseTranscriptFilename(name string) (transcriptEntry, bool) {
	if len(name) < len("20060102-150405-a.jsonl") || !strings.HasSuffix(name, ".jsonl") || name[15] != '-' {
		return transcriptEntry{}, false
	}

	date, err := time.Parse("20060102-150405", name[:15])
	if err != nil {
		return transcriptEntry{}, false
	}
	slug := strings.TrimSuffix(name[16:], ".jsonl")
	if slug == "" {
		return transcriptEntry{}, false
	}
	return transcriptEntry{date: date, slug: slug, filename: name}, true
}

func resolveResumeSource(fileFlag string, args []string) (string, error) {
	if fileFlag != "" {
		return fileFlag, nil
	}

	return resolveTranscriptSource(args[0])
}

func resolveTranscriptSource(input string) (string, error) {
	settings, err := config.LoadDefaultSettings()
	if err != nil {
		return "", fmt.Errorf("loading settings: %w", err)
	}
	dir := settings.DefaultOutputDir
	if dir == "" {
		dir, err = config.TranscriptStoreDir()
		if err != nil {
			return "", err
		}
	}

	return resolveTranscriptArtifact(input, dir)
}

var stdinIsTerminal = func() bool {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return true
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

func requireAutoApprovalForNonTTY(yes, dryRun bool) error {
	if yes || dryRun {
		return nil
	}
	if stdinIsTerminal() {
		return nil
	}
	return fmt.Errorf("stdin is not a terminal: --auto requires --yes (auto-approve config) or --dry-run (preview only, no execution)")
}

func confirmProceed() bool {
	if !stdinIsTerminal() {
		return true
	}
	fmt.Print("Proceed with deliberation? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	return line == "y" || line == "Y"
}

func loadTranscriptFile(path string) ([]types.TurnRecord, error) {
	return transcript.LoadFileStrict(path)
}

func statsToDict(s types.DeliberationStats) map[string]any {
	perAgent := make(map[string]any, len(s.PerAgent))
	for id, as := range s.PerAgent {
		perAgent[id] = map[string]any{
			"turns":  as.Turns,
			"tokens": as.Tokens,
			"cost":   as.Cost,
		}
	}

	var consensusEvents []any
	for _, ce := range s.ConsensusEvents {
		consensusEvents = append(consensusEvents, map[string]any{
			"turn":      ce.Turn,
			"agent_id":  ce.AgentID,
			"statement": ce.Statement,
		})
	}

	return map[string]any{
		"total_turns":               s.TotalTurns,
		"total_tokens":              s.TotalTokens,
		"total_cost":                s.TotalCost,
		"avg_turn_duration_seconds": s.AvgTurnDuration,
		"per_agent":                 perAgent,
		"consensus_events":          consensusEvents,
	}
}
