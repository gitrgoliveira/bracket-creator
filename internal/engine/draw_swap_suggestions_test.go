package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures ---------------------------------------------------------------

// koDraw is a knockout-only draw of len(roster) leaves filled top to bottom,
// numbered as the draw numbers it (first round first), with the roster's ids
// as the DrawOrder so the sides carry numbers when the competition does.
func koDraw(roster []domain.Player) *state.Bracket {
	leaves := len(roster)
	var rounds [][]state.BracketMatch
	number := 0
	for size, r := leaves/2, 0; size >= 1; size, r = size/2, r+1 {
		var round []state.BracketMatch
		for m := 0; m < size; m++ {
			number++
			bm := state.BracketMatch{ID: fmt.Sprintf("m-r%d-%d", r+1, m), MatchNumber: number, DisplayRound: size}
			if r == 0 {
				a, b := roster[2*m], roster[2*m+1]
				bm.SideA, bm.SideAID, bm.SideB, bm.SideBID = a.Name, a.ID, b.Name, b.ID
			}
			round = append(round, bm)
		}
		rounds = append(rounds, round)
	}
	order := make([]string, len(roster))
	for i, p := range roster {
		order[i] = p.ID
	}
	return &state.Bracket{Rounds: rounds, DrawOrder: order}
}

// koRoster names one competitor per dojo in dojos, slot by slot ("s0", ...).
func koRoster(dojos ...string) []domain.Player {
	out := make([]domain.Player, len(dojos))
	for i, d := range dojos {
		out[i] = domain.Player{ID: fmt.Sprintf("s%d", i), Name: fmt.Sprintf("S%d", i), Dojo: d}
	}
	return out
}

func drawReady(format string, opts ...func(*state.Competition)) *state.Competition {
	c := &state.Competition{ID: "c", Format: format, Status: state.CompStatusDrawReady}
	for _, o := range opts {
		o(c)
	}
	return c
}

// suggestedPairs is each suggestion's two ids.
func suggestedPairs(got SwapSuggestions) [][2]string {
	out := [][2]string{}
	for _, s := range got.Suggestions {
		out = append(out, [2]string{s.A.ID, s.B.ID})
	}
	return out
}

// indexOf is the entrant index of id.
func indexOf(t *testing.T, s *swapSuggester, id string) int {
	t.Helper()
	for i, e := range s.entrants {
		if e.c.side.ID == id {
			return i
		}
	}
	t.Fatalf("%s is not drawn", id)
	return -1
}

// rowsByPair keys a summary by its unordered pairs.
func rowsByPair(rows []DojoMeeting) map[[2]string]DojoMeeting {
	out := map[[2]string]DojoMeeting{}
	for _, dm := range rows {
		out[pairIDs(dm.A.ID, dm.B.ID)] = dm
	}
	return out
}

// checkAgainstTheSummary is the oracle: for EVERY pair of drawn competitors,
// the incremental search's verdict, and for an accepted one each pair it
// says meets later with the row it says the summary would show, equal what
// DojoSeparation answers on the draw planSwap's dry run writes in memory.
// It returns how many candidates were accepted.
func checkAgainstTheSummary(t *testing.T, comp *state.Competition, pools []helper.Pool, poolMatches []state.MatchResult, bracket *state.Bracket, roster []domain.Player) int {
	t.Helper()
	summary := DojoSeparation(comp, pools, bracket, roster)
	s := newSwapSuggester(comp, pools, bracket, roster, summary)
	require.NotNil(t, s)
	snap := swapSnapshot{comp: comp, pools: pools, poolMatches: poolMatches, bracket: bracket, roster: roster}
	before := rowsByPair(summary)
	rounds := len(bracket.Rounds)
	accepted := 0
	for x := range s.entrants {
		for y := x + 1; y < len(s.entrants); y++ {
			improves, ok := s.evaluate(x, y)
			ex, ey := s.entrants[x], s.entrants[y]
			if ex.seed > 0 || ey.seed > 0 || ex.group == ey.group {
				require.False(t, ok, "%s and %s are no candidate", ex.c.side.ID, ey.c.side.ID)
				continue
			}
			plan, err := planSwap(snap, SwapRequest{
				A:      SwapSide{ID: ex.c.side.ID, Place: clonePlace(ex.place)},
				B:      SwapSide{ID: ey.c.side.ID, Place: clonePlace(ey.place)},
				DryRun: true,
			})
			require.NoError(t, err)
			require.False(t, plan.answer.BracketRebuilt, "an unseeded swap rebuilds nothing")
			after := rowsByPair(DojoSeparation(comp, plan.pools, plan.bracket, roster))
			require.Len(t, after, len(before), "a swap keeps every pair")

			var want []SwapImprovement
			earlier := false
			for k, b := range before {
				a, has := after[k]
				require.True(t, has, "pair %v after the swap", k)
				switch c := rankMeeting(a, rounds).compare(rankMeeting(b, rounds)); {
				case c < 0:
					earlier = true
				case c > 0:
					want = append(want, SwapImprovement{Dojo: b.Dojo, Before: b, After: a})
				}
			}
			label := fmt.Sprintf("swap %s and %s", ex.c.side.ID, ey.c.side.ID)
			require.Equal(t, !earlier && len(want) > 0, ok, label)
			if !ok {
				continue
			}
			accepted++
			got := make([]SwapImprovement, len(improves))
			for i, imp := range improves {
				got[i] = imp.SwapImprovement
			}
			byPair := func(list []SwapImprovement) {
				sort.Slice(list, func(i, j int) bool {
					a, b := pairIDs(list[i].Before.A.ID, list[i].Before.B.ID), pairIDs(list[j].Before.A.ID, list[j].Before.B.ID)
					return a[0]+"|"+a[1] < b[0]+"|"+b[1]
				})
			}
			byPair(want)
			byPair(got)
			require.Equal(t, want, got, label)
		}
	}
	return accepted
}

