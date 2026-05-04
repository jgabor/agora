# Plan: Go Port of Kumbaja

<!-- Level: full · Created: 2026-05-04 · Status: active -->
<!-- Reviewed: 2026-05-04 | Critic issues: 11 found, 8 addressed, 3 dismissed -->
<!-- Updated: 2026-05-04 | Deliberation review: 5 semantic gaps surfaced, ACs tightened -->

## What
Port the entire Kumbaja Python CLI to Go, producing a single static binary that orchestrates heterogeneous LLM agents in ring, star, or mesh topologies. The Go implementation replicates all current commands, configuration formats, termination conditions, and transcript output.

## Why
The decision profile strongly favors Go for CLI infrastructure. A compiled static binary is more portable, starts faster, and signals production intent. The Python version validated the architecture; the Go port solidifies it.

## Constraints
- Existing JSONL transcript schema must remain wire-compatible with the Python version
- YAML configuration format must remain identical
- All current CLI flags and subcommands must behave identically
- The Python branch must remain untouched
- No new runtime dependencies beyond the opencode subprocess
- Five Python-to-Go semantic gaps must be explicitly bridged with behavioral ACs: regex engine dialect (RE2 vs backtracking), exception-based control flow to error-return, signal delivery via channels, `re.sub` callback equivalents for consensus extraction, and JSON marshaling edge cases (null vs zero values, unexported fields)

## Scope
**In**: run, stats, validate, resume commands; ring/star/mesh topologies; time/turn/budget/consensus termination; synthesis; dry-run; signal handling; terminal tables and stats display
**Out**: new topologies, new termination modes, Windows-specific packaging
**Deferred**: TUI enhancements, plugin system, cost estimation model

## Design
A modular internal package layout separates configuration, domain structs, transcript I/O, agent execution, orchestration (including final synthesis), terminal output, and CLI wiring. The CLI layer delegates to these packages without owning business logic. JSON envelopes passed to opencode remain structurally identical. The orchestrator owns the full deliberation loop plus the post-loop synthesis call, keeping the synthesis concern colocated with its invocation point. Terminal output uses lipgloss for styled tables and colored text, separated as a standalone rendering concern with no LLM-call knowledge. Tests follow table-driven Go conventions with dedicated semantic-parity cases covering regex behavior, error propagation, signal channel delivery, and JSON marshal round-trips against Python-produced transcripts. The `opencode` subprocess uses `os/exec` with stdin/stdout JSON streaming. Error handling mirrors Python: non-zero exit produces an error with stderr detail, empty responses raise an error, and missing opencode binary surfaces a clear path-not-found diagnostic. Signal handling bridges Python's signal handler callback to Go's `signal.Notify` channel, delivering SIGTERM/SIGINT with the same partial-transcript-save-and-exit semantics.

## Tasks

### Task 1: Bootstrap module, types, and config
**Depends on**: none
**Status**: ■ complete
**Acceptance**:
- GIVEN a valid YAML config file with topology, agents, and thresholds WHEN the system loads it THEN it returns a validated struct rejecting duplicates, missing IDs, or missing models
- GIVEN an invalid topology string WHEN the system attempts to load it THEN it returns a clear error naming valid options
- GIVEN the defined struct types WHEN marshaled to JSON THEN the output keys match the Python transcript schema including optional fields (null handling, absent keys)

### Task 2: Transcript manager
**Depends on**: Task 1
**Status**: ■ complete
**Acceptance**:
- GIVEN a transcript file path WHEN records are appended THEN each record appears as a single JSON line in the file
- GIVEN an existing transcript with prior records WHEN the manager loads it THEN all prior records are available in memory in order
- GIVEN a ring topology with a lookback window WHEN history is requested for an agent THEN only messages from the predecessor agent are returned up to the window limit
- GIVEN a star or mesh topology WHEN history is requested for an agent THEN the last K messages from any agent are returned

### Task 3: Agent runner and consensus extraction
**Depends on**: Task 1
**Status**: ■ complete
**Acceptance**:
- GIVEN an agent configuration and a JSON envelope WHEN the runner executes in normal mode THEN it invokes the external command and returns the text content plus token and cost metadata
- GIVEN dry-run mode is enabled WHEN the runner executes THEN it returns a placeholder response without invoking the external command
- GIVEN a response containing a consensus marker WHEN the extractor processes it THEN it returns the cleaned text, a true consensus flag, and the extracted statement
- GIVEN a response with no consensus marker WHEN the extractor processes it THEN it returns the original text unchanged with a false consensus flag
- GIVEN a consensus marker spanning multiple lines or with surrounding whitespace WHEN the Go regex using `(?s)` processes it THEN the output matches Python's `re.DOTALL` behavior
- GIVEN the opencode subprocess exits non-zero WHEN the runner receives the result THEN it propagates a descriptive error containing the exit code and stderr content (no swallowed failures)

