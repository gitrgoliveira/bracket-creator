package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
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

// ---------------------------------------------------------------------------
// matchBroadcastCoalescer unit tests
// ---------------------------------------------------------------------------

func TestMatchBroadcastCoalescer_Allow(t *testing.T) {
	t.Run("first call always allowed", func(t *testing.T) {
		c := newMatchBroadcastCoalescer()
		assert.True(t, c.Allow("m1", true))
	})

	t.Run("second call within window is coalesced", func(t *testing.T) {
		c := newMatchBroadcastCoalescer()
		assert.True(t, c.Allow("m1", true))
		assert.False(t, c.Allow("m1", true), "should be coalesced within 250ms")
	})

	t.Run("call after window proceeds", func(t *testing.T) {
		c := newMatchBroadcastCoalescer()
		assert.True(t, c.Allow("m1", true))
		// Force the last timestamp to be old.
		c.mu.Lock()
		c.last["m1"] = time.Now().Add(-300 * time.Millisecond)
		c.mu.Unlock()
		assert.True(t, c.Allow("m1", true), "should proceed after window expires")
	})

	t.Run("non-running writes always allowed regardless of window", func(t *testing.T) {
		c := newMatchBroadcastCoalescer()
		assert.True(t, c.Allow("m1", false))
		assert.True(t, c.Allow("m1", false), "completed writes must never be coalesced")
	})

	t.Run("different matches have independent windows", func(t *testing.T) {
		c := newMatchBroadcastCoalescer()
		assert.True(t, c.Allow("m1", true))
		assert.False(t, c.Allow("m1", true)) // m1 coalesced
		assert.True(t, c.Allow("m2", true))  // m2 independent
		assert.False(t, c.Allow("m2", true)) // m2 now coalesced
	})

	t.Run("competition-scoped keys are independent", func(t *testing.T) {
		c := newMatchBroadcastCoalescer()
		// Same bare match id "Pool A-1" in two different competitions must not
		// share a coalesce window — the call site keys by compID:matchID.
		assert.True(t, c.Allow("comp1:Pool A-1", true))
		assert.False(t, c.Allow("comp1:Pool A-1", true)) // comp1 coalesced
		assert.True(t, c.Allow("comp2:Pool A-1", true))  // comp2 independent
	})
}

