package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGETCompetitionScheduleEstimate covers the per-competition schedule
// estimator endpoint registered in RegisterCompetitionHandlers.
// Route: GET /api/competitions/:id/schedule/estimate
// mp-zoh Phase 3.
func TestGETCompetitionScheduleEstimate(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	// Seed a tournament so EstimateForCounts can read defaults.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test Tournament",
		Password: "secret",
		Courts:   []string{"A"},
	}))

	t.Run("404 for unknown competition id", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/nonexistent/schedule/estimate", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
		assert.Contains(t, w.Body.String(), "nonexistent")
	})

	t.Run("200 with valid ScheduleEstimate JSON shape for existing competition", func(t *testing.T) {
		comp := state.Competition{
			ID:     "estimate-comp",
			Name:   "Estimate Competition",
			Format: state.CompFormatPlayoffs,
			Status: state.CompStatusSetup,
		}
		require.NoError(t, store.SaveCompetition(&comp))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/estimate-comp/schedule/estimate", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		var resp engine.ScheduleEstimate
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		// JSON shape must have the three canonical fields.
		// No participants yet → zero total is valid.
		assert.GreaterOrEqual(t, resp.TotalDurationMinutes, 0)
		assert.GreaterOrEqual(t, resp.CeremonyMinutes, 0)
	})

	t.Run("200 with non-zero totalDurationMinutes when competition has participants", func(t *testing.T) {
		// Use a mixed format so both pool and playoff matches are estimated.
		comp := state.Competition{
			ID:       "estimate-with-players",
			Name:     "With Players",
			Format:   state.CompFormatMixed,
			Status:   state.CompStatusSetup,
			PoolSize: 4,
			Courts:   []string{"A"},
		}
		require.NoError(t, store.SaveCompetition(&comp))

		// Save 8 participants so the estimator has real data.
		players := make([]domain.Player, 8)
		for i := range players {
			players[i] = domain.Player{Name: "Player", Dojo: "Dojo"}
		}
		players[0] = domain.Player{Name: "Alpha", Dojo: "Dojo"}
		players[1] = domain.Player{Name: "Beta", Dojo: "Dojo"}
		players[2] = domain.Player{Name: "Gamma", Dojo: "Dojo"}
		players[3] = domain.Player{Name: "Delta", Dojo: "Dojo"}
		players[4] = domain.Player{Name: "Epsilon", Dojo: "Dojo"}
		players[5] = domain.Player{Name: "Zeta", Dojo: "Dojo"}
		players[6] = domain.Player{Name: "Eta", Dojo: "Dojo"}
		players[7] = domain.Player{Name: "Theta", Dojo: "Dojo"}
		require.NoError(t, store.SaveParticipants("estimate-with-players", players))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/estimate-with-players/schedule/estimate", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body: %s", w.Body.String())

		var resp engine.ScheduleEstimate
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Greater(t, resp.TotalDurationMinutes, 0,
			"expected non-zero total duration for competition with 8 participants")
	})

	t.Run("no elevated auth required, main password sufficient", func(t *testing.T) {
		// The endpoint is registered under adminGroup (main-password gated)
		// but does NOT require elevated (admin) auth. This test confirms
		// the handler itself does not check for elevated credentials;
		// AuthMiddleware enforcement is tested via the real server wiring,
		// not this unit-level router (setupTestRouter omits AuthMiddleware).
		comp := state.Competition{
			ID:     "main-auth-estimate",
			Name:   "Main Auth Estimate",
			Format: state.CompFormatPlayoffs,
			Status: state.CompStatusSetup,
		}
		require.NoError(t, store.SaveCompetition(&comp))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/main-auth-estimate/schedule/estimate", nil)
		// No X-Admin-Password, only main-password auth is needed (enforced
		// by AuthMiddleware in production, not by the handler itself).
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code,
			"estimate endpoint must not require elevated auth")
	})
}
