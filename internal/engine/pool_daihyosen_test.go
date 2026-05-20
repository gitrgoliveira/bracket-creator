package engine

import (
	"sort"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsPoolDaihyosenMatchID covers the ID-recognition helper.
func TestIsPoolDaihyosenMatchID(t *testing.T) {
	tests := []struct {
		id   string
		want bool
	}{
		{"Pool A-DH-0", true},
		{"Pool A-DH-1", true},
		{"Pool B-DH-42", true},
		{"Pool A-0", false},
		{"Pool A-TB-0", false},
		{"Pool A-DH", false},    // no index after DH
		{"Pool A-D-0", false},   // different prefix
		{"Pool A-DHx-0", false}, // wrong prefix
		{"DH-0", false},         // no pool separator
		{"", false},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			assert.Equal(t, tc.want, IsPoolDaihyosenMatchID(tc.id))
		})
	}
}

// TestGeneratePoolDaihyosenMatches_TwoWayTie verifies that two tied teams
// produce one DH match.
func TestGeneratePoolDaihyosenMatches_TwoWayTie(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{Name: "TeamA"}},
		{Player: domain.Player{Name: "TeamB"}},
	}
	matches := generatePoolDaihyosenMatches("Pool X", group, 0, "A", map[string]bool{})
	require.Len(t, matches, 1)
	m := matches[0]
	assert.Equal(t, "Pool X-DH-0", m.ID)
	assert.True(t, IsPoolDaihyosenMatchID(m.ID))
	assert.Equal(t, state.MatchStatusScheduled, m.Status)
	assert.Equal(t, "A", m.Court)
}

// TestGeneratePoolDaihyosenMatches_ThreeWayTie verifies round-robin for 3 teams.
func TestGeneratePoolDaihyosenMatches_ThreeWayTie(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{Name: "TeamA"}},
		{Player: domain.Player{Name: "TeamB"}},
		{Player: domain.Player{Name: "TeamC"}},
	}
	matches := generatePoolDaihyosenMatches("Pool X", group, 0, "B", map[string]bool{})
	require.Len(t, matches, 3)
	assert.Equal(t, "Pool X-DH-0", matches[0].ID)
	assert.Equal(t, "Pool X-DH-1", matches[1].ID)
	assert.Equal(t, "Pool X-DH-2", matches[2].ID)
}

// TestGeneratePoolDaihyosenMatches_SkipsExistingPairs ensures idempotency.
func TestGeneratePoolDaihyosenMatches_SkipsExistingPairs(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{Name: "TeamA"}},
		{Player: domain.Player{Name: "TeamB"}},
		{Player: domain.Player{Name: "TeamC"}},
	}
	existing := map[string]bool{tiebreakerPairKey("TeamA", "TeamB"): true}
	matches := generatePoolDaihyosenMatches("Pool X", group, 1, "A", existing)
	require.Len(t, matches, 2, "TeamA-TeamB already exists; only other 2 pairs generated")
}

