package idstamp

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStampPlayerID_DeterministicAndDojoScoped(t *testing.T) {
	a1 := StampPlayerID("Tanaka Kenji", "Tokyo")
	a2 := StampPlayerID("Tanaka Kenji", "Tokyo")
	b := StampPlayerID("Tanaka Kenji", "Osaka")
	require.Equal(t, a1, a2, "the same (name, dojo) pair must always derive the same id")
	assert.NotEqual(t, a1, b, "a different dojo must derive a different id for the same name")
	assert.NotEmpty(t, a1)
}

func TestStampIDs_StampsPlayersAndFillsMatchSides(t *testing.T) {
	players := []domain.Player{
		{Name: "Alice", Dojo: "Dojo A"},
		{Name: "Bob", Dojo: "Dojo B"},
	}
	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted},
	}

	StampIDs(players, matches)

	require.NotEmpty(t, players[0].ID)
	require.NotEmpty(t, players[1].ID)
	assert.Equal(t, players[0].ID, matches[0].SideAID)
	assert.Equal(t, players[1].ID, matches[0].SideBID)
	assert.Equal(t, players[0].ID, matches[0].WinnerID, "Winner matches SideA's name, so WinnerID must resolve to SideA's id")
}

func TestStampIDs_LeavesExistingFieldsUntouched(t *testing.T) {
	players := []domain.Player{
		{ID: "explicit-id", Name: "Alice", Dojo: "Dojo A"},
		{Name: "Bob", Dojo: "Dojo B"},
	}
	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideAID: "stale-foreign-id", SideB: "Bob", Winner: "Bob"},
	}

	StampIDs(players, matches)

	assert.Equal(t, "explicit-id", players[0].ID, "a player that already has an id must be left alone")
	assert.Equal(t, "stale-foreign-id", matches[0].SideAID, "a match that already has SideAID must be left alone, even if it doesn't match the roster")
	assert.Equal(t, players[1].ID, matches[0].SideBID)
	assert.Equal(t, players[1].ID, matches[0].WinnerID)
}

func TestStampIDs_SameNameSideDoesNotGuessWinner(t *testing.T) {
	// A genuine same-name pairing (two different competitors sharing a
	// display name, e.g. across dojos) cannot be disambiguated by name
	// alone; StampIDs must not guess which one won.
	players := []domain.Player{
		{Name: "Tanaka Kenji", Dojo: "Tokyo"},
		{Name: "Tanaka Kenji", Dojo: "Osaka"},
	}
	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Tanaka Kenji", SideAID: "tokyo-id", SideB: "Tanaka Kenji", SideBID: "osaka-id", Winner: "Tanaka Kenji"},
	}

	StampIDs(players, matches)

	assert.Empty(t, matches[0].WinnerID, "a same-name pairing's winner cannot be resolved by name and must be left for the fixture to set by hand")
}

func TestStampPoolIDs(t *testing.T) {
	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice", Dojo: "Dojo A"},
			{ID: "explicit", Name: "Bob", Dojo: "Dojo B"},
		}},
	}

	StampPoolIDs(pools)

	assert.NotEmpty(t, pools[0].Players[0].ID)
	assert.Equal(t, "explicit", pools[0].Players[1].ID, "a player that already has an id must be left alone")
}

func TestStampStandingIDs(t *testing.T) {
	standings := []state.PlayerStanding{
		{Player: domain.Player{Name: "Alice", Dojo: "Dojo A"}},
		{Player: domain.Player{ID: "explicit", Name: "Bob", Dojo: "Dojo B"}},
	}

	StampStandingIDs(standings)

	assert.NotEmpty(t, standings[0].Player.ID)
	assert.Equal(t, "explicit", standings[1].Player.ID)
}
