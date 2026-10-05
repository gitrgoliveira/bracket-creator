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
// A team is identified on a match by participant id (SideAID/SideBID), never
// by name: a lineup saved for a match only counts as one of T's earlier lineups
// while that match is in the current draw and T is seated in it, so an entry
// left behind by a re-seat is ignored. Lineups themselves are keyed by the
// participant id the lineup editor writes, then by the team name older data
// used.

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

// seats reports whether a team, as the set of keys its lineups are stored
// under, is seated in the match by participant id. A side that carries no id (a
// bye, an unresolved feeder) seats nobody.
func (d drawnMatch) seats(keys map[string]int) bool {
	_, a := keys[d.sideAID]
	_, b := keys[d.sideBID]
	return (d.sideAID != "" && a) || (d.sideBID != "" && b)
}

// lineupDraw is the draw as the rule sees it: every team match by id.
type lineupDraw map[string]drawnMatch

func newLineupDraw(poolMatches []state.MatchResult, bracket *state.Bracket) lineupDraw {
	draw := lineupDraw{}
	seq := 0
	for _, m := range poolMatches {
		seq++
		if m.ID == "" || IsPoolDaihyosenMatchID(m.ID) || IsTiebreakerMatchID(m.ID) {
			continue
		}
		swissRound, _ := parseSwissMatchRound(m.ID)
		draw[m.ID] = drawnMatch{
			place:   matchPlace{phase: placePool, group: swissRound, index: poolPhaseMatchNumber(m.ID), seq: seq},
			sideAID: m.SideAID, sideBID: m.SideBID,
		}
	}
	if bracket == nil {
		return draw
	}
	add := func(bm state.BracketMatch, round, index int) {
		seq++
		if bm.ID == "" {
			return
		}
		draw[bm.ID] = drawnMatch{
			place:   matchPlace{round: round, phase: placeKnockout, index: index, seq: seq},
			sideAID: bm.SideAID, sideBID: bm.SideBID,
		}
	}
	for round, matches := range bracket.Rounds {
		for index, bm := range matches {
			add(bm, round, index)
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil {
		add(*bm, len(bracket.Rounds), 0)
	}
	return draw
}

// lineupRule answers which lineup a team fields at a match, from lineups, a
// draw and a roster loaded once, so an export asking for every match loads
// them once rather than once per match.
type lineupRule struct {
	lineups map[string]domain.TeamLineup
	draw    lineupDraw
	roster  []domain.Player
}

func newLineupRule(lineups map[string]domain.TeamLineup, roster []domain.Player, poolMatches []state.MatchResult, bracket *state.Bracket) *lineupRule {
	return &lineupRule{lineups: lineups, draw: newLineupDraw(poolMatches, bracket), roster: roster}
}

// teamLineupKeys lists the keys a team's lineups may be stored under, best
// first: the participant id (the lineup editor's key), then the team name
// (older data). Either half may be missing, and the roster supplies it.
func teamLineupKeys(roster []domain.Player, id, name string) []string {
	var keys []string
	add := func(key string) {
		if key != "" && !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	add(id)
	switch {
	case id == "":
		for _, p := range roster {
			if p.Name == name {
				add(p.ID)
			}
		}
	case name == "":
		for _, p := range roster {
			if p.ID == id {
				name = p.Name
				break
			}
		}
	}
	add(name)
	return keys
}

// placedLineup is a saved lineup of the team being asked about, with where it
// sits in match order and how well the key it was saved under ranks.
type placedLineup struct {
	lineup domain.TeamLineup
	place  matchPlace
	rank   int
}

// inForce resolves the lineup for the team whose participant id and team name
// are given (either may be empty) at matchID. A match the draw does not hold
// has no place in match order, so only its own lineup (1) and the lowest-round
// Lineups-page lineup (3) can apply to it.
func (r *lineupRule) inForce(id, name, matchID string) InForceLineup {
	keys := teamLineupKeys(r.roster, id, name)
	rank := make(map[string]int, len(keys))
	for i, key := range keys {
		rank[key] = i
	}
	at, located := r.draw[matchID]

	var own, before, rounds []placedLineup
	for _, l := range r.lineups {
		keyRank, mine := rank[l.TeamID]
		if !mine {
			continue
		}
		switch l.MatchID {
		case "":
			p := placedLineup{lineup: l, place: matchPlace{round: l.Round, phase: placeRoundStart}, rank: keyRank}
			rounds = append(rounds, p)
			if located && l.Round <= at.place.round {
				before = append(before, p)
			}
		case matchID:
			own = append(own, placedLineup{lineup: l, rank: keyRank})
		default:
			earlier, drawn := r.draw[l.MatchID]
			if located && drawn && earlier.seats(rank) && earlier.place.compare(at.place) < 0 {
				before = append(before, placedLineup{lineup: l, place: earlier.place, rank: keyRank})
			}
		}
	}

	var best placedLineup
	switch {
	case len(own) > 0:
		best = slices.MinFunc(own, func(a, b placedLineup) int { return cmp.Compare(a.rank, b.rank) })
	case len(before) > 0:
		// The latest place wins; under one place, the better key.
		best = slices.MaxFunc(before, func(a, b placedLineup) int {
			return cmp.Or(a.place.compare(b.place), cmp.Compare(b.rank, a.rank))
		})
	case len(rounds) > 0:
		best = slices.MinFunc(rounds, func(a, b placedLineup) int {
			return cmp.Or(a.place.compare(b.place), cmp.Compare(a.rank, b.rank))
		})
	default:
		return InForceLineup{}
	}
	source := LineupSource{Round: best.lineup.Round}
	if best.lineup.MatchID != "" {
		source = LineupSource{MatchID: best.lineup.MatchID}
	}
	return InForceLineup{Lineup: best.lineup, Source: source, Found: true}
}

// loadLineupRoster reads the roster that translates a team's participant id to
// its name and back. A roster that cannot be read only loses that translation,
// so it degrades to none rather than failing the lookup.
func (e *Engine) loadLineupRoster(compID string, comp *state.Competition) []domain.Player {
	if comp == nil {
		return nil
	}
	roster, err := e.store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
	if err != nil {
		log.Printf("engine.lineupInForce compId=%s: participant load error: %v; lineup lookup degrades to the keys the match carries", compID, err)
		return nil
	}
	return roster
}

// loadLineupRule reads what the rule needs for compID. A competition with no
// saved lineups skips the draw and roster reads altogether.
func (e *Engine) loadLineupRule(compID string, comp *state.Competition) (*lineupRule, error) {
	lineups, err := e.store.LoadTeamLineups(compID)
	if err != nil {
		return nil, err
	}
	if len(lineups) == 0 {
		return &lineupRule{}, nil
	}
	poolMatches, err := e.store.LoadPoolMatches(compID)
	if err != nil {
		return nil, err
	}
	bracket, err := e.store.LoadBracket(compID)
	if err != nil {
		return nil, err
	}
	return newLineupRule(lineups, e.loadLineupRoster(compID, comp), poolMatches, bracket), nil
}

// LineupInForce returns the lineup the team with participant id teamID fields
// at match matchID, and where it was saved. Found is false when the team has
// no saved lineup that applies.
func (e *Engine) LineupInForce(compID, teamID, matchID string) (InForceLineup, error) {
	comp, err := e.store.LoadCompetition(compID)
	if err != nil {
		return InForceLineup{}, err
	}
	rule, err := e.loadLineupRule(compID, comp)
	if err != nil {
		return InForceLineup{}, err
	}
	return rule.inForce(teamID, "", matchID), nil
}
