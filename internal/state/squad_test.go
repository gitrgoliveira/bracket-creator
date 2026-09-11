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

// newSquadTestStore returns a store, a competition id, and the participant
// ids of two REAL teams in it. AddTeamMember refuses a team id no
// participant carries, so a squad test needs genuine teams rather than a
// placeholder string.
//
// The competition's TeamSize is 3 and neither team carries any Metadata, so
// by the time this returns, BOTH teams already carry 3 SEEDED members
// (indices 1-3, blank names) -- upgradeSquadsFromMetadataLocked runs as
// part of the SaveParticipants/LoadParticipants calls below (bc-pnum). Any
// test that calls AddTeamMember on teamA/teamB starting from here must
// account for those 3 pre-existing slots: a first Add gets index 4, not 1.
func newSquadTestStore(t *testing.T) (store *Store, compID, teamA, teamB string) {
	t.Helper()
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Tora", Dojo: "Tora Dojo"},
		{Name: "Kaze", Dojo: "Kaze Dojo"},
	}))
	stored, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	require.Len(t, stored, 2)
	require.NotEmpty(t, stored[0].ID)
	require.NotEmpty(t, stored[1].ID)
	return s, id, stored[0].ID, stored[1].ID
}

// --- AddTeamMember / RenameTeamMember ---------------------------------------

// A reserve added beyond the 3 seeded slots (TeamSize 3) gets index 4, a
// second gets index 5, and both keep their id and index across a rename.
func TestSquad_AddMintsIDAndIndex_RenameKeepsBoth(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t) // teamA already carries 3 seeded blank slots (indices 1-3)

	m1, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)
	assert.NotEmpty(t, m1.ID)
	assert.Equal(t, 4, m1.Index, "a reserve added beyond the seeded TeamSize slots must continue the index sequence, not restart at 1")
	assert.Equal(t, "Alice", m1.Name)

	m2, err := s.AddTeamMember(id, teamA, "Bob")
	require.NoError(t, err)
	assert.NotEmpty(t, m2.ID)
	assert.NotEqual(t, m1.ID, m2.ID)
	assert.Equal(t, 5, m2.Index, "a second reserve must get the next index, not a fresh 1")

	require.NoError(t, s.RenameTeamMember(id, teamA, m1.ID, "Alicia"))

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	members := squads[teamA]
	require.Len(t, members, 5, "3 seeded slots plus the 2 reserves added above")
	byID := map[string]domain.TeamMember{}
	for _, m := range members {
		byID[m.ID] = m
	}
	renamed, ok := byID[m1.ID]
	require.True(t, ok, "the renamed member's id must survive")
	assert.Equal(t, "Alicia", renamed.Name)
	assert.Equal(t, 4, renamed.Index, "a rename must not touch the index")
	assert.Equal(t, "Bob", byID[m2.ID].Name)
	assert.Equal(t, 5, byID[m2.ID].Index)
}

// Duplicate names within ONE team are refused on add, including
// normalized forms (case, whitespace, Latin combining marks); the same
// name on a DIFFERENT team is allowed.
func TestSquad_AddRefusesDuplicateWithinOneTeam(t *testing.T) {
	s, id, teamA, teamB := newSquadTestStore(t)

	_, err := s.AddTeamMember(id, teamA, "Sato")
	require.NoError(t, err)

	for _, variant := range []string{"Sato", "sato", " Sato ", "Satō"} {
		t.Run(variant, func(t *testing.T) {
			_, err := s.AddTeamMember(id, teamA, variant)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
		})
	}

	// The same name on a DIFFERENT team is fine.
	_, err = s.AddTeamMember(id, teamB, "Sato")
	require.NoError(t, err, "members of different teams may share a name")
}

