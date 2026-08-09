# 006: Operator-led kachinuki + unregulated team sizes + 3-way schedule estimate

**Bead:** mp-gmcg
**Status:** Approved and implemented on branch `feat/kachinuki-operator-led-mp-gmcg`
(PR #379). The "Resolved decisions" below are the authority cited by the code
comments in `internal/engine/kachinuki.go`, `internal/mobileapp/handlers_match.go`
and `web-mobile/js/admin_scoring_team.jsx`; see Implementation plan below.

## Problem

Kachinuki (winner-stays-on) as shipped auto-finalizes: the engine decides the
encounter is over when its roster snapshot says one side is exhausted, and a
pre-write 409 gate (`CheckKachinukiPrematureCompletion`) blocks the operator
from completing a match the app thinks is unfinished. Three real-world rules
break this model:

1. **Two ways to win.** A kachinuki encounter is won by exhaustion OR by the
   taisho-defeated rule (the taisho — always a team's LAST fighter — loses, so
   the team loses). The taisho rule can be in force in pools or in early
   knockout rounds; only the shiaijo operator knows which governs a given
   match. The app must never hard-code the rule by phase.
2. **Team sizes are not regulated.** The configured `TeamSize` is a nominal
   planning number, not a guarantee. The app's roster snapshot (lineups +
   bout-log retirements) is therefore ADVISORY: a side that looks exhausted
   may still have fighters the app has never seen. Auto-finalize and the 409
   gate both second-guess the operator from unreliable data.
3. **Position vacancies are irrelevant.** The FIK 5-person back-fill/DQ rule
   (`TeamLineup.Validate`/`validateFive`) must not be enforced — it was
   already dead on every production path, and the frontend's "Lineup
   incomplete" hint contradicts unregulated play.

Separately, the schedule estimator returns a single number, but a kachinuki
match has a variable bout count: nominal size N runs anywhere from N bouts
(one fighter sweeps) to 2N−1 bouts (every bout retires exactly one player).
Operators need best / average / worst.

## Resolved decisions (user-confirmed)

1. **Completion is operator-led, two always-visible buttons, no modals.** The
   score editor in kachinuki bout mode shows **[Record bout]** and
   **[End match]**. Which button the operator taps IS the "does the match
   continue?" answer. Record bout keeps today's auto-append of the next
   pairing (winner stays; tie retires both).
2. **End match derives the outcome from context — NO picker, ever.** The code
   decides whether someone won (last scored bout) and whether a draw is
   acceptable (phase):

   | Last scored bout | Pools / League | Knockout |
   |---|---|---|
   | Has a winner | Ends instantly: that team wins. | Same. |
   | Tied | Ends instantly: **drawn encounter**. | **End blocked** (no draws in a knockout): inline hint tells the operator to continue — next bout or encho — until there is a point. |

   - No roster math gates End match: whether a team is out of players is the
     operator's knowledge, expressed by WHICH button they tap.
   - Tie against a NON-last opponent where the surviving team should win: the
     operator taps **Record bout** — the app appends the next bout KEEPING
     THE FIGHTER WHO JUST TIED on the replacement-less side (operator
     ruling 2026-08-01: under the taisho rule they stay on, with nothing
     to re-type) paired against the surviving team's next fighter; under
     plain exhaustion the operator gives the survivor the walkover point
     (per-bout fusensho) — then **End match** on that point. The extra
     bout IS how the win is expressed; no menu shortcut.
     **Implementation note (2026-08-01):** this flow was initially a
     spec/engine gap — advanceAfterHikiwake flagged advisory MatchEnded and
     appended nothing, and a self-review WRONGLY "fixed" the docs to match
     the engine (docs define behaviour; the engine is the suspect when they
     diverge — user correction). The engine now appends the walkover slot
     on a hikiwake that empties exactly one advisory side (pinned by
     TestAdvanceKachinuki_HikiwakeSideAExhausted/BExhausted and
     TestScoreHandler_KachinukiHikiwakeAppendsWalkoverSlot); the slot now
     KEEPS the tied fighter's name (stays-on ruling — no re-typing for
     the taisho rule; exhaustion mode fusenshos the survivor over it);
     an abandoned slot is stripped on the completed write. The WIN path
     is unchanged: a decisive last bout needs no walkover, End derives
     from it directly. Kachinuki sides with no resolved name and no
     lineup route offer a free-typed input (manual rows / bootstrap /
     rosterless; also fixed a latent 4xx for lineup picks past
     teamSize).
   - Last player vs last player in a knockout: **encho** — the SAME pair
     continues on the SAME bout (existing encho metadata, "(E)" suffix) until
     one loses; then End match. A tied final pair in pools is simply the
     drawn-encounter row above.
   - **Refinement (user ruling, 2026-08-01):** whether the final pairing
     must produce a result (taisho must be defeated) is OPERATOR DISCRETION
     in ANY phase — never derivable from pool-vs-bracket. So encho is
     offered on every tied last bout, pools included: the operator chooses
     End (drawn encounter, pools/league only), Record bout (both retire,
     next pair), or Encho (same pair fights to a result). The server
     accepts bout-level encho for kachinuki competitions in every phase
     (an earlier bracket-only validation scoping was reverted as phase
     hard-coding). Only the knockout END stays blocked while tied.
