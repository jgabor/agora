# Changelog

## [Unreleased]

### Added
- `--auto <level>` flag on `agora resume` — generates agent configs when resuming from existing transcript
- `--yes` flag to skip preview confirmation prompt on both `run` and `resume`
- `--auto <level>` flag on `agora run` — generates agent configs via LLM meta-call (levels: off/quick/normal/deep/yolo)
- LLM-generated agent configs: meta-call designs agent roles and system prompts within level caps
- Level-based hard caps on agents, turns, and time (Quick: 2/4/60s, Normal: 4/10/300s, Deep: 6/20/600s, YOLO: unlimited)
- Preview-before-confirm flow: generated config displayed before deliberation starts; non-interactive contexts skip prompt
- Synthesis forced on for all auto mode levels regardless of `--synthesize` flag
- `--model` flag for specifying the LLM model used in auto mode config generation
- MaxTurns=0 means unlimited turns in orchestrator — enables YOLO mode (consensus-only halt)
- LoadConfigFromBytes for parsing YAML config from byte slice (required by autogen)
- Dry-run fallback: auto mode generates and previews config even with `--dry-run`
- Go module bootstrap with domain types, YAML config loading, and validation
- Transcript manager for JSONL file I/O with ring/star/mesh history windowing
- Agent runner wrapping opencode subprocess with JSON event stream parsing
- Consensus extraction via Go-compatible regex matching Python `re.DOTALL` behavior
- Deliberation orchestrator with five termination conditions and signal handling
- Synthesis engine producing structured JSON summaries from deliberation transcripts
- Terminal output with ANSI-styled panels, colored agent names, and formatted tables
- CLI commands (run, stats, validate, resume) via cobra with all Python parity flags
- Test suite with 43 tests covering config, transcript, agent, and semantic-parity verification

### Changed
- Merged go-port branch into main — Go is the canonical implementation
- Removed all Python source files and build configuration
- Rewrote README for Go-only project
- Added GitHub Actions CI (build, test with race detector, vet lint)

### Added
- VISION.md — north star for adversarial deliberation as standard infrastructure
- Cross-version parity test with Python golden transcript (44 tests)
- Orchestrator test coverage — termination conditions and turn execution (50 tests)