// Duplicate names within ONE team are refused on rename too, and renaming
// a member to their OWN current name is not a self-collision.
func TestSquad_RenameRefusesDuplicateWithinOneTeam_ButNotSelf(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)

	alice, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)
	_, err = s.AddTeamMember(id, teamA, "Bob")
	require.NoError(t, err)

	t.Run("renaming to another member's name is refused", func(t *testing.T) {
		err := s.RenameTeamMember(id, teamA, alice.ID, "Bob")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
	})

	t.Run("renaming to another member's name in a normalized form is refused", func(t *testing.T) {
		err := s.RenameTeamMember(id, teamA, alice.ID, " bob ")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
	})

	t.Run("renaming a member to their OWN current name is not a self-collision", func(t *testing.T) {
		err := s.RenameTeamMember(id, teamA, alice.ID, "Alice")
		require.NoError(t, err, "a rename to the member's own current name must not be refused as a collision with itself")
	})
}

// RenameTeamMember on an unknown (team, member) pair -- including a team
// with no squad at all -- returns ErrTeamMemberNotFound.
func TestSquad_RenameUnknownMemberOrTeam(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)

	t.Run("no squad for the team at all", func(t *testing.T) {
		err := s.RenameTeamMember(id, "no-such-team", "no-such-member", "X")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTeamMemberNotFound))
	})

	t.Run("team exists but member id does not", func(t *testing.T) {
		_, err := s.AddTeamMember(id, teamA, "Alice")
		require.NoError(t, err)
		err = s.RenameTeamMember(id, teamA, "no-such-member", "X")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTeamMemberNotFound))
	})
}

// A squad may exceed the competition's TeamSize: reserves and replacements
// are unconstrained (operator ruling 2026-09-09).
func TestSquad_SizeMayExceedCompetitionTeamSize(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t) // TeamSize 3: teamA already carries 3 seeded blank slots

	names := []string{"Alice", "Bob", "Carol", "Dan", "Eve"} // 5 more reserves beyond the 3 seeded slots
	for _, n := range names {
		_, err := s.AddTeamMember(id, teamA, n)
		require.NoError(t, err, "squad size must not be capped by TeamSize")
	}

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	assert.Len(t, squads[teamA], 8, "the 3 seeded slots plus all 5 reserves must be persisted despite a TeamSize of 3")
}

// --- saveSquadsLocked / directory creation ----------------------------------

// A squad write to a DELETED competition fails (ENOENT) rather than
// resurrecting the competition directory: only saveCompetitionChangedLocked
// may create it.
func TestSquad_WriteAfterDeleteDoesNotResurrectDirectory(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	require.NoError(t, s.DeleteCompetition(id))

	// Drives saveSquadsLocked DIRECTLY rather than going through
	// AddTeamMember. The public door now refuses a team id no participant
	// carries, and a deleted competition has no participants, so it would
	// fail before ever reaching the writer -- leaving the property this test
	// exists for (the saver never creates the competition directory)
	// unexercised while the test still passed.
	err := s.saveSquadsLocked(id, map[string][]domain.TeamMember{
		teamA: {{ID: "m1", Index: 1, Name: "Alice"}},
	}, s.directWrite)
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
// migration pass: re-running the fold must not duplicate members. The 2
// named members are padded to TeamSize 3 with 1 blank slot on the FIRST
// migration (bc-pnum seeding); the second migration pass must neither
// re-fold Metadata into new members nor pad again, since the squad is
// already at TeamSize.
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
	require.Len(t, firstIDs, 3, "2 named members plus 1 blank slot padded up to TeamSize 3")
	assert.Equal(t, "", squads[teamID][2].Name, "the padded slot must be blank")
	assert.Equal(t, 3, squads[teamID][2].Index)

	// A second write over the SAME Metadata, and a second load: the fold
	// must not run again for this team, and the squad -- already at
	// TeamSize -- must not be padded further.
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{ID: teamID, Name: "Tora A", Dojo: "Tora Dojo", Metadata: []string{"Alice", "Bob"}, CheckedIn: true},
	}))
	_, err = s.LoadParticipants(id, false) // would-be migration #2
	require.NoError(t, err)

	squads2, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, squads2[teamID], 3, "an already-migrated team must not be re-folded or padded again once at TeamSize")
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

