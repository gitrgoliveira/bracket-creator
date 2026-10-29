package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func TestGETCompetitionScheduleEstimate_CourtEnds(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer func() {
		require.NoError(t, os.RemoveAll(tempDir))
	}()

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test Tournament",
		Date:     "2026-11-01",
		Password: "secret",
		Courts:   []string{"A"},
	}))

	t.Run("undrawn competition omits courtEnds key", func(t *testing.T) {
		comp := state.Competition{
			ID:        "comp-undrawn",
			Name:      "Undrawn Competition",
			Date:      "2026-11-01",
			Format:    state.CompFormatKnockout,
			Status:    state.CompStatusSetup,
			Courts:    []string{"A"},
			StartTime: "09:00",
		}
		require.NoError(t, store.SaveCompetition(&comp))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/comp-undrawn/schedule/estimate", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
		_, exists := raw["courtEnds"]
		assert.False(t, exists, "undrawn competition must omit courtEnds key from JSON response")
	})

	t.Run("drawn competition carries courtEnds in response", func(t *testing.T) {
		comp := state.Competition{
			ID:                       "comp-drawn",
			Name:                     "Drawn Competition",
			Date:                     "2026-11-01",
			Format:                   state.CompFormatMixed,
			Status:                   state.CompStatusDrawReady,
			Courts:                   []string{"A"},
			StartTime:                "09:00",
			PoolMatchDurationSeconds: 180,
		}
		require.NoError(t, store.SaveCompetition(&comp))

		// Matches on Court A: 09:00 + 5m = 09:05
		matches := []state.MatchResult{
			{
				ID:          "p1",
				Court:       "A",
				ScheduledAt: "09:00",
				Status:      state.MatchStatusScheduled,
			},
		}
		require.NoError(t, store.SavePoolMatches("comp-drawn", matches))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/comp-drawn/schedule/estimate", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
		endsRaw, exists := raw["courtEnds"]
		require.True(t, exists, "drawn competition must include courtEnds key in JSON response")

		var resp engine.ScheduleEstimate
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.NotNil(t, resp.CourtEnds)
		assert.Equal(t, "09:05", resp.CourtEnds["A"])
		_ = endsRaw
	})

	t.Run("drawn competition with corrupt bracket answers 200 without courtEnds", func(t *testing.T) {
		comp := state.Competition{
			ID:                       "comp-drawn-corrupt",
			Name:                     "Corrupt Drawn Competition",
			Date:                     "2026-11-01",
			Format:                   state.CompFormatMixed,
			Status:                   state.CompStatusDrawReady,
			Courts:                   []string{"A"},
			StartTime:                "09:00",
			PoolMatchDurationSeconds: 180,
		}
		require.NoError(t, store.SaveCompetition(&comp))

		matches := []state.MatchResult{
			{
				ID:          "p1",
				Court:       "A",
				ScheduledAt: "09:00",
				Status:      state.MatchStatusScheduled,
			},
		}
		require.NoError(t, store.SavePoolMatches("comp-drawn-corrupt", matches))

		// Overwrite bracket.json with invalid JSON after the draw.
		bracketPath := filepath.Join(tempDir, "competitions", "comp-drawn-corrupt", "bracket.json")
		require.NoError(t, os.WriteFile(bracketPath, []byte("{invalid-json"), 0o600))

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/comp-drawn-corrupt/schedule/estimate", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var raw map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
		_, exists := raw["courtEnds"]
		assert.False(t, exists, "corrupt bracket must omit courtEnds key from JSON response")

		var resp engine.ScheduleEstimate
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Nil(t, resp.CourtEnds)
	})
}
