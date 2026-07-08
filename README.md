# Agora

**Closed-loop multi-agent deliberation system.**

Agora orchestrates N heterogeneous LLM-based agents in a ring, star, or mesh topology, passing turns and maintaining a shared transcript. Agents argue, debate, and deliberate on a topic until a time limit, turn limit, cost budget, or consensus threshold is reached.

Written in Go. Compiles to a single static binary.

## Quick Start

<!-- agora-contract: ./agora list -->
<!-- agora-contract: ./agora show is-ai-alignment-a-solvable-problem -->
<!-- agora-contract: ./agora stats is-ai-alignment-a-solvable-problem -->

```bash
# Clone and build
git clone https://github.com/jgabor/agora && cd agora
go build -o agora ./cmd/agora

# Run a deliberation (only --config and --topic are required)
./agora run \
  --config examples/example-default.yaml \
  --topic "Is AI alignment a solvable problem?"

# Dry-run (no API keys, no cost)
./agora run \
  --config examples/example-default.yaml \
  --topic "Is AI alignment a solvable problem?" \
  --dry-run --verbose

# Add current web research before deliberation
./agora run \
  --config examples/research-stress-test.yaml \
  --topic "What changed in EU AI policy this month?" \
  --research

# Add local text context before deliberation
./agora run \
  --config examples/code-review.yaml \
  --topic "How should we improve these docs?" \
  --context README.md \
  --context examples/

# Browse saved transcripts, then inspect one by slug
./agora list
./agora show is-ai-alignment-a-solvable-problem
./agora stats is-ai-alignment-a-solvable-problem

# Give an external agent Agora's CLI operating context
./agora prime --format markdown
```

### With your own config and real LLMs

```bash
./agora run \
  --config examples/code-review.yaml \
  --topic "Should we adopt microservices?"
```

### Install globally

<!-- agora-contract: agora validate examples/example-default.yaml -->
<!-- agora-contract: agora config init -->

```bash
go install ./cmd/agora
agora validate examples/example-default.yaml
agora config init
```

## Example Configs

The `examples/` directory contains ready-to-use configs demonstrating different topologies, agent panels, and features:

| File | Topology | Agents | Features |
|---|---|---|---|
| `example-default.yaml` | ring | 5 | General-purpose deliberation panel — strategist, domain expert, skeptic, optimist, user advocate |
| `code-review.yaml` | ring | 5 | Architecture review panel with consensus (threshold 3) and synthesis model |
| `research-stress-test.yaml` | star | 5 | Academic research stress-test with cross-pollination via star topology |
| `quick-sanity-check.yaml` | ring | 2 | Minimal 2-agent setup — fastest possible deliberation |
| `ethical-debate.yaml` | mesh | 5 | Ethical deliberation with mesh topology, consensus, and synthesis |
| `startup-validation.yaml` | star | 5 | Startup idea validation panel with synthesis model |

All configs use `opencode-go/deepseek-v4-flash` as the default agent model.

## CLI Reference

### `agora run` — Start a deliberation

```
agora run --config PATH --topic TEXT [flags]

Required:
  -c, --config PATH        YAML agent configuration file
  -t, --topic TEXT         Topic or goal for deliberation

Optional (all have sensible defaults):
  -T, --time SECONDS       Time limit in seconds (default: 60)
  -w, --window N           Number of predecessor messages each agent sees (default: 2)
  -m, --max-turns N        Maximum total turns (default: 10)
  -o, --output PATH        JSONL transcript output path (default: transcript.jsonl)
  -q, --quiet              Suppress live response bodies; show metadata/progress only
  -v, --verbose            Print response bodies plus diagnostics and metrics
  --budget FLOAT           Cost cap in dollars
  --synthesize             Run final synthesis agent after deliberation
  --full-context           Show last K messages from ALL agents (not just predecessor)
  --research               Enable topic-inferred web research before deliberation
  --no-research            Disable config-enabled web research for this run
  --context PATH           Local text context path to include before deliberation (repeatable)
  --dry-run                Run with simulated agent responses (no LLM calls)
  --auto LEVEL             Auto-generate agent config (quick, normal, deep, yolo)
  -M, --model MODEL        Model for auto config generation and deliberation agents
  --yes                    Skip auto config preview confirmation
```