3. **Daihyosen does not exist in kachinuki.** A tied final bout goes to encho,
   never a separate representative bout. `POST .../daihyosen` returns 400 for
   kachinuki competitions; the UI affordance is hidden.
4. **Mistake recovery.** **[Reopen match]** on a completed kachinuki match
   (status back to running, winner/decision cleared, more bouts addable);
   correction mode (every bout editable on a completed match) already exists.
   On a completed write, trailing UNSCORED auto-appended bouts are stripped so
   an abandoned pairing never reaches exports — this replaces a dedicated
   "undo last bout" endpoint.
5. **Win decisions persist unchanged.** Both win rules record
   `decision: "kachinuki-exhaustion"` with the winning team; a drawn pool
   encounter records `hikiwake`. No new wire decision values.
6. **Estimator range.** best = N, worst = 2N−1, average = midpoint (3N−1)/2,
   with N = configured TeamSize as the nominal planning number.
   `totalDurationMinutes` becomes the AVERAGE scenario (was implicitly the
   best case); new additive fields `bestCaseMinutes` / `worstCaseMinutes`
   bracket it. Individual and fixed-format matches: all three equal. The
   court-slot assigners keep booking nominal N-bout blocks (pre-existing,
   now documented as the best case).
7. **A kachinuki encounter stays ONE team match everywhere** (one match ID,
   bouts as subResults); nothing about identity, SSE, standings, or display
   surfaces changes.

## Implementation plan

### Phase A — engine & handlers (backend)

- [x] `MaybeAdvanceKachinuki` append-only: no auto-finalize, no auto pool
  draw, no bracket propagation from this path (`engine/kachinuki.go`).
- [x] Delete `CheckKachinukiPrematureCompletion` + `ErrKachinukiPrematureCompletion`
  + handler 409 + `deps.go` interface method + stub.
- [x] `POST .../daihyosen` → 400 for kachinuki (`handlers_daihyosen.go`).
- [x] `validateBracketCompletion` message mentions encho (winnerless knockout
  completion stays rejected — that guard is phase-correct).
- [x] Delete `TeamLineup.Validate`/`validateFive`/vacancy sentinels
  (`domain/team_lineup.go`); `ValidatePositions` (key-only) remains.
- [x] Engine test suite rewritten to the operator-led contract (11 tests).
- [x] Strip trailing unscored bouts on a completed kachinuki write
  (`applyKachinukiMerge` chokepoint — covers pool/bracket, tx/non-tx;
  trailing legacy daihyosen sentinels halt the walk, never deleted).
- [x] Rewrite `TestScoreHandler_KachinukiPrematureCompletionRejected` →
  `TestScoreHandler_KachinukiEarlyCompletionAccepted` (200; pins the
  taisho-defeat early end + the strip).
- [x] Encho-on-final-bout persistence test through the score path. This
  surfaced a production gap: `validateSubBout` rejected encho on any
  numbered bout (daihyosen-only). Relaxed for kachinuki competitions
  only (`allowNumberedEncho`); hantei stays daihyosen-only.
- [x] New handler test: daihyosen on kachinuki → 400.
- [x] Sanctioned reopen path (recon found completed→running score writes
  are deliberately discarded, so [Reopen match] needed a backend):
  `POST /api/competitions/:id/matches/:mid/reopen`, kachinuki-only
  (400 otherwise), admin-gated, 409 when not completed or when a
  bracket match's propagated winner was already fought downstream;
  unfought downstream slots are retracted to their generation
  placeholders. Stale-write guard untouched and pinned by test.

### Phase B — estimator (backend)

- [x] `ScheduleEstimate` + `bestCaseMinutes`/`worstCaseMinutes`; average is
  the headline (`engine/schedule.go`).
- [x] `kachinukiBoutRange`, `perMatchElapsedBouts` (float bouts),
  `perMatchElapsedMinutesBouts` (`engine/scheduler_slots.go`).
- [x] Both producers price 3 scenarios; `GET /api/schedule/estimate` takes
  `teamMatchType=kachinuki`.
- [x] Tests: range math in both producers (exact hand-derived values),
  handler param, non-kachinuki collapse, fractional-bout helpers.

### Phase C — frontend (`web-mobile/js/`)

- [x] **[End match]** button + context-derived outcome (winner from the last
  scored bout; tie → draw in pools/league, blocked-with-hint in knockout; NO
  picker) in `admin_scoring_team.jsx`; derivation as a pure helper
  (`deriveKachinukiEndOutcome`), unit-tested.
