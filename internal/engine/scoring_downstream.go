package engine

import (
	"fmt"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// winnerActuallyChanged compares a bracket match's winner against what it held
// before the write, by id when both sides carry one and by name otherwise
// (bc-pnum: an id is authoritative when present, and the three id-less
// BracketMatch shapes are why the name arm survives).
//
// The force path needs this because force deliberately SKIPS the guard, so
// without it a confirmed write requeued the next round even when it stored the
// very same winner -- no displacement, nothing to unwind, a round cleared for
// nothing.
func winnerActuallyChanged(priorWinner, priorWinnerID string, bm *state.BracketMatch) bool {
	if priorWinnerID != "" && bm.WinnerID != "" {
		return priorWinnerID != bm.WinnerID
	}
	return priorWinner != bm.Winner
}

// forceReopenDownstreamChain reopens exactly what propagatedDownstream.played
// named, and nothing else. It asks that same method rather than re-deriving
// the target, so the refusal and the confirmation can never disagree about
// which matches are at stake.
//
// ONE HOP, but every match in it. Normally that is one match; a semifinal
// feeds two slots -- the final and the bronze -- and BOTH go on the one
// confirmation that named both. Splitting them into a dialog each was tried
// and is unimplementable: once the first confirmation applies, the winner no
// longer changes, so repeating the correction raises nothing and the second
// sibling would sit contradicting itself forever (verified against the running
// app: re-saving the same correction returned 200 with the final still showing
// the old winner). A bye the winner was passed through is not a hop: nobody
// fought it, so the hop is the first match past it. Rounds BEYOND that are not
// reopened here. One nobody has touched only stops showing the winner the
// reopened match had sent on (retractIntoUntouched); one already played is
// asked about in its own turn, when the re-fought result propagates into it.
//
// A reopen (reopenDisplacedBracketMatch), not requeueBracketMatch: the match was already played
// and stays where it is, reopened in place, so the queue is left alone
// (operator ruling 2026-09-19: "no changes in the queue necessary if the
// matches were already played"). It carries its own audit note rather than
// owing one -- see downstreamReopenReason.
//
// It leaves the representatives alone and names none as cleared: the caller,
// which holds the door's snapshot (repPickSnapshot), marks the reopened
// matches from the diff once every re-seat of the write is done
// (markRepPicksCleared).
func forceReopenDownstreamChain(bracket *state.Bracket, rIdx, mIdx int, correctedID string) []ReopenedMatch {
	d := propagatedDownstreamOf(bracket, rIdx, mIdx)
	var reopened []ReopenedMatch
	for _, m := range d.played() {
		ref := reopenDisplacedBracketMatch(m, downstreamReopenReason(correctedID))
		reopened = append(reopened, ref)
		// The reopened next-round match no longer has a winner, so what it
		// had propagated comes back out of a round nobody has touched, the
		// same retraction a pool correction's reopen applies
		// (applyRequalification). The bronze match has no downstream.
		if m == d.next {
			retractIntoUntouched(bracket, d.feed.R+1, d.feed.M/2)
		}
	}
	return reopened
}

// reopenDisplacedBracketMatch reopens ONE bracket match whose side a
// correction elsewhere has just displaced, and reports it for the caller's
// broadcast and eligibility restore. It is the per-match body of both doors
// that do this: forceReopenDownstreamChain (a knockout correction repainting
// the next round) and applyRequalification (a pool correction moving who
// qualified into it), so the two cannot drift on what "reopened because its
// competitor was replaced" leaves behind.
func reopenDisplacedBracketMatch(m *state.BracketMatch, reason string) ReopenedMatch {
	// Captured before the reopen clears it, for the eligibility
	// restore the caller runs once the bracket is saved
	// (restoreForceReopened). It cannot run here: this runs inside an
	// UpdateBracket callback, which on the bare store holds the
	// non-reentrant per-competition lock a status write would take again.
	priorDecision := m.Decision
	// A downstream reopen leaves the match SCHEDULED, where the kachinuki
	// reopen leaves it RUNNING, and the difference is who is standing at
	// the shiaijo. Kachinuki reopens the encounter the operator has this
	// second, to fight on; this one reopens a match that was played
	// earlier, with nobody on the court yet.
	//
	// Running is not merely inaccurate here, it is unusable: a running
	// match holds its court, and a semifinal reopens BOTH the final and
	// the 3rd-place match, which a draw runs on the same court. Two
	// running matches there deadlocked each other -- scoring either was
	// refused with court_busy naming the other, so neither could ever be
	// completed and the operator had no way out of the state their own
	// confirmation had created. Found by scoring a reopened bronze in the
	// browser; both statuses look identical until you try to finish one.
	//
	// The status is not the queue: the match stays exactly where it sat
	// (operator ruling 2026-09-19, "no changes in the queue necessary if
	// the matches were already played"), it simply is not claimed as in
	// progress until someone starts it.
	//
	// The fight stays: what was struck, its bouts and its engi flags (operator
	// ruling 2026-09-26: scores are never cleared, the operator removes a wrong
	// mark). The correction puts a different competitor in this match's slot
	// and discards only the verdict, exactly as a match still waiting in the
	// queue keeps its points when the name in it changes.
	reopenBracketMatchKeepingTheFight(m, reason, state.MatchStatusScheduled)
	if state.DaihyosenSubIndex(m.SubResults) >= 0 {
		clearDaihyosenVerdict(m.SubResults)
		// The representative bout's verdict went with the match's: it is
		// stamped with the reopen, so an older write cannot put it back.
		m.StampGroups(m.GroupStamp(state.GroupResult), state.BoutGroup(state.DaihyosenSubPosition))
	}
	ref := bracketMatchRef(m)
	ref.PriorDecision = priorDecision
	// The reopen leaves the picks as they were; what the re-seat of a side
	// does to them is judged by the door's repPickClears diff, which
	// markRepPicksCleared copies onto the reopen line.
	return ref
}

// clearDaihyosenVerdict drops the verdict a daihyosen row carries, its
// winner and a hantei mark, and keeps the points struck in it. The row
// settles the encounter on its own: the next write copies its winner to the
// match (deriveDaihyosenWinner), matching it through the row's own side
// names. The row's names follow the match's (seatBracketSide), but the winner
// it records was fought by the OLD pairing, so the one put in their place would
// inherit a win they never fought. Only the verdict goes, exactly as at match
// level.
func clearDaihyosenVerdict(subs []state.SubMatchResult) {
	for i := range subs {
		if subs[i].Position != state.DaihyosenSubPosition {
			continue
		}
		subs[i].Winner, subs[i].WinnerMemberID = "", ""
		subs[i].IpponsA, subs[i].IpponsB = struckIppons(subs[i].IpponsA), struckIppons(subs[i].IpponsB)
	}
}

// restoreForceReopened is the eligibility half of forceReopenDownstreamChain.
// A downstream match that a withdrawal decided and that a confirmed reopen or
// correction has just reopened no longer carries that withdrawal, so the
// competitor it barred is restored (restoreIfWithdrawalRemoved, the one
// statement of that rule) and the restored status is recorded on the
// ReopenedMatch for the handler to broadcast. Without it the reopened match
// kept its withdrawer ineligible, so it could never be started again.
//
// Every caller of forceReopenDownstreamChain runs this AFTER its bracket write
// has returned, never inside the UpdateBracket callback (see the capture in
// forceReopenDownstreamChain), with the same handle it wrote through.
func (e *Engine) restoreForceReopened(h state.StoreTx, compID string, reopened []ReopenedMatch) {
	for i := range reopened {
		reopened[i].Restored = e.restoreIfWithdrawalRemoved(h, compID, reopened[i].ID, reopened[i].PriorDecision, "", nil)
		// The reopen changed the match outside the merge, so it records its
		// own history entry (bc-mrgc).
		groups := reopenedBracketGroups
		if len(reopened[i].RepPicksCleared) > 0 {
			// A copy: reopenedBracketGroups is shared.
			groups = append(append([]string(nil), groups...), reopened[i].RepPicksCleared...)
		}
		e.recordDirectHistory(h, compID, reopened[i].ID, doorDownstreamReopen, serverNowMs(), groups...)
	}
}

// downstreamReopenReason is the audit note a downstream match carries when a
// correction upstream reopened it. It describes THIS match's own event -- it
// was reopened, and why -- rather than repeating the operator's note about the
// match they corrected, which would read as someone else's history on this
// match's trail.
//
// The system knows why this one was reopened, so it records that itself rather
// than leaving the match's trail without an explanation of something it did.
func downstreamReopenReason(correctedID string) string {
	return fmt.Sprintf("reopened: the result of match %s was corrected", correctedID)
}

// bracketWinnerChanged reports whether result's resolved winner differs from
// the winner bm currently stands behind (priorName/priorID, read before this
// write mutates bm). It
// backfills result.SideA/SideB and result.SideAID/SideBID from bm and
// resolves result.WinnerID via resolveWinnerIDFromSides first -- the same
// backfill+resolve applyBracketMatchResult performs later, called here early
// so the comparison and the eventual write agree on the same id; the later
// call becomes a no-op once WinnerID is set.
//
// bc-cse finding 3: the NAME backfill (reconcileSides) must run BEFORE the id
// backfill and resolveWinnerIDFromSides, exactly as applyBracketMatchResult
// orders it (reconcileSides at the top, the SideAID/SideBID+resolve block
// afterwards) -- not after, and not omitted. resolveWinnerIDFromSides' own
// name-fallback (result.Winner == result.SideA/SideB) depends on
// result.SideA/SideB already reflecting the stored pairing: a payload that
// names a winner but omits side names (result.SideA/SideB left "" by the
// client) would otherwise fail that fallback silently and fall through to
// the ippon-count guess, which resolves to whichever side scored more
// regardless of which competitor the payload actually named -- the wrong
// winner id on a payload that named the right winner. reconcileSides'
// reported mismatch is ignored here (the actual write's own reconcileSides
// call rejects a genuine disagreement); this call's only job is the
// backfill side effect, run under the exact same stored pairing
// applyBracketMatchResult itself is about to write against.
//
// The prior winner is the caller's propagatedWinnerOf, not bm's bare stored
// winner, so re-entering a reopened match is judged against who it still
// feeds downstream. Comparison is BY ID when that prior carries one (a
// resolved competitor, bc-brid); it falls back to bare-name comparison only for the
// three legitimate id-less BracketMatch shapes documented on
// BracketMatch.SideAID (a bye, an unresolved "Winner of ..." feeder, or an
// unrepaired legacy row) -- see CLAUDE.md's bc-pnum id-only-when-present
// ruling.
func bracketWinnerChanged(bm *state.BracketMatch, priorName, priorID string, result *state.MatchResult, policy matchWritePolicy) (bool, error) {
	reconcileSides(result, storedSides{A: bm.SideA, B: bm.SideB, AID: bm.SideAID, BID: bm.SideBID})
	if result.SideAID == "" {
		result.SideAID = bm.SideAID
	}
	if result.SideBID == "" {
		result.SideBID = bm.SideBID
	}
	// Restore a stored daihyosen verdict BEFORE reading the winner, exactly as
	// applyBracketMatchResult does before persisting it. A team knockout decided
	// on the rep bout is re-scored verdict-silent by design (buildPatch omits an
	// untouched daihyosen row), so the incoming result arrives winner-less and a
	// comparison made here would read "the winner changed" for a write that
	// stores the very same winner: the operator was asked to clear the next
	// round for a correction that changes nothing.
	if policy == matchWriteForward {
		preserveDaihyosenOutcome(bm.SubResults, result)
	}
	if err := resolveWinnerIDFromSides(result, policy); err != nil {
		return false, err
	}
	return winnerDiffers(result.Winner, result.WinnerID, priorName, priorID), nil
}

// winnerDiffers is the knockout-correction doors' one test of whether a new
// winner differs from the one already propagated: by id when the prior carries
// one, by name only for the id-less BracketMatch shapes (bc-brid).
func winnerDiffers(newName, newID, priorName, priorID string) bool {
	if priorID != "" {
		return newID != priorID
	}
	return newName != priorName
}

// propagatedWinnerOf is the winner a bracket match's result currently stands
// behind DOWNSTREAM: its own stored winner, or, when a reopen cleared that
// (reopenBracketMatch), the competitor still sitting in the next-round slot it
// fed. The three knockout-correction doors (score, override, engi) compare a
// new winner against this, never against the bare stored winner.
//
// A reopened match keeps its old winner in the next round whenever that round
// had already been played (the reopen reaches one hop and no further), so
// comparing against the cleared "" read every re-entry as a winner change:
// fighting the match again and entering the SAME winner still warned that the
// next round would be reopened, although nobody in it moves. A match that has
// never been completed feeds nothing yet (its slot holds a "Winner of ..."
// placeholder), so it still compares against "" exactly as before.
func propagatedWinnerOf(bracket *state.Bracket, rIdx, mIdx int, bm *state.BracketMatch) (name, id string) {
	if bm.Winner != "" || bm.WinnerID != "" {
		return bm.Winner, bm.WinnerID
	}
	_, next := downstreamTargets(bracket, rIdx, mIdx)
	if next == nil {
		return "", ""
	}
	slot, slotID := next.SideB, next.SideBID
	if feedsSide(mIdx) == domain.MatchSideA {
		slot, slotID = next.SideA, next.SideAID
	}
	if isUnresolvedBracketSide(slot) {
		return "", ""
	}
	return slot, slotID
}

// newDownstreamKnockoutPlayedError builds the refusal for every blocking match
// the correction would displace someone from.
//
// Usually one. A SEMIFINAL is the exception: it feeds the final AND the bronze
// match, and both can be closed, so both are named and both are cleared on one
// confirmation. That pairing has to be handled together, not one dialog each:
// the second dialog would never arrive. Once the first confirmation applies,
// the winner no longer changes, so a repeat of the same correction is not a
// correction at all and raises nothing -- leaving the other sibling displaying
// a competitor its own result contradicts, permanently. Verified in the
// browser before this was written.
//
// Deeper rounds are NOT bundled in: each is reached by its own write, when the
// re-fought result propagates, and that write is a genuine winner change which
// raises this again.
func newDownstreamKnockoutPlayedError(bm *state.BracketMatch, blocking []*state.BracketMatch, mIdx int) *DownstreamKnockoutPlayedError {
	blocked := make([]ReopenedMatch, 0, len(blocking))
	for _, b := range blocking {
		blocked = append(blocked, bracketMatchRef(b))
	}
	return &DownstreamKnockoutPlayedError{
		MatchID:         bm.ID,
		Label:           MatchLabel(bracketMatchRef(bm)),
		BlockingMatchID: blocked[0].ID,
		Blocking:        blocked,
		Displaced:       displacedCompetitor(bm, blocking[0], mIdx),
	}
}

// displacedCompetitor names the competitor the correction would knock out of
// the blocking match: the one SITTING IN THE SLOT this match feeds, which is
// the name the operator is looking at on the board.
//
// Not simply the corrected match's stored winner, which is empty exactly when
// the operator needs the name most. Walking the bracket forward one round at a
// time, the second match they correct has already been requeued by the first
// confirmation, so its winner was cleared: the dialog fell back to "The
// competitor currently recorded as advancing" in the ORDINARY path, not in
// some edge case. Seen in the browser during UAT.
//
// The slot rule mirrors propagateBracketWinner's: even slots feed SideA, odd
// slots feed SideB, for the next-round match and for the bronze match alike.
// Falls back to the stored winner when the slot holds an unresolved feeder
// placeholder rather than a competitor.
func displacedCompetitor(bm, blocking *state.BracketMatch, mIdx int) string {
	slot := blocking.SideB
	if feedsSide(mIdx) == domain.MatchSideA {
		slot = blocking.SideA
	}
	if slot != "" && !helper.IsWinnerOfPlaceholder(slot) {
		return slot
	}
	return bm.Winner
}

// effectiveBracketWriteStatus is the status a bracket write actually LANDS
// with, which is not always the one the payload carries: an omitted status
// means Completed, for older clients that never sent the field.
//
// It exists because the writer and the guard have to read that rule the same
// way, and did not. applyBracketMatchResult defaulted internally while
// guardDownstreamKnockoutCorrection compared `result.Status` raw, so a
// status-omitted completing write -- which mobileapp.validateMatchResult
// accepts, its status check being gated on `r.Status != ""`, and which the
// bulk-score path sends -- completed the match, propagated its winner and
// repainted an already-played next round with no refusal at all. A default
// that lives inside one of two readers is a fork in the rule; this is the one
// owner, and a new reader of "is this write completing the match?" calls it.
func effectiveBracketWriteStatus(result *state.MatchResult) state.MatchStatus {
	if result.Status == "" {
		return state.MatchStatusCompleted
	}
	return result.Status
}

// guardDownstreamKnockoutCorrection is bc-kcdg's refusal rule. It fires only
// for a forward-policy write that would COMPLETE the match (a "running"
// live-status update never propagates, see applyBracketResultIn) AND would
// CHANGE the already-propagated winner (bracketWinnerChanged); a plain
// re-score that keeps the same winner, or a first-ever completion (nothing
// downstream could carry a real result yet, since bracketMatchPlayable
// requires this match's own feeders, and therefore this match itself for
// its own downstream, to already be resolved), passes straight through. A
// matchWriteRestore (the K3 rollback replaying a trusted snapshot) is never
// refused, via the policy check below, matching every other bracket-write
// guard's restore exemption.
//
// A changed winner that reaches a later match being FOUGHT is refused first
// (runningDownstreamRefusal), whatever force says: force confirms reopening a
// played match, never repainting one in progress (operator decision
// 2026-09-27). Only then does force skip the played refusal.
func guardDownstreamKnockoutCorrection(bracket *state.Bracket, rIdx, mIdx int, bm *state.BracketMatch, result *state.MatchResult, policy matchWritePolicy, force bool) error {
	if policy != matchWriteForward || effectiveBracketWriteStatus(result) != state.MatchStatusCompleted {
		return nil
	}
	priorName, priorID := propagatedWinnerOf(bracket, rIdx, mIdx, bm)
	changed, err := bracketWinnerChanged(bm, priorName, priorID, result, policy)
	if err != nil {
		return err
	}
	if !changed {
		return nil
	}
	return downstreamCorrectionRefusal(bracket, rIdx, mIdx, bm, force)
}

// downstreamCorrectionRefusal is the refusal both correction guards end on,
// once they know the winner changed: a running later match is refused whatever
// force says, and force skips only the played refusal.
func downstreamCorrectionRefusal(bracket *state.Bracket, rIdx, mIdx int, m *state.BracketMatch, force bool) error {
	d := propagatedDownstreamOf(bracket, rIdx, mIdx)
	if err := runningDownstreamRefusal(m.ID, d.running()); err != nil {
		return err
	}
	if force {
		return nil
	}
	blocking := d.played()
	if len(blocking) == 0 {
		return nil
	}
	return newDownstreamKnockoutPlayedError(m, blocking, d.displacedSlot(blocking, mIdx))
}

// guardBareNameDownstreamKnockoutCorrection applies the bc-kcdg refusal
// rule adapted to a bare-name input (e.g. engi.go's dispatch seam, which
// decides its winner from flag counts rather than a submitted result struct).
// Like its twin it refuses a running later match first, whatever force says,
// and force skips only the played refusal.
func guardBareNameDownstreamKnockoutCorrection(bracket *state.Bracket, rIdx, mIdx int, m *state.BracketMatch, winnerName string, force bool) error {
	var newWinnerID string
	aMatches := winnerName != "" && winnerName == m.SideA
	bMatches := winnerName != "" && winnerName == m.SideB
	switch {
	case aMatches && !bMatches:
		newWinnerID = m.SideAID
	case bMatches && !aMatches:
		newWinnerID = m.SideBID
	}
	priorName, priorID := propagatedWinnerOf(bracket, rIdx, mIdx, m)
	if !winnerDiffers(winnerName, newWinnerID, priorName, priorID) {
		return nil
	}
	return downstreamCorrectionRefusal(bracket, rIdx, mIdx, m, force)
}
