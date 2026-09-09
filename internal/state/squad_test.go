package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- AddTeamMember / RenameTeamMember ---------------------------------------

// A first member gets index 1, a second gets index 2, and both keep their
// id and index across a rename.
func TestSquad_AddMintsIDAndIndex_RenameKeepsBoth(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)

	m1, err := s.AddTeamMember(id, "team-1", "Alice")
	require.NoError(t, err)
	assert.NotEmpty(t, m1.ID)
	assert.Equal(t, 1, m1.Index)
	assert.Equal(t, "Alice", m1.Name)

	m2, err := s.AddTeamMember(id, "team-1", "Bob")
	require.NoError(t, err)
	assert.NotEmpty(t, m2.ID)
	assert.NotEqual(t, m1.ID, m2.ID)
	assert.Equal(t, 2, m2.Index, "a second member must get the next index, not a fresh 1")

	require.NoError(t, s.RenameTeamMember(id, "team-1", m1.ID, "Alicia"))

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	members := squads["team-1"]
	require.Len(t, members, 2)
	byID := map[string]domain.TeamMember{}
	for _, m := range members {
		byID[m.ID] = m
	}
	renamed, ok := byID[m1.ID]
	require.True(t, ok, "the renamed member's id must survive")
	assert.Equal(t, "Alicia", renamed.Name)
	assert.Equal(t, 1, renamed.Index, "a rename must not touch the index")
	assert.Equal(t, "Bob", byID[m2.ID].Name)
	assert.Equal(t, 2, byID[m2.ID].Index)
}

// Duplicate names within ONE team are refused on add, including
// normalized forms (case, whitespace, Latin combining marks); the same
// name on a DIFFERENT team is allowed.
func TestSquad_AddRefusesDuplicateWithinOneTeam(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)

	_, err := s.AddTeamMember(id, "team-1", "Sato")
	require.NoError(t, err)

	for _, variant := range []string{"Sato", "sato", " Sato ", "Satō"} {
		t.Run(variant, func(t *testing.T) {
			_, err := s.AddTeamMember(id, "team-1", variant)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
		})
	}

	// The same name on a DIFFERENT team is fine.
	_, err = s.AddTeamMember(id, "team-2", "Sato")
	require.NoError(t, err, "members of different teams may share a name")
}

// Duplicate names within ONE team are refused on rename too, and renaming
// a member to their OWN current name is not a self-collision.
func TestSquad_RenameRefusesDuplicateWithinOneTeam_ButNotSelf(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)

	alice, err := s.AddTeamMember(id, "team-1", "Alice")
	require.NoError(t, err)
	_, err = s.AddTeamMember(id, "team-1", "Bob")
	require.NoError(t, err)

	t.Run("renaming to another member's name is refused", func(t *testing.T) {
		err := s.RenameTeamMember(id, "team-1", alice.ID, "Bob")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
	})

	t.Run("renaming to another member's name in a normalized form is refused", func(t *testing.T) {
		err := s.RenameTeamMember(id, "team-1", alice.ID, " bob ")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
	})

	t.Run("renaming a member to their OWN current name is not a self-collision", func(t *testing.T) {
		err := s.RenameTeamMember(id, "team-1", alice.ID, "Alice")
		require.NoError(t, err, "a rename to the member's own current name must not be refused as a collision with itself")
	})
}

// RenameTeamMember on an unknown (team, member) pair -- including a team
// with no squad at all -- returns ErrTeamMemberNotFound.
func TestSquad_RenameUnknownMemberOrTeam(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)

	t.Run("no squad for the team at all", func(t *testing.T) {
		err := s.RenameTeamMember(id, "no-such-team", "no-such-member", "X")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTeamMemberNotFound))
	})

	t.Run("team exists but member id does not", func(t *testing.T) {
		_, err := s.AddTeamMember(id, "team-1", "Alice")
		require.NoError(t, err)
		err = s.RenameTeamMember(id, "team-1", "no-such-member", "X")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTeamMemberNotFound))
	})
}

// A squad may exceed the competition's TeamSize: reserves and replacements
// are unconstrained (operator ruling 2026-09-09).
func TestSquad_SizeMayExceedCompetitionTeamSize(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false) // TeamSize: 3

	names := []string{"Alice", "Bob", "Carol", "Dan", "Eve"} // 5 > TeamSize 3
	for _, n := range names {
		_, err := s.AddTeamMember(id, "team-1", n)
		require.NoError(t, err, "squad size must not be capped by TeamSize")
	}

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	assert.Len(t, squads["team-1"], 5, "all 5 reserves/starters must be persisted despite a TeamSize of 3")
}

