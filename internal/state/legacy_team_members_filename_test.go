package state

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-dnst renamed the team-member store from squads.yaml to
// team-members.yaml, and its root key from `squads` to `members`. Operator
// rule: a storage change carries a migration path on load covering the last
// two releases. v2.0.0 wrote the shape below; v1.1.0 and earlier stored no
// team members at all, so this is the whole history to carry.
//
// The fixture bytes are written by hand rather than produced by the current
// code ON PURPOSE: the current code can no longer emit this shape, so a
// fixture derived from it would pin nothing. These are the bytes v2.0.0 left
// on disk, and they stay that way forever.
const v200SquadsYAML = `squads:
    c1-p1:
        - id: 11111111-1111-4111-8111-111111111111
          index: 1
          name: Haruki Tanaka
        - id: 22222222-2222-4222-8222-222222222222
          index: 2
          name: ""
`

func TestLegacyUpgrade_AdoptsV200SquadsFile(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte(v200SquadsYAML), 0o600))

	// Precondition: nothing is readable under the current name yet. Without
	// the migration this is what the whole tournament would see.
	_, statErr := os.Stat(filepath.Join(dir, teamMembersFilename))
	require.True(t, os.IsNotExist(statErr), "precondition: the new file must not exist yet")

	s.EnsureLegacyUpgraded(id)

	members, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, members["c1-p1"], 2, "both members recorded by v2.0.0 must survive the rename")
	assert.Equal(t, "Haruki Tanaka", members["c1-p1"][0].Name)
	assert.Equal(t, "11111111-1111-4111-8111-111111111111", members["c1-p1"][0].ID,
		"ids must carry over: a lineup position and a fought bout resolve by them")
	assert.Equal(t, 2, members["c1-p1"][1].Index, "a blank-named slot keeps its number")

	// Converged, not dual-read: exactly one shape is live afterwards.
	data, err := os.ReadFile(filepath.Join(dir, teamMembersFilename))
	require.NoError(t, err)
	assert.Contains(t, string(data), "members:", "the new file carries the new root key")
	_, statErr = os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.True(t, os.IsNotExist(statErr), "the old file is removed once the new one is safely written")
}

// TestLegacyUpgrade_AdoptsEvenWithNoParticipantsYet pins the FIRST of
// upgradeSquadsFromMetadataLocked's two adoption call sites (legacy_upgrade.go):
// its own explicit call to upgradeTeamMembersFilenameLocked, which runs before
// the roster is even loaded. That placement is otherwise unpinned, because
// upgradeSquadsFromMetadataLocked's own doc comment says a SECOND adopter sits
// a few lines below it: "upgradeSquadsFromMetadataLocked keeps its own
// explicit call even so. It returns before reaching this read [loadSquadsLocked]
// when the roster is empty, and a plain load must converge the file for a
// competition with no entrants yet." TestLegacyUpgrade_AdoptsV200SquadsFile
// above cannot tell the two apart: it goes on to call LoadSquads, which
// adopts via that very same loadSquadsLocked, so it would stay green even if
// the first call were deleted.
//
// This competition has NO participants saved at all (newTeamMemberTestStore
// only saves the competition, never a roster), so upgradeSquadsFromMetadataLocked
// takes its early "len(players) == 0" return and never reaches
// loadSquadsLocked; the other three EnsureLegacyUpgraded steps that also read
// squads (pool-match/bracket/lineup id repair) each no-op before touching
// squads too, since this fresh competition has none of those files either.
// The assertions read the FILESYSTEM directly rather than through
// LoadSquads/AddTeamMember/RenameTeamMember, because any of those would
// adopt the file themselves and mask the exact deletion this test exists to
// catch.
func TestLegacyUpgrade_AdoptsEvenWithNoParticipantsYet(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte(v200SquadsYAML), 0o600))

	s.EnsureLegacyUpgraded(id)

	// Read the filesystem directly -- see the doc comment above for why a
	// LoadSquads/mutator call here would not distinguish this call site from
	// its own masking second adopter.
	data, err := os.ReadFile(filepath.Join(dir, teamMembersFilename))
	require.NoError(t, err, "an empty-roster team competition must still have its legacy file adopted on load")
	assert.Contains(t, string(data), "members:", "the new file carries the new root key")
	assert.Contains(t, string(data), "Haruki Tanaka", "the v2.0.0 member data must survive the rename")

	_, statErr := os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.True(t, os.IsNotExist(statErr), "the old file is removed once the new one is safely written")
}

