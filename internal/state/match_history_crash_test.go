package state

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file pins crash atomicity between a match write (pool-matches.csv)
// and its match-history entry (history/<matchID>.jsonl), both staged through
// the SAME transaction per the match_history.go package doc and the
// StoreTx.AppendMatchHistory contract.
//
// Two complementary techniques are used, mirroring the two that already
// exist in transactions_test.go:
//
//  1. A REAL failing Apply (the preferred technique, used by
//     TestWithTransaction_MatchHistoryCrashAtomicity_ApplyFailsOn*): a
//     genuine store.WithTransaction call whose fn stages both writes, after
//     which a directory's permissions are changed (chmod 0500) so that the
//     real Apply loop's atomicWriteFile call fails on one specific intent,
//     exactly as TestWithTransaction_ApplyFailureAfterCommitIsMarked does
//     for a single file. Apply() (wal.go) writes intents strictly in
//     insertion order and returns on the FIRST error, so chmod'ing the
//     history/ subdirectory lets the pool-matches.csv intent (first,
//     directly inside the competition directory) land before Apply fails on
//     the history intent (second); chmod'ing the competition directory
//     itself instead fails the pool-matches.csv intent before Apply ever
//     attempts the history one. No seam is needed because a real file-
//     system permission denial IS the fault.
//  2. A hand-built wal.WAL (TestWithTransaction_MatchHistoryCrashAtomicity
//     and TestWithTransaction_MatchHistoryCrashMidApply), same technique as
//     the existing TestWithTransaction_MultiFileAtomicityCrashAfterCommit:
//     Commit without Apply, then let a fresh NewStore's startup replay scan
//     complete the job. This is kept for the "crash before Apply even
//     starts" case and for pinning that replay is insertion-order-
//     independent, neither of which the permission-based technique can
//     reach (a real transaction always stages the match write before its
//     history entry, see CLAUDE.md's "The history" section on
//     recordWriteHistory). realMatchWriteAndHistoryBytes drives the real
//     production code once to capture genuine bytes, rather than hand-
//     typing CSV/JSON, so what's hand-built is only the WAL file itself.
//
// TestWithTransaction_MatchHistoryAbortTogether exercises the OTHER half
// through the real, unmodified WithTransaction path (no WAL hand-crafting
// or chmod needed): fn stages both writes and then returns an error, so
// neither must land.

// realMatchWriteAndHistoryBytes drives the real application code (a normal,
// fully-applied WithTransaction call) to produce the exact bytes a pool
// match write and its history entry stage, so the crash tests below replay
// genuine production output. The source store is a throwaway temp dir,
// discarded immediately after the bytes are captured.
func realMatchWriteAndHistoryBytes(t *testing.T, compID, matchID string) (poolMatchesCSV, historyJSONL []byte) {
	t.Helper()
	srcDir, err := os.MkdirTemp("", "state-tx-hist-crash-src-*")
	require.NoError(t, err)
	defer os.RemoveAll(srcDir)

	src, err := NewStore(srcDir)
	require.NoError(t, err)
	require.NoError(t, src.SaveCompetition(&Competition{ID: compID, Name: "crash-src"}))

	txErr := src.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SavePoolMatches(compID, []MatchResult{{
			ID:     matchID,
			SideA:  "Alice",
			SideB:  "Bob",
			Winner: "Alice",
			Status: MatchStatusCompleted,
			Court:  "A",
		}}); err != nil {
			return err
		}
		return tx.AppendMatchHistory(compID, MatchHistoryEntry{
			MatchID:  matchID,
			Door:     "score",
			Stamp:    1000,
			Changed:  []string{"result"},
			Outcomes: map[string]string{"result": HistoryOutcomeApplied},
		})
	})
	require.NoError(t, txErr, "source transaction must land for real so we can read back real bytes")

	poolMatchesCSV, err = os.ReadFile(src.compPath(compID, "pool-matches.csv")) // #nosec G304, test-controlled path
	require.NoError(t, err)
	historyJSONL, err = os.ReadFile(src.matchHistoryPath(compID, matchID)) // #nosec G304, test-controlled path
	require.NoError(t, err)
	return poolMatchesCSV, historyJSONL
}

// newCrashStoreWithComp creates a fresh store + competition for a crash
// simulation. It also creates the match-history directory directly, since
// ensureMatchHistoryDirLocked's os.Mkdir runs synchronously inside fn
// (before Commit, not WAL-staged) in the real path, so by the time a real
// crash could strand a committed-but-unapplied WAL, that directory already
// exists on disk. atomicWriteFile never creates parent directories, so
// skipping this step would make Apply fail on the history intent for a
// reason the real code path never has.
func newCrashStoreWithComp(t *testing.T, compID string) (store *Store, dir string) {
	t.Helper()
	dir, err := os.MkdirTemp("", "state-tx-hist-crash-*")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })

	store, err = NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "crash-test"}))
	require.NoError(t, os.Mkdir(store.compPath(compID, matchHistoryDir), 0o700))
	return store, dir
}

