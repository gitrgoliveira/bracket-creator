package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
)

// bc-xqtb: with extraQualifiers larger-pools an oversized pool sends one more
// qualifier than Winners per pool, so a tie over THAT place decides who goes
// through exactly as a tie over any other qualifying place does. The cut a tie
// is judged against is the pool's own qualifier count (poolTieBreakCut, which is
// Competition.QualifiersForPool), never the uniform Winners per pool.

// largerPools makes c a minimum-size competition whose pools above poolSize
// players send one extra qualifier.
func largerPools(poolSize int) func(*state.Competition) {
	return func(c *state.Competition) {
		c.PoolSize = poolSize
		c.ExtraQualifiers = state.ExtraQualifiersLargerPools
	}
}

// poolBouts numbers a pool's completed bouts in the order given; a winner of ""
// is a hikiwake.
func poolBouts(pool string, pairs ...[3]string) []state.MatchResult {
	out := make([]state.MatchResult, len(pairs))
	for i, p := range pairs {
		out[i] = state.MatchResult{
			ID: fmt.Sprintf("%s-%d", pool, i), SideA: p[0], SideB: p[1],
			Status: state.MatchStatusCompleted, Winner: p[2], Court: "A",
		}
		if p[2] == "" {
			out[i].Decision = string(domain.DecisionHikiwake)
		}
	}
	return out
}

// Four players: Alice unbeaten, Bob and Carol level for 2nd (each beat Dave and
// drew each other), Dave last.
func fourPlayerTieForSecond() []state.MatchResult {
	return poolBouts("Pool A",
		[3]string{"Alice", "Bob", "Alice"}, [3]string{"Alice", "Carol", "Alice"},
		[3]string{"Alice", "Dave", "Alice"}, [3]string{"Bob", "Carol", ""},
		[3]string{"Bob", "Dave", "Bob"}, [3]string{"Carol", "Dave", "Carol"})
}

// Five players, Alice and Bob on top in that order. Carol and Dave are level
// for 3rd (one win over Erin each, a hikiwake between them); Erin is last.
func fivePlayerTieForThird() []state.MatchResult {
	return poolBouts("Pool A",
		[3]string{"Alice", "Bob", "Alice"}, [3]string{"Alice", "Carol", "Alice"},
		[3]string{"Alice", "Dave", "Alice"}, [3]string{"Alice", "Erin", "Alice"},
		[3]string{"Bob", "Carol", "Bob"}, [3]string{"Bob", "Dave", "Bob"},
		[3]string{"Bob", "Erin", "Bob"}, [3]string{"Carol", "Dave", ""},
		[3]string{"Carol", "Erin", "Carol"}, [3]string{"Dave", "Erin", "Dave"})
}

// Five players with a strict top three; Dave and Erin are level for 4th.
func fivePlayerTieForFourth() []state.MatchResult {
	return poolBouts("Pool A",
		[3]string{"Alice", "Bob", "Alice"}, [3]string{"Alice", "Carol", "Alice"},
		[3]string{"Alice", "Dave", "Alice"}, [3]string{"Alice", "Erin", "Alice"},
		[3]string{"Bob", "Carol", "Bob"}, [3]string{"Bob", "Dave", "Bob"},
		[3]string{"Bob", "Erin", "Bob"}, [3]string{"Carol", "Dave", "Carol"},
		[3]string{"Carol", "Erin", "Carol"}, [3]string{"Dave", "Erin", ""})
}

