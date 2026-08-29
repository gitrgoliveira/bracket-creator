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

// bc-cse: the app deliberately ALLOWS two competitors to share a display name
// when their dojos differ (helper.CheckDuplicateEntriesByNameDojo only
// refuses same-name AND same-dojo). Pool/league standings used to be keyed by
// bare name (computeStandingsFrom in scoring.go), so two same-named
// competitors in one pool collapsed to a single standings row and win
// attribution fell back to a raw name comparison that both sides satisfy.
// These tests pin the fix: standings are keyed by standingsPlayerKey
// (id-preferring, name-fallback for legacy data) and the winning side is
// resolved by WinnerID when present.
const (
	snIDTokyo    = "11111111-1111-4111-8111-111111111111" // Tanaka Kenji, Tokyo
	snIDOsaka    = "22222222-2222-4222-8222-222222222222" // Tanaka Kenji, Osaka
	snIDSuzuki   = "33333333-3333-4333-8333-333333333333"
	snIDWatanabe = "44444444-4444-4444-8444-444444444444"
)

// snPlayers returns the canonical 4-player fixture shared by the pool and
// league tests below: two "Tanaka Kenji" from different dojos, plus two
// uniquely-named competitors. A strict dominance chain (Osaka > Suzuki >
// Watanabe > Tokyo) makes every competitor's W/L record distinct, so a
// collapsed or misattributed row is immediately visible in the assertions.
func snPlayers() []domain.Player {
	return []domain.Player{
		{ID: snIDTokyo, Name: "Tanaka Kenji", Dojo: "Tokyo"},
		{ID: snIDOsaka, Name: "Tanaka Kenji", Dojo: "Osaka"},
		{ID: snIDSuzuki, Name: "Suzuki Hiro", Dojo: "Nagoya"},
		{ID: snIDWatanabe, Name: "Watanabe Ryo", Dojo: "Kyoto"},
	}
}

// snRank orders the fixture so the winner of any pairing is simply the side
// with the higher rank; this reproduces a full round robin with every
// competitor finishing on a distinct record (Osaka 3-0, Suzuki 2-1,
// Watanabe 1-2, Tokyo 0-3).
func snRank(id string) int {
	switch id {
	case snIDOsaka:
		return 4
	case snIDSuzuki:
		return 3
	case snIDWatanabe:
		return 2
	case snIDTokyo:
		return 1
	}
	return 0
}

// TestCalculatePoolStandings_SameNameDifferentDojo is the pool-standings
// regression: two same-name, different-dojo competitors placed in ONE pool
// must appear as two distinct rows, each with its own W/L record.
func TestCalculatePoolStandings_SameNameDifferentDojo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-samename"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Pool Same Name",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	players := snPlayers()
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: players},
	}))

	// Full round robin (6 matches). SideAID/SideBID/WinnerID are stamped as
	// the real generation + scoring pipeline would stamp them (pools.go sets
	// SideAID/SideBID at generation; backfillMatchIdentity resolves WinnerID
	// from a WinnerSide/name hint at score time) -- this fixture skips
	// straight to the persisted shape both paths produce.
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

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	// Must be 4 distinct rows, not 3: a name-keyed merge collapses the two
	// "Tanaka Kenji" entries into one.
	require.Len(t, poolA, 4, "same-name-different-dojo competitors must both appear")

	byID := make(map[string]state.PlayerStanding, len(poolA))
	for _, s := range poolA {
		byID[s.Player.ID] = s
	}
	require.Contains(t, byID, snIDTokyo)
	require.Contains(t, byID, snIDOsaka)

	assert.Equal(t, 3, byID[snIDOsaka].Wins, "Osaka Tanaka beats everyone, including the derby")
	assert.Equal(t, 0, byID[snIDOsaka].Losses)
	assert.Equal(t, 0, byID[snIDTokyo].Wins, "Tokyo Tanaka loses every match, including the derby")
	assert.Equal(t, 3, byID[snIDTokyo].Losses)
	assert.Equal(t, 2, byID[snIDSuzuki].Wins)
	assert.Equal(t, 1, byID[snIDSuzuki].Losses)
	assert.Equal(t, 1, byID[snIDWatanabe].Wins)
	assert.Equal(t, 2, byID[snIDWatanabe].Losses)
}

