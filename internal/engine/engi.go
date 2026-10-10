// Package engine, engi.go owns the ENTIRE Engi-kyogi (kata competition / flag
// scoring) vertical slice. Engi is a second scoring paradigm: bouts are decided
// by referee flag counts (FlagsA/FlagsB) instead of ippon waza letters, and
// standings rank by wins then accumulated own-side flags.
//
// HARD SEPARATION PRINCIPLE (user directive): engi logic MUST NOT be mixed into
// the kendo scoring code. There are no `if comp.Engi` branches sprinkled through
// computeStandingsFrom, writeMatchResult, recordBracketMatchResult, or the
// shared tie-break logic. The kendo functions are BRANCHED AROUND at single
// dispatch seams (RecordMatchResultWithIneligibility(+Tx) and computeStandings)
// that delegate here; they are never edited internally. The only shared seam is
// the additive persistence DTO fields (MatchResult.FlagsA/FlagsB,
// PlayerStanding.Flags, Competition.Engi).
//
// Reusing the PURE helper propagateBracketWinner is allowed: it only advances a
// decided winner's name forward and computes no score, so it is not kendo
// scoring logic.
package engine

import (
	"fmt"
	"slices"
	"sort"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// engiValidTotal reports whether a flag pair is a valid engi result. Valid
// totals are {1, 3, 5}: odd (so there is always a strict majority and never a
// draw) and at most 5 (the hard cap, there are never more than 5 referees on an
// official panel). The oddness of the total is what guarantees a strict winner:
// an equal split can only sum to an even number, so a {1, 3, 5} total already
// implies flagsA != flagsB and the winner derivation below is total.
func engiValidTotal(flagsA, flagsB int) bool {
	if flagsA < 0 || flagsB < 0 {
		return false
	}
	t := flagsA + flagsB
	return t == 1 || t == 3 || t == 5
}

// engiWinnerSide returns "A" or "B" for the side with more flags. Callers MUST
// have validated via engiValidTotal first (which guarantees flagsA != flagsB).
func engiWinnerSide(flagsA, flagsB int) string {
	if flagsA > flagsB {
		return "A"
	}
	return "B"
}

// recordEngiMatchResult records a completed engi bout (POOL or BRACKET), keyed
// by competition + match id and the two flag counts. It is the engi twin of the
// kendo record path and does NOT route through writeMatchResult /
// recordBracketMatchResult. Validation ({1,3,5}, no draw) lives here.
//
// Pool match: updates the pool-match record in place (winner from flag majority,
// flag counts stored, status completed).
//
// Bracket match (including the "m-bronze" 3rd-place knockout): sets
// Winner/FlagsA/FlagsB on the stored match, then calls the pure
// propagateBracketWinner to advance the decided winner (no advancement out of
// bronze).
//
// correctionReason is the operator-supplied audit note when overwriting a
// previously completed match. It mirrors the kendo path's CorrectionReason
// so the audit trail is preserved for engi competitions.
//
// Returns the persisted MatchResult so the handler can echo / broadcast it.
func (e *Engine) recordEngiMatchResult(h state.StoreTx, compID, matchID string, flagsA, flagsB int, correctionReason string, opts ...ForceOptions) (*state.MatchResult, error) {
	return e.recordEngiMatch(h, compID, matchID, flagsA, flagsB, correctionReason, 0, nil, false, opts...)
}

// engiFinishProbe is the write an engi finish makes, built on the stored
// match: the flags, the winner they decide, completed, stamped with the
// client's stamp and naming the two groups it changes (result and flags).
func engiFinishProbe(prior, result *state.MatchResult) state.MatchResult {
	p := *prior
	p.SubResults = state.CloneSubResults(prior.SubResults)
	p.GroupStamps = nil
	p.Merge = nil
	applyEngiToMatchResult(&p, result.FlagsA, result.FlagsB, engiWinnerSide(result.FlagsA, result.FlagsB), result.CorrectionReason)
	p.ModifiedAt = result.ModifiedAt
	p.Changed = append([]string(nil), engiChangedGroups...)
	p.WriteDoor = doorEngi
	p.RevSession = result.RevSession
	return p
}

// HoldReasonEngiAtomic is the history reason of an engi finish held whole
// because a newer change to one of its two groups is stored: the flags and
// the winner they decide are one change, never applied apart.
const HoldReasonEngiAtomic = "the flags and the winner they decide are kept together"

// engiFinishHeld judges an engi finish by the merge owner before the engi
// recorder writes it (bc-mrgc phase 3): ordered by its stamp against the
// stored result and flags groups, through the same rule every other write
// takes. The flags and the winner they decide are atomic, so when either
// group is held both are (a stale finish must not set a winner over newer
// flags, nor new flags under an older winner). On a hold the report is left
// on result.Merge for the history entry and true is returned.
func engiFinishHeld(prior, result *state.MatchResult, comp *state.Competition, knockout bool) bool {
	probe := engiFinishProbe(prior, result)
	rep := mergeMatchWrite(prior, &probe, matchWriteForward, mergeCtx{comp: comp, knockout: knockout})
	// Default for a write this function ultimately holds whole (the atomic
	// re-merge below, `return true`): the finish's own attempted stamp,
	// exactly as before. The two "applied" returns below override this with
	// the merge's actually-settled stamp instead (bc-cse): a held write
	// never lands, so there is no merged state for it to report.
	result.ModifiedAt = rep.Stamp
	result.WriteDoor = doorEngi
	if len(rep.Held) == 0 {
		// Applied. A HeldEcho here is no loss (bc-mrgc phase 3: the stored
		// flags already held this value, just at an older stamp than the
		// write), so it does not make the finish atomic -- only a REAL held
		// group does. The report stays on the write for what it moved to the
		// history (S2 with R4: a newer recount that left no valid count),
		// which the caller records once the recorder has written the finish.
		result.Merge = rep
		// The merge is the ONE place that ordered the result and flags
		// groups against every stored group, including S2's
		// displaceNewerScoring, which can legitimately move a group's stamp
		// BACK to the finish's own, older time when it replaces a newer but
		// invalid stored count. The recorder has no way to work that out
		// again on its own, so the merge's own per-group stamps ride along
		// on the write for it to apply directly (engi.go's recordEngiMatch,
		// through state.ApplyMergedGroupStamps).
		result.GroupStamps = state.CloneGroupStamps(probe.GroupStamps)
		// The answer must report the stamp the merge actually settled on
		// (bc-cse), never the finish's own attempted stamp (which mergeCtx
		// only used to ORDER the write): probe.ModifiedAt already holds the
		// merged value here -- it is what the recorder below persists onto
		// the stored match too, via ApplyMergedGroupStamps over GroupStamps.
		// rec.ModifiedAt is NOT that value: on the bracket branch,
		// applyEngiToBracketMatch builds a fresh *state.MatchResult before
		// stampEngiChanges ever runs, so it is always 0.
		result.ModifiedAt = probe.ModifiedAt
		return false
	}
	// S2 (operator ruling 2026-10-04, "the newest count decides" in both
	// arrival orders): the finish's own flags are held above because a
	// NEWER count is already on record, but mergeMatchWrite's own S2 block
	// (through deriveWinnerAfterMerge's engi branch) has already turned
	// that stored count into a winner on probe, exactly as the OTHER
	// arrival order reaches when R3 applies the same recount to an
	// already-finished match. Accepting that derived result here, instead
	// of discarding it for the atomic re-merge below, is what makes both
	// arrival orders of the same two writes land on the same state: the
	// recorder is handed the STORED count (never the finish's own, stale
	// one) and the winner it gives; the finish's own attempted flags survive
	// only in the history, as held (reportHeld captured them before the
	// merge copied the stored count over them). engiValidTotal is checked
	// again here defensively -- deriveWinnerAfterMerge's engi branch already
	// refuses an invalid total, so this can never fail in practice, but a
	// write that answers for what it derives should not lean on that alone.
	if rep.ResultChanged && slices.Contains(rep.Held, state.GroupFlags) && engiValidTotal(probe.FlagsA, probe.FlagsB) {
		result.FlagsA, result.FlagsB = probe.FlagsA, probe.FlagsB
		result.Merge = rep
		result.GroupStamps = state.CloneGroupStamps(probe.GroupStamps)
		// Same rule as the branch above: the answer reports the merge's own
		// settled stamp, not the finish's own, older attempted one.
		result.ModifiedAt = probe.ModifiedAt
		return false
	}
	if !rep.Superseded() {
		probe = engiFinishProbe(prior, result)
		rep = mergeMatchWrite(prior, &probe, matchWriteForward, mergeCtx{comp: comp, knockout: knockout, holdAll: HoldReasonEngiAtomic})
	}
	result.Merge = rep
	result.WriteDoor = doorEngi
	return true
}

// backfillEngiResult copies the engine-derived identity from a recorded engi
// MatchResult (rec) onto the caller's result so the handler's SSE
// match_updated broadcast carries the winner. The engi score client submits
// only flag counts and status, never a winner, so without this the bracket
// card / scoreboard would show the match completed but with no winner
// highlight until the next background refetch. Winner/WinnerSide are set by
// engiWinnerSide; WinnerID is populated from SideAID/SideBID for both pool
// bouts and, since bc-brid, a stamped bracket bout (applyEngiToBracketMatch),
// matching the authoritative on-disk state either way. An unstamped bracket
// row (a bye, an unresolved feeder, or an unrepaired legacy row) still
// carries no WinnerID, exactly as it carries no SideAID/SideBID.
func backfillEngiResult(result, rec *state.MatchResult) {
	if result == nil || rec == nil {
		return
	}
	result.Winner = rec.Winner
	result.WinnerSide = rec.WinnerSide
	result.WinnerID = rec.WinnerID
	result.Status = rec.Status
}

// engiChangedGroups are the groups an engi finish changes: the flags and the
// verdict they decide.
var engiChangedGroups = []string{state.GroupResult, state.GroupFlags}

// engiStampable is what stampEngiChanges needs: *state.MatchResult and
// *state.BracketMatch both satisfy it.
type engiStampable interface {
	StampGroups(stamp int64, groups ...string)
	ApplyMergedGroupStamps(decided map[string]int64, groups ...string)
}

// stampEngiChanges stamps the groups an engi write changes (result and
// flags) on m. When groupStamps is set, engiFinishHeld has already ordered
// the write against every stored group (bc-mrgc fix): the merge is the ONE
// place that can decide a group's stamp must move BACK to the write's own,
// older time (S2's displaceNewerScoring, replacing a newer but invalid
// stored count with the finish's own scoreline and moving that count to the
// history), so the recorder applies exactly what it decided rather than
// re-deriving a stamp with StampGroups' never-lower guard, which cannot
// express a move backward and would leave the group stuck at the displaced
// count's later stamp. A writer the merge never saw (an unstamped direct
// caller, e.g. recordEngiMatchResult's test callers) has no decided stamps
// to apply, so it keeps the ordinary never-lower stamping.
func stampEngiChanges(m engiStampable, stamp int64, groupStamps map[string]int64) {
	if groupStamps != nil {
		m.ApplyMergedGroupStamps(groupStamps, engiChangedGroups...)
		return
	}
	m.StampGroups(stamp, engiChangedGroups...)
}

// recordEngiMatch is the shared record core. The store handle h abstracts the
// persistence layer: *state.Store satisfies state.StoreTx, so the same body
// runs against either the store itself (each call locks) or a live transaction
// (the caller's per-comp lock is already held) — see writeToPoolOrBracket for
// the handle convention. This used to inject two closures per persistence op;
// the handle carries both.
func (e *Engine) recordEngiMatch(
	h state.StoreTx,
	compID, matchID string,
	flagsA, flagsB int,
	correctionReason string,
	stamp int64,
	groupStamps map[string]int64,
	// skipDirectHistory is true for the one caller (RecordMatchResultWithIneligibilityTx,
	// via engiFinishHeld) that has already built a full merge report for this
	// write: that caller records the richer, per-group history itself
	// (recordWriteHistory), so this blunt "every group applied" entry would
	// only contradict it -- most visibly for bc-mrgc's S2 fix, where the
	// flags group did NOT apply (a newer stored count stood; the finish's
	// own flags are held in that caller's own history entry instead). The
	// other caller, recordEngiMatchResult (the engine-internal callers and
	// the test suite's direct writes), has no merge report at all, so it
	// still needs this function's own history entry and passes false.
	skipDirectHistory bool,
	opts ...ForceOptions,
) (*state.MatchResult, error) {
	fo := firstForceOptions(opts)
	force := fo.Force
	var reopened []ReopenedMatch
	// The picks the propagation takes from matches it re-seats without
	// reopening, for their own history lines (bracket_seat_audit.go).
	var clears []repPickClear
	if !engiValidTotal(flagsA, flagsB) {
		return nil, validationErrorf(
			"engi: flag total %d+%d=%d is invalid; total must be odd and in {1,3,5} (3- or 5-referee panel, no draw possible)",
			flagsA, flagsB, flagsA+flagsB,
		)
	}
	winnerSide := engiWinnerSide(flagsA, flagsB)
	// The engi finish is stamped with the client's stamp, when the operator
	// made it (bc-mrgc phase 3): the dispatch seam has already ordered it
	// against the stored groups (engiFinishHeld). Only a writer with no stamp
	// (the engine's own callers, a legacy client) takes the server's clock.
	if stamp <= 0 {
		stamp = serverNowMs()
	}

	// Try the pool stage first.
	var out *state.MatchResult
	err := e.withPoolMatch(h, compID, matchID, func(r *state.MatchResult) error {
		applyEngiToMatchResult(r, flagsA, flagsB, winnerSide, correctionReason)
		stampEngiChanges(r, stamp, groupStamps)
		cp := *r
		out = &cp
		return nil
	})
	if err == nil {
		if !skipDirectHistory {
			e.recordDirectHistory(h, compID, matchID, doorEngi, stamp, engiChangedGroups...)
		}
		return out, nil
	}
	if err != errMatchNotFound {
		return nil, err
	}

	// Fall through to the bracket stage (rounds + bronze).
	var result *state.MatchResult
	updateErr := h.UpdateBracket(compID, func(b *state.Bracket) error {
		for rIdx, round := range b.Rounds {
			for mIdx := range round {
				if b.Rounds[rIdx][mIdx].ID != matchID {
					continue
				}
				bm := &b.Rounds[rIdx][mIdx]
				if !bracketMatchPlayable(bm) {
					// PURE (bm already carries Number/DisplayRound): safe
					// inside UpdateBracket's mutate callback (bc-cse item 14).
					return validationErrorf("%s is not ready to score: a feeder pool or match has not finished", SentenceCase(MatchLabel(bracketMatchRef(bm))))
				}
				// bc-kcdg: engi is a knockout like any other, so a correction
				// here repaints the next round exactly as the kendo path's
				// does, and must answer the same way. This seam RETURNS before
				// writeToPoolOrBracket, so the guard on that path never sees an
				// engi write and engi corrections silently repainted a played
				// downstream match.
				//
				// guardOverrideDownstreamKnockoutCorrection, not the
				// MatchResult-shaped twin: engi decides its winner from the
				// flag count rather than from a submitted result, so the
				// override guard's bare-name input is the shape that fits.
				newWinner := bm.SideB
				if winnerSide == "A" {
					newWinner = bm.SideA
				}
				if err := guardOverrideDownstreamKnockoutCorrection(b, rIdx, mIdx, bm, newWinner, force); err != nil {
					return err
				}
				priorWinner, priorWinnerID := propagatedWinnerOf(b, rIdx, mIdx, bm)
				priorPicks := snapshotDownstreamRepPicks(b, rIdx, mIdx)
				before := repPickSnapshot(b, matchID)
				result = applyEngiToBracketMatch(bm, flagsA, flagsB, winnerSide, correctionReason)
				stampEngiChanges(bm, stamp, groupStamps)
				e.propagateBracketWinner(b, rIdx, mIdx)
				if force && winnerActuallyChanged(priorWinner, priorWinnerID, bm) {
					reopened = forceReopenDownstreamChain(b, rIdx, mIdx, bm.ID, priorPicks)
				}
				clears = repPickClears(before, b)
				return nil
			}
		}
		if b.ThirdPlaceMatch != nil && b.ThirdPlaceMatch.ID == matchID {
			bm := b.ThirdPlaceMatch
			if !bracketMatchPlayable(bm) {
				// PURE, same reasoning as the round branch above.
				return validationErrorf("%s is not ready to score: a feeder pool or match has not finished", SentenceCase(MatchLabel(bracketMatchRef(bm))))
			}
			result = applyEngiToBracketMatch(bm, flagsA, flagsB, winnerSide, correctionReason)
			stampEngiChanges(bm, stamp, groupStamps)
			// No propagation out of bronze.
			return nil
		}
		return notFoundErrorf("bracket match %s not found", matchID)
	})
	if updateErr != nil {
		return nil, updateErr
	}
	if !skipDirectHistory {
		e.recordDirectHistory(h, compID, matchID, doorEngi, stamp, engiChangedGroups...)
	}
	e.restoreForceReopened(h, compID, reopened)
	e.recordRepPickClears(h, compID, clears, reopened)
	if fo.Reopened != nil {
		*fo.Reopened = append(*fo.Reopened, reopened...)
	}
	return result, nil
}

// applyEngiToMatchResult writes a flag-decided result into a pool MatchResult.
// correctionReason is the operator audit note for overwrites; it is persisted
// only when non-empty, mirroring the kendo path's CorrectionReason semantics.
// Also sets WinnerID from SideAID/SideBID (when present) so same-name
// participants from different dojos remain distinguishable downstream (e.g.
// computeEngiStandings), mirroring the non-engi scoring path.
func applyEngiToMatchResult(r *state.MatchResult, flagsA, flagsB int, winnerSide, correctionReason string) {
	if winnerSide == "A" {
		r.Winner = r.SideA
		r.WinnerID = r.SideAID
	} else {
		r.Winner = r.SideB
		r.WinnerID = r.SideBID
	}
	r.WinnerSide = winnerSide
	r.FlagsA = flagsA
	r.FlagsB = flagsB
	r.Status = state.MatchStatusCompleted
	if correctionReason != "" {
		r.CorrectionReason = correctionReason
	}
}

// applyEngiToBracketMatch writes a flag-decided result into a BracketMatch and
// returns the equivalent MatchResult for the caller to echo / broadcast.
// correctionReason is persisted on the bracket match when non-empty.
//
// bm.WinnerID is stamped from bm.SideAID/SideBID (bc-brid), mirroring
// applyEngiToMatchResult's pool twin: an engi bracket bout's sides are
// stamped by the SAME generation/propagation writers a kendo bracket uses
// (this function is the one place that decides an engi bracket winner, so
// it is also the one place responsible for the id half), and
// propagateBracketWinner (called right after this by the caller) advances
// bm.WinnerID into the next round exactly like it does for a kendo match --
// without this stamp, that propagation would silently carry an empty id
// through the rest of the bracket even though the sides themselves are
// correctly identified.
func applyEngiToBracketMatch(bm *state.BracketMatch, flagsA, flagsB int, winnerSide, correctionReason string) *state.MatchResult {
	if winnerSide == "A" {
		bm.Winner = bm.SideA
		bm.WinnerID = bm.SideAID
	} else {
		bm.Winner = bm.SideB
		bm.WinnerID = bm.SideBID
	}
	bm.FlagsA = flagsA
	bm.FlagsB = flagsB
	bm.Status = state.MatchStatusCompleted
	if correctionReason != "" {
		bm.CorrectionReason = correctionReason
	}
	return &state.MatchResult{
		ID:               bm.ID,
		SideA:            bm.SideA,
		SideB:            bm.SideB,
		SideAID:          bm.SideAID,
		SideBID:          bm.SideBID,
		Winner:           bm.Winner,
		WinnerID:         bm.WinnerID,
		WinnerSide:       winnerSide,
		FlagsA:           flagsA,
		FlagsB:           flagsB,
		Status:           state.MatchStatusCompleted,
		Court:            bm.Court,
		ScheduledAt:      bm.ScheduledAt,
		CorrectionReason: correctionReason,
	}
}

// resolveWinnerSide reports which side of a match won, by participant id
// only (operator ruling bc-pnum). A match with no WinnerID resolves to no
// win at all, even when Winner/SideA/SideB carry names that would otherwise
// look like a match: "m.Winner == m.SideA" can be true for BOTH sides of a
// same-name pairing (legal when the dojos differ), so a name comparison
// could credit the loser. There is no such ambiguity by id.
//
// Both results can be false: an unfinished match, a draw, a winner naming
// neither side, or a row with no WinnerID at all (unresolvable by the
// operator ruling, not a fallback case). Callers treat that as "no win to
// award" rather than as an error.
func resolveWinnerSide(m state.MatchResult) (winnerIsA, winnerIsB bool) {
	if m.WinnerID == "" {
		return false, false
	}
	return m.WinnerID == m.SideAID, m.WinnerID == m.SideBID
}

// newStandingsIndex builds the standings lookup for a roster and returns it
// alongside the same pointers in roster order.
//
// Callers assemble their output from the returned slice (order), never from
// the map: a roster entry with no id is appended to order but never indexed
// into byKey at all (see registerStandingsPlayer), so ranging over the map's
// values would silently skip every id-less competitor.
func newStandingsIndex(players []domain.Player) (map[string]*state.PlayerStanding, []*state.PlayerStanding) {
	byKey := make(map[string]*state.PlayerStanding, len(players))
	order := make([]*state.PlayerStanding, 0, len(players))
	for _, p := range players {
		order = append(order, registerStandingsPlayer(byKey, p))
	}
	return byKey, order
}

// registerStandingsPlayer indexes a fresh *state.PlayerStanding for player
// into m under its participant id (when player.ID is non-empty), and
// returns the standing so the caller can keep populating it. A row is keyed
// by its participant id; a row without one resolves to nothing (operator
// ruling bc-pnum), so an id-less player is deliberately NOT inserted at
// all -- that player is still present in the `order` slice
// newStandingsIndex returns, so it still appears in the standings output,
// it just cannot be matched to a bare match side that carries no id.
func registerStandingsPlayer(m map[string]*state.PlayerStanding, player domain.Player) *state.PlayerStanding {
	st := &state.PlayerStanding{Player: player}
	if player.ID != "" {
		m[player.ID] = st
	}
	return st
}

// lookupStandingsPlayer resolves a match side's id to the
// *state.PlayerStanding registered by registerStandingsPlayer: a plain map
// index, no separate empty-id guard needed, since registerStandingsPlayer
// never inserts a "" key, so id == "" already misses like any other
// unregistered id.
func lookupStandingsPlayer(m map[string]*state.PlayerStanding, id string) *state.PlayerStanding {
	return m[id]
}

// engiScoreSummary renders the human-readable score cell for an engi
// standing. One format definition shared by the pool/league and Swiss engi
// standings so the two tables can never drift.
func engiScoreSummary(s *state.PlayerStanding) string {
	return fmt.Sprintf("W:%d Flags:%d", s.Wins, s.Flags)
}

// computeEngiStandings is the engi standings core, fully independent of the
// kendo computeStandingsFrom. It ranks each pool by (1) total Wins, then
// (2) total accumulated OWN-SIDE flags across every completed bout (the winner
// accrues their flags AND the loser accrues theirs, so a 3-2 bout adds +3 to
// the winner and +2 to the loser toward the tiebreaker).
//
// Works for BOTH pool and league formats because the dispatch seam in
// computeStandings sits above the pool/league split: a league competition
// stores all its bouts as pool matches under its single league pool, so the
// same per-pool aggregation applies.
// It takes the same poolStandingsLoader as computeStandingsFrom (it only calls
// LoadPools + LoadPoolMatches, never LoadCompetition), so both *state.Store and
// state.StoreTx satisfy it.
func (e *Engine) computeEngiStandings(loader poolStandingsLoader, compID string) (map[string][]state.PlayerStanding, error) {
	pools, err := loader.LoadPools(compID)
	if err != nil {
		return nil, err
	}
	results, err := loader.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}

	poolResults := make(map[string][]state.MatchResult)
	for _, r := range results {
		if pn, ok := poolNameFromMatchID(r.ID); ok {
			poolResults[pn] = append(poolResults[pn], r)
		}
	}

	allStandings := make(map[string][]state.PlayerStanding)
	for _, p := range pools {
		matches := poolResults[p.PoolName]

		playerStandings, order := newStandingsIndex(p.Players)

		for _, m := range matches {
			if m.Status != state.MatchStatusCompleted {
				continue
			}
			// Supplementary bouts (TB/DH) don't count toward engi standings.
			if IsTiebreakerMatchID(m.ID) || IsPoolDaihyosenMatchID(m.ID) {
				continue
			}
			sA := lookupStandingsPlayer(playerStandings, m.SideAID)
			sB := lookupStandingsPlayer(playerStandings, m.SideBID)
			if sA == nil || sB == nil {
				continue
			}
			// Winner by id only (operator ruling bc-pnum); see resolveWinnerSide.
			winnerIsA, winnerIsB := resolveWinnerSide(m)
			switch {
			case winnerIsA:
				sA.Wins++
			case winnerIsB:
				sB.Wins++
			}
			// Own-side flag accrual: winner AND loser both accumulate the flags
			// raised for their own side.
			sA.Flags += m.FlagsA
			sB.Flags += m.FlagsB
		}

		sorted := make([]state.PlayerStanding, 0, len(order))
		for _, s := range order {
			s.ScoreSummary = engiScoreSummary(s)
			sorted = append(sorted, *s)
		}

		// Stable sort: more Wins first, then more accumulated own-side Flags,
		// then by name so the order is deterministic for fully-tied
		// competitors. Points is left at its zero value: engi has no points
		// metric, so an honest 0 reaches the wire rather than a packed sort key.
		sort.SliceStable(sorted, func(i, j int) bool {
			if sorted[i].Wins != sorted[j].Wins {
				return sorted[i].Wins > sorted[j].Wins
			}
			if sorted[i].Flags != sorted[j].Flags {
				return sorted[i].Flags > sorted[j].Flags
			}
			return sorted[i].Player.Name < sorted[j].Player.Name
		})

		for i := range sorted {
			sorted[i].Rank = i + 1
		}
		allStandings[p.PoolName] = sorted
	}
	return allStandings, nil
}

