package engine

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures ---------------------------------------------------------------

// drawCandidatesComp draws a competition of one competitor per entry of
// dojos (P01, P02, ...), the first `seeds` of them seeded 1, 2, ... in roster
// order, and answers it loaded.
func drawCandidatesComp(t *testing.T, eng *Engine, store *state.Store, compID, format string, poolSize int, dojos []string, seeds int, opts ...func(*state.Competition)) *state.Competition {
	t.Helper()
	createTestCompetition(t, store, compID, format, poolSize, opts...)
	players := make([]domain.Player, len(dojos))
	for i, d := range dojos {
		players[i] = domain.Player{Name: fmt.Sprintf("P%02d", i+1), Dojo: d}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	var assignments []domain.SeedAssignment
	for r := 1; r <= seeds; r++ {
		assignments = append(assignments, domain.SeedAssignment{Name: players[r-1].Name, Dojo: players[r-1].Dojo, SeedRank: r})
	}
	if len(assignments) > 0 {
		require.NoError(t, store.SaveSeeds(compID, assignments))
	}
	require.NoError(t, eng.GenerateDraw(compID))
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusDrawReady, comp.Status)
	return comp
}

// browserLateness is meetingLateness in web-mobile/js/admin_draw_swap_check.jsx,
// ported as it is, so the oracle below judges a meeting the way the
// organiser's dialog does: a shared pool earliest, a pair that cannot meet
// latest, then rounds from the end, the if-both-win meeting breaking a tie.
func browserLateness(row DojoMeeting) [2]int {
	fromEnd := func(m *BracketMeeting) int {
		if m.DisplayRound > 0 {
			return m.DisplayRound - 1
		}
		return m.Rounds - 1 - m.Round
	}
	switch {
	case row.SamePool != "":
		return [2]int{-1000, -1000}
	case row.Earliest == nil:
		return [2]int{1000, 1000}
	case row.IfBothWin != nil:
		return [2]int{-fromEnd(row.Earliest), -fromEnd(row.IfBothWin)}
	}
	return [2]int{-fromEnd(row.Earliest), -fromEnd(row.Earliest)}
}

// browserVerdict is the verdict the swap's dry run answer implies, read the
// browser's way (swapVerdict's separationChanges, ported): each dojo's pairs,
// matched by their two ids, are ranked by browserLateness before and after,
// each list sorted earliest first and compared from the earliest; the first
// difference decides that dojo. Worse when any dojo is worse off, better when
// none is and one is better off, the same otherwise.
func browserVerdict(sep SwapSeparation) string {
	key := func(dojo string) string { return strings.ToLower(strings.Join(strings.Fields(dojo), " ")) }
	before := map[[2]string]DojoMeeting{}
	for _, dm := range sep.Before {
		before[pairIDs(dm.A.ID, dm.B.ID)] = dm
	}
	type lists struct{ before, after [][2]int }
	byDojo := map[string]*lists{}
	for _, dm := range sep.After {
		was, ok := before[pairIDs(dm.A.ID, dm.B.ID)]
		if !ok {
			continue
		}
		k := key(was.Dojo)
		if byDojo[k] == nil {
			byDojo[k] = &lists{}
		}
		byDojo[k].before = append(byDojo[k].before, browserLateness(was))
		byDojo[k].after = append(byDojo[k].after, browserLateness(dm))
	}
	cmp := func(x, y [2]int) int {
		if x[0] != y[0] {
			return x[0] - y[0]
		}
		return x[1] - y[1]
	}
	worse, better := false, false
	for _, l := range byDojo {
		sort.Slice(l.before, func(i, j int) bool { return cmp(l.before[i], l.before[j]) < 0 })
		sort.Slice(l.after, func(i, j int) bool { return cmp(l.after[i], l.after[j]) < 0 })
		for i := range l.before {
			if c := cmp(l.after[i], l.before[i]); c != 0 {
				worse = worse || c < 0
				better = better || c > 0
				break
			}
		}
	}
	switch {
	case worse:
		return SwapVerdictWorse
	case better:
		return SwapVerdictBetter
	}
	return SwapVerdictSame
}

