// Package mobileapp, reason_human_barred_test.go pins the bc-rawm/bc-cse
// operator sentence a 409 ineligible_competitor / already_ineligible refusal
// carries: it names the barred competitor, the match they were barred IN
// (its operator label), and the remedy -- record the default win for the
// opponent, or reinstate for a kiken-injury.
package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// barredCompetitorFixture builds a mixed competition with three participants
// (Alice, Bob, Carol) and two SCHEDULED pool matches: "Pool A-0" (Alice vs
// Bob) and "Pool A-1" (Alice vs Carol, or Carol vs Alice when aliceIsSideA is
// false -- used to exercise both the SideA and SideB attribution arms).
// Returns the wired gin router and the three participant ids.
func barredCompetitorFixture(t *testing.T, compID string, aliceIsSideA bool) (*gin.Engine, string, string, string) {
	t.Helper()
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))

	second := state.MatchResult{
		ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID,
		Status: state.MatchStatusScheduled,
	}
	if !aliceIsSideA {
		second = state.MatchResult{
			ID: "Pool A-1", SideA: "Carol", SideB: "Alice", SideAID: carolID, SideBID: aliceID,
			Status: state.MatchStatusScheduled,
		}
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
		second,
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)
	return r, aliceID, bobID, carolID
}

func postDecisionJSON(t *testing.T, r *gin.Engine, compID, mid string, req DecisionRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	httpReq, err := http.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/matches/"+mid+"/decision", bytes.NewBuffer(body))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, httpReq)
	return w
}

func putScoreJSON(t *testing.T, r *gin.Engine, compID, mid string, mr state.MatchResult) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(mr)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	httpReq, err := http.NewRequest(http.MethodPut, "/api/competitions/"+compID+"/matches/"+mid+"/score", bytes.NewBuffer(body))
	require.NoError(t, err)
	httpReq.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, httpReq)
	return w
}

// TestScoreHandler_IneligibleCompetitor_ReasonHumanNamesMatchAndRemedy
// covers /score's 409 ineligible_competitor for each decision that can bar a
// competitor: Alice withdraws from "Pool A-0" (against Bob), then starting
// "Pool A-1" (Alice vs Carol) is refused, and the refusal must name Alice,
// label "Pool A-0" in operator terms, and give the remedy.
func TestScoreHandler_IneligibleCompetitor_ReasonHumanNamesMatchAndRemedy(t *testing.T) {
	cases := []struct {
		name     string
		decision string
		want     string
	}{
		{
			"kiken-voluntary", "kiken-voluntary",
			"Alice withdrew in Pool A · Match 1 and cannot fight again. Record the default win for Carol.",
		},
		{
			"kiken-injury", "kiken-injury",
			"Alice withdrew injured in Pool A · Match 1. Reinstate Alice if the doctor allows, or record the default win for Carol.",
		},
		{
			"fusenpai", "fusenpai",
			"Alice did not appear for Pool A · Match 1 and cannot fight again. Record the default win for Carol.",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compID := "ineligible-reason-" + tc.name
			r, aliceID, _, _ := barredCompetitorFixture(t, compID, true)

			// Alice (aka/SideA) is barred from "Pool A-0" by this decision.
			decisionResp := postDecisionJSON(t, r, compID, "Pool A-0", DecisionRequest{
				Decision: tc.decision, DecisionBy: "aka", DecisionReason: "test",
			})
			require.Equal(t, http.StatusOK, decisionResp.Code, decisionResp.Body.String())

			// Starting "Pool A-1" (Alice vs Carol) must be refused: Alice is
			// barred by a DIFFERENT match.
			startResp := putScoreJSON(t, r, compID, "Pool A-1", state.MatchResult{
				ID: "Pool A-1", Status: state.MatchStatusRunning,
			})
			require.Equal(t, http.StatusConflict, startResp.Code, startResp.Body.String())

			var body map[string]any
			require.NoError(t, json.Unmarshal(startResp.Body.Bytes(), &body))
			assert.Equal(t, "ineligible_competitor", body["error"])
			assert.Equal(t, aliceID, body["playerId"])
			assert.Equal(t, tc.want, body["reasonHuman"])
			// The wire's raw `reason`/`error` keys are unchanged (task contract).
			assert.Contains(t, body["reason"], tc.decision+" at Pool A-0")
		})
	}
}