- [x] **Encho** path: `applyKachinukiEncho` marks the tied final bout encho,
  bumps the match-level counter for the "(E)" suffix, keeps scoring same pair.
- [x] **Manual next bout** (`addManualBout`) when the server can't infer the
  pairing (unknown roster continue path); local subResult, free-typed names.
- [x] **[Reopen match]** on completed kachinuki matches (`canReopenKachinukiMatch`
  gate + two-step arm/confirm + inline 409 surfacing).
- [x] Suppress "Lineup incomplete" hint for kachinuki; hide daihyosen
  affordance for kachinuki (add-affordance gated on `!isKachinuki`).
- [x] Estimator UI shows Best / Average / Worst (`estimateRangeParts` in
  `admin_schedule_utils.jsx`; schedule page + competition-settings panel).
- [x] JSX tests: derivation helper (win/tie by phase, blocked states),
  reopen gate, bout-mode, estimator formatter; render matrix updated for the
  hidden kachinuki daihyosen affordance.

### Phase D — docs, spec, verification

- [x] `specs/openapi.yaml`: estimate fields + `teamMatchType` param,
  daihyosen kachinuki 400, reopen endpoint, kachinuki numbered-bout encho,
  operator-led completion semantics note (also fixed stale `ceremony` →
  `ceremonyMinutes` and `matchDuration` integer → number).
- [x] `docs/user-guide/organisers/team-tournaments.md` +
  `docs/user-guide/court-operators/scoring-a-match.md`: rewritten kachinuki
  scoring flow (two win rules, End match, tie/encho, no daihyosen, reopen,
  manual bout) plus the unregulated-team-sizes rewrite of the old FIK
  incomplete-team section; captured UI screenshots embedded (MCP capture,
  no internal references, no em-dashes). Estimator best/average/worst reading
  added to `run-tournament.md` with the Overview screenshot.
- [x] `CLAUDE.md`: Team Lineups & Kachinuki bullet rewritten (operator-led,
  removed FIK 5-person rule, encho/reopen); stale
  `specs/005-per-match-lineup/spec.md` note superseded with a mp-gmcg block.
- [x] `make go/test` (≥85% coverage gate), JSX tests (2489), `make examples`,
  `make docs/build` (strict) all green.
- [x] Browser UAT via a live mobile-app: kachinuki knockout end-to-end
  (decisive end, knockout tie → blocked hint → encho, reopen with downstream
  retract, manual bout), estimator showing three numbers; MCP screenshots
  captured (docs/screenshots/kachinuki-*.png).
- [x] Single PR from `feat/kachinuki-operator-led-mp-gmcg` with the template
  body and every test-plan checkbox executed: PR #379.
- [x] Tri-review round (3 lenses + adversarial verify, wf_0b6c2828-20e). One
  verifier crashed and its finding silently dropped (the known false-clean
  mode), so all findings were hand-verified. Fixes applied in-PR:
  reopen's self-run gate moved off the hand-rolled check (which lacked the
  F4 empty-password fail-closed branch) into the central
  isSelfRunMainGatedConfigRoute allowlist + TestSelfRun_ReopenRequiresMainPassword
  (401 now, was 403 — openapi updated); stale auto-finalize doc comments
  in engine/kachinuki.go rewritten to the advisory contract. The round's
  third fix — bout encho scoped to BRACKET matches only — was WRONG and
  is superseded by the operator-discretion refinement under decision 2:
  the scoping hard-coded the taisho rule by phase. Reverted
  (allowNumberedEnchoFor is competition-scoped again, IsBracketMatchID
  removed, TestScoreHandler_KachinukiPoolBoutEnchoAccepted pins pool
  encho persisting, kachinukiEnchoAvailable renders the Encho affordance
  on every tied last bout, docs gained a "Kachinuki modes" section with
  the per-mode what-happens-when table).
- [x] User ruling (2026-08-01): "Operator input determines the bout outcome" —
  overrode the tri-review disposition that kept two scored-bout predicates.
  subBoutHasBeenPlayed is now THE single played-bout primitive:
  buildKachinukiEndEntries filters End-derivation entries with it, so End,
  the wire filter, visible positions, and the encho target all agree on
  which bout is last (they previously disagreed: Encho targeted the last
  INPUT bout while End derived from the last OUTCOME bout). A fouls-only
  last bout is a fought bout with no ippon = hikiwake → pool draw /
  knockout blocked, never skipped. Pinned by the fouls-only composition
  test (verified red against the old outcome-filter).

## Out of scope

- Slot-assigner cadence change (still books nominal N-bout blocks).
- Removing legacy daihyosen rows from previously-recorded kachinuki data
  (the engine keeps skipping Position=-1 rows defensively).
- Expected-value (probabilistic) bout-count model — midpoint only.
