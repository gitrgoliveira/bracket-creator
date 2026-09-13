package state_test

// legacy_upgrade_submatch_ids_test.go pins the bc-tmid pass 2 EXTENSION of
// the existing pool-matches.csv / bracket.json id repairs
// (upgradePoolMatchSideIDsLocked / upgradeBracketSideIDsLocked): a
// sub-bout row missing its member ids is resolved against the two TEAMS'
// OWN squads, using the match's OWN already-resolved SideAID/SideBID,
// exactly as legacy_upgrade.go's header comment documents. The match-level
// triple is set up already-resolved in every fixture below, so the sub-bout
// resolution is exercised in isolation from the match-level repair itself
// (covered by legacy_upgrade_pool_ids_test.go / legacy_upgrade_bracket_ids_test.go).

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacyPoolMatchSubBoutMemberIDUpgradeOnRead: a pool match's bout log
// gets its member ids stamped from the two teams' squads, WinnerMemberID
// follows the row's own resolved sides, and a name matching no squad
// member is left alone.
func TestLegacyPoolMatchSubBoutMemberIDUpgradeOnRead(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	teams := legacyUpgradeTeams(t, s, "RedTeam", "WhiteTeam")
	redID, whiteID := teams[0], teams[1]
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "D"},
		{ID: whiteID, Name: "WhiteTeam", Dojo: "D"},
	}))

	sato, err := s.AddTeamMember("c1", redID, "Sato")
	require.NoError(t, err)
	tanaka, err := s.AddTeamMember("c1", whiteID, "Tanaka")
	require.NoError(t, err)

	// Match-level ids already resolved (a fresh draw, or a bc-pnum-repaired
	// row); only the bout log is legacy-shaped.
	require.NoError(t, s.SavePoolMatches("c1", []state.MatchResult{
		{
			ID: "P1-0", SideA: "RedTeam", SideAID: redID, SideB: "WhiteTeam", SideBID: whiteID,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Sato", SideB: "Tanaka", Winner: "Sato"},
				{Position: 2, SideA: "Ghost", SideB: "Tanaka"}, // Ghost matches no squad member
			},
		},
	}))

	fresh := freshLegacyUpgradeStore(t, dir)
	matches, err := fresh.LoadPoolMatches("c1")
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 2)

	repaired := matches[0].SubResults[0]
	assert.Equal(t, sato.ID, repaired.SideAMemberID)
	assert.Equal(t, tanaka.ID, repaired.SideBMemberID)
	assert.Equal(t, sato.ID, repaired.WinnerMemberID, "WinnerMemberID follows the row's own resolved side (Sato)")

	residue := matches[0].SubResults[1]
	assert.Empty(t, residue.SideAMemberID, "Ghost matches no squad member and is left alone")
	assert.Equal(t, tanaka.ID, residue.SideBMemberID, "Tanaka's own slot still resolves independently")
}

// TestLegacyBracketSubBoutMemberIDUpgradeOnRead is the bracket.json twin:
// same resolution, same residue behaviour, for a BracketMatch's bout log.
func TestLegacyBracketSubBoutMemberIDUpgradeOnRead(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)

	teams := legacyUpgradeTeams(t, s, "RedTeam", "WhiteTeam")
	redID, whiteID := teams[0], teams[1]
	require.NoError(t, s.SaveParticipants("c1", []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "D"},
		{ID: whiteID, Name: "WhiteTeam", Dojo: "D"},
	}))

	sato, err := s.AddTeamMember("c1", redID, "Sato")
	require.NoError(t, err)
	tanaka, err := s.AddTeamMember("c1", whiteID, "Tanaka")
	require.NoError(t, err)

	require.NoError(t, s.SaveBracket("c1", &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID: "M1", SideA: "RedTeam", SideAID: redID, SideB: "WhiteTeam", SideBID: whiteID,
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "Sato", SideB: "Tanaka", Winner: "Tanaka"},
						{Position: 2, SideA: "Ghost", SideB: "Tanaka"},
					},
				},
			},
		},
	}))

	fresh := freshLegacyUpgradeStore(t, dir)
	bracket, err := fresh.LoadBracket("c1")
	require.NoError(t, err)
	require.Len(t, bracket.Rounds[0][0].SubResults, 2)

	repaired := bracket.Rounds[0][0].SubResults[0]
	assert.Equal(t, sato.ID, repaired.SideAMemberID)
	assert.Equal(t, tanaka.ID, repaired.SideBMemberID)
	assert.Equal(t, tanaka.ID, repaired.WinnerMemberID, "WinnerMemberID follows the row's own resolved side (Tanaka)")

	residue := bracket.Rounds[0][0].SubResults[1]
	assert.Empty(t, residue.SideAMemberID, "Ghost matches no squad member and is left alone")
	assert.Equal(t, tanaka.ID, residue.SideBMemberID)
}
