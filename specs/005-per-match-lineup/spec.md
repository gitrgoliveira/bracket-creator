# 005: Per-match team lineups

**Bead:** mp-825
**Status:** Backend slice (Phases 0–4). UI (Phase 5) and viewer overlay (Phase 6) tracked as follow-ups.

## Problem

`domain.TeamLineup` is keyed `(competitionID, teamID, round)`. Pool matches have no
round field, so the store collapses every pool match to **round 0**
(`internal/state/team_lineup.go` `roundHasLiveOrCompletedMatchLocked`,
`internal/engine/scoring.go` `maybeLockTeamLineupsForRound` hard-codes `round = 0`).

Consequences for a pool of N teams (each team plays N−1 encounters in one "round"):

- A team has a single round-0 lineup.
- When that team's **first** pool match goes live, `LockTeamLineupsForRound(comp, 0)`
  freezes it.
- The lineup is then frozen for pool matches 2..N: the operator **cannot** field a
  different order/roster for later encounters.

This contradicts FIK practice: a team may change its order and players between each
team encounter.

> **Since removed (mp-q722):** the freeze itself is gone. No lineup is locked any
> more, round-scoped or match-scoped; see resolved decisions 1 and 2 for today's rule.

> Note: bracket/elimination is already correct: a team appears in exactly one match
> per `Rounds[N]`, so `(teamID, round)` is already 1:1 with the match there. The defect
> is **pool-specific**.

## Approach: additive `MatchID` key (not a full re-key)

Add an optional `MatchID string` to `TeamLineup`.

- Store key resolves to a **match-scoped** key when `MatchID != ""`, else the existing
  **round-scoped** key. The two namespaces never collide (match keys are prefixed).
- Bracket and legacy round-only data load unchanged: **no on-disk migration**. A
  round-only lineup remains valid and is the fallback when no per-match entry exists.
- Per-match entries are independent: pool match 1's lineup and pool match 2's are
  saved and read apart.

### Resolved decisions

1. **No lock.** A lineup, match-scoped or round-scoped, is never locked: it can be
   saved while its match is scheduled, running or finished (mp-q722). The lock this
   decision first described (`LockTeamLineupForMatch`, `LockTeamLineupsForRound`,
   called from the score path) was removed.
2. **Who is refused, per route.** A caller with the valid main password is never
   refused for the state of a match. Otherwise:
   - Officiated tournament: every lineup write needs the main password (401).
   - Self-run tournament, round lineup PUT and DELETE, and match lineup DELETE: main-gated
     as in an officiated one (401 for an empty or a wrong password).
   - Self-run tournament, match lineup PUT: public, because the public score sheet
     saves it when a competitor names a bout's fighter (bc-dhas). An empty password is
     a participant, refused for a match that does not exist (404) or has finished (409
     `result_finalized`, read under the competition's lock); a password that is sent
     but wrong gets 401.

   The `ErrLineupLocked` set-guard this decision first described was removed with the
   lock.
3. **Migration.** None on disk. The fallback key means existing round-keyed lineups
   keep working; the new UI writes match-keyed entries going forward.
4. **FIK 5-person rule.** `Validate(teamSize)` is unchanged and runs per set, regardless
   of keying.
5. **Engine advancement.** `MaybeAdvanceKachinuki` does **not** read lineups (it works
   off a roster snapshot): untouched. The only lineup consumer in the engine is the
   XLSX exporter (`kachinuki_export.go`), which now prefers a match-scoped lineup when
   present.

> **Superseded (2026-08-01, mp-gmcg — see `specs/006-kachinuki-operator-led/spec.md`):**
> decisions 4 and 5 above no longer hold. `TeamLineup.Validate`/`validateFive` (the
> FIK 5-person back-fill/DQ rule) were removed; only the key-only `ValidatePositions`
> check remains, and position vacancies never block a lineup (team sizes are
> unregulated). `MaybeAdvanceKachinuki` now DOES read lineups: it builds each side's
> remaining-roster queue from the saved TeamLineup (match-scoped entry preferred,
> round-scoped fallback) via `kachinukiRemainingRoster`, degrading to the bout-log
> heuristic when no lineup exists — and it is append-only: it never auto-finalizes a
> kachinuki encounter; completion is an explicit operator score write.

## API

`GET/PUT/DELETE /api/competitions/:id/teams/:tid/lineups/:round`: unchanged (round-scoped).

New, match-scoped (added alongside, both live one release):
`GET/PUT/DELETE /api/competitions/:id/teams/:tid/match-lineups/:matchId`

`GET` answers 200 with an empty lineup marked `saved: false` when no match-scoped
lineup exists; the caller then falls back to the round-scoped endpoint (operator
decision 2026-09-27, bc-k404).

## Out of scope (follow-up beads)

- **Phase 5**: Score-editor UI: per-side lineup edit panel + "Copy from previous match".
- **Phase 6**: Viewer/TV overlay rendering of the per-match lineup.
