package engine

import (
	"fmt"
	"strings"
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

// bc-fbtb: under fill-bracket some pools are DRAFTED to send their 2nd as well
// (helper.SelectFillBracketDraftIndices), and Competition.QualifiersForPool
// deliberately cannot say which. A tie over a drafted pool's 2nd decides who
// takes the "Pool X-2nd" knockout place, so it is owed a bout exactly like a
// tie over any other qualifying place; the same tie in a pool that was not
// drafted shares its place. The tests draw through the real pipeline and read
// the drafted pool off the stored bracket, so they fail if the cut stops
// following the draw.

// startFillBracket draws an 11-entrant fill-bracket competition (Winners per
// pool 1, min 3: pools of 3, 4 and 4, one 2nd drafted from an oversized pool)
// and returns the drafted pool and the oversized pool left undrafted.
func startFillBracket(t *testing.T, compID string, team bool) (eng *Engine, store *state.Store, drafted, undrafted string) {
	t.Helper()
	eng, store, _ = setupTestEngine(t)
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.PoolWinners = 1
		c.ExtraQualifiers = state.ExtraQualifiersFillBracket
		if team {
			c.Kind = "team"
			c.TeamSize = 2
		}
	})
	names := make([]string, 11)
	for i := range names {
		names[i] = fmt.Sprintf("Player%02d", i)
	}
	saveTestParticipants(t, store, compID, names)
	require.NoError(t, eng.StartCompetition(compID))

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	for _, m := range bracket.Rounds[0] {
		for _, label := range []string{m.PlaceholderA, m.PlaceholderB} {
			if strings.HasSuffix(label, "-2nd") {
				require.Empty(t, drafted, "exactly one 2nd is drafted")
				drafted = qualifierLabelPool(label)
			}
		}
	}
	require.NotEmpty(t, drafted, "the draw drafts one pool's 2nd")
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	for _, p := range pools {
		if p.PoolName != drafted && len(p.Players) == 4 {
			undrafted = p.PoolName
		}
	}
	require.NotEmpty(t, undrafted, "an oversized pool is left undrafted")
	return eng, store, drafted, undrafted
}

// scoreFillBracketPools completes the pool matches: the lower-seated player of
// each pair wins, except that in tiePool the 2nd and 3rd players draw each
// other (level for 2nd). Pools other than tiePool are scored only when all is
// set, so a test can leave them incomplete.
func scoreFillBracketPools(t *testing.T, eng *Engine, store *state.Store, compID, tiePool string, team, all bool) {
	t.Helper()
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	order, poolOf := map[string]int{}, map[string]string{}
	for _, p := range pools {
		for i, pl := range p.Players {
			order[pl.Name], poolOf[pl.Name] = i, p.PoolName
		}
	}
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range matches {
		m := &matches[i]
		pn := poolOf[m.SideA]
		if pn != tiePool && !all {
			continue
		}
		a, b := order[m.SideA], order[m.SideB]
		winner, winnerID := m.SideA, m.SideAID
		if b < a {
			winner, winnerID = m.SideB, m.SideBID
		}
		if pn == tiePool && a*b == 2 && a+b == 3 {
			winner, winnerID = "", ""
		}
		m.Status = state.MatchStatusCompleted
		m.Winner, m.WinnerID = winner, winnerID
		if winner == "" {
			m.Decision = string(domain.DecisionHikiwake)
		}
		if team {
			m.SubResults = []state.SubMatchResult{
				{Position: 1, SideA: m.SideA, SideB: m.SideB, Winner: winner, Decision: m.Decision},
				{Position: 2, SideA: m.SideA, SideB: m.SideB, Winner: winner, Decision: m.Decision},
			}
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)
}

// drawnPoolNames returns pool's player names in draw order.
func drawnPoolNames(t *testing.T, store *state.Store, compID, pool string) []string {
	t.Helper()
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	for _, p := range pools {
		if p.PoolName == pool {
			names := make([]string, len(p.Players))
			for i, pl := range p.Players {
				names[i] = pl.Name
			}
			return names
		}
	}
	require.Failf(t, "pool not found", "%s", pool)
	return nil
}

func TestFillBracketDraftedSecondTie_OwesATieBreakBout(t *testing.T) {
	for _, tc := range []struct {
		name    string
		drafted bool
	}{
		{"a tie for 2nd in the drafted pool is tie-broken", true},
		{"the same tie in an oversized pool that was not drafted shares its place", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, drafted, undrafted := startFillBracket(t, "fbtb-ind", false)
			pool := undrafted
			if tc.drafted {
				pool = drafted
			}
			scoreFillBracketPools(t, eng, store, "fbtb-ind", pool, false, false)

			injected, err := eng.InjectTiebreakerMatches("fbtb-ind")
			require.NoError(t, err)
			if !tc.drafted {
				assert.Empty(t, injected)
				return
			}
			require.Len(t, injected, 1)
			names := drawnPoolNames(t, store, "fbtb-ind", pool)
			assert.ElementsMatch(t, []string{names[1], names[2]}, []string{injected[0].SideA, injected[0].SideB})
			assert.True(t, IsTiebreakerMatchID(injected[0].ID) && strings.HasPrefix(injected[0].ID, pool+"-TB-"), "got %q", injected[0].ID)
		})
	}
}

