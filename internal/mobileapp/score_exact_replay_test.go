package mobileapp

// bc-xrpl: a completing score write the server stored but whose answer the
// client never read (the page went away, the connection dropped, the fetch
// timed out) is sent again by the offline queue with the SAME stamp. That
// resend is the write it already recorded, so it is answered as recorded
// (engine.ExactScoreReplay), with no correction reason asked and nothing
// written to the match file. A same-stamp write that differs from what is
// stored is still a correction, and still needs its reason.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// A self-run participant's finish whose answer was lost is re-sent by the
// offline queue exactly as it was: it is answered as recorded, as the
// organiser's is, and not refused as a result already reported (which the
// page would show as "Not saved" for a result that is stored). A resend that
// differs in anything it would land is still refused: a finished match is
// the organiser's to correct.
func TestScoreHandler_SelfRunExactResendOfAFinishIsAnsweredAsRecorded(t *testing.T) {
	for _, knockout := range []bool{false, true} {
		name := "pool"
		if knockout {
			name = "knockout"
		}
		t.Run(name, func(t *testing.T) {
			const compID = "exact-replay-self-run"
			r, store, mid, file, dataDir := exactReplayServer(t, compID, knockout)
			require.NoError(t, store.SaveTournament(&state.Tournament{
				Name: "T", Password: "main-pw", Courts: []string{"A"}, Mode: state.TournamentModeSelfRun,
			}))
			url := "/api/competitions/" + compID + "/matches/" + mid + "/score"
			at := time.Now().UnixMilli() - 5_000
			anonymous := func(payload map[string]any) *httptest.ResponseRecorder {
				body, _ := json.Marshal(payload)
				req, _ := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(body))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("X-Tournament-Password", "")
				w := httptest.NewRecorder()
				r.ServeHTTP(w, req)
				return w
			}

			w := anonymous(exactReplayFinish(at))
			require.Equal(t, http.StatusOK, w.Code, "the participant's finish lands: %s", w.Body.String())

			path := filepath.Join(dataDir, "competitions", compID, file)
			bytesBefore, err := os.ReadFile(path)
			require.NoError(t, err)
			versionBefore := store.FileVersion(compID, file)

			out := requireAnsweredAsRecorded(t, anonymous(exactReplayFinish(at)), store, compID, file, versionBefore)
			assert.Equal(t, mgAlice, out["winner"])
			assert.Equal(t, []any{"M", "K"}, out["ipponsA"])
			assert.EqualValues(t, at, out["modifiedAt"])
			bytesAfter, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, string(bytesBefore), string(bytesAfter), "nothing is written to the match file")

			history, err := store.LoadMatchHistory(compID, mid)
			require.NoError(t, err)
			assert.Len(t, history, 1, "a participant's resend records no history line, so looping it cannot grow the file")

			// The same stamp with another scoreline is no resend.
			differs := exactReplayFinish(at)
			differs["ipponsA"] = []string{"M"}
			w = anonymous(differs)
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "result_finalized")
			// A later stamp with the same scoreline is no resend either.
			w = anonymous(exactReplayFinish(at + 1_000))
			assert.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "result_finalized")
			assert.Equal(t, versionBefore, store.FileVersion(compID, file), "a refused resend writes nothing")
			history, err = store.LoadMatchHistory(compID, mid)
			require.NoError(t, err)
			assert.Len(t, history, 1, "a refused resend leaves no history line")
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

