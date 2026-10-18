package engine

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/require"
)

var rv5p1, rv5p2 = domain.PositionNumbered(1), domain.PositionNumbered(2)

// rv5Save saves one lineup the way the lineup door does: the changed
// positions landed on the lineup stored for that scope (lineupToStore),
// stamped, then carried to the lineups matches started with
// (FollowLineupSave), in one transaction. matchID "" is the starting lineup.
func rv5Save(t *testing.T, eng *Engine, store *state.Store, c, matchID string, names map[domain.Position]string, stamp int64) {
	t.Helper()
	comp, err := store.LoadCompetition(c)
	require.NoError(t, err)
	changed := slices.SortedFunc(maps.Keys(names), domain.ComparePositions)
	ids := map[domain.Position]string{}
	stamps := map[domain.Position]int64{}
	for p, n := range names {
		ids[p] = "m-" + strings.ToLower(n)
		stamps[p] = stamp
	}
	require.NoError(t, store.WithTransaction(c, func(tx state.StoreTx) error {
		lineups, err := tx.LoadTeamLineups(c)
		require.NoError(t, err)
		var base domain.TeamLineup
		for _, l := range lineups {
			if l.TeamID == "ta" && l.MatchID == matchID && l.Round == 0 && !l.IsStartPin() {
				base = l
			}
		}
		saved, err := base.ApplyChanges(changed, names, ids)
		require.NoError(t, err)
		saved.TeamID, saved.CompetitionID, saved.MatchID, saved.Round = "ta", c, matchID, 0
		saved.StartedAt, saved.StartedFrom = 0, ""
		saved.StampChanges(changed, stamps, stamp+1_000_000)
		require.NoError(t, tx.SetTeamLineup(c, saved, 3))
		_, err = eng.FollowLineupSave(tx, comp, saved, changed)
		return err
	}))
}

// rv5Pin is the lineup match P1-2 started with, as names by position, and the
// lineup it names as its source.
func rv5Pin(t *testing.T, store *state.Store, c string) (string, string) {
	t.Helper()
	lineups, err := store.LoadTeamLineups(c)
	require.NoError(t, err)
	for _, l := range lineups {
		if l.TeamID == "ta" && l.MatchID == "P1-2" && l.IsStartPin() {
			var parts []string
			for _, p := range slices.SortedFunc(maps.Keys(l.Positions), domain.ComparePositions) {
				parts = append(parts, fmt.Sprintf("%s=%s/%s", p, l.Positions[p], l.MemberIDs[p]))
			}
			return strings.Join(parts, " "), l.StartedFrom
		}
	}
	t.Fatalf("P1-2 kept no lineup at its start")
	return "", ""
}

// rv5Orders is every ordering of n steps.
func rv5Orders(n int) [][]int {
	if n == 0 {
		return [][]int{{}}
	}
	var out [][]int
	for _, rest := range rv5Orders(n - 1) {
		for at := 0; at <= len(rest); at++ {
			o := append(append(append([]int{}, rest[:at]...), n-1), rest[at:]...)
			out = append(out, o)
		}
	}
	return out
}

