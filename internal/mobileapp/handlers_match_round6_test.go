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
	assert.Equal(t, engine.EventReasonLogUnreadable, out["reason"])
	assert.NotEmpty(t, out["message"])
	assert.Contains(t, logged.String(), "ERROR", "the fault is logged as one")
	assert.Contains(t, logged.String(), "Pool%20A-0.jsonl", "and names the file")
}