// A TEAM competition whose row carries no Metadata at all is now SEEDED
// with TeamSize blank-named members on load (bc-pnum ruling: "by default
// teams have x team members, as defined in the competition config, and
// those positions have their numbers"), rather than left with no squad at
// all -- what this test (formerly
// TestSquadMigration_WritesNothingForTeamWithNoMetadataToMigrate) pinned
// before that ruling.
//
// SaveParticipants' own pre-write migration call (participants.go) sees
// nothing yet the FIRST time it runs against a brand-new roster (it reads
// whatever is CURRENTLY on disk, which is nothing before this very write
// lands), so squads.yaml still does not exist immediately after
// SaveParticipants returns; seeding happens on the LoadParticipants call
// below, once the roster (and the team's minted id) are actually on disk.
func TestSquadMigration_TeamWithNoMetadataIsSeededToTeamSize(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo"}, // no Metadata at all
	}))

	squadsPath := filepath.Join(s.GetFolder(), "competitions", id, squadsFilename)
	_, statErrBefore := os.Stat(squadsPath)
	require.True(t, os.IsNotExist(statErrBefore), "the very first roster write must not itself have seeded a squad")

	stored, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	teamID := stored[0].ID
	require.NotEmpty(t, teamID)

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	members := squads[teamID]
	require.Len(t, members, 3, "a team with no metadata at all must still be seeded to TeamSize")
	for i, m := range members {
		assert.NotEmpty(t, m.ID)
		assert.Equal(t, i+1, m.Index)
		assert.Equal(t, "", m.Name, "a seeded slot's name must be blank")
	}
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

// TestLegacyUpgradeRoster_CompetitionDoesNotLoadTheRoster pins the split
// between loadComp and get. The squad migration gates on Kind/TeamSize
// before it needs a participant, and saveParticipantsNoLock runs that
// migration on EVERY roster write, so an individual competition must not be
// charged a participants.csv parse and a RosterIndex build to answer a
// question config.md already answers.
//
// Asserting on the holder's own laziness rather than on timings: r.loaded
// stays false and r.players stays nil until something actually asks for the
// roster.
func TestLegacyUpgradeRoster_CompetitionDoesNotLoadTheRoster(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "", 0, false)
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Alice", Dojo: "D"}, {Name: "Bob", Dojo: "D"},
	}))

	roster := &legacyUpgradeRoster{store: s, compID: id}

	comp, err := roster.competition()
	require.NoError(t, err)
	require.NotNil(t, comp, "the competition record must still be returned")

	assert.False(t, roster.loaded, "asking for the competition must not trigger the roster load")
	assert.Nil(t, roster.players, "no participant should have been parsed yet")
	assert.Nil(t, roster.index, "no roster index should have been built yet")

	// And the roster still loads correctly when something does ask.
	players, err := roster.rosterPlayers()
	require.NoError(t, err)
	assert.Len(t, players, 2)
	assert.True(t, roster.loaded)
}