// checkCandidatesAgainstTheDryRun is the oracle: for every pick of the draw,
// a seeded pick is refused SwapSeeded, as its swap's dry run is; for an
// unseeded pick the candidates are every other unseeded drawn competitor in
// draw order, and each verdict is what the REAL swap's dry run
// (SwapDrawPlaces, dryRun) says about the separation before and after. It
// answers how many verdicts of each kind it saw.
func checkCandidatesAgainstTheDryRun(t *testing.T, eng *Engine, comp *state.Competition, picks int) (verdicts map[string]int) {
	t.Helper()
	snap := candidatesSnapshot(t, eng, comp.ID)
	s := newSwapSuggester(comp, snap.pools, snap.bracket, snap.roster, DojoSeparation(comp, snap.pools, snap.bracket, snap.roster))
	require.NotNil(t, s)
	var unseeded []int
	for y, ey := range s.entrants {
		if ey.seed == 0 {
			unseeded = append(unseeded, y)
		}
	}
	verdicts = map[string]int{}
	for x, ex := range s.entrants {
		if x >= picks {
			break
		}
		got, err := eng.SwapCandidatesFor(comp.ID, ex.c.side.ID)
		if ex.seed > 0 {
			requireSwapRefused(t, err, SwapSeeded)
			ey := s.entrants[unseeded[0]]
			_, err = eng.SwapDrawPlaces(comp.ID, SwapRequest{
				A:      SwapSide{ID: ex.c.side.ID, Place: clonePlace(ex.place)},
				B:      SwapSide{ID: ey.c.side.ID, Place: clonePlace(ey.place)},
				DryRun: true,
			})
			requireSwapRefused(t, err, SwapSeeded)
			continue
		}
		require.NoError(t, err)
		assert.Equal(t, SwapSide{ID: ex.c.side.ID, Place: ex.place}, got.Pick)
		require.Len(t, got.Candidates, len(unseeded)-1, "every other unseeded competitor")
		k := 0
		for _, y := range unseeded {
			if y == x {
				continue
			}
			ey := s.entrants[y]
			cand := got.Candidates[k]
			k++
			require.Equal(t, ey.c.side.ID, cand.ID, "draw order")
			assert.Equal(t, ey.place, cand.Place)
			res, err := eng.SwapDrawPlaces(comp.ID, SwapRequest{
				A:      SwapSide{ID: ex.c.side.ID, Place: clonePlace(ex.place)},
				B:      SwapSide{ID: ey.c.side.ID, Place: clonePlace(ey.place)},
				DryRun: true,
			})
			require.NoError(t, err)
			assert.Equal(t, browserVerdict(res.Separation), cand.Verdict, "swap %s and %s", ex.c.side.ID, ey.c.side.ID)
			verdicts[cand.Verdict]++
		}
	}
	return verdicts
}

// candidatesSnapshot reads the draw as the candidates' transaction does.
func candidatesSnapshot(t *testing.T, eng *Engine, compID string) swapSnapshot {
	t.Helper()
	var snap swapSnapshot
	require.NoError(t, eng.store.WithTransaction(compID, func(tx state.StoreTx) error {
		var err error
		snap, err = loadSwapSnapshot(tx, compID)
		return err
	}))
	return snap
}

// --- the verdicts against the real dry run ----------------------------------

