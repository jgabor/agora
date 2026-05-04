# Decisions

## Decision 1 · 2026-05-03

**Question**: How should the closed-loop multi-agent deliberation system be architected?

**Context**: Need a simple, deterministic script that calls `opencode run` with configurable heterogeneous agents, ring topology, time limits, and raw transcript output. Inspired by karpathy/autoresearch and anthropic/ralph-loop. Agents should argue and deliberate on a provided goal.

**Alternatives**:
- [Shared history topology]: agents see the last K messages from the global transcript regardless of author. Win: more context for each turn. Lose: less adversarial tension, more complex orchestrator state.
- [Go implementation]: compile to a single static binary. Win: portable, profile signal is strong. Lose: slower iteration, more code for subprocess/JSONL handling in a glue script.
- [Orchestrator-composed prompts]: orchestrator assembles the full prompt including system prompt, topic, and history formatting. Win: full control over prompt engineering. Lose: orchestrator becomes prompt-aware, violates minimalism principle.
- [Raw text history in envelope]: history array contains just strings. Win: fewer tokens, simpler parsing. Lose: agents cannot address predecessor by role or name.
- [Synthesized output]: a final dedicated agent produces a summary after the timer stops. Win: human-readable artifact. Lose: extra model call, additional complexity, summary quality variance.

**Choice**: Ring topology with annotated history objects, Python implementation, minimal orchestrator with structured JSON envelope, rich JSONL transcript to file, dual termination caps (time OR max turns), halt-on-failure, orchestrator seed message.

**Reasoning**: Ring topology preserves adversarial tension with trivial handoff logic ( predecessor = (i-1) mod N ). Python minimizes friction for subprocess orchestration and JSONL handling. Minimal orchestrator keeps the script dumb and the agents smart: it only pipes a structured envelope to `opencode run` stdin and appends the response to the transcript. Raw transcript avoids synthesis complexity. Dual caps prevent runaway cost. Halt-on-failure keeps semantics simple.

**Confidence**: firm

**Feeds into**: standalone (this is the project itself)

## Decision 2 · 2026-05-04

**Question**: Should Kumbaja migrate from Python to Go as the canonical implementation?

**Context**: The Python version (Decision 1) validated the architecture across 9 modules. A full Go port has been built with identical behavior: same YAML config, JSONL transcript format, CLI flags, and termination logic. 43 tests pass with 35.6% coverage. The decision profile strongly favors Go for CLI infrastructure (confidence 95).

**Alternatives**:
- [Keep both]: Python and Go side by side. Win: migration safety. Lose: maintenance burden, confusion about which is authoritative.
- [Go only]: delete Python, Go becomes canonical. Win: single build surface, portable static binary. Lose: one-time deletion risk (recoverable via git).

**Choice**: Go only. Delete all Python source files, build artifacts, and Python-specific tooling.

**Reasoning**: The Go port is complete and verified. A single implementation eliminates confusion and halves the maintenance surface. The Python implementation remains accessible in git history if ever needed.

**Confidence**: firm

**Feeds into**: README.md, .gitignore, CI setup

## Decision 3 · 2026-05-04

**Question**: What should the project be named? "Kumbaja" was a codename.

**Context**: The tool runs adversarial deliberation loops — agents in a ring topology debating a topic. The VISION.md identity is clinical, rigorous, terse. Naming principle: pronounceable, searchable, not a pun.

**Alternatives**:
- [Classical arena]: Agora, Forum, Lyceum — ancient spaces of debate and judgment. Win condition: the name evokes deliberation without explanation.
- [Coined word]: e.g. Rignet, Velox — unique, no baggage. Win condition: search results are entirely about this tool from day one.

**Choice**: Agora — the Athenian marketplace and public square where citizens gathered to debate, challenge ideas, and deliberate.

**Reasoning**: Agora is the most direct classical match for "space where adversarial deliberation happens." Five letters, pronounceable, distinctive in the Go CLI space. The marketplace connotation is acknowledged but accepted — the tool earns the word through its function.

**Confidence**: firm

**Feeds into**: VISION.md

## Decision 4 · 2026-05-04

**Question**: Should Agora add an auto mode where the LLM procedurally generates agent configurations from the topic?

**Context**: Currently requires a handcrafted YAML config file. Auto mode would let users run `agora run --auto <level> --topic "..."` and have the model design the agent panel. Inspired by Factory's autonomy levels (Off/Low/Medium/High). Need hard caps to prevent runaway cost. Must remain backward-compatible with manual configs.