// requireAnsweredAsRecorded checks a resend was answered like a landed write
// (no refusal, nothing held) and that the match file was not written.
func requireAnsweredAsRecorded(t *testing.T, w *httptest.ResponseRecorder, store *state.Store, compID, file string, versionBefore uint64) map[string]any {
	t.Helper()
	require.Equal(t, http.StatusOK, w.Code, "an exact resend is answered as recorded: %s", w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.NotContains(t, out, "applied", "answered like a landed write, not a refusal")
	assert.NotContains(t, out, "heldGroups")
	assert.Equal(t, "completed", out["status"])
	assert.Equal(t, versionBefore, store.FileVersion(compID, file), "nothing is written to the match file")
	return out
}

// exactReplayLaterPoint is another board adding Alice's kote at `at`, after
// (R3) or before (S2) the finish arrived.
func exactReplayLaterPoint(at int64) map[string]any {
	return map[string]any{
		"sideA": mgAlice, "sideB": mgBob, "sideAId": mgAliceID, "sideBId": mgBobID,
		"ipponsA": []string{"M", "K"}, "ipponsB": []string{},
		"status": "running", "changed": []string{"points"}, "modifiedAt": at,
	}
}

// A finish whose answer was lost, while another board added a point stamped
// after it, is resent with the finish's stamp. The merge would hold its points
// (the stored ones are newer) and apply only its verdict, which equals the
// stored one, so nothing of it would land: it is the finish already recorded
// and answered as such, not refused for a correction reason. Both arrival
// orders of the later point (R3: after the finish; S2: before it) end there.
func TestScoreHandler_ExactResendAfterALaterPointIsAnsweredAsRecorded(t *testing.T) {
	for _, tc := range []struct {
		name         string
		knockout     bool
		pointArrives string // "after" (R3) or "before" (S2) the finish
	}{
		{name: "pool, R3", pointArrives: "after"},
		{name: "knockout, R3", knockout: true, pointArrives: "after"},
		{name: "pool, S2", pointArrives: "before"},
		{name: "knockout, S2", knockout: true, pointArrives: "before"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const compID = "exact-replay-later-point"
			r, store, mid, file, _ := exactReplayServer(t, compID, tc.knockout)
			url := "/api/competitions/" + compID + "/matches/" + mid + "/score"
			at := time.Now().UnixMilli() - 5_000
			finish := func() map[string]any {
				b := exactReplayFinish(at)
				b["ipponsA"] = []string{"M"}
				return b
			}

			if tc.pointArrives == "before" {
				w := serveJSON(r, "PUT", url, exactReplayLaterPoint(at+3_000))
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			}
			w := serveJSON(r, "PUT", url, finish())
			require.Equal(t, http.StatusOK, w.Code, "the finish lands: %s", w.Body.String())
			if tc.pointArrives == "after" {
				w = serveJSON(r, "PUT", url, exactReplayLaterPoint(at+3_000))
				require.Equal(t, http.StatusOK, w.Code, "R3: the later point applies to the finished match: %s", w.Body.String())
			}
			versionBefore := store.FileVersion(compID, file)

			out := requireAnsweredAsRecorded(t, serveJSON(r, "PUT", url, finish()), store, compID, file, versionBefore)
			assert.Equal(t, mgAlice, out["winner"])
			assert.Equal(t, []any{"M", "K"}, out["ipponsA"], "the answer is the match as stored, the later point included")
		})
	}
}

// The other half: a same-stamp resend over that state whose verdict (a group
// the merge would apply) differs from the stored one is a correction, and
// still needs its reason.
func TestScoreHandler_SameStampResendWhoseAppliedGroupDiffersStillNeedsAReason(t *testing.T) {
	for _, knockout := range []bool{false, true} {
		t.Run(map[bool]string{false: "pool", true: "knockout"}[knockout], func(t *testing.T) {
			const compID = "exact-replay-later-point-differs"
			r, store, mid, file, _ := exactReplayServer(t, compID, knockout)
			url := "/api/competitions/" + compID + "/matches/" + mid + "/score"
			at := time.Now().UnixMilli() - 5_000
			finish := exactReplayFinish(at)
			finish["ipponsA"] = []string{"M"}
			w := serveJSON(r, "PUT", url, finish)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			w = serveJSON(r, "PUT", url, exactReplayLaterPoint(at+3_000))
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			versionBefore := store.FileVersion(compID, file)

			changed := exactReplayFinish(at)
			changed["winner"], changed["winnerId"] = mgBob, mgBobID
			changed["ipponsA"], changed["ipponsB"] = []string{}, []string{"M", "K"}
			w = serveJSON(r, "PUT", url, changed)
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "correctionReason")
			assert.Equal(t, versionBefore, store.FileVersion(compID, file), "the refused correction changes nothing")
		})
	}
}

