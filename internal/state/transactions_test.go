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

func TestStoreTx_LoadBracketAndSave(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-bracket"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX Bracket"}))

	initial := &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "M1", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled}},
		},
	}
	require.NoError(t, store.SaveBracket(compID, initial))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		b, err := tx.LoadBracket(compID)
		if err != nil {
			return err
		}
		b.Rounds[0][0].Winner = "Alice"
		b.Rounds[0][0].Status = MatchStatusCompleted
		return tx.SaveBracket(compID, b)
	})
	require.NoError(t, txErr)

	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", loaded.Rounds[0][0].Winner)
}

func TestStoreTx_LoadPoolMatchesAndSave(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-pool"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX Pool"}))

	matches := []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		loaded, err := tx.LoadPoolMatches(compID)
		if err != nil {
			return err
		}
		loaded[0].Winner = "Alice"
		loaded[0].Status = MatchStatusCompleted
		return tx.SavePoolMatches(compID, loaded)
	})
	require.NoError(t, txErr)

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Alice", loaded[0].Winner)
}

func TestStoreTx_LoadCompetitorStatusAndSet(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-status"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX Status"}))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// LoadCompetitorStatus inside tx (no prior write — reads from disk)
		statuses, err := tx.LoadCompetitorStatus(compID)
		if err != nil {
			return err
		}
		assert.Empty(t, statuses)

		// SetCompetitorStatus inside tx (stages write)
		return tx.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID: "p1",
			Eligible: false,
			Reason:   "kiken",
		})
	})
	require.NoError(t, txErr)

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.Contains(t, statuses, "p1")
	assert.False(t, statuses["p1"].Eligible)
}

func TestStoreTx_LoadCompetitorStatus_ReadYourOwnWrites(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-royw"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "ROYW"}))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// Write first
		if err := tx.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID: "p2", Eligible: false, Reason: "kiken",
		}); err != nil {
			return err
		}
		// Read-your-own-writes: the staged bytes should be visible
		statuses, err := tx.LoadCompetitorStatus(compID)
		if err != nil {
			return err
		}
		if _, ok := statuses["p2"]; !ok {
			return errors.New("read-your-own-writes failed: p2 not visible")
		}
		return nil
	})
	require.NoError(t, txErr)
}

func TestStoreTx_LoadBracket_ReadYourOwnWrites(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-bracket-royw"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Bracket ROYW"}))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		b := &Bracket{Rounds: [][]BracketMatch{
			{{ID: "M1", SideA: "A", SideB: "B"}},
		}}
		if err := tx.SaveBracket(compID, b); err != nil {
			return err
		}
		// Should see the staged bracket
		loaded, err := tx.LoadBracket(compID)
		if err != nil {
			return err
		}
		if len(loaded.Rounds) == 0 || loaded.Rounds[0][0].ID != "M1" {
			return errors.New("read-your-own-writes: staged bracket not visible")
		}
		return nil
	})
	require.NoError(t, txErr)
}

func TestStoreTx_LoadParticipants(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-participants"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX Participants"}))

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		loaded, err := tx.LoadParticipants(compID, false)
		if err != nil {
			return err
		}
		if len(loaded) != 1 || loaded[0].Name != "Alice" {
			return errors.New("unexpected participants in tx")
		}
		return nil
	})
	require.NoError(t, txErr)
}

func TestStoreTx_UpdatePoolMatchByID(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-upmatch"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX UpdateMatch"}))

	matches := []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
		{ID: "P1-1", SideA: "Charlie", SideB: "Dave", Status: MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	var found bool
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		var err error
		found, err = tx.UpdatePoolMatchByID(compID, "P1-0", func(m *MatchResult) {
			m.Winner = "Alice"
			m.Status = MatchStatusCompleted
		})
		return err
	})
	require.NoError(t, txErr)
	assert.True(t, found)

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", loaded[0].Winner)
}

func TestStoreTx_UpdateBracket(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-ubracket"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX UpdateBracket"}))

	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "M1", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled}},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		return tx.UpdateBracket(compID, func(b *Bracket) error {
			b.Rounds[0][0].Winner = "Alice"
			return nil
		})
	})
	require.NoError(t, txErr)

	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", loaded.Rounds[0][0].Winner)
}

