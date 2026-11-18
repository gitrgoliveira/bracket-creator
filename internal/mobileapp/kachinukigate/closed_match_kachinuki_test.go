package kachinukigate

// D5 of the closed-match rule (.claude/rules/match-writes.md) for a kachinuki
// encounter: a tap stamped before a finish that its basis does not hold is
// turned away (beforeBaseline) and kept in the history, so the finished bout
// and the team figures stand. It shares the gate's world, so it lives beside it.

import (
	"net/http"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/mobileapp"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func d5Router(t *testing.T) (*gin.Engine, *state.Store, *engine.Engine) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	dir, err := os.MkdirTemp("", "d5-*")
	require.NoError(t, err)
	t.Cleanup(func() { os.RemoveAll(dir) })
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "secret"}))
	eng := engine.New(store)
	hub := mobileapp.NewHub()
	t.Cleanup(hub.Close)
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html></html>")}})
	r, _, limiter := mobileapp.NewRouterWithHub(store, eng, res, nil, hub, false)
	t.Cleanup(limiter.Close)
	return r, store, eng
}

func TestD5_Kachinuki(t *testing.T) {
	r, store, eng := d5Router(t)
	w := newGateWorld(t, r, store, eng, "d5k", false, map[string]int{})
	w.step(opStart)
	_, o := w.tap("dev1", domain.ScoreEvent{ID: "t1", Seq: 1, Stamp: w.base + 400, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"})
	require.Equal(t, domain.ScoreOutcomeApplied, o)
	m := w.stored(gateMatch)
	rec := w.do(http.MethodPut, w.matchPath(gateMatch)+"/score", map[string]any{
		"sideA": m.SideA, "sideB": m.SideB, "sideAId": m.SideAID, "sideBId": m.SideBID,
		"status": "completed", "winner": m.SideA, "winnerId": m.SideAID, "decision": "kachinuki-exhaustion",
		"ipponsA": []string{}, "ipponsB": []string{}, "subResults": m.SubResults,
		"changed": []string{state.GroupResult}, "modifiedAt": time.Now().UnixMilli(), "writeId": "fin",
		"session": "dev1", "basis": map[string]any{"seen": w.logLen(), "own": []string{"t1"}, "seq": 1},
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	before := w.stored(gateMatch)
	require.Equal(t, state.MatchStatusCompleted, before.Status)
	_, late := w.tap("dev2", domain.ScoreEvent{ID: "late", Seq: 1, Stamp: w.base + 450, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "K"})
	got := w.stored(gateMatch)
	assert.Equal(t, domain.ScoreOutcomeBeforeBaseline, late)
	assert.Equal(t, before.SubResults[0].Winner, got.SubResults[0].Winner, "bout 1's winner stands")
	assert.Equal(t, before.TeamResult(), got.TeamResult(), "the team figures stand")
	assert.Equal(t, before.Winner, got.Winner)
}
