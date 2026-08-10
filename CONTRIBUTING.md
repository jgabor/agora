# Contributing

Thanks for helping improve Agora. This project is a single Go binary with a focused CLI surface; keep changes small and test-backed.

## Prerequisites

- Go **1.26.2+** (see `go.mod`)
- [OpenCode](https://opencode.ai) for live deliberation runs (not required for unit tests or `--dry-run`)
- [Mage](https://magefile.org/) for Mage targets (the module pins its version)
- Optional: [golangci-lint](https://golangci-lint.run/) and [lefthook](https://github.com/evilmartians/lefthook) for local hooks

## Quick start

```bash
git clone https://github.com/jgabor/agora && cd agora
go build -o agora ./cmd/agora
go test ./...
```

## Development workflow

### Build and install

```bash
go build ./...
go install ./cmd/agora

# Or via mage (outputs to build/agora)
mage build
mage install
```

### Test and lint

```bash
go test ./... -race -cover
go vet ./...
golangci-lint run ./...
```

CI runs the same checks on every push and pull request to `main` (see `.github/workflows/ci.yml`).

### CLI-discovery evaluator

This opt-in maintainer tool evaluates whether a coding agent can discover and
successfully invoke Agora from the CLI. It assigns an ordinal to every first
valid, unique OpenCode `part.id` tool call. The score is the ordinal at the
first completed qualifying checkout-wrapper Agora run. Failed, denied, and
spoofing attempts before that run also receive ordinals. Exact duplicate
lifecycle snapshots add no ordinal; lifecycle updates retain their original
ordinal, and later calls cannot change the frozen score.

Analysis writes a schema-versioned result with checkout and OpenCode provenance,
a deterministic checklist and trace, known-versus-unknown usage fields, and
relative references to raw evidence. Put trial output under the ignored
`.agora-evaluator/cli-discovery/` root. Never commit results or credentials.

For a live trial, use `mage eval:cliDiscoveryHelp` for the current local-tool
requirements and tested OpenCode boundary. It needs a supported OpenCode
executable and authentication for the selected outer provider. It isolates user
state, keeps only that outer call live, and forces nested Agora runs to a dry
run with fresh output. For the `opencode` provider, an inherited nonempty
`OPENCODE_API_KEY` takes precedence over the selected `auth.json` entry. The
evaluator stages only the selected credential in mode-restricted temporary
OpenCode auth state. The outer process and every shell or nested child start
without credential environment variables, and the temporary state is removed
after child reaping.

One live outer agent/model session can incur provider cost. The self-test and
offline modes do not. Do not infer a dollar cost from this tool.

Discover the targets and use the evaluator help before a live trial. The help
surface is the sole contract for flags, defaults, allowed values, and detailed
usage, so this guide does not repeat them.

```bash
mage -l
mage eval:cliDiscoveryHelp

# Provider-free checks
./scripts/check-eval-cli-discovery.sh
mage eval:cliDiscoveryOfflineSelfTest
mage eval:cliDiscoverySelfTest

# After reviewing the help, prerequisites, and cost
mage eval:cliDiscovery <new-output-directory>
```

Treat a result as one sensitive scenario, not a general benchmark. Model,
OpenCode version and behavior, prompt, and environment can change it. Live
results are not fully reproducible, and CI never runs a provider-backed trial.

### Git hooks

If you use lefthook:

```bash
lefthook install
```

Pre-commit runs `go mod tidy`, `golangci-lint run --fix`, and `go vet`. Pre-push runs lint and tests.

### Optional terminal e2e

Maintainer-only smoke test. Requires `termctrl` in `PATH` and keeps its binary and transcript in an isolated temporary directory.

```bash
./scripts/e2e-termctrl.sh
# or
mage e2e
```

By default the script dry-runs a quick auto deliberation. Set `AGORA_E2E_DRY_RUN=0` for a live API smoke test when OpenCode and model credentials are configured.

| Variable | Default | Purpose |
|---|---|---|
| `AGORA_E2E_DRY_RUN` | `1` | `0` runs a short live deliberation instead of `--dry-run` |
| `AGORA_E2E_SESSION` | `agora-e2e-<PID>` | termctrl session name |
| `AGORA_E2E_COLS` | `100` | Terminal width |
| `AGORA_E2E_ROWS` | `35` | Terminal height |
| `AGORA_E2E_COMMAND_TIMEOUT_MS` | `10000` | Timeout for each short command |
| `AGORA_E2E_DELIBERATION_TIMEOUT_MS` | `300000` | Timeout for the deliberation |
| `AGORA_E2E_MODEL` | `opencode/big-pickle` | Model used by the deliberation |

### README contract tests

README command examples marked with `<!-- agora-contract: ... -->` are verified by `cmd/agora/command_contract_test.go`. Update both the README and the live CLI when changing flags or commands.

## Project layout

| Path | Purpose |
|---|---|
| `cmd/agora/` | CLI entrypoint and command wiring |
| `internal/` | Core packages (orchestrator, agent, transcript, output, evidence, …) |
| `examples/` | Sample YAML configs |
| `scripts/` | Maintainer tooling (`e2e-termctrl.sh`) |
| `.agentera/` | Tracked Agentera SDLC artifacts (vision, decisions, progress, health) |
| `.agentera/archive/` | Completed plan archive |

Project vision and architectural decisions live in `.agentera/vision.yaml` and `.agentera/decisions.yaml`. Completed implementation plans are archived under `.agentera/archive/` rather than `docs/plans/`.

## Pull requests

1. Branch from `main`.
2. Add or update tests for behavior changes.
3. Run `go test ./...` and `go vet ./...` before opening the PR.
4. Update `README.md` and `CHANGELOG.md` when user-facing CLI behavior changes.

## Reporting issues

See [SECURITY.md](SECURITY.md) for sensitive reports. For bugs and feature requests, open a GitHub issue with reproduction steps, config, and expected vs actual behavior.
