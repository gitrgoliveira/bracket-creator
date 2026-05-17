package engine

import (
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// swissTestPlayer is a compact constructor for participant rows used by
// the Swiss engine tests. Each player gets a fresh UUID v4 so the
// eligibility / kiken paths can resolve them by ID consistently with
// the rest of the engine code.
func swissTestPlayer(name string) domain.Player {
	return domain.Player{
		ID:   helper.NewUUID4(),
		Name: name,
		Dojo: name + "-Dojo",
	}
}

// setupSwissCompetition creates a minimal Swiss competition with the
// given player names and seed assignments (nil = no seeds). Returns
// the engine, store, comp ID, and a name → player map for the test to
// look up IDs when seeding CompetitorStatus.
func setupSwissCompetition(t *testing.T, names []string, seeds map[string]int, rounds int) (*Engine, *state.Store, string, map[string]domain.Player) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	compID := "swiss-test"

	comp := &state.Competition{
		ID:                compID,
		Name:              "Swiss Test",
		Kind:              "individual",
		Format:            state.CompFormatSwiss,
		SwissRounds:       rounds,
		Courts:            []string{"A", "B"},
		StartTime:         "09:00",
		Status:            state.CompStatusSetup,
		PoolMatchDuration: 3,
	}
	require.NoError(t, store.SaveCompetition(comp))

	players := make([]domain.Player, len(names))
	byName := make(map[string]domain.Player, len(names))
	for i, n := range names {
		p := swissTestPlayer(n)
		if seedRank, ok := seeds[n]; ok {
			p.Seed = seedRank
		}
		players[i] = p
		byName[n] = p
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	// Persist seed assignments so resolveSeedsForSwiss can find them
	// (matches the same shape pools/playoffs use). Engine.Generate
	// reads the seeds field on each player which SaveParticipants
	// preserves, but ApplySeeds defensively re-applies from seeds.csv.
	var seedAssignments []domain.SeedAssignment
	for name, rank := range seeds {
		seedAssignments = append(seedAssignments, domain.SeedAssignment{Name: name, SeedRank: rank})
	}
	if len(seedAssignments) > 0 {
		require.NoError(t, store.SaveSeeds(compID, seedAssignments))
	}

	return eng, store, compID, byName
}

// completeSwissMatch persists a completed match outcome with the
// winner ippons set to two (kote/men/dou doesn't matter for standings).
// SideA/SideB are looked up off the supplied match.
func completeSwissMatch(t *testing.T, store *state.Store, compID, matchID, winner string) {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	found := false
	for i := range matches {
		if matches[i].ID == matchID {
			matches[i].Winner = winner
			matches[i].Status = state.MatchStatusCompleted
			switch winner {
			case matches[i].SideA:
				matches[i].IpponsA = []string{"M", "M"}
				matches[i].IpponsB = nil
			case matches[i].SideB:
				matches[i].IpponsB = []string{"M", "M"}
				matches[i].IpponsA = nil
			}
			found = true
			break
		}
	}
	require.True(t, found, "match %s not found in pool-matches.csv", matchID)
	require.NoError(t, store.SavePoolMatches(compID, matches))
}

// TestSwissRound1FoldPairing verifies FR-050b: when seeds are present,
// round 1 uses fold pairing — seed 1 vs seed N, 2 vs N-1, etc.
func TestSwissRound1FoldPairing(t *testing.T) {
	names := []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"}
	seeds := map[string]int{
		"P1": 1, "P2": 2, "P3": 3, "P4": 4,
		"P5": 5, "P6": 6, "P7": 7, "P8": 8,
	}
	eng, _, compID, _ := setupSwissCompetition(t, names, seeds, 3)

	matches, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)
	require.Len(t, matches, 4, "8 players ⇒ 4 matches in round 1")

	// Expected fold pairings (seed 1 vs 8, 2 vs 7, …).
	expected := [][2]string{
		{"P1", "P8"},
		{"P2", "P7"},
		{"P3", "P6"},
		{"P4", "P5"},
	}

	// Each match has both sides populated, all unique IDs, and the
	// pairing matches the fold expectation (either A vs B or B vs A
	// is acceptable — assignment of seat A/B is implementation detail).
	seen := make(map[string]bool)
	for i, m := range matches {
		require.NotEmpty(t, m.SideA, "match %d missing SideA", i)
		require.NotEmpty(t, m.SideB, "match %d missing SideB", i)
		require.NotEqual(t, m.SideA, m.SideB, "match %d: SideA and SideB cannot be the same player", i)
		require.False(t, seen[m.ID], "duplicate match ID %s", m.ID)
		seen[m.ID] = true

		want := expected[i]
		got := [2]string{m.SideA, m.SideB}
		match := (got == want) || (got == [2]string{want[1], want[0]})
		assert.True(t, match, "match %d: want %v, got %v", i, want, got)
	}
}

