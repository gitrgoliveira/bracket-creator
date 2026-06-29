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
	require.Equalf(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

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

// TestSwissGenerateRound_CounterAdvances verifies that SwissCurrentRound
// increments 1→2→3 across successive generate-round calls once each prior
// round is fully completed, pinning the round-tracking contract end-to-end.
func TestSwissGenerateRound_CounterAdvances(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := makeSwissComp(t, store, []string{"P1", "P2", "P3", "P4"}, 3)

	completeCurrentRound := func(t *testing.T) {
		t.Helper()
		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for i := range matches {
			if matches[i].Status != state.MatchStatusCompleted {
				matches[i].Winner = matches[i].SideA
				matches[i].Status = state.MatchStatusCompleted
				matches[i].IpponsA = []string{"M", "M"}
			}
		}
		require.NoError(t, store.SavePoolMatches(compID, matches))
	}

	type roundResp struct {
		Round             int `json:"round"`
		SwissCurrentRound int `json:"swissCurrentRound"`
	}

	for wantRound := 1; wantRound <= 3; wantRound++ {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, adminReqSwiss("POST", fmt.Sprintf("/api/competitions/%s/swiss/generate-round", compID)))
		require.Equalf(t, http.StatusCreated, w.Code, "round %d body=%s", wantRound, w.Body.String())

		var resp roundResp
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Equalf(t, wantRound, resp.Round, "resp.round for iteration %d", wantRound)
		assert.Equalf(t, wantRound, resp.SwissCurrentRound, "resp.swissCurrentRound for iteration %d", wantRound)

		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		assert.Equalf(t, wantRound, comp.SwissCurrentRound, "persisted SwissCurrentRound after round %d", wantRound)

		if wantRound < 3 {
			completeCurrentRound(t)
		}
	}
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
	require.Equalf(t, http.StatusConflict, w2.Code, "body=%s", w2.Body.String())

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
	assert.Equalf(t, http.StatusNotFound, w.Code, "body=%s", w.Body.String())
}

// TestSwissGenerateRound_NonSwissFormat rejects competitions whose
// format is not swiss (the engine returns ValidationError → 400).
func TestSwissGenerateRound_NonSwissFormat(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "pools-comp",
		Name:   "Pools",
		Format: state.CompFormatMixed,
	}))

	w := httptest.NewRecorder()
	r.ServeHTTP(w, adminReqSwiss("POST", "/api/competitions/pools-comp/swiss/generate-round"))
	assert.Equalf(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
}

// TestSwissStandings_Empty returns an empty list for a brand-new
// swiss competition with no matches recorded.
func TestSwissStandings_Empty(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := makeSwissComp(t, store, []string{"P1", "P2", "P3", "P4"}, 2)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, adminReqSwiss("GET", fmt.Sprintf("/api/competitions/%s/swiss/standings", compID)))
	require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

	var standings []state.PlayerStanding
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &standings))
	// 4 participants, no matches: all zeros, ranks 1..4 in stable order.
	require.Len(t, standings, 4)
	for i, s := range standings {
		assert.Equalf(t, 0, s.Wins, "standings[%d] wins should be 0", i)
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
	require.Equalf(t, http.StatusOK, w.Code, "body=%s", w.Body.String())

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
	require.Equalf(t, http.StatusBadRequest, w.Code, "body=%s", w.Body.String())
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
	require.Equalf(t, http.StatusCreated, w.Code, "body=%s", w.Body.String())

	saved, err := store.LoadCompetition("swiss-ok")
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, state.CompFormatSwiss, saved.Format)
	assert.Equal(t, 4, saved.SwissRounds)
}

