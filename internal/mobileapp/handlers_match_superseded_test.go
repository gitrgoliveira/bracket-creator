package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-lww1. The timestamp last-write-wins guard drops a write stamped older than
// the stored result. Before this fix the drop was reported as an unqualified
// success at every layer: the engine returned a nil error, the handler answered
// 200 with the operator's OWN echoed payload, and it broadcast that discarded
// payload over SSE. The losing operator's screen, the court display and every
// viewer therefore agreed on a result the disk never held.
//
// The scenario is the one the guard exists for: a court that scored a match
// offline reconnects and its queued write arrives after another device has
// already recorded a newer result for the same match.
func TestScoreHandler_SupersededWriteIsReportedNotSilentlyDropped(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "lww", Courts: []string{"A"}}))

	const storedAt, staleAt = 2_000_000, 1_000_000

	// The stored match carries a stamp NEWER than the incoming write while not
	// being completed. That combination is what the guard actually fences in
	// practice, and it is deliberate rather than convenient: a stored COMPLETED
	// result cannot reach the guard at all, because overwriting one requires a
	// correctionReason and a correction outranks the timestamp by design
	// (applyMatchWrite). The shape below is what RevertMatchToQueue leaves
	// behind — it stamps ModifiedAt = now() precisely so a queued pre-revert
	// result loses — and equally what a restart on another device leaves.
	require.NoError(t, store.SavePoolMatches("lww", []state.MatchResult{{
		ID: "lww-m1", SideA: "Alice", SideB: "Bob",
		Status: state.MatchStatusRunning, ModifiedAt: storedAt,
	}}))
	runningRevStore.Delete("lww:lww-m1")

	// Count broadcasts: the discarded result must reach NOBODY.
	var broadcasts atomic.Int64
	ch := hub.Subscribe()
	require.NotNil(t, ch)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range ch {
			broadcasts.Add(1)
		}
	}()

	// The reconnecting court's queued result: Bob won, stamped BEFORE the
	// stored one. It carries its enqueue-time stamp, not a fresh one — the SPA
	// stamps in recordScore and _flushQueue replays the payload verbatim.
	payload, _ := json.Marshal(map[string]any{
		"sideA": "Alice", "sideB": "Bob",
		"winner": "Bob", "ipponsA": []string{}, "ipponsB": []string{"M", "D"},
		"status": "completed", "modifiedAt": staleAt,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/lww/matches/lww-m1/score", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// 200, never 4xx/5xx: a superseded write is not a fault and can never win a
	// retry, and the SPA's offline queue retries 5xx forever.
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	// The heart of the bug: the body must SAY the write did not land. It used to
	// be the echoed request, so `winner` read "Bob" and the editor cleared to a
	// saved look.
	applied, ok := body["applied"]
	require.True(t, ok, "the response must state whether the write applied; got %s", w.Body.String())
	assert.Equal(t, false, applied)
	assert.Equal(t, "superseded", body["reason"])
	assert.NotEmpty(t, body["message"], "the operator needs something to read")
	assert.NotContains(t, w.Body.String(), "Bob\"", "the discarded payload must not be echoed back as if stored")

	hub.Unsubscribe(ch)
	wg.Wait()
	assert.Zero(t, broadcasts.Load(),
		"a dropped write must not be broadcast: doing so pushed the discarded result to every viewer and board")

	// And the disk still holds the newer result, untouched.
	stored, err := store.LoadPoolMatches("lww")
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Empty(t, stored[0].Winner, "the stale result must not have landed")
	assert.Equal(t, state.MatchStatusRunning, stored[0].Status, "the newer stored state stands")
	assert.EqualValues(t, storedAt, stored[0].ModifiedAt, "and keeps its stamp")
}

// The other half of the contract: a write that DOES land must not start
// reporting itself superseded, and must still broadcast. Without this the fix
// could pass its own test by rejecting everything.
func TestScoreHandler_AppliedWriteStillSucceedsAndBroadcasts(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "lww2", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("lww2", []state.MatchResult{{
		ID: "lww2-m1", SideA: "Alice", SideB: "Bob",
		Status: state.MatchStatusRunning, ModifiedAt: 1_000_000,
	}}))
	runningRevStore.Delete("lww2:lww2-m1")

	var broadcasts atomic.Int64
	ch := hub.Subscribe()
	require.NotNil(t, ch)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range ch {
			broadcasts.Add(1)
		}
	}()

	payload, _ := json.Marshal(map[string]any{
		"sideA": "Alice", "sideB": "Bob",
		"winner": "Bob", "ipponsA": []string{}, "ipponsB": []string{"M", "D"},
		"status": "completed", "modifiedAt": 2_000_000,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/lww2/matches/lww2-m1/score", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	_, hasApplied := body["applied"]
	assert.False(t, hasApplied, "a normal write answers with the result, not an applied verdict")
	assert.Equal(t, "Bob", body["winner"])

	hub.Unsubscribe(ch)
	wg.Wait()
	assert.NotZero(t, broadcasts.Load(), "an applied write must still reach the viewers")

	stored, err := store.LoadPoolMatches("lww2")
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, "Bob", stored[0].Winner)
}