// setupTeamPoolComp creates a team-pool competition with three teams in Pool A,
// all matches completed. If tieAll is true all teams share identical statistics
// (full 3-way 8-criteria tie). If tieAll is false Alpha has a clear lead.
//
// SubMatchResult.SideA/SideB are always set to avoid the "" == "" false-positive
// in computeStandings (sub.Winner == sub.SideA when both are "").
func setupTeamPoolComp(t *testing.T, compID string, tieAll bool) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Team Pool Test",
		Format:   state.CompFormatPools,
		Status:   state.CompStatusPools,
		Courts:   []string{"A"},
		TeamSize: 2, // 2-person teams keeps the SubResults simple
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alpha"}, {Name: "Beta"}, {Name: "Gamma"},
		}},
	}))

	var matches []state.MatchResult
	if tieAll {
		// All three matches drawn at match level and sub-match level →
		// W=0, L=0, T=2, IV=0, IL=0, IT=4, PW=0, PL=0 for every team.
		matches = []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alpha", SideB: "Beta",
				Status: state.MatchStatusCompleted,
				Winner: "", Decision: string(domain.DecisionHikiwake), Court: "A",
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "Alpha", SideB: "Beta", Winner: "", Decision: string(domain.DecisionHikiwake)},
					{Position: 2, SideA: "Alpha", SideB: "Beta", Winner: "", Decision: string(domain.DecisionHikiwake)},
				}},
			{ID: "Pool A-1", SideA: "Alpha", SideB: "Gamma",
				Status: state.MatchStatusCompleted,
				Winner: "", Decision: string(domain.DecisionHikiwake), Court: "A",
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "Alpha", SideB: "Gamma", Winner: "", Decision: string(domain.DecisionHikiwake)},
					{Position: 2, SideA: "Alpha", SideB: "Gamma", Winner: "", Decision: string(domain.DecisionHikiwake)},
				}},
			{ID: "Pool A-2", SideA: "Beta", SideB: "Gamma",
				Status: state.MatchStatusCompleted,
				Winner: "", Decision: string(domain.DecisionHikiwake), Court: "A",
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "Beta", SideB: "Gamma", Winner: "", Decision: string(domain.DecisionHikiwake)},
					{Position: 2, SideA: "Beta", SideB: "Gamma", Winner: "", Decision: string(domain.DecisionHikiwake)},
				}},
		}
	} else {
		// Alpha wins both matches (W=2); Beta and Gamma each win one (W=1, L=1) and
		// then face each other with Beta winning — distinct standings, no tie.
		matches = []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alpha", SideB: "Beta",
				Status: state.MatchStatusCompleted, Winner: "Alpha", Court: "A",
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "Alpha", SideB: "Beta", Winner: "Alpha"},
					{Position: 2, SideA: "Alpha", SideB: "Beta", Winner: "Alpha"},
				}},
			{ID: "Pool A-1", SideA: "Alpha", SideB: "Gamma",
				Status: state.MatchStatusCompleted, Winner: "Alpha", Court: "A",
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "Alpha", SideB: "Gamma", Winner: "Alpha"},
					{Position: 2, SideA: "Alpha", SideB: "Gamma", Winner: "Alpha"},
				}},
			{ID: "Pool A-2", SideA: "Beta", SideB: "Gamma",
				Status: state.MatchStatusCompleted, Winner: "Beta", Court: "A",
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "Beta", SideB: "Gamma", Winner: "Beta"},
					{Position: 2, SideA: "Beta", SideB: "Gamma", Winner: "Beta"},
				}},
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
	return eng, store
}

// TestInjectPoolDaihyosenMatches_NoTie verifies no DH matches are injected
// when team standings are distinct.
func TestInjectPoolDaihyosenMatches_NoTie(t *testing.T) {
	eng, _ := setupTeamPoolComp(t, "dh-no-tie", false)
	injected, err := eng.InjectPoolDaihyosenMatches("dh-no-tie")
	require.NoError(t, err)
	assert.Empty(t, injected)
}

// TestInjectPoolDaihyosenMatches_TwoWayTie verifies that a two-way team tie
// produces one DH match.
func TestInjectPoolDaihyosenMatches_TwoWayTie(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "dh-two-tie", true)
	// Override with a 2-way tie: Alpha/Beta both win one, draw one (same record),
	// Gamma loses both — Alpha vs Beta draw decides the 2-way tie.
	// Crucially, SubMatchResult.SideA/SideB are set to prevent "" == "" false-positives.
	require.NoError(t, store.SavePoolMatches("dh-two-tie", []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alpha", SideB: "Beta",
			Status: state.MatchStatusCompleted,
			Winner: "", Decision: string(domain.DecisionHikiwake), Court: "A",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Alpha", SideB: "Beta", Winner: "", Decision: string(domain.DecisionHikiwake)},
				{Position: 2, SideA: "Alpha", SideB: "Beta", Winner: "", Decision: string(domain.DecisionHikiwake)},
			}},
		{ID: "Pool A-1", SideA: "Alpha", SideB: "Gamma",
			Status: state.MatchStatusCompleted, Winner: "Alpha", Court: "A",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Alpha", SideB: "Gamma", Winner: "Alpha"},
				{Position: 2, SideA: "Alpha", SideB: "Gamma", Winner: "Alpha"},
			}},
		{ID: "Pool A-2", SideA: "Beta", SideB: "Gamma",
			Status: state.MatchStatusCompleted, Winner: "Beta", Court: "A",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Beta", SideB: "Gamma", Winner: "Beta"},
				{Position: 2, SideA: "Beta", SideB: "Gamma", Winner: "Beta"},
			}},
	}))

	injected, err := eng.InjectPoolDaihyosenMatches("dh-two-tie")
	require.NoError(t, err)
	require.Len(t, injected, 1, "one DH match expected for a two-way team tie")
	m := injected[0]
	assert.True(t, IsPoolDaihyosenMatchID(m.ID))
	assert.Equal(t, state.MatchStatusScheduled, m.Status)
	assert.Equal(t, "A", m.Court)
	assert.ElementsMatch(t, []string{"Alpha", "Beta"}, []string{m.SideA, m.SideB})
}