**Alternatives**:
- [Template-based]: preset panels per level, topic fills in details. Win: predictable, fast, no meta-LLM cost. But agent roles would be generic, not topic-tailored.
- [LLM-designed with flexible caps]: model designs panel AND suggests its own budget. Win: maximally adaptive. But model could lowball caps to stay within budget, reducing deliberation quality.
- [LLM-designed with hard-coded caps (chosen)]: model designs agents within fixed caps per level. Win: creative agent design with predictable cost boundaries. Lose: requires meta-LLM call before deliberation adds latency.

**Choice**: LLM-designed with hard-coded caps. `--auto <level>` flag on `agora run`, mutually exclusive with `--config`. Five levels: Off/Quick/Normal/Deep/YOLO. Same model designs and deliberates. LLM returns YAML parsed through existing LoadConfig. Preview-before-confirm. Synthesis always on.

**Reasoning**: Levels as pure budget constraints — not creative direction — keeps the model honest and behavior predictable. The LLM invents agent roles and system prompts tailored to the topic, but cannot exceed the hard caps for its level. Reusing LoadConfig for generated YAML means the same validation path covers both manual and auto configs. Preview-before-confirm builds trust and catches weird outputs. YOLO is truly uncapped (consensus-only halt) — power users opt into the risk explicitly.

Level definitions (hard-coded in binary):
- Quick: 2 agents, 4 turns, 60s time cap
- Normal: 4 agents, 10 turns, 300s time cap
- Deep: 8 agents, 20 turns, 900s time cap
- YOLO: no caps, runs until consensus reached

**Confidence**: provisional — the Quick/Normal/Deep numbers are reasonable starting points but will likely need tuning based on usage patterns.

**Feeds into**: VISION.md, TODO.md

## Decision 5 · 2026-05-04

**Question**: How should Agora support global user settings and a managed transcript store?

**Context**: Currently every `agora run` requires `--config` or `--auto`, and every `--output` defaults to `transcript.jsonl` in the working directory. Users want a global settings file to set preferred defaults, and a managed store for transcripts so they don't have to track `.jsonl` files across directories. The design must remain backward-compatible with existing CLI behavior and preserve the explicit-over-implicit principle.

**Alternatives**:
- [Single fallback config only]: `~/.config/agora/config.yaml` used when no `--config` given. Win: simple, one file. Lose: no place for non-config preferences (model, output dir), and deliberation configs are different from user preferences.
- [Managed store only]: transcripts saved to XDG dirs, no global settings. Win: solves the file sprawl problem. Lose: users still must set `--model` and `--auto` every time if they deviate from hard-coded defaults.
- [Broader settings file + managed store (chosen)]: `~/.config/agora/settings.yaml` holds default model, auto level, topology, and output directory. Transcripts stored in `XDG_DATA_HOME/agora/transcripts/<datetime>-<slug>.jsonl`. `agora list` shows the store. `agora resume <slug>` resumes by topic slug; `--file` flag preserves path-based resume. Win: both convenience problems solved with one architectural layer. Lose: more complexity in config loading path and XDG directory management.

**Choice**: Broader settings file plus managed transcript store.

Settings file (`settings.yaml`):
- `default_model`: string, e.g. `opencode-go/deepseek-v4-flash`
- `default_auto_level`: string, one of `quick`, `normal`, `deep`, `yolo`
- `default_topology`: string, one of `ring`, `star`, `mesh`
- `default_output_dir`: string, path where transcripts are stored (default: XDG_DATA_HOME/agora/transcripts)

Transcript store:
- Default output path when `--output` is omitted: `<datetime>-<slug>.jsonl` in the configured output directory
- `agora list` prints stored transcripts with date, slug, and turn count
- `agora resume <slug>` matches the latest transcript whose filename contains the slug
- `agora resume --file <path>` explicitly resumes from a file path, preserving existing behavior

Config precedence (three layers, highest wins):
1. CLI flags (`--model`, `--config`, `--auto`, `--output`)
2. Deliberation config file (agent models, topology)
3. Global settings file (default model, topology, auto level, output dir)

A settings value only applies when no higher layer provides that value. For example, `settings.default_model` fills in when an agent config omits `model`, but a `--model` flag overrides both.

Cross-platform directories via XDG equivalents:
- Linux: `XDG_CONFIG_HOME/agora/` and `XDG_DATA_HOME/agora/`
- macOS: `~/Library/Application Support/agora/` for both
- Windows: `%LOCALAPPDATA%/agora/`

**Reasoning**: The settings/transcript split maps cleanly to existing Unix conventions: config is stateless preferences, data is mutable transcripts. Four settings keys is minimal — enough to cover the common friction points without bloating the file. The three-layer precedence model keeps explicit CLI behavior intact while letting power users set sane defaults. The `--file` flag on resume ensures scripts and existing workflows don't break.

**Confidence**: firm

**Feeds into**: PLAN.md, TODO.md, config.go, transcript.go, cmd/agora/main.go
