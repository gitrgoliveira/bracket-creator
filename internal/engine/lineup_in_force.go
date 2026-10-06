package engine

// lineup_in_force.go owns the rule for which lineup a team fields in a match
// (operator ruling 2026-10-05: "by default, a team carries the previous team
// match lineup. You can have a different team lineup in every team match").
// The kachinuki advance, the Kachinuki Detail export and the public
// lineup-in-force read all ask it, so no surface restates it.
//
// The lineup in force for team T at match M is, in order:
//
//  1. the lineup saved for T at M itself: a match's own lineup always wins;
//  2. else the latest lineup of T placed before M in match order: one saved for
//     an earlier match of T, or, at the very start, T's starting lineup (the
//     Lineups page's lineup, stored as round 0);
//  3. else, for a match the draw does not hold, the starting lineup.
//
// A lineup saved for a later round (round >= 1, which releases up to v2.1.1 let
// the Lineups page save) is not read. The state layer moves each onto the first
// match its team is seated in at that round (state.settleRoundLineups), on load
// and then in the write that seats the team, so one that still exists is
// waiting for its team to be seated, and cannot apply.
//
// Match order is state.MatchPlace: pool and league matches in pool-match number,
// which is playing order (a Swiss team's rounds in round order); knockout matches
// after them at their bracket round index, in position order; the 3rd-place match
// last, at len(Rounds). A pool daihyosen or tiebreaker is one individual bout,
// not a team match: it holds no lineup and is never a previous match.
//
// A team is its participant id, and nothing else. Every caller asks with the id
// (the HTTP read's :tid, a match's SideAID/SideBID) and a lineup is the team's
// only when it is stored under exactly that id, the key the lineup editor
// writes: a lineup stored under a team NAME is not the team's, and a side that
// carries no id (a bye, an unresolved feeder) has none. A lineup saved for a
// match only counts as one of T's earlier lineups while that match is in the
// current draw and T is seated in it by id, so an entry left behind by a re-seat
// is ignored.

