# Output Conventions

This document is the implementation contract for the Charm v2 agent theater output system. It exists so future renderer work shares one visual language instead of adding one-off Lipgloss, Glamour, ANSI, or width behavior.

## Renderer Boundary

- Lipgloss v2 owns structured terminal UI: cast previews, deliberation headers, status lines, agent metadata, panels, tables, metric rows, section dividers, badges, borders, spacing, alignment, wrapping, and all color/fallback decisions.
- Glamour v2 is only for long-form Markdown-like prose where paragraph and list rendering help readability: verbose agent bodies, synthesis recommendation prose, arguments, agreements, and tensions when they are prose-heavy.
- Do not pass compact metadata through Glamour. Agent identity, model, elapsed time, tokens, cost, consensus, warnings, errors, and stats remain Lipgloss-rendered structured UI even when adjacent prose uses Glamour.
- Plain text fallback is part of the renderer contract, not an afterthought. Any styled component must first have a readable text representation.

## Cast Identity

- Agent label: render the exact configured or transcript `agent_id` text everywhere. Do not title-case, abbreviate, or substitute role names. If extra display names are ever added, keep `agent_id` visible.
- Agent ordering: use deliberation config order for previews, headers, turn legends, and per-agent stats. Orchestrator/system rows appear before the cast only when they are actual transcript records. Synthesizer output appears after the deliberation cast as a separate synthesis role.
- Agent badge: structured surfaces identify agents with a stable ordinal plus label, for example `[A1 strategist]` or `[A3 skeptic]`. The ordinal comes from config order and is reused on all output surfaces for that run.
- Agent visual treatment: each agent gets one stable accent derived from normalized `agent_id` and config order. The same accent applies to that agent's badge, turn marker, and per-agent stats row. Color is decorative; the ordinal and label carry identity.
- Unknown or resumed agents not present in the current config keep their transcript label and receive a fallback badge after configured agents, for example `[A? legacy_agent]`. Do not drop them from stats or consensus events.

## Semantic Labels

Color must never be the only carrier of meaning. Every semantic state uses a text label and, when supported, an optional symbol.

| Meaning | Rich Symbol | Plain Label | Example Plain Text |
| --- | --- | --- | --- |
| Success | `✓` | `SUCCESS` | `[SUCCESS] Deliberation complete` |
| Error | `✗` | `ERROR` | `[ERROR] config validation failed` |
| Consensus | `✓` | `CONSENSUS` | `[CONSENSUS] agent statement` |
| Warning | `!` | `WARNING` | `[WARNING] budget nearly exhausted` |
| Cost | `$` | `COST` | `COST $0.012345` |
| Elapsed time | `⏱` | `ELAPSED` | `ELAPSED 3.4s` |
| Model | none | `MODEL` | `MODEL opencode/foo` |
| Agent identity | badge | `AGENT` | `AGENT [A2 skeptic]` |

Plain/no-color output keeps the bracketed labels. Rich terminals may add color and Unicode symbols, but the label remains present for success, error, consensus, and warning. Compact metric rows may use `MODEL`, `ELAPSED`, and `COST` labels instead of symbols.

## Terminal Fallback

- Treat `NO_COLOR` as a hard request for no color.
- Treat `TERM=dumb`, unset `TERM`, non-TTY stdout, and CI as plain-first modes: no required meaning depends on ANSI color, Unicode borders, or dim text.
- Low-color terminals may use bold and a small color palette only after the plain labels are present.
- Plain mode uses ASCII-safe structure: `+`, `-`, `|`, `*`, `!`, and bracketed labels. Avoid ambiguous glyph-only markers.
- Unicode ornamentation is optional. If width measurement or terminal capability is uncertain, prefer ASCII borders and text labels over box drawing or symbol-heavy output.

## Width Rules

- Determine render width once per top-level output call and pass it through child renderers.
- Clamp structured UI to a practical range: minimum 40 columns, target terminal width when known, and no wider than 100 columns unless the terminal is wider and the surface materially benefits.
- Required labels and values must not disappear at 40 columns. Wrap before truncating; truncate only decorative prose previews, and mark truncation with `...`.
- Tables must degrade to stacked key/value rows when columns cannot fit without losing labels.
- Glamour prose width must match the content width inside its surrounding Lipgloss container so prose does not overflow borders.
- Width tests should prefer semantic assertions plus 40/80/120-column coverage over brittle full ANSI snapshots.

## Surface Mapping

- Config preview and deliberation header: Lipgloss structured UI; show cast in config order using stable badges.
- Turn progress: Lipgloss structured status; include `AGENT`, `MODEL`, `ELAPSED`, tokens, `COST`, and `CONSENSUS` labels where applicable.
- Verbose agent content: Lipgloss metadata header followed by Glamour-rendered prose when Markdown-like content is present; plain text otherwise.
- Final stats and transcript stats: Lipgloss tables or stacked rows; per-agent rows follow cast order.
- Synthesis: Lipgloss section and metadata; Glamour only for recommendation/argument prose blocks.