// TestSquadMigration_ZekkenRosterMigratesTheRightColumns covers the one
// layout variant every other squad test omits. With a zekken column the
// participant row gains DisplayName between Name and Dojo
// ([id, Name, DisplayName, Dojo, ...Metadata] rather than
// [id, Name, Dojo, ...Metadata]), so the members the migration folds are at a
// different offset. This package already carries a documented disaster from
// exactly that class: a row parsed one column out turned a member name into a
// dojo and destroyed the id every record pointed at. A migration that reads
// trailing columns must be pinned against both layouts, not just the one the
// fixtures happen to use.
//
// Written through the real writer so the bytes on disk are whatever
// marshalParticipantsCSV actually produces for a zekken competition, rather
// than a hand-built row asserting my own reading of the layout.
func TestSquadMigration_ZekkenRosterMigratesTheRightColumns(t *testing.T) {
	s, err := NewStore(t.TempDir())
	require.NoError(t, err)
	comp := &Competition{ID: "z1", Name: "Z1", Kind: "team", TeamSize: 3, WithZekkenName: true}
	require.NoError(t, s.SaveCompetition(comp))

	teamID := "33333333-3333-4333-8333-333333333333"
	require.NoError(t, s.SaveParticipants(comp.ID, []domain.Player{{
		ID:          teamID,
		Name:        "Tora",
		DisplayName: "TORA",
		Dojo:        "Tora Dojo",
		Metadata:    []string{"Sato", "Tanaka", "Yamada"},
	}}))

	// Trigger EnsureLegacyUpgraded through a public load, with the zekken
	// flag this competition actually carries, so the roster the migration
	// reads is parsed under the same layout it was written with.
	stored, err := s.LoadParticipants(comp.ID, true)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	require.Equal(t, "Tora", stored[0].Name, "precondition: the row parsed under the zekken layout")
	require.Equal(t, "Tora Dojo", stored[0].Dojo, "precondition: the dojo did not shift")

	squads, err := s.LoadSquads(comp.ID)
	require.NoError(t, err)

	members := squads[teamID]
	require.Len(t, members, 3, "the zekken layout must yield the same three members as the plain one")

	names := make([]string, len(members))
	for i, m := range members {
		names[i] = m.Name
	}
	assert.Equal(t, []string{"Sato", "Tanaka", "Yamada"}, names,
		"a column-shifted read would fold the dojo or the zekken in as a member")
	assert.NotContains(t, names, "TORA", "the zekken must never be read as a squad member")
	assert.NotContains(t, names, "Tora Dojo", "the dojo must never be read as a squad member")
}

// A team id no participant carries is refused, and nothing is written for
// it. Without an entry-removal operation a member minted under such an id
// could never be cleaned up through the app, so a typo would leave
// permanent, unreachable data behind.
func TestSquad_AddRefusesATeamIDNoParticipantCarries(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t) // teamA already carries 3 seeded blank slots (indices 1-3)

	_, err := s.AddTeamMember(id, "not-a-real-team", "Alice")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTeamNotFound), "the refusal must carry its own sentinel")
	assert.Contains(t, err.Error(), "not-a-real-team", "the message must name the offending id")

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	assert.Empty(t, squads["not-a-real-team"], "a refused add must not persist a squad for the bogus id")
	assert.Len(t, squads, 2, "only the two real teams' pre-seeded squads must exist; the bogus id must add nothing")

	// And a real team still works, so the guard refuses only what it should.
	m, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err, "a genuine team must still accept a member")
	assert.Equal(t, 4, m.Index, "teamA already carries 3 seeded slots (TeamSize 3), so a new reserve continues the sequence")
}

// --- squadDuplicateNameCheck: blanks must never collide -----------------

// The team's own default state -- several members sharing a blank name --
// must never be treated as a duplicate collision (bc-pnum), or seeding a
// team (or adding a reserve to one) would break for every team the moment
// it has two blank slots, which is every seeded team (TeamSize is always
// >= 2 for a team competition).
func TestSquadDuplicateNameCheck_BlanksNeverCollideWithEachOther(t *testing.T) {
	err := squadDuplicateNameCheck("team-1", "", []string{"", "", ""})
	assert.NoError(t, err, "a blank candidate against blank slots must never be a collision")

	err = squadDuplicateNameCheck("team-1", "Dan", []string{"", "", ""})
	assert.NoError(t, err, "a real candidate name must not collide with blank slots")

	err = squadDuplicateNameCheck("team-1", "Dan", []string{"", "Dan", ""})
	require.Error(t, err, "a real duplicate must still be refused even alongside blanks")
	assert.True(t, errors.Is(err, ErrDuplicateTeamMember))
}

