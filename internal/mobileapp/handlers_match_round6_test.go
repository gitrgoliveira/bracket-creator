package mobileapp

import (
	"bytes"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bc-evts stage 2b review round 6: a reason-less finish stamped after a
// recorded result asks the match's score log whether its basis saw that result.
// A log this build cannot read is answered as the score-events door answers it,
// a 200 refusal naming the cause (score_log_unreadable) with the file named in
// the ERROR log, never the 400 "missing correction reason" a queued replay
// would loop on.
func TestScoreHandler_AnUnreadableScoreLogIsARefusalNotAMissingReason(t *testing.T) {
	compID := "merge-r6-unreadable"
	r, store, dir := mergeServerDir(t, compID)
	now := time.Now().UnixMilli()
	w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/Pool A-0/decision",
		map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": now - 20_000})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	logDir := filepath.Join(dir, "competitions", compID, "events")
	require.NoError(t, os.MkdirAll(logDir, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(logDir, "Pool%20A-0.jsonl"), []byte("{not json\n"), 0o600))
	_, lerr := store.LoadScoreLog(compID, "Pool A-0")
	require.ErrorIs(t, lerr, state.ErrScoreLogUnreadable, "the log is unreadable, as set up")
	before, herr := store.LoadMatchHistory(compID, "Pool A-0")
	require.NoError(t, herr)

	var logged bytes.Buffer
	log.SetOutput(&logged)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	code, out := mergeReviewPut(t, r, compID, map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "status": "completed", "winner": mgBob,
		"ipponsA": []string{}, "ipponsB": []string{"M"},
		"changed": []string{"points", "result"}, "modifiedAt": now - 10_000,
	})
	require.Equal(t, http.StatusOK, code, "never the 400 a queued replay loops on: %v", out)
	assert.Equal(t, false, out["applied"])
	assert.NotEqual(t, engine.EventReasonLogUnreadable, out["reason"], "the write is judged, not dropped before the engine")
	after, herr := store.LoadMatchHistory(compID, "Pool A-0")
	require.NoError(t, herr)
	require.Len(t, after, len(before)+1, "the refused write is kept in the match's history, as the answer says")
	last := after[len(after)-1]
	assert.Equal(t, now-10_000, last.Stamp)
	assert.Contains(t, string(last.Held["points"]), "M", "the history holds the write's values")
	assert.Contains(t, logged.String(), "ERROR", "the fault is logged as one")
	assert.Contains(t, logged.String(), "Pool%20A-0.jsonl", "and names the file")
}

// bc-evts stage 2b review round 8: a write that SENDS a basis over a log this
// build cannot read. The engine reads that log as empty and mints a migration
// baseline at line 0; a basis naming lines (Seen 3) must not count that
// synthetic line as seen, or the write is "informed", lands on the closed match
// and its values are neither stored nor held. It is judged as one that saw
// nothing: match_closed, kept in the match's history, nothing overwritten.
func TestScoreHandler_ABasisOverAnUnreadableLogSeesNothing(t *testing.T) {
	for _, first := range []string{"kiken", "finish"} {
		t.Run(first, func(t *testing.T) {
			compID := "merge-r8-basis-" + first
			r, store, dir := mergeServerDir(t, compID)
			now := time.Now().UnixMilli()
			if first == "kiken" {
				w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/Pool A-0/decision",
					map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": now - 20_000})
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			} else {
				code, out := mergeReviewPut(t, r, compID, map[string]any{
					"sideA": mgAlice, "sideB": mgBob, "status": "completed", "winner": mgAlice,
					"ipponsA": []string{"M"}, "ipponsB": []string{},
					"changed": []string{"points", "result"}, "modifiedAt": now - 20_000,
				})
				require.Equal(t, http.StatusOK, code, "%v", out)
			}
			logDir := filepath.Join(dir, "competitions", compID, "events")
			require.NoError(t, os.MkdirAll(logDir, 0o700))
			require.NoError(t, os.WriteFile(filepath.Join(logDir, "Pool%20A-0.jsonl"), []byte("{not json\n"), 0o600))
			before, herr := store.LoadMatchHistory(compID, "Pool A-0")
			require.NoError(t, herr)
			stored, serr := store.LoadPoolMatches(compID)
			require.NoError(t, serr)
			var winnerBefore string
			for _, m := range stored {
				if m.ID == "Pool A-0" {
					winnerBefore = m.Winner
				}
			}

			code, out := mergeReviewPut(t, r, compID, map[string]any{
				"sideA": mgAlice, "sideB": mgBob, "status": "completed", "winner": mgBob,
				"ipponsA": []string{}, "ipponsB": []string{"M"},
				"changed": []string{"points", "result"}, "modifiedAt": now - 10_000,
				"basis": map[string]any{"seen": 3},
			})
			require.Equal(t, http.StatusOK, code, "%v", out)
			assert.Equal(t, false, out["applied"], "the closed match is not overwritten: %v", out)
			assert.Equal(t, "match_closed", out["heldReason"])
			after, herr := store.LoadMatchHistory(compID, "Pool A-0")
			require.NoError(t, herr)
			require.Len(t, after, len(before)+1, "the refused write is kept in the history")
			assert.Contains(t, string(after[len(after)-1].Held["points"]), "M", "with its values")
			stored, serr = store.LoadPoolMatches(compID)
			require.NoError(t, serr)
			for _, m := range stored {
				if m.ID == "Pool A-0" {
					assert.Equal(t, winnerBefore, m.Winner, "the first result stands")
					assert.Empty(t, m.IpponsB)
				}
			}
		})
	}
}
