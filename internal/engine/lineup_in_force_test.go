package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The team asked about in these tests is "t" (participant id), named "TeamT".
const lineupTeam = "t"

// lineupOf is a lineup of team whose Senpo is tag, so a test can tell which
// saved lineup came back. matchID empty makes it a Lineups-page lineup for round.
func lineupOf(team, matchID string, round int, tag string) domain.TeamLineup {
	return domain.TeamLineup{
		TeamID: team, MatchID: matchID, Round: round,
		Positions: map[domain.Position]string{domain.PosSenpo: tag},
	}
}

func lineupsOf(ls ...domain.TeamLineup) map[string]domain.TeamLineup {
	out := make(map[string]domain.TeamLineup, len(ls))
	for i, l := range ls {
		out[string(rune('a'+i))] = l
	}
	return out
}

func drawnPoolMatch(id, sideAID, sideBID string) state.MatchResult {
	return state.MatchResult{ID: id, SideA: "name-" + sideAID, SideB: "name-" + sideBID, SideAID: sideAID, SideBID: sideBID}
}

func drawnKnockoutMatch(id, sideAID, sideBID string) state.BracketMatch {
	return state.BracketMatch{ID: id, SideA: "name-" + sideAID, SideB: "name-" + sideBID, SideAID: sideAID, SideBID: sideBID}
}

// lineupDrawFixture is a team's whole competition: three pool matches (and the
// individual bouts that settled a tie in them), then a two-round knockout and a
// 3rd-place match.
//
//	Pool A-0     t v o1     r0-m0  t v x      r1-m0  t v o1     bronze  t v o2
//	Pool A-1     t v o2     r0-m1  o1 v o2
//	Pool A-2     t v o3
//	Pool A-DH-0  t v o1     (a representative bout, not a team match)
//	Pool A-TB-0  t v o2     (a tiebreaker, not a team match)
func lineupDrawFixture() ([]state.MatchResult, *state.Bracket) {
	pool := []state.MatchResult{
		drawnPoolMatch("Pool A-0", lineupTeam, "o1"),
		drawnPoolMatch("Pool A-1", lineupTeam, "o2"),
		drawnPoolMatch("Pool A-2", lineupTeam, "o3"),
		drawnPoolMatch("Pool A-DH-0", lineupTeam, "o1"),
		drawnPoolMatch("Pool A-TB-0", lineupTeam, "o2"),
	}
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{drawnKnockoutMatch("r0-m0", lineupTeam, "x"), drawnKnockoutMatch("r0-m1", "o1", "o2")},
			{drawnKnockoutMatch("r1-m0", lineupTeam, "o1")},
		},
		ThirdPlaceMatch: &state.BracketMatch{ID: "bronze", SideAID: lineupTeam, SideBID: "o2"},
	}
	return pool, bracket
}

func senpoOf(in InForceLineup) string {
	return in.Lineup.Positions[domain.PosSenpo]
}

// ruleOver is the rule over a draw loaded from pool and bracket, for a
// competition that plays a knockout stage: the bracket of every fixture here
// is one. A test about a competition that does not play one asks
// newLineupRuleFrom with false.
func ruleOver(lineups map[string]domain.TeamLineup, pool []state.MatchResult, bracket *state.Bracket) *lineupRule {
	return newLineupRuleFrom(lineups, pool, bracket, true)
}