// TestLeagueStandings_SameNameDifferentDojo is the headline reachable case
// from the bug report: "a league is a single pool holding every competitor
// ... for a league this needs no unusual draw at all: any two same-name
// competitors collide." Drives the REAL write path (StartCompetition +
// RecordMatchResult, exactly as the mobile-app handler would) rather than
// hand-crafted pool-matches.csv rows, so this also exercises
// backfillMatchIdentity's WinnerID resolution from a WinnerSide hint.
func TestLeagueStandings_SameNameDifferentDojo(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "league-samename"

	createTestCompetition(t, store, compID, state.CompFormatLeague, 4)
	require.NoError(t, store.SaveParticipants(compID, snPlayers()))
	require.NoError(t, eng.StartCompetition(compID))

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 6, "4-player round robin -> 6 matches")

	for _, m := range matches {
		winnerName, winnerSide := m.SideA, "A"
		if snRank(m.SideBID) > snRank(m.SideAID) {
			winnerName, winnerSide = m.SideB, "B"
		}
		require.NoError(t, eng.RecordMatchResult(compID, m.ID, &state.MatchResult{
			ID: m.ID, SideA: m.SideA, SideB: m.SideB,
			Winner: winnerName, WinnerSide: winnerSide, Status: state.MatchStatusCompleted,
		}))
	}

	standings, err := eng.LeagueStandings(compID)
	require.NoError(t, err)
	require.Len(t, standings, 4, "same-name-different-dojo competitors must both appear")

	byID := make(map[string]state.PlayerStanding, len(standings))
	for _, s := range standings {
		byID[s.Player.ID] = s
	}
	assert.Equal(t, 3, byID[snIDOsaka].Wins, "Osaka Tanaka beats everyone, including the derby")
	assert.Equal(t, 0, byID[snIDOsaka].Losses)
	assert.Equal(t, 0, byID[snIDTokyo].Wins, "Tokyo Tanaka loses every match, including the derby")
	assert.Equal(t, 3, byID[snIDTokyo].Losses)
	assert.Equal(t, 2, byID[snIDSuzuki].Wins)
	assert.Equal(t, 1, byID[snIDWatanabe].Wins)
}

// TestSwissStandings_SameNameDifferentDojo_KnownLimitation is NOT a fix
// verification: it pins the CURRENT, still-unfixed behavior for Swiss.
//
// Unlike pool/league matches, a Swiss match persists no per-side id at all
// (buildSwissMatches in swiss.go sets only SideA/SideB; the SPA's write
// payload only ever echoes an id it already received from the server, so it
// never invents one either). The whole Swiss pairing pipeline -- rematch
// avoidance (priorPair), win/bye tracking (wins, hadBye), and rank ordering
// (buildRankByName) -- treats display name as the pairing identity end to
// end, not just the final standings tally.
//
// Applying the same standingsPlayerKey fix used for computeStandingsFrom
// here would NOT close this gap: the roster would key by "id:<uuid>" (real
// participant ids are available) while every match lookup would still
// resolve to "name:<name>" (match-side ids are always empty), so the two
// could never agree and EVERY Swiss standings tally would silently stop
// working, not just the same-name case. This is exactly the failure mode
// computeEngiSwissStandings' doc comment (engi.go) already documents and
// deliberately avoids for its own Swiss twin. Closing it for real requires
// threading participant ids through the pairing generation itself
// (computeSwissPairings / buildSwissMatches and their name-keyed maps), which
// is a materially larger change than this fix and is reported separately
// rather than shipped as a partial/no-op edit here.
func TestSwissStandings_SameNameDifferentDojo_KnownLimitation(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "swiss-samename"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                       compID,
		Name:                     "Swiss Same Name",
		Kind:                     "individual",
		Format:                   state.CompFormatSwiss,
		SwissRounds:              1,
		Courts:                   []string{"A"},
		StartTime:                "09:00",
		Status:                   state.CompStatusSetup,
		PoolMatchDurationSeconds: 180,
	}))
	require.NoError(t, store.SaveParticipants(compID, snPlayers()))

	ms, err := eng.GenerateSwissRound(compID, 1)
	require.NoError(t, err)
	require.NoError(t, store.SavePoolMatches(compID, ms))

	for i := range ms {
		if ms[i].SideB == "" {
			continue // bye, already completed
		}
		completeSwissMatch(t, store, compID, ms[i].ID, ms[i].SideA)
	}

	standings, err := eng.SwissStandings(compID)
	require.NoError(t, err)
	// Known limitation: the two "Tanaka Kenji" entries collapse to one row
	// (3, not 4). If this ever starts asserting 4, the underlying data gap
	// described above has been closed -- update this test to assert the
	// fixed behavior instead of the limitation.
	assert.Len(t, standings, 3, "documents the current same-name collapse in Swiss standings (see doc comment)")
}