// A competition already on the current name must not be clobbered by a stale
// squads.yaml left beside it: the current file wins and the old one is left
// untouched rather than overwriting live data.
func TestLegacyUpgrade_CurrentFileWinsOverStaleLegacyFile(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)

	// Written directly in the CURRENT shape: this test is about which file
	// wins, so it needs a live team-members.yaml and nothing else.
	const current = "members:\n    c1-p1:\n        - id: 99999999-9999-4999-8999-999999999999\n          index: 1\n          name: Current Member\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, teamMembersFilename), []byte(current), 0o600))
	before, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, before["c1-p1"], 1)

	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte(v200SquadsYAML), 0o600))
	s.EnsureLegacyUpgraded(id)

	after, err := s.LoadSquads(id)
	require.NoError(t, err)
	assert.Equal(t, before["c1-p1"], after["c1-p1"], "the live file must be untouched")
	_, statErr := os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.False(t, os.IsNotExist(statErr), "the stale file is left alone, not consumed")
}

// A file at the legacy path that is not v2.0.0's shape (no `squads` key) must
// not be read as "this competition has no members" and written over the new
// name as an empty list.
func TestLegacyUpgrade_IgnoresAForeignFileAtTheLegacyPath(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte("something: else\n"), 0o600))

	s.EnsureLegacyUpgraded(id)

	_, statErr := os.Stat(filepath.Join(dir, teamMembersFilename))
	assert.True(t, os.IsNotExist(statErr), "no new file should have been minted from a foreign document")
}

// The old file must survive a migration that FAILS to write the new one,
// because the ordering is the whole crash-safety story: write first, remove
// only after. Without it a failed write loses the members outright.
//
// The write is failed through saveSquadsLocked's own writer seam, which is the
// only way to fail it while the REMOVE would still have succeeded. An earlier
// version of this test made the directory read-only, which failed both, so it
// passed with the ordering reversed and pinned nothing.
func TestLegacyUpgrade_FailedWriteLeavesTheLegacyFileIntact(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)
	legacy := filepath.Join(dir, legacySquadsFilename)
	require.NoError(t, os.WriteFile(legacy, []byte(v200SquadsYAML), 0o600))
	kept, err := os.ReadFile(legacy) // #nosec G304, test-owned temp path.
	require.NoError(t, err)

	failing := func(string, []byte, fs.FileMode) error { return errors.New("disk full") }
	err = s.upgradeTeamMembersFilenameLocked(id, failing)
	require.Error(t, err, "a migration that cannot write must report it, not swallow it")

	survived, readErr := os.ReadFile(legacy) // #nosec G304, test-owned temp path.
	require.NoError(t, readErr, "the legacy file must still be there for the next load to retry")
	assert.Equal(t, kept, survived, "and must be byte-identical: nothing was consumed")

	_, statErr := os.Stat(filepath.Join(dir, teamMembersFilename))
	assert.True(t, os.IsNotExist(statErr), "and no half-written new file is left behind")
}

