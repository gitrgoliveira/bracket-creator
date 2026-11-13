package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
)

// teamLeagueFromResults saves a single-pool team league (2-person teams, each
// team's dojo "Dojo <name>") whose every regular match is completed: winner
// names the side that won both bouts, "" a match drawn bout for bout.
func teamLeagueFromResults(t *testing.T, compID string, teams []string, winner func(a, b string) string, opts ...func(*state.Competition)) (*Engine, *state.Store) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	comp := &state.Competition{
		ID: compID, Name: "League " + compID, Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"}, TeamSize: 2, Kind: "team",
	}
	for _, opt := range opts {
		opt(comp)
	}
	require.NoError(t, store.SaveCompetition(comp))
	players := make([]helper.Player, len(teams))
	for i, n := range teams {
		players[i] = helper.Player{Name: n, Dojo: "Dojo " + n}
	}
	var matches []state.MatchResult
	idx := 0
	for i, a := range teams {
		for _, b := range teams[i+1:] {
			w := winner(a, b)
			m := state.MatchResult{ID: fmt.Sprintf("Pool A-%d", idx), SideA: a, SideB: b, Status: state.MatchStatusCompleted, Court: "A", Winner: w}
			for pos := 1; pos <= 2; pos++ {
				sub := state.SubMatchResult{Position: pos, SideA: a, SideB: b, Winner: w}
				if w == "" {
					sub.Decision = string(domain.DecisionHikiwake)
				}
				m.SubResults = append(m.SubResults, sub)
			}
			if w == "" {
				m.Decision = string(domain.DecisionHikiwake)
			}
			matches = append(matches, m)
			idx++
		}
	}
	bctest.StampIDs(players, matches)
	require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
	require.NoError(t, store.SavePoolMatches(compID, matches))
	return eng, store
}

// rankOrder is the order teams appear in, by name.
func rankOrder(order []string) func(a, b string) string {
	pos := map[string]int{}
	for i, n := range order {
		pos[n] = i
	}
	return func(a, b string) string {
		if pos[a] < pos[b] {
			return a
		}
		return b
	}
}

func teamID(name string) string { return bctest.StampPlayerID(name, "Dojo "+name) }

// cycleLeagueAt3rd is a 6-team league: Alpha, then Beta, then Gamma, Delta and
// Epsilon level at 3rd to 5th (Gamma beat Delta, Delta beat Epsilon, Epsilon
// beat Gamma), then Zeta. A league's places 3rd to 5th are consequential (the
// tie-break band is the top three by default), but its Winners per pool is 2.
func cycleLeagueAt3rd(t *testing.T, compID string, opts ...func(*state.Competition)) (*Engine, *state.Store) {
	t.Helper()
	order := rankOrder([]string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta"})
	return teamLeagueFromResults(t, compID, []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon", "Zeta"}, func(a, b string) string {
		if a == "Gamma" && b == "Epsilon" {
			return "Epsilon"
		}
		return order(a, b)
	}, opts...)
}

// circle scores a daihyosen round over Gamma, Delta and Epsilon in a circle.
func circle(sideA, sideB string) string {
	pair := map[string]bool{sideA: true, sideB: true}
	switch {
	case pair["Gamma"] && pair["Delta"]:
		return "Gamma"
	case pair["Delta"] && pair["Epsilon"]:
		return "Delta"
	default:
		return "Epsilon"
	}
}

