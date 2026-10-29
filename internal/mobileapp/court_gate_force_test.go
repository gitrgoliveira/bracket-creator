package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-adna: the organiser may force a start (or a reopen) on a court that
// already runs a bout with the request-only forceCourt; without it the
// answer is court_busy as before, an anonymous self-run caller's forceCourt
// is dropped, the sides gate still runs, and the court gates judge only a
// write that moves a match into running, so a court left with two running
// bouts keeps taking writes to both.

func courtGateWrite(t *testing.T, r *gin.Engine, method, path, password string, payload map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", password)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func courtGateScorePath(compID, matchID string) string {
	return "/api/competitions/" + compID + "/matches/" + matchID + "/score"
}

func courtGateStatus(t *testing.T, store *state.Store, compID, matchID string) state.MatchStatus {
	t.Helper()
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == matchID {
			return m.Status
		}
	}
	t.Fatalf("match %s not found in %s", matchID, compID)
	return ""
}

func courtGateErrorCode(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), w.Body.String())
	code, _ := resp["error"].(string)
	return code
}

func startPayload(sideA, sideB string, force bool) map[string]any {
	p := map[string]any{
		"sideA": sideA, "sideB": sideB,
		"ipponsA": []string{}, "ipponsB": []string{}, "status": "running",
	}
	if force {
		p["forceCourt"] = true
	}
	return p
}

func TestScore_ForceCourt_SameCompetition(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	compID := "force-same"
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-1", SideA: "Alice", SideB: "Bob", Court: "A", Status: state.MatchStatusRunning},
		{ID: "PoolA-2", SideA: "Carol", SideB: "Dan", Court: "A", Status: state.MatchStatusScheduled},
	}))

	w := courtGateWrite(t, r, http.MethodPut, courtGateScorePath(compID, "PoolA-2"), "", startPayload("Carol", "Dan", false))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "court_busy", courtGateErrorCode(t, w))
	assert.Equal(t, state.MatchStatusScheduled, courtGateStatus(t, store, compID, "PoolA-2"))

	w = courtGateWrite(t, r, http.MethodPut, courtGateScorePath(compID, "PoolA-2"), "", startPayload("Carol", "Dan", true))
	require.Equal(t, http.StatusOK, w.Code, "a forced start lands beside the running bout: %s", w.Body.String())
	assert.Equal(t, state.MatchStatusRunning, courtGateStatus(t, store, compID, "PoolA-2"))
	assert.Equal(t, state.MatchStatusRunning, courtGateStatus(t, store, compID, "PoolA-1"))

	// Two bouts run on court A now: the console's writes to either are not
	// refused for the court.
	w = courtGateWrite(t, r, http.MethodPut, courtGateScorePath(compID, "PoolA-1"), "", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "ipponsA": []string{"M"}, "ipponsB": []string{}, "status": "running",
	})
	require.Equal(t, http.StatusOK, w.Code, "a running write to a bout already running is not judged: %s", w.Body.String())
	w = courtGateWrite(t, r, http.MethodPut, courtGateScorePath(compID, "PoolA-2"), "", map[string]any{
		"sideA": "Carol", "sideB": "Dan", "winner": "Carol",
		"ipponsA": []string{"M", "K"}, "ipponsB": []string{}, "status": "completed",
	})
	require.Equal(t, http.StatusOK, w.Code, "a finish is not judged: %s", w.Body.String())
	assert.Equal(t, state.MatchStatusCompleted, courtGateStatus(t, store, compID, "PoolA-2"))
	w = courtGateWrite(t, r, http.MethodPut, courtGateScorePath(compID, "PoolA-1"), "", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "winner": "Alice",
		"ipponsA": []string{"M", "K"}, "ipponsB": []string{}, "status": "completed",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, state.MatchStatusCompleted, courtGateStatus(t, store, compID, "PoolA-1"))
}