// --- the pure search on hand-built draws ------------------------------------

func TestSwapSuggestions_SplitsASharedPool(t *testing.T) {
	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "a1", Name: "A1", Dojo: "Omega", Number: "K1"},
			{ID: "a2", Name: "A2", Dojo: "omega ", Number: "K2"},
			{ID: "a3", Name: "A3", Dojo: "Solo A", Number: "K3"},
		}},
		{PoolName: "Pool B", Players: []helper.Player{
			{ID: "b1", Name: "B1", Dojo: "Solo B", Number: "K4"},
			{ID: "b2", Name: "B2", Dojo: "Solo C", Number: "K5"},
			{ID: "b3", Name: "B3", Dojo: "Solo D", Number: "K6"},
		}},
	}
	bracket := handBracket([4]string{"Pool A-1st", "Pool B-2nd", "Pool B-1st", "Pool A-2nd"})
	comp := drawReady(state.CompFormatMixed, func(c *state.Competition) { c.NumberPrefix = "K" })

	got := SuggestSwaps(comp, pools, bracket, nil)
	// Moving either Omega competitor to Pool B splits them; six such swaps
	// fix the same meeting by the same amount, so the ids order them and
	// the answer holds five.
	assert.Equal(t, [][2]string{{"a1", "b1"}, {"a1", "b2"}, {"a1", "b3"}, {"a2", "b1"}, {"a2", "b2"}}, suggestedPairs(got))
	first := got.Suggestions[0]
	assert.Equal(t, SuggestedSide{ID: "a1", Name: "A1", Number: "K1", Dojo: "Omega", Place: DrawPlace{Pool: "Pool A", Position: 1}}, first.A)
	assert.Equal(t, SuggestedSide{ID: "b1", Name: "B1", Number: "K4", Dojo: "Solo B", Place: DrawPlace{Pool: "Pool B", Position: 1}}, first.B)
	require.Len(t, first.Improves, 1)
	imp := first.Improves[0]
	assert.Equal(t, "Omega", imp.Dojo)
	assert.Equal(t, "Pool A", imp.Before.SamePool)
	assert.Empty(t, imp.After.SamePool)
	require.NotNil(t, imp.After.Earliest)
	assert.Equal(t, 0, imp.After.Earliest.Round, "split, they can meet in the first round")
	require.NotNil(t, imp.After.IfBothWin)
	assert.Equal(t, 1, imp.After.IfBothWin.Round, "and in the final if both win their pools")
	assert.Equal(t, "a2", imp.After.A.ID, "a2 is now the earlier in the draw")
	assert.Equal(t, DojoSide{ID: "a1", Name: "A1", Number: "K4", Pool: "Pool B"}, imp.After.B, "a1 takes b1's place and its number")
	assert.Empty(t, got.Stuck, "Omega has a fix")
	assert.Positive(t, checkAgainstTheSummary(t, comp, pools, nil, bracket, nil))

	t.Run("a seeded competitor is never suggested", func(t *testing.T) {
		seeded := clonePools(pools)
		seeded[1].Players[2].Seed = 1
		seeded[0].Players[0].Seed = 2
		got := SuggestSwaps(comp, seeded, bracket, nil)
		assert.Equal(t, [][2]string{{"a2", "b1"}, {"a2", "b2"}}, suggestedPairs(got))
		checkAgainstTheSummary(t, comp, seeded, nil, bracket, nil)
	})
}

