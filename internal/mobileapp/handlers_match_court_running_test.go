package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func putCourtJSON(t *testing.T, r *gin.Engine, compID, matchID string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req, err := http.NewRequest(http.MethodPut, fmt.Sprintf("/api/competitions/%s/matches/%s/court", compID, matchID), bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func setupCourtMoveFixture(t *testing.T) (*gin.Engine, *state.Store, *engine.Engine, *Hub, string, string) {
	t.Helper()
	r, store, eng, hub, tempDir := setupTestRouter(t)
	compID := "court-comp"
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A", "B"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Court Move", Format: state.CompFormatLeague}))
	aliceID, bobID := "alice-id", "bob-id"
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "Dojo A"},
		{ID: bobID, Name: "Bob", Dojo: "Dojo B"},
		{ID: "carol-id", Name: "Carol", Dojo: "Dojo C"},
		{ID: "dave-id", Name: "Dave", Dojo: "Dojo D"},
	}))
	return r, store, eng, hub, compID, tempDir
}

// (a) a confirmed running move (stopsRunning true) answers 200, and the
// match_updated push has result.status "scheduled", the new court, and the
// first-in-queue scheduledAt (one minute before the earliest waiting match on
// the target court), followed by schedule_updated.
func TestCourtMoveRunning_ConfirmedMove(t *testing.T) {
	r, store, _, hub, compID, _ := setupCourtMoveFixture(t)

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "m-run", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "11:00"},
		{ID: "m-b1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
		{ID: "m-b2", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:30"},
	}))

	// Start m-run via score handler so it is running.
	startResp := putScore(t, r, compID, "m-run", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "status": "running", "startOnly": true,
	})
	require.Equal(t, http.StatusOK, startResp.Code, startResp.Body.String())

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	resp := putCourtJSON(t, r, compID, "m-run", map[string]any{
		"court":        "B",
		"stopsRunning": true,
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	events := drainHubEvents(t, ch, 500*time.Millisecond)
	require.NotEmpty(t, events, "must broadcast events")

	var matchUpdatedEv *SSEEvent
	var scheduleUpdatedEv *SSEEvent
	for i := range events {
		if events[i].Type == EventMatchUpdated {
			matchUpdatedEv = &events[i]
		}
		if events[i].Type == EventScheduleUpdated {
			scheduleUpdatedEv = &events[i]
		}
	}
	require.NotNil(t, matchUpdatedEv, "must push match_updated")
	require.NotNil(t, scheduleUpdatedEv, "must push schedule_updated")

	data, ok := matchUpdatedEv.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, compID, data["competitionId"])
	assert.Equal(t, "m-run", data["matchId"])
	assert.Equal(t, "B", data["court"])

	res, ok := data["result"].(map[string]any)
	require.True(t, ok, "result must be present in match_updated push")
	assert.Equal(t, "scheduled", res["status"])
	assert.Equal(t, "B", res["court"])
	assert.Equal(t, "09:59", res["scheduledAt"], "placed one minute before earliest waiting match on court B (10:00)")
}

// (b) a waiting move pushes the new court and its NEW last-in-queue scheduledAt
// (one minute after the latest waiting match on the target court).
func TestCourtMoveRunning_WaitingMove(t *testing.T) {
	r, store, _, hub, compID, _ := setupCourtMoveFixture(t)

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "m-wait", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "m-b1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
		{ID: "m-b2", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:30"},
	}))

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	resp := putCourtJSON(t, r, compID, "m-wait", map[string]any{
		"court": "B",
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	events := drainHubEvents(t, ch, 500*time.Millisecond)
	require.NotEmpty(t, events, "must broadcast events")

	var matchUpdatedEv *SSEEvent
	for i := range events {
		if events[i].Type == EventMatchUpdated {
			matchUpdatedEv = &events[i]
		}
	}
	require.NotNil(t, matchUpdatedEv, "must push match_updated")

	data, ok := matchUpdatedEv.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "B", data["court"])

	res, ok := data["result"].(map[string]any)
	require.True(t, ok, "result must be present in match_updated push")
	assert.Equal(t, "scheduled", res["status"])
	assert.Equal(t, "B", res["court"])
	assert.Equal(t, "10:31", res["scheduledAt"], "placed one minute after latest waiting match on court B (10:30)")
}

