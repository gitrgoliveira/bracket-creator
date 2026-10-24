package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
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

// carryStep is one partial lineup save of the property below: the match
// ("" for the team's starting lineup), the one position it changes and the
// name it gives it, and when the change was made (ms from the start).
type carryStep struct {
	match string
	pos   int // 0: the lineup is taken away (a DELETE), at its own stamp
	name  string
	at    int64
}

// carryRaceKeep is how many arrival orders of each scenario run under the race
// detector (raceSample); a scenario of 24 or 120 runs whole there too.
const carryRaceKeep = 120

// TestLineupCarry_AMatchLineupIsTheSameInEveryArrivalOrder is the property the
// operator asked for (decision L1): a match's own lineup comes out the same in
// every arrival order of its saves, given their stamps. A first partial save
// of a match's own lineup takes the positions it does not name from the carry
// rule BY STAMP (what the match would have taken from the saves made before
// it), and a save made earlier that arrives later is worked into it. A
// kachinuki match's start keeps the lineup in force then the same way.
func TestLineupCarry_AMatchLineupIsTheSameInEveryArrivalOrder(t *testing.T) {
	steps := []carryStep{
		{"P1-0", 1, "Mori", -100},
		{"P1-0", 2, "Doi", 50},
		{"P1-1", 2, "Endo", -300},
		{"P1-1", 1, "Kato", 70},
		{"", 1, "Aoki", -50},
	}
	// Without a start, a partial save of P1-2 is what keeps carried positions:
	// P1-0 and P1-1 end up naming both their positions themselves.
	fixedOrder := append(slices.Clone(steps), carryStep{"P1-2", 2, "Ueda", -200})
	// Two changes of one position: it holds the later BY STAMP, and a copy
	// made between them carries the earlier.
	samePosition := []carryStep{
		{"", 1, "Aoki", -50},
		{"", 1, "Kato", -150},
		{"P1-1", 2, "Endo", -100},
		{"P1-0", 1, "Mori", -20},
	}
	// A removal ("Use the previous match's lineup") is a change stamped when
	// it was made, ordered by that stamp like every change (stage 6 review 8,
	// m1): the lineups made while the removed one was in force keep what they
	// carried from it, a save of the removed match made before the removal
	// is taken away by it whenever it arrives, and one made after it makes
	// the match's own lineup again.
	removal := []carryStep{
		{"P1-0", 1, "Mori", -100},
		{"P1-1", 2, "Endo", -50},
		{"", 1, "Aoki", -150},
		{"P1-0", 0, "", -20},
		{"P1-0", 2, "Kato", 30},
	}
	removeStart := []carryStep{
		{"P1-1", 2, "Endo", -50},
		{"", 1, "Aoki", -150},
		{"P1-0", 2, "Doi", 10},
		{"", 0, "", -20},
		{"", 1, "Mori", 40},
	}
	// M1 (stage 6 review 8): a copy whose stamp moves back carries nothing
	// from a lineup made after the earlier stamp.
	movedBack := []carryStep{
		{"P1-1", 1, "Mori", 10},
		{"P1-0", 3, "Endo", 50},
		{"P1-1", 2, "Doi", 100},
		{"", 1, "Aoki", 70},
	}
	// And a kachinuki start's pin made a copy at a stamp before the start.
	pinToCopy := []carryStep{
		{"P1-0", 3, "Endo", -100},
		{"P1-2", 2, "Doi", -300},
		{"", 1, "Aoki", -50},
	}
	// The started match's own lineup: saved after the start (and after a
	// removal of it made before the start), whatever arrives first, it is the
	// lineup the start kept made the operator's own, carried from the start.
	startedOwn := []carryStep{
		{"", 2, "Doi", 25},
		{"P1-2", 1, "Mori", 50},
		{"P1-2", 0, "", -20},
	}
	// A removal of the started match's own lineup made after the start, the
	// save of it made after that.
	startedRemoved := []carryStep{
		{"", 2, "Doi", 25},
		{"P1-2", 0, "", 40},
		{"P1-2", 1, "Mori", 60},
		{"", 1, "Aoki", 50},
	}
	gin.SetMode(gin.TestMode)
	for k, sc := range []struct {
		name      string
		steps     []carryStep
		kachinuki bool
	}{
		{"the started match's own lineup saved after the start", startedOwn, true},
		{"the started match's own lineup taken away after the start, then saved", startedRemoved, true},
		{"fixed order", fixedOrder, false}, {"kachinuki, with the start", steps, true}, {"one position changed twice", samePosition, false},
		{"a match's lineup removed", removal, false}, {"a match's lineup removed, kachinuki, with the start", removal, true},
		{"the starting lineup removed", removeStart, false}, {"the starting lineup removed, kachinuki, with the start", removeStart, true},
		{"a copy's stamp moved back", movedBack, false}, {"a pin made a copy before the start", pinToCopy, true},
	} {
		steps, kachinuki, comp := sc.steps, sc.kachinuki, fmt.Sprintf("carry%d", k)
		t.Run(sc.name, func(t *testing.T) {
			// Each scenario has its own competition, so they run side by side.
			t.Parallel()
			fx := newCarryFixture(t, comp, kachinuki)
			n := len(steps)
			if kachinuki {
				n++ // the start of P1-2, at 0
			}
			idx := make([]int, n)
			for i := range idx {
				idx[i] = i
			}
			// The reference: every change in the order it was made, a removal
			// at its own stamp too.
			ref := slices.Clone(idx)
			at := func(i int) int64 {
				if i == len(steps) {
					return 0
				}
				return steps[i].at
			}
			slices.SortFunc(ref, func(a, b int) int { return int(at(a) - at(b)) })
			want := carryRun(t, fx, steps, ref)
			t.Logf("in time: %s", want)
			differ, total := 0, 0
			var first []string
			all := carryPerms(idx)
			orders := raceSample(all, carryRaceKeep)
			t.Logf("%d of %d arrival orders driven", len(orders), len(all))
			for _, order := range orders {
				total++
				if got := carryRun(t, fx, steps, order); got != want {
					differ++
					if len(first) < 4 {
						first = append(first, fmt.Sprintf("order %v: %s", order, got))
					}
				}
			}
			require.Zerof(t, differ, "%d of %d arrival orders give another lineup than the one in time (%s):\n%s", differ, total, want, strings.Join(first, "\n"))
		})
	}
}

