package state

import (
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state/wal"
)

// ErrKeptChangeUnsettled marks a write refused because an earlier change to
// the same competition is still kept in the transaction log and the disk
// refused it again. The new write was not made, nothing of it is kept, and
// the earlier change is tried again before the next write (and by a read,
// keptSettleReadBackoff after the last failed try). It is the same
// disk fault as ErrTxCommitted and is answered the same way (a 500 whose log
// line names the disk), so the device keeps retrying.
var ErrKeptChangeUnsettled = errors.New("an earlier change kept in the transaction log could not be written; this write was not made")

// keptSettleReadBackoff is how long a read waits, after a settle of a kept
// change failed, before it tries again. While the disk keeps refusing, every
// try takes the competition's write lock for a scan of .wal and a retried
// Apply, and every viewer, display and SSE refetch would queue behind it; a
// write is not held to it (it must not write over the kept change anyway).
const keptSettleReadBackoff = 5 * time.Second

// keptChange is the record keptWAL holds for a competition whose log this
// process kept. Its fields are read without the competition's lock (the read
// path asks before it takes one), so they are atomic.
type keptChange struct {
	// failedAt is the store clock's time of the last settle that failed; nil
	// while none has. Kept as a time.Time, never as Unix nanoseconds, so the
	// store's own clock (time.Now) keeps its monotonic reading and the wait
	// is measured on it: a wall clock stepped back cannot stretch it.
	failedAt atomic.Pointer[time.Time]
	// warned is set by the first failed settle of the run, which logs a
	// warning; the run ends when the change lands and the record goes.
	warned atomic.Bool
	// attempts counts the settles tried, for the test that pins the read
	// path's wait.
	attempts atomic.Int32
}

// noteKeptWAL records that compID holds a transaction log this process kept
// after a commit it could not finish. Called by WithTransaction under the
// competition's write lock.
func (s *Store) noteKeptWAL(compID string) {
	// LoadOrStore: noting it again never resets a run of failed settles.
	s.keptWAL.LoadOrStore(compID, &keptChange{})
}

// noteKeptWALOf records the competition a log names, for a log the startup
// replay could not finish (its Apply or Done failed) and keeps while the
// process goes on: the competition's next write or read finishes it, exactly
// as for a log a transaction of this process kept.
func (s *Store) noteKeptWALOf(w *wal.WAL) {
	if id := s.walCompetition(w); id != "" {
		s.noteKeptWAL(id)
	}
}

// walCompetition names the competition the first step of w under
// competitions/ belongs to, "" when no step is under it.
func (s *Store) walCompetition(w *wal.WAL) string {
	compsDir := filepath.Clean(filepath.Join(s.folder, "competitions")) + string(filepath.Separator)
	for _, in := range w.Intents() {
		rest, ok := strings.CutPrefix(storePath(in.Path), compsDir)
		if !ok {
			continue
		}
		if id, _, _ := strings.Cut(rest, string(filepath.Separator)); id != "" {
			return id
		}
	}
	return ""
}

// lockCompWrite takes compID's write lock and, when this process kept a
// transaction log for compID, finishes that change before returning, so the
// caller reads and writes on top of it rather than over it (a write over one
// of its files would leave the log stale, and the next startup would set the
// whole change aside). Every writer of a competition's files takes its lock
// here. With no kept log it costs one map lookup. When the kept change still
// cannot be written, the lock is released and the error wraps
// ErrKeptChangeUnsettled: the caller must not write.
func (s *Store) lockCompWrite(compID string) (*sync.RWMutex, error) {
	// lockComp re-checks the mutex it waited on is still compID's, so a writer
	// that waited through DeleteCompetition never holds an orphaned one.
	mu := s.lockComp(compID)
	if err := s.settleKeptWALLocked(compID); err != nil {
		mu.Unlock()
		return nil, err
	}
	return mu, nil
}

