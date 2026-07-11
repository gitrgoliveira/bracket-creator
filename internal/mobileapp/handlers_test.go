package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

func setupTestRouter(t testing.TB) (*gin.Engine, *state.Store, *engine.Engine, *Hub, string) {
	tempDir, err := os.MkdirTemp("", "mobileapp-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)

	eng := engine.New(store)
	hub := NewHub()

	gin.SetMode(gin.TestMode)
	r := gin.New()

	// Public viewer
	viewer := r.Group("/api/viewer")
	RegisterViewerHandlers(viewer, store, eng)
	RegisterDisplayHandlers(viewer, store)

	// Stateless schedule estimator, public, no auth.
	publicAPI := r.Group("/api")
	RegisterScheduleHandlers(publicAPI)
	RegisterPublicSwissHandlers(publicAPI, store, eng)
	RegisterPublicLeagueHandlers(publicAPI, eng)

	// Admin API
	admin := r.Group("/api")
	RegisterTournamentHandlers(admin, store, hub, NewFileVerifier(store))
	RegisterImportHandlers(admin, store, hub, NewFileElevatedVerifier(store))
	RegisterCompetitionHandlers(admin, store, eng, hub, NewFileElevatedVerifier(store))
	RegisterParticipantHandlers(admin, store, eng, hub, NewFileElevatedVerifier(store))
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)
	RegisterEligibilityHandlers(admin, store, hub)
	RegisterLineupHandlers(admin, store, store, store, stubBroadcaster{})
	RegisterSwissHandlers(admin, store, eng, hub)

	return r, store, eng, hub, tempDir
}

func TestTournamentHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Create initial tournament (no longer auto-created by store init).
	// Courts is required by the POST/PUT validateCourts guard (1..26
	// single-character labels); the test PUTs below reuse this `tour`
	// so we need it populated to satisfy the new contract.
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Initial Tournament", Password: "", Courts: []string{"A"}}))

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
	// comp.Name trim in handlers_competition.go, the CreateTournament
	// UI now trims client-side, but older clients and direct API
	// callers could still send padded values. Date is included for
	// cross-file guard symmetry with the competition/import paths
	// which trim their own Date field.
	tour.Name = "  Padded Tournament  "
	tour.Venue = "  Crystal Palace  "
	tour.Date = "  12-05-2026  "
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
	assert.Equal(t, "12-05-2026", t3.Date, "Date should be trimmed on PUT")

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

	// POST /api/tournament also trims. CreateTournament in app.jsx now
	// validates the trimmed name client-side before submit, but this
	// regression test covers older cached clients and direct API callers
	// that can still send padded values, the server-side trim is the
	// canonical defense layer so persisted records are always trimmed.
	postTour := state.Tournament{Name: "  Posted Tournament  ", Venue: "  Some Venue  ", Date: "  20-07-2026  ", Password: "secret", Courts: []string{"A"}}
	body, _ = json.Marshal(postTour)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/tournament", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)
	t4, _ := store.LoadTournament()
	assert.Equal(t, "Posted Tournament", t4.Name)
	assert.Equal(t, "Some Venue", t4.Venue)
	assert.Equal(t, "20-07-2026", t4.Date, "Date should be trimmed on POST")

	// Whitespace-only name must be rejected after trim. Persisting an
	// empty Name produces a blank tournament title in the admin UI and
	// violates the documented "tournament has a name" invariant. The
	// frontend CreateTournament/AdminEditTournament forms both
	// pre-validate, but this regression covers older cached clients and
	// direct API callers.
	for _, method := range []string{"PUT", "POST"} {
		blank := state.Tournament{Name: "   ", Venue: "Anywhere", Password: "secret"}
		body, _ = json.Marshal(blank)
		w = httptest.NewRecorder()
		req, _ = http.NewRequest(method, "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code,
			"%s /api/tournament with whitespace-only Name must return 400", method)
		assert.Contains(t, w.Body.String(), "tournament name is required",
			"%s /api/tournament rejection should explain the empty-name reason", method)
	}

	// Date must be DD-MM-YYYY (canonical format). Reject ISO YYYY-MM-DD
	// shape and semantically-invalid days (Feb 31). The frontend converts
	// ISO→DMY at the input boundary; direct API callers must send DMY.
	for _, method := range []string{"PUT", "POST"} {
		for _, badDate := range []string{
			"2026-05-12", // ISO shape, not accepted
			"31-02-2026", // Feb 31 semantically invalid
			"32-01-2026", // day 32 invalid
			"12-13-2026", // month 13 invalid
			"not a date",
		} {
			bad := state.Tournament{Name: "Some Name", Venue: "Venue", Date: badDate, Password: "secret", Courts: []string{"A"}}
			body, _ = json.Marshal(bad)
			w = httptest.NewRecorder()
			req, _ = http.NewRequest(method, "/api/tournament", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"%s /api/tournament with Date=%q must return 400", method, badDate)
			assert.Contains(t, w.Body.String(), "date must be DD-MM-YYYY",
				"%s /api/tournament rejection should explain the date format requirement", method)
		}
	}

	// Year must be within minDateYear..maxDateYear (1900..2100). The JS
	// validator at admin_helpers.jsx applies the same range; without
	// matching bounds here, a direct API call landing e.g. "01-01-1800"
	// on disk would block every subsequent admin settings save because
	// the frontend's saveLater re-validates the stored date on every
	// PUT and surfaces an inline error before reaching the wire.
	for _, method := range []string{"PUT", "POST"} {
		for _, outOfRangeDate := range []string{
			"01-01-1800", // below MIN_YEAR
			"01-01-1899", // just below MIN_YEAR
			"01-01-2101", // just above MAX_YEAR
			"01-01-3000", // far above MAX_YEAR
		} {
			bad := state.Tournament{Name: "Some Name", Venue: "Venue", Date: outOfRangeDate, Password: "secret", Courts: []string{"A"}}
			body, _ = json.Marshal(bad)
			w = httptest.NewRecorder()
			req, _ = http.NewRequest(method, "/api/tournament", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"%s /api/tournament with Date=%q must return 400 (year out of range)", method, outOfRangeDate)
			assert.Contains(t, w.Body.String(), "date year must be between",
				"%s /api/tournament rejection should explain the year-range requirement", method)
		}
	}

	// POST /api/tournament must reject empty Password. AuthMiddleware
	// allows POST /api/tournament unauthenticated when the tournament
	// is uninitialized (bootstrap path). If Password == "" lands on
	// disk, AuthMiddleware's later `password != t.Password` check
	// vacuously passes for any request with an empty
	// X-Tournament-Password header, exposing every /api/* endpoint
	// unauthenticated. Pin the guard here so a future refactor that
	// drops it surfaces immediately.
	{
		os.Remove(filepath.Join(tempDir, "tournament.md"))
		// Courts populated so the Password check (not Courts validation)
		// is the rejection reason, handler validates Name → Courts →
		// Password in order, and we're specifically pinning the
		// Password guard here.
		emptyPass := state.Tournament{Name: "No Password", Password: "", Courts: []string{"A"}}
		body, _ := json.Marshal(emptyPass)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code,
			"POST /api/tournament with empty Password must return 400")
		assert.Contains(t, w.Body.String(), "tournament password is required",
			"rejection should explain the empty-password reason")
		// Confirm it didn't land on disk.
		stored, _ := store.LoadTournament()
		assert.Nil(t, stored, "empty-password tournament should not be persisted")
	}

	// PUT /api/tournament must PRESERVE the stored Password when the
	// incoming body sends "" (the frontend AdminEditTournament uses
	// `password: pass || undefined` to mean "keep current"). Without
	// this preserve step, a routine name-edit save would silently
	// clobber the stored password with "" and expose the same
	// AuthMiddleware vacuous-pass scenario as the POST guard above.
	{
		// Seed a tournament with a real password.
		seed := state.Tournament{Name: "Preserve Test", Password: "kept-secret", Courts: []string{"A"}}
		require.NoError(t, store.SaveTournament(&seed))

		// PUT with empty Password, should preserve "kept-secret".
		update := state.Tournament{Name: "Preserve Test", Venue: "New Venue", Password: "", Courts: []string{"A"}}
		body, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code,
			"PUT with empty Password should succeed (preserve-stored semantics)")
		stored, err := store.LoadTournament()
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "kept-secret", stored.Password,
			"PUT with empty Password must preserve the stored password, not clobber to empty")
		assert.Equal(t, "New Venue", stored.Venue, "other fields should still update")
	}

	// PUT /api/tournament with a new non-empty Password should update
	// the stored password (legitimate password-change flow).
	{
		seed := state.Tournament{Name: "Change Test", Password: "old-secret", Courts: []string{"A"}}
		require.NoError(t, store.SaveTournament(&seed))

		update := state.Tournament{Name: "Change Test", Password: "new-secret", Courts: []string{"A"}}
		body, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		stored, _ := store.LoadTournament()
		assert.Equal(t, "new-secret", stored.Password,
			"PUT with non-empty Password should update the stored password")
	}

	// PUT /api/tournament when the stored tournament ALSO has an empty
	// Password (legacy state from a pre-fix install) must reject at
	// the handler, defense-in-depth for the case where AuthMiddleware
	// is bypassed (this test setupTestRouter intentionally does NOT
	// install AuthMiddleware; in production it does, and the middleware
	// also fails closed on empty-stored-password as of the
	// TestAuthMiddleware_LegacyEmptyStoredPassword_NoBypass commit).
	// Two-layer defense: middleware blocks any request reaching the
	// handler with empty stored password; handler rejects the save
	// anyway in case some future test or codepath bypasses middleware.
	{
		// Force a legacy state: save directly via store, bypassing the
		// new POST guard.
		legacy := state.Tournament{Name: "Legacy", Password: "", Courts: []string{"A"}}
		require.NoError(t, store.SaveTournament(&legacy))

		update := state.Tournament{Name: "Legacy", Venue: "Update", Password: "", Courts: []string{"A"}}
		body, _ := json.Marshal(update)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code,
			"PUT with empty Password against legacy empty-password tournament must reject")
		assert.Contains(t, w.Body.String(), "tournament password is required")
	}

	// mp-ef3 Copilot round 2: public info field trimming, contacts-count
	// validation, URL validation, and contacts field-length validation.

	t.Run("PUT trims public info fields", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Trim Test", Password: "secret", Courts: []string{"A"}}))
		tour := state.Tournament{
			Name:         "Trim Test",
			Password:     "secret",
			Courts:       []string{"A"},
			VenueAddress: "  123 Main St  ",
			OpeningTime:  "  09:00  ",
			Contacts:     []state.TournamentContact{{Label: "  Email  ", Value: "  test@example.com  "}},
		}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		saved, _ := store.LoadTournament()
		assert.Equal(t, "123 Main St", saved.VenueAddress)
		assert.Equal(t, "09:00", saved.OpeningTime)
		require.Len(t, saved.Contacts, 1)
		assert.Equal(t, "Email", saved.Contacts[0].Label)
		assert.Equal(t, "test@example.com", saved.Contacts[0].Value)
	})

	t.Run("PUT rejects contacts over MaxTournamentContacts", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Max Contacts", Password: "secret", Courts: []string{"A"}}))
		contacts := make([]state.TournamentContact, MaxTournamentContacts+1)
		for i := range contacts {
			contacts[i] = state.TournamentContact{Label: fmt.Sprintf("Label%d", i), Value: fmt.Sprintf("value%d@example.com", i)}
		}
		tour := state.Tournament{
			Name:     "Max Contacts",
			Password: "secret",
			Courts:   []string{"A"},
			Contacts: contacts,
		}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "contacts")
	})

	t.Run("PUT rejects javascript: URL in venueMapURL", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "URL Test", Password: "secret", Courts: []string{"A"}}))
		tour := state.Tournament{
			Name:        "URL Test",
			Password:    "secret",
			Courts:      []string{"A"},
			VenueMapURL: "javascript:alert(1)",
		}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("PUT validates contacts field lengths", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Field Len", Password: "secret", Courts: []string{"A"}}))
		longLabel := strings.Repeat("x", MaxLenContactLabel+1)
		tour := state.Tournament{
			Name:     "Field Len",
			Password: "secret",
			Courts:   []string{"A"},
			Contacts: []state.TournamentContact{{Label: longLabel, Value: "ok@example.com"}},
		}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "contacts[0].label")
	})

	// mp-s1gl: publicURL validation + trailing-slash normalization.
	t.Run("PUT accepts empty publicURL (field is optional)", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}}))
		tour := state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}, PublicURL: ""}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})

	t.Run("PUT accepts valid https publicURL and strips trailing slash", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}}))
		tour := state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}, PublicURL: "https://my-tournament.example.com/"}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		stored, err := store.LoadTournament()
		require.NoError(t, err)
		assert.Equal(t, "https://my-tournament.example.com", stored.PublicURL, "trailing slash must be stripped")
	})

	t.Run("PUT rejects non-http publicURL", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}}))
		tour := state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}, PublicURL: "javascript:alert(1)"}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "publicURL")
	})

	t.Run("PUT rejects over-500-char publicURL", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}}))
		longURL := "https://example.com/" + strings.Repeat("x", MaxLenPublicURL)
		tour := state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}, PublicURL: longURL}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "publicURL")
	})

	t.Run("PUT accepts http publicURL (non-https passes validation)", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}}))
		tour := state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}, PublicURL: "http://staging.example.com"}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
	t.Run("PUT rejects scheme-only publicURL (no host)", func(t *testing.T) {
		require.NoError(t, store.SaveTournament(&state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}}))
		tour := state.Tournament{Name: "PU Test", Password: "secret", Courts: []string{"A"}, PublicURL: "https://"}
		body, _ := json.Marshal(tour)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "publicURL")
	})
}