// TestLineupInForce_Rule is the operator's rule (2026-10-05) as a table: a
// team keeps the lineup of its previous match unless one is entered for the
// match, and a Lineups-page lineup for round r begins at the start of round r.
func TestLineupInForce_Rule(t *testing.T) {
	tests := []struct {
		name    string
		lineups []domain.TeamLineup
		match   string
		want    string // the Senpo of the lineup in force; "" means none is
		source  LineupSource
	}{
		{
			name:    "a match's own lineup wins over the one it would carry and the starting lineup",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0"), lineupOf(lineupTeam, "Pool A-1", 0, "m1")},
			match:   "Pool A-1", want: "m1", source: LineupSource{MatchID: "Pool A-1"},
		},
		{
			name:    "a match's own lineup wins even when the team is not seated in it",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "r0-m1", 0, "own")},
			match:   "r0-m1", want: "own", source: LineupSource{MatchID: "r0-m1"},
		},
		{
			name:    "a match's place comes from the match, even where the team is not seated in it yet",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0")},
			match:   "r0-m1", want: "m0", source: LineupSource{MatchID: "Pool A-0"},
		},
		{
			name:    "the first match uses the starting lineup",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-1", 0, "m1")},
			match:   "Pool A-0", want: "start", source: LineupSource{Round: 0},
		},
		{
			name:    "a lineup entered for the first match is carried to the second",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0")},
			match:   "Pool A-1", want: "m0", source: LineupSource{MatchID: "Pool A-0"},
		},
		{
			name:    "and on to the third",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0")},
			match:   "Pool A-2", want: "m0", source: LineupSource{MatchID: "Pool A-0"},
		},
		{
			name:    "the latest earlier entry is the one carried",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0"), lineupOf(lineupTeam, "Pool A-1", 0, "m1")},
			match:   "Pool A-2", want: "m1", source: LineupSource{MatchID: "Pool A-1"},
		},
		{
			name:    "a lineup carries from the pool into the knockout",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-2", 0, "m2")},
			match:   "r0-m0", want: "m2", source: LineupSource{MatchID: "Pool A-2"},
		},
		{
			name:    "a later entry changes what later matches get, never an earlier match",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-2", 0, "m2")},
			match:   "Pool A-1", want: "start", source: LineupSource{Round: 0},
		},
		{
			name:    "an entry for a later match alone reaches back to nothing",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "Pool A-2", 0, "m2")},
			match:   "Pool A-0", want: "",
		},
		{
			name:    "a lineup for a later round is not read: its team's match was given it when the team was seated",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "", 1, "round1")},
			match:   "r1-m0", want: "start", source: LineupSource{Round: 0},
		},
		{
			name:    "nor before it",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "", 1, "round1")},
			match:   "r0-m0", want: "start", source: LineupSource{Round: 0},
		},
		{
			name:    "it does not displace the entry carried from an earlier match",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0"), lineupOf(lineupTeam, "", 1, "round1")},
			match:   "r1-m0", want: "m0", source: LineupSource{MatchID: "Pool A-0"},
		},
		{
			name:    "and the earlier match still carries its own entry",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0"), lineupOf(lineupTeam, "", 1, "round1")},
			match:   "r0-m0", want: "m0", source: LineupSource{MatchID: "Pool A-0"},
		},
		{
			name:    "an entry for a round 1 match is carried on, past a lineup for the round",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 1, "round1"), lineupOf(lineupTeam, "r1-m0", 0, "m-r1")},
			match:   "bronze", want: "m-r1", source: LineupSource{MatchID: "r1-m0"},
		},
		{
			name:    "the 3rd-place match is last, and takes the starting lineup when nothing is carried to it",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "", 2, "round2")},
			match:   "bronze", want: "start", source: LineupSource{Round: 0},
		},
		{
			name:    "a team with only lineups for later rounds has none in force: they wait for the team to be seated",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 2, "round2"), lineupOf(lineupTeam, "", 1, "round1")},
			match:   "Pool A-0", want: "",
		},
		{
			name:    "a lineup entered for an earlier match is carried whatever lineups wait for later rounds",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 1, "round1"), lineupOf(lineupTeam, "Pool A-0", 0, "m0")},
			match:   "Pool A-1", want: "m0", source: LineupSource{MatchID: "Pool A-0"},
		},
		{
			name:    "a match that is not in the draw takes its own lineup",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "nope", 0, "own")},
			match:   "nope", want: "own", source: LineupSource{MatchID: "nope"},
		},
		{
			name:    "and otherwise the starting lineup, having no place in match order",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 1, "round1"), lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0")},
			match:   "nope", want: "start", source: LineupSource{Round: 0},
		},
		{
			name:    "an entry for a match that is not in the draw is ignored",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool Z-0", 0, "stale")},
			match:   "Pool A-1", want: "start", source: LineupSource{Round: 0},
		},
		{
			name:    "a pool daihyosen or tiebreaker is not a team match, so an entry for one is ignored",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-DH-0", 0, "dh"), lineupOf(lineupTeam, "Pool A-TB-0", 0, "tb")},
			match:   "r0-m0", want: "start", source: LineupSource{Round: 0},
		},
		{
			name:    "another team's lineup is never taken",
			lineups: []domain.TeamLineup{lineupOf(lineupTeam, "", 0, "start"), lineupOf("o1", "Pool A-0", 0, "theirs"), lineupOf("o1", "", 0, "their-start")},
			match:   "Pool A-1", want: "start", source: LineupSource{Round: 0},
		},
		{
			name:    "a team with no lineup at all has none in force",
			lineups: []domain.TeamLineup{lineupOf("o1", "", 0, "theirs")},
			match:   "Pool A-1", want: "",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pool, bracket := lineupDrawFixture()
			rule := ruleOver(lineupsOf(tc.lineups...), pool, bracket)

			got := rule.inForce(lineupTeam, tc.match)

			if tc.want == "" {
				assert.False(t, got.Found, "nothing is in force, got %q", senpoOf(got))
				return
			}
			require.True(t, got.Found)
			assert.Equal(t, tc.want, senpoOf(got))
			assert.Equal(t, tc.source, got.Source)
		})
	}
}