func TestSwapSuggestions_NobodyElseMeetsEarlier(t *testing.T) {
	// Eight leaves: Omega meets in the first round (slots 0 and 1), Kappa
	// only in the final (slots 2 and 4).
	roster := koRoster("Omega", "Omega", "Kappa", "Solo 3", "Kappa", "Solo 5", "Solo 6", "Solo 7")
	bracket := koDraw(roster)
	comp := drawReady(state.CompFormatKnockout)
	s := newSwapSuggester(comp, nil, bracket, roster, DojoSeparation(comp, nil, bracket, roster))
	require.NotNil(t, s)

	_, ok := s.evaluate(indexOf(t, s, "s1"), indexOf(t, s, "s4"))
	assert.False(t, ok, "Omega would meet only in the final, but Kappa would meet in the semi-final")
	_, ok = s.evaluate(indexOf(t, s, "s0"), indexOf(t, s, "s1"))
	assert.False(t, ok, "two competitors of one first-round match are no candidate")
	_, ok = s.evaluate(indexOf(t, s, "s5"), indexOf(t, s, "s6"))
	assert.False(t, ok, "a swap of two competitors with no dojo-mate changes nothing")
	improves, ok := s.evaluate(indexOf(t, s, "s1"), indexOf(t, s, "s3"))
	require.True(t, ok)
	require.Len(t, improves, 1)
	assert.Equal(t, 1, improves[0].After.Earliest.Round, "to the other quarter: the semi-final")

	got := SuggestSwaps(comp, nil, bracket, roster)
	// Into the other half is two rounds later, which beats the other quarter.
	assert.Equal(t, [][2]string{{"s0", "s5"}, {"s0", "s6"}, {"s0", "s7"}, {"s1", "s5"}, {"s1", "s6"}}, suggestedPairs(got))
	for _, sug := range got.Suggestions {
		require.Len(t, sug.Improves, 1)
		assert.Equal(t, "Omega", sug.Improves[0].Dojo)
		assert.Equal(t, 0, sug.Improves[0].Before.Earliest.Round)
		assert.Equal(t, 2, sug.Improves[0].After.Earliest.Round)
		assert.NotNil(t, sug.B.Place.Slot)
	}
	assert.Empty(t, got.Stuck, "Omega has a fix and Kappa meets only in the final")
	assert.Positive(t, checkAgainstTheSummary(t, comp, nil, nil, bracket, roster))
}

func TestSwapSuggestions_RankedByTheMeetingTheyFix(t *testing.T) {
	// Omega shares Pool A; Kappa meets in the first round (B-1st v C-2nd).
	// Splitting Omega comes first, though it fixes less than Kappa's swaps.
	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{ID: "a1", Dojo: "Omega"}, {ID: "a2", Dojo: "Omega"}}},
		{PoolName: "Pool B", Players: []helper.Player{{ID: "b1", Dojo: "Kappa"}, {ID: "b2", Dojo: "Solo B"}}},
		{PoolName: "Pool C", Players: []helper.Player{{ID: "c1", Dojo: "Kappa"}, {ID: "c2", Dojo: "Solo C"}}},
		{PoolName: "Pool D", Players: []helper.Player{{ID: "d1", Dojo: "Solo D1"}, {ID: "d2", Dojo: "Solo D2"}}},
	}
	labels := []string{"Pool A-1st", "Pool D-2nd", "Pool B-1st", "Pool C-2nd", "Pool C-1st", "Pool B-2nd", "Pool D-1st", "Pool A-2nd"}
	roster := make([]domain.Player, len(labels))
	for i := range roster {
		roster[i] = domain.Player{ID: fmt.Sprintf("x%d", i)}
	}
	bracket := koDraw(roster)
	bracket.DrawOrder = nil
	for mi := range bracket.Rounds[0] {
		m := &bracket.Rounds[0][mi]
		m.SideA, m.SideAID, m.SideB, m.SideBID = labels[2*mi], "", labels[2*mi+1], ""
		m.PlaceholderA, m.PlaceholderB = labels[2*mi], labels[2*mi+1]
	}
	comp := drawReady(state.CompFormatMixed)
	got := SuggestSwaps(comp, pools, bracket, nil)
	// Every swap moving an Omega competitor out of Pool A comes first. Into
	// Pool C is later still if both win (the final, not the semi-final), and
	// a swap with c1 also takes Kappa's first-round meeting to the
	// semi-final, so it improves two pairs.
	assert.Equal(t, [][2]string{{"a1", "c1"}, {"a2", "c1"}, {"a1", "c2"}, {"a2", "c2"}, {"a1", "b1"}}, suggestedPairs(got))
	var dojos []string
	for _, imp := range got.Suggestions[0].Improves {
		dojos = append(dojos, imp.Dojo)
	}
	assert.Equal(t, []string{"Omega", "Kappa"}, dojos, "the shared pool first")
	assert.Equal(t, "Pool A", got.Suggestions[0].Improves[0].Before.SamePool)
	assert.Equal(t, got, SuggestSwaps(comp, pools, bracket, nil), "the same draw answers the same list")
	assert.Positive(t, checkAgainstTheSummary(t, comp, pools, nil, bracket, nil))
}

