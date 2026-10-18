package mobileapp

// D5 of the closed-match rule (.claude/rules/match-writes.md): a finish (or a
// decision) governs every scoring group at its stamp, the ones it names and
// the ones it does not, so a tap stamped BEFORE it that its basis does not hold
// is turned away (beforeBaseline) and kept in the history, whichever arrives
// first. A fought Finish on a running match names `result` alone, so before
// this rule such a tap arriving after the finish was folded into the finished
// match: an individual 1-0 became 1-1 with the winner unmoved, and a team
// bout's winner was cleared with the team figures. A finish that sent no basis
// (an older build's) is taken to have seen every tap stamped before it.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"testing/fstest"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
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
	hub := NewHub()
	t.Cleanup(hub.Close)
	res := resources.NewResources(nil, fstest.MapFS{"web-mobile/index.html": {Data: []byte("<html></html>")}})
	r, _, limiter := NewRouterWithHub(store, eng, res, nil, hub, false)
	t.Cleanup(limiter.Close)
	return r, store, eng
}

// d5Match drives one match through the HTTP doors: a tap, a finish naming
// result alone (with a basis holding that tap, or none), and a tap stamped
// before the finish that arrives after it.
type d5Match struct {
	t     *testing.T
	r     *gin.Engine
	store *state.Store
	comp  string
	path  string
	base  int64
}

func (d d5Match) tap(sess, id string, ev domain.ScoreEvent) string {
	d.t.Helper()
	ev.ID, ev.Session, ev.Seq = id, sess, 1
	w := httptest.NewRecorder()
	d.r.ServeHTTP(w, squadJSONReq(http.MethodPost, d.path+"/score-events", "secret", map[string]any{"session": sess, "events": []domain.ScoreEvent{ev}}))
	require.Equal(d.t, http.StatusOK, w.Code, w.Body.String())
	var out struct {
		Outcomes []scoreEventOutcome `json:"outcomes"`
	}
	require.NoError(d.t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(d.t, out.Outcomes, 1)
	return out.Outcomes[0].Outcome
}

func (d d5Match) stored() state.MatchResult {
	d.t.Helper()
	ms, err := d.store.LoadPoolMatches(d.comp)
	require.NoError(d.t, err)
	return ms[0]
}

func (d d5Match) finish(body map[string]any, withBasis bool, own ...string) {
	d.t.Helper()
	body["status"], body["changed"], body["modifiedAt"], body["writeId"] = "completed", []string{state.GroupResult}, time.Now().UnixMilli(), "fin"
	if withBasis {
		lines, err := d.store.LoadScoreLog(d.comp, "Pool A-0")
		require.NoError(d.t, err)
		body["session"] = "dev1"
		body["basis"] = map[string]any{"seen": len(lines), "own": own, "seq": len(own)}
	}
	w := httptest.NewRecorder()
	d.r.ServeHTTP(w, squadJSONReq(http.MethodPut, d.path+"/score", "secret", body))
	require.Equal(d.t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(d.t, state.MatchStatusCompleted, d.stored().Status, "the finish lands")
}

func d5Setup(t *testing.T, comp *state.Competition, players []domain.Player, m func(ids map[string]string) state.MatchResult) d5Match {
	t.Helper()
	r, store, _ := d5Router(t)
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(comp.ID, players))
	ps, err := store.LoadParticipants(comp.ID, false)
	require.NoError(t, err)
	ids := map[string]string{}
	for _, p := range ps {
		ids[p.Name] = p.ID
	}
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{m(ids)}))
	return d5Match{t: t, r: r, store: store, comp: comp.ID, path: "/api/competitions/" + comp.ID + "/matches/" + url.PathEscape("Pool A-0"), base: time.Now().UnixMilli() - 60_000}
}

func TestD5_Individual(t *testing.T) {
	for _, withBasis := range []bool{true, false} {
		t.Run(fmt.Sprint("basis=", withBasis), func(t *testing.T) {
			d := d5Setup(t, &state.Competition{ID: "d5i", Kind: "individual", Status: state.CompStatusPools},
				[]domain.Player{{Name: "Alice", Dojo: "A"}, {Name: "Bob", Dojo: "B"}},
				func(ids map[string]string) state.MatchResult {
					return state.MatchResult{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: ids["Alice"], SideBID: ids["Bob"], Status: state.MatchStatusRunning, IpponsA: []string{}, IpponsB: []string{}}
				})
			require.Equal(t, domain.ScoreOutcomeApplied, d.tap("dev1", "t1", domain.ScoreEvent{Stamp: d.base + 100, Kind: domain.ScoreIpponAdd, Side: "a", Letter: "M"}))
			m := d.stored()
			d.finish(map[string]any{"sideA": m.SideA, "sideB": m.SideB, "sideAId": m.SideAID, "sideBId": m.SideBID,
				"winner": m.SideA, "winnerId": m.SideAID, "ipponsA": []string{"M"}, "ipponsB": []string{}}, withBasis, "t1")
			late := d.tap("dev2", "late", domain.ScoreEvent{Stamp: d.base + 150, Kind: domain.ScoreIpponAdd, Side: "b", Letter: "K"})
			got := d.stored()
			if withBasis {
				assert.Equal(t, domain.ScoreOutcomeBeforeBaseline, late, "the finish never saw it: turned away")
				assert.Equal(t, []string{"M"}, got.IpponsA)
				assert.Empty(t, got.IpponsB, "the finished 1-0 stays 1-0")
			} else {
				assert.Equal(t, domain.ScoreOutcomeApplied, late, "a finish with no basis saw every tap stamped before it")
			}
			assert.Equal(t, "Alice", got.Winner)
			assert.Equal(t, state.MatchStatusCompleted, got.Status)
		})
	}
}

