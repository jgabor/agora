package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"

	"github.com/jgabor/kumbaja"
	"github.com/spf13/cobra"
)

var version = "0.1.0"

func main() {
	rootCmd.AddCommand(runCmd, statsCmd, validateCmd, resumeCmd)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------------
// Root command
// ---------------------------------------------------------------------------

var rootCmd = &cobra.Command{
	Use:     "kumbaja",
	Short:   "Kumbaja — Closed-loop multi-agent deliberation system",
	Version: version,
}

// ---------------------------------------------------------------------------
// run
// ---------------------------------------------------------------------------

var (
	runConfig      string
	runTopic       string
	runTimeLimit   int
	runWindow      int
	runMaxTurns    int
	runOutput      string
	runVerbose     bool
	runBudget      float64
	runBudgetFlag  bool // tracks whether --budget was explicitly set
	runSynthesize  bool
	runFullContext bool
	runDryRun      bool
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run a multi-agent deliberation",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := kumbaja.LoadConfig(runConfig)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		var budget *float64
		if runBudgetFlag {
			budget = &runBudget
		}

		state := &kumbaja.DeliberationState{
			Config:      cfg,
			Topic:       runTopic,
			Window:      runWindow,
			MaxTurns:    runMaxTurns,
			TimeLimit:   runTimeLimit,
			Budget:      budget,
			FullContext: runFullContext,
		}

		transcript := kumbaja.NewTranscriptManager(runOutput)
		outMgr := kumbaja.NewOutputManager(runVerbose)
		runner := kumbaja.NewAgentRunner(runDryRun)
		orchestrator := kumbaja.NewOrchestrator(state, transcript, runner)

		outMgr.DeliberationHeader(state)

		stats := orchestrator.Run()

		outMgr.FinalStats(transcript.Records(), state)

		if runSynthesize {
			result := orchestrator.Synthesize()
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
	runCmd.Flags().IntVarP(&runTimeLimit, "time", "T", 0, "Time limit in seconds")
	runCmd.Flags().IntVarP(&runWindow, "window", "w", 0, "Number of predecessor messages each agent sees")
	runCmd.Flags().IntVarP(&runMaxTurns, "max-turns", "m", 0, "Maximum total turns")
	runCmd.Flags().StringVarP(&runOutput, "output", "o", "", "Path to write the JSONL transcript")
	runCmd.Flags().BoolVarP(&runVerbose, "verbose", "v", false, "Print agent responses in real-time")
	runCmd.Flags().Float64Var(&runBudget, "budget", 0, "Cost cap in dollars")
	runCmd.Flags().BoolVar(&runSynthesize, "synthesize", false, "Run final synthesis agent after deliberation")
	runCmd.Flags().BoolVar(&runFullContext, "full-context", false, "Show last K messages from ALL agents (not just predecessor)")
	runCmd.Flags().BoolVar(&runDryRun, "dry-run", false, "Run with simulated agent responses (no LLM calls)")

	_ = runCmd.MarkFlagRequired("config")
	_ = runCmd.MarkFlagRequired("topic")
	_ = runCmd.MarkFlagRequired("time")
	_ = runCmd.MarkFlagRequired("window")
	_ = runCmd.MarkFlagRequired("max-turns")
	_ = runCmd.MarkFlagRequired("output")

	// Track whether --budget was explicitly set (since 0 is the zero-value).
	runCmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		runBudgetFlag = cmd.Flags().Changed("budget")
		return nil
	}
}

// ---------------------------------------------------------------------------
// stats
// ---------------------------------------------------------------------------

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

		stats := kumbaja.ComputeStats(records)
		outMgr := kumbaja.NewOutputManager(false)
		outMgr.PrintStats(statsToDict(stats))

		return nil
	},
}

// ---------------------------------------------------------------------------
// validate
// ---------------------------------------------------------------------------

