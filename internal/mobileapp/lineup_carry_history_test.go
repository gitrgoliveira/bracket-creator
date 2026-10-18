package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/require"
)

// carryEnv is one competition behind the real lineup and match doors, as the
// server wires them (the engine follows every lineup save): team Taka meets
// Kuma (P1-0), Washi (P1-1) and Inu (P1-2), and its starting lineup is Sato
// at position 1 and Ueda at position 2, both changed at base-1000.
type carryEnv struct {
	t       *testing.T
	r       *gin.Engine
	store   *state.Store
	eng     *engine.Engine
	id      map[string]string
	members map[string]string
	base    int64
	comp    string
	writes  int
}

func newCarryEnv(t *testing.T, kachinuki bool) *carryEnv {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Carry", Password: "secret"}))
	eng := engine.New(store)
	hub := NewHub()
	t.Cleanup(hub.Close)
	verifier := NewFileVerifier(store)
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(verifier, store))
	RegisterMatchHandlers(admin, eng, store, store, hub, verifier, store)
	RegisterLineupHandlers(admin, store, store, store, hub, store, verifier, eng)
	const comp = "carry"
	c := &state.Competition{ID: comp, Kind: "team", TeamSize: 3, Status: state.CompStatusPools}
	if kachinuki {
		c.TeamMatchType = state.TeamMatchTypeKachinuki
	}
	require.NoError(t, store.SaveCompetition(c))
	require.NoError(t, store.SaveParticipants(comp, []domain.Player{{Name: "Taka", Dojo: "T"}, {Name: "Kuma", Dojo: "K"}, {Name: "Washi", Dojo: "W"}, {Name: "Inu", Dojo: "I"}}))
	ps, err := store.LoadParticipants(comp, false)
	require.NoError(t, err)
	e := &carryEnv{t: t, r: r, store: store, eng: eng, id: map[string]string{}, members: map[string]string{}, comp: comp}
	for _, p := range ps {
		e.id[p.Name] = p.ID
	}
	names := []string{"Sato", "Ueda", "Mori", "Doi", "Endo", "Kato", "Aoki"}
	for i := 1; i <= 16; i++ {
		names = append(names, fmt.Sprintf("N%d", i))
	}
	for _, name := range names {
		m, err := store.AddTeamMember(comp, e.id["Taka"], name)
		require.NoError(t, err)
		e.members[name] = m.ID
	}
	e.base = time.Now().UnixMilli() - 600_000
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)
	require.NoError(t, store.SetTeamLineup(comp, domain.TeamLineup{TeamID: e.id["Taka"],
		Positions: map[domain.Position]string{p1: "Sato", p2: "Ueda"},
		MemberIDs: map[domain.Position]string{p1: e.members["Sato"], p2: e.members["Ueda"]},
		Stamps:    map[domain.Position]int64{p1: e.base - 1000, p2: e.base - 1000}}, 3))
	require.NoError(t, store.SavePoolMatches(comp, []state.MatchResult{
		{ID: "P1-0", SideA: "Taka", SideB: "Kuma", SideAID: e.id["Taka"], SideBID: e.id["Kuma"], Status: state.MatchStatusScheduled},
		{ID: "P1-1", SideA: "Taka", SideB: "Washi", SideAID: e.id["Taka"], SideBID: e.id["Washi"], Status: state.MatchStatusScheduled},
		{ID: "P1-2", SideA: "Taka", SideB: "Inu", SideAID: e.id["Taka"], SideBID: e.id["Inu"], Status: state.MatchStatusScheduled},
	}))
	return e
}