// TestTournamentHandlers_LockedMode_PUTRejectsPasswordChange pins the
// rule that locked-mode admins cannot rotate the credential via the
// normal admin edit form. The on-disk Password is non-authoritative
// (auth uses the env-var bcrypt hash), so silently accepting a PUT
// with a new password would mislead the operator into believing
// rotation worked when it didn't. Reject with a 400 explaining the
// situation.
//
// Empty password remains acceptable (the SPA's
// `password: pass || undefined` pattern is the common shape when an
// operator is editing name/venue/courts without touching the password
// field).
func TestTournamentHandlers_LockedMode_PUTRejectsPasswordChange(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "locked-put-reject-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	hub := NewHub()

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Locked",
		Password: "preserved-canary-Aa",
		Courts:   []string{"A"},
	}))

	hash, err := bcrypt.GenerateFromPassword([]byte("kotai-A"), bcrypt.MinCost)
	require.NoError(t, err)
	bcryptV, err := NewBcryptVerifier(string(hash))
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(AuthMiddleware(bcryptV, store))
	RegisterTournamentHandlers(api, store, hub, bcryptV)

	t.Run("non-empty password rejected with 400", func(t *testing.T) {
		body, _ := json.Marshal(state.Tournament{
			Name:     "Locked Renamed",
			Password: "operator-tried-to-rotate",
			Courts:   []string{"A"},
		})
		req := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewReader(body))
		req.Header.Set("X-Tournament-Password", "kotai-A")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "locked mode")
		// On-disk record must NOT have been touched.
		loaded, err := store.LoadTournament()
		require.NoError(t, err)
		assert.Equal(t, "Locked", loaded.Name, "PUT must not partially apply when password change is rejected")
		assert.Equal(t, "preserved-canary-Aa", loaded.Password)
	})

	t.Run("empty password is allowed (admin editing other fields)", func(t *testing.T) {
		body, _ := json.Marshal(state.Tournament{
			Name:   "Locked Renamed",
			Courts: []string{"A"},
		})
		req := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewReader(body))
		req.Header.Set("X-Tournament-Password", "kotai-A")
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		loaded, err := store.LoadTournament()
		require.NoError(t, err)
		assert.Equal(t, "Locked Renamed", loaded.Name)
		assert.Equal(t, "preserved-canary-Aa", loaded.Password)
	})
}

// TestTournamentHandlers_FileMode_PUTPasswordChange_BroadcastsResetEvent
// pins that rotating the admin password via PUT /api/tournament (the
// admin edit form) broadcasts EventPasswordReset alongside
// EventTournamentUpdated. Without the second event, other logged-in
// admins keep the stale `bc_password` in localStorage and only
// notice on their next write that 401s, surprising UX. The /reset
// endpoint already does this; mirroring it here closes the gap for
// the in-admin rotation path.
func TestTournamentHandlers_FileMode_PUTPasswordChange_BroadcastsResetEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "file-put-pwreset-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	hub := NewHub()
	ch := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(ch) })

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "File Tournament",
		Password: "alpha",
		Courts:   []string{"A"},
	}))
	fileV := NewFileVerifier(store)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(AuthMiddleware(fileV, store))
	RegisterTournamentHandlers(api, store, hub, fileV)

	body, _ := json.Marshal(state.Tournament{
		Name:     "File Tournament",
		Password: "beta",
		Courts:   []string{"A"},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "alpha")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "PUT body=%s", w.Body.String())

	seen := map[string]bool{}
	for range 4 {
		select {
		case msg := <-ch:
			for _, ev := range []string{"tournament_updated", "password_reset"} {
				if strings.Contains(msg, `"type":"`+ev+`"`) {
					seen[ev] = true
				}
			}
		default:
		}
	}
	assert.True(t, seen["tournament_updated"], "tournament_updated event missing")
	assert.True(t, seen["password_reset"], "password_reset event missing, other admins keep stale credential until next write 401s")
}

