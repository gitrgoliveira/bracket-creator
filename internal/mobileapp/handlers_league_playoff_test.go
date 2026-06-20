package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test stubs
// ---------------------------------------------------------------------------

// stubLeaguePlayoffStore implements LeaguePlayoffStore for handler tests.
type stubLeaguePlayoffStore struct {
	comp       *state.Competition
	loadErr    error
	matches    []state.MatchResult
	matchesErr error
	saveErr    error
	updateErr  error
	// updateFn is called inside UpdateCompetitionChanged if non-nil, allowing
	// tests to inspect or modify the transform's behaviour.
	updateFn func(*state.Competition) (*state.Competition, error)
}

func (s *stubLeaguePlayoffStore) LoadCompetition(id string) (*state.Competition, error) {
	return s.comp, s.loadErr
}

func (s *stubLeaguePlayoffStore) LoadPoolMatches(id string) ([]state.MatchResult, error) {
	return s.matches, s.matchesErr
}

func (s *stubLeaguePlayoffStore) SavePoolMatches(id string, matches []state.MatchResult) error {
	return s.saveErr
}

func (s *stubLeaguePlayoffStore) UpdateCompetitionChanged(id string, transform func(*state.Competition) (*state.Competition, error)) (bool, error) {
	if s.updateErr != nil {
		return false, s.updateErr
	}
	if s.updateFn != nil {
		updated, err := s.updateFn(s.comp)
		if err != nil {
			return false, err
		}
		if updated == nil {
			return false, nil
		}
		s.comp = updated
		return true, nil
	}
	// Default: run the transform against the stub competition.
	updated, err := transform(s.comp)
	if err != nil {
		return false, err
	}
	if updated == nil {
		return false, nil
	}
	s.comp = updated
	return true, nil
}

// stubLeaguePlayoffEngine implements LeaguePlayoffEngine for handler tests.
type stubLeaguePlayoffEngine struct {
	candidates    []engine.TiedGroup
	candidatesErr error
	generated     []state.MatchResult
	generateErr   error
	autoOutcome   engine.AutoCompleteOutcome
	autoErr       error
}

func (e *stubLeaguePlayoffEngine) LeaguePlayoffCandidates(string) ([]engine.TiedGroup, error) {
	return e.candidates, e.candidatesErr
}

func (e *stubLeaguePlayoffEngine) GenerateLeaguePlayoffMatches(compID string, tiedTeamNames []string) ([]state.MatchResult, error) {
	return e.generated, e.generateErr
}

func (e *stubLeaguePlayoffEngine) MaybeAutoCompletePools(string) (engine.AutoCompleteOutcome, error) {
	return e.autoOutcome, e.autoErr
}

// leaguePlayoffRouter sets up a gin engine with all league-playoff handlers
// wired on the same unauthenticated group, matching the old test layout.
// This is used by the business-logic tests (happy/error paths) where we
// test handler behaviour, not auth enforcement.
func leaguePlayoffRouter(eng LeaguePlayoffEngine, store LeaguePlayoffStore, hub Broadcaster) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")
	// Public (unauthenticated) read endpoint.
	RegisterPublicLeaguePlayoffHandlers(g, eng, store)
	// Mutation endpoints — no auth middleware here; business logic only.
	RegisterLeaguePlayoffHandlers(g, eng, store, hub)
	return r
}

// makeTeamLeagueComp returns a minimal team-league Competition for tests.
func makeTeamLeagueComp(status state.CompetitionStatus) *state.Competition {
	return &state.Competition{
		ID:       "comp-1",
		Name:     "Test League",
		Format:   state.CompFormatLeague,
		Kind:     "team",
		TeamSize: 5,
		Status:   status,
	}
}

