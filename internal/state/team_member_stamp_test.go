package state

// team_member_stamp_test.go pins the server stamp a team member carries
// (operator decision 2026-10-07: "Do it in this PR"). Every write that creates a
// member or changes its name stamps it, under the store's lock, with the later of
// the server's time in milliseconds and one more than that member's previous
// stamp, so one member's stamps only grow. A client holding two copies of a member
// keeps the one with the larger stamp. A member no write of this release has
// touched carries 0, and a file written before the field existed round-trips byte
// for byte.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// memberOf is the stored member of a team, read back from the file.
func memberOf(t *testing.T, s *Store, compID, teamID, memberID string) domain.TeamMember {
	t.Helper()
	squads, err := s.LoadSquads(compID)
	require.NoError(t, err)
	for _, m := range squads[teamID] {
		if m.ID == memberID {
			return m
		}
	}
	require.Failf(t, "no such member", "%s", memberID)
	return domain.TeamMember{}
}

// serverClock stands the store's clock still at t and returns what moves it.
func serverClock(s *Store, t time.Time) (set func(time.Time)) {
	now := t
	s.clock = func() time.Time { return now }
	return func(to time.Time) { now = to }
}

func TestTeamMemberStamp_GrowsForOneMemberAcrossEveryWrite(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	at := time.UnixMilli(1_760_000_000_000)
	serverClock(s, at)

	added, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)
	assert.Equal(t, at.UnixMilli(), added.ModifiedAt, "a new member is stamped with the server's time")

	renamed, err := s.RenameTeamMember(id, teamA, added.ID, "Alicia")
	require.NoError(t, err)
	assert.Equal(t, added.ModifiedAt+1, renamed.ModifiedAt, "the clock has not moved, so the stamp grows by one")
	assert.Equal(t, "Alicia", renamed.Name)

	cleared, err := s.ClearTeamMemberName(id, teamA, added.ID)
	require.NoError(t, err)
	assert.Equal(t, renamed.ModifiedAt+1, cleared.ModifiedAt)
	assert.Empty(t, cleared.Name)

	again, err := s.RenameTeamMember(id, teamA, added.ID, "Alice B")
	require.NoError(t, err)
	assert.Equal(t, cleared.ModifiedAt+1, again.ModifiedAt)

	stored := memberOf(t, s, id, teamA, added.ID)
	assert.Equal(t, again, stored, "what each write answers is what it stored")
}

func TestTeamMemberStamp_FollowsTheClockWhenItMovesOn(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	at := time.UnixMilli(1_760_000_000_000)
	moveTo := serverClock(s, at)
	added, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)

	moveTo(at.Add(time.Hour))
	renamed, err := s.RenameTeamMember(id, teamA, added.ID, "Alicia")
	require.NoError(t, err)

	assert.Equal(t, at.Add(time.Hour).UnixMilli(), renamed.ModifiedAt)
}

// A server whose clock is set back between two writes still orders one member's
// writes: the stamp is never lower than the previous one plus one.
func TestTeamMemberStamp_StillGrowsAfterTheClockStepsBack(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	at := time.UnixMilli(1_760_000_000_000)
	moveTo := serverClock(s, at)
	added, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)

	moveTo(at.Add(-10 * time.Minute))
	renamed, err := s.RenameTeamMember(id, teamA, added.ID, "Alicia")
	require.NoError(t, err)
	cleared, err := s.ClearTeamMemberName(id, teamA, added.ID)
	require.NoError(t, err)

	assert.Equal(t, added.ModifiedAt+1, renamed.ModifiedAt)
	assert.Equal(t, added.ModifiedAt+2, cleared.ModifiedAt)
}

func TestTeamMemberStamp_NamingABlankMemberStampsIt(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	at := time.UnixMilli(1_760_000_000_000)
	serverClock(s, at)
	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	blank := squads[teamA][0]
	require.Empty(t, blank.Name)
	require.Zero(t, blank.ModifiedAt)

	named, err := s.NameUnnamedTeamMember(id, teamA, blank.ID, "Ren Abe")
	require.NoError(t, err)

	assert.Equal(t, at.UnixMilli(), named.ModifiedAt)
	assert.Equal(t, "Ren Abe", named.Name)
	assert.Equal(t, blank.Index, named.Index)
	assert.Equal(t, named, memberOf(t, s, id, teamA, blank.ID))
}

