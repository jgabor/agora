# Changelog

## [Unreleased]

### Added
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

### Added
- VISION.md — north star for adversarial deliberation as standard infrastructure