With `--auto`, the level supplies default time and max-turn caps. Explicit `--time` or `--max-turns` values override those caps.

### Scripting and CI

When stdin is not a terminal, `--auto` requires explicit approval before config generation runs:

- `--yes` — auto-approve the generated cast and proceed
- `AGORA_YES=1` — same as `--yes` (useful in scripts and CI)
- `--dry-run` — preview only, no deliberation execution

Example:

```bash
agora run --auto normal --yes --topic "Should we adopt feature X?"
```

Large `--window` values multiply per-turn context cost: evidence is delivered once per agent on first turn, and each turn includes up to `window` prior messages in history.

By default, live output includes agent response bodies as turns complete. Use `--quiet` for the lower-noise metadata/progress-only stream, or `--verbose` to keep response bodies and add diagnostics such as token, cost, timing, and consensus metrics when available.

### `agora prime` — Inspect agent-operating context

<!-- agora-contract: agora prime -->

```
agora prime [--format text|json|markdown]
```

Prints Agora-provided operating context for agents and tools: command surface, flags, defaults, enum values, config keys, effective config with secret-like values redacted, transcript metadata, and output-format contracts. `text` is the default; `json` and `markdown` are non-ANSI and intended for machine or agent startup use.

`agora prime` is not deliberation evidence. Use it before operating the CLI. Use `agora run --context PATH` only when you want local text files or directories delivered to deliberation agents as bounded user-provided evidence.

### Formatted Inspection Output

Supported inspection commands accept one shared output selector:

```
--format text|json|markdown
```

`text` is the default and preserves the human CLI presentation. `json` emits schema-versioned, non-ANSI data. `markdown` emits non-ANSI prose/tables for agents and humans. Invalid format values are rejected with the valid values named.

| Command | Formats |
|---|---|
| `agora prime` | `--format text|json|markdown` |
| `agora metadata` | `--format text|json|markdown` |
| `agora list` | `--format text|json|markdown` |
| `agora show TRANSCRIPT|SLUG` | `--format text|json|markdown` |
| `agora stats TRANSCRIPT|SLUG` | `--format text|json|markdown` |
| `agora validate CONFIG|SLUG` | `--format text|json|markdown` |
| `agora config get --all` | `--format text|json|markdown` |

### Transcript slugs and paths

Agora writes managed transcripts under the configured transcript store with filenames like `20260507-143022-is-ai-alignment-a-solvable-problem.jsonl`. `agora list` shows the human-readable `Slug` column from those filenames.

Transcript commands are slug-first and path-compatible:

| Command | Slug input | Path input |
|---|---|---|
| `agora show SLUG` | Shows the newest managed transcript whose slug matches exactly, then by prefix, then by substring | `agora show path/to/transcript.jsonl` reads that file directly |
| `agora stats SLUG` | Computes stats from the newest matching managed transcript | `agora stats path/to/transcript.jsonl` reads that file directly |
| `agora resume ... SLUG` | Continues from the newest matching managed transcript | `agora resume ... path/to/transcript.jsonl` reads that file directly |

Path-like inputs stay paths: if you pass a missing `*.jsonl`, `./file`, `../file`, or directory-style path, Agora reports the missing path instead of searching transcript slugs.

### `agora list` — List managed transcripts

<!-- agora-contract: agora list -->

```
agora list [--format text|json|markdown]
```

Lists managed transcripts from the configured transcript store, newest first, with date, slug, turn count, and filename. Use the slug with `show`, `stats`, or `resume`; use the filename/path when you need an exact file. Formatted output reports the transcript store path, transcript count, empty-state status, and transcript rows.

### `agora show` — Show a transcript

<!-- agora-contract: agora show TRANSCRIPT|SLUG -->

```
agora show TRANSCRIPT|SLUG [--format text|json|markdown]
```

