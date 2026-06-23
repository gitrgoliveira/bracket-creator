package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// courtCurrentSide mirrors the per-side payload defined in
// contracts/api-viewer-court-current.md. Kept local to the test file
// because the production response type does not exist yet — the Red
// state for T052/T053/T055 is that the route is not registered, so
// the JSON body assertions on this struct will fail because Gin's
// default no-route returns plain text.
type courtCurrentSide struct {
	PlayerID    string `json:"playerId"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Dojo        string `json:"dojo"`
	Number      string `json:"number"`
}

// courtCurrentResponse mirrors the full current/idle response shape from the
// contract. Fields that don't appear on the idle branch are zero-valued
// after json.Unmarshal — the assertions read only what each test cares
// about.
type courtCurrentResponse struct {
	Court       string `json:"court"`
	Status      string `json:"status"`
	Competition *struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"competition,omitempty"`
	Phase      string            `json:"phase,omitempty"`
	SideA      *courtCurrentSide `json:"sideA,omitempty"`
	SideB      *courtCurrentSide `json:"sideB,omitempty"`
	IpponsA    []string          `json:"ipponsA,omitempty"`
	IpponsB    []string          `json:"ipponsB,omitempty"`
	HansokuA   int               `json:"hansokuA,omitempty"`
	HansokuB   int               `json:"hansokuB,omitempty"`
	RepPlayerA string            `json:"repPlayerA,omitempty"`
	RepPlayerB string            `json:"repPlayerB,omitempty"`
	Error      string            `json:"error,omitempty"`
}

// TestCourtCurrentReturnsCurrentPayload — T052
// Setup a tournament with Court A, save a competition, place a match in
// "running" status on Court A, then GET /api/viewer/court/A/current and
// assert the contract payload (200 + status=="current" + sides + counts).
func TestCourtCurrentReturnsCurrentPayload(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test Tournament",
		Password: "secret",
		Courts:   []string{"A", "B"},
	}))

	comp := state.Competition{
		ID:     "kyu-individual",
		Name:   "Individual — Kyu Division",
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("kyu-individual", []domain.Player{
		{Name: "Takeshi Yamada", DisplayName: "Takeshi Yamada", Dojo: "Nakano Kendo Club"},
		{Name: "Ichiro Tanaka", DisplayName: "Ichiro Tanaka", Dojo: "Setagaya Dojo"},
	}))
	require.NoError(t, store.SavePoolMatches("kyu-individual", []state.MatchResult{
		{
			ID:       "PoolA-1",
			SideA:    "Takeshi Yamada",
			SideB:    "Ichiro Tanaka",
			Status:   state.MatchStatusRunning,
			Court:    "A",
			IpponsA:  []string{"M"},
			IpponsB:  nil,
			HansokuA: 0,
			HansokuB: 1,
		},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"expected 200 for current match on Court A; got %d body=%q", w.Code, w.Body.String())

	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp),
		"response body must be valid JSON: %q", w.Body.String())
	assert.Equal(t, "current", resp.Status, "status field must be 'current' when a match is running")
	assert.Equal(t, "A", resp.Court, "court must be normalized to uppercase 'A'")
	require.NotNil(t, resp.Competition, "competition object must be present on current payload")
	assert.Equal(t, "kyu-individual", resp.Competition.ID)
	assert.NotEmpty(t, resp.Phase, "phase field must be populated (pool name or round label)")
	require.NotNil(t, resp.SideA, "sideA must be present on current payload")
	require.NotNil(t, resp.SideB, "sideB must be present on current payload")
	assert.Equal(t, "Takeshi Yamada", resp.SideA.Name)
	assert.Equal(t, "Ichiro Tanaka", resp.SideB.Name)
}

// TestCourtCurrentSurfacesRepPlayersForDaihyosen — mp-62vr.
// A pool daihyosen bout carries TEAM names in sideA/sideB; the representative
// fighter for each side lives in repPlayerA/repPlayerB. The polled OBS/vMix
// endpoint must forward those names or the overlay can't show who is fighting.
func TestCourtCurrentSurfacesRepPlayersForDaihyosen(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test Tournament",
		Password: "secret",
		Courts:   []string{"A"},
	}))

	comp := state.Competition{
		ID:     "team-pool",
		Name:   "Team — Pool",
		Status: state.CompStatusPools,
		Courts: []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SavePoolMatches("team-pool", []state.MatchResult{
		{
			ID:         "Pool A-DH-0",
			SideA:      "Nakano Kendo Club",
			SideB:      "Setagaya Dojo",
			Status:     state.MatchStatusRunning,
			Court:      "A",
			IpponsA:    []string{"M"},
			RepPlayerA: "Takeshi Yamada",
			RepPlayerB: "Ichiro Tanaka",
		},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"expected 200 for current DH match; got %d body=%q", w.Code, w.Body.String())

	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp),
		"response body must be valid JSON: %q", w.Body.String())
	assert.Equal(t, "current", resp.Status)
	assert.Equal(t, "Takeshi Yamada", resp.RepPlayerA,
		"rep-player A must be forwarded for a pool daihyosen bout")
	assert.Equal(t, "Ichiro Tanaka", resp.RepPlayerB,
		"rep-player B must be forwarded for a pool daihyosen bout")
}

