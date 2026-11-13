package mobileapp

import (
	"bytes"
	"encoding/json"
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

// setupEligibilityTestRouter builds a router that mirrors the production
// server.go layout: GET is on the public api group (no auth), POST is on
// the admin group (AuthMiddleware). Used to verify the auth split is
// correct, i.e. that the public GET doesn't accidentally sit behind auth.
func setupEligibilityTestRouter(t *testing.T) (*gin.Engine, *state.Store, string) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir, err := os.MkdirTemp("", "eligibility-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	r := gin.New()
	hub := NewHub()

	// Public group, same as production server.go
	api := r.Group("/api")
	RegisterPublicEligibilityHandlers(api, store)

	// Admin group, AuthMiddleware gates all writes
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterEligibilityHandlers(admin, store, &stubEligibilityEngine{}, hub)

	return r, store, dir
}

// TestPublicEligibilityGET_NoAuthRequired is the primary regression test for
// the bug where GET /competitor-status was behind AuthMiddleware. The viewer
// and display surfaces call this endpoint without a password; a password-
// protected tournament must not return 401/403 for the GET.
func TestPublicEligibilityGET_NoAuthRequired(t *testing.T) {
	r, store, _ := setupEligibilityTestRouter(t)

	// Create competition and a password-protected tournament.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

	t.Run("no statuses returns empty slice, no auth needed", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/competitions/c1/competitor-status", nil)
		// Deliberately no X-Tournament-Password header
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Statuses []domain.CompetitorStatus `json:"statuses"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.Statuses)
	})

	t.Run("persisted status is visible without auth", func(t *testing.T) {
		require.NoError(t, store.SetCompetitorStatus("c1", domain.CompetitorStatus{
			PlayerID: "p1",
			Eligible: false,
			Reason:   "kiken",
		}))

		req := httptest.NewRequest(http.MethodGet, "/api/competitions/c1/competitor-status", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Statuses []domain.CompetitorStatus `json:"statuses"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Len(t, resp.Statuses, 1)
		assert.Equal(t, "p1", resp.Statuses[0].PlayerID)
	})
}

// TestEligibilityPOST_RequiresAuth confirms that POST /competitor-status
// remains on the admin group and is rejected without the password header.
func TestEligibilityPOST_RequiresAuth(t *testing.T) {
	r, store, _ := setupEligibilityTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test",
		Password: "secret",
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

	body, _ := json.Marshal(map[string]any{
		"playerId": "p1",
		"eligible": false,
		"reason":   "kiken",
	})

	t.Run("no password header returns 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/competitions/c1/competitor-status",
			bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("correct password header succeeds", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/competitions/c1/competitor-status",
			bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestEligibilityPOST_MissingPlayerID verifies that a POST with an empty
// playerId returns 400 (domain.ErrCompetitorStatusMissingPlayerID).
func TestEligibilityPOST_MissingPlayerID(t *testing.T) {
	r, store, _ := setupEligibilityTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "pass"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

	body, _ := json.Marshal(map[string]any{
		"playerId": "",
		"eligible": false,
		"reason":   "kiken",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/competitions/c1/competitor-status",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "pass")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestEligibilityPOST_MissingReason verifies that marking a player ineligible
// without a reason returns 400.
func TestEligibilityPOST_MissingReason(t *testing.T) {
	r, store, _ := setupEligibilityTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Test", Password: "pass"}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

	body, _ := json.Marshal(map[string]any{
		"playerId": "p1",
		"eligible": false,
		"reason":   "",
	})
	req := httptest.NewRequest(http.MethodPost, "/api/competitions/c1/competitor-status",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "pass")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// setupReinstateTestRouter builds a minimal auth-protected router for
// RegisterReinstateHandler tests. The EligibilityEngine is a stub so
// callers control what ReinstateCompetitor returns.
func setupReinstateTestRouter(t *testing.T, eng EligibilityEngine) (*gin.Engine, *state.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	dir, err := os.MkdirTemp("", "reinstate-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	r := gin.New()
	hub := NewHub()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(store), store))
	RegisterReinstateHandler(admin, eng, hub)
	return r, store
}

// TestReinstateHandler covers the HTTP layer of RegisterReinstateHandler:
// auth gating, happy path 200, 409 for not-ineligible / not-reinstateable.
func TestReinstateHandler(t *testing.T) {
	const pw = "pass"

	t.Run("no password returns 401", func(t *testing.T) {
		eng := &stubEligibilityEngine{Status: &domain.CompetitorStatus{PlayerID: "p1", Eligible: true}}
		r, store := setupReinstateTestRouter(t, eng)
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: pw}))
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

		req := httptest.NewRequest(http.MethodPost, "/api/competitions/c1/competitors/p1/reinstate", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("happy path returns 200 with eligible status", func(t *testing.T) {
		reinstated := &domain.CompetitorStatus{PlayerID: "p1", Eligible: true}
		eng := &stubEligibilityEngine{Status: reinstated}
		r, store := setupReinstateTestRouter(t, eng)
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: pw}))
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

		req := httptest.NewRequest(http.MethodPost, "/api/competitions/c1/competitors/p1/reinstate", nil)
		req.Header.Set("X-Tournament-Password", pw)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp domain.CompetitorStatus
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.True(t, resp.Eligible)
		assert.Equal(t, "p1", resp.PlayerID)
	})

	t.Run("engine validation error returns 409", func(t *testing.T) {
		eng := &stubEligibilityEngine{Err: &engine.ValidationError{Msg: "not reinstateable"}}
		r, store := setupReinstateTestRouter(t, eng)
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: pw}))
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

		req := httptest.NewRequest(http.MethodPost, "/api/competitions/c1/competitors/p1/reinstate", nil)
		req.Header.Set("X-Tournament-Password", pw)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code)
	})
}