// TestLineupInForce_SameRoundOrder covers a round in which a team plays more
// than once, as it always does in the pool phase: a per-match entry on an
// earlier match is carried to the team's next match of the round, and a lineup
// still waiting for a later round never stands in its way.
func TestLineupInForce_SameRoundOrder(t *testing.T) {
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
		{drawnKnockoutMatch("a", "o1", "o2")},
		{drawnKnockoutMatch("b", lineupTeam, "o1"), drawnKnockoutMatch("c", lineupTeam, "o2")},
	}}
	rule := ruleOver(lineupsOf(
		lineupOf(lineupTeam, "", 1, "round1"),
		lineupOf(lineupTeam, "b", 0, "b"),
	), nil, bracket)

	assert.Equal(t, "b", senpoOf(rule.inForce(lineupTeam, "b")), "the entry saved for b")
	assert.Equal(t, "b", senpoOf(rule.inForce(lineupTeam, "c")),
		"c carries the entry on b, which comes before it in the round")
}

// TestLineupInForce_MatchOrder pins the order a team's matches are played in.
func TestLineupInForce_MatchOrder(t *testing.T) {
	t.Run("pool matches follow their number, not their stored order", func(t *testing.T) {
		pool := []state.MatchResult{
			drawnPoolMatch("Pool A-2", lineupTeam, "o3"),
			drawnPoolMatch("Pool A-0", lineupTeam, "o1"),
			drawnPoolMatch("Pool A-1", lineupTeam, "o2"),
		}
		rule := ruleOver(lineupsOf(lineupOf(lineupTeam, "Pool A-0", 0, "m0")), pool, nil)

		assert.True(t, rule.inForce(lineupTeam, "Pool A-1").Found, "match 2 follows match 1")
		assert.True(t, rule.inForce(lineupTeam, "Pool A-2").Found, "match 3 follows match 1, though stored first")
	})

	t.Run("a pool match numbered 10 follows number 2", func(t *testing.T) {
		pool := []state.MatchResult{
			drawnPoolMatch("Pool A-10", lineupTeam, "o1"),
			drawnPoolMatch("Pool A-2", lineupTeam, "o2"),
		}
		rule := ruleOver(lineupsOf(lineupOf(lineupTeam, "Pool A-2", 0, "m2")), pool, nil)

		assert.Equal(t, "m2", senpoOf(rule.inForce(lineupTeam, "Pool A-10")), "numbered numerically, not as text")
	})

	t.Run("Swiss rounds are played in round order", func(t *testing.T) {
		pool := []state.MatchResult{
			drawnPoolMatch("Swiss-R2-0", lineupTeam, "o2"),
			drawnPoolMatch("Swiss-R1-3", lineupTeam, "o1"),
		}
		rule := ruleOver(lineupsOf(lineupOf(lineupTeam, "Swiss-R1-3", 0, "round1")), pool, nil)

		assert.Equal(t, "round1", senpoOf(rule.inForce(lineupTeam, "Swiss-R2-0")))
		assert.Equal(t, "round1", senpoOf(rule.inForce(lineupTeam, "Swiss-R1-3")), "its own entry")
	})

	t.Run("the knockout follows the pool, and rounds follow one another", func(t *testing.T) {
		pool, bracket := lineupDrawFixture()
		rule := ruleOver(lineupsOf(lineupOf(lineupTeam, "r0-m0", 0, "k0")), pool, bracket)

		assert.False(t, rule.inForce(lineupTeam, "Pool A-2").Found, "the pool is before the knockout")
		assert.Equal(t, "k0", senpoOf(rule.inForce(lineupTeam, "r1-m0")))
		assert.Equal(t, "k0", senpoOf(rule.inForce(lineupTeam, "bronze")))
	})

	t.Run("a bracket the competition does not play holds no team match", func(t *testing.T) {
		pool, bracket := lineupDrawFixture()
		lineups := lineupsOf(lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0"))

		played := newLineupRuleFrom(lineups, pool, bracket, true).inForce(lineupTeam, "r0-m0")
		vestigial := newLineupRuleFrom(lineups, pool, bracket, false).inForce(lineupTeam, "r0-m0")

		assert.Equal(t, "m0", senpoOf(played), "a knockout match comes after the pool, so the pool's lineup is carried to it")
		assert.Equal(t, "start", senpoOf(vestigial), "a vestigial one has no place in the order: only its own lineup and the starting one apply")
	})

	t.Run("a Swiss bye is not a match the team plays", func(t *testing.T) {
		// A Swiss round with an odd number of teams stores its odd team out as a
		// completed match against nobody.
		pool := []state.MatchResult{
			drawnPoolMatch("Swiss-R1-0", lineupTeam, "o1"),
			{ID: "Swiss-R2-0", SideA: "name-" + lineupTeam, SideAID: lineupTeam, Status: state.MatchStatusCompleted},
			drawnPoolMatch("Swiss-R3-0", lineupTeam, "o2"),
		}
		rule := ruleOver(lineupsOf(
			lineupOf(lineupTeam, "Swiss-R1-0", 0, "round1"),
			lineupOf(lineupTeam, "Swiss-R2-0", 0, "bye"),
		), pool, nil)

		assert.Equal(t, "round1", senpoOf(rule.inForce(lineupTeam, "Swiss-R3-0")), "the bye is no earlier match of the team")
	})

	t.Run("a structural bye is not a match the team plays", func(t *testing.T) {
		bracket := &state.Bracket{Rounds: [][]state.BracketMatch{
			{drawnKnockoutMatch("r0-m0", "o1", "o2")},
			{{ID: "r1-m0", SideA: "name-" + lineupTeam, SideAID: lineupTeam, Hidden: true}, drawnKnockoutMatch("r1-m1", "o3", "o4")},
			{drawnKnockoutMatch("r2-m0", lineupTeam, "o1")},
		}}
		rule := ruleOver(lineupsOf(
			lineupOf(lineupTeam, "", 0, "start"),
			lineupOf(lineupTeam, "r1-m0", 0, "bye"),
		), nil, bracket)

		assert.Equal(t, "start", senpoOf(rule.inForce(lineupTeam, "r2-m0")), "the bye is no earlier match of the team")
	})
}

// TestLineupInForce_TeamIdentity pins that a team is identified by participant
// id on a match, never by name.
func TestLineupInForce_TeamIdentity(t *testing.T) {
	t.Run("a team renamed since its earlier match is still seated in it", func(t *testing.T) {
		pool := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Old Name", SideAID: lineupTeam, SideB: "Other", SideBID: "o1"},
			drawnPoolMatch("Pool A-1", lineupTeam, "o2"),
		}
		rule := ruleOver(lineupsOf(lineupOf(lineupTeam, "Pool A-0", 0, "m0")), pool, nil)

		assert.Equal(t, "m0", senpoOf(rule.inForce(lineupTeam, "Pool A-1")))
	})

	t.Run("a match naming the team but seating another id is not the team's match", func(t *testing.T) {
		pool := []state.MatchResult{
			{ID: "Pool A-0", SideA: "TeamT", SideAID: "imposter", SideB: "Other", SideBID: "o1"},
			drawnPoolMatch("Pool A-1", lineupTeam, "o2"),
		}
		rule := ruleOver(lineupsOf(
			lineupOf(lineupTeam, "", 0, "start"),
			lineupOf(lineupTeam, "Pool A-0", 0, "stale"),
		), pool, nil)

		assert.Equal(t, "start", senpoOf(rule.inForce(lineupTeam, "Pool A-1")),
			"the entry on a match the team is not seated in by id is a stale one")
	})

	t.Run("a side with no id seats nobody", func(t *testing.T) {
		pool := []state.MatchResult{
			{ID: "Pool A-0", SideA: "TeamT", SideB: "Other"},
			drawnPoolMatch("Pool A-1", lineupTeam, "o2"),
		}
		rule := ruleOver(lineupsOf(
			lineupOf(lineupTeam, "", 0, "start"),
			lineupOf(lineupTeam, "Pool A-0", 0, "stale"),
		), pool, nil)

		assert.Equal(t, "start", senpoOf(rule.inForce(lineupTeam, "Pool A-1")))
	})

	t.Run("a lineup stored under the team's name is not the team's", func(t *testing.T) {
		pool, bracket := lineupDrawFixture()
		rule := ruleOver(lineupsOf(
			lineupOf("TeamT", "", 0, "start-by-name"),
			lineupOf("TeamT", "Pool A-0", 0, "m0-by-name"),
		), pool, bracket)

		for _, match := range []string{"Pool A-0", "Pool A-1", "r0-m0"} {
			assert.False(t, rule.inForce(lineupTeam, match).Found,
				"%s: the team is its participant id, and nothing is stored under it", match)
		}
	})

	t.Run("an id-less side has no lineup, whatever is stored", func(t *testing.T) {
		pool, bracket := lineupDrawFixture()
		rule := ruleOver(lineupsOf(lineupOf("", "", 0, "under-no-key"), lineupOf(lineupTeam, "", 0, "start")), pool, bracket)

		assert.False(t, rule.inForce("", "Pool A-0").Found)
	})
}

