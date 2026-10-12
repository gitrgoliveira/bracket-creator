package mobileapp

// bc-xrpl: a completing score write the server stored but whose answer the
// client never read (the page went away, the connection dropped, the fetch
// timed out) is sent again by the offline queue with the SAME stamp. That
// resend is the write it already recorded, so it is answered as recorded
// (engine.ExactScoreReplay), with no correction reason asked and nothing
// written to the match file. A same-stamp write that differs from what is
// stored is still a correction, and still needs its reason.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// exactReplayServer is a running individual match on court A, Alice v Bob,
// in a pool (Pool A-0) or a knockout (m-r1-0, feeding the final m-r2-0). It
// returns the match's id, the file that holds it and the data folder.
func exactReplayServer(t *testing.T, compID string, knockout bool) (http.Handler, *state.Store, string, string, string) {
	t.Helper()
	r, store, _, _, dataDir := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	comp := &state.Competition{
		ID: compID, Name: compID, Kind: "individual", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}
	if knockout {
		comp.Format, comp.Status = state.CompFormatKnockout, state.CompStatusKnockout
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: mgAliceID, Name: mgAlice, Dojo: "D1"},
		{ID: mgBobID, Name: mgBob, Dojo: "D2"},
	}))
	if !knockout {
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool A-0", SideA: mgAlice, SideAID: mgAliceID, SideB: mgBob, SideBID: mgBobID, Court: "A", Status: state.MatchStatusRunning},
		}))
		runningRevStore.Delete(compID + ":Pool A-0")
		return r, store, "Pool A-0", "pool-matches.csv", dataDir
	}
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{ID: "m-r1-0", SideA: mgAlice, SideAID: mgAliceID, SideB: mgBob, SideBID: mgBobID,
				Status: state.MatchStatusRunning, MatchNumber: 1, Court: "A", ScheduledAt: "10:00"},
		},
		{
			{ID: "m-r2-0", SideA: "Winner of m-r1-0", MatchNumber: 2, Court: "A"},
		},
	}}))
	runningRevStore.Delete(compID + ":m-r1-0")
	return r, store, "m-r1-0", "bracket.json", dataDir
}

// exactReplayFinish is the Finish an operator's score editor sends: Alice
// wins M-K, stamped at.
func exactReplayFinish(at int64) map[string]any {
	return map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "sideAId": mgAliceID, "sideBId": mgBobID,
		"winner": mgAlice, "winnerId": mgAliceID,
		"ipponsA": []string{"M", "K"}, "ipponsB": []string{},
		"hansokuA": 0, "hansokuB": 0, "decision": "",
		"status": "completed", "modifiedAt": at,
	}
}