// TestInjectPoolDaihyosenMatches_Idempotent verifies that calling inject twice
// does not create duplicate DH matches.
func TestInjectPoolDaihyosenMatches_Idempotent(t *testing.T) {
	eng, _ := setupTeamPoolComp(t, "dh-idempotent", true)

	first, err := eng.InjectPoolDaihyosenMatches("dh-idempotent")
	require.NoError(t, err)
	require.NotEmpty(t, first)

	second, err := eng.InjectPoolDaihyosenMatches("dh-idempotent")
	require.NoError(t, err)
	assert.Empty(t, second, "second inject must produce no new matches")
}

// TestMaybeAutoCompletePools_TeamTieInjectsDH verifies that MaybeAutoCompletePools
// returns AutoCompleteTiebreakInjected and injects DH matches for a team
// competition with tied pools.
func TestMaybeAutoCompletePools_TeamTieInjectsDH(t *testing.T) {
	eng, _ := setupTeamPoolComp(t, "autocomplete-team-tie", true)

	outcome, err := eng.MaybeAutoCompletePools("autocomplete-team-tie")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTiebreakInjected, outcome)
}

// TestMaybeAutoCompletePools_TeamNoTieTransitions verifies that a team pool
// with no ties transitions to complete.
func TestMaybeAutoCompletePools_TeamNoTieTransitions(t *testing.T) {
	eng, _ := setupTeamPoolComp(t, "autocomplete-team-notie", false)

	outcome, err := eng.MaybeAutoCompletePools("autocomplete-team-notie")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome)
}

// TestMaybeAutoCompletePools_TeamDHCompleteTransitions verifies that after all
// DH matches are completed, the competition transitions to complete.
func TestMaybeAutoCompletePools_TeamDHCompleteTransitions(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "autocomplete-team-dhcomplete", true)

	// First pass: inject DH.
	outcome, err := eng.MaybeAutoCompletePools("autocomplete-team-dhcomplete")
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTiebreakInjected, outcome)

	// Mark all pool matches (including the injected DH) as completed.
	allMatches, err := store.LoadPoolMatches("autocomplete-team-dhcomplete")
	require.NoError(t, err)
	for i := range allMatches {
		allMatches[i].Status = state.MatchStatusCompleted
		if allMatches[i].Winner == "" {
			allMatches[i].Winner = allMatches[i].SideA // assign a winner to DH match
		}
	}
	require.NoError(t, store.SavePoolMatches("autocomplete-team-dhcomplete", allMatches))

	outcome, err = eng.MaybeAutoCompletePools("autocomplete-team-dhcomplete")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome)
}