// (c) a finished match's move pushes its court and its UNCHANGED scheduledAt.
func TestCourtMoveRunning_FinishedMatch(t *testing.T) {
	r, store, _, hub, compID, _ := setupCourtMoveFixture(t)

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "m-fin", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Court: "A", ScheduledAt: "09:15", Winner: "Alice"},
		{ID: "m-b1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
	}))

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	resp := putCourtJSON(t, r, compID, "m-fin", map[string]any{
		"court": "B",
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	events := drainHubEvents(t, ch, 500*time.Millisecond)
	require.NotEmpty(t, events, "must broadcast events")

	var matchUpdatedEv *SSEEvent
	for i := range events {
		if events[i].Type == EventMatchUpdated {
			matchUpdatedEv = &events[i]
		}
	}
	require.NotNil(t, matchUpdatedEv, "must push match_updated")

	data, ok := matchUpdatedEv.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "B", data["court"])

	res, ok := data["result"].(map[string]any)
	require.True(t, ok, "result must be present in match_updated push")
	assert.Equal(t, "completed", res["status"])
	assert.Equal(t, "B", res["court"])
	assert.Equal(t, "09:15", res["scheduledAt"], "scheduledAt must remain unchanged for a finished match")
}

// (d) an unconfirmed move of a running match is 409 with code match_status_changed
// and the sentence in reasonHuman, message and error, and pushes nothing, and the
// match is unchanged.
func TestCourtMoveRunning_UnconfirmedRunningMoveRefused(t *testing.T) {
	r, store, _, hub, compID, _ := setupCourtMoveFixture(t)

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "m-run", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "m-b1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
	}))

	startResp := putScore(t, r, compID, "m-run", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "status": "running", "startOnly": true,
	})
	require.Equal(t, http.StatusOK, startResp.Code, startResp.Body.String())

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	resp := putCourtJSON(t, r, compID, "m-run", map[string]any{
		"court":        "B",
		"stopsRunning": false,
	})
	require.Equal(t, http.StatusConflict, resp.Code, resp.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "match_status_changed", body["code"])
	reasonHuman, ok := body["reasonHuman"].(string)
	require.True(t, ok)
	assert.NotEmpty(t, reasonHuman)
	assert.Equal(t, reasonHuman, body["message"])
	assert.Equal(t, reasonHuman, body["error"])
	assert.Contains(t, reasonHuman, "was started on Shiaijo A before your move reached the server")

	// Pushes nothing
	events := drainHubEvents(t, ch, 100*time.Millisecond)
	assert.Empty(t, events, "a refused move pushes nothing")

	// Match in store is unchanged
	m, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, match := range m {
		if match.ID == "m-run" {
			assert.Equal(t, state.MatchStatusRunning, match.Status)
			assert.Equal(t, "A", match.Court)
			assert.Equal(t, "09:00", match.ScheduledAt)
		}
	}
}

