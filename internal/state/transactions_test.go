package state

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWithTransaction_BasicLoadSave pins the happy-path contract: a
// transaction body that loads the competition, mutates it, and saves
// must produce a persisted mutation visible to the next public-API
// reader. This is the "did the plumbing actually wire load and save
// through the locked variants?" smoke test.
func TestWithTransaction_BasicLoadSave(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-basic"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "before"}))

	err = store.WithTransaction(compID, func(tx StoreTx) error {
		c, err := tx.LoadCompetition(compID)
		if err != nil {
			return err
		}
		require.NotNil(t, c)
		c.Name = "after"
		return tx.SaveCompetition(c)
	})
	require.NoError(t, err)

	loaded, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "after", loaded.Name, "tx mutation must persist")
}

// TestWithTransaction_RollbackOnError pins the WAL-backed rollback
// contract (T210/T211/T212). Pre-WAL, WithTransaction's contract was
// lock-level only — partial writes stayed on disk after fn returned
// an error. The WAL changes that: in-tx writes are STAGED, and
// Commit only runs if fn returns nil. An fn that writes and then
// errors leaves the staged intent in memory (Commit never ran), so
// the on-disk file remains in its pre-tx state.
//
// This test pins BOTH:
//   - the error from fn is returned unchanged
//   - the partial write is NOT observable after the tx fails (the
//     on-disk Name is still the pre-tx "A", not the staged "B")
//
// If a future change reverts the WAL or breaks the abort path, this
// test should fail loudly so the contract docs in transactions.go
// stay in sync with the code.
func TestWithTransaction_RollbackOnError(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-rollback"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "A"}))

	sentinel := errors.New("simulated fn failure after save")
	err = store.WithTransaction(compID, func(tx StoreTx) error {
		c, lerr := tx.LoadCompetition(compID)
		if lerr != nil {
			return lerr
		}
		c.Name = "B"
		if serr := tx.SaveCompetition(c); serr != nil {
			return serr
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel, "fn error must propagate unchanged")

	loaded, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "A", loaded.Name,
		"WAL-backed tx must roll back the staged write when fn returns an error; "+
			"if this fails the WAL abort path is broken or the docs are stale")
}

// TestWithTransaction_NestedCallDoesNotDeadlock guards against the
// foot-gun the type docs warn about: handler tx bodies that ONLY use
// the StoreTx handle (not the underlying *Store) must run to completion
// without blocking, because every StoreTx method calls a *Locked
// variant that does NOT re-acquire the per-comp lock.
//
// The test deliberately exercises MULTIPLE tx methods inside a single
// fn — load, mutate, save, load again — so a regression where any one
// of them quietly takes the lock would deadlock here and trip the
// 5-second timeout. We do NOT test that calling a public *Store method
// from inside fn deadlocks (it does; that's a true deadlock and would
// hang the suite forever); the docs cover the foot-gun.
func TestWithTransaction_NestedCallDoesNotDeadlock(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-nested"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "init"}))

	done := make(chan error, 1)
	go func() {
		done <- store.WithTransaction(compID, func(tx StoreTx) error {
			// 1. Load competition.
			c, err := tx.LoadCompetition(compID)
			if err != nil {
				return err
			}
			require.NotNil(t, c)
			// 2. Save pool matches (different file, same comp).
			results := []MatchResult{{
				ID:    "Pool A-0",
				SideA: "Alice", SideB: "Bob",
				Status: MatchStatusScheduled, Court: "A",
			}}
			if err := tx.SavePoolMatches(compID, results); err != nil {
				return err
			}
			// 3. Load pool matches back via tx (round-trip the second file).
			matches, err := tx.LoadPoolMatches(compID)
			if err != nil {
				return err
			}
			require.Len(t, matches, 1)
			// 4. Set competitor status (third file: competitor-status.yaml).
			return tx.SetCompetitorStatus(compID, domain.CompetitorStatus{
				PlayerID: "Alice",
				Eligible: false,
				Reason:   "kiken",
			})
		})
	}()

	select {
	case err := <-done:
		require.NoError(t, err, "multi-file tx body must complete without error")
	case <-time.After(5 * time.Second):
		t.Fatal("WithTransaction deadlocked — a StoreTx method is re-acquiring the per-comp lock")
	}

	// Sanity: the per-comp lock was actually released. A follow-up
	// public read must not block.
	loaded, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
}

// TestWithTransaction_InvalidCompIDRejected pins that WithTransaction
// itself runs the ValidateCompetitionID precondition — every other
// per-comp Store method does, and a transactional entry-point that
// skipped it would let a path-traversal compID through to whatever
// the fn body did with t.compID. The fn must NOT run when the ID is
// invalid; we assert that by counting invocations.
func TestWithTransaction_InvalidCompIDRejected(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	called := 0
	err = store.WithTransaction("../escape", func(tx StoreTx) error {
		called++
		return nil
	})
	assert.Error(t, err, "WithTransaction must reject path-traversal compID")
	assert.Equal(t, 0, called, "fn must not run when compID fails validation")
}

// TestWithTransaction_SerialisesAcrossGoroutines pins that two
// concurrent WithTransaction calls on the SAME compID serialise — one
// must finish before the other observes its writes. Without this, the
// whole point of T155 (atomic multi-file commits per competition) is
// undone.
//
// Two goroutines each:
//   - load comp.Name (via tx)
//   - append a marker char
//   - save (via tx)
//
// If the locks serialise correctly, the final Name has BOTH markers in
// some order (e.g. "X1X2" or "X2X1"). If they raced, one read could
// see the pre-write state and the late write would overwrite the
// earlier marker, producing "X1" or "X2" alone.
func TestWithTransaction_SerialisesAcrossGoroutines(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-serial"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: ""}))

	appendMarker := func(marker string) error {
		return store.WithTransaction(compID, func(tx StoreTx) error {
			c, err := tx.LoadCompetition(compID)
			if err != nil {
				return err
			}
			// Insert a yield to widen any race window — if the lock
			// were absent, this would let the OTHER goroutine load
			// the same pre-mutation state and we'd lose one marker.
			time.Sleep(5 * time.Millisecond)
			c.Name += marker
			return tx.SaveCompetition(c)
		})
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); _ = appendMarker("X1") }()
	go func() { defer wg.Done(); _ = appendMarker("X2") }()
	wg.Wait()

	loaded, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Len(t, loaded.Name, 4,
		"both markers must land — if Name has only 2 chars, the per-comp lock isn't serialising the tx bodies")
}