// TestMaybeAutoCompletePools_TeamDHCompletedWithoutWinner verifies that a DH
// match saved as completed but with no winner (e.g. hikiwake) does not allow
// the competition to transition — standings would remain tied.
func TestMaybeAutoCompletePools_TeamDHCompletedWithoutWinner(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "autocomplete-team-dh-nowinner", true)

	// Inject DH.
	outcome, err := eng.MaybeAutoCompletePools("autocomplete-team-dh-nowinner")
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTiebreakInjected, outcome)

	// Mark all matches completed but leave DH winner empty (hikiwake).
	allMatches, err := store.LoadPoolMatches("autocomplete-team-dh-nowinner")
	require.NoError(t, err)
	for i := range allMatches {
		allMatches[i].Status = state.MatchStatusCompleted
		// Deliberately do NOT set Winner on DH matches.
	}
	require.NoError(t, store.SavePoolMatches("autocomplete-team-dh-nowinner", allMatches))

	outcome, err = eng.MaybeAutoCompletePools("autocomplete-team-dh-nowinner")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "DH with no winner should block auto-completion")
}

// TestDHCycleExists_NoCycle verifies that dhCycleExists returns false when
// DH results unambiguously break all ties (A wins DH against B).
func TestDHCycleExists_NoCycle(t *testing.T) {
	standings := map[string][]state.PlayerStanding{
		"Pool A": {
			{Player: helper.Player{Name: "Alpha"}, Points: 0},
			{Player: helper.Player{Name: "Beta"}, Points: 0},
		},
	}
	matches := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Alpha", SideB: "Beta",
			Status: state.MatchStatusCompleted, Winner: "Alpha"},
	}
	assert.False(t, dhCycleExists(standings, matches, nil))
}

// TestDHCycleExists_Cycle verifies that dhCycleExists returns true for a
// three-way cyclic result (A>B, B>C, C>A).
func TestDHCycleExists_Cycle(t *testing.T) {
	standings := map[string][]state.PlayerStanding{
		"Pool A": {
			{Player: helper.Player{Name: "Alpha"}, Points: 0},
			{Player: helper.Player{Name: "Beta"}, Points: 0},
			{Player: helper.Player{Name: "Gamma"}, Points: 0},
		},
	}
	matches := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Alpha", SideB: "Beta", Status: state.MatchStatusCompleted, Winner: "Alpha"},
		{ID: "Pool A-DH-1", SideA: "Beta", SideB: "Gamma", Status: state.MatchStatusCompleted, Winner: "Beta"},
		{ID: "Pool A-DH-2", SideA: "Alpha", SideB: "Gamma", Status: state.MatchStatusCompleted, Winner: "Gamma"},
	}
	assert.True(t, dhCycleExists(standings, matches, nil))
}

// TestDHCycleExists_CycleResolvedByOverrides verifies that a cyclic DH result
// is NOT flagged when the operator has manually ranked all tied members.
func TestDHCycleExists_CycleResolvedByOverrides(t *testing.T) {
	standings := map[string][]state.PlayerStanding{
		"Pool A": {
			{Player: helper.Player{Name: "Alpha"}, Points: 0},
			{Player: helper.Player{Name: "Beta"}, Points: 0},
			{Player: helper.Player{Name: "Gamma"}, Points: 0},
		},
	}
	matches := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Alpha", SideB: "Beta", Status: state.MatchStatusCompleted, Winner: "Alpha"},
		{ID: "Pool A-DH-1", SideA: "Beta", SideB: "Gamma", Status: state.MatchStatusCompleted, Winner: "Beta"},
		{ID: "Pool A-DH-2", SideA: "Alpha", SideB: "Gamma", Status: state.MatchStatusCompleted, Winner: "Gamma"},
	}
	// Operator manually resolved the cycle by assigning explicit ranks.
	poolRanks := map[string]map[string]int{
		"Pool A": {"Alpha": 1, "Beta": 2, "Gamma": 3},
	}
	assert.False(t, dhCycleExists(standings, matches, poolRanks))
}

