package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
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

// carryStep is one partial lineup save of the property below: the match
// ("" for the team's starting lineup), the one position it changes and the
// name it gives it, and when the change was made (ms from the start).
type carryStep struct {
	match string
	pos   int // 0: the lineup is taken away (a DELETE), stamped as it lands
	name  string
	at    int64
}

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
	// A removal ("Use the previous match's lineup", stamped as it lands, so
	// after every change here) is ordered like a change: the lineups made
	// while the removed one was in force keep what they carried from it,
	// whatever arrives before or after it. A save of the removed match
	// itself is ordered by arrival against the removal (it makes the
	// match's own lineup again), so it always arrives before it here.
	removal := []carryStep{
		{"P1-0", 1, "Mori", -100},
		{"P1-1", 2, "Endo", -50},
		{"", 2, "Doi", 10},
		{"", 1, "Aoki", -150},
		{"P1-0", 0, "", 0},
	}
	removeStart := []carryStep{
		{"P1-1", 2, "Endo", -50},
		{"", 1, "Aoki", -150},
		{"P1-0", 2, "Doi", 10},
		{"", 0, "", 0},
	}
	for _, sc := range []struct {
		name      string
		steps     []carryStep
		kachinuki bool
	}{
		{"fixed order", fixedOrder, false}, {"kachinuki, with the start", steps, true}, {"one position changed twice", samePosition, false},
		{"a match's lineup removed", removal, false}, {"a match's lineup removed, kachinuki, with the start", removal, true},
		{"the starting lineup removed", removeStart, false}, {"the starting lineup removed, kachinuki, with the start", removeStart, true},
	} {
		steps, kachinuki := sc.steps, sc.kachinuki
		t.Run(sc.name, func(t *testing.T) {
			n := len(steps)
			if kachinuki {
				n++ // the start of P1-2, at 0
			}
			idx := make([]int, n)
			for i := range idx {
				idx[i] = i
			}
			// The reference: every change in the order it was made, a
			// removal (stamped as it lands) after every device's change.
			ref := slices.Clone(idx)
			at := func(i int) int64 {
				if i == len(steps) {
					return 0
				}
				if steps[i].pos == 0 {
					return 1 << 40
				}
				return steps[i].at
			}
			slices.SortFunc(ref, func(a, b int) int { return int(at(a) - at(b)) })
			want := carryRun(t, steps, kachinuki, ref)
			differ, total := 0, 0
			var first []string
			for _, order := range carryPerms(idx) {
				if !carrySavesBeforeTheirRemoval(steps, order) {
					continue
				}
				total++
				if got := carryRun(t, steps, kachinuki, order); got != want {
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

// carrySavesBeforeTheirRemoval says every save of a lineup arrives before
// that lineup's removal in order.
func carrySavesBeforeTheirRemoval(steps []carryStep, order []int) bool {
	removed := map[string]bool{}
	for _, i := range order {
		if i >= len(steps) {
			continue
		}
		s := steps[i]
		if s.pos == 0 {
			removed[s.match] = true
		} else if removed[s.match] {
			return false
		}
	}
	return true
}

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
func carryRun(t *testing.T, steps []carryStep, kachinuki bool, order []int) string {
	t.Helper()
	// The doors as the server wires them: the lineup PUTs with the engine
	// that follows a save (setupTestRouter registers them without it).
	gin.SetMode(gin.TestMode)
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Carry", Password: "secret"}))
	eng := engine.New(store)
	hub := NewHub()
	defer hub.Close()
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
			rec := do(http.MethodDelete, path, map[string]any{"writeId": fmt.Sprintf("remove-%d", i)})
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
