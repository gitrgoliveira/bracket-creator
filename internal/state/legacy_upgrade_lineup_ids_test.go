package state_test

// legacy_upgrade_lineup_ids_test.go pins the bc-tmid pass 2 load-time
// repair for lineups.yaml: an occupied position holding a NAME but no
// MemberIDs entry is filled from the team's OWN squad (squads.yaml) when
// the name resolves to exactly one member on that team, exactly like the
// header comment on legacy_upgrade.go documents. Two members of ONE team
// sharing a name is already impossible (bc-tmdup), so this repair needs
// none of the NameCount-gated uniqueness dance the participants.csv-facing
// upgrades in the sibling test files carry.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacyLineupMemberIDUpgradeOnRead: a lineup position holding a name
// with no MemberIDs entry is repaired against the team's own squad, and a
// position whose name matches no squad member (or a team with no squad at
// all) is left alone rather than guessed at.
func TestLegacyLineupMemberIDUpgradeOnRead(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	const teamID = "team-1"

	sato, err := s.AddTeamMember("c1", teamID, "Sato")
	require.NoError(t, err)

	// Legacy-shaped: Positions only, no MemberIDs at all -- exactly what a
	// lineup saved before this pass looks like on disk.
	require.NoError(t, s.SetTeamLineup("c1", domain.TeamLineup{
		TeamID: teamID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "Sato",
			domain.PositionNumbered(2): "Ghost", // no such squad member
		},
	}, 3))

	// A second team with a lineup but NO squad recorded at all: also left
	// alone, not just an unmatched name within a squad that exists.
	require.NoError(t, s.SetTeamLineup("c1", domain.TeamLineup{
		TeamID: "team-no-squad", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "Whoever",
		},
	}, 3))

	// A fresh Store instance: EnsureLegacyUpgraded's once-map is per-process
	// (here, per Store), so reading "c1" through a store that already ran
	// the repair would silently skip it.
	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	lineups, err := fresh.LoadTeamLineups("c1")
	require.NoError(t, err)

	repaired, ok := state.FindBestLineup(lineups, teamID, "", 0)
	require.True(t, ok, "the repaired lineup must still be found")
	assert.Equal(t, sato.ID, repaired.MemberIDs[domain.PositionNumbered(1)],
		"Sato's slot resolves against the squad by an exact, unambiguous name match")
	assert.Empty(t, repaired.MemberIDs[domain.PositionNumbered(2)],
		"a name matching no squad member is left alone, not guessed at")
	assert.Equal(t, "Ghost", repaired.Positions[domain.PositionNumbered(2)],
		"the name itself is untouched; only the id half is ever filled")

	noSquad, ok := state.FindBestLineup(lineups, "team-no-squad", "", 0)
	require.True(t, ok)
	assert.Empty(t, noSquad.MemberIDs, "a team with no squad recorded at all is left alone entirely")

	// Repairing lands on disk, not just in the returned copy.
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", "c1", "lineups.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), sato.ID, "the repair lands on disk, not just in the returned copy")
}