// A league tie at 3rd to 5th whose tie-breaker went in a circle blocks the
// completion (dhCycleExists), so the chusen that settles it must be offered
// too: the band the tie-breaker was run for, the chusen offer and the cycle
// gate are the same one (tieNeedsBreak). The chusen used to be judged against
// the league's Winners per pool (2), so it was never offered and every door
// refused: the league was stranded.
func TestChusenStatus_LeagueCircleAt3rdIsOffered(t *testing.T) {
	const compID = "league-circle-3rd"
	eng, store := cycleLeagueAt3rd(t, compID)

	cands, err := eng.LeagueTiebreakCandidates(compID)
	require.NoError(t, err)
	require.Len(t, cands, 1)
	require.Equal(t, 3, cands[0].MinPosition)
	_, err = eng.GenerateLeagueTiebreakMatches(compID, []string{teamID("Gamma"), teamID("Delta"), teamID("Epsilon")})
	require.NoError(t, err)
	scoreInjectedDH(t, eng, store, compID, circle)

	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.NotEqual(t, AutoCompleteTransitioned, outcome, "a tie-breaker that went in a circle leaves the places undecided")

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the circle's chusen is offered")
	assert.Equal(t, 3, report.Pending[0].MinPosition)
	assert.ElementsMatch(t, []string{"Gamma", "Delta", "Epsilon"}, chusenTeamNames(report.Pending[0]))

	require.NoError(t, store.SaveRankOverride(compID, "Pool A", teamID("Delta"), 3))
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", teamID("Epsilon"), 4))
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", teamID("Gamma"), 5))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)
	outcome, err = eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteTransitioned, outcome, "the chusen settles the circle")
}

// A drawn representative bout over a two-way league tie at 3rd and 4th is
// settled only by a rematch or the lots, so its chusen is offered as well.
func TestChusenStatus_LeagueDrawnDaihyosenAt3rdIsOffered(t *testing.T) {
	const compID = "league-drawn-3rd"
	order := rankOrder([]string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon"})
	eng, store := teamLeagueFromResults(t, compID, []string{"Alpha", "Beta", "Gamma", "Delta", "Epsilon"}, func(a, b string) string {
		if a == "Gamma" && b == "Delta" {
			return ""
		}
		return order(a, b)
	})
	_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{teamID("Gamma"), teamID("Delta")})
	require.NoError(t, err)
	scoreInjectedDH(t, eng, store, compID, func(string, string) string { return "" })

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the drawn bout's chusen is offered")
	assert.Equal(t, 3, report.Pending[0].MinPosition)
}

// poolsVerdictNow is PoolsSettledNow inside a transaction of its own.
func poolsVerdictNow(t *testing.T, eng *Engine, store *state.Store, compID string) PoolsVerdict {
	t.Helper()
	var verdict PoolsVerdict
	require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
		var err error
		verdict, err = eng.PoolsSettledNow(tx, compID)
		return err
	}))
	return verdict
}

