package engine

import (
	"fmt"
	"sort"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsTiebreakerMatchID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"Pool A-TB-0", true},
		{"Pool A-TB-1", true},
		{"Pool B-TB-42", true},
		{"Pool A-East-TB-0", true}, // hyphenated pool name
		{"Pool A-0", false},
		{"Pool A-1", false},
		{"Pool A-TB", false},    // no index after TB
		{"Pool A-T-0", false},   // different prefix
		{"Pool A-TBx-0", false}, // wrong prefix
		{"TB-0", false},         // no pool name separator
		{"", false},
		// Same sibling scenario as IsPoolDaihyosenMatchID: a pool literally
		// named "Pool A-TB-East" must not have its regular match ids
		// misclassified as tiebreaker bouts.
		{"Pool A-TB-East-0", false},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			assert.Equal(t, tc.want, IsTiebreakerMatchID(tc.id))
		})
	}
}

// namesAt resolves a tied position group back to competitor names, for stable
// assertions when the input was sorted by Points.
func namesAt(standings []state.PlayerStanding, positions []int) []string {
	out := make([]string, len(positions))
	for i, idx := range positions {
		out[i] = standings[idx].Player.Name
	}
	return out
}

// pointsStandings builds standings with the given Points values (names P0..Pn).
func pointsStandings(points ...int) []state.PlayerStanding {
	s := make([]state.PlayerStanding, len(points))
	for i, p := range points {
		s[i] = state.PlayerStanding{Player: domain.Player{Name: fmt.Sprintf("P%d", i)}, Points: p}
	}
	return s
}

// TestDetectPoolTies_Positions exhaustively pins the position groups returned
// for every shape of (already Points-sorted) standings: no ties, ties at the
// top / middle / bottom, multiple groups (adjacent and with gaps), all-tied,
// negatives and zeros, and the empty/single degenerate inputs.
func TestDetectPoolTies_Positions(t *testing.T) {
	tests := []struct {
		name   string
		points []int
		want   [][]int
	}{
		{"empty", []int{}, nil},
		{"single", []int{100}, nil},
		{"two distinct", []int{200, 100}, nil},
		{"all distinct", []int{300, 200, 100}, nil},
		{"two tied", []int{100, 100}, [][]int{{0, 1}}},
		{"three tied", []int{100, 100, 100}, [][]int{{0, 1, 2}}},
		{"tie at top", []int{200, 200, 100}, [][]int{{0, 1}}},
		{"tie at bottom", []int{300, 100, 100}, [][]int{{1, 2}}},
		{"tie in middle", []int{300, 200, 200, 100}, [][]int{{1, 2}}},
		{"two adjacent groups", []int{300, 300, 200, 200}, [][]int{{0, 1}, {2, 3}}},
		{"two groups with a gap", []int{300, 300, 250, 200, 200}, [][]int{{0, 1}, {3, 4}}},
		{"all four tied", []int{100, 100, 100, 100}, [][]int{{0, 1, 2, 3}}},
		{"group single group", []int{500, 500, 400, 300, 300, 300}, [][]int{{0, 1}, {3, 4, 5}}},
		{"negative points tie", []int{-50, -50}, [][]int{{0, 1}}},
		{"zero points all tied", []int{0, 0, 0}, [][]int{{0, 1, 2}}},
		{"single then pair", []int{300, 200, 200}, [][]int{{1, 2}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, detectPoolTies(pointsStandings(tc.points...)))
		})
	}
}

