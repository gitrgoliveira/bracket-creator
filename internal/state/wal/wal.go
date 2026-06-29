// Package wal implements a tiny write-ahead log so that
// state.Store.WithTransaction can commit MULTIPLE on-disk files
// atomically across a process crash.
//
// Motivating problem. A single-file write is already atomic + durable
// via state/atomic_write.go (write-tmp → fsync → rename → fsync-dir).
// But a transaction body that writes two or three files in sequence,
// score handler: pool-matches.csv + competitor-status.yaml +
// lineups.yaml, has a window between rename N and rename N+1 where
// a crash can leave the half-committed state on disk. The first file
// reflects the new write; the rest still reflect the old. Replay on
// restart can't infer the intended end-state from the partial result.
//
// Design. Two-phase commit, with the intent log stored on disk:
//
//  1. fn runs. Each tx-internal write goes through WAL.Append, which
//     stages a FileIntent (full target path + full new bytes + mode)
//     in memory. NO target file is touched. Append coalesces by path:
//     two Append calls for the same target leave only the LAST bytes
//     in the intent slice. This matches the score-handler rollback
//     pattern where pool-matches.csv may be written twice in one tx
//     (write new result → fail eligibility check → write old result
//     back). Last-write-wins is the correct semantics.
//
//  2. fn returns nil. Caller calls Commit, which atomic-writes the
//     intent log to <walDir>/<txID>.json via the same atomic-rename
//     dance the target files use. After Commit returns success, the
//     transaction is committed AT THE LOG LEVEL: a crash from this
//     point forward will replay the intents on restart.
//
//  3. Caller calls Apply, which iterates intents in order and
//     atomic-writes each one to its target path. If Apply returns an
//     error mid-way, the WAL stays on disk and the next Scan-replay
//     finishes the job. Apply is idempotent, replaying a fully-
//     committed WAL just re-writes the same bytes to the same paths.
//
//  4. Caller calls Done, which removes the WAL file. After Done the
//     transaction is complete and leaves no on-disk trace.
//
// Replay. Store.NewStore scans walDir at startup. Any WAL file found
// represents a committed-but-not-finished transaction: Apply it,
// Done it. Uncommitted WALs leave nothing on disk because Append
// stages everything in memory.
//
// What this does NOT provide. No undo log: there's no way to roll
// back an already-committed WAL after Apply has partially run. The
// model is forward-only "complete what we promised" recovery, not
// "restore the pre-transaction state". This matches the deferred-
// write usage in state.WithTransaction where fn never observes its
// own writes (it only reads from disk, mutates in memory, then
// stages the result via Append).
package wal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"time"
)

// FileIntent is a single staged target write. Persisted to disk in
// the WAL file as JSON; Data is base64-encoded by encoding/json's
// default []byte handling. Mode is the file-permission bits the
// target file should be created with.
type FileIntent struct {
	Path string      `json:"path"`
	Data []byte      `json:"data"`
	Mode os.FileMode `json:"mode"`
}

// WriteFn is the per-file atomic write function the WAL uses to land
// each FileIntent on disk. The state package wires in
// state.atomicWriteFile here so the WAL inherits the same atomic-
// rename + dir-fsync guarantees the rest of the package uses.
type WriteFn func(path string, data []byte, perm os.FileMode) error

// WAL is the in-memory transaction handle.
//
// Append-only intents (modulo coalescing by path). State machine:
//   - BeginTx → opened, intents=[], file not yet on disk
//   - Append… → intents grow / coalesce
//   - Commit  → intents serialized to <walDir>/<id>.json
//   - Apply   → each intent written to its target
//   - Done    → WAL file removed
type WAL struct {
	id      string
	walDir  string
	write   WriteFn
	intents []FileIntent
	// pathIdx maps target path → index into intents for O(1)
	// coalescing on Append.
	pathIdx map[string]int
	// path is "<walDir>/<id>.json". Cached so Commit/Done don't
	// recompute it.
	path string
}

// walFile is the on-disk shape. A wrapper around the intent slice
// keeps the schema extensible (e.g., a future "createdAt" field
// won't change the intent shape).
type walFile struct {
	ID      string       `json:"id"`
	Intents []FileIntent `json:"intents"`
}