Displays transcript records in order using the same turn cards and agent response styling as `run`, including evidence summaries/source references and consensus statements. Plain output remains available in the same environments as `run` (`NO_COLOR`, CI, or dumb terminals). Transcript input is slug-first and path-compatible as described above. Malformed non-blank JSONL records fail instead of being skipped. `--format json` emits a schema-versioned inspection document, not a replacement for raw JSONL transcript storage.

### `agora resume` — Continue from an existing transcript

```
agora resume --config PATH --topic TEXT TRANSCRIPT|SLUG [flags]
agora resume --config PATH --topic TEXT --file PATH/TO/transcript.jsonl [flags]
```

Same optional flags as run, except evidence flags are rejected on resume. Loads prior records from a transcript slug, explicit positional path, or `--file` override and continues from the last turn. Live output uses the same modes as `run`: default shows response bodies, `--quiet` suppresses live response bodies for metadata/progress only, and `--verbose` shows response bodies plus diagnostics/metrics. If the transcript already contains research/context evidence, Agora reuses that evidence and does not refresh web research or local context.

### `agora stats` — Show transcript statistics

<!-- agora-contract: agora stats TRANSCRIPT|SLUG -->

```
agora stats TRANSCRIPT|SLUG [--format text|json|markdown]
```

Displays total turns, tokens, cost, per-agent breakdown, and consensus events for a transcript slug or explicit path. Malformed non-blank JSONL records fail instead of being skipped.

### `agora validate` — Validate a config file

<!-- agora-contract: agora validate CONFIG|SLUG -->

```
agora validate CONFIG|SLUG [--format text|json|markdown]
```

Checks config for errors without starting a deliberation. Explicit paths are read directly. Non-path slugs resolve by config file stem in the current directory and `examples/`; ambiguous slug matches report candidate files instead of guessing.

### `agora metadata` — Inspect command metadata

<!-- agora-contract: agora metadata -->

```
agora metadata [--format text|json|markdown]
```

Reports the live command metadata used by contract verification, including commands, flags, defaults, enum values, config keys, transcript metadata, and supported output formats.

### `agora config` — Manage global config

```
agora config init
agora config get --all
agora config get --all [--format text|json|markdown]
agora config get KEY
agora config set KEY VALUE
```

Reads and writes the global `config.yaml` file. `agora config init` creates it with Agora's effective defaults and refuses to overwrite unless `--force` is passed. `agora config get --all --format json|markdown` reports every supported setting with value, source, type, default/effective-value policy, allowed values, and redaction behavior. Supported keys are `default_model`, `default_auto_level`, `default_topology`, `default_output_dir`, `research_max_sources`, `context_max_bytes`, and `context_max_depth`.

## Configuration

