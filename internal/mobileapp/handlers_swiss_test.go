package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adminReqSwiss is a tiny helper that wraps a request through the
// admin-protected swiss endpoints. Tournament password is the empty
// string by default (set on the tournament in setupTestRouter); we
// re-use the existing test plumbing.
func adminReqSwiss(method, path string) *http.Request {
	req, _ := http.NewRequest(method, path, nil)
	// The setupTestRouter wires admin routes WITHOUT auth middleware
	// (see handlers_test.go) so this header is unused; kept for parity
	// with production wire shape.
	req.Header.Set("X-Tournament-Password", "")
	return req
}

// makeSwissComp creates a swiss competition with the given roster
// and persists it. Returns the comp ID.
func makeSwissComp(t *testing.T, store *state.Store, names []string, rounds int) string {
	t.Helper()
	compID := "swiss-handler-test"
	comp := &state.Competition{
		ID:                compID,
		Name:              "Swiss Handler",
		Kind:              "individual",
		Format:            state.CompFormatSwiss,
		SwissRounds:       rounds,
		Courts:            []string{"A", "B"},
		StartTime:         "09:00",
		Status:            state.CompStatusSetup,
		PoolMatchDuration: 3,
	}
	require.NoError(t, store.SaveCompetition(comp))
	players := make([]domain.Player, len(names))
	for i, n := range names {
		players[i] = domain.Player{
			ID:   helper.NewUUID4(),
			Name: n,
			Seed: i + 1,
			Dojo: n + "-Dojo",
		}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	return compID
}

// TestSwissGenerateRound_Success exercises the happy-path round-1
// generation: empty competition with no prior matches, first
// POST /generate-round produces matches and bumps SwissCurrentRound.
func TestSwissGenerateRound_Success(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := makeSwissComp(t, store, []string{"P1", "P2", "P3", "P4"}, 2)

	w := httptest.NewRecorder()
	req := adminReqSwiss("POST", fmt.Sprintf("/api/competitions/%s/swiss/generate-round", compID))
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	var resp struct {
		Round             int                 `json:"round"`
		Matches           []state.MatchResult `json:"matches"`
		SwissCurrentRound int                 `json:"swissCurrentRound"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, 1, resp.Round)
	assert.Equal(t, 1, resp.SwissCurrentRound)
	assert.Len(t, resp.Matches, 2, "4 players ⇒ 2 round-1 matches")

	// Sanity: SwissCurrentRound persisted on the comp config.
	updated, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, 1, updated.SwissCurrentRound)

	// Sanity: matches were persisted to pool-matches.csv.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Len(t, matches, 2)
}

// TestSwissGenerateRound_IncompleteRound returns 409 when the current
// round has un-completed matches (FR-050d).
func TestSwissGenerateRound_IncompleteRound(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := makeSwissComp(t, store, []string{"P1", "P2", "P3", "P4"}, 2)

	// Generate round 1 first.
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, adminReqSwiss("POST", fmt.Sprintf("/api/competitions/%s/swiss/generate-round", compID)))
	require.Equal(t, http.StatusCreated, w1.Code)

	// Attempt to generate round 2 without completing round 1's matches.
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, adminReqSwiss("POST", fmt.Sprintf("/api/competitions/%s/swiss/generate-round", compID)))
	require.Equal(t, http.StatusConflict, w2.Code, "body=%s", w2.Body.String())

	var resp struct {
		Error string `json:"error"`
		Code  string `json:"code"`
		Round int    `json:"round"`
	}
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp))
	assert.Equal(t, "round_incomplete", resp.Code)
	assert.Equal(t, 1, resp.Round)
}

// TestSwissGenerateRound_NotFound returns 404 for an unknown comp.
func TestSwissGenerateRound_NotFound(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, adminReqSwiss("POST", "/api/competitions/missing-comp/swiss/generate-round"))
	assert.Equal(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
}

// TestSwissGenerateRound_NonSwissFormat rejects competitions whose
// format is not swiss (the engine returns ValidationError → 400).
func TestSwissGenerateRound_NonSwissFormat(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "pools-comp",
		Name:   "Pools",
		Format: state.CompFormatPools,
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, adminReqSwiss("POST", "/api/competitions/pools-comp/swiss/generate-round"))
	assert.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
}

// TestSwissStandings_Empty returns an empty list for a brand-new
// swiss competition with no matches recorded.
func TestSwissStandings_Empty(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := makeSwissComp(t, store, []string{"P1", "P2", "P3", "P4"}, 2)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, adminReqSwiss("GET", fmt.Sprintf("/api/competitions/%s/swiss/standings", compID)))
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var standings []state.PlayerStanding
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &standings))
	// 4 participants, no matches: all zeros, ranks 1..4 in stable order.
	require.Len(t, standings, 4)
	for i, s := range standings {
		assert.Equal(t, 0, s.Wins, "standings[%d] wins should be 0", i)
		assert.Equal(t, i+1, s.Rank)
	}
}

// TestSwissStandings_AfterRound1 surfaces wins after round 1
// completes.
func TestSwissStandings_AfterRound1(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := makeSwissComp(t, store, []string{"P1", "P2", "P3", "P4"}, 2)

	// Generate round 1 via the API endpoint.
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, adminReqSwiss("POST", fmt.Sprintf("/api/competitions/%s/swiss/generate-round", compID)))
	require.Equal(t, http.StatusCreated, w1.Code)

	// Mark round-1 matches completed (P1 and P2 win, having higher seeds).
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range matches {
		// SideA in fold pairing is the higher seed (P1/P2 here).
		matches[i].Winner = matches[i].SideA
		matches[i].Status = state.MatchStatusCompleted
		matches[i].IpponsA = []string{"M", "M"}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, adminReqSwiss("GET", fmt.Sprintf("/api/competitions/%s/swiss/standings", compID)))
	require.Equal(t, http.StatusOK, w.Code)

	var standings []state.PlayerStanding
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &standings))
	require.Len(t, standings, 4)

	// Top two ranks should be the winners (1 win each, 2 points each).
	assert.Equal(t, 1, standings[0].Wins)
	assert.Equal(t, 1, standings[1].Wins)
	assert.Equal(t, 0, standings[2].Wins)
	assert.Equal(t, 0, standings[3].Wins)
}

// TestSwissCompetitionGetIncludesSwissFields verifies T189 / FR-050a:
// GET /api/competitions/:id surfaces swissRounds and swissCurrentRound
// in the response JSON.
func TestSwissCompetitionGetIncludesSwissFields(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := makeSwissComp(t, store, []string{"P1", "P2", "P3", "P4"}, 3)
	// Bump the current round so omitempty doesn't drop the field.
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.SwissCurrentRound = 1
	require.NoError(t, store.SaveCompetition(comp))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, adminReqSwiss("GET", fmt.Sprintf("/api/competitions/%s", compID)))
	require.Equal(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.EqualValues(t, 3, resp["swissRounds"])
	assert.EqualValues(t, 1, resp["swissCurrentRound"])
	assert.Equal(t, "swiss", resp["format"])
}

// TestPostCompetition_SwissRequiresRounds verifies T181 / FR-050a:
// POST /api/competitions with format="swiss" and no swissRounds is
// rejected with 400.
func TestPostCompetition_SwissRequiresRounds(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	body := `{"id":"swiss-no-rounds","name":"Swiss No Rounds","kind":"individual","format":"swiss","courts":["A"],"date":"15-05-2026"}`
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
	assert.Contains(t, w.Body.String(), "swissRounds")
}

// TestPostCompetition_SwissAccepted verifies T181: with swissRounds >=
// 1, swiss-format POSTs succeed (no longer 501).
func TestPostCompetition_SwissAccepted(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))

	body := `{"id":"swiss-ok","name":"Swiss OK","kind":"individual","format":"swiss","swissRounds":4,"courts":["A"],"date":"15-05-2026"}`
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	saved, err := store.LoadCompetition("swiss-ok")
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, state.CompFormatSwiss, saved.Format)
	assert.Equal(t, 4, saved.SwissRounds)
}
