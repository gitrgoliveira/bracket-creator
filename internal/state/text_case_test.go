package state_test

// text_case_test.go pins the stored form of typed text at the store's doors
// (operator decisions D14 and D20, 2026-11-01): a competitor's name and dojo
// and a team member's name are title-cased, a zekken is written in capitals.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddAndUpdateParticipant_StoreTheCanonicalText(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1", WithZekkenName: true}))

	added, err := s.AddParticipant("c1", domain.Player{Name: "rin sato", DisplayName: "rin", Dojo: "seibukan"}, true)
	require.NoError(t, err)
	assert.Equal(t, "Rin Sato", added.Name)
	assert.Equal(t, "RIN", added.DisplayName)
	assert.Equal(t, "Seibukan", added.Dojo)

	require.NoError(t, s.SaveSeeds("c1", []domain.SeedAssignment{{ID: added.ID, Name: added.Name, Dojo: added.Dojo, SeedRank: 1}}))
	before := stampFiles(t, dir, "seeds.csv")

	// A case-only edit is no edit of the competitor's identity, so it leaves
	// the seeding alone.
	updated, err := s.UpdateParticipant("c1", added.ID, true, func(p *domain.Player) error {
		p.Dojo = "seibukan"
		p.DisplayName = "r. sato"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "Seibukan", updated.Dojo)
	assert.Equal(t, "R. SATO", updated.DisplayName)
	assert.Equal(t, before, stampFiles(t, dir, "seeds.csv"), "a case-only edit does not rewrite seeds.csv")

	// Title-casing never lowers a letter (an acronym such as "BKA" stays), so
	// a dojo typed in capitals is a new spelling and the seeding follows it.
	updated, err = s.UpdateParticipant("c1", added.ID, true, func(p *domain.Player) error {
		p.Dojo = "SEIBUKAN"
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, "SEIBUKAN", updated.Dojo)
	assert.Contains(t, stampFiles(t, dir, "seeds.csv")["seeds.csv"].body, "SEIBUKAN")
}

// The bulk roster save stores the canonical text too, not only a single add.
func TestSaveParticipants_StoresTheCanonicalText(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	require.NoError(t, s.SaveCompetition(&state.Competition{ID: "c1", Name: "C1", WithZekkenName: true}))
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{{Name: "yuki tanaka", DisplayName: "yuki", Dojo: "tobukan"}}))
	stamp := stampFiles(t, dir, "participants.csv")["participants.csv"]
	assert.Contains(t, stamp.body, "Yuki Tanaka")
	assert.Contains(t, stamp.body, "YUKI")
	assert.Contains(t, stamp.body, "Tobukan")
	assert.NotContains(t, stamp.body, "tobukan")
}

func TestTeamMemberNames_AreTitleCasedAndLineupsFollow(t *testing.T) {
	_, s := newLegacyUpgradeFixture(t)
	teamID := legacyUpgradeTeams(t, s, "Tora")[0]

	added, err := s.AddTeamMember("c1", teamID, "  sato ken ")
	require.NoError(t, err)
	assert.Equal(t, "Sato Ken", added.Name)

	require.NoError(t, s.SetTeamLineup("c1", domain.TeamLineup{
		TeamID: teamID, Round: 0,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): added.Name},
		MemberIDs: map[domain.Position]string{domain.PositionNumbered(1): added.ID},
	}, 3))

	renamed, err := s.RenameTeamMember("c1", teamID, added.ID, "mori jun")
	require.NoError(t, err)
	assert.Equal(t, "Mori Jun", renamed.Name)
	lineups, err := s.LoadTeamLineups("c1")
	require.NoError(t, err)
	l, ok := roundZeroLineup(lineups, teamID)
	require.True(t, ok)
	assert.Equal(t, "Mori Jun", l.Positions[domain.PositionNumbered(1)], "the lineup follows the rename by member id")

	blank, err := s.AddTeamMember("c1", teamID, "")
	require.NoError(t, err)
	named, err := s.NameUnnamedTeamMember("c1", teamID, blank.ID, "ito aki")
	require.NoError(t, err)
	assert.Equal(t, "Ito Aki", named.Name)
}
