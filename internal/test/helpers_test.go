package test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateTestFS(t *testing.T) {
	fs := CreateTestFS(t)

	// Check that we can read the template.xlsx file
	file, err := fs.Open("template.xlsx")
	require.NoError(t, err)
	defer file.Close()

	// The fact that we can open the file is enough for this test
	t.Log("Successfully created and accessed test filesystem")
}

func TestCreateTestPlayers(t *testing.T) {
	players := CreateTestPlayers()

	// Check that we have the expected number of players
	require.Len(t, players, 2)

	// Check the first player
	assert.Equal(t, "player1", players[0].ID)
	assert.Equal(t, "John Doe", players[0].Name)

	// Check the second player
	assert.Equal(t, "player2", players[1].ID)
	assert.Equal(t, "Jane Smith", players[1].Name)
}

func TestCreateTestPools(t *testing.T) {
	pools := CreateTestPools()

	// Check that we have pools
	require.NotEmpty(t, pools)

	// Check the first pool
	assert.Equal(t, "pool1", pools[0].ID)

	// Check the pool has players
	assert.NotEmpty(t, pools[0].Players)

	// Check the pool has matches
	assert.NotEmpty(t, pools[0].Matches)
}

func TestCreateTestTournament(t *testing.T) {
	tournament := CreateTestTournament()

	// Check the tournament name
	assert.Equal(t, "Test Tournament", tournament.Name)

	// Check that we have pools
	require.NotEmpty(t, tournament.Pools)

	// Check that we have elimination matches
	require.NotEmpty(t, tournament.EliminationMatches)
}