// TestSwissRound1RandomPairingNoSeeds verifies FR-050b: with no seeds,
// round 1 produces N/2 matches with no rematches and every player
// included exactly once.
func TestSwissRound1RandomPairingNoSeeds(t *testing.T) {
	names := []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"}
	eng, _, compID, _ := setupSwissCompetition(t, names, nil, 3)

	matches, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)
	require.Len(t, matches, 4, "8 players ⇒ 4 matches in round 1")

	seenPlayer := make(map[string]int)
	for i, m := range matches {
		require.NotEmpty(t, m.SideA, "match %d missing SideA", i)
		require.NotEmpty(t, m.SideB, "match %d missing SideB", i)
		require.NotEqual(t, m.SideA, m.SideB, "match %d: SideA and SideB cannot be the same player", i)
		seenPlayer[m.SideA]++
		seenPlayer[m.SideB]++
	}
	for _, n := range names {
		assert.Equal(t, 1, seenPlayer[n], "player %s should appear exactly once in round 1", n)
	}
}

// TestSwissRound2PairsByWins verifies FR-050c: round 2 pairs players
// with equal win counts. After round 1 with P1..P4 winning, round 2
// should pair within the 1-win and 0-win groups (no rematches).
func TestSwissRound2PairsByWins(t *testing.T) {
	names := []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"}
	seeds := map[string]int{
		"P1": 1, "P2": 2, "P3": 3, "P4": 4,
		"P5": 5, "P6": 6, "P7": 7, "P8": 8,
	}
	eng, store, compID, _ := setupSwissCompetition(t, names, seeds, 3)

	r1, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)
	require.NoError(t, store.SavePoolMatches(compID, r1))

	// Fold pairings: P1 v P8, P2 v P7, P3 v P6, P4 v P5.
	// Award wins to P1..P4 (the top-seeded sides).
	winners := []string{"P1", "P2", "P3", "P4"}
	for _, w := range winners {
		// Find the match this winner is in and complete it.
		var matchID string
		for _, m := range r1 {
			if m.SideA == w || m.SideB == w {
				matchID = m.ID
				break
			}
		}
		require.NotEmpty(t, matchID, "could not find match for winner %s", w)
		completeSwissMatch(t, store, compID, matchID, w)
	}

	r2, err := eng.GenerateSwissRound(compID, 2)
	require.NoError(t, err)
	require.Len(t, r2, 4, "round 2 should still have 4 matches for 8 players")

	// Build the rematch set from round 1 to detect violations.
	rematch := func(a, b string) string {
		if a < b {
			return a + "|" + b
		}
		return b + "|" + a
	}
	prior := make(map[string]bool)
	for _, m := range r1 {
		prior[rematch(m.SideA, m.SideB)] = true
	}

	winnersSet := map[string]bool{"P1": true, "P2": true, "P3": true, "P4": true}
	losersSet := map[string]bool{"P5": true, "P6": true, "P7": true, "P8": true}

	for _, m := range r2 {
		key := rematch(m.SideA, m.SideB)
		assert.False(t, prior[key], "round 2 pair %s vs %s is a rematch of round 1", m.SideA, m.SideB)

		aWin := winnersSet[m.SideA]
		bWin := winnersSet[m.SideB]
		aLos := losersSet[m.SideA]
		bLos := losersSet[m.SideB]
		assert.True(t, (aWin && bWin) || (aLos && bLos),
			"round 2 should pair winners with winners and losers with losers, got %s (win=%v los=%v) vs %s (win=%v los=%v)",
			m.SideA, aWin, aLos, m.SideB, bWin, bLos)
	}
}

