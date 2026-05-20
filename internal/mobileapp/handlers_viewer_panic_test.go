package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViewer_PanicInSpawnedGoroutine_ReturnsHTTP500_DoesNotCrash verifies
// the safeGo wiring in handlers_viewer.go: a panic inside one of the
// spawned goroutines must be converted into a 500 response rather than
// crashing the process. This is the regression test for the unrecovered-
// goroutine vulnerability documented in mp-663.
//
// The viewerLoadCompetition package-level hook lets the test swap in a
// panicking implementation without corrupting on-disk state. All 9
// goroutines in handlers_viewer.go go through safeGo, so this single
// integration test covers the wiring for every call site by transitivity
// of the helper.
func TestViewer_PanicInSpawnedGoroutine_ReturnsHTTP500_DoesNotCrash(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Save a competition so the /competitions handler has something to
	// iterate over — without this, the spawned-goroutine code path is
	// never entered and the test would pass vacuously.
	comp := state.Competition{ID: "c1", Name: "Comp 1"}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("c1", nil))

	// Swap in a panicking loader. Restore on cleanup so other tests in
	// the package see the production implementation.
	original := viewerLoadCompetition
	viewerLoadCompetition = func(_ *state.Store, _ string) (*state.Competition, error) {
		panic("simulated corrupt-state panic in LoadCompetition")
	}
	t.Cleanup(func() { viewerLoadCompetition = original })

	// If safeGo were missing, this request would crash the test binary
	// (`panic: ...` with no recovery → goroutine fatal → process exit).
	// The test surviving past ServeHTTP is itself part of the assertion.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code,
		"handler should convert spawned-goroutine panic into 500, got %d (body: %s)", w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "internal error",
		"500 response should not leak panic details to the client")
	assert.NotContains(t, w.Body.String(), "simulated corrupt-state",
		"500 response must not leak the panic value")
}

// TestViewer_NoPanic_HappyPathStillWorks ensures the safeGo refactor
// didn't break the non-panicking path. A regression here would mean the
// goroutine results aren't being collected correctly.
func TestViewer_NoPanic_HappyPathStillWorks(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	comp := state.Competition{ID: "c1", Name: "Comp 1"}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("c1", nil))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