// TestLineupInForce_ThroughTheStore runs the rule over what a competition
// stores, loaded by Engine.LineupInForce.
func TestLineupInForce_ThroughTheStore(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "lineup-in-force"
	comp := &state.Competition{ID: compID, TeamSize: 5, Kind: "team"}
	require.NoError(t, store.SaveCompetition(comp))

	t.Run("nothing saved is not an error", func(t *testing.T) {
		got, err := eng.LineupInForce(compID, lineupTeam, "Pool A-0")
		require.NoError(t, err)
		assert.False(t, got.Found)
	})

	t.Run("an unknown competition has nothing in force", func(t *testing.T) {
		got, err := eng.LineupInForce("no-such-comp", lineupTeam, "Pool A-0")
		require.NoError(t, err)
		assert.False(t, got.Found)
	})

	t.Run("before the draw, a Lineups-page lineup is already in force", func(t *testing.T) {
		const early = "lineup-before-draw"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: early, TeamSize: 5, Kind: "team"}))
		require.NoError(t, store.SetTeamLineup(early, lineupOf(lineupTeam, "", 0, "start"), 5))

		got, err := eng.LineupInForce(early, lineupTeam, "Pool A-0")
		require.NoError(t, err)
		require.True(t, got.Found)
		assert.Equal(t, "start", senpoOf(got))
		assert.Equal(t, LineupSource{Round: 0}, got.Source)
	})

	t.Run("a league's vestigial bracket is not a match the team plays", func(t *testing.T) {
		const league = "lineup-league"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: league, TeamSize: 5, Kind: "team", Format: state.CompFormatLeague}))
		pool, bracket := lineupDrawFixture()
		require.NoError(t, store.SavePoolMatches(league, pool))
		require.NoError(t, store.SaveBracket(league, bracket))
		require.NoError(t, store.SetTeamLineup(league, lineupOf(lineupTeam, "", 0, "start"), 5))
		require.NoError(t, store.SetTeamLineup(league, lineupOf(lineupTeam, "Pool A-0", 0, "m0"), 5))

		got, err := eng.LineupInForce(league, lineupTeam, "r0-m0")

		require.NoError(t, err)
		require.True(t, got.Found)
		assert.Equal(t, "start", senpoOf(got), "the league plays no knockout, so the bracket match is no team match")
		got, err = eng.LineupInForce(league, lineupTeam, "Pool A-2")
		require.NoError(t, err)
		assert.Equal(t, "m0", senpoOf(got), "a pool match carries as ever")
	})

	t.Run("pool matches, the bracket and the lineups are read from the store", func(t *testing.T) {
		pool, bracket := lineupDrawFixture()
		require.NoError(t, store.SavePoolMatches(compID, pool))
		require.NoError(t, store.SaveBracket(compID, bracket))
		require.NoError(t, store.SetTeamLineup(compID, lineupOf(lineupTeam, "", 0, "start"), 5))
		require.NoError(t, store.SetTeamLineup(compID, lineupOf(lineupTeam, "Pool A-0", 0, "m0"), 5))
		// A lineup for round 1 written straight to the store, as an older
		// release left one: nothing reads it.
		require.NoError(t, store.SetTeamLineup(compID, lineupOf(lineupTeam, "", 1, "round1"), 5))

		cases := []struct {
			match, want string
			source      LineupSource
		}{
			{"Pool A-0", "m0", LineupSource{MatchID: "Pool A-0"}},
			{"Pool A-2", "m0", LineupSource{MatchID: "Pool A-0"}},
			{"r0-m0", "m0", LineupSource{MatchID: "Pool A-0"}},
			{"r1-m0", "m0", LineupSource{MatchID: "Pool A-0"}},
			{"bronze", "m0", LineupSource{MatchID: "Pool A-0"}},
		}
		for _, c := range cases {
			got, err := eng.LineupInForce(compID, lineupTeam, c.match)
			require.NoError(t, err)
			require.True(t, got.Found, c.match)
			assert.Equal(t, c.want, senpoOf(got), c.match)
			assert.Equal(t, c.source, got.Source, c.match)
		}
	})
}