// TestSwissOddPlayerBye verifies FR-050c: with 7 players, the lowest-
// ranked unmatched player gets a bye (auto-completed win, 0 points).
// Without seeds, "lowest-ranked" is the player with no wins yet so far
// in round 1 every player is at 0 wins — the implementation picks one
// deterministically. We assert there is exactly one bye, that the bye
// player has no SideB and Status = Completed, and that the remaining
// 3 matches cover all other 6 players exactly once.
func TestSwissOddPlayerBye(t *testing.T) {
	names := []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7"}
	seeds := map[string]int{
		"P1": 1, "P2": 2, "P3": 3, "P4": 4,
		"P5": 5, "P6": 6, "P7": 7,
	}
	eng, _, compID, _ := setupSwissCompetition(t, names, seeds, 3)

	matches, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)
	require.Len(t, matches, 4, "7 players ⇒ 3 played matches + 1 bye = 4 entries")

	var byes []state.MatchResult
	playerCount := make(map[string]int)
	for _, m := range matches {
		if m.SideB == "" {
			byes = append(byes, m)
			playerCount[m.SideA]++
			continue
		}
		playerCount[m.SideA]++
		playerCount[m.SideB]++
	}
	require.Len(t, byes, 1, "exactly one bye expected")
	bye := byes[0]
	assert.Equal(t, state.MatchStatusCompleted, bye.Status, "bye must be auto-completed")
	assert.Equal(t, bye.SideA, bye.Winner, "bye player wins the bye")
	assert.Empty(t, bye.IpponsA, "bye carries 0 points scored")
	// Per FR-050c "lowest-ranked unmatched player receives a bye." When
	// seeds are provided we pick the lowest-seeded (highest seed number)
	// player. With no prior round results it should land on the highest
	// seed: P7.
	assert.Equal(t, "P7", bye.SideA, "lowest-ranked seed (P7) should receive the round-1 bye")

	for _, n := range names {
		assert.Equal(t, 1, playerCount[n], "player %s should appear exactly once across played+bye matches", n)
	}
}

// TestSwissKikenExclusion verifies FR-050f: a kiken'd or fusenpai'd
// player is excluded from subsequent round pairings; their would-be
// opponent receives a bye.
func TestSwissKikenExclusion(t *testing.T) {
	names := []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"}
	seeds := map[string]int{
		"P1": 1, "P2": 2, "P3": 3, "P4": 4,
		"P5": 5, "P6": 6, "P7": 7, "P8": 8,
	}
	eng, store, compID, byName := setupSwissCompetition(t, names, seeds, 3)

	// Round 1: fold pairing P1v8, P2v7, P3v6, P4v5.
	r1, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)
	require.NoError(t, store.SavePoolMatches(compID, r1))

	// Complete all round-1 matches: top seeds win.
	for _, w := range []string{"P1", "P2", "P3", "P4"} {
		for _, m := range r1 {
			if m.SideA == w || m.SideB == w {
				completeSwissMatch(t, store, compID, m.ID, w)
				break
			}
		}
	}

	// Mark P3 ineligible (kiken). Engine.GenerateSwissRound must skip
	// P3 in round-2 pairings; the player who would have paired with
	// P3 (within the 1-win group: P1, P2, P3, P4) gets a bye instead.
	p3 := byName["P3"]
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID:   p3.ID,
		Eligible:   false,
		Reason:     "kiken at Swiss-R1-2",
		MatchID:    "Swiss-R1-2",
		RecordedAt: time.Now().UTC(),
	}))

	r2, err := eng.GenerateSwissRound(compID, 2)
	require.NoError(t, err)

	// Round 2 should have entries for all *active* players. P3 is out:
	// 7 active players → 3 played matches + 1 bye = 4 entries.
	require.Len(t, r2, 4, "7 active players ⇒ 3 played + 1 bye = 4 round-2 entries")

	playerSeen := make(map[string]int)
	byeCount := 0
	for _, m := range r2 {
		assert.NotEqual(t, "P3", m.SideA, "P3 (kiken) must not appear in round 2")
		assert.NotEqual(t, "P3", m.SideB, "P3 (kiken) must not appear in round 2")
		if m.SideB == "" {
			byeCount++
			playerSeen[m.SideA]++
			continue
		}
		playerSeen[m.SideA]++
		playerSeen[m.SideB]++
	}
	assert.Equal(t, 1, byeCount, "exactly one bye expected (opponent of the kiken'd player)")
	for n := range byName {
		if n == "P3" {
			assert.Equal(t, 0, playerSeen[n], "P3 should not be paired in round 2 (ineligible)")
			continue
		}
		assert.Equal(t, 1, playerSeen[n], "active player %s should appear exactly once in round 2", n)
	}
}

