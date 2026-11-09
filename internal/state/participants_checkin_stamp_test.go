package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCheckInStampStore makes a store with competition "c" holding Alice and
// Bob, and answers their ids.
func newCheckInStampStore(t *testing.T) (*Store, string, string, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))
	require.NoError(t, s.SaveParticipants("c", []domain.Player{
		{Name: "Alice", Dojo: "Kobe"}, {Name: "Bob", Dojo: "Nara"},
	}))
	players, err := s.LoadParticipants("c", false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	return s, dir, players[0].ID, players[1].ID
}

func participantsFile(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", "c", "participants.csv"))
	require.NoError(t, err)
	return string(raw)
}

func TestCheckInMarker_WriterAndReaderAgree(t *testing.T) {
	cases := []struct {
		in   bool
		at   int64
		want string
	}{
		{true, 1700000000123, "checked_in@1700000000123"},
		{false, 1700000000123, "not_checked_in@1700000000123"},
		{true, 0, "checked_in"},
		{false, 0, ""},
	}
	for _, tc := range cases {
		got := checkInMarker(tc.in, tc.at)
		assert.Equal(t, tc.want, got)
		if got == "" {
			continue
		}
		in, at, ok := parseCheckInMarker(got)
		require.True(t, ok, got)
		assert.Equal(t, tc.in, in, got)
		assert.Equal(t, tc.at, at, got)
	}
	for _, notMarker := range []string{"checked_in@", "checked_in@-5", "checked_in@+5", "checked_in@5x", "checkedin@5", "out@5", "Kobe"} {
		_, _, ok := parseCheckInMarker(notMarker)
		assert.False(t, ok, notMarker)
	}
	in, at, ok := parseCheckInMarker(" CHECKED_IN ")
	assert.True(t, ok && in && at == 0, "the bare legacy marker reads in any case")
}

func TestCheckInStamp_RoundTripsThroughParticipantsCSV(t *testing.T) {
	s, dir, alice, bob := newCheckInStampStore(t)

	_, applied, err := s.SetCheckIn("c", alice, false, true, 1000)
	require.NoError(t, err)
	require.True(t, applied)
	_, applied, err = s.SetCheckIn("c", bob, false, true, 1001)
	require.NoError(t, err)
	require.True(t, applied)
	_, applied, err = s.SetCheckIn("c", bob, false, false, 1002)
	require.NoError(t, err)
	require.True(t, applied)

	file := participantsFile(t, dir)
	assert.Contains(t, file, ",checked_in@1000\n")
	assert.Contains(t, file, ",not_checked_in@1002\n", "a taken-back check-in keeps its stamp")

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	players, err := fresh.LoadParticipants("c", false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	assert.True(t, players[0].CheckedIn)
	assert.Equal(t, int64(1000), players[0].CheckInAt)
	assert.False(t, players[1].CheckedIn)
	assert.Equal(t, int64(1002), players[1].CheckInAt)
	assert.Equal(t, "Nara", players[1].Dojo, "the marker is not read as the dojo")
}

func TestCheckInStamp_LegacyBareMarkerLoadsAtStampZeroWithNoRewrite(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))
	legacy := "3f2504e0-4f89-41d3-9a0c-0305e82c3301,Alice,Kobe,checked_in\n" +
		"3f2504e0-4f89-41d3-9a0c-0305e82c3302,Bob,Nara\n"
	path := filepath.Join(dir, "competitions", "c", "participants.csv")
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	players, err := fresh.LoadParticipants("c", false)
	require.NoError(t, err)
	require.Len(t, players, 2)
	assert.True(t, players[0].CheckedIn)
	assert.Equal(t, int64(0), players[0].CheckInAt)
	assert.False(t, players[1].CheckedIn)
	assert.Equal(t, int64(0), players[1].CheckInAt)
	assert.Equal(t, legacy, participantsFile(t, dir), "loading a legacy roster rewrites nothing")

	// The first stamped tap beats the unstamped legacy check-in.
	p, applied, err := fresh.SetCheckIn("c", players[0].ID, false, false, 5)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.False(t, p.CheckedIn)
}

func TestSetCheckIn_TheLaterTapWins(t *testing.T) {
	s, _, alice, _ := newCheckInStampStore(t)

	p, applied, err := s.SetCheckIn("c", alice, false, true, 2000)
	require.NoError(t, err)
	require.True(t, applied)
	assert.True(t, p.CheckedIn)
	assert.Equal(t, int64(2000), p.CheckInAt)

	// An older take-back that arrives late loses and writes nothing.
	p, applied, err = s.SetCheckIn("c", alice, false, false, 1500)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.True(t, p.CheckedIn, "answered as stored")
	assert.Equal(t, int64(2000), p.CheckInAt)

	// An exact resend is harmless, and a same-stamp tap from another desk
	// loses to the one that arrived first.
	_, applied, err = s.SetCheckIn("c", alice, false, true, 2000)
	require.NoError(t, err)
	assert.False(t, applied)
	p, applied, err = s.SetCheckIn("c", alice, false, false, 2000)
	require.NoError(t, err)
	assert.False(t, applied)
	assert.True(t, p.CheckedIn)

	// A later take-back wins.
	p, applied, err = s.SetCheckIn("c", alice, false, false, 2500)
	require.NoError(t, err)
	assert.True(t, applied)
	assert.False(t, p.CheckedIn)
	assert.Equal(t, int64(2500), p.CheckInAt)

	// The stamp is kept with the take-back, so a late, older check-in loses.
	_, applied, err = s.SetCheckIn("c", alice, false, true, 2400)
	require.NoError(t, err)
	assert.False(t, applied)

	_, _, err = s.SetCheckIn("c", "no-such-id", false, true, 3000)
	assert.ErrorIs(t, err, ErrParticipantNotFound)
}

