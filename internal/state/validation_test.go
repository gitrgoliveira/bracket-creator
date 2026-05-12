package state

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStore_SaveInvalidDate(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-validation-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	tourney := &Tournament{
		Name:  "Invalid Date Tournament",
		Date:  "60510-02-20",
		Venue: "Chaos Arena",
	}

	// Should not crash, just save the string
	err = store.SaveTournament(tourney)
	require.NoError(t, err)

	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	require.Equal(t, "60510-02-20", loaded.Date)
}

func TestStore_SaveInvalidCompetitionDate(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-validation-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	comp := &Competition{
		ID:   "invalid-date-comp",
		Name: "Invalid Date Comp",
		Date: "60510-02-20",
	}

	// Should not crash, just save the string
	err = store.SaveCompetition(comp)
	require.NoError(t, err)

	loaded, err := store.LoadCompetition("invalid-date-comp")
	require.NoError(t, err)
	require.Equal(t, "60510-02-20", loaded.Date)
}
