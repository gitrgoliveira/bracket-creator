package state

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

func TestSaveRoster_KeepsTheStoredCheckInsAndRefusesAStaleVersion(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&Competition{ID: "c", Name: "C"}))
	require.NoError(t, store.SaveParticipants("c", []domain.Player{
		{Name: "Alice", Dojo: "A", CheckedIn: true}, {Name: "Bob", Dojo: "B"},
	}))
	stored, err := store.LoadParticipantsOpt("c", false, LoadParticipantsOpts{WithSeeds: true})
	require.NoError(t, err)
	v0 := RosterVersion(stored)

	// The write says Alice is not checked in and Bob is: neither is written.
	next := []domain.Player{stored[0], stored[1], {Name: "Carol", Dojo: "C", CheckedIn: true}}
	next[0].CheckedIn, next[1].CheckedIn = false, true
	next[1].Seed = 1
	seedsErr, err := store.SaveRoster("c", next, []domain.SeedAssignment{{ID: next[1].ID, Name: "Bob", Dojo: "B", SeedRank: 1}}, v0)
	require.NoError(t, err)
	require.NoError(t, seedsErr)
	got, err := store.LoadParticipantsOpt("c", false, LoadParticipantsOpts{WithSeeds: true})
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.True(t, got[0].CheckedIn, "Alice keeps her stored check-in")
	assert.False(t, got[1].CheckedIn, "Bob is not checked in by a roster write")
	assert.False(t, got[2].CheckedIn, "a competitor new to the roster is not checked in by it")
	assert.Equal(t, 1, got[1].Seed)
	assert.NotEqual(t, v0, RosterVersion(got))

	// A write built from v0 now is refused with the roster as it stands.
	_, err = store.SaveRoster("c", stored, nil, v0)
	var changed *RosterChangedError
	require.True(t, errors.As(err, &changed), "%v", err)
	assert.ErrorIs(t, err, ErrRosterChanged)
	assert.Equal(t, RosterVersion(got), changed.Version)
	assert.Len(t, changed.Players, 3)
	after, err := store.LoadParticipants("c", false)
	require.NoError(t, err)
	assert.Len(t, after, 3, "a refused write writes nothing")
}

func TestSaveRoster_ALegacyRowWithoutAnIdKeepsItsCheckIn(t *testing.T) {
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	writeLegacyRoster(t, store, "legacy", "Alice,DojoA,checked_in\nBob,DojoB\n")
	stored, err := store.LoadParticipants("legacy", false)
	require.NoError(t, err)
	require.True(t, stored[0].CheckedIn)
	require.Empty(t, stored[0].ID)

	// The page gives every row an id on its first save.
	next := []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "DojoA"},
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob", Dojo: "DojoB", CheckedIn: true},
	}
	_, err = store.SaveRoster("legacy", next, nil, "")
	require.NoError(t, err)
	got, err := store.LoadParticipants("legacy", false)
	require.NoError(t, err)
	assert.True(t, got[0].CheckedIn, "a row saved before ids existed is found by its name and dojo")
	assert.False(t, got[1].CheckedIn)
}

func TestRosterVersion_ReadsTheRosterNotItsCheckIns(t *testing.T) {
	a := []domain.Player{{ID: "1", Name: "A", Dojo: "D"}, {ID: "2", Name: "B", Dojo: "D"}}
	b := []domain.Player{{ID: "1", Name: "A", Dojo: "D", CheckedIn: true}, {ID: "2", Name: "B", Dojo: "D"}}
	assert.Equal(t, RosterVersion(a), RosterVersion(b), "a check-in is not a roster change")
	moved := []domain.Player{a[1], a[0]}
	assert.NotEqual(t, RosterVersion(a), RosterVersion(moved), "the order is")
	seeded := []domain.Player{a[0], {ID: "2", Name: "B", Dojo: "D", Seed: 1}}
	assert.NotEqual(t, RosterVersion(a), RosterVersion(seeded), "a seed is")
	// Field boundaries are not ambiguous.
	x := []domain.Player{{ID: "1", Name: "AB", Dojo: "C"}}
	y := []domain.Player{{ID: "1", Name: "A", Dojo: "BC"}}
	assert.NotEqual(t, RosterVersion(x), RosterVersion(y))
}