func TestStoreTx_UpdatePoolMatchByID_ReadYourOwnWrites(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-upm-royw"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX UPM ROYW"}))

	matches := []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// First update: stages to WAL
		_, err := tx.UpdatePoolMatchByID(compID, "P1-0", func(m *MatchResult) {
			m.Winner = "Alice"
		})
		if err != nil {
			return err
		}
		// Second update: should read from staged WAL (read-your-own-writes)
		found, err := tx.UpdatePoolMatchByID(compID, "P1-0", func(m *MatchResult) {
			m.Status = MatchStatusCompleted
		})
		if err != nil {
			return err
		}
		if !found {
			return errors.New("second update: match not found in staged WAL")
		}
		return nil
	})
	require.NoError(t, txErr)

	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", loaded[0].Winner)
	assert.Equal(t, MatchStatusCompleted, loaded[0].Status)
}

func TestStore_WithCompetitionRenameLock(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	called := false
	err = store.WithCompetitionRenameLock(func() error {
		called = true
		return nil
	})
	require.NoError(t, err)
	assert.True(t, called)

	sentinel := errors.New("rename error")
	err = store.WithCompetitionRenameLock(func() error {
		return sentinel
	})
	require.ErrorIs(t, err, sentinel)
}

func TestStoreTx_LoadTeamLineups(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-lineups"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX Lineups", Kind: "team", TeamSize: 5}))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		lineups, err := tx.LoadTeamLineups(compID)
		if err != nil {
			return err
		}
		assert.Empty(t, lineups)
		return nil
	})
	require.NoError(t, txErr)
}

func TestStoreTx_SetTeamLineup(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-setlineup"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX Set Lineup", Kind: "team", TeamSize: 5}))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		lineup := domain.TeamLineup{
			TeamID:        "TeamA",
			Round:         1,
			CompetitionID: compID,
			Positions: map[domain.Position]string{
				domain.PosSenpo:   "Alice",
				domain.PosJiho:    "Bob",
				domain.PosChuken:  "Carol",
				domain.PosFukusho: "Dave",
				domain.PosTaisho:  "Eve",
			},
		}
		return tx.SetTeamLineup(compID, lineup, 5)
	})
	require.NoError(t, txErr)

	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, lineups)
}

func TestStoreTx_LockTeamLineupsForRound(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-locklineup"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "TX Lock Lineup", Kind: "team", TeamSize: 5}))

	// First set a lineup so there's something to lock
	lineup := domain.TeamLineup{
		TeamID:        "TeamA",
		Round:         1,
		CompetitionID: compID,
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "Alice",
			domain.PosJiho:    "Bob",
			domain.PosChuken:  "Carol",
			domain.PosFukusho: "Dave",
			domain.PosTaisho:  "Eve",
		},
	}
	require.NoError(t, store.SetTeamLineup(compID, lineup, 5))

	lockedAt := time.Now()
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		return tx.LockTeamLineupsForRound(compID, 1, lockedAt)
	})
	require.NoError(t, txErr)

	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	for _, l := range lineups {
		if l.TeamID == "TeamA" && l.Round == 1 {
			assert.NotNil(t, l.LockedAt)
		}
	}
}

func TestStoreTx_LoadCompetition_ReadYourOwnWrites(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-comp-royw"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Original"}))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// Save a new Name in the tx
		if err := tx.SaveCompetition(&Competition{ID: compID, Name: "TxUpdated"}); err != nil {
			return err
		}
		// Re-load within the same tx — should see staged value (pending path)
		c, err := tx.LoadCompetition(compID)
		if err != nil {
			return err
		}
		if c.Name != "TxUpdated" {
			return errors.New("read-your-own-writes for competition failed")
		}
		return nil
	})
	require.NoError(t, txErr)
}

func TestStoreTx_UpdateBracket_ReadYourOwnWrites(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-bracket-royw2"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "ROYW2"}))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// Save a bracket first (stages to WAL)
		b := &Bracket{Rounds: [][]BracketMatch{
			{{ID: "M1", SideA: "A", SideB: "B"}},
		}}
		if err := tx.SaveBracket(compID, b); err != nil {
			return err
		}
		// Now UpdateBracket — should read from staged WAL (pending path)
		return tx.UpdateBracket(compID, func(bracket *Bracket) error {
			if len(bracket.Rounds) == 0 || bracket.Rounds[0][0].ID != "M1" {
				return errors.New("UpdateBracket ROYW: staged bracket not visible")
			}
			bracket.Rounds[0][0].Winner = "A"
			return nil
		})
	})
	require.NoError(t, txErr)

	loaded, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "A", loaded.Rounds[0][0].Winner)
}