// makeTiedGroup builds a TiedGroup for two teams.
func makeTiedGroup(teamA, teamB string, minPos, maxPos int) engine.TiedGroup {
	return engine.TiedGroup{
		Teams: []state.PlayerStanding{
			{Player: domain.Player{Name: teamA}},
			{Player: domain.Player{Name: teamB}},
		},
		MinPosition: minPos,
		MaxPosition: maxPos,
	}
}

// ---------------------------------------------------------------------------
// GET /competitions/:id/league-playoff/candidates
// ---------------------------------------------------------------------------

func TestLeaguePlayoffCandidates_Happy(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeaguePlayoffEngine{candidates: candidates}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-playoff/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	cands, ok := body["candidates"].([]any)
	require.True(t, ok)
	assert.Len(t, cands, 1)
	assert.Equal(t, false, body["finalized"])
}

func TestLeaguePlayoffCandidates_Empty(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{candidates: nil}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-playoff/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	cands := body["candidates"].([]any)
	assert.Len(t, cands, 0)
}

func TestLeaguePlayoffCandidates_CompNotFound(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{comp: nil}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-playoff/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestLeaguePlayoffCandidates_EngineError(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{candidatesErr: fmt.Errorf("engine error")}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-playoff/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestLeaguePlayoffCandidates_Finalized(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{candidates: nil}
	comp := makeTeamLeagueComp(state.CompStatusPools)
	comp.LeaguePlayoffFinalized = true
	store := &stubLeaguePlayoffStore{comp: comp}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-playoff/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, true, body["finalized"])
}

// ---------------------------------------------------------------------------
// POST /competitions/:id/league-playoff
// ---------------------------------------------------------------------------

func TestLeaguePlayoffPost_Happy(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	generated := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B"},
	}
	eng := &stubLeaguePlayoffEngine{
		candidates: candidates,
		generated:  generated,
	}
	store := &stubLeaguePlayoffStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil, // no existing DH matches
	}
	hub := &recordingBroadcaster{}
	r := leaguePlayoffRouter(eng, store, hub)

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	matches := resp["matches"].([]any)
	assert.Len(t, matches, 1)
	// Two SSE events should have been broadcast.
	assert.GreaterOrEqual(t, len(hub.events), 2)
}