// (e) a confirmed move over a scheduled or completed match succeeds (200),
// moving it as a waiting or finished match, and pushes match_updated with the
// new court and the stored status.
func TestCourtMoveRunning_ConfirmedMoveOverScheduledMatchMovesAsWaiting(t *testing.T) {
	r, store, _, hub, compID, tempDir := setupCourtMoveFixture(t)

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "m-sched", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "m-b1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
		{ID: "m-done", SideA: "Alice", SideB: "Carol", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A", ScheduledAt: "08:30"},
	}))

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	resp := putCourtJSON(t, r, compID, "m-sched", map[string]any{
		"court":        "B",
		"stopsRunning": true,
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	events := drainHubEvents(t, ch, 500*time.Millisecond)
	require.NotEmpty(t, events, "must broadcast events")

	var matchUpdatedEv *SSEEvent
	for i := range events {
		if events[i].Type == EventMatchUpdated {
			matchUpdatedEv = &events[i]
		}
	}
	require.NotNil(t, matchUpdatedEv, "must push match_updated")

	data, ok := matchUpdatedEv.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, compID, data["competitionId"])
	assert.Equal(t, "m-sched", data["matchId"])
	assert.Equal(t, "B", data["court"])

	res, ok := data["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "scheduled", res["status"])
	assert.Equal(t, "B", res["court"])
	assert.Equal(t, "10:01", res["scheduledAt"])

	// Match in store: court B, scheduledAt 10:01, status scheduled.
	pms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range pms {
		if m.ID == "m-sched" {
			assert.Equal(t, state.MatchStatusScheduled, m.Status)
			assert.Equal(t, "B", m.Court)
			assert.Equal(t, "10:01", m.ScheduledAt)
		}
	}

	historyFile := filepath.Join(tempDir, "competitions", compID, "history", "m-sched.jsonl")
	_, err = os.Stat(historyFile)
	assert.True(t, os.IsNotExist(err), "waiting move must not write history line")

	// Now move completed match with stopsRunning: true -> 200, court B, time and result kept.
	respDone := putCourtJSON(t, r, compID, "m-done", map[string]any{
		"court":        "B",
		"stopsRunning": true,
	})
	require.Equal(t, http.StatusOK, respDone.Code, respDone.Body.String())

	doneEvents := drainHubEvents(t, ch, 500*time.Millisecond)
	require.NotEmpty(t, doneEvents)
	var doneEv *SSEEvent
	for i := range doneEvents {
		if doneEvents[i].Type == EventMatchUpdated {
			doneEv = &doneEvents[i]
		}
	}
	require.NotNil(t, doneEv)
	doneData, ok := doneEv.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "B", doneData["court"])
	doneRes, ok := doneData["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "completed", doneRes["status"])
	assert.Equal(t, "B", doneRes["court"])
	assert.Equal(t, "08:30", doneRes["scheduledAt"])
}

// (f) a late duplicate writeId of a RUNNING move, sent through a fresh
// handler/router over the same store (so the handler's in-memory once.do memory
// is gone), writes nothing (match file contents, history files and score log
// unchanged) and pushes the stored match again.
func TestCourtMoveRunning_LateDuplicateRunningMovePushesStoredMatch(t *testing.T) {
	r, store, _, _, compID, tempDir := setupCourtMoveFixture(t)

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "m-run", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "11:00"},
		{ID: "m-b1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
	}))

	startResp := putScore(t, r, compID, "m-run", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "status": "running", "startOnly": true,
	})
	require.Equal(t, http.StatusOK, startResp.Code, startResp.Body.String())

	writeID := "w-dup-run-test"
	resp := putCourtJSON(t, r, compID, "m-run", map[string]any{
		"court":        "B",
		"stopsRunning": true,
		"writeId":      writeID,
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Record file contents after the move landed
	poolMatchesFile := filepath.Join(tempDir, "competitions", compID, "pool-matches.csv")
	poolBytesBefore, err := os.ReadFile(poolMatchesFile)
	require.NoError(t, err)

	historyFile := filepath.Join(tempDir, "competitions", compID, "history", "m-run.jsonl")
	historyBytesBefore, err := os.ReadFile(historyFile)
	require.NoError(t, err)

	eventsFile := filepath.Join(tempDir, "competitions", compID, "events", "m-run.jsonl")
	eventsBytesBefore, err := os.ReadFile(eventsFile)
	require.NoError(t, err)

	// Create a FRESH router over the exact same store (once.do in-memory map is fresh).
	freshHub := NewHub()
	t.Cleanup(freshHub.Close)
	freshRouter := gin.New()
	admin := freshRouter.Group("/api")
	eng := engine.New(store)
	RegisterMatchHandlers(admin, eng, store, store, freshHub, NewFileVerifier(store), store)

	freshCh := freshHub.Subscribe()
	defer freshHub.Unsubscribe(freshCh)

	// Send duplicate request to fresh router with same writeId
	dupResp := putCourtJSON(t, freshRouter, compID, "m-run", map[string]any{
		"court":        "B",
		"stopsRunning": true,
		"writeId":      writeID,
	})
	require.Equal(t, http.StatusOK, dupResp.Code, dupResp.Body.String())

	// Assert files were NOT changed (nothing written)
	poolBytesAfter, err := os.ReadFile(poolMatchesFile)
	require.NoError(t, err)
	assert.Equal(t, poolBytesBefore, poolBytesAfter, "pool-matches.csv must not change on late duplicate")

	historyBytesAfter, err := os.ReadFile(historyFile)
	require.NoError(t, err)
	assert.Equal(t, historyBytesBefore, historyBytesAfter, "history must not change on late duplicate")

	eventsBytesAfter, err := os.ReadFile(eventsFile)
	require.NoError(t, err)
	assert.Equal(t, eventsBytesBefore, eventsBytesAfter, "score log events must not change on late duplicate")

	// And the fresh hub receives the push of the stored match again
	events := drainHubEvents(t, freshCh, 500*time.Millisecond)
	require.NotEmpty(t, events, "fresh router must broadcast the stored match again")

	var matchUpdatedEv *SSEEvent
	for i := range events {
		if events[i].Type == EventMatchUpdated {
			matchUpdatedEv = &events[i]
		}
	}
	require.NotNil(t, matchUpdatedEv, "must push match_updated")

	data, ok := matchUpdatedEv.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "B", data["court"])
	res, ok := data["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "scheduled", res["status"])
	assert.Equal(t, "B", res["court"])
	assert.Equal(t, "09:59", res["scheduledAt"])
}

// (g) a refused move's resend (same writeId) is judged again (a second refusal
// 409 and, after the state changes so the move is legal, it applies).
func TestCourtMoveRunning_RefusedMoveResendJudgedAgain(t *testing.T) {
	r, store, _, _, compID, _ := setupCourtMoveFixture(t)

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "m-run", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "11:00"},
		{ID: "m-b1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
	}))

	startResp := putScore(t, r, compID, "m-run", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "status": "running", "startOnly": true,
	})
	require.Equal(t, http.StatusOK, startResp.Code, startResp.Body.String())

	writeID := "w-retry-test"

	// First attempt: unconfirmed -> refused 409
	resp1 := putCourtJSON(t, r, compID, "m-run", map[string]any{
		"court":        "B",
		"stopsRunning": false,
		"writeId":      writeID,
	})
	require.Equal(t, http.StatusConflict, resp1.Code, resp1.Body.String())

	// Resend with same writeId: judged again -> second refusal 409
	resp2 := putCourtJSON(t, r, compID, "m-run", map[string]any{
		"court":        "B",
		"stopsRunning": false,
		"writeId":      writeID,
	})
	require.Equal(t, http.StatusConflict, resp2.Code, resp2.Body.String())

	// Now state changes so move is legal (operator confirms with stopsRunning: true)
	// Resent with same writeId -> it applies and returns 200 OK
	resp3 := putCourtJSON(t, r, compID, "m-run", map[string]any{
		"court":        "B",
		"stopsRunning": true,
		"writeId":      writeID,
	})
	require.Equal(t, http.StatusOK, resp3.Code, resp3.Body.String())

	// Verify match in store is now moved
	m, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, match := range m {
		if match.ID == "m-run" {
			assert.Equal(t, state.MatchStatusScheduled, match.Status)
			assert.Equal(t, "B", match.Court)
			assert.Equal(t, "09:59", match.ScheduledAt)
		}
	}
}

