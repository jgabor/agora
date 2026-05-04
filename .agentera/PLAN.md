# Plan: Auto Mode — LLM-Generated Agent Configs

<!-- Level: full | Created: 2026-05-04 | Status: active -->

## What
Add `--auto <level>` flag to `agora run` that generates a deliberation config from the topic via a meta-LLM call. Five levels (Off/Quick/Normal/Deep/YOLO) set hard-coded caps on agents, turns, and time. The LLM designs agent roles and system prompts within those caps. Generated YAML flows through existing LoadConfig validation. User previews the config before deliberation starts. Synthesis always runs in auto mode.

## Why
Currently requires a handcrafted YAML config — a barrier for casual or exploratory use. Auto mode makes `agora run --auto normal --topic "..."` a one-liner, like running a test suite. Decision 4 (provisional) drove this design. Aligns with VISION.md principle: sharp tool, one thing well.

## Constraints
- `--auto` and `--config` are mutually exclusive; `--config` path unchanged
- Same model designs and deliberates (Decision 4)
- Level caps hard-coded in binary, not LLM-advised (Decision 4)
- Synthesis forced on for all auto levels (Decision 4)
- YOLO: truly uncapped, consensus-only halt, user accepts risk (Decision 4)
- Non-interactive/pipe contexts must not block on preview prompt
- Orchestrator must handle MaxTurns=0 as "unlimited" for YOLO

## Scope
**In**: AutoLevel types, level caps, config generation via Runner, LoadConfigFromBytes, CLI --auto flag, preview-confirm flow, YOLO infinite-turns support, tests
**Out**: Auto mode for `resume` command (deferred), configurable level caps (deferred), --yes flag to skip preview (deferred)
**Deferred**: Resume with auto, custom level definitions, preview skip flag

## Design

### New types (internal/types)
AutoLevel string type with constants AutoOff/AutoQuick/AutoNormal/AutoDeep/AutoYOLO. ParseAutoLevel function. LevelCaps struct with MaxAgents, MaxTurns, TimeLimit fields. CapsForLevel function returns hard-coded caps.

### Config bytes loader (internal/config)
LoadConfigFromBytes function — same logic as LoadConfig but reads from []byte instead of file path. Required by autogen since LLM returns YAML string, not a file.

### Auto config generator (internal/autogen — new package)
GenerateConfig function: takes topic, AutoLevel, model string, Runner. Creates a synthetic AgentConfig for "config designer" with a system prompt instructing the LLM to produce valid YAML within the level's caps. Calls Runner.Run(), parses response YAML through LoadConfigFromBytes, validates agent count against level caps. Returns DeliberationConfig.

System prompt for config designer: instructs the model to return ONLY valid YAML matching the DeliberationConfig schema. Includes the level caps as constraints. Specifies that agent IDs must be lowercase with underscores, system prompts should be 2-4 sentence role descriptions.

### CLI integration (cmd/agora/main.go)
Add --auto string flag to runCmd. In PreRunE: if --auto is set, --config is not required; if both --auto and --config are set, error. In RunE: when --auto is set, call autogen.GenerateConfig, display preview panel, prompt for confirmation (skip if stdin is not a TTY), then construct DeliberationState using level caps for MaxTurns/TimeLimit and the generated DeliberationConfig.

### Orchestrator YOLO support
Change main loop condition from `o.state.Turn < o.state.MaxTurns` to `o.state.MaxTurns <= 0 || o.state.Turn < o.state.MaxTurns`. MaxTurns=0 means "no turn cap." This parallels the existing TimeLimit=0 convention for "no time cap."

## Tasks

### Task 1: AutoLevel types and level caps
**Depends on**: none
**Status**: ■ complete
**Acceptance**:
▸ GIVEN a string "quick" WHEN ParseAutoLevel called THEN returns AutoQuick, no error
▸ GIVEN a string "yolo" WHEN ParseAutoLevel called THEN returns AutoYOLO, no error
▸ GIVEN a string "off" WHEN ParseAutoLevel called THEN returns AutoOff, no error
▸ GIVEN CapsForLevel(AutoQuick) called THEN returns MaxAgents=2, MaxTurns=4, TimeLimit=60
▸ GIVEN CapsForLevel(AutoYOLO) called THEN returns MaxAgents=0, MaxTurns=0, TimeLimit=0 (0 = unlimited)
▸ GIVEN an invalid string "turbo" WHEN ParseAutoLevel called THEN returns error mentioning valid levels
▸ Test proportionality: 1 pass + 1 fail per testable unit (ParseAutoLevel, CapsForLevel)

