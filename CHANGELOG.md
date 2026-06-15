# Changelog

## [Unreleased]

_No user-facing changes yet._

## [0.3.0] - 2026-06-15

### Added
- `agora prime` provides read-only agent-operating context for the CLI, including commands, flags, defaults, enum values, settings keys, transcript metadata, and the boundary from deliberation `--context` evidence.
- `--format text|json|markdown` is available on supported inspection surfaces: `prime`, `metadata`, `list`, `show`, `stats`, `validate`, and `config get --all`.
- Command-contract verification checks live Cobra commands, canonical flags, supported formats, settings keys, enum values, schema versions, and README contract markers against the documented CLI surface.
- `agora show` displays transcript records by slug or path using the same turn cards and response styling as `run`, including plain-output fallback, evidence summaries/source references, and consensus statements.

### Changed
- Transcripts now persist run setup metadata on the first record, including full config and enriched cast entries with numeric ID, generated name, persona, provider/model, and theme-adaptive ANSI color slot for faithful `show` replay.
- Transcript commands now use slug-first inputs while preserving explicit path compatibility: `show`, `stats`, and positional `resume` resolve managed transcript slugs; `validate` resolves config slugs from the current directory and `examples/`.
- User-facing transcript loading is strict: malformed non-blank JSONL records now fail for `show`, `stats`, and `resume` instead of being silently skipped.
- Default live output for `run` and `resume` now shows agent response bodies; `--quiet` keeps metadata/progress-only output, and `--verbose` adds diagnostics to response output.
- `--context` now delivers bounded safe local text to agents once while transcripts keep source references only.

## [0.2.0] - 2026-05-05

### Added
- Opt-in pre-deliberation evidence: `--research`, `--no-research`, repeatable `--context`, config `research`/`context`, settings caps, topic-derived OpenCode web evidence, text-only local context safety, source-reference transcript summaries, dry-run reporting, and resume evidence reuse.
- OutputManager terminal renderer coverage for panels, tables, text wrapping, config preview, stats output, and status methods.
- SynthesisEngine and Orchestrator.Synthesize test coverage (extractJSON, formatTranscript, full engine flow).
- Slug-based `agora resume` with latest-match selection and a `--file` path override.
- Managed transcript store output paths and `agora list` for browsing saved deliberations.
- Settings-aware defaults now fill missing agent models, default topology, and auto level when CLI/config values omit them.
- Global settings path/loading layer: XDG config/data dirs on Linux, Application Support on macOS, LOCALAPPDATA on Windows, plus `settings.yaml` parsing.
- `--auto <level>` flag on `agora resume` — generates agent configs when resuming from existing transcript.
- `--yes` flag to skip preview confirmation prompt on both `run` and `resume`.
- `--auto <level>` flag on `agora run` — generates agent configs via LLM meta-call (levels: off/quick/normal/deep/yolo).
- LLM-generated agent configs: meta-call designs agent roles and system prompts within level caps.
- Level-based hard caps on agents, turns, and time (Quick: 2/4/60s, Normal: 4/10/300s, Deep: 6/20/600s, YOLO: unlimited).
- Preview-before-confirm flow: generated config displayed before deliberation starts; non-interactive contexts require `--yes` or `--dry-run`.
- Synthesis forced on for all auto mode levels regardless of `--synthesize` flag.
- `--model` flag for specifying the LLM model used in auto mode config generation.
- MaxTurns=0 means unlimited turns in orchestrator — enables YOLO mode (consensus-only halt).
- LoadConfigFromBytes for parsing YAML config from byte slice (required by autogen).
- Dry-run fallback: auto mode generates and previews config even with `--dry-run`.
- Go module bootstrap with domain types, YAML config loading, and validation.
- Transcript manager for JSONL file I/O with ring/star/mesh history windowing.
- Agent runner wrapping opencode subprocess with JSON event stream parsing.
- Consensus extraction via DOTALL-compatible regex.
- Deliberation orchestrator with five termination conditions and signal handling.
- Synthesis engine producing structured JSON summaries from deliberation transcripts.
- Terminal output with ANSI-styled panels, colored agent names, and formatted tables.
- CLI commands (`run`, `stats`, `validate`, `resume`, `list`, `show`, `config`, `metadata`) via cobra.
- Project vision captured in `.agentera/vision.yaml`.
- GitHub Actions CI (build, test with race detector, golangci-lint).

### Fixed
- Terminal visual-width calculation now counts Unicode glyphs as runes while ignoring ANSI escape sequences.
- CLI auto mode now uses `settings.default_model` when `--model` is omitted.

### Changed
- Merged go-port branch into main — Go is the canonical implementation.
- Removed all Python source files and build configuration.
- Rewrote README for Go-only project.