// TestDetectPoolTies_TeamCriteria proves that, for TEAM standings, two teams
// are flagged tied only when they match on the full chain
// W>L>T>IV>IL>IT>PW>PL: a difference in any single criterion (however far down)
// separates them via the packed Points, so no daihyosen would be injected.
func TestDetectPoolTies_TeamCriteria(t *testing.T) {
	type team struct {
		name                         string
		w, l, td, iv, il, it, pw, pl int
	}
	mk := func(teams ...team) []state.PlayerStanding {
		s := make([]state.PlayerStanding, len(teams))
		for i, tm := range teams {
			ps := state.PlayerStanding{
				Player: domain.Player{Name: tm.name},
				Wins:   tm.w, Losses: tm.l, Draws: tm.td,
				IndividualWins: tm.iv, IndividualLosses: tm.il, IndividualDraws: tm.it,
				PointsWon: tm.pw, PointsLost: tm.pl,
			}
			ps.Points = teamStandingPoints(ps)
			s[i] = ps
		}
		sort.SliceStable(s, func(a, b int) bool { return s[a].Points > s[b].Points })
		return s
	}

	t.Run("identical on every criterion -> tied", func(t *testing.T) {
		s := mk(
			team{name: "A", w: 1, td: 1, iv: 2, it: 2, pw: 4},
			team{name: "B", w: 1, td: 1, iv: 2, it: 2, pw: 4},
			team{name: "C", l: 2, il: 4, pl: 8},
		)
		groups := detectPoolTies(s)
		require.Len(t, groups, 1)
		assert.ElementsMatch(t, []string{"A", "B"}, namesAt(s, groups[0]))
	})
	t.Run("same W/L/T/IV but different PW -> not tied", func(t *testing.T) {
		s := mk(
			team{name: "A", w: 1, td: 1, iv: 2, pw: 5},
			team{name: "B", w: 1, td: 1, iv: 2, pw: 4},
		)
		assert.Empty(t, detectPoolTies(s))
		assert.Equal(t, "A", s[0].Player.Name)
	})
	t.Run("same down to PW but different PL -> not tied", func(t *testing.T) {
		s := mk(
			team{name: "A", w: 1, iv: 1, pw: 2, pl: 0},
			team{name: "B", w: 1, iv: 1, pw: 2, pl: 1},
		)
		assert.Empty(t, detectPoolTies(s))
		assert.Equal(t, "A", s[0].Player.Name)
	})
	t.Run("differ only on IT (individual draws) -> not tied", func(t *testing.T) {
		s := mk(
			team{name: "A", w: 1, iv: 1, it: 2},
			team{name: "B", w: 1, iv: 1, it: 1},
		)
		assert.Empty(t, detectPoolTies(s))
	})
	t.Run("one more team win beats a large PW deficit -> not tied", func(t *testing.T) {
		s := mk(
			team{name: "A", w: 2, pw: 0},
			team{name: "B", w: 1, pw: 99},
		)
		assert.Empty(t, detectPoolTies(s))
		assert.Equal(t, "A", s[0].Player.Name)
	})
	t.Run("two separate two-way ties -> two groups", func(t *testing.T) {
		s := mk(
			team{name: "A", w: 2, iv: 3},
			team{name: "B", w: 2, iv: 3},
			team{name: "C", w: 1, iv: 1},
			team{name: "D", w: 1, iv: 1},
		)
		groups := detectPoolTies(s)
		require.Len(t, groups, 2)
		assert.ElementsMatch(t, []string{"A", "B"}, namesAt(s, groups[0]))
		assert.ElementsMatch(t, []string{"C", "D"}, namesAt(s, groups[1]))
	})
}

// TestDetectPoolTies_IndividualCriteria mirrors the team test for INDIVIDUAL
// standings (chain W>L>D>ipponsGiven>ipponsTaken).
func TestDetectPoolTies_IndividualCriteria(t *testing.T) {
	type ind struct {
		name              string
		w, l, d, ig, itak int
	}
	mk := func(players ...ind) []state.PlayerStanding {
		s := make([]state.PlayerStanding, len(players))
		for i, p := range players {
			ps := state.PlayerStanding{
				Player: domain.Player{Name: p.name},
				Wins:   p.w, Losses: p.l, Draws: p.d,
				IpponsGiven: p.ig, IpponsTaken: p.itak,
			}
			ps.Points = individualStandingPoints(ps)
			s[i] = ps
		}
		sort.SliceStable(s, func(a, b int) bool { return s[a].Points > s[b].Points })
		return s
	}

	t.Run("identical -> tied", func(t *testing.T) {
		s := mk(
			ind{name: "A", w: 2, ig: 4, itak: 1},
			ind{name: "B", w: 2, ig: 4, itak: 1},
			ind{name: "C", l: 2, itak: 4},
		)
		groups := detectPoolTies(s)
		require.Len(t, groups, 1)
		assert.ElementsMatch(t, []string{"A", "B"}, namesAt(s, groups[0]))
	})
	t.Run("same W/L/D but different ippons given -> not tied", func(t *testing.T) {
		s := mk(
			ind{name: "A", w: 2, ig: 5},
			ind{name: "B", w: 2, ig: 4},
		)
		assert.Empty(t, detectPoolTies(s))
		assert.Equal(t, "A", s[0].Player.Name)
	})
	t.Run("same down to given but different taken -> not tied", func(t *testing.T) {
		s := mk(
			ind{name: "A", w: 2, ig: 4, itak: 1},
			ind{name: "B", w: 2, ig: 4, itak: 2},
		)
		assert.Empty(t, detectPoolTies(s))
		assert.Equal(t, "A", s[0].Player.Name)
	})
	t.Run("one more win beats a large ippon deficit -> not tied", func(t *testing.T) {
		s := mk(
			ind{name: "A", w: 2, ig: 0},
			ind{name: "B", w: 1, ig: 50},
		)
		assert.Empty(t, detectPoolTies(s))
		assert.Equal(t, "A", s[0].Player.Name)
	})
}