func TestScore_ForceCourt_AcrossCompetitions(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "teams", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("teams", []state.MatchResult{
		{ID: "PoolA-1", SideA: "Alice", SideB: "Bob", Court: "A", Status: state.MatchStatusRunning},
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "indiv", Courts: []string{"A"}}))
	require.NoError(t, store.SavePoolMatches("indiv", []state.MatchResult{
		{ID: "PoolA-1", SideA: "Carol", SideB: "Dan", Court: "A", Status: state.MatchStatusScheduled},
	}))

	w := courtGateWrite(t, r, http.MethodPut, courtGateScorePath("indiv", "PoolA-1"), "", startPayload("Carol", "Dan", false))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "court_busy", courtGateErrorCode(t, w))

	w = courtGateWrite(t, r, http.MethodPut, courtGateScorePath("indiv", "PoolA-1"), "", startPayload("Carol", "Dan", true))
	require.Equal(t, http.StatusOK, w.Code, "a forced start lands beside another competition's bout: %s", w.Body.String())
	assert.Equal(t, state.MatchStatusRunning, courtGateStatus(t, store, "indiv", "PoolA-1"))

	// Each competition's console keeps scoring its own bout.
	w = courtGateWrite(t, r, http.MethodPut, courtGateScorePath("teams", "PoolA-1"), "", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "ipponsA": []string{"M"}, "ipponsB": []string{}, "status": "running",
	})
	require.Equal(t, http.StatusOK, w.Code, "the first bout's console is not refused: %s", w.Body.String())
	w = courtGateWrite(t, r, http.MethodPut, courtGateScorePath("indiv", "PoolA-1"), "", map[string]any{
		"sideA": "Carol", "sideB": "Dan", "ipponsA": []string{}, "ipponsB": []string{"K"}, "status": "running",
	})
	require.Equal(t, http.StatusOK, w.Code, "the forced bout's console is not refused: %s", w.Body.String())
	w = courtGateWrite(t, r, http.MethodPut, courtGateScorePath("teams", "PoolA-1"), "", map[string]any{
		"sideA": "Alice", "sideB": "Bob", "winner": "Alice",
		"ipponsA": []string{"M", "K"}, "ipponsB": []string{}, "status": "completed",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, state.MatchStatusCompleted, courtGateStatus(t, store, "teams", "PoolA-1"))
}

func TestScore_ForceCourt_SidesGateStillRuns(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	compID := "force-barred"
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Courts: []string{"A"}}))
	aliceID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "DojoA"},
		{ID: helper.NewUUID4(), Name: "Bob", Dojo: "DojoB"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-1", SideA: "Carol", SideB: "Dan", Court: "A", Status: state.MatchStatusRunning},
		{ID: "PoolA-2", SideA: "Alice", SideB: "Bob", Court: "A", Status: state.MatchStatusScheduled},
	}))
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID:   aliceID,
		Eligible:   false,
		Reason:     "kiken at m_prev",
		MatchID:    "m_prev",
		RecordedAt: time.Now().UTC(),
	}))

	w := courtGateWrite(t, r, http.MethodPut, courtGateScorePath(compID, "PoolA-2"), "", startPayload("Alice", "Bob", true))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "ineligible_competitor", courtGateErrorCode(t, w), "forcing the court never lets a barred side fight")
	assert.Equal(t, state.MatchStatusScheduled, courtGateStatus(t, store, compID, "PoolA-2"))
}

func TestScore_ForceCourt_AnonymousSelfRunIsStillCourtBusy(t *testing.T) {
	const mainPw = "organiser-secret"
	r, store, compID := setupSelfRunScoreRouter(t, mainPw)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Courts = []string{"A"}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "PoolA-1", SideA: "Carol", SideB: "Dan", Court: "A", Status: state.MatchStatusRunning},
		{ID: "PoolA-2", SideA: "Alice", SideB: "Bob", Court: "A", Status: state.MatchStatusScheduled},
	}))

	w := courtGateWrite(t, r, http.MethodPut, courtGateScorePath(compID, "PoolA-2"), "", startPayload("Alice", "Bob", true))
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "court_busy", courtGateErrorCode(t, w), "a participant's forceCourt is dropped")
	assert.Equal(t, state.MatchStatusScheduled, courtGateStatus(t, store, compID, "PoolA-2"))

	w = courtGateWrite(t, r, http.MethodPut, courtGateScorePath(compID, "PoolA-2"), mainPw, startPayload("Alice", "Bob", true))
	require.Equal(t, http.StatusOK, w.Code, "the organiser's forceCourt lands: %s", w.Body.String())
	assert.Equal(t, state.MatchStatusRunning, courtGateStatus(t, store, compID, "PoolA-2"))
}

func TestReopen_ForceCourt(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	compID := "force-reopen"
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Courts: []string{"A"}, TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3,
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "PoolA-1", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
			Winner: "RedTeam", Decision: "kachinuki-exhaustion", Court: "A",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		},
		{ID: "PoolA-2", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusRunning, Court: "A"},
	}))
	reopenPath := "/api/competitions/" + compID + "/matches/PoolA-1/reopen"

	w := courtGateWrite(t, r, http.MethodPost, reopenPath, "", map[string]any{"reason": "need more bouts"})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Equal(t, "court_busy", courtGateErrorCode(t, w))
	assert.Equal(t, state.MatchStatusCompleted, courtGateStatus(t, store, compID, "PoolA-1"))

	w = courtGateWrite(t, r, http.MethodPost, reopenPath, "", map[string]any{"reason": "need more bouts", "forceCourt": true})
	require.Equal(t, http.StatusOK, w.Code, "a forced reopen lands: %s", w.Body.String())
	assert.Equal(t, state.MatchStatusRunning, courtGateStatus(t, store, compID, "PoolA-1"))
	assert.Equal(t, state.MatchStatusRunning, courtGateStatus(t, store, compID, "PoolA-2"))
}
