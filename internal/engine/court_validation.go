package engine

import (
	"fmt"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// SuggestedMaxCourts returns the recommended maximum number of courts
// for a single-pool competition with numPlayers players.
// Formula: floor(numPlayers/2) - 1, minimum 1.
// At this count, every player gets at least one rest slot between fights.
func SuggestedMaxCourts(numPlayers int) int {
	return max(1, numPlayers/2-1)
}

// ValidateCourtCount checks if numCourts is valid for a single-pool
// competition with numPlayers players.
//
// Returns error if numCourts > floor(numPlayers/2): courts would sit
// idle because there are not enough players to fill a round.
//
// The warning case (numCourts == floor(N/2), no rest between fights) is
// handled exclusively by the frontend, see admin_competition.jsx.
func ValidateCourtCount(numPlayers, numCourts int) error {
	hardCap := max(1, numPlayers/2)
	if numCourts > hardCap {
		return fmt.Errorf(
			"too many courts: %d courts for %d players exceeds maximum of %d (floor(N/2)); extra courts would sit idle",
			numCourts, numPlayers, hardCap,
		)
	}
	return nil
}

// CompetitionDrawsBracket reports whether a competition's DRAW builds a
// knockout bracket, which is the scope of the shiaijo-count rule: the rule
// exists because bracket regions pair up court by court, so it only binds
// where there are bracket regions.
//
// It mirrors the format switch in runDrawPipeline exactly. League and Swiss
// produce pools / rounds and never a bracket; mixed builds a preview
// bracket after its pools; every other value, including "playoffs" and a
// legacy record carrying no format at all, falls to that switch's default
// branch and generates a standalone playoffs bracket.
//
// Deliberately NOT state.Competition.IsPlayoffEnabled: that predicate asks
// whether the UI should offer playoff affordances and answers false for an
// unset format, whereas the draw pipeline's default branch DOES build a
// playoffs bracket for one. This check has to follow the pipeline, not the
// UI. Mirrored client-side as formatDrawsBracket in
// web-mobile/js/admin_helpers.jsx.
func CompetitionDrawsBracket(format string) bool {
	switch format {
	case state.CompFormatLeague, state.CompFormatSwiss:
		return false
	default:
		return true
	}
}

// ValidateCourtPairing is the engine-side entry point for the shiaijo-count
// rule: a competition runs on 1 shiaijo or an even number, never on an odd
// number greater than 1. The rule itself (and its message) lives in
// helper.ValidateCourtPairing so the CLI, the HTTP API, the engine and the
// operator UI all reject exactly the same allocations.
//
// Unlike its sibling ValidateCourtCount above, which applies only to a
// SINGLE-pool competition (the idle-court cap depends on the roster size),
// this check applies to every competition that draws a bracket
// (CompetitionDrawsBracket) and is enforced by runDrawPipeline, the one
// path both GenerateDraw and StartCompetition take to build a draw. That
// placement is deliberate:
//
//   - it catches every caller, including a draw generated outside the HTTP
//     layer, which the API-level validators cannot see;
//   - it validates on WRITE, not on read, so a competition already saved
//     with an odd allocation (a legacy record, or one that inherited an odd
//     venue court list before this rule existed) keeps running and keeps
//     serving its existing matches. Only a NEW draw is refused, and the
//     operator fixes it by reassigning shiaijo in competition settings.
func ValidateCourtPairing(numCourts int) error {
	return helper.ValidateCourtPairing(numCourts)
}

// CourtsOutsideTournament returns, in the competition's own order, every
// shiaijo a competition is assigned that the tournament does not have. It is
// the single source of the "orphaned shiaijo" predicate.
//
// A competition's court list is a SUBSET of the venue's: the tournament owns
// the shiaijo, competitions are allocated some of them. Nothing used to
// enforce that after creation, so shrinking the tournament's court count left
// a competition holding a court that no longer exists. Such a court has no
// operator view (/admin/shiaijo/:court is built from the tournament list), so
// any match drawn onto it is invisible for the whole event.
//
// An empty tournament list returns nothing: it means "not known yet" (the
// bootstrap window before POST /tournament, and the no-tournament-yet edge
// resolveCompetitionCourts defends against), not "no courts exist". Treating
// it as the latter would reject every competition in that window.
//
// Duplicates in compCourts are reported once, in first-seen order, so the
// message never repeats a label.
//
// Mirrored client-side as courtsOutsideTournament in
// web-mobile/js/admin_helpers.jsx, which drives the settings screen's
// orphaned-shiaijo pill and hint.
func CourtsOutsideTournament(compCourts, tournCourts []string) []string {
	if len(compCourts) == 0 || len(tournCourts) == 0 {
		return nil
	}
	have := make(map[string]bool, len(tournCourts))
	for _, cc := range tournCourts {
		have[cc] = true
	}
	var missing []string
	seen := make(map[string]bool, len(compCourts))
	for _, cc := range compCourts {
		if have[cc] || seen[cc] {
			continue
		}
		seen[cc] = true
		missing = append(missing, cc)
	}
	return missing
}

// ValidateCourtsInTournament rejects a competition allocation that names a
// shiaijo the tournament does not have, and owns the operator-facing message
// for it. Callers: the engine draw gate (runDrawPipeline) and the competition
// write paths in internal/mobileapp/handlers_competition.go.
//
// The draw gate is the authoritative one. Refusing the tournament update
// while a live competition depends on a removed court (handlers_tournament.go)
// stops the orphan being CREATED, but records orphaned before that guard
// existed, or by a hand-edited config.md or an imported manifest, can still
// reach the pipeline. Blocking there is what makes "a competition can never be
// drawn onto a shiaijo the tournament does not have" true rather than likely,
// and it validates the same list the draw then uses.
func ValidateCourtsInTournament(compCourts, tournCourts []string) error {
	missing := CourtsOutsideTournament(compCourts, tournCourts)
	if len(missing) == 0 {
		return nil
	}
	verb := "is"
	if len(missing) > 1 {
		verb = "are"
	}
	return fmt.Errorf(
		"shiaijo %s %s not part of this tournament (the tournament has %s): reassign the competition's shiaijo, or add the shiaijo back to the tournament",
		strings.Join(missing, ", "), verb, strings.Join(tournCourts, ", "),
	)
}
