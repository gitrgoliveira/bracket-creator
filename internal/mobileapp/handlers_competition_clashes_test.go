package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGETCompetitionScheduleClashes covers the court-clash endpoint
// GET /api/competitions/:id/schedule/clashes (mp-4a52).
func TestGETCompetitionScheduleClashes(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer func() { require.NoError(t, os.RemoveAll(tempDir)) }()

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Test Tournament", Password: "secret", Courts: []string{"A", "B"},
	}))

	save := func(id, name, date, start string, courts []string) {
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: id, Name: name, Format: state.CompFormatPlayoffs, Kind: "individual",
			Date: date, StartTime: start, Courts: courts, Status: state.CompStatusSetup,
		}))
	}

	t.Run("404 for unknown competition", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/nope/schedule/clashes", nil)
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("empty array when no other competitions", func(t *testing.T) {
		// Different day from the clash subtest below so it can't interfere
		// (subtests share one store).
		save("solo", "Solo", "05-07-2026", "09:00", []string{"A"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/solo/schedule/clashes", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		var clashes []engine.ClashWarning
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &clashes))
		assert.Empty(t, clashes)
	})

	t.Run("reports a clash on a shared court at overlapping times", func(t *testing.T) {
		save("alpha", "Alpha", "01-07-2026", "09:00", []string{"A"})
		save("bravo", "Bravo", "01-07-2026", "09:15", []string{"A"})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/alpha/schedule/clashes", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "body: %s", w.Body.String())
		var clashes []engine.ClashWarning
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &clashes))
		require.Len(t, clashes, 1)
		assert.Equal(t, "bravo", clashes[0].OtherCompID)
		assert.Equal(t, []string{"A"}, clashes[0].SharedCourts)
	})
}
