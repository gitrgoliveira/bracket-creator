package mobileapp

import (
	"bytes"
	"encoding/json"
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
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestRouter(t *testing.T) (*gin.Engine, *state.Store, *engine.Engine, *Hub, string) {
	tempDir, err := os.MkdirTemp("", "mobileapp-test-*")
	require.NoError(t, err)

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

	// Stateless schedule estimator — public, no auth.
	publicAPI := r.Group("/api")
	RegisterScheduleHandlers(publicAPI)

	// Admin API
	admin := r.Group("/api")
	RegisterTournamentHandlers(admin, store, hub)
	RegisterImportHandlers(admin, store, hub)
	RegisterCompetitionHandlers(admin, store, eng, hub)
	RegisterParticipantHandlers(admin, store)
	RegisterMatchHandlers(admin, eng, hub)
	RegisterDecisionHandlers(admin, eng, hub)
	RegisterEligibilityHandlers(admin, store, hub)
	RegisterLineupHandlers(admin, store, store, store)

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
	// comp.Name trim in handlers_competition.go — the CreateTournament
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
	// that can still send padded values — the server-side trim is the
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
			"2026-05-12", // ISO shape — not accepted
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
	// X-Tournament-Password header — exposing every /api/* endpoint
	// unauthenticated. Pin the guard here so a future refactor that
	// drops it surfaces immediately.
	{
		os.Remove(filepath.Join(tempDir, "tournament.md"))
		// Courts populated so the Password check (not Courts validation)
		// is the rejection reason — handler validates Name → Courts →
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

		// PUT with empty Password — should preserve "kept-secret".
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
	// the handler — defense-in-depth for the case where AuthMiddleware
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
}

// TestTournamentHandlers_ConcurrentPUT_PasswordChangeNotLost pins the
// TOCTOU fix in store.UpdateTournamentChanged. Pre-atomic-primitive,
// the PUT handler called LoadTournament + SaveTournamentChanged
// sequentially with no shared lock — two concurrent PUTs (one
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
			assert.Equal(t, http.StatusOK, w.Code, "PUT A (preserve) should succeed; iter %d", i)
		}()
		go func() {
			defer wg.Done()
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/tournament", bytes.NewBuffer(bodyB))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusOK, w.Code, "PUT B (change) should succeed; iter %d", i)
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
		// or Hall B — the test doesn't constrain that (standard
		// last-write-wins for fields both PUTs explicitly set).
		stored, err := store.LoadTournament()
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, newPass, stored.Password,
			"iter %d: B's password-change intent must NEVER be lost — "+
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
// arrival order, the final stored Status is one of {invalid, complete} —
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
		// undefined — they're racing.
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

		// Final stored Status must reflect whichever request won —
		// never the original "pools" (that would mean BOTH writes
		// lost their mutation, which is the pre-fix bug we're
		// proving is closed).
		stored, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Contains(t, []state.CompetitionStatus{state.CompStatusInvalid, state.CompStatusComplete}, stored.Status,
			"iter %d: Status must be Invalid or Complete (got %q — neither write landed?)",
			i, stored.Status)

		os.RemoveAll(tempDir)
	}
}

// TestCompetitionHandlers_ConcurrentPUTUniqueNameRace pins the
// rename-uniqueness atomicity fix. Pre-fix, two concurrent PUTs
// renaming different competitions to the same new name each loaded
// the OTHER comp to do the uniqueness check, saw it still had its
// old name, passed the check, and both landed — leaving two
// competitions on disk with the same Name.
//
// An earlier attempt folded the check into UpdateCompetitionChanged's
// per-comp lock transform — deadlocked AB-BA (each goroutine holds
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
		require.NotNil(t, a, "iter %d: comp-a must still exist", i)
		require.NotNil(t, b, "iter %d: comp-b must still exist", i)
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

// TestCreatePlayoff_RejectsNameCollision pins the cross-file guard
// symmetry fix for POST /competitions/:id/playoffs. Pre-fix, the
// playoff path computed `name = src.Name + " - Playoffs"`,
// slugified to an ID, and called SaveCompetitionChanged directly —
// no uniqueness check. If an admin manually created a competition
// whose name matched the derived playoff name (e.g. a comp named
// "Cup - Playoffs" exists, then create a playoff from a comp named
// "Cup"), SaveCompetitionChanged would silently overwrite the
// existing comp's config — data loss.
//
// Now the playoff save runs under WithCompetitionRenameLock with a
// checkUniqueCompName — same symmetry as POST + PUT /competitions.
// Collision → 400 with "already exists" error; the existing
// competition is untouched.
func TestCreatePlayoff_RejectsNameCollision(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Source competition that, when used to create a playoff, would
	// produce name "Source - Playoffs".
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "source",
		Name:   "Source",
		Format: state.CompFormatPools,
		Status: state.CompStatusPools,
	}))
	// Pre-existing competition with the same name the playoff would
	// derive. Pre-fix, SaveCompetitionChanged would have overwritten
	// this config.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           "manually-created",
		Name:         "Source - Playoffs",
		NumberPrefix: "PRESERVED",
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/source/playoffs", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"POST /playoffs should reject when derived name collides with existing comp")
	assert.Contains(t, w.Body.String(), "already exists",
		"error message should explain the collision")

	// Verify the manually-created comp's config is untouched (the
	// pre-fix bug would have replaced it with the default playoff
	// config, losing the NumberPrefix).
	preserved, err := store.LoadCompetition("manually-created")
	require.NoError(t, err)
	require.NotNil(t, preserved)
	assert.Equal(t, "PRESERVED", preserved.NumberPrefix,
		"existing comp's config must be untouched on playoff-name collision")
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