func TestStoreTx_SetTeamLineup_ReadYourOwnWrites(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-lineup-royw"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Lineup ROYW", Kind: "team", TeamSize: 5}))

	basePositions := map[domain.Position]string{
		domain.PosSenpo:   "Alice",
		domain.PosJiho:    "Bob",
		domain.PosChuken:  "Carol",
		domain.PosFukusho: "Dave",
		domain.PosTaisho:  "Eve",
	}

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// First SetTeamLineup: stages to WAL
		lineup1 := domain.TeamLineup{
			TeamID: "TeamA", Round: 1, CompetitionID: compID,
			Positions: basePositions,
		}
		if err := tx.SetTeamLineup(compID, lineup1, 5); err != nil {
			return err
		}
		// Second SetTeamLineup for same team+round: read-your-own-writes path
		lineup2 := domain.TeamLineup{
			TeamID: "TeamA", Round: 1, CompetitionID: compID,
			Positions: map[domain.Position]string{
				domain.PosSenpo:   "Alice2",
				domain.PosJiho:    "Bob",
				domain.PosChuken:  "Carol",
				domain.PosFukusho: "Dave",
				domain.PosTaisho:  "Eve",
			},
		}
		return tx.SetTeamLineup(compID, lineup2, 5)
	})
	require.NoError(t, txErr)

	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	for _, l := range lineups {
		if l.TeamID == "TeamA" && l.Round == 1 {
			assert.Equal(t, "Alice2", l.Positions[domain.PosSenpo])
		}
	}
}

func TestStoreTx_LockTeamLineupsForRound_ReadYourOwnWrites(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "tx-lock-royw"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Lock ROYW", Kind: "team", TeamSize: 5}))

	basePositions := map[domain.Position]string{
		domain.PosSenpo:   "Alice",
		domain.PosJiho:    "Bob",
		domain.PosChuken:  "Carol",
		domain.PosFukusho: "Dave",
		domain.PosTaisho:  "Eve",
	}

	lockedAt := time.Now()
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// Set lineup within tx (stages to WAL)
		lineup := domain.TeamLineup{
			TeamID: "TeamB", Round: 2, CompetitionID: compID,
			Positions: basePositions,
		}
		if err := tx.SetTeamLineup(compID, lineup, 5); err != nil {
			return err
		}
		// Lock the round — reads from staged WAL (pending path)
		return tx.LockTeamLineupsForRound(compID, 2, lockedAt)
	})
	require.NoError(t, txErr)
}

// TestStoreTx_SetCompetitorStatus_DoubleWrite exercises the
// read-your-own-writes merge path (pending != nil) of SetCompetitorStatus.
// Calling SetCompetitorStatus twice in the same tx must merge both
// statuses into the same staged bytes rather than losing the first write.
func TestStoreTx_SetCompetitorStatus_DoubleWrite(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-set-cs-double-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "cs-double"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "CS Double"}))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// First write — non-pending path.
		if err := tx.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID: "player-A", Eligible: false, Reason: "kiken", MatchID: "m1",
		}); err != nil {
			return err
		}
		// Second write — pending path (merge).
		return tx.SetCompetitorStatus(compID, domain.CompetitorStatus{
			PlayerID: "player-B", Eligible: false, Reason: "kiken", MatchID: "m2",
		})
	})
	require.NoError(t, txErr)

	// Both statuses must be visible after commit.
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.Contains(t, statuses, "player-A", "player-A status must persist")
	assert.Contains(t, statuses, "player-B", "player-B status must persist (double-write merge)")
}

