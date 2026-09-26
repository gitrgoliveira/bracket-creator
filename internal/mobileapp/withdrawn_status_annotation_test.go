package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// annotateEligibility stamps, on a COMPLETED match a withdrawal or default win
// decided, where the withdrawn side's competitor status stands now, so the
// editor's clear control can word its consequence without fetching the
// statuses itself.
func TestAnnotateEligibility_WithdrawnStatus(t *testing.T) {
	statuses := map[string]domain.CompetitorStatus{
		"alice": {PlayerID: "alice", Eligible: false, MatchID: "Pool A-0", Reason: "kiken-voluntary at Pool A-0"},
		"bob":   {PlayerID: "bob", Eligible: false, MatchID: "Pool A-3", Reinstateable: true},
		"carol": {PlayerID: "carol", Eligible: true, MatchID: "m-r1-0"},
	}
	pool := []state.MatchResult{
		{ID: "Pool A-0", SideAID: "alice", SideBID: "dave", Status: state.MatchStatusCompleted, Decision: "kiken-voluntary", DecisionBy: "aka"},
		{ID: "Pool A-1", SideAID: "erin", SideBID: "bob", Status: state.MatchStatusCompleted, Decision: "fusensho", DecisionBy: "shiro"},
		{ID: "Pool A-2", SideAID: "alice", SideBID: "erin", Status: state.MatchStatusCompleted, WinnerID: "erin"},
		{ID: "Pool A-4", SideAID: "alice", SideBID: "frank", Status: state.MatchStatusScheduled},
	}
	bracket := &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "m-r1-0", SideAID: "carol", SideBID: "gina", Status: state.MatchStatusCompleted, Decision: "fusenpai", WinnerID: "gina"},
	}}}

	annotateEligibility(pool, bracket, statuses)

	require.NotNil(t, pool[0].WithdrawnStatus, "a kiken names the side that withdrew")
	assert.Equal(t, state.WithdrawnStatusAnnotation{Eligible: false, MatchID: "Pool A-0"}, *pool[0].WithdrawnStatus)
	require.NotNil(t, pool[1].WithdrawnStatus, "a default win names the barred side")
	assert.Equal(t, state.WithdrawnStatusAnnotation{Eligible: false, MatchID: "Pool A-3", Reinstateable: true}, *pool[1].WithdrawnStatus)
	assert.Nil(t, pool[2].WithdrawnStatus, "a fought match withdrew nobody")
	assert.Nil(t, pool[3].WithdrawnStatus, "a scheduled match is not decided")
	assert.NotNil(t, pool[3].IneligibleSides, "and keeps its own barred-side stamp")
	b := bracket.Rounds[0][0]
	require.NotNil(t, b.WithdrawnStatus, "with no decisionBy, the side that did not win")
	assert.Equal(t, state.WithdrawnStatusAnnotation{Eligible: true, MatchID: "m-r1-0"}, *b.WithdrawnStatus)
}

// pushRecorder keeps each match_updated push's result, keyed by match id.
type pushRecorder struct{ results map[string]*state.MatchResult }

func (p *pushRecorder) Broadcast(t EventType, data any) {
	if t != EventMatchUpdated {
		return
	}
	if h, ok := data.(gin.H); ok {
		if r, ok := h["result"].(*state.MatchResult); ok && r != nil {
			p.results[r.ID] = r
		}
	}
}

// The push that records a withdrawal carries the stamp too. A host applies the
// push at once and refetches a moment later, so a pushed row without it left
// an editor on that match wording its clear from no stamp until the refetch.
func TestDecisionPush_CarriesWithdrawnStatus(t *testing.T) {
	for _, door := range []string{"/decision", "/score"} {
		t.Run(door, func(t *testing.T) {
			store, err := state.NewStore(t.TempDir())
			require.NoError(t, err)
			eng := engine.New(store)
			compID := "withdrawn-status-push"
			require.NoError(t, store.SaveCompetition(&state.Competition{
				ID: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools,
			}))
			aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
			require.NoError(t, store.SaveParticipants(compID, []domain.Player{
				{ID: aliceID, Name: "Alice", Dojo: "A"},
				{ID: bobID, Name: "Bob", Dojo: "B"},
				{ID: carolID, Name: "Carol", Dojo: "C"},
			}))
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
				{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
				{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID, Status: state.MatchStatusScheduled},
			}))

			gin.SetMode(gin.TestMode)
			r := gin.New()
			pushes := &pushRecorder{results: map[string]*state.MatchResult{}}
			admin := r.Group("/api")
			registerScoreHandler(admin, eng, store, store, pushes, NewFileVerifier(store), store)
			RegisterDecisionHandlers(admin, eng, store, store, pushes)

			// Alice withdraws in Pool A-0, then does not appear for Pool A-1:
			// that no-show chains onto the withdrawal's bar (bc-kfup).
			resp := postDecisionJSON(t, r, compID, "Pool A-0", DecisionRequest{Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test"})
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
			if door == "/decision" {
				resp = postDecisionJSON(t, r, compID, "Pool A-1", DecisionRequest{Decision: "fusenpai", DecisionBy: "aka"})
			} else {
				resp = putScoreJSON(t, r, compID, "Pool A-1", state.MatchResult{
					ID: "Pool A-1", Status: state.MatchStatusCompleted, Decision: "fusenpai", DecisionBy: "aka",
					Winner: "Carol", IpponsB: domain.DefaultWinIppons(false),
				})
			}
			require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

			pushed := pushes.results["Pool A-1"]
			require.NotNil(t, pushed, "the no-show was pushed")
			require.NotNil(t, pushed.WithdrawnStatus, "the pushed no-show carries the stamp")
			assert.False(t, pushed.WithdrawnStatus.Eligible)
			assert.Equal(t, "Pool A-0", pushed.WithdrawnStatus.MatchID, "the bar is the withdrawal's")
		})
	}
}

// Bulk-score can record a no-show too, and its push carries the stamp the same
// way. The bulk handler is wired to the real hub, so this reads the push off a
// subscription.
func TestBulkScorePush_CarriesWithdrawnStatus(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := engine.New(store)
	compID := "withdrawn-status-bulk"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools,
	}))
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID, Status: state.MatchStatusScheduled},
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	hub := NewHub()
	defer hub.Close()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)

	resp := postDecisionJSON(t, r, compID, "Pool A-0", DecisionRequest{Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test"})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	sub := hub.Subscribe()
	defer hub.Unsubscribe(sub)
	body, err := json.Marshal([]state.MatchResult{{
		ID: "Pool A-1", Status: state.MatchStatusCompleted, Decision: "fusenpai", DecisionBy: "aka",
		Winner: "Carol", IpponsB: domain.DefaultWinIppons(false),
	}})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/matches/bulk-score", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sub:
			if !strings.Contains(ev.payload, `"match_updated"`) || !strings.Contains(ev.payload, `"Pool A-1"`) {
				continue
			}
			assert.Contains(t, ev.payload, `"withdrawnStatus":{"eligible":false,"matchId":"Pool A-0"}`,
				"the pushed no-show carries the stamp")
			return
		case <-deadline:
			t.Fatal("no match_updated push for Pool A-1")
		}
	}
}