// Negative: a PUT that does NOT change the password (e.g. admin
// editing the venue with `password: pass || undefined`) must NOT
// fire EventPasswordReset, otherwise every admin gets kicked out on
// every name edit.
func TestTournamentHandlers_FileMode_PUTNoPasswordChange_NoResetEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "file-put-no-pwreset-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	hub := NewHub()
	ch := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(ch) })

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Original",
		Password: "alpha",
		Courts:   []string{"A"},
	}))
	fileV := NewFileVerifier(store)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(AuthMiddleware(fileV, store))
	RegisterTournamentHandlers(api, store, hub, fileV)

	body, _ := json.Marshal(state.Tournament{
		Name:   "Renamed",
		Courts: []string{"A"},
	})
	req := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewReader(body))
	req.Header.Set("X-Tournament-Password", "alpha")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	sawPasswordReset := false
	for range 4 {
		select {
		case msg := <-ch:
			if strings.Contains(msg, `"type":"password_reset"`) {
				sawPasswordReset = true
			}
		default:
		}
	}
	assert.False(t, sawPasswordReset, "name-only edit must NOT broadcast password_reset")
}

// TestTournamentHandlers_FileMode_POSTBootstrap_NoResetEvent pins the
// fix for a real bug Copilot caught on PR #108: the POST handler used to
// compare `t.Password != oldPass` where `oldPass = ""` on a fresh
// bootstrap, so any non-empty new password broadcast EventPasswordReset.
// The creating tab's SSE subscription (active from the
// CreateTournament-screen mount) then received the empty-originator
// event and immediately cleared the freshly cached password,
// kicking the user back to AuthModal moments after they finished
// bootstrap. POST must only emit password_reset when OVERWRITING an
// existing record whose password actually changed.
func TestTournamentHandlers_FileMode_POSTBootstrap_NoResetEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "file-post-bootstrap-no-pwreset-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	hub := NewHub()
	ch := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(ch) })

	fileV := NewFileVerifier(store)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(AuthMiddleware(fileV, store))
	RegisterTournamentHandlers(api, store, hub, fileV)

	// Fresh deploy: no tournament on disk yet. AuthMiddleware's
	// uninitialized-bootstrap branch lets the unauthenticated POST through.
	body, _ := json.Marshal(state.Tournament{
		Name:     "Fresh Tournament",
		Password: "alpha",
		Courts:   []string{"A"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusCreated, w.Code, "POST body=%s", w.Body.String())

	sawPasswordReset := false
	sawTournamentUpdated := false
	for range 6 {
		select {
		case msg := <-ch:
			if strings.Contains(msg, `"type":"password_reset"`) {
				sawPasswordReset = true
			}
			if strings.Contains(msg, `"type":"tournament_updated"`) {
				sawTournamentUpdated = true
			}
		default:
		}
	}
	assert.True(t, sawTournamentUpdated, "tournament_updated event expected on bootstrap (so viewers refresh)")
	assert.False(t, sawPasswordReset, "first-time bootstrap must NOT broadcast password_reset, the creating tab's own SSE would clear the credential it just persisted")
}

// TestTournamentHandlers_FileMode_POSTOverwriteWithNewPassword_BroadcastsResetEvent
// is the positive counterpart of the bootstrap test above: when a POST
// OVERWRITES an existing tournament with a new password (a re-bootstrap
// path, rare but possible), other logged-in admin sessions need to
// clear their stale credentials. Same rationale as the PUT password-
// change broadcast.
func TestTournamentHandlers_FileMode_POSTOverwriteWithNewPassword_BroadcastsResetEvent(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "file-post-overwrite-pwreset-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	hub := NewHub()
	ch := hub.Subscribe()
	t.Cleanup(func() { hub.Unsubscribe(ch) })

	// Pre-seed an existing tournament with password "alpha".
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Existing Tournament",
		Password: "alpha",
		Courts:   []string{"A"},
	}))
	fileV := NewFileVerifier(store)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	api.Use(AuthMiddleware(fileV, store))
	RegisterTournamentHandlers(api, store, hub, fileV)

	// Re-bootstrap with a different password. AuthMiddleware no longer
	// takes the uninitialized path (a real tournament exists), so we
	// must authenticate with the current password to reach the POST
	// handler.
	body, _ := json.Marshal(state.Tournament{
		Name:     "Existing Tournament",
		Password: "beta",
		Courts:   []string{"A"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/tournament", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "alpha")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusCreated, w.Code, "POST body=%s", w.Body.String())

	seen := map[string]bool{}
	for range 6 {
		select {
		case msg := <-ch:
			for _, ev := range []string{"tournament_updated", "password_reset"} {
				if strings.Contains(msg, `"type":"`+ev+`"`) {
					seen[ev] = true
				}
			}
		default:
		}
	}
	assert.True(t, seen["tournament_updated"], "tournament_updated event missing")
	assert.True(t, seen["password_reset"], "password_reset event missing, overwriting an existing tournament's password must clear other admins' cached credentials")
}

// TestTournamentHandlers_ModeSwitchPreservesStoredPassword walks the
// operator-facing scenario where a deployment migrates between auth
// modes:
//
//  1. Start in file mode, set the admin password to "alpha".
//  2. Switch to locked mode. The on-disk Password is now non-
//     authoritative (auth uses the env-var bcrypt hash), but the
//     stored value MUST be preserved on disk so that a future flip
//     back to file mode is recoverable, the operator might be
//     experimenting with locked mode and need to roll back.
//  3. Confirm that locked-mode PUTs preserve the stored value
//     without leaking it through responses.
//  4. Switch back to file mode. The originally-set "alpha" password
//     authenticates again, proving the on-disk record was never
//     clobbered.
//
// This is the integration test for the "mode-switching leaks the
// original file-mode password" caveat documented in
// docs/user-guide/mobile-app.md. Pinning the contract here means a
// future change that decides to scrub the stored password on lock
// will break this test and force a deliberate update to the docs.
func TestTournamentHandlers_ModeSwitchPreservesStoredPassword(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mode-switch-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	hub := NewHub()

	// --- Phase 1: file mode, set password "alpha".
	fileV := NewFileVerifier(store)

	gin.SetMode(gin.TestMode)
	fileR := gin.New()
	fileAPI := fileR.Group("/api")
	fileAPI.Use(AuthMiddleware(fileV, store))
	RegisterTournamentHandlers(fileAPI, store, hub, fileV)

	bootstrapBody, _ := json.Marshal(state.Tournament{
		Name: "Migration Test", Password: "alpha", Courts: []string{"A"},
	})
	bootReq := httptest.NewRequest(http.MethodPost, "/api/tournament", bytes.NewReader(bootstrapBody))
	bootReq.Header.Set("Content-Type", "application/json")
	bootW := httptest.NewRecorder()
	fileR.ServeHTTP(bootW, bootReq)
	require.Equalf(t, http.StatusCreated, bootW.Code, "phase 1 bootstrap failed: %s", bootW.Body.String())

	// Confirm "alpha" authenticates in file mode.
	authReq := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	authReq.Header.Set("X-Tournament-Password", "alpha")
	authW := httptest.NewRecorder()
	fileR.ServeHTTP(authW, authReq)
	require.Equal(t, http.StatusOK, authW.Code, "phase 1 'alpha' must authenticate in file mode")

	// --- Phase 2: switch to locked mode (operator restarts with
	// --lock-password). The on-disk record still has Password="alpha".
	hash, err := bcrypt.GenerateFromPassword([]byte("kotai-A"), bcrypt.MinCost)
	require.NoError(t, err)
	bcryptV, err := NewBcryptVerifier(string(hash))
	require.NoError(t, err)

	lockedR := gin.New()
	lockedAPI := lockedR.Group("/api")
	lockedAPI.Use(AuthMiddleware(bcryptV, store))
	RegisterTournamentHandlers(lockedAPI, store, hub, bcryptV)

	// "alpha" no longer authenticates (auth uses env-var hash).
	alphaReq := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	alphaReq.Header.Set("X-Tournament-Password", "alpha")
	alphaW := httptest.NewRecorder()
	lockedR.ServeHTTP(alphaW, alphaReq)
	require.Equal(t, http.StatusUnauthorized, alphaW.Code, "phase 2 'alpha' must NOT authenticate under env-var hash")

	// "kotai-A" does authenticate, and the response strips the stored
	// "alpha" so the admin UI doesn't see a stale credential.
	envReq := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	envReq.Header.Set("X-Tournament-Password", "kotai-A")
	envW := httptest.NewRecorder()
	lockedR.ServeHTTP(envW, envReq)
	require.Equal(t, http.StatusOK, envW.Code)
	var lockedT state.Tournament
	require.NoError(t, json.Unmarshal(envW.Body.Bytes(), &lockedT))
	assert.Empty(t, lockedT.Password, "phase 2 GET response must redact stored password")

	// --- Phase 3: PUT a rename under locked mode. The stored password
	// MUST stay intact so phase 4 can recover.
	putBody, _ := json.Marshal(state.Tournament{
		Name: "Renamed", Courts: []string{"A"},
	})
	putReq := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewReader(putBody))
	putReq.Header.Set("X-Tournament-Password", "kotai-A")
	putReq.Header.Set("Content-Type", "application/json")
	putW := httptest.NewRecorder()
	lockedR.ServeHTTP(putW, putReq)
	require.Equalf(t, http.StatusOK, putW.Code, "phase 3 PUT failed: %s", putW.Body.String())

	// Inspect the on-disk record directly: name should be the new value
	// AND the original password should still be there (just not visible
	// via the API).
	loadedT, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "Renamed", loadedT.Name)
	assert.Equal(t, "alpha", loadedT.Password,
		"phase 3 locked-mode PUT must preserve the original file-mode password on disk")

	// --- Phase 4: switch back to file mode (operator drops
	// --lock-password). The originally-set "alpha" authenticates again,
	// proving the rollback path works.
	fileV2 := NewFileVerifier(store)
	fileR2 := gin.New()
	fileAPI2 := fileR2.Group("/api")
	fileAPI2.Use(AuthMiddleware(fileV2, store))
	RegisterTournamentHandlers(fileAPI2, store, hub, fileV2)

	recoveryReq := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	recoveryReq.Header.Set("X-Tournament-Password", "alpha")
	recoveryW := httptest.NewRecorder()
	fileR2.ServeHTTP(recoveryW, recoveryReq)
	assert.Equal(t, http.StatusOK, recoveryW.Code,
		"phase 4 rollback to file mode must accept the original 'alpha' password")

	// And the GET response in file mode reveals the password (not
	// redacted), since file mode treats it as the live credential.
	var recoveredT state.Tournament
	require.NoError(t, json.Unmarshal(recoveryW.Body.Bytes(), &recoveredT))
	assert.Equal(t, "alpha", recoveredT.Password,
		"phase 4 file-mode GET must surface the recovered password (not redacted)")
}