var validateCmd = &cobra.Command{
	Use:   "validate CONFIG",
	Short: "Validate a configuration file without running deliberation",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := kumbaja.LoadConfig(args[0])
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

// ---------------------------------------------------------------------------
// resume
// ---------------------------------------------------------------------------

var (
	resumeConfig      string
	resumeTopic       string
	resumeTimeLimit   int
	resumeWindow      int
	resumeMaxTurns    int
	resumeOutput      string
	resumeVerbose     bool
	resumeBudget      float64
	resumeBudgetFlag  bool
	resumeFullContext bool
	resumeDryRun      bool
)

var resumeCmd = &cobra.Command{
	Use:   "resume TRANSCRIPT",
	Short: "Continue deliberation from an existing transcript",
	Args:  cobra.ExactArgs(1),
	PreRunE: func(cmd *cobra.Command, args []string) error {
		resumeBudgetFlag = cmd.Flags().Changed("budget")
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		sourcePath := args[0]

		cfg, err := kumbaja.LoadConfig(resumeConfig)
		if err != nil {
			return fmt.Errorf("loading config: %w", err)
		}

		// Load records from the source transcript.
		sourceRecords, err := loadTranscriptFile(sourcePath)
		if err != nil {
			return fmt.Errorf("loading source transcript: %w", err)
		}
		if len(sourceRecords) == 0 {
			return fmt.Errorf("no existing transcript found — use 'kumbaja run' to start")
		}

		// Create transcript manager for the output path and load any existing records.
		tm := kumbaja.NewTranscriptManager(resumeOutput)
		if _, err := tm.LoadExisting(); err != nil {
			return fmt.Errorf("loading existing output transcript: %w", err)
		}

		// Copy source records into the output manager.
		for _, r := range sourceRecords {
			if err := tm.Append(r); err != nil {
				return fmt.Errorf("copying records: %w", err)
			}
		}

		// Determine starting turn (count non-orchestrator turns).
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

		state := &kumbaja.DeliberationState{
			Config:      cfg,
			Topic:       resumeTopic,
			Window:      resumeWindow,
			MaxTurns:    existingTurns + resumeMaxTurns,
			TimeLimit:   resumeTimeLimit,
			Budget:      budget,
			FullContext: resumeFullContext,
			Turn:        existingTurns,
		}

		outMgr := kumbaja.NewOutputManager(resumeVerbose)
		runner := kumbaja.NewAgentRunner(resumeDryRun)
		orchestrator := kumbaja.NewOrchestrator(state, tm, runner)

		outMgr.DeliberationHeader(state)

		stats := orchestrator.Run()

		outMgr.FinalStats(tm.Records(), state)
		outMgr.Success(fmt.Sprintf("Resumed deliberation complete (%d total turns)", stats.TotalTurns))
		outMgr.Success(fmt.Sprintf("Transcript: %s", resumeOutput))

		return nil
	},
}

func init() {
	resumeCmd.Flags().StringVarP(&resumeConfig, "config", "c", "", "Path to YAML agent configuration file")
	resumeCmd.Flags().StringVarP(&resumeTopic, "topic", "t", "", "Topic or goal for deliberation")
	resumeCmd.Flags().IntVarP(&resumeTimeLimit, "time", "T", 0, "Additional time limit in seconds")
	resumeCmd.Flags().IntVarP(&resumeWindow, "window", "w", 0, "Window size")
	resumeCmd.Flags().IntVarP(&resumeMaxTurns, "max-turns", "m", 0, "Additional max turns")
	resumeCmd.Flags().StringVarP(&resumeOutput, "output", "o", "", "Path to write the updated JSONL transcript")
	resumeCmd.Flags().BoolVarP(&resumeVerbose, "verbose", "v", false, "Print agent responses in real-time")
	resumeCmd.Flags().Float64Var(&resumeBudget, "budget", 0, "Remaining cost budget")
	resumeCmd.Flags().BoolVar(&resumeFullContext, "full-context", false, "Show last K messages from ALL agents (not just predecessor)")
	resumeCmd.Flags().BoolVar(&resumeDryRun, "dry-run", false, "Run with simulated agent responses (no LLM calls)")

	_ = resumeCmd.MarkFlagRequired("config")
	_ = resumeCmd.MarkFlagRequired("topic")
	_ = resumeCmd.MarkFlagRequired("time")
	_ = resumeCmd.MarkFlagRequired("window")
	_ = resumeCmd.MarkFlagRequired("max-turns")
	_ = resumeCmd.MarkFlagRequired("output")
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// loadTranscriptFile reads a JSONL transcript file and returns the parsed records.
func loadTranscriptFile(path string) ([]kumbaja.TurnRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	var records []kumbaja.TurnRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		var r kumbaja.TurnRecord
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			continue // skip malformed lines
		}
		records = append(records, r)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return records, nil
}

// statsToDict converts a DeliberationStats value into the map[string]any
// expected by OutputManager.PrintStats.
func statsToDict(s kumbaja.DeliberationStats) map[string]any {
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
