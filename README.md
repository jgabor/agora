# Kumbaja

**Closed-loop multi-agent deliberation system.**

Kumbaja orchestrates N heterogeneous LLM-based agents in a ring topology (or star/mesh), passing turns and maintaining a shared transcript. Agents argue, debate, and deliberate on a topic until a time limit, turn limit, cost budget, or consensus threshold is reached.

Written in Go. Compiles to a single static binary.

## Quick Start

```bash
# Clone and build
git clone https://github.com/jgabor/kumbaja && cd kumbaja
go build -o kumbaja ./cmd/kumbaja

# Dry-run (no API keys, no cost)
./kumbaja run \
  --config example-config.yaml \
  --topic "Is AI alignment a solvable problem?" \
  --time 30 --window 2 --max-turns 5 \
  --output /tmp/deliberation.jsonl \
  --verbose --dry-run

# Stats
./kumbaja stats /tmp/deliberation.jsonl
```

### With your own config and real LLMs

```bash
./kumbaja run \
  --config my-agents.yaml \
  --topic "Should we adopt microservices?" \
  --time 120 --window 3 --max-turns 20 \
  --output deliberation.jsonl \
  --verbose
```

### Install globally

```bash
go install ./cmd/kumbaja
kumbaja validate example-config.yaml
```

## CLI Reference

### `kumbaja run` — Start a deliberation

```
kumbaja run --config PATH --topic TEXT --time SECONDS --window N --max-turns N --output PATH [flags]

Required:
  -c, --config PATH        YAML agent configuration file
  -t, --topic TEXT         Topic or goal for deliberation
  -T, --time SECONDS       Time limit in seconds
  -w, --window N           Number of predecessor messages each agent sees
  -m, --max-turns N        Maximum total turns
  -o, --output PATH        JSONL transcript output path

Optional:
  -v, --verbose            Print agent responses in real-time
  --budget FLOAT           Cost cap in dollars
  --synthesize             Run final synthesis agent after deliberation
  --full-context           Show last K messages from ALL agents (not just predecessor)
  --dry-run                Run with simulated agent responses (no LLM calls)
```

### `kumbaja resume` — Continue from an existing transcript

```
kumbaja resume --config PATH --topic TEXT --time SECONDS --window N --max-turns N --output PATH TRANSCRIPT

Same flags as run. Loads prior records and continues from the last turn.
```

### `kumbaja stats` — Show transcript statistics

```
kumbaja stats TRANSCRIPT
```

Displays total turns, tokens, cost, per-agent breakdown, and consensus events.

### `kumbaja validate` — Validate a config file

```
kumbaja validate CONFIG
```

Checks config for errors without starting a deliberation.

## Configuration

| Key | Type | Default | Description |
|---|---|---|---|
| `topology` | string | `ring` | `ring`, `star`, or `mesh` |
| `consensus_threshold` | int | `0` | Consecutive consensus signals to trigger early stop (0 = disabled) |
| `synthesis_model` | string | — | Override model for final synthesis (defaults to first agent's model) |
| `agents` | list | required | Agent configs with `id`, `model`, and optional `system_prompt` |

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

JSONL, one turn per line:

```json
{"turn": 0, "agent_id": "skeptic", "model": "openai/gpt-4o", "timestamp": 1715000000.0, "content": "...", "tokens": {"total": 150, "input": 100, "output": 50}, "cost": 0.001, "consensus": false, "consensus_statement": "", "elapsed": 2.5}
```

## Development

```bash
go build ./...              # Build
go test ./... -v -cover     # Test
go vet ./...                # Lint
```

## Requirements

- Go 1.21+
- [OpenCode](https://opencode.ai) — the LLM agent runner
