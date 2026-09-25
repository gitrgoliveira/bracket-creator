// Package mobileapp, decision_both_barred_hikiwake_test.go pins bc-cse item
// 10: POST /decision accepts {"decision":"hikiwake"} for a scheduled pool
// match whose both sides are already barred (a completed draw, no winner,
// no eligibility status written), refuses it exactly as before for every
// other shape, and a knockout match with both sides barred gets its own
// "Neither ... can fight" sentence instead of naming one side.
package mobileapp

import (
	"encoding/json"
	"net/http"
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

// bothBarredPoolFixture builds a pool competition where Alice is barred by a
// kiken-voluntary on "Pool A-0" (Alice vs Bob) and Dave is barred by a
// SEPARATE kiken-voluntary on "Pool A-1" (Dave vs Carol). "Pool A-2" (Alice
// vs Dave) is left scheduled -- both of its competitors are already barred,
// each by an unrelated earlier match. "Pool A-3" (Bob vs Carol, neither ever
// barred) is left scheduled too, for the non-qualifying-hikiwake refusal
// tests.
func bothBarredPoolFixture(t *testing.T, compID string) (*gin.Engine, *state.Store, *engine.Engine) {
	t.Helper()
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools,
	}))
	aliceID, bobID, carolID, daveID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
		{ID: daveID, Name: "Dave", Dojo: "D"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Dave", SideB: "Carol", SideAID: daveID, SideBID: carolID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-2", SideA: "Alice", SideB: "Dave", SideAID: aliceID, SideBID: daveID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-3", SideA: "Bob", SideB: "Carol", SideAID: bobID, SideBID: carolID, Status: state.MatchStatusScheduled},
		// Only Alice is barred here (via Pool A-0); Carol never is -- the
		// ONE-side-barred refusal case.
		{ID: "Pool A-4", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID, Status: state.MatchStatusScheduled},
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)

	// Alice (aka/SideA) withdraws from Pool A-0; Bob credited.
	resp := postDecisionJSON(t, r, compID, "Pool A-0", DecisionRequest{
		Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test",
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	// Dave (aka/SideA) withdraws from Pool A-1; Carol credited. A SEPARATE
	// withdrawal from a DIFFERENT match, so BarredSides finds each barred
	// competitor by their OWN match, not Alice's.
	resp = postDecisionJSON(t, r, compID, "Pool A-1", DecisionRequest{
		Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test",
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	return r, store, eng
}

func TestDecisionHandler_BothSidesBarredHikiwake_Accepted(t *testing.T) {
	compID := "both-barred-hikiwake-accept"
	r, store, _ := bothBarredPoolFixture(t, compID)

	// decisionBy is deliberately OMITTED: this shape bypasses Validate()
	// entirely (bc-cse item 10), so it must not be required.
	resp := postDecisionJSON(t, r, compID, "Pool A-2", DecisionRequest{
		Decision: "hikiwake", DecisionReason: "both competitors already withdrawn",
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var m2 *state.MatchResult
	for i := range matches {
		if matches[i].ID == "Pool A-2" {
			m2 = &matches[i]
		}
	}
	require.NotNil(t, m2)
	assert.Equal(t, state.MatchStatusCompleted, m2.Status)
	assert.Equal(t, "hikiwake", m2.Decision)
	assert.Empty(t, m2.Winner, "a draw between two barred competitors names no winner")
	assert.Empty(t, m2.WinnerID)
	assert.Empty(t, m2.IpponsA, "no points on a hikiwake between two barred competitors")
	assert.Empty(t, m2.IpponsB)

	// Writes NO eligibility status of its own: hikiwake is not a withdrawal
	// decision, so both bars are untouched, each still attributed to its
	// OWN original match, never to Pool A-2.
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.Len(t, statuses, 2, "hikiwake must not add or remove any eligibility record")
	for _, st := range statuses {
		assert.False(t, st.Eligible)
		assert.NotEqual(t, "Pool A-2", st.MatchID, "the draw must not claim to have barred anyone itself")
	}
}

func TestDecisionHandler_HikiwakeNeitherBarred_Refused400SameAsToday(t *testing.T) {
	compID := "hikiwake-neither-barred"
	r, _, _ := bothBarredPoolFixture(t, compID)

	// Pool A-3 pairs Bob against Carol; NEITHER was ever barred.
	resp := postDecisionJSON(t, r, compID, "Pool A-3", DecisionRequest{
		Decision: "hikiwake", DecisionReason: "test",
	})
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "unsupported on /decision endpoint",
		"a non-qualifying hikiwake must keep today's refusal, unchanged")
}

func TestDecisionHandler_HikiwakeOneSideBarred_Refused400SameAsToday(t *testing.T) {
	compID := "hikiwake-one-side-barred"
	r, _, _ := bothBarredPoolFixture(t, compID)

	// Pool A-4 pairs Alice (barred, via Pool A-0) against Carol (never
	// barred): only ONE side qualifies, so this must not be accepted either.
	resp := postDecisionJSON(t, r, compID, "Pool A-4", DecisionRequest{
		Decision: "hikiwake", DecisionReason: "test",
	})
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Contains(t, body["error"], "unsupported on /decision endpoint")
}

// TestScoreHandler_BothSidesBarredKnockout_NeitherCanFightSentence covers
// bc-cse item 10's knockout wording: a bracket match whose both competitors
// are already barred (each by a separate earlier pool match) is refused
// with a "Neither ... can fight ..." sentence, naming BOTH competitors,
// rather than the single-sided "X withdrew ..." sentence that used to name
// only one and silently drop the other's own bar.
func TestScoreHandler_BothSidesBarredKnockout_NeitherCanFightSentence(t *testing.T) {
	compID := "both-barred-knockout"
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusKnockout,
	}))
	aliceID, bobID, carolID, daveID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
		{ID: daveID, Name: "Dave", Dojo: "D"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Dave", SideB: "Carol", SideAID: daveID, SideBID: carolID, Status: state.MatchStatusScheduled},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideA: "Alice", SideAID: aliceID, SideB: "Dave", SideBID: daveID,
			Status: state.MatchStatusScheduled, DisplayRound: 1, MatchNumber: 1}},
	}}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)

	require.Equal(t, http.StatusOK, postDecisionJSON(t, r, compID, "Pool A-0",
		DecisionRequest{Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test"}).Code)
	require.Equal(t, http.StatusOK, postDecisionJSON(t, r, compID, "Pool A-1",
		DecisionRequest{Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test"}).Code)

	// Starting the knockout match (Alice vs Dave, both barred) is refused.
	resp := putScoreJSON(t, r, compID, "m-r1-0", state.MatchResult{ID: "m-r1-0", Status: state.MatchStatusRunning})
	require.Equal(t, http.StatusConflict, resp.Code, resp.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "ineligible_competitor", body["error"])
	reasonHuman, _ := body["reasonHuman"].(string)
	assert.Contains(t, reasonHuman, "Neither")
	assert.Contains(t, reasonHuman, "Alice")
	assert.Contains(t, reasonHuman, "Dave")
	assert.Contains(t, reasonHuman, "correct the earlier withdrawal or the draw")
}

// TestDecisionHandler_BothSidesBarredHikiwake_ClockSkewRefused pins bc-cse
// finding 3: the clock-skew refusal runs BEFORE the both-sides-barred
// hikiwake carve-out's own write, exactly as it does on the main /decision
// path and on PUT /score. A device clock implausibly far in the future gets
// the same {"applied":false,"reason":"clock_skew"} 200, and nothing is
// written -- Pool A-2 stays scheduled.
func TestDecisionHandler_BothSidesBarredHikiwake_ClockSkewRefused(t *testing.T) {
	compID := "both-barred-hikiwake-clock-skew"
	r, store, _ := bothBarredPoolFixture(t, compID)

	future := time.Now().Add(time.Hour).UnixMilli()
	resp := postDecisionJSON(t, r, compID, "Pool A-2", DecisionRequest{
		Decision: "hikiwake", DecisionReason: "both competitors already withdrawn",
		ModifiedAt: future,
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, false, body["applied"])
	assert.Equal(t, "clock_skew", body["reason"])

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var m2 *state.MatchResult
	for i := range matches {
		if matches[i].ID == "Pool A-2" {
			m2 = &matches[i]
		}
	}
	require.NotNil(t, m2)
	assert.Equal(t, state.MatchStatusScheduled, m2.Status, "nothing was written")
}

// TestDecisionHandler_BothSidesBarredHikiwake_SupersededNot500 pins bc-cse
// finding 3's second half: the carve-out's own write competes on timestamps
// exactly like the main /decision path, and losing that race must answer
// 200 {"applied":false,"reason":"superseded"} -- never the 500
// respondEngineError's default arm used to produce for
// engine.ErrMatchSuperseded, which the SPA's offline write queue would
// retry forever against a write that can never win.
func TestDecisionHandler_BothSidesBarredHikiwake_SupersededNot500(t *testing.T) {
	compID := "both-barred-hikiwake-superseded"
	r, store, _ := bothBarredPoolFixture(t, compID)

	// Stamp Pool A-2's STORED ModifiedAt into the future so the carve-out's
	// own write (an older, still clock-valid stamp) loses the timestamp
	// last-write-wins guard.
	storedStamp := time.Now().Add(time.Hour).UnixMilli()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range matches {
		if matches[i].ID == "Pool A-2" {
			matches[i].ModifiedAt = storedStamp
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	writeStamp := time.Now().Add(-time.Hour).UnixMilli()
	resp := postDecisionJSON(t, r, compID, "Pool A-2", DecisionRequest{
		Decision: "hikiwake", DecisionReason: "both competitors already withdrawn",
		ModifiedAt: writeStamp,
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, false, body["applied"])
	assert.Equal(t, "superseded", body["reason"])

	after, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var m2 *state.MatchResult
	for i := range after {
		if after[i].ID == "Pool A-2" {
			m2 = &after[i]
		}
	}
	require.NotNil(t, m2)
	assert.Equal(t, state.MatchStatusScheduled, m2.Status, "the superseded write must not overwrite the stored match")
	assert.Equal(t, storedStamp, m2.ModifiedAt, "the stored stamp is untouched")
}

// TestDecisionHandler_BothSidesBarredHikiwake_OwesTheReopenReason drives the
// one way a both-barred match comes to carry ReopenPending: Carol's default
// win over the barred Alice is reopened with no reason, which lands it
// SCHEDULED because Alice is still barred (engine.reopenTargetStatus), and
// Carol then withdraws from another match. Ending that match as drawn must
// ask for the reopen's reason exactly as the main /decision flow does, and
// store it when given, rather than clearing the flag with no reason at all.
func TestDecisionHandler_BothSidesBarredHikiwake_OwesTheReopenReason(t *testing.T) {
	compID := "both-barred-hikiwake-reopen"
	r, store, _ := bothBarredPoolFixture(t, compID)

	load := func() state.MatchResult {
		t.Helper()
		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range matches {
			if m.ID == "Pool A-4" {
				return m
			}
		}
		t.Fatal("Pool A-4 not found")
		return state.MatchResult{}
	}

	// Pool A-4 is Alice (aka, barred by Pool A-0) v Carol: the default win
	// goes to Carol, then it is reopened with a one-tap empty body.
	resp := postDecisionJSON(t, r, compID, "Pool A-4", DecisionRequest{
		Decision: "fusensho", DecisionBy: "aka", DecisionReason: "Alice withdrew earlier",
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	resp = postReopenRaw(t, r, compID, "Pool A-4", nil)
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	reopened := load()
	require.Equal(t, state.MatchStatusScheduled, reopened.Status, "Alice is still barred, so the reopen lands scheduled")
	require.True(t, reopened.ReopenPending, "a reason-less reopen owes its reason to whatever ends the match next")

	// Carol (shiro on Pool A-3) withdraws: both sides of Pool A-4 are barred.
	resp = postDecisionJSON(t, r, compID, "Pool A-3", DecisionRequest{
		Decision: "kiken-voluntary", DecisionBy: "shiro", DecisionReason: "test",
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())

	resp = postDecisionJSON(t, r, compID, "Pool A-4", DecisionRequest{Decision: "hikiwake"})
	require.Equal(t, http.StatusBadRequest, resp.Code, resp.Body.String())
	assert.Contains(t, resp.Body.String(), ReopenNeedsReasonMessage)
	refused := load()
	assert.Equal(t, state.MatchStatusScheduled, refused.Status, "a refusal writes nothing")
	assert.True(t, refused.ReopenPending, "a refusal leaves the reason still owed")

	resp = postDecisionJSON(t, r, compID, "Pool A-4", DecisionRequest{
		Decision: "hikiwake", DecisionReason: "both withdrew",
	})
	require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
	drawn := load()
	assert.Equal(t, state.MatchStatusCompleted, drawn.Status)
	assert.Equal(t, "hikiwake", drawn.Decision)
	assert.False(t, drawn.ReopenPending, "the reason is given, so nothing is owed")
	assert.Equal(t, "both withdrew", drawn.CorrectionReason, "the reopen's reason is recorded, as the main flow records it")
}