func TestLeaguePlayoffPost_InvalidSelection(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeaguePlayoffEngine{candidates: candidates}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	// Request for teams not in any candidate group.
	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team X", "Team Y"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLeaguePlayoffPost_TooFewTeams(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLeaguePlayoffPost_AlreadyExists(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B"},
	}
	eng := &stubLeaguePlayoffEngine{candidates: candidates}
	store := &stubLeaguePlayoffStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestLeaguePlayoffPost_LoadMatchesError(t *testing.T) {
	// When LoadPoolMatches fails, the handler must return 500.
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeaguePlayoffEngine{candidates: candidates}
	store := &stubLeaguePlayoffStore{
		comp:       makeTeamLeagueComp(state.CompStatusPools),
		matchesErr: fmt.Errorf("disk error"),
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestLeaguePlayoffPost_EngineValidationError(t *testing.T) {
	// GenerateLeaguePlayoffMatches can return a ValidationError when the
	// competition is not a team-league type. The handler must map that to 400.
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeaguePlayoffEngine{
		candidates:  candidates,
		generateErr: &engine.ValidationError{Msg: "not a team-league competition"},
	}
	store := &stubLeaguePlayoffStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil,
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLeaguePlayoffPost_BadBody(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// DELETE /competitions/:id/league-playoff
// ---------------------------------------------------------------------------

func TestLeaguePlayoffDelete_Happy(t *testing.T) {
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B", Status: state.MatchStatusScheduled},
	}
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	hub := &recordingBroadcaster{}
	r := leaguePlayoffRouter(eng, store, hub)

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, float64(1), resp["deleted"])
	assert.GreaterOrEqual(t, len(hub.events), 2)
}

func TestLeaguePlayoffDelete_NotFound(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil, // no matches
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestLeaguePlayoffDelete_ScoredMatch(t *testing.T) {
	existing := []state.MatchResult{
		{
			ID:     "Pool A-DH-0",
			SideA:  "Team A",
			SideB:  "Team B",
			Winner: "Team A",
			Status: state.MatchStatusCompleted,
		},
	}
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestLeaguePlayoffDelete_TooFewTeams(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// POST /competitions/:id/league-playoff/finalize
// ---------------------------------------------------------------------------

func TestLeaguePlayoffFinalize_Happy(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{
		autoOutcome: engine.AutoCompleteTransitioned,
	}
	store := &stubLeaguePlayoffStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
	}
	hub := &recordingBroadcaster{}
	r := leaguePlayoffRouter(eng, store, hub)

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["finalized"])
	// Competition should have LeaguePlayoffFinalized=true set in the store.
	assert.True(t, store.comp.LeaguePlayoffFinalized)
	// CompetitionCompleted event should have been broadcast.
	assert.GreaterOrEqual(t, len(hub.events), 1)
}

func TestLeaguePlayoffFinalize_CompNotFound(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{comp: nil}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestLeaguePlayoffFinalize_AlreadyComplete(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{
		comp: makeTeamLeagueComp(state.CompStatusComplete),
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestLeaguePlayoffFinalize_MaybeAutoCompleteError(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{
		autoErr: fmt.Errorf("engine error"),
	}
	store := &stubLeaguePlayoffStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Still returns 200 with the finalized flag (same pattern as tryAutoCompletePools
	// — the score itself succeeded; the auto-complete failure is a background concern
	// surfaced via the error header).
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, AutoCompleteErrorValue, w.Header().Get(AutoCompleteErrorHeader))
}

func TestLeaguePlayoffFinalize_NoChange(t *testing.T) {
	// AutoCompleteNoChange = not all matches done yet, but finalized flag is set.
	eng := &stubLeaguePlayoffEngine{
		autoOutcome: engine.AutoCompleteNoChange,
	}
	store := &stubLeaguePlayoffStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
	}
	hub := &recordingBroadcaster{}
	r := leaguePlayoffRouter(eng, store, hub)

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, store.comp.LeaguePlayoffFinalized)
	// EventScheduleUpdated should have been broadcast even with NoChange.
	assert.GreaterOrEqual(t, len(hub.events), 1)
}

// ---------------------------------------------------------------------------
// Additional error-path tests to bring internal/mobileapp to ≥85% coverage
// ---------------------------------------------------------------------------

// GET — LoadCompetition returns an error (500).
func TestLeaguePlayoffCandidates_LoadError(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{loadErr: fmt.Errorf("disk I/O failure")}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-playoff/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// GET — LeaguePlayoffCandidates returns *engine.NotFoundError (404).
func TestLeaguePlayoffCandidates_EngineNotFound(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{
		candidatesErr: &engine.NotFoundError{Msg: "competition not in engine"},
	}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-playoff/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// GET — requireValidCompID returns false (empty :id → 400).
func TestLeaguePlayoffCandidates_InvalidID(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	// A route param that fails ValidateCompetitionID (e.g. empty string via
	// a sub-path that doesn't carry :id — use a contrived bad value).
	req := httptest.NewRequest("GET", "/api/competitions/%00/league-playoff/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Either 400 (invalid ID) or 404 (gin route not matched) is acceptable;
	// what matters is it does NOT return 200 or 500.
	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.NotEqual(t, http.StatusInternalServerError, w.Code)
}

// POST — LeaguePlayoffCandidates returns *engine.NotFoundError (404).
func TestLeaguePlayoffPost_CandidatesNotFound(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{
		candidatesErr: &engine.NotFoundError{Msg: "competition not in engine"},
	}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// POST — GenerateLeaguePlayoffMatches returns *engine.NotFoundError (404).
func TestLeaguePlayoffPost_GenerateNotFound(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeaguePlayoffEngine{
		candidates:  candidates,
		generateErr: &engine.NotFoundError{Msg: "competition vanished"},
	}
	store := &stubLeaguePlayoffStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil,
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// POST — GenerateLeaguePlayoffMatches returns a generic error (500).
func TestLeaguePlayoffPost_GenerateInternalError(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeaguePlayoffEngine{
		candidates:  candidates,
		generateErr: fmt.Errorf("unexpected engine failure"),
	}
	store := &stubLeaguePlayoffStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil,
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// DELETE — LoadPoolMatches returns an error (500).
func TestLeaguePlayoffDelete_LoadMatchesError(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{
		comp:       makeTeamLeagueComp(state.CompStatusPools),
		matchesErr: fmt.Errorf("store unavailable"),
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// DELETE — SavePoolMatches returns an error after removal (500).
func TestLeaguePlayoffDelete_SaveError(t *testing.T) {
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B", Status: state.MatchStatusScheduled},
	}
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
		saveErr: fmt.Errorf("write failure"),
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// DELETE — bad request body (400).
func TestLeaguePlayoffDelete_BadBody(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// Finalize — UpdateCompetitionChanged returns an error (500).
func TestLeaguePlayoffFinalize_UpdateError(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{
		comp:      makeTeamLeagueComp(state.CompStatusPools),
		updateErr: fmt.Errorf("transaction failure"),
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Finalize — AutoCompleteTransitioned broadcasts EventCompetitionCompleted.
func TestLeaguePlayoffFinalize_BroadcastsCompleted(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{
		autoOutcome: engine.AutoCompleteTransitioned,
	}
	store := &stubLeaguePlayoffStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
	}
	hub := &recordingBroadcaster{}
	r := leaguePlayoffRouter(eng, store, hub)

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	// EventCompetitionCompleted must be one of the broadcast events.
	found := false
	for _, ev := range hub.events {
		if ev == EventCompetitionCompleted {
			found = true
			break
		}
	}
	assert.True(t, found, "expected EventCompetitionCompleted to be broadcast; got %v", hub.events)
}

// ---------------------------------------------------------------------------
// Auth-split tests — verify GET is public and POST is admin-gated.
//
// These tests use a real state.Store (temporary directory) and the real
// AuthMiddleware, mirroring the pattern in handlers_eligibility_test.go.
// They are the primary evidence that the production server.go wiring is
// correct: GET /candidates serves 200 without X-Tournament-Password;
// POST /league-playoff returns 401 without it.
// ---------------------------------------------------------------------------

// setupLeaguePlayoffAuthRouter builds a router that mirrors the production
// split: GET /candidates on the public api group, mutations on the admin group.
func setupLeaguePlayoffAuthRouter(t *testing.T) (*gin.Engine, *state.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir, err := os.MkdirTemp("", "lp-auth-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	r := gin.New()
	// Stub engine: returns no candidates (enough for a 200 on GET).
	eng := &stubLeaguePlayoffEngine{candidates: nil}
	hub := stubBroadcaster{}

	// Public group — GET /candidates is unauthenticated.
	api := r.Group("/api")
	RegisterPublicLeaguePlayoffHandlers(api, eng, store)

	// Admin-gated group — mutations require X-Tournament-Password.
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterLeaguePlayoffHandlers(admin, eng, store, hub)

	return r, store
}

// TestLeaguePlayoffCandidates_IsPublic is the primary regression test for
// the bug reported in mp-8rc9 Phase 3b: GET /candidates returned 401 because
// the endpoint was registered on the admin group. It must be 200 without any
// X-Tournament-Password header, even for a password-protected tournament.
func TestLeaguePlayoffCandidates_IsPublic(t *testing.T) {
	r, store := setupLeaguePlayoffAuthRouter(t)

	// Set up a password-protected tournament and a competition.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "comp-1",
		Format: state.CompFormatLeague,
		Kind:   "team",
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/comp-1/league-playoff/candidates", nil)
	// Deliberately no X-Tournament-Password header.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "GET /candidates must be public (no auth header)")
}

// TestLeaguePlayoffPost_RequiresAuth verifies that the POST mutation returns
// 401 when called without X-Tournament-Password — i.e. the route is still on
// the admin-gated group after the public/admin split.
func TestLeaguePlayoffPost_RequiresAuth(t *testing.T) {
	r, store := setupLeaguePlayoffAuthRouter(t)

	// Set up a password-protected tournament so auth middleware activates.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest(http.MethodPost, "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	// No X-Tournament-Password header.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "POST /league-playoff must be admin-gated")
}

// TestLeaguePlayoffDelete_RequiresAuth verifies that the DELETE mutation
// returns 401 without auth.
func TestLeaguePlayoffDelete_RequiresAuth(t *testing.T) {
	r, store := setupLeaguePlayoffAuthRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest(http.MethodDelete, "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "DELETE /league-playoff must be admin-gated")
}

// TestLeaguePlayoffFinalize_RequiresAuth verifies that the POST /finalize
// mutation returns 401 without auth.
func TestLeaguePlayoffFinalize_RequiresAuth(t *testing.T) {
	r, store := setupLeaguePlayoffAuthRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/competitions/comp-1/league-playoff/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "POST /league-playoff/finalize must be admin-gated")
}

// ---------------------------------------------------------------------------
// recordingBroadcaster — captures broadcast calls for assertions.
// ---------------------------------------------------------------------------

type recordingBroadcaster struct {
	events []EventType
}

func (b *recordingBroadcaster) Broadcast(t EventType, _ any) {
	b.events = append(b.events, t)
}

// ---------------------------------------------------------------------------
// Tri-review fixes: duplicate names, partial-group, running-match guards
// ---------------------------------------------------------------------------

// TestLeaguePlayoffPost_DuplicateNames covers the candidacy-gate bypass: a
// duplicated team name must be rejected up front, not silently deduped into a
// smaller group that matches a larger candidate group.
func TestLeaguePlayoffPost_DuplicateNames(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	// Add a third team so {A,A,B} (len 3) would have matched a 3-team group
	// under the old raw-len comparison.
	candidates[0].Teams = append(candidates[0].Teams, state.PlayerStanding{Player: domain.Player{Name: "Team C"}})
	candidates[0].MaxPosition = 3
	eng := &stubLeaguePlayoffEngine{candidates: candidates}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate")
}

// TestLeaguePlayoffDelete_DuplicateNames — DELETE must reject duplicates too.
func TestLeaguePlayoffDelete_DuplicateNames(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team A"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate")
}

// TestLeaguePlayoffDelete_PartialGroup — naming only part of a play-off group
// (a DH match with exactly one side in the request) must be rejected so the
// remaining round-robin bouts aren't orphaned.
func TestLeaguePlayoffDelete_PartialGroup(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	// A 3-team round-robin play-off: A-B, A-C, B-C all unscored.
	store := &stubLeaguePlayoffStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
		matches: []state.MatchResult{
			{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B"},
			{ID: "Pool A-DH-1", SideA: "Team A", SideB: "Team C"},
			{ID: "Pool A-DH-2", SideA: "Team B", SideB: "Team C"},
		},
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	// Request only {A,B}: the A-C and B-C matches each have one side in the set.
	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "complete play-off group")
}

// TestLeaguePlayoffDelete_RunningMatch — an in-progress DH match must block
// deletion (409), not be silently removed out from under the scoring session.
func TestLeaguePlayoffDelete_RunningMatch(t *testing.T) {
	eng := &stubLeaguePlayoffEngine{}
	store := &stubLeaguePlayoffStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
		matches: []state.MatchResult{
			{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B", Status: state.MatchStatusRunning},
		},
	}
	r := leaguePlayoffRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leaguePlayoffRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-playoff", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
}