### Task 4: Orchestrator with termination logic and synthesis
**Depends on**: Task 2, Task 3
**Status**: ■ complete
**Acceptance**:
- GIVEN a deliberation with no prior transcript WHEN the orchestrator starts THEN it emits an orchestrator seed message as the first record
- GIVEN a running deliberation WHEN the elapsed time exceeds the configured limit THEN the loop stops and records the time limit as the halt reason
- GIVEN a running deliberation WHEN the turn count reaches the maximum THEN the loop stops and records max turns as the halt reason
- GIVEN a configured budget and accumulated costs exceeding it THEN the loop stops and records budget exceeded as the halt reason
- GIVEN a configured consensus threshold and consecutive consensus markers meeting it THEN the loop stops and records consensus as the halt reason
- GIVEN an interrupt signal during deliberation WHEN the signal is received via Go's `signal.Notify` channel THEN the loop stops, the partial transcript is saved to disk, and the process exits gracefully
- GIVEN a completed deliberation with a synthesis model WHEN synthesis is requested THEN the transcript is formatted, sent to the external command, and a parsed structured summary with key arguments, agreements, tensions, recommendation, and confidence is returned

### Task 5: Terminal output
**Depends on**: Task 1
**Status**: ■ complete
**Acceptance**:
- GIVEN a deliberation start state WHEN the output manager renders the header THEN it displays the topic, agent list, and settings with styled panels and colored text
- GIVEN turn records and cumulative statistics WHEN the output manager renders progress or final stats THEN it displays per-agent metrics (turns, tokens, cost), totals, and halt reason in formatted tables with right-aligned columns
- GIVEN a synthesis result WHEN the output manager renders it THEN key arguments, agreements, tensions, recommendation, and confidence are displayed with appropriate styling per section

### Task 6: CLI commands
**Depends on**: Task 4, Task 5
**Status**: ■ complete
**Acceptance**:
- GIVEN the installed binary WHEN the user invokes the run subcommand with all required flags THEN the deliberation executes and writes a JSONL transcript to the specified path
- GIVEN an existing transcript file WHEN the user invokes the stats subcommand THEN it prints aggregated statistics without running a deliberation
- GIVEN a config file path WHEN the user invokes the validate subcommand THEN it confirms validity or reports errors
- GIVEN an existing transcript and configuration WHEN the user invokes the resume subcommand THEN it loads prior records, continues the deliberation from the last turn, and respects the additional time and turn limits

### Task 7: Test suite with semantic-parity verification
**Depends on**: Task 1, Task 2, Task 3
**Status**: □ pending
**Acceptance**:
- GIVEN the test runner is executed WHEN all package tests run THEN they pass with coverage over config, transcript, and agent packages
- GIVEN tests for config validation WHEN invalid inputs are provided THEN at least one test verifies each distinct validation rule
- GIVEN tests for transcript history WHEN different topologies and window sizes are used THEN tests verify correct message inclusion and exclusion
- GIVEN a Python-produced JSONL transcript WHEN the Go transcript manager loads and marshals it THEN serialization round-trips are lossless and struct fields map correctly
- GIVEN the consensus extraction function WHEN tested against multiline, case-insensitive, whitespace-heavy, and absent marker inputs THEN Go results match Python `extract_consensus` output character-for-character
- GIVEN the agent runner WHEN tested with a mock subprocess simulating non-zero exit, empty output, and partial JSON streams THEN each failure mode produces the expected error type and message
- **Proportionality target**: 1 pass + 1 fail per testable unit. Semantic-gap units (regex, signal, JSON marshal, error propagation) get 2 positive + 2 negative cases each due to high porting risk.

### Task 8: Plan-level freshness checkpoint
**Depends on**: all prior tasks
**Status**: □ pending
**Acceptance**:
- GIVEN this plan's user-facing work has shipped WHEN CHANGELOG.md is checked THEN it has Added/Changed/Fixed entries under [Unreleased] covering each task's user-visible impact
- GIVEN this plan is otherwise complete WHEN PROGRESS.md is checked THEN it has at least one cycle entry whose **What** field summarizes the plan and whose **Commits** field lists the commits this plan produced
- GIVEN this plan is otherwise complete WHEN TODO.md is checked THEN every task has a corresponding Resolved entry and the active milestone has been advanced to the next planned version

## Overall Acceptance
- GIVEN the same YAML config and topic used with the Python version WHEN the Go binary runs a deliberation THEN the produced JSONL transcript can be parsed by the Python stats command without errors
- GIVEN a dry-run deliberation with identical settings across both versions WHEN outputs are compared THEN the Go version produces the same number of turns with the same agent sequence and halt reason
- GIVEN the Go binary is built for the current platform WHEN the executable is inspected THEN it has no dynamic library dependencies beyond the system libc

## Surprises