func TestSwapSuggestions_StuckDojo(t *testing.T) {
	// Three of one dojo in four leaves: two of them always share a
	// first-round match, so no swap helps without another pair meeting
	// earlier.
	roster := koRoster("Omega", "Omega", "Omega", "Solo")
	bracket := koDraw(roster)
	comp := drawReady(state.CompFormatKnockout)
	got := SuggestSwaps(comp, nil, bracket, roster)
	assert.Empty(t, got.Suggestions)
	require.Len(t, got.Stuck, 1)
	assert.Equal(t, "Omega", got.Stuck[0].Dojo)
	assert.Equal(t, "s0", got.Stuck[0].Meeting.A.ID)
	assert.Equal(t, "s1", got.Stuck[0].Meeting.B.ID)
	assert.Equal(t, 0, got.Stuck[0].Meeting.Earliest.Round, "the dojo's earliest meeting")
	assert.Zero(t, checkAgainstTheSummary(t, comp, nil, nil, bracket, roster))

	// A pair that meets only in the final is not stuck: there is nothing
	// later to move it to.
	roster = koRoster("Omega", "Solo 1", "Solo 2", "Omega")
	got = SuggestSwaps(comp, nil, koDraw(roster), roster)
	assert.Empty(t, got.Suggestions)
	assert.Empty(t, got.Stuck)
}

func TestSwapSuggestions_AHelpPastTheCapIsNeitherSuggestedNorHidden(t *testing.T) {
	// Eight dojos each hold a first-round match to themselves, so far more than
	// maxSwapSuggestions swaps help. Omega's pair is helped only by swaps that
	// rank below the cap: the card shows none of them, so the dojo must be
	// listed as stuck rather than left out of both lists.
	roster := koRoster("A", "A", "B", "B", "Omega", "Omega", "C", "C", "D", "D", "E", "E", "F", "F", "G", "G")
	got := SuggestSwaps(drawReady(state.CompFormatKnockout), nil, koDraw(roster), roster)
	require.Len(t, got.Suggestions, maxSwapSuggestions)
	for _, sug := range got.Suggestions {
		for _, imp := range sug.Improves {
			assert.NotEqual(t, "Omega", imp.Dojo, "no shown suggestion helps Omega")
		}
	}
	var stuck []string
	for _, st := range got.Stuck {
		stuck = append(stuck, st.Dojo)
	}
	assert.Contains(t, stuck, "Omega")
}

