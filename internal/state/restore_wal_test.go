package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// plantCommittedWAL leaves a committed, not applied, WAL whose one intent
// writes bracket.json in compID, as a transaction whose Apply failed does.
func plantCommittedWAL(t *testing.T, s *Store, dir, compID, bracketJSON string) {
	t.Helper()
	w, err := wal.BeginTx(filepath.Join(dir, ".wal"), wal.NewWALID(), s.directWriteWAL, s.directRemoveWAL)
	require.NoError(t, err)
	w.Append(wal.FileIntent{Path: s.compPath(compID, "bracket.json"), Data: []byte(bracketJSON), Mode: 0o600})
	require.NoError(t, w.Commit())
}

func TestRestore_AStaleWALIsNotReplayedOverTheRestore(t *testing.T) {
	s, dir := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeCompetition, "men")
	plantCommittedWAL(t, s, dir, "men", `{"rounds":[[{"id":"stale","sideA":"Alice","sideB":"Bob","winner":"","status":"scheduled","court":"A","scheduledAt":""}]]}`)

	_, err := s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}}})
	require.NoError(t, err)

	reopened, err := NewStore(dir)
	require.NoError(t, err)
	b, err := reopened.LoadBracket("men")
	require.NoError(t, err)
	if b != nil {
		for _, round := range b.Rounds {
			for _, m := range round {
				assert.NotEqual(t, "stale", m.ID, "the old intent must not land over the restored competition")
			}
		}
	}
	pending, err := wal.Scan(filepath.Join(dir, ".wal"), reopened.directWriteWAL, reopened.directRemoveWAL)
	require.NoError(t, err)
	assert.Empty(t, pending, "the restore settled the WAL")
}

func TestRestore_AWALThatCannotBeSettledRefuses(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 is not enforced on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory mode 0500")
	}
	s, dir := newRestoreTestStore(t)
	addRestoreComp(t, s, "men", "Men", "M", MatchStatusScheduled, "A")
	a := archiveOf(t, s, ScopeCompetition, "men")
	plantCommittedWAL(t, s, dir, "men", `{"rounds":[]}`)
	compDir := s.compPath("men")
	require.NoError(t, os.Chmod(compDir, 0o500))
	defer func() { require.NoError(t, os.Chmod(compDir, 0o700)) }()

	_, err := s.Restore(RestoreRequest{Archive: a, Decisions: map[string]RestoreDecision{"men": {Action: RestoreReplace}}})
	assert.Equal(t, RestoreCodePendingWriteUnsettled, restoreCode(t, err))
	assert.ErrorIs(t, err, ErrPendingWALUnsettled)
	assertNoStaging(t, dir)
}