// TestTournamentHandlers_LockedMode_StripPasswordOnResponses pins the
// locked-mode redaction contract: GET, PUT, and POST responses must
// NOT leak any stored password value to the client. The on-disk
// Password is irrelevant in locked mode (auth comes from the env-var
// bcrypt hash) but the handler still preserves it on PUT to avoid
// clobbering a value that might be carried back if locked mode is
// later disabled. Pre-fix, that preserved value flowed through the
// PUT response body, re-exposing an old file-mode credential to any
// authenticated admin who happened to PUT a name/venue change.
func TestTournamentHandlers_LockedMode_StripPasswordOnResponses(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "locked-mode-test-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(tempDir) })

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	hub := NewHub()

	// Seed a tournament with a known stored password, simulating the
	// case where an operator originally ran file mode then re-deployed
	// in locked mode without scrubbing tournament.md.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Locked Tournament",
		Password: "preserved-canary-Aa",
		Courts:   []string{"A"},
	}))

	hash, err := bcrypt.GenerateFromPassword([]byte("kotai-A"), bcrypt.MinCost)
	require.NoError(t, err)
	bcryptV, err := NewBcryptVerifier(string(hash))
	require.NoError(t, err)

	gin.SetMode(gin.TestMode)
	r := gin.New()
	api := r.Group("/api")
	// AuthMiddleware delegates verification to bcryptV; tests below
	// send the matching env password.
	api.Use(AuthMiddleware(bcryptV, store))
	RegisterTournamentHandlers(api, store, hub, bcryptV)

	// GET /tournament, password must be stripped.
	getReq := httptest.NewRequest(http.MethodGet, "/api/tournament", nil)
	getReq.Header.Set("X-Tournament-Password", "kotai-A")
	getW := httptest.NewRecorder()
	r.ServeHTTP(getW, getReq)
	require.Equal(t, http.StatusOK, getW.Code)
	var getT state.Tournament
	require.NoError(t, json.Unmarshal(getW.Body.Bytes(), &getT))
	assert.Empty(t, getT.Password, "GET response must not leak stored password in locked mode")
	assert.NotContains(t, getW.Body.String(), "preserved-canary-Aa")

	// PUT /tournament, admin changes the name; response must redact
	// the password that was preserved via the transform.
	putBody, _ := json.Marshal(state.Tournament{
		Name:   "Renamed Tournament",
		Courts: []string{"A"},
		// No Password field; even if the SPA sent one, the locked-mode
		// transform ignores it and preserves the stored value.
	})
	putReq := httptest.NewRequest(http.MethodPut, "/api/tournament", bytes.NewReader(putBody))
	putReq.Header.Set("X-Tournament-Password", "kotai-A")
	putReq.Header.Set("Content-Type", "application/json")
	putW := httptest.NewRecorder()
	r.ServeHTTP(putW, putReq)
	require.Equalf(t, http.StatusOK, putW.Code, "body=%s", putW.Body.String())
	var putT state.Tournament
	require.NoError(t, json.Unmarshal(putW.Body.Bytes(), &putT))
	assert.Empty(t, putT.Password, "PUT response must not leak stored password in locked mode (regression for handlers_tournament.go:270)")
	assert.NotContains(t, putW.Body.String(), "preserved-canary-Aa")

	// And the stored record on disk should still hold the original
	// password, locked-mode redaction is response-only, not a destructive
	// rewrite (the operator can flip back to file mode and recover).
	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, "preserved-canary-Aa", loaded.Password,
		"locked-mode response redaction must not clobber the on-disk value")
}

// TestTournamentHandlers_MaxLengthCaps verifies the defense-in-depth
// length caps from validation.go are enforced on POST and PUT
// /tournament. These caps guard against unbounded YAML inflation
// in tournament.md, a 1MB Name or Venue is silently accepted
// pre-fix and bloats every subsequent load.
func TestTournamentHandlers_MaxLengthCaps(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed an initialized tournament so PUT (and not POST-bootstrap)
	// is what runs the cap check.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Seed",
		Password: "secret",
		Courts:   []string{"A"},
	}))

	type lengthCase struct {
		field string
		body  state.Tournament
	}
	overCap := []lengthCase{
		{
			field: "name",
			body: state.Tournament{
				Name:     strings.Repeat("n", 201),
				Password: "secret",
				Courts:   []string{"A"},
			},
		},
		{
			field: "venue",
			body: state.Tournament{
				Name:     "OK",
				Venue:    strings.Repeat("v", 201),
				Password: "secret",
				Courts:   []string{"A"},
			},
		},
		{
			field: "password",
			body: state.Tournament{
				Name:     "OK",
				Password: strings.Repeat("p", 257),
				Courts:   []string{"A"},
			},
		},
		{
			field: "openingBlock",
			body: state.Tournament{
				Name:         "OK",
				Password:     "secret",
				Courts:       []string{"A"},
				OpeningBlock: strings.Repeat("o", 17),
			},
		},
	}
	for _, method := range []string{"PUT", "POST"} {
		for _, lc := range overCap {
			body, _ := json.Marshal(lc.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(method, "/api/tournament", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"%s /api/tournament over-cap %s must return 400", method, lc.field)
			assert.Contains(t, w.Body.String(), lc.field,
				"%s /api/tournament rejection must name the field", method)
		}
	}

	// Sanity: exactly-at-cap values pass (when the rest of the body is
	// valid). Bounds inclusive so the cap is "<= N", not "< N".
	atCap := state.Tournament{
		Name:     strings.Repeat("n", 200),
		Venue:    strings.Repeat("v", 200),
		Password: "secret",
		Courts:   []string{"A"},
	}
	body, _ := json.Marshal(atCap)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code,
		"PUT /api/tournament with exactly-200-char Name/Venue must be accepted")
}