// TestStandingPoints_CriteriaPriority proves the packing preserves strict
// criterion priority: a one-unit advantage in a higher criterion always
// outranks any (realistic) deficit in every lower criterion combined.
func TestStandingPoints_CriteriaPriority(t *testing.T) {
	t.Run("team", func(t *testing.T) {
		// big = maxed-out lower tiers; each higher-tier bump must still win.
		big := func(s state.PlayerStanding) state.PlayerStanding {
			s.IndividualWins += 90
			s.IndividualDraws += 90
			s.PointsWon += 90
			return s
		}
		// +1 W beats any lower deficit
		assert.Greater(t,
			teamStandingPoints(state.PlayerStanding{Wins: 1}),
			teamStandingPoints(big(state.PlayerStanding{Wins: 0})))
		// fewer L (at equal W) beats lower deficit
		assert.Greater(t,
			teamStandingPoints(state.PlayerStanding{Wins: 1, Losses: 0}),
			teamStandingPoints(big(state.PlayerStanding{Wins: 1, Losses: 1})))
		// +1 IV beats any PW/PL deficit
		assert.Greater(t,
			teamStandingPoints(state.PlayerStanding{IndividualWins: 1}),
			teamStandingPoints(state.PlayerStanding{IndividualWins: 0, PointsWon: 90}))
		// +1 PW beats a PL deficit
		assert.Greater(t,
			teamStandingPoints(state.PlayerStanding{PointsWon: 1, PointsLost: 1}),
			teamStandingPoints(state.PlayerStanding{PointsWon: 0, PointsLost: 0}))
	})
	t.Run("individual", func(t *testing.T) {
		// +1 W beats any ippon advantage
		assert.Greater(t,
			individualStandingPoints(state.PlayerStanding{Wins: 1}),
			individualStandingPoints(state.PlayerStanding{Wins: 0, IpponsGiven: 90}))
		// +1 ippon given beats an ippons-taken deficit
		assert.Greater(t,
			individualStandingPoints(state.PlayerStanding{IpponsGiven: 1, IpponsTaken: 1}),
			individualStandingPoints(state.PlayerStanding{IpponsGiven: 0, IpponsTaken: 0}))
	})
}

// TestStandingsAt covers position->standing resolution: order preservation and
// defensive skipping of out-of-range indices.
func TestStandingsAt(t *testing.T) {
	s := []state.PlayerStanding{
		{Player: domain.Player{Name: "A"}},
		{Player: domain.Player{Name: "B"}},
		{Player: domain.Player{Name: "C"}},
	}
	t.Run("preserves position order", func(t *testing.T) {
		got := standingsAt(s, []int{2, 0})
		require.Len(t, got, 2)
		assert.Equal(t, "C", got[0].Player.Name)
		assert.Equal(t, "B", s[1].Player.Name) // input untouched
		assert.Equal(t, "A", got[1].Player.Name)
	})
	t.Run("skips out-of-range indices", func(t *testing.T) {
		got := standingsAt(s, []int{0, 9, 2, -1})
		require.Len(t, got, 2)
		assert.Equal(t, []string{"A", "C"}, []string{got[0].Player.Name, got[1].Player.Name})
	})
	t.Run("empty positions -> empty", func(t *testing.T) {
		assert.Empty(t, standingsAt(s, nil))
	})
}

func TestGenerateTiebreakerMatches_TwoWay(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{Name: "Alice"}},
		{Player: domain.Player{Name: "Bob"}},
	}
	matches := generateTiebreakerMatches("Pool A", group, 0, "A", map[string]bool{})
	require.Len(t, matches, 1)
	m := matches[0]
	assert.Equal(t, "Pool A-TB-0", m.ID)
	assert.Empty(t, m.Decision, "injected TB match must have empty Decision, ID convention identifies it")
	assert.Equal(t, state.MatchStatusScheduled, m.Status)
	assert.Equal(t, "A", m.Court)
	assert.ElementsMatch(t, []string{m.SideA, m.SideB}, []string{"Alice", "Bob"})
}

