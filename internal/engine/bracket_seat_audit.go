package engine

import (
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The history is the record of a re-seat; the log line seatBracketSide prints is
// only its trace.
//
// A write that gives a knockout side another team takes the representative the
// old team's member held on that side with it (seatBracketSide, the one clearer).
// A downstream match the same write REOPENS names the group on its reopen line
// (ReopenedMatch.RepPicksCleared, restoreForceReopened). Every other match a
// bracket door re-seats -- the scheduled or requeued next round, the bronze, a
// round further on that a retraction unwinds into, a slot a requalification
// repaint paints -- changed outside any score write of its own, so it records
// its own line too (recordDirectHistory: "every direct writer appends its own,
// INSIDE the transaction that makes its write"), or a pick would vanish with
// nothing in the match's history to say when or why.
//
// The mechanism needs no change to the re-seating functions and so covers every
// route to a re-seat, the byes a winner passes through included: a door takes a
// snapshot of the bracket's picks before it mutates the bracket
// (repPickSnapshot), diffs it afterwards (repPickClears) while it still holds the
// bracket, and records the difference beside the place that already records its
// reopen lines (recordRepPickClears), in the same transaction. Only clears are
// diffed: nothing a door does inside its bracket callback sets a pick, and one a
// writer sets is that write's own change, which the merge's history line names.

// clearedPick is one side's representative pick that a door's mutation took
// out, with the stamp seatBracketSide gave the group when it did.
type clearedPick struct {
	Group string
	Stamp int64
}

// repPickClear is the picks one match lost to a door's mutation.
type repPickClear struct {
	MatchID string
	Cleared []clearedPick
}

// repPickSnapshot records, for every round match and the 3rd-place match, the
// representatives it holds on side A and side B, leaving out a match that holds
// none. own is the match the door itself writes: its pick groups are that
// write's to name (the merge's per-group line), never a re-seat's, so it is left
// out of the snapshot and so can never appear in the diff. "" for a door that
// writes no bracket match of its own.
func repPickSnapshot(b *state.Bracket, own string) map[string][2]string {
	out := map[string][2]string{}
	if b == nil {
		return out
	}
	note := func(m *state.BracketMatch) {
		if m.ID == own {
			return
		}
		if a, c := m.RepPicks(); a != "" || c != "" {
			out[m.ID] = [2]string{a, c}
		}
	}
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			note(&b.Rounds[ri][mi])
		}
	}
	if b.ThirdPlaceMatch != nil {
		note(b.ThirdPlaceMatch)
	}
	return out
}

// repPickClears lists, in bracket order, the picks of before that the bracket no
// longer holds: a side whose pick was non-empty and is now empty or has no
// bout row left to carry one. The stamp of each is read from the bracket now, so
// the recorder needs no second read. A nil before (a door that records nothing,
// e.g. the K3 rollback replaying a prior) yields nothing.
func repPickClears(before map[string][2]string, b *state.Bracket) []repPickClear {
	if len(before) == 0 || b == nil {
		return nil
	}
	var out []repPickClear
	diff := func(m *state.BracketMatch) {
		was, ok := before[m.ID]
		if !ok {
			return
		}
		a, c := m.RepPicks()
		now := [2]string{a, c}
		lost := repPickClear{MatchID: m.ID}
		for i, side := range [...]domain.MatchSide{domain.MatchSideA, domain.MatchSideB} {
			if was[i] != "" && now[i] == "" {
				g := state.RepPickGroup(side)
				lost.Cleared = append(lost.Cleared, clearedPick{Group: g, Stamp: m.GroupStamp(g)})
			}
		}
		if len(lost.Cleared) > 0 {
			out = append(out, lost)
		}
	}
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			diff(&b.Rounds[ri][mi])
		}
	}
	if b.ThirdPlaceMatch != nil {
		diff(b.ThirdPlaceMatch)
	}
	return out
}

// recordRepPickClears appends, for each match in clears, one history line with
// door doorReseat naming the pick groups that went (state.RepPickGroup), applied,
// stamped with the group's stamp after the mutation (the later of the two when a
// match lost both). A group a reopen line in reopened already names is skipped:
// that line is its record, and a second would claim the change twice. It is
// called with the SAME handle the bracket write used, beside restoreForceReopened,
// so the lines land with the write or not at all.
func (e *Engine) recordRepPickClears(h state.StoreTx, compID string, clears []repPickClear, reopened []ReopenedMatch) {
	for _, c := range clears {
		var groups []string
		var stamp int64
		for _, p := range c.Cleared {
			named := slices.ContainsFunc(reopened, func(r ReopenedMatch) bool {
				return r.ID == c.MatchID && slices.Contains(r.RepPicksCleared, p.Group)
			})
			if named {
				continue
			}
			groups = append(groups, p.Group)
			stamp = max(stamp, p.Stamp)
		}
		if len(groups) == 0 {
			continue
		}
		e.recordDirectHistory(h, compID, c.MatchID, doorReseat, stamp, groups...)
	}
}
