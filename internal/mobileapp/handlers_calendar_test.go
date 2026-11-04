package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupCalendarTestEnv(t *testing.T) (*state.Store, http.Handler) {
	t.Helper()
	store := newTempStore(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Calendar Test Tournament",
		Date:         "01-06-2026",
		DurationDays: 2,
		Venue:        "Dojo",
		Courts:       []string{"A", "B"},
		Password:     "main-pw",
		Mode:         state.TournamentModeOfficiated,
	}))
	return store, setupSelfRunRouter(t, store, NewFileVerifier(store))
}

func getCalendar(t *testing.T, r http.Handler, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/schedule/calendar", nil)
	if password != "" {
		req.Header.Set("X-Tournament-Password", password)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func getCalendarPreview(t *testing.T, r http.Handler, query string, password string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/schedule/calendar/preview?"+query, nil)
	if password != "" {
		req.Header.Set("X-Tournament-Password", password)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestCalendar_RequiresPassword(t *testing.T) {
	_, r := setupCalendarTestEnv(t)

	t.Run("no password on calendar returns 401", func(t *testing.T) {
		w := getCalendar(t, r, "")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("wrong password on calendar returns 401", func(t *testing.T) {
		w := getCalendar(t, r, "wrong-pw")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("no password on preview returns 401", func(t *testing.T) {
		w := getCalendarPreview(t, r, "competitionId=c1&date=01-06-2026&startTime=09:00&courts=A", "")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("wrong password on preview returns 401", func(t *testing.T) {
		w := getCalendarPreview(t, r, "competitionId=c1&date=01-06-2026&startTime=09:00&courts=A", "wrong-pw")
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestCalendar_SelfRun_AnonymousGets401(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))

	wCal := getCalendar(t, r, "")
	assert.Equal(t, http.StatusUnauthorized, wCal.Code, "calendar must be main-gated in self-run mode")

	wPrev := getCalendarPreview(t, r, "competitionId=c1&date=01-06-2026&startTime=09:00&courts=A", "")
	assert.Equal(t, http.StatusUnauthorized, wPrev.Code, "calendar preview must be main-gated in self-run mode")
}

func TestCalendar_EmptyTournament_ReturnsEmptyArrays(t *testing.T) {
	_, r := setupCalendarTestEnv(t)

	w := getCalendar(t, r, "main-pw")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var raw map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))

	assert.JSONEq(t, `[]`, string(raw["unusedPauses"]))
	assert.JSONEq(t, `[]`, string(raw["competitions"]))

	var result engine.TournamentCalendarResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))

	require.Len(t, result.Days, 2)
	assert.Equal(t, []string{"A", "B"}, result.Courts)
	assert.Empty(t, result.UnusedPauses)
	assert.Empty(t, result.Competitions)

	for _, d := range result.Days {
		assert.NotNil(t, d.Breaks)
		assert.Empty(t, d.Breaks)
	}
}

func TestCalendar_TwoCompetitions(t *testing.T) {
	store, r := setupCalendarTestEnv(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   "c1",
		Name:                 "Open Individual",
		Date:                 "01-06-2026",
		StartTime:            "09:00",
		Courts:               []string{"A"},
		Format:               state.CompFormatKnockout,
		ExpectedParticipants: 8,
		Status:               state.CompStatusSetup,
	}))

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   "c2",
		Name:                 "Women Individual",
		Date:                 "01-06-2026",
		StartTime:            "10:00",
		Courts:               []string{"B"},
		Format:               state.CompFormatKnockout,
		ExpectedParticipants: 8,
		Status:               state.CompStatusSetup,
	}))

	w := getCalendar(t, r, "main-pw")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var result engine.TournamentCalendarResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &result))
	require.Len(t, result.Competitions, 2)

	for _, comp := range result.Competitions {
		assert.NotEmpty(t, comp.ConfigVersion)
		assert.True(t, comp.Placement.Placed)
		assert.NotEmpty(t, comp.Placement.Courts)
	}
}