// TestTournamentHandlers_ConcurrentPUT_PasswordChangeNotLost pins the
// TOCTOU fix in store.UpdateTournamentChanged. Pre-atomic-primitive,
// the PUT handler called LoadTournament + SaveTournamentChanged
// sequentially with no shared lock, two concurrent PUTs (one
// preserving the password by sending "", one changing the password)
// could race so the empty-password PUT's late save overwrote the
// change-password PUT's earlier save, silently losing the password
// change. With the atomic store primitive, the entire
// load + preserve + save sequence runs under the store's write lock,
// so the password-change intent always wins regardless of arrival
// order.
//
// The test runs many iterations because a single-pass race window is
// narrow even pre-fix (just the I/O time between LoadTournament
// returning and SaveTournamentChanged starting). With the fix, every
// iteration deterministically lands with Password == "new-secret-N"
// (B's intent) regardless of scheduling.
func TestTournamentHandlers_ConcurrentPUT_PasswordChangeNotLost(t *testing.T) {
	const iterations = 20

	for i := range iterations {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		initialPass := "initial-secret"
		newPass := "new-secret"
		require.NoError(t, store.SaveTournament(&state.Tournament{
			Name:     "Concurrent Test",
			Password: initialPass,
			Courts:   []string{"A"},
		}))

		// PUT A: preserve-password intent (omits password by sending "")
		bodyA, _ := json.Marshal(state.Tournament{
			Name: "Concurrent Test", Venue: "Hall A", Password: "", Courts: []string{"A"},
		})
		// PUT B: change-password intent
		bodyB, _ := json.Marshal(state.Tournament{
			Name: "Concurrent Test", Venue: "Hall B", Password: newPass, Courts: []string{"A"},
		})

		var wg sync.WaitGroup
		wg.Add(2)
		// Run A and B in parallel. Even with the store-level lock,
		// they could interleave at any granularity outside the
		// critical section; the test is that B's password change is
		// NEVER lost regardless of which interleaving wins.
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(bodyA))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equalf(t, http.StatusOK, w.Code, "PUT A (preserve) should succeed; iter %d", i)
		}()
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(bodyB))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equalf(t, http.StatusOK, w.Code, "PUT B (change) should succeed; iter %d", i)
		}()
		wg.Wait()

		// Both PUTs are done. The password-change intent (B) must win
		// regardless of arrival order:
		//   - If B's transaction won the lock first: stored = newPass,
		//     then A's transaction loads current.Password = newPass,
		//     preserves it, saves newPass + Hall A. Final: newPass + Hall A.
		//   - If A's transaction won the lock first: stored = initialPass
		//     + Hall A (preserved). Then B's transaction: current.Password
		//     = initialPass, but desired.Password = newPass (no preserve
		//     needed). Saves newPass + Hall B. Final: newPass + Hall B.
		// Either way, Password == newPass. The Venue can be either Hall A
		// or Hall B, the test doesn't constrain that (standard
		// last-write-wins for fields both PUTs explicitly set).
		stored, err := store.LoadTournament()
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, newPass, stored.Password,
			"iter %d: B's password-change intent must NEVER be lost,  "+
				"got %q, expected %q. (Pre-fix: A's late save with preserved "+
				"initial password could clobber B's saved new password.)",
			i, stored.Password, newPass)
	}
}

// TestCompetitionHandlers_ConcurrentInvalidateVsComplete pins the
// TOCTOU fix for the competition-status admin actions. Pre-atomic-
// primitive, POST /invalidate and POST /complete each did
// LoadCompetition + saveCompetitionWithPlayers sequentially with no
// shared lock between Load and Save. Two concurrent admin actions
// (or admin-vs-auto-complete) could race so the later save clobbered
// the earlier mutation with stale Status.
//
// Now both handlers run through state.Store.UpdateCompetitionChanged,
// which holds the per-competition lock across load + status check +
// save. The status check happens INSIDE the lock so whichever
// transition lands first sets the floor; the second sees the moved
// status and 400s with "cannot be completed/invalidated from status X".
//
// 20 iterations to exercise the scheduler. Assertion: regardless of
// arrival order, the final stored Status is one of {invalid, complete},
// never the original "pools". One of the two requests succeeds
// (200), the other is 400-rejected (precondition no longer holds).
func TestCompetitionHandlers_ConcurrentInvalidateVsComplete(t *testing.T) {
	const iterations = 20

	for i := range iterations {
		r, store, _, _, tempDir := setupTestRouter(t)

		compID := "concurrent-status"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:     compID,
			Name:   "Concurrent Status",
			Status: state.CompStatusPools,
		}))

		var wg sync.WaitGroup
		wg.Add(2)
		var codeInvalidate, codeComplete int
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/invalidate", nil)
			r.ServeHTTP(w, req)
			codeInvalidate = w.Code
		}()
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/complete", nil)
			r.ServeHTTP(w, req)
			codeComplete = w.Code
		}()
		wg.Wait()

		// Exactly one should succeed (200), the other should reject
		// with 400 (precondition Status=="pools" no longer holds
		// after the first commit). Note: which of the two wins is
		// undefined, they're racing.
		successes := 0
		if codeInvalidate == http.StatusOK {
			successes++
		}
		if codeComplete == http.StatusOK {
			successes++
		}
		assert.Equal(t, 1, successes,
			"iter %d: exactly one of invalidate/complete should succeed (got invalidate=%d, complete=%d)",
			i, codeInvalidate, codeComplete)

		// Final stored Status must reflect whichever request won,
		// never the original "pools" (that would mean BOTH writes
		// lost their mutation, which is the pre-fix bug we're
		// proving is closed).
		stored, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Contains(t, []state.CompetitionStatus{state.CompStatusInvalid, state.CompStatusComplete}, stored.Status,
			"iter %d: Status must be Invalid or Complete (got %q, neither write landed?)",
			i, stored.Status)

		os.RemoveAll(tempDir)
	}
}

// TestCompetitionHandlers_ConcurrentPUTUniqueNameRace pins the
// rename-uniqueness atomicity fix. Pre-fix, two concurrent PUTs
// renaming different competitions to the same new name each loaded
// the OTHER comp to do the uniqueness check, saw it still had its
// old name, passed the check, and both landed, leaving two
// competitions on disk with the same Name.
//
// An earlier attempt folded the check into UpdateCompetitionChanged's
// per-comp lock transform, deadlocked AB-BA (each goroutine holds
// its own comp's write lock and tries to read-lock the other to
// check uniqueness). The fix is a separate global mutex
// (state.Store.WithCompetitionRenameLock) that serializes only the
// check+save window across all competitions; per-comp locks remain
// fine-grained for everything else.
//
// 20 iterations of two concurrent renames. Assertion: exactly one
// succeeds (200), the other rejects with 400. On disk, exactly one
// comp has the new name; the other keeps its original.
func TestCompetitionHandlers_ConcurrentPUTUniqueNameRace(t *testing.T) {
	const iterations = 20

	for i := range iterations {
		r, store, _, _, tempDir := setupTestRouter(t)

		// Two existing competitions, both will try to rename to "Cup".
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "comp-a", Name: "Comp A"}))
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "comp-b", Name: "Comp B"}))

		renameA, _ := json.Marshal(state.Competition{ID: "comp-a", Name: "Cup"})
		renameB, _ := json.Marshal(state.Competition{ID: "comp-b", Name: "Cup"})

		var wg sync.WaitGroup
		wg.Add(2)
		var codeA, codeB int
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/comp-a", bytes.NewBuffer(renameA))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			codeA = w.Code
		}()
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/comp-b", bytes.NewBuffer(renameB))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			codeB = w.Code
		}()
		wg.Wait()

		// Exactly one rename should succeed. Pre-fix: both could
		// pass the check and both land with Name="Cup".
		successes := 0
		if codeA == http.StatusOK {
			successes++
		}
		if codeB == http.StatusOK {
			successes++
		}
		assert.Equal(t, 1, successes,
			"iter %d: exactly one rename should succeed (got A=%d, B=%d)", i, codeA, codeB)

		// On disk: exactly one comp is named "Cup"; the other keeps
		// its original name.
		a, _ := store.LoadCompetition("comp-a")
		b, _ := store.LoadCompetition("comp-b")
		require.NotNilf(t, a, "iter %d: comp-a must still exist", i)
		require.NotNilf(t, b, "iter %d: comp-b must still exist", i)
		cupCount := 0
		if a.Name == "Cup" {
			cupCount++
		}
		if b.Name == "Cup" {
			cupCount++
		}
		assert.Equal(t, 1, cupCount,
			"iter %d: exactly one comp should be named 'Cup' (got A=%q, B=%q)", i, a.Name, b.Name)

		os.RemoveAll(tempDir)
	}
}

