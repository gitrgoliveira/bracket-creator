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

// courtCurrentSide mirrors the per-side payload the
// GET /api/viewer/court/:court/current handler builds via buildSide (see
// handlers_display.go; documented in specs/openapi.yaml). Kept local to the
// test file because the handler assembles the JSON inline, there is no
// exported production response type to assert against.
type courtCurrentSide struct {
	PlayerID    string `json:"playerId"`
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
	Dojo        string `json:"dojo"`
	Number      string `json:"number"`
}

// courtCurrentResponse mirrors the full current/idle response shape from the
// contract. Fields that don't appear on the idle branch are zero-valued
// after json.Unmarshal, the assertions read only what each test cares
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

// TestCourtCurrentReturnsCurrentPayload, T052
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
		Name:   "Individual, Kyu Division",
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

// TestCourtCurrentReturnsRunningBracketMatch, mp-9h1f follow-up. A running
// KNOCKOUT (bracket) bout must surface as the court's current match; the prior
// handler scanned only poolMatches, so an elimination bout read as idle. The
// bracket persists its running score as the formatted ScoreA/ScoreB string, so
// the handler parses it back into ippon/hansoku via parseScore.
func TestCourtCurrentReturnsRunningBracketMatch(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Test Tournament", Password: "secret", Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "ko", Name: "Knockout", Status: state.CompStatusPlayoffs, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants("ko", []domain.Player{
		{Name: "Aoi Mori", DisplayName: "Aoi Mori", Dojo: "North"},
		{Name: "Ken Sato", DisplayName: "Ken Sato", Dojo: "South"},
	}))
	require.NoError(t, store.SaveBracket("ko", &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{
				ID:     "m-r1-0",
				SideA:  "Aoi Mori",
				SideB:  "Ken Sato",
				Status: state.MatchStatusRunning,
				Court:  "A",
				ScoreA: "MK (H1)", // two ippons + one hansoku
				ScoreB: "D",       // one ippon
			},
		}},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body=%q", w.Body.String())

	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%q", w.Body.String())
	assert.Equal(t, "current", resp.Status, "running bracket match must be 'current', not idle")
	require.NotNil(t, resp.Competition)
	assert.Equal(t, "ko", resp.Competition.ID)
	require.NotNil(t, resp.SideA)
	require.NotNil(t, resp.SideB)
	assert.Equal(t, "Aoi Mori", resp.SideA.Name)
	assert.Equal(t, "Ken Sato", resp.SideB.Name)
	// ScoreA/ScoreB parsed back to ippon arrays + hansoku counts.
	assert.Equal(t, []string{"M", "K"}, resp.IpponsA)
	assert.Equal(t, 1, resp.HansokuA)
	assert.Equal(t, []string{"D"}, resp.IpponsB)
	assert.Equal(t, 0, resp.HansokuB)
}

// TestCourtCurrentEmptyIpponsAreArraysNotNull, Copilot review. An unscored
// match (here a bracket bout with only a hansoku and no ippons) must encode
// ipponsA/ipponsB as [] on the wire, not null, the contract models them as
// arrays and overlay clients assume []. Asserted on the raw body because the
// test struct's omitempty can't distinguish [] from null.
func TestCourtCurrentEmptyIpponsAreArraysNotNull(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "secret", Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "ko", Name: "Knockout", Status: state.CompStatusPlayoffs, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveBracket("ko", &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{
				ID: "m-r1-0", SideA: "Aoi", SideB: "Ken",
				Status: state.MatchStatusRunning, Court: "A",
				ScoreA: "(H2)", // hansoku only, no ippons → parseScore returns nil
				ScoreB: "",     // nothing scored yet → nil
			},
		}},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	body := w.Body.String()
	assert.Contains(t, body, `"ipponsA":[]`, "nil ippons must encode as [] not null: %q", body)
	assert.Contains(t, body, `"ipponsB":[]`, "nil ippons must encode as [] not null: %q", body)
	assert.NotContains(t, body, `"ipponsA":null`)
	assert.NotContains(t, body, `"ipponsB":null`)
}

// TestParseScore covers the inverse of engine.formatScore used by the bracket
// branch of the current-match handler.
func TestParseScore(t *testing.T) {
	tests := []struct {
		in      string
		ippons  []string
		hansoku int
	}{
		{"", nil, 0},
		{"MK", []string{"M", "K"}, 0},
		{"MK (H1)", []string{"M", "K"}, 1},
		{"(H2)", nil, 2},
		{"D (H1)", []string{"D"}, 1},
		{"  M K  ", []string{"M", "K"}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			ippons, hansoku := parseScore(tc.in)
			assert.Equal(t, tc.ippons, ippons)
			assert.Equal(t, tc.hansoku, hansoku)
		})
	}
}

