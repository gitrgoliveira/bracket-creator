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

	// receivedTeamIDs captures the tiedTeamIDs argument GenerateLeagueTiebreakMatches
	// was last called with, so a test can assert the handler forwarded
	// req.TeamIDs through (bc-idfx) rather than silently dropping it.
	receivedTeamIDs []string
}

func (e *stubLeagueTiebreakEngine) LeagueTiebreakCandidates(string) ([]engine.TiedGroup, error) {
	return e.candidates, e.candidatesErr
}

func (e *stubLeagueTiebreakEngine) GenerateLeagueTiebreakMatches(compID string, tiedTeamNames []string, tiedTeamIDs []string) ([]state.MatchResult, error) {
	e.receivedTeamIDs = tiedTeamIDs
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

// makeTiedGroupWithIDs builds a TiedGroup for two teams carrying participant
// ids. Selection is id-only (operator ruling bc-pnum): a candidate group can
// only be matched by a request's teamIds against each team's Player.ID, so
// any test exercising that match (rather than just reading candidates back)
// needs a group built with this helper instead of the id-less makeTiedGroup.
func makeTiedGroupWithIDs(teamA, idA, teamB, idB string, minPos, maxPos int) engine.TiedGroup {
	return engine.TiedGroup{
		Teams: []state.PlayerStanding{
			{Player: domain.Player{ID: idA, Name: teamA}},
			{Player: domain.Player{ID: idB, Name: teamB}},
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

// TestLeagueTiebreakCandidates_TeamsCarryIdentity is the bc-idfx finding 11
// regression: the "teams" array (id/name/dojo per team, mirroring what
// GET /chusen-candidates already emits) must be present alongside the
// legacy "teamNames" array, so a namesake-holding group's members can be
// told apart on the wire.
func TestLeagueTiebreakCandidates_TeamsCarryIdentity(t *testing.T) {
	candidates := []engine.TiedGroup{
		{
			Teams: []state.PlayerStanding{
				{Player: domain.Player{ID: "id-team-x-a", Name: "Team X", Dojo: "Dojo A"}},
				{Player: domain.Player{ID: "id-team-x-b", Name: "Team X", Dojo: "Dojo B"}},
			},
			MinPosition: 1, MaxPosition: 2,
		},
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-tiebreak/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var body struct {
		Candidates []struct {
			TeamNames []string `json:"teamNames"`
			Teams     []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Dojo string `json:"dojo"`
			} `json:"teams"`
		} `json:"candidates"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&body))
	require.Len(t, body.Candidates, 1)
	require.Len(t, body.Candidates[0].Teams, 2, "teams must carry both namesake-holding entries")
	gotIDs := []string{body.Candidates[0].Teams[0].ID, body.Candidates[0].Teams[1].ID}
	assert.ElementsMatch(t, []string{"id-team-x-a", "id-team-x-b"}, gotIDs)
	gotDojos := []string{body.Candidates[0].Teams[0].Dojo, body.Candidates[0].Teams[1].Dojo}
	assert.ElementsMatch(t, []string{"Dojo A", "Dojo B"}, gotDojos, "dojo must disambiguate what teamNames alone cannot")
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

// TestLeagueTiebreakCandidates_CorruptOverrides is bc-pnum gap 3:
// LeagueTiebreakCandidates calls CalculatePoolStandings, which loads
// overrides.json. Before this fix a wrapped state.ErrCorruptOverrides fell
// through to this handler's generic 500 branch alongside every other engine
// error; the fix maps it to the same terminal 422 corrupt_overrides every
// other LoadOverrides-reaching endpoint answers with, via the shared
// respondIfCorruptOverrides helper (errors.go).
func TestLeagueTiebreakCandidates_CorruptOverrides(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{candidatesErr: fmt.Errorf("compute standings: %w", state.ErrCorruptOverrides)}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	req := httptest.NewRequest("GET", "/api/competitions/comp-1/league-tiebreak/candidates", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "corrupt_overrides")
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
		makeTiedGroupWithIDs("Team A", "id-a", "Team B", "id-b", 1, 2),
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

	// teamIds is required (operator ruling bc-pnum); teamNames is bound but
	// no longer read for selection.
	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
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

// TestLeagueTiebreakPost_TeamIDsSelectsNamesakeGroupAndForwards is the
// bc-idfx finding 11 regression: a request naming a namesake-holding group
// by teamIds (not resolvable by teamNames alone, since both teams share the
// name "Team X") must match the candidate group by id and forward teamIds
// through to GenerateLeagueTiebreakMatches unchanged.
func TestLeagueTiebreakPost_TeamIDsSelectsNamesakeGroupAndForwards(t *testing.T) {
	candidates := []engine.TiedGroup{
		{
			Teams: []state.PlayerStanding{
				{Player: domain.Player{ID: "id-team-x-a", Name: "Team X", Dojo: "Dojo A"}},
				{Player: domain.Player{ID: "id-team-x-b", Name: "Team X", Dojo: "Dojo B"}},
			},
			MinPosition: 1, MaxPosition: 2,
		},
	}
	generated := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team X", SideAID: "id-team-x-a", SideB: "Team X", SideBID: "id-team-x-b"},
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates, generated: generated}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools), matches: nil}
	hub := &recordingBroadcaster{}
	r := leagueTiebreakRouter(eng, store, hub)

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team X", "Team X"},
		TeamIDs:   []string{"id-team-x-a", "id-team-x-b"},
	})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
	assert.ElementsMatch(t, []string{"id-team-x-a", "id-team-x-b"}, eng.receivedTeamIDs,
		"the handler must forward teamIds to GenerateLeagueTiebreakMatches, not silently drop it")
}

// TestLeagueTiebreakPost_MismatchedTeamIDsLength used to pin a dedicated
// 1:1 length check between teamIds and teamNames. That check no longer
// exists (operator ruling bc-pnum: teamNames is display-only and is never
// compared against teamIds). This still 400s, but now via the "teamIds
// must contain at least two teams" floor: the single id supplied here is
// below that floor regardless of how many teamNames accompany it.
func TestLeagueTiebreakPost_MismatchedTeamIDsLength(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a"},
	})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestLeagueTiebreakPost_BlankTeamIDsEntryRejected pins the second-Opus-pass
// item 4 fix: dedupedStringSet has no opinion on what the strings ARE, so a
// single "" entry in teamIds deduped cleanly and was passed straight through
// to the group-match check as if it were a real participant id. Every
// id-less DH row (SideAID/SideBID both "") would then match that "" entry on
// BOTH sides, group membership for a request that supplied no real id at
// all. Rejected outright before it ever reaches dedupedStringSet.
func TestLeagueTiebreakPost_BlankTeamIDsEntryRejected(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"", "id-b"},
	})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "teamIds entries must be non-empty")
}

// TestLeagueTiebreakPost_InvalidSelection was previously driven by a
// teamNames-only body naming teams not in any candidate group. Under the
// id-only contract (operator ruling bc-pnum) a teamNames-only body 400s
// earlier, on the "teamIds must contain at least two teams" floor, and
// never reaches the candidate-group match at all; that floor case is
// already covered by TestLeagueTiebreakPost_TooFewTeams. Converted to
// supply a well-formed teamIds set that still matches no candidate group,
// so this test continues to exercise the "does not match any consequential
// tied group" rejection itself.
func TestLeagueTiebreakPost_InvalidSelection(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroupWithIDs("Team A", "id-a", "Team B", "id-b", 1, 2),
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	// Request for teamIds not present in any candidate group.
	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team X", "Team Y"},
		TeamIDs:   []string{"id-x", "id-y"},
	})
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
		makeTiedGroupWithIDs("Team A", "id-a", "Team B", "id-b", 1, 2),
	}
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideAID: "id-a", SideB: "Team B", SideBID: "id-b"},
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestLeagueTiebreakPost_LoadMatchesError(t *testing.T) {
	// When LoadPoolMatches fails, the handler must return 500.
	candidates := []engine.TiedGroup{
		makeTiedGroupWithIDs("Team A", "id-a", "Team B", "id-b", 1, 2),
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{
		comp:       makeTeamLeagueComp(state.CompStatusPools),
		matchesErr: fmt.Errorf("disk error"),
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
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
		makeTiedGroupWithIDs("Team A", "id-a", "Team B", "id-b", 1, 2),
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

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
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
		{ID: "Pool A-DH-0", SideA: "Team A", SideAID: "id-a", SideB: "Team B", SideBID: "id-b", Status: state.MatchStatusScheduled},
	}
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	hub := &recordingBroadcaster{}
	r := leagueTiebreakRouter(eng, store, hub)

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
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

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestLeagueTiebreakDelete_ScoredMatch(t *testing.T) {
	existing := []state.MatchResult{
		{
			ID:    "Pool A-DH-0",
			SideA: "Team A", SideAID: "id-a",
			SideB: "Team B", SideBID: "id-b",
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

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
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

// TestLeagueTiebreakDelete_TeamIDsRemovesNamesakeGroup pins the bc-idfx
// review's item 9 fix: a namesake tie-breaker group (two "Team X" from
// different dojos) can only ever be CREATED via teamIds -- POST's own
// teamNames-only path collapses the duplicate name and is rejected -- and
// generatePoolDaihyosenMatches stamps SideAID/SideBID on the DH row it
// writes for exactly that reason. Before the fix, DELETE had no teamIds
// counterpart at all: a name-only delete request also collapses the
// duplicate name in dedupedStringSet and is rejected before ever reaching the
// group match, so such a group could be created but never removed.
func TestLeagueTiebreakDelete_TeamIDsRemovesNamesakeGroup(t *testing.T) {
	existing := []state.MatchResult{
		{
			ID:    "Pool A-DH-0",
			SideA: "Team X", SideAID: "id-team-x-a",
			SideB: "Team X", SideBID: "id-team-x-b",
			Status: state.MatchStatusScheduled,
		},
	}
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	hub := &recordingBroadcaster{}
	r := leagueTiebreakRouter(eng, store, hub)

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team X", "Team X"},
		TeamIDs:   []string{"id-team-x-a", "id-team-x-b"},
	})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
	var resp map[string]any
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, float64(1), resp["deleted"])
	assert.Empty(t, store.matches, "the namesake group's DH row must actually be removed")
}

// TestLeagueTiebreakDelete_NamesakeGroup_DuplicateNamesPointsAtTeamIDs
// originally pinned a teamNames-only delete of a namesake group: names
// can't disambiguate the pair, so the request was rejected, but the error
// message had to point the operator at teamIds. Under the id-only contract
// (operator ruling bc-pnum) a teamNames-only body no longer reaches any
// duplicate check at all -- it 400s earlier on "teamIds must contain at
// least two teams", which names the field but says nothing about
// duplicates. Converted to send a DUPLICATE teamIds entry instead (the only
// remaining way to reach the "teamIds contains duplicate entries" message),
// keeping the namesake-group fixture and the "nothing removed" assertion so
// this still exercises the DELETE call site's own parseTiebreakSelection use.
func TestLeagueTiebreakDelete_NamesakeGroup_DuplicateNamesPointsAtTeamIDs(t *testing.T) {
	existing := []state.MatchResult{
		{
			ID:    "Pool A-DH-0",
			SideA: "Team X", SideAID: "id-team-x-a",
			SideB: "Team X", SideBID: "id-team-x-b",
			Status: state.MatchStatusScheduled,
		},
	}
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team X", "Team X"},
		TeamIDs:   []string{"id-team-x-a", "id-team-x-a"},
	})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate")
	assert.Contains(t, w.Body.String(), "teamIds", "the message must point the operator at the disambiguating field")
	assert.Len(t, store.matches, 1, "the rejected request must not remove anything")
}

// TestLeagueTiebreakDelete_BlankTeamIDsEntryRejected pins the second-Opus-pass
// item 4 fix. Before it, a blank teamIds entry deduped to "" and matched
// BOTH sides of every id-less DH row -- reproduced here with TWO id-less
// DH rows from entirely UNRELATED groups (Team C/Team D and Team E/Team F);
// a request naming neither group, but carrying one blank teamIds entry,
// used to delete both of them (200 {"deleted":2}) instead of being rejected.
func TestLeagueTiebreakDelete_BlankTeamIDsEntryRejected(t *testing.T) {
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team C", SideB: "Team D", Status: state.MatchStatusScheduled},
		{ID: "Pool A-DH-1", SideA: "Team E", SideB: "Team F", Status: state.MatchStatusScheduled},
	}
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	// Neither "Alice" nor "Bob" names any real group; the blank teamIds
	// entry is what the pre-fix code silently matched every id-less row on.
	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Alice", "Bob"},
		TeamIDs:   []string{"", "id-bob"},
	})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "teamIds entries must be non-empty")
	assert.Len(t, store.matches, 2, "neither unrelated group's bout may be removed")
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

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestLeagueTiebreakPost_CandidatesCorruptOverrides is the POST-validation
// sibling of TestLeagueTiebreakCandidates_CorruptOverrides: this handler also
// calls LeagueTiebreakCandidates (to validate the selection before
// generating), reaching the identical LoadOverrides call. Same fix, same
// mapping.
func TestLeagueTiebreakPost_CandidatesCorruptOverrides(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{
		candidatesErr: fmt.Errorf("compute standings: %w", state.ErrCorruptOverrides),
	}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "corrupt_overrides")
}

// POST, GenerateLeagueTiebreakMatches returns *engine.NotFoundError (404).
func TestLeagueTiebreakPost_GenerateNotFound(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroupWithIDs("Team A", "id-a", "Team B", "id-b", 1, 2),
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

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// POST, GenerateLeagueTiebreakMatches returns a generic error (500).
func TestLeagueTiebreakPost_GenerateInternalError(t *testing.T) {
	candidates := []engine.TiedGroup{
		makeTiedGroupWithIDs("Team A", "id-a", "Team B", "id-b", 1, 2),
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

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
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

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// DELETE, SavePoolMatches returns an error after removal (500).
func TestLeagueTiebreakDelete_SaveError(t *testing.T) {
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team A", SideAID: "id-a", SideB: "Team B", SideBID: "id-b", Status: state.MatchStatusScheduled},
	}
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
		saveErr: fmt.Errorf("write failure"),
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
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

// TestLeagueTiebreakPost_DuplicateNames used to cover a candidacy-gate
// bypass reachable through a duplicated team NAME: {A,A,B} (len 3) could
// match a 3-team candidate group under a raw-length comparison. Under the
// id-only contract (operator ruling bc-pnum) selection is teamIds-only, and
// parseTiebreakSelection's dedupedStringSet check runs on teamIds BEFORE
// the handler ever loads candidates, so the equivalent bypass is now a
// duplicated team ID, and it is rejected outright rather than silently
// deduped -- there is no candidates/store setup left to construct the
// original bypass shape with, since the request never reaches that code.
func TestLeagueTiebreakPost_DuplicateNames(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-a", "id-b"},
	})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate")
}

// TestLeagueTiebreakDelete_DuplicateNames, DELETE must reject duplicate
// teamIds too (teamNames is display-only and is never checked for
// duplicates under the id-only contract, operator ruling bc-pnum).
func TestLeagueTiebreakDelete_DuplicateNames(t *testing.T) {
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools)}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team A"},
		TeamIDs:   []string{"id-a", "id-a"},
	})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "duplicate")
}

// TestLeagueTiebreakDelete_TeamIDs_LegacyIDlessRowNotRemovable is the
// converted twin of the deleted
// TestLeagueTiebreakDelete_TeamIDs_LegacyIDlessRowStillRemovable, which
// pinned inGroup's pre-bc-pnum row-level id-or-name fallback:
// generatePoolDaihyosenMatches only began stamping SideAID/SideBID on
// 2026-08-29, so a DH row written before that carries blank ids, and the
// fallback let a teamIds-based DELETE still find such a row by matching its
// names. The operator ruling bc-pnum removed that fallback entirely --
// inGroup is now purely `ids[m.SideAID], ids[m.SideBID]` -- so a row with no
// id on a side is never a member of any group on that side, full stop. This
// pins the new, OPPOSITE behaviour: the legacy id-less row is invisible to
// a teamIds-based DELETE, so the request finds no rows in the group at all
// (404 no_tiebreak_matches) and the row survives untouched.
func TestLeagueTiebreakDelete_TeamIDs_LegacyIDlessRowNotRemovable(t *testing.T) {
	existing := []state.MatchResult{
		{
			ID:     "Pool A-DH-0",
			SideA:  "Team Alpha", // no SideAID: legacy row
			SideB:  "Team Beta",  // no SideBID: legacy row
			Status: state.MatchStatusScheduled,
		},
	}
	eng := &stubLeagueTiebreakEngine{}
	store := &stubLeagueTiebreakStore{
		comp:    makeTeamLeagueComp(state.CompStatusPools),
		matches: existing,
	}
	hub := &recordingBroadcaster{}
	r := leagueTiebreakRouter(eng, store, hub)

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team Alpha", "Team Beta"},
		TeamIDs:   []string{"id-alpha", "id-beta"},
	})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equalf(t, http.StatusNotFound, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "no_tiebreak_matches")
	assert.Len(t, store.matches, 1, "the legacy id-less row must survive untouched, it was never in the selected group")
}

// TestLeagueTiebreakPost_TeamIDs_LegacyIDlessRowDoesNotBlockRegeneration is
// POST's converted twin of the DELETE test above (originally
// TestLeagueTiebreakPost_TeamIDs_LegacyIDlessRowBlocksRegeneration, which
// pinned the row-level id-or-name fallback counting this row towards
// pairsExist and refusing regeneration 409). Under the id-only inGroup
// (operator ruling bc-pnum), the legacy id-less row is invisible to the
// id-based pairsExist count -- neither side matches any real id -- so the
// "already exists" guard never fires and the request proceeds to generate
// a second, redundant set of tie-breaker matches (masked here, as before,
// by the stub's zero-value GenerateLeagueTiebreakMatches returning no
// matches and no error).
func TestLeagueTiebreakPost_TeamIDs_LegacyIDlessRowDoesNotBlockRegeneration(t *testing.T) {
	candidates := []engine.TiedGroup{
		{
			Teams: []state.PlayerStanding{
				{Player: domain.Player{ID: "id-alpha", Name: "Team Alpha", Dojo: "Dojo A"}},
				{Player: domain.Player{ID: "id-beta", Name: "Team Beta", Dojo: "Dojo B"}},
			},
			MinPosition: 1, MaxPosition: 2,
		},
	}
	existing := []state.MatchResult{
		{ID: "Pool A-DH-0", SideA: "Team Alpha", SideB: "Team Beta"}, // legacy row, no ids
	}
	eng := &stubLeagueTiebreakEngine{candidates: candidates}
	store := &stubLeagueTiebreakStore{comp: makeTeamLeagueComp(state.CompStatusPools), matches: existing}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team Alpha", "Team Beta"},
		TeamIDs:   []string{"id-alpha", "id-beta"},
	})
	req := httptest.NewRequest("POST", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equalf(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
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
			{ID: "Pool A-DH-0", SideA: "Team A", SideAID: "id-a", SideB: "Team B", SideBID: "id-b"},
			{ID: "Pool A-DH-1", SideA: "Team A", SideAID: "id-a", SideB: "Team C", SideBID: "id-c"},
			{ID: "Pool A-DH-2", SideA: "Team B", SideAID: "id-b", SideB: "Team C", SideBID: "id-c"},
		},
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	// Request only {A,B}: the A-C and B-C matches each have one side in the set.
	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
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
			{ID: "Pool A-DH-0", SideA: "Team A", SideAID: "id-a", SideB: "Team B", SideBID: "id-b", Status: state.MatchStatusRunning},
		},
	}
	r := leagueTiebreakRouter(eng, store, stubBroadcaster{})

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
	req := httptest.NewRequest("DELETE", "/api/competitions/comp-1/league-tiebreak", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
}

// TestLeagueTiebreakDelete_RejectsNonTeamLeague pins the Copilot fix: the
// league-only DELETE must refuse a non-league (e.g. mixed) competition so an
// operator can't delete a mixed team comp's auto-injected DH matches through it.
// The notTeamLeague guard fires right after LoadCompetition, before the DH
// rows are ever inspected, so the fixture's ids don't need to line up with
// anything; teamIds just needs to pass parseTiebreakSelection's floor.
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

	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})
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
// not team-leagues, returning 400 without injecting any matches. Like the
// DELETE test above, the notTeamLeague guard fires before candidates are
// ever loaded, so teamIds just needs to pass parseTiebreakSelection's floor.
func TestLeagueTiebreakPost_RejectsNonTeamLeague(t *testing.T) {
	body := jsonBody(leagueTiebreakRequest{
		TeamNames: []string{"Team A", "Team B"},
		TeamIDs:   []string{"id-a", "id-b"},
	})

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