// THE REPORTED DATA LOSS. A v2.0.0 squads.yaml the migration cannot read must
// never be followed by the seeding pass minting blank members over it: that
// write permanently arms the migration's refuse-to-overwrite guard, so the real
// names are stranded under a name nothing looks for, with fresh ids that orphan
// every lineup position and fought bout referencing the old ones.
//
// The competition IS still stamped, which is this package's documented failure
// policy (see the assertion below for why leaving it unstamped was reverted).
// What retries once the fault clears is the ADOPTION, through every path that
// reaches loadSquadsLocked plus LoadSquads' own probe, not this sweep.
func TestLegacyUpgrade_UnreadableLegacyFileNeverMintsBlanksOverIt(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte("squads: [this is not a map\n"), 0o600))
	// ID fixed at "c1-p1" (matching v200SquadsYAML's own key below) on BOTH
	// saves in this test, rather than left to auto-mint: this test's roster
	// save also runs saveParticipantsNoLock's own-roster orphan prune
	// (bc-tmfn), so a real caller's identity-preserving re-save -- the only
	// realistic way two saves of "the same Tora A" happen -- is exactly what
	// must be modeled here for the migration's recovered "c1-p1" entry to
	// still match the roster the second save actually persists.
	require.NoError(t, s.SaveParticipants(id, []domain.Player{{ID: "c1-p1", Name: "Tora A", Dojo: "Tora Dojo"}}))

	s.EnsureLegacyUpgraded(id)

	_, statErr := os.Stat(filepath.Join(dir, teamMembersFilename))
	assert.True(t, os.IsNotExist(statErr),
		"no blank-seeded team-members.yaml may be written while an unadopted legacy file is present")
	_, statErr = os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.False(t, os.IsNotExist(statErr), "the legacy file must still be there to retry")

	// STAMPED ANYWAY, which is this package's documented failure policy rather
	// than an oversight. Leaving it unstamped was tried and reverted: this
	// function is on the viewer's hot path, so a permanently unreadable file
	// made every poll re-take the exclusive lock and re-parse four files for
	// the life of the process. The stamp gates only that sweep; what protects
	// the members is the abort asserted above, which runs on every call.
	_, stamped := s.legacyUpgraded.Load(id)
	assert.True(t, stamped, "a failed squad step still stamps: this runs on the viewer's hot path")

	// And once the fault clears the adoption still happens, through a caller
	// the stamp does not gate. A roster write is one; so is any squad mutator.
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte(v200SquadsYAML), 0o600))
	// Same fixed ID as the first save above: the SAME Tora A, re-saved.
	require.NoError(t, s.SaveParticipants(id, []domain.Player{{ID: "c1-p1", Name: "Tora A", Dojo: "Tora Dojo"}}))
	members, err := s.LoadSquads(id)
	require.NoError(t, err)
	assert.Equal(t, "Haruki Tanaka", members["c1-p1"][0].Name, "the retry must recover the real names")
}

// The same protection must hold on the OTHER caller: a roster write that lands
// before anything ever reads the competition. That path calls the seeding pass
// directly, which is why the migration lives inside it rather than beside the
// load hook.
func TestLegacyUpgrade_RosterWriteAdoptsTheLegacyFileFirst(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte(v200SquadsYAML), 0o600))

	// A roster save, with no prior read of this competition. ID fixed at
	// "c1-p1" (matching v200SquadsYAML's own key), same reasoning as
	// TestLegacyUpgrade_UnreadableLegacyFileNeverMintsBlanksOverIt above:
	// this save also runs saveParticipantsNoLock's own-roster orphan prune
	// (bc-tmfn), so the roster it persists must carry the id the legacy
	// file's adopted entry is keyed on, or the prune correctly (and
	// unhelpfully, for what this test wants to observe) drops it right back
	// out in the same call.
	require.NoError(t, s.SaveParticipants(id, []domain.Player{{ID: "c1-p1", Name: "Tora A", Dojo: "Tora Dojo"}}))

	members, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.NotEmpty(t, members["c1-p1"], "the v2.0.0 members must have been adopted, not replaced by blanks")
	assert.Equal(t, "Haruki Tanaka", members["c1-p1"][0].Name)
}

// unadoptedLegacyCompetition returns a team competition holding ONE real team
// participant, an unadopted v2.0.0 squads.yaml keyed to that team, and nothing
// under the current name. That is the state a failed or never-run adoption
// leaves behind, and it is the state the three squad mutators can reach
// without EnsureLegacyUpgraded ever having run for this competition.
//
// The roster save is what mints the team's id, and it adopts as it goes, so
// the legacy file is (re)planted afterwards and the current file removed.
func unadoptedLegacyCompetition(t *testing.T, legacyBody string) (*Store, string, string, string) {
	t.Helper()
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)

	require.NoError(t, s.SaveParticipants(id, []domain.Player{{Name: "Tora A", Dojo: "Tora Dojo"}}))
	players, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	teamID := players[0].ID
	require.NotEmpty(t, teamID, "the team needs a real id: AddTeamMember validates it against the roster")

	require.NoError(t, os.Remove(filepath.Join(dir, teamMembersFilename)))
	body := strings.ReplaceAll(legacyBody, "c1-p1", teamID)
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte(body), 0o600))
	return s, id, teamID, dir
}

