package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// PoolMatchesMissingSideIDsMessage names pool-matches.csv rows that are
// missing a side id for a side that IS named, or that record a winner
// (a non-draw result) with no WinnerID, and states the consequence and the
// residual remedy. Returns "" when no row is affected.
//
// Mirrors helper.MissingParticipantIDsMessage /
// helper.PoolsMissingParticipantIDsMessage's role for the third and last
// on-disk record that carries an id field a side/winner is resolved from: a
// pool-matches.csv row (state.MatchResult) carries SideAID/SideBID/
// WinnerID, so every standings/scoring/eligibility consumer resolves a side
// by id only (operator ruling bc-pnum). A row missing the id for a side
// that IS named (SideA/SideB non-empty) contributes NOTHING to that side's
// record; a row recording a winner (Winner non-empty, i.e. not a hikiwake
// draw) with no WinnerID is counted as no one's win. This lives in
// internal/engine rather than internal/helper because it needs
// state.MatchResult, and internal/helper must not import internal/state
// (see CLAUDE.md's layering note); internal/engine already imports both.
//
// Names the affected rows as "SideA vs SideB" labels, via the SAME
// helper.NamedLabelsMessage / helper.TruncatedLabels composer the two
// helper notices use (count + first three, "match(es)" as this notice's own
// noun), so the naming/truncation shape cannot drift between the three.
// matchLabel composes defensively: a blank side is simply omitted rather
// than leaving a dangling "Alice vs " or " vs " in the notice, and a row
// with neither side named (both blank, only reachable via a hand-edited
// pool-matches.csv naming a Winner with no SideA/SideB at all) falls back
// to the match's own ID so the row is still identifiable.
//
// A legacy pool-matches.csv row predating these columns is now repaired
// automatically at load time (state.upgradePoolMatchSideIDsLocked resolves
// a named side against the roster when its name is unique there, and
// derives WinnerID from the row's own resolved side, never the roster).
// This message therefore only ever names the residue that repair could not
// resolve -- most often two or more competitors sharing that exact name (an
// individual competition, since team names must stay unique), which is not
// a bug to fix here, the operator ruling above is precisely that an
// ambiguous name resolves to nothing rather than a guess -- but this
// function only knows WHICH rows are still affected, not why any one of
// them is, so the returned message states the rows, the consequence, and
// the remedy, and leaves the cause unstated. A hand-edited file can produce
// the same residue.
func PoolMatchesMissingSideIDsMessage(matches []state.MatchResult) string {
	count := 0
	var labels []string
	for _, m := range matches {
		if !m.MissingSideOrWinnerID() {
			continue
		}
		count++
		if len(labels) < helper.MaxNamedRows {
			labels = append(labels, matchLabel(m))
		}
	}
	return helper.NamedLabelsMessage(helper.TruncatedLabels(count, labels, "match(es)"),
		"a side or winner has no id and could not be resolved automatically. They are not counted in standings; re-enter the result to assign a winner id, and regenerate the draw while it is still draw-ready to restore a missing side id.")
}

// matchLabel names a pool-matches.csv row for a data-issues notice: "SideA
// vs SideB" when both sides are present, just the one side that is present
// when the other is blank (a bye, or a row with the id issue on only one
// named side), or the match's own ID when neither side is named at all.
func matchLabel(m state.MatchResult) string {
	switch {
	case m.SideA != "" && m.SideB != "":
		return m.SideA + " vs " + m.SideB
	case m.SideA != "":
		return m.SideA
	case m.SideB != "":
		return m.SideB
	default:
		return m.ID
	}
}
