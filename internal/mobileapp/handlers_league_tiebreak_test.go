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

// stubLeagueTiebreakStore implements LeagueTiebreakStore for handler tests.
type stubLeagueTiebreakStore struct {
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

func (s *stubLeagueTiebreakStore) LoadCompetition(id string) (*state.Competition, error) {
	return s.comp, s.loadErr
}

func (s *stubLeagueTiebreakStore) LoadPoolMatches(id string) ([]state.MatchResult, error) {
	return s.matches, s.matchesErr
}

func (s *stubLeagueTiebreakStore) SavePoolMatches(id string, matches []state.MatchResult) error {
	if s.saveErr == nil {
		s.matches = matches
	}
	return s.saveErr
}

// WithTransaction runs fn against a stub StoreTx that delegates the three
// methods the DELETE handler uses (LoadCompetition / LoadPoolMatches /
// SavePoolMatches) back to this stub. The DELETE read-modify-write is the only
// transactional path in this handler family.
func (s *stubLeagueTiebreakStore) WithTransaction(compID string, fn func(tx state.StoreTx) error) error {
	return fn(&stubLeagueTiebreakTx{store: s})
}

// stubLeagueTiebreakTx satisfies state.StoreTx by embedding the interface (so
// the type checks) and implementing only the methods the DELETE handler calls.
// Any other method would panic, none are reached by these tests.
type stubLeagueTiebreakTx struct {
	state.StoreTx
	store *stubLeagueTiebreakStore
}

func (t *stubLeagueTiebreakTx) LoadCompetition(id string) (*state.Competition, error) {
	return t.store.comp, t.store.loadErr
}

func (t *stubLeagueTiebreakTx) LoadPoolMatches(id string) ([]state.MatchResult, error) {
	return t.store.matches, t.store.matchesErr
}

func (t *stubLeagueTiebreakTx) SavePoolMatches(id string, matches []state.MatchResult) error {
	if t.store.saveErr == nil {
		t.store.matches = matches
	}
	return t.store.saveErr
}

func (s *stubLeagueTiebreakStore) UpdateCompetitionChanged(id string, transform func(*state.Competition) (*state.Competition, error)) (bool, error) {
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

// stubLeagueTiebreakEngine implements LeagueTiebreakEngine for handler tests.
type stubLeagueTiebreakEngine struct {
	candidates    []engine.TiedGroup
	candidatesErr error
	generated     []state.MatchResult
	generateErr   error
	autoOutcome   engine.AutoCompleteOutcome
	autoErr       error
}

func (e *stubLeagueTiebreakEngine) LeagueTiebreakCandidates(string) ([]engine.TiedGroup, error) {
	return e.candidates, e.candidatesErr
}

func (e *stubLeagueTiebreakEngine) GenerateLeagueTiebreakMatches(compID string, tiedTeamNames []string) ([]state.MatchResult, error) {
	return e.generated, e.generateErr
}

func (e *stubLeagueTiebreakEngine) MaybeAutoCompletePools(string) (engine.AutoCompleteOutcome, error) {
	return e.autoOutcome, e.autoErr
}

// leagueTiebreakRouter sets up a gin engine with all league-tiebreak handlers
// wired on the same unauthenticated group, matching the old test layout.
// This is used by the business-logic tests (happy/error paths) where we
// test handler behaviour, not auth enforcement.
func leagueTiebreakRouter(eng LeagueTiebreakEngine, store LeagueTiebreakStore, hub Broadcaster) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	g := r.Group("/api")
	// Public (unauthenticated) read endpoint.
	RegisterPublicLeagueTiebreakHandlers(g, eng, store)
	// Mutation endpoints, no auth middleware here; business logic only.
	RegisterLeagueTiebreakHandlers(g, eng, store, hub)
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
// GET /competitions/:id/league-tiebreak/candidates
// ---------------------------------------------------------------------------

func TestLeagueTiebreakCandidates_Happy(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-tiebreak/candidates", nil)
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

func TestLeagueTiebreakCandidates_Empty(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{candidates: nil}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-tiebreak/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	cands := body["candidates"].([]any)
	assert.Len(t, cands, 0)
}

func TestLeagueTiebreakCandidates_CompNotFound(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: nil}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-tiebreak/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestLeagueTiebreakCandidates_EngineError(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{candidatesErr: fmt.Errorf("engine error")}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-tiebreak/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestLeagueTiebreakCandidates_Finalized(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{candidates: nil}
	comp := makeTeamLeagueComp(state.CompStatusPools)
	comp.LeagueTiebreakFinalized = true
	store := &stubLeagueTiebreakStore{comp: comp}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-tiebreak/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	assert.Equal(t, true, body["finalized"])
}

// ---------------------------------------------------------------------------
// POST /competitions/:id/league-tiebreak
// ---------------------------------------------------------------------------

func TestLeagueTiebreakPost_Happy(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	generated := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B"},
	}
	eng := &stubLeagueTiebreakEngine{
		candidates: candidates,
		generated:  generated,
	}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil, // no existing DH matches
	}
	hub := &recordingBroadcaster{}
	r := leagueTiebreakRouter(eng, store, hub)

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
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

func TestLeagueTiebreakPost_InvalidSelection(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	// Request for teams not in any candidate group.
	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team X", "Team Y"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLeagueTiebreakPost_TooFewTeams(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLeagueTiebreakPost_AlreadyExists(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B"},
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestLeagueTiebreakPost_LoadMatchesError(t *testing.T) {
	// When LoadPoolMatches fails, the handler must return 500.
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{
		comp:       makeTeamLeagueComp(state.CompStatusPools),
		matchesErr: fmt.Errorf("disk error"),
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestLeagueTiebreakPost_EngineValidationError(t *testing.T) {
	// GenerateLeagueTiebreakMatches can return a ValidationError when the
	// competition is not a team-league type. The handler must map that to 400.
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeagueTiebreakEngine{
		candidates:  candidates,
		generateErr: &engine.ValidationError{Msg: "not a team-league competition"},
	}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil,
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestLeagueTiebreakPost_BadBody(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// DELETE /competitions/:id/league-tiebreak
// ---------------------------------------------------------------------------

func TestLeagueTiebreakDelete_Happy(t *testing.T) {
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B", Status: state.MatchStatusScheduled},
	}
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	hub := &recordingBroadcaster{}
	r := leagueTiebreakRouter(eng, store, hub)

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, float64(1), resp["deleted"])
	assert.GreaterOrEqual(t, len(hub.events), 2)
}

func TestLeagueTiebreakDelete_NotFound(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil, // no matches
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestLeagueTiebreakDelete_ScoredMatch(t *testing.T) {
	existing := []state.MatchResult{
		{
			ID:     "Pool A-DH-0",
			SideA:  "Team A",
			SideB:  "Team B",
			Winner: "Team A",
			Status: state.MatchStatusCompleted,
		},
	}
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestLeagueTiebreakDelete_TooFewTeams(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// ---------------------------------------------------------------------------
// POST /competitions/:id/league-tiebreak/finalize
// ---------------------------------------------------------------------------

func TestLeagueTiebreakFinalize_Happy(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{
		autoOutcome: engine.AutoCompleteTransitioned,
	}
	store := &stubLeagueTiebreakStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
	}
	hub := &recordingBroadcaster{}
	r := leagueTiebreakRouter(eng, store, hub)

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, true, resp["finalized"])
	// Competition should have LeagueTiebreakFinalized=true set in the store.
	assert.True(t, store.comp.LeagueTiebreakFinalized)
	// CompetitionCompleted event should have been broadcast.
	assert.GreaterOrEqual(t, len(hub.events), 1)
}

func TestLeagueTiebreakFinalize_CompNotFound(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: nil}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestLeagueTiebreakFinalize_AlreadyComplete(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp: makeTeamLeagueComp(state.CompStatusComplete),
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestLeagueTiebreakFinalize_MaybeAutoCompleteError(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{
		autoErr: fmt.Errorf("engine error"),
	}
	store := &stubLeagueTiebreakStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Still returns 200 with the finalized flag (same pattern as tryAutoCompletePools,
	// the score itself succeeded; the auto-complete failure is a background concern
	// surfaced via the error header).
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, AutoCompleteErrorValue, w.Header().Get(AutoCompleteErrorHeader))
}

func TestLeagueTiebreakFinalize_NoChange(t *testing.T) {
	// AutoCompleteNoChange = not all matches done yet, but finalized flag is set.
	eng := &stubLeagueTiebreakEngine{
		autoOutcome: engine.AutoCompleteNoChange,
	}
	store := &stubLeagueTiebreakStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
	}
	hub := &recordingBroadcaster{}
	r := leagueTiebreakRouter(eng, store, hub)

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	assert.True(t, store.comp.LeagueTiebreakFinalized)
	// EventScheduleUpdated should have been broadcast even with NoChange.
	assert.GreaterOrEqual(t, len(hub.events), 1)
}

// ---------------------------------------------------------------------------
// Additional error-path tests to bring internal/mobileapp to ≥85% coverage
// ---------------------------------------------------------------------------

// GET, LoadCompetition returns an error (500).
func TestLeagueTiebreakCandidates_LoadError(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{loadErr: fmt.Errorf("disk I/O failure")}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-tiebreak/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// GET, LeagueTiebreakCandidates returns *engine.NotFoundError (404).
func TestLeagueTiebreakCandidates_EngineNotFound(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{
		candidatesErr: &engine.NotFoundError{Msg: "competition not in engine"},
	}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-tiebreak/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// GET, requireValidCompID returns false (empty: id → 400).
func TestLeagueTiebreakCandidates_InvalidID(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	// A route param that fails ValidateCompetitionID (e.g. empty string via
	// a sub-path that doesn't carry: id, use a contrived bad value).
	req := httptest.NewRequest("GET", "/api/competitions/%00/league-tiebreak/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Either 400 (invalid ID) or 404 (gin route not matched) is acceptable;
	// what matters is it does NOT return 200 or 500.
	assert.NotEqual(t, http.StatusOK, w.Code)
	assert.NotEqual(t, http.StatusInternalServerError, w.Code)
}

// POST, LeagueTiebreakCandidates returns *engine.NotFoundError (404).
func TestLeagueTiebreakPost_CandidatesNotFound(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{
		candidatesErr: &engine.NotFoundError{Msg: "competition not in engine"},
	}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// POST, GenerateLeagueTiebreakMatches returns *engine.NotFoundError (404).
func TestLeagueTiebreakPost_GenerateNotFound(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeagueTiebreakEngine{
		candidates:  candidates,
		generateErr: &engine.NotFoundError{Msg: "competition vanished"},
	}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil,
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// POST, GenerateLeagueTiebreakMatches returns a generic error (500).
func TestLeagueTiebreakPost_GenerateInternalError(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	eng := &stubLeagueTiebreakEngine{
		candidates:  candidates,
		generateErr: fmt.Errorf("unexpected engine failure"),
	}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: nil,
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// DELETE, LoadPoolMatches returns an error (500).
func TestLeagueTiebreakDelete_LoadMatchesError(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:       makeTeamLeagueComp(state.CompStatusPools),
		matchesErr: fmt.Errorf("store unavailable"),
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// DELETE, SavePoolMatches returns an error after removal (500).
func TestLeagueTiebreakDelete_SaveError(t *testing.T) {
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B", Status: state.MatchStatusScheduled},
	}
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
		saveErr: fmt.Errorf("write failure"),
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// DELETE, bad request body (400).
func TestLeagueTiebreakDelete_BadBody(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", bytes.NewBufferString("not json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// Finalize, UpdateCompetitionChanged returns an error (500).
func TestLeagueTiebreakFinalize_UpdateError(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:      makeTeamLeagueComp(state.CompStatusPools),
		updateErr: fmt.Errorf("transaction failure"),
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// Finalize, AutoCompleteTransitioned broadcasts EventCompetitionCompleted.
func TestLeagueTiebreakFinalize_BroadcastsCompleted(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{
		autoOutcome: engine.AutoCompleteTransitioned,
	}
	store := &stubLeagueTiebreakStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
	}
	hub := &recordingBroadcaster{}
	r := leagueTiebreakRouter(eng, store, hub)

	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak/finalize", nil)
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
// Auth-split tests, verify GET is public and POST is admin-gated.
//
// These tests use a real state.Store (temporary directory) and the real
// AuthMiddleware, mirroring the pattern in handlers_eligibility_test.go.
// They are the primary evidence that the production server.go wiring is
// correct: GET /candidates serves 200 without X-Tournament-Password;
// POST /league-tiebreak returns 401 without it.
// ---------------------------------------------------------------------------

// setupLeagueTiebreakAuthRouter builds a router that mirrors the production
// split: GET /candidates on the public api group, mutations on the admin group.
func setupLeagueTiebreakAuthRouter(t *testing.T) (*gin.Engine, *state.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir, err := os.MkdirTemp("", "lp-auth-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	r := gin.New()
	// Stub engine: returns no candidates (enough for a 200 on GET).
	eng := &stubLeagueTiebreakEngine{candidates: nil}
	hub := stubBroadcaster{}

	// Public group, GET /candidates is unauthenticated.
	api := r.Group("/api")
	RegisterPublicLeagueTiebreakHandlers(api, eng, store)

	// Admin-gated group, mutations require X-Tournament-Password.
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterLeagueTiebreakHandlers(admin, eng, store, hub)

	return r, store
}

// TestLeagueTiebreakCandidates_IsPublic is the primary regression test for
// the bug reported in mp-8rc9 Phase 3b: GET /candidates returned 401 because
// the endpoint was registered on the admin group. It must be 200 without any
// X-Tournament-Password header, even for a password-protected tournament.
func TestLeagueTiebreakCandidates_IsPublic(t *testing.T) {
	r, store := setupLeagueTiebreakAuthRouter(t)

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

	req := httptest.NewRequest(http.MethodGet, "/api/competitions/comp-1/league-tiebreak/candidates", nil)
	// Deliberately no X-Tournament-Password header.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "GET /candidates must be public (no auth header)")
}

// TestLeagueTiebreakPost_RequiresAuth verifies that the POST mutation returns
// 401 when called without X-Tournament-Password, i.e. the route is still on
// the admin-gated group after the public/admin split.
func TestLeagueTiebreakPost_RequiresAuth(t *testing.T) {
	r, store := setupLeagueTiebreakAuthRouter(t)

	// Set up a password-protected tournament so auth middleware activates.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest(http.MethodPost, "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	// No X-Tournament-Password header.
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "POST /league-tiebreak must be admin-gated")
}

// TestLeagueTiebreakDelete_RequiresAuth verifies that the DELETE mutation
// returns 401 without auth.
func TestLeagueTiebreakDelete_RequiresAuth(t *testing.T) {
	r, store := setupLeagueTiebreakAuthRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest(http.MethodDelete, "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "DELETE /league-tiebreak must be admin-gated")
}

// TestLeagueTiebreakFinalize_RequiresAuth verifies that the POST /finalize
// mutation returns 401 without auth.
func TestLeagueTiebreakFinalize_RequiresAuth(t *testing.T) {
	r, store := setupLeagueTiebreakAuthRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/competitions/comp-1/league-tiebreak/finalize", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code, "POST /league-tiebreak/finalize must be admin-gated")
}

// ---------------------------------------------------------------------------
// recordingBroadcaster, captures broadcast calls for assertions.
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

// TestLeagueTiebreakPost_DuplicateNames covers the candidacy-gate bypass: a
// duplicated team name must be rejected up front, not silently deduped into a
// smaller group that matches a larger candidate group.
func TestLeagueTiebreakPost_DuplicateNames(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroup("Team A", "Team B", 1, 2),
	}
	// Add a third team so {A,A,B} (len 3) would have matched a 3-team group
	// under the old raw-len comparison.
	candidates[0].Teams = append(candidates[0].Teams, state.PlayerStanding{Player: domain.Player{Name: "Team C"}})
	candidates[0].MaxPosition = 3
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team A", "Team B"}})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate")
}

// TestLeagueTiebreakDelete_DuplicateNames, DELETE must reject duplicates too.
func TestLeagueTiebreakDelete_DuplicateNames(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team A"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate")
}

// TestLeagueTiebreakDelete_PartialGroup, naming only part of a tie-breaker group
// (a DH match with exactly one side in the request) must be rejected so the
// remaining round-robin bouts aren't orphaned.
func TestLeagueTiebreakDelete_PartialGroup(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	// A 3-team round-robin tie-breaker: A-B, A-C, B-C all unscored.
	store := &stubLeagueTiebreakStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
		matches: []state.MatchResult{
			{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B"},
			{ID: "Pool A-DH-1", SideA: "Team A", SideB: "Team C"},
			{ID: "Pool A-DH-2", SideA: "Team B", SideB: "Team C"},
		},
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	// Request only {A,B}: the A-C and B-C matches each have one side in the set.
	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "complete tie-breaker group")
}

// TestLeagueTiebreakDelete_RunningMatch, an in-progress DH match must block
// deletion (409), not be silently removed out from under the scoring session.
func TestLeagueTiebreakDelete_RunningMatch(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp: makeTeamLeagueComp(state.CompStatusPools),
		matches: []state.MatchResult{
			{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B", Status: state.MatchStatusRunning},
		},
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
}

// TestLeagueTiebreakDelete_RejectsNonTeamLeague pins the Copilot fix: the
// league-only DELETE must refuse a non-league (e.g. mixed) competition so an
// operator can't delete a mixed team comp's auto-injected DH matches through it.
func TestLeagueTiebreakDelete_RejectsNonTeamLeague(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	mixed := makeTeamLeagueComp(state.CompStatusPools)
	mixed.Format = state.CompFormatMixed // not a league
	store := &stubLeagueTiebreakStore{
		comp: mixed,
		matches: []state.MatchResult{
			{ID: "Pool A-DH-0", SideA: "Team A", SideB: "Team B"},
		},
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	// The mixed comp's DH match must NOT have been removed.
	assert.Len(t, store.matches, 1, "non-league DELETE must not touch matches")
}

// TestLeagueTiebreakPost_RejectsNonTeamLeague pins the POST guard added in the
// same Copilot fix round: POST /league-tiebreak must refuse competitions that are
// not team-leagues, returning 400 without injecting any matches.
func TestLeagueTiebreakPost_RejectsNonTeamLeague(t *testing.T) {
	body := jsonBody(leagueTiebreakRequest{TeamNames: []string{"Team A", "Team B"}})

	t.Run("non-league format (mixed)", func(t *testing.T) {
		eng := &stubLeagueTiebreakEngine{}
		mixed := makeTeamLeagueComp(state.CompStatusPools)
		mixed.Format = state.CompFormatMixed // valid team comp, but not a league
		store := &stubLeagueTiebreakStore{comp: mixed}
		r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

		req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Empty(t, store.matches, "non-league POST must not inject any matches")
	})

	t.Run("non-team kind (individual league)", func(t *testing.T) {
		eng := &stubLeagueTiebreakEngine{}
		indv := makeTeamLeagueComp(state.CompStatusPools)
		indv.Kind = "individual" // league format but not a team comp
		indv.TeamSize = 0
		store := &stubLeagueTiebreakStore{comp: indv}
		r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

		req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Empty(t, store.matches, "individual-league POST must not inject any matches")
	})
}

// TestLeagueTiebreakFinalize_RejectsNonTeamLeague pins the finalize handler's
// team-league guard: POST /league-tiebreak/finalize must return 400 for any
// competition that is not a team-league, without mutating LeagueTiebreakFinalized.
func TestLeagueTiebreakFinalize_RejectsNonTeamLeague(t *testing.T) {
	t.Run("non-league format (mixed)", func(t *testing.T) {
		eng := &stubLeagueTiebreakEngine{}
		mixed := makeTeamLeagueComp(state.CompStatusPools)
		mixed.Format = state.CompFormatMixed
		store := &stubLeagueTiebreakStore{comp: mixed}
		r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

		req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak/finalize", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.False(t, store.comp.LeagueTiebreakFinalized, "non-league finalize must not set LeagueTiebreakFinalized")
	})

	t.Run("non-team kind (individual league)", func(t *testing.T) {
		eng := &stubLeagueTiebreakEngine{}
		indv := makeTeamLeagueComp(state.CompStatusPools)
		indv.Kind = "individual"
		indv.TeamSize = 0
		store := &stubLeagueTiebreakStore{comp: indv}
		r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

		req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak/finalize", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.False(t, store.comp.LeagueTiebreakFinalized, "individual-league finalize must not set LeagueTiebreakFinalized")
	})
}
