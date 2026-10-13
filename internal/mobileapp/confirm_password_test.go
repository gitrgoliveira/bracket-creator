package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// Deleting a competition asks for the password again: the sign-in password
// typed in that moment, in X-Confirm-Password, checked by the same
// PasswordVerifier as the sign-in (operator ruling 2026-10-12).

const confirmWrongSentence = "Wrong password. The competition was not deleted."

func confirmDeleteRig(t *testing.T, mode string, locked bool) (*gin.Engine, *state.Store) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store := newTempStore(t)
	var v PasswordVerifier
	if locked {
		hash, err := bcrypt.GenerateFromPassword([]byte("main-pw"), bcrypt.MinCost)
		require.NoError(t, err)
		v, err = NewBcryptVerifier(string(hash))
		require.NoError(t, err)
	}
	tour := &state.Tournament{Name: "T", Date: "01-06-2026", Venue: "Dojo", Courts: []string{"A"}, Mode: mode}
	if !locked {
		tour.Password = "main-pw"
	}
	require.NoError(t, store.SaveTournament(tour))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "doomed", Name: "Doomed"}))
	return setupSelfRunRouter(t, store, v), store
}

func confirmDelete(r *gin.Engine, signIn, confirm string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodDelete, "/api/competitions/doomed", nil)
	if signIn != "" {
		req.Header.Set("X-Tournament-Password", signIn)
	}
	if confirm != "" {
		req.Header.Set("X-Confirm-Password", confirm)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestDeleteCompetition_AsksForThePasswordAgain(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mode   string
		locked bool
	}{
		{"officiated file mode", state.TournamentModeOfficiated, false},
		{"officiated locked mode", state.TournamentModeOfficiated, true},
		{"self-run file mode", state.TournamentModeSelfRun, false},
		{"self-run locked mode", state.TournamentModeSelfRun, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store := confirmDeleteRig(t, tc.mode, tc.locked)
			stillThere := func() {
				t.Helper()
				c, err := store.LoadCompetition("doomed")
				require.NoError(t, err)
				require.NotNil(t, c, "the competition must still exist")
			}

			for label, w := range map[string]*httptest.ResponseRecorder{
				"no confirm header":                     confirmDelete(r, "main-pw", ""),
				"wrong confirm header":                  confirmDelete(r, "main-pw", "nope"),
				"sign-in as the confirm, wrong sign-in": confirmDelete(r, "nope", "main-pw"),
			} {
				assert.Equal(t, http.StatusUnauthorized, w.Code, label)
				stillThere()
			}

			// The refusal names the sentence in the house shape.
			w := confirmDelete(r, "main-pw", "nope")
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, confirmWrongSentence, body["error"])
			assert.Equal(t, confirmWrongSentence, body["message"])
			assert.Equal(t, "confirm_password_wrong", body["code"])

			w = confirmDelete(r, "main-pw", "")
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, confirmWrongSentence, body["error"])
			assert.Equal(t, "confirm_password_required", body["code"])
			stillThere()

			w = confirmDelete(r, "main-pw", "main-pw")
			require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
			gone, err := store.LoadCompetition("doomed")
			require.NoError(t, err)
			assert.Nil(t, gone, "the right password deletes the competition")
		})
	}
}

// The gate that already keeps an anonymous caller out of the route in self-run
// answers first, with its own body, whatever confirm header is sent.
func TestDeleteCompetition_SelfRunAnonymousStillRefusedByTheGate(t *testing.T) {
	r, store := confirmDeleteRig(t, state.TournamentModeSelfRun, false)
	w := confirmDelete(r, "", "main-pw")
	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Contains(t, w.Body.String(), "invalid admin password")
	c, err := store.LoadCompetition("doomed")
	require.NoError(t, err)
	require.NotNil(t, c)
}

// Only the competition delete asks. Discarding a draw and the other
// destructive routes stay one-step for a signed-in organiser.
func TestConfirmPassword_OnlyDeleteCompetitionAsks(t *testing.T) {
	r, _ := confirmDeleteRig(t, state.TournamentModeOfficiated, false)
	req := httptest.NewRequest(http.MethodDelete, "/api/competitions/doomed/draw", nil)
	req.Header.Set("X-Tournament-Password", "main-pw")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.NotEqual(t, http.StatusUnauthorized, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), confirmWrongSentence)
}

func TestCORSAllowsTheConfirmPasswordHeader(t *testing.T) {
	r, _ := confirmDeleteRig(t, state.TournamentModeOfficiated, false)
	req := httptest.NewRequest(http.MethodOptions, "/api/competitions/doomed", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Headers"), "X-Confirm-Password")
}
