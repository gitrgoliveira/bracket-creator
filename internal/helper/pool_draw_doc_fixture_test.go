package helper

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The pool draw page carries an interactive walk-through of the descent
// (docs/assets/javascripts/pool-draw-animation.js). It replays the placement
// rules in JavaScript, over example rosters whose drawn pools are recorded in
// the same file, between the BCDA-FIXTURE markers, as valid JSON.
//
// Recorded pools rot: the page would go on describing a draw the application
// no longer makes, and nothing would say so. This test reads that fixture and
// re-runs every roster through the real distributor, so a change to the
// placement rules fails here and names the file to update.
//
// It pins the SETTINGS and the OUTCOME, which is what the page shows. It does
// not pin the JavaScript implementation of the descent; that is verified in a
// browser against these same recorded pools.

const poolDrawFixturePath = "../../docs/assets/javascripts/pool-draw-animation.js"

type docDrawFixture struct {
	Presets []docDrawPreset `json:"presets"`
}

type docDrawPreset struct {
	ID                string        `json:"id"`
	Label             string        `json:"label"`
	PoolSize          int           `json:"poolSize"`
	PoolSizeIsMaximum bool          `json:"poolSizeIsMaximum"`
	Courts            int           `json:"courts"`
	PoolWinners       int           `json:"poolWinners"`
	PoolNames         []string      `json:"poolNames"`
	PoolSizes         []int         `json:"poolSizes"`
	WinnerSlots       []int         `json:"winnerSlots"`
	Roster            []docDrawEntr `json:"roster"`
	PoolsAfterDescent [][]string    `json:"poolsAfterDescent"`
	Exchanges         []docDrawSwap `json:"exchanges"`
	Pools             [][]string    `json:"pools"`
}

type docDrawEntr struct {
	Name string `json:"name"`
	Dojo string `json:"dojo"`
}

type docDrawSwap struct {
	A string `json:"a"`
	B string `json:"b"`
}

// loadDocDrawFixture slices the JSON out of the widget's source. The markers
// and the single `var NAME = ...;` wrapper are the whole contract; keeping the
// fixture inside the file it serves means the page and its pin cannot drift
// apart in a copy step.
func loadDocDrawFixture(t *testing.T) docDrawFixture {
	t.Helper()
	raw, err := os.ReadFile(poolDrawFixturePath)
	require.NoError(t, err, "the pool draw page's walk-through must exist at %s", poolDrawFixturePath)

	const startMark, endMark = "/* BCDA-FIXTURE-START */", "/* BCDA-FIXTURE-END */"
	src := string(raw)
	start := strings.Index(src, startMark)
	end := strings.Index(src, endMark)
	require.GreaterOrEqual(t, start, 0, "fixture start marker missing from %s", poolDrawFixturePath)
	require.Greater(t, end, start, "fixture end marker missing or before the start marker")

	body := strings.TrimSpace(src[start+len(startMark) : end])
	body = strings.TrimPrefix(body, "var BCDA_FIXTURE =")
	body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), ";"))

	var fixture docDrawFixture
	require.NoError(t, json.Unmarshal([]byte(body), &fixture),
		"the block between the BCDA-FIXTURE markers must stay valid JSON")
	require.NotEmpty(t, fixture.Presets)
	return fixture
}

func docDrawPlayers(preset docDrawPreset) []Player {
	players := make([]Player, len(preset.Roster))
	for i, e := range preset.Roster {
		players[i] = Player{Name: e.Name, Dojo: e.Dojo}
	}
	return players
}

func docDrawNames(pools []Pool) [][]string {
	out := make([][]string, len(pools))
	for i, p := range pools {
		out[i] = make([]string, len(p.Players))
		for j, pl := range p.Players {
			out[i][j] = pl.Name
		}
	}
	return out
}

func TestPoolDrawDocWalkthroughMatchesTheDraw(t *testing.T) {
	fixture := loadDocDrawFixture(t)

	for _, preset := range fixture.Presets {
		t.Run(preset.ID, func(t *testing.T) {
			players := docDrawPlayers(preset)
			require.NotEmpty(t, players)

			// The widget is handed the pool sizes and the winner slot of each
			// pool rather than deriving them, so both have to be the real ones.
			_, base, err := poolTargetSizes(len(players), preset.PoolSize, preset.PoolSizeIsMaximum)
			require.NoError(t, err)
			sizes := realTargetSizes(base, len(players))
			assert.Equal(t, preset.PoolSizes, sizes, "recorded pool sizes")
			require.Len(t, preset.PoolNames, len(sizes), "one name per pool")

			slots := treeAwareQualifierSlots(sizes, preset.PoolWinners, preset.Courts, qualifierMode{})
			require.Len(t, slots, len(sizes))
			winners := make([]int, len(slots))
			for i, s := range slots {
				require.Len(t, s, 1, "pool %d should send exactly one winner up", i)
				winners[i] = s[0]
			}
			assert.Equal(t, preset.WinnerSlots, winners, "recorded winner slots")

			// Stage one: the descent, which is the stage the widget animates.
			descended := make([]Pool, len(sizes))
			for i := range descended {
				descended[i] = Pool{PoolName: preset.PoolNames[i]}
			}
			require.NoError(t, assignUnseededByDojoTree(descended, sizes, players, slots, make(dojoKeyCache)))
			assert.Equal(t, preset.PoolsAfterDescent, docDrawNames(descended),
				"recorded pools after the descent")

			// Stage two: the exchange pass, which the widget reports rather
			// than replays, so the exchanges it names have to be the real ones.
			exchanged := make([]Pool, len(descended))
			for i := range descended {
				exchanged[i] = Pool{
					PoolName: descended[i].PoolName,
					Players:  append([]Player(nil), descended[i].Players...),
				}
			}
			improveDojoMeetings(exchanged, slots, make(dojoKeyCache))
			assert.Equal(t, docDrawSwaps(preset, descended, exchanged), preset.Exchanges,
				"recorded exchanges")

			// And the whole draw, the pools the page finally shows.
			pools, _, err := BuildPoolPhaseTreeAware(players, preset.PoolSize,
				preset.PoolSizeIsMaximum, preset.Courts, preset.PoolWinners)
			require.NoError(t, err)
			assert.Equal(t, preset.Pools, docDrawNames(pools), "recorded final pools")
			for i, p := range pools {
				assert.Equal(t, preset.PoolNames[i], p.PoolName, "recorded name of pool %d", i)
			}
		})
	}
}