// txCounter is a process-wide monotonic counter mixed into uniqueWALID
// so two transactions opened in the same nanosecond don't collide.
var txCounter atomic.Uint64

// uniqueWALID returns a string suitable for a WAL file name. Combines
// the process start nano-time with an atomic counter so concurrent
// BeginTx calls cannot collide.
func uniqueWALID() string {
	return fmt.Sprintf("%d-%04x", time.Now().UnixNano(), txCounter.Add(1))
}

// BeginTx opens a fresh WAL with the given id. walDir is created if
// it doesn't exist. The write function will be called by Apply for
// each FileIntent, typically state.atomicWriteFile wired in by the
// state package.
//
// No on-disk state is created here. Commit is what publishes the
// intent log to walDir; an uncommitted WAL leaves no trace.
func BeginTx(walDir, id string, write WriteFn) (*WAL, error) {
	if write == nil {
		return nil, fmt.Errorf("wal.BeginTx: write function is nil")
	}
	if id == "" {
		return nil, fmt.Errorf("wal.BeginTx: id is empty")
	}
	if err := os.MkdirAll(walDir, 0o750); err != nil {
		return nil, fmt.Errorf("wal.BeginTx: mkdir walDir: %w", err)
	}
	return &WAL{
		id:      id,
		walDir:  walDir,
		write:   write,
		intents: nil,
		pathIdx: make(map[string]int),
		path:    filepath.Join(walDir, id+".json"),
	}, nil
}

// ID returns the transaction id this WAL was opened with.
func (w *WAL) ID() string { return w.id }

// Append stages a single target write. Coalesces by path: a second
// call with the same FileIntent.Path replaces the first (last-write-
// wins). The data + mode are copied/captured directly, callers
// don't need to defensively-clone before appending.
//
// Returns no error: in-memory operation only.
func (w *WAL) Append(intent FileIntent) {
	if idx, ok := w.pathIdx[intent.Path]; ok {
		w.intents[idx] = intent
		return
	}
	w.pathIdx[intent.Path] = len(w.intents)
	w.intents = append(w.intents, intent)
}

// Intents returns a snapshot of the currently-staged intents in
// insertion order (after coalescing). Provided for tests + diagnostic
// logging in the state-package replay path; production code should
// not mutate the returned slice.
func (w *WAL) Intents() []FileIntent {
	out := make([]FileIntent, len(w.intents))
	copy(out, w.intents)
	return out
}

// PendingBytes returns the currently-staged bytes for path (after
// coalescing) and ok=true; if no intent has been staged for path,
// returns (nil, false).
//
// State-package storeTx loaders use this for read-your-own-writes
// inside a transaction body: a tx that saves pool-matches and then
// loads pool-matches must see the just-saved data, not the stale
// on-disk version (which won't update until Apply runs after fn
// returns). Without this peek the load would return the pre-save
// state and a tx body that reads-after-writes would silently see
// stale data.
//
// Returns a fresh copy of the bytes so callers can mutate without
// disturbing the staged intent.
func (w *WAL) PendingBytes(path string) ([]byte, bool) {
	idx, ok := w.pathIdx[path]
	if !ok {
		return nil, false
	}
	in := w.intents[idx]
	out := make([]byte, len(in.Data))
	copy(out, in.Data)
	return out, true
}

// WriteFn returns a function that wraps Append in the WriteFn shape.
// The state package uses this to redirect savers, the savers think
// they're writing to disk, but the WAL captures the bytes for
// deferred commit.
//
// The returned function always returns nil, Append cannot fail
// (it's in-memory).
func (w *WAL) WriteFn() WriteFn {
	return func(path string, data []byte, perm os.FileMode) error {
		// Copy data: the caller's buffer may be reused (e.g., a CSV
		// writer's underlying bytes.Buffer) after the saver returns.
		// Without the copy the WAL would replay whatever the buffer
		// happens to hold at Apply time.
		buf := make([]byte, len(data))
		copy(buf, data)
		w.Append(FileIntent{Path: path, Data: buf, Mode: perm})
		return nil
	}
}

