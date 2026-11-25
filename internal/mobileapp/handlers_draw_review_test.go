package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// drawReviewRig is the real router over a temp store, so the draw review's
// routes are reached through server.go's registration and AuthMiddleware.
type drawReviewRig struct {
	t        *testing.T
	router   *gin.Engine
	hub      *Hub
	store    *state.Store
	password string
}

func newDrawReviewRig(t *testing.T) *drawReviewRig {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	password := "review-pw"
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "Draw Review Taikai", Date: "10-05-2026", Courts: []string{"A", "B"},
		Password: password, DurationDays: 1, Mode: state.TournamentModeOfficiated,
	}))
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html/>")}})
	router, hub, limiter := NewRouter(store, engine.New(store), res, NewFileVerifier(store))
	t.Cleanup(limiter.Close)
	t.Cleanup(hub.Close)
	return &drawReviewRig{t: t, router: router, hub: hub, store: store, password: password}
}

func (g *drawReviewRig) send(method, path string, body any) *httptest.ResponseRecorder {
	g.t.Helper()
	return populatedServeJSON(g.t, g.router, g.password, method, path, body)
}

// create makes a competition through the API and, when draw is true,
// generates its draw.
func (g *drawReviewRig) create(id string, body map[string]any, draw bool) {
	g.t.Helper()
	body["id"] = id
	w := g.send(http.MethodPost, "/api/competitions", body)
	require.Equal(g.t, http.StatusCreated, w.Code, w.Body.String())
	if draw {
		w = g.send(http.MethodPost, "/api/competitions/"+id+"/generate-draw", nil)
		require.Equal(g.t, http.StatusOK, w.Code, w.Body.String())
	}
}

func (g *drawReviewRig) knockout(id string) {
	g.t.Helper()
	g.create(id, map[string]any{
		"name": "Knockout " + id, "kind": "individual", "format": state.CompFormatKnockout,
		"courts": []string{"A", "B"},
		"players": []map[string]any{
			{"name": "Arthur Dick", "dojo": "Omega"},
			{"name": "Lena Park", "dojo": "Omega"},
			{"name": "Mika Sato", "dojo": "Kappa"},
			{"name": "Noor Haddad", "dojo": "Pi"},
		},
	}, true)
}

// slotsOf answers each first-round competitor's place, by id.
func (g *drawReviewRig) slotsOf(compID string) map[string]engine.DrawPlace {
	g.t.Helper()
	b, err := g.store.LoadBracket(compID)
	require.NoError(g.t, err)
	require.NotNil(g.t, b)
	out := map[string]engine.DrawPlace{}
	for _, m := range b.Rounds[0] {
		for _, id := range []string{m.SideAID, m.SideBID} {
			if id == "" {
				continue
			}
			place, ok := engine.LeafSlotOf(b, id)
			require.True(g.t, ok, "%s holds a first-round place", id)
			out[id] = place
		}
	}
	return out
}

// twoInDifferentMatches answers two competitors in different first-round
// matches, so a swap of the two changes both matches.
func (g *drawReviewRig) twoInDifferentMatches(compID string) (a, b string) {
	g.t.Helper()
	bracket, err := g.store.LoadBracket(compID)
	require.NoError(g.t, err)
	require.GreaterOrEqual(g.t, len(bracket.Rounds[0]), 2)
	return bracket.Rounds[0][0].SideAID, bracket.Rounds[0][1].SideAID
}

func swapBody(a string, pa engine.DrawPlace, b string, pb engine.DrawPlace, dryRun bool) map[string]any {
	return map[string]any{
		"a":      map[string]any{"id": a, "place": pa},
		"b":      map[string]any{"id": b, "place": pb},
		"dryRun": dryRun,
	}
}

func decodeBody(t *testing.T, w *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out), w.Body.String())
	return out
}

