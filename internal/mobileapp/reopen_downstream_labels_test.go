// Package mobileapp, reopen_downstream_labels_test.go pins bc-cse's
// operator-facing wire shapes for the reopen family: a downstream match
// being fought answers 409 downstream_knockout_running (never the bare
// ErrReopenDownstreamFought text), and a busy-court refusal names the
// blocking match by its operator label rather than its internal id.
package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestReopenHandler_DownstreamRunning_RespondsWithLabel reopens a semifinal
// whose winner already fed a RUNNING final: the refusal must be 409
// downstream_knockout_running (engine.DownstreamKnockoutRunningError), with
// the final named by its operator label in the message, not the raw
// ErrReopenDownstreamFought sentinel text.
func TestReopenHandler_DownstreamRunning_RespondsWithLabel(t *testing.T) {
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	compID := "reopen-running-downstream"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Kind: "individual", Status: state.CompStatusKnockout,
	}))
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideA: "Alice", SideAID: aliceID, SideB: "Bob", SideBID: bobID,
			Status: state.MatchStatusRunning, DisplayRound: 2, MatchNumber: 1}},
		{{ID: "m-r2-0", SideB: "Carol", SideBID: carolID, DisplayRound: 1, MatchNumber: 2}},
	}}))
	_, _, err = eng.RecordDecision(compID, "m-r1-0", "fusenpai", "shiro", "", nil, false)
	require.NoError(t, err)
	require.NoError(t, store.UpdateBracket(compID, func(b *state.Bracket) error {
		b.Rounds[1][0].Status = state.MatchStatusRunning
		b.Rounds[1][0].IpponsA = []string{"M"}
		return nil
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)

	body, _ := json.Marshal(map[string]any{"reason": "Withdrawal recorded by mistake"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/matches/m-r1-0/reopen", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "downstream_knockout_running", resp["error"])
	assert.Equal(t, "m-r1-0", resp["matchId"])
	msg, _ := resp["message"].(string)
	assert.Contains(t, msg, "Match 2 (Final)", "the running downstream is named by its operator label")
	assert.NotContains(t, msg, "cannot reopen: a downstream knockout match has already started",
		"the bare ErrReopenDownstreamFought text must not reach the operator")
	// bc-cse item 7: the reopen door's own remedy, never the save-path's
	// "then save again" -- a reopen has no save step to retry.
	assert.Equal(t,
		"Match 2 (Final) is being fought now. Finish it or send it back to the queue, then reopen this match again.",
		msg)
	assert.NotContains(t, msg, "then save again",
		"a reopen refusal must never tell the operator to save again")
	running, ok := resp["runningMatches"].([]any)
	require.True(t, ok && len(running) == 1)
	first := running[0].(map[string]any)
	assert.Equal(t, "m-r2-0", first["id"])
	assert.Equal(t, "Match 2 (Final)", first["label"])
}

// TestScoreHandler_CourtBusy_BodyCarriesLabelAndCourtNoRawID pins the
// court_busy wire shape (bc-cse): `label` and `court` are present, and the
// prose `message` never names the blocking match's internal id, only its
// operator label.
func TestScoreHandler_CourtBusy_BodyCarriesLabelAndCourtNoRawID(t *testing.T) {
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	compID := "court-busy-label"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	aliceID, bobID, carolID, davidID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
		{ID: davidID, Name: "David", Dojo: "D"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID,
			Status: state.MatchStatusRunning, Court: "A"},
		{ID: "Pool A-1", SideA: "Carol", SideB: "David", SideAID: carolID, SideBID: davidID,
			Status: state.MatchStatusScheduled, Court: "A"},
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)

	resp := putScoreJSON(t, r, compID, "Pool A-1", state.MatchResult{ID: "Pool A-1", Status: state.MatchStatusRunning})
	require.Equal(t, http.StatusConflict, resp.Code, resp.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "court_busy", body["error"])
	assert.Equal(t, "A", body["court"])
	assert.Equal(t, "Pool A-0", body["matchId"], "the id stays in the body for the client's own use")
	assert.Equal(t, "Pool A · Match 1", body["label"])
	msg, _ := body["message"].(string)
	assert.Contains(t, msg, "Pool A · Match 1")
	assert.False(t, strings.Contains(msg, "Pool A-0"), "the message must not name the internal match id")
}

// TestReopenHandler_ThirdPlaceMatchNotCompleted_MessageIsSentenceCased
// covers bc-cse item 6: engine.MatchLabel names the 3rd-place match
// deliberately lowercase mid-sentence ("the 3rd-place match"), which reads
// wrong the moment it opens a sentence ("%s is not completed yet..."). The
// reopen handler's ErrReopenNotCompleted refusal must sentence-case it.
func TestReopenHandler_ThirdPlaceMatchNotCompleted_MessageIsSentenceCased(t *testing.T) {
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	compID := "reopen-bronze-not-completed"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Kind: "individual", Status: state.CompStatusKnockout,
	}))
	aliceID, bobID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		ThirdPlaceMatch: &state.BracketMatch{
			ID: state.BronzeMatchID, SideA: "Alice", SideAID: aliceID, SideB: "Bob", SideBID: bobID,
			Status: state.MatchStatusScheduled,
		},
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)

	body, _ := json.Marshal(map[string]any{})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/matches/"+state.BronzeMatchID+"/reopen", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	msg, _ := resp["error"].(string)
	assert.Equal(t, "The 3rd-place match is not completed yet, so there is nothing to reopen.", msg)
}