// TestPublicSwissStandings_NotFound verifies that requesting standings for
// a non-existent competition returns 404.
func TestPublicSwissStandings_NotFound(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	req := httptest.NewRequest(http.MethodGet,
		"/api/competitions/no-such-comp/swiss/standings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestSwissEndToEnd_3Rounds_6Participants verifies the complete, end-to-end
// Swiss competition flow with 6 participants over 3 rounds using the live
// API handlers (T189 / FR-050d / FR-050e).
func TestSwissEndToEnd_3Rounds_6Participants(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// 1. Create a 6-player Swiss competition.
	compID := makeSwissComp(t, store, []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"}, 3)

	// Build a seed map keyed by participant name so winnerOf can pick the
	// better-ranked player without relying on which side (SideA/SideB) the
	// pairing assigns them to. Swiss matches store names in SideA/SideB
	// (see engine.buildSwissMatches), so the map must be name-keyed.
	participants, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	seedByName := make(map[string]int, len(participants))
	for _, p := range participants {
		seedByName[p.Name] = p.Seed
	}
	// winnerOf returns the player with the lower seed number (higher rank).
	// Falls back to SideA when both seeds are absent or equal.
	winnerOf := func(m state.MatchResult) string {
		seedA, seedB := seedByName[m.SideA], seedByName[m.SideB]
		if seedB > 0 && (seedA == 0 || seedB < seedA) {
			return m.SideB
		}
		return m.SideA
	}

	// Start the competition via API.
	wStart := httptest.NewRecorder()
	reqStart, _ := http.NewRequest("POST", fmt.Sprintf("/api/competitions/%s/start", compID), nil)
	r.ServeHTTP(wStart, reqStart)
	require.Equalf(t, http.StatusOK, wStart.Code, "Start competition failed: %s", wStart.Body.String())

	// Helper to GET standings and assert player counts.
	getStandings := func() []state.PlayerStanding {
		t.Helper()
		w := httptest.NewRecorder()
		req := adminReqSwiss("GET", fmt.Sprintf("/api/competitions/%s/swiss/standings", compID))
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var standings []state.PlayerStanding
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &standings))
		return standings
	}

	// Helper to submit score for a match.
	submitScore := func(mid, sideA, sideB, winner string, ipponsA, ipponsB []string) {
		t.Helper()
		result := ScoreRequest{
			SideA:   sideA,
			SideB:   sideB,
			Winner:  winner,
			IpponsA: ipponsA,
			IpponsB: ipponsB,
			Status:  state.MatchStatusCompleted,
		}
		body, err := json.Marshal(result)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", fmt.Sprintf("/api/competitions/%s/matches/%s/score", compID, mid), bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code, "Score PUT failed: %s", w.Body.String())
	}

	// Helper to generate Swiss round via API.
	generateRound := func() []state.MatchResult {
		t.Helper()
		w := httptest.NewRecorder()
		req := adminReqSwiss("POST", fmt.Sprintf("/api/competitions/%s/swiss/generate-round", compID))
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusCreated, w.Code, "Generate round failed: %s", w.Body.String())
		var resp struct {
			Round             int                 `json:"round"`
			Matches           []state.MatchResult `json:"matches"`
			SwissCurrentRound int                 `json:"swissCurrentRound"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return resp.Matches
	}

	// --- ROUND 1 ---
	// Round 1 is auto-generated by StartCompetition (engine generates it
	// at start for Swiss format). Load the pre-generated matches from the
	// store rather than calling generateRound(), that endpoint would
	// return HTTP 409 Conflict because SwissCurrentRound is already 1
	// with incomplete matches.
	r1Matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, r1Matches, 3, "6 participants ⇒ 3 matches in R1 (auto-generated by start)")

	// Verify initially standings have 6 players with 0 wins.
	standingsR0 := getStandings()
	require.Len(t, standingsR0, 6)
	for _, s := range standingsR0 {
		assert.Equal(t, 0, s.Wins)
	}

	// Complete R1 matches: higher seed wins each match.
	for _, m := range r1Matches {
		submitScore(m.ID, m.SideA, m.SideB, winnerOf(m), []string{"M", "K"}, nil)
	}

	// Verify standings after Round 1.
	standingsR1 := getStandings()
	// Top 3 players should have 1 win, bottom 3 should have 0 wins.
	winnersR1 := 0
	losersR1 := 0
	for _, s := range standingsR1 {
		switch s.Wins {
		case 1:
			winnersR1++
		case 0:
			losersR1++
		}
	}
	assert.Equal(t, 3, winnersR1)
	assert.Equal(t, 3, losersR1)

	// --- ROUND 2 ---
	// Generate Round 2.
	r2Matches := generateRound()
	require.Len(t, r2Matches, 3)

	// Complete R2 matches: higher seed wins each match.
	for _, m := range r2Matches {
		submitScore(m.ID, m.SideA, m.SideB, winnerOf(m), []string{"M"}, nil)
	}

	// Verify standings after Round 2.
	standingsR2 := getStandings()
	// High-point pairings mean we have players with 2, 1, and 0 wins.
	// The exact distribution depends on the pairing algorithm (Swiss
	// pairs by win-count groups), but the differentiation is what we're
	// pinning: at least one player at the top (2 wins) and at least one
	// at the bottom (0 wins), otherwise the pairing didn't honor seeds.
	winsTally := map[int]int{}
	for _, s := range standingsR2 {
		winsTally[s.Wins]++
	}
	require.Len(t, standingsR2, 6)
	assert.GreaterOrEqual(t, winsTally[2], 1, "at least one player should have 2 wins after R2")
	assert.GreaterOrEqual(t, winsTally[0], 1, "at least one player should have 0 wins after R2")
	assert.Equal(t, 6, winsTally[2]+winsTally[1]+winsTally[0], "all 6 players should land in {0,1,2}-wins buckets after R2")

	// --- ROUND 3 ---
	// Generate Round 3.
	r3Matches := generateRound()
	require.Len(t, r3Matches, 3)

	// Complete R3 matches: higher seed wins each match.
	for _, m := range r3Matches {
		submitScore(m.ID, m.SideA, m.SideB, winnerOf(m), []string{"M"}, nil)
	}

	// Verify final standings.
	finalStandings := getStandings()
	require.Len(t, finalStandings, 6)
	// Ranks should be 1 to 6.
	for i, s := range finalStandings {
		assert.Equal(t, i+1, s.Rank)
	}

	// 4. Complete competition.
	wComplete := httptest.NewRecorder()
	reqComplete, _ := http.NewRequest("POST", fmt.Sprintf("/api/competitions/%s/complete", compID), nil)
	r.ServeHTTP(wComplete, reqComplete)
	require.Equalf(t, http.StatusOK, wComplete.Code, "Complete competition failed: %s", wComplete.Body.String())

	// Verify comp status is "complete".
	updated, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusComplete, updated.Status)
}
