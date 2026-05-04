# Plan: CLI defaults and example configs

<!-- Level: light | Created: 2026-05-04 | Status: active -->
<!-- Reviewed: 2026-05-04 | Critic issues: 10 found, 7 addressed, 3 dismissed -->

## What
Set sensible default values for optional CLI flags so `agora run` needs only `--config` and `--topic`. Move `example-config.yaml` to `examples/example-default.yaml` and create 5 themed example configs demonstrating different topologies, agent panels, and features.

## Why
VISION.md promises "run agora on it" as routine. Currently every flag is required. Defaulting time/window/max-turns/output lets the user focus on what matters — config and topic. The example suite shows what's possible: ring/star/mesh topologies, consensus thresholds, synthesis models, and budget constraints.

## Constraints
- `--config` and `--topic` stay required (no meaningful defaults)
- `--synthesize` stays `false` by default (cost-sensitive, no breaking change)
- Existing tests must pass; `TestLoadConfigExampleFile` path must be updated
- Example YAML files must pass `agora validate` individually
- README quick-start must reflect new defaults

## Scope
**In**: CLI flag defaults for `run` and `resume`, help text updates showing defaults, `examples/` directory with 6 configs, test and README updates
**Out**: embedding a default config in the binary, config-file budget fields (budget is CLI-only)

## Tasks

### Task 1: CLI flag defaults
**Depends on**: none
**Status**: ■ complete
**Acceptance**:
▸ GIVEN `agora run --config examples/example-default.yaml --topic "test"` WHEN executed THEN deliberation runs with defaults (time=60s, window=2, max-turns=10, output=transcript.jsonl)
▸ GIVEN `agora resume transcript.jsonl --config examples/example-default.yaml --topic "continue"` WHEN executed THEN resume runs with same defaults
▸ GIVEN `agora run --help` WHEN displayed THEN each flag shows its default value
▸ GIVEN `go test ./...` WHEN run THEN all tests pass including updated `TestLoadConfigExampleFile`
▸ GIVEN `agora run --help` WHEN displayed THEN `--config` and `--topic` still marked required

### Task 2: Example configs and README
**Depends on**: Task 1
**Status**: ■ complete
**Acceptance**:
▸ GIVEN `examples/` directory WHEN listed THEN it contains 6 YAML files: example-default, code-review, research-stress-test, quick-sanity-check, ethical-debate, startup-validation
▸ GIVEN each example YAML WHEN run through `agora validate` THEN each passes validation
▸ GIVEN each example YAML WHEN read THEN it demonstrates a distinct topology, agent panel, or feature combination
▸ GIVEN README.md quick-start section WHEN read THEN it reflects new defaults and references `examples/example-default.yaml`

## Overall Acceptance
▸ GIVEN a first-time user WHEN they run `agora run --config examples/example-default.yaml --topic "Is AI alignment solvable?"` THEN a deliberation runs with sensible defaults and produces a transcript
▸ GIVEN the example suite WHEN browsed THEN each config demonstrates a different use case (code review, research stress-test, quick sanity check, ethical debate, startup validation)