func TestCalendarPreview_Refusals(t *testing.T) {
	store, r := setupCalendarTestEnv(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   "c1",
		Name:                 "Preview Comp",
		Date:                 "01-06-2026",
		StartTime:            "09:00",
		Courts:               []string{"A"},
		Format:               state.CompFormatKnockout,
		ExpectedParticipants: 8,
		Status:               state.CompStatusSetup,
	}))

	cases := []struct {
		name        string
		query       string
		wantSnippet string
	}{
		{"missing competitionId", "date=01-06-2026&startTime=09:00&courts=A", "competitionId is required"},
		{"empty competitionId", "competitionId=&date=01-06-2026&startTime=09:00&courts=A", "competitionId is required"},
		{"date in YYYY-MM-DD spelling", "competitionId=c1&date=2026-06-01&startTime=09:00&courts=A", "date must be one of the tournament days"},
		{"date with trailing text", "competitionId=c1&date=01-06-2026extra&startTime=09:00&courts=A", "date must be one of the tournament days"},
		{"missing date", "competitionId=c1&startTime=09:00&courts=A", "date is required"},
		{"date not a tournament day", "competitionId=c1&date=03-06-2026&startTime=09:00&courts=A", "date must be one of the tournament days"},
		{"bad startTime format", "competitionId=c1&date=01-06-2026&startTime=25:00&courts=A", "startTime must be HH:MM"},
		{"bad startTime length", "competitionId=c1&date=01-06-2026&startTime=9:00&courts=A", "startTime must be HH:MM"},
		{"empty startTime", "competitionId=c1&date=01-06-2026&startTime=&courts=A", "startTime is required"},
		{"missing courts", "competitionId=c1&date=01-06-2026&startTime=09:00", "courts is required"},
		{"empty courts", "competitionId=c1&date=01-06-2026&startTime=09:00&courts=", "courts is required"},
		{"court label empty entry", "competitionId=c1&date=01-06-2026&startTime=09:00&courts=A,,B", "court label cannot be empty"},
		{"duplicate court label", "competitionId=c1&date=01-06-2026&startTime=09:00&courts=A,A", "duplicate court label"},
		{"court label not held by tournament", "competitionId=c1&date=01-06-2026&startTime=09:00&courts=Z", "court label \"Z\" is not held by the tournament"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := getCalendarPreview(t, r, tc.query, "main-pw")
			assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Contains(t, body["error"], tc.wantSnippet)
		})
	}
}

func TestCalendarPreview_NotFound(t *testing.T) {
	_, r := setupCalendarTestEnv(t)
	w := getCalendarPreview(t, r, "competitionId=nonexistent&date=01-06-2026&startTime=09:00&courts=A", "main-pw")
	assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	var body map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "Competition not found", body["error"])
}