// TestWithTransaction_MatchHistoryCrashAtomicity pins the A1/bc-mrgc claim
// that a process dying after the WAL commit but before any Apply runs still
// lands BOTH the match write and its history entry together on the next
// startup, never just one of them.
func TestWithTransaction_MatchHistoryCrashAtomicity(t *testing.T) {
	const compID = "tx-hist-crash"
	const matchID = "Pool A-0"

	poolMatchesCSV, historyJSONL := realMatchWriteAndHistoryBytes(t, compID, matchID)

	store, dir := newCrashStoreWithComp(t, compID)

	// Baseline: neither target exists before we stage anything.
	_, err := os.Stat(store.compPath(compID, "pool-matches.csv"))
	require.True(t, os.IsNotExist(err), "pool-matches.csv must not exist before staging")
	_, err = os.Stat(store.matchHistoryPath(compID, matchID))
	require.True(t, os.IsNotExist(err), "history file must not exist before staging")

	w, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL)
	require.NoError(t, err)
	w.Append(wal.FileIntent{
		Path: store.compPath(compID, "pool-matches.csv"),
		Data: poolMatchesCSV,
		Mode: 0o600,
	})
	w.Append(wal.FileIntent{
		Path: store.matchHistoryPath(compID, matchID),
		Data: historyJSONL,
		Mode: 0o600,
	})
	require.NoError(t, w.Commit())

	// Simulated crash: the WAL is committed to disk, but NOTHING has been
	// Applied yet, mirrors a crash right after Commit returns.
	_, err = os.Stat(store.compPath(compID, "pool-matches.csv"))
	require.True(t, os.IsNotExist(err), "pool-matches.csv must not exist pre-replay")
	_, err = os.Stat(store.matchHistoryPath(compID, matchID))
	require.True(t, os.IsNotExist(err), "history file must not exist pre-replay")
	walEntriesPreReplay, err := os.ReadDir(store.walDir)
	require.NoError(t, err)
	hasCommittedWAL := false
	for _, e := range walEntriesPreReplay {
		if strings.HasSuffix(e.Name(), ".json") {
			hasCommittedWAL = true
		}
	}
	require.True(t, hasCommittedWAL, "the committed WAL file must be on disk pre-replay")

	// Restart: NewStore's init scans .wal/ and replays the committed WAL.
	store2, err := NewStore(dir)
	require.NoError(t, err)

	matches, err := store2.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1, "WAL replay must land the match write")
	assert.Equal(t, "Alice", matches[0].Winner)
	assert.Equal(t, MatchStatusCompleted, matches[0].Status)

	history, err := store2.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	require.Len(t, history, 1, "WAL replay must land the match's history entry in the SAME replay")
	assert.Equal(t, "score", history[0].Door)
	assert.Equal(t, int64(1000), history[0].Stamp)
	assert.Equal(t, []string{"result"}, history[0].Changed)

	walEntries, err := os.ReadDir(store.walDir)
	require.NoError(t, err)
	for _, e := range walEntries {
		assert.False(t, strings.HasSuffix(e.Name(), ".json"),
			"WAL file must be removed after replay completes; found %s", e.Name())
	}
}