### Task 2: LoadConfigFromBytes in config package
**Depends on**: none
**Status**: ■ complete
**Acceptance**:
▸ GIVEN valid YAML bytes for a 2-agent config WHEN LoadConfigFromBytes called THEN returns DeliberationConfig with 2 agents, no error
▸ GIVEN invalid YAML bytes (missing agents) WHEN LoadConfigFromBytes called THEN returns error about missing agents
▸ GIVEN LoadConfig("path/to/file.yaml") called THEN behavior unchanged from current
▸ Test proportionality: 1 pass + 1 fail

### Task 3: Auto config generator (internal/autogen)
**Depends on**: Task 1, Task 2
**Status**: ■ complete
**Acceptance**:
▸ GIVEN topic "Is microservices worth it?", AutoQuick, a mock runner returning valid YAML WHEN GenerateConfig called THEN returns DeliberationConfig with ≤2 agents
▸ GIVEN a mock runner returning invalid YAML WHEN GenerateConfig called THEN returns error about config generation failure, not a cryptic parse error
▸ GIVEN a mock runner returning 5 agents for AutoQuick (max 2) WHEN GenerateConfig called THEN returns error about exceeding level caps
▸ GIVEN AutoYOLO WHEN GenerateConfig called THEN system prompt does not include agent count constraint
▸ Test proportionality: 1 pass + 1 fail per testable unit, edge case expansion for cap enforcement (3 branches: valid, invalid YAML, cap exceeded)

### Task 4: CLI --auto flag and preview-confirm flow
**Depends on**: Task 3
**Status**: □ pending
**Acceptance**:
▸ GIVEN `agora run --auto normal --topic "test"` WHEN executed THEN config is generated, preview shown, user prompted for confirmation
▸ GIVEN `agora run --auto normal --config foo.yaml` WHEN executed THEN error: --auto and --config are mutually exclusive
▸ GIVEN `agora run --auto normal` without --topic WHEN executed THEN error: --topic is required with --auto
▸ GIVEN user declines preview prompt WHEN confirmed THEN exits with message, exit code 0
▸ GIVEN --auto is set THEN synthesis is forced on regardless of --synthesize flag
▸ GIVEN non-interactive stdin (piped) WHEN preview would prompt THEN preview is printed but no prompt blocks execution
▸ GIVEN --auto is NOT set WHEN `agora run --config ...` executed THEN behavior identical to current
▸ Test proportionality: 1 pass + 1 fail per testable unit (flag validation, mutual exclusion, synthesis forcing)

### Task 5: YOLO infinite turns in orchestrator
**Depends on**: Task 1
**Status**: □ pending
**Acceptance**:
▸ GIVEN MaxTurns=0 WHEN orchestrator Run() called THEN loop continues until another termination condition fires (consensus, time, error)
▸ GIVEN MaxTurns=10 WHEN orchestrator Run() called THEN loop stops at 10 turns (behavior unchanged)
▸ GIVEN MaxTurns=0, consensus threshold=3, 3 consecutive consensuses WHEN orchestrator Run() called THEN halts with "consensus" reason
▸ Test proportionality: 1 pass + 1 fail

### Task 6: Plan-level freshness checkpoint
**Depends on**: Task 4, Task 5
**Status**: □ pending
**Acceptance**:
▸ GIVEN all prior tasks complete WHEN this task runs THEN CHANGELOG.md has entry for auto mode feature
▸ GIVEN all prior tasks complete WHEN this task runs THEN PROGRESS.md has cycle entry summarizing auto mode work
▸ GIVEN all prior tasks complete WHEN this task runs THEN TODO.md updated with auto mode items (any follow-ups)

## Overall Acceptance
▸ GIVEN `agora run --auto quick --topic "test"` WHEN executed with --dry-run THEN generates config, shows preview, runs deliberation with ≤2 agents, synthesis forced on
▸ GIVEN `agora run --auto yolo --topic "test"` WHEN executed with --dry-run THEN generates config with no cap constraints, deliberation runs until consensus or user interrupt
▸ GIVEN `agora run --config examples/example-default.yaml --topic "test"` WHEN executed THEN behavior identical to current (backward compat)
▸ GIVEN invalid auto level `agora run --auto turbo --topic "test"` WHEN executed THEN error message lists valid levels

## Surprises