func TestCalendarPreview_OverlapsAndTouching(t *testing.T) {
	store, r := setupCalendarTestEnv(t)

	// c1 placed on 01-06-2026 (Day 1), court A from 09:00.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   "c1",
		Name:                 "Competition 1",
		Date:                 "01-06-2026",
		StartTime:            "09:00",
		Courts:               []string{"A"},
		Format:               state.CompFormatKnockout,
		ExpectedParticipants: 8,
		Status:               state.CompStatusSetup,
	}))

	// c2 to preview
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   "c2",
		Name:                 "Competition 2",
		Date:                 "01-06-2026",
		StartTime:            "11:00",
		Courts:               []string{"A"},
		Format:               state.CompFormatKnockout,
		ExpectedParticipants: 8,
		Status:               state.CompStatusSetup,
	}))

	// Query calendar to inspect c1's placement window
	wCal := getCalendar(t, r, "main-pw")
	require.Equal(t, http.StatusOK, wCal.Code)
	var calResult engine.TournamentCalendarResult
	require.NoError(t, json.Unmarshal(wCal.Body.Bytes(), &calResult))
	var c1Placement engine.CompetitionPlacement
	for _, c := range calResult.Competitions {
		if c.ID == "c1" {
			c1Placement = c.Placement
			break
		}
	}
	require.True(t, c1Placement.Placed)
	c1Win := c1Placement.Windows["A"]
	require.Positive(t, c1Win.End)

	t.Run("overlapping window returns overlap", func(t *testing.T) {
		// Preview c2 starting at 09:00 on Court A, overlapping with c1
		w := getCalendarPreview(t, r, "competitionId=c2&date=01-06-2026&startTime=09:00&courts=A", "main-pw")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var resp calendarPreviewResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equal(t, "c2", resp.CompetitionID)
		assert.True(t, resp.Placement.Placed)
		require.Len(t, resp.Overlaps, 1)
		assert.Equal(t, "c1", resp.Overlaps[0].CompetitionID)
		assert.Equal(t, "Competition 1", resp.Overlaps[0].Name)
		assert.Equal(t, "A", resp.Overlaps[0].Court)
		assert.Equal(t, c1Win.Start, resp.Overlaps[0].Start)
		assert.Equal(t, c1Win.End, resp.Overlaps[0].End)
	})

	t.Run("touching window returns no overlap", func(t *testing.T) {
		// Touching window: preview Start equals c1Win.End
		touchingTime := state.FormatClockHHMM(c1Win.End)
		w := getCalendarPreview(t, r, fmt.Sprintf("competitionId=c2&date=01-06-2026&startTime=%s&courts=A", touchingTime), "main-pw")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var resp calendarPreviewResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Empty(t, resp.Overlaps)
	})
}

func TestCalendarPreview_PreviewedStatus(t *testing.T) {
	store, r := setupCalendarTestEnv(t)

	// Setup competition: preview computes placement from requested parameters.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                   "c_setup",
		Name:                 "Setup Comp",
		Date:                 "01-06-2026",
		StartTime:            "09:00",
		Courts:               []string{"A"},
		Format:               state.CompFormatKnockout,
		ExpectedParticipants: 8,
		Status:               state.CompStatusSetup,
	}))

	// Drawn competition: already has stored draw windows.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                       "c_drawn",
		Name:                     "Drawn Comp",
		Date:                     "01-06-2026",
		StartTime:                "09:00",
		Courts:                   []string{"A"},
		Format:                   state.CompFormatKnockout,
		PoolMatchDurationSeconds: 180,
		Status:                   state.CompStatusDrawReady,
	}))
	require.NoError(t, store.SavePoolMatches("c_drawn", []state.MatchResult{
		{ID: "m1", Court: "A", ScheduledAt: "09:00", Status: state.MatchStatusScheduled},
	}))

	t.Run("setup competition reports previewed true and reflects requested startTime", func(t *testing.T) {
		w := getCalendarPreview(t, r, "competitionId=c_setup&date=01-06-2026&startTime=14:00&courts=A", "main-pw")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var resp calendarPreviewResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.True(t, resp.Previewed, "setup competition must have previewed=true")
		assert.True(t, resp.Placement.Placed)
		assert.Equal(t, 14*60, resp.Placement.Windows["A"].Start, "requested 14:00 start must be used")
	})

	t.Run("drawn competition reports previewed false and returns stored draw windows", func(t *testing.T) {
		w := getCalendarPreview(t, r, "competitionId=c_drawn&date=01-06-2026&startTime=14:00&courts=A", "main-pw")
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())

		var resp calendarPreviewResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.False(t, resp.Previewed, "drawn competition must have previewed=false")
		assert.True(t, resp.Placement.Placed)
		assert.Equal(t, 9*60, resp.Placement.Windows["A"].Start, "stored draw start (09:00) must be returned, not requested 14:00")
	})
}
