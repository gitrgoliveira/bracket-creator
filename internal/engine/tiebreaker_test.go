package engine

import (
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
		{"Pool A-0", false},
		{"Pool A-1", false},
		{"Pool A-TB", false},    // no index after TB
		{"Pool A-T-0", false},   // different prefix
		{"Pool A-TBx-0", false}, // wrong prefix
		{"TB-0", false},         // no pool name separator
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			assert.Equal(t, tc.want, IsTiebreakerMatchID(tc.id))
		})
	}
}

func TestDetectPoolTies_NoTies(t *testing.T) {
	standings := []state.PlayerStanding{
		{Player: domain.Player{Name: "Alice"}, Points: 300},
		{Player: domain.Player{Name: "Bob"}, Points: 200},
		{Player: domain.Player{Name: "Charlie"}, Points: 100},
	}
	groups := detectPoolTies(standings)
	assert.Empty(t, groups)
}

func TestDetectPoolTies_TwoWayTie(t *testing.T) {
	standings := []state.PlayerStanding{
		{Player: domain.Player{Name: "Alice"}, Points: 300},
		{Player: domain.Player{Name: "Bob"}, Points: 100},
		{Player: domain.Player{Name: "Charlie"}, Points: 100},
	}
	groups := detectPoolTies(standings)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0], 2)
	names := []string{groups[0][0].Player.Name, groups[0][1].Player.Name}
	assert.ElementsMatch(t, []string{"Bob", "Charlie"}, names)
}

func TestDetectPoolTies_ThreeWayTie(t *testing.T) {
	standings := []state.PlayerStanding{
		{Player: domain.Player{Name: "Alice"}, Points: 100},
		{Player: domain.Player{Name: "Bob"}, Points: 100},
		{Player: domain.Player{Name: "Charlie"}, Points: 100},
	}
	groups := detectPoolTies(standings)
	require.Len(t, groups, 1)
	assert.Len(t, groups[0], 3)
}

func TestDetectPoolTies_MultipleGroups(t *testing.T) {
	standings := []state.PlayerStanding{
		{Player: domain.Player{Name: "A"}, Points: 400},
		{Player: domain.Player{Name: "B"}, Points: 400},
		{Player: domain.Player{Name: "C"}, Points: 200},
		{Player: domain.Player{Name: "D"}, Points: 200},
	}
	groups := detectPoolTies(standings)
	require.Len(t, groups, 2)
	assert.Len(t, groups[0], 2)
	assert.Len(t, groups[1], 2)
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
	assert.Empty(t, m.Decision, "injected TB match must have empty Decision — ID convention identifies it")
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
		Format: state.CompFormatPools,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"}, {Name: "Charlie"},
		}},
	}))
	// Alice wins both, Bob beats Charlie — distinct standings, no tie
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
		Format: state.CompFormatPools,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice"}, {Name: "Bob"}, {Name: "Charlie"},
		}},
	}))
	// Alice wins both — Bob and Charlie both lose once, both 0 ippons: tie
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
	assert.Empty(t, m.Decision, "injected TB match must have empty Decision — ID convention identifies it")
	assert.Equal(t, state.MatchStatusScheduled, m.Status)
	assert.Equal(t, "A", m.Court)
}

func TestInjectTiebreakerMatches_Idempotent(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "inject-idempotent"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Idempotent Test",
		Format: state.CompFormatPools,
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
		Format: state.CompFormatPools,
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
		Format: state.CompFormatPools,
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

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "TB Done",
		Format: state.CompFormatPools,
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
	assert.Equal(t, AutoCompleteTransitioned, outcome, "completed TB match must allow completion")

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
		Format: state.CompFormatPools,
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
		assert.Equal(t, 0, s.Wins, "%s: TB win must not count as a regular win", s.Player.Name)
		assert.Equal(t, 0, s.Losses, "%s: must have no regular losses", s.Player.Name)
		assert.Equal(t, 1, s.Draws, "%s: draw from regular match must be counted", s.Player.Name)
	}
}

func TestComputeStandings_TBSecondarySort(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "tb-secondary-sort"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "TB Secondary Sort",
		Format: state.CompFormatPools,
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
