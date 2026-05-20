//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// diagnoseFolderError returns an actionable Hint block when data-folder
// initialisation fails at startup. It reports the effective UID/GID of the
// running process alongside the ownership and mode bits of the folder (or its
// parent when the folder doesn't exist yet), so operators running in Docker
// with bind-mounted volumes can immediately identify a UID/GID mismatch
// without having to read source code.
//
// The typical failure chain is:
//
//	host bind mount /tournament-data owned by uid=1000
//	container USER directive → uid=999
//	→ os.MkdirAll("/tournament-data/competitions") → EACCES
//
// On POSIX, syscall.Stat_t carries Uid/Gid; on Windows this file is excluded
// by the build tag and the stub in diag_windows.go returns "".
func diagnoseFolderError(folder string) string {
	return diagnoseFolderErrorForProcess(folder, os.Geteuid(), os.Getegid())
}

// diagnoseFolderErrorForProcess is the testable core of diagnoseFolderError.
// uid and gid are the effective credentials of the running process; callers
// other than diagnoseFolderError should only use this in tests where
// injecting a synthetic uid/gid is needed to exercise the mismatch path
// without requiring root or a real foreign-owned directory.
func diagnoseFolderErrorForProcess(folder string, uid, gid int) string {
	hint := fmt.Sprintf("Hint: process running as uid=%d gid=%d\n", uid, gid)

	// Prefer statting the folder itself — it often exists as the bind-mount
	// root even when a sub-directory (competitions/, .wal/) can't be created.
	// Fall back to the parent when the folder itself doesn't exist yet.
	target := folder
	info, err := os.Stat(folder)
	if err != nil {
		if !os.IsNotExist(err) {
			// EACCES or other non-NotExist errors mean the folder exists but we
			// can't inspect it — report that directly rather than silently falling
			// back to the parent, which could have different ownership and mislead
			// the operator about what's wrong.
			hint += fmt.Sprintf("  %s: could not stat (%v)", folder, err)
			return hint
		}
		// Folder doesn't exist yet (common: data dir was never created on this
		// host). Stat the parent to show who owns the volume root. Clean first
		// so a trailing separator (e.g. "/tournament-data/") doesn't make
		// filepath.Dir return the folder itself instead of its parent.
		parent := filepath.Dir(filepath.Clean(folder))
		info, err = os.Stat(parent)
		if err != nil {
			hint += fmt.Sprintf("  %s: could not stat (%v)", parent, err)
			return hint
		}
		target = parent
	}

	mode := info.Mode().Perm()
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		hint += fmt.Sprintf("  %s exists, mode=%04o", target, mode)
		return hint
	}

	ownerUID := stat.Uid
	ownerGID := stat.Gid

	hint += fmt.Sprintf("  %s exists, mode=%04o, owner=%d:%d", target, mode, ownerUID, ownerGID)

	if uint32(uid) != ownerUID { // #nosec G115 — POSIX Geteuid() is always >= 0 and fits in uint32
		hint += fmt.Sprintf("\n  → %s is owned by uid %d but the container runs as uid %d", target, ownerUID, uid)
		hint += fmt.Sprintf("\n  → docker-compose: add `user: \"%d:%d\"` to the mobile-app service,", ownerUID, ownerGID)
		hint += "\n    OR chown the host folder to match the container's USER"
	}

	return hint
}
