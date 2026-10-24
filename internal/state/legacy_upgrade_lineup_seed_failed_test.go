package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// A team ON THE ROSTER that holds no members is one whose member seeding
// failed, not one no member is on (stage 3 review 11, Y9): the lineup repair
// leaves its lineup's ids as they are and logs the skip, since clearing them
// would throw away the placements of members the team has once its members
// can be read. A lineup of a team the roster does not hold is still repaired
// (TestLegacyLineupUpgrade_ClearsAMemberIDOfATeamWithNoMembers).
func TestLineupRepair_KeepsTheIDsOfARosterTeamWithNoMembers(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c1", Name: "C1"}))
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{{Name: "Tora", Dojo: "Tora Dojo"}}))
	players, err := s.LoadParticipants("c1", false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	teamID := players[0].ID
	const placed = "0d0d0d0d-0d0d-4d0d-8d0d-0d0d0d0d0d0d"

	body, err := yaml.Marshal(&struct {
		Lineups []domain.TeamLineup `yaml:"lineups"`
	}{Lineups: []domain.TeamLineup{{
		TeamID: teamID, Round: 0,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): "Ren Abe"},
		MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): placed},
	}}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "lineups.yaml"), body, 0o600))

	// The team's members as a failed seeding leaves them: none.
	roster := &legacyUpgradeRoster{store: s, compID: "c1", squadsLoaded: true, squadsData: map[string][]domain.TeamMember{}}
	require.NoError(t, s.upgradeLineupMemberIDsLocked("c1", roster))

	lineups, err := s.loadTeamLineupsLocked("c1")
	require.NoError(t, err)
	for _, l := range lineups {
		if l.TeamID == teamID {
			require.Equal(t, placed, l.MemberIDs[domain.PositionNumbered(1)], "a roster team's ids are not judged against members it failed to be given")
			return
		}
	}
	t.Fatal("the team's lineup is gone")
}
