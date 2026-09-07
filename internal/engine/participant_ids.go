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
			labels = append(labels, m.SideA+" vs "+m.SideB)
		}
	}
	return helper.NamedLabelsMessage(helper.TruncatedLabels(count, labels, "match(es)"),
		"a side or winner has no id. They are not counted in standings; re-enter the results once the sides have ids.")
}
