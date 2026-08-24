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
	// being completed. That is the shape RevertMatchToQueue leaves behind — it
	// stamps ModifiedAt = now() precisely so a queued pre-revert result loses —
	// and equally what a restart on another device leaves.
	//
	// It used to be the ONLY shape that could reach the guard: overwriting a
	// stored COMPLETED result requires a correctionReason, and a correction
	// bypassed the timestamp outright. That bypass is gone (see applyMatchWrite:
	// it could not tell a live correction from one the offline queue replayed
	// hours later, so a stale correction silently overwrote a newer result), so
	// a stale correction over a completed result now reaches the guard too and
	// is fenced like anything else.
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

// The superseded branch's rev-store rule, whose BOTH halves were previously
// unpinned: mutating the condition to `if true` or to `if false` left the whole
// package green. It is a small rule but it decides whether a client session's
// out-of-order-delivery fence survives a drop, so getting it backwards either
// leaks an entry for the process lifetime or silently disarms the fence.
//
// The rule mirrors the success path: a match that has LEFT running has no live
// high-water mark worth keeping, while a still-running match must keep its
// entry so a later out-of-order delivery from the same session is still fenced.
func TestScoreHandler_SupersededWriteFollowsTheRevStoreRule(t *testing.T) {
	const storedAt, staleAt = 2_000_000, 1_000_000

	// Both cases drive a write the guard will drop; they differ only in the
	// status the write carries, which is exactly what the rule keys on.
	newSupersededRequest := func(t *testing.T, status string) (*state.Store, string) {
		t.Helper()
		r, store, _, _, tempDir := setupTestRouter(t)
		t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "rev", Courts: []string{"A"}}))
		require.NoError(t, store.SavePoolMatches("rev", []state.MatchResult{{
			ID: "rev-m1", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusRunning, ModifiedAt: storedAt,
		}}))

		key := "rev:rev-m1"
		// Seed a high-water mark, as a prior running write from this session
		// would have left.
		runningRevStore.Store(key, runningRev{Session: "sess-1", Rev: 5})

		payload, _ := json.Marshal(map[string]any{
			"sideA": "Alice", "sideB": "Bob",
			"status": status, "modifiedAt": staleAt,
			"rev": 6, "revSession": "sess-1",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/rev/matches/rev-m1/score", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		require.Equal(t, "superseded", body["reason"],
			"the fixture must actually reach the guard, or this pins nothing; body: %s", w.Body.String())
		return store, key
	}

	t.Run("a superseded RUNNING write keeps the session fence", func(t *testing.T) {
		_, key := newSupersededRequest(t, "running")
		_, present := runningRevStore.Load(key)
		assert.True(t, present,
			"the match is still running, so a later out-of-order write from this session must still be fenced")
		runningRevStore.Delete(key)
	})

	t.Run("a superseded write that leaves running drops the entry", func(t *testing.T) {
		_, key := newSupersededRequest(t, "completed")
		_, present := runningRevStore.Load(key)
		assert.False(t, present,
			"the match has left running, so keeping its high-water mark leaks for the process lifetime")
	})
}