// TestMaybeAutoCompletePools_TeamDHCycleBlocks verifies that auto-completion
// is blocked when DH results form a cycle and standings remain tied.
func TestMaybeAutoCompletePools_TeamDHCycleBlocks(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "autocomplete-team-dh-cycle", true)

	// Inject DH matches (3-way tie → 3 DH bouts injected).
	outcome, err := eng.MaybeAutoCompletePools("autocomplete-team-dh-cycle")
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTiebreakInjected, outcome)

	allMatches, err := store.LoadPoolMatches("autocomplete-team-dh-cycle")
	require.NoError(t, err)

	// Score DH matches in a cycle: first DH bout Alpha beats whoever is SideB,
	// second DH bout Beta wins, third DH bout the remaining team wins —
	// producing a 1-win-each cycle.
	dhCount := 0
	sides := [][2]string{}
	for _, m := range allMatches {
		if IsPoolDaihyosenMatchID(m.ID) {
			sides = append(sides, [2]string{m.SideA, m.SideB})
			dhCount++
		}
	}
	require.Equal(t, 3, dhCount, "expected 3 DH matches for 3-way tie")

	// Build a deterministic 3-way cycle from the actual team names rather
	// than positional indices — DH match order is non-deterministic because
	// standings are assembled from a map.  Sort all unique names, then apply
	// the cycle names[0]>names[1], names[1]>names[2], names[2]>names[0].
	nameSet := map[string]bool{}
	for _, pair := range sides {
		nameSet[pair[0]] = true
		nameSet[pair[1]] = true
	}
	sortedNames := make([]string, 0, len(nameSet))
	for n := range nameSet {
		sortedNames = append(sortedNames, n)
	}
	sort.Strings(sortedNames) // deterministic: Alpha < Beta < Gamma
	// cycle: sortedNames[0] beats [1], [1] beats [2], [2] beats [0]
	cycleBeats := map[string]string{
		sortedNames[0]: sortedNames[1],
		sortedNames[1]: sortedNames[2],
		sortedNames[2]: sortedNames[0],
	}
	for i := range allMatches {
		allMatches[i].Status = state.MatchStatusCompleted
		if IsPoolDaihyosenMatchID(allMatches[i].ID) {
			sA, sB := allMatches[i].SideA, allMatches[i].SideB
			if cycleBeats[sA] == sB {
				allMatches[i].Winner = sA
			} else {
				allMatches[i].Winner = sB
			}
		}
		// Leave regular match Winners unchanged — they were drawn (hikiwake)
		// and must stay that way to keep the three-way tie intact.
	}
	require.NoError(t, store.SavePoolMatches("autocomplete-team-dh-cycle", allMatches))

	outcome, err = eng.MaybeAutoCompletePools("autocomplete-team-dh-cycle")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteNoChange, outcome, "cyclic DH results should block auto-completion")
}

// TestMaybeAutoCompletePools_TeamDHCycleWithOverridesTransitions verifies that
// when DH results form a cycle but the operator has applied manual rank
// overrides covering every tied team, MaybeAutoCompletePools transitions to
// complete instead of blocking forever.
func TestMaybeAutoCompletePools_TeamDHCycleWithOverridesTransitions(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "autocomplete-team-dh-cycle-override", true)

	// Inject DH and build the same 3-way cycle as TeamDHCycleBlocks.
	outcome, err := eng.MaybeAutoCompletePools("autocomplete-team-dh-cycle-override")
	require.NoError(t, err)
	require.Equal(t, AutoCompleteTiebreakInjected, outcome)

	allMatches, err := store.LoadPoolMatches("autocomplete-team-dh-cycle-override")
	require.NoError(t, err)

	nameSet := map[string]bool{}
	for _, m := range allMatches {
		if IsPoolDaihyosenMatchID(m.ID) {
			nameSet[m.SideA] = true
			nameSet[m.SideB] = true
		}
	}
	sortedNames := make([]string, 0, len(nameSet))
	for n := range nameSet {
		sortedNames = append(sortedNames, n)
	}
	sort.Strings(sortedNames)
	cycleBeats := map[string]string{
		sortedNames[0]: sortedNames[1],
		sortedNames[1]: sortedNames[2],
		sortedNames[2]: sortedNames[0],
	}
	for i := range allMatches {
		allMatches[i].Status = state.MatchStatusCompleted
		if IsPoolDaihyosenMatchID(allMatches[i].ID) {
			sA, sB := allMatches[i].SideA, allMatches[i].SideB
			if cycleBeats[sA] == sB {
				allMatches[i].Winner = sA
			} else {
				allMatches[i].Winner = sB
			}
		}
	}
	require.NoError(t, store.SavePoolMatches("autocomplete-team-dh-cycle-override", allMatches))

	// Without overrides the cycle blocks.
	outcome, err = eng.MaybeAutoCompletePools("autocomplete-team-dh-cycle-override")
	require.NoError(t, err)
	require.Equal(t, AutoCompleteNoChange, outcome, "cycle must block before overrides are set")

	// Operator manually ranks all three tied teams — cycle is now resolved.
	require.NoError(t, store.SaveOverrides("autocomplete-team-dh-cycle-override", &state.Overrides{
		PoolRanks: map[string]map[string]int{
			"Pool A": {sortedNames[0]: 1, sortedNames[1]: 2, sortedNames[2]: 3},
		},
		Winners: map[string]string{},
	}))

	outcome, err = eng.MaybeAutoCompletePools("autocomplete-team-dh-cycle-override")
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "manual overrides must unblock cyclic DH completion")
}

