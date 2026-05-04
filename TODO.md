# TODO

## ⇶ Critical

## ⇉ Degraded
- [ ] go.mod module path says kumbaja, should be agora (Decision 3 renamed project)
- [x] Orchestrator core loop at 0% coverage (partial: termination + turn execution tested in Cycle 5; Run/Synthesize still uncovered)

## → Normal
- [x] Cross-version parity test: run identical deliberation in Python and Go, diff transcripts
- [x] CI workflow (Go build, test, lint) for main branch

## ⇢ Annoying
- [ ] Decompose executeTurn (106 lines) — extract token/cost parsing helpers
- [ ] OutputManager at 0% coverage — snapshot-test drawPanel/drawTable/wrapText

## Resolved
- [x] Merge go-port to main — Python removed, Go is canonical
- [x] Write VISION.md — north star, indie researcher persona, human-in-the-loop direction
- [x] Go module bootstrap with types and YAML config loading
- [x] Transcript manager with ring/star/mesh history windowing
- [x] Agent runner with opencode subprocess and consensus extraction
- [x] Orchestrator with five termination conditions and signal handling
- [x] Synthesis engine producing structured JSON summaries
- [x] Terminal output with ANSI-styled panels and tables
- [x] CLI commands (run, stats, validate, resume) via cobra
- [x] Test suite with semantic-parity verification (43 tests)
