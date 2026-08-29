package engine

import (
	"strconv"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCalculatePoolStandings_Override_SameNameDifferentDojo is the manual
// pool-rank-override regression (bc-cse): state.Overrides.PoolRanks used to be
// keyed by bare player name (poolID -> playerName -> rank), so two same-name,
// different-dojo competitors in one pool shared a single override entry.
// Chusen (drawing-lots) is the last-resort tie-break for a CONSEQUENTIAL tie,
// so a misapplied override changes who advances -- recording a chusen result
// for one Tanaka Kenji must never be visible on the other.
//
// This drives the real write path (store.SaveRankOverride, which builds the
// identity key via helper.CompetitorKey exactly like the mobileapp
// override-rank handler resolves it) and the real read path
// (eng.CalculatePoolStandings -> computeStandingsFrom's override-sort block).
func TestCalculatePoolStandings_Override_SameNameDifferentDojo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-override-samename"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Pool Override Same Name",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	players := snPlayers()
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: players},
	}))

	// Full round robin so every competitor has a distinct, non-tied finish
	// BEFORE any override (Osaka 3-0 > Suzuki 2-1 > Watanabe 1-2 > Tokyo 0-3),
	// stamped with SideAID/SideBID/WinnerID exactly as the real pipeline would
	// (mirrors TestCalculatePoolStandings_SameNameDifferentDojo).
	var matches []state.MatchResult
	idx := 0
	for i := 0; i < len(players); i++ {
		for j := i + 1; j < len(players); j++ {
			a, b := players[i], players[j]
			winner, winnerID := a, a.ID
			if snRank(b.ID) > snRank(a.ID) {
				winner, winnerID = b, b.ID
			}
			matches = append(matches, state.MatchResult{
				ID:       "Pool A-" + strconv.Itoa(idx),
				SideA:    a.Name,
				SideB:    b.Name,
				SideAID:  a.ID,
				SideBID:  b.ID,
				Winner:   winner.Name,
				WinnerID: winnerID,
				Status:   state.MatchStatusCompleted,
			})
			idx++
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	// Operator records a chusen (drawing-lots) result for OSAKA Tanaka Kenji
	// only, using her real id/dojo exactly as the mobileapp handler's
	// resolvePoolOverrideTarget would resolve them from the pool roster.
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", snIDOsaka, "Tanaka Kenji", "Osaka", 1))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, 4, "same-name-different-dojo competitors must both appear")

	byID := make(map[string]state.PlayerStanding, len(poolA))
	for _, s := range poolA {
		byID[s.Player.ID] = s
	}
	require.Contains(t, byID, snIDTokyo)
	require.Contains(t, byID, snIDOsaka)

	assert.True(t, byID[snIDOsaka].IsOverridden, "the competitor the chusen was recorded for must show as overridden")
	assert.Equal(t, 1, byID[snIDOsaka].Rank)
	assert.False(t, byID[snIDTokyo].IsOverridden,
		"the namesake from a DIFFERENT dojo must NOT inherit an override meant for the other Tanaka Kenji")
}

// TestCalculatePoolStandings_Override_LegacyBareNameKey loads an
// overrides.json shaped exactly as one written BEFORE bc-cse: PoolRanks
// keyed by bare player name, no identity at all. The read path
// (lookupPoolRankOverride) must still honour it -- a live tournament's
// existing overrides must not be silently dropped by the upgrade -- so this
// pins the READ-ONLY compatibility decision: legacy entries are never
// rewritten, only ever recognised as a fallback when the identity-keyed
// lookup misses.
func TestCalculatePoolStandings_Override_LegacyBareNameKey(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-override-legacy"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Pool Override Legacy",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	players := []domain.Player{
		{ID: "legacy-p1", Name: "Alice", Dojo: "Dojo A"},
		{ID: "legacy-p2", Name: "Bob", Dojo: "Dojo B"},
	}
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: players},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: "legacy-p1", SideBID: "legacy-p2",
			Winner: "Bob", WinnerID: "legacy-p2", Status: state.MatchStatusCompleted,
		},
	}))

	// Written directly via SaveOverrides (bypassing SaveRankOverride*, which
	// always writes the NEW identity-keyed form) to simulate a file on disk
	// from before this fix existed.
	require.NoError(t, store.SaveOverrides(compID, &state.Overrides{
		PoolRanks: map[string]map[string]int{
			"Pool A": {"Alice": 1},
		},
		Winners: map[string]string{},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, 2)

	byID := make(map[string]state.PlayerStanding, len(poolA))
	for _, s := range poolA {
		byID[s.Player.ID] = s
	}
	assert.True(t, byID["legacy-p1"].IsOverridden, "a legacy bare-name-keyed override must still apply")
	assert.Equal(t, 1, byID["legacy-p1"].Rank)
	assert.False(t, byID["legacy-p2"].IsOverridden, "the override must not spuriously apply to the other competitor")
}