// TestDecisionHandler_AlreadyIneligible_ReasonHumanNamesMatchAndRemedy
// covers /decision's 409 already_ineligible (T105/CHK047 concurrent
// withdrawal guard): Alice withdraws from "Pool A-0" first, then a SECOND
// decision attempt naming Alice as the loser of "Pool A-1" (Carol vs Alice,
// Alice on SideB this time) is refused because she is already barred by
// "Pool A-0".
func TestDecisionHandler_AlreadyIneligible_ReasonHumanNamesMatchAndRemedy(t *testing.T) {
	compID := "already-ineligible-reason"
	r, aliceID, _, _ := barredCompetitorFixture(t, compID, false) // "Pool A-1" = Carol vs Alice

	first := postDecisionJSON(t, r, compID, "Pool A-0", DecisionRequest{
		Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test",
	})
	require.Equal(t, http.StatusOK, first.Code, first.Body.String())

	// Alice is SideB of "Pool A-1" (Carol vs Alice); decisionBy "shiro" names
	// her as the (attempted) loser again.
	second := postDecisionJSON(t, r, compID, "Pool A-1", DecisionRequest{
		Decision: "kiken-voluntary", DecisionBy: "shiro", DecisionReason: "test",
	})
	require.Equal(t, http.StatusConflict, second.Code, second.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(second.Body.Bytes(), &body))
	assert.Equal(t, "already_ineligible", body["error"])
	assert.Equal(t, aliceID, body["playerId"])
	assert.Equal(t, "Pool A-0", body["matchId"])
	assert.Equal(t,
		"Alice withdrew in Pool A · Match 1 and cannot fight again. Record the default win for Carol.",
		body["reasonHuman"])
}

// TestScoreHandler_SimultaneityReason_IsAlreadyAnOperatorSentence pins that
// the simultaneity-gate refusal (a DIFFERENT IneligibleCompetitorError
// producer, MatchID=="") reaches the wire as reasonHuman VERBATIM: it is
// already a complete operator sentence (checkSimultaneousMatchTx), so the
// handler must not run it through the barred-competitor builder (which
// would only degrade to the domain.ResolveReasonHuman fallback, since this
// Reason is not the "<decision> at <matchId>" shape at all).
func TestScoreHandler_SimultaneityReason_IsAlreadyAnOperatorSentence(t *testing.T) {
	compID := "simultaneity-reason"
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusRunning, Court: "A"},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID, Status: state.MatchStatusScheduled},
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)

	resp := putScoreJSON(t, r, compID, "Pool A-1", state.MatchResult{ID: "Pool A-1", Status: state.MatchStatusRunning})
	require.Equal(t, http.StatusConflict, resp.Code, resp.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "ineligible_competitor", body["error"])
	assert.Equal(t, "Alice is fighting now in Pool A · Match 1 on Shiaijo A. Finish that match first.", body["reasonHuman"])
	assert.Equal(t, body["reason"], body["reasonHuman"], "the simultaneity sentence IS the reason, verbatim")
}

