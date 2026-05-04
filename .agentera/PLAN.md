# Plan: Auto mode for resume and --yes flag

<!-- Level: light | Created: 2026-05-04 | Status: active -->

## What
Extend auto mode to `agora resume` and add a `--yes` flag to both `run` and `resume` that skips the preview confirmation prompt.

## Why
Users resuming deliberation may no longer have the original config file. Auto mode lets them regenerate an appropriate agent panel from the topic alone. The `--yes` flag removes friction in scripted or trusted contexts where manual confirmation is unwanted.

## Constraints
- Existing `resume --config TRANSCRIPT --topic ...` must continue to work unchanged
- Reuse existing autogen, types, and output packages — no new abstractions
- `--yes` is only meaningful with `--auto`; manual config path has no prompt to skip
- Preview must still render even when `--yes` is set

## Acceptance Criteria
▸ GIVEN an existing transcript `transcript.jsonl` WHEN `agora resume transcript.jsonl --auto quick --topic "Continue the debate"` THEN a config is auto-generated, previewed, and deliberation continues from the transcript with Quick level caps applied
▸ GIVEN `agora resume transcript.jsonl --auto quick --topic "..." --yes` WHEN executed in a TTY THEN the preview prints but the prompt is skipped and deliberation proceeds immediately
▸ GIVEN `agora resume transcript.jsonl --config examples/code-review.yaml --topic "..."` WHEN executed THEN behavior is identical to before this change
▸ GIVEN `agora run --auto normal --topic "..." --yes` WHEN executed THEN config preview prints and deliberation starts without interactive confirmation
▸ GIVEN `agora run --config examples/code-review.yaml --topic "..." --yes` WHEN executed THEN `--yes` is silently ignored and behavior is unchanged
