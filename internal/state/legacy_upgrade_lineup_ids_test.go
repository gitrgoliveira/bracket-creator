package state_test

// legacy_upgrade_lineup_ids_test.go pins the bc-tmid pass 2 load-time
// repair for lineups.yaml: an occupied position holding a NAME but no
// MemberIDs entry is filled from the team's OWN squad (team-members.yaml) when
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// TestLegacyLineupMemberIDUpgradeOnRead: a lineup position holding a name
// with no MemberIDs entry is repaired against the team's own squad, and a
// position whose name matches no squad member (or a team with no squad at
// all) is left alone rather than guessed at.
func TestLegacyLineupMemberIDUpgradeOnRead(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	teams := legacyUpgradeTeams(t, s, "Tora", "Kaze")
	teamID, noSquadTeamID := teams[0], teams[1]

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
		TeamID: noSquadTeamID, Round: 0,
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

	repaired, ok := roundZeroLineup(lineups, teamID)
	require.True(t, ok, "the repaired lineup must still be found")
	assert.Equal(t, sato.ID, repaired.MemberIDs[domain.PositionNumbered(1)],
		"Sato's slot resolves against the squad by an exact, unambiguous name match")
	assert.Empty(t, repaired.MemberIDs[domain.PositionNumbered(2)],
		"a name matching no squad member is left alone, not guessed at")
	assert.Equal(t, "Ghost", repaired.Positions[domain.PositionNumbered(2)],
		"the name itself is untouched; only the id half is ever filled")

	noSquad, ok := roundZeroLineup(lineups, noSquadTeamID)
	require.True(t, ok)
	assert.Empty(t, noSquad.MemberIDs, "a team with no squad recorded at all is left alone entirely")

	// Repairing lands on disk, not just in the returned copy.
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", "c1", "lineups.yaml"))
	require.NoError(t, err)
	assert.Contains(t, string(raw), sato.ID, "the repair lands on disk, not just in the returned copy")
}

// A lineup may not field one member at two positions (ValidatePositions
// refuses it), so this pass must neither CREATE such a row nor leave one it
// inherited.
//
// The creation half is the sharp one: the backfill resolves by NAME, so a
// legacy lineup naming one person at two positions used to stamp the SAME
// member id onto both. The server then rejected that lineup on the operator's
// next unrelated edit, for damage the operator never made.
func TestLegacyLineupUpgrade_NeverFieldsOneMemberAtTwoPositions(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	teamID := legacyUpgradeTeams(t, s, "Tora")[0]

	sato, err := s.AddTeamMember("c1", teamID, "Sato")
	require.NoError(t, err)

	// One person named at two positions, the pre-guard shape, with no ids.
	require.NoError(t, s.SetTeamLineup("c1", domain.TeamLineup{
		TeamID: teamID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "Sato",
			domain.PositionNumbered(2): "Sato",
		},
	}, 3))

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	lineups, err := fresh.LoadTeamLineups("c1")
	require.NoError(t, err)
	repaired, ok := roundZeroLineup(lineups, teamID)
	require.True(t, ok)

	ids := []string{
		repaired.MemberIDs[domain.PositionNumbered(1)],
		repaired.MemberIDs[domain.PositionNumbered(2)],
	}
	assert.Equal(t, sato.ID, ids[0], "the FIRST position fielded takes the id, deterministically")
	assert.Empty(t, ids[1], "the second must not receive the same id: that row is refused on every future write")

	// Both positions keep their NAME: the repair touches only the id half, so
	// nothing changes on the operator's screen.
	assert.Equal(t, "Sato", repaired.Positions[domain.PositionNumbered(1)])
	assert.Equal(t, "Sato", repaired.Positions[domain.PositionNumbered(2)])

	// And the repaired lineup is one the server will now accept.
	require.NoError(t, repaired.ValidatePositions(3),
		"a lineup this pass has repaired must pass the guard that refuses duplicates")
}

