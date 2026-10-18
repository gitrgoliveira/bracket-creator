package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Starting a kachinuki match keeps the lineup each side starts with as the
// match's own (engine/kachinuki_opener.go), and that is announced like every
// lineup write, naming the team and the match, so a lineup screen showing the
// match reads it again. A client batch under the server's own event session is
// refused at the door.
func TestKachinukiStart_AnnouncesTheLineupItStartedWith(t *testing.T) {
	dir, err := os.MkdirTemp("", "kachinuki-start-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	const c = "kstart"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: c, Kind: "team", TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 3, Status: state.CompStatusPools}))
	require.NoError(t, store.SavePoolMatches(c, []state.MatchResult{{ID: "Pool A-0", SideA: "Tora", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled}}))
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta", Positions: map[domain.Position]string{domain.PositionNumbered(1): "Aoki"}}, 3))
	hub := NewHub()
	t.Cleanup(hub.Close)
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html></html>")}})
	r, _, limiter := NewRouterWithHub(store, engine.New(store), res, nil, hub, false)
	t.Cleanup(limiter.Close)
	ch := hub.Subscribe()
	defer hub.Unsubscribe(ch)

	path := "/api/competitions/" + c + "/matches/" + url.PathEscape("Pool A-0")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, squadJSONReq(http.MethodPut, path+"/score", "secret", map[string]any{
		"sideA": "Tora", "sideB": "Washi", "sideAId": "ta", "sideBId": "tb", "status": "running", "startOnly": true,
		"ipponsA": []string{}, "ipponsB": []string{}, "changed": []string{state.GroupResult}, "modifiedAt": time.Now().UnixMilli() - 1000,
	}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	announced := map[string]bool{}
	for done := false; !done; {
		select {
		case msg := <-ch:
			evt := decodeHubEvent(t, msg)
			if evt.Type != EventLineupUpdated {
				continue
			}
			data, _ := evt.Data.(map[string]any)
			if data["matchId"] == "Pool A-0" {
				announced[data["teamId"].(string)] = true
			}
		default:
			done = true
		}
	}
	assert.Equal(t, map[string]bool{"ta": true, "tb": true}, announced, "both sides' start lineups are announced")

	w = httptest.NewRecorder()
	r.ServeHTTP(w, squadJSONReq(http.MethodPost, path+"/score-events", "secret", map[string]any{
		"session": engine.ServerEventSession,
		"events":  []domain.ScoreEvent{{ID: "spoof", Stamp: time.Now().UnixMilli() - 500, Kind: domain.ScoreBoutFighter, Pos: 1, Side: "a", Name: "Zed"}},
	}))
	assert.Equal(t, http.StatusBadRequest, w.Code, "no client may send the server's own events")
	assert.Contains(t, w.Body.String(), "server's own")
}
