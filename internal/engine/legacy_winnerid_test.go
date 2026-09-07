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
// wire shape. Pre-bc-pnum, resolveWinnerSide fell back to the roster for
// exactly this shape, crediting the winner despite the row's own missing
// side ids. The bc-pnum operator ruling removed that fallback: an empty id
// (here, the row's own SideAID/SideBID) resolves to nothing, so this legacy
// shape now credits nobody -- see the two "NoLongerResolves" tests below,
// converted from the pre-ruling tests that pinned the opposite. These
// fixtures use unique names (no namesake ambiguity) to isolate that from the
// separate namesake tie-break policy covered by TestResolveSwissRosterKey_*
// and TestSwissFieldKeysFromMatches_* in swiss_test.go.
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

// TestCalculatePoolStandings_LegacyWinnerIDWithoutSideIDs used to pin the
// pool/league standings side of the pre-bc-pnum fix: a completed pool match
// carrying WinnerID but no SideAID/SideBID still credited the winner's win
// and the loser's loss, by cross-referencing WinnerID against the roster
// since the row itself carried no side ids to compare against.
//
// CONVERTED (not deleted) under the bc-pnum operator ruling: resolveWinnerSide
// now compares m.WinnerID ONLY against m.SideAID/m.SideBID on the SAME row,
// with no roster fallback at all. A row whose own SideAID/SideBID are both
// empty can never match WinnerID, no matter how real WinnerID's participant
// id is, so this legacy shape now resolves to nobody -- the same "empty id
// resolves to nothing" rule the override/rename/participant-index paths
// converted to (see TestCalculatePoolStandings_Override_LegacyBareNameKeyIsUnresolvable
// in pool_rank_override_test.go for the sibling conversion). This asserts
// the new, opposite behaviour: neither side is credited.
func TestCalculatePoolStandings_LegacyWinnerIDWithoutSideIDsNoLongerResolves(t *testing.T) {
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
	assert.Equal(t, 0, byID[lwIDAlpha].Wins, "a row with no SideAID/SideBID resolves to nobody, even with a real WinnerID (operator ruling bc-pnum)")
	assert.Equal(t, 0, byID[lwIDAlpha].Losses)
	assert.Equal(t, 0, byID[lwIDBeta].Wins)
	assert.Equal(t, 0, byID[lwIDBeta].Losses, "the loser is likewise not credited: the row simply contributes nothing")
}

// TestSwissStandings_LegacyWinnerIDWithoutSideIDs is the Swiss twin of
// TestCalculatePoolStandings_LegacyWinnerIDWithoutSideIDsNoLongerResolves
// above (see that test's doc comment for the bc-pnum conversion rationale):
// the same mixed shape (WinnerID set, SideAID/SideBID empty) fed through
// SwissStandings, which shares resolveWinnerSide with the pool path, must
// likewise resolve to nobody.
func TestSwissStandings_LegacyWinnerIDWithoutSideIDsNoLongerResolves(t *testing.T) {
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
	assert.Equal(t, 0, byID[lwIDAlpha].Wins, "a row with no SideAID/SideBID resolves to nobody, even with a real WinnerID (operator ruling bc-pnum)")
	assert.Equal(t, 0, byID[lwIDAlpha].Losses)
	assert.Equal(t, 0, byID[lwIDBeta].Wins)
	assert.Equal(t, 0, byID[lwIDBeta].Losses, "the loser is likewise not credited: the row simply contributes nothing")
}
