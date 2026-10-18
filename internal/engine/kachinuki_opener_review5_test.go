package engine

import (
	"cmp"
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
func rv5Save(t *testing.T, eng *Engine, store *state.Store, c, matchID string, names map[domain.Position]string, stamp int64) (copied string) {
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
		// The base the save lands on, as the handler reads it under the lock
		// (lineupSave.base): the starting lineup's stored entry; a match's
		// own stored lineup; else the lineup in force there.
		var base domain.TeamLineup
		found := false
		for _, l := range lineups {
			if l.TeamID == "ta" && l.MatchID == matchID && (matchID != "" || l.Round == 0) {
				base, found = l, true
			}
		}
		if !found && matchID != "" {
			pool, err := tx.LoadPoolMatches(c)
			require.NoError(t, err)
			br, err := tx.LoadBracket(c)
			require.NoError(t, err)
			base = LineupInForceFrom(lineups, pool, br, comp.IsKnockoutEnabled(), "ta", matchID).Lineup
			// A first save of a match's own lineup copies the positions it
			// does not change from the lineup in force as it ARRIVES.
			var parts []string
			for _, p := range slices.SortedFunc(maps.Keys(base.Positions), domain.ComparePositions) {
				if _, ok := names[p]; !ok {
					parts = append(parts, fmt.Sprintf("%s=%s", p, base.Positions[p]))
				}
			}
			copied = strings.Join(parts, " ")
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
	return copied
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
	type save = rv5Step
	full := []save{
		{"", map[domain.Position]string{rv5p1: "Aoki", rv5p2: "Baba"}, start - 50},
		{"P1-0", map[domain.Position]string{rv5p1: "Mori", rv5p2: "Doi"}, start - 100},
		{"P1-1", map[domain.Position]string{rv5p1: "Kato", rv5p2: "Endo"}, start - 200},
	}
	// The same with the P1-1 save PARTIAL: it names one position and, as its
	// match's first own lineup, takes the others from the lineup in force.
	partial := slices.Clone(full)
	partial[2].names = map[domain.Position]string{rv5p1: "Kato"}
	for _, set := range []struct {
		name  string
		saves []save
	}{{"full saves", full}, {"a partial first save", partial}} {
		t.Run(set.name, func(t *testing.T) { rv5PinGate(t, start, set.saves) })
	}
}

// rv5Step is one lineup save of the gate: the match (""; the starting
// lineup), the positions it names and when it was made.
type rv5Step struct {
	match string
	names map[domain.Position]string
	stamp int64
}

// rv5PinGate runs every subset of saves in every arrival order around the
// start of P1-2 and holds the pin to the reference: the saves arriving in the
// order they were made, then the start.
func rv5PinGate(t *testing.T, start int64, saves []rv5Step) {
	type save = rv5Step
	n, marked, diverged := 0, 0, 0
	for mask := 0; mask < 1<<len(saves); mask++ {
		var chosen []save
		for i, s := range saves {
			if mask&(1<<i) != 0 {
				chosen = append(chosen, s)
			}
		}
		var want, wantFrom string
		var wantCopied map[string]string
		// The reference: every save arriving in the order it was made (its
		// stamp), then the start, as the handler would see them in time.
		inTime := make([]int, 0, len(chosen)+1)
		for i := range chosen {
			inTime = append(inTime, i)
		}
		slices.SortFunc(inTime, func(a, b int) int { return cmp.Compare(chosen[a].stamp, chosen[b].stamp) })
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
			copied := map[string]string{}
			for _, i := range order {
				if i == len(chosen) {
					steps = append(steps, "start")
					startKachinuki(t, eng, c, "P1-2", start)
					continue
				}
				s := chosen[i]
				steps = append(steps, "save "+s.match)
				if got := rv5Save(t, eng, store, c, s.match, s.names, s.stamp); got != "" {
					copied[s.match] = got
				}
			}
			got, from := rv5Pin(t, store, c)
			if wantCopied == nil {
				// The first order is the reference.
				require.Equal(t, "start", steps[len(steps)-1], "the first order starts last")
				want, wantFrom, wantCopied = got, from, copied
				continue
			}
			// Finding P (stage 6 review 6, put to the operator, production
			// unchanged): a match's first PARTIAL lineup save copies the
			// positions it does not name from the lineup in force when it
			// ARRIVES, so an order in which that lineup differs then from
			// what it was in time copies something else. Such an order is
			// named here and not held to the reference; every other order is.
			if !maps.Equal(copied, wantCopied) {
				marked++
				if want != got || wantFrom != from {
					diverged++
				}
				t.Logf("finding P (copies the lineup in force on arrival): saves %d, order %v: copied %v, in time %v; pin %q from %q, in time %q from %q",
					mask, steps, copied, wantCopied, got, from, want, wantFrom)
				// Still the carry rule at the start over what the save copied.
				require.Equal(t, wantFrom, from, "saves %d, order %v: the lineup the pin came from", mask, steps)
				if c := copied[from]; c != "" {
					for _, part := range strings.Fields(c) {
						require.Contains(t, got, part+"/", "saves %d, order %v: the pin holds what its source copied", mask, steps)
					}
				}
				continue
			}
			require.Equal(t, want, got, "saves %d, order %v", mask, steps)
			require.Equal(t, wantFrom, from, "saves %d, order %v: the lineup the pin came from", mask, steps)
		}
	}
	t.Logf("%d orderings, %d marked as finding P (%d of them with another pin)", n, marked, diverged)
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