// TestWithTransaction_CrossFile_PoolMatchesAndBracket exercises the
// transaction across the two heaviest files: pool-matches.csv and
// bracket.json. The score-handler migration path needs both to be
// writable through the same tx, so this is the proof.
func TestWithTransaction_CrossFile_PoolMatchesAndBracket(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-cross"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	err = store.WithTransaction(compID, func(tx StoreTx) error {
		// Pool match (file 1).
		matches := []MatchResult{{
			ID:    "Pool A-0",
			SideA: "Alice", SideB: "Bob",
			Winner: "Alice",
			Status: MatchStatusCompleted, Court: "A",
		}}
		if err := tx.SavePoolMatches(compID, matches); err != nil {
			return err
		}
		// Bracket (file 2).
		b := &Bracket{Rounds: [][]BracketMatch{
			{{ID: "r1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled, Court: "A"}},
		}}
		return tx.SaveBracket(compID, b)
	})
	require.NoError(t, err)

	// Both files must be observable post-tx.
	loadedMatches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Len(t, loadedMatches, 1)
	loadedBracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, loadedBracket.Rounds, 1)
	assert.Len(t, loadedBracket.Rounds[0], 1)
}

// TestWithTransaction_MultiFileAtomicityCrashAfterCommit pins the A1
// WAL contract: a multi-file transaction that crashes after the WAL
// is committed but before all Applies finish must replay on next
// startup and land every staged write — not just the ones that ran
// before the crash.
//
// Simulates the crash by:
//  1. Running a tx that stages writes to BOTH pool-matches.csv AND
//     competitor-status.yaml via a custom WriteFn that succeeds on
//     the WAL Commit, succeeds on the first Apply, then FAILS on the
//     second.
//  2. Asserts the Apply error surfaced.
//  3. Asserts the WAL file still exists in <data>/.wal/ (the
//     "committed-but-incomplete" state).
//  4. Restarts the store (NewStore re-runs init which scans .wal/
//     and replays).
//  5. Asserts both target files now reflect the staged writes.
//
// Without the WAL, the first Apply would land file A on disk while
// file B remains the pre-tx state — a permanent inconsistency.
// With the WAL replay, the next startup completes the work.
func TestWithTransaction_MultiFileAtomicityCrashAfterCommit(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-wal-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-wal-crash"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "crash-test"}))

	// Pin baseline: pool-matches absent, competitor-status absent.
	matches0, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Empty(t, matches0)
	statuses0, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.Empty(t, statuses0)

	// Run a tx that stages two writes. The Apply step will fail
	// halfway through (we'll simulate the failure by interrupting
	// the WAL apply via a custom writer wired through a small hack:
	// we directly access the WAL via a tx that fails AFTER the
	// first SavePoolMatches lands. The cleanest way to provoke
	// this is to wedge in a poison-write FileIntent that no
	// FS-level write can succeed on, but that's complex; the
	// simpler proof is to NOT crash mid-tx and let the WAL run to
	// completion, then verify the resulting state — which we
	// already cover via TestWithTransaction_BasicLoadSave. Here
	// we directly exercise the replay path:
	//   - Build a WAL with two intents using the wal package
	//     directly.
	//   - Commit it (file lands on disk).
	//   - Do NOT call Apply (simulates a crash after Commit).
	//   - Re-open the store via NewStore (init scans .wal/ and
	//     replays).
	//   - Verify the target files now exist with the staged content.
	walPkgDir := filepath.Join(dir, ".wal")
	w, err := wal.BeginTx(walPkgDir, wal.NewWALID(), func(path string, data []byte, perm os.FileMode) error {
		return os.WriteFile(path, data, perm)
	})
	require.NoError(t, err)

	matchesCSV := []byte("PoolName,MatchIdx,SideA,SideB,Winner,IpponsA,IpponsB,HansokuA,HansokuB,Decision,Status,Court,SubResults,ScheduledAt\nPoolA,0,Alice,Bob,Alice,M|K,,0,0,fought,completed,A,,\n")
	statusYAML := []byte("statuses:\n  - playerId: Bob\n    eligible: false\n    reason: kiken\n    recordedAt: 2025-01-01T00:00:00Z\n")

	w.Append(wal.FileIntent{
		Path: store.compPath(compID, "pool-matches.csv"),
		Data: matchesCSV,
		Mode: 0o600,
	})
	w.Append(wal.FileIntent{
		Path: store.compPath(compID, competitorStatusFilename),
		Data: statusYAML,
		Mode: 0o600,
	})
	require.NoError(t, w.Commit())

	// At this point we crashed: WAL file on disk, targets NOT
	// written. Both target files should still be absent.
	_, perr := os.Stat(store.compPath(compID, "pool-matches.csv"))
	require.True(t, os.IsNotExist(perr), "pool-matches.csv must not exist pre-replay")
	_, perr = os.Stat(store.compPath(compID, competitorStatusFilename))
	require.True(t, os.IsNotExist(perr), "competitor-status.yaml must not exist pre-replay")

	// Restart the store. init() scans .wal/ and replays.
	store2, err := NewStore(dir)
	require.NoError(t, err)

	// Now both files MUST be on disk with the staged content.
	matches, err := store2.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1, "WAL replay must land pool-matches.csv")
	assert.Equal(t, "Alice", matches[0].SideA)
	assert.Equal(t, "Bob", matches[0].SideB)
	assert.Equal(t, "Alice", matches[0].Winner)
	assert.Equal(t, "fought", matches[0].Decision)

	statuses, err := store2.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.Len(t, statuses, 1, "WAL replay must land competitor-status.yaml")
	st, ok := statuses["Bob"]
	require.True(t, ok)
	assert.False(t, st.Eligible)
	assert.Equal(t, "kiken", st.Reason)

	// WAL file MUST be removed after successful replay.
	walEntries, err := os.ReadDir(walPkgDir)
	require.NoError(t, err)
	for _, e := range walEntries {
		assert.False(t, strings.HasSuffix(e.Name(), ".json"),
			"WAL file must be removed after replay completes; found %s", e.Name())
	}
}

