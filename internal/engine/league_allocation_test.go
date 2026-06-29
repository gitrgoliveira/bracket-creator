package engine

import (
	"fmt"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// startLeague is a helper: create + start a league competition with the given
// roster across the given courts, returning the generated pool matches.
func startLeague(t *testing.T, compID string, teamSize int, courts []string, players []string) []state.MatchResult {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:         compID,
		Name:       compID,
		Kind:       map[bool]string{true: "team", false: "individual"}[teamSize > 0],
		Format:     state.CompFormatLeague,
		TeamSize:   teamSize,
		PoolSize:   len(players),
		RoundRobin: true,
		Courts:     courts,
		StartTime:  "09:00",
		Status:     "setup",
	}))
	saveTestParticipants(t, store, compID, players)
	require.NoError(t, eng.StartCompetition(compID))
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	return matches
}

// tryStartLeague is like startLeague but returns the StartCompetition error
// instead of failing, so tests can assert the court-cap rejection path.
func tryStartLeague(t *testing.T, compID string, courts []string, players []string) error {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:         compID,
		Name:       compID,
		Kind:       "individual",
		Format:     state.CompFormatLeague,
		PoolSize:   len(players),
		RoundRobin: true,
		Courts:     courts,
		StartTime:  "09:00",
		Status:     "setup",
	}))
	saveTestParticipants(t, store, compID, players)
	return eng.StartCompetition(compID)
}

func names(n int) []string {
	out := make([]string, n)
	for i := 0; i < n; i++ {
		out[i] = fmt.Sprintf("P%d", i+1)
	}
	return out
}

// TestLeagueAllocation_EdgeCases pins how a league (single pool spanning the
// whole roster) allocates its matches to courts across a range of roster/court
// shapes. The core rules under test (engine/pools.go):
//   - a league spreads its matches across ALL assigned courts (no court idle),
//   - a single-court league keeps everything on that one court,
//   - every generated match gets a court when courts are configured,
//   - the round-robin court assignment never co-locates two matches of the SAME
//     round on the same court while another court is free (so same-round matches,
//     which always have distinct participants, can run in parallel).
func TestLeagueAllocation_EdgeCases(t *testing.T) {
	t.Run("single court keeps every match on that court", func(t *testing.T) {
		matches := startLeague(t, "lg-1court", 0, []string{"A"}, names(5))
		require.Len(t, matches, 10) // 5-player round-robin
		for _, m := range matches {
			assert.Equal(t, "A", m.Court, "single-court league: every match on A")
		}
	})

	t.Run("two teams on one court produce exactly one match", func(t *testing.T) {
		// floor(2/2)=1, so a 2-entrant league is capped at a single court.
		matches := startLeague(t, "lg-2team", 2, []string{"A"}, names(2))
		require.Len(t, matches, 1, "2 entrants → a single round-robin match")
		assert.Equal(t, "A", matches[0].Court, "the lone match runs on the only court")
	})

	t.Run("rejects more courts than floor(players/2), extras would sit idle", func(t *testing.T) {
		// The allocation guard (ValidateCourtCount) caps courts at floor(N/2):
		// you can't run more simultaneous matches than there are pairs of players.
		// 2 players → max 1 court; 4 players → max 2 courts.
		require.Error(t, tryStartLeague(t, "lg-cap-2p2c", []string{"A", "B"}, names(2)),
			"2 players with 2 courts must be rejected (cap is 1)")
		require.Error(t, tryStartLeague(t, "lg-cap-4p3c", []string{"A", "B", "C"}, names(4)),
			"4 players with 3 courts must be rejected (cap is 2)")
		// At exactly the cap it is accepted.
		require.NoError(t, tryStartLeague(t, "lg-cap-4p2c", []string{"A", "B"}, names(4)),
			"4 players with 2 courts (== floor(N/2)) must be accepted")
	})

	t.Run("multi-court league uses every assigned court (no court idle)", func(t *testing.T) {
		matches := startLeague(t, "lg-3court", 0, []string{"A", "B", "C"}, names(6))
		require.Len(t, matches, 15)
		used := map[string]int{}
		for _, m := range matches {
			require.NotEmpty(t, m.Court, "every match must have a court")
			used[m.Court]++
		}
		assert.Len(t, used, 3, "all three assigned courts must carry matches")
	})

	t.Run("odd roster (byes) allocates cleanly across courts", func(t *testing.T) {
		// 7 players (odd) → 21 matches; one entrant byes each round. Allocation
		// must not panic and must give every real match a court.
		matches := startLeague(t, "lg-odd", 0, []string{"A", "B"}, names(7))
		require.Len(t, matches, 21)
		for _, m := range matches {
			assert.NotEmpty(t, m.Court, "odd-roster league: every match gets a court")
		}
	})

	t.Run("team league spreads across courts just like an individual league", func(t *testing.T) {
		matches := startLeague(t, "lg-team", 2, []string{"A", "B"}, names(4))
		require.Len(t, matches, 6) // 4-team round-robin
		used := map[string]bool{}
		for _, m := range matches {
			used[m.Court] = true
		}
		assert.Len(t, used, 2, "team league must use both courts")
	})

	t.Run("same-round matches are never stacked on one court while another is free", func(t *testing.T) {
		// For every roster/court combo, within a single round the matches are
		// assigned to DISTINCT courts up to the court count. Same-round matches
		// always have distinct participants, so distinct-court placement lets them
		// run in parallel safely. When a round has more matches than courts, the
		// surplus reuses courts (sequential), which is expected.
		cases := []struct {
			players, courts int
		}{
			{4, 2}, {5, 2}, {6, 2}, {6, 3}, {8, 2}, {8, 3},
		}
		for _, tc := range cases {
			courtLabels := []string{"A", "B", "C", "D"}[:tc.courts]
			id := fmt.Sprintf("lg-round-%dp-%dc", tc.players, tc.courts)
			matches := startLeague(t, id, 0, courtLabels, names(tc.players))
			byRound := map[int][]string{} // round -> courts used (in order)
			for _, m := range matches {
				byRound[m.Round] = append(byRound[m.Round], m.Court)
			}
			for round, used := range byRound {
				// The first min(len(used), courts) matches of a round must be on
				// distinct courts (a round-robin cycle), so no court is double-booked
				// within a round before every court has one match.
				limit := tc.courts
				if len(used) < limit {
					limit = len(used)
				}
				seen := map[string]bool{}
				for i := 0; i < limit; i++ {
					assert.Falsef(t, seen[used[i]],
						"%s round %d: court %q reused within the first %d matches of the round (would idle another court)",
						id, round, used[i], limit)
					seen[used[i]] = true
				}
			}
		}
	})
}
