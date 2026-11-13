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
//     A step can also be a DELETE (FileIntent.Delete, staged through
//     RemoveFn): Apply removes its path, and a path already absent is
//     success, so a replay of a log that partly applied stays idempotent.
//     Writes and deletes share the one ordered, path-coalesced list.
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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
	"syscall"
	"time"
)

// FileIntent is a single staged step: a target write, or (Delete) the
// removal of the target. Persisted to disk in the WAL file as JSON; Data is
// base64-encoded by encoding/json's default []byte handling. Mode is the
// file-permission bits the target file should be created with. A delete
// carries no Data and no Mode.
type FileIntent struct {
	Path string      `json:"path"`
	Data []byte      `json:"data"`
	Mode os.FileMode `json:"mode"`
	// Delete marks a staged removal of Path. omitempty leaves it out of a
	// write's encoding, as the previous release wrote one.
	Delete bool `json:"delete,omitempty"`
	// Before is what the target held when the log was committed
	// (fileDigest), so Scan can tell a log some other writer overtook
	// (staleStep). A build from before the field ignores it, as
	// encoding/json does any unknown key, and replays such a log as before.
	Before string `json:"before,omitempty"`
}

// absentDigest is fileDigest's answer for a target that does not exist.
const absentDigest = "absent"

// staleSuffix is added to the name of a log Scan sets aside (staleStep).
const staleSuffix = ".stale"

// invalidSuffix is added to the name of a log whose content does not parse,
// once, so Scan (which reads only names ending in .json) never meets it again.
const invalidSuffix = ".invalid"

// fileDigest names what path holds, read through read: absentDigest when
// there is no file, else the SHA-256 of its bytes.
func fileDigest(read ReadFn, path string) (string, error) {
	data, err := read(path)
	if errors.Is(err, fs.ErrNotExist) {
		return absentDigest, nil
	}
	if err != nil {
		return "", err
	}
	return bytesDigest(data), nil
}

func bytesDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// staleStep answers the path of the first step whose target holds neither
// what it held at commit (the step not yet applied) nor the step's result
// (applied): some other writer changed it after the log was made. That is a
// log kept after its Apply or Done failed while the process went on writing,
// or a version-2 log a build from before the delete step kept unapplied.
// Replaying it would put the older content back over newer. A step with no
// Before was written by a build from before the field and is not judged
// stale, but its file is read all the same: a read that fails (a path outside
// the data folder, a file that cannot be read) marks the log unchecked
// whatever version wrote it, so it is never replayed unchecked.
func staleStep(read ReadFn, steps []FileIntent) (string, error) {
	for _, in := range steps {
		now, err := fileDigest(read, in.Path)
		if err != nil {
			return "", err
		}
		if in.Before == "" {
			continue
		}
		result := absentDigest
		if !in.Delete {
			result = bytesDigest(in.Data)
		}
		if now != in.Before && now != result {
			return in.Path, nil
		}
	}
	return "", nil
}

// WriteFn is the per-file atomic write function the WAL uses to land
// each FileIntent on disk. The state package wires in
// state.atomicWriteFile here so the WAL inherits the same atomic-
// rename + dir-fsync guarantees the rest of the package uses.
type WriteFn func(path string, data []byte, perm os.FileMode) error

// RemoveFn removes one target for a delete step. It may report a path that is
// already absent as an error; Apply reads that as success (the step's end
// state already holds), which is what keeps a replay idempotent.
type RemoveFn func(path string) error

// ReadFn reads one target, for the record of what it held (fileDigest). It
// reports a file that is not there, or whose folder is not, as
// fs.ErrNotExist. The store passes its root-backed reader, so the WAL opens
// no target by path, as it writes and removes none by path.
type ReadFn func(path string) ([]byte, error)

// WAL is the in-memory transaction handle.
//
// Append-only intents (modulo coalescing by path). State machine:
//   - BeginTx → opened, intents=[], file not yet on disk
//   - Append… → intents grow / coalesce
//   - Commit  → intents serialized to <walDir>/<id>.json
//   - Apply   → each intent written to (or removed from) its target
//   - Done    → WAL file removed
type WAL struct {
	id     string
	walDir string
	write  WriteFn
	remove RemoveFn
	read   ReadFn
	// stale is set by Scan on a log another writer overtook: the first such
	// file (staleStep). Such a log is never applied (Stale, SetAside).
	stale string
	// invalid is set by Scan on a log whose content is not a log (it does
	// not parse). It holds no steps, is never applied, and is set aside under
	// invalidSuffix (Invalid, SetAside).
	invalid bool
	// unchecked is set by Scan on a log whose files it could not read, so it
	// cannot tell whether another writer overtook it; it wraps
	// ErrLogUnchecked and names the log. Such a log is never applied
	// (Unchecked).
	unchecked error
	intents   []FileIntent
	// pathIdx maps target path → index into intents for O(1)
	// coalescing on Append.
	pathIdx map[string]int
	// path is "<walDir>/<id>.json". Cached so Commit/Done don't
	// recompute it.
	path string
}

