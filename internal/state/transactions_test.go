package state

import (
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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

// TestWithTransaction_RollbackOnError pins the documented
// "no actual rollback" semantics. WithTransaction's contract is
// lock-level atomicity, NOT filesystem ACID — if fn writes a value and
// then returns an error, the write stays on disk and the error
// propagates to the caller. The test asserts BOTH:
//   - the error from fn is returned unchanged
//   - the partial write is observable after the tx fails
//
// If a future change introduces real rollback, this test should fail
// loudly so the contract docs in transactions.go get updated in lock-step.
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
	assert.Equal(t, "B", loaded.Name,
		"WithTransaction is lock-level only — partial writes are NOT undone; "+
			"if this fails the package-level rollback comment is now stale")
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
