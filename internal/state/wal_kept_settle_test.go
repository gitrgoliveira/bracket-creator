package state

import (
	"bytes"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A transaction whose log committed and whose Apply then failed part-way
// keeps its log in .wal (ErrTxCommitted). These tests pin that the next
// write to the competition finishes that change before it runs, so a later
// write can never overtake the log (which would set the whole change aside
// at the next startup), and that the write is refused while the disk still
// refuses the kept change.

func skipUnlessChmodEnforced(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("chmod 0500 is not enforced on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory mode 0500")
	}
}

func liveWALs(t *testing.T, s *Store) []string {
	t.Helper()
	live, err := filepath.Glob(filepath.Join(s.walDir, "*.json"))
	require.NoError(t, err)
	return live
}

// keptMatchAndHistory runs a transaction writing pool-matches.csv and a
// history entry, with history/ made read-only before the commit, so Apply
// lands pool-matches.csv and fails on the history file. Returns the history
// folder, left read-only for the caller to restore.
func keptMatchAndHistory(t *testing.T, s *Store, compID, matchID string) string {
	t.Helper()
	historyDir := s.compPath(compID, matchHistoryDir)
	txErr := s.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.SavePoolMatches(compID, []MatchResult{{
			ID: matchID, SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status: MatchStatusCompleted, Court: "A",
		}}); err != nil {
			return err
		}
		if err := tx.AppendMatchHistory(compID, MatchHistoryEntry{MatchID: matchID, Door: "score", Stamp: 1000}); err != nil {
			return err
		}
		return os.Chmod(historyDir, 0o500)
	})
	t.Cleanup(func() { _ = os.Chmod(historyDir, 0o700) })
	require.ErrorIs(t, txErr, ErrTxCommitted)
	_, err := os.Stat(s.matchHistoryPath(compID, matchID))
	require.True(t, os.IsNotExist(err), "the history file must not have landed yet")
	require.Len(t, liveWALs(t, s), 1, "the committed log is kept")
	return historyDir
}

func TestStore_TheNextWriteFinishesAKeptChangeFirst(t *testing.T) {
	skipUnlessChmodEnforced(t)
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID, matchID = "kept-settle", "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Kept Settle"}))
	historyDir := keptMatchAndHistory(t, store, compID, matchID)
	require.NoError(t, os.Chmod(historyDir, 0o700), "the disk accepts writes again")

	// A later write to the same file the kept log names.
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{{
		ID: matchID, SideA: "Alice", SideB: "Bob", Winner: "Bob",
		Status: MatchStatusCompleted, Court: "A",
	}}))

	_, err = os.Stat(store.matchHistoryPath(compID, matchID))
	require.NoError(t, err, "the kept change's history file landed before the later write")
	assert.Empty(t, liveWALs(t, store), "the kept log is done")
	_, kept := store.keptWAL.Load(compID)
	assert.False(t, kept, "the record is cleared once the change landed")

	restarted, err := NewStore(store.folder)
	require.NoError(t, err)
	history, err := restarted.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	require.Len(t, history, 1)
	matches, err := restarted.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, "Bob", matches[0].Winner, "the later write is what stands")
	stale, err := filepath.Glob(filepath.Join(store.walDir, "*.stale"))
	require.NoError(t, err)
	assert.Empty(t, stale, "nothing is set aside at the restart")
}

func TestStore_AWriteIsRefusedWhileTheKeptChangeStillCannotBeWritten(t *testing.T) {
	skipUnlessChmodEnforced(t)
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID, matchID = "kept-refuse", "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Kept Refuse"}))
	historyDir := keptMatchAndHistory(t, store, compID, matchID)
	before, err := os.ReadFile(store.compPath(compID, "pool-matches.csv"))
	require.NoError(t, err)

	// The disk still refuses the history file.
	err = store.SavePoolMatches(compID, []MatchResult{{
		ID: matchID, SideA: "Alice", SideB: "Bob", Winner: "Bob",
		Status: MatchStatusCompleted, Court: "A",
	}})
	require.ErrorIs(t, err, ErrKeptChangeUnsettled)
	assert.NotErrorIs(t, err, ErrPendingWALUnsettled, "a write is not answered as a backup refusal")
	err = store.WithTransaction(compID, func(StoreTx) error {
		t.Fatal("a transaction must not run over an unsettled kept change")
		return nil
	})
	require.ErrorIs(t, err, ErrKeptChangeUnsettled)
	after, err := os.ReadFile(store.compPath(compID, "pool-matches.csv"))
	require.NoError(t, err)
	assert.Equal(t, string(before), string(after), "the refused write wrote nothing")
	assert.Len(t, liveWALs(t, store), 1, "the kept log stays")
	_, kept := store.keptWAL.Load(compID)
	assert.True(t, kept, "the record stays for the next write")

	// A read goes on with what is on hand.
	_, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)

	// Once the disk accepts it, the next write lands both.
	require.NoError(t, os.Chmod(historyDir, 0o700))
	require.NoError(t, store.SavePoolMatches(compID, []MatchResult{{
		ID: matchID, SideA: "Alice", SideB: "Bob", Winner: "Bob",
		Status: MatchStatusCompleted, Court: "A",
	}}))
	_, err = os.Stat(store.matchHistoryPath(compID, matchID))
	require.NoError(t, err)
	assert.Empty(t, liveWALs(t, store))
}