// A squad mutator reaches loadSquadsLocked without ever passing through
// EnsureLegacyUpgraded, so the adoption has to sit under that read. Without it
// AddTeamMember reads an unadopted competition as "no members", appends one,
// and saves the whole file, which strands the operator's real members under a
// name nothing looks for AND permanently arms the refuse-to-overwrite guard.
func TestSquadMutator_AdoptsTheLegacyFileBeforeWriting(t *testing.T) {
	s, id, teamID, dir := unadoptedLegacyCompetition(t, v200SquadsYAML)

	added, err := s.AddTeamMember(id, teamID, "Kenji Mori")
	require.NoError(t, err)

	members, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, members[teamID], 3, "the two v2.0.0 members must survive, plus the one added")
	names := make([]string, 0, 3)
	for _, m := range members[teamID] {
		names = append(names, m.Name)
	}
	assert.Contains(t, names, "Haruki Tanaka", "an Add must not mint a fresh file over the real members")
	assert.Contains(t, names, "Kenji Mori")
	assert.Equal(t, "11111111-1111-4111-8111-111111111111", members[teamID][0].ID,
		"and the adopted ids must carry over: lineups and fought bouts resolve by them")
	assert.Equal(t, 3, added.Index, "the new member takes the next index AFTER the adopted two")

	_, statErr := os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.True(t, os.IsNotExist(statErr), "the legacy file is consumed by the adoption, not left behind")
}

// The same floor on the rename path. Renaming a member recorded by v2.0.0 used
// to fail its lookup against an empty map and return ErrTeamMemberNotFound,
// which is a lesser bug than the Add's overwrite but the same missing step.
func TestSquadMutator_RenameAdoptsTheLegacyFileBeforeLookingUp(t *testing.T) {
	s, id, teamID, _ := unadoptedLegacyCompetition(t, v200SquadsYAML)

	err := s.RenameTeamMember(id, teamID, "11111111-1111-4111-8111-111111111111", "Haruki Sato")
	require.NoError(t, err, "a member recorded by v2.0.0 must be reachable by id after adoption")

	members, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, members[teamID], 2)
	assert.Equal(t, "Haruki Sato", members[teamID][0].Name)
}

// And a mutator that CANNOT adopt must fail rather than proceed: proceeding is
// what writes the blank file the migration then refuses to overwrite forever.
// Same protection as TestLegacyUpgrade_UnreadableLegacyFileNeverMintsBlanksOverIt,
// reached through the door that bypasses EnsureLegacyUpgraded.
func TestSquadMutator_RefusesToMintOverAnUnreadableLegacyFile(t *testing.T) {
	s, id, teamID, dir := unadoptedLegacyCompetition(t, "squads: [this is not a map\n")

	_, err := s.AddTeamMember(id, teamID, "Kenji Mori")
	require.Error(t, err, "an Add that cannot adopt must report it, not mint a file over the real members")

	_, statErr := os.Stat(filepath.Join(dir, teamMembersFilename))
	assert.True(t, os.IsNotExist(statErr),
		"no file may be minted under the current name while the legacy one is unadopted")
	_, statErr = os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.False(t, os.IsNotExist(statErr), "and the legacy file stays put for the next retry")
}

// The READ path adopts too, and it is the one that needed saying out loud.
// EnsureLegacyUpgraded stamps even when its squad step failed, so nothing on
// the read side retries afterwards; every squad MUTATOR retries through
// loadSquadsLocked, but LoadSquads is cache-aware and goes straight to the
// current filename. A file repaired on disk mid-run was therefore invisible to
// GET /team-members, the viewer payload and the export for the life of the
// process, and the operator's symptom was "this team has no members yet"
// printed under a full roster.
func TestLoadSquads_AdoptsALegacyFileTheStampedUpgradeNeverReached(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)

	// Stamp the competition with the squad step FAILING, exactly as a
	// transient fault at startup would leave it.
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte("squads: [not a map\n"), 0o600))
	s.EnsureLegacyUpgraded(id)
	_, stamped := s.legacyUpgraded.Load(id)
	require.True(t, stamped, "precondition: the failed step still stamps")

	// A read now sees nothing, which is the honest state.
	members, err := s.LoadSquads(id)
	require.NoError(t, err, "a corrupt legacy file must not take the viewer down")
	require.Empty(t, members)

	// The operator repairs the file. No further EnsureLegacyUpgraded call can
	// help: the competition is stamped for the life of this process.
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte(v200SquadsYAML), 0o600))
	s.EnsureLegacyUpgraded(id)

	members, err = s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, members["c1-p1"], 2, "the read itself must adopt, or nothing on the read side ever does")
	assert.Equal(t, "Haruki Tanaka", members["c1-p1"][0].Name)

	// Converged, so the next read is the ordinary cached one.
	_, statErr := os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.True(t, os.IsNotExist(statErr), "the legacy file is consumed by the adopting read")
}
