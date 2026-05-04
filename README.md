# Agora

**Closed-loop multi-agent deliberation system.**

Agora orchestrates N heterogeneous LLM-based agents in a ring, star, or mesh topology, passing turns and maintaining a shared transcript. Agents argue, debate, and deliberate on a topic until a time limit, turn limit, cost budget, or consensus threshold is reached.

Written in Go. Compiles to a single static binary.

## Quick Start

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

# Stats
./agora stats transcript.jsonl
```

### With your own config and real LLMs

```bash
./agora run \
  --config examples/code-review.yaml \
  --topic "Should we adopt microservices?"
```

### Install globally

```bash
go install ./cmd/agora
agora validate examples/example-default.yaml
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
  -v, --verbose            Print agent responses in real-time
  --budget FLOAT           Cost cap in dollars
  --synthesize             Run final synthesis agent after deliberation
  --full-context           Show last K messages from ALL agents (not just predecessor)
  --dry-run                Run with simulated agent responses (no LLM calls)
```

### `agora resume` — Continue from an existing transcript

```
agora resume --config PATH --topic TEXT TRANSCRIPT [flags]

Same optional flags as run. Loads prior records and continues from the last turn.
```

### `agora stats` — Show transcript statistics

```
agora stats TRANSCRIPT
```

Displays total turns, tokens, cost, per-agent breakdown, and consensus events.

### `agora validate` — Validate a config file

```
agora validate CONFIG
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