func TestStore_AReadFinishesAKeptChange(t *testing.T) {
	skipUnlessChmodEnforced(t)
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID, matchID = "kept-read", "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Kept Read"}))
	historyDir := keptMatchAndHistory(t, store, compID, matchID)
	require.NoError(t, os.Chmod(historyDir, 0o700))

	history, err := store.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	require.Len(t, history, 1, "the read finished the kept change and sees it")
	assert.Empty(t, liveWALs(t, store))
}

// While the disk keeps refusing a kept change, a read does not try it again
// for keptSettleReadBackoff after a failed settle, so the viewer and display
// refetches of a live tournament do not queue behind one failing settle after
// another; a write always tries. The first failure of the run is logged at
// WARN, the reads after it are not.
func TestStore_AReadWaitsAfterAFailedSettleAndAWriteStillTries(t *testing.T) {
	skipUnlessChmodEnforced(t)
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	now := time.Unix(1_800_000_000, 0)
	store.clock = func() time.Time { return now }
	const compID, matchID = "kept-backoff", "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Kept Backoff"}))
	historyDir := keptMatchAndHistory(t, store, compID, matchID)

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	attempts := func() int32 {
		v, ok := store.keptWAL.Load(compID)
		require.True(t, ok, "the record stays while the change is not written")
		return v.(*keptChange).attempts.Load()
	}

	// Two quick reads: the first tries and fails, the second waits.
	_, err = store.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	_, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, int32(1), attempts(), "the second read does not try again so soon")
	assert.Equal(t, 1, strings.Count(logs.String(), "level=WARN"), "the first failure of the run is a warning, once")

	// A write still tries, and is refused.
	err = store.SavePoolMatches(compID, []MatchResult{{
		ID: matchID, SideA: "Alice", SideB: "Bob", Winner: "Bob",
		Status: MatchStatusCompleted, Court: "A",
	}})
	require.ErrorIs(t, err, ErrKeptChangeUnsettled)
	assert.Equal(t, int32(2), attempts(), "a write always tries")
	assert.Equal(t, 1, strings.Count(logs.String(), "level=WARN"), "later failures of the run are not warned again")

	// The disk accepts it again; a read inside the wait still does not try.
	require.NoError(t, os.Chmod(historyDir, 0o700))
	_, err = store.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	assert.Len(t, liveWALs(t, store), 1, "a read inside the wait leaves the kept log")

	// Past the wait, the next read finishes it.
	now = now.Add(keptSettleReadBackoff)
	history, err := store.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	assert.Len(t, history, 1)
	assert.Empty(t, liveWALs(t, store))
	_, kept := store.keptWAL.Load(compID)
	assert.False(t, kept)
}

// LoadSquads is a read: a v2.0.0 squads.yaml it cannot adopt because a kept
// change still cannot be written degrades to the empty read, as a failed
// adoption does, rather than failing GET team-members and the viewer.
func TestStore_LoadSquadsReadsOnWhileAKeptChangeCannotBeWritten(t *testing.T) {
	skipUnlessChmodEnforced(t)
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID, matchID = "kept-squads", "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Kept Squads", Kind: "team", TeamSize: 3}))
	keptMatchAndHistory(t, store, compID, matchID)
	require.NoError(t, os.WriteFile(store.compPath(compID, legacySquadsFilename), []byte("squads: {}\n"), 0o600))

	squads, err := store.LoadSquads(compID)
	require.NoError(t, err, "a read goes on with what is on hand")
	assert.Empty(t, squads)
}

// A log the startup replay could not apply is kept while the server runs, and
// is finished by the competition's next write like one a transaction kept.
func TestStore_ALogTheReplayCouldNotApplyIsRecordedForItsCompetition(t *testing.T) {
	// The record the replay's failure path makes, by log; the startup replay
	// itself is pinned by TestNewStore_ARecordIsMadeForALogTheReplayCannotApply.
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	w, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	w.Append(wal.FileIntent{Path: store.compPath("kept-replay", "pools.csv"), Data: []byte("x"), Mode: 0o600})
	store.noteKeptWALOf(w)
	_, kept := store.keptWAL.Load("kept-replay")
	assert.True(t, kept)

	other, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	other.Append(wal.FileIntent{Path: filepath.Join(store.folder, "tournament.md"), Data: []byte("x"), Mode: 0o600})
	store.noteKeptWALOf(other)
	n := 0
	store.keptWAL.Range(func(any, any) bool { n++; return true })
	assert.Equal(t, 1, n, "a log naming no competition records nothing")
}