// computeEngiSwissStandings is the engi twin of SwissStandings: the
// flag-scored standings core for a Swiss competition. It is the delegate the
// kendo SwissStandings branches to at its engi dispatch seam, so engi's
// flag ranking stays out of the kendo tally (engi.go hard-separation
// principle). It mirrors SwissStandings' cumulative-across-rounds structure
// (one flat group, byes are auto-wins, head-to-head is the final tiebreak
// before name) but ranks by (1) Wins then (2) accumulated OWN-SIDE flags,
// exactly like the pool/league computeEngiStandings.
//
// Identity is keyed via registerStandingsPlayer / lookupStandingsPlayer,
// by participant id ONLY (operator ruling bc-pnum), exactly like the
// kendo SwissStandings it twins. buildSwissMatches stamps SideAID/SideBID on
// every match it generates (mirroring pools.go), including engi Swiss
// matches, since GenerateSwissRound has no engi/kendo fork and the same
// generator produces both -- so a same-name-different-dojo pair in an engi
// Swiss field never collapses to one standings row, and a match with no
// side id simply resolves to nothing.
func (e *Engine) computeEngiSwissStandings(participants []domain.Player, matches []state.MatchResult) ([]state.PlayerStanding, error) {
	// order holds one *PlayerStanding per participant, in roster order: the
	// assembly loop below ranges over THIS, not over byKey's values, because
	// an id-less participant is never inserted into byKey at all (see
	// registerStandingsPlayer).
	byKey, order := newStandingsIndex(participants)

	headToHead := make(map[string]map[string]string) // winner key → opponent key → winner key
	for _, m := range matches {
		if _, ok := parseSwissMatchRound(m.ID); !ok {
			continue
		}
		// Bye: SideA wins, no flags accrued, no head-to-head.
		if m.SideB == "" {
			if sA := lookupStandingsPlayer(byKey, m.SideAID); sA != nil {
				sA.Wins++
			}
			continue
		}
		if m.Status != state.MatchStatusCompleted {
			continue
		}
		sA := lookupStandingsPlayer(byKey, m.SideAID)
		sB := lookupStandingsPlayer(byKey, m.SideBID)
		if sA == nil || sB == nil {
			continue
		}
		// Winner by id only (operator ruling bc-pnum); see resolveWinnerSide.
		winnerIsA, winnerIsB := resolveWinnerSide(m)
		keyA := sA.Player.ID
		keyB := sB.Player.ID
		switch {
		case winnerIsA:
			sA.Wins++
			recordHeadToHead(headToHead, keyA, keyB, keyA)
		case winnerIsB:
			sB.Wins++
			recordHeadToHead(headToHead, keyA, keyB, keyB)
		}
		// Own-side flag accrual: winner AND loser both accumulate the flags
		// raised for their own side.
		sA.Flags += m.FlagsA
		sB.Flags += m.FlagsB
	}

	standings := make([]state.PlayerStanding, 0, len(order))
	for _, s := range order {
		s.ScoreSummary = engiScoreSummary(s)
		standings = append(standings, *s)
	}
	sort.SliceStable(standings, func(i, j int) bool {
		a, b := standings[i], standings[j]
		if a.Wins != b.Wins {
			return a.Wins > b.Wins
		}
		if a.Flags != b.Flags {
			return a.Flags > b.Flags
		}
		// Head-to-head: if a beat b directly, a ranks higher.
		keyA := a.Player.ID
		keyB := b.Player.ID
		if winner, ok := lookupH2H(headToHead, keyA, keyB); ok {
			if winner == keyA {
				return true
			}
			if winner == keyB {
				return false
			}
		}
		return a.Player.Name < b.Player.Name
	})
	for i := range standings {
		standings[i].Rank = i + 1
	}
	return standings, nil
}
