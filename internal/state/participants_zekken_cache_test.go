package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// withZekkenName changes the PARSE, so it has to be part of the participants
// cache key. It was not, and a single read with the wrong flag left the entry
// every later reader hits: column 3 of a zekken roster is the zekken, and with
// the flag off it is read as the dojo. Eligibility, Swiss, ranking and the
// dojo-conflict avoidance in pool creation all go through LoadParticipants, so
// each of them saw "ZEK1" as the competitor's dojo until some write invalidated
// the entry.
//
// Reached in production from the PUT /seeds validator, which read the roster
// with a hard-coded false to check names against it.
func TestParticipantsCacheKeepsZekkenParsesApart(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{
		ID: "c1", Name: "C1", Date: "11-06-2026", WithZekkenName: true,
	}))
	row := "11111111-1111-4111-8111-111111111111,Alice Smith,ZEK1,Dojo One\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "competitions", "c1", "participants.csv"), []byte(row), 0o644))

	// The wrong-flag read first, which is the order the bug needed.
	off, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, off, 1)

	on, err := s.LoadParticipants("c1", true)
	require.NoError(t, err)
	require.Len(t, on, 1)
	require.Equal(t, "Alice Smith", on[0].Name)
	require.Equal(t, "Dojo One", on[0].Dojo,
		"a prior withZekkenName=false read must not leave the zekken cached as the dojo")
	require.Equal(t, "ZEK1", on[0].DisplayName)

	// And the reverse order: the zekken read must not poison the plain one.
	dir2 := t.TempDir()
	s2, err := NewStore(dir2)
	require.NoError(t, err)
	require.NoError(t, s2.SaveCompetition(&Competition{ID: "c2", Name: "C2", Date: "11-06-2026"}))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir2, "competitions", "c2", "participants.csv"), []byte(row), 0o644))
	_, err = s2.LoadParticipants("c2", true)
	require.NoError(t, err)
	plain, err := s2.LoadParticipants("c2", false)
	require.NoError(t, err)
	require.Equal(t, "ZEK1", plain[0].Dojo,
		"without the zekken flag column 3 IS the dojo; the zekken read must not have changed that")
}