func (e *carryEnv) do(method, path string, body any) *httptest.ResponseRecorder {
	var b []byte
	if body != nil {
		b, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tournament-Password", "secret")
	w := httptest.NewRecorder()
	e.r.ServeHTTP(w, req)
	return w
}

func (e *carryEnv) lineupPath(match string) string {
	if match == "" {
		return "/api/competitions/" + e.comp + "/teams/" + e.id["Taka"] + "/lineups/0"
	}
	return "/api/competitions/" + e.comp + "/teams/" + e.id["Taka"] + "/match-lineups/" + url.PathEscape(match)
}

// save changes one position of the starting lineup (match "") or of a
// match's own lineup, stamped base+at (at 0: no stamp, an older build's).
func (e *carryEnv) save(match string, pos int, name string, at int64) {
	e.t.Helper()
	e.writes++
	p := domain.PositionNumbered(pos)
	body := map[string]any{
		"positions": map[domain.Position]string{p: name}, "memberIds": map[domain.Position]string{p: e.members[name]},
		"changed": []domain.Position{p}, "writeId": fmt.Sprintf("save-%d", e.writes),
	}
	if at != 0 {
		body["changedAt"] = map[domain.Position]int64{p: e.base + at}
	}
	rec := e.do(http.MethodPut, e.lineupPath(match), body)
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
}

// remove takes a match's own lineup away (or the starting lineup, match "").
func (e *carryEnv) remove(match string) {
	e.t.Helper()
	e.writes++
	rec := e.do(http.MethodDelete, e.lineupPath(match), map[string]any{"writeId": fmt.Sprintf("remove-%d", e.writes)})
	require.Equal(e.t, http.StatusNoContent, rec.Code, rec.Body.String())
}

// start starts a match at base+at, as the court console's Start does.
func (e *carryEnv) start(match, opp string, at int64) {
	e.t.Helper()
	rec := e.do(http.MethodPut, "/api/competitions/"+e.comp+"/matches/"+url.PathEscape(match)+"/score", map[string]any{
		"sideA": "Taka", "sideB": opp, "sideAId": e.id["Taka"], "sideBId": e.id[opp],
		"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
		"changed": []string{state.GroupResult}, "modifiedAt": e.base + at, "writeId": "start-" + match,
	})
	require.Equal(e.t, http.StatusOK, rec.Code, rec.Body.String())
}

func (e *carryEnv) lineup(match string) (domain.TeamLineup, bool) {
	e.t.Helper()
	lineups, err := e.store.LoadTeamLineups(e.comp)
	require.NoError(e.t, err)
	for _, l := range lineups {
		if l.TeamID == e.id["Taka"] && l.MatchID == match && (match != "" || l.Round == 0) {
			return l, true
		}
	}
	return domain.TeamLineup{}, false
}

// at reads a stored lineup's position: "name" when the name and the member
// id agree, "name/id-of-other" otherwise, "" when it is empty.
func (e *carryEnv) at(match string, pos int) string {
	e.t.Helper()
	l, ok := e.lineup(match)
	if !ok {
		return "(none)"
	}
	p := domain.PositionNumbered(pos)
	name := l.Positions[p]
	if id := l.MemberIDs[p]; id != e.members[name] {
		for n, mid := range e.members {
			if mid == id {
				return name + "/" + n
			}
		}
		return name + "/?"
	}
	return name
}

func (e *carryEnv) dump() string {
	var out []string
	for _, match := range []string{"", "P1-0", "P1-1", "P1-2"} {
		l, ok := e.lineup(match)
		if !ok {
			continue
		}
		var parts []string
		for _, p := range slices.SortedFunc(maps.Keys(l.Positions), domain.ComparePositions) {
			parts = append(parts, fmt.Sprintf("%s=%s", p, l.Positions[p]))
		}
		label := match
		if label == "" {
			label = "start"
		}
		out = append(out, label+"{"+strings.Join(parts, " ")+"}")
	}
	return strings.Join(out, " ")
}

func (e *carryEnv) stored(match string) state.MatchResult {
	e.t.Helper()
	ms, err := e.store.LoadPoolMatches(e.comp)
	require.NoError(e.t, err)
	for _, m := range ms {
		if m.ID == match {
			return m
		}
	}
	e.t.Fatalf("match %s not stored", match)
	return state.MatchResult{}
}

func (e *carryEnv) boutOne(match string) string {
	e.t.Helper()
	m := e.stored(match)
	for _, row := range m.SubResults {
		if row.Position == 1 {
			return row.SideA
		}
	}
	return "(no bout 1)"
}

// History is bounded, but never loses the value a copy reads: a copy made
// before nine and more changes of a position keeps what the position held
// when it was made, whichever of those changes arrive.
func TestLineupCarry_ACopyKeepsItsValuePastTheHistoryBound(t *testing.T) {
	e := newCarryEnv(t, false)
	e.save("P1-1", 2, "Endo", -500)
	require.Equal(t, "Sato", e.at("P1-1", 1))
	for i := 1; i <= domain.MaxPositionHistory+4; i++ {
		e.save("", 1, fmt.Sprintf("N%d", i), int64(i*10))
		require.Equalf(t, "Sato", e.at("P1-1", 1), "after %d changes of the starting lineup made after the copy: %s", i, e.dump())
	}
}

// The lineup a finished kachinuki match started with keeps its value however
// often the starting lineup changes after the start, and so does bout 1.
func TestLineupCarry_AFinishedMatchsPinKeepsItsValuePastTheHistoryBound(t *testing.T) {
	e := newCarryEnv(t, true)
	e.start("P1-2", "Inu", 0)
	require.Equal(t, "Sato", e.at("P1-2", 1))
	ms, err := e.store.LoadPoolMatches(e.comp)
	require.NoError(t, err)
	for i := range ms {
		if ms[i].ID == "P1-2" {
			ms[i].Status = state.MatchStatusCompleted
			ms[i].Winner, ms[i].WinnerID = "Taka", e.id["Taka"]
			ms[i].SubResults = []state.SubMatchResult{{Position: 1, SideA: "Sato", SideAMemberID: e.members["Sato"], SideB: "Kumo", IpponsA: []string{"M", "K"}, IpponsB: []string{}, Winner: "Sato"}}
		}
	}
	require.NoError(t, e.store.SavePoolMatches(e.comp, ms))
	for i := 1; i <= domain.MaxPositionHistory+4; i++ {
		e.save("", 1, fmt.Sprintf("N%d", i), int64(1000+i*10))
		require.Equalf(t, "Sato", e.at("P1-2", 1), "the pin after %d later changes: %s", i, e.dump())
		require.Equalf(t, "Sato", e.boutOne("P1-2"), "bout 1 after %d later changes", i)
		require.Equal(t, "Sato", e.stored("P1-2").SubResults[0].Winner)
	}
}

// Many changes of one position against copies and a pin of every age, in
// many arrival orders: each reads the latest change stamped before it.
func TestLineupCarry_ManyChangesOfOnePositionAgainstCopiesAndPinsOfEveryAge(t *testing.T) {
	type ages struct{ c0, c1, s int64 }
	rng := rand.New(rand.NewSource(7))
	run := func(t *testing.T, changes int, a ages, order []int) {
		t.Helper()
		// The model, in time: the starting lineup's position 1 at stamp at.
		startAt := func(at int64) string {
			name := "Sato"
			for k := 1; k <= changes; k++ {
				if int64(k*10) < at {
					name = fmt.Sprintf("N%d", k)
				}
			}
			return name
		}
		p0 := startAt(a.c0)
		p1 := startAt(a.c1)
		if a.c0 < a.c1 {
			p1 = p0
		}
		pin := startAt(a.s)
		switch {
		case a.c1 < a.s:
			pin = p1
		case a.c0 < a.s:
			pin = p0
		}
		want := fmt.Sprintf("P1-0=%s P1-1=%s P1-2=%s", p0, p1, pin)
		e := newCarryEnv(t, true)
		for _, i := range order {
			switch {
			case i < changes:
				e.save("", 1, fmt.Sprintf("N%d", i+1), int64((i+1)*10))
			case i == changes:
				e.save("P1-0", 2, "Endo", a.c0)
			case i == changes+1:
				e.save("P1-1", 2, "Doi", a.c1)
			default:
				e.start("P1-2", "Inu", a.s)
			}
		}
		got := fmt.Sprintf("P1-0=%s P1-1=%s P1-2=%s", e.at("P1-0", 1), e.at("P1-1", 1), e.at("P1-2", 1))
		require.Equalf(t, want, got, "arrival order %v: %s", order, e.dump())
	}
	all := []ages{{-500, 25, 45}, {25, -500, 45}, {45, 135, 25}, {135, 45, -500}, {15, 35, 55}}
	// Every arrival order (a sample of them), within the bound: a copy or
	// the start arriving after the changes still reads the one before it.
	t.Run("within the bound", func(t *testing.T) {
		const changes = domain.MaxPositionHistory - 2
		for _, a := range all {
			ref := make([]int, changes+3)
			for i := range ref {
				ref[i] = i
			}
			rev := slices.Clone(ref)
			slices.Reverse(rev)
			orders := [][]int{ref, rev}
			for range 6 {
				o := slices.Clone(ref)
				rng.Shuffle(len(o), func(i, j int) { o[i], o[j] = o[j], o[i] })
				orders = append(orders, o)
			}
			for _, o := range orders {
				run(t, changes, a, o)
			}
		}
	})
	// Past the bound: the copies and the start are made first, in any order,
	// and more changes than the bound then arrive in any order, those made
	// before each copy included: none of them moves a copy or the pin.
	t.Run("past the bound", func(t *testing.T) {
		const changes = domain.MaxPositionHistory + 6
		for _, a := range all {
			for range 4 {
				first := []int{changes, changes + 1, changes + 2}
				rng.Shuffle(len(first), func(i, j int) { first[i], first[j] = first[j], first[i] })
				rest := make([]int, changes)
				for i := range rest {
					rest[i] = i
				}
				rng.Shuffle(len(rest), func(i, j int) { rest[i], rest[j] = rest[j], rest[i] })
				run(t, changes, a, append(first, rest...))
			}
		}
	})
}