// --- saveSquadsLocked / directory creation ----------------------------------

// A squad write to a DELETED competition fails (ENOENT) rather than
// resurrecting the competition directory: only saveCompetitionChangedLocked
// may create it.
func TestSquad_WriteAfterDeleteDoesNotResurrectDirectory(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	require.NoError(t, s.DeleteCompetition(id))

	_, err := s.AddTeamMember(id, "team-1", "Alice")
	require.Error(t, err, "a squad write to a deleted competition must fail")

	_, statErr := os.Stat(filepath.Join(s.GetFolder(), "competitions", id))
	assert.True(t, os.IsNotExist(statErr), "the write must not resurrect the competition directory")
}

// --- Migration from Player.Metadata ------------------------------------------

// A team competition whose members live in Metadata gets a squad on load,
// with indices in array order and stable, non-empty ids.
func TestSquadMigration_TeamMetadataMigratesOnLoad(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob", "Carol"}},
	}))

	// Trigger EnsureLegacyUpgraded via a public load; the save above minted
	// the team's participant id, which the migration needs as its key.
	stored, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	teamID := stored[0].ID
	require.NotEmpty(t, teamID)

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	members := squads[teamID]
	require.Len(t, members, 3)
	assert.Equal(t, "Alice", members[0].Name)
	assert.Equal(t, 1, members[0].Index)
	assert.Equal(t, "Bob", members[1].Name)
	assert.Equal(t, 2, members[1].Index)
	assert.Equal(t, "Carol", members[2].Name)
	assert.Equal(t, 3, members[2].Index)
	for _, m := range members {
		assert.NotEmpty(t, m.ID)
	}
}

// An INDIVIDUAL competition's Metadata (dan grade) is never folded into
// members, even when it happens to carry more than one entry.
func TestSquadMigration_IndividualMetadataNeverFolded(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "individual", 0, false)
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Akira Tanaka", Dojo: "Gyokusen", Metadata: []string{"3", "extra"}},
	}))

	_, err := s.LoadParticipants(id, false)
	require.NoError(t, err)

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	assert.Empty(t, squads, "an individual competitor's Metadata must never be read as a member list")
}

// A team that already has a squad entry is left untouched by a later
// migration pass: re-running the fold must not duplicate members.
func TestSquadMigration_AlreadyMigratedTeamIsUntouched(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob"}},
	}))
	stored, err := s.LoadParticipants(id, false) // migration #1
	require.NoError(t, err)
	teamID := stored[0].ID

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	firstIDs := make([]string, len(squads[teamID]))
	for i, m := range squads[teamID] {
		firstIDs[i] = m.ID
	}
	require.Len(t, firstIDs, 2)

	// A second write over the SAME Metadata, and a second load: the fold
	// must not run again for this team.
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{ID: teamID, Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob"}, CheckedIn: true},
	}))
	_, err = s.LoadParticipants(id, false) // would-be migration #2
	require.NoError(t, err)

	squads2, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, squads2[teamID], 2, "an already-migrated team must not be re-folded")
	for i, m := range squads2[teamID] {
		assert.Equal(t, firstIDs[i], m.ID, "re-folding must not mint new ids for an already-migrated team")
	}
}

// Nothing is written when there is nothing to migrate: file bytes AND
// Store.FileVersion for squads.yaml stay unchanged.
func TestSquadMigration_WritesNothingWhenNothingToMigrate(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "individual", 0, false)
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Akira Tanaka", Dojo: "Gyokusen", Metadata: []string{"3"}},
	}))

	versionBefore := s.FileVersion(id, squadsFilename)
	squadsPath := filepath.Join(s.GetFolder(), "competitions", id, squadsFilename)
	_, statErrBefore := os.Stat(squadsPath)
	require.True(t, os.IsNotExist(statErrBefore), "squads.yaml must not exist before any migration-eligible load")

	_, err := s.LoadParticipants(id, false)
	require.NoError(t, err)

	assert.Equal(t, versionBefore, s.FileVersion(id, squadsFilename), "FileVersion must not bump when nothing changed")
	_, statErrAfter := os.Stat(squadsPath)
	assert.True(t, os.IsNotExist(statErrAfter), "squads.yaml must still not exist; nothing was written")
}

