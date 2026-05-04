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