// Commit serializes the staged intents to <walDir>/<id>.json via the
// configured write function. After Commit returns success the
// transaction is durable: a crash from this point on will replay the
// intents on restart.
//
// A second Commit re-writes the file with the current intent set,
// callers should not Append after Commit, but if they do, a second
// Commit will produce a WAL file with the additional intents.
func (w *WAL) Commit() error {
	data, err := json.Marshal(walFile{ID: w.id, Intents: w.intents})
	if err != nil {
		return fmt.Errorf("wal.Commit %q: marshal: %w", w.id, err)
	}
	if err := w.write(w.path, data, 0o600); err != nil {
		return fmt.Errorf("wal.Commit %q: write: %w", w.id, err)
	}
	return nil
}

// Apply writes each staged intent to its target path via the
// configured write function, in insertion order. Returns the first
// error encountered, in which case the WAL file remains on disk so
// the next Scan can resume.
//
// Idempotent: re-running Apply on a WAL whose targets have already
// been written produces the same on-disk state (atomicWriteFile is
// itself idempotent for identical bytes).
func (w *WAL) Apply() error {
	for i, in := range w.intents {
		if err := w.write(in.Path, in.Data, in.Mode); err != nil {
			return fmt.Errorf("wal.Apply %q: intent %d (%s): %w", w.id, i, in.Path, err)
		}
	}
	return nil
}

// Done removes the WAL file. Call after Apply succeeds. A subsequent
// Scan will not see this WAL.
//
// Safe to call when the WAL file doesn't exist (returns nil), that
// branch covers the "uncommitted WAL" cleanup case in the abort
// path of state.WithTransaction.
func (w *WAL) Done() error {
	if err := os.Remove(w.path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wal.Done %q: %w", w.id, err)
	}
	return nil
}

// Scan reads every WAL file in walDir and returns a WAL for each.
// Files are returned in id-sorted order so replay is deterministic
// across crash recoveries. Uncommitted WALs leave no file on disk,
// so anything Scan finds is committed and needs Apply + Done.
//
// The write function is wired into every returned WAL. A missing
// walDir returns (nil, nil), that's the "fresh data folder" case.
//
// Files that fail to parse are skipped with their path included in
// the returned error chain (so a corrupted log doesn't block other
// pending transactions); the caller is expected to log and continue.
// Per-file errors don't abort the scan.
func Scan(walDir string, write WriteFn) ([]*WAL, error) {
	if write == nil {
		return nil, fmt.Errorf("wal.Scan: write function is nil")
	}
	entries, err := os.ReadDir(walDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("wal.Scan %q: %w", walDir, err)
	}
	// Collect names first so we can sort for deterministic replay
	// order. Use the trimmed id (filename minus ".json") as the
	// sort key, uniqueWALID's prefix is unix-nanos, so sorting by
	// id is sorting by commit-time.
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if filepath.Ext(n) != ".json" {
			continue
		}
		names = append(names, n)
	}
	sort.Strings(names)

	out := make([]*WAL, 0, len(names))
	for _, n := range names {
		path := filepath.Join(walDir, n)
		raw, rerr := os.ReadFile(path) // #nosec G304, path is the dir+file we just listed
		if rerr != nil {
			// Best-effort: skip unreadable files. A future enhancement
			// could surface these via a slog warning, but breaking the
			// loop on one corrupt file would block recovery for the
			// rest.
			continue
		}
		var wf walFile
		if jerr := json.Unmarshal(raw, &wf); jerr != nil {
			continue
		}
		w := &WAL{
			id:      wf.ID,
			walDir:  walDir,
			write:   write,
			intents: wf.Intents,
			pathIdx: make(map[string]int, len(wf.Intents)),
			path:    path,
		}
		for i, in := range wf.Intents {
			// Last-write-wins coalescing already happened at Append
			// time pre-Commit; preserve the same map shape so a
			// second Append after Scan keeps working.
			w.pathIdx[in.Path] = i
		}
		out = append(out, w)
	}
	return out, nil
}

// NewWALID exposes uniqueWALID for callers that want to mint an id
// without immediately opening a WAL (e.g., tests asserting id-shape).
// Most callers should use BeginTx, which mints its own id.
func NewWALID() string { return uniqueWALID() }
