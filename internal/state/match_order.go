package state

// match_order.go owns which matches of a draw are team matches and the order a
// team plays them in. TeamMatches is the one list of them, and MatchPlace the
// one order, that the lineup rule (engine.LineupInForce, which asks "which of a
// team's lineups comes before this match") and the round-lineup settlement
// (round_lineups.go, which gives a team a lineup for each match it is seated in)
// both read, so the two cannot disagree about either.

import (
	"cmp"
	"slices"
	"strconv"
	"strings"
)

// SwissMatchIDPrefix is the prefix of a Swiss match id ("Swiss-R2-0": round 2,
// match 0). Swiss matches are stored beside pool matches, so the id is what
// tells them apart.
const SwissMatchIDPrefix = "Swiss-R"

// ParseSwissMatchRound extracts the round number from a Swiss match ID.
// Returns (round, true) on success; (0, false) for any non-Swiss match ID or
// malformed shape.
func ParseSwissMatchRound(id string) (int, bool) {
	if !strings.HasPrefix(id, SwissMatchIDPrefix) {
		return 0, false
	}
	rest := strings.TrimPrefix(id, SwissMatchIDPrefix)
	dash := strings.Index(rest, "-")
	if dash < 0 {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:dash])
	if err != nil || n < 1 {
		return 0, false
	}
	return n, true
}

// PoolPhaseMatchNumber mirrors pool_ids.jsx's poolMatchNumberOf: the bout's
// 1-based match number WITHIN its pool/round, taken from the id's own
// trailing "-<digits>" ordinal (0-based on disk, so +1 here). Returns 0 for
// a supplementary id (DH/TB) or an id with no numeric ordinal at all.
func PoolPhaseMatchNumber(matchID string) int {
	if IsPoolDaihyosenMatchID(matchID) || IsTiebreakerMatchID(matchID) {
		return 0
	}
	i := strings.LastIndexByte(matchID, '-')
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(matchID[i+1:])
	if err != nil {
		return 0
	}
	return n + 1
}

// The phases of match order: the pool phase (pool, league and Swiss matches)
// comes before the knockout.
const (
	placePool = iota
	placeKnockout
)

// MatchPlace is where a team match falls in match order. Pool, league and
// Swiss matches come first, in pool-match number (the number is the place in
// the playing order; a Swiss team's rounds in round order); the knockout
// follows, each round in position order; the 3rd-place match is last, after
// the final's round. Two matches never share a place: the stored order breaks
// every tie.
type MatchPlace struct {
	// round is 0 for the pool phase, the bracket round index for a knockout
	// match, and len(Rounds) for the 3rd-place match.
	round int
	// phase is placePool or placeKnockout.
	phase int
	// group is the Swiss round, so a Swiss team's rounds play in order; 0 for
	// every other match.
	group int
	// index is the pool-match number, or the match's position in its round.
	index int
	// seq is the stored order.
	seq int
}

// Compare orders two places: negative when a comes before b.
func (a MatchPlace) Compare(b MatchPlace) int {
	return cmp.Or(
		cmp.Compare(a.round, b.round),
		cmp.Compare(a.phase, b.phase),
		cmp.Compare(a.group, b.group),
		cmp.Compare(a.index, b.index),
		cmp.Compare(a.seq, b.seq),
	)
}

// Place is where m falls in match order, seq being its position in the slice
// DrawMatchesFrom returned. ok is false for a match with no place in the order:
// a pool daihyosen or tiebreaker is one individual bout, and a match with no id
// cannot be named. TeamMatches leaves out the rest of what is not a team match.
func (m DrawMatch) Place(seq int) (place MatchPlace, ok bool) {
	if m.ID == "" {
		return MatchPlace{}, false
	}
	if m.Knockout {
		return MatchPlace{round: m.Round, phase: placeKnockout, index: m.Index, seq: seq}, true
	}
	if IsPoolDaihyosenMatchID(m.ID) || IsTiebreakerMatchID(m.ID) {
		return MatchPlace{}, false
	}
	swissRound, _ := ParseSwissMatchRound(m.ID)
	return MatchPlace{phase: placePool, group: swissRound, index: PoolPhaseMatchNumber(m.ID), seq: seq}, true
}

// Seats reports whether the team with participant id teamID is seated in the
// match. A side that carries no id (a bye, an unresolved feeder) seats nobody.
func (m DrawMatch) Seats(teamID string) bool {
	return teamID != "" && (m.SideAID == teamID || m.SideBID == teamID)
}

// TeamMatch is a team match of the draw and its place in match order.
type TeamMatch struct {
	DrawMatch
	Place MatchPlace
}

// TeamMatches lists the team matches of a projected draw, the matches a team can
// be seated in and play, in match order. knockout says whether the competition
// plays a knockout stage (Competition.IsKnockoutEnabled): a league or Swiss
// competition has none, so whatever a vestigial bracket.json holds is left out.
// Also left out are a structural bye (Hidden) and a pool-shaped match with an
// empty side (Bye, a Swiss round's odd team out), which nobody plays; a pool
// representative bout or tie-break, which is one individual bout; and a match
// with no id, which cannot be named (DrawMatch.Place).
func TeamMatches(draw []DrawMatch, knockout bool) []TeamMatch {
	matches := make([]TeamMatch, 0, len(draw))
	for seq, m := range draw {
		if (m.Knockout && !knockout) || m.Hidden || m.Bye {
			continue
		}
		if place, ok := m.Place(seq); ok {
			matches = append(matches, TeamMatch{DrawMatch: m, Place: place})
		}
	}
	slices.SortFunc(matches, func(a, b TeamMatch) int { return a.Place.Compare(b.Place) })
	return matches
}
