# TODO

Open follow-up work for Agora. Completed items live in git history, `CHANGELOG.md`, and `.agentera/progress.yaml`.

## ⇶ Critical

_None._

## ⇉ Degraded

_None._

## → Normal

_None._

## ⇢ Annoying

- [ ] Add source/domain allowlists for web research evidence when users need stricter provenance controls
- [ ] Add explicit research refresh/replay controls for resumed transcripts instead of always reusing prior evidence
- [ ] Evaluate non-text context support (PDF/DOCX/browser-rendered pages) without weakening current text-only safety
- [ ] Add defined output themes and named cast color palettes; default remains terminal theme-adaptive ANSI slots
- [ ] Evaluate named profiles after `prime` exists; current `settings.yaml` covers defaults but not reusable identities
- [ ] Tune auto mode level caps based on usage — Decision 4 caps are provisional

## Roadmap

What limits deliberation quality today

1.  Consensus is free-text markers with regex heuristics, on no shared
    object. ExtractConsensus matches [CONSENSUS: ...] and then filters
    via hardcoded rejection phrases, including \brefine the laws\b and
    the deliverableLawLine regex ^\d+\. An agent must in deliverable.go.
    Those are overfit to one specific "three laws" test topic living in
    general infrastructure. Worse, ConsecutiveAgentConsensusCount counts
    consecutive marked turns without checking the statements refer to
    the same thing. Agents can "reach consensus" while endorsing
    different statements, and in ring topology an agent often can't
    even see the statement it is supposedly agreeing with.

2.  The moderator doesn't exist at runtime. ModeratorPrompt and
    ModeratorConfig are defined but never invoked; "moderator" is just a
    label on seed/evidence records. Nothing detects repetition or
    stalemate, redirects drift, or forces a vote.

3.  Situational blindness reduced. Agents now receive the turn number,
    rounds remaining, time/budget pressure, the halting rule, who else
    is on the panel, and their own agent ID via the situational-awareness
    envelope fields. The remaining gap is per-turn directive instructions
    ("round 2 of 3; you must address X's objection to point 2") — that
    belongs to the phase-structure roadmap item.

4.  Defaults now support depth. Non-auto runs scale time, max_turns, and
    window off the cast size, consensus_threshold enables by default,
    and min_rounds floors at three. Scheduling is still rigid
    round-robin, and mesh still shares a star branch; the proposal,
    moderator, and phase-structure roadmap items remain the next levers.

### Missing pieces, ranked by impact

1.  A first-class proposal artifact with real voting. Make consensus an
    endorsement of a versioned canonical draft (proposal v3), not a
    free-text marker streak. Agents emit structured output (position,
    responds_to, concessions, vote: endorse|object(reason)). This
    deletes consensusRejectionPatterns, the deliverable-gate regex, and
    the autogen prompt hack ("agents must not mark CONSENSUS until the
    draft is endorsed verbatim"), replacing hope-based prompting with a
    mechanism.

2.  An active moderator loop. Run the already-written moderator every
    round or on trigger: summarize into the ledger, name the crux,
    select the next speaker (disagreement-driven instead of
    round-robin), call the vote, or declare "no consensus, recording
    dissents" instead of stalling until max_turns.

3.  Phase structure. Opening positions (parallelizable, independent) →
    rebuttal rounds → drafting/convergence → vote. Per-turn instructions
    in the envelope ("round 2 of 3; you must address X's objection to
    point 2") rather than the same static persona prompt each turn.

4.  Convergence and stalemate metrics. Turn-over-turn similarity (even
    lexical) to detect repetition; surfaced in stats and used as a
    moderator trigger. Right now nothing measures whether the debate is
    moving.