func TestGenerateTiebreakerMatches_ThreeWay(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{Name: "A"}},
		{Player: domain.Player{Name: "B"}},
		{Player: domain.Player{Name: "C"}},
	}
	matches := generateTiebreakerMatches("Pool X", group, 0, "B", map[string]bool{})
	// 3-way round-robin = 3 matches
	require.Len(t, matches, 3)
	assert.Equal(t, "Pool X-TB-0", matches[0].ID)
	assert.Equal(t, "Pool X-TB-1", matches[1].ID)
	assert.Equal(t, "Pool X-TB-2", matches[2].ID)
}

func TestGenerateTiebreakerMatches_ExistingCountOffset(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{Name: "A"}},
		{Player: domain.Player{Name: "B"}},
	}
	matches := generateTiebreakerMatches("Pool X", group, 5, "A", map[string]bool{})
	require.Len(t, matches, 1)
	assert.Equal(t, "Pool X-TB-5", matches[0].ID)
}

func TestGenerateTiebreakerMatches_SkipsExistingPairs(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{Name: "A"}},
		{Player: domain.Player{Name: "B"}},
		{Player: domain.Player{Name: "C"}},
	}
	existingPairs := map[string]bool{tiebreakerPairKey("A", "B"): true}
	matches := generateTiebreakerMatches("Pool X", group, 1, "A", existingPairs)
	// Only A-C and B-C should be generated
	require.Len(t, matches, 2)
}

func TestInjectTiebreakerMatches_NoTie(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "inject-no-tie"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "No Tie",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"}, {Name: "Charlie"},
		}},
	}))
	// Alice wins both, Bob beats Charlie, distinct standings, no tie
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "Pool A-2", SideA: "Bob", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Bob"},
	}))

	injected, err := eng.InjectTiebreakerMatches(compID)
	require.NoError(t, err)
	assert.Empty(t, injected, "no tiebreaker matches expected when standings are distinct")
}

func TestInjectTiebreakerMatches_TwoWayTie(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "inject-tie"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Two-way Tie",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"}, {Name: "Charlie"},
		}},
	}))
	// Alice wins both, Bob and Charlie both lose once, both 0 ippons: tie
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A"},
		{ID: "Pool A-2", SideA: "Bob", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake), Court: "A"},
	}))

	injected, err := eng.InjectTiebreakerMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 1, "one tiebreaker match expected for a two-way tie")

	m := injected[0]
	assert.True(t, IsTiebreakerMatchID(m.ID), "injected match must have a TB ID")
	assert.Empty(t, m.Decision, "injected TB match must have empty Decision, ID convention identifies it")
	assert.Equal(t, state.MatchStatusScheduled, m.Status)
	assert.Equal(t, "A", m.Court)
}

func TestInjectTiebreakerMatches_Idempotent(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "inject-idempotent"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Idempotent Test",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"},
		}},
	}))
	// A draw → both have identical stats → tie
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake)},
	}))

	first, err := eng.InjectTiebreakerMatches(compID)
	require.NoError(t, err)
	require.Len(t, first, 1)

	second, err := eng.InjectTiebreakerMatches(compID)
	require.NoError(t, err)
	assert.Empty(t, second, "second call must not inject duplicate tiebreaker matches")

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	tbCount := 0
	for _, m := range all {
		if IsTiebreakerMatchID(m.ID) {
			tbCount++
		}
	}
	assert.Equal(t, 1, tbCount, "exactly one TB match must exist after idempotent injection")
}

func TestMaybeAutoCompletePools_TiesDetected(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-tie-detect"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Tie Detect",
		Format: state.CompFormatLeague,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"},
		}},
	}))
	// Draw → tie
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake)},
	}))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTiebreakInjected, outcome, "tie should inject tiebreaker matches")

	// Competition must still be in Pools status
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status)

	// A TB match must now exist
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	tbCount := 0
	for _, m := range all {
		if IsTiebreakerMatchID(m.ID) {
			tbCount++
		}
	}
	assert.Equal(t, 1, tbCount)
}

func TestMaybeAutoCompletePools_TiebreakersIncomplete(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-tb-pending"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "TB Pending",
		Format: state.CompFormatLeague,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	// Regular match complete, TB match still scheduled
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake)},
		{ID: "Pool A-TB-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled,
			Decision: string(domain.DecisionIpponShobu)},
	}))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "pending TB match must block auto-complete")
}

