package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jgabor/agora/internal/agent"
	"github.com/jgabor/agora/internal/autogen"
	"github.com/jgabor/agora/internal/config"
	"github.com/jgabor/agora/internal/orchestrator"
	"github.com/jgabor/agora/internal/output"
	"github.com/jgabor/agora/internal/transcript"
	"github.com/jgabor/agora/internal/types"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

func main() {
	rootCmd.SetUsageTemplate(rootCmd.UsageTemplate() + "\n\nAuthor:\n  Jonathan Gabor (https://jgabor.se)\n\nSource:\n  https://github.com/jgabor/agora\n")
	rootCmd.AddCommand(runCmd, statsCmd, validateCmd, resumeCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:     "agora",
	Short:   "Agora — Closed-loop multi-agent deliberation system",
	Version: version,
}

// --- run ----------------------------------------------------------

var (
	runConfig      string
	runTopic       string
	runTimeLimit   int    = 60
	runWindow      int    = 2
	runMaxTurns    int    = 10
	runOutput      string = "transcript.jsonl"
	runVerbose     bool
	runBudget      float64
	runBudgetFlag  bool
	runSynthesize  bool
	runFullContext bool
	runDryRun      bool
	runAuto        string
	runModel       string = "opencode-go/deepseek-v4-flash"
	runYes         bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a multi-agent deliberation",
	RunE: func(cmd *cobra.Command, args []string) error {
		var cfg *types.DeliberationConfig
		var levelCaps types.LevelCaps
		autoMode := runAuto != ""

		if autoMode {
			level, err := types.ParseAutoLevel(runAuto)
			if err != nil {
				return err
			}
			levelCaps = types.CapsForLevel(level)

			if runDryRun {
				cfg, err = autogen.GenerateDryRunConfig(runTopic, level, runModel)
				if err != nil {
					return fmt.Errorf("auto config generation: %w", err)
				}
			} else {
				runner := agent.NewAgentRunner(false)
				cfg, err = autogen.GenerateConfig(runTopic, level, runModel, runner)
				if err != nil {
					return fmt.Errorf("auto config generation: %w", err)
				}
			}

			outMgr := output.NewOutputManager(runVerbose)
			outMgr.ConfigPreview(cfg, level, levelCaps)

			if !runYes {
				if !confirmProceed() {
					fmt.Println("Aborted.")
					return nil
				}
			}
		} else {
			var err error
			cfg, err = config.LoadConfig(runConfig)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
		}

		var budget *float64
		if runBudgetFlag {
			budget = &runBudget
		}

		state := &types.DeliberationState{
			Config:      cfg,
			Topic:       runTopic,
			Window:      runWindow,
			MaxTurns:    runMaxTurns,
			TimeLimit:   runTimeLimit,
			Budget:      budget,
			FullContext: runFullContext,
		}

		// Override MaxTurns and TimeLimit with level caps for auto mode
		if autoMode {
			state.MaxTurns = levelCaps.MaxTurns
			state.TimeLimit = levelCaps.TimeLimit
		}

		tm := transcript.NewTranscriptManager(runOutput)
		outMgr := output.NewOutputManager(runVerbose)
		runner := agent.NewAgentRunner(runDryRun)
		orch := orchestrator.NewOrchestrator(state, tm, runner)
		orch.OnTurn(outMgr.TurnProgress)

		outMgr.DeliberationHeader(state)

		stats := orch.Run()

		outMgr.FinalStats(tm.Records(), state)

		// Force synthesis ON for auto mode
		if runSynthesize || autoMode {
			result := orch.Synthesize()
			if result != nil {
				outMgr.SynthesizeHeader()
				outMgr.SynthesisResult(result)
				outMgr.Success("Synthesis complete")
			}
		}

		outMgr.Success(fmt.Sprintf("Deliberation complete (%d turns)", stats.TotalTurns))
		outMgr.Success(fmt.Sprintf("Transcript: %s", runOutput))
		outMgr.Info(fmt.Sprintf("Halted by: %s", state.HaltedBy))

		return nil
	},
}

func init() {
	runCmd.Flags().StringVarP(&runConfig, "config", "c", "", "Path to YAML agent configuration file")
	runCmd.Flags().StringVarP(&runTopic, "topic", "t", "", "Topic or goal for deliberation")
	runCmd.Flags().IntVarP(&runTimeLimit, "time", "T", 60, "Time limit in seconds")
	runCmd.Flags().IntVarP(&runWindow, "window", "w", 2, "Number of predecessor messages each agent sees")
	runCmd.Flags().IntVarP(&runMaxTurns, "max-turns", "m", 10, "Maximum total turns")
	runCmd.Flags().StringVarP(&runOutput, "output", "o", "transcript.jsonl", "Path to write the JSONL transcript")
	runCmd.Flags().BoolVarP(&runVerbose, "verbose", "v", false, "Print agent responses in real-time")
	runCmd.Flags().Float64Var(&runBudget, "budget", 0, "Cost cap in dollars")
	runCmd.Flags().BoolVar(&runSynthesize, "synthesize", false, "Run final synthesis agent after deliberation")
	runCmd.Flags().BoolVar(&runFullContext, "full-context", false, "Show last K messages from ALL agents")
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "Run with simulated agent responses")
	runCmd.Flags().StringVar(&runAuto, "auto", "", "Auto-generate agent config (off, quick, normal, deep, yolo)")
	runCmd.Flags().StringVarP(&runModel, "model", "M", "opencode-go/deepseek-v4-flash", "Model to use for auto config generation and deliberation agents")
	runCmd.Flags().BoolVar(&runYes, "yes", false, "Skip preview confirmation prompt")

	_ = runCmd.MarkFlagRequired("topic")

	runCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		runBudgetFlag = cmd.Flags().Changed("budget")
		if err := applySettingsDefaults(cmd, &runModel, &runAuto); err != nil {
			return err
		}

		autoSet := runAuto != ""
		configSet := runConfig != ""

		if autoSet && configSet {
			return fmt.Errorf("cannot use --auto and --config together")
		}

		if autoSet {
			level, err := types.ParseAutoLevel(runAuto)
			if err != nil {
				return err
			}
			if level == types.AutoOff {
				return fmt.Errorf("--auto off is not a valid mode; omit --auto to run with a config file")
			}
			if runTopic == "" {
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

// --- stats --------------------------------------------------------

var statsCmd = &cobra.Command{
	Use:   "stats TRANSCRIPT",
	Short: "Print statistics from a deliberation transcript",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		records, err := loadTranscriptFile(args[0])
		if err != nil {
			return fmt.Errorf("loading transcript: %w", err)
		}
		if len(records) == 0 {
			return fmt.Errorf("transcript empty or invalid")
		}

		stats := types.ComputeStats(records)
		outMgr := output.NewOutputManager(false)
		outMgr.PrintStats(statsToDict(stats))

		return nil
	},
}

// --- validate -----------------------------------------------------

var validateCmd = &cobra.Command{
	Use:   "validate CONFIG",
	Short: "Validate a configuration file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig(args[0])
		if err != nil {
			return fmt.Errorf("ERROR: %w", err)
		}

		fmt.Println("Configuration is valid.")
		fmt.Printf("  Topology: %s\n", cfg.Topology)
		fmt.Printf("  Agents (%d):\n", len(cfg.Agents))
		for _, a := range cfg.Agents {
			fmt.Printf("    - %s (%s)\n", a.ID, a.Model)
		}
		if cfg.ConsensusThreshold > 0 {
			fmt.Printf("  Consensus threshold: %d\n", cfg.ConsensusThreshold)
		}
		if cfg.SynthesisModel != nil {
			fmt.Printf("  Synthesis model: %s\n", *cfg.SynthesisModel)
		}

		return nil
	},
}

// --- resume -------------------------------------------------------

var (
	resumeConfig      string
	resumeTopic       string
	resumeTimeLimit   int    = 60
	resumeWindow      int    = 2
	resumeMaxTurns    int    = 10
	resumeOutput      string = "transcript.jsonl"
	resumeVerbose     bool
	resumeBudget      float64
	resumeBudgetFlag  bool
	resumeFullContext bool
	resumeDryRun      bool
	resumeAuto        string
	resumeModel       string = "opencode-go/deepseek-v4-flash"
	resumeYes         bool
)

var resumeCmd = &cobra.Command{
	Use:   "resume TRANSCRIPT",
	Short: "Continue deliberation from an existing transcript",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		resumeBudgetFlag = cmd.Flags().Changed("budget")
		if err := applySettingsDefaults(cmd, &resumeModel, &resumeAuto); err != nil {
			return err
		}

		autoSet := resumeAuto != ""
		configSet := resumeConfig != ""

		if autoSet && configSet {
			return fmt.Errorf("cannot use --auto and --config together")
		}

		if autoSet {
			level, err := types.ParseAutoLevel(resumeAuto)
			if err != nil {
				return err
			}
			if level == types.AutoOff {
				return fmt.Errorf("--auto off is not a valid mode; omit --auto to run with a config file")
			}
			if resumeTopic == "" {
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
		autoMode := resumeAuto != ""

		if autoMode {
			level, err := types.ParseAutoLevel(resumeAuto)
			if err != nil {
				return err
			}
			levelCaps = types.CapsForLevel(level)

			if resumeDryRun {
				cfg, err = autogen.GenerateDryRunConfig(resumeTopic, level, resumeModel)
				if err != nil {
					return fmt.Errorf("auto config generation: %w", err)
				}
			} else {
				runner := agent.NewAgentRunner(false)
				cfg, err = autogen.GenerateConfig(resumeTopic, level, resumeModel, runner)
				if err != nil {
					return fmt.Errorf("auto config generation: %w", err)
				}
			}

			outMgr := output.NewOutputManager(resumeVerbose)
			outMgr.ConfigPreview(cfg, level, levelCaps)

			if !resumeYes {
				if !confirmProceed() {
					fmt.Println("Aborted.")
					return nil
				}
			}
		} else {
			var err error
			cfg, err = config.LoadConfig(resumeConfig)
			if err != nil {
				return fmt.Errorf("loading config: %w", err)
			}
		}

		sourceRecords, err := loadTranscriptFile(args[0])
		if err != nil {
			return fmt.Errorf("loading source transcript: %w", err)
		}
		if len(sourceRecords) == 0 {
			return fmt.Errorf("no existing transcript found — use 'agora run' to start")
		}

		tm := transcript.NewTranscriptManager(resumeOutput)
		if _, err := tm.LoadExisting(); err != nil {
			return fmt.Errorf("loading existing output transcript: %w", err)
		}

		for _, r := range sourceRecords {
			if err := tm.Append(r); err != nil {
				return fmt.Errorf("copying records: %w", err)
			}
		}

		existingTurns := 0
		for _, r := range sourceRecords {
			if r.AgentID != "orchestrator" {
				existingTurns++
			}
		}

		var budget *float64
		if resumeBudgetFlag {
			budget = &resumeBudget
		}

		state := &types.DeliberationState{
			Config:      cfg,
			Topic:       resumeTopic,
			Window:      resumeWindow,
			MaxTurns:    existingTurns + resumeMaxTurns,
			TimeLimit:   resumeTimeLimit,
			Budget:      budget,
			FullContext: resumeFullContext,
			Turn:        existingTurns,
		}

		// Override MaxTurns and TimeLimit with level caps for auto mode
		if autoMode {
			state.TimeLimit = levelCaps.TimeLimit
			if levelCaps.MaxTurns == 0 {
				state.MaxTurns = 0
			} else {
				state.MaxTurns = existingTurns + levelCaps.MaxTurns
			}
		}

		outMgr := output.NewOutputManager(resumeVerbose)
		runner := agent.NewAgentRunner(resumeDryRun)
		orch := orchestrator.NewOrchestrator(state, tm, runner)
		orch.OnTurn(outMgr.TurnProgress)

		outMgr.DeliberationHeader(state)

		stats := orch.Run()

		outMgr.FinalStats(tm.Records(), state)
		outMgr.Success(fmt.Sprintf("Resumed deliberation complete (%d total turns)", stats.TotalTurns))
		outMgr.Success(fmt.Sprintf("Transcript: %s", resumeOutput))

		return nil
	},
}

func init() {
	resumeCmd.Flags().StringVarP(&resumeConfig, "config", "c", "", "Path to YAML agent configuration file")
	resumeCmd.Flags().StringVarP(&resumeTopic, "topic", "t", "", "Topic or goal for deliberation")
	resumeCmd.Flags().IntVarP(&resumeTimeLimit, "time", "T", 60, "Additional time limit in seconds")
	resumeCmd.Flags().IntVarP(&resumeWindow, "window", "w", 2, "Window size")
	resumeCmd.Flags().IntVarP(&resumeMaxTurns, "max-turns", "m", 10, "Additional max turns")
	resumeCmd.Flags().StringVarP(&resumeOutput, "output", "o", "transcript.jsonl", "Path to write the updated JSONL transcript")
	resumeCmd.Flags().BoolVarP(&resumeVerbose, "verbose", "v", false, "Print agent responses in real-time")
	resumeCmd.Flags().Float64Var(&resumeBudget, "budget", 0, "Remaining cost budget")
	resumeCmd.Flags().BoolVar(&resumeFullContext, "full-context", false, "Show last K messages from ALL agents")
	resumeCmd.Flags().BoolVar(&resumeDryRun, "dry-run", false, "Run with simulated agent responses")
	resumeCmd.Flags().StringVar(&resumeAuto, "auto", "", "Auto-generate agent config (off, quick, normal, deep, yolo)")
	resumeCmd.Flags().StringVarP(&resumeModel, "model", "M", "opencode-go/deepseek-v4-flash", "Model to use for auto config generation and deliberation agents")
	resumeCmd.Flags().BoolVar(&resumeYes, "yes", false, "Skip preview confirmation prompt")

	_ = resumeCmd.MarkFlagRequired("topic")
}

// --- helpers ------------------------------------------------------

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

func confirmProceed() bool {
	fi, _ := os.Stdin.Stat()
	isTerminal := (fi.Mode() & os.ModeCharDevice) != 0
	if !isTerminal {
		return true
	}
	fmt.Print("Proceed with deliberation? [y/N] ")
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(line)
	return line == "y" || line == "Y"
}

func loadTranscriptFile(path string) ([]types.TurnRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var records []types.TurnRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var r types.TurnRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue
		}
		records = append(records, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
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