func TestFillBracketDraftedSecondTie_TeamPoolGetsADaihyosen(t *testing.T) {
	for _, tc := range []struct {
		name    string
		drafted bool
		want    int
	}{
		{"drafted pool: the level teams play a daihyosen", true, 1},
		{"undrafted pool: the level teams share the place", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, drafted, undrafted := startFillBracket(t, "fbtb-team", true)
			pool := undrafted
			if tc.drafted {
				pool = drafted
			}
			scoreFillBracketPools(t, eng, store, "fbtb-team", pool, true, false)

			injected, err := eng.InjectPoolDaihyosenMatches("fbtb-team")
			require.NoError(t, err)
			require.Len(t, injected, tc.want)
		})
	}
}

func TestFillBracketDraftedSecondTie_ChusenWaitsOnlyInTheDraftedPool(t *testing.T) {
	for _, tc := range []struct {
		name    string
		drafted bool
		want    int
	}{
		{"drafted pool: a daihyosen that leaves them level needs a chusen", true, 1},
		{"undrafted pool: nothing to draw", false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, drafted, undrafted := startFillBracket(t, "fbtb-chusen", true)
			pool := undrafted
			if tc.drafted {
				pool = drafted
			}
			scoreFillBracketPools(t, eng, store, "fbtb-chusen", pool, true, false)
			_, err := eng.InjectPoolDaihyosenMatches("fbtb-chusen")
			require.NoError(t, err)
			all, err := store.LoadPoolMatches("fbtb-chusen")
			require.NoError(t, err)
			for i := range all {
				if IsPoolDaihyosenMatchID(all[i].ID) {
					all[i].Status = state.MatchStatusCompleted
					all[i].Winner = "" // hikiwake: the round leaves them level
				}
			}
			require.NoError(t, store.SavePoolMatches("fbtb-chusen", all))
			eng.standingsCache.Delete("fbtb-chusen")
			eng.standingsFlight.Delete("fbtb-chusen")

			report, err := eng.ChusenStatus("fbtb-chusen")
			require.NoError(t, err)
			assert.Len(t, report.Pending, tc.want)
		})
	}
}