// TestCourtCurrentSurfacesRepPlayersForDaihyosen, mp-62vr.
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
		Name:   "Team, Pool",
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

// TestCourtCurrentReturnsIdleWhenNoCurrent, T053
// Tournament with Court A but no running match, assert
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

// TestCourtCurrentReturns404ForUnknownCourt, T054
// Tournament has Courts A,B; GET court Z, assert 404 +
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

// TestCourtCurrentRespectsZekkenName, T055
// Competition has withZekkenName=true; GET, assert sideA.displayName
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

// TestCourtCurrentReturns503WhenNoTournament, T055a
// No tournament loaded; GET, assert 503 + error=="no_active_tournament".
func TestCourtCurrentReturns503WhenNoTournament(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Intentionally do NOT save a tournament, setupTestRouter starts
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

// --- Court → matches feed (operator-console data source) ---

// courtMatchesResponse mirrors GET /api/viewer/court/:court/matches. Each entry
// is the same {config, poolMatches, bracket} per-competition payload the
// aggregate GET /competitions returns, scoped to comps with a match on the court.
type courtMatchesResponse struct {
	Court        string `json:"court"`
	Competitions []struct {
		Config struct {
			ID     string `json:"id"`
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"config"`
		PoolMatches []struct {
			ID    string `json:"id"`
			Court string `json:"court"`
			SideA string `json:"sideA"`
			SideB string `json:"sideB"`
		} `json:"poolMatches"`
		Bracket *struct {
			Rounds [][]struct {
				Court string `json:"court"`
			} `json:"rounds"`
			Preview bool `json:"preview"`
		} `json:"bracket"`
	} `json:"competitions"`
	Error string `json:"error,omitempty"`
}

func getCourtMatches(t *testing.T, r http.Handler, court string) (*httptest.ResponseRecorder, courtMatchesResponse) {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/"+court+"/matches", nil)
	r.ServeHTTP(w, req)
	var resp courtMatchesResponse
	if w.Body.Len() > 0 {
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp),
			"response body must be valid JSON: %q", w.Body.String())
	}
	return w, resp
}

func compIDs(resp courtMatchesResponse) []string {
	out := make([]string, 0, len(resp.Competitions))
	for _, c := range resp.Competitions {
		out = append(out, c.Config.ID)
	}
	return out
}

// TestCourtMatches_ListsCompsWithRealMatchOnCourt, a comp with a real pool
// match on the court appears (with its full per-comp payload); a comp whose
// match is on another court does not.
func TestCourtMatches_ListsCompsWithRealMatchOnCourt(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A", "B"},
	}))

	onA := state.Competition{ID: "on-a", Name: "Comp On A", Status: state.CompStatusPools, Courts: []string{"A"}}
	require.NoError(t, store.SaveCompetition(&onA))
	require.NoError(t, store.SavePoolMatches("on-a", []state.MatchResult{
		{ID: "PoolA-1", SideA: "P1", SideB: "P2", Status: state.MatchStatusScheduled, Court: "A"},
	}))

	onB := state.Competition{ID: "on-b", Name: "Comp On B", Status: state.CompStatusPools, Courts: []string{"B"}}
	require.NoError(t, store.SaveCompetition(&onB))
	require.NoError(t, store.SavePoolMatches("on-b", []state.MatchResult{
		{ID: "PoolA-1", SideA: "P3", SideB: "P4", Status: state.MatchStatusScheduled, Court: "B"},
	}))

	w, resp := getCourtMatches(t, r, "A")
	require.Equal(t, http.StatusOK, w.Code, "body=%q", w.Body.String())
	require.Equal(t, []string{"on-a"}, compIDs(resp), "only the comp with a match on A should appear")
	assert.Equal(t, "Comp On A", resp.Competitions[0].Config.Name)
	assert.Equal(t, "pools", resp.Competitions[0].Config.Status)
	require.Len(t, resp.Competitions[0].PoolMatches, 1, "the comp's match data must be included for the queue")
	assert.Equal(t, "A", resp.Competitions[0].PoolMatches[0].Court)
}

// TestCourtMatches_ReturnsFullCompMatchDataNotCourtFiltered, the comp's payload
// includes ALL its matches (both courts), not just the requested court, so
// client-side derivations like pool "Match N of M" counts stay correct.
func TestCourtMatches_ReturnsFullCompMatchDataNotCourtFiltered(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A", "B"},
	}))

	comp := state.Competition{ID: "spread", Name: "Spread", Status: state.CompStatusPools, Courts: []string{"A", "B"}}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SavePoolMatches("spread", []state.MatchResult{
		{ID: "PoolA-0", SideA: "P1", SideB: "P2", Status: state.MatchStatusScheduled, Court: "A"},
		{ID: "PoolA-1", SideA: "P1", SideB: "P3", Status: state.MatchStatusScheduled, Court: "B"},
	}))

	_, resp := getCourtMatches(t, r, "A")
	require.Equal(t, []string{"spread"}, compIDs(resp))
	require.Len(t, resp.Competitions[0].PoolMatches, 2,
		"full comp match data (both courts) must be returned so pool counts stay correct")
}

// TestCourtMatches_FollowsMovedMatchNotConfig, the load-bearing case: a comp
// configured for court A whose match was MOVED to court B appears under B
// (actual placement), not A (config).
func TestCourtMatches_FollowsMovedMatchNotConfig(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A", "B"},
	}))

	comp := state.Competition{ID: "moved", Name: "Moved Comp", Status: state.CompStatusPools, Courts: []string{"A"}}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SavePoolMatches("moved", []state.MatchResult{
		{ID: "PoolA-1", SideA: "P1", SideB: "P2", Status: state.MatchStatusScheduled, Court: "B"},
	}))

	_, onA := getCourtMatches(t, r, "A")
	assert.Empty(t, onA.Competitions, "config-court A must NOT list the comp once its match moved to B")

	_, onB := getCourtMatches(t, r, "B")
	assert.Equal(t, []string{"moved"}, compIDs(onB), "actual-court B must list the comp whose match was moved there")
}

// TestCourtMatches_IncludesBracketMatch, a comp whose only match on the court
// is a (non-preview) bracket match appears, with its bracket payload.
func TestCourtMatches_IncludesBracketMatch(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A"},
	}))
	comp := state.Competition{ID: "ko", Name: "Knockout", Status: state.CompStatusPlayoffs, Courts: []string{"A"}}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveBracket("ko", &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "r0-m0", SideA: "P1", SideB: "P2", Status: state.MatchStatusScheduled, Court: "A"}},
		},
	}))

	w, resp := getCourtMatches(t, r, "A")
	require.Equal(t, http.StatusOK, w.Code)
	require.Equal(t, []string{"ko"}, compIDs(resp))
	require.NotNil(t, resp.Competitions[0].Bracket, "bracket payload must be present")
	require.Len(t, resp.Competitions[0].Bracket.Rounds, 1)
}

// TestCourtMatches_ExcludesPlaceholderOnlyAndPreviewAndSetup, comps whose only
// court match is an unresolved placeholder, a preview bracket, or that are still
// in setup, do NOT appear.
func TestCourtMatches_ExcludesPlaceholderOnlyAndPreviewAndSetup(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A"},
	}))

	ph := state.Competition{ID: "ph", Name: "Placeholder", Status: state.CompStatusPlayoffs, Courts: []string{"A"}}
	require.NoError(t, store.SaveCompetition(&ph))
	require.NoError(t, store.SaveBracket("ph", &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "r0-m0", SideA: "Winner of r1-m0", SideB: "Pool A-1st", Status: state.MatchStatusScheduled, Court: "A"}},
		},
	}))

	pv := state.Competition{ID: "pv", Name: "Preview", Status: state.CompStatusPools, Courts: []string{"A"}}
	require.NoError(t, store.SaveCompetition(&pv))
	require.NoError(t, store.SaveBracket("pv", &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{
			{{ID: "r0-m0", SideA: "P1", SideB: "P2", Status: state.MatchStatusScheduled, Court: "A"}},
		},
	}))

	su := state.Competition{ID: "su", Name: "Setup", Status: state.CompStatusSetup, Courts: []string{"A"}}
	require.NoError(t, store.SaveCompetition(&su))
	require.NoError(t, store.SavePoolMatches("su", []state.MatchResult{
		{ID: "PoolA-1", SideA: "P1", SideB: "P2", Status: state.MatchStatusScheduled, Court: "A"},
	}))

	w, resp := getCourtMatches(t, r, "A")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, resp.Competitions,
		"placeholder-only, preview-bracket, and setup comps must all be excluded; got %v", compIDs(resp))
}

// TestCourtMatches_UnknownCourtAndNoTournament, 404 for an unknown court, 503
// when no tournament is loaded (same contract as /court/:court/current).
func TestCourtMatches_UnknownCourtAndNoTournament(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	w503, resp503 := getCourtMatches(t, r, "A")
	require.Equal(t, http.StatusServiceUnavailable, w503.Code)
	assert.Equal(t, "no_active_tournament", resp503.Error)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A", "B"},
	}))

	w404, resp404 := getCourtMatches(t, r, "Z")
	require.Equal(t, http.StatusNotFound, w404.Code)
	assert.Equal(t, "court_not_found", resp404.Error)
	assert.Equal(t, "Z", resp404.Court)
}

// TestCourtCurrent_ThirdPlaceMatchShownAsCurrent is a Finding 1 regression
// test: when the Naginata bronze match (bracket.ThirdPlaceMatch, ID="m-bronze")
// is Running on a court, GET /court/:court/current must return "current" status
// for that court, not "idle". Previously the Rounds loop skipped ThirdPlaceMatch.
func TestCourtCurrent_ThirdPlaceMatchShownAsCurrent(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "nagi",
		Name:     "Naginata",
		Status:   state.CompStatusPlayoffs,
		Courts:   []string{"A"},
		Naginata: true,
	}))
	// All regular rounds completed; only the bronze match is running.
	require.NoError(t, store.SaveBracket("nagi", &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-sf1", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Court: "A", Winner: "Alice"}},
			{{ID: "m-final", SideA: "Alice", SideB: "Charlie", Status: state.MatchStatusRunning, Court: "A", ScoreA: "M"}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:     "m-bronze",
			SideA:  "Bob",
			SideB:  "Dave",
			Status: state.MatchStatusRunning,
			Court:  "A",
			ScoreA: "K",
			ScoreB: "",
		},
	}))

	// Court A has two running matches (final + bronze). The handler returns
	// the first one it finds. Regardless of which is returned, status must
	// not be "idle" and the response must decode cleanly.
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body=%q", w.Body.String())

	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%q", w.Body.String())
	assert.NotEqual(t, "idle", resp.Status,
		"a running ThirdPlaceMatch on court A must not report idle")
}

// TestCourtCurrent_ThirdPlaceMatchOnlyOnCourt is a Finding 1 regression test
// where ONLY the ThirdPlaceMatch is running and the Rounds loop finds nothing.
// The court must still appear as "current" rather than "idle".
func TestCourtCurrent_ThirdPlaceMatchOnlyOnCourt(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Password: "", Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "nagi2",
		Name:     "Naginata2",
		Status:   state.CompStatusPlayoffs,
		Courts:   []string{"A"},
		Naginata: true,
	}))
	require.NoError(t, store.SaveBracket("nagi2", &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-final", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Court: "A", Winner: "Alice"}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:     "m-bronze",
			SideA:  "Carol",
			SideB:  "Dave",
			Status: state.MatchStatusRunning,
			Court:  "A",
		},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/court/A/current", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, "body=%q", w.Body.String())

	var resp courtCurrentResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp), "body=%q", w.Body.String())
	assert.Equal(t, "current", resp.Status,
		"court with only ThirdPlaceMatch running must return 'current', not 'idle'")
	require.NotNil(t, resp.SideA)
	require.NotNil(t, resp.SideB)
	assert.Equal(t, "Carol", resp.SideA.Name)
	assert.Equal(t, "Dave", resp.SideB.Name)
}

// TestMatchesPresentOnCourt_ThirdPlaceMatch is a Finding 4 regression test:
// matchesPresentOnCourt must return true when only ThirdPlaceMatch has real
// sides on the court, so a bronze-only court is not excluded from the court feed.
func TestMatchesPresentOnCourt_ThirdPlaceMatch(t *testing.T) {
	// No pool matches; only a ThirdPlaceMatch on court A with real sides.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			// Final completed, not on court A.
			{{ID: "m-final", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Court: "B"}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:    "m-bronze",
			SideA: "Carol",
			SideB: "Dave",
			Court: "A",
		},
	}
	got := matchesPresentOnCourt(nil, bracket, "A")
	assert.True(t, got, "matchesPresentOnCourt must return true when ThirdPlaceMatch has real sides on court A")

	// Court B (the final) must still be detected.
	gotB := matchesPresentOnCourt(nil, bracket, "B")
	assert.True(t, gotB, "court B (final match) must still be detected")

	// Court C (no match at all) must return false.
	gotC := matchesPresentOnCourt(nil, bracket, "C")
	assert.False(t, gotC, "court C with no match must return false")

	// Placeholder sides (winner-of strings) must NOT count.
	bracketPlaceholder := &state.Bracket{
		ThirdPlaceMatch: &state.BracketMatch{
			ID:    "m-bronze",
			SideA: "Winner of r2-m1",
			SideB: "Winner of r2-m2",
			Court: "A",
		},
	}
	gotPlaceholder := matchesPresentOnCourt(nil, bracketPlaceholder, "A")
	assert.False(t, gotPlaceholder,
		"ThirdPlaceMatch with placeholder sides must not count as a real match on court")
}
