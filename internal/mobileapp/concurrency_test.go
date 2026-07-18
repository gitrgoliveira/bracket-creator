// Phase 12.E, T219: integration-level proof for A2 closure.
//
// The hub-level unit tests (broadcast_order_test.go) prove that the seq
// counter is unique and strictly monotonic under concurrent Broadcast
// calls. This test takes the same guarantee through the full handler
// stack: 10 goroutines hit the score endpoint for distinct matches on
// the same competition, and we assert:
//
//  1. All 10 HTTP requests return 200 (no deadlock, no lost write).
//  2. The hub's SSE channel sees match_updated events from all 10
//     scores, with strictly-monotonic seqs.
//  3. The on-disk state for each match equals the score sent by its
//     goroutine (no torn writes / lost updates).
//
// This complements TestScoreHandler_NoDeadlockUnderConcurrentLoad
// (T156 era); that test guarantees liveness, this one guarantees SSE
// ordering. Both should pass under `go test -race`.
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
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrentScoresPreserveOrder(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "score-concurrent-order-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	// Subscribe BEFORE wiring handlers so we don't miss any broadcasts.
	// The channel buffer (100) easily accommodates 10 score writes plus
	// the auto-complete-pools follow-up broadcast that fires once every
	// match in the comp is done.
	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)

	compID := "concurrent-order"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Format: state.CompFormatMixed,
		Status: state.CompStatusPools,
	}))
	const N = 10
	players := make([]domain.Player, 0, 2*N)
	matches := make([]state.MatchResult, 0, N)
	for i := range N {
		pa := domain.Player{ID: helper.NewUUID4(), Name: nameFor("A", i), Dojo: "DojoA"}
		pb := domain.Player{ID: helper.NewUUID4(), Name: nameFor("B", i), Dojo: "DojoB"}
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
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)

	// Goroutines all fire concurrently; each scores a different match
	// so the per-comp lock is contended on every step (LoadCompetition
	// to check encho cap, StartMatchTx, RecordMatchResultWithIneligibilityTx,
	// MaybeAdvanceKachinuki, tryAutoCompletePools).
	var wg sync.WaitGroup
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
			assert.Equalf(t, http.StatusOK, w.Code, "match %d returned %d: %s", idx, w.Code, w.Body.String())
		}(i)
	}

	// Drain SSE events concurrently with the score writes so we don't
	// block the hub's per-broadcast non-blocking send (channel cap 100
	// is well above 10*N events, but draining in real time exercises
	// the live-streaming path).
	type recvEvent struct {
		Seq           int64
		Type          EventType
		MatchID       string
		CompetitionID string
	}
	var recvMu sync.Mutex
	var events []recvEvent
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for {
			select {
			case msg, ok := <-sub:
				if !ok {
					return
				}
				var env struct {
					Type EventType `json:"type"`
					Seq  int64     `json:"seq"`
					Data struct {
						CompetitionID string `json:"competitionId"`
						MatchID       string `json:"matchId"`
						Result        struct {
							ID string `json:"id"`
						} `json:"result"`
					} `json:"data"`
				}
				if err := json.Unmarshal([]byte(msg.payload), &env); err != nil {
					t.Errorf("envelope decode failed: %v (raw: %s)", err, msg.payload)
					continue
				}
				matchID := env.Data.MatchID
				if matchID == "" {
					matchID = env.Data.Result.ID
				}
				recvMu.Lock()
				events = append(events, recvEvent{
					Seq:           env.Seq,
					Type:          env.Type,
					MatchID:       matchID,
					CompetitionID: env.Data.CompetitionID,
				})
				recvMu.Unlock()
			case <-time.After(2 * time.Second):
				return
			}
		}
	}()

	httpDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(httpDone)
	}()
	select {
	case <-httpDone:
	case <-time.After(10 * time.Second):
		t.Fatal("score writes did not complete within 10s; possible deadlock")
	}

	// Wait briefly for the drain goroutine to settle on the post-write
	// burst (auto-complete-pools may fire a final event) before
	// asserting the receive log.
	time.Sleep(150 * time.Millisecond)
	hub.Unsubscribe(sub)
	<-drainDone

	recvMu.Lock()
	collected := append([]recvEvent(nil), events...)
	recvMu.Unlock()

	// Assertion 1: every emitted seq is strictly monotonic in the
	// receive order. This is the A2 contract under multi-operator load.
	require.NotEmpty(t, collected, "no SSE events captured")
	prev := int64(0)
	seen := make(map[int64]bool, len(collected))
	for i, e := range collected {
		require.Falsef(t, seen[e.Seq], "duplicate seq %d at index %d (event %+v)", e.Seq, i, e)
		seen[e.Seq] = true
		require.Greaterf(t, e.Seq, prev, "seq %d at index %d not strictly greater than previous %d", e.Seq, i, prev)
		prev = e.Seq
	}

	// Assertion 2: at least one match_updated event per scored match.
	// (Auto-complete-pools may fire EventCompetitionCompleted at the
	// end; kachinuki advance may fire extra match_updated events. We
	// only care that every scored match shows up in the stream.)
	updatedMatches := make(map[string]bool)
	for _, e := range collected {
		if e.Type == EventMatchUpdated && e.MatchID != "" {
			updatedMatches[e.MatchID] = true
		}
	}
	for i := range N {
		assert.Truef(t, updatedMatches[poolMatchID(i)], "no match_updated event captured for match %s", poolMatchID(i))
	}

	// Assertion 3: on-disk state matches what each goroutine sent.
	final, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, final, N)
	for i := range N {
		var found bool
		for _, m := range final {
			if m.ID == poolMatchID(i) {
				found = true
				assert.Equalf(t, nameFor("A", i), m.Winner, "match %d on-disk winner mismatch", i)
				assert.Equalf(t, state.MatchStatusCompleted, m.Status, "match %d on-disk status mismatch", i)
			}
		}
		assert.Truef(t, found, "match %d missing from on-disk read", i)
	}
}