// The same rule for a duplicate this pass did not create: one written by an
// older release, already carrying the same id twice on disk. It is cleared on
// load so the write-time guard only ever answers for what a write introduced.
func TestLegacyLineupUpgrade_RepairsADuplicateAlreadyOnDisk(t *testing.T) {
	dir, s := newLegacyUpgradeFixture(t)
	teamID := legacyUpgradeTeams(t, s, "Tora")[0]

	sato, err := s.AddTeamMember("c1", teamID, "Sato")
	require.NoError(t, err)

	// Written straight to disk in the pre-guard shape: SetTeamLineup would
	// refuse this today, which is the whole point of repairing it on load.
	// Produced by marshalling the real types through the real file shape (a
	// LIST under `lineups:`), not hand-typed, so the fixture cannot drift from
	// what the store actually writes.
	type lineupFileShape struct {
		Lineups []domain.TeamLineup `yaml:"lineups"`
	}
	body, err := yaml.Marshal(&lineupFileShape{Lineups: []domain.TeamLineup{{
		TeamID: teamID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "Sato",
			domain.PositionNumbered(2): "Sato",
		},
		MemberIDs: map[domain.Position]string{
			domain.PositionNumbered(1): sato.ID,
			domain.PositionNumbered(2): sato.ID,
		},
	}}})
	require.NoError(t, err)
	path := filepath.Join(dir, "competitions", "c1", "lineups.yaml")
	require.NoError(t, os.WriteFile(path, body, 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	fresh.EnsureLegacyUpgraded("c1")

	lineups, err := fresh.LoadTeamLineups("c1")
	require.NoError(t, err)
	repaired, ok := roundZeroLineup(lineups, teamID)
	require.True(t, ok)

	assert.Equal(t, sato.ID, repaired.MemberIDs[domain.PositionNumbered(1)])
	assert.Empty(t, repaired.MemberIDs[domain.PositionNumbered(2)],
		"the inherited duplicate is cleared on load, not left to fail every future write")
	assert.Equal(t, "Sato", repaired.Positions[domain.PositionNumbered(2)], "its name is kept")
	require.NoError(t, repaired.ValidatePositions(3))
}

// The position that keeps the id when one member is at two is the first the team
// fields (domain.ComparePositions, the order the roster walks), not the first as
// text: for the five named positions that is senpo before chuken, where a text
// order read chuken first and kept the id at the position the roster fields second.
func TestLegacyLineupUpgrade_AMemberAtTwoNamedPositionsKeepsTheIdAtTheFirstFielded(t *testing.T) {
	writeLineup := func(t *testing.T, dir string, l domain.TeamLineup) {
		t.Helper()
		type lineupFileShape struct {
			Lineups []domain.TeamLineup `yaml:"lineups"`
		}
		body, err := yaml.Marshal(&lineupFileShape{Lineups: []domain.TeamLineup{l}})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", "c1", "lineups.yaml"), body, 0o600))
	}

	t.Run("an id already at both positions stays at senpo", func(t *testing.T) {
		dir, s := newLegacyUpgradeFixture(t)
		teamID := legacyUpgradeTeams(t, s, "Tora")[0]
		sato, err := s.AddTeamMember("c1", teamID, "Sato")
		require.NoError(t, err)
		writeLineup(t, dir, domain.TeamLineup{
			TeamID: teamID, Round: 0,
			Positions: map[domain.Position]string{domain.PosSenpo: "Sato", domain.PosChuken: "Sato"},
			MemberIDs: map[domain.Position]string{domain.PosSenpo: sato.ID, domain.PosChuken: sato.ID},
		})

		fresh := freshLegacyUpgradeStore(t, dir)
		fresh.EnsureLegacyUpgraded("c1")

		lineups, err := fresh.LoadTeamLineups("c1")
		require.NoError(t, err)
		repaired, ok := roundZeroLineup(lineups, teamID)
		require.True(t, ok)
		assert.Equal(t, sato.ID, repaired.MemberIDs[domain.PosSenpo])
		assert.Empty(t, repaired.MemberIDs[domain.PosChuken], "the second position fielded loses the id, and keeps its name")
		assert.Equal(t, "Sato", repaired.Positions[domain.PosChuken])
	})

	t.Run("a name at both positions is given its id at senpo", func(t *testing.T) {
		dir, s := newLegacyUpgradeFixture(t)
		teamID := legacyUpgradeTeams(t, s, "Tora")[0]
		sato, err := s.AddTeamMember("c1", teamID, "Sato")
		require.NoError(t, err)
		writeLineup(t, dir, domain.TeamLineup{
			TeamID: teamID, Round: 0,
			Positions: map[domain.Position]string{domain.PosSenpo: "Sato", domain.PosChuken: "Sato"},
		})

		fresh := freshLegacyUpgradeStore(t, dir)
		fresh.EnsureLegacyUpgraded("c1")

		lineups, err := fresh.LoadTeamLineups("c1")
		require.NoError(t, err)
		repaired, ok := roundZeroLineup(lineups, teamID)
		require.True(t, ok)
		assert.Equal(t, sato.ID, repaired.MemberIDs[domain.PosSenpo])
		assert.Empty(t, repaired.MemberIDs[domain.PosChuken])
	})
}

// roundZeroLineup reads a team's round-0 lineup (the team's starting lineup)
// out of a loaded lineups map. The map's keys are the store's own, so it scans
// for the entry instead.
func roundZeroLineup(lineups map[string]domain.TeamLineup, teamID string) (domain.TeamLineup, bool) {
	for _, l := range lineups {
		if l.TeamID == teamID && l.MatchID == "" && l.Round == 0 {
			return l, true
		}
	}
	return domain.TeamLineup{}, false
}