// TestDHStandingsApplied verifies that a completed pool-DH match result is
// applied as a secondary sort to break a tie in team pool standings.
func TestDHStandingsApplied(t *testing.T) {
	eng, store := setupTeamPoolComp(t, "dh-standings", true)

	// Inject DH matches.
	_, err := eng.InjectPoolDaihyosenMatches("dh-standings")
	require.NoError(t, err)

	// Find the injected DH match and mark Alpha as the winner.
	allMatches, err := store.LoadPoolMatches("dh-standings")
	require.NoError(t, err)
	for i := range allMatches {
		if IsPoolDaihyosenMatchID(allMatches[i].ID) {
			allMatches[i].Status = state.MatchStatusCompleted
			// Determine which team is "Alpha" (SideA or SideB).
			if allMatches[i].SideA == "Alpha" || allMatches[i].SideB == "Alpha" {
				allMatches[i].Winner = "Alpha"
			}
		}
	}
	require.NoError(t, store.SavePoolMatches("dh-standings", allMatches))
	eng.standingsCache.Delete("dh-standings")
	eng.standingsFlight.Delete("dh-standings")

	standings, err := eng.CalculatePoolStandings("dh-standings")
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.NotEmpty(t, poolA)
	// Alpha should rank first after winning the DH (all three teams were tied).
	assert.Equal(t, "Alpha", poolA[0].Player.Name, "Alpha should rank first after winning DH")
}

// TestInjectPoolDaihyosenMatches_PreservesExistingScheduledAt is the
// regression guard that ensures InjectPoolDaihyosenMatches does not
// overwrite operator-adjusted ScheduledAt values on pre-existing matches.
// Only newly injected DH matches (empty ScheduledAt) should receive fresh
// slot assignments.
func TestInjectPoolDaihyosenMatches_PreservesExistingScheduledAt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "dh-preserves-time"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:        compID,
		Name:      "DH Preserves Time",
		Format:    state.CompFormatPools,
		Status:    state.CompStatusPools,
		Courts:    []string{"A"},
		TeamSize:  2,
		StartTime: "09:00",
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alpha"}, {Name: "Beta"},
		}},
	}))

	// Operator adjusts the match time well outside the auto-assigned window.
	const operatorTime = "15:00"
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alpha", SideB: "Beta",
			Status: state.MatchStatusCompleted,
			Winner: "", Decision: string(domain.DecisionHikiwake), Court: "A",
			ScheduledAt: operatorTime,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Alpha", SideB: "Beta", Winner: "", Decision: string(domain.DecisionHikiwake)},
				{Position: 2, SideA: "Alpha", SideB: "Beta", Winner: "", Decision: string(domain.DecisionHikiwake)},
			}},
	}))

	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 1, "one DH match expected for a two-way team tie")

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)

	for _, m := range all {
		if IsPoolDaihyosenMatchID(m.ID) {
			assert.NotEmpty(t, m.ScheduledAt,
				"DH match must receive an auto-assigned slot")
		} else {
			assert.Equal(t, operatorTime, m.ScheduledAt,
				"existing match %s must retain its operator-adjusted ScheduledAt", m.ID)
		}
	}
}