// PoolsSettledNow judges a league on more than its rows: every match played
// is not enough while a tie in the band has no tie-breaker, or a tie-breaker
// left one level that only the lots can decide. POST /complete and the
// league's own completion both ask it.
func TestPoolsSettledNow_LeagueOwesItsBreaks(t *testing.T) {
	t.Run("a team tie in the band with no tie-breaker", func(t *testing.T) {
		const compID = "verdict-tiebreak-owed"
		eng, store := cycleLeagueAt3rd(t, compID)
		assert.Equal(t, PoolsTiebreakOwed, poolsVerdictNow(t, eng, store, compID))
	})
	t.Run("the operator accepted shared ranks", func(t *testing.T) {
		const compID = "verdict-tiebreak-finalized"
		eng, store := cycleLeagueAt3rd(t, compID, func(c *state.Competition) { c.LeagueTiebreakFinalized = true })
		assert.Equal(t, PoolsSettled, poolsVerdictNow(t, eng, store, compID))
	})
	t.Run("a team tie-breaker that went in a circle", func(t *testing.T) {
		const compID = "verdict-chusen-owed"
		eng, store := cycleLeagueAt3rd(t, compID)
		_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{teamID("Gamma"), teamID("Delta"), teamID("Epsilon")})
		require.NoError(t, err)
		scoreInjectedDH(t, eng, store, compID, circle)
		assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID))
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", teamID("Delta"), 3))
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", teamID("Epsilon"), 4))
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", teamID("Gamma"), 5))
		assert.Equal(t, PoolsSettled, poolsVerdictNow(t, eng, store, compID), "the lots decide the circle")
	})
	t.Run("a team kind with no team size still owes the lots for a circle", func(t *testing.T) {
		const compID = "verdict-chusen-owed-kind-only"
		eng, store := cycleLeagueAt3rd(t, compID)
		_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{teamID("Gamma"), teamID("Delta"), teamID("Epsilon")})
		require.NoError(t, err)
		scoreInjectedDH(t, eng, store, compID, circle)
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		comp.TeamSize = 0 // hand-edited: Kind alone says it is a team competition
		require.NoError(t, store.SaveCompetition(comp))
		assert.Equal(t, PoolsChusenOwed, poolsVerdictNow(t, eng, store, compID))
	})
	t.Run("a tie-breaker run for one pair only", func(t *testing.T) {
		const compID = "verdict-tiebreak-partial"
		eng, store := cycleLeagueAt3rd(t, compID)
		_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{teamID("Gamma"), teamID("Delta"), teamID("Epsilon")})
		require.NoError(t, err)
		ms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		kept := ms[:0]
		dropped := false
		for _, m := range ms {
			if IsPoolDaihyosenMatchID(m.ID) && !dropped {
				dropped = true // one pair of the three is left without its bout
				continue
			}
			kept = append(kept, m)
		}
		require.True(t, dropped)
		require.NoError(t, store.SavePoolMatches(compID, kept))
		scoreInjectedDH(t, eng, store, compID, circle)
		assert.Equal(t, PoolsTiebreakOwed, poolsVerdictNow(t, eng, store, compID))
	})
	t.Run("an individual tie in the band with no ippon-shobu", func(t *testing.T) {
		const compID = "verdict-individual-owed"
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: compID, Format: state.CompFormatLeague, Status: state.CompStatusPools, Courts: []string{"A"},
		}))
		players := []helper.Player{{Name: "Alice", Dojo: "Dojo Alice"}, {Name: "Bob", Dojo: "Dojo Bob"}}
		matches := []state.MatchResult{{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted,
			Decision: string(domain.DecisionHikiwake)}}
		bctest.StampIDs(players, matches)
		require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
		require.NoError(t, store.SavePoolMatches(compID, matches))
		assert.Equal(t, PoolsTiebreakOwed, poolsVerdictNow(t, eng, store, compID))
	})
}

// A correction that lands between the league's unlocked scan and the lock that
// completes it is judged: the scan saw no tie, the correction made one in the
// band, and the league waits for its tie-breaker instead of completing on an
// unearned shared place. The tie was once judged in the scan, before the lock.
func TestMaybeAutoCompletePools_ATieMadeAfterTheScanHoldsTheLeague(t *testing.T) {
	const compID = "league-tie-after-scan"
	order := rankOrder([]string{"Alpha", "Beta", "Gamma"})
	eng, store := teamLeagueFromResults(t, compID, []string{"Alpha", "Beta", "Gamma"}, order)
	eng.beforePoolsCompleteLock = func(id string) {
		if id != compID {
			return
		}
		eng.beforePoolsCompleteLock = nil
		// Beta - Gamma corrected to a draw: Beta and Gamma are now level for 2nd.
		_, err := store.UpdatePoolMatchByID(compID, "Pool A-2", func(m *state.MatchResult) error {
			m.Winner, m.WinnerID, m.Decision = "", "", string(domain.DecisionHikiwake)
			for i := range m.SubResults {
				m.SubResults[i].Winner = ""
				m.SubResults[i].Decision = string(domain.DecisionHikiwake)
			}
			return nil
		})
		require.NoError(t, err)
	}
	outcome, err := eng.MaybeAutoCompletePools(compID)
	require.NoError(t, err)
	assert.Equal(t, AutoCompleteAwaitingLeagueTiebreak, outcome)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, state.CompStatusPools, comp.Status)
}