// TestWithTransaction_MatchHistoryCrashMidApply pins a crash after the real
// Apply loop has already landed the FIRST of the two intents but before it
// reaches the second, in BOTH orderings. The "match write landed first"
// ordering is the realistic one (a real transaction always stages
// SavePoolMatches before AppendMatchHistory, see CLAUDE.md's "The history"
// section on recordWriteHistory running after the write); the reversed
// ordering is NOT something a real transaction produces, it is included to
// pin that replay itself is insertion-order-independent, a property of
// wal.Apply's loop rather than of this specific pair of writers.
//
// No fault-injection seam is needed for this: the "first intent already on
// disk" state is produced by calling store.directWriteWAL directly with the
// real captured bytes, exactly the write the real Apply loop would have
// performed for that intent before a crash interrupted it.
func TestWithTransaction_MatchHistoryCrashMidApply(t *testing.T) {
	const compID = "tx-hist-crash-mid"
	const matchID = "Pool A-0"

	poolMatchesCSV, historyJSONL := realMatchWriteAndHistoryBytes(t, compID, matchID)

	cases := []struct {
		name      string
		firstPath func(s *Store) string
		firstData []byte
	}{
		{
			name:      "match write landed before the crash, history did not",
			firstPath: func(s *Store) string { return s.compPath(compID, "pool-matches.csv") },
			firstData: poolMatchesCSV,
		},
		{
			name:      "history entry landed before the crash, match write did not",
			firstPath: func(s *Store) string { return s.matchHistoryPath(compID, matchID) },
			firstData: historyJSONL,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store, dir := newCrashStoreWithComp(t, compID)

			w, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL)
			require.NoError(t, err)
			w.Append(wal.FileIntent{Path: store.compPath(compID, "pool-matches.csv"), Data: poolMatchesCSV, Mode: 0o600})
			w.Append(wal.FileIntent{Path: store.matchHistoryPath(compID, matchID), Data: historyJSONL, Mode: 0o600})
			require.NoError(t, w.Commit())

			// Simulate the crash landing mid-Apply: the FIRST intent made
			// it to disk, exactly as the real Apply loop would have
			// written it, the second did not.
			require.NoError(t, store.directWriteWAL(tc.firstPath(store), tc.firstData, 0o600))

			// Restart: NewStore's init scans .wal/ and replays the still-
			// committed WAL (Apply is idempotent, so re-writing the
			// already-landed intent is harmless).
			store2, err := NewStore(dir)
			require.NoError(t, err)

			matches, err := store2.LoadPoolMatches(compID)
			require.NoError(t, err)
			require.Len(t, matches, 1, "the match write must land regardless of apply order")
			assert.Equal(t, "Alice", matches[0].Winner)

			history, err := store2.LoadMatchHistory(compID, matchID)
			require.NoError(t, err)
			require.Len(t, history, 1, "the history entry must land regardless of apply order")
			assert.Equal(t, "score", history[0].Door)
		})
	}
}

// TestWithTransaction_MatchHistoryCrashAtomicity_ApplyFailsOnSecondIntent
// pins a real WithTransaction call whose Apply loop succeeds on the FIRST
// intent (the match write) and then genuinely FAILS on the SECOND (the
// history append), via a real permission denial rather than any
// hand-crafted WAL. wal.Apply (wal.go) writes staged intents strictly in
// insertion order and returns the first error it hits; this is the same
// technique TestWithTransaction_ApplyFailureAfterCommitIsMarked uses for a
// single file, applied here to the match-write/history-entry pair: chmod
// the history/ subdirectory to 0500 as the last step of fn (after both
// writes are staged), so the loop writes pool-matches.csv successfully and
// then fails creating the history file's tmp sibling inside the now-
// unwritable history/ directory.
func TestWithTransaction_MatchHistoryCrashAtomicity_ApplyFailsOnSecondIntent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 isn't enforced on Windows the same way")
	}
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test: root bypasses file permission restrictions")
	}

	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "tx-hist-apply-fail-2nd"
	const matchID = "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Apply Fails On History"}))

	walDir := store.walDir
	historyDir := store.compPath(compID, matchHistoryDir)

	walCountBefore := func() int {
		entries, rerr := os.ReadDir(walDir)
		require.NoError(t, rerr)
		return len(entries)
	}
	before := walCountBefore()

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if serr := tx.SavePoolMatches(compID, []MatchResult{{
			ID:     matchID,
			SideA:  "Alice",
			SideB:  "Bob",
			Winner: "Alice",
			Status: MatchStatusCompleted,
			Court:  "A",
		}}); serr != nil {
			return serr
		}
		// AppendMatchHistory's ensureMatchHistoryDirLocked creates
		// historyDir synchronously here (0700), before Commit even
		// runs. Only AFTER it exists do we lock it down.
		if serr := tx.AppendMatchHistory(compID, MatchHistoryEntry{
			MatchID: matchID, Door: "score", Stamp: 1000,
		}); serr != nil {
			return serr
		}
		return os.Chmod(historyDir, 0500)
	})
	require.NoError(t, os.Chmod(historyDir, 0700), "restore permissions before any further I/O")
	require.Error(t, txErr)
	assert.ErrorIs(t, txErr, ErrTxCommitted, "a real Apply failure must be marked ErrTxCommitted")
	assert.Contains(t, txErr.Error(), "Apply:")
	assert.Equal(t, before+1, walCountBefore(), "the committed WAL stays on disk for the replay")

	// The match write (first intent) landed for real before Apply hit the
	// second intent's permission error.
	onDisk, err := os.ReadFile(store.compPath(compID, "pool-matches.csv")) // #nosec G304, test-controlled path
	require.NoError(t, err, "the first intent must have landed on disk before Apply failed")
	assert.Contains(t, string(onDisk), "Alice")

	// The history file must NOT exist yet, Apply never reached it.
	_, err = os.Stat(store.matchHistoryPath(compID, matchID))
	require.True(t, os.IsNotExist(err), "the second (failed) intent must not exist pre-replay")

	// Restart: NewStore's init scans .wal/ and replays the still-committed
	// WAL, now that permissions are restored, completing the stranded
	// history intent.
	restarted, err := NewStore(dir)
	require.NoError(t, err)

	matches, err := restarted.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "Alice", matches[0].Winner)

	history, err := restarted.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	require.Len(t, history, 1, "replay must land the history entry the failed Apply never reached")
	assert.Equal(t, "score", history[0].Door)
}