func TestMaybeAutoCompletePools_TiebreakersComplete(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-tb-done"

	// Use league format: league auto-completes after all pool matches (including TB).
	// Mixed format does not auto-complete after pools; the knockout fills in incrementally.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "TB Done",
		Format: state.CompFormatLeague,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	// Regular match complete + TB match complete
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake)},
		{ID: "Pool A-TB-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice",
			Decision: string(domain.DecisionIpponShobu)},
	}))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "completed TB match must allow completion (league format)")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status)
}

func TestComputeStandings_TBExcludedFromStats(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tb-excluded"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "TB Excluded",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice"}, {Name: "Bob"}}},
	}))
	// Regular draw + TB win for Alice
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake)},
		{ID: "Pool A-TB-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice",
			Decision: string(domain.DecisionIpponShobu)},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, 2)

	// W/L/D must reflect only the regular draw match, not the TB win
	for _, s := range poolA {
		assert.Equalf(t, 0, s.Wins, "%s: TB win must not count as a regular win", s.Player.Name)
		assert.Equalf(t, 0, s.Losses, "%s: must have no regular losses", s.Player.Name)
		assert.Equalf(t, 1, s.Draws, "%s: draw from regular match must be counted", s.Player.Name)
	}
}

// TestInjectTiebreakerMatches_PreservesExistingScheduledAt is the
// regression guard for the 🔴 bug fix: assignPoolMatchSlots must not
// overwrite operator-adjusted ScheduledAt values on pre-existing matches.
// Only newly injected TB matches (empty ScheduledAt) should receive fresh
// slot assignments.
func TestInjectTiebreakerMatches_PreservesExistingScheduledAt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "inject-preserves-time"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        compID,
		Name:      "Preserves Time",
		Format:    state.CompFormatMixed,
		Status:    state.CompStatusPools,
		Courts:    []string{"A"},
		StartTime: "09:00",
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"},
		}},
	}))

	// Simulate an operator adjusting the match time well outside the
	// auto-assigned window (~09:00) so the assertion is unambiguous.
	const operatorTime = "14:30"
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake),
			Court:    "A", ScheduledAt: operatorTime},
	}))

	injected, err := eng.InjectTiebreakerMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 1, "one TB match expected")

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	for _, m := range all {
		if IsTiebreakerMatchID(m.ID) {
			assert.NotEmpty(t, m.ScheduledAt,
				"TB match must receive an auto-assigned slot")
		} else {
			assert.Equal(t, operatorTime, m.ScheduledAt,
				"existing match %s must retain its operator-adjusted ScheduledAt", m.ID)
		}
	}
}

// TestMaybeAutoCompletePools_NoTies verifies the backward-compatible
// path: when all regular matches are complete with no ties, the
// competition transitions directly to CompStatusComplete without
// injecting any tiebreaker matches.
func TestMaybeAutoCompletePools_NoTies(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "auto-no-ties"

	// Use league format: league auto-completes after all pool matches without ties.
	// Mixed format does not auto-complete after pools; the knockout fills in incrementally.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "No Ties",
		Format: state.CompFormatLeague,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"}, {Name: "Charlie"},
		}},
	}))
	// Alice wins all → distinct standings (no tie)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "Pool A-2", SideA: "Bob", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Bob"},
	}))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome,
		"no ties → must transition directly to completed without TB injection (league format)")

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, comp.Status)

	// Confirm no TB matches were injected
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range all {
		assert.False(t, IsTiebreakerMatchID(m.ID),
			"no TB matches expected when standings are distinct")
	}
}

