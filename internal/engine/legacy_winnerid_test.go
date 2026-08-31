package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Legacy Swiss/TB/DH rows written before bc-cse stamped SideAID/SideBID:
// the client resolved winnerId against the roster and sent it alongside the
// winner's display name, but the per-side id fields never existed on that
// wire shape. resolveWinnerSide used to treat "WinnerID set" as license to
// compare it ONLY against SideAID/SideBID -- both empty on these rows, an
// unwinnable comparison that credited nobody. These fixtures use unique
// names (no namesake ambiguity) to isolate that bug from the separate
// namesake tie-break policy covered by TestResolveSwissRosterKey_* and
// TestSwissFieldKeysFromMatches_* in swiss_test.go.
const (
	lwIDAlpha = "aaaaaaaa-1111-4111-8111-111111111111"
	lwIDBeta  = "bbbbbbbb-2222-4222-8222-222222222222"
)

func legacyWinnerIDPlayers() []domain.Player {
	return []domain.Player{
		{ID: lwIDAlpha, Name: "Alpha Competitor", Dojo: "Dojo A"},
		{ID: lwIDBeta, Name: "Beta Competitor", Dojo: "Dojo B"},
	}
}

// TestCalculatePoolStandings_LegacyWinnerIDWithoutSideIDs pins the pool/
// league standings side of the fix: a completed pool match carrying
// WinnerID but no SideAID/SideBID must still credit the winner's win and
// the loser's loss.
func TestCalculatePoolStandings_LegacyWinnerIDWithoutSideIDs(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pool-legacy-winnerid"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Pool Legacy WinnerID",
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}))

	players := legacyWinnerIDPlayers()
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: players},
	}))

	// The pre-bc-cse wire shape: WinnerID resolved against the roster,
	// Winner set to the matching display name, but SideAID/SideBID never
	// stamped.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:       "Pool A-0",
			SideA:    "Alpha Competitor",
			SideB:    "Beta Competitor",
			Winner:   "Alpha Competitor",
			WinnerID: lwIDAlpha,
			Status:   state.MatchStatusCompleted,
		},
	}))

	standings, err := eng.CalculatePoolStandings(compID)
	require.NoError(t, err)
	poolA := standings["Pool A"]
	require.Len(t, poolA, 2)

	byID := make(map[string]state.PlayerStanding, len(poolA))
	for _, s := range poolA {
		byID[s.Player.ID] = s
	}
	assert.Equal(t, 1, byID[lwIDAlpha].Wins, "WinnerID must credit the win even with no SideAID/SideBID on the row")
	assert.Equal(t, 0, byID[lwIDAlpha].Losses)
	assert.Equal(t, 0, byID[lwIDBeta].Wins)
	assert.Equal(t, 1, byID[lwIDBeta].Losses, "the losing side must also be credited, not just silently skipped")
}

// TestSwissStandings_LegacyWinnerIDWithoutSideIDs is the Swiss twin: the
// same mixed shape (WinnerID set, SideAID/SideBID empty) fed through
// SwissStandings, which shares resolveWinnerSide with the pool path.
func TestSwissStandings_LegacyWinnerIDWithoutSideIDs(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "swiss-legacy-winnerid"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                       compID,
		Name:                     "Swiss Legacy WinnerID",
		Kind:                     "individual",
		Format:                   state.CompFormatSwiss,
		SwissRounds:              1,
		Courts:                   []string{"A"},
		StartTime:                "09:00",
		Status:                   state.CompStatusSetup,
		PoolMatchDurationSeconds: 180,
	}))
	players := legacyWinnerIDPlayers()
	require.NoError(t, store.SaveParticipants(compID, players))

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:       "Swiss-R1-0",
			SideA:    "Alpha Competitor",
			SideB:    "Beta Competitor",
			Winner:   "Alpha Competitor",
			WinnerID: lwIDAlpha,
			Status:   state.MatchStatusCompleted,
		},
	}))

	standings, err := eng.SwissStandings(compID)
	require.NoError(t, err)
	require.Len(t, standings, 2)

	byID := make(map[string]state.PlayerStanding, len(standings))
	for _, s := range standings {
		byID[s.Player.ID] = s
	}
	assert.Equal(t, 1, byID[lwIDAlpha].Wins, "WinnerID must credit the win even with no SideAID/SideBID on the row")
	assert.Equal(t, 0, byID[lwIDAlpha].Losses)
	assert.Equal(t, 0, byID[lwIDBeta].Wins)
	assert.Equal(t, 1, byID[lwIDBeta].Losses, "the losing side must also be credited, not just silently skipped")
}
