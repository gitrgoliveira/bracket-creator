package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedIndividualLeagueCircle stores an individual league of three whose regular
// round (and, when withTiebreakRound, a finished tie-break round) went in a
// circle: every regular and tie-break bout is won by the first side of the
// pair, so each competitor beat the next one and all three finish level.
func seedIndividualLeagueCircle(t *testing.T, store *state.Store, compID string, withTiebreakRound bool) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Circle", Status: state.CompStatusPools, Format: state.CompFormatLeague, Courts: []string{"A"},
	}))
	names := map[string]string{"aki": "Aki", "bea": "Bea", "cal": "Cal"}
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{
		{ID: "aki", Name: "Aki", Dojo: "Dojo Aki"}, {ID: "bea", Name: "Bea", Dojo: "Dojo Bea"}, {ID: "cal", Name: "Cal", Dojo: "Dojo Cal"},
	}}}))
	circle := [][2]string{{"aki", "bea"}, {"bea", "cal"}, {"cal", "aki"}}
	kinds := []string{""}
	if withTiebreakRound {
		kinds = append(kinds, "TB-")
	}
	var matches []state.MatchResult
	for _, kind := range kinds {
		for i, p := range circle {
			matches = append(matches, state.MatchResult{
				ID: fmt.Sprintf("Pool A-%s%d", kind, i), Status: state.MatchStatusCompleted, Court: "A",
				SideA: names[p[0]], SideB: names[p[1]], SideAID: p[0], SideBID: p[1], Winner: names[p[0]], WinnerID: p[0],
			})
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
}

var circleLots = []RankOverride{{PlayerID: "cal", Rank: 1}, {PlayerID: "aki", Rank: 2}, {PlayerID: "bea", Rank: 3}}

// The individual tie's lots are judged by the write itself, under the
// competition's lock: a tie no finished tie-breaker round has left level is
// refused with ErrRankOverrideNotLevel and records nothing, however the
// caller got there.
func TestOverridePoolRanks_IndividualTieNoRoundLeftLevelIsRefusedByTheEngine(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "lots-not-level"
	seedIndividualLeagueCircle(t, store, compID, false)

	changed, err := eng.OverridePoolRanks(compID, "Pool A", circleLots)
	require.ErrorIs(t, err, ErrRankOverrideNotLevel)
	assert.False(t, changed)

	ranks, err := store.LoadPoolRanks(compID)
	require.NoError(t, err)
	assert.Empty(t, ranks["Pool A"], "a refused order records nothing")
}

// Once a tie-break round has gone in a circle too, lots are offered, so the
// same call is accepted and recorded.
func TestOverridePoolRanks_IndividualTieAFinishedRoundLeftLevelTakesTheLots(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "lots-level"
	seedIndividualLeagueCircle(t, store, compID, true)

	changed, err := eng.OverridePoolRanks(compID, "Pool A", circleLots)
	require.NoError(t, err)
	assert.True(t, changed)

	ranks, err := store.LoadPoolRanks(compID)
	require.NoError(t, err)
	assert.Len(t, ranks["Pool A"], 3)
}

// A lots set naming a competitor outside the offered tie is refused as well.
func TestOverridePoolRanks_IndividualLotsOutsideTheOfferedTieAreRefused(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "lots-stranger"
	seedIndividualLeagueCircle(t, store, compID, true)

	_, err := eng.OverridePoolRanks(compID, "Pool A", []RankOverride{{PlayerID: "cal", Rank: 1}, {PlayerID: "zed", Rank: 2}})
	require.ErrorIs(t, err, ErrRankOverrideNotLevel)
}