func TestFillBracketDraftedSecondTie_QualificationWaitsForTheBout(t *testing.T) {
	eng, store, drafted, _ := startFillBracket(t, "fbtb-wait", false)
	scoreFillBracketPools(t, eng, store, "fbtb-wait", drafted, false, true)
	names := drawnPoolNames(t, store, "fbtb-wait", drafted)
	label := drafted + "-2nd"

	// slot reads who sits in the knockout place the drafted pool's 2nd fills.
	slot := func() string {
		bracket, err := store.LoadBracket("fbtb-wait")
		require.NoError(t, err)
		for _, m := range bracket.Rounds[0] {
			if m.PlaceholderA == label {
				return m.SideA
			}
			if m.PlaceholderB == label {
				return m.SideB
			}
		}
		require.Failf(t, "slot not found", "%s", label)
		return ""
	}

	_, err := eng.MaybeAutoCompletePools("fbtb-wait")
	require.NoError(t, err)
	matches, err := store.LoadPoolMatches("fbtb-wait")
	require.NoError(t, err)
	var tbs []state.MatchResult
	for _, m := range matches {
		if IsTiebreakerMatchID(m.ID) {
			tbs = append(tbs, m)
		}
	}
	require.Len(t, tbs, 1, "the tie over the drafted 2nd is owed a tie-break bout")
	assert.ElementsMatch(t, []string{names[1], names[2]}, []string{tbs[0].SideA, tbs[0].SideB})
	assert.Equal(t, label, slot(), "the place stays open while the bout is unplayed")

	tbs[0].Status = state.MatchStatusCompleted
	tbs[0].Winner, tbs[0].WinnerID = names[2], tbs[0].SideAID
	if tbs[0].SideB == names[2] {
		tbs[0].WinnerID = tbs[0].SideBID
	}
	for i := range matches {
		if matches[i].ID == tbs[0].ID {
			matches[i] = tbs[0]
		}
	}
	require.NoError(t, store.SavePoolMatches("fbtb-wait", matches))
	eng.standingsCache.Delete("fbtb-wait")
	eng.standingsFlight.Delete("fbtb-wait")
	_, err = eng.MaybeAutoCompletePools("fbtb-wait")
	require.NoError(t, err)
	assert.Equal(t, names[2], slot(), "the bout's winner takes the drafted 2nd place")
}

// tiebreakBoutsOf lists the tie-break bouts a competition's pool matches hold.
func tiebreakBoutsOf(t *testing.T, store *state.Store, compID string) []state.MatchResult {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var tbs []state.MatchResult
	for _, m := range matches {
		if IsTiebreakerMatchID(m.ID) {
			tbs = append(tbs, m)
		}
	}
	return tbs
}

// bothPlayed finds the stored pool match whose sides are a and b.
func bothPlayed(t *testing.T, store *state.Store, compID, a, b string) string {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		if (m.SideA == a && m.SideB == b) || (m.SideA == b && m.SideB == a) {
			return m.ID
		}
	}
	require.Failf(t, "match not found", "%s v %s", a, b)
	return ""
}

