// Package mobileapp — handlers_match_tx_test.go pins the T156
// migration invariants on the score and decision handlers under
// concurrent load. These tests are the safety net against future
// regressions where someone introduces a sibling engine method that
// silently re-acquires the per-comp lock from inside the tx body.
package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScoreHandler_NoDeadlockUnderConcurrentLoad spawns N goroutines
// hitting the score endpoint for distinct matches in the same
// competition and asserts they all return within a bounded time.
// Pre-T156 the handler ran three lock-acquiring engine calls back to
// back; mixing in MaybeAdvanceKachinuki and tryAutoCompletePools made
// the concurrency surface large enough that a future tx-aware
// refactor could easily introduce a deadlock without anyone
// noticing.
func TestScoreHandler_NoDeadlockUnderConcurrentLoad(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "score-deadlock-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	compID := "concurrent-score"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Format: state.CompFormatPools,
		Status: state.CompStatusPools,
	}))
	const N = 8
	players := make([]helper.Player, 0, 2*N)
	matches := make([]state.MatchResult, 0, N)
	for i := range N {
		pa := helper.Player{ID: helper.NewUUID4(), Name: nameFor("A", i), Dojo: "DojoA"}
		pb := helper.Player{ID: helper.NewUUID4(), Name: nameFor("B", i), Dojo: "DojoB"}
		players = append(players, pa, pb)
		matches = append(matches, state.MatchResult{
			ID:     poolMatchID(i),
			SideA:  pa.Name,
			SideB:  pb.Name,
			Status: state.MatchStatusScheduled,
		})
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, store.SavePoolMatches(compID, matches))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub)

	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(N)
	for i := range N {
		go func(idx int) {
			defer wg.Done()
			body, _ := json.Marshal(state.MatchResult{
				ID:     poolMatchID(idx),
				Winner: nameFor("A", idx),
				Status: state.MatchStatusCompleted,
			})
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/matches/"+poolMatchID(idx)+"/score", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "match %d failed: %s", idx, w.Body.String())
		}(i)
	}
	go func() {
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("score handler deadlocked under concurrent load — T156 migration regressed")
	}

	// Final check: every match landed on disk with the recorded winner.
	final, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, final, N)
	for i := range N {
		var found bool
		for _, m := range final {
			if m.ID == poolMatchID(i) {
				found = true
				assert.Equal(t, nameFor("A", i), m.Winner, "match %d had wrong winner", i)
				assert.Equal(t, state.MatchStatusCompleted, m.Status, "match %d had wrong status", i)
			}
		}
		assert.True(t, found, "match %d missing from final read", i)
	}
}

// TestDecisionHandler_NoDeadlockOnConcurrentKiken hits the decision
// endpoint concurrently with two operators trying to kiken the same
// player from different matches. The T156 migration must preserve the
// T105/CHK047 concurrent-kiken behaviour: exactly one succeeds, the
// other returns 409 already_ineligible, and the losing match rolls
// back its partial score-write. Throughout, the per-comp lock under
// WithTransaction must never deadlock.
func TestDecisionHandler_NoDeadlockOnConcurrentKiken(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "decision-deadlock-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	compID := "concurrent-kiken-handler"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Format: state.CompFormatPools,
		Status: state.CompStatusPools,
	}))
	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	carolID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []helper.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Carol", SideB: "Alice", Status: state.MatchStatusScheduled},
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterDecisionHandlers(admin, eng, store, store, hub)

	type res struct {
		code    int
		body    string
		matchID string
	}
	results := make(chan res, 2)
	postDecision := func(mid, decisionBy string) {
		body, _ := json.Marshal(DecisionRequest{
			Decision:       "kiken",
			DecisionBy:     decisionBy,
			DecisionReason: "concurrent",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/matches/"+mid+"/decision", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		results <- res{code: w.Code, body: w.Body.String(), matchID: mid}
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); postDecision("Pool A-0", "aka") }()
	go func() { defer wg.Done(); postDecision("Pool A-1", "shiro") }()
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("decision handler deadlocked under concurrent kiken — T156 migration regressed")
	}

	var winners, losers []res
	for range 2 {
		r := <-results
		switch r.code {
		case http.StatusOK:
			winners = append(winners, r)
		case http.StatusConflict:
			losers = append(losers, r)
		default:
			t.Fatalf("unexpected status code %d for match %s: %s", r.code, r.matchID, r.body)
		}
	}
	require.Len(t, winners, 1, "exactly one decision should succeed; got winners=%+v losers=%+v", winners, losers)
	require.Len(t, losers, 1, "exactly one decision should be rejected with 409")
	assert.Contains(t, losers[0].body, "already_ineligible", "loser should see already_ineligible body, got %s", losers[0].body)
}

func nameFor(side string, i int) string {
	return "Player" + side + string('0'+rune(i))
}

func poolMatchID(i int) string {
	return "Pool A-" + string('0'+rune(i))
}