func TestTeamMemberStamp_AWriteStampsOnlyTheMemberItWrites(t *testing.T) {
	s, id, teamA, teamB := newSquadTestStore(t)
	serverClock(s, time.UnixMilli(1_760_000_000_000))
	other, err := s.AddTeamMember(id, teamA, "Bob")
	require.NoError(t, err)
	third, err := s.AddTeamMember(id, teamB, "Carol")
	require.NoError(t, err)
	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	seeded := squads[teamA][0]

	target, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)
	_, err = s.RenameTeamMember(id, teamA, target.ID, "Alicia")
	require.NoError(t, err)

	assert.Equal(t, other, memberOf(t, s, id, teamA, other.ID), "a teammate is untouched")
	assert.Equal(t, third, memberOf(t, s, id, teamB, third.ID), "so is a member of another team")
	assert.Equal(t, seeded, memberOf(t, s, id, teamA, seeded.ID))
}

func TestTeamMemberStamp_SeededSlotsAndMembersNoWriteHasTouchedCarryZero(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)

	require.NotEmpty(t, squads[teamA])
	for _, m := range squads[teamA] {
		assert.Zerof(t, m.ModifiedAt, "seeded slot %d", m.Index)
	}
}

func TestTeamMemberStamp_ShapeOnTheWireAndOnDisk(t *testing.T) {
	t.Run("json always sends it, 0 for an untouched member", func(t *testing.T) {
		raw, err := json.Marshal(domain.TeamMember{ID: "m", Index: 1})

		require.NoError(t, err)
		assert.JSONEq(t, `{"id":"m","index":1,"name":"","modifiedAt":0}`, string(raw))
	})

	t.Run("yaml leaves it out at 0 and keeps it otherwise", func(t *testing.T) {
		untouched, err := yaml.Marshal(domain.TeamMember{ID: "m", Index: 1, Name: "A"})
		require.NoError(t, err)
		stamped, err := yaml.Marshal(domain.TeamMember{ID: "m", Index: 1, Name: "A", ModifiedAt: 1760000000000})
		require.NoError(t, err)

		assert.NotContains(t, string(untouched), "modifiedAt")
		assert.Contains(t, string(stamped), "modifiedAt: 1760000000000")
	})
}

// A team-members.yaml written before the field existed loads, and saves back as
// the same bytes: nothing in it gains a key.
func TestTeamMemberStamp_ALegacyFileRoundTripsByteForByte(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	legacy := "members:\n    " + teamA + ":\n        - id: 11111111-1111-4111-8111-111111111111\n          index: 1\n          name: Ito\n        - id: 22222222-2222-4222-8222-222222222222\n          index: 2\n          name: \"\"\n"
	path := filepath.Join(s.GetFolder(), "competitions", id, teamMembersFilename)
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))
	squads, err := s.loadSquadsLocked(id)
	require.NoError(t, err)

	require.NoError(t, s.saveSquadsLocked(id, squads, s.directWrite))

	raw, err := os.ReadFile(path) // #nosec G304 -- a path under the test store
	require.NoError(t, err)
	assert.Equal(t, legacy, string(raw))
	for _, m := range squads[teamA] {
		assert.Zero(t, m.ModifiedAt)
	}
}

func TestTeamMemberStamp_AWriteLeavesTheOthersOfALegacyFileAsTheyWere(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	legacy := "members:\n    " + teamA + ":\n        - id: 11111111-1111-4111-8111-111111111111\n          index: 1\n          name: Ito\n        - id: 22222222-2222-4222-8222-222222222222\n          index: 2\n          name: \"\"\n"
	path := filepath.Join(s.GetFolder(), "competitions", id, teamMembersFilename)
	require.NoError(t, os.WriteFile(path, []byte(legacy), 0o600))
	serverClock(s, time.UnixMilli(1_760_000_000_000))

	_, err := s.NameUnnamedTeamMember(id, teamA, "22222222-2222-4222-8222-222222222222", "Ueno")
	require.NoError(t, err)

	raw, err := os.ReadFile(path) // #nosec G304 -- a path under the test store
	require.NoError(t, err)
	assert.Equal(t, "members:\n    "+teamA+":\n        - id: 11111111-1111-4111-8111-111111111111\n          index: 1\n          name: Ito\n        - id: 22222222-2222-4222-8222-222222222222\n          index: 2\n          name: Ueno\n          modifiedAt: 1760000000000\n", string(raw))
}