func TestSwapSuggestions_StuckDojoNamesThePairNoSwapHelps(t *testing.T) {
	// Omega holds slots 0, 2, 4 and 5. Swaps can move the pair at 4 and 5
	// apart, but none helps the pair at slots 0 and 2, which meet in the
	// semi-final: moving either of them brings it next to another Omega and
	// that pair meets sooner. The dojo is stuck on that pair even though other
	// pairs of it have a fix.
	roster := koRoster("Omega", "Solo 1", "Omega", "Solo 3", "Omega", "Omega", "Solo 6", "Solo 7")
	bracket := koDraw(roster)
	comp := drawReady(state.CompFormatKnockout)
	got := SuggestSwaps(comp, nil, bracket, roster)
	require.NotEmpty(t, got.Suggestions, "some pair of the dojo has a fix")
	require.Len(t, got.Stuck, 1)
	assert.Equal(t, "Omega", got.Stuck[0].Dojo)
	assert.Equal(t, "s0", got.Stuck[0].Meeting.A.ID)
	assert.Equal(t, "s2", got.Stuck[0].Meeting.B.ID)
	for _, sug := range got.Suggestions {
		for _, imp := range sug.Improves {
			assert.NotEqual(t, pairIDs("s0", "s2"), pairIDs(imp.Before.A.ID, imp.Before.B.ID), "no suggestion helps the stuck pair")
		}
	}
	assert.Positive(t, checkAgainstTheSummary(t, comp, nil, nil, bracket, roster))
}

func TestSwapSuggestions_ReportsAByeThatChangesHands(t *testing.T) {
	// Eight leaves with S1 missing, so S0 holds the first-round bye. Omega's
	// S0 and S3 meet in the semi-final. Moving S0 to S4's match keeps them
	// apart until the final, takes S0's bye away and gives it to S4, who
	// takes S0's place alone in a match.
	roster := koRoster("Omega", "Solo 1", "Solo 2", "Omega", "Solo 4", "Solo 5", "Solo 6", "Solo 7")
	bracket := koDraw(roster)
	bracket.Rounds[0][0].SideB, bracket.Rounds[0][0].SideBID = "", ""
	comp := drawReady(state.CompFormatKnockout)
	got := SuggestSwaps(comp, nil, bracket, roster)

	byPair := map[[2]string]SwapSuggestion{}
	for _, sug := range got.Suggestions {
		byPair[[2]string{sug.A.ID, sug.B.ID}] = sug
	}
	require.Contains(t, byPair, [2]string{"s0", "s4"})
	assert.Equal(t, []SwapBye{{ID: "s0", Name: "S0", Bye: false}, {ID: "s4", Name: "S4", Bye: true}}, byPair[[2]string{"s0", "s4"}].Byes)
	for pair, sug := range byPair {
		if pair[0] != "s0" && pair[1] != "s0" {
			assert.Empty(t, sug.Byes, "a swap between two matches that both hold two competitors hands over no bye: %v", pair)
		}
	}

	// A pools draw has no byes to hand over.
	assert.Nil(t, (&swapSuggester{}).byes(0, 1))
}

func TestSwapSuggestions_EmptyAnswers(t *testing.T) {
	pools := []helper.Pool{{PoolName: "Pool A", Players: []helper.Player{{ID: "a1", Dojo: "Omega"}, {ID: "a2", Dojo: "Omega"}}}}
	for _, comp := range []*state.Competition{
		nil,
		{ID: "l", Format: state.CompFormatLeague, Status: state.CompStatusDrawReady},
		{ID: "r", Format: state.CompFormatMixed, Status: state.CompStatusPools},
		{ID: "s", Format: state.CompFormatMixed, Status: "setup"},
	} {
		got := SuggestSwaps(comp, pools, handBracket([4]string{"Pool A-1st", "", "", "Pool A-2nd"}), nil)
		raw, err := json.Marshal(got)
		require.NoError(t, err)
		assert.JSONEq(t, `{"suggestions":[],"stuck":[]}`, string(raw), "lists, never null")
	}
}

// --- generated draws through the engine ---------------------------------------

