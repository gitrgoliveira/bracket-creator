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
// exists because the draw gives each shiaijo its own block of the bracket and
// those blocks merge in pairs, so it only binds where there are blocks.
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

// ValidateCompetitionShiaijoCount rejects a competition-level court allocation
// that is not a power of two (helper.ValidateShiaijoCount owns the rule and its
// message: the knockout draw gives each shiaijo its own block of the bracket and
// the blocks merge in pairs, so the count has to halve cleanly). 1, 2, 4, 8 and
// 16 are legal; 3, 5, 6, 7, 10 and the rest are not.
//
// The gate is the EXEMPTIONS plus the rule, and it lives here rather than at the
// HTTP boundary because the draw pipeline enforces the same composite and the
// engine is the deeper of the two. A handler-side copy would let the API accept
// what the draw then refuses (or the reverse) the moment either exemption moves.
//
// An empty list passes, because it means "inherit the tournament's courts" and
// carries no count of its own. Every caller either resolves it first (POST
// /competitions, the manifest importer) or is comparing it against a stored
// allocation that was itself already resolved (the settings PUT). InheritedDrawCourts
// is what materialises the inherited value, and it must run BEFORE this check:
// what gets persisted is what gets validated, or a 3-shiaijo venue smuggles a
// 3-shiaijo competition in by inheritance while the operator who typed the same
// three courts out is refused.
//
// Scoped by format to the competitions whose draw builds a bracket
// (CompetitionDrawsBracket): a league or Swiss competition has no bracket blocks
// to merge and its courts run in parallel, so the rule does not bind there. Note
// the app itself suggests court counts like 3 for a league (SuggestedMaxCourts is
// floor(N/2)-1), which a format-blind rule would then reject.
//
// Note what else is NOT validated: the TOURNAMENT's court list. The rule is per
// competition, so a 3-, 5- or 7-shiaijo venue is perfectly legal and simply
// cannot give all of them to one bracket competition (4 + 1 across two
// competitions is the intended shape on a 5-court venue; a 3-court venue runs its
// competitions on 2 and 1).
//
// Existing data is validated on WRITE only. A competition already saved with an
// invalid allocation keeps running and keeps being editable; the operator UI
// shows a persistent warning on its settings screen until the allocation is
// changed.
func ValidateCompetitionShiaijoCount(courts []string, format string) error {
	if len(courts) == 0 || !CompetitionDrawsBracket(format) {
		return nil
	}
	return helper.ValidateShiaijoCount(len(courts))
}

// InheritedDrawCourts materialises the shiaijo allocation a draw runs on.
//
// A competition's own list wins whenever it has one, untouched: that is the
// operator's allocation, and it has its own validators at every write path
// plus the gates in runDrawPipeline. An EMPTY list has always meant "inherit
// the tournament's shiaijo" (validateCourtLabels documents it, and every HTTP
// write path resolves it that way), so this returns the venue's list. The
// ["A"] fallback covers the no-tournament-yet bootstrap edge, where the
// generators would otherwise produce unnamed courts.
//
// Nothing is trimmed to a legal shiaijo count here. When a venue's court count
// is not a legal allocation the inherited list is REFUSED by the caller's
// count gate rather than silently reduced, because choosing which two of three
// shiaijo a competition runs on is the operator's decision; the create path
// makes the identical ruling, so omitting a court list and stating it reach the
// same outcome.
//
// This is the one owner of the rule. The HTTP write paths reach it through
// resolveCompetitionCourts (internal/mobileapp/handlers_tournament.go), which
// resolves an allocation before persisting it; runDrawPipeline calls it
// directly, because records with no courts key still reach the engine from
// legacy data, imported manifests and hand-edited config files. Both must
// answer identically or a competition is drawn on a different allocation from
// the one it was saved with.
func InheritedDrawCourts(compCourts []string, tourn *state.Tournament) []string {
	if len(compCourts) > 0 {
		return compCourts
	}
	if tourn != nil && len(tourn.Courts) > 0 {
		return append([]string(nil), tourn.Courts...)
	}
	return []string{helper.CourtLabel(0)}
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

// CourtsStillInUse returns the shiaijo that scheduled or running matches of this
// competition are on but the proposed allocation drops, in the order the first
// blocking match on each is found (pool matches first, then the bracket).
//
// The competition-level twin of the tournament's orphan guard: removing a court
// a live match is still assigned to leaves that match pointing at a shiaijo the
// competition claims not to use, which is a bout with no operator view to run it
// from. The operator's route is the one they already have -- move those matches
// to a shiaijo they are keeping, then drop the court.
//
// COMPLETED matches are deliberately ignored. They are a record of where
// something was already fought, not work that still has to be scheduled
// somewhere, and refusing on them would make a court unremovable for the rest of
// the tournament.
func CourtsStillInUse(proposed []string, poolMatches []state.MatchResult, bracket *state.Bracket) []string {
	keep := make(map[string]bool, len(proposed))
	for _, c := range proposed {
		keep[c] = true
	}
	// ONE walk. A second pass to order the output is how the bronze got lost:
	// ThirdPlaceMatch is a SIBLING of bracket.Rounds, not a row in it, so a
	// rounds-only loop silently skips it and a shiaijo carrying only a live
	// bronze bout reported as free to remove.
	var out []string
	seen := make(map[string]bool)
	note := func(court string, status state.MatchStatus) {
		if court == "" || keep[court] || seen[court] || status == state.MatchStatusCompleted {
			return
		}
		seen[court] = true
		out = append(out, court)
	}
	for _, m := range poolMatches {
		note(m.Court, m.Status)
	}
	if bracket != nil {
		for _, round := range bracket.Rounds {
			for i := range round {
				note(round[i].Court, round[i].Status)
			}
		}
		if bracket.ThirdPlaceMatch != nil {
			note(bracket.ThirdPlaceMatch.Court, bracket.ThirdPlaceMatch.Status)
		}
	}
	return out
}

// ValidateCourtsNotInUse is CourtsStillInUse with the operator-facing refusal,
// so the predicate and the sentence live together the way every other rule in
// this file does (ValidateCourtsInTournament, ValidateCompetitionShiaijoCount)
// rather than being reassembled at the HTTP boundary.
func ValidateCourtsNotInUse(proposed []string, poolMatches []state.MatchResult, bracket *state.Bracket) error {
	return CourtsInUseError(CourtsStillInUse(proposed, poolMatches, bracket))
}

// CourtsInUseError is the operator-facing refusal for a set of shiaijo that
// still carry live bouts, or nil for an empty set. Split out so a caller that
// NARROWS the set first (the settings PUT, which reports only the shiaijo its
// own edit removes) words the refusal identically to one that does not.
func CourtsInUseError(busy []string) error {
	if len(busy) == 0 {
		return nil
	}
	verb, pronoun := "has", "it"
	if len(busy) > 1 {
		verb, pronoun = "have", "them"
	}
	return fmt.Errorf(
		"shiaijo %s still %s matches scheduled on %s; move them to a shiaijo this competition keeps, then remove %s",
		strings.Join(busy, ", "), verb, pronoun, pronoun)
}
