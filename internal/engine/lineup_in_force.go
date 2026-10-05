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
//     an earlier match of T, or a Lineups-page lineup for round r, which sits
//     at the START of round r (after T's round r-1 matches, before its round r
//     ones), so a round's lineup is simply where that round begins;
//  3. else T's lowest-round Lineups-page lineup, so a team whose only saved
//     lineup belongs to a later round still shows names rather than an empty
//     sheet.
//
// Match order is fixed. Pool and league matches (a Swiss team's rounds in
// round order) are round 0, in pool-match number, which is playing order;
// knockout matches follow at their bracket round index, in position order; the
// 3rd-place match is last, at len(Rounds). A pool daihyosen or tiebreaker is
// one individual bout, not a team match: it holds no lineup and is never a
// previous match.
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
	"cmp"
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
	// only when MatchID is empty. Round 0 is the team's starting lineup.
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

// Places in match order within a round. A round's Lineups-page lineup sits
// before every match of that round.
const (
	placeRoundStart = iota - 1
	placePool
	placeKnockout
)

// matchPlace is a place in match order: where a team match is played, or
// where a round's Lineups-page lineup begins.
type matchPlace struct {
	// round is 0 for the pool phase, the bracket round index for a knockout
	// match, and len(Rounds) for the 3rd-place match.
	round int
	// phase is placeRoundStart, placePool or placeKnockout.
	phase int
	// group is the Swiss round, so a Swiss team's rounds play in order; 0 for
	// every other match.
	group int
	// index is the pool-match number, or the match's position in its round.
	index int
	// seq is the stored order, so no two matches share a place.
	seq int
}

func (a matchPlace) compare(b matchPlace) int {
	return cmp.Or(
		cmp.Compare(a.round, b.round),
		cmp.Compare(a.phase, b.phase),
		cmp.Compare(a.group, b.group),
		cmp.Compare(a.index, b.index),
		cmp.Compare(a.seq, b.seq),
	)
}

// drawnMatch is one team match of the draw: its place, and who is seated.
type drawnMatch struct {
	place            matchPlace
	sideAID, sideBID string
}

// seats reports whether the team with participant id teamID is seated in the
// match. A side that carries no id (a bye, an unresolved feeder) seats nobody.
func (d drawnMatch) seats(teamID string) bool {
	return teamID != "" && (d.sideAID == teamID || d.sideBID == teamID)
}

// lineupDraw is the draw as the rule sees it: every team match by id.
type lineupDraw map[string]drawnMatch

// newLineupDraw places each team match of a projected draw: a pool or league
// match by its pool-match number (a Swiss team's rounds in round order), a
// knockout match by round and position, the 3rd-place match last. A pool
// daihyosen or tiebreaker is not a team match and is left out.
func newLineupDraw(matches []state.DrawMatch) lineupDraw {
	draw := make(lineupDraw, len(matches))
	for seq, m := range matches {
		if m.ID == "" {
			continue
		}
		place := matchPlace{round: m.Round, phase: placeKnockout, index: m.Index, seq: seq}
		if !m.Knockout {
			if IsPoolDaihyosenMatchID(m.ID) || IsTiebreakerMatchID(m.ID) {
				continue
			}
			swissRound, _ := parseSwissMatchRound(m.ID)
			place = matchPlace{phase: placePool, group: swissRound, index: poolPhaseMatchNumber(m.ID), seq: seq}
		}
		draw[m.ID] = drawnMatch{place: place, sideAID: m.SideAID, sideBID: m.SideBID}
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

// placedLineup is a saved lineup of the team being asked about, with where it
// sits in match order.
type placedLineup struct {
	lineup domain.TeamLineup
	place  matchPlace
}

// inForce resolves the lineup the team with participant id teamID fields at
// matchID. An empty teamID, a side with no id, has no lineup. A match the draw
// does not hold has no place in match order, so only its own lineup (1) and the
// lowest-round Lineups-page lineup (3) can apply to it.
func (r *lineupRule) inForce(teamID, matchID string) InForceLineup {
	if teamID == "" {
		return InForceLineup{}
	}
	at, located := r.draw[matchID]

	var own, before, rounds []placedLineup
	for _, l := range r.lineups {
		if l.TeamID != teamID {
			continue
		}
		switch l.MatchID {
		case "":
			p := placedLineup{lineup: l, place: matchPlace{round: l.Round, phase: placeRoundStart}}
			rounds = append(rounds, p)
			if located && l.Round <= at.place.round {
				before = append(before, p)
			}
		case matchID:
			own = append(own, placedLineup{lineup: l})
		default:
			earlier, drawn := r.draw[l.MatchID]
			if located && drawn && earlier.seats(teamID) && earlier.place.compare(at.place) < 0 {
				before = append(before, placedLineup{lineup: l, place: earlier.place})
			}
		}
	}

	// Each pick is unambiguous: a team has one lineup per match and one per
	// round, and no two places are equal.
	var best placedLineup
	switch {
	case len(own) > 0:
		best = own[0]
	case len(before) > 0:
		best = slices.MaxFunc(before, func(a, b placedLineup) int { return a.place.compare(b.place) })
	case len(rounds) > 0:
		best = slices.MinFunc(rounds, func(a, b placedLineup) int { return a.place.compare(b.place) })
	default:
		return InForceLineup{}
	}
	source := LineupSource{Round: best.lineup.Round}
	if best.lineup.MatchID != "" {
		source = LineupSource{MatchID: best.lineup.MatchID}
	}
	return InForceLineup{Lineup: best.lineup, Source: source, Found: true}
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