// TestComputeStandings_MultiGroupTBSortIsolation verifies that when a
// pool has two separate tied groups each with their own TB match, the
// per-group win-count scoping correctly ranks each group's TB winner
// above their opponent, independently of the other group's results.
func TestComputeStandings_MultiGroupTBSortIsolation(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "multi-group-isolation"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Multi-Group Isolation",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	// 5-player pool: Alpha first, then {Beta,Gamma} tied, then {Delta,Epsilon} tied.
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alpha"}, {Name: "Beta"}, {Name: "Gamma"},
			{Name: "Delta"}, {Name: "Epsilon"},
		}},
	}))
	// Alpha beats everyone. Beta and Gamma both beat Delta and Epsilon and
	// draw each other → same Points. Delta and Epsilon draw each other and
	// both lose the same matches → same (lower) Points.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		// Alpha wins all regular matches
		{ID: "Pool A-0", SideA: "Alpha", SideB: "Beta", Status: state.MatchStatusCompleted, Winner: "Alpha"},
		{ID: "Pool A-1", SideA: "Alpha", SideB: "Gamma", Status: state.MatchStatusCompleted, Winner: "Alpha"},
		{ID: "Pool A-2", SideA: "Alpha", SideB: "Delta", Status: state.MatchStatusCompleted, Winner: "Alpha"},
		{ID: "Pool A-3", SideA: "Alpha", SideB: "Epsilon", Status: state.MatchStatusCompleted, Winner: "Alpha"},
		// Group 1: Beta and Gamma draw → tied
		{ID: "Pool A-4", SideA: "Beta", SideB: "Gamma", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake)},
		// Beta and Gamma both beat Delta and Epsilon
		{ID: "Pool A-5", SideA: "Beta", SideB: "Delta", Status: state.MatchStatusCompleted, Winner: "Beta"},
		{ID: "Pool A-6", SideA: "Beta", SideB: "Epsilon", Status: state.MatchStatusCompleted, Winner: "Beta"},
		{ID: "Pool A-7", SideA: "Gamma", SideB: "Delta", Status: state.MatchStatusCompleted, Winner: "Gamma"},
		{ID: "Pool A-8", SideA: "Gamma", SideB: "Epsilon", Status: state.MatchStatusCompleted, Winner: "Gamma"},
		// Group 2: Delta and Epsilon draw → tied at lower Points
		{ID: "Pool A-9", SideA: "Delta", SideB: "Epsilon", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake)},
		// TB match for group 1: Beta beats Gamma
		{ID: "Pool A-TB-0", SideA: "Beta", SideB: "Gamma", Status: state.MatchStatusCompleted, Winner: "Beta"},
		// TB match for group 2: Epsilon beats Delta
		{ID: "Pool A-TB-1", SideA: "Delta", SideB: "Epsilon", Status: state.MatchStatusCompleted, Winner: "Epsilon"},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, 5)

	assert.Equal(t, "Alpha", poolA[0].Player.Name, "Alpha: wins all regular matches")
	assert.Equal(t, "Beta", poolA[1].Player.Name, "Beta: group-1 TB winner → rank 2")
	assert.Equal(t, "Gamma", poolA[2].Player.Name, "Gamma: group-1 TB loser → rank 3")
	assert.Equal(t, "Epsilon", poolA[3].Player.Name, "Epsilon: group-2 TB winner → rank 4")
	assert.Equal(t, "Delta", poolA[4].Player.Name, "Delta: group-2 TB loser → rank 5")
}

func TestComputeStandings_TBSecondarySort(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tb-secondary-sort"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "TB Secondary Sort",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"}, {Name: "Charlie"},
		}},
	}))
	// Alice wins all regular matches; Bob and Charlie draw → tie
	// TB: Alice won (irrelevant to tie-breaking), Bob beats Charlie in tiebreaker
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Alice"},
		{ID: "Pool A-2", SideA: "Bob", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake)},
		{ID: "Pool A-TB-0", SideA: "Bob", SideB: "Charlie", Status: state.MatchStatusCompleted, Winner: "Bob",
			Decision: string(domain.DecisionIpponShobu)},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, 3)

	assert.Equal(t, "Alice", poolA[0].Player.Name, "Alice should be rank 1")
	assert.Equal(t, "Bob", poolA[1].Player.Name, "Bob won TB match → rank 2")
	assert.Equal(t, "Charlie", poolA[2].Player.Name, "Charlie lost TB match → rank 3")
}

// fourPlayerOneTiedPairTB builds a 4-player round-robin where Alice and Bob
// finish distinct at the top and Carol & Dave tie for 3rd/4th (each loses to
// Alice & Bob and draws the other). Mirrors the DH band-aware fixture.
func fourPlayerOneTiedPairTB() []state.MatchResult {
	return []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A"},
		{ID: "Pool A-2", SideA: "Alice", SideB: "Dave", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A"},
		{ID: "Pool A-3", SideA: "Bob", SideB: "Carol", Status: state.MatchStatusCompleted, Winner: "Bob", Court: "A"},
		{ID: "Pool A-4", SideA: "Bob", SideB: "Dave", Status: state.MatchStatusCompleted, Winner: "Bob", Court: "A"},
		{ID: "Pool A-5", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusCompleted, Winner: "",
			Decision: string(domain.DecisionHikiwake), Court: "A"},
	}
}

func setupIndividualPoolTB(t *testing.T, compID string, poolWinners int) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "TB band-aware", Format: state.CompFormatMixed,
		Status: state.CompStatusPools, Courts: []string{"A"}, PoolWinners: poolWinners,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{
		{Name: "Alice"}, {Name: "Bob"}, {Name: "Carol"}, {Name: "Dave"},
	}}}))
	require.NoError(t, store.SavePoolMatches(compID, fourPlayerOneTiedPairTB()))
	return eng, store
}