// bc-fbtb / bc-xqtb review: a CORRECTION that leaves two players level on the
// pool's extra place goes through the requalification, which judges the tie
// against the pool's own cut (poolTieBreakCut), not the uniform Winners per
// pool. A pool whose extra place is cut by Winners per pool alone would seat
// one of the two by sort order.
func TestRequalify_CorrectionTieOnLargerPoolsExtraPlace_WaitsForTheBout(t *testing.T) {
	f := newRQFixture(t, "rq-xqtb-fix", 1, [][]string{
		{"A1", "A2", "A3", "A4"}, {"B1", "B2", "B3"}, {"C1", "C2", "C3"},
	}, largerPools(3))
	require.NoError(t, f.store.SaveBracket(f.compID, &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", MatchNumber: 1, SideA: "Pool A-1st", SideB: "Pool C-1st", PlaceholderA: "Pool A-1st", PlaceholderB: "Pool C-1st"},
		{ID: "m-r1-1", MatchNumber: 2, SideA: "Pool B-1st", SideB: "Pool A-2nd", PlaceholderA: "Pool B-1st", PlaceholderB: "Pool A-2nd"},
	}}}))
	for _, w := range [][2]string{
		{"Pool A-0", "A1"}, {"Pool A-1", "A1"}, {"Pool A-2", "A1"},
		{"Pool A-3", "A2"}, {"Pool A-4", "A2"}, {"Pool A-5", "A3"},
		{"Pool B-0", "B1"}, {"Pool B-1", "B1"}, {"Pool B-2", "B2"},
		{"Pool C-0", "C1"}, {"Pool C-1", "C1"}, {"Pool C-2", "C2"},
	} {
		f.scorePool(w[0], w[1])
	}
	f.resolve()
	setCompStatus(t, f.store, f.compID, state.CompStatusKnockout)
	m, side := f.slot("Pool A-2nd")
	name, _ := sideOf(m, side)
	require.Equal(t, "A2", name, "before the correction A2 is the clear 2nd")

	// Correct A2 v A3 to a hikiwake: A2 and A3 are level for the extra place.
	require.NoError(t, f.write("Pool A-3", &state.MatchResult{
		SideA: "A2", SideB: "A3", SideAID: rqID("A2"), SideBID: rqID("A3"),
		Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
	}))
	m, side = f.slot("Pool A-2nd")
	name, id := sideOf(m, side)
	assert.Equal(t, "Pool A-2nd", name, "a tied extra place is not seated by sort order")
	assert.Empty(t, id)

	_, err := f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	tbs := tiebreakBoutsOf(t, f.store, f.compID)
	require.Len(t, tbs, 1, "the tie over the extra place is owed a tie-break bout")
	assert.ElementsMatch(t, []string{"A2", "A3"}, []string{tbs[0].SideA, tbs[0].SideB})

	f.scorePool(tbs[0].ID, "A3")
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	m, side = f.slot("Pool A-2nd")
	name, id = sideOf(m, side)
	assert.Equal(t, "A3", name, "the bout's winner takes the extra place")
	assert.Equal(t, rqID("A3"), id)
}

// The same correction over a fill-bracket drafted pool's 2nd, drawn through the
// real pipeline.
func TestRequalify_CorrectionTieOnFillBracketDraftedSecond_WaitsForTheBout(t *testing.T) {
	const compID = "fbtb-fix"
	eng, store, drafted, _ := startFillBracket(t, compID, false)
	// A clean result everywhere: the drafted pool's order is the draw order.
	scoreFillBracketPools(t, eng, store, compID, "", false, true)
	names := drawnPoolNames(t, store, compID, drafted)
	f := &rqFix{t: t, eng: eng, store: store, compID: compID}
	label := drafted + "-2nd"

	_, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	m, side := f.slot(label)
	name, _ := sideOf(m, side)
	require.Equal(t, names[1], name, "before the correction the draw's second seed holds the drafted 2nd")

	// Correct the 2nd v 3rd result to a hikiwake: they are level for 2nd.
	matchID := bothPlayed(t, store, compID, names[1], names[2])
	prior := loadPoolMatchByID(t, store, compID, matchID)
	require.NotNil(t, prior)
	require.NoError(t, f.write(matchID, &state.MatchResult{
		SideA: prior.SideA, SideB: prior.SideB, SideAID: prior.SideAID, SideBID: prior.SideBID,
		Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
	}))
	m, side = f.slot(label)
	name, id := sideOf(m, side)
	assert.Equal(t, label, name, "a tied drafted 2nd is not seated by sort order")
	assert.Empty(t, id)

	_, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	tbs := tiebreakBoutsOf(t, store, compID)
	require.Len(t, tbs, 1, "the tie over the drafted 2nd is owed a tie-break bout")
	assert.ElementsMatch(t, []string{names[1], names[2]}, []string{tbs[0].SideA, tbs[0].SideB})

	f.scorePool(tbs[0].ID, names[2])
	_, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	m, side = f.slot(label)
	name, _ = sideOf(m, side)
	assert.Equal(t, names[2], name, "the bout's winner takes the drafted 2nd place")
}