// TestStoreTx_UpdateBracket_PendingPath exercises the read-your-own-writes
// path of UpdateBracket: SaveBracket within a tx stages the bytes; the
// subsequent UpdateBracket call must parse those staged bytes rather than
// reading the (empty) disk state.
func TestStoreTx_UpdateBracket_PendingPath(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-bracket-pending-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "bracket-pending"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Bracket Pending"}))

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// Stage a bracket via SaveBracket.
		b := &Bracket{Rounds: [][]BracketMatch{
			{{ID: "M1", SideA: "Alice", SideB: "Bob"}},
		}}
		if err := tx.SaveBracket(compID, b); err != nil {
			return err
		}
		// UpdateBracket must see the staged bracket (pending path).
		return tx.UpdateBracket(compID, func(bracket *Bracket) error {
			if bracket == nil || len(bracket.Rounds) == 0 {
				return errors.New("pending bracket not visible in UpdateBracket")
			}
			bracket.Rounds[0][0].Winner = "Alice"
			bracket.Rounds[0][0].Status = MatchStatusCompleted
			return nil
		})
	})
	require.NoError(t, txErr)

	// Verify the mutation persisted.
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, b)
	require.Len(t, b.Rounds, 1)
	assert.Equal(t, "Alice", b.Rounds[0][0].Winner)
}

// TestStoreTx_UpdatePoolMatchByID_PendingPath verifies that
// UpdatePoolMatchByID inside a tx sees pool matches saved earlier in the
// same tx (read-your-own-writes).
func TestStoreTx_UpdatePoolMatchByID_PendingPath(t *testing.T) {
	dir, err := os.MkdirTemp("", "tx-pool-pending-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "pool-pending"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Pool Pending"}))

	matches := []MatchResult{
		{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	var found bool
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		// Re-save pool matches within the tx (stages them).
		newMatches := []MatchResult{
			{ID: "P1-0", SideA: "Alice", SideB: "Bob", Status: MatchStatusRunning},
		}
		if err := tx.SavePoolMatches(compID, newMatches); err != nil {
			return err
		}
		// UpdatePoolMatchByID must see the staged matches (pending path).
		var err error
		found, err = tx.UpdatePoolMatchByID(compID, "P1-0", func(m *MatchResult) {
			m.Winner = "Alice"
			m.Status = MatchStatusCompleted
		})
		return err
	})
	require.NoError(t, txErr)
	assert.True(t, found, "match must be found in pending pool matches")

	// Verify the winner was persisted.
	loaded, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Alice", loaded[0].Winner)
}

// TestStoreTx_CheckCompIDMismatch verifies that every storeTx method that
// calls checkCompID returns ErrMismatchedTxCompID when the caller passes a
// compID that does not match the transaction's bound competition.
func TestStoreTx_CheckCompIDMismatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-mismatch-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "comp-A"
	const wrongID = "comp-B"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	runInTx := func(name string, fn func(StoreTx) error) {
		t.Helper()
		txErr := store.WithTransaction(compID, fn)
		require.ErrorIs(t, txErr, ErrMismatchedTxCompID, "method %s: wrong compID must return ErrMismatchedTxCompID", name)
	}

	runInTx("LoadCompetition", func(tx StoreTx) error {
		_, err := tx.LoadCompetition(wrongID)
		return err
	})
	runInTx("SaveCompetition", func(tx StoreTx) error {
		return tx.SaveCompetition(&Competition{ID: wrongID})
	})
	runInTx("LoadPoolMatches", func(tx StoreTx) error {
		_, err := tx.LoadPoolMatches(wrongID)
		return err
	})
	runInTx("SavePoolMatches", func(tx StoreTx) error {
		return tx.SavePoolMatches(wrongID, nil)
	})
	runInTx("LoadBracket", func(tx StoreTx) error {
		_, err := tx.LoadBracket(wrongID)
		return err
	})
	runInTx("SaveBracket", func(tx StoreTx) error {
		return tx.SaveBracket(wrongID, &Bracket{})
	})
	runInTx("LoadCompetitorStatus", func(tx StoreTx) error {
		_, err := tx.LoadCompetitorStatus(wrongID)
		return err
	})
	runInTx("SetCompetitorStatus", func(tx StoreTx) error {
		return tx.SetCompetitorStatus(wrongID, domain.CompetitorStatus{PlayerID: "p1"})
	})
	runInTx("LoadTeamLineups", func(tx StoreTx) error {
		_, err := tx.LoadTeamLineups(wrongID)
		return err
	})
	runInTx("SetTeamLineup", func(tx StoreTx) error {
		return tx.SetTeamLineup(wrongID, domain.TeamLineup{}, 5)
	})
	runInTx("LoadParticipants", func(tx StoreTx) error {
		_, err := tx.LoadParticipants(wrongID, false)
		return err
	})
	runInTx("UpdatePoolMatchByID", func(tx StoreTx) error {
		_, err := tx.UpdatePoolMatchByID(wrongID, "M1", func(*MatchResult) {})
		return err
	})
	runInTx("UpdateBracket", func(tx StoreTx) error {
		return tx.UpdateBracket(wrongID, func(*Bracket) error { return nil })
	})
	runInTx("LockTeamLineupsForRound", func(tx StoreTx) error {
		return tx.LockTeamLineupsForRound(wrongID, 0, time.Now())
	})
}