// requireNoEvent fails when the hub sends anything within a short wait.
func requireNoEvent(t *testing.T, ch chan historyEntry, why string) {
	t.Helper()
	select {
	case msg := <-ch:
		t.Fatalf("%s: got %s", why, decodeHubEvent(t, msg).Type)
	case <-time.After(300 * time.Millisecond):
	}
}

func TestDrawReview_DojoSeparation(t *testing.T) {
	g := newDrawReviewRig(t)
	g.knockout("ko")

	w := g.send(http.MethodGet, "/api/competitions/ko/dojo-separation", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var got struct {
		Meetings []engine.DojoMeeting `json:"meetings"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got.Meetings, 1, "Arthur and Lena are the one pair of dojo-mates")
	assert.Equal(t, "Omega", got.Meetings[0].Dojo)
	assert.NotNil(t, got.Meetings[0].Earliest)

	t.Run("a competition with no draw answers an empty list", func(t *testing.T) {
		g.create("setup", map[string]any{
			"name": "Setup", "kind": "individual", "format": state.CompFormatKnockout, "courts": []string{"A"},
			"players": []map[string]any{{"name": "A1", "dojo": "D"}, {"name": "A2", "dojo": "D"}},
		}, false)
		w := g.send(http.MethodGet, "/api/competitions/setup/dojo-separation", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.JSONEq(t, `{"meetings":[]}`, w.Body.String())
	})

	t.Run("an unknown competition is 404", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, g.send(http.MethodGet, "/api/competitions/nope/dojo-separation", nil).Code)
	})

	t.Run("organiser only", func(t *testing.T) {
		w := populatedServeJSON(t, g.router, "", http.MethodGet, "/api/competitions/ko/dojo-separation", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestDrawReview_SwapSuggestions(t *testing.T) {
	g := newDrawReviewRig(t)
	g.knockout("ko")

	w := g.send(http.MethodGet, "/api/competitions/ko/draw/swap-suggestions", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	body := decodeBody(t, w)
	assert.IsType(t, []any{}, body["suggestions"], "a list, never null")
	assert.IsType(t, []any{}, body["stuck"], "a list, never null")

	t.Run("an unknown competition is 404", func(t *testing.T) {
		assert.Equal(t, http.StatusNotFound, g.send(http.MethodGet, "/api/competitions/nope/draw/swap-suggestions", nil).Code)
	})

	t.Run("organiser only", func(t *testing.T) {
		w := populatedServeJSON(t, g.router, "", http.MethodGet, "/api/competitions/ko/draw/swap-suggestions", nil)
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})
}

func TestDrawSwap_AppliedOnceBroadcastOnce(t *testing.T) {
	g := newDrawReviewRig(t)
	g.knockout("ko")
	a, b := g.twoInDifferentMatches("ko")
	slots := g.slotsOf("ko")
	path := "/api/competitions/ko/draw/swap"

	ch := g.hub.Subscribe()
	defer g.hub.Unsubscribe(ch)

	t.Run("a dry run answers the plan and saves and broadcasts nothing", func(t *testing.T) {
		before, err := g.store.LoadBracket("ko")
		require.NoError(t, err)
		w := g.send(http.MethodPost, path, swapBody(a, slots[a], b, slots[b], true))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		got := decodeBody(t, w)
		assert.Equal(t, true, got["dryRun"])
		assert.Equal(t, false, got["applied"], "a dry run applies nothing")
		assert.Len(t, got["moves"], 2)
		after, err := g.store.LoadBracket("ko")
		require.NoError(t, err)
		assert.Equal(t, before, after, "nothing is saved")
		requireNoEvent(t, ch, "a dry run broadcasts nothing")
	})

	t.Run("the swap applies and broadcasts draw_generated once", func(t *testing.T) {
		w := g.send(http.MethodPost, path, swapBody(a, slots[a], b, slots[b], false))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		got := decodeBody(t, w)
		assert.Equal(t, true, got["applied"])
		assert.Nil(t, got["dryRun"])
		select {
		case msg := <-ch:
			env := decodeHubEvent(t, msg)
			assert.Equal(t, EventDrawGenerated, env.Type)
		case <-time.After(time.Second):
			t.Fatal("no draw_generated after an applied swap")
		}
		requireNoEvent(t, ch, "one broadcast per applied swap")
		after := g.slotsOf("ko")
		assert.Equal(t, slots[b], after[a], "each competitor is at the other's place")
		assert.Equal(t, slots[a], after[b])
	})

	t.Run("a resend answers already_swapped and broadcasts nothing", func(t *testing.T) {
		w := g.send(http.MethodPost, path, swapBody(a, slots[a], b, slots[b], false))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		got := decodeBody(t, w)
		assert.Equal(t, false, got["applied"])
		assert.Equal(t, engine.SwapAlreadySwapped, got["reason"])
		requireNoEvent(t, ch, "already_swapped broadcasts nothing")
	})
}

func TestDrawSwap_Refusals(t *testing.T) {
	g := newDrawReviewRig(t)
	g.knockout("ko")
	a, b := g.twoInDifferentMatches("ko")
	slots := g.slotsOf("ko")
	path := "/api/competitions/ko/draw/swap"

	refused := func(t *testing.T, w *httptest.ResponseRecorder, status int, code string) map[string]any {
		t.Helper()
		require.Equal(t, status, w.Code, w.Body.String())
		got := decodeBody(t, w)
		assert.Equal(t, code, got["error"])
		assert.NotEmpty(t, got["message"], "a sentence the organiser reads")
		return got
	}

	t.Run("an unreadable body is 400 invalid", func(t *testing.T) {
		refused(t, g.send(http.MethodPost, path, "not a swap"), http.StatusBadRequest, engine.SwapInvalid)
	})
	t.Run("a place with both shapes is 400 invalid", func(t *testing.T) {
		zero := 0
		both := engine.DrawPlace{Pool: "Pool A", Position: 1, Slot: &zero}
		refused(t, g.send(http.MethodPost, path, swapBody(a, both, b, slots[b], false)), http.StatusBadRequest, engine.SwapInvalid)
	})
	t.Run("a pool place on a knockout draw is 400 invalid", func(t *testing.T) {
		pool := engine.DrawPlace{Pool: "Pool A", Position: 1}
		refused(t, g.send(http.MethodPost, path, swapBody(a, pool, b, slots[b], false)), http.StatusBadRequest, engine.SwapInvalid)
	})
	t.Run("the same competitor twice is 400 same_competitor", func(t *testing.T) {
		refused(t, g.send(http.MethodPost, path, swapBody(a, slots[a], a, slots[b], false)), http.StatusBadRequest, engine.SwapSameCompetitor)
	})
	t.Run("an id not in the draw is 400 not_in_draw", func(t *testing.T) {
		stranger := "33333333-3333-4333-8333-333333333333"
		refused(t, g.send(http.MethodPost, path, swapBody(a, slots[a], stranger, slots[b], false)), http.StatusBadRequest, engine.SwapNotInDraw)
	})
	t.Run("a place the competitor is not at is 409 draw_changed with the places", func(t *testing.T) {
		var third string
		for id := range slots {
			if id != a && id != b {
				third = id
				break
			}
		}
		got := refused(t, g.send(http.MethodPost, path, swapBody(a, slots[third], b, slots[b], false)), http.StatusConflict, engine.SwapDrawChanged)
		assert.NotNil(t, got["places"], "where the two competitors are now, so the page refetches")
	})
	t.Run("an unknown competition is 404", func(t *testing.T) {
		w := g.send(http.MethodPost, "/api/competitions/nope/draw/swap", swapBody(a, slots[a], b, slots[b], false))
		assert.Equal(t, http.StatusNotFound, w.Code, w.Body.String())
	})
	t.Run("organiser only", func(t *testing.T) {
		w := populatedServeJSON(t, g.router, "", http.MethodPost, path, swapBody(a, slots[a], b, slots[b], false))
		assert.Equal(t, http.StatusUnauthorized, w.Code)
	})

	t.Run("no draw is 409 no_draw", func(t *testing.T) {
		g.create("setup", map[string]any{
			"name": "Setup", "kind": "individual", "format": state.CompFormatKnockout, "courts": []string{"A"},
			"players": []map[string]any{{"name": "A1", "dojo": "D"}, {"name": "A2", "dojo": "E"}},
		}, false)
		refused(t, g.send(http.MethodPost, "/api/competitions/setup/draw/swap", swapBody(a, slots[a], b, slots[b], false)),
			http.StatusConflict, engine.SwapNoDraw)
	})
	t.Run("a league is 409 swap_not_supported", func(t *testing.T) {
		g.create("league", map[string]any{
			"name": "League", "kind": "individual", "format": state.CompFormatLeague, "courts": []string{"A"},
			"players": []map[string]any{{"name": "L1", "dojo": "D"}, {"name": "L2", "dojo": "E"}, {"name": "L3", "dojo": "F"}},
		}, true)
		pool := engine.DrawPlace{Pool: "Pool A", Position: 1}
		refused(t, g.send(http.MethodPost, "/api/competitions/league/draw/swap", swapBody(a, pool, b, pool, false)),
			http.StatusConflict, engine.SwapNotSupported)
	})
	t.Run("a started competition is 409 draw_started", func(t *testing.T) {
		g.knockout("started")
		sa, sb := g.twoInDifferentMatches("started")
		ss := g.slotsOf("started")
		w := g.send(http.MethodPost, "/api/competitions/started/start", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		refused(t, g.send(http.MethodPost, "/api/competitions/started/draw/swap", swapBody(sa, ss[sa], sb, ss[sb], false)),
			http.StatusConflict, engine.SwapDrawStarted)
	})
}

// After a swap, a lineup save for a team on the match it left is refused for
// the organiser too (bc-drev FR6), and the team's lineup moved with it.
func TestDrawSwap_LineupSaveForTheMatchATeamLeftIsRefused(t *testing.T) {
	g := newDrawReviewRig(t)
	g.create("teams", map[string]any{
		"name": "Teams", "kind": "team", "teamSize": 3, "format": state.CompFormatKnockout,
		"courts": []string{"A", "B"},
		"players": []map[string]any{
			{"name": "Aoi Team", "dojo": "Aoi"},
			{"name": "Midori Team", "dojo": "Midori"},
			{"name": "Akane Team", "dojo": "Akane"},
			{"name": "Shiro Team", "dojo": "Shiro"},
		},
	}, true)
	bracket, err := g.store.LoadBracket("teams")
	require.NoError(t, err)
	left := bracket.Rounds[0][0]
	a, b := left.SideAID, bracket.Rounds[0][1].SideAID
	slots := g.slotsOf("teams")
	lineupPath := "/api/competitions/teams/teams/" + a + "/match-lineups/" + left.ID
	lineup := map[string]any{"positions": map[string]string{"1": "Ito"}}

	w := g.send(http.MethodPut, lineupPath, lineup)
	require.Equal(t, http.StatusOK, w.Code, "%s", w.Body.String())

	ch := g.hub.Subscribe()
	defer g.hub.Unsubscribe(ch)
	w = g.send(http.MethodPost, "/api/competitions/teams/draw/swap", swapBody(a, slots[a], b, slots[b], false))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, float64(1), decodeBody(t, w)["lineupsMoved"], "the team's lineup went with it")
	for _, want := range []EventType{EventDrawGenerated, EventLineupUpdated} {
		select {
		case msg := <-ch:
			assert.Equal(t, want, decodeHubEvent(t, msg).Type)
		case <-time.After(time.Second):
			t.Fatalf("no %s after a swap that moved a lineup", want)
		}
	}
	requireNoEvent(t, ch, "one of each per applied swap")

	w = g.send(http.MethodPut, lineupPath, lineup)
	requireRefusal(t, w, http.StatusNotFound, "team_not_in_match",
		"This team is not in this match. Check the score sheet and try again.")
}