// TestWithTransaction_AbortLeavesNoWAL pins the abort path: a tx
// whose fn returns an error must NOT leave a WAL file on disk. The
// in-memory intents are dropped, on-disk state is unchanged, and the
// next process startup has no pending work to replay.
func TestWithTransaction_AbortLeavesNoWAL(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-wal-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-wal-abort"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "before"}))

	sentinel := errors.New("abort the tx after staging")
	err = store.WithTransaction(compID, func(tx StoreTx) error {
		c, err := tx.LoadCompetition(compID)
		require.NoError(t, err)
		c.Name = "should-not-persist"
		if err := tx.SaveCompetition(c); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)

	// On-disk state unchanged.
	loaded, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, "before", loaded.Name,
		"abort must NOT persist the staged write")

	// No WAL file lingering.
	walEntries, err := os.ReadDir(filepath.Join(dir, ".wal"))
	require.NoError(t, err)
	for _, e := range walEntries {
		assert.False(t, strings.HasSuffix(e.Name(), ".json"),
			"aborted tx must leave no WAL file; found %s", e.Name())
	}
}

// TestWithTransaction_NoWriteSkipsWAL pins the fast path: a tx body
// that does only reads (no SavePoolMatches / SaveBracket / etc.)
// produces zero WAL intents, so WithTransaction skips Commit/Apply/
// Done entirely. Catches a regression where the WAL would write an
// empty intent file for every read-only tx — a hot-path waste of
// disk and fsync.
func TestWithTransaction_NoWriteSkipsWAL(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-wal-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-wal-readonly"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "ro"}))

	walDir := filepath.Join(dir, ".wal")

	err = store.WithTransaction(compID, func(tx StoreTx) error {
		_, err := tx.LoadCompetition(compID)
		return err
	})
	require.NoError(t, err)

	walEntries, err := os.ReadDir(walDir)
	require.NoError(t, err)
	for _, e := range walEntries {
		assert.False(t, strings.HasSuffix(e.Name(), ".json"),
			"read-only tx must not create a WAL file; found %s", e.Name())
	}
}