// The resend of a finish of every other shape the score sheet sends: each
// lands once, then is sent again identically with the same stamp, and is
// answered as recorded however the write path rewrote the first landing
// (applyKachinukiMerge's strip of a trailing unscored pairing and member ids,
// the representative bout's hantei mark and names, the engi winner the flags
// decide).
func TestScoreHandler_ExactResendOfATeamOrEngiFinishIsAnsweredAsRecorded(t *testing.T) {
	t.Run("kachinuki encounter", func(t *testing.T) {
		const compID = "exact-replay-kachinuki"
		r, store := setupKachinukiScoreServer(t, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M", "K"}, Winner: "R-1", Decision: "fought"},
				{Position: 2, SideA: "R-1", SideB: "W-2"},
			},
		}}))
		at := time.Now().UnixMilli() - 5_000
		finish := func() map[string]any {
			return map[string]any{
				"sideA": "Ryu", "sideB": "Tora", "winner": "Ryu", "status": "completed", "decision": "kachinuki-exhaustion",
				"modifiedAt": at,
				"subResults": []map[string]any{
					kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
					kachinukiSub(2, "R-1", "W-2", []string{}, "", ""),
				},
			}
		}
		w := putScore(t, r, compID, "P1-0", finish())
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Len(t, loadPoolMatch(t, store, compID, "P1-0").SubResults, 1, "the first landing strips the unscored pairing")
		versionBefore := store.FileVersion(compID, "pool-matches.csv")

		out := requireAnsweredAsRecorded(t, putScore(t, r, compID, "P1-0", finish()), store, compID, "pool-matches.csv", versionBefore)
		assert.Equal(t, "Ryu", out["winner"])
	})

	t.Run("fixed-order team knockout decided by a hantei on the representative bout", func(t *testing.T) {
		f := newRepBoutFixture(t, false)
		w := f.send(http.MethodPost, repBoutMatchPath+"/daihyosen", "main-pw", map[string]any{"modifiedAt": f.now})
		require.Equal(t, http.StatusOK, w.Code, "adding the representative bout: %s", w.Body.String())
		at := f.now + 100
		w = f.score("main-pw", state.MatchStatusCompleted, "TeamA", at, hanteiRow())
		require.Equal(t, http.StatusOK, w.Code, "the finish lands: %s", w.Body.String())
		require.True(t, f.storedRepBout(t).HanteiDecided())
		versionBefore := f.store.FileVersion("c1", "bracket.json")

		out := requireAnsweredAsRecorded(t, f.score("main-pw", state.MatchStatusCompleted, "TeamA", at, hanteiRow()), f.store, "c1", "bracket.json", versionBefore)
		assert.Equal(t, "TeamA", out["winner"])
		assert.True(t, f.storedRepBout(t).HanteiDecided(), "the verdict stands")
	})

	t.Run("engi", func(t *testing.T) {
		const compID = "exact-replay-engi"
		r, store := mergeServer(t, compID)
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		comp.Engi = true
		require.NoError(t, store.SaveCompetition(comp))
		at := time.Now().UnixMilli() - 5_000
		finish := func() map[string]any {
			return map[string]any{
				"sideA": mgAlice, "sideB": mgBob, "status": "completed", "flagsA": 3, "flagsB": 0,
				"changed": []string{"result", "flags"}, "modifiedAt": at,
			}
		}
		code, out := mergeReviewPut(t, r, compID, finish())
		require.Equal(t, http.StatusOK, code, out)
		require.Equal(t, mgAlice, mergeStored(t, store, compID).Winner)
		versionBefore := store.FileVersion(compID, "pool-matches.csv")

		got := requireAnsweredAsRecorded(t, serveJSON(r, "PUT", "/api/competitions/"+compID+"/matches/Pool A-0/score", finish()), store, compID, "pool-matches.csv", versionBefore)
		assert.Equal(t, mgAlice, got["winner"])
	})
}
