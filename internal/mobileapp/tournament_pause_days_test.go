package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func pauseTestRouter(t *testing.T, dir string) *gin.Engine {
	t.Helper()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	hub := NewHub()
	t.Cleanup(hub.Close)
	RegisterTournamentHandlers(r.Group("/api"), store, hub, NewFileVerifier(store))
	return r
}

func getTournamentJSON(t *testing.T, r *gin.Engine) state.Tournament {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/tournament", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var tour state.Tournament
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &tour))
	return tour
}

// putTournamentJSON saves tour as a save made from the settings stored now: it
// carries the tournamentVersion the admin GET answers, so a pause refusal is
// judged rather than the save being refused as made from an older copy.
func putTournamentJSON(t *testing.T, r *gin.Engine, tour state.Tournament) *httptest.ResponseRecorder {
	t.Helper()
	g := httptest.NewRecorder()
	greq, _ := http.NewRequest("GET", "/api/tournament", nil)
	r.ServeHTTP(g, greq)
	require.Equal(t, http.StatusOK, g.Code)
	var current struct {
		TournamentVersion string `json:"tournamentVersion"`
	}
	require.NoError(t, json.Unmarshal(g.Body.Bytes(), &current))
	require.NotEmpty(t, current.TournamentVersion)
	raw, err := json.Marshal(tour)
	require.NoError(t, err)
	var m map[string]any
	require.NoError(t, json.Unmarshal(raw, &m))
	m["tournamentVersion"] = current.TournamentVersion
	body, err := json.Marshal(m)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	return w
}

// A legacy length v2.3 stored that cannot be read shows as a pause with no
// length; a save that leaves it as it is goes through, the schedule skips it,
// and once the organiser gives it a length it is an ordinary pause, made once.
func TestTournamentPUT_UnreadableLegacyLengthIsAPauseToFix(t *testing.T) {
	dir := t.TempDir()
	tournPath := filepath.Join(dir, "tournament.md")
	require.NoError(t, os.WriteFile(tournPath, []byte(`---
name: Legacy Tourn
venue: Hall
date: 01-01-2026
password: secret
courts:
  - A
opening_block: 45 min
lunch_block: 1h 30m
---
`), 0o600))

	r := pauseTestRouter(t, dir)
	tour := getTournamentJSON(t, r)
	require.Equal(t, []state.Pause{
		{Name: "Opening ceremony (45 min)"},
		{Name: "Lunch (1h 30m)", Start: "12:00"},
	}, tour.Pauses)

	// An unrelated save is accepted and keeps both pauses as they were.
	tour.Venue = "Other hall"
	w := putTournamentJSON(t, r, tour)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, tour.Pauses, getTournamentJSON(t, r).Pauses)

	// The organiser gives the lunch its length.
	tour.Pauses[1].Name = "Lunch"
	tour.Pauses[1].Length = "1h30m"
	w = putTournamentJSON(t, r, tour)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	raw, err := os.ReadFile(tournPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "lunch_block")
	assert.NotContains(t, string(raw), "opening_block")

	// A later start makes no pause again.
	again := getTournamentJSON(t, pauseTestRouter(t, dir))
	assert.Equal(t, []state.Pause{
		{Name: "Opening ceremony (45 min)"},
		{Name: "Lunch", Start: "12:00", Length: "1h30m"},
	}, again.Pauses)
}

// A new or changed pause on a day the tournament does not have is refused; a
// stored one left past the end by shortening the tournament is not.
func TestTournamentPUT_PauseDayPastTheEnd(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tournament.md"), []byte(`---
name: Three Days
venue: Hall
date: 01-01-2026
password: secret
duration_days: 3
courts:
  - A
pauses:
  - name: Grading
    start: "14:00"
    length: 1h
    day: 3
---
`), 0o600))
	r := pauseTestRouter(t, dir)

	tour := getTournamentJSON(t, r)
	tour.DurationDays = 2
	w := putTournamentJSON(t, r, tour)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, 3, getTournamentJSON(t, r).Pauses[0].Day)

	changed := tour
	changed.Pauses = []state.Pause{{Name: "Grading", Start: "15:00", Length: "1h", Day: 3}}
	w = putTournamentJSON(t, r, changed)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.JSONEq(t, `{"error":"Pause \"Grading\": the tournament has 2 days, so it has no Day 3. Choose a day it has, or Every day."}`, w.Body.String())

	added := tour
	added.Pauses = append(append([]state.Pause{}, tour.Pauses...), state.Pause{Name: "Lunch", Start: "12:00", Length: "1h", Day: 4})
	w = putTournamentJSON(t, r, added)
	require.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), `Pause \"Lunch\": the tournament has 2 days, so it has no Day 4.`)
}

// A save made from an older copy is told the settings changed before its
// pauses are judged: the stale form gets the current copy, not a refusal about
// a day it was never going to write over the newer settings.
func TestTournamentPUT_StaleSaveWithAPausePastTheEndIsTournamentChanged(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "tournament.md"), []byte(`---
name: Two Days
venue: Hall
date: 01-01-2026
password: secret
duration_days: 2
courts:
  - A
---
`), 0o600))
	r := pauseTestRouter(t, dir)

	g := httptest.NewRecorder()
	greq, _ := http.NewRequest("GET", "/api/tournament", nil)
	r.ServeHTTP(g, greq)
	require.Equal(t, http.StatusOK, g.Code)
	var old map[string]any
	require.NoError(t, json.Unmarshal(g.Body.Bytes(), &old))

	// Another device renames the venue.
	tour := getTournamentJSON(t, r)
	tour.Venue = "Other hall"
	require.Equal(t, http.StatusOK, putTournamentJSON(t, r, tour).Code)

	// The older copy adds a pause on a day the tournament does not have.
	old["pauses"] = []map[string]any{{"name": "Lunch", "start": "12:00", "length": "1h", "day": 5}}
	body, err := json.Marshal(old)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), `"code":"tournament_changed"`)
}

func TestValidatePauseDays(t *testing.T) {
	stored := []state.Pause{{Name: "Grading", Day: 3}}
	assert.Nil(t, validatePauseDays([]state.Pause{{Name: "Grading", Day: 3}}, stored, 1))
	assert.Nil(t, validatePauseDays([]state.Pause{{Name: "Lunch", Day: 0}, {Name: "Lunch", Day: 2}}, nil, 2))
	err := validatePauseDays([]state.Pause{{Day: 2}}, nil, 0)
	require.NotNil(t, err)
	assert.Equal(t, "Pause 1: the tournament has 1 day, so it has no Day 2. Choose a day it has, or Every day.", err.Error())
}