// A TEAM competition whose row carries no Metadata at all (nothing to
// migrate, as opposed to the individual-competition early exit the sibling
// test above pins) must also write nothing.
func TestSquadMigration_WritesNothingForTeamWithNoMetadataToMigrate(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo"}, // no Metadata at all
	}))

	versionBefore := s.FileVersion(id, squadsFilename)
	squadsPath := filepath.Join(s.GetFolder(), "competitions", id, squadsFilename)
	_, statErrBefore := os.Stat(squadsPath)
	require.True(t, os.IsNotExist(statErrBefore))

	_, err := s.LoadParticipants(id, false)
	require.NoError(t, err)

	assert.Equal(t, versionBefore, s.FileVersion(id, squadsFilename), "FileVersion must not bump when a team competition's row has nothing to migrate")
	_, statErrAfter := os.Stat(squadsPath)
	assert.True(t, os.IsNotExist(statErrAfter), "squads.yaml must still not exist; nothing was written")
}

// THE DATA-LOSS PIN (bc-tmid). A roster write that blanks a team's
// Metadata (the Apply flow's shape) must leave the squad intact, even when
// the team had NEVER been migrated before that write -- i.e. even when
// NO prior managed write or load has ever run EnsureLegacyUpgraded /
// saveParticipantsNoLock's migration hook against this competition.
//
// participants.csv is written directly (bypassing SaveParticipants
// entirely), simulating a roster that predates this process ever touching
// it -- an existing tournament folder from before this feature shipped, or
// a hand-authored/restored file. If the migration call in
// saveParticipantsNoLock ran AFTER the write instead of before, this is
// exactly the scenario that loses the squad forever: the ONLY write this
// process ever performs against this roster is the blanking one below, so
// there is no earlier managed write whose own migration could have saved
// it first.
func TestSquadMigration_SurvivesMetadataBlankingWriteEvenWhenNeverMigratedBefore(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	teamID := "11111111-1111-4111-8111-111111111111"

	participantsPath := filepath.Join(s.GetFolder(), "competitions", id, "participants.csv")
	raw := teamID + ",Tora A,Tora Dojo,Alice,Bob,Carol\n"
	require.NoError(t, os.WriteFile(participantsPath, []byte(raw), 0o600))

	// The Apply-flow's blanking write: same participant, Metadata collapsed
	// to a single empty dan-grade-shaped slot. This is the FIRST managed
	// write this process performs against this competition's roster.
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{ID: teamID, Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{""}},
	}))

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	members := squads[teamID]
	require.Len(t, members, 3, "the squad must survive a Metadata-blanking write even though it was never migrated before that write")
	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.Name
	}
	assert.Equal(t, []string{"Alice", "Bob", "Carol"}, names)

	// And the blanking write itself succeeded / participants.csv reflects it.
	stored, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, []string{""}, stored[0].Metadata)
}

// TestSquadMigration_InvalidatesTheSharedLazyCache pins the cache-coherence
// half of EnsureLegacyUpgraded's step ordering. The sub-bout and lineup
// member-id repairs resolve against a squads.yaml the squad migration may
// have just written, and they read it through legacyUpgradeRoster's shared
// lazy accessor. If that accessor was materialised before the migration ran,
// it holds the PRE-migration map, and every downstream repair sees "no
// squad" for the very team the migration just built one for -- skipped
// silently, for a whole extra load, with nothing logged.
//
// Materialising squads() first is what a squad-reading step inserted above
// the migration would do, which is why the ordering must not be the only
// thing standing between this code and a stale read.
func TestSquadMigration_InvalidatesTheSharedLazyCache(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	teamID := "22222222-2222-4222-8222-222222222222"

	participantsPath := filepath.Join(s.GetFolder(), "competitions", id, "participants.csv")
	raw := teamID + ",Tora A,Tora Dojo,Alice,Bob,Carol\n"
	require.NoError(t, os.WriteFile(participantsPath, []byte(raw), 0o600))

	roster := &legacyUpgradeRoster{store: s, compID: id}

	before, err := roster.squads()
	require.NoError(t, err)
	require.Empty(t, before[teamID], "precondition: the team has no squad before the migration")

	require.NoError(t, s.upgradeSquadsFromMetadataLocked(id, roster))

	after, err := roster.squads()
	require.NoError(t, err)
	names := make([]string, 0, len(after[teamID]))
	for _, m := range after[teamID] {
		names = append(names, m.Name)
	}
	assert.Equal(t, []string{"Alice", "Bob", "Carol"}, names,
		"a repair reading squads after the migration must see what the migration wrote")
}
