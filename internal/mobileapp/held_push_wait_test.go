package mobileapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestHeldPushWaitMatchesCoalesceWindow pins the page's refetch wait to the
// server's push hold (bc-sseq FR4). A page sends no refetch sooner than
// HELD_PUSH_WAIT_MS (web-mobile/js/patch.jsx) after the event that asked for
// it, so a running match's push the coalescer is still holding goes out first
// and the read's X-Event-Seq covers it. The shared table
// testdata/held_push_wait.json is read here and by
// web-mobile/js/__tests__/refetch_gate.test.jsx: a change to
// matchCoalesceWindow fails this test until the table, and with it the JS
// constant, moves too.
func TestHeldPushWaitMatchesCoalesceWindow(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "held_push_wait.json"))
	require.NoError(t, err)
	var table struct {
		MatchCoalesceWindowMs int64 `json:"matchCoalesceWindowMs"`
		MarginMs              int64 `json:"marginMs"`
		HeldPushWaitMs        int64 `json:"heldPushWaitMs"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))

	require.Equal(t, matchCoalesceWindow, time.Duration(table.MatchCoalesceWindowMs)*time.Millisecond,
		"testdata/held_push_wait.json matchCoalesceWindowMs must equal matchCoalesceWindow")
	require.Positive(t, table.MarginMs, "the wait must outlast the hold")
	require.Equal(t, table.MatchCoalesceWindowMs+table.MarginMs, table.HeldPushWaitMs,
		"heldPushWaitMs is the coalescing window plus the margin")
}