// TestLineupInForce_ReadsByTheTeamIDAlone pins, through the store read, that a
// lineup is the team's only when it is stored under the team's participant id:
// the roster knows the team's name too, which is what a name lookup would
// translate through, and it must not.
func TestLineupInForce_ReadsByTheTeamIDAlone(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "lineup-by-id-alone"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, TeamSize: 5, Kind: "team"}))
	teamID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{{ID: teamID, Name: "TeamT", Dojo: "Dojo"}}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "TeamT", SideAID: teamID, SideB: "Other", SideBID: "other"},
		{ID: "Pool A-1", SideA: "TeamT", SideAID: teamID, SideB: "Third", SideBID: "third"},
	}))
	require.NoError(t, store.SetTeamLineup(compID, lineupOf("TeamT", "", 0, "start-by-name"), 5))
	require.NoError(t, store.SetTeamLineup(compID, lineupOf("TeamT", "Pool A-0", 0, "m0-by-name"), 5))

	for _, match := range []string{"Pool A-0", "Pool A-1"} {
		got, err := eng.LineupInForce(compID, teamID, match)
		require.NoError(t, err)
		assert.False(t, got.Found, "%s: a lineup stored under the team's name is not the team's", match)
	}

	require.NoError(t, store.SetTeamLineup(compID, lineupOf(teamID, "", 0, "start"), 5))
	got, err := eng.LineupInForce(compID, teamID, "Pool A-1")
	require.NoError(t, err)
	require.True(t, got.Found, "stored under its id, it is found")
	assert.Equal(t, "start", senpoOf(got))
}