// TestPlayoff_RejectsDerivedIDCollision pins the Copilot #3 fix: the
// playoff endpoint derived `name = src.Name + " - Playoffs"` and
// `id = slugifyID(name)` and called SaveCompetitionChanged directly.
// If a competition existed with the same slug ID but a different
// name, the playoff save silently overwrote it. Now both ID and name
// uniqueness are checked inside the rename lock.
func TestPlayoff_RejectsDerivedIDCollision(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Source comp; derived playoff ID will be "source-playoffs".
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "source",
		Name:   "Source",
		Format: state.CompFormatPools,
		Status: state.CompStatusPools,
	}))

	// Pre-existing comp with the SAME slug as the derived playoff ID
	// but a DIFFERENT name. checkUniqueCompName would have passed
	// (names differ), then SaveCompetitionChanged would have
	// overwritten this with the new playoff config — data loss.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           "source-playoffs",
		Name:         "Unrelated Cup",
		NumberPrefix: "PRESERVED",
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/source/playoffs", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code,
		"POST /playoffs must reject when derived ID collides with existing comp")
	assert.Contains(t, w.Body.String(), "already exists")

	// Existing comp untouched.
	stored, _ := store.LoadCompetition("source-playoffs")
	require.NotNil(t, stored)
	assert.Equal(t, "Unrelated Cup", stored.Name)
	assert.Equal(t, "PRESERVED", stored.NumberPrefix)
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
// a directory where participants.csv is supposed to be a file — on
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
		"format": "pools",
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

// TestCreatePlayoff_RollbackOnReservedSlotFailure pins the K3 rollback
// for POST /competitions/:id/playoffs. SaveCompetitionChanged commits the
// playoff config inside WithCompetitionRenameLock, then the
// AddReservedSlot loop runs OUTSIDE the lock. If any slot's I/O fails,
// the orphaned playoff config would block the operator's retry with
// "derived playoff ID already exists" — the same shape as the
// POST /competitions rollback above and the import handler's rollback.
//
// Forces deterministic failure by planting a directory at the playoff's
// future participants.csv path (AddReservedSlot writes a placeholder
// participant via saveParticipantsLocked, which encounters EISDIR).
func TestCreatePlayoff_RollbackOnReservedSlotFailure(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Source competition with 3 participants → numPools=1 with default
	// poolSize=3 → totalWinners = 1*2 = 2 (two AddReservedSlot calls).
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "rollback-src",
		Name:   "Rollback Src",
		Format: state.CompFormatPools,
		Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SaveParticipants("rollback-src", []helper.Player{
		{Name: "P1", Dojo: "D"},
		{Name: "P2", Dojo: "D"},
		{Name: "P3", Dojo: "D"},
	}))

	// Plant a directory where the playoff's participants.csv should be
	// a file. Derived playoff ID = slugifyID("Rollback Src - Playoffs")
	// = "rollback-src-playoffs". Created BEFORE the POST so when
	// AddReservedSlot tries to saveParticipantsLocked the placeholder,
	// the path is a directory → EISDIR.
	playoffID := "rollback-src-playoffs"
	participantsAsDir := filepath.Join(tempDir, "competitions", playoffID, "participants.csv")
	require.NoError(t, os.MkdirAll(participantsAsDir, 0700))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/rollback-src/playoffs", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"POST /playoffs must surface AddReservedSlot I/O failure as 500")
	assert.Contains(t, w.Body.String(), "failed to add reserved slot",
		"error message must identify the failing step")

	// Rollback assertion: the orphaned playoff config must be gone so a
	// retry isn't blocked by the ID-collision guard.
	stored, _ := store.LoadCompetition(playoffID)
	assert.Nil(t, stored,
		"rollback must remove the playoff config.md so retry passes the ID-collision guard")
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
		// rendering and `byCourt` bucketing on the label string — duplicates
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

	// POST /api/competitions (no ID — auto-generated from name)
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

	// DELETE /api/competitions/:id (idempotent — non-existent ID returns 204)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("DELETE", "/api/competitions/not-exists", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)

	// POST /api/competitions/:id/start
	comp = state.Competition{ID: "c1", Status: "setup", Courts: []string{"A"}}
	store.SaveCompetition(&comp)
	store.SaveParticipants("c1", []helper.Player{{Name: "P1"}, {Name: "P2"}})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/c1/start", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// DELETE /api/competitions/:id (already started — rejected; must invalidate first)
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
	// Name must be non-empty post-trim — the handler now rejects
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
	// Re-seed c1 first — the test deleted it above (line 907) and the
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
	// PUT /api/competitions/:id (not found — never creates per OpenAPI)
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
	// exists" — making it impossible to distinguish "the target doesn't
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

	// POST /api/competitions/:id/participants
	req, _ = http.NewRequest("POST", "/api/competitions/c1/participants", bytes.NewBufferString(`{"players": [{"name": "P1"}]}`))
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

	// PUT /api/competitions/:id/matches/:mid/score (not found)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/c1/matches/not-exists/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)

	// Verify update
	updatedMatches, _ := store.LoadPoolMatches("c1")
	assert.Equal(t, "A", updatedMatches[0].Winner)

	// PUT /api/competitions/:id/matches/:mid/score (invalid competition)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/not-exists/matches/PoolA-1/score", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
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

	// GET /api/viewer/tournament (not found)
	os.Remove(filepath.Join(tempDir, "tournament.md"))
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/viewer/tournament", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
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
	require.NoError(t, store.SaveParticipants("c1", []helper.Player{{Name: "P1"}, {Name: "P2"}}))

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
