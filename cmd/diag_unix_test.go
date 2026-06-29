//go:build !windows

package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestDiagnoseFolderError_ExistingFolder(t *testing.T) {
	dir := t.TempDir()
	result := diagnoseFolderError(dir)

	// Must include process UID in the Hint: header
	if !strings.Contains(result, fmt.Sprintf("uid=%d", os.Geteuid())) {
		t.Errorf("expected effective uid=%d in output, got:\n%s", os.Geteuid(), result)
	}
	// Must include mode and owner info for an existing dir
	if !strings.Contains(result, "exists, mode=") {
		t.Errorf("expected mode info in output, got:\n%s", result)
	}
}

func TestDiagnoseFolderError_NonexistentFolderFallsBackToParent(t *testing.T) {
	dir := t.TempDir()
	// Pass a path whose direct stat will fail; diagnoseFolderError
	// must fall back to statting the parent (dir).
	target := filepath.Join(dir, "does-not-exist")
	result := diagnoseFolderError(target)

	// Process UID must still be present
	if !strings.Contains(result, fmt.Sprintf("uid=%d", os.Geteuid())) {
		t.Errorf("expected effective uid=%d in output, got:\n%s", os.Geteuid(), result)
	}
	// The parent dir path should appear in the output
	if !strings.Contains(result, dir) {
		t.Errorf("expected parent dir %s in output, got:\n%s", dir, result)
	}
}

func TestDiagnoseFolderError_OwnerInfo(t *testing.T) {
	dir := t.TempDir()
	result := diagnoseFolderError(dir)

	// The diagnostic prints the directory's ACTUAL owner (stat.Uid:stat.Gid),
	// not the process's uid/gid. Those usually match for t.TempDir(), but the
	// group can differ: on macOS a temp dir created under a setgid parent
	// inherits the parent's gid (often 0/wheel) rather than the process's
	// effective gid (e.g. 20/staff). So derive the expected owner from the
	// same stat the implementation reads, rather than assuming Getegid().
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat temp dir: %v", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Skipf("Sys() is not *syscall.Stat_t on this platform (%T)", info.Sys())
	}
	expected := fmt.Sprintf("owner=%d:%d", stat.Uid, stat.Gid)
	if !strings.Contains(result, expected) {
		t.Errorf("expected %q in output, got:\n%s", expected, result)
	}
}

func TestDiagnoseFolderError_MismatchArrowWhenUIDDiffers(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root, UID mismatch against root-owned dir not testable")
	}
	// "/" is always present on any Unix system and owned by root (uid=0).
	// Inject uid=999 (≠ 0) to trigger the mismatch arrow without needing Docker
	// or elevated privileges.
	result := diagnoseFolderErrorForProcess("/", 999, 999)

	if !strings.Contains(result, "→") {
		t.Errorf("expected UID mismatch arrow when uid=999 != owner(0), got:\n%s", result)
	}
	if !strings.Contains(result, "uid 999") {
		t.Errorf("expected injected uid 999 in mismatch message, got:\n%s", result)
	}
}

func TestDiagnoseFolderError_NoArrowWhenUIDMatches(t *testing.T) {
	dir := t.TempDir()
	// Inject the actual process uid/gid, they match the t.TempDir() owner.
	result := diagnoseFolderErrorForProcess(dir, os.Geteuid(), os.Getegid())

	if strings.Contains(result, "→") {
		t.Errorf("expected no mismatch arrow when uid matches dir owner, got:\n%s", result)
	}
}

// TestDiagnoseFolderError_TrailingSeparatorFallsBackToTrueParent guards against
// the filepath.Dir quirk where Dir("/x/") returns "/x" rather than "/", the
// fallback must Clean before computing the parent, otherwise it would stat the
// folder itself again.
func TestDiagnoseFolderError_TrailingSeparatorFallsBackToTrueParent(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist")
	// Append trailing slash to the missing path; the parent must still be `dir`.
	result := diagnoseFolderError(missing + string(filepath.Separator))

	if !strings.Contains(result, dir) {
		t.Errorf("expected true parent %s in output (not the path itself), got:\n%s", dir, result)
	}
	if !strings.Contains(result, "exists, mode=") {
		t.Errorf("expected parent's mode info in output, got:\n%s", result)
	}
}

func TestDiagnoseFolderError_TargetAndParentMissing(t *testing.T) {
	// Construct a guaranteed-missing nested path under t.TempDir() so that
	// both the target folder and its immediate parent are absent regardless
	// of the environment. diagnoseFolderError must not panic.
	result := diagnoseFolderError(filepath.Join(t.TempDir(), "a", "b"))

	if !strings.Contains(result, "uid=") {
		t.Errorf("expected uid= in output even when both paths missing, got:\n%s", result)
	}
	if !strings.Contains(result, "could not stat") {
		t.Errorf("expected 'could not stat' message, got:\n%s", result)
	}
}

// TestDiagnoseFolderError_StatErrorReportedDirectly covers the non-IsNotExist
// branch: when os.Stat returns EACCES (folder exists but is unreadable due to
// a parent dir we can't traverse), the diagnostic must report the failure on
// the original folder rather than silently falling back to the parent, the
// parent's ownership would be misleading in this case.
func TestDiagnoseFolderError_StatErrorReportedDirectly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses POSIX permission checks; EACCES branch not reachable")
	}
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatalf("mkdir child: %v", err)
	}
	// Strip search permission on parent. stat(parent/child) now returns EACCES
	// (not IsNotExist). Restore in cleanup so t.TempDir() can remove the tree.
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatalf("chmod parent 000: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	result := diagnoseFolderError(child)

	if !strings.Contains(result, "could not stat") {
		t.Errorf("expected 'could not stat' for EACCES, got:\n%s", result)
	}
	// The hint must reference the target itself, not its parent, falling back
	// to the parent would tell the operator about a different directory's owner.
	if !strings.Contains(result, child) {
		t.Errorf("expected target %s in output, got:\n%s", child, result)
	}
}