// bigLeagueWithLineups is an eight-team league of five-person teams in which
// every team holds a lineup of its own for each of its seven matches, as the
// round-lineup conversion leaves a competition recorded by an older release: 56
// match lineups and 8 starting lineups, each with its member ids. It returns the
// engine and the first team's id and last match.
func bigLeagueWithLineups(t testing.TB) (eng *Engine, teamID, matchID string) {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng = New(store)
	const compID = "big-league"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Kind: "team", TeamSize: 5, Format: state.CompFormatLeague, RoundLineupsConverted: true,
	}))
	players := make([]domain.Player, 8)
	for i := range players {
		players[i] = domain.Player{ID: helper.NewUUID4(), Name: fmt.Sprintf("Team%d", i), Dojo: fmt.Sprintf("Dojo%d", i)}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	var matches []state.MatchResult
	for i := range players {
		for j := i + 1; j < len(players); j++ {
			matches = append(matches, state.MatchResult{
				ID:    fmt.Sprintf("Pool A-%d", len(matches)),
				SideA: players[i].Name, SideAID: players[i].ID, SideB: players[j].Name, SideBID: players[j].ID,
			})
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
	lineupFor := func(team domain.Player, matchID string) domain.TeamLineup {
		l := domain.TeamLineup{
			TeamID: team.ID, MatchID: matchID,
			Positions: map[domain.Position]string{}, MemberIDs: map[domain.Position]string{},
		}
		for _, pos := range []domain.Position{domain.PosSenpo, domain.PosJiho, domain.PosChuken, domain.PosFukusho, domain.PosTaisho} {
			l.Positions[pos] = team.Name + "-" + string(pos)
			l.MemberIDs[pos] = helper.NewUUID4()
		}
		return l
	}
	byID := map[string]domain.Player{}
	for _, p := range players {
		byID[p.ID] = p
		require.NoError(t, store.SetTeamLineup(compID, lineupFor(p, ""), 5))
	}
	for _, m := range matches {
		require.NoError(t, store.SetTeamLineup(compID, lineupFor(byID[m.SideAID], m.ID), 5))
		require.NoError(t, store.SetTeamLineup(compID, lineupFor(byID[m.SideBID], m.ID), 5))
	}
	return eng, players[0].ID, matches[len(matches)-1].ID
}

// A read answers with a copy of the one lineup it returns: changing it leaves the
// stored lineups, and the next read, alone.
func TestLineupInForce_ReturnsACopyOfTheStoredLineup(t *testing.T) {
	eng, teamID, matchID := bigLeagueWithLineups(t)
	first, err := eng.LineupInForce("big-league", teamID, matchID)
	require.NoError(t, err)
	require.True(t, first.Found)
	was := first.Lineup.Positions[domain.PosSenpo]
	require.NotEmpty(t, was)

	first.Lineup.Positions[domain.PosSenpo] = "changed by the caller"
	first.Lineup.MemberIDs[domain.PosSenpo] = "changed by the caller"
	again, err := eng.LineupInForce("big-league", teamID, matchID)

	require.NoError(t, err)
	assert.Equal(t, was, again.Lineup.Positions[domain.PosSenpo])
	assert.NotEqual(t, "changed by the caller", again.Lineup.MemberIDs[domain.PosSenpo])
}

// LineupInForceFrom is the one rule asked over what a caller already holds, for a
// caller inside a transaction: it answers as the engine's read does from the store,
// for a draw with a knockout and for one without.
func TestLineupInForceFrom_IsTheEnginesRuleOverWhatTheCallerHolds(t *testing.T) {
	pool, bracket := lineupDrawFixture()
	lineups := lineupsOf(lineupOf(lineupTeam, "", 0, "start"), lineupOf(lineupTeam, "Pool A-0", 0, "m0"))

	t.Run("a draw with a knockout", func(t *testing.T) {
		for _, match := range []string{"Pool A-0", "Pool A-1", "Pool A-2", "r0-m0", "bronze", "not-in-the-draw"} {
			got := LineupInForceFrom(lineups, pool, bracket, true, lineupTeam, match)

			assert.Equal(t, ruleOver(lineups, pool, bracket).inForce(lineupTeam, match), got, match)
		}
		got := LineupInForceFrom(lineups, pool, bracket, true, lineupTeam, "Pool A-1")
		require.True(t, got.Found)
		assert.Equal(t, "m0", senpoOf(got), "carried from the previous match")
		assert.Equal(t, LineupSource{MatchID: "Pool A-0"}, got.Source)
	})

	t.Run("a draw with no knockout leaves its vestigial bracket out", func(t *testing.T) {
		got := LineupInForceFrom(lineups, pool, bracket, false, lineupTeam, "r0-m0")

		assert.Equal(t, "start", senpoOf(got), "the bracket match is no team match, so only the starting lineup applies")
	})

	t.Run("a team with no lineup has none in force", func(t *testing.T) {
		assert.False(t, LineupInForceFrom(lineups, pool, bracket, true, "somebody-else", "Pool A-1").Found)
		assert.False(t, LineupInForceFrom(nil, pool, bracket, true, lineupTeam, "Pool A-1").Found)
	})
}

// The read copies only the lineup it returns, so what it costs does not grow with
// the lineups the competition holds: against 64 stored lineups it allocates a
// small fraction of what copying them all does (LoadTeamLineups).
func TestLineupInForce_DoesNotCopyEveryLineupOfTheCompetition(t *testing.T) {
	eng, teamID, matchID := bigLeagueWithLineups(t)
	_, err := eng.LineupInForce("big-league", teamID, matchID) // warm the caches and the one-time legacy upgrade
	require.NoError(t, err)

	read := testing.AllocsPerRun(20, func() { _, _ = eng.LineupInForce("big-league", teamID, matchID) })
	copyAll := testing.AllocsPerRun(20, func() { _, _ = eng.store.LoadTeamLineups("big-league") })

	t.Logf("allocations per read: LineupInForce %.0f, copying every lineup %.0f", read, copyAll)
	assert.Less(t, read*3, copyAll, "a read allocates %.0f against %.0f for copying every stored lineup", read, copyAll)
}

// BenchmarkLineupInForce is the cost of one read of the lineup in force, in a
// competition holding a lineup for every match of every team.
func BenchmarkLineupInForce(b *testing.B) {
	eng, teamID, matchID := bigLeagueWithLineups(b)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := eng.LineupInForce("big-league", teamID, matchID); err != nil {
			b.Fatal(err)
		}
	}
}

// TestLineupInForce_DegradesOnAnUnreadableMatchFile pins that a damaged match
// file costs the read the part of the draw it held, never the read: the lineups
// are intact, so the answer is built from the file that loaded and the other is
// logged by name. A lineups.yaml that cannot be read is still an error, since
// "nothing saved" would be false.
func TestLineupInForce_DegradesOnAnUnreadableMatchFile(t *testing.T) {
	garbage := func(t *testing.T, store *state.Store, compID, file, body string) {
		t.Helper()
		require.NoError(t, os.WriteFile(filepath.Join(store.GetFolder(), "competitions", compID, file), []byte(body), 0o600))
	}
	team := func(t *testing.T, id string) (*Engine, *state.Store) {
		t.Helper()
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: id, TeamSize: 5, Kind: "team"}))
		return eng, store
	}

	t.Run("an unreadable bracket.json still answers from the pool matches", func(t *testing.T) {
		const compID = "degrade-bracket"
		eng, store := team(t, compID)
		pool, _ := lineupDrawFixture()
		require.NoError(t, store.SavePoolMatches(compID, pool))
		garbage(t, store, compID, "bracket.json", "{not json")
		require.NoError(t, store.SetTeamLineup(compID, lineupOf(lineupTeam, "", 0, "start"), 5))
		require.NoError(t, store.SetTeamLineup(compID, lineupOf(lineupTeam, "Pool A-0", 0, "m0"), 5))

		var got InForceLineup
		var err error
		out := captureLog(t, func() { got, err = eng.LineupInForce(compID, lineupTeam, "Pool A-2") })

		require.NoError(t, err)
		require.True(t, got.Found)
		assert.Equal(t, "m0", senpoOf(got), "carried from Pool A-0, which the pool file still places")
		assert.Contains(t, out, "bracket.json", "the file that could not be read is named")
	})

	t.Run("an unreadable pool-matches.csv still answers from the bracket", func(t *testing.T) {
		const compID = "degrade-pool"
		eng, store := team(t, compID)
		_, bracket := lineupDrawFixture()
		require.NoError(t, store.SaveBracket(compID, bracket))
		garbage(t, store, compID, "pool-matches.csv", "\"unterminated")
		require.NoError(t, store.SetTeamLineup(compID, lineupOf(lineupTeam, "", 0, "start"), 5))
		require.NoError(t, store.SetTeamLineup(compID, lineupOf(lineupTeam, "r0-m0", 0, "k0"), 5))

		var got InForceLineup
		var err error
		out := captureLog(t, func() { got, err = eng.LineupInForce(compID, lineupTeam, "r1-m0") })

		require.NoError(t, err)
		require.True(t, got.Found)
		assert.Equal(t, "k0", senpoOf(got), "carried from r0-m0, which the bracket still places")
		assert.Contains(t, out, "pool-matches.csv", "the file that could not be read is named")
	})

	t.Run("an unreadable lineups.yaml is an error", func(t *testing.T) {
		const compID = "degrade-lineups"
		eng, store := team(t, compID)
		garbage(t, store, compID, "lineups.yaml", "lineups: [this is: not: valid yaml")

		_, err := eng.LineupInForce(compID, lineupTeam, "Pool A-0")

		require.Error(t, err)
	})
}
