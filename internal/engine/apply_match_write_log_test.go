package engine

import (
	"bytes"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bc-cse. The unstamped bypass STAYS (legacy clients and pre-column files depend
// on it), but it is the last path that overwrites a known-newer result with no
// comparison possible, so it must not stay invisible. An operator asking where a
// result went needs a server-side record naming the match.
//
// Both negative controls matter: a log that fires on every write is noise no one
// will read, and a log that fires when nothing was bypassed is a false alarm.
func TestApplyMatchWrite_LogsTheUnstampedOverwrite(t *testing.T) {
	// captureLog swaps the default logger's sink for the duration of fn
	// (same approach as TestK2ChecksItsHandleIsTransactional).
	captureLog := func(t *testing.T, fn func()) string {
		t.Helper()
		var buf bytes.Buffer
		prevOut, prevFlags := log.Writer(), log.Flags()
		log.SetOutput(&buf)
		log.SetFlags(0)
		t.Cleanup(func() { log.SetOutput(prevOut); log.SetFlags(prevFlags) })
		fn()
		return buf.String()
	}

	const marker = "unstamped write overwrites"

	t.Run("an unstamped write over a stamped result is logged", func(t *testing.T) {
		var applied bool
		out := captureLog(t, func() {
			applied = applyMatchWrite(&state.MatchResult{ID: "Pool A-1"}, 1_700_000_000_000, matchWriteForward)
		})
		require.True(t, applied, "the bypass must still APPLY: logging it is not refusing it")
		assert.Contains(t, out, marker)
		assert.Contains(t, out, "Pool A-1", "the log must name the match, it is the only identifier in scope here")
	})

	t.Run("a stamped write over a stamped result is not logged", func(t *testing.T) {
		out := captureLog(t, func() {
			applyMatchWrite(&state.MatchResult{ID: "Pool A-2", ModifiedAt: 1_700_000_001_000}, 1_700_000_000_000, matchWriteForward)
		})
		assert.NotContains(t, out, marker, "both sides are stamped, so the guard compared them; nothing was bypassed")
	})

	t.Run("an unstamped write over an unstamped result is not logged", func(t *testing.T) {
		out := captureLog(t, func() {
			applyMatchWrite(&state.MatchResult{ID: "Pool A-3"}, 0, matchWriteForward)
		})
		assert.NotContains(t, out, marker,
			"there is no stamped result being overwritten; this is a pre-column file or a legacy client, the ordinary case")
	})

	t.Run("a restore is exempt before any stamp is read", func(t *testing.T) {
		out := captureLog(t, func() {
			applyMatchWrite(&state.MatchResult{ID: "Pool A-4"}, 1_700_000_000_000, matchWriteRestore)
		})
		assert.NotContains(t, out, marker,
			"a rollback replays a trusted snapshot of this same match and is not an overwrite to report")
	})
}