// A replay that fails at startup: a log naming a file under a folder of the
// competition that does not exist passes the startup write check (which
// probes only the folders that are there) and fails its Apply with ENOENT.
// The replay records the competition, so its next write or read tries again.
func TestNewStore_ARecordIsMadeForALogTheReplayCannotApply(t *testing.T) {
	folder := t.TempDir()
	store, err := NewStore(folder)
	require.NoError(t, err)
	const compID = "replay-fails"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Replay Fails"}))
	w, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	w.Append(wal.FileIntent{Path: store.compPath(compID, "nope", "x"), Data: []byte("x"), Mode: 0o600})
	require.NoError(t, w.Commit())

	reopened, err := NewStore(folder)
	require.NoError(t, err, "a log whose Apply fails does not refuse startup")
	_, kept := reopened.keptWAL.Load(compID)
	assert.True(t, kept, "the replay recorded the competition its log names")
	assert.Len(t, liveWALs(t, reopened), 1, "the log stays for the next try")
}

// A log Scan cannot check (a file it names cannot be read) refuses the settle
// of its own competition only: another competition's kept change still lands
// and its write goes on. Startup still refuses on it, whichever competition it
// names.
func TestStore_ALogOfAnotherCompetitionThatCannotBeCheckedDoesNotRefuseAWrite(t *testing.T) {
	folder := t.TempDir()
	store, err := NewStore(folder)
	require.NoError(t, err)
	const compA, compB = "comp-a", "comp-b"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compA, Name: "Comp A"}))
	require.NoError(t, store.SaveCompetition(&Competition{ID: compB, Name: "Comp B"}))

	// B: a log whose file cannot be read (a folder stands where it was).
	bTarget := store.compPath(compB, "notes.txt")
	wb, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	wb.Append(wal.FileIntent{Path: bTarget, Data: []byte("b"), Mode: 0o600})
	require.NoError(t, wb.Commit())
	require.NoError(t, os.MkdirAll(bTarget, 0o700))

	// A: a change this process kept.
	aTarget := store.compPath(compA, "notes.txt")
	wa, err := wal.BeginTx(store.walDir, wal.NewWALID(), store.directWriteWAL, store.directRemoveWAL, store.directReadWAL)
	require.NoError(t, err)
	wa.Append(wal.FileIntent{Path: aTarget, Data: []byte("a landed"), Mode: 0o600})
	require.NoError(t, wa.Commit())
	store.noteKeptWAL(compA)

	require.NoError(t, store.SaveCompetition(&Competition{ID: compA, Name: "Comp A again"}), "B's log does not refuse A's write")
	got, err := os.ReadFile(aTarget)
	require.NoError(t, err)
	assert.Equal(t, "a landed", string(got), "A's kept change landed first")
	_, kept := store.keptWAL.Load(compA)
	assert.False(t, kept)

	// B's own kept change cannot be settled while its file cannot be read.
	store.noteKeptWAL(compB)
	err = store.SaveCompetition(&Competition{ID: compB, Name: "Comp B again"})
	require.ErrorIs(t, err, ErrKeptChangeUnsettled)
	assert.Contains(t, err.Error(), "could not be checked")

	_, err = NewStore(folder)
	require.ErrorIs(t, err, wal.ErrLogUnchecked, "startup refuses on a log it cannot check, whichever competition it names")
}

func TestStore_ARosterEditAfterAKeptRenameDoesNotUndoIt(t *testing.T) {
	skipUnlessChmodEnforced(t)
	store, err := NewStore(t.TempDir())
	require.NoError(t, err)
	const compID, matchID = "kept-roster", "Pool A-0"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Kept Roster"}))
	first, err := store.AddParticipant(compID, domain.Player{Name: "Old Name", Dojo: "Dojo A"}, false)
	require.NoError(t, err)
	historyDir := store.compPath(compID, matchHistoryDir)

	// The history entry is staged FIRST, so Apply fails before the roster
	// lands: participants.csv keeps the old name while the log holds the new.
	txErr := store.WithTransaction(compID, func(tx StoreTx) error {
		if err := tx.AppendMatchHistory(compID, MatchHistoryEntry{MatchID: matchID, Door: "score", Stamp: 1000}); err != nil {
			return err
		}
		if err := os.Chmod(historyDir, 0o500); err != nil {
			return err
		}
		_, err := tx.UpdateParticipant(compID, first.ID, false, func(p *domain.Player) error {
			p.Name = "New Name"
			return nil
		})
		return err
	})
	require.NoError(t, os.Chmod(historyDir, 0o700))
	require.ErrorIs(t, txErr, ErrTxCommitted)

	_, err = store.AddParticipant(compID, domain.Player{Name: "Second One", Dojo: "Dojo B"}, false)
	require.NoError(t, err)

	names := func(s *Store) []string {
		players, lerr := s.LoadParticipants(compID, false)
		require.NoError(t, lerr)
		out := make([]string, 0, len(players))
		for _, p := range players {
			out = append(out, p.Name)
		}
		return out
	}
	assert.ElementsMatch(t, []string{"New Name", "Second One"}, names(store), "the edit is made on top of the kept rename")
	assert.Empty(t, liveWALs(t, store))

	restarted, err := NewStore(store.folder)
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"New Name", "Second One"}, names(restarted))
}
