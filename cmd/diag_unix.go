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
	uid := os.Geteuid()
	gid := os.Getegid()

	hint := fmt.Sprintf("Hint: process running as uid=%d gid=%d\n", uid, gid)

	// Prefer statting the folder itself — it often exists as the bind-mount
	// root even when a sub-directory (competitions/, .wal/) can't be created.
	// Fall back to the parent when the folder itself doesn't exist yet.
	target := folder
	info, err := os.Stat(folder)
	if err != nil {
		parent := filepath.Dir(folder)
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