import (
	"log"
	"slices"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// LineupSource says where an InForceLineup was saved.
type LineupSource struct {
	// MatchID is the match the lineup was saved for: the match asked about
	// (its own lineup) or an earlier match of the team it is carried from.
	// Empty for a Lineups-page lineup.
	MatchID string
	// Round is the Lineups-page round the lineup was saved for, meaningful
	// only when MatchID is empty. It is 0, the team's starting lineup: a
	// lineup for a later round is moved onto a match, never read as one.
	Round int
}

// InForceLineup is the lineup a team fields at a match and where it came from.
type InForceLineup struct {
	// Lineup is the stored entry itself, so its own MatchID and Round name
	// the source as well.
	Lineup domain.TeamLineup
	Source LineupSource
	// Found is false when the team has no saved lineup that applies.
	Found bool
}

// drawnMatch is one team match of the draw: who is seated, and its place in
// match order.
type drawnMatch struct {
	state.DrawMatch
	place state.MatchPlace
}

// lineupDraw is the draw as the rule sees it: every team match by id.
type lineupDraw map[string]drawnMatch

// newLineupDraw places each team match of a projected draw in match order
// (state.DrawMatch.Place). A pool daihyosen or tiebreaker is not a team match
// and is left out.
func newLineupDraw(matches []state.DrawMatch) lineupDraw {
	draw := make(lineupDraw, len(matches))
	for seq, m := range matches {
		if place, ok := m.Place(seq); ok {
			draw[m.ID] = drawnMatch{DrawMatch: m, place: place}
		}
	}
	return draw
}

// lineupRule answers which lineup a team fields at a match, from lineups and a
// draw loaded once, so an export asking for every match loads them once rather
// than once per match. The zero value, &lineupRule{}, is the rule over no
// lineups: every team has none in force, and so no position is labelled.
type lineupRule struct {
	lineups map[string]domain.TeamLineup
	draw    lineupDraw
}

// newLineupRule builds the rule over lineups and the draw they were saved
// against. With no lineups there is nothing to place, so the draw is not built.
func newLineupRule(lineups map[string]domain.TeamLineup, draw []state.DrawMatch) *lineupRule {
	if len(lineups) == 0 {
		return &lineupRule{}
	}
	return &lineupRule{lineups: lineups, draw: newLineupDraw(draw)}
}

// newLineupRuleFrom is newLineupRule for a caller that already holds the pool
// matches and the bracket, which the kachinuki advance and the export do, so
// the draw is projected from what they loaded rather than read again. A nil
// bracket leaves the knockout out of the draw.
func newLineupRuleFrom(lineups map[string]domain.TeamLineup, poolMatches []state.MatchResult, bracket *state.Bracket) *lineupRule {
	return newLineupRule(lineups, state.DrawMatchesFrom(poolMatches, bracket))
}

// placedLineup is a saved lineup of the team being asked about, for an earlier
// match, with where that match sits in match order.
type placedLineup struct {
	lineup domain.TeamLineup
	place  state.MatchPlace
}

// inForce resolves the lineup the team with participant id teamID fields at
// matchID. An empty teamID, a side with no id, has no lineup. A match the draw
// does not hold has no place in match order, so only its own lineup (1) and the
// starting lineup (3) can apply to it.
func (r *lineupRule) inForce(teamID, matchID string) InForceLineup {
	if teamID == "" {
		return InForceLineup{}
	}
	at, located := r.draw[matchID]

	var own, start []domain.TeamLineup
	var before []placedLineup
	for _, l := range r.lineups {
		if l.TeamID != teamID {
			continue
		}
		switch l.MatchID {
		case "":
			// Only round 0, the starting lineup, is read. Any other round's
			// lineup is waiting for its team to be seated (see the rule above).
			if l.Round == 0 {
				start = append(start, l)
			}
		case matchID:
			own = append(own, l)
		default:
			earlier, drawn := r.draw[l.MatchID]
			if located && drawn && earlier.Seats(teamID) && earlier.place.Compare(at.place) < 0 {
				before = append(before, placedLineup{lineup: l, place: earlier.place})
			}
		}
	}

	// Each pick is unambiguous: a team has one lineup per match and one
	// starting lineup, and no two places are equal.
	var best domain.TeamLineup
	switch {
	case len(own) > 0:
		best = own[0]
	case len(before) > 0:
		best = slices.MaxFunc(before, func(a, b placedLineup) int { return a.place.Compare(b.place) }).lineup
	case len(start) > 0:
		best = start[0]
	default:
		return InForceLineup{}
	}
	source := LineupSource{Round: best.Round}
	if best.MatchID != "" {
		source = LineupSource{MatchID: best.MatchID}
	}
	return InForceLineup{Lineup: best, Source: source, Found: true}
}

// lineupRuleOrNone builds the rule for a caller that holds the pool matches and
// the bracket and keeps working without lineups, the kachinuki advance and the
// Kachinuki Detail export. A lineups.yaml that cannot be read is logged, naming
// caller, and answers no lineup, so the roster falls back to the bout log and
// no position is labelled.
func (e *Engine) lineupRuleOrNone(caller, compID string, poolMatches []state.MatchResult, bracket *state.Bracket) *lineupRule {
	lineups, err := e.store.LoadTeamLineups(compID)
	if err != nil {
		log.Printf("%s compId=%s: lineups.yaml load error: %v; resolving no lineup", caller, compID, err)
		return &lineupRule{}
	}
	return newLineupRuleFrom(lineups, poolMatches, bracket)
}

// LineupInForce returns the lineup the team with participant id teamID fields
// at match matchID, and where it was saved. Found is false when the team has
// no saved lineup that applies.
//
// A lineups.yaml that cannot be read is returned, since an answer of "nothing
// saved" would be false. A pool-matches.csv or bracket.json that cannot be read
// is logged, naming the file, and the draw is built from the one that loaded, so
// a damaged match file costs only the carry across the matches it held, not
// every lineup read. The competition itself is not read, and a competition with
// no saved lineups skips the draw read.
func (e *Engine) LineupInForce(compID, teamID, matchID string) (InForceLineup, error) {
	lineups, err := e.store.LoadTeamLineups(compID)
	if err != nil {
		return InForceLineup{}, err
	}
	if len(lineups) == 0 {
		return InForceLineup{}, nil
	}
	draw, err := e.store.DrawMatches(compID)
	if err != nil {
		log.Printf("engine.LineupInForce compId=%s: %v; the draw is built from the files that loaded", compID, err)
	}
	return newLineupRule(lineups, draw).inForce(teamID, matchID), nil
}