// logVersionSteps is the log format that can hold a delete step. A log with
// only writes keeps the previous release's format (no version, the steps
// under "intents"; the added "before" key is one that release ignores), so
// either release replays the other's.
//
// A log holding a delete is written as this version, with its steps under
// "steps" and, under "intents", ONE step that names no file
// (versionGuardIntent). A build from before the delete step reads only
// "intents": its writer refuses the empty path ("outside data directory"), so
// its Apply fails on that step before it writes any file, it logs the failure
// at ERROR and keeps the log for the next start. Had the delete been a field
// on an ordinary intent, that build would have written an EMPTY file over the
// path it names; had the steps been hidden from it, it would have applied the
// writes alone and removed the log, losing the delete for good.
const logVersionSteps = 2

// ErrUnknownLogVersion is returned by Scan for a log written in a format this
// build cannot read (a later release's). Replaying around it would apply the
// later logs without it, so the caller refuses rather than skipping.
var ErrUnknownLogVersion = errors.New("wal: transaction log of an unknown version")

// ErrLogUnchecked marks a log Scan could not check: the log file itself
// could not be read (Scan returns it), or a file it names could not be
// (staleStep; the log is returned marked, WAL.Unchecked). Replaying it might
// put older content back, and skipping it might lose it, so the caller
// refuses.
var ErrLogUnchecked = errors.New("wal: transaction log could not be checked")

// ErrCommitLeftLog is returned by Commit when writing the log failed after
// the log was already in place and removing it failed too: the transaction
// is then committed after all, and the next start replays it.
var ErrCommitLeftLog = errors.New("wal: a failed commit left its log in place")

// versionGuardIntent is what a log of logVersionSteps carries under "intents"
// for builds that read only that key: see logVersionSteps.
var versionGuardIntent = FileIntent{}

