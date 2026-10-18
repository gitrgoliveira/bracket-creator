package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/require"
)

// A tap answered "applied" is later turned away when a finish whose basis never
// held it arrives after it (rule D5). Its own answer told its device it
// landed, so the device learns of the change from the match: every copy of it
// (the finish's answer, the push the finish sends, a fetch) names the tap in
// turnedAway with its outcome (stage 6 review 5, finding 2). The history keeps
// the tap and says it was not applied.
func TestTurnedAway_AnAppliedTapAFinishNeverSawIsNamedOnEveryCopyOfTheMatch(t *testing.T) {
	r, store, _, hub, _ := setupTestRouter(t)
	const c, mid = "turnedaway", "Pool A-0"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: c, Kind: "individual", Status: state.CompStatusPools}))
	require.NoError(t, store.SaveParticipants(c, []domain.Player{{Name: "Alice", Dojo: "A"}, {Name: "Bob", Dojo: "B"}}))
	ps, err := store.LoadParticipants(c, false)
	require.NoError(t, err)
	id := map[string]string{}
	for _, p := range ps {
		id[p.Name] = p.ID
	}
	require.NoError(t, store.SavePoolMatches(c, []state.MatchResult{{ID: mid, SideA: "Alice", SideB: "Bob", SideAID: id["Alice"], SideBID: id["Bob"],
		Status: state.MatchStatusRunning, IpponsA: []string{}, IpponsB: []string{}}}))
	path := "/api/competitions/" + c + "/matches/" + url.PathEscape(mid)
	do := func(method, p string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, p, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	tap := func(session, eid string, stamp int64, side, letter string) string {
		w := do(http.MethodPost, path+"/score-events", map[string]any{"session": session, "events": []domain.ScoreEvent{
			{ID: eid, Session: session, Seq: 1, Stamp: stamp, Kind: domain.ScoreIpponAdd, Side: side, Letter: letter}}})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out struct {
			Outcomes []scoreEventOutcome `json:"outcomes"`
			Match    state.MatchResult   `json:"match"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		require.Len(t, out.Outcomes, 1)
		require.Empty(t, out.Match.TurnedAway, "nothing is turned away yet")
		return out.Outcomes[0].Outcome
	}
	base := time.Now().UnixMilli() - 60_000
	require.Equal(t, domain.ScoreOutcomeApplied, tap("dev1", "dev1.1", base+100, "a", "M"))
	logged, err := store.LoadScoreLog(c, mid)
	require.NoError(t, err)
	seen := len(logged) // what the finishing device's board showed
	require.Equal(t, domain.ScoreOutcomeApplied, tap("dev2", "dev2.1", base+150, "b", "K"), "dev2's tap lands: nothing has finished the match")

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)
	w := do(http.MethodPut, path+"/score", map[string]any{"sideA": "Alice", "sideB": "Bob", "sideAId": id["Alice"], "sideBId": id["Bob"],
		"status": "completed", "winner": "Alice", "winnerId": id["Alice"], "ipponsA": []string{"M"}, "ipponsB": []string{},
		"changed": []string{state.GroupResult}, "modifiedAt": base + 200, "writeId": "fin", "session": "dev1",
		"basis": map[string]any{"seen": seen, "own": []string{}, "seq": 1}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	want := map[string]string{"dev2.1": domain.ScoreOutcomeBeforeBaseline}
	var answered state.MatchResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &answered))
	require.Equal(t, want, answered.TurnedAway, "the finish's answer")
	require.Empty(t, answered.IpponsB, "the finish stands without dev2's point")

	// The push dev2 hears.
	pushed := false
	for deadline := time.After(2 * time.Second); !pushed; {
		select {
		case msg := <-ch:
			evt := decodeHubEvent(t, msg)
			if evt.Type != EventMatchUpdated {
				continue
			}
			data, _ := evt.Data.(map[string]any)
			res, _ := data["result"].(map[string]any)
			if res == nil {
				continue
			}
			require.Equal(t, map[string]any{"dev2.1": domain.ScoreOutcomeBeforeBaseline}, res["turnedAway"], "the push")
			pushed = true
		case <-deadline:
			t.Fatal("no match_updated push for the finish")
		}
	}

	// A fetch, as a device coming back online or reloading reads it.
	stored, err := store.LoadPoolMatches(c)
	require.NoError(t, err)
	require.Equal(t, want, stored[0].TurnedAway, "the stored match, every read's source")
	for _, p := range []string{"/api/viewer/competitions/" + c} {
		w = do(http.MethodGet, p, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Contains(t, w.Body.String(), `"turnedAway":{"dev2.1":"beforeBaseline"}`, "the fetch %s", p)
	}

	// The history keeps dev2's tap, and says it was not applied.
	hist, err := store.LoadMatchHistory(c, mid)
	require.NoError(t, err)
	last := ""
	for _, e := range hist {
		if e.Event != nil && e.Event.ID == "dev2.1" {
			last = e.EventOutcome
		}
	}
	require.Equal(t, domain.ScoreOutcomeBeforeBaseline, last, "the history's latest line for dev2's tap")
}
