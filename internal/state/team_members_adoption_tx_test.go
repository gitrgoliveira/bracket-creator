package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The v2.0.0 squads.yaml adoption is a move (write team-members.yaml, remove
// squads.yaml). Inside a transaction both halves are staged in the WAL
// (operator decision D6 (b), 2026-11-12), so they commit or abort together
// with the rest of the transaction.

var errAbortAdoption = errors.New("abort the transaction")

func renameParticipant(name string) func(*domain.Player) error {
	return func(p *domain.Player) error {
		p.Name = name
		return nil
	}
}

func TestStoreTx_AdoptionThenAbortLeavesTheLegacyFileAndTheCaches(t *testing.T) {
	s, id, teamID, dir := unadoptedLegacyCompetition(t, v200SquadsYAML)
	squadsPath := filepath.Join(dir, legacySquadsFilename)
	membersPath := filepath.Join(dir, teamMembersFilename)

	err := s.WithTransaction(id, func(tx StoreTx) error {
		if _, err := tx.UpdateParticipant(id, teamID, false, renameParticipant("Tora B")); err != nil {
			return err
		}
		// Inside the transaction the adoption is staged: the disk has not moved.
		_, statErr := os.Stat(squadsPath)
		require.NoError(t, statErr, "the staged delete must not touch the disk before commit")
		_, statErr = os.Stat(membersPath)
		require.True(t, os.IsNotExist(statErr), "the staged write must not land before commit")
		return errAbortAdoption
	})
	require.ErrorIs(t, err, errAbortAdoption)

	_, statErr := os.Stat(squadsPath)
	assert.NoError(t, statErr, "an aborted adoption keeps squads.yaml")
	_, statErr = os.Stat(membersPath)
	assert.True(t, os.IsNotExist(statErr), "an aborted adoption writes no team-members.yaml")

	// The saver published the staged members into the cache at stage time; the
	// abort must have dropped them, so nothing serves a file that never landed.
	cache := s.getFileCache(id, teamMembersFilename)
	cache.mu.Lock()
	cached := cache.data
	cache.mu.Unlock()
	assert.Nil(t, cached, "the abort must drop the team-members cache the staged adoption filled")

	players, err := s.LoadParticipants(id, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, "Tora A", players[0].Name, "the roster is the pre-transaction one")
}

func TestStoreTx_AdoptionCommitLandsTeamMembersAndRemovesTheLegacyFile(t *testing.T) {
	s, id, teamID, dir := unadoptedLegacyCompetition(t, v200SquadsYAML)

	require.NoError(t, s.WithTransaction(id, func(tx StoreTx) error {
		_, err := tx.UpdateParticipant(id, teamID, false, renameParticipant("Tora B"))
		return err
	}))

	_, statErr := os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.True(t, os.IsNotExist(statErr), "the commit removes squads.yaml")
	members, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.NotEmpty(t, members[teamID])
	assert.Equal(t, "Haruki Tanaka", members[teamID][0].Name, "the v2.0.0 members are on the current name")
	assert.Equal(t, "11111111-1111-4111-8111-111111111111", members[teamID][0].ID)
}

func TestStoreTx_ALoadInsideTheTransactionSeesTheAdoptedMembers(t *testing.T) {
	s, id, teamID, dir := unadoptedLegacyCompetition(t, v200SquadsYAML)

	require.NoError(t, s.WithTransaction(id, func(tx StoreTx) error {
		members, err := tx.LoadSquads(id)
		require.NoError(t, err)
		require.NotEmpty(t, members[teamID], "the adoption's staged members are read back inside the transaction")
		assert.Equal(t, "Haruki Tanaka", members[teamID][0].Name)

		// The move is staged, not made: squads.yaml is still on disk, and the
		// transaction reads it as gone (a second adoption finds nothing to do).
		_, statErr := os.Stat(filepath.Join(dir, legacySquadsFilename))
		require.NoError(t, statErr)
		st := tx.(*storeTx)
		assert.True(t, st.removedFor(legacySquadsFilename), "the transaction sees squads.yaml as absent")
		again, err := tx.LoadSquads(id)
		require.NoError(t, err)
		assert.Equal(t, members, again)
		return nil
	}))
	_, statErr := os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.True(t, os.IsNotExist(statErr))
}

// A transaction that committed its log and died before applying it: the next
// start replays the log, the delete step included.
func TestStoreInit_ReplaysACommittedLogWithADelete(t *testing.T) {
	s, id, teamID, dir := unadoptedLegacyCompetition(t, v200SquadsYAML)
	squadsPath := filepath.Join(dir, legacySquadsFilename)

	w, err := wal.BeginTx(s.walDir, wal.NewWALID(), s.directWriteWAL, s.directRemoveWAL)
	require.NoError(t, err)
	body := []byte("members:\n    " + teamID + ":\n        - id: 11111111-1111-4111-8111-111111111111\n          index: 1\n          name: Haruki Tanaka\n")
	require.NoError(t, w.WriteFn()(s.compPath(id, teamMembersFilename), body, 0o600))
	require.NoError(t, w.RemoveFn()(squadsPath))
	require.NoError(t, w.Commit())

	reopened, err := NewStore(s.GetFolder())
	require.NoError(t, err)
	_, statErr := os.Stat(squadsPath)
	assert.True(t, os.IsNotExist(statErr), "the replay removes the file the delete step names")
	members, err := reopened.LoadSquads(id)
	require.NoError(t, err)
	require.NotEmpty(t, members[teamID])
	assert.Equal(t, "Haruki Tanaka", members[teamID][0].Name)
	pending, err := wal.Scan(reopened.walDir, reopened.directWriteWAL, reopened.directRemoveWAL)
	require.NoError(t, err)
	assert.Empty(t, pending, "the replayed log is done")
}

// A log this build cannot read refuses startup rather than being replayed
// around.
func TestStoreInit_RefusesALogOfAnUnknownVersion(t *testing.T) {
	s, _, _, _ := unadoptedLegacyCompetition(t, v200SquadsYAML)
	require.NoError(t, os.WriteFile(filepath.Join(s.walDir, "future.json"),
		[]byte(`{"id":"future","version":99,"steps":[],"intents":[{"path":"","data":null,"mode":0}]}`), 0o600))
	_, err := NewStore(s.GetFolder())
	require.Error(t, err)
	assert.ErrorIs(t, err, wal.ErrUnknownLogVersion)
}

// The direct remove the WAL replays a delete with keeps to the data folder,
// as the direct write does, and reads an absent file as removed.
func TestDirectRemoveWAL_StaysInsideTheDataFolder(t *testing.T) {
	s, _, _, _ := unadoptedLegacyCompetition(t, v200SquadsYAML)
	outside := filepath.Join(t.TempDir(), "x")
	require.NoError(t, os.WriteFile(outside, []byte("x"), 0o600))
	require.Error(t, s.directRemoveWAL(outside))
	_, statErr := os.Stat(outside)
	assert.NoError(t, statErr, "a path outside the data folder is never removed")

	inside := filepath.Join(s.GetFolder(), "competitions", "gone.txt")
	require.NoError(t, os.WriteFile(inside, []byte("x"), 0o600))
	require.NoError(t, s.directRemoveWAL(inside))
	require.NoError(t, s.directRemoveWAL(inside), "removing a file already gone is success")
}