// walFile is the on-disk shape. A wrapper around the intent slice
// keeps the schema extensible (e.g., a future "createdAt" field
// won't change the intent shape).
type walFile struct {
	ID      string       `json:"id"`
	Version int          `json:"version,omitempty"`
	Intents []FileIntent `json:"intents"`
	// Steps holds the steps of a logVersionSteps log; nil otherwise.
	Steps []FileIntent `json:"steps,omitempty"`
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
func BeginTx(walDir, id string, write WriteFn, remove RemoveFn, read ReadFn) (*WAL, error) {
	if write == nil {
		return nil, fmt.Errorf("wal.BeginTx: write function is nil")
	}
	if remove == nil {
		return nil, fmt.Errorf("wal.BeginTx: remove function is nil")
	}
	if read == nil {
		return nil, fmt.Errorf("wal.BeginTx: read function is nil")
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
		remove:  remove,
		read:    read,
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
	if !ok || w.intents[idx].Delete {
		return nil, false
	}
	in := w.intents[idx]
	out := make([]byte, len(in.Data))
	copy(out, in.Data)
	return out, true
}

// PendingRemove reports whether the transaction's last staged step for path
// is a delete: a read inside the transaction must then see the file as
// absent, whatever the disk still holds. PendingBytes answers false for such a
// path, so a reader asks this first to tell "staged absent" from "not staged".
func (w *WAL) PendingRemove(path string) bool {
	idx, ok := w.pathIdx[path]
	return ok && w.intents[idx].Delete
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

// RemoveFn returns a RemoveFn that stages a delete of path in this WAL
// instead of removing it, the delete counterpart of WriteFn. The step lands
// in staging order with the writes, and replaces any write of the same path
// staged before it (a later write of the path replaces the delete in turn).
func (w *WAL) RemoveFn() RemoveFn {
	return func(path string) error {
		w.Append(FileIntent{Path: path, Delete: true})
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
	// Each step records what its target holds now, so Scan can tell a log
	// another writer overtook (staleStep). Recorded on a copy: the staged
	// intents stay as Append left them.
	steps := make([]FileIntent, len(w.intents))
	copy(steps, w.intents)
	hasDelete := false
	for i := range steps {
		before, err := fileDigest(w.read, steps[i].Path)
		if err != nil {
			return fmt.Errorf("wal.Commit %q: read %s: %w", w.id, steps[i].Path, err)
		}
		steps[i].Before = before
		hasDelete = hasDelete || steps[i].Delete
	}
	wf := walFile{ID: w.id, Intents: steps}
	if hasDelete {
		// The format older builds refuse (logVersionSteps).
		wf = walFile{ID: w.id, Version: logVersionSteps, Intents: []FileIntent{versionGuardIntent}, Steps: steps}
	}
	data, err := json.Marshal(wf)
	if err != nil {
		return fmt.Errorf("wal.Commit %q: marshal: %w", w.id, err)
	}
	if err := w.write(w.path, data, 0o600); err != nil {
		// The write can fail after the log is in place (the folder sync
		// after the rename). Removing it keeps a failed Commit an abort;
		// if that fails too, the log stands and the caller is told so. A log
		// folder that is missing or not a folder holds no log at all.
		if !w.logMayStand() {
			return fmt.Errorf("wal.Commit %q: write: %w", w.id, err)
		}
		if derr := w.Done(); derr != nil {
			return fmt.Errorf("wal.Commit %q: write: %w; removing the log: %w: %w", w.id, err, derr, ErrCommitLeftLog)
		}
		return fmt.Errorf("wal.Commit %q: write: %w", w.id, err)
	}
	return nil
}

// logMayStand reports whether the log file may be in place after a failed
// write: false only when it, or the log folder, is provably not there (a
// folder that is missing or is not a folder holds no log). Asked through the
// caller's read function, as every target is read, so the WAL makes no call
// by the log's own path; this is the failure path only, so reading the log
// back costs nothing that matters.
func (w *WAL) logMayStand() bool {
	_, err := w.read(w.path)
	return !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, syscall.ENOTDIR)
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
	if w.unchecked != nil {
		return w.unchecked
	}
	for i, in := range w.intents {
		if in.Delete {
			// Already absent is the step's end state: success, so a replay
			// after a crash between this step and Done is idempotent.
			if err := w.remove(in.Path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return fmt.Errorf("wal.Apply %q: intent %d (delete %s): %w", w.id+".json", i, in.Path, err)
			}
			continue
		}
		if err := w.write(in.Path, in.Data, in.Mode); err != nil {
			return fmt.Errorf("wal.Apply %q: intent %d (%s): %w", w.id+".json", i, in.Path, err)
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
func (w *WAL) Done() (err error) {
	// Removed through a root on walDir, as Scan reads: the OS keeps the
	// removal inside the folder.
	root, err := os.OpenRoot(w.walDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("wal.Done %q: %w", w.id, err)
	}
	defer func() {
		if cerr := root.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("wal.Done %q: close: %w", w.id, cerr)
		}
	}()
	if err := root.Remove(filepath.Base(w.path)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("wal.Done %q: %w", w.id, err)
	}
	return nil
}

// Stale answers the file that marks a log Scan found overtaken (staleStep),
// or "" for one it may replay. A stale log is never applied: replaying it
// would put older content back over what was written since.
func (w *WAL) Stale() string {
	return w.stale
}

// Unchecked returns, for a log Scan could not check (a file it names could
// not be read), the error naming it, which wraps ErrLogUnchecked; nil
// otherwise. Such a log is never applied: replaying it might put older
// content back, and skipping it might lose it, so the caller refuses until
// access is fixed.
func (w *WAL) Unchecked() error {
	return w.unchecked
}

// Invalid reports whether Scan found this log's content damaged: it does not
// parse, so it names no step and can never be applied. Startup sets it aside
// (SetAside renames it to end in .invalid) and goes on.
func (w *WAL) Invalid() bool {
	return w.invalid
}

// SetAside renames the log to "<name>.json.stale" (or "<name>.json.invalid"
// for a log whose content does not parse, Invalid), kept beside the logs for
// the operator; its name no longer ends in ".json", so later scans pass it by.
func (w *WAL) SetAside() (err error) {
	root, err := os.OpenRoot(w.walDir)
	if err != nil {
		return fmt.Errorf("wal.SetAside %q: %w", w.id, err)
	}
	defer func() {
		if cerr := root.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("wal.SetAside %q: close: %w", w.id, cerr)
		}
	}()
	name := filepath.Base(w.path)
	suffix := staleSuffix
	if w.invalid {
		suffix = invalidSuffix
	}
	if err := root.Rename(name, name+suffix); err != nil {
		return fmt.Errorf("wal.SetAside %q: %w", w.id, err)
	}
	return nil
}

// Scan reads every WAL file in walDir and returns a WAL for each.
// Files are returned in id-sorted order so replay is deterministic
// across crash recoveries. Uncommitted WALs leave no file on disk,
// so anything Scan finds is committed and needs Apply + Done.
//
// The write, remove and read functions are wired into every returned WAL
// (read also checks each log's files, staleStep). A
// missing walDir returns (nil, nil), that's the "fresh data folder" case.
//
// A log whose content does not parse is returned marked Invalid (no steps).
// A log file that cannot be read is NOT skipped: Scan returns
// ErrLogUnchecked naming it, since nothing tells which files it touches. A
// log that parses but names a version this build does not know is NOT
// skipped either: Scan returns ErrUnknownLogVersion naming the file, since
// replaying the other logs without it could apply a later transaction before
// an earlier one. A log whose files were changed after it was written
// (staleStep) is returned marked Stale, so a caller never replays it. A log
// whose files Scan cannot read to check it is returned marked Unchecked,
// with its steps, so a caller can tell which files it touches (startup
// refuses on any; a settle only on one touching its competition). Scan
// itself changes nothing.
func Scan(walDir string, write WriteFn, remove RemoveFn, read ReadFn) ([]*WAL, error) {
	if write == nil {
		return nil, fmt.Errorf("wal.Scan: write function is nil")
	}
	if remove == nil {
		return nil, fmt.Errorf("wal.Scan: remove function is nil")
	}
	if read == nil {
		return nil, fmt.Errorf("wal.Scan: read function is nil")
	}
	// The listed files are read through a root on walDir, so the OS keeps
	// each read inside it.
	root, err := os.OpenRoot(walDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("wal.Scan %q: %w", walDir, err)
	}
	defer func() {
		// Read-only: every file is already read, so a close failure loses
		// nothing the replay needs; it is logged rather than failing it.
		if cerr := root.Close(); cerr != nil {
			slog.Warn("wal: closing the transaction log folder failed", "dir", walDir, "err", cerr)
		}
	}()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
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
		raw, rerr := root.ReadFile(n)
		if rerr != nil {
			if errors.Is(rerr, fs.ErrNotExist) {
				// Removed between the listing and the read (a Done, or a
				// set-aside): nothing is pending under that name.
				continue
			}
			// An access or I/O fault: the log may hold a change that has not
			// landed, and skipping it would replay the logs after it without
			// it, so the whole scan refuses until access is fixed; the log
			// then replays with its stale guard intact.
			return nil, fmt.Errorf("wal.Scan %q: %s cannot be read: %w: %w", walDir, n, rerr, ErrLogUnchecked)
		}
		var wf walFile
		if jerr := json.Unmarshal(raw, &wf); jerr != nil {
			// Damaged content that no build can read: it names no step, so
			// nothing can be applied from it. Handed back marked Invalid for
			// startup to set aside once and say so.
			out = append(out, &WAL{
				id:      n[:len(n)-len(".json")],
				walDir:  walDir,
				write:   write,
				remove:  remove,
				read:    read,
				invalid: true,
				pathIdx: map[string]int{},
				path:    path,
			})
			continue
		}
		steps := wf.Intents
		switch wf.Version {
		case 0:
			// Writes only, under "intents".
		case logVersionSteps:
			steps = wf.Steps
		default:
			// A later release's log. Skipping it would replay the logs
			// after it without it, so the whole replay refuses.
			return nil, fmt.Errorf("wal.Scan %q: %s has version %d: %w", walDir, n, wf.Version, ErrUnknownLogVersion)
		}
		w := &WAL{
			id:      wf.ID,
			walDir:  walDir,
			write:   write,
			remove:  remove,
			read:    read,
			intents: steps,
			pathIdx: make(map[string]int, len(steps)),
			path:    path,
		}
		stale, serr := staleStep(read, steps)
		if serr != nil {
			// A file it names cannot be read: handed back marked, with its
			// steps, so the caller knows which files it touches. Startup
			// refuses on it; a settle refuses only for the competition it
			// touches.
			w.unchecked = fmt.Errorf("wal.Scan %q: %s: %w: %w", walDir, n, serr, ErrLogUnchecked)
		}
		w.stale = stale
		for i, in := range steps {
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
