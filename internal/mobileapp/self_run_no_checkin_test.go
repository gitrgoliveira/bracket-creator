package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Operator ruling 2026-10-13: "Self run mode has no check-in." The three
// check-in writes are refused 409 for every caller in a self-run tournament,
// the organiser included, and work as before in an officiated one.

const noCheckInComp = "no-checkin"

type checkInCall struct {
	name, method, path string
	body               any
}

func checkInCalls(pid string) []checkInCall {
	base := "/api/competitions/" + noCheckInComp + "/participants"
	return []checkInCall{
		{"PUT checkin", http.MethodPut, base + "/" + pid + "/checkin", nil},
		{"POST checkin-bulk", http.MethodPost, base + "/checkin-bulk", map[string]any{"participantIds": []string{pid}}},
		{"DELETE checkin", http.MethodDelete, base + "/" + pid + "/checkin", nil},
	}
}

func seedCheckInRoster(t *testing.T, store *state.Store) string {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: noCheckInComp, Name: "No Check-in", Status: state.CompStatusSetup, CheckInEnabled: true,
	}))
	require.NoError(t, store.SaveParticipants(noCheckInComp, []domain.Player{{Name: "Alice", Dojo: "Suigetsu"}}))
	players, err := store.LoadParticipants(noCheckInComp, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	return players[0].ID
}

func serve(r http.Handler, c checkInCall, password *string) *httptest.ResponseRecorder {
	req := jsonReq(c.method, c.path, c.body)
	if password != nil {
		req.Header.Set("X-Tournament-Password", *password)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestSelfRun_CheckInWrites_RefusedForTheOrganiser(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	pid := seedCheckInRoster(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	organiser := "main-pw"

	for _, c := range checkInCalls(pid) {
		t.Run(c.name, func(t *testing.T) {
			w := serve(r, c, &organiser)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, "check_in_not_in_self_run", body["error"])
			assert.Equal(t, "Self-run mode has no check-in.", body["message"])
		})
	}

	players, err := store.LoadParticipants(noCheckInComp, false)
	require.NoError(t, err)
	assert.False(t, players[0].CheckedIn, "a refused check-in writes nothing")
}

func TestSelfRun_CheckInWrites_RefusedForAnAnonymousCaller(t *testing.T) {
	store := newTempStore(t)
	seedSelfRunTournament(t, store)
	pid := seedCheckInRoster(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	anonymous := ""

	for _, c := range checkInCalls(pid)[:2] { // forward check-in: ungated, so the 409 answers
		t.Run(c.name, func(t *testing.T) {
			w := serve(r, c, &anonymous)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), "check_in_not_in_self_run")
		})
	}

	// The reversal stays on the organiser-only list: the gate answers first.
	w := serve(r, checkInCalls(pid)[2], &anonymous)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestOfficiated_CheckInWrites_StillWork(t *testing.T) {
	store := newTempStore(t)
	seedOfficiatedTournament(t, store)
	pid := seedCheckInRoster(t, store)
	r := setupSelfRunRouter(t, store, NewFileVerifier(store))
	organiser := "main-pw"

	calls := checkInCalls(pid)
	for _, c := range []checkInCall{calls[0], calls[2], calls[1]} {
		w := serve(r, c, &organiser)
		require.Equal(t, http.StatusOK, w.Code, "%s: %s", c.name, w.Body.String())
	}
}
