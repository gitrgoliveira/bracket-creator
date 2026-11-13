package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSnapshot_SettlesCommittedWALBeforeReading(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 is not enforced on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory mode 0500")
	}

	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "wal-settle"
	const otherID = "wal-other"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "WAL Settle"}))
	require.NoError(t, store.SaveCompetition(&Competition{ID: otherID, Name: "Other Comp"}))

	compDir := store.compPath(compID)
	otherDir := store.compPath(otherID)
	walDir := filepath.Join(dir, ".wal")

	// Run a transaction whose Commit succeeds into .wal/ and whose Apply fails
	// because compDir is made read-only right before Commit/Apply.
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SaveBracket(compID, &Bracket{
			Rounds: [][]BracketMatch{{{ID: "m-settled", SideA: "Alice", SideB: "Bob", Status: MatchStatusCompleted, Winner: "Alice"}}},
		}); err != nil {
			return err
		}
		if err := tx.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID: "Bob",
			Eligible: false,
			Reason:   "kiken",
			MatchID:  "m-settled",
		}); err != nil {
			return err
		}
		return os.Chmod(compDir, 0o500)
	})
	require.NoError(t, os.Chmod(compDir, 0o700))
	require.ErrorIs(t, txErr, ErrTxCommitted)

	// Also plant a committed WAL for otherID whose directory is read-only so
	// that settling otherID would fail: Snapshot of compID must ignore it.
	otherWAL, err := wal.BeginTx(walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL)
	require.NoError(t, err)
	otherWAL.Append(wal.FileIntent{
		Path: store.compPath(otherID, "bracket.json"),
		Data: []byte(`{"rounds":[]}`),
		Mode: 0o600,
	})
	require.NoError(t, otherWAL.Commit())
	require.NoError(t, os.Chmod(otherDir, 0o500))
	defer func() { _ = os.Chmod(otherDir, 0o700) }()

	// Before Snapshot: target files for compID are not on disk yet.
	_, statErr := os.Stat(store.compPath(compID, "bracket.json"))
	require.True(t, os.IsNotExist(statErr), "bracket.json must not exist before WAL is settled")

	snap, err := store.Snapshot(ScopeCompetition, compID)
	require.NoError(t, err)

	bracketRaw, ok := snap.Files["competitions/"+compID+"/bracket.json"]
	require.True(t, ok, "snapshot must contain bracket.json after settling the WAL")
	parsedBracket, err := parseBracketBytes(bracketRaw)
	require.NoError(t, err)
	require.Len(t, parsedBracket.Rounds, 1)
	assert.Equal(t, "m-settled", parsedBracket.Rounds[0][0].ID)
	assert.Equal(t, "Alice", parsedBracket.Rounds[0][0].Winner)

	statusRaw, ok := snap.Files["competitions/"+compID+"/competitor-status.yaml"]
	require.True(t, ok, "snapshot must contain competitor-status.yaml after settling the WAL")
	statuses, err := parseCompetitorStatusBytes(statusRaw)
	require.NoError(t, err)
	assert.Contains(t, statuses, "Bob")

	// compID's WAL is gone; only otherID's WAL remains.
	remaining, err := wal.Scan(walDir, store.directWriteWAL, store.directRemoveWAL)
	require.NoError(t, err)
	require.Len(t, remaining, 1)
	assert.Equal(t, otherWAL.ID(), remaining[0].ID())
}

func TestSnapshot_FindsWALCommittedWhileWaitingForLock(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "wal-late-commit"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Late Commit"}))

	// Hold the competition write lock before Snapshot starts.
	compMu := store.getCompLock(compID)
	compMu.Lock()

	type snapResult struct {
		snap Snapshot
		err  error
	}
	done := make(chan snapResult, 1)
	go func() {
		s, serr := store.Snapshot(ScopeCompetition, compID)
		done <- snapResult{snap: s, err: serr}
	}()

	// Give the goroutine time to block on compMu.RLock() after any pre-lock code.
	time.Sleep(25 * time.Millisecond)

	// While Snapshot is waiting for the lock, commit a WAL for compID without applying it.
	w, err := wal.BeginTx(filepath.Join(dir, ".wal"), wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL)
	require.NoError(t, err)
	bracketBytes := []byte(`{"rounds":[[{"id":"m-under-lock","sideA":"Ken","sideB":"Ryu","winner":"Ken","status":"completed"}]]}`)
	w.Append(wal.FileIntent{
		Path: store.compPath(compID, "bracket.json"),
		Data: bracketBytes,
		Mode: 0o600,
	})
	require.NoError(t, w.Commit())

	// Release the competition lock so Snapshot proceeds.
	compMu.Unlock()

	res := <-done
	require.NoError(t, res.err)

	gotBytes, ok := res.snap.Files["competitions/"+compID+"/bracket.json"]
	require.True(t, ok, "WAL committed while Snapshot waited for the lock must be found and settled under the lock")
	b, err := parseBracketBytes(gotBytes)
	require.NoError(t, err)
	require.Len(t, b.Rounds, 1)
	assert.Equal(t, "m-under-lock", b.Rounds[0][0].ID)

	remaining, err := wal.Scan(filepath.Join(dir, ".wal"), store.directWriteWAL, store.directRemoveWAL)
	require.NoError(t, err)
	assert.Empty(t, remaining, "the settled WAL must be removed")
}

func TestSnapshot_SecondApplyFailureRefusesWithErrPendingWALUnsettled(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 is not enforced on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory mode 0500")
	}

	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "wal-unsettled"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Unsettled Comp"}))
	compDir := store.compPath(compID)

	// Commit a WAL whose Apply fails, and keep compDir at 0500 so the second
	// Apply inside Snapshot also fails.
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SaveBracket(compID, &Bracket{
			Rounds: [][]BracketMatch{{{ID: "m-fail", SideA: "A", SideB: "B", Status: MatchStatusScheduled}}},
		}); err != nil {
			return err
		}
		return os.Chmod(compDir, 0o500)
	})
	defer func() { _ = os.Chmod(compDir, 0o700) }()
	require.ErrorIs(t, txErr, ErrTxCommitted)

	for _, scope := range []SnapshotScope{ScopeCompetition, ScopeTournament} {
		snap, serr := store.Snapshot(scope, compID)
		require.Error(t, serr)
		assert.ErrorIs(t, serr, ErrPendingWALUnsettled)
		assert.Contains(t, serr.Error(), compID, "error must name the competition")
		assert.Empty(t, snap.Files, "failed WAL settlement must return no files")
	}
}
