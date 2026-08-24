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
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-cse. A client stamp implausibly far in the FUTURE used to be clamped to 0,
// and 0 is ApplyByTimestamp's unstamped bypass: the write then applied
// UNCONDITIONALLY, silently overwriting a newer stored result. So the one shape
// the guard could not fence was the one that beat it every time, and a device
// with a fast clock won every race a flaky network put it in.
//
// It is now refused and reported: nothing is written, nothing is broadcast, and
// the operator is told to let the app resync and re-enter the result.
func TestScoreHandler_FutureStampIsRefusedNotSilentlyApplied(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "skew", Courts: []string{"A"}}))

	// The stored result is fresh and stamped. Note it is NOT what makes this
	// test pass or fail: a zeroed incoming stamp bypasses the comparison
	// entirely, so before the fix this write landed whatever the stored stamp
	// said. That is the point.
	storedAt := time.Now().UnixMilli()
	require.NoError(t, store.SavePoolMatches("skew", []state.MatchResult{{
		ID: "skew-m1", SideA: "Alice", SideB: "Bob",
		Status: state.MatchStatusRunning, ModifiedAt: storedAt,
	}}))
	runningRevStore.Delete("skew:skew-m1")

	// Count broadcasts: a refused write must reach NOBODY, exactly as the
	// superseded arm skips its broadcast.
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
		"status": "completed", "modifiedAt": time.Now().UnixMilli() + 10_000,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/skew/matches/skew-m1/score", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// 200, never 4xx/5xx: the SPA's offline queue retries 5xx forever and a
	// skewed write cannot win a retry while the clock is still wrong.
	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	applied, ok := body["applied"]
	require.True(t, ok, "the response must state whether the write applied; got %s", w.Body.String())
	assert.Equal(t, false, applied)
	assert.Equal(t, "clock_skew", body["reason"],
		"the client must be able to tell this from a supersede: the remedy differs (resync and re-enter, vs someone else won)")
	assert.NotEmpty(t, body["message"], "the operator needs something to read")
	serverNow, ok := body["serverNowMs"].(float64)
	require.True(t, ok, "the body must carry the server clock as diagnostic data; got %s", w.Body.String())
	assert.InDelta(t, float64(time.Now().UnixMilli()), serverNow, 60_000,
		"serverNowMs must be a plausible unix-ms reading")
	assert.NotContains(t, w.Body.String(), "\"winner\"", "the discarded payload must not be echoed back as if stored")

	hub.Unsubscribe(ch)
	wg.Wait()
	assert.Zero(t, broadcasts.Load(),
		"a refused write must not be broadcast: doing so pushes a result the disk never held to every viewer and board")

	// The disk is untouched. This is the assertion that goes red when the
	// refusal is reverted to the old clamp-to-zero.
	stored, err := store.LoadPoolMatches("skew")
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Empty(t, stored[0].Winner, "the skewed result must not have landed")
	assert.Equal(t, state.MatchStatusRunning, stored[0].Status, "the stored state stands")
	assert.EqualValues(t, storedAt, stored[0].ModifiedAt, "and keeps its stamp")
}

