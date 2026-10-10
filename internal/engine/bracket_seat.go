package engine

import (
	"log"
	"slices"

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
// (SubMatchResult.SideAMemberID/SideBMemberID, the state.GroupRepPickA and
// state.GroupRepPickB groups), so a side given ANOTHER team takes its pick away.
//
// INVARIANT: after every call -- a winner propagated, a slot cleared back to
// "" or a "Winner of ..." placeholder, a pool qualifier painted, a rename
// re-propagated -- the representative bout row's SideA/SideB for THIS side
// EQUALS the match's SideA/SideB for that side, whatever the string is (a
// placeholder or empty included: one rule, no special case). The hantei mark
// is placed on the winner's side by comparing the winner against that row's
// names (placeHt), so a row still naming the team that was taken out would
// attribute the verdict to nobody. The name is written on EVERY call, before
// anything is judged, so it follows a rename as well as a re-seat; it dates
// nothing on its own (no group stamp, no ModifiedAt: a name write is not a
// change of the bout, which is what lets a draw-time caller produce exactly
// the bracket it always did). A match with no row has nothing to write.
//
// Whether the side was given ANOTHER team is judged by id when both the side as
// stored and the incoming one carry an id, and by name otherwise
// (seatedAnotherTeam):
//
//   - Both ids set: another team exactly when they differ. A rename (the same
//     id under a new name), or a correction that stores the winner already
//     recorded and so re-propagates the same team, is not one: the name is
//     written and the pick stays.
//   - Either id empty: another team exactly when the names differ. That covers
//     a slot going back to a placeholder or to "" (the name differs from any
//     team's), a placeholder resolved to a team, and legacy id-less data,
//     where the client resolves an id-less side's team by name and so a pick
//     CAN sit on it. The same name with an id arriving (the side that carried
//     only its team's name gaining the team's id) is the same team, and so is
//     a writer that does not know the id (an incoming "" beside the name
//     already seated): neither clears the pick, and the stored id is never
//     thrown away for a writer that did not know it.
//
// This mirrors the client's rule (admin_scoring_team.jsx: a side is given
// another team only when BOTH its lookup key and the team it resolves to
// change; "a side that carried only its team's name gaining that team's id is
// not one") and the carve-out BracketMatch has in CLAUDE.md (resolve by id when
// present, fall back to the name only for an empty slot, an unresolved feeder
// or a legacy row a repair has not reached).
//
// When the side was given another team, name and id are written, and a pick
// held for THAT side is cleared. The other side's pick is never touched. With
// no pick on that side, or no rep bout row at all, nothing else moves.
//
// The cleared pick is dated max(now, its previous stamp + 1, the match's
// ModifiedAt) through StampGroups, which never lowers, and ONLY that side's
// group is dated: the other side's pick keeps its date, so a pick made for it
// on another device is still ordered by its own write. The date is strictly
// above the date the pick held, so a replay of the write that made it is kept
// in the match's history instead of seating the old team's member on the new
// team's side, and never below the match. It is NOT the stamp of the correction
// that caused the re-seat: that is a client stamp made against a different
// match, and a pick written after the correction but before it arrived would
// keep a stamp at or above it.
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
	another := seatedAnotherTeam(*nameField, *idField, name, id)
	*nameField = name
	// An incoming "" beside the name already seated is a writer that did not
	// know the id, not a removal: the id a repair resolved stays.
	if id != "" || another {
		*idField = id
	}

	row := state.DaihyosenSubIndex(bm.SubResults)
	if row < 0 {
		return
	}
	// The row's name follows the match's on every call, a rename included, and
	// dates nothing (the invariant in the doc comment).
	nameRepBoutRow(bm, row, side, name)
	if !another {
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
	bm.SetRepPick(side, "")
	// The bout's winner id is derived from the picks and the winner's name, so
	// it follows the pick that went.
	bm.SubResults[row].ReconcileWinnerMemberID()
	log.Printf("engine: bracket match %s: side %s was given another team, so its representative pick %q is cleared", bm.ID, side, held)
	g := state.RepPickGroup(side)
	bm.StampGroups(max(serverNowMs(), bm.GroupStamp(g)+1, bm.ModifiedAt), g)
}

// seatedAnotherTeam reports whether re-seating a side that is stored as
// (storedName, storedID) to (name, id) gives it another team: by id when both
// carry one, by name otherwise (the rule in seatBracketSide's doc comment).
func seatedAnotherTeam(storedName, storedID, name, id string) bool {
	if storedID != "" && id != "" {
		return storedID != id
	}
	return storedName != name
}

// nameRepBoutRow writes name as side's name on the rep bout row at index row.
// The list is replaced, never edited in place, so a row another copy of the
// match shares is never written through (state.withRepPick does the same for a
// pick); a name the row already has writes nothing and copies nothing.
func nameRepBoutRow(bm *state.BracketMatch, row int, side domain.MatchSide, name string) {
	field := func(r *state.SubMatchResult) *string {
		if side == domain.MatchSideA {
			return &r.SideA
		}
		return &r.SideB
	}
	if *field(&bm.SubResults[row]) == name {
		return
	}
	subs := slices.Clone(bm.SubResults)
	*field(&subs[row]) = name
	bm.SubResults = subs
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

// markRepPicksCleared records, on every reopened match that no longer holds a
// pick it held when its prior picks were captured (at its reopen, or from a
// snapshotDownstreamRepPicks taken before the re-seat), the group of each side
// whose pick went (RepPicksCleared). Call it once the callback's re-seats are
// done: the only writer of a pick inside these callbacks is seatBracketSide,
// which only clears.
func markRepPicksCleared(bracket *state.Bracket, reopened []ReopenedMatch) {
	for i := range reopened {
		m := bracket.MatchByID(reopened[i].ID)
		if m == nil {
			continue
		}
		a, b := m.RepPicks()
		r := &reopened[i]
		if r.priorPickA != "" && a != r.priorPickA {
			r.RepPicksCleared = appendUnique(r.RepPicksCleared, state.RepPickGroup(domain.MatchSideA))
		}
		if r.priorPickB != "" && b != r.priorPickB {
			r.RepPicksCleared = appendUnique(r.RepPicksCleared, state.RepPickGroup(domain.MatchSideB))
		}
	}
}

// appendUnique appends s to list unless it is already there. markRepPicksCleared
// can judge the same reopened match twice (the reopen door marks the chain
// forceReopenDownstreamChain already marked, after its retraction), and a group
// must be named once.
func appendUnique(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}