// Exercised through the public door too: a real name added to a freshly
// seeded team (3 blank slots) must not be refused as a duplicate of them.
func TestSquad_BlankSeededMembersAreNeverDuplicatesOfEachOther(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t) // TeamSize 3: 3 blank seeded slots already exist for teamA

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, squads[teamA], 3, "precondition: the team's default state already carries 3 blank-named slots")

	m, err := s.AddTeamMember(id, teamA, "Dan")
	require.NoError(t, err, "a real name must never collide with the team's blank seeded slots")
	assert.Equal(t, "Dan", m.Name)
}

// --- Seeding to TeamSize: raise pads, lower never trims ------------------

// Raising TeamSize pads a squad already at its old size with new blank
// slots, keeping the original members' ids untouched. Lowering TeamSize
// back down must NEVER trim: a bout already fought may refer to a position
// by its index, and a smaller roster limit does not un-fight it.
//
// Calls upgradeSquadsFromMetadataLocked directly (the same pattern
// TestSquadMigration_InvalidatesTheSharedLazyCache uses above) rather than
// through EnsureLegacyUpgraded, which only runs the whole legacy-upgrade
// pass ONCE per competition per process and would not re-run this step
// just because TeamSize changed.
func TestSquadMigration_RaisingTeamSizePadsLoweringDoesNotTrim(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	require.NoError(t, s.SaveParticipants(id, []domain.Player{
		{Name: "Tora A", Dojo: "Tora Dojo"},
	}))
	stored, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	teamID := stored[0].ID

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, squads[teamID], 3, "seeded to the initial TeamSize")
	firstThreeIDs := []string{squads[teamID][0].ID, squads[teamID][1].ID, squads[teamID][2].ID}

	comp, err := s.LoadCompetition(id)
	require.NoError(t, err)
	comp.TeamSize = 5
	require.NoError(t, s.SaveCompetition(comp))

	roster := &legacyUpgradeRoster{store: s, compID: id}
	require.NoError(t, s.upgradeSquadsFromMetadataLocked(id, roster))

	squadsAfterRaise, err := s.LoadSquads(id)
	require.NoError(t, err)
	members := squadsAfterRaise[teamID]
	require.Len(t, members, 5, "raising TeamSize must pad up to the new size")
	for i, wantID := range firstThreeIDs {
		assert.Equal(t, wantID, members[i].ID, "the original slots must keep their ids")
	}
	assert.Equal(t, 4, members[3].Index)
	assert.Equal(t, 5, members[4].Index)
	assert.Equal(t, "", members[3].Name)
	assert.Equal(t, "", members[4].Name)

	// Lowering TeamSize back down must NOT trim the squad.
	comp.TeamSize = 2
	require.NoError(t, s.SaveCompetition(comp))

	roster2 := &legacyUpgradeRoster{store: s, compID: id}
	require.NoError(t, s.upgradeSquadsFromMetadataLocked(id, roster2))

	squadsAfterLower, err := s.LoadSquads(id)
	require.NoError(t, err)
	assert.Len(t, squadsAfterLower[teamID], 5, "lowering TeamSize must never trim an existing squad")
}

// --- ClearTeamMemberName ---------------------------------------------------

// Clearing blanks the name and keeps the id and index.
func TestSquad_ClearBlanksNameKeepsIDAndIndex(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	m, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)

	require.NoError(t, s.ClearTeamMemberName(id, teamA, m.ID))

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	var cleared domain.TeamMember
	found := false
	for _, mm := range squads[teamA] {
		if mm.ID == m.ID {
			cleared = mm
			found = true
		}
	}
	require.True(t, found, "the member must still be present after clearing")
	assert.Equal(t, "", cleared.Name)
	assert.Equal(t, m.Index, cleared.Index, "clearing must not touch the index")
	assert.Equal(t, m.ID, cleared.ID, "clearing must not touch the id")
}

// Clearing before the draw (competition still in setup) is allowed.
func TestSquad_ClearBeforeDrawIsAllowed(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	m, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)

	require.NoError(t, s.ClearTeamMemberName(id, teamA, m.ID), "a competition still in setup must allow clearing")
}