func TestD5_Engi(t *testing.T) {
	d := d5Setup(t, &state.Competition{ID: "d5e", Kind: "individual", Engi: true, Status: state.CompStatusPools},
		[]domain.Player{{Name: "Alice", Dojo: "A"}, {Name: "Bob", Dojo: "B"}},
		func(ids map[string]string) state.MatchResult {
			return state.MatchResult{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: ids["Alice"], SideBID: ids["Bob"], Status: state.MatchStatusRunning, IpponsA: []string{}, IpponsB: []string{}}
		})
	for i, side := range []string{"a", "a", "b"} {
		require.Equal(t, domain.ScoreOutcomeApplied, d.tap("dev1", fmt.Sprint("t", i), domain.ScoreEvent{Stamp: d.base + int64(100+i), Kind: domain.ScoreFlagAdd, Side: side}))
	}
	m := d.stored()
	d.finish(map[string]any{"sideA": m.SideA, "sideB": m.SideB, "sideAId": m.SideAID, "sideBId": m.SideBID,
		"winner": m.SideA, "winnerId": m.SideAID, "flagsA": 2, "flagsB": 1}, true, "t0", "t1", "t2")
	late := d.tap("dev2", "late", domain.ScoreEvent{Stamp: d.base + 150, Kind: domain.ScoreFlagAdd, Side: "b"})
	got := d.stored()
	assert.Equal(t, domain.ScoreOutcomeBeforeBaseline, late)
	assert.Equal(t, []int{2, 1}, []int{got.FlagsA, got.FlagsB}, "the finished count stands")
	assert.Equal(t, "Alice", got.Winner)
}

func TestD5_FixedOrderTeam(t *testing.T) {
	d := d5Setup(t, &state.Competition{ID: "d5f", Kind: "team", TeamMatchType: state.TeamMatchTypeFixed, TeamSize: 3, Status: state.CompStatusPools},
		[]domain.Player{{Name: "Tora", Dojo: "T"}, {Name: "Washi", Dojo: "W"}},
		func(ids map[string]string) state.MatchResult {
			var rows []state.SubMatchResult
			for i := 1; i <= 3; i++ {
				row := state.SubMatchResult{Position: i, SideA: fmt.Sprint("A", i), SideB: fmt.Sprint("B", i), IpponsA: []string{}, IpponsB: []string{}}
				if i > 1 {
					row.Decision = "hikiwake"
				}
				rows = append(rows, row)
			}
			return state.MatchResult{ID: "Pool A-0", SideA: "Tora", SideB: "Washi", SideAID: ids["Tora"], SideBID: ids["Washi"], Status: state.MatchStatusRunning, SubResults: rows}
		})
	require.Equal(t, domain.ScoreOutcomeApplied, d.tap("dev1", "t1", domain.ScoreEvent{Stamp: d.base + 100, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "a", Letter: "M"}))
	m := d.stored()
	d.finish(map[string]any{"sideA": m.SideA, "sideB": m.SideB, "sideAId": m.SideAID, "sideBId": m.SideBID,
		"winner": m.SideA, "winnerId": m.SideAID, "ipponsA": []string{}, "ipponsB": []string{}, "subResults": m.SubResults}, true, "t1")
	before := d.stored()
	late := d.tap("dev2", "late", domain.ScoreEvent{Stamp: d.base + 150, Kind: domain.ScoreIpponAdd, Pos: 1, Side: "b", Letter: "K"})
	got := d.stored()
	assert.Equal(t, domain.ScoreOutcomeBeforeBaseline, late)
	assert.Equal(t, before.SubResults[0].Winner, got.SubResults[0].Winner, "bout 1's winner stands")
	assert.Equal(t, before.TeamResult(), got.TeamResult(), "the team figures stand")
	assert.Equal(t, "Tora", got.Winner)
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
