package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// The flush is skipped in test binaries on macOS only (operator decision
// 2026-11-18, round flush-1): Linux and Windows test runs keep the real
// flush, so CI still exercises it.
func TestFlush_SkippedOnlyInMacTestBinaries(t *testing.T) {
	require.True(t, testing.Testing())
	require.Equal(t, runtime.GOOS == "darwin", skipFlush)
}

// Where the flush runs, a failing Sync reaches the caller: a closed file
// cannot be flushed. On macOS test binaries the flush is skipped, so the same
// call answers nil.
func TestFlush_ReportsASyncFailureWhereItRuns(t *testing.T) {
	f, err := os.Create(filepath.Join(t.TempDir(), "f"))
	require.NoError(t, err)
	require.NoError(t, f.Close())
	if skipFlush {
		require.NoError(t, flush(f))
		return
	}
	require.Error(t, flush(f))
}