// A lifted bar owes a finished league the auto-complete pass (a drawn
// ippon-shobu bout settled on both sides barred opens again), and a league the
// pass puts back into match play is announced as started. A bar recorded does
// not owe it: it can only settle a bout, never open one.
func TestLiftedBar_RunsTheAutoCompletePassAndAnnouncesAReopenedLeague(t *testing.T) {
	post := func(t *testing.T, path string, body any, eng *stubEligibilityEngine) []EventType {
		t.Helper()
		gin.SetMode(gin.TestMode)
		store, err := state.NewStore(t.TempDir())
		require.NoError(t, err)
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))
		hub := &recordingBroadcaster{}
		r := gin.New()
		api := r.Group("/api")
		RegisterEligibilityHandlers(api, store, eng, hub)
		RegisterReinstateHandler(api, eng, hub)
		raw, err := json.Marshal(body)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(raw))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		return hub.events
	}

	t.Run("a reinstatement", func(t *testing.T) {
		eng := &stubEligibilityEngine{Status: &domain.CompetitorStatus{PlayerID: "p1", Eligible: true}, Outcome: engine.AutoCompleteStarted}
		events := post(t, "/api/competitions/c1/competitors/p1/reinstate", gin.H{}, eng)
		assert.Equal(t, 1, eng.Passes)
		assert.Contains(t, events, EventCompetitionStarted)
	})
	t.Run("a status written eligible", func(t *testing.T) {
		eng := &stubEligibilityEngine{Outcome: engine.AutoCompleteStarted}
		events := post(t, "/api/competitions/c1/competitor-status", gin.H{"playerId": "p1", "eligible": true}, eng)
		assert.Equal(t, 1, eng.Passes)
		assert.Contains(t, events, EventCompetitionStarted)
	})
	t.Run("a bar recorded", func(t *testing.T) {
		eng := &stubEligibilityEngine{Outcome: engine.AutoCompleteStarted}
		events := post(t, "/api/competitions/c1/competitor-status", gin.H{"playerId": "p1", "eligible": false, "reason": "kiken-voluntary: withdrew"}, eng)
		assert.Zero(t, eng.Passes)
		assert.NotContains(t, events, EventCompetitionStarted)
	})
}
