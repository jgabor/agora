# TODO

Open follow-up work for Agora. Completed items live in git history, `CHANGELOG.md`, and `.agentera/progress.yaml`.

## ⇶ Critical

- [ ] [id:fhmdtxdfvn] Make the debate ledger the authoritative control plane for proposals,
      objections, votes, halting, and synthesis. Replace free-text consensus
      marker streaks with a versioned canonical proposal and structured votes
      from unique agents against that exact version; revisions invalidate stale
      endorsements. Halt for consensus only when one current proposal satisfies
      the configured threshold, minimum rounds, deliverable contract, and
      objection-disposition rules; otherwise record an explicit no-consensus
      outcome with dissents. Give synthesis the proposal, votes, objections,
      evidence status, and halt reason so it cannot present an independent
      recommendation as group consensus. Remove the consensus-rejection and
      topic-specific deliverable regexes once the structured protocol replaces
      them. Cover incompatible endorsements, stale votes, repeated prose without
      votes, max-turn no-consensus, and non-consensus synthesis in regression
      tests.
- [ ] [id:wbdfmkswsb] Add active deliberation control driven by the typed ledger. Run a
      moderator step at round boundaries and on stagnation to select the most
      important unresolved crux, direct a specific agent to answer a specific
      objection, request verification or a proposal revision, call a vote, or
      declare no consensus with recorded dissents. Add explicit opening,
      rebuttal, drafting, and voting phases plus convergence and repetition
      signals so speaker selection and per-turn instructions respond to debate
      state instead of remaining static round-robin prompts.
- [ ] [id:kxgevwibzz] Make decisive factual claims evidence-backed. Let objections request
      verification; require claims that materially support the current proposal
      to cite supplied evidence or remain explicitly unverified; and preserve
      unresolved evidence gaps through proposal revisions and synthesis. The
      final artifact must distinguish verified facts, inferences, assumptions,
      and recommendations. Cover unsupported code claims, conflicting evidence,
      failed verification, and synthesis with unresolved evidence in regression
      tests.

## ⇉ Degraded

_None._

## → Normal

_None._

## ⇢ Annoying

- [ ] [id:uhndulkcgf] Add source/domain allowlists for web research evidence when users need stricter provenance controls
- [ ] [id:kzhpfqkdhu] Add explicit research refresh/replay controls for resumed transcripts instead of always reusing prior evidence
- [ ] [id:ynlzskgutb] Evaluate non-text context support (PDF/DOCX/browser-rendered pages) without weakening current text-only safety
- [ ] [id:oowkozctdp] Add defined output themes and named cast color palettes; default remains terminal theme-adaptive ANSI slots
- [ ] [id:bcimzeorwv] Evaluate named profiles after `prime` exists; current `config.yaml` covers defaults but not reusable identities
- [ ] [id:fbjyuemzvg] Tune auto mode level caps based on usage — Decision 4 caps are provisional

## → Degraded
- [ ] [id:dynufsdiqr] [fix] Align opening proposal contract with independent opening envelopes
- [ ] [id:eprrpqhkrb] [fix] Bind terminal consensus gates to the established run contract
