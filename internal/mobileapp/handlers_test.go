package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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

func setupTestRouter(t *testing.T) (*gin.Engine, *state.Store, *engine.Engine, *Hub, string) {
	tempDir, err := os.MkdirTemp("", "mobileapp-test-*")
	require.NoError(t, err)

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)

	eng := engine.New(store)
	hub := NewHub()

	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Public viewer
	viewer := r.Group("/api/viewer")
	RegisterViewerHandlers(viewer, store, eng)

	// Admin API
	admin := r.Group("/api")
	RegisterTournamentHandlers(admin, store, hub)
	RegisterImportHandlers(admin, store, hub)
	RegisterCompetitionHandlers(admin, store, eng, hub)
	RegisterParticipantHandlers(admin, store)
	RegisterMatchHandlers(admin, store, eng, hub)

	return r, store, eng, hub, tempDir
}

func TestTournamentHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Create initial tournament (no longer auto-created by store init)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Initial Tournament", Password: ""}))

	// GET /api/tournament
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var tour state.Tournament
	err := json.Unmarshal(w.Body.Bytes(), &tour)
	assert.NoError(t, err)
	assert.Equal(t, "Initial Tournament", tour.Name)

	// PUT /api/tournament
	tour.Name = "Updated Tournament"
	tour.Password = "secret"
	body, _ := json.Marshal(tour)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify update
	t2, _ := store.LoadTournament()
	assert.Equal(t, "Updated Tournament", t2.Name)
	assert.Equal(t, "secret", t2.Password)

	// PUT /api/tournament (invalid JSON)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/tournament", bytes.NewBufferString(`{invalid`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// PUT /api/tournament trims string fields. Sibling of the
	// comp.Name trim in handlers_competition.go — the CreateTournament
	// UI now trims client-side, but older clients and direct API
	// callers could still send padded values. Date is included for
	// cross-file guard symmetry with the competition/import paths
	// which trim their own Date field.
	tour.Name = "  Padded Tournament  "
	tour.Venue = "  Crystal Palace  "
	tour.Date = "  2026-05-12  "
	tour.Password = "secret"
	body, _ = json.Marshal(tour)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	t3, _ := store.LoadTournament()
	assert.Equal(t, "Padded Tournament", t3.Name)
	assert.Equal(t, "Crystal Palace", t3.Venue)
	assert.Equal(t, "2026-05-12", t3.Date, "Date should be trimmed on PUT")

	// GET /api/tournament (not found)
	os.Remove(filepath.Join(tempDir, "tournament.md"))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// GET /api/tournament (load error)
	os.Remove(filepath.Join(tempDir, "tournament.md"))
	os.Mkdir(filepath.Join(tempDir, "tournament.md"), 0755)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	// PUT /api/tournament (save error)
	os.Remove(filepath.Join(tempDir, "tournament.md"))
	os.Mkdir(filepath.Join(tempDir, "tournament.md"), 0755)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	os.RemoveAll(filepath.Join(tempDir, "tournament.md"))

	// POST /api/tournament also trims. CreateTournament in app.jsx
	// hits this endpoint on first-time setup, where the empty-check
	// `if (!name || !pass)` is truthy for whitespace, so without the
	// trim "  My Tournament  " would persist with the spaces.
	postTour := state.Tournament{Name: "  Posted Tournament  ", Venue: "  Some Venue  ", Date: "  2026-07-20  ", Password: "secret"}
	body, _ = json.Marshal(postTour)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	t4, _ := store.LoadTournament()
	assert.Equal(t, "Posted Tournament", t4.Name)
	assert.Equal(t, "Some Venue", t4.Venue)
	assert.Equal(t, "2026-07-20", t4.Date, "Date should be trimmed on POST")
}

func TestCompetitionHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// POST /api/competitions
	comp := state.Competition{
		ID:   "test-comp",
		Name: "Test Competition",
	}
	body, _ := json.Marshal(comp)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// GET /api/competitions
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var comps []state.Competition
	json.Unmarshal(w.Body.Bytes(), &comps)
	assert.Len(t, comps, 1)
	assert.Equal(t, "test-comp", comps[0].ID)

	// GET /api/competitions/:id
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions/test-comp", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// PUT /api/competitions/:id
	comp.Name = "Updated Name"
	body, _ = json.Marshal(comp)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/test-comp", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// POST /api/competitions (no ID — auto-generated from name)
	body, _ = json.Marshal(state.Competition{Name: "Missing ID"})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
	var autoComp state.Competition
	json.Unmarshal(w.Body.Bytes(), &autoComp)
	assert.Equal(t, "missing-id", autoComp.ID)

	// POST /api/competitions (no ID, name yields empty slug)
	body, _ = json.Marshal(state.Competition{Name: "!!!"})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// GET /api/competitions (list error) - removing failing chmod test

	// DELETE /api/competitions/:id (idempotent — non-existent ID returns 204)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/competitions/not-exists", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// POST /api/competitions/:id/start
	comp = state.Competition{ID: "c1", Status: "setup", Courts: []string{"A"}}
	store.SaveCompetition(&comp)
	store.SaveParticipants("c1", []helper.Player{{Name: "P1"}, {Name: "P2"}})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/c1/start", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// DELETE /api/competitions/:id (already started — rejected; must invalidate first)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/competitions/c1", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	// Invalidate, then DELETE succeeds.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/c1/invalidate", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/competitions/c1", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// POST /api/competitions/:id/start (not found)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/not-exists/start", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// POST /api/competitions (save error)
	os.RemoveAll(filepath.Join(tempDir, "competitions"))
	os.WriteFile(filepath.Join(tempDir, "competitions"), []byte("not a dir"), 0644)
	comp = state.Competition{ID: "fail"}
	body, _ = json.Marshal(comp)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	os.Remove(filepath.Join(tempDir, "competitions"))
	os.Mkdir(filepath.Join(tempDir, "competitions"), 0755)

	// GET /api/competitions (list error)
	os.RemoveAll(filepath.Join(tempDir, "competitions"))
	os.WriteFile(filepath.Join(tempDir, "competitions"), []byte("not a dir"), 0644)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	os.Remove(filepath.Join(tempDir, "competitions"))
	os.Mkdir(filepath.Join(tempDir, "competitions"), 0755)
	// PUT /api/competitions/:id (update existing)
	comp.Name = "Updated c1"
	body, _ = json.Marshal(comp)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	// PUT /api/competitions/:id (invalid JSON)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1", bytes.NewBufferString(`{invalid`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCompetitionsEmptyList(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "[]", strings.TrimSpace(w.Body.String()))
}