// TestSwissStandingsRanking verifies FR-050e: cumulative standings
// rank by wins → points scored → head-to-head. Runs an 8-player
// 4-round Swiss tournament with deterministic results and asserts
// the top-of-table ordering.
func TestSwissStandingsRanking(t *testing.T) {
	names := []string{"P1", "P2", "P3", "P4", "P5", "P6", "P7", "P8"}
	seeds := map[string]int{
		"P1": 1, "P2": 2, "P3": 3, "P4": 4,
		"P5": 5, "P6": 6, "P7": 7, "P8": 8,
	}
	eng, store, compID, _ := setupSwissCompetition(t, names, seeds, 4)

	// Helper: generate the next round, append it to the persisted pool-
	// matches.csv (preserving prior rounds), and complete all played
	// matches by awarding wins to a caller-supplied set of winners.
	//
	// GenerateSwissRound returns ONLY the new round's matches — the
	// caller is responsible for merging with prior rounds before save.
	// This mirrors the HTTP handler shape: POST /swiss/generate-round
	// loads existing pool-matches, calls the engine, appends, saves.
	playRound := func(roundNum int, winners map[string]bool) {
		t.Helper()
		ms, err := eng.GenerateSwissRound(compID, roundNum)
		require.NoError(t, err)
		prior, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		merged := append([]state.MatchResult{}, prior...)
		merged = append(merged, ms...)
		require.NoError(t, store.SavePoolMatches(compID, merged))
		for _, m := range ms {
			if m.SideB == "" {
				// Already a completed bye — skip.
				continue
			}
			winner := ""
			if winners[m.SideA] {
				winner = m.SideA
			} else if winners[m.SideB] {
				winner = m.SideB
			} else {
				// Default: SideA wins when neither is explicitly named.
				winner = m.SideA
			}
			completeSwissMatch(t, store, compID, m.ID, winner)
		}
	}

	// Round 1: top seeds win.
	playRound(1, map[string]bool{"P1": true, "P2": true, "P3": true, "P4": true})
	// Round 2: P1 + P2 stay perfect, P3/P4 lose. P5/P6/P7/P8 group:
	// give P5 a win and P6 a win.
	playRound(2, map[string]bool{"P1": true, "P2": true, "P5": true, "P6": true})
	// Round 3: P1 stays perfect; P2 loses. P3/P5/P6 win their matches.
	playRound(3, map[string]bool{"P1": true, "P3": true, "P5": true, "P6": true})
	// Round 4: P1 stays perfect.
	playRound(4, map[string]bool{"P1": true, "P3": true, "P5": true, "P6": true})

	standings, err := eng.SwissStandings(compID)
	require.NoError(t, err)
	require.NotEmpty(t, standings)

	// Build a name → standing map and assert ordering invariants
	// rather than exact rank numbers (the tiebreaker chain accepts
	// any deterministic resolution under the configured outcomes).
	byName := make(map[string]state.PlayerStanding, len(standings))
	for _, s := range standings {
		byName[s.Player.Name] = s
	}

	// P1 won all 4 rounds and must be rank 1.
	require.Equal(t, "P1", standings[0].Player.Name, "P1 (4 wins) must be top of final standings")
	assert.Equal(t, 4, byName["P1"].Wins)

	// Every player has a rank assigned, 1..N.
	for i, s := range standings {
		assert.Equal(t, i+1, s.Rank, "standing %d should have Rank=%d", i, i+1)
	}

	// Sum of all wins across players equals total wins awarded. Each
	// played round awards exactly one win per match; a bye also awards
	// one win. 4 rounds × 4 matches = 16 wins total.
	totalWins := 0
	for _, s := range standings {
		totalWins += s.Wins
	}
	assert.Equal(t, 16, totalWins, "4 rounds × 4 wins per round = 16 cumulative wins")

	// SwissStandings returns one entry per participant (regression
	// guard against duplicate or missing rows).
	require.Len(t, standings, len(names))

	// IDs are stable: the standings entry for each player carries the
	// original participant's name unchanged (no truncation / casing
	// drift through the pipeline).
	for _, n := range names {
		_, ok := byName[n]
		assert.True(t, ok, "player %s missing from standings", n)
	}

	// Sanity: rank ordering respects wins (descending). Within a
	// tie-bucket we accept any deterministic order — but a player with
	// more wins must rank higher than a player with fewer.
	for i := 1; i < len(standings); i++ {
		assert.GreaterOrEqual(t, standings[i-1].Wins, standings[i].Wins,
			"standings[%d] wins=%d should be >= standings[%d] wins=%d",
			i-1, standings[i-1].Wins, i, standings[i].Wins)
	}

	// Match-ID format sanity: every match generated should carry an
	// ID prefixed "Swiss-R{N}-" so the existing pool-match scoring
	// infrastructure can route it back to the right round.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range matches {
		assert.True(t, strings.HasPrefix(m.ID, "Swiss-R"),
			"match ID %q should have Swiss-R prefix", m.ID)
	}
}

