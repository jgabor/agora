# Plan: Global settings and managed transcript store

<!-- Level: full | Created: 2026-05-04 | Status: active -->

## What
Cross-platform global user settings (`~/.config/agora/settings.yaml` or equivalent) and a managed transcript store (`XDG_DATA_HOME/agora/transcripts/<datetime>-<slug>.jsonl`). Settings provide defaults for model, auto level, topology, and output directory. Transcripts persist automatically to the store; `agora list` shows history; `agora resume <slug>` resumes by topic match. `--file` flag preserves explicit path resume. `--output` flag always wins over managed paths.

## Why
Users currently track `.jsonl` files manually and repeat `--model` / `--auto` on every invocation. A settings layer and managed store turn Agora from a per-directory tool into a personal deliberation history — consistent with VISION.md's "run a kumbaja on it" becoming routine.

## Constraints
- Existing `agora resume TRANSCRIPT --config ...` must continue working unchanged
- `--output` flag always wins; no `--output` saves to the managed store
- Settings only fill gaps, never override explicit deliberation config values or CLI flags
- Cross-platform: Linux (XDG), macOS (Application Support), Windows (LOCALAPPDATA)
- No new external dependencies for XDG — Go stdlib `os`, `path/filepath`, `runtime` only
- Four settings keys max: default_model, default_auto_level, default_topology, default_output_dir
- Breaking change acknowledged: scripts relying on implicit `./transcript.jsonl` must add `--output transcript.jsonl`

## Scope
**In**: XDG dirs, settings YAML loading, settings-aware CLI defaults, config merge, datetime-slug output paths, `list` command, slug-based resume, `--file` flag, default_auto_level wiring
**Out**: Settings migration, encrypted settings, cloud sync, transcript compression, `agora delete`
**Deferred**: Settings validation schema, topic slug customisation, `agora prune` for old transcripts

## Design
- `internal/config/xdg.go`: `ConfigDir()`, `DataDir()` resolving XDG / macOS / Windows paths. Linux uses `XDG_CONFIG_HOME` and `XDG_DATA_HOME` with `~/.config` / `~/.local/share` fallbacks. macOS uses `~/Library/Application Support/agora/`. Windows uses `%LOCALAPPDATA%/agora/`.
- `internal/config/settings.go`: `Settings` struct with four string fields, `LoadSettings(path)` returning zero value when file missing, error on invalid YAML.
- Config merge: `LoadConfig` path fills missing agent `model` fields from `settings.default_model` before validation. Topology omitted in deliberation config defaults to `settings.default_topology` then `"ring"`.
- CLI defaults: `--model`, `--auto`, and `--topology` flags use empty string defaults. After settings load, empty flag values resolve to settings defaults. Explicit flags always win.
- Output path generation: when `--output` omitted, filename is `<datetime>-<slug>.jsonl`. Datetime in Go reference format `20060102-150405`. Slug is topic lowercased, spaces replaced with hyphens, non-alphanumeric-hyphen chars stripped, truncated to 50 characters. Directory is `settings.default_output_dir` if set, else XDG data dir `agora/transcripts/`.
- `agora list`: reads output dir, parses filenames matching `\d{8}-\d{6}-.+\.jsonl`, prints date / slug / filename table. Non-matching files ignored.
- `agora resume`: bare arg checked with `os.Stat`. If file exists, treated as path (existing behavior). If not, treated as slug: latest filename containing the slug is selected. `--file` flag forces path mode and makes bare arg optional.

## Tasks

### Task 1: Cross-platform XDG paths and settings loading
**Depends on**: none
**Status**: ■ complete
**Acceptance**:
▸ GIVEN `XDG_CONFIG_HOME=/tmp/cfg` on Linux WHEN loading settings THEN settings are read from `/tmp/cfg/agora/settings.yaml`
▸ GIVEN `XDG_DATA_HOME=/tmp/data` on Linux WHEN generating a store path THEN path is under `/tmp/data/agora/transcripts/`
▸ GIVEN no XDG env vars on macOS WHEN loading settings THEN settings are read from `~/Library/Application Support/agora/settings.yaml`
▸ GIVEN a valid `settings.yaml` with `default_model: "gpt-4"` WHEN loading THEN `default_model` is available to the CLI
▸ GIVEN missing settings file WHEN loading THEN zero-value settings are returned with no error
▸ GIVEN invalid YAML in settings file WHEN loading THEN a non-nil error is returned