// carryPerms is every order of xs.
func carryPerms(xs []int) [][]int {
	if len(xs) <= 1 {
		return [][]int{slices.Clone(xs)}
	}
	var out [][]int
	for i := range xs {
		rest := append(slices.Clone(xs[:i]), xs[i+1:]...)
		for _, p := range carryPerms(rest) {
			out = append(out, append([]int{xs[i]}, p...))
		}
	}
	return out
}

// carryRun plays one arrival order through the lineup doors and the start, and
// answers the team's three match lineups as stored.
func carryRun(t *testing.T, fx carryFixture, steps []carryStep, order []int) string {
	t.Helper()
	// Every ordering starts from a copy of the fixture's data folder, opened by
	// a store of its own, as a restarted server opens it.
	dir := t.TempDir()
	require.NoError(t, os.CopyFS(dir, os.DirFS(fx.dir)))
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	// The doors as the server wires them: the lineup PUTs with the engine
	// that follows a save (setupTestRouter registers them without it).
	eng := engine.New(store)
	hub := NewHub()
	defer hub.Close()
	verifier := NewFileVerifier(store)
	r := gin.New()
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(verifier, store))
	RegisterMatchHandlers(admin, eng, store, store, hub, verifier, store)
	RegisterLineupHandlers(admin, store, store, store, hub, store, verifier, eng)
	comp, id, members, base := fx.comp, fx.id, fx.members, fx.base
	do := func(method, path string, body any) *httptest.ResponseRecorder {
		b, _ := json.Marshal(body)
		req := httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", "secret")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	for _, i := range order {
		if i == len(steps) {
			path := "/api/competitions/" + comp + "/matches/" + url.PathEscape("P1-2") + "/score"
			rec := do(http.MethodPut, path, map[string]any{
				"sideA": "Taka", "sideB": "Inu", "sideAId": id["Taka"], "sideBId": id["Inu"],
				"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
				"changed": []string{state.GroupResult}, "modifiedAt": base, "writeId": "start",
			})
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			continue
		}
		s := steps[i]
		pos := domain.PositionNumbered(s.pos)
		path := "/api/competitions/" + comp + "/teams/" + id["Taka"] + "/lineups/0"
		if s.match != "" {
			path = "/api/competitions/" + comp + "/teams/" + id["Taka"] + "/match-lineups/" + url.PathEscape(s.match)
		}
		if s.pos == 0 {
			rec := do(http.MethodDelete, path, map[string]any{"writeId": fmt.Sprintf("remove-%d", i), "removedAt": base + s.at})
			require.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
			continue
		}
		rec := do(http.MethodPut, path, map[string]any{
			"positions": map[domain.Position]string{pos: s.name}, "memberIds": map[domain.Position]string{pos: members[s.name]},
			"changed": []domain.Position{pos}, "changedAt": map[domain.Position]int64{pos: base + s.at},
			"writeId": fmt.Sprintf("save-%d", i),
		})
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	}
	lineups, err := store.LoadTeamLineups(comp)
	require.NoError(t, err)
	byName := map[string]string{}
	for n, mid := range members {
		byName[mid] = n
	}
	var out []string
	for _, match := range []string{"P1-0", "P1-1", "P1-2"} {
		for _, l := range lineups {
			if l.TeamID != id["Taka"] || l.MatchID != match {
				continue
			}
			var parts []string
			for _, p := range slices.SortedFunc(maps.Keys(l.Positions), domain.ComparePositions) {
				parts = append(parts, fmt.Sprintf("%s=%s/%s", p, l.Positions[p], byName[l.MemberIDs[p]]))
			}
			out = append(out, match+"{"+strings.Join(parts, " ")+"}")
		}
	}
	return strings.Join(out, " ")
}

