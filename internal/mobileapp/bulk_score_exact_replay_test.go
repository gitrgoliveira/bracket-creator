package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A bulk-score entry's exact-resend probe replays the match's real log, as
// /score's and the engine's own do (bc-evts review round 12): bulk-score gives
// each entry a probe-only seenResultCheck. It keeps asking every reason-less
// overwrite stamped after the result for its reason: the rival bypass is the
// score door's.
func TestBulkScore_ExactResendOfAFinishIsAnsweredAsRecorded(t *testing.T) {
	for _, tc := range []struct {
		name       string
		knockout   bool
		laterPoint bool // a point stamped after the finish is in the log, turned away
	}{
		{name: "pool"},
		{name: "knockout", knockout: true},
		{name: "pool, a later point turned away", laterPoint: true},
		{name: "knockout, a later point turned away", knockout: true, laterPoint: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const compID = "bulk-exact-replay"
			r, store, mid, file, _ := exactReplayServer(t, compID, tc.knockout)
			bulk := "/api/competitions/" + compID + "/matches/bulk-score"
			at := time.Now().UnixMilli() - 5_000
			entry := func() []map[string]any {
				e := exactReplayFinish(at)
				e["id"] = mid
				return []map[string]any{e}
			}
			requireBulk := func(w *httptest.ResponseRecorder, succeeded int) map[string]any {
				t.Helper()
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				var out map[string]any
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
				assert.EqualValues(t, succeeded, out["succeeded"], w.Body.String())
				return out
			}

			requireBulk(serveJSON(r, "POST", bulk, entry()), 1)
			if tc.laterPoint {
				w := serveJSON(r, "PUT", "/api/competitions/"+compID+"/matches/"+mid+"/score", exactReplayLaterPoint(at+3_000))
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			}
			versionBefore := store.FileVersion(compID, file)
			before, err := store.LoadMatchHistory(compID, mid)
			require.NoError(t, err)

			// The answer was lost: the same entry is sent again.
			out := requireBulk(serveJSON(r, "POST", bulk, entry()), 1)
			assert.Empty(t, out["errors"], "an exact resend is answered as recorded, never asked for a reason")
			assert.Equal(t, versionBefore, store.FileVersion(compID, file), "nothing is written to the match file")
			after, err := store.LoadMatchHistory(compID, mid)
			require.NoError(t, err)
			require.Len(t, after, len(before)+1, "the resend has its own history line")
			last := after[len(after)-1]
			assert.NotEmpty(t, last.Outcomes)
			for group, outcome := range last.Outcomes {
				assert.Equal(t, state.HistoryOutcomeUnchanged, outcome, "group %s", group)
			}
			got := loadExactReplayMatch(t, store, compID, mid, tc.knockout)
			assert.Equal(t, []string{"M", "K"}, got.IpponsA)
			assert.Equal(t, mgAlice, got.Winner)
		})
	}
}

// The probe-only check opens no rival bypass: a reason-less bulk finish
// stamped after a stored kiken still needs its reason.
func TestBulkScore_ReasonlessFinishOverAResultStillNeedsAReason(t *testing.T) {
	for _, knockout := range []bool{false, true} {
		const compID = "bulk-reasonless"
		r, _, mid, _, _ := exactReplayServer(t, compID, knockout)
		now := time.Now().UnixMilli()
		w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/"+mid+"/decision",
			map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka", "modifiedAt": now - 20_000})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		e := exactReplayFinish(now - 10_000)
		e["id"], e["winner"], e["winnerId"] = mid, mgBob, mgBobID
		e["ipponsA"], e["ipponsB"] = []string{}, []string{"M"}
		w = serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/bulk-score", []map[string]any{e})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "requires a non-empty correctionReason", "knockout=%v", knockout)
		assert.Contains(t, w.Body.String(), `"succeeded":0`, "knockout=%v", knockout)
	}
}

// loadExactReplayMatch reads the match back from either branch.
func loadExactReplayMatch(t *testing.T, store *state.Store, compID, mid string, knockout bool) state.MatchResult {
	t.Helper()
	if !knockout {
		ms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range ms {
			if m.ID == mid {
				return m
			}
		}
		t.Fatalf("match %s not found", mid)
	}
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bm := b.Rounds[0][0]
	return state.MatchResult{ID: bm.ID, Winner: bm.Winner, IpponsA: bm.IpponsA, IpponsB: bm.IpponsB, Status: bm.Status}
}
