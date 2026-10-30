package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A shared shiaijo another competition of the day holds until after midnight
// leaves no "HH:MM" slot for the next one (bc-seqc): generate-draw answers a
// 409 that says what to do, never a 500 (which the app would retry) and never
// a draw on a wrapped or clamped time. Nothing is written.
func TestGenerateDraw_CourtPlannedPastMidnightIs409(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "late", Name: "Late Open", Format: state.CompFormatLeague,
		Date: "2026-11-01", StartTime: "21:00", Courts: []string{"A"},
		PoolMatchDurationSeconds: 180, Status: state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SavePoolMatches("late", []state.MatchResult{
		{ID: "Pool A-0", Court: "A", ScheduledAt: "23:58", Status: state.MatchStatusScheduled},
	}))

	players := make([]domain.Player, 6)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%02d", i+1), Dojo: fmt.Sprintf("Dojo %02d", i+1)}
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "next", Name: "Next", Format: state.CompFormatMixed, PoolSize: 3, PoolSizeMode: "min", PoolWinners: 2,
		Date: "2026-11-01", StartTime: "22:00", Courts: []string{"A"},
		PoolMatchDurationSeconds: 180, Status: state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants("next", players))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/next/generate-draw", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "court_past_midnight", body["code"])
	assert.Equal(t, "A", body["court"])
	assert.Equal(t, "late", body["competitionId"])
	assert.Contains(t, body["error"], "Shiaijo A is planned until after midnight by Late Open")

	pm, err := store.LoadPoolMatches("next")
	require.NoError(t, err)
	assert.Empty(t, pm, "a refused draw writes no matches")
	comp, err := store.LoadCompetition("next")
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusSetup, comp.Status)
}