// Clearing is still allowed once a draw exists but the competition has not
// yet started (draw-ready): CanStart accepts this status, so the clear must
// too.
func TestSquad_ClearAtDrawReadyIsAllowed(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	m, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)

	comp, err := s.LoadCompetition(id)
	require.NoError(t, err)
	comp.Status = CompStatusDrawReady
	require.NoError(t, s.SaveCompetition(comp))

	require.NoError(t, s.ClearTeamMemberName(id, teamA, m.ID), "draw-ready is still before start; clearing must be allowed")
}

// Clearing is refused once the competition has started, with its own
// sentinel, and nothing is written.
func TestSquad_ClearRefusedOnceStarted(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	m, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)

	comp, err := s.LoadCompetition(id)
	require.NoError(t, err)
	comp.Status = CompStatusPools
	require.NoError(t, s.SaveCompetition(comp))

	versionBefore := s.FileVersion(id, squadsFilename)
	err = s.ClearTeamMemberName(id, teamA, m.ID)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrTeamMemberClearAfterStart))

	squads, loadErr := s.LoadSquads(id)
	require.NoError(t, loadErr)
	stillNamed := false
	for _, mm := range squads[teamA] {
		if mm.ID == m.ID && mm.Name == "Alice" {
			stillNamed = true
		}
	}
	assert.True(t, stillNamed, "a refused clear must not have changed the stored name")
	assert.Equal(t, versionBefore, s.FileVersion(id, squadsFilename), "a refused clear must write nothing")
}

// Renaming a member and adding one stay available once the competition has
// started. The operator ruling is that a member can never be removed but its
// name can be corrected, and that a team may field a replacement mid
// tournament, so only CLEARING a name is gated on the start
// (TestSquad_ClearRefusedOnceStarted above). The three operations therefore do
// NOT share one rule, and the regression this pins is the symmetry argument
// that would give them one: every other squad test passes with a start gate
// added to Rename and Add.
func TestSquad_RenameAndAddRemainAvailableOnceStarted(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)
	m, err := s.AddTeamMember(id, teamA, "Alice")
	require.NoError(t, err)

	comp, err := s.LoadCompetition(id)
	require.NoError(t, err)
	comp.Status = CompStatusPools
	require.NoError(t, s.SaveCompetition(comp))

	require.NoError(t, s.RenameTeamMember(id, teamA, m.ID, "Alicia"),
		"a started competition must still allow a member's name to be corrected")

	reserve, err := s.AddTeamMember(id, teamA, "Bob")
	require.NoError(t, err, "a started competition must still allow a replacement to be added")
	assert.Equal(t, m.Index+1, reserve.Index, "a replacement added after the start continues the index sequence")

	squads, err := s.LoadSquads(id)
	require.NoError(t, err)
	byID := make(map[string]domain.TeamMember, len(squads[teamA]))
	for _, mm := range squads[teamA] {
		byID[mm.ID] = mm
	}
	assert.Equal(t, "Alicia", byID[m.ID].Name, "the corrected name must be the stored one")
	assert.Equal(t, m.Index, byID[m.ID].Index, "a rename must keep the index whatever the status")
	assert.Equal(t, "Bob", byID[reserve.ID].Name)
}

// Clearing an unknown (team, member) pair -- including a team with no squad
// at all -- returns ErrTeamMemberNotFound, matching RenameTeamMember's
// contract exactly.
func TestSquad_ClearUnknownMemberOrTeam(t *testing.T) {
	s, id, teamA, _ := newSquadTestStore(t)

	t.Run("no squad for the team at all", func(t *testing.T) {
		err := s.ClearTeamMemberName(id, "no-such-team", "no-such-member")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTeamMemberNotFound))
	})

	t.Run("team exists but member id does not", func(t *testing.T) {
		err := s.ClearTeamMemberName(id, teamA, "no-such-member")
		require.Error(t, err)
		assert.True(t, errors.Is(err, ErrTeamMemberNotFound))
	})
}