// TestScoreHandler_DirectlySetIneligibility_TranslatesRatherThanLeaksRawReason
// covers bc-cse item 3: a competitor-status entry set directly via
// POST /competitor-status (unlike every OTHER producer in this file) carries
// no MatchID at all -- domain.CompetitorStatus.Validate never requires one.
// Before engine.IneligibleCompetitorError.Simultaneous existed, that empty
// MatchID was indistinguishable from the simultaneity gate's own sentinel
// (also MatchID==""), so the handler skipped reasonHumanForBarredCompetitor
// entirely and echoed the raw, untranslated Reason onto the wire as
// reasonHuman. The barred-status producer must still be recognised as such
// and get a translated sentence.
func TestScoreHandler_DirectlySetIneligibility_TranslatesRatherThanLeaksRawReason(t *testing.T) {
	compID := "direct-ineligible-no-match"
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	aliceID, bobID := helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Status: state.MatchStatusScheduled},
	}))

	// Set directly, the way POST /competitor-status would: no MatchID, and
	// a bare decision keyword as Reason -- the engine's own writers always
	// append " at <matchID>" (see reasonHumanPatterns), so a directly-set
	// status naturally lacks that suffix too.
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: aliceID, Eligible: false, Reason: "kiken-voluntary",
	}))

	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)

	resp := putScoreJSON(t, r, compID, "Pool A-0", state.MatchResult{ID: "Pool A-0", Status: state.MatchStatusRunning})
	require.Equal(t, http.StatusConflict, resp.Code, resp.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	assert.Equal(t, "ineligible_competitor", body["error"])
	assert.Equal(t, aliceID, body["playerId"])
	assert.Equal(t, "kiken-voluntary", body["reason"], "the raw reason is unchanged on the wire")
	assert.Equal(t, "voluntary withdrawal", body["reasonHuman"],
		"a status set with no matchId must still be translated, not echoed raw")
}

// TestBulkScoreHandler_AlreadyIneligible_ReasonHumanNamesMatchAndRemedy
// covers bc-cse item 5: bulk-score's partial-success shape has no dedicated
// 409 to answer with, so an ineligibility refusal rides in the per-entry
// scoreError.Error string. Before the fix that string was err.Error()'s raw
// `competitor "<uuid>" already ineligible (match Pool A-0)`, naming an
// internal id and no remedy; it must instead read the same operator
// sentence the single-match endpoints give.
func TestBulkScoreHandler_AlreadyIneligible_ReasonHumanNamesMatchAndRemedy(t *testing.T) {
	compID := "bulk-already-ineligible"
	r, aliceID, _, carolID := barredCompetitorFixture(t, compID, true)

	// Alice (aka/SideA of "Pool A-0") withdraws first, via the ordinary
	// decision path.
	decisionResp := postDecisionJSON(t, r, compID, "Pool A-0", DecisionRequest{
		Decision: "kiken-voluntary", DecisionBy: "aka", DecisionReason: "test",
	})
	require.Equal(t, http.StatusOK, decisionResp.Code, decisionResp.Body.String())

	// A bulk-score entry now tries to ALSO record Alice as the loser of
	// "Pool A-1" (Alice vs Carol) by a kiken-voluntary decision embedded in
	// the plain MatchResult write -- bulk-score has no /decision shape, so
	// this is how a withdrawal reaches it. Alice is already barred by a
	// DIFFERENT match, so this must be refused per-entry.
	body, err := json.Marshal([]state.MatchResult{
		{
			ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID,
			Winner: "Carol", WinnerID: carolID, WinnerSide: "B",
			Decision: "kiken-voluntary", Status: state.MatchStatusCompleted,
		},
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodPost, "/api/competitions/"+compID+"/matches/bulk-score", bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var resp struct {
		Succeeded int `json:"succeeded"`
		Errors    []struct {
			MatchID string `json:"matchId"`
			Error   string `json:"error"`
			Reason  string `json:"reason,omitempty"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 0, resp.Succeeded)
	require.Len(t, resp.Errors, 1)
	assert.Equal(t, "Pool A-1", resp.Errors[0].MatchID)
	assert.Equal(t,
		"Alice withdrew in Pool A · Match 1 and cannot fight again. Record the default win for Carol.",
		resp.Errors[0].Error)
	assert.NotContains(t, resp.Errors[0].Error, aliceID, "the raw participant id must not leak into the operator-facing error")
}