// The counter-cases. Without them the refusal could pass its own test by
// refusing everything, or by refusing the whole future half-line (which would
// break every client whose learned offset carries honest residual drift), or by
// refusing unstamped writes (which would break every legacy client and every
// server-built write).
func TestScoreHandler_ClockSkewBoundaries(t *testing.T) {
	// write drives one PUT /score against a freshly-stored match stamped a
	// minute in the past, so the last-write-wins comparison itself can never be
	// what drops the write; only the skew refusal can.
	write := func(t *testing.T, comp string, modifiedAt any) (int, map[string]any, *state.Store) {
		t.Helper()
		r, store, _, _, tempDir := setupTestRouter(t)
		t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: comp, Courts: []string{"A"}}))
		require.NoError(t, store.SavePoolMatches(comp, []state.MatchResult{{
			ID: comp + "-m1", SideA: "Alice", SideB: "Bob",
			Status: state.MatchStatusRunning, ModifiedAt: time.Now().UnixMilli() - 60_000,
		}}))
		runningRevStore.Delete(comp + ":" + comp + "-m1")

		fields := map[string]any{
			"sideA": "Alice", "sideB": "Bob",
			"winner": "Bob", "ipponsA": []string{}, "ipponsB": []string{"M", "D"},
			"status": "completed",
		}
		if modifiedAt != nil {
			fields["modifiedAt"] = modifiedAt
		}
		payload, _ := json.Marshal(fields)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+comp+"/matches/"+comp+"-m1/score", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		var body map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "body: %s", w.Body.String())
		return w.Code, body, store
	}

	assertLanded := func(t *testing.T, store *state.Store, comp string) {
		t.Helper()
		stored, err := store.LoadPoolMatches(comp)
		require.NoError(t, err)
		require.Len(t, stored, 1)
		assert.Equal(t, "Bob", stored[0].Winner, "the write must have landed")
	}

	t.Run("a stamp inside the tolerance is honoured as-is", func(t *testing.T) {
		// 3s ahead: past the old 2s clamp threshold, inside the 5s refusal
		// margin. Honest offset drift plus request jitter lives here, and the
		// operator's decision is that it is written, not refused.
		code, body, store := write(t, "inband", time.Now().UnixMilli()+3_000)
		require.Equal(t, http.StatusOK, code)
		assert.NotEqual(t, "clock_skew", body["reason"], "3s ahead is inside the margin; body: %v", body)
		assert.Equal(t, "Bob", body["winner"], "an honoured write answers with the result")
		assertLanded(t, store, "inband")
	})

	t.Run("a stamp just past the margin is refused", func(t *testing.T) {
		code, body, store := write(t, "outband", time.Now().UnixMilli()+6_000)
		require.Equal(t, http.StatusOK, code)
		assert.Equal(t, "clock_skew", body["reason"], "6s ahead is past the 5s margin; body: %v", body)
		assert.Equal(t, false, body["applied"])

		stored, err := store.LoadPoolMatches("outband")
		require.NoError(t, err)
		assert.Empty(t, stored[0].Winner, "nothing may be written past the margin")
	})

	t.Run("an unstamped write still applies", func(t *testing.T) {
		// The unstamped bypass must survive: legacy clients, and every
		// server-built write, carry no stamp at all.
		code, body, store := write(t, "nostamp", nil)
		require.Equal(t, http.StatusOK, code)
		assert.NotEqual(t, "clock_skew", body["reason"], "an unstamped write is not skew; body: %v", body)
		assertLanded(t, store, "nostamp")
	})

	t.Run("a past stamp still applies", func(t *testing.T) {
		code, body, store := write(t, "past", time.Now().UnixMilli()-1_000)
		require.Equal(t, http.StatusOK, code)
		assert.NotEqual(t, "clock_skew", body["reason"], "body: %v", body)
		assertLanded(t, store, "past")
	})
}

