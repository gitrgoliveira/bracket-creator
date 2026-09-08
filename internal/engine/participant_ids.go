package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// PoolMatchesMissingSideIDsMessage names pool-matches.csv rows that are
// missing a side id for a side that IS named, or that record a winner
// (a non-draw result) with no WinnerID, and states the consequence and
// remedy. Returns "" when no row is affected.
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
// This can only affect pool-matches rows written before id-only resolution
// went live, or a hand-edited file: every match this engine generates
// stamps SideAID/SideBID at creation and WinnerID at score time. Remedy:
// re-enter the result once the sides carry ids -- regenerating the draw
// (helper.PoolsMissingParticipantIDsMessage's remedy) is what actually
// assigns those ids to the roster in the first place.
func PoolMatchesMissingSideIDsMessage(matches []state.MatchResult) string {
	count := 0
	var labels []string
	for _, m := range matches {
		affected := (m.SideA != "" && m.SideAID == "") ||
			(m.SideB != "" && m.SideBID == "") ||
			(m.Winner != "" && m.WinnerID == "")
		if !affected {
			continue
		}
		count++
		if len(labels) < helper.MaxNamedRows {
			labels = append(labels, matchLabel(m))
		}
	}
	return helper.NamedLabelsMessage(helper.TruncatedLabels(count, labels, "match(es)"),
		"a side or winner has no id. They are not counted in standings; re-enter the result to assign a winner id, and regenerate the draw while it is still draw-ready to restore a missing side id.")
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
