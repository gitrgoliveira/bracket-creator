package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func TestDrawReview_SwapCandidates(t *testing.T) {
	g := newDrawReviewRig(t)
	g.knockout("ko")
	slots := g.slotsOf("ko")
	pick, _ := g.twoInDifferentMatches("ko")
	path := "/api/competitions/ko/draw/swap-candidates?id=" + pick

	w := g.send(http.MethodGet, path, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := decodeBody(t, w)
	got := body["pick"].(map[string]any)
	assert.Equal(t, pick, got["id"])
	assert.EqualValues(t, *slots[pick].Slot, got["place"].(map[string]any)["slot"], "the pick's place, in the swap request's shape")
	candidates := body["candidates"].([]any)
	require.Len(t, candidates, len(slots)-1, "every other drawn competitor")
	seen := map[string]bool{}
	for _, c := range candidates {
		cand := c.(map[string]any)
		id := cand["id"].(string)
		assert.NotEqual(t, pick, id, "the pick is not a candidate of itself")
		seen[id] = true
		assert.EqualValues(t, *slots[id].Slot, cand["place"].(map[string]any)["slot"])
		assert.Contains(t, []any{engine.SwapVerdictBetter, engine.SwapVerdictWorse, engine.SwapVerdictSame}, cand["verdict"])
		// The dojos the verdict is about: a list for better or worse, absent
		// for the same.
		if cand["verdict"] == engine.SwapVerdictSame {
			assert.NotContains(t, cand, "dojos", id)
		} else {
			dojos, ok := cand["dojos"].([]any)
			require.True(t, ok, "%s names the dojos: %v", id, cand)
			assert.NotEmpty(t, dojos, id)
		}
	}
	assert.Len(t, seen, len(slots)-1)

	refused := func(t *testing.T, w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		require.Equal(t, status, w.Code, w.Body.String())
		got := decodeBody(t, w)
		assert.Equal(t, code, got["error"])
		assert.NotEmpty(t, got["message"], "a sentence the organiser reads")
	}
	t.Run("no id is 400 invalid", func(t *testing.T) {
		refused(t, g.send(http.MethodGet, "/api/competitions/ko/draw/swap-candidates", nil), http.StatusBadRequest, engine.SwapInvalid)
		refused(t, g.send(http.MethodGet, "/api/competitions/ko/draw/swap-candidates?id=%20", nil), http.StatusBadRequest, engine.SwapInvalid)
	})
	t.Run("an id the draw does not hold is 409 not_in_draw", func(t *testing.T) {
		refused(t, g.send(http.MethodGet, "/api/competitions/ko/draw/swap-candidates?id=33333333-3333-4333-8333-333333333333", nil),
			http.StatusConflict, engine.SwapNotInDraw)
	})
	t.Run("an unknown competition is 404", func(t *testing.T) {
		w := g.send(http.MethodGet, "/api/competitions/nope/draw/swap-candidates?id="+pick, nil)
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})
	t.Run("organiser only", func(t *testing.T) {
		w := populatedServeJSON(t, g.router, "", http.MethodGet, path, nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
	t.Run("no draw is 409 no_draw", func(t *testing.T) {
		g.create("setup", map[string]any{
			"name": "Setup", "kind": "individual", "format": state.CompFormatKnockout, "courts": []string{"A"},
			"players": []map[string]any{{"name": "A1", "dojo": "D"}, {"name": "A2", "dojo": "E"}},
		}, false)
		refused(t, g.send(http.MethodGet, "/api/competitions/setup/draw/swap-candidates?id="+pick, nil), http.StatusConflict, engine.SwapNoDraw)
	})
	t.Run("a league is 409 swap_not_supported", func(t *testing.T) {
		g.create("league", map[string]any{
			"name": "League", "kind": "individual", "format": state.CompFormatLeague, "courts": []string{"A"},
			"players": []map[string]any{{"name": "L1", "dojo": "D"}, {"name": "L2", "dojo": "E"}, {"name": "L3", "dojo": "F"}},
		}, true)
		refused(t, g.send(http.MethodGet, "/api/competitions/league/draw/swap-candidates?id="+pick, nil), http.StatusConflict, engine.SwapNotSupported)
	})
	t.Run("a started competition is 409 draw_started", func(t *testing.T) {
		g.knockout("started")
		sa, _ := g.twoInDifferentMatches("started")
		w := g.send(http.MethodPost, "/api/competitions/started/start", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		refused(t, g.send(http.MethodGet, "/api/competitions/started/draw/swap-candidates?id="+sa, nil), http.StatusConflict, engine.SwapDrawStarted)
	})
}

// In self-run the swap candidates stay the organiser's: an anonymous caller
// gets the gate's 401, the organiser the answer.
func TestDrawReview_SwapCandidates_SelfRunIsOrganiserOnly(t *testing.T) {
	g := newDrawReviewRig(t)
	g.knockout("ko")
	tourn, err := g.store.LoadTournament()
	require.NoError(t, err)
	tourn.Mode = state.TournamentModeSelfRun
	require.NoError(t, g.store.SaveTournament(tourn))
	pick, _ := g.twoInDifferentMatches("ko")
	path := "/api/competitions/ko/draw/swap-candidates?id=" + pick

	w := populatedServeJSON(t, g.router, "", http.MethodGet, path, nil)
	assert.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	w = g.send(http.MethodGet, path, nil)
	assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
}