// The lineup a kachinuki match started with is what the carry rule gave it at
// its start from every save stamped before the start, whatever order those
// saves arrive in, before or after the start (stage 6 review 5, finding 1). A
// late save of an EARLIER match's own lineup used to be merged into the pin
// position by position against the stamps of the lineup the pin had been
// taken from, so the pin hung on arrival order: two earlier matches' saves
// arriving after the start in the wrong order left the earlier match's
// lineup, and an earlier match's save stamped before a starting-lineup save
// kept the starting lineup's position. Every subset of three saves (the
// starting lineup, the own lineups of P1-0 and P1-1, each changing both
// positions and stamped before P1-2 starts), in every arrival order with the
// start among them, must leave the pin the saves give when all arrive before
// the start.
func TestKachinukiOpener_PinIsTheCarryRuleAtTheStartInEveryArrivalOrder(t *testing.T) {
	const start = int64(1_000_000)
	type save struct {
		match string
		names map[domain.Position]string
		stamp int64
	}
	saves := []save{
		{"", map[domain.Position]string{rv5p1: "Aoki", rv5p2: "Baba"}, start - 50},
		{"P1-0", map[domain.Position]string{rv5p1: "Mori", rv5p2: "Doi"}, start - 100},
		{"P1-1", map[domain.Position]string{rv5p1: "Kato", rv5p2: "Endo"}, start - 200},
	}
	n := 0
	for mask := 0; mask < 1<<len(saves); mask++ {
		var chosen []save
		for i, s := range saves {
			if mask&(1<<i) != 0 {
				chosen = append(chosen, s)
			}
		}
		var want, wantFrom string
		inTime := make([]int, 0, len(chosen)+1)
		for i := range chosen {
			inTime = append(inTime, i)
		}
		// Every save first, then the start: the reference, run first.
		for _, order := range append([][]int{append(inTime, len(chosen))}, rv5Orders(len(chosen)+1)...) {
			eng, store, _ := setupTestEngine(t)
			c := fmt.Sprintf("rv5p%d", n)
			n++
			openerComp(t, store, c,
				state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Kuma", SideAID: "ta", SideBID: "tk", Status: state.MatchStatusScheduled},
				state.MatchResult{ID: "P1-1", SideA: "Taka", SideB: "Washi", SideAID: "ta", SideBID: "tb", Status: state.MatchStatusScheduled},
				state.MatchResult{ID: "P1-2", SideA: "Taka", SideB: "Inu", SideAID: "ta", SideBID: "ti", Status: state.MatchStatusScheduled})
			// The starting lineup the team had before any of these saves.
			require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta",
				Positions: map[domain.Position]string{rv5p1: "Sato", rv5p2: "Ueda"},
				MemberIDs: map[domain.Position]string{rv5p1: "m-sato", rv5p2: "m-ueda"},
				Stamps:    map[domain.Position]int64{rv5p1: start - 1000, rv5p2: start - 1000}}, 3))
			var steps []string
			for _, i := range order {
				if i == len(chosen) {
					steps = append(steps, "start")
					startKachinuki(t, eng, c, "P1-2", start)
					continue
				}
				s := chosen[i]
				steps = append(steps, "save "+s.match)
				rv5Save(t, eng, store, c, s.match, s.names, s.stamp)
			}
			got, from := rv5Pin(t, store, c)
			if want == "" {
				// The first order saves everything before the start: the pin
				// the carry rule gives at the start.
				require.Equal(t, "start", steps[len(steps)-1], "the first order starts last")
				want, wantFrom = got, from
				continue
			}
			require.Equal(t, want, got, "saves %d, order %v", mask, steps)
			require.Equal(t, wantFrom, from, "saves %d, order %v: the lineup the pin came from", mask, steps)
		}
	}
	t.Logf("%d orderings", n)
}

// The lineup a match started with keeps the member id of a position that has
// no name yet (a blank slot picked by number): pinLineup copied only the
// positions with a name entry and silently dropped such an id (stage 6
// review 5, finding 5).
func TestKachinukiOpener_PinKeepsAMemberIDWithNoName(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c = "rv5blank"
	openerComp(t, store, c,
		state.MatchResult{ID: "P1-2", SideA: "Taka", SideB: "Inu", SideAID: "ta", SideBID: "ti", Status: state.MatchStatusScheduled})
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta",
		Positions: map[domain.Position]string{rv5p1: "Sato"},
		MemberIDs: map[domain.Position]string{rv5p1: "m-sato", rv5p2: "m-slot2"}}, 3))
	startKachinuki(t, eng, c, "P1-2", 1_000_000)
	got, _ := rv5Pin(t, store, c)
	require.Contains(t, got, "2=/m-slot2", "the picked, unnamed slot keeps its member: %s", got)
}

// A source taken up whole keeps, at a position it changed only after the
// start, what the pin held there: the source's value at the start is not kept
// anywhere (a lineup keeps no history), and the later change is not one the
// match started with.
func TestKachinukiOpener_PinTakenFromALaterSourceKeepsWhatItHadWhereTheSourceChangedAfterTheStart(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const c, start = "rv5after", int64(1_000_000)
	openerComp(t, store, c,
		state.MatchResult{ID: "P1-0", SideA: "Taka", SideB: "Kuma", SideAID: "ta", SideBID: "tk", Status: state.MatchStatusScheduled},
		state.MatchResult{ID: "P1-2", SideA: "Taka", SideB: "Inu", SideAID: "ta", SideBID: "ti", Status: state.MatchStatusScheduled})
	require.NoError(t, store.SetTeamLineup(c, domain.TeamLineup{TeamID: "ta",
		Positions: map[domain.Position]string{rv5p1: "Sato", rv5p2: "Ueda"},
		MemberIDs: map[domain.Position]string{rv5p1: "m-sato", rv5p2: "m-ueda"},
		Stamps:    map[domain.Position]int64{rv5p1: start - 1000, rv5p2: start - 1000}}, 3))
	startKachinuki(t, eng, c, "P1-2", start)
	rv5Save(t, eng, store, c, "P1-0", map[domain.Position]string{rv5p1: "Zeni"}, start+50)
	got, from := rv5Pin(t, store, c)
	require.Equal(t, "1=Sato/m-sato 2=Ueda/m-ueda", got, "a save made after the start reaches no pin")
	require.Empty(t, from)
	rv5Save(t, eng, store, c, "P1-0", map[domain.Position]string{rv5p2: "Doi"}, start-100)
	got, from = rv5Pin(t, store, c)
	require.Equal(t, "1=Sato/m-sato 2=Doi/m-doi", got)
	require.Equal(t, "P1-0", from)
}