// settleKeptWALLocked finishes the change kept for compID, if any. Caller
// holds compID's write lock. settleCompWALsLocked clears the record once
// every log for compID has landed.
func (s *Store) settleKeptWALLocked(compID string) error {
	v, kept := s.keptWAL.Load(compID)
	if !kept {
		return nil
	}
	k := v.(*keptChange)
	k.attempts.Add(1)
	if err := s.settleCompWALsLocked(compID); err != nil {
		// Every failure, a Scan of .wal included, is the same class: the
		// door that asked is refused (a write) or reads what is on hand (a
		// read), and the record stays for the next one. The first failure
		// of a run is a warning; the ones after it would repeat it on every
		// retry, so they are quiet.
		failedAt := s.now()
		k.failedAt.Store(&failedAt)
		logID := ""
		var pending *PendingWALError
		if errors.As(err, &pending) {
			logID = pending.Log
		}
		if k.warned.CompareAndSwap(false, true) {
			slog.Warn("state: a change kept in the transaction log still cannot be written; the competition's writes are refused until it lands, and its reads wait a few seconds between tries. If the folder the log names was removed outside the app, restore it, or set the named log aside by renaming it so it no longer ends in .json; the competition's next write then goes ahead, with no restart",
				"comp", compID, "wal", logID, "err", err)
		} else {
			slog.Debug("state: the kept change still cannot be written", "comp", compID, "wal", logID, "err", err)
		}
		return fmt.Errorf("state: competition %q: finishing the change kept in the transaction log: %v: %w", compID, err, ErrKeptChangeUnsettled)
	}
	if k.warned.Load() {
		slog.Info("state: the change kept in the transaction log has been written", "comp", compID)
	}
	return nil
}

// settleKeptWALBeforeRead finishes the change kept for compID before a read
// takes the competition's read lock, so the roster and the draw are read
// from the same landed files: between the failed write and its settle, the
// draw's caches hold the staged content while the roster is read from the
// file, which may not have landed. Called with NO competition lock held (an
// RWMutex cannot be upgraded). A settle that fails here lets the read go on
// with what is on hand (the write path refuses), and the reads after it do
// not try again until keptSettleReadBackoff has passed (keptSettleDue).
func (s *Store) settleKeptWALBeforeRead(compID string) {
	if !s.keptSettleDue(compID) {
		return
	}
	mu, err := s.lockCompWriteForRead(compID)
	if err != nil {
		// The read goes on with what is on hand; settleKeptWALLocked has
		// logged the failure (a warning once per run).
		slog.Debug("state: a read could not finish the kept change; reading what is on hand", "comp", compID, "err", err)
		return
	}
	mu.Unlock()
}

// lockCompWriteForRead is lockCompWrite for a read door (the read path's
// settle, EnsureLegacyUpgraded, LoadSquads's adoption). Each asks
// keptSettleDue before it takes the lock, and readers that all saw a settle
// due queue on it; so the wait is asked again once the lock is held, and a
// reader that finds the settle failed moments ago (by the reader ahead of it)
// does not try again: the lock is released and the error wraps
// ErrKeptChangeUnsettled, as for a settle that failed.
func (s *Store) lockCompWriteForRead(compID string) (*sync.RWMutex, error) {
	mu := s.lockComp(compID)
	if s.keptSettleWaits(compID) {
		mu.Unlock()
		return nil, fmt.Errorf("state: competition %q: the change kept in the transaction log failed to land moments ago; not tried again yet: %w", compID, ErrKeptChangeUnsettled)
	}
	if err := s.settleKeptWALLocked(compID); err != nil {
		mu.Unlock()
		return nil, err
	}
	return mu, nil
}

// keptSettleDue reports whether a read should try to finish compID's kept
// change now: there is one, and no settle failed in the last
// keptSettleReadBackoff. Read without any lock, which is why the record's
// fields are atomic. Only the read doors ask it (settleKeptWALBeforeRead,
// EnsureLegacyUpgraded); a write always tries (lockCompWrite), since it must
// not write over the kept change either way.
func (s *Store) keptSettleDue(compID string) bool {
	v, kept := s.keptWAL.Load(compID)
	if !kept {
		return false
	}
	failedAt := v.(*keptChange).failedAt.Load()
	return failedAt == nil || s.now().Sub(*failedAt) >= keptSettleReadBackoff
}

// keptSettleWaits reports whether compID has a kept change whose last settle
// failed within keptSettleReadBackoff: a read door that would take the write
// lock for it (EnsureLegacyUpgraded, LoadSquads's adoption) skips its step.
func (s *Store) keptSettleWaits(compID string) bool {
	if _, kept := s.keptWAL.Load(compID); !kept {
		return false
	}
	return !s.keptSettleDue(compID)
}
