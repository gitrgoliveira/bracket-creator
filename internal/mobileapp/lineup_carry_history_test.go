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

// removeAt takes match's own lineup away ("" the starting lineup) as a
// removal made at at (relative to base), as this build's outbox sends it.
func (e *carryEnv) removeAt(match string, at int64) {
	e.t.Helper()
	e.writes++
	rec := e.do(http.MethodDelete, e.lineupPath(match), map[string]any{"writeId": fmt.Sprintf("remove-%d", e.writes), "removedAt": e.base + at})
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

// manyChanges is a number of changes of one position past the count History
// was once bounded by (stage 6 reviews 7 and 8: it is no longer bounded).
const manyChanges = 16

// History never loses the value a copy reads: a copy made before many
// changes of a position keeps what the position held when it was made,
// whichever of those changes arrive.
func TestLineupCarry_ACopyKeepsItsValueAfterManyLaterChanges(t *testing.T) {
	e := newCarryEnv(t, false)
	e.save("P1-1", 2, "Endo", -500)
	require.Equal(t, "Sato", e.at("P1-1", 1))
	for i := 1; i <= manyChanges+4; i++ {
		e.save("", 1, fmt.Sprintf("N%d", i), int64(i*10))
		require.Equalf(t, "Sato", e.at("P1-1", 1), "after %d changes of the starting lineup made after the copy: %s", i, e.dump())
	}
}

// The lineup a finished kachinuki match started with keeps its value however
// often the starting lineup changes after the start, and so does bout 1.
func TestLineupCarry_AFinishedMatchsPinKeepsItsValueAfterManyLaterChanges(t *testing.T) {
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
	for i := 1; i <= manyChanges+4; i++ {
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
	// Every arrival order (a sample of them), a few changes: a copy or the
	// start arriving after the changes still reads the one before it.
	t.Run("a few changes", func(t *testing.T) {
		const changes = manyChanges - 2
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
	// Many changes: the copies and the start are made first, in any order,
	// and the changes then arrive in any order, those made before each copy
	// included: none of them moves a copy or the pin. Then every step in any
	// order, the copies and the start arriving after many later changes too
	// (stage 6 review 8, m2: History is no longer bounded by count, so a late
	// copy reads what the position held at its stamp).
	t.Run("many changes", func(t *testing.T) {
		const changes = manyChanges + 6
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
			for range 2 {
				o := make([]int, changes+3)
				for i := range o {
					o[i] = i
				}
				rng.Shuffle(len(o), func(i, j int) { o[i], o[j] = o[j], o[i] })
				run(t, changes, a, o)
			}
			last := make([]int, 0, changes+3)
			for i := range changes {
				last = append(last, i)
			}
			run(t, changes, a, append(last, changes, changes+1, changes+2))
		}
	})
}

// Taking a match's own lineup away is a change stamped when it is made: a
// copy made while that lineup was in force keeps what it carried from it,
// whether the removal arrives before or after an unrelated save.
func TestLineupCarry_ARemovalIsOrderedLikeAChange(t *testing.T) {
	for _, removeFirst := range []bool{false, true} {
		t.Run(fmt.Sprintf("removal first %v", removeFirst), func(t *testing.T) {
			e := newCarryEnv(t, false)
			e.save("P1-0", 1, "Mori", -100)
			e.save("P1-1", 2, "Endo", -50)
			require.Equal(t, "Mori", e.at("P1-1", 1))
			if removeFirst {
				e.remove("P1-0")
				e.save("", 2, "Doi", 10)
			} else {
				e.save("", 2, "Doi", 10)
				e.remove("P1-0")
			}
			require.Equal(t, "(none)", e.at("P1-0", 1), "the match's own lineup is gone")
			require.Equal(t, "Mori", e.at("P1-1", 1), "made while P1-0's lineup was in force: %s", e.dump())
			require.Equal(t, "Endo", e.at("P1-1", 2))
			// P1-0 now carries the starting lineup, the later change included.
			in, err := e.eng.LineupInForce(e.comp, e.id["Taka"], "P1-0")
			require.NoError(t, err)
			require.Equal(t, "", in.Source.MatchID)
			require.Equal(t, "Doi", in.Lineup.Positions[domain.PositionNumbered(2)])
			require.Equal(t, "Sato", in.Lineup.Positions[domain.PositionNumbered(1)])
		})
	}
}

// A removal is ordered by when it was made, like every change (stage 6 review
// 8, m1): device A's removal made at 200 that arrives after device B's save
// made at 300 takes nothing of B's change away, and a save made before the
// removal is taken away by it whenever it arrives.
func TestLineupCarry_ARemovalIsOrderedByItsOwnStamp(t *testing.T) {
	for _, removalLast := range []bool{true, false} {
		e := newCarryEnv(t, false)
		if !removalLast {
			e.removeAt("P1-1", 200)
		}
		e.save("P1-1", 2, "Endo", 300)
		e.save("P1-1", 3, "Kato", 100)
		if removalLast {
			e.removeAt("P1-1", 200)
		}
		require.Equalf(t, "Endo", e.at("P1-1", 2), "removal last %v: %s", removalLast, e.dump())
		require.Equal(t, "Sato", e.at("P1-1", 1), "carried as at 300")
		l, ok := e.lineup("P1-1")
		require.True(t, ok)
		_, kept := l.Positions[domain.PositionNumbered(3)]
		require.False(t, kept, "the change made before the removal is taken away by it")
	}
}

// A removal keeps each change in one record only (stage 6 review 8, m3): the
// record of a removed copy holds its own positions and their History, never
// the positions it carried, and a save after it starts a lineup of its own
// with none of the removed one's History.
func TestLineupCarry_ARemovalKeepsEachChangeOnce(t *testing.T) {
	e := newCarryEnv(t, false)
	for i := range 3 {
		at := int64(i * 100)
		e.save("P1-0", 1, "Mori", at+10)
		e.save("P1-0", 1, "Kato", at+20)
		e.removeAt("P1-0", at+50)
	}
	e.save("P1-0", 1, "Doi", 400)
	all, err := e.store.LoadTeamLineupsWithRemoved(e.comp)
	require.NoError(t, err)
	p1 := domain.PositionNumbered(1)
	changes, records := 0, 0
	for _, l := range all {
		if l.MatchID != "P1-0" || (l.IsRemoved() && l.CopiedAt == l.RemovedAt) {
			continue // a removal marker holds no change
		}
		records++
		if l.IsRemoved() {
			require.Equal(t, []domain.Position{p1}, slices.Sorted(maps.Keys(l.Positions)), "a removed record holds its own position alone: %+v", l)
		}
		changes += 1 + len(l.History[p1])
	}
	require.Equal(t, 4, records, "three removed lineups and the live one")
	require.Equal(t, 7, changes, "each of the seven changes kept once")
}

// Every arrival stamp is after every stamp on disk (stage 6 review 8, m5): a
// restart, or a clock stepped back, never gives a removal an older stamp than
// one kept, nor its record another's key.
func TestLineupCarry_ArrivalStampsComeAfterEveryStampOnDisk(t *testing.T) {
	e := newCarryEnv(t, false)
	ahead := time.Now().UnixMilli() + 120_000
	p1 := domain.PositionNumbered(1)
	require.NoError(t, e.store.SetTeamLineup(e.comp, domain.TeamLineup{TeamID: e.id["Taka"], MatchID: "P1-0",
		Positions: map[domain.Position]string{p1: "Mori"}, Stamps: map[domain.Position]int64{p1: ahead},
		CopiedAt: ahead, Own: []domain.Position{p1}}, 3))
	_, err := e.store.LoadTeamLineupsWithRemoved(e.comp)
	require.NoError(t, err)
	require.Greater(t, state.LineupArrivalStamp(), ahead)
	// Two removals sent with no stamp, one after the other, are two records.
	e.remove("P1-0")
	e.save("P1-0", 1, "Kato", 0)
	e.remove("P1-0")
	all, err := e.store.LoadTeamLineupsWithRemoved(e.comp)
	require.NoError(t, err)
	stamps := map[int64]bool{}
	for _, l := range all {
		if l.MatchID == "P1-0" {
			require.True(t, l.IsRemoved())
			require.Greater(t, l.RemovedAt, ahead)
			require.False(t, stamps[l.RemovedAt], "a removal's stamp is its own")
			stamps[l.RemovedAt] = true
		}
	}
	require.Len(t, stamps, 2)
}

// DELETE lineups/:round takes a writeId like every lineup door: a late
// duplicate is answered as recorded and changes nothing, and a removal that
// takes nothing away works nothing out again (stage 6 review 8, m8).
func TestLineupCarry_ARoundRemovalIsOnceAndFollowsOnlyWhatItTook(t *testing.T) {
	e := newCarryEnv(t, false)
	r := gin.New()
	admin := r.Group("/api")
	verifier := NewFileVerifier(e.store)
	admin.Use(AuthMiddleware(verifier, e.store))
	hub := NewHub()
	t.Cleanup(hub.Close)
	RegisterLineupHandlers(admin, e.store, e.store, e.store, hub, e.store, verifier, refusingFollow{e.eng})
	e.r = r
	ghost := "/api/competitions/" + e.comp + "/teams/" + e.id["Kuma"] + "/lineups/0"
	rec := e.do(http.MethodDelete, ghost, map[string]any{"writeId": "rm-ghost"})
	require.Equal(t, http.StatusNoContent, rec.Code, "nothing taken away, so nothing to work out again: %s", rec.Body.String())
	rec = e.do(http.MethodDelete, e.lineupPath(""), map[string]any{"writeId": "rm-start"})
	require.Equal(t, http.StatusInternalServerError, rec.Code, "a lineup taken away is followed (here refused)")
	require.Equal(t, "Sato", e.at("", 1), "the refused removal was not kept")
	// With a follow that stores, the removal lands once.
	e2 := newCarryEnv(t, false)
	rec = e2.do(http.MethodDelete, e2.lineupPath(""), map[string]any{"writeId": "rm-1", "removedAt": e2.base})
	require.Equal(t, http.StatusNoContent, rec.Code)
	e2.save("", 1, "Aoki", 10)
	rec = e2.do(http.MethodDelete, e2.lineupPath(""), map[string]any{"writeId": "rm-1", "removedAt": e2.base})
	require.Equal(t, http.StatusNoContent, rec.Code)
	require.Equal(t, "Aoki", e2.at("", 1), "a late duplicate takes nothing away")
}

// A removal made after a kachinuki match started never reaches the lineup it
// started with, nor its bout 1, whatever arrives after it.
func TestLineupCarry_ARemovalAfterAStartNeverReachesThePin(t *testing.T) {
	e := newCarryEnv(t, true)
	e.save("P1-0", 1, "Mori", -100)
	e.start("P1-2", "Inu", 0)
	require.Equal(t, "Mori", e.at("P1-2", 1))
	ms, err := e.store.LoadPoolMatches(e.comp)
	require.NoError(t, err)
	for i := range ms {
		if ms[i].ID == "P1-2" {
			ms[i].SubResults = []state.SubMatchResult{{Position: 1, SideA: "Mori", SideAMemberID: e.members["Mori"], SideB: "Kumo", IpponsA: []string{}, IpponsB: []string{}}}
		}
	}
	require.NoError(t, e.store.SavePoolMatches(e.comp, ms))
	e.remove("P1-0")
	require.Equal(t, "Mori", e.at("P1-2", 1))
	e.save("", 2, "Doi", 500)
	require.Equal(t, "Mori", e.at("P1-2", 1), "an unrelated save after the removal: %s", e.dump())
	require.Equal(t, "Mori", e.boutOne("P1-2"))
}

// Taking the starting lineup away reaches no copy made before it.
func TestLineupCarry_RemovingTheStartingLineupReachesNoEarlierCopy(t *testing.T) {
	e := newCarryEnv(t, false)
	e.save("P1-1", 2, "Endo", -50)
	require.Equal(t, "Sato", e.at("P1-1", 1))
	e.remove("")
	require.Equal(t, "(none)", e.at("", 1))
	require.Equal(t, "Sato", e.at("P1-1", 1))
	e.save("P1-0", 2, "Doi", 10)
	require.Equal(t, "Sato", e.at("P1-1", 1), "a save after the removal: %s", e.dump())
	// A change made before the removal that arrives after it reads the
	// starting lineup as it stood then.
	require.Equal(t, "Sato", e.at("P1-0", 1))
	require.Equal(t, "Doi", e.at("P1-0", 2))
	// Unstamped, it is made as it lands, after the removal: nothing carried.
	e.save("P1-1", 3, "Kato", 0)
	require.Equal(t, "Sato", e.at("P1-1", 1), "the copy keeps the point it was made at")
}

// A change sent with no stamp (an older build's) takes its arrival as its
// stamp: a match's first such save makes a copy at that time, and a change
// stamped before it that arrives later still reaches it.
func TestLineupCarry_AnUnstampedSaveIsMadeAsItLands(t *testing.T) {
	e := newCarryEnv(t, false)
	e.save("P1-1", 2, "Endo", 0)
	l, ok := e.lineup("P1-1")
	require.True(t, ok)
	require.True(t, l.IsCarriedCopy(), "a copy, made at its arrival")
	require.Greater(t, l.CopiedAt, e.base+500_000)
	require.Equal(t, "Sato", e.at("P1-1", 1))
	e.save("", 1, "Mori", -100)
	require.Equal(t, "Mori", e.at("P1-1", 1), "made before the copy: %s", e.dump())
}

// An older build's whole save (no `changed`) of the starting lineup is a
// change of every position at its arrival: a copy made before it keeps what
// it carried, then and after any later save.
func TestLineupCarry_AnOlderBuildsWholeSaveReachesNoEarlierCopy(t *testing.T) {
	e := newCarryEnv(t, false)
	e.save("P1-1", 2, "Endo", -500)
	require.Equal(t, "Sato", e.at("P1-1", 1))
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)
	rec := e.do(http.MethodPut, e.lineupPath(""), map[string]any{
		"positions": map[domain.Position]string{p1: "Aoki", p2: "Kato"},
		"memberIds": map[domain.Position]string{p1: e.members["Aoki"], p2: e.members["Kato"]},
		"writeId":   "whole",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "Aoki", e.at("", 1))
	require.Equal(t, "Sato", e.at("P1-1", 1), "after the whole save: %s", e.dump())
	e.save("", 3, "Doi", 10)
	require.Equal(t, "Sato", e.at("P1-1", 1), "after a later partial save: %s", e.dump())
	// A copy made after the whole save carries it.
	e.save("P1-0", 2, "Mori", 0)
	require.Equal(t, "Aoki", e.at("P1-0", 1))
	// A whole save of a match's own lineup replaces it whole: the positions
	// it leaves out are empty, and carried from nothing.
	rec = e.do(http.MethodPut, e.lineupPath("P1-1"), map[string]any{
		"positions": map[domain.Position]string{p1: "Ueda"}, "memberIds": map[domain.Position]string{p1: e.members["Ueda"]},
		"writeId": "whole-match",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, "Ueda", e.at("P1-1", 1))
	require.Equal(t, "(none)", func() string {
		l, _ := e.lineup("P1-1")
		if n, ok := l.Positions[p2]; ok {
			return n
		}
		return "(none)"
	}())
}

// refusingFollow is the engine with a follow whose store refuses the write.
type refusingFollow struct{ *engine.Engine }

func (refusingFollow) FollowLineupSave(state.StoreTx, *state.Competition, domain.TeamLineup, []domain.Position) ([]string, error) {
	return nil, fmt.Errorf("disk refuses")
}

// A save whose follow cannot be stored is not kept: the door answers 500 (the
// outbox sends it again) and the transaction leaves nothing behind.
func TestLineupCarry_AFollowThatCannotBeStoredFailsTheSave(t *testing.T) {
	e := newCarryEnv(t, false)
	r := gin.New()
	admin := r.Group("/api")
	verifier := NewFileVerifier(e.store)
	admin.Use(AuthMiddleware(verifier, e.store))
	hub := NewHub()
	t.Cleanup(hub.Close)
	RegisterLineupHandlers(admin, e.store, e.store, e.store, hub, e.store, verifier, refusingFollow{e.eng})
	e.r = r
	p := domain.PositionNumbered(1)
	for _, path := range []string{e.lineupPath(""), e.lineupPath("P1-0")} {
		rec := e.do(http.MethodPut, path, map[string]any{
			"positions": map[domain.Position]string{p: "Aoki"}, "memberIds": map[domain.Position]string{p: e.members["Aoki"]},
			"changed": []domain.Position{p}, "changedAt": map[domain.Position]int64{p: e.base}, "writeId": "w-" + path,
		})
		require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	}
	require.Equal(t, "Sato", e.at("", 1), "the starting lineup save was not kept")
	require.Equal(t, "(none)", e.at("P1-0", 1))
	rec := e.do(http.MethodDelete, e.lineupPath(""), map[string]any{"writeId": "rm"})
	require.Equal(t, http.StatusInternalServerError, rec.Code, rec.Body.String())
	require.Equal(t, "Sato", e.at("", 1), "the removal was not kept")
}