// TestCurrentSwissRoundCompleted covers all branches of the function.
func TestCurrentSwissRoundCompleted(t *testing.T) {
	t.Run("initial state (SwissCurrentRound==0) is always complete", func(t *testing.T) {
		eng, store, compID, _ := setupSwissCompetition(t, []string{"A", "B", "C", "D"}, nil, 3)
		// SwissCurrentRound defaults to 0 → immediately returns true.
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		require.Equal(t, 0, comp.SwissCurrentRound)

		done, err := eng.CurrentSwissRoundCompleted(compID)
		require.NoError(t, err)
		assert.True(t, done)
	})

	t.Run("returns false when current round has incomplete matches", func(t *testing.T) {
		eng, store, compID, _ := setupSwissCompetition(t, []string{"A", "B", "C", "D"}, nil, 3)

		// Generate round 1 and save (without completing any match).
		ms, err := eng.GenerateSwissRound(compID, 1)
		require.NoError(t, err)
		require.NoError(t, store.SavePoolMatches(compID, ms))

		// Bump SwissCurrentRound to 1 so CurrentSwissRoundCompleted checks round 1.
		_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
			c.SwissCurrentRound = 1
			return c, nil
		})
		require.NoError(t, err)

		done, err := eng.CurrentSwissRoundCompleted(compID)
		require.NoError(t, err)
		assert.False(t, done, "incomplete round 1 must return false")
	})

	t.Run("returns true when all matches in current round are completed", func(t *testing.T) {
		eng, store, compID, _ := setupSwissCompetition(t, []string{"A", "B", "C", "D"}, nil, 3)

		ms, err := eng.GenerateSwissRound(compID, 1)
		require.NoError(t, err)
		require.NoError(t, store.SavePoolMatches(compID, ms))
		_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
			c.SwissCurrentRound = 1
			return c, nil
		})
		require.NoError(t, err)

		for _, m := range ms {
			completeSwissMatch(t, store, compID, m.ID, m.SideA)
		}

		done, err := eng.CurrentSwissRoundCompleted(compID)
		require.NoError(t, err)
		assert.True(t, done)
	})

	t.Run("competition not found returns error", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		_, err := eng.CurrentSwissRoundCompleted("nonexistent-comp")
		assert.Error(t, err)
	})
}

