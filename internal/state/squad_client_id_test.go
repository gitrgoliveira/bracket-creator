package state

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-evts stage 3 review 2, R1: the store's half of a client-made member id.
func TestAddTeamMemberAs(t *testing.T) {
	s, cleanup := newTestStore(t)
	defer cleanup()
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "Teams", Kind: "team", TeamSize: 3}))
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{{Name: "Tora", Dojo: "Tora Dojo"}, {Name: "Kuma", Dojo: "Kuma Dojo"}}))
	teams, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)
	a, b := teams[0].ID, teams[1].ID
	const id = "0a1b2c3d-4e5f-4a6b-8c7d-8e9f0a1b2c3d"

	m, created, err := s.AddTeamMemberAs("c1", a, id, " Ren Abe ")
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, id, m.ID)
	assert.Equal(t, "Ren Abe", m.Name)

	again, created, err := s.AddTeamMemberAsUpTo("c1", a, id, "Ren Abe", 1)
	require.NoError(t, err, "a resend is judged before the limit")
	assert.False(t, created)
	assert.Equal(t, m, again)

	_, _, err = s.AddTeamMemberAs("c1", a, id, "Mei Ito")
	assert.ErrorIs(t, err, ErrTeamMemberIDConflict)
	_, _, err = s.AddTeamMemberAs("c1", b, id, "Ren Abe")
	assert.ErrorIs(t, err, ErrTeamMemberIDConflict)
	_, _, err = s.AddTeamMemberAs("c1", a, "not-an-id", "Mei Ito")
	assert.ErrorIs(t, err, ErrInvalidMemberID)

	assert.True(t, IsClientMemberID(id))
	assert.False(t, IsClientMemberID("0a1b2c3d-4e5f-1a6b-8c7d-8e9f0a1b2c3d"), "a version-1 id")
	assert.False(t, IsClientMemberID("0A1B2C3D-4E5F-4A6B-8C7D-8E9F0A1B2C3D"), "upper case")
}