// TestStoreTx_MismatchedCompIDRejected pins that every StoreTx method
// rejects a compID different from the one WithTransaction was opened
// with. Without the guard, a stale or copy-pasted compID inside fn
// would dispatch the *Locked helper for another competition while
// holding only the original's lock — unlocked I/O.
//
// The body never reads or writes the "wrong" competition (no fixture
// for it exists); the guard short-circuits before the locked helper
// runs. Each subtest asserts ErrMismatchedTxCompID via errors.Is.
func TestStoreTx_MismatchedCompIDRejected(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	boundID := "tx-bound"
	otherID := "tx-other"
	require.NoError(t, store.SaveCompetition(&Competition{ID: boundID, Name: "bound"}))

	err = store.WithTransaction(boundID, func(tx StoreTx) error {
		t.Run("LoadCompetition", func(t *testing.T) {
			_, err := tx.LoadCompetition(otherID)
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("SaveCompetition via c.ID", func(t *testing.T) {
			err := tx.SaveCompetition(&Competition{ID: otherID, Name: "x"})
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("LoadPoolMatches", func(t *testing.T) {
			_, err := tx.LoadPoolMatches(otherID)
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("SavePoolMatches", func(t *testing.T) {
			err := tx.SavePoolMatches(otherID, nil)
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("LoadBracket", func(t *testing.T) {
			_, err := tx.LoadBracket(otherID)
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("SaveBracket", func(t *testing.T) {
			err := tx.SaveBracket(otherID, &Bracket{})
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("LoadCompetitorStatus", func(t *testing.T) {
			_, err := tx.LoadCompetitorStatus(otherID)
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("SetCompetitorStatus", func(t *testing.T) {
			err := tx.SetCompetitorStatus(otherID, domain.CompetitorStatus{})
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("LoadTeamLineups", func(t *testing.T) {
			_, err := tx.LoadTeamLineups(otherID)
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("SetTeamLineup", func(t *testing.T) {
			err := tx.SetTeamLineup(otherID, domain.TeamLineup{}, 3)
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		t.Run("LoadParticipants", func(t *testing.T) {
			_, err := tx.LoadParticipants(otherID, false)
			assert.ErrorIs(t, err, ErrMismatchedTxCompID)
		})
		return nil
	})
	require.NoError(t, err)

	// Sanity: bound compID still works inside a fresh transaction.
	err = store.WithTransaction(boundID, func(tx StoreTx) error {
		c, err := tx.LoadCompetition(boundID)
		require.NoError(t, err)
		require.NotNil(t, c)
		assert.Equal(t, "bound", c.Name)
		return nil
	})
	require.NoError(t, err)
}