// TestSwissRoundNotCompletedError_Error pins the Error() string format.
func TestSwissRoundNotCompletedError_Error(t *testing.T) {
	err := &SwissRoundNotCompletedError{CompID: "my-comp", Round: 3}
	msg := err.Error()
	assert.Contains(t, msg, "3")
	assert.Contains(t, msg, "my-comp")
}

// TestAdvanceSwissRound covers the main happy path and error branches.
func TestAdvanceSwissRound(t *testing.T) {
	t.Run("happy path: first round advance", func(t *testing.T) {
		eng, store, compID, _ := setupSwissCompetition(t,
			[]string{"A", "B", "C", "D"}, nil, 3)

		// Generate round 1, complete all matches, then advance.
		ms, err := eng.GenerateSwissRound(compID, 1)
		require.NoError(t, err)
		require.NoError(t, store.SavePoolMatches(compID, ms))
		_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
			c.SwissCurrentRound = 1
			return c, nil
		})
		require.NoError(t, err)
		for _, m := range ms {
			completeSwissMatch(t, store, compID, m.ID, m.SideA)
		}

		newMatches, nextRound, err := eng.AdvanceSwissRound(compID)
		require.NoError(t, err)
		assert.Equal(t, 2, nextRound)
		assert.NotEmpty(t, newMatches)

		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		assert.Equal(t, 2, comp.SwissCurrentRound)
	})

	t.Run("competition not found", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		_, _, err := eng.AdvanceSwissRound("no-such-comp")
		assert.Error(t, err)
	})

	t.Run("non-swiss format returns validation error", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "pools-comp", Format: state.CompFormatPools, Status: state.CompStatusPools,
		}))
		_, _, err := eng.AdvanceSwissRound("pools-comp")
		assert.Error(t, err)
	})

	t.Run("all rounds already completed returns error", func(t *testing.T) {
		eng, store, compID, _ := setupSwissCompetition(t,
			[]string{"A", "B", "C", "D"}, nil, 1) // 1 round max

		_, err := store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
			c.SwissCurrentRound = 1 // already at max
			return c, nil
		})
		require.NoError(t, err)

		_, _, err = eng.AdvanceSwissRound(compID)
		assert.Error(t, err)
	})

	t.Run("current round not complete returns SwissRoundNotCompletedError", func(t *testing.T) {
		eng, store, compID, _ := setupSwissCompetition(t,
			[]string{"A", "B", "C", "D"}, nil, 3)

		ms, err := eng.GenerateSwissRound(compID, 1)
		require.NoError(t, err)
		require.NoError(t, store.SavePoolMatches(compID, ms))
		_, err = store.UpdateCompetitionChanged(compID, func(c *state.Competition) (*state.Competition, error) {
			c.SwissCurrentRound = 1
			return c, nil
		})
		require.NoError(t, err)
		// Intentionally NOT completing the matches.

		_, _, err = eng.AdvanceSwissRound(compID)
		var notDone *SwissRoundNotCompletedError
		assert.ErrorAs(t, err, &notDone,
			"AdvanceSwissRound with an incomplete round must return SwissRoundNotCompletedError")
	})
}

// TestParseSwissMatchRound covers the error branches: non-matching
// prefix, no dash, and non-numeric round component.
func TestParseSwissMatchRound(t *testing.T) {
	// Prefix is "Swiss-R" (capital R).
	tests := []struct {
		id     string
		wantN  int
		wantOK bool
	}{
		{"Swiss-R1-m0", 1, true},    // happy path
		{"Pool A-0", 0, false},      // wrong prefix
		{"Swiss-R1", 0, false},      // no dash in remainder
		{"Swiss-Rabc-m0", 0, false}, // non-numeric round
		{"Swiss-R0-m0", 0, false},   // n < 1
	}
	for _, tc := range tests {
		n, ok := parseSwissMatchRound(tc.id)
		assert.Equal(t, tc.wantOK, ok, "id=%q", tc.id)
		if ok {
			assert.Equal(t, tc.wantN, n, "id=%q", tc.id)
		}
	}
}