### Task 2: Settings-aware config merge and CLI default resolution
**Depends on**: Task 1
**Status**: □ pending
**Acceptance**:
▸ GIVEN settings with `default_model: "gpt-4"` and a deliberation config omitting agent `model` WHEN loading THEN agent model is filled from settings before validation succeeds
▸ GIVEN settings with `default_model: "gpt-4"` and a deliberation config with agent `model: "claude"` WHEN loading THEN agent model remains `"claude"`
▸ GIVEN settings with `default_auto_level: "normal"` WHEN running `agora run --topic "X"` without `--auto` THEN auto mode uses `"normal"` level
▸ GIVEN `--model o1` flag and settings `default_model: "gpt-4"` WHEN running THEN model flag wins and settings default is ignored
▸ GIVEN `--auto quick` flag and settings `default_auto_level: "normal"` WHEN running THEN auto level `"quick"` wins
▸ GIVEN `--config example.yaml` without `--model` and settings `default_model: "gpt-4"` WHEN running THEN settings model fills any empty agent model but explicit config values remain unchanged

### Task 3: Managed store output paths and `agora list` command
**Depends on**: Task 1
**Status**: □ pending
**Acceptance**:
▸ GIVEN topic `"My Topic"` and `--output` omitted WHEN running THEN transcript filename contains datetime prefix and slug `my-topic`
▸ GIVEN `--output custom.jsonl` and topic `"My Topic"` WHEN running THEN transcript path is `custom.jsonl` regardless of store
▸ GIVEN settings `default_output_dir: "/tmp/agora"` and topic `"Test"` WHEN running THEN transcript is saved to `/tmp/agora/<datetime>-test.jsonl`
▸ GIVEN an empty transcript store WHEN running `agora list` THEN output indicates no transcripts found
▸ GIVEN a store with files `20260504-143022-my-topic.jsonl` and `20260504-150000-other.jsonl` WHEN running `agora list` THEN both entries appear with parsed dates and slugs
▸ GIVEN a store with non-`.jsonl` files WHEN running `agora list` THEN non-JSONL files are ignored

### Task 4: `agora resume` with slug matching and `--file` flag
**Depends on**: Task 2, Task 3
**Status**: □ pending
**Acceptance**:
▸ GIVEN a file exists at path `./my.jsonl` WHEN `agora resume ./my.jsonl --config ...` THEN resumes from that file with existing behavior unchanged
▸ GIVEN a store containing `20260504-143022-my-topic.jsonl` WHEN `agora resume my-topic --auto quick --topic "..."` THEN finds and resumes from the matching file
▸ GIVEN no matching slug in store WHEN `agora resume nonexistent` THEN error indicates no matching transcript found
▸ GIVEN `--file ./my.jsonl` flag WHEN `agora resume --file ./my.jsonl` THEN resumes from `./my.jsonl` without requiring a bare arg
▸ GIVEN a store with multiple files whose names contain `my-topic` WHEN `agora resume my-topic` THEN resumes from the latest (newest datetime) match
▸ GIVEN both a file `./my-topic` in cwd and a store match for `my-topic` WHEN `agora resume my-topic` THEN the cwd file takes precedence (existing behavior for file paths)

### Task 5: Plan-level freshness checkpoint
**Depends on**: Task 4
**Status**: □ pending
**Acceptance**:
▸ GIVEN all tasks complete WHEN checking `CHANGELOG.md` THEN `## [Unreleased]` has entries for settings, managed store, `list` command, and resume slug matching
▸ GIVEN all tasks complete WHEN checking `.agentera/PROGRESS.md` THEN a cycle entry summarises the whole plan
▸ GIVEN all tasks complete WHEN checking `TODO.md` THEN any new annoying items discovered during work are logged

## Overall Acceptance
▸ GIVEN a fresh machine with no settings file WHEN `agora run --auto quick --topic "Test" --dry-run` THEN transcript is saved to the default XDG data directory
▸ GIVEN a settings file with preferred defaults WHEN `agora run --topic "Test" --dry-run` THEN defaults apply and explicit flags override them
▸ GIVEN transcripts in the managed store WHEN `agora list` THEN all transcripts are visible with dates and slugs
▸ GIVEN a slug matching multiple transcripts WHEN `agora resume <slug>` THEN the latest matching transcript is used

## Surprises