func TestSwapCandidates_AgreeWithTheSwapDryRun(t *testing.T) {
	dojos := []string{"Omega", "Omega", "Omega", "Kappa", "Kappa", "Pi", "Pi", "Pi", "Rho", "Rho"}
	dojos = append(dojos, uniqueDojos(6)...)

	t.Run("pools and knockout, seeded", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		// Omega's first and Kappa's first are seeded, so they are refused as
		// picks and offered as no candidate.
		order := append([]string{"Omega", "Kappa", "Omega", "Omega", "Kappa", "Pi", "Pi", "Pi", "Rho", "Rho"}, uniqueDojos(6)...)
		comp := drawCandidatesComp(t, eng, store, "cand-mixed", state.CompFormatMixed, 3, order, 2, func(c *state.Competition) { c.NumberPrefix = "K" })
		verdicts := checkCandidatesAgainstTheDryRun(t, eng, comp, len(order))
		assert.Positive(t, verdicts[SwapVerdictWorse], "%v", verdicts)
		assert.Positive(t, verdicts[SwapVerdictSame], "%v", verdicts)
	})

	t.Run("knockout only with byes, seeded", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		comp := drawCandidatesComp(t, eng, store, "cand-ko", state.CompFormatKnockout, 3, dojos[:13], 2, func(c *state.Competition) { c.NumberPrefix = "K" })
		require.NotEmpty(t, byeHolders(loadBracket(t, store, comp.ID)), "fixture must hold a bye")
		verdicts := checkCandidatesAgainstTheDryRun(t, eng, comp, 13)
		assert.Positive(t, verdicts[SwapVerdictWorse], "%v", verdicts)
		assert.Positive(t, verdicts[SwapVerdictSame], "%v", verdicts)
	})

	t.Run("a team competition", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		comp := drawCandidatesComp(t, eng, store, "cand-team", state.CompFormatMixed, 3, dojos[:12], 1, swapTeamComp)
		require.True(t, comp.IsTeam())
		verdicts := checkCandidatesAgainstTheDryRun(t, eng, comp, 12)
		assert.Positive(t, verdicts[SwapVerdictWorse]+verdicts[SwapVerdictBetter], "%v", verdicts)
	})
}

// --- each dojo judged as a whole (operator decision C1) ---------------------