// ---------------------------------------------------------------------------
// BenchmarkScoreWrite_TwoCourts — measures per-comp-lock serialisation when
// two courts of the SAME competition write running-status scores concurrently.
//
// C3 concern: the per-comp mutex (state.WithTransaction) is non-reentrant
// and held for the entire engine write + ineligibility update + broadcast.
// Two courts on the same competition must queue. This benchmark measures
// whether that serialisation is a bottleneck at the autosave rate (≤3.3/s
// per court after C1's 300ms debounce).
//
// Result (2026-06-15, M1 Pro, -10 GOMAXPROCS): ~47ms/op for a two-court write
// PAIR, dominated by the engine's per-write CSV persist to the temp data dir
// (real disk I/O), not by lock contention — the per-comp mutex is held only
// for the in-memory critical section. At the ≤3.3 writes/s-per-court autosave
// rate this is comfortably non-blocking. No tuning required.
//
// (An earlier draft used a per-goroutine fixed rev; under RunParallel that made
// many out-of-order writes hit the rev-guard's stale fast path — skipping the
// engine write entirely — so it under-measured. The constant rev below ensures
// every request traverses the full guard + lock + engine path.)
// ---------------------------------------------------------------------------
func BenchmarkScoreWrite_TwoCourts(b *testing.B) {
	r, store, _, _, tempDir := setupTestRouter(b)
	defer os.RemoveAll(tempDir)

	require.NoError(b, store.SaveTournament(&state.Tournament{
		Name: "Bench", Password: "", Courts: []string{"A", "B"},
	}))
	require.NoError(b, store.SaveCompetition(&state.Competition{
		ID: "bench1", Courts: []string{"A", "B"},
	}))
	require.NoError(b, store.SavePoolMatches("bench1", []state.MatchResult{
		{ID: "courtA-1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
		{ID: "courtB-1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusRunning},
	}))
	// Reset the rev store so the benchmark starts clean.
	runningRevStore.Delete("bench1:courtA-1")
	runningRevStore.Delete("bench1:courtB-1")

	// A FIXED rev (not a monotonic per-write counter) is intentional here. Under
	// b.RunParallel, monotonically-increasing revs would arrive out of order, so
	// a lower rev landing after a higher one would be stale-rejected by the
	// rev-guard on the fast path — skipping the engine write + per-comp lock
	// entirely, which is exactly the cost this benchmark measures. A constant rev
	// (equal revs always proceed) makes every request traverse the full
	// guard + lock + engine path, giving a representative serialisation cost.
	const benchRev = int64(1)
	makeRunningWrite := func(matchID, sideA, sideB string) func() {
		return func() {
			payload, _ := json.Marshal(map[string]any{
				"sideA": sideA, "sideB": sideB,
				"ipponsA": []string{"M"}, "ipponsB": []string{},
				"status":     "running",
				"rev":        benchRev,
				"revSession": "bench-sess",
			})
			req, _ := http.NewRequest("PUT",
				fmt.Sprintf("/api/competitions/bench1/matches/%s/score", matchID),
				bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
		}
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		// Each goroutine alternates between the two courts.
		courtA := makeRunningWrite("courtA-1", "Alice", "Bob")
		courtB := makeRunningWrite("courtB-1", "Carol", "Dave")
		for pb.Next() {
			courtA()
			courtB()
		}
	})
}

// ---------------------------------------------------------------------------
// TestScoreHandler_C3Coalescer — integration: coalesced running writes
// do not broadcast but completed writes always do.
// ---------------------------------------------------------------------------
func TestScoreHandler_C3Coalescer(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c3", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("c3", []state.MatchResult{
		{ID: "c3-m1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning},
	}))
	runningRevStore.Delete("c3:c3-m1")

	// Count how many match_updated broadcasts the hub receives.
	var broadcastCount atomic.Int64
	// Subscribe to the hub so we can count broadcasts.
	ch := hub.Subscribe()
	require.NotNil(t, ch)
	defer hub.Unsubscribe(ch)

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range ch {
			broadcastCount.Add(1)
		}
	}()

	scoreRunning := func(rev int64) int {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"sideA": "Alice", "sideB": "Bob",
			"ipponsA": []string{"M"}, "ipponsB": []string{},
			"status": "running", "rev": rev,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c3/matches/c3-m1/score", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}

	scoreCompleted := func() int {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"sideA": "Alice", "sideB": "Bob",
			"winner": "Alice", "ipponsA": []string{"M", "K"}, "ipponsB": []string{},
			"status": "completed",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/c3/matches/c3-m1/score", bytes.NewBuffer(payload))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w.Code
	}

	// First running write — should broadcast.
	assert.Equal(t, http.StatusOK, scoreRunning(1))
	// Second running write immediately after — should be coalesced (no broadcast).
	assert.Equal(t, http.StatusOK, scoreRunning(2))

	// The first write's broadcast reaches the subscriber goroutine
	// asynchronously; wait on the observable count rather than a fixed sleep
	// (flaky on slow/contended CI). The second write is coalesced synchronously
	// in the handler (no broadcast enqueued), so the count settles at exactly 1.
	require.Eventually(t, func() bool { return broadcastCount.Load() == 1 }, time.Second, time.Millisecond,
		"first running write should broadcast exactly once; second is coalesced within 250ms")

	// Completed write — always broadcasts regardless of coalesce window.
	assert.Equal(t, http.StatusOK, scoreCompleted())
	require.Eventually(t, func() bool { return broadcastCount.Load() >= 2 }, time.Second, time.Millisecond,
		"completed write must always broadcast")
	hub.Unsubscribe(ch)
	wg.Wait()
}