// TestInjectTiebreaker_BelowCutIsNonConsequential: a tie entirely below the
// advancement cut (Carol/Dave at 3rd/4th with top-2 advancing) injects NO
// tiebreaker matches; the two players simply share the rank.
func TestInjectTiebreaker_BelowCutIsNonConsequential(t *testing.T) {
	eng, _ := setupIndividualPoolTB(t, "tb-below-cut", 2)
	injected, err := eng.InjectTiebreakerMatches("tb-below-cut")
	require.NoError(t, err)
	assert.Empty(t, injected, "a tie below the top-2 cut must not inject tiebreaker matches")
}

// TestInjectTiebreaker_ConsequentialWhenAllAdvance: the identical Carol/Dave tie
// IS consequential when the cut is 4 (everyone advances, so the 3rd vs 4th seed
// must be decided) - proving the difference is the band, not the standings.
func TestInjectTiebreaker_ConsequentialWhenAllAdvance(t *testing.T) {
	eng, _ := setupIndividualPoolTB(t, "tb-all-advance", 4)
	injected, err := eng.InjectTiebreakerMatches("tb-all-advance")
	require.NoError(t, err)
	require.Len(t, injected, 1, "with all players advancing, the 3rd/4th seed tie needs one tiebreaker")
	assert.True(t, IsTiebreakerMatchID(injected[0].ID))
}

// setupEngiComp builds a minimal saved engi competition in pools status.
func setupEngiComp(t *testing.T, store *state.Store, id, format string) {
	t.Helper()
	comp := &state.Competition{
		ID:           id,
		Name:         "Engi Test",
		Kind:         "individual",
		Format:       format,
		PoolSize:     2,
		PoolSizeMode: "min",
		PoolWinners:  1,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       state.CompStatusPools,
		Engi:         true,
	}
	require.NoError(t, store.SaveCompetition(comp))
}

// TestInjectTiebreakerMatches_Engi_DecisiveResult verifies that a decisive
// engi result (flag winner in each pool) produces no tiebreaker injection.
// Engi ranks by wins then flags; Points is always 0 so detectPoolTies would
// otherwise see every pool as fully tied.
func TestInjectTiebreakerMatches_Engi_DecisiveResult(t *testing.T) {
	tests := []struct {
		name    string
		compID  string
		matches []state.MatchResult
	}{
		{
			name:   "single pool decisive",
			compID: "engi-decisive-single",
			matches: []state.MatchResult{
				{
					ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
					Status: state.MatchStatusCompleted, Winner: "Alice",
					FlagsA: 3, FlagsB: 2, Court: "A",
				},
			},
		},
		{
			name:   "two pools both decisive",
			compID: "engi-decisive-two",
			matches: []state.MatchResult{
				{
					ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
					Status: state.MatchStatusCompleted, Winner: "Alice",
					FlagsA: 3, FlagsB: 2, Court: "A",
				},
				{
					ID: "Pool B-0", SideA: "Carol", SideB: "Dave",
					Status: state.MatchStatusCompleted, Winner: "Carol",
					FlagsA: 5, FlagsB: 0, Court: "A",
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := tc.compID

			setupEngiComp(t, store, compID, state.CompFormatMixed)
			require.NoError(t, store.SavePools(compID, []helper.Pool{
				{PoolName: "Pool A", Players: []helper.Player{
					{Name: "Alice"}, {Name: "Bob"},
				}},
				{PoolName: "Pool B", Players: []helper.Player{
					{Name: "Carol"}, {Name: "Dave"},
				}},
			}))
			require.NoError(t, store.SavePoolMatches(compID, tc.matches))

			injected, err := eng.InjectTiebreakerMatches(compID)
			require.NoError(t, err)
			assert.Nil(t, injected, "no tiebreaker injection for engi competitions")

			all, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			for _, m := range all {
				assert.False(t, IsTiebreakerMatchID(m.ID),
					"no TB rows expected on disk for engi comp")
			}
		})
	}
}

// TestInjectTiebreakerMatches_Engi_FullCycleTie verifies that even a genuine
// 3-player cycle (A beats B, B beats C, C beats A: all players 1W-1L) produces
// no tiebreaker injection for an engi competition. Engi uses wins then flags for
// ranking; ippon-shobu supplementary bouts are never held.
func TestInjectTiebreakerMatches_Engi_FullCycleTie(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-cycle-tie"

	setupEngiComp(t, store, compID, state.CompFormatLeague)
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"}, {Name: "Charlie"},
		}},
	}))
	// Cycle: Alice beats Bob (3-2), Bob beats Charlie (3-2), Charlie beats Alice (3-2).
	// All players: 1 win, 1 loss, same flag total (5 own-side flags). Engi Points=0 for all.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusCompleted, Winner: "Alice", FlagsA: 3, FlagsB: 2, Court: "A"},
		{ID: "Pool A-1", SideA: "Bob", SideB: "Charlie",
			Status: state.MatchStatusCompleted, Winner: "Bob", FlagsA: 3, FlagsB: 2, Court: "A"},
		{ID: "Pool A-2", SideA: "Charlie", SideB: "Alice",
			Status: state.MatchStatusCompleted, Winner: "Charlie", FlagsA: 3, FlagsB: 2, Court: "A"},
	}))

	injected, err := eng.InjectTiebreakerMatches(compID)
	require.NoError(t, err)
	assert.Nil(t, injected, "engi: no injection even when all pools are Points-tied")

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	tbCount := 0
	for _, m := range all {
		if IsTiebreakerMatchID(m.ID) {
			tbCount++
		}
	}
	assert.Equal(t, 0, tbCount, "no TB rows expected for engi comp")
}

