package engine

import (
	"log"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// seatBracketSide is the ONE writer of a knockout match's side name and id
// once the match exists: every re-seat of a side after the draw (a winner
// propagated into the next round, a semifinal loser into the 3rd-place match,
// a slot cleared back to its "Winner of ..." placeholder, a pool qualifier
// painted over a placeholder) goes through it. It owns the one consequence a
// plain assignment misses: the representative a team match's rep bout holds
// for a side is a member of the team that was seated there
// (SubMatchResult.SideAMemberID/SideBMemberID, the state.GroupRepPicks
// group), so a side given ANOTHER team takes its pick away.
//
//   - The id is unchanged (a rename, or a correction that stores the winner
//     already recorded and so re-propagates the same team): the name is written
//     and nothing else, the pick stays.
//   - The id changes (team X to team Y, to "" when the slot goes back to a
//     placeholder, or from "" when a placeholder is resolved): name and id are
//     written, and a pick held for THAT side is cleared. The other side's pick
//     is never touched. The rule is id-based, with no name fallback: an id-less
//     side cannot carry a pick (the picker's roster and the member mint both
//     need the team id).
//   - No pick on that side, or no rep bout row at all: nothing else moves, no
//     stamp, no ModifiedAt, so the draw-time callers produce exactly the
//     bracket they always did.
//
// The cleared pick is dated max(now, its previous stamp + 1, the match's
// ModifiedAt) through StampGroups, which never lowers: strictly above the date
// the pick held, so a replay of the write that made it is kept in the match's
// history instead of seating the old team's member on the new team's side, and
// never below the match. It is NOT the stamp of the correction that caused the
// re-seat: that is a client stamp made against a different match, and a pick
// written after the correction but before it arrived would keep a stamp at or
// above it.
//
// It runs inside UpdateBracket callbacks, which hold no store handle, so it
// writes the match in place and records no history line of its own (a played
// downstream match gets one for its reopen, which names the group
// (markRepPicksCleared); a scheduled one is logged here).
func seatBracketSide(bm *state.BracketMatch, side domain.MatchSide, name, id string) {
	var nameField, idField *string
	switch side {
	case domain.MatchSideA:
		nameField, idField = &bm.SideA, &bm.SideAID
	case domain.MatchSideB:
		nameField, idField = &bm.SideB, &bm.SideBID
	default:
		log.Printf("engine: BUG: bracket match %s: seatBracketSide called with side %q; nothing written", bm.ID, side)
		return
	}
	idChanged := *idField != id
	*nameField = name
	if !idChanged {
		return
	}
	*idField = id

	row := state.DaihyosenSubIndex(bm.SubResults)
	if row < 0 {
		return
	}
	pickA, pickB := bm.RepPicks()
	held := pickA
	if side == domain.MatchSideB {
		held = pickB
	}
	if held == "" {
		return
	}
	if side == domain.MatchSideA {
		pickA = ""
	} else {
		pickB = ""
	}
	bm.SetRepPicks(pickA, pickB)
	// The bout's winner id is derived from the picks and the winner's name, so
	// it follows the pick that went.
	bm.SubResults[row].ReconcileWinnerMemberID()
	log.Printf("engine: bracket match %s: side %s was given another team, so its representative pick %q is cleared", bm.ID, side, held)
	bm.StampGroups(max(serverNowMs(), bm.GroupStamp(state.GroupRepPicks)+1, bm.ModifiedAt), state.GroupRepPicks)
}

// feedsSide is the side of the next match (and of the 3rd-place match, for a
// semifinal loser) that the match at index mIdx of its round feeds: the
// round's even matches feed side A, the odd ones side B.
func feedsSide(mIdx int) domain.MatchSide {
	if mIdx%2 == 0 {
		return domain.MatchSideA
	}
	return domain.MatchSideB
}

// downstreamRepPicks is the representatives held, when it was taken, by each
// match a winner feeds that holds any (the next match past any byes, and the
// 3rd-place match), by match id.
type downstreamRepPicks map[string][2]string

// snapshotDownstreamRepPicks takes it for the matches the winner of
// bracket.Rounds[rIdx][mIdx] is propagated into. A write that propagates takes
// it BEFORE propagateBracketWinner, so forceReopenDownstreamChain can tell
// which of the matches it reopens lost a pick to the re-seat.
func snapshotDownstreamRepPicks(bracket *state.Bracket, rIdx, mIdx int) downstreamRepPicks {
	d := propagatedDownstreamOf(bracket, rIdx, mIdx)
	out := downstreamRepPicks{}
	for _, m := range []*state.BracketMatch{d.next, d.bronze} {
		if m == nil {
			continue
		}
		if a, b := m.RepPicks(); a != "" || b != "" {
			out[m.ID] = [2]string{a, b}
		}
	}
	return out
}

// markRepPicksCleared sets RepPickCleared on every reopened match that no
// longer holds a pick it held when its prior picks were captured (at its
// reopen, or from a snapshotDownstreamRepPicks taken before the re-seat). Call
// it once the callback's re-seats are done: the only writer of a pick inside
// these callbacks is seatBracketSide, which only clears.
func markRepPicksCleared(bracket *state.Bracket, reopened []ReopenedMatch) {
	for i := range reopened {
		m := bracket.MatchByID(reopened[i].ID)
		if m == nil {
			continue
		}
		a, b := m.RepPicks()
		if (reopened[i].priorPickA != "" && a != reopened[i].priorPickA) ||
			(reopened[i].priorPickB != "" && b != reopened[i].priorPickB) {
			reopened[i].RepPickCleared = true
		}
	}
}