// (h) a resent move whose writeId is already in the score log broadcasts the
// court the store holds rather than the request's court if they differ.
func TestCourtMoveRunning_BroadcastNamesStoredCourtOnDuplicateWithDifferentRequestCourt(t *testing.T) {
	r, store, _, _, compID, _ := setupCourtMoveFixture(t)

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "m-run", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "11:00"},
		{ID: "m-b1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
	}))

	startResp := putScore(t, r, compID, "m-run", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "status": "running", "startOnly": true,
	})
	require.Equal(t, http.StatusOK, startResp.Code, startResp.Body.String())

	writeID := "w-dup-diff-court"
	resp := putCourtJSON(t, r, compID, "m-run", map[string]any{
		"court":        "B",
		"stopsRunning": true,
		"writeId":      writeID,
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	// Create a fresh router over the same store so the once.do cache is clear.
	freshHub := NewHub()
	t.Cleanup(freshHub.Close)
	freshRouter := gin.New()
	admin := freshRouter.Group("/api")
	eng := engine.New(store)
	RegisterMatchHandlers(admin, eng, store, store, freshHub, NewFileVerifier(store), store)

	freshCh := freshHub.Subscribe()
	defer freshHub.Unsubscribe(freshCh)

	// Resend the move with the same writeId but a different requested court "A".
	// The engine recognizes the logged writeId and is a no-op, leaving the match on court "B".
	// The broadcast must report the stored match's court "B", not the request's court "A".
	dupResp := putCourtJSON(t, freshRouter, compID, "m-run", map[string]any{
		"court":        "A",
		"stopsRunning": true,
		"writeId":      writeID,
	})
	require.Equal(t, http.StatusOK, dupResp.Code, dupResp.Body.String())

	events := drainHubEvents(t, freshCh, 500*time.Millisecond)
	require.NotEmpty(t, events, "fresh router must broadcast the stored match again")

	var matchUpdatedEv *SSEEvent
	for i := range events {
		if events[i].Type == EventMatchUpdated {
			matchUpdatedEv = &events[i]
		}
	}
	require.NotNil(t, matchUpdatedEv, "must push match_updated")

	data, ok := matchUpdatedEv.Data.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "B", data["court"], "broadcast top-level court must match store, not request court")
	assert.NotEqual(t, "A", data["court"])

	res, ok := data["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "B", res["court"])
}