func TestBulkCheckInAt_JudgesEachIdByItsStamp(t *testing.T) {
	s, _, alice, bob := newCheckInStampStore(t)

	// Bob was taken back later on another desk.
	_, applied, err := s.SetCheckIn("c", bob, false, false, 5000)
	require.NoError(t, err)
	require.True(t, applied)

	res, err := s.BulkCheckInAt("c", []string{alice, bob, "ghost", alice}, 4000)
	require.NoError(t, err)
	assert.Equal(t, 1, res.CheckedIn)
	assert.Equal(t, 0, res.AlreadyCheckedIn)
	assert.Equal(t, []string{"ghost"}, res.NotFound)
	require.Len(t, res.Superseded, 1)
	assert.Equal(t, bob, res.Superseded[0].ID)
	assert.False(t, res.Superseded[0].CheckedIn, "answered as stored")
	assert.Equal(t, int64(5000), res.Superseded[0].CheckInAt)

	// Alice already checked in: counted, and her stamp advances so a change
	// stamped between the two still loses.
	res, err = s.BulkCheckInAt("c", []string{alice}, 6000)
	require.NoError(t, err)
	assert.Equal(t, 0, res.CheckedIn)
	assert.Equal(t, 1, res.AlreadyCheckedIn)
	assert.Empty(t, res.Superseded)
	_, applied, err = s.SetCheckIn("c", alice, false, false, 5500)
	require.NoError(t, err)
	assert.False(t, applied)

	players, err := s.LoadParticipants("c", false)
	require.NoError(t, err)
	assert.True(t, players[0].CheckedIn)
	assert.Equal(t, int64(6000), players[0].CheckInAt)
	assert.False(t, players[1].CheckedIn)
}

func TestKeepStoredCheckIns_CarriesTheStamp(t *testing.T) {
	stored := []domain.Player{
		{ID: "1", Name: "Alice", Dojo: "Kobe", CheckedIn: true, CheckInAt: 10},
		{ID: "2", Name: "Bob", Dojo: "Nara", CheckInAt: 20},
		{Name: "Cleo", Dojo: "Gifu", CheckedIn: true, CheckInAt: 30},
	}
	sent := []domain.Player{
		{ID: "1", Name: "Alice", Dojo: "Kobe"},
		{ID: "2", Name: "Bob", Dojo: "Nara", CheckedIn: true, CheckInAt: 99},
		{Name: "Cleo", Dojo: "Gifu"},
		{ID: "4", Name: "Dan", Dojo: "Oita", CheckedIn: true, CheckInAt: 99},
	}
	got := keepStoredCheckIns(sent, stored)
	assert.True(t, got[0].CheckedIn)
	assert.Equal(t, int64(10), got[0].CheckInAt, "the id arm")
	assert.False(t, got[1].CheckedIn)
	assert.Equal(t, int64(20), got[1].CheckInAt, "a taken-back stamp is kept too")
	assert.True(t, got[2].CheckedIn)
	assert.Equal(t, int64(30), got[2].CheckInAt, "the no-id arm")
	assert.False(t, got[3].CheckedIn)
	assert.Equal(t, int64(0), got[3].CheckInAt, "a new competitor starts unstamped")
}

func TestFingerprint_CheckInStampsAreNormalised(t *testing.T) {
	const head = "3f2504e0-4f89-41d3-9a0c-0305e82c3301,Alice,Kobe"
	fp := func(rows ...string) string {
		return Fingerprint(map[string][]byte{
			"competitions/c/participants.csv": []byte(strings.Join(rows, "")),
		})
	}
	never := fp(head + "\n")
	legacy := fp(head + ",checked_in\n")

	assert.Equal(t, legacy, fp(head+",checked_in@100\n"), "a stamped check-in fingerprints as the bare one")
	assert.Equal(t, fp(head+",checked_in@100\n"), fp(head+",checked_in@200\n"), "a restamp changes nothing")
	assert.Equal(t, never, fp(head+",not_checked_in@300\n"), "a check-in taken back fingerprints like one never made")
	assert.NotEqual(t, never, legacy, "a real check-in changes the fingerprint")

	// A data column that only reads like a marker in another case is data.
	const titled = "3f2504e0-4f89-41d3-9a0c-0305e82c3301,Alice,Not_checked_in@300\n"
	assert.NotEqual(t, never, fp(titled))
	// Two columns are never a marker, as the reader's guard says.
	assert.NotEqual(t, fp("Alice,checked_in@5\n"), fp("Alice,checked_in@6\n"))
}