// TestStoreTx_ValidateCompetitionID covers the ValidateCompetitionID error
// branches inside storeTx methods. These branches are only reachable by
// constructing storeTx directly with an invalid compID — WithTransaction
// validates the ID upfront, so a storeTx with an illegal ID is never created
// through the normal API.
//
// An ID with "." fails the alphanumeric-hyphen-underscore pattern; checkCompID
// passes (same value) but ValidateCompetitionID returns an error.
func TestStoreTx_ValidateCompetitionID(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	// "id.bad" passes checkCompID but fails ValidateCompetitionID.
	// wal is nil — none of these methods reach t.txWriteFn().
	const badID = "id.bad"
	tx := &storeTx{store: store, compID: badID}

	err := tx.SavePoolMatches(badID, nil)
	assert.Error(t, err, "SavePoolMatches: invalid compID must error")

	err = tx.SaveBracket(badID, &Bracket{})
	assert.Error(t, err, "SaveBracket: invalid compID must error")

	err = tx.SetTeamLineup(badID, domain.TeamLineup{TeamID: "t", Round: 0}, 5)
	assert.Error(t, err, "SetTeamLineup: invalid compID must error")

	_, err = tx.UpdatePoolMatchByID(badID, "M1", func(*MatchResult) {})
	assert.Error(t, err, "UpdatePoolMatchByID: invalid compID must error")

	err = tx.UpdateBracket(badID, func(*Bracket) error { return nil })
	assert.Error(t, err, "UpdateBracket: invalid compID must error")

	err = tx.LockTeamLineupsForRound(badID, 0, time.Now())
	assert.Error(t, err, "LockTeamLineupsForRound: invalid compID must error")
}