// TestInjectTiebreakerMatches_Engi_SelfHeal verifies the self-heal path:
// winnerless non-completed TB rows written by a pre-fix engine (scheduled or
// running) are removed, while a completed TB row (with a Winner) is preserved.
func TestInjectTiebreakerMatches_Engi_SelfHeal(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-self-heal"

	setupEngiComp(t, store, compID, state.CompFormatLeague)
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"},
		}},
	}))

	// Pre-seed the state a pre-fix engine would have left: one regular
	// completed match, one spurious scheduled TB row (winnerless), one
	// spurious running TB row (winnerless, opened for scoring pre-fix; kept
	// in place it would block pool completion forever), and one
	// already-completed TB row whose result we must preserve.
	const spuriousTB = "Pool A-TB-0"
	const completedTB = "Pool A-TB-1"
	const runningTB = "Pool A-TB-2"
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusCompleted, Winner: "Alice",
			FlagsA: 3, FlagsB: 2, Court: "A",
		},
		{
			// Spurious scheduled TB row: no Winner, no Decision.
			ID: spuriousTB, SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusScheduled, Court: "A",
		},
		{
			// Completed TB row: must be kept even for engi.
			ID: completedTB, SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A",
		},
		{
			// Spurious running TB row: winnerless, must also be removed.
			ID: runningTB, SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusRunning, Court: "A",
		},
	}))

	injected, err := eng.InjectTiebreakerMatches(compID)
	require.NoError(t, err)
	assert.Nil(t, injected, "self-heal returns nil injected (nothing new was added)")

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	var ids []string
	for _, m := range all {
		ids = append(ids, m.ID)
	}
	assert.NotContains(t, ids, spuriousTB, "spurious scheduled TB row must be removed")
	assert.NotContains(t, ids, runningTB, "spurious running winnerless TB row must be removed")
	assert.Contains(t, ids, "Pool A-0", "regular match must be preserved")
	assert.Contains(t, ids, completedTB, "completed TB row must be preserved")
}

// TestMaybeAutoCompletePools_Engi_NoTiebreakInjection verifies that an engi
// competition with all regular matches complete transitions normally to
// CompStatusComplete without triggering AutoCompleteTiebreakInjected.
// Without the engi guard, detectPoolTies sees all Points=0 and injects
// spurious ippon-shobu bouts that block completion.
func TestMaybeAutoCompletePools_Engi_NoTiebreakInjection(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "engi-auto-complete"

	// League format: auto-completes once all pool matches are done.
	setupEngiComp(t, store, compID, state.CompFormatLeague)
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"},
		}},
	}))
	// One decisive flag-scored match. Points=0 for all engi standings, so
	// without the guard this would inject a spurious TB bout.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "Pool A-0", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusCompleted, Winner: "Alice",
			FlagsA: 3, FlagsB: 2, Court: "A",
		},
	}))

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome,
		"engi league with all matches done must complete, not inject tiebreakers")

	// No spurious TB rows on disk.
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range all {
		assert.False(t, IsTiebreakerMatchID(m.ID),
			"no TB rows expected on disk for engi comp after MaybeAutoCompletePools")
	}
}