// bulk-score reports per-entry failures inside an overall 200, so a skewed entry
// cannot use the shared body. It gets the same machine-readable reason in its
// errs[] slot instead, and must not take the good entries down with it.
func TestBulkScore_SkewedEntryIsRefusedPerEntry(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "bulk", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("bulk", []state.MatchResult{
		{ID: "bulk-m1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
		{ID: "bulk-m2", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusRunning},
		{ID: "bulk-m3", SideA: "Erin", SideB: "Frank", Status: state.MatchStatusRunning},
	}))

	now := time.Now().UnixMilli()
	payload, _ := json.Marshal([]map[string]any{
		{"id": "bulk-m1", "sideA": "Alice", "sideB": "Bob", "winner": "Alice",
			"ipponsA": []string{"M"}, "ipponsB": []string{}, "status": "completed", "modifiedAt": now},
		{"id": "bulk-m2", "sideA": "Carol", "sideB": "Dave", "winner": "Carol",
			"ipponsA": []string{"M"}, "ipponsB": []string{}, "status": "completed", "modifiedAt": now + 10_000},
		{"id": "bulk-m3", "sideA": "Erin", "sideB": "Frank", "winner": "Erin",
			"ipponsA": []string{"M"}, "ipponsB": []string{}, "status": "completed", "modifiedAt": now},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/bulk/matches/bulk-score", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var body struct {
		Succeeded int `json:"succeeded"`
		Errors    []struct {
			MatchID string `json:"matchId"`
			Error   string `json:"error"`
			Reason  string `json:"reason"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))

	assert.Equal(t, 2, body.Succeeded, "the good entries must still land; body: %s", w.Body.String())
	require.Len(t, body.Errors, 1, "exactly the skewed entry must fail; body: %s", w.Body.String())
	assert.Equal(t, "bulk-m2", body.Errors[0].MatchID)
	assert.Equal(t, "clock_skew", body.Errors[0].Reason,
		"an importer must be able to tell this from a genuine rejection: re-running the import would not fix a wrong clock")
	assert.NotEmpty(t, body.Errors[0].Error, "the free-text message is what an operator reads")

	stored, err := store.LoadPoolMatches("bulk")
	require.NoError(t, err)
	byID := map[string]state.MatchResult{}
	for _, m := range stored {
		byID[m.ID] = m
	}
	assert.Equal(t, "Alice", byID["bulk-m1"].Winner)
	assert.Empty(t, byID["bulk-m2"].Winner, "the skewed entry must not have landed")
	assert.Equal(t, "Erin", byID["bulk-m3"].Winner)
}

// override-winner takes the same refusal, before the engine is called. Its
// normal 200 is already {"applied": <bool>}, so the refusal widens that body
// with the reason rather than changing its shape.
func TestOverrideWinner_SkewedStampIsRefused(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "ow", Courts: []string{"A"}}))
	require.NoError(t, store.SaveBracket("ow", &state.Bracket{
		Rounds: [][]state.BracketMatch{{{ID: "b1", SideA: "P1", SideB: "P2"}}},
	}))

	reqBody, _ := json.Marshal(map[string]any{
		"winnerName": "P1",
		"modifiedAt": time.Now().UnixMilli() + 10_000,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/ow/matches/b1/override-winner", bytes.NewBuffer(reqBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, false, body["applied"])
	assert.Equal(t, "clock_skew", body["reason"])
	assert.NotEmpty(t, body["serverNowMs"])
	assert.NotEmpty(t, body["message"])

	stored, err := store.LoadBracket("ow")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Empty(t, stored.Rounds[0][0].Winner, "the override must not have been applied")
}

// The rev-store half of the refusal, mirroring
// TestScoreHandler_SupersededWriteFollowsTheRevStoreRule. The superseded arm
// runs AFTER the rev guard, so it has a high-water mark to decide about; the
// clock-skew arm is deliberately EARLIER, before the engine and before the rev
// guard stores anything, so a refused write must leave the map exactly as it
// found it.
//
// It matters because runningRevStore is process-lifetime and keyed by
// competition:match. A refusal that recorded a mark would fence the operator's
// own next write from the same session behind a rev the server never accepted a
// result for, and the entry would never be cleaned up (nothing else runs for
// this match). A skewed device would poison its own recovery.
func TestScoreHandler_ClockSkewRefusalRecordsNoRunningRev(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "skewrev", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("skewrev", []state.MatchResult{{
		ID: "skewrev-m1", SideA: "Alice", SideB: "Bob",
		Status: state.MatchStatusRunning,
	}}))

	key := "skewrev:skewrev-m1"
	// Start from nothing, so anything present afterwards was written by THIS
	// request rather than inherited from another test in the package.
	runningRevStore.Delete(key)
	t.Cleanup(func() { runningRevStore.Delete(key) })

	// A RUNNING write carrying the session fence fields: exactly the shape that
	// makes the rev guard store a high-water mark on the accepted path.
	payload, _ := json.Marshal(map[string]any{
		"sideA": "Alice", "sideB": "Bob",
		"status": "running", "modifiedAt": time.Now().UnixMilli() + 10_000,
		"rev": 7, "revSession": "sess-skew",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/skewrev/matches/skewrev-m1/score", bytes.NewBuffer(payload))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	require.Equal(t, "clock_skew", body["reason"],
		"the fixture must actually reach the refusal, or this pins nothing; body: %s", w.Body.String())
	assert.Equal(t, false, body["applied"])

	_, present := runningRevStore.Load(key)
	assert.False(t, present,
		"a refused write must record no high-water mark: it would fence this session's own later writes behind a rev nothing was ever stored for")
}