| Key | Type | Default | Description |
|---|---|---|---|
| `topology` | string | `ring` | `ring`, `star`, or `mesh` |
| `consensus_threshold` | int | `0` | Consecutive consensus signals to trigger early stop (0 = disabled) |
| `synthesis_model` | string | — | Override model for final synthesis (defaults to first agent's model) |
| `research` | bool | `false` | Enable topic-inferred web research before deliberation for runs using this config |
| `context` | list | `[]` | Local text files or directories to include before deliberation |
| `agents` | list | required | Agent configs with `id`, `model`, and optional `system_prompt` |

CLI flags override project config. For evidence, `--research` enables web research, `--no-research` disables config-enabled research, and any `--context` flags replace config `context` paths for that run.

Global `config.yaml` can be managed with `agora config`. It may set CLI defaults and evidence caps, but does not silently enable web access:

| Key | Default | Description |
|---|---|---|
| `default_model` | `opencode-go/deepseek-v4-flash` | Model for auto config generation and omitted agent models |
| `default_auto_level` | — | Auto level used when `--auto` and `--config` are omitted: `quick`, `normal`, `deep`, or `yolo` |
| `default_topology` | `ring` | Topology used when a YAML config omits `topology` |
| `default_output_dir` | platform data dir | Directory for managed transcript output |
| `research_max_sources` | `20` | Maximum web sources, generated web queries, and local context file references |
| `context_max_bytes` | `1048576` | Maximum total bytes of local context |
| `context_max_depth` | `5` | Maximum directory traversal depth for local context |

When these evidence caps are unset in `config.yaml`, auto mode raises their fallback defaults for broader runs:

| Auto level | `research_max_sources` | `context_max_bytes` | `context_max_depth` |
|---|---:|---:|---:|
| `quick` | `20` | `1048576` | `5` |
| `normal` | `40` | `4194304` | `6` |
| `deep` | `300` | `16777216` | `8` |
| `yolo` | `1000` | `67108864` | `12` |

### Research and Local Context

Research and context run once before the first deliberation turn. Web research derives bounded queries from the topic, then uses the normal OpenCode-backed agent runtime to collect source references. Local context reads bounded safe text from readable files and directories and delivers it to each agent once; transcripts store source references only, not full local file contents. Directory traversal respects `.gitignore`, skips VCS directories, binary files, and secret-looking files such as `.env` and private key names, but does not blanket-skip hidden project directories.

All model calls are given this read-only filesystem guard: `CRITICAL: DO NOT MODIFY OR WRITE TO ANY FILES! You are only permitted to read and explore files.` Agora also avoids OpenCode's dangerous auto-approval flag when launching agents.

Agora prints a pre-deliberation evidence summary before agent turns. Web query/source limits and local file, byte, and depth limits are soft caps: Agora includes what fits, truncates local text at the byte cap, skips paths beyond caps, and warns in the evidence summary when a cap is hit. Agora still halts before any agent response if enabled research or context cannot produce usable source references, if web evidence is malformed, or if explicit local context paths cannot be resolved. `--dry-run --research` reports deterministic planned research behavior without live web tool calls. `--dry-run --context` still validates local paths without model cost.

Current limitations: local context is text-only; PDF, DOCX, binary parsing, browser rendering, source/domain allowlists, persistent source caching, and replay-perfect research refresh are not implemented.

## Topologies

**Ring** (default): Each agent sees K messages from its immediate predecessor. Creates adversarial tension.

**Star**: All agents see the last K messages from any agent. Each responds to the full discussion context.

**Mesh**: Same as star — agents see last K from any agent.

## Consensus

Agents signal consensus with `[CONSENSUS: <statement>]` in their response. When `consensus_threshold` consecutive turns contain markers, deliberation terminates early.

## Synthesis

When `--synthesize` is enabled, a final agent call produces a structured JSON summary:

```json
{
  "key_arguments": ["..."],
  "points_of_agreement": ["..."],
  "unresolved_tensions": ["..."],
  "recommended_decision": "...",
  "confidence": "high|medium|low"
}
```

## Transcript Format

JSONL, one turn per line. The first written record embeds a `transcript` metadata block with schema version, full run config, and the durable cast used for replay. Cast members include numeric ID, generated display name, persona, provider/model, and a theme-adaptive ANSI color slot so `agora show` can replay the same `[A1 persona]` labels and styling as the original run without reloading the config file. Research/context runs add an orchestrator evidence summary before agent turns. Transcript evidence stores source references and a readable summary, not full source content; the same evidence bundle is delivered to each agent exactly once on that agent's first turn. User-facing transcript commands (`show`, `stats`, and `resume`) load transcripts strictly: malformed non-blank records fail with an error instead of being ignored.

```json
{"turn": 0, "agent_id": "skeptic", "model": "openai/gpt-4o", "transcript": {"schema_version": 1, "cast": [{"id": 1, "name": "Solon", "persona": "skeptic", "provider_model": "openai/gpt-4o", "color": "6"}], "config": {"agents": [{"id": "skeptic", "model": "openai/gpt-4o", "system_prompt": "..."}], "topology": "ring", "consensus_threshold": 0}}, "timestamp": 1715000000.0, "content": "...", "tokens": {"total": 150, "input": 100, "output": 50}, "cost": 0.001, "consensus": false, "consensus_statement": "", "elapsed": 2.5}
```

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md) for the full workflow (mage targets, hooks, optional e2e, and README contract tests).

```bash
go build ./...              # Build
go test ./... -v -cover     # Test
go vet ./...                # Lint
```

## Requirements

- Go 1.26.2+
- [OpenCode](https://opencode.ai) — the LLM agent runner

## License

MIT — see [LICENSE](LICENSE).
