// Package state — atomic_write.go provides atomic, durable file writes
// for all on-disk persistence in internal/state.
//
// Constitution Principle VII requires that "tournament and match state
// MUST be persisted to disk before the server responds with success."
// A direct os.WriteFile call is neither atomic (a crash mid-write
// leaves a half-written file that won't parse on restart) nor durable
// (page-cache buffered writes return success but can be lost on power
// loss). This helper closes both gaps for every save in the package.
//
// Algorithm: write to "<path>.tmp-<pid>-<nanos>" next to the target,
// fsync that file, close it, rename(tmp, target) — atomic on POSIX
// when src and dst are on the same filesystem — then fsync the parent
// directory so the rename metadata is durable across power loss (a
// no-op on Windows where directory fsync isn't supported).
//
// Why a unique-suffix .tmp filename? Per-comp mutex serializes writes
// to the same target in practice, but an explicit PID + nanosecond
// suffix is defensive: two processes pointing at the same data folder
// (e.g. a stuck-old-process / new-process restart overlap) would
// otherwise collide on a fixed ".tmp" sibling. The unique suffix also
// avoids ambiguity if the existing cache layer (getFileCache,
// pools.go:344) ever grew an mtime poll on directory listings — the
// .tmp file is invisible to the cache key because the cache is keyed
// by canonical filename, not by directory scan.
package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

// atomicWriteFile writes data to path atomically and durably.
//
// On success the file at path contains exactly the supplied bytes with
// the supplied mode bits. On failure the file at path is left unchanged
// (either still containing the previous content or still absent) and
// no .tmp orphan remains.
//
// Implementation steps:
//  1. Create a sibling temp file "<base>.tmp-<pid>-<nanos>" with perm.
//  2. Write data, then tmp.Sync() to flush the file contents.
//  3. Close the temp file.
//  4. Rename tmp -> path. This is atomic on POSIX when both paths live
//     on the same filesystem (they do — tmp is a sibling of path).
//  5. Open the parent directory and Sync() it so the rename metadata is
//     itself durable. On Windows this is a graceful no-op.
//
// Any failure between steps 1 and 4 removes the temp file before
// returning the error. A failure at step 5 returns the error but the
// rename has already happened — the file is visible at path with the
// right content; only the directory-entry durability across power loss
// is at risk, which is the same risk every fsync-less write already
// has.
// atomicWrite is the Store-bound write gate. It validates that path is
// within the store's data folder before delegating to atomicWriteFile,
// giving CodeQL a verifiable sanitisation boundary for the path value
// (which ultimately originates from HTTP-supplied competition IDs).
func (s *Store) atomicWrite(path string, data []byte, perm fs.FileMode) error {
	cleanPath := filepath.Clean(path)
	cleanBase := filepath.Clean(s.folder) + string(filepath.Separator)
	if !strings.HasPrefix(cleanPath, cleanBase) {
		return fmt.Errorf("write to %q is outside data directory %q", cleanPath, s.folder)
	}
	return atomicWriteFile(cleanPath, data, perm)
}

func atomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	dir, base := filepath.Split(path)
	if dir == "" {
		dir = "."
	}

	// Unique suffix: pid + monotonic-ish nanos. The rename clears it
	// immediately, so a collision is extremely unlikely even without
	// the suffix — but the suffix is cheap insurance against stuck-old
	// + new-process overlap on shared TOURNAMENT_DATA_DIR mounts.
	tmpName := fmt.Sprintf("%s.tmp-%d-%d", base, os.Getpid(), time.Now().UnixNano())
	tmpPath := filepath.Join(dir, tmpName)

	// O_CREATE|O_WRONLY|O_TRUNC with explicit perm. O_EXCL is omitted
	// because the unique suffix already guarantees absence; using O_EXCL
	// would only add a race window between the time.Now() and the open
	// for nothing.
	tmp, err := os.OpenFile(tmpPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm) // #nosec G304 — tmpPath is constructed from a caller-supplied target plus a deterministic suffix.
	if err != nil {
		return err
	}

	// Cleanup helper: removes tmpPath on any error path so we never
	// leave .tmp orphans. Best-effort: a removal failure is ignored
	// because the original error is more interesting.
	cleanup := func() {
		_ = os.Remove(tmpPath)
	}

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}

	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		cleanup()
		return err
	}

	// Sync the parent directory so the rename's metadata update lands
	// in the on-disk journal/log. Without this, a power loss right
	// after rename() returns can revert the rename even though the
	// data file itself was fsync'd. On Windows this returns
	// "operation not supported" — swallow that specific error.
	if err := syncDir(dir); err != nil {
		return err
	}

	return nil
}

// writeFn is the signature shared by atomicWriteFile (the default,
// direct-to-disk writer) and the WAL-capturing variant returned by
// wal.WAL.WriteFn. Savers reachable from StoreTx accept a writeFn
// parameter so the same code path serves both:
//
//   - Non-transactional callers pass directWrite, which forwards to
//     atomicWriteFile and the saver behaves exactly as before.
//   - Transactional callers (storeTx methods) pass the WAL's WriteFn,
//     which captures the bytes into the transaction's intent log
//     instead of touching the target. After fn returns nil, the
//     enclosing WithTransaction commits the WAL and Applies it.
//
// The signature matches wal.WriteFn so the WAL package and the state
// package can interop without a cross-package adapter.
type writeFn func(path string, data []byte, perm fs.FileMode) error

// directWrite is the default writeFn for non-transactional savers.
// It validates that path is within the store's data folder (giving
// CodeQL a verifiable sanitisation boundary) before delegating to
// atomicWriteFile. Callers that currently pass the bare function name
// as a writeFn argument must use `s.directWrite` (a method value) so
// the Store receiver is captured.
func (s *Store) directWrite(path string, data []byte, perm fs.FileMode) error {
	return s.atomicWrite(path, data, perm)
}

// syncDir opens dirPath and calls Sync() on the resulting directory
// handle. On Linux/macOS this forces the directory-entry update
// (including the rename we just performed) to be durable across power
// loss. On Windows opening a directory for sync isn't supported by the
// runtime; the error returned in that case is swallowed so callers
// don't have to special-case the platform.
//
// Any other error (e.g. permission denied on POSIX) is returned to
// the caller — those represent real problems we shouldn't silently
// ignore.
func syncDir(dirPath string) error {
	// On Windows, opening a directory for writing isn't a thing, and
	// even reading it doesn't give a syncable handle. Skip outright.
	if runtime.GOOS == "windows" {
		return nil
	}

	d, err := os.Open(dirPath) // #nosec G304 — dirPath is derived from the caller's target path.
	if err != nil {
		return err
	}
	defer func() {
		_ = d.Close()
	}()

	if err := d.Sync(); err != nil {
		// Some filesystems (notably tmpfs on certain kernels, and FUSE
		// mounts) return ENOTSUP / EINVAL for directory fsync. Treat
		// those as "best-effort done": the rename already happened,
		// the data file was fsync'd, and we can't do better on a
		// filesystem that refuses dir-sync. Other errors propagate.
		if errors.Is(err, syscall.ENOTSUP) || errors.Is(err, syscall.EINVAL) {
			return nil
		}
		return err
	}
	return nil
}