// docDrawSwaps reports the exchange pass's effect the way the page states it:
// the pairs of competitors that traded pools. It fails the test rather than
// guessing if the movement is not a set of clean pairs, since the page has no
// way to describe anything else.
func docDrawSwaps(preset docDrawPreset, before, after []Pool) []docDrawSwap {
	poolOf := func(pools []Pool) map[string]int {
		m := make(map[string]int)
		for i, p := range pools {
			for _, pl := range p.Players {
				m[pl.Name] = i
			}
		}
		return m
	}
	from, to := poolOf(before), poolOf(after)

	moved := make([]string, 0, 4)
	for name, was := range from {
		if to[name] != was {
			moved = append(moved, name)
		}
	}
	sort.Strings(moved)

	swaps := make([]docDrawSwap, 0, len(moved)/2)
	taken := make(map[string]bool, len(moved))
	for _, a := range moved {
		if taken[a] {
			continue
		}
		for _, b := range moved {
			if b == a || taken[b] {
				continue
			}
			if from[a] == to[b] && from[b] == to[a] {
				taken[a], taken[b] = true, true
				swaps = append(swaps, docDrawSwap{A: a, B: b})
				break
			}
		}
	}
	for _, name := range moved {
		if !taken[name] {
			panic(fmt.Sprintf("preset %s: %s moved pools without a partner, which the page "+
				"cannot describe as an exchange", preset.ID, name))
		}
	}
	return swaps
}

// The pool draw allocates a placeholder seat per pool seat while it works out
// which knockout slot each pool feeds, so a pool size has to be bounded by
// something stated rather than by whatever number reached the arithmetic
// (CodeQL go/uncontrolled-allocation-size). MaxPoolSize is that bound; these
// pin both ends of it, since a ceiling nothing tests is a ceiling a later
// change deletes.
func TestPoolSizeCeiling(t *testing.T) {
	t.Run("poolTargetSizes refuses a pool at the ceiling and says so", func(t *testing.T) {
		_, _, err := poolTargetSizes(MaxPoolSize*4, MaxPoolSize, true)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "pool size must be less than")

		// One under it still draws, so the guard bounds without narrowing.
		_, sizes, err := poolTargetSizes(MaxPoolSize*2, MaxPoolSize-1, true)
		require.NoError(t, err)
		require.NotEmpty(t, sizes)
		for i, size := range sizes {
			assert.LessOrEqual(t, size, MaxPoolSize, "pool %d", i)
		}
	})

	t.Run("the skeleton refuses an oversized pool at the allocation", func(t *testing.T) {
		// Reachable only by a caller that skipped poolTargetSizes, which is
		// exactly why the bound is asserted here too.
		assert.Nil(t, buildQualifierSkeleton([]int{4, MaxPoolSize + 1}),
			"a pool over the ceiling must refuse the shape, not allocate it")
		assert.Nil(t, poolQualifierPaths([]int{4, MaxPoolSize + 1}, 1, 1))

		ok := buildQualifierSkeleton([]int{4, MaxPoolSize})
		require.Len(t, ok, 2)
		assert.Len(t, ok[1].Players, MaxPoolSize)
	})

	t.Run("a real tournament is nowhere near the ceiling", func(t *testing.T) {
		_, sizes, err := poolTargetSizes(512, 4, true)
		require.NoError(t, err)
		for _, size := range sizes {
			assert.LessOrEqual(t, size, 4)
		}
	})
}

// realTargetSizes spreads a min-mode remainder across the pools, taking the
// two outermost first and working inward (0, last, 1, last-1, ...), and now
// does it over counts rather than by fabricating a player per seat. Its only other coverage is indirect (the goldens and the gate
// scorecard), so the spread itself is pinned here: a walk that drifts would
// otherwise surface as an unexplained golden diff.
func TestRealTargetSizesSpreadsTheRemainderFromTheEndsInward(t *testing.T) {
	for _, tc := range []struct {
		base       []int
		numPlayers int
		want       []int
	}{
		{[]int{3, 3, 3}, 9, []int{3, 3, 3}},  // nothing left over
		{[]int{3, 3, 3}, 10, []int{4, 3, 3}}, // one over: the outermost pool
		{[]int{3, 3, 3}, 11, []int{4, 3, 4}}, // two: then the other end
		{[]int{4, 4, 4, 4}, 18, []int{5, 4, 4, 5}},
		{[]int{4, 4, 4, 4, 4}, 23, []int{5, 5, 4, 4, 5}}, // 0, then 4, then 1: ends first, inward
		{[]int{5}, 7, []int{7}},                          // a lone pool takes them all
	} {
		got := realTargetSizes(append([]int(nil), tc.base...), tc.numPlayers)
		assert.Equal(t, tc.want, got, "base %v with %d players", tc.base, tc.numPlayers)
		sum := 0
		for _, n := range got {
			sum += n
		}
		assert.Equal(t, tc.numPlayers, sum, "every player must get a seat")
	}
}
