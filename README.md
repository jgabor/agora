# Kumbaja

**Closed-loop multi-agent deliberation system.**

Kumbaja orchestrates N heterogeneous LLM-based agents in a ring topology (or star/mesh), passing turns and maintaining a shared transcript. Agents argue, debate, and deliberate on a topic until a time limit, turn limit, cost budget, or consensus threshold is reached.

## Quick Start

### No installation — run directly

```bash
# Dry-run with the bundled example config (no API keys, no cost)
uv run kumbaja run \
  --config example-config.yaml \
  --topic "Is AI alignment a solvable problem?" \
  --time 30 --window 2 --max-turns 5 \
  --output /tmp/deliberation.jsonl \
  --verbose --dry-run

# View the stats
uv run kumbaja stats /tmp/deliberation.jsonl
```

### Run from anywhere with uvx (no clone needed)

```bash
uvx --from git+https://github.com/.../kumbaja kumbaja run \
  --config example-config.yaml \
  --topic "React vs Svelte for a startup MVP?" \
  --time 60 --window 2 --max-turns 10 \
  --output deliberation.jsonl \
  --dry-run --verbose
```

### With your own config and real LLMs

```bash
# 1. Clone and sync
git clone https://github.com/.../kumbaja && cd kumbaja
uv sync

# 2. Create your config
cat > my-agents.yaml << 'EOF'
topology: ring

agents:
  - id: skeptic
    model: openai/gpt-4o
    system_prompt: |
      You are a skeptic. Challenge assumptions, find weaknesses.
      Respond in 1-2 sentences.

  - id: optimist
    model: anthropic/claude-3-5-sonnet
    system_prompt: |
      You are an optimist. Find best-case scenarios and practical solutions.
      Respond in 1-2 sentences.
EOF

# 3. Run
uv run kumbaja run \
  --config my-agents.yaml \
  --topic "Should we adopt microservices?" \
  --time 120 --window 3 --max-turns 20 \
  --output deliberation.jsonl \
  --verbose

# 4. Inspect
uv run kumbaja stats deliberation.jsonl
```

### Existing config? Just run

```bash
uv run kumbaja run \
  --config example-config.yaml \
  --topic "Your topic here" \
  --time 60 --window 2 --max-turns 10 \
  --output transcript.jsonl

## CLI Reference

### `kumbaja run` — Start a deliberation

```
kumbaja run [OPTIONS]

Options:
  -c, --config PATH        YAML agent configuration file  [required]
  -t, --topic TEXT         Topic or goal for deliberation  [required]
  -T, --time INTEGER       Time limit in seconds  [required]
  -w, --window INTEGER     Number of predecessor messages each agent sees  [required]
  -m, --max-turns INTEGER  Maximum total turns  [required]
  -o, --output PATH        JSONL transcript output path  [required]
  -v, --verbose            Print agent responses in real-time
  --budget FLOAT           Cost cap in dollars
  --synthesize / --no-synthesize
                           Run final synthesis agent after deliberation
  --full-context           Show last K messages from ALL agents (not just predecessor)
  --dry-run                Run with simulated agent responses (no LLM calls)
```

### `kumbaja resume` — Continue from an existing transcript

```
kumbaja resume [OPTIONS] TRANSCRIPT

Options:
  -c, --config PATH        YAML agent configuration file  [required]
  -t, --topic TEXT         Topic or goal for deliberation  [required]
  -T, --time INTEGER       Additional time limit in seconds  [required]
  -w, --window INTEGER     Window size  [required]
  -m, --max-turns INTEGER  Additional max turns  [required]
  -o, --output PATH        Output path for updated transcript  [required]
  -v, --verbose            Print agent responses in real-time
  --budget FLOAT           Remaining cost budget
  --full-context           Show last K messages from ALL agents
  --dry-run                Run with simulated agent responses
```

### `kumbaja stats` — Show transcript statistics

```
kumbaja stats TRANSCRIPT
```

Displays total turns, total tokens, total cost, per-agent breakdown, and consensus events.

### `kumbaja validate` — Validate a config file

```
kumbaja validate CONFIG_FILE
```

Checks the config for errors without starting a deliberation.

## Configuration Reference

### Top-level options

| Key | Type | Default | Description |
|---|---|---|---|
| `topology` | string | `ring` | Agent interaction pattern: `ring`, `star`, or `mesh` |
| `consensus_threshold` | int | `0` | Consecutive consensus signals to trigger early stop (0 = disabled) |
| `synthesis_model` | string | null | Override model for final synthesis (defaults to first agent's model) |

### Agent configuration

| Key | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Unique agent identifier |
| `model` | string | Yes | Provider/model string (e.g. `openai/gpt-4o`) |
| `system_prompt` | string | No | System prompt setting the agent's persona |

## Topologies

### Ring (default)
Each agent sees K messages from its **immediate predecessor**. Creates adversarial tension with simple handoff logic.

### Star
All agents see the last K messages from **any agent** in the transcript. Useful for independent analysis where each agent responds to the full discussion context.

### Mesh
Same as star for the envelope — agents see last K messages from any agent.

## Consensus Detection

Agents can signal consensus by including `[CONSENSUS: <statement>]` in their response. When the configured `consensus_threshold` number of consecutive turns contain consensus markers, the deliberation terminates early.

## Final Synthesis

When `--synthesize` is enabled, kumbaja runs one final agent call after the deliberation to generate a structured JSON summary:

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

The JSONL transcript contains one turn per line:

```json
{"turn": 0, "agent_id": "skeptic", "model": "openai/gpt-4o", "timestamp": 1715000000.0, "content": "...", "tokens": {"total": 150, "input": 100, "output": 50}, "cost": 0.001, "consensus": false, "consensus_statement": "", "elapsed": 2.5}
```

## Architecture

```
kumbaja/
  cli.py          CLI (click-based)
  config.py       YAML config loading + validation
  orchestrator.py Core deliberation loop
  agent.py        opencode subprocess wrapper
  transcript.py   JSONL read/write + history assembly
  synthesis.py    Final summary generation
  output.py       Rich terminal output
  types.py        Data types (AgentConfig, TurnRecord, etc.)
```

## Development

```bash
uv sync                     # Install with dev dependencies
uv run pytest tests/ -v     # Run tests
uv run ruff check .         # Lint
uv run mypy kumbaja/        # Type check
```

## Requirements

- Python 3.10+
- [uv](https://docs.astral.sh/uv/) — Python package manager
- [OpenCode](https://opencode.ai) — the LLM agent runner