func TestSlugifyID(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"normal name", "London Cup 2026", "london-cup-2026"},
		{"extra spaces", "  My  Event ", "my-event"},
		{"special chars", "London Cup (2026)!", "london-cup-2026"},
		{"all special chars", "!!!", ""},
		{"empty string", "", ""},
		{"numeric start", "2026 Cup", "2026-cup"},
		{"unicode letters stripped", "Tōkyō Cup", "t-ky-cup"},
		{"long name truncated", strings.Repeat("a", 70), strings.Repeat("a", 64)},
		{"truncate avoids trailing hyphen", strings.Repeat("a", 63) + "-extra", strings.Repeat("a", 63)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := slugifyID(tt.in)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestParticipantHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Setup competition first
	comp := state.Competition{ID: "c1"}
	store.SaveCompetition(&comp)

	// GET /api/competitions/:id/participants (not found)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/competitions/not-exists/participants", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// GET /api/competitions/:id/participants
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions/c1/participants", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET /api/competitions/:id/participants (load error)
	path := filepath.Join(tempDir, "competitions", "c1", "participants.csv")
	os.Remove(path)
	os.MkdirAll(path, 0755)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions/c1/participants", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	os.RemoveAll(path)

	// POST /api/competitions/:id/participants
	req, _ = http.NewRequest("POST", "/api/competitions/c1/participants", bytes.NewBufferString(`{"players": [{"name": "P1"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET /api/competitions/:id/seeds
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions/c1/seeds", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET /api/competitions/:id/seeds (load error)
	seedsPath := filepath.Join(tempDir, "competitions", "c1", "seeds.csv")
	os.Remove(seedsPath)
	os.MkdirAll(seedsPath, 0755)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions/c1/seeds", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	os.RemoveAll(seedsPath)

	// PUT /api/competitions/:id/seeds
	assignments := []domain.SeedAssignment{
		{Name: "P1", SeedRank: 1},
	}
	body, _ := json.Marshal(assignments)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1/seeds", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// PUT /api/competitions/:id/seeds (save error)
	os.Remove(seedsPath)
	os.MkdirAll(seedsPath, 0755)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1/seeds", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	os.RemoveAll(seedsPath)

	// POST /api/competitions/:id/participants (invalid JSON)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/c1/participants", bytes.NewBufferString(`{invalid`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// POST /api/competitions/:id/participants (not found)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/nonexistent/participants", bytes.NewBufferString(`{"players": []}`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// PUT /api/competitions/:id/seeds (invalid JSON)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1/seeds", bytes.NewBufferString(`{invalid`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMatchHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	var w *httptest.ResponseRecorder
	var req *http.Request

	// Setup competition and matches
	comp := state.Competition{ID: "c1"}
	store.SaveCompetition(&comp)

	poolMatches := []state.MatchResult{
		{ID: "PoolA-1", SideA: "A", SideB: "B"},
	}
	store.SavePoolMatches("c1", poolMatches)

	// PUT /api/competitions/:id/matches/:mid/score
	result := state.MatchResult{
		IpponsA: []string{"M"},
		Winner:  "A",
	}
	body, _ := json.Marshal(result)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// PUT /api/competitions/:id/matches/:mid/score (invalid JSON)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1/matches/PoolA-1/score", bytes.NewBufferString(`{invalid`))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)

	// PUT /api/competitions/:id/matches/:mid/score (not found)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1/matches/not-exists/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Verify update
	updatedMatches, _ := store.LoadPoolMatches("c1")
	assert.Equal(t, "A", updatedMatches[0].Winner)

	// PUT /api/competitions/:id/matches/:mid/score (invalid competition)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/not-exists/matches/PoolA-1/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestMatchHandlers_BracketMatch(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Setup competition and bracket
	comp := state.Competition{ID: "c1", Courts: []string{"A"}}
	store.SaveCompetition(&comp)

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m1", SideA: "Player 1", SideB: "Player 2"},
			},
		},
	}
	store.SaveBracket("c1", bracket)

	// PUT /api/competitions/:id/matches/:mid/score
	result := state.MatchResult{
		Winner: "Player 1",
	}
	body, _ := json.Marshal(result)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/c1/matches/m1/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify update
	updatedBracket, _ := store.LoadBracket("c1")
	assert.Equal(t, "Player 1", updatedBracket.Rounds[0][0].Winner)
	assert.Equal(t, state.MatchStatusCompleted, updatedBracket.Rounds[0][0].Status)
}

func TestViewerHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Setup some data
	store.SaveTournament(&state.Tournament{Name: "Test Tournament"})
	comp := state.Competition{ID: "c1", Name: "Comp 1"}
	store.SaveCompetition(&comp)

	// GET /api/viewer/tournament
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET /api/viewer/competitions
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET /api/viewer/competitions/:id
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/c1", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET /api/viewer/competitions
	// Add another competition to test the loop
	store.SaveCompetition(&state.Competition{ID: "c2"})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET /api/viewer/schedule
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/schedule", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// GET /api/viewer/tournament (not found)
	os.Remove(filepath.Join(tempDir, "tournament.md"))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	// Restore
	store.SaveTournament(&state.Tournament{Name: "Test"})

	// GET /api/viewer/competitions/:id (not found)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/not-exists", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// GET /api/viewer/competitions (load error)
	// We already have c1 and c2. Let's make c1/config.md unreadable.
	path := filepath.Join(tempDir, "competitions", "c1", "config.md")
	os.Remove(path)
	os.MkdirAll(path, 0755)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/competitions/c1", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
	os.RemoveAll(path)
}

// TestStartCompetition_BroadcastContract verifies the exact events emitted by
// POST /competitions/:id/start in the common case (playoffs format, or pools
// with at least one un-completed match): only EventCompetitionStarted is sent.
// The competition_started handler in app.js already calls load() so a separate
// EventTournamentUpdated would cause a redundant second reload per viewer.
//
// The start handler ALSO invokes MaybeAutoCompletePools, which for a pools
// competition that finishes generation with every match already completed (a
// theoretical zero-match edge case) would additionally emit
// EventCompetitionCompleted. That branch is covered at the engine layer by
// TestMaybeAutoCompletePools/transitions_when_there_are_zero_pool_matches.
func TestStartCompetition_BroadcastContract(t *testing.T) {
	r, store, _, hub, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Format omitted → playoffs path; MaybeAutoCompletePools is a no-op.
	comp := state.Competition{ID: "c1", Status: "setup", Courts: []string{"A"}}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("c1", []helper.Player{{Name: "P1"}, {Name: "P2"}}))

	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/c1/start", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Broadcast is synchronous, so it is already in the buffered channel.
	receiveEvent := func(d time.Duration) (SSEEvent, bool) {
		select {
		case msg := <-ch:
			var e SSEEvent
			require.NoError(t, json.Unmarshal([]byte(msg), &e))
			return e, true
		case <-time.After(d):
			return SSEEvent{}, false
		}
	}

	event, got := receiveEvent(100 * time.Millisecond)
	require.True(t, got, "expected EventCompetitionStarted broadcast")
	assert.Equal(t, EventCompetitionStarted, event.Type)
	compData, isMap := event.Data.(map[string]any)
	require.True(t, isMap, "EventCompetitionStarted data must be a map")
	assert.Equal(t, "c1", compData["competitionId"])

	_, extra := receiveEvent(10 * time.Millisecond)
	assert.False(t, extra, "start must emit exactly 1 broadcast for the common case")
}