func TestScoreHandler_ExactResendOfAFinishIsAnsweredAsRecorded(t *testing.T) {
	for _, tc := range []struct {
		name     string
		knockout bool
		// body shapes the resend: as the editor sends it, naming its groups,
		// and with no winnerId (the client sends one only when the server
		// supplied the side ids; the write fills it in from the sides).
		body func(map[string]any)
	}{
		{name: "pool", body: func(map[string]any) {}},
		{name: "pool, naming its groups", body: func(b map[string]any) { b["changed"] = []string{"points", "result"} }},
		{name: "pool, no winnerId", body: func(b map[string]any) { delete(b, "winnerId") }},
		{name: "knockout", knockout: true, body: func(map[string]any) {}},
		{name: "knockout, naming its groups", knockout: true, body: func(b map[string]any) { b["changed"] = []string{"points", "result"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const compID = "exact-replay"
			r, store, mid, file, dataDir := exactReplayServer(t, compID, tc.knockout)
			url := "/api/competitions/" + compID + "/matches/" + mid + "/score"
			at := time.Now().UnixMilli() - 5_000

			first := exactReplayFinish(at)
			tc.body(first)
			w := serveJSON(r, "PUT", url, first)
			require.Equal(t, http.StatusOK, w.Code, "the first send records the finish: %s", w.Body.String())
			var recorded map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &recorded))

			path := filepath.Join(dataDir, "competitions", compID, file)
			bytesBefore, err := os.ReadFile(path)
			require.NoError(t, err)
			versionBefore := store.FileVersion(compID, file)

			// The answer was lost: the queue sends the same write again.
			resend := exactReplayFinish(at)
			tc.body(resend)
			w = serveJSON(r, "PUT", url, resend)
			require.Equal(t, http.StatusOK, w.Code, "an exact resend is answered as recorded: %s", w.Body.String())
			var out map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
			assert.NotContains(t, out, "applied", "answered like a landed write, not a refusal")
			assert.NotContains(t, out, "heldGroups")
			assert.Equal(t, "completed", out["status"])
			assert.Equal(t, mgAlice, out["winner"])
			assert.Equal(t, mgAliceID, out["winnerId"])
			assert.Equal(t, []any{"M", "K"}, out["ipponsA"])
			assert.EqualValues(t, at, out["modifiedAt"])
			assert.Equal(t, "A", out["court"], "the answer carries the match's court, as the first one did")
			assert.Equal(t, recorded["court"], out["court"])
			assert.Equal(t, recorded["scheduledAt"], out["scheduledAt"])

			bytesAfter, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(bytesBefore), string(bytesAfter), "nothing is written to the match file")
			assert.Equal(t, versionBefore, store.FileVersion(compID, file), "nor is its version moved")

			history, err := store.LoadMatchHistory(compID, mid)
			require.NoError(t, err)
			require.Len(t, history, 2, "the resend has its own history line, as an exact decision replay does")
			assert.NotEmpty(t, history[1].Outcomes)
			for group, outcome := range history[1].Outcomes {
				assert.Equal(t, state.HistoryOutcomeUnchanged, outcome, "group %s", group)
			}
			assert.Empty(t, history[1].Held)
		})
	}
}

func TestScoreHandler_SameStampFinishThatDiffersStillNeedsAReason(t *testing.T) {
	for _, tc := range []struct {
		name     string
		knockout bool
		change   func(map[string]any)
	}{
		{name: "pool, another winner", change: func(b map[string]any) {
			b["winner"], b["winnerId"], b["ipponsA"], b["ipponsB"] = mgBob, mgBobID, []string{}, []string{"M", "K"}
		}},
		{name: "pool, another scoreline", change: func(b map[string]any) { b["ipponsA"] = []string{"M", "D"} }},
		{name: "knockout, another winner", knockout: true, change: func(b map[string]any) {
			b["winner"], b["winnerId"], b["ipponsA"], b["ipponsB"] = mgBob, mgBobID, []string{}, []string{"M", "K"}
		}},
		{name: "knockout, another scoreline", knockout: true, change: func(b map[string]any) { b["ipponsA"] = []string{"M", "D"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const compID = "exact-replay-differs"
			r, _, mid, file, dataDir := exactReplayServer(t, compID, tc.knockout)
			url := "/api/competitions/" + compID + "/matches/" + mid + "/score"
			at := time.Now().UnixMilli() - 5_000

			w := serveJSON(r, "PUT", url, exactReplayFinish(at))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			path := filepath.Join(dataDir, "competitions", compID, file)
			bytesBefore, err := os.ReadFile(path)
			require.NoError(t, err)

			changed := exactReplayFinish(at)
			tc.change(changed)
			w = serveJSON(r, "PUT", url, changed)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "correctionReason")
			bytesAfter, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(bytesBefore), string(bytesAfter), "the refused correction changes nothing")

			// With its reason it is the correction it is.
			changed["correctionReason"] = "wrong winner entered"
			w = serveJSON(r, "PUT", url, changed)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		})
	}
}

// A completed write with no stamp over a finished match is a correction like
// any other: it needs its reason (only a stamp can say it is the same write).
func TestScoreHandler_UnstampedFinishOverAFinishStillNeedsAReason(t *testing.T) {
	const compID = "exact-replay-unstamped"
	r, _, mid, _, _ := exactReplayServer(t, compID, false)
	url := "/api/competitions/" + compID + "/matches/" + mid + "/score"
	w := serveJSON(r, "PUT", url, exactReplayFinish(time.Now().UnixMilli()-5_000))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	unstamped := exactReplayFinish(0)
	delete(unstamped, "modifiedAt")
	w = serveJSON(r, "PUT", url, unstamped)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "correctionReason")
}