func TestSwapSuggestions_GeneratedDrawsMatchTheSummary(t *testing.T) {
	dojos := []string{"Omega", "Omega", "Omega", "Kappa", "Kappa", "Pi", "Pi", "Pi", "Pi", "Rho", "Rho"}
	dojos = append(dojos, uniqueDojos(10)...)

	t.Run("pools and knockout", func(t *testing.T) {
		for _, winners := range []int{1, 2} {
			eng, store, _ := setupTestEngine(t)
			compID := fmt.Sprintf("sug-mixed-%d", winners)
			drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, dojos, func(c *state.Competition) {
				c.PoolWinners = winners
				c.NumberPrefix = "K"
			})
			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			pools := loadSwapPools(t, store, compID)
			checkAgainstTheSummary(t, comp, pools, loadSwapMatches(t, store, compID), loadBracket(t, store, compID), nil)

			// Put two Pi competitors in one pool by hand: a suggestion splits
			// them again.
			pi := map[int]int{}
			var a, b SwapSide
			for pi0, p := range pools {
				for row, pl := range p.Players {
					if pl.Dojo == "Pi" {
						pi[pi0] = row
					}
				}
			}
			var poolsWithPi []int
			for p := range pi {
				poolsWithPi = append(poolsWithPi, p)
			}
			sort.Ints(poolsWithPi)
			require.GreaterOrEqual(t, len(poolsWithPi), 2)
			a = poolSwapSide(pools, poolsWithPi[0], pi[poolsWithPi[0]])
			target := poolsWithPi[1]
			other := (pi[target] + 1) % len(pools[target].Players)
			b = poolSwapSide(pools, target, other)
			require.NotEqual(t, "Pi", pools[target].Players[other].Dojo, "fixture: no pool holds two Pi")
			_, err = eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
			require.NoError(t, err)
			comp, err = store.LoadCompetition(compID)
			require.NoError(t, err)
			pools = loadSwapPools(t, store, compID)
			bracket := loadBracket(t, store, compID)
			assert.Positive(t, checkAgainstTheSummary(t, comp, pools, loadSwapMatches(t, store, compID), bracket, nil))

			got, err := eng.SwapSuggestionsFor(comp)
			require.NoError(t, err)
			assert.Equal(t, SuggestSwaps(comp, pools, bracket, nil), *got)
			// Pi now shares a pool: either a suggestion splits them, ranked
			// first, or no swap can without another Pi pair meeting earlier,
			// and Pi is stuck on that shared pool.
			if len(got.Suggestions) > 0 && got.Suggestions[0].Improves[0].Before.SamePool != "" {
				assert.Equal(t, "Pi", got.Suggestions[0].Improves[0].Dojo)
			} else {
				var stuck []string
				for _, st := range got.Stuck {
					stuck = append(stuck, st.Dojo+"|"+st.Meeting.SamePool)
				}
				assert.Contains(t, stuck, "Pi|"+b.Place.Pool)
			}
		}
	})

	t.Run("knockout with byes", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		const compID = "sug-ko"
		drawDojoComp(t, eng, store, compID, state.CompFormatKnockout, 3, dojos[:13], func(c *state.Competition) { c.NumberPrefix = "K" })
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		roster, err := store.LoadParticipants(compID, comp.EffectiveWithZekkenName())
		require.NoError(t, err)
		bracket := loadBracket(t, store, compID)
		require.NotEmpty(t, byeHolders(bracket), "fixture must hold a bye")
		checkAgainstTheSummary(t, comp, nil, nil, bracket, roster)
		got, err := eng.SwapSuggestionsFor(comp)
		require.NoError(t, err)
		assert.Equal(t, SuggestSwaps(comp, nil, bracket, roster), *got)
	})

	t.Run("a started competition has none", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		const compID = "sug-started"
		drawDojoComp(t, eng, store, compID, state.CompFormatKnockout, 3, dojos[:8])
		require.NoError(t, eng.StartCompetition(compID))
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		got, err := eng.SwapSuggestionsFor(comp)
		require.NoError(t, err)
		assert.Equal(t, &SwapSuggestions{Suggestions: []SwapSuggestion{}, Stuck: []StuckDojo{}}, got)
	})
}

func TestSwapSuggestions_TimingFor256(t *testing.T) {
	// FR8: a 256-competitor draw answers within one second on a normal
	// machine (about 40 ms measured locally). The assertion allows ten
	// seconds because a loaded CI runner under the race detector measured
	// 1.4 s, so it only catches a slowdown of an order of magnitude, such as
	// a candidate search gone quadratic, not machine noise. Sixteen dojos of
	// sixteen is the heavy case: every candidate rescores thirty pairs.
	const budget = 10 * time.Second
	dojos := make([]string, 256)
	for i := range dojos {
		dojos[i] = fmt.Sprintf("Dojo %02d", i%16)
	}
	for _, format := range []string{state.CompFormatMixed, state.CompFormatKnockout} {
		t.Run(format, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "sug-256-" + format
			drawDojoComp(t, eng, store, compID, format, 4, dojos)
			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			best := time.Duration(1 << 62)
			for range 3 {
				start := time.Now()
				_, err := eng.SwapSuggestionsFor(comp)
				require.NoError(t, err)
				best = min(best, time.Since(start))
			}
			t.Logf("%s: best of three %v", format, best)
			assert.Less(t, best, budget)
		})
	}
}
