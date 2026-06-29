# 005 : Per-match team lineups

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
- The lineup is then frozen for pool matches 2..N : the operator **cannot** field a
  different order/roster for later encounters.

This contradicts FIK practice: a team may change its order and players between each
team encounter.

> Note: bracket/elimination is already correct : a team appears in exactly one match
> per `Rounds[N]`, so `(teamID, round)` is already 1:1 with the match there. The defect
> is **pool-specific**.

## Approach : additive `MatchID` key (not a full re-key)

Add an optional `MatchID string` to `TeamLineup`.

- Store key resolves to a **match-scoped** key when `MatchID != ""`, else the existing
  **round-scoped** key. The two namespaces never collide (match keys are prefixed).
- Bracket and legacy round-only data load unchanged : **no on-disk migration**. A
  round-only lineup remains valid and is the fallback when no per-match entry exists.
- Per-match entries lock independently: locking pool match 1 does **not** freeze the
  (still-unstarted) lineup for pool match 2.

### Resolved decisions

1. **Lock granularity.** A per-match (`MatchID != ""`) lineup locks only when *its*
   match transitions to live/completed. Round-keyed (legacy) lineups keep the existing
   round-0 freeze behavior. The engine score path calls **both**
   `LockTeamLineupForMatch(comp, matchID)` (new) and the legacy
   `LockTeamLineupsForRound(comp, 0)` (unchanged) so neither keying regresses during
   transition.
2. **TOCTOU set-guard.** `SetTeamLineup` for a match-scoped lineup refuses with
   `ErrLineupLocked` when *that match* is already running/completed (checked by ID).
   Round-scoped sets keep the existing round-status check.
3. **Migration.** None on disk. The fallback key means existing round-keyed lineups
   keep working; the new UI writes match-keyed entries going forward.
4. **FIK 5-person rule.** `Validate(teamSize)` is unchanged and runs per set, regardless
   of keying.
5. **Engine advancement.** `MaybeAdvanceKachinuki` does **not** read lineups (it works
   off a roster snapshot) : untouched. The only lineup consumer in the engine is the
   XLSX exporter (`kachinuki_export.go`), which now prefers a match-scoped lineup when
   present.

## API

`GET/PUT/DELETE /api/competitions/:id/teams/:tid/lineups/:round` : unchanged (round-scoped).

New, match-scoped (added alongside, both live one release):
`GET/PUT/DELETE /api/competitions/:id/teams/:tid/match-lineups/:matchId`

`GET` returns 404 when no match-scoped lineup exists (caller may fall back to the
round-scoped endpoint).

## Out of scope (follow-up beads)

- **Phase 5** : Score-editor UI: per-side lineup edit panel + "Copy from previous match".
- **Phase 6** : Viewer/TV overlay rendering of the per-match lineup.
