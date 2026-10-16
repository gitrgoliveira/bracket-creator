package mobileapp

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A finish whose basis holds an OLD recorded result but not the one in force
// (a second device finished the reopened match since) did not see the result in
// force: it is no correction and asks for no reason. The replay turns it away as
// match_closed and keeps it, where the handler used to answer it a 400 it could
// never fix (the basis was judged against ANY completed line of the log).
func TestScoreHandler_ABasisThatSawAnOlderResultSeesNotTheOneInForce(t *testing.T) {
	const compID = "merge-r10-basis"
	r, store, _ := mergeServerDir(t, compID)
	now := time.Now().UnixMilli()
	finish := func(winner string, at int64, basis map[string]any) (int, map[string]any) {
		body := map[string]any{
			"sideA": mgAlice, "sideB": mgBob, "status": "completed", "winner": winner,
			"ipponsA": []string{"M"}, "ipponsB": []string{},
			"changed": []string{"points", "result"}, "modifiedAt": at,
		}
		if winner == mgBob {
			body["ipponsA"], body["ipponsB"] = []string{}, []string{"M"}
		}
		if basis != nil {
			body["basis"] = basis
		}
		return mergeReviewPut(t, r, compID, body)
	}
	w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/Pool A-0/decision",
		map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": now - 40_000})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/Pool A-0/reopen", map[string]any{})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	lines, err := store.LoadScoreLog(compID, "Pool A-0")
	require.NoError(t, err)
	seen := len(lines) // the old result and the reopen, nothing after

	code, out := finish(mgBob, now+2_000, nil) // another device ends the reopened match
	require.Equal(t, http.StatusOK, code, "%v", out)

	code, out = finish(mgAlice, now+4_000, map[string]any{"seen": seen})
	require.Equal(t, http.StatusOK, code, "no correction reason is asked: %v", out)
	assert.Equal(t, false, out["applied"], "%v", out)
	assert.Equal(t, "match_closed", out["heldReason"], "%v", out)
}