// TestCourtCurrentReturnsIdleWhenNoCurrent — T053
// Tournament with Court A but no running match — assert
// 200 + {court:"A", status:"idle"}.
func TestCourtCurrentReturnsIdleWhenNoCurrent(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test Tournament",
		Password: "secret",
		Courts:   []string{"A", "B"},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"expected 200 for idle court A; got %d body=%q", w.Code, w.Body.String())

	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp),
		"response body must be valid JSON: %q", w.Body.String())
	assert.Equal(t, "A", resp.Court)
	assert.Equal(t, "idle", resp.Status, "status must be 'idle' when no current match")
	assert.Nil(t, resp.Competition, "idle payload must not include competition object")
	assert.Nil(t, resp.SideA, "idle payload must not include sideA")
	assert.Nil(t, resp.SideB, "idle payload must not include sideB")
}

// TestCourtCurrentReturns404ForUnknownCourt — T054
// Tournament has Courts A,B; GET court Z — assert 404 +
// error=="court_not_found", court=="Z".
func TestCourtCurrentReturns404ForUnknownCourt(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test Tournament",
		Password: "secret",
		Courts:   []string{"A", "B"},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/Z/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code,
		"expected 404 for unknown court Z; got %d body=%q", w.Code, w.Body.String())

	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp),
		"response body must be valid JSON: %q", w.Body.String())
	assert.Equal(t, "court_not_found", resp.Error,
		"error field must be exactly 'court_not_found' per contract")
	assert.Equal(t, "Z", resp.Court,
		"court field must echo the requested (normalized uppercase) court label")
}

// TestCourtCurrentRespectsZekkenName — T055
// Competition has withZekkenName=true; GET — assert sideA.displayName
// equals the zekken (different from name).
func TestCourtCurrentRespectsZekkenName(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:     "Test Tournament",
		Password: "secret",
		Courts:   []string{"A", "B"},
	}))

	comp := state.Competition{
		ID:             "zekken-comp",
		Name:           "Zekken Test",
		Status:         state.CompStatusPools,
		Courts:         []string{"A"},
		WithZekkenName: true,
	}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("zekken-comp", []domain.Player{
		{Name: "Takeshi Yamada", DisplayName: "Yamada", Dojo: "Nakano Kendo Club"},
		{Name: "Ichiro Tanaka", DisplayName: "Tanaka", Dojo: "Setagaya Dojo"},
	}))
	require.NoError(t, store.SavePoolMatches("zekken-comp", []state.MatchResult{
		{
			ID:     "PoolA-1",
			SideA:  "Takeshi Yamada",
			SideB:  "Ichiro Tanaka",
			Status: state.MatchStatusRunning,
			Court:  "A",
		},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code,
		"expected 200 for current match; got %d body=%q", w.Code, w.Body.String())

	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp),
		"response body must be valid JSON: %q", w.Body.String())
	require.NotNil(t, resp.SideA, "sideA must be present on current payload")
	require.NotNil(t, resp.SideB, "sideB must be present on current payload")
	assert.Equal(t, "Takeshi Yamada", resp.SideA.Name)
	assert.Equal(t, "Yamada", resp.SideA.DisplayName,
		"displayName must equal the zekken when withZekkenName=true")
	assert.NotEqual(t, resp.SideA.Name, resp.SideA.DisplayName,
		"displayName must differ from name when withZekkenName=true")
	assert.Equal(t, "Tanaka", resp.SideB.DisplayName,
		"sideB.displayName must equal the zekken when withZekkenName=true")
}

// TestCourtCurrentReturns503WhenNoTournament — T055a
// No tournament loaded; GET — assert 503 + error=="no_active_tournament".
func TestCourtCurrentReturns503WhenNoTournament(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Intentionally do NOT save a tournament — setupTestRouter starts
	// with an empty Store. The contract requires 503 in this case.

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusServiceUnavailable, w.Code,
		"expected 503 when no tournament loaded; got %d body=%q", w.Code, w.Body.String())

	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp),
		"response body must be valid JSON: %q", w.Body.String())
	assert.Equal(t, "no_active_tournament", resp.Error,
		"error field must be exactly 'no_active_tournament' per contract")
}

// TestPhaseFromMatchID_NoDash verifies that an ID with no dash (or a leading
// dash) returns the full ID unchanged.
func TestPhaseFromMatchID_NoDash(t *testing.T) {
	assert.Equal(t, "B1", phaseFromMatchID("B1"))
	assert.Equal(t, "-leading", phaseFromMatchID("-leading"))
}

// TestPhaseFromMatchID_WithDash verifies that an ID with a mid-string dash
// returns everything before the last dash.
func TestPhaseFromMatchID_WithDash(t *testing.T) {
	assert.Equal(t, "Pool A", phaseFromMatchID("Pool A-0"))
	assert.Equal(t, "R1", phaseFromMatchID("R1-2"))
}