// carryFixture is the data every ordering of one scenario starts from, built
// once (seven team members added one by one rewrite team-members.yaml seven
// times, about half of an ordering's cost) and copied for each ordering.
type carryFixture struct {
	dir, comp   string
	base        int64
	id, members map[string]string
}

// newCarryFixture builds the team competition the orderings run on: four
// teams, Taka's seven members, its starting lineup and its three pool matches.
func newCarryFixture(t *testing.T, comp string, kachinuki bool) carryFixture {
	t.Helper()
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Carry", Password: "secret"}))
	c := &state.Competition{ID: comp, Kind: "team", TeamSize: 3, Status: state.CompStatusPools}
	if kachinuki {
		c.TeamMatchType = state.TeamMatchTypeKachinuki
	}
	require.NoError(t, store.SaveCompetition(c))
	require.NoError(t, store.SaveParticipants(comp, []domain.Player{{Name: "Taka", Dojo: "T"}, {Name: "Kuma", Dojo: "K"}, {Name: "Washi", Dojo: "W"}, {Name: "Inu", Dojo: "I"}}))
	ps, err := store.LoadParticipants(comp, false)
	require.NoError(t, err)
	id := map[string]string{}
	for _, p := range ps {
		id[p.Name] = p.ID
	}
	members := map[string]string{}
	for _, name := range []string{"Sato", "Ueda", "Mori", "Doi", "Endo", "Kato", "Aoki"} {
		m, err := store.AddTeamMember(comp, id["Taka"], name)
		require.NoError(t, err)
		members[name] = m.ID
	}
	base := time.Now().UnixMilli() - 600_000
	p1, p2 := domain.PositionNumbered(1), domain.PositionNumbered(2)
	require.NoError(t, store.SetTeamLineup(comp, domain.TeamLineup{TeamID: id["Taka"],
		Positions: map[domain.Position]string{p1: "Sato", p2: "Ueda"},
		MemberIDs: map[domain.Position]string{p1: members["Sato"], p2: members["Ueda"]},
		Stamps:    map[domain.Position]int64{p1: base - 1000, p2: base - 1000}}, 3))
	require.NoError(t, store.SavePoolMatches(comp, []state.MatchResult{
		{ID: "P1-0", SideA: "Taka", SideB: "Kuma", SideAID: id["Taka"], SideBID: id["Kuma"], Status: state.MatchStatusScheduled},
		{ID: "P1-1", SideA: "Taka", SideB: "Washi", SideAID: id["Taka"], SideBID: id["Washi"], Status: state.MatchStatusScheduled},
		{ID: "P1-2", SideA: "Taka", SideB: "Inu", SideAID: id["Taka"], SideBID: id["Inu"], Status: state.MatchStatusScheduled},
	}))
	return carryFixture{dir: dir, comp: comp, base: base, id: id, members: members}
}