// TestWithTransaction_MatchHistoryCrashAtomicity_ApplyFailsOnFirstIntent
// pins the mirror case: Apply fails on the VERY FIRST intent (the match
// write), so wal.Apply's loop (which returns on the first error) never even
// attempts the second (the history append). Both must still land together
// once the real transaction replays. This is produced by chmod'ing the
// competition directory itself (not history/) to 0500: pool-matches.csv
// lives directly inside it, so its atomicWriteFile call fails at tmp-create;
// history/ is a subdirectory with its own (untouched) permissions, so had
// Apply reached it, it would have succeeded, but Apply never gets that far.
func TestWithTransaction_MatchHistoryCrashAtomicity_ApplyFailsOnFirstIntent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 isn't enforced on Windows the same way")
	}
	if os.Getuid() == 0 {
		t.Skip("Skipping permission test: root bypasses file permission restrictions")
	}

	dir := t.TempDir()
	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "tx-hist-apply-fail-1st"
	const matchID = "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Apply Fails On Match"}))

	walDir := store.walDir
	compDir := store.compPath(compID)

	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if serr := tx.SavePoolMatches(compID, []MatchResult{{
			ID:     matchID,
			SideA:  "Alice",
			SideB:  "Bob",
			Winner: "Alice",
			Status: MatchStatusCompleted,
			Court:  "A",
		}}); serr != nil {
			return serr
		}
		if serr := tx.AppendMatchHistory(compID, MatchHistoryEntry{
			MatchID: matchID, Door: "score", Stamp: 1000,
		}); serr != nil {
			return serr
		}
		return os.Chmod(compDir, 0500)
	})
	require.NoError(t, os.Chmod(compDir, 0700), "restore permissions before any further I/O")
	require.Error(t, txErr)
	assert.ErrorIs(t, txErr, ErrTxCommitted)
	assert.Contains(t, txErr.Error(), "Apply:")

	walEntries, err := os.ReadDir(walDir)
	require.NoError(t, err)
	committedWALCount := 0
	for _, e := range walEntries {
		if strings.HasSuffix(e.Name(), ".json") {
			committedWALCount++
		}
	}
	assert.Equal(t, 1, committedWALCount, "the committed WAL stays on disk for the replay")

	// Neither file landed, Apply failed on the first intent before even
	// attempting the second.
	_, err = os.Stat(store.compPath(compID, "pool-matches.csv"))
	require.True(t, os.IsNotExist(err))
	_, err = os.Stat(store.matchHistoryPath(compID, matchID))
	require.True(t, os.IsNotExist(err))

	restarted, err := NewStore(dir)
	require.NoError(t, err)

	matches, err := restarted.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "Alice", matches[0].Winner)

	history, err := restarted.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	require.Len(t, history, 1, "replay must land both intents even though neither applied pre-crash")
}

// TestWithTransaction_MatchHistoryAbortTogether exercises the OTHER half of
// the atomicity claim through the real, unmodified WithTransaction path: an
// fn that stages a match write AND its history entry, then returns an
// error, must leave NEITHER on disk. No WAL hand-crafting is needed for
// this case since the abort path never calls Commit.
func TestWithTransaction_MatchHistoryAbortTogether(t *testing.T) {
	dir, err := os.MkdirTemp("", "state-tx-hist-abort-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	const compID = "tx-hist-abort"
	const matchID = "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "abort-test"}))

	sentinel := errors.New("simulated fn failure after both writes staged")
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SavePoolMatches(compID, []MatchResult{{
			ID:     matchID,
			SideA:  "Alice",
			SideB:  "Bob",
			Winner: "Alice",
			Status: MatchStatusCompleted,
			Court:  "A",
		}}); err != nil {
			return err
		}
		if err := tx.AppendMatchHistory(compID, MatchHistoryEntry{
			MatchID: matchID,
			Door:    "score",
			Stamp:   1000,
		}); err != nil {
			return err
		}
		return sentinel
	})
	require.ErrorIs(t, txErr, sentinel, "fn error must propagate unchanged")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Empty(t, matches, "an aborted tx must NOT persist the staged match write")

	history, err := store.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	assert.Empty(t, history, "an aborted tx must NOT persist the staged history entry either")

	walEntries, err := os.ReadDir(store.walDir)
	require.NoError(t, err)
	for _, e := range walEntries {
		assert.False(t, strings.HasSuffix(e.Name(), ".json"),
			"aborted tx must leave no WAL file; found %s", e.Name())
	}
}
