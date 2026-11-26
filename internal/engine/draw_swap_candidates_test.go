package engine

import (
	"fmt"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
// browser's way: worse when any pair of its separation rows is less late
// after, better when none is and one is later, the same otherwise.
func browserVerdict(sep SwapSeparation) string {
	before := map[[2]string][2]int{}
	for _, dm := range sep.Before {
		before[pairIDs(dm.A.ID, dm.B.ID)] = browserLateness(dm)
	}
	later, earlier := false, false
	for _, dm := range sep.After {
		was, ok := before[pairIDs(dm.A.ID, dm.B.ID)]
		if !ok {
			continue
		}
		now := browserLateness(dm)
		c := now[0] - was[0]
		if c == 0 {
			c = now[1] - was[1]
		}
		switch {
		case c < 0:
			earlier = true
		case c > 0:
			later = true
		}
	}
	return shiftVerdict(later, earlier)
}

// checkCandidatesAgainstTheDryRun is the oracle: for every pick of the draw,
// the candidates are every other drawn competitor in draw order, and each
// verdict is what the REAL swap's dry run (SwapDrawPlaces, dryRun) says about
// the separation before and after. It answers how many verdicts of each kind
// it saw, and how many of the dry runs rebuilt the knockout.
func checkCandidatesAgainstTheDryRun(t *testing.T, eng *Engine, comp *state.Competition, picks int) (verdicts map[string]int, rebuilt int) {
	t.Helper()
	snap := candidatesSnapshot(t, eng, comp.ID)
	s := newSwapSuggester(comp, snap.pools, snap.bracket, snap.roster, DojoSeparation(comp, snap.pools, snap.bracket, snap.roster))
	require.NotNil(t, s)
	verdicts = map[string]int{}
	for x, ex := range s.entrants {
		if x >= picks {
			break
		}
		got, err := eng.SwapCandidatesFor(comp.ID, ex.c.side.ID)
		require.NoError(t, err)
		assert.Equal(t, SwapSide{ID: ex.c.side.ID, Place: ex.place}, got.Pick)
		require.Len(t, got.Candidates, len(s.entrants)-1)
		k := 0
		for y, ey := range s.entrants {
			if y == x {
				continue
			}
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
			if res.BracketRebuilt {
				rebuilt++
			}
			assert.Equal(t, browserVerdict(res.Separation), cand.Verdict, "swap %s and %s", ex.c.side.ID, ey.c.side.ID)
			verdicts[cand.Verdict]++
		}
	}
	return verdicts, rebuilt
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
		// Omega's first and Kappa's first are seeded, so their swaps rebuild
		// the knockout and are judged on the dry run itself.
		order := append([]string{"Omega", "Kappa", "Omega", "Omega", "Kappa", "Pi", "Pi", "Pi", "Rho", "Rho"}, uniqueDojos(6)...)
		comp := drawCandidatesComp(t, eng, store, "cand-mixed", state.CompFormatMixed, 3, order, 2, func(c *state.Competition) { c.NumberPrefix = "K" })
		verdicts, rebuilt := checkCandidatesAgainstTheDryRun(t, eng, comp, len(order))
		assert.Positive(t, rebuilt, "a seeded swap rebuilds the knockout")
		assert.Positive(t, verdicts[SwapVerdictWorse], "%v", verdicts)
		assert.Positive(t, verdicts[SwapVerdictSame], "%v", verdicts)
	})

	t.Run("knockout only with byes, seeded", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		comp := drawCandidatesComp(t, eng, store, "cand-ko", state.CompFormatKnockout, 3, dojos[:13], 2, func(c *state.Competition) { c.NumberPrefix = "K" })
		require.NotEmpty(t, byeHolders(loadBracket(t, store, comp.ID)), "fixture must hold a bye")
		verdicts, rebuilt := checkCandidatesAgainstTheDryRun(t, eng, comp, 13)
		assert.Zero(t, rebuilt, "a knockout-only swap rebuilds nothing")
		assert.Positive(t, verdicts[SwapVerdictWorse], "%v", verdicts)
		assert.Positive(t, verdicts[SwapVerdictSame], "%v", verdicts)
	})

	t.Run("a team competition", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		comp := drawCandidatesComp(t, eng, store, "cand-team", state.CompFormatMixed, 3, dojos[:12], 1, swapTeamComp)
		require.True(t, comp.IsTeam())
		verdicts, _ := checkCandidatesAgainstTheDryRun(t, eng, comp, 12)
		assert.Positive(t, verdicts[SwapVerdictWorse]+verdicts[SwapVerdictBetter], "%v", verdicts)
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
	verdicts, _ := checkCandidatesAgainstTheDryRun(t, eng, comp, len(byID)+1)
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

// A seeded pick in a 256-competitor pools + knockout draw judges every
// candidate on the dry run, each a knockout rebuild; it must stay usable.
func TestSwapCandidates_TimingFor256(t *testing.T) {
	const budget = 20 * time.Second
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
			var seeded, unseeded string
			for _, e := range s.entrants {
				if e.seed > 0 && seeded == "" {
					seeded = e.c.side.ID
				}
				if e.seed == 0 && unseeded == "" {
					unseeded = e.c.side.ID
				}
			}
			require.NotEmpty(t, seeded)
			for name, pick := range map[string]string{"seeded": seeded, "unseeded": unseeded} {
				start := time.Now()
				got, err := eng.SwapCandidatesFor(comp.ID, pick)
				require.NoError(t, err)
				took := time.Since(start)
				t.Logf("%s, %s pick: %v", format, name, took)
				assert.Len(t, got.Candidates, 255)
				assert.Less(t, took, budget)
			}
		})
	}
}