// TestCompetitionHandlers_ConcurrentPOSTSameName pins the same
// uniqueness guard for the create path: two concurrent POSTs creating
// new competitions with the same name (different IDs) must not both
// succeed.
func TestCompetitionHandlers_ConcurrentPOSTSameName(t *testing.T) {
	const iterations = 20

	for i := range iterations {
		r, store, _, _, tempDir := setupTestRouter(t)

		bodyA, _ := json.Marshal(state.Competition{ID: "new-a", Name: "Cup"})
		bodyB, _ := json.Marshal(state.Competition{ID: "new-b", Name: "Cup"})

		var wg sync.WaitGroup
		wg.Add(2)
		var codeA, codeB int
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(bodyA))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			codeA = w.Code
		}()
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(bodyB))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			codeB = w.Code
		}()
		wg.Wait()

		successes := 0
		if codeA == http.StatusCreated {
			successes++
		}
		if codeB == http.StatusCreated {
			successes++
		}
		assert.Equal(t, 1, successes,
			"iter %d: exactly one POST should succeed (got A=%d, B=%d)", i, codeA, codeB)

		// On disk: exactly one comp named "Cup".
		ids, _ := store.ListCompetitions()
		cupCount := 0
		for _, id := range ids {
			c, _ := store.LoadCompetition(id)
			if c != nil && c.Name == "Cup" {
				cupCount++
			}
		}
		assert.Equal(t, 1, cupCount,
			"iter %d: exactly one comp should be named 'Cup' (found %d)", i, cupCount)

		os.RemoveAll(tempDir)
	}
}

// TestPUTCompetition_RejectsBodyIDMismatch pins the Copilot #1 fix:
// PUT /api/competitions/comp-a with body `{id: "comp-b"}` previously
// silently overrode body.ID = "comp-a" (the URL value) and saved the
// record at path comp-a. That accepted malformed input as valid; the
// tightened contract returns 400 to surface the mismatch.
func TestPUTCompetition_RejectsBodyIDMismatch(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "comp-a", Name: "Comp A"}))

	body, _ := json.Marshal(state.Competition{ID: "comp-b", Name: "Comp A"}) // body ID disagrees with URL
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/comp-a", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"PUT with body.ID != URL.id must return 400")
	assert.Contains(t, w.Body.String(), "competition ID mismatch",
		"error message should explain the mismatch")

	// Verify both possible "victim" paths were untouched.
	a, _ := store.LoadCompetition("comp-a")
	b, _ := store.LoadCompetition("comp-b")
	require.NotNil(t, a)
	assert.Equal(t, "Comp A", a.Name, "comp-a must keep its name")
	assert.Nil(t, b, "comp-b must NOT have been created from the body")
}

// TestPOSTCompetition_RejectsExistingID pins the Copilot #2 fix:
// POST /api/competitions with an `id` that already exists previously
// passed name-uniqueness (the names differed) and then
// SaveCompetitionChanged overwrote the existing competition's config.
// POST is documented as CREATE; pre-existing ID is now 400.
func TestPOSTCompetition_RejectsExistingID(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "existing", Name: "Existing", NumberPrefix: "PRESERVED",
	}))

	body, _ := json.Marshal(state.Competition{ID: "existing", Name: "Different Name"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"POST with existing ID must return 400")
	assert.Contains(t, w.Body.String(), "already exists")

	// Verify the pre-existing competition's config is untouched.
	stored, _ := store.LoadCompetition("existing")
	require.NotNil(t, stored)
	assert.Equal(t, "Existing", stored.Name, "existing name must be preserved")
	assert.Equal(t, "PRESERVED", stored.NumberPrefix, "existing config must be preserved")
}

// TestPOSTCompetition_RollbackOnSaveParticipantsFailure pins the K3
// rollback: when POST /competitions carries a populated Players list,
// saveCompetitionWithPlayers does SaveCompetitionChanged → SaveParticipants
// sequentially. If SaveParticipants fails after SaveCompetitionChanged
// already wrote config.md, the orphaned config blocks retries with the
// ID-collision guard ("competition ID already exists"). The rollback
// removes config.md so the operator can re-run the POST after fixing
// the I/O issue. Mirrors the import handler's rollback contract
// (handlers_import_test.go Import Rollback On SaveSeeds I/O Failure).
//
// Forces a deterministic SaveParticipants I/O failure by pre-creating
// a directory where participants.csv is supposed to be a file, on
// macOS/Linux/Windows, os.WriteFile against a directory path returns
// EISDIR / equivalent reliably.
func TestPOSTCompetition_RollbackOnSaveParticipantsFailure(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const cid = "rollback-create-test"
	// Plant a directory where participants.csv must be a file. POST
	// /competitions for `cid` will create the comp dir (MkdirAll is
	// idempotent), then SaveCompetitionChanged writes config.md
	// successfully, then SaveParticipants tries to write to
	// participants.csv → fails because the path is a directory.
	participantsAsDir := filepath.Join(tempDir, "competitions", cid, "participants.csv")
	require.NoError(t, os.MkdirAll(participantsAsDir, 0700))

	body := map[string]any{
		"id":     cid,
		"name":   "Rollback Test",
		"kind":   "individual",
		"format": "mixed",
		"date":   "12-05-2026",
		"courts": []string{"A"},
		// Populated Players triggers the saveCompetitionWithPlayers
		// SaveParticipants step that the planted directory blocks.
		"players": []map[string]any{
			{"Name": "Player 1", "Dojo": "Dojo A"},
		},
	}
	bodyBytes, _ := json.Marshal(body)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"POST must surface SaveParticipants I/O failure as 500")
	assert.Contains(t, w.Body.String(), "failed to save participants",
		"error message must identify the failing step")

	// Rollback assertion: the orphaned config.md must be gone so a
	// retry of the same POST passes the ID-collision guard at the
	// top of WithCompetitionRenameLock.
	stored, _ := store.LoadCompetition(cid)
	assert.Nil(t, stored,
		"rollback must remove config.md so retry isn't blocked by 'ID already exists'")
}

// TestPOSTTournament_ValidatesCourts pins Copilot #8: POST and PUT
// /api/tournament now call validateCourts (1..26 single-char labels)
// matching the spec. Direct API callers can no longer persist >26
// courts or multi-character labels.
func TestPOSTTournament_ValidatesCourts(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cases := []struct {
		name        string
		courts      []string
		wantErrText string
	}{
		// JSON-encoded responses escape `<` and `>` as `<`/`>`,
		// so the assertion strings test for the unique unescaped tokens.
		{"empty courts", []string{}, "courts must be"},
		{"27 courts (over A-Z cap)", func() []string {
			c := make([]string, 27)
			for i := range c {
				c[i] = string(rune('A' + i%26))
			}
			return c
		}(), "courts must be"},
		{"multi-char label", []string{"AA", "B"}, "must be a single character"},
		{"empty label", []string{"A", ""}, "cannot be empty"},
		// Duplicate labels are rejected because the frontend keys per-court
		// rendering and `byCourt` bucketing on the label string, duplicates
		// collapse two courts' matches into one lane and trigger React
		// duplicate-key warnings. The admin UI generates unique A,B,C,...
		// so duplicates only arise via direct API/import callers.
		{"duplicate labels", []string{"A", "A"}, "duplicate court label"},
		{"duplicate labels non-adjacent", []string{"A", "B", "A"}, "duplicate court label"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			os.Remove(filepath.Join(tempDir, "tournament.md"))
			body, _ := json.Marshal(state.Tournament{
				Name:     "Cup",
				Password: "secret",
				Courts:   tc.courts,
			})
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/tournament", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code,
				"POST /tournament with invalid courts must return 400")
			assert.Contains(t, w.Body.String(), tc.wantErrText)
			// Not persisted.
			stored, _ := store.LoadTournament()
			assert.Nil(t, stored, "invalid courts must not land on disk")
		})
	}
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

	// POST /api/competitions (no ID, auto-generated from name)
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

	// DELETE /api/competitions/:id (idempotent, non-existent ID returns 204)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/competitions/not-exists", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// POST /api/competitions/:id/start
	comp = state.Competition{ID: "c1", Status: "setup", Courts: []string{"A"}}
	store.SaveCompetition(&comp)
	store.SaveParticipants("c1", []domain.Player{{Name: "P1"}, {Name: "P2"}})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/c1/start", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// DELETE /api/competitions/:id (already started, rejected; must invalidate first)
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
	// Name must be non-empty post-trim, the handler now rejects
	// empty-after-trim Name with a 400 before reaching the save path,
	// which would mask this 500-from-save-error test.
	comp = state.Competition{ID: "fail", Name: "Save Error Comp"}
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
	// Re-seed c1 first, the test deleted it above (line 907) and the
	// PUT handler now correctly returns 404 for missing competitions
	// (settings-only update, never creates; OpenAPI documents this and
	// pre-fix the handler would silently create via
	// saveCompetitionWithPlayers).
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1", Name: "c1 reseed"}))
	comp = state.Competition{ID: "c1", Name: "Updated c1"}
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
	// PUT /api/competitions/:id (not found, never creates per OpenAPI)
	missing := state.Competition{ID: "never-existed", Name: "Phantom"}
	body, _ = json.Marshal(missing)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/never-existed", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code, "PUT must 404 on missing competition")
	assert.Contains(t, w.Body.String(), "not found")

	// PUT /api/competitions/:id (missing target, but body name collides
	// with an existing competition). Pre-fix the uniqueness check ran
	// BEFORE the existence check, so this returned 400 "name already
	// exists", making it impossible to distinguish "the target doesn't
	// exist" from "the name is taken." Post-fix the existence check
	// runs first, so a missing target is always reported as 404
	// regardless of the body's Name.
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "collision-target", Name: "TakenName"}))
	collision := state.Competition{ID: "missing-id", Name: "TakenName"}
	body, _ = json.Marshal(collision)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/missing-id", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code, "PUT must 404 on missing competition even when body name collides")
	assert.NotContains(t, w.Body.String(), "already exists", "missing-target 404 must not leak name-collision detail")
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

	// POST /api/competitions/:id/participants, dojo is required (matches the
	// single-add path and the documented CSV schema), so the smoke payload
	// supplies one. A blank dojo is rejected with 400; see
	// TestBatchPostBlankDojo_400.
	req, _ = http.NewRequest("POST", "/api/competitions/c1/participants", bytes.NewBufferString(`{"players": [{"name": "P1", "dojo": "Dojo A"}]}`))
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

func TestCheckInHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	comp := state.Competition{ID: "ci-comp"}
	require.NoError(t, store.SaveCompetition(&comp))

	// PUT on unknown competition → 404
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/nonexistent/participants/any-pid/checkin", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// DELETE on unknown competition → 404
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/competitions/nonexistent/participants/any-pid/checkin", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Seed a participant.
	require.NoError(t, store.SaveParticipants("ci-comp", []domain.Player{{Name: "Alice", Dojo: "Suigetsu"}}))
	existing, err := store.LoadParticipants("ci-comp", false)
	require.NoError(t, err)
	aliceID := existing[0].ID

	// PUT on unknown participant → 404
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/ci-comp/participants/nonexistent-pid/checkin", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// PUT (check in) known participant → 200, checkedIn=true in response
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/ci-comp/participants/"+aliceID+"/checkin", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.True(t, resp.CheckedIn, "PUT response must have checkedIn=true")

	// Verify check-in persists to disk.
	reloaded, err := store.LoadParticipants("ci-comp", false)
	require.NoError(t, err)
	require.Len(t, reloaded, 1)
	assert.True(t, reloaded[0].CheckedIn, "check-in must persist to disk")

	// DELETE (undo check-in) known participant → 200, checkedIn=false in response
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/competitions/ci-comp/participants/"+aliceID+"/checkin", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.False(t, resp.CheckedIn, "DELETE response must have checkedIn=false")

	// Verify undo persists to disk.
	reloaded, err = store.LoadParticipants("ci-comp", false)
	require.NoError(t, err)
	require.Len(t, reloaded, 1)
	assert.False(t, reloaded[0].CheckedIn, "undo check-in must persist to disk")

	// DELETE on unknown participant → 404
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/competitions/ci-comp/participants/nonexistent-pid/checkin", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestCheckInPreservedOnRosterReplace(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "ci-pr"}))

	// Seed an initial roster with Alice.
	body, _ := json.Marshal(map[string]any{
		"players": []map[string]string{{"name": "Alice", "dojo": "Dojo A"}},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/ci-pr/participants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Retrieve Alice's ID.
	existing, err := store.LoadParticipants("ci-pr", false)
	require.NoError(t, err)
	require.Len(t, existing, 1)
	aliceID := existing[0].ID
	require.NotEmpty(t, aliceID)

	// Check Alice in via PUT /checkin.
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/ci-pr/participants/"+aliceID+"/checkin", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Replace the roster via POST (Alice still in the list, Bob added).
	body, _ = json.Marshal(map[string]any{
		"players": []map[string]string{
			{"name": "Alice", "dojo": "Dojo A"},
			{"name": "Bob", "dojo": "Dojo B"},
		},
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/ci-pr/participants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	// Alice's check-in must be preserved; Bob (new) must be unchecked.
	reloaded, err := store.LoadParticipants("ci-pr", false)
	require.NoError(t, err)
	require.Len(t, reloaded, 2)
	byName := map[string]domain.Player{}
	for _, p := range reloaded {
		byName[p.Name] = p
	}
	assert.True(t, byName["Alice"].CheckedIn, "Alice's check-in must survive a roster replace")
	assert.False(t, byName["Bob"].CheckedIn, "Bob (newly added) must start unchecked")
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

	// PUT /api/competitions/:id/matches/:mid/score (not found, match doesn't exist → 404)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1/matches/not-exists/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)

	// Verify update
	updatedMatches, _ := store.LoadPoolMatches("c1")
	assert.Equal(t, "A", updatedMatches[0].Winner)

	// PUT /api/competitions/:id/matches/:mid/score (invalid competition, not found → 404)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/not-exists/matches/PoolA-1/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
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

	// GET /api/viewer/tournament (no tournament: 200 with a null body, not a
	// 404, so the SPA bootstrap doesn't log a console error)
	os.Remove(filepath.Join(tempDir, "tournament.md"))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "null", w.Body.String())
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
	require.NoError(t, store.SaveParticipants("c1", []domain.Player{{Name: "P1"}, {Name: "P2"}}))

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

// --- Tournament DurationDays validation ---

func TestTournamentHandlers_DurationDays_Validation(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed a valid tournament so PUT has something to update.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Base Tournament",
		Password: "secret",
		Courts:   []string{"A"},
		Date:     "01-06-2026",
	}))

	tests := []struct {
		name         string
		method       string // "PUT" or "POST"
		durationDays int
		wantStatus   int
		wantErrFrag  string
	}{
		{"PUT valid 1", "PUT", 1, http.StatusOK, ""},
		{"PUT valid 30", "PUT", 30, http.StatusOK, ""},
		{"PUT zero accepted defaults to 1", "PUT", 0, http.StatusOK, ""},
		{"PUT negative rejected", "PUT", -1, http.StatusBadRequest, "durationDays must be between"},
		{"PUT over 30 rejected", "PUT", 31, http.StatusBadRequest, "durationDays must be between"},
		{"POST valid 1", "POST", 1, http.StatusCreated, ""},
		{"POST zero accepted", "POST", 0, http.StatusCreated, ""},
		{"POST negative rejected", "POST", -1, http.StatusBadRequest, "durationDays must be between"},
		{"POST over 30 rejected", "POST", 31, http.StatusBadRequest, "durationDays must be between"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tour := state.Tournament{
				Name:         "Duration Test",
				Password:     "secret",
				Courts:       []string{"A"},
				Date:         "01-06-2026",
				DurationDays: tc.durationDays,
			}
			body, _ := json.Marshal(tour)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(tc.method, "/api/tournament", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equalf(t, tc.wantStatus, w.Code, "method=%s durationDays=%d", tc.method, tc.durationDays)
			if tc.wantErrFrag != "" {
				assert.Contains(t, w.Body.String(), tc.wantErrFrag)
			}
		})
	}
}

func TestTournamentHandlers_DurationDays_PersistsAndLoads(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	tour := state.Tournament{
		Name:         "Multi-day Tournament",
		Password:     "secret",
		Courts:       []string{"A"},
		Date:         "05-06-2026",
		DurationDays: 3,
	}
	body, _ := json.Marshal(tour)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/tournament", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	loaded, err := store.LoadTournament()
	require.NoError(t, err)
	assert.Equal(t, 3, loaded.DurationDays)
	assert.Equal(t, []string{"05-06-2026", "06-06-2026", "07-06-2026"}, loaded.Days())
}

// --- Competition date range validation ---