// TestPickByeFromOrdered covers the two missing branches:
// empty-ordered (returns "") and all-had-bye (returns last).
func TestPickByeFromOrdered(t *testing.T) {
	t.Run("empty ordered returns empty string", func(t *testing.T) {
		assert.Equal(t, "", pickByeFromOrdered(nil, map[string]bool{}))
	})

	t.Run("all players already had bye returns last", func(t *testing.T) {
		ordered := []string{"P3", "P2", "P1"}
		hadBye := map[string]bool{"P1": true, "P2": true, "P3": true}
		// All had byes → returns last element.
		assert.Equal(t, "P1", pickByeFromOrdered(ordered, hadBye))
	})

	t.Run("some players without bye returns last-without-bye", func(t *testing.T) {
		ordered := []string{"P3", "P2", "P1"}
		hadBye := map[string]bool{"P1": true}
		// Iterates from end: P1 has bye → P2 has no bye → returns P2.
		assert.Equal(t, "P2", pickByeFromOrdered(ordered, hadBye))
	})
}

// TestParseWinnerOf covers the error branch (no match) and the happy
// path for the "Winner of r1-m0" pattern.
func TestParseWinnerOf(t *testing.T) {
	t.Run("valid pattern", func(t *testing.T) {
		round, idx := parseWinnerOf("Winner of r1-m2", 4)
		// depth=1, numRounds=4 → round = 4-1 = 3; matchIdx = 2.
		assert.Equal(t, 3, round)
		assert.Equal(t, 2, idx)
	})

	t.Run("invalid pattern returns -1,-1", func(t *testing.T) {
		round, idx := parseWinnerOf("not a winner string", 4)
		assert.Equal(t, -1, round)
		assert.Equal(t, -1, idx)
	})
}

// TestSwissStandings_Draw verifies that a draw (hikiwake) increments
// both players' Draws counter in SwissStandings.
func TestSwissStandings_Draw(t *testing.T) {
	eng, store, compID, _ := setupSwissCompetition(t,
		[]string{"A", "B", "C", "D"}, nil, 3)

	ms, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)
	// Set the first match as a draw (hikiwake), winner A for the rest.
	for i, m := range ms {
		if m.SideB == "" {
			continue // bye
		}
		if i == 0 {
			ms[i].Status = state.MatchStatusCompleted
			ms[i].Winner = ""
			ms[i].Decision = string(state.DecisionDraw)
		} else {
			ms[i].Status = state.MatchStatusCompleted
			ms[i].Winner = m.SideA
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, ms))

	standings, err := eng.SwissStandings(compID)
	require.NoError(t, err)
	require.NotEmpty(t, standings)

	// Players in the draw (ms[0].SideA and ms[0].SideB) must each have 1 draw.
	if len(ms) > 0 && ms[0].SideB != "" {
		byName := make(map[string]state.PlayerStanding, len(standings))
		for _, s := range standings {
			byName[s.Player.Name] = s
		}
		assert.Equal(t, 1, byName[ms[0].SideA].Draws, "SideA of draw match must have 1 draw")
		assert.Equal(t, 1, byName[ms[0].SideB].Draws, "SideB of draw match must have 1 draw")
	}
}

// TestMaybeLockTeamLineupsForRoundViaRecordResult exercises the non-tx
// variant of maybeLockTeamLineupsForRound through RecordMatchResult on
// a team competition.
func TestMaybeLockTeamLineupsForRoundViaRecordResult(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "mlttr-nonteam-team"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, TeamSize: 5, Format: "pools",
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusScheduled},
	}))
	// Save a lineup so LockTeamLineupsForRound has something to lock.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "p1",
			domain.PosJiho:    "p2",
			domain.PosChuken:  "p3",
			domain.PosFukusho: "p4",
			domain.PosTaisho:  "p5",
		},
	}, 5))

	// RecordMatchResult with Running status triggers maybeLockTeamLineupsForRound.
	err := eng.RecordMatchResult(compID, "Pool A-0", &state.MatchResult{
		SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
	})
	require.NoError(t, err)

	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	got := lineups["RedTeam-0"]
	assert.NotNil(t, got.LockedAt, "lineup must be locked after RecordMatchResult with Running status")
}