// fourPoolDraw is four pools of two over an eight-leaf knockout that takes
// both places of each pool: A's at leaves 0 and 7, B's at 2 and 5, C's at 4
// and 3, D's at 6 and 1. So A and B can first meet in a semi-final (and in
// one if both win), A and C in a semi-final (the final if both win), and B
// and C in the first round (the final if both win).
func fourPoolDraw(dojos [4][2]string) ([]helper.Pool, *state.Bracket) {
	var pools []helper.Pool
	for p, names := range "ABCD" {
		pool := helper.Pool{PoolName: "Pool " + string(names)}
		for i, d := range dojos[p] {
			id := fmt.Sprintf("%c%d", 'a'+p, i+1)
			pool.Players = append(pool.Players, helper.Player{ID: id, Name: strings.ToUpper(id), Dojo: d})
		}
		pools = append(pools, pool)
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
	return pools, bracket
}

// verdictOf is the swap candidates' verdict for swapping a and b, checked
// against the browser's reading of the swap's own dry run separation, so the
// two agree.
func verdictOf(t *testing.T, snap swapSnapshot, a, b string) string {
	t.Helper()
	got, err := swapCandidates(snap, a)
	require.NoError(t, err)
	verdict := ""
	var place DrawPlace
	var dojos []string
	for _, c := range got.Candidates {
		if c.ID == b {
			verdict, place, dojos = c.Verdict, c.Place, c.Dojos
		}
	}
	require.NotEmpty(t, verdict, "%s is a candidate", b)
	plan, err := planSwap(snap, SwapRequest{A: got.Pick, B: SwapSide{ID: b, Place: place}, DryRun: true})
	require.NoError(t, err)
	if verdict == SwapVerdictSame {
		assert.Empty(t, dojos, "the same names no dojo")
	} else {
		assert.NotEmpty(t, dojos, "a better or worse mark names its dojos")
	}
	assert.Equal(t, verdict, browserVerdict(plan.answer.Separation), "the browser reads it alike")
	return verdict
}

func TestSwapVerdict_EachDojoAsAWhole(t *testing.T) {
	t.Run("two of one dojo trading places changes nothing for it", func(t *testing.T) {
		// Fudo holds a1, b1 and c1. Swapping a1 and b1 moves a1's meeting with
		// c1 earlier (A-C to B-C) and b1's later (B-C to A-C): pair by pair
		// one is earlier, but Fudo's three meetings are the three it had.
		pools, bracket := fourPoolDraw([4][2]string{{"Fudo", "S1"}, {"Fudo", "S2"}, {"Fudo", "S3"}, {"S4", "S5"}})
		comp := drawReady(state.CompFormatMixed)
		snap := swapSnapshot{comp: comp, pools: pools, bracket: bracket}
		assert.Equal(t, SwapVerdictSame, verdictOf(t, snap, "a1", "b1"))
		assert.NotContains(t, suggestedPairs(SuggestSwaps(comp, pools, bracket, nil)), [2]string{"a1", "b1"})
		checkAgainstTheSummary(t, comp, pools, nil, bracket, nil)
	})

	t.Run("a dojo whose earliest meeting gets later is better off though one pair meets earlier", func(t *testing.T) {
		// Omega holds leaves 0, 1, 2 and 6 of eight: its meetings are a
		// quarter-final, two semi-finals and three finals. Moving s0 to leaf
		// 5 brings s0 and s6 from the final to a semi-final, but takes the
		// quarter-final away: two semi-finals and four finals.
		roster := koRoster("Omega", "Omega", "Omega", "D3", "D4", "D5", "Omega", "D7")
		bracket := koDraw(roster)
		comp := drawReady(state.CompFormatKnockout)
		snap := swapSnapshot{comp: comp, bracket: bracket, roster: roster}
		assert.Equal(t, SwapVerdictBetter, verdictOf(t, snap, "s0", "s5"))

		s := newSwapSuggester(comp, nil, bracket, roster, DojoSeparation(comp, nil, bracket, roster))
		improves, ok := s.evaluate(indexOf(t, s, "s0"), indexOf(t, s, "s5"))
		require.True(t, ok, "a suggestion")
		var pairs [][2]string
		for _, imp := range improves {
			pairs = append(pairs, pairIDs(imp.Before.A.ID, imp.Before.B.ID))
		}
		assert.ElementsMatch(t, [][2]string{pairIDs("s0", "s1"), pairIDs("s0", "s2")}, pairs,
			"the pairs that meet later; s0 and s6, who meet earlier, are not an improvement")
		assert.Positive(t, checkAgainstTheSummary(t, comp, nil, nil, bracket, roster))
	})

	t.Run("one dojo better off and another worse off is worse", func(t *testing.T) {
		// Omega meets in the first round (leaves 0 and 1), Kappa only in the
		// final (2 and 4). Swapping s1 and s4 sends Omega to the final and
		// brings Kappa to a semi-final.
		roster := koRoster("Omega", "Omega", "Kappa", "Solo 3", "Kappa", "Solo 5", "Solo 6", "Solo 7")
		bracket := koDraw(roster)
		snap := swapSnapshot{comp: drawReady(state.CompFormatKnockout), bracket: bracket, roster: roster}
		assert.Equal(t, SwapVerdictWorse, verdictOf(t, snap, "s1", "s4"))
	})
}

// candidateOf is the swap candidate b for the pick a.
func candidateOf(t *testing.T, snap swapSnapshot, a, b string) SwapCandidate {
	t.Helper()
	got, err := swapCandidates(snap, a)
	require.NoError(t, err)
	for _, c := range got.Candidates {
		if c.ID == b {
			return c
		}
	}
	require.Failf(t, "no candidate", "%s is not a candidate for %s", b, a)
	return SwapCandidate{}
}

// A candidate names the dojos its verdict is about (operator decision C9):
// the dojos worse off for worse, even when another is better off; the dojos
// better off for better; none for the same. Never the candidate's own dojo
// for being theirs.
func TestSwapCandidates_NameTheDojosAffected(t *testing.T) {
	t.Run("worse names only the dojos worse off", func(t *testing.T) {
		// Swapping s1 and s4 sends Omega to the final (better off) and brings
		// Kappa to a semi-final (worse off).
		roster := koRoster("Omega", "Omega", "Kappa", "Solo 3", "Kappa", "Solo 5", "Solo 6", "Solo 7")
		snap := swapSnapshot{comp: drawReady(state.CompFormatKnockout), bracket: koDraw(roster), roster: roster}
		c := candidateOf(t, snap, "s1", "s4")
		assert.Equal(t, SwapVerdictWorse, c.Verdict)
		assert.Equal(t, []string{"Kappa"}, c.Dojos)
	})
	t.Run("better names the dojos better off", func(t *testing.T) {
		roster := koRoster("Omega", "Omega", "Omega", "D3", "D4", "D5", "Omega", "D7")
		snap := swapSnapshot{comp: drawReady(state.CompFormatKnockout), bracket: koDraw(roster), roster: roster}
		c := candidateOf(t, snap, "s0", "s5")
		assert.Equal(t, SwapVerdictBetter, c.Verdict)
		assert.Equal(t, []string{"Omega"}, c.Dojos)
	})
	t.Run("several dojos worse off are sorted, the same names none", func(t *testing.T) {
		// Hizen meets only in the final (leaves 0 and 4), Fudo too (2 and 6).
		// Moving Hizen's s4 to leaf 3 brings Hizen to a semi-final with s0;
		// Fudo is untouched.
		roster := koRoster("Hizen", "Solo 1", "Fudo", "Solo 3", "Hizen", "Solo 5", "Fudo", "Solo 7")
		snap := swapSnapshot{comp: drawReady(state.CompFormatKnockout), bracket: koDraw(roster), roster: roster}
		c := candidateOf(t, snap, "s4", "s3")
		assert.Equal(t, SwapVerdictWorse, c.Verdict)
		assert.Equal(t, []string{"Hizen"}, c.Dojos)
		// Swapping s4 (Hizen) and s2 (Fudo): s4 goes to leaf 2, beside s0's
		// half (semi-final with s0), and s2 goes to leaf 4, beside s6's half
		// (semi-final with s6): both dojos are worse off.
		c = candidateOf(t, snap, "s4", "s2")
		assert.Equal(t, SwapVerdictWorse, c.Verdict)
		assert.Equal(t, []string{"Fudo", "Hizen"}, c.Dojos)
		same := candidateOf(t, snap, "s1", "s3")
		assert.Equal(t, SwapVerdictSame, same.Verdict)
		assert.Empty(t, same.Dojos)
	})
}

// A pair of dojo-mates sharing a pool: swapping one of them out of it is
// better unless it brings another pair together earlier, and a swap inside
// the pool changes no meeting at all.
func TestSwapCandidates_SamePoolPick(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	comp := drawCandidatesComp(t, eng, store, "cand-pool", state.CompFormatMixed, 3, uniqueDojos(9), 0)
	pools := loadSwapPools(t, store, comp.ID)
	require.Len(t, pools, 3)
	// Give the first two competitors of Pool A one dojo by hand: they share
	// that pool.
	pools[0].Players[1].Dojo = pools[0].Players[0].Dojo
	require.NoError(t, store.SavePools(comp.ID, pools))
	pick := pools[0].Players[0].ID

	got, err := eng.SwapCandidatesFor(comp.ID, pick)
	require.NoError(t, err)
	assert.Equal(t, DrawPlace{Pool: pools[0].PoolName, Position: 1}, got.Pick.Place)
	byID := map[string]SwapCandidate{}
	for _, c := range got.Candidates {
		byID[c.ID] = c
	}
	for row, pl := range pools[0].Players[1:] {
		assert.Equal(t, SwapVerdictSame, byID[pl.ID].Verdict, "row %d shares the pick's pool: nothing moves", row+2)
	}
	for _, p := range pools[1:] {
		for _, pl := range p.Players {
			assert.Equal(t, SwapVerdictBetter, byID[pl.ID].Verdict, "%s takes the pick out of the shared pool", pl.ID)
		}
	}
	verdicts := checkCandidatesAgainstTheDryRun(t, eng, comp, len(byID)+1)
	assert.Positive(t, verdicts[SwapVerdictBetter])
}

// A swap that moves a pair to another match of the same round is not earlier.
func TestSwapCandidates_TheSameRoundIsNotEarlier(t *testing.T) {
	// Eight leaves: s0 and s4 (one dojo) can first meet in the final.
	// Swapping s0 with anyone in its own half keeps the final (another match,
	// the same round); with s5 they meet in the first round, with s6 or s7 in
	// a semifinal.
	roster := koRoster("Omega", "D1", "D2", "D3", "Omega", "D5", "D6", "D7")
	bracket := koDraw(roster)
	comp := drawReady(state.CompFormatKnockout)
	snap := swapSnapshot{comp: comp, bracket: bracket, roster: roster}
	got, err := swapCandidates(snap, "s0")
	require.NoError(t, err)
	want := map[string]string{
		"s1": SwapVerdictSame,  // s1 takes s0's place: the pair still meets in the final
		"s2": SwapVerdictSame,  // another first-round match of the same half
		"s3": SwapVerdictSame,  // ditto
		"s4": SwapVerdictSame,  // the pair trades places
		"s5": SwapVerdictWorse, // first round
		"s6": SwapVerdictWorse, // semifinal
		"s7": SwapVerdictWorse, // semifinal
	}
	for _, c := range got.Candidates {
		assert.Equal(t, want[c.ID], c.Verdict, c.ID)
	}

	// From s4's side: a swap with s1 puts the pair in the first round, a swap
	// inside its own half keeps the final.
	got, err = swapCandidates(snap, "s4")
	require.NoError(t, err)
	for _, c := range got.Candidates {
		switch c.ID {
		case "s1":
			assert.Equal(t, SwapVerdictWorse, c.Verdict)
		case "s7", "s6", "s5":
			assert.Equal(t, SwapVerdictSame, c.Verdict)
		}
	}
}

// A pair that meets in the first round: a swap that moves one of them to the
// other half is better.
func TestSwapCandidates_BetterInAKnockout(t *testing.T) {
	roster := koRoster("Omega", "Omega", "D2", "D3", "D4", "D5", "D6", "D7")
	snap := swapSnapshot{comp: drawReady(state.CompFormatKnockout), bracket: koDraw(roster), roster: roster}
	got, err := swapCandidates(snap, "s1")
	require.NoError(t, err)
	want := map[string]string{"s0": SwapVerdictSame, "s2": SwapVerdictBetter, "s3": SwapVerdictBetter,
		"s4": SwapVerdictBetter, "s5": SwapVerdictBetter, "s6": SwapVerdictBetter, "s7": SwapVerdictBetter}
	for _, c := range got.Candidates {
		assert.Equal(t, want[c.ID], c.Verdict, c.ID)
	}
	assert.Equal(t, 1, *got.Pick.Place.Slot)
}

func TestSwapCandidates_Refusals(t *testing.T) {
	roster := koRoster("Omega", "Omega", "D2", "D3")
	ready := swapSnapshot{comp: drawReady(state.CompFormatKnockout), bracket: koDraw(roster), roster: roster}

	_, err := swapCandidates(ready, "nobody")
	requireSwapRefused(t, err, SwapNotInDraw)
	_, err = swapCandidates(ready, "")
	requireSwapRefused(t, err, SwapNotInDraw)

	league := ready
	league.comp = drawReady(state.CompFormatLeague)
	_, err = swapCandidates(league, "s0")
	requireSwapRefused(t, err, SwapNotSupported)

	none := ready
	none.bracket = nil
	_, err = swapCandidates(none, "s0")
	requireSwapRefused(t, err, SwapNoDraw)

	started := ready
	started.comp = drawReady(state.CompFormatKnockout, func(c *state.Competition) { c.Status = state.CompStatusKnockout })
	_, err = swapCandidates(started, "s0")
	requireSwapRefused(t, err, SwapDrawStarted)

	fought := ready
	fought.fought = "m-r1-0"
	_, err = swapCandidates(fought, "s0")
	requireSwapRefused(t, err, SwapDrawStarted)

	t.Run("through the engine", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		comp := drawCandidatesComp(t, eng, store, "cand-started", state.CompFormatKnockout, 3, uniqueDojos(4), 0)
		ids := loadBracket(t, store, comp.ID).DrawOrder
		require.NoError(t, eng.StartCompetition(comp.ID))
		_, err := eng.SwapCandidatesFor(comp.ID, ids[0])
		requireSwapRefused(t, err, SwapDrawStarted)
		_, err = eng.SwapCandidatesFor("missing", ids[0])
		require.Error(t, err)
	})
}

