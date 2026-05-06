# TODO

## ⇶ Critical

## ⇉ Degraded

- [x] Orkestrera Task 3 blocked: config preview/header implementation dispatch returned empty twice, leaving `.agentera/plan.yaml` pending and no progress evidence — resolved by recovery inspection in Cycle 30
- [x] go.mod module path says kumbaja, should be agora (Decision 3 renamed project) — fixed in 8b729a4
- [x] Orchestrator core loop at 0% coverage (partial: termination + turn execution tested in Cycle 5; Synthesize tested in Cycle 16; Run loop: consensus/max_turns/unlimited tested, time+budget halts tested at checkTerminationConditions level)

## → Normal

- [x] Fix `--context` delivery: agents must receive bounded safe text content once, while transcripts keep source references only
- [x] Cross-version parity test: run identical deliberation in Python and Go, diff transcripts
- [x] CI workflow (Go build, test, lint) for main branch

## ⇢ Annoying

- [x] Auto mode for `resume` command
- [x] `--yes` flag to skip preview in auto mode
- [ ] Tune auto mode level caps based on usage — Decision 4 caps are provisional
- [ ] Decompose executeTurn (106 lines) — extract token/cost parsing helpers
- [ ] Add source/domain allowlists for web research evidence when users need stricter provenance controls
- [ ] Add explicit research refresh/replay controls for resumed transcripts instead of always reusing prior evidence
- [ ] Evaluate non-text context support (PDF/DOCX/browser-rendered pages) without weakening current text-only safety
- [x] OutputManager at 0% coverage — snapshot-test drawPanel/drawTable/wrapText (covered in d956003; package coverage 67.3%)

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