func TestInjectTiebreaker_CutIsThePoolsOwnQualifierCount(t *testing.T) {
	four := []string{"Alice", "Bob", "Carol", "Dave"}
	five := []string{"Alice", "Bob", "Carol", "Dave", "Erin"}
	tests := []struct {
		name    string
		players []string
		matches []state.MatchResult
		opts    []func(*state.Competition)
		want    [][2]string // the injected bouts' sides
	}{
		{
			// The shape the wizard allows: larger-pools locks Winners per pool at
			// 1, so the oversized pool's extra place is its 2nd.
			name:    "larger-pools, 1 winner, 4 players, 2nd and 3rd level: the extra 2nd place is tie-broken",
			players: four, matches: fourPlayerTieForSecond(),
			opts: []func(*state.Competition){largerPools(3), func(c *state.Competition) { c.PoolWinners = 1 }},
			want: [][2]string{{"Bob", "Carol"}},
		},
		{
			name:    "1 winner, no extra qualifiers: a tie for 2nd shares its place with no bout",
			players: four, matches: fourPlayerTieForSecond(),
			opts: []func(*state.Competition){func(c *state.Competition) { c.PoolWinners = 1 }},
		},
		{
			name:    "larger-pools, 4 players, 3rd and 4th level: the extra place is tie-broken",
			players: four, matches: fourPlayerOneTiedPairTB(),
			opts: []func(*state.Competition){largerPools(3)},
			want: [][2]string{{"Carol", "Dave"}},
		},
		{
			name:    "no extra qualifiers: the same tie shares its place with no bout",
			players: four, matches: fourPlayerOneTiedPairTB(),
		},
		{
			name:    "larger-pools but the pool is not oversized: no extra place to break",
			players: four, matches: fourPlayerOneTiedPairTB(),
			opts: []func(*state.Competition){largerPools(4)},
		},
		{
			name:    "larger-pools, 5 players, 3rd and 4th level: the extra place is tie-broken",
			players: five, matches: fivePlayerTieForThird(),
			opts: []func(*state.Competition){largerPools(3)},
			want: [][2]string{{"Carol", "Dave"}},
		},
		{
			name:    "larger-pools, a tie wholly below the extra place gets no bout",
			players: five, matches: fivePlayerTieForFourth(),
			opts: []func(*state.Competition){largerPools(3)},
		},
		{
			name:    "no extra qualifiers: a tie for 3rd shares its place with no bout",
			players: five, matches: fivePlayerTieForThird(),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			comp := &state.Competition{
				ID: "xqtb", Name: "xqtb", Format: state.CompFormatMixed,
				Status: state.CompStatusPools, Courts: []string{"A"}, PoolWinners: 2,
			}
			for _, o := range tc.opts {
				o(comp)
			}
			require.NoError(t, store.SaveCompetition(comp))
			players := make([]helper.Player, len(tc.players))
			for i, n := range tc.players {
				players[i] = helper.Player{Name: n, Dojo: "Dojo " + n}
			}
			bctest.StampIDs(players, tc.matches)
			require.NoError(t, store.SavePools("xqtb", []helper.Pool{{PoolName: "Pool A", Players: players}}))
			require.NoError(t, store.SavePoolMatches("xqtb", tc.matches))

			injected, err := eng.InjectTiebreakerMatches("xqtb")
			require.NoError(t, err)
			got := make([][2]string, len(injected))
			for i, m := range injected {
				got[i] = [2]string{m.SideA, m.SideB}
			}
			if len(tc.want) == 0 {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestInjectPoolDaihyosen_CutIsThePoolsOwnQualifierCount(t *testing.T) {
	tests := []struct {
		name string
		opts []func(*state.Competition)
		want int
	}{
		{"larger-pools, 4 teams, 3rd and 4th level: the extra place gets a representative bout", []func(*state.Competition){largerPools(3)}, 1},
		{"no extra qualifiers: the same tie shares its place", nil, 0},
		{"larger-pools but the pool is not oversized", []func(*state.Competition){largerPools(4)}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, _ := setupTeamPoolWinners(t, "dh-xqtb", []string{"Alpha", "Beta", "Gamma", "Delta"}, 2, fourTeamOneTiedPair(), tc.opts...)
			injected, err := eng.InjectPoolDaihyosenMatches("dh-xqtb")
			require.NoError(t, err)
			require.Len(t, injected, tc.want)
			if tc.want == 1 {
				got := map[string]bool{injected[0].SideA: true, injected[0].SideB: true}
				assert.True(t, got["Gamma"] && got["Delta"], "the representative bout is between the level teams")
			}
		})
	}
}

// A representative bout that leaves the extra-place tie level is a chusen, so the
// operator is offered the lots-draw for it.
func TestChusenStatus_CutIsThePoolsOwnQualifierCount(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []func(*state.Competition)
		want int
	}{
		{"larger-pools: a level extra place needs a chusen", []func(*state.Competition){largerPools(3)}, 1},
		{"no extra qualifiers: nothing to draw", nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compID := "chusen-xqtb"
			eng, store := setupTeamPoolWinners(t, compID, []string{"Alpha", "Beta", "Gamma", "Delta"}, 2, fourTeamOneTiedPair(), tc.opts...)
			_, err := eng.InjectPoolDaihyosenMatches(compID)
			require.NoError(t, err)
			all, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			for i := range all {
				if IsPoolDaihyosenMatchID(all[i].ID) {
					all[i].Status = state.MatchStatusCompleted
					all[i].Winner = "" // hikiwake: the round leaves them level
				}
			}
			require.NoError(t, store.SavePoolMatches(compID, all))
			eng.standingsCache.Delete(compID)
			eng.standingsFlight.Delete(compID)

			report, err := eng.ChusenStatus(compID)
			require.NoError(t, err)
			assert.Len(t, report.Pending, tc.want)
		})
	}
}

// The whole path: the extra place's tie holds the pool open, the bout is
// scored, and its winner is seated in the knockout slot of that place.
func TestLargerPoolsExtraPlaceTie_QualificationWaitsForTheBout(t *testing.T) {
	f := newRQFixture(t, "rq-xqtb", 2, [][]string{{"A1", "A2", "A3", "A4"}, {"B1", "B2", "B3"}}, largerPools(3))
	require.NoError(t, f.store.SaveBracket(f.compID, &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", MatchNumber: 1, SideA: "Pool A-1st", SideB: "Pool B-2nd", PlaceholderA: "Pool A-1st", PlaceholderB: "Pool B-2nd"},
		{ID: "m-r1-1", MatchNumber: 2, SideA: "Pool B-1st", SideB: "Pool A-2nd", PlaceholderA: "Pool B-1st", PlaceholderB: "Pool A-2nd"},
		{ID: "m-r1-2", MatchNumber: 3, SideA: "Pool A-3rd", SideB: "Pool B-3rd", PlaceholderA: "Pool A-3rd", PlaceholderB: "Pool B-3rd"},
	}}}))
	// Pool A: A1 beats all, A2 beats A3 and A4, A3 and A4 draw: level for 3rd,
	// the place only an oversized pool sends.
	for _, w := range [][2]string{{"Pool A-0", "A1"}, {"Pool A-1", "A1"}, {"Pool A-2", "A1"}, {"Pool A-3", "A2"}, {"Pool A-4", "A2"}} {
		f.scorePool(w[0], w[1])
	}
	require.NoError(t, f.write("Pool A-5", &state.MatchResult{
		SideA: "A3", SideB: "A4", SideAID: rqID("A3"), SideBID: rqID("A4"),
		Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
	}))
	for _, w := range [][2]string{{"Pool B-0", "B1"}, {"Pool B-1", "B1"}, {"Pool B-2", "B2"}} {
		f.scorePool(w[0], w[1])
	}

	_, err := f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	var tbs []state.MatchResult
	matches, err := f.store.LoadPoolMatches(f.compID)
	require.NoError(t, err)
	for _, m := range matches {
		if IsTiebreakerMatchID(m.ID) {
			tbs = append(tbs, m)
		}
	}
	require.Len(t, tbs, 1, "the tie over the extra qualifier place is owed a tie-break bout")
	assert.ElementsMatch(t, []string{"A3", "A4"}, []string{tbs[0].SideA, tbs[0].SideB})

	m, side := f.slot("Pool A-3rd")
	name, _ := sideOf(m, side)
	assert.Equal(t, "Pool A-3rd", name, "the place stays open while the bout is unplayed")

	f.scorePool(tbs[0].ID, "A4")
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	m, side = f.slot("Pool A-3rd")
	name, id := sideOf(m, side)
	assert.Equal(t, "A4", name, "the bout's winner takes the extra place")
	assert.Equal(t, rqID("A4"), id)
	m, side = f.slot("Pool A-2nd")
	name, _ = sideOf(m, side)
	assert.Equal(t, "A2", name, "the rest of the pool is seated once the bout is played")
}