// A seed keeps their place (operator ruling 2026-11-27): a seeded pick is
// refused, and no seed is offered as a candidate for an unseeded pick.
func TestSwapCandidates_SeedsAreLocked(t *testing.T) {
	roster := koRoster("Omega", "Omega", "D2", "D3", "D4", "D5", "D6", "D7")
	roster[2].Seed, roster[5].Seed = 1, 2
	snap := swapSnapshot{comp: drawReady(state.CompFormatKnockout), bracket: koDraw(roster), roster: roster}

	_, err := swapCandidates(snap, "s2")
	requireSwapRefused(t, err, SwapSeeded)
	assert.Contains(t, err.Error(), roster[2].Name+" is seed 1")

	got, err := swapCandidates(snap, "s1")
	require.NoError(t, err)
	var ids []string
	for _, c := range got.Candidates {
		ids = append(ids, c.ID)
	}
	assert.Equal(t, []string{"s0", "s3", "s4", "s6", "s7"}, ids, "every unseeded competitor but the pick, in draw order")
}

// An unseeded pick in a 256-competitor draw judges every candidate on the two
// competitors' own pairs; it must stay usable. The budget catches an order of
// magnitude lost. It is not asserted under the race detector, where the
// timings are only logged.
func TestSwapCandidates_TimingFor256(t *testing.T) {
	const budget = 5 * time.Second
	dojos := make([]string, 256)
	for i := range dojos {
		dojos[i] = fmt.Sprintf("Dojo %02d", i%16)
	}
	for _, format := range []string{state.CompFormatMixed, state.CompFormatKnockout} {
		t.Run(format, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			comp := drawCandidatesComp(t, eng, store, "cand-256-"+format, format, 4, dojos, 4)
			snap := candidatesSnapshot(t, eng, comp.ID)
			s := newSwapSuggester(comp, snap.pools, snap.bracket, snap.roster, DojoSeparation(comp, snap.pools, snap.bracket, snap.roster))
			var unseeded string
			for _, e := range s.entrants {
				if e.seed == 0 {
					unseeded = e.c.side.ID
					break
				}
			}
			require.NotEmpty(t, unseeded)
			start := time.Now()
			got, err := eng.SwapCandidatesFor(comp.ID, unseeded)
			require.NoError(t, err)
			took := time.Since(start)
			t.Logf("%s, unseeded pick: %v", format, took)
			assert.Len(t, got.Candidates, 255-4, "the four seeds are no candidates")
			if !raceEnabled {
				assert.Less(t, took, budget)
			}
		})
	}
}