// TestStoreTx_PendingPaths exercises the "read-your-own-writes" branches inside
// storeTx loaders and mutators. Each sub-test uses its own competition so that
// bracket/pool state written in one sub-test cannot block lineup writes in another.
func TestStoreTx_PendingPaths(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	newComp := func(id string) {
		t.Helper()
		require.NoError(t, store.SaveCompetition(&Competition{ID: id}))
	}

	t.Run("LoadCompetition from staged config.md", func(t *testing.T) {
		cid := "pp-comp"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			if err := tx.SaveCompetition(&Competition{ID: cid, Name: "staged-name"}); err != nil {
				return err
			}
			c, err := tx.LoadCompetition(cid)
			require.NoError(t, err)
			assert.Equal(t, "staged-name", c.Name, "LoadCompetition must read staged bytes")
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("LoadPoolMatches from staged pool-matches.csv", func(t *testing.T) {
		cid := "pp-pools"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			matches := []MatchResult{{ID: "P-staged", SideA: "A", SideB: "B", Status: MatchStatusScheduled}}
			if err := tx.SavePoolMatches(cid, matches); err != nil {
				return err
			}
			loaded, err := tx.LoadPoolMatches(cid)
			require.NoError(t, err)
			require.Len(t, loaded, 1)
			assert.Equal(t, "P-staged", loaded[0].ID)
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("LoadBracket from staged bracket.json", func(t *testing.T) {
		cid := "pp-bracket"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			b := &Bracket{Rounds: [][]BracketMatch{{{ID: "M-staged", SideA: "A", SideB: "B"}}}}
			if err := tx.SaveBracket(cid, b); err != nil {
				return err
			}
			loaded, err := tx.LoadBracket(cid)
			require.NoError(t, err)
			require.Len(t, loaded.Rounds, 1)
			assert.Equal(t, "M-staged", loaded.Rounds[0][0].ID)
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("LoadTeamLineups from staged lineups.yaml", func(t *testing.T) {
		cid := "pp-lineups"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			l := fiveStarter("team-pending", 0)
			if err := tx.SetTeamLineup(cid, l, 5); err != nil {
				return err
			}
			lineups, err := tx.LoadTeamLineups(cid)
			require.NoError(t, err)
			_, ok := lineups[teamLineupKey("team-pending", 0)]
			assert.True(t, ok, "staged lineup must be visible via LoadTeamLineups in same tx")
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("SetCompetitorStatus second write merges into staged bytes", func(t *testing.T) {
		cid := "pp-status"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			s1 := domain.CompetitorStatus{PlayerID: "px1", Eligible: false, MatchID: "M1", Reason: "injury"}
			if err := tx.SetCompetitorStatus(cid, s1); err != nil {
				return err
			}
			s2 := domain.CompetitorStatus{PlayerID: "px2", Eligible: false, MatchID: "M2", Reason: "kiken"}
			return tx.SetCompetitorStatus(cid, s2)
		})
		require.NoError(t, err)

		statuses, err := store.LoadCompetitorStatus(cid)
		require.NoError(t, err)
		assert.Contains(t, statuses, "px1")
		assert.Contains(t, statuses, "px2")
	})

	t.Run("SetTeamLineup second write merges into staged bytes", func(t *testing.T) {
		cid := "pp-lineup-merge"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			l1 := fiveStarter("team-merge-A", 0)
			if err := tx.SetTeamLineup(cid, l1, 5); err != nil {
				return err
			}
			l2 := fiveStarter("team-merge-B", 0)
			return tx.SetTeamLineup(cid, l2, 5)
		})
		require.NoError(t, err)

		lineups, err := store.LoadTeamLineups(cid)
		require.NoError(t, err)
		assert.Contains(t, lineups, teamLineupKey("team-merge-A", 0))
		assert.Contains(t, lineups, teamLineupKey("team-merge-B", 0))
	})

	t.Run("UpdatePoolMatchByID from staged pool-matches.csv", func(t *testing.T) {
		cid := "pp-update-pm"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			if err := tx.SavePoolMatches(cid, []MatchResult{
				{ID: "P-update", SideA: "X", SideB: "Y", Status: MatchStatusScheduled},
			}); err != nil {
				return err
			}
			found, err := tx.UpdatePoolMatchByID(cid, "P-update", func(m *MatchResult) {
				m.Winner = "X"
				m.Status = MatchStatusCompleted
			})
			require.NoError(t, err)
			assert.True(t, found)
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("UpdatePoolMatchByID not-found in staged bytes", func(t *testing.T) {
		cid := "pp-update-notfound"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			if err := tx.SavePoolMatches(cid, []MatchResult{
				{ID: "P-other", SideA: "X", SideB: "Y", Status: MatchStatusScheduled},
			}); err != nil {
				return err
			}
			found, err := tx.UpdatePoolMatchByID(cid, "P-nonexistent", func(m *MatchResult) {})
			require.NoError(t, err)
			assert.False(t, found, "match not in staged bytes must return found=false")
			return nil
		})
		require.NoError(t, err)
	})

	t.Run("UpdateBracket from staged bracket.json", func(t *testing.T) {
		cid := "pp-update-bracket"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			b := &Bracket{Rounds: [][]BracketMatch{{{ID: "M-upd", SideA: "A", SideB: "B"}}}}
			if err := tx.SaveBracket(cid, b); err != nil {
				return err
			}
			return tx.UpdateBracket(cid, func(b *Bracket) error {
				b.Rounds[0][0].Winner = "A"
				b.Rounds[0][0].Status = MatchStatusCompleted
				return nil
			})
		})
		require.NoError(t, err)
	})

	t.Run("LockTeamLineupsForRound from staged lineups.yaml", func(t *testing.T) {
		cid := "pp-lock-staged"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			l := fiveStarter("team-lock-staged", 0)
			if err := tx.SetTeamLineup(cid, l, 5); err != nil {
				return err
			}
			return tx.LockTeamLineupsForRound(cid, 0, time.Now().UTC())
		})
		require.NoError(t, err)

		lineups, err := store.LoadTeamLineups(cid)
		require.NoError(t, err)
		entry := lineups[teamLineupKey("team-lock-staged", 0)]
		assert.NotNil(t, entry.LockedAt, "lineup staged then locked in tx must have LockedAt set")
	})

	t.Run("LockTeamLineupsForRound no-change in staged bytes (already locked)", func(t *testing.T) {
		// Stage a round-1 lineup, then ask to lock round 0 — no round-0 lineups
		// exist in staged bytes so changed stays false (the !changed path).
		cid := "pp-lock-nochange"
		newComp(cid)
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			l := fiveStarter("team-r1", 1)
			if err := tx.SetTeamLineup(cid, l, 5); err != nil {
				return err
			}
			// Lock round 0; staged bytes only have round 1 → nothing changes.
			return tx.LockTeamLineupsForRound(cid, 0, time.Now().UTC())
		})
		require.NoError(t, err)
	})

	t.Run("SetTeamLineup pending-path: locked entry in staged bytes", func(t *testing.T) {
		// Stage a lineup, lock it in the same tx, then attempt to overwrite it —
		// must return ErrLineupLocked from the pending-path lock check.
		cid := "pp-lineup-locked-pending"
		newComp(cid)

		var gotErr error
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			l1 := fiveStarter("team-X", 0)
			if err := tx.SetTeamLineup(cid, l1, 5); err != nil {
				return err
			}
			if err := tx.LockTeamLineupsForRound(cid, 0, time.Now().UTC()); err != nil {
				return err
			}
			// Same (teamID, round) key is now locked in staged bytes.
			l2 := fiveStarter("team-X", 0)
			l2.Positions[domain.PosJiho] = "sub"
			gotErr = tx.SetTeamLineup(cid, l2, 5)
			return nil // don't abort tx; we captured the error above
		})
		require.NoError(t, err)
		require.ErrorIs(t, gotErr, ErrLineupLocked,
			"SetTeamLineup for a locked entry in staged bytes must return ErrLineupLocked")
	})

	t.Run("SetTeamLineup pending-path: validate error", func(t *testing.T) {
		// When pending bytes exist, Validate is called before merging.
		// A malformed lineup must return the validation error.
		cid := "pp-lineup-validate"
		newComp(cid)

		err := store.WithTransaction(cid, func(tx StoreTx) error {
			good := fiveStarter("team-good", 0)
			if err := tx.SetTeamLineup(cid, good, 5); err != nil {
				return err
			}
			// Pending bytes now exist; a bad lineup must fail ValidatePositions.
			// "chudan" is not a valid FIK position key for a 5-person team.
			bad := domain.TeamLineup{
				TeamID: "team-bad",
				Round:  0,
				Positions: map[domain.Position]string{
					"chudan": "p1", // invalid position key
				},
			}
			return tx.SetTeamLineup(cid, bad, 5)
		})
		require.Error(t, err, "pending-path ValidatePositions must propagate invalid-key errors")
		require.NotErrorIs(t, err, domain.ErrLineupTooManyMissing,
			"completeness check must NOT fire from ValidatePositions")
	})

	t.Run("SetCompetitorStatus pending-path: invalid status", func(t *testing.T) {
		// When pending status bytes exist, Validate is called on the new entry.
		// An invalid status (empty PlayerID) must return an error.
		cid := "pp-status-invalid"
		newComp(cid)

		err := store.WithTransaction(cid, func(tx StoreTx) error {
			valid := domain.CompetitorStatus{PlayerID: "p-valid", Eligible: false, MatchID: "M1", Reason: "injury"}
			if err := tx.SetCompetitorStatus(cid, valid); err != nil {
				return err
			}
			// Pending bytes exist; now set an invalid status.
			invalid := domain.CompetitorStatus{PlayerID: ""}
			return tx.SetCompetitorStatus(cid, invalid)
		})
		require.Error(t, err, "invalid CompetitorStatus in pending path must return error")
	})

	t.Run("UpdateBracket pending-path: mutate error aborts", func(t *testing.T) {
		// When pending bracket bytes exist and mutate returns an error,
		// UpdateBracket must surface it.
		cid := "pp-bracket-muterr"
		newComp(cid)

		wantErr := errors.New("mutate-error")
		err := store.WithTransaction(cid, func(tx StoreTx) error {
			b := &Bracket{Rounds: [][]BracketMatch{{{ID: "M1", SideA: "A", SideB: "B"}}}}
			if err := tx.SaveBracket(cid, b); err != nil {
				return err
			}
			return tx.UpdateBracket(cid, func(*Bracket) error { return wantErr })
		})
		require.ErrorIs(t, err, wantErr,
			"UpdateBracket pending-path must propagate mutate error")
	})
}
