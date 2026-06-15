# Contributing

Thanks for helping improve Agora. This project is a single Go binary with a focused CLI surface; keep changes small and test-backed.

## Prerequisites

- Go **1.26.2+** (see `go.mod`)
- [OpenCode](https://opencode.ai) for live deliberation runs (not required for unit tests or `--dry-run`)
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
go run magefile.go build
go run magefile.go install
```

### Test and lint

```bash
go test ./... -race -cover
go vet ./...
golangci-lint run ./...
```

CI runs the same checks on every push and pull request to `main` (see `.github/workflows/ci.yml`).

### Git hooks

If you use lefthook:

```bash
lefthook install
```

Pre-commit runs `go mod tidy`, `golangci-lint run --fix`, and `go vet`. Pre-push runs lint and tests.

### Optional terminal e2e

Maintainer-only smoke test. Requires `rmux` in `PATH` and builds a temporary binary at `/tmp/agora-e2e`.

```bash
./scripts/e2e-rmux.sh
# or
go run magefile.go e2e
```

By default the script dry-runs a quick auto deliberation. Set `AGORA_E2E_DRY_RUN=0` for a live API smoke test when OpenCode and model credentials are configured.

| Variable | Default | Purpose |
|---|---|---|
| `AGORA_E2E_DRY_RUN` | `1` | `0` runs a short live deliberation instead of `--dry-run` |
| `AGORA_E2E_SESSION` | `agora-e2e` | rmux session name |
| `AGORA_E2E_COLS` | `100` | Terminal width |
| `AGORA_E2E_ROWS` | `35` | Terminal height |
| `AGORA_E2E_WAIT` | `2` | Seconds to wait after each command before capture |

### README contract tests

README command examples marked with `<!-- agora-contract: ... -->` are verified by `cmd/agora/command_contract_test.go`. Update both the README and the live CLI when changing flags or commands.

## Project layout

| Path | Purpose |
|---|---|
| `cmd/agora/` | CLI entrypoint and command wiring |
| `internal/` | Core packages (orchestrator, agent, transcript, output, evidence, …) |
| `examples/` | Sample YAML configs |
| `scripts/` | Maintainer tooling (`e2e-rmux.sh`) |
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
