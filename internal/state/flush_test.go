package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// The flush is skipped in test binaries on macOS only (operator decision
// 2026-11-18, round flush-1). A go build binary (testing.Testing false)
// flushes on every OS, so the shipped app keeps its power-loss safety, and
// Linux and Windows test runs keep the real flush.
func TestFlushSkipped_OnlyInMacTestBinaries(t *testing.T) {
	for _, c := range []struct {
		testBinary bool
		goos       string
		skipped    bool
	}{
		{testBinary: false, goos: "darwin", skipped: false},
		{testBinary: false, goos: "linux", skipped: false},
		{testBinary: false, goos: "windows", skipped: false},
		{testBinary: true, goos: "darwin", skipped: true},
		{testBinary: true, goos: "linux", skipped: false},
		{testBinary: true, goos: "windows", skipped: false},
	} {
		require.Equal(t, c.skipped, flushSkipped(c.testBinary, c.goos),
			"test binary %v on %s", c.testBinary, c.goos)
	}
	require.Equal(t, flushSkipped(testing.Testing(), runtime.GOOS), skipFlush)
}

// Where the flush runs, a failing Sync reaches the caller: a closed file
// cannot be flushed. Linux CI runs this; a macOS test binary skips the flush.
func TestFlush_ReportsASyncFailure(t *testing.T) {
	if skipFlush {
		t.Skip("a macOS test binary skips the flush; Linux CI runs this test")
	}
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.Error(t, flush(f))
}