func TestCompetitionHandlers_DateRangeValidation(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Three-day tournament starting June 5.
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Multi-day",
		Password:     "secret",
		Courts:       []string{"A"},
		Date:         "05-06-2026",
		DurationDays: 3,
	}))

	tests := []struct {
		name        string
		compDate    string
		wantStatus  int
		wantErrFrag string
	}{
		{"Day 1 accepted", "05-06-2026", http.StatusCreated, ""},
		{"Day 2 accepted", "06-06-2026", http.StatusCreated, ""},
		{"Day 3 accepted", "07-06-2026", http.StatusCreated, ""},
		{"before Day 1 rejected", "04-06-2026", http.StatusBadRequest, "date must be one of the tournament days"},
		{"after Day 3 rejected", "08-06-2026", http.StatusBadRequest, "date must be one of the tournament days"},
		{"empty date defaults to Day 1", "", http.StatusCreated, ""},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			comp := state.Competition{
				ID:     fmt.Sprintf("c-range-%d", i),
				Name:   fmt.Sprintf("Range Test %d", i),
				Courts: []string{"A"},
				Date:   tc.compDate,
			}
			body, _ := json.Marshal(comp)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equalf(t, tc.wantStatus, w.Code, "case %q: comp date=%q", tc.name, tc.compDate)
			if tc.wantErrFrag != "" {
				assert.Containsf(t, w.Body.String(), tc.wantErrFrag, "case %q", tc.name)
			}
		})
	}
}

func TestCompetitionHandlers_DateRangeValidation_PUT(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Multi-day",
		Password:     "secret",
		Courts:       []string{"A"},
		Date:         "05-06-2026",
		DurationDays: 2,
	}))

	// Create an initial competition on Day 1.
	initComp := state.Competition{ID: "c-put-range", Name: "Range PUT Test", Courts: []string{"A"}, Date: "05-06-2026"}
	require.NoError(t, store.SaveCompetition(&initComp))

	tests := []struct {
		name        string
		newDate     string
		wantStatus  int
		wantErrFrag string
	}{
		{"Day 1 accepted", "05-06-2026", http.StatusOK, ""},
		{"Day 2 accepted", "06-06-2026", http.StatusOK, ""},
		{"outside range rejected", "07-06-2026", http.StatusBadRequest, "date must be one of the tournament days"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			comp := state.Competition{
				ID:     "c-put-range",
				Name:   "Range PUT Test",
				Courts: []string{"A"},
				Date:   tc.newDate,
			}
			body, _ := json.Marshal(comp)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/c-put-range", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equalf(t, tc.wantStatus, w.Code, "case %q: date=%q", tc.name, tc.newDate)
			if tc.wantErrFrag != "" {
				assert.Contains(t, w.Body.String(), tc.wantErrFrag)
			}
		})
	}
}

func TestCompetitionHandlers_DefaultDate_IsDay1(t *testing.T) {
	// When POST /competitions sends an empty date, the handler should
	// default to the tournament's Day 1 (not today).
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:         "Multi-day",
		Password:     "secret",
		Courts:       []string{"A"},
		Date:         "15-07-2026",
		DurationDays: 3,
	}))

	comp := state.Competition{
		ID:     "c-default-date",
		Name:   "Default Date Test",
		Courts: []string{"A"},
		// Date intentionally empty, should default to tournament Day 1
	}
	body, _ := json.Marshal(comp)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code)

	// Reload and check the date was set to Day 1.
	loaded, err := store.LoadCompetition("c-default-date")
	require.NoError(t, err)
	assert.Equal(t, "15-07-2026", loaded.Date, "empty competition date should default to tournament Day 1")
}

// TestCompleteHandler_NaginataBronzeGate is a regression for the tri-review
// finding: POST /complete must not seal a naginata competition whose 3rd-place
// (bronze) match is unscored (the Awards podium would show an incomplete 3rd).
// The JS "Complete competition" button enforces this via bracketFullyComplete;
// this pins the same guard server-side against a direct API call.
func TestCompleteHandler_NaginataBronzeGate(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "nag-complete-gate"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Nag Complete Gate", Status: state.CompStatusPlayoffs, Naginata: true,
	}))

	post := func() *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/complete", nil)
		r.ServeHTTP(w, req)
		return w
	}
	final := func(status state.MatchStatus) state.BracketMatch {
		return state.BracketMatch{ID: "m-final", SideA: "Alice", SideB: "Bob", Status: status}
	}
	bronze := func(status state.MatchStatus) *state.BracketMatch {
		return &state.BracketMatch{ID: "m-bronze", SideA: "Charlie", SideB: "Dave", Status: status}
	}

	// Completed final but an UNSCORED bronze → blocked (bronze is a required
	// must-play match).
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds:          [][]state.BracketMatch{{final(state.MatchStatusCompleted)}},
		ThirdPlaceMatch: bronze(state.MatchStatusScheduled),
	}))
	w := post()
	assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "all bracket matches")

	// Scored bronze but an UNSCORED final → still blocked (the general gate
	// covers non-bronze matches too; closes the kendo direct-API hole).
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds:          [][]state.BracketMatch{{final(state.MatchStatusScheduled)}},
		ThirdPlaceMatch: bronze(state.MatchStatusCompleted),
	}))
	assert.Equal(t, http.StatusBadRequest, post().Code)

	// Both the final and the bronze completed → completion succeeds.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds:          [][]state.BracketMatch{{final(state.MatchStatusCompleted)}},
		ThirdPlaceMatch: bronze(state.MatchStatusCompleted),
	}))
	w = post()
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}

// TestBracketFullyComplete pins the completion predicate, especially the
// placeholder case behind Copilot #326: a downstream match still holding a
// "Winner of rX-mY" placeholder side (SSE hasn't propagated its real sides yet)
// must count as required-and-incomplete, so the gate can't fire early. Byes
// (one empty side) and Hidden phantoms are excluded.
func TestBracketFullyComplete(t *testing.T) {
	comp := func(id string, a, b string, s state.MatchStatus) state.BracketMatch {
		return state.BracketMatch{ID: id, SideA: a, SideB: b, Status: s}
	}

	t.Run("nil / empty bracket is not complete", func(t *testing.T) {
		assert.False(t, bracketFullyComplete(nil))
		assert.False(t, bracketFullyComplete(&state.Bracket{}))
	})

	t.Run("final still a placeholder blocks completion", func(t *testing.T) {
		b := &state.Bracket{Rounds: [][]state.BracketMatch{
			{comp("m-sf1", "Alice", "Bob", state.MatchStatusCompleted)},
			{comp("m-final", "Winner of r0-m0", "Winner of r0-m1", state.MatchStatusScheduled)},
		}}
		assert.False(t, bracketFullyComplete(b), "placeholder final is required-and-incomplete")
	})

	t.Run("all real matches completed is complete", func(t *testing.T) {
		b := &state.Bracket{Rounds: [][]state.BracketMatch{
			{comp("m-sf1", "Alice", "Bob", state.MatchStatusCompleted)},
			{comp("m-final", "Alice", "Carol", state.MatchStatusCompleted)},
		}}
		assert.True(t, bracketFullyComplete(b))
	})

	t.Run("unscored bronze blocks completion", func(t *testing.T) {
		b := &state.Bracket{
			Rounds:          [][]state.BracketMatch{{comp("m-final", "Alice", "Carol", state.MatchStatusCompleted)}},
			ThirdPlaceMatch: &state.BracketMatch{ID: "m-bronze", SideA: "Bob", SideB: "Dave", Status: state.MatchStatusScheduled},
		}
		assert.False(t, bracketFullyComplete(b))
	})

	t.Run("byes and hidden phantoms are excluded", func(t *testing.T) {
		b := &state.Bracket{Rounds: [][]state.BracketMatch{
			{
				comp("m-final", "Alice", "Bob", state.MatchStatusCompleted),
				comp("m-bye", "Carol", "", state.MatchStatusScheduled),              // bye: one empty side
				{ID: "m-phantom", Hidden: true, Status: state.MatchStatusScheduled}, // hidden phantom
			},
		}}
		assert.True(t, bracketFullyComplete(b), "a scheduled bye/phantom must not block completion")
	})
}

// TestCompleteHandler_BracketLoadErrorFailsClosed pins Copilot #326: completion
// is IRREVERSIBLE, so a genuine bracket I/O/parse fault must fail CLOSED (500),
// not fall through and seal the competition with unknown bracket state. (A
// MISSING bracket.json is not a fault -- LoadBracket maps it to an empty bracket
// + nil error -- so bracketless formats still complete.)
func TestCompleteHandler_BracketLoadErrorFailsClosed(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)

	compID := "complete-bracket-io-fail"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "IO Fail", Status: state.CompStatusPlayoffs,
	}))
	// Corrupt bracket.json so LoadBracket returns a parse error (distinct from
	// os.IsNotExist, which maps to an empty bracket).
	bracketPath := filepath.Join(tempDir, "competitions", compID, "bracket.json")
	require.NoError(t, os.WriteFile(bracketPath, []byte("{ not valid json"), 0o600))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/complete", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}
