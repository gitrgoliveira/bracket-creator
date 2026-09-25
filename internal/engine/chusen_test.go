package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
)

// TestChusenCandidates_CycleNeedsChusen: three teams tied on every criterion play
// a daihyosen round-robin that ends in a perfect cycle (Alpha>Beta, Beta>Gamma,
// Gamma>Alpha, one win each). The order is undetermined, so the group surfaces as
// a chusen (drawing-lots) candidate for the operator to resolve.
func TestChusenCandidates_CycleNeedsChusen(t *testing.T) {
	compID := "chusen-cycle"
	eng, store := setupTeamPoolComp(t, compID, true) // 3 teams fully tied
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)

	// Checked as an UNORDERED pair, not by assuming a fixed side: the
	// pre-DH tied group's order (and so which side of each pairing lands
	// in SideA) is itself sorted by Player.ID when Points tie
	// (computeStandingsFrom's deterministic tiebreak), and ids are
	// bctest.StampPlayerID's UUID-v4-shaped hash -- unrelated to name
	// order.
	scoreInjectedDH(t, eng, store, compID, func(sideA, sideB string) string {
		pair := map[string]bool{sideA: true, sideB: true}
		switch {
		case pair["Alpha"] && pair["Beta"]:
			return "Alpha"
		case pair["Alpha"] && pair["Gamma"]:
			return "Gamma" // Gamma > Alpha
		case pair["Beta"] && pair["Gamma"]:
			return "Beta"
		}
		return sideA
	})

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	cands := report.Pending
	require.Len(t, cands, 1, "an unresolved 3-way daihyosen cycle needs a chusen")
	assert.Equal(t, "Pool A", cands[0].PoolName)
	assert.Len(t, cands[0].Teams, 3)
	assert.Equal(t, 1, cands[0].MinPosition)
}

// TestChusenCandidates_StrictOrderNeedsNoChusen: the same tied group, but the
// daihyosen produces a strict 2/1/0 win order (Alpha beats all, Beta beats
// Gamma), so no chusen is needed.
func TestChusenCandidates_StrictOrderNeedsNoChusen(t *testing.T) {
	compID := "chusen-resolved"
	eng, store := setupTeamPoolComp(t, compID, true)
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)

	scoreInjectedDH(t, eng, store, compID, func(sideA, sideB string) string {
		if sideA == "Alpha" || sideB == "Alpha" {
			return "Alpha"
		}
		return "Beta"
	})

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	cands := report.Pending
	assert.Empty(t, cands, "a strictly-ordered daihyosen needs no chusen")
}

// TestChusenCandidates_ResolvedByOverride: once the operator records the drawn
// order (a per-pool rank override for every tied member), the cycle group no
// longer needs a chusen.
func TestChusenCandidates_ResolvedByOverride(t *testing.T) {
	compID := "chusen-override"
	eng, store := setupTeamPoolComp(t, compID, true)
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	// Checked as an UNORDERED pair; see TestChusenCandidates_CycleNeedsChusen's
	// identical comment for why a fixed sideA/sideB orientation cannot be
	// assumed.
	scoreInjectedDH(t, eng, store, compID, func(sideA, sideB string) string {
		pair := map[string]bool{sideA: true, sideB: true}
		switch {
		case pair["Alpha"] && pair["Beta"]:
			return "Alpha"
		case pair["Alpha"] && pair["Gamma"]:
			return "Gamma"
		case pair["Beta"] && pair["Gamma"]:
			return "Beta"
		}
		return sideA
	})
	// Recorded via SaveRankOverride (the real operator path, identity-keyed:
	// helper.CompetitorKey(id, "", "")), not a raw bare-name literal --
	// PoolRanks lookup is id-only per lookupPoolRankOverride, never a legacy
	// bare-name key (operator ruling bc-pnum; see
	// TestCalculatePoolStandings_Override_LegacyBareNameKeyIsUnresolvable in
	// pool_rank_override_test.go for a bare-name override going the other,
	// unresolvable way). setupTeamPoolComp's roster carries "Dojo <Name>"
	// dojos, so the ids match bctest.StampPlayerID's derivation exactly.
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", bctest.StampPlayerID("Alpha", "Dojo Alpha"), 1))
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", bctest.StampPlayerID("Beta", "Dojo Beta"), 2))
	require.NoError(t, store.SaveRankOverride(compID, "Pool A", bctest.StampPlayerID("Gamma", "Dojo Gamma"), 3))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	cands := report.Pending
	assert.Empty(t, cands, "a chusen recorded as a full rank override clears the candidate")
}

// scoreDHCycle scores setupTeamPoolComp's injected daihyosen round into the
// perfect cycle Alpha > Beta > Gamma > Alpha (one win each), the tie only a
// chusen settles. Checked as UNORDERED pairs; see
// TestChusenCandidates_CycleNeedsChusen for why a fixed side cannot be assumed.
func scoreDHCycle(t *testing.T, eng *Engine, store *state.Store, compID string) {
	t.Helper()
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	scoreInjectedDH(t, eng, store, compID, func(sideA, sideB string) string {
		pair := map[string]bool{sideA: true, sideB: true}
		switch {
		case pair["Alpha"] && pair["Beta"]:
			return "Alpha"
		case pair["Alpha"] && pair["Gamma"]:
			return "Gamma"
		case pair["Beta"] && pair["Gamma"]:
			return "Beta"
		}
		return sideA
	})
}

// recordChusenOrder records a drawn order for Pool A through the store, one
// team per rank starting at 1, and drops the standings cache so the next read
// sees it.
func recordChusenOrder(t *testing.T, eng *Engine, store *state.Store, compID string, order ...string) {
	t.Helper()
	for i, name := range order {
		require.NoError(t, store.SaveRankOverride(compID, "Pool A", bctest.StampPlayerID(name, "Dojo "+name), i+1))
	}
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)
}

func chusenTeamNames(g ChusenGroup) []string {
	names := make([]string, len(g.Teams))
	for i, s := range g.Teams {
		names[i] = s.Player.Name
	}
	return names
}

// A chusen recorded in the wrong order must stay fixable, so ChusenStatus
// reports a group a recorded chusen settled, in the recorded order and with
// the recorded ranks, and keeps reporting it after the order is changed. The
// orders used here are deliberately not the natural one, so the reported
// order can only have come from the override.
func TestChusenStatus_ReportsTheRecordedOrder(t *testing.T) {
	compID := "chusen-recorded"
	eng, store := setupTeamPoolComp(t, compID, true)
	scoreDHCycle(t, eng, store, compID)

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1)
	assert.Empty(t, report.Recorded, "a tie still waiting for its chusen is not a recorded one")
	assert.Nil(t, report.Pending[0].Ranks, "a pending group carries no recorded ranks")

	recordChusenOrder(t, eng, store, compID, "Gamma", "Alpha", "Beta")
	report, err = eng.ChusenStatus(compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending, "the recorded chusen clears the candidate")
	require.Len(t, report.Recorded, 1, "the group the chusen settled is reported as recorded")
	g := report.Recorded[0]
	assert.Equal(t, "Pool A", g.PoolName)
	assert.Equal(t, 1, g.MinPosition)
	assert.Equal(t, []string{"Gamma", "Alpha", "Beta"}, chusenTeamNames(g))
	assert.Equal(t, []int{1, 2, 3}, g.Ranks)

	// The operator changes it: still the same recorded group, now in the new order.
	recordChusenOrder(t, eng, store, compID, "Beta", "Gamma", "Alpha")
	report, err = eng.ChusenStatus(compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending)
	require.Len(t, report.Recorded, 1)
	assert.Equal(t, []string{"Beta", "Gamma", "Alpha"}, chusenTeamNames(report.Recorded[0]))
	assert.Equal(t, []int{1, 2, 3}, report.Recorded[0].Ranks)
}

// Only a group the daihyosen left undetermined is a chusen, so only such a
// group is reported as recorded. An override on a group the daihyosen put in
// a strict order (reachable only through the API) is not a chusen, and a
// partly recorded one is still waiting for its chusen.
func TestChusenStatus_RecordedIsOnlyAChusen(t *testing.T) {
	t.Run("an override on a daihyosen-decided group is not a recorded chusen", func(t *testing.T) {
		compID := "chusen-strict-override"
		eng, store := setupTeamPoolComp(t, compID, true)
		_, err := eng.InjectPoolDaihyosenMatches(compID)
		require.NoError(t, err)
		scoreInjectedDH(t, eng, store, compID, func(sideA, sideB string) string {
			if sideA == "Alpha" || sideB == "Alpha" {
				return "Alpha"
			}
			return "Beta"
		})
		recordChusenOrder(t, eng, store, compID, "Alpha", "Beta", "Gamma")

		report, err := eng.ChusenStatus(compID)
		require.NoError(t, err)
		assert.Empty(t, report.Pending)
		assert.Empty(t, report.Recorded)
	})
	t.Run("a partly recorded chusen is still pending", func(t *testing.T) {
		compID := "chusen-partial-override"
		eng, store := setupTeamPoolComp(t, compID, true)
		scoreDHCycle(t, eng, store, compID)
		recordChusenOrder(t, eng, store, compID, "Gamma")

		report, err := eng.ChusenStatus(compID)
		require.NoError(t, err)
		assert.Len(t, report.Pending, 1)
		assert.Empty(t, report.Recorded)
	})
}

// TestChusenCandidates_NonTeamHasNone: individual (non-team) competitions never
// surface chusen candidates (chusen here resolves team-pool daihyosen cycles).
func TestChusenCandidates_NonTeamHasNone(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "chusen-indiv", Name: "Individual", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"}, TeamSize: 0,
	}))
	report, err := eng.ChusenStatus("chusen-indiv")
	require.NoError(t, err)
	cands := report.Pending
	assert.Empty(t, cands)
}

// TestChusenCandidates_PartialRoundNotPremature: with only ONE of a 3-team
// group's three daihyosen bouts scored, the partial win counts (1/0/0) contain a
// spurious duplicate (the two zeros). Chusen must NOT surface until the whole
// pairwise round is complete.
func TestChusenCandidates_PartialRoundNotPremature(t *testing.T) {
	compID := "chusen-partial"
	eng, store := setupTeamPoolComp(t, compID, true) // 3 teams fully tied
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)

	// Complete exactly one of the three injected daihyosen bouts.
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	completedOne := false
	for i := range all {
		if IsPoolDaihyosenMatchID(all[i].ID) && !completedOne {
			all[i].Status = state.MatchStatusCompleted
			all[i].Winner = all[i].SideA
			completedOne = true
		}
	}
	require.True(t, completedOne, "expected at least one injected DH bout")
	require.NoError(t, store.SavePoolMatches(compID, all))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	cands := report.Pending
	assert.Empty(t, cands, "chusen must not surface mid-round (only 1 of 3 DH bouts scored)")
}

// TestChusenCandidates_AllDrawnNeedsChusen: a full daihyosen round completed as
// all hikiwake (every bout Winner="") leaves every team on 0 wins, so the order
// is undetermined and must surface as needing chusen (otherwise the competition
// can never advance).
func TestChusenCandidates_AllDrawnNeedsChusen(t *testing.T) {
	compID := "chusen-alldrawn"
	eng, store := setupTeamPoolComp(t, compID, true) // 3 teams fully tied
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)

	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	scored := 0
	for i := range all {
		if IsPoolDaihyosenMatchID(all[i].ID) {
			all[i].Status = state.MatchStatusCompleted
			all[i].Winner = "" // hikiwake
			scored++
		}
	}
	require.Equal(t, 3, scored, "expected the 3-team round-robin of DH bouts")
	require.NoError(t, store.SavePoolMatches(compID, all))
	eng.standingsCache.Delete(compID)
	eng.standingsFlight.Delete(compID)

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	cands := report.Pending
	require.Len(t, cands, 1, "an all-drawn daihyosen round leaves the order undetermined -> chusen")
}

// TestChusenCandidates_PartialTieWithoutCycleNeedsChusen: a 4-team tied group
// whose daihyosen round has NO win/loss cycle anywhere (Alpha beats everyone
// and loses to nobody), yet two teams still finish level on daihyosen wins
// (Beta and Gamma draw each other and each beat Delta once: 1 win apiece).
// groupNeedsChusen fires on any duplicate win count, not only a true cycle -
// this pins that a plain partial tie also surfaces the chusen panel.
func TestChusenCandidates_PartialTieWithoutCycleNeedsChusen(t *testing.T) {
	compID := "chusen-partial-tie"
	teams := []string{"Alpha", "Beta", "Gamma", "Delta"}
	// Fully tied 4-team pool (every match drawn) so all four share one tied
	// group in standings.
	matches := []state.MatchResult{
		teamPoolMatch("Pool A-0", "A", "Alpha", "Beta", ""),
		teamPoolMatch("Pool A-1", "A", "Alpha", "Gamma", ""),
		teamPoolMatch("Pool A-2", "A", "Alpha", "Delta", ""),
		teamPoolMatch("Pool A-3", "A", "Beta", "Gamma", ""),
		teamPoolMatch("Pool A-4", "A", "Beta", "Delta", ""),
		teamPoolMatch("Pool A-5", "A", "Gamma", "Delta", ""),
	}
	eng, store := setupTeamPool(t, compID, teams, matches)

	injected, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	require.Len(t, injected, 6, "expected the full 4-team DH round-robin")

	winner := func(a, b string) string {
		pair := map[string]bool{a: true, b: true}
		switch {
		case pair["Alpha"]:
			return "Alpha" // beats every opponent, loses to nobody: no cycle involves Alpha
		case pair["Beta"] && pair["Gamma"]:
			return "" // draw: the source of the Beta/Gamma win-count tie
		case pair["Beta"] && pair["Delta"]:
			return "Beta"
		case pair["Gamma"] && pair["Delta"]:
			return "Gamma"
		}
		t.Fatalf("unexpected DH pair %s vs %s", a, b)
		return ""
	}
	scoreInjectedDH(t, eng, store, compID, winner)
	// Final daihyosen wins: Alpha=3, Beta=1, Gamma=1, Delta=0 - a duplicate at
	// 1 with no cyclic relationship anywhere in the results.

	report, err := eng.ChusenStatus(compID)
	require.NoError(t, err)
	cands := report.Pending
	require.Len(t, cands, 1, "Beta and Gamma tie on daihyosen wins with no cycle present -> still needs chusen")
	assert.Len(t, cands[0].Teams, 4)
}

// TestGroupNeedsChusen_SameNameTeamsKeyOnIdentity pins the identity keying in
// groupNeedsChusen at unit level. Two TEAMS sharing a display name is not a
// state normal operation produces (team names are unique by rule; see the
// comment in groupNeedsChusen), so the group is hand-built rather than driven
// through a draw: the point is that the function reads the daihyosen result
// per competitor, not per name, if such a roster ever reaches it.
//
// Fixture: three tied teams, two of them named "Tora" from different dojos.
// The daihyosen round-robin is complete with a strict 2/1/0 win order, so the
// group is DECIDED and needs no chusen. Keyed by bare name, both Toras read
// the same win count, the order looks tied, and a chusen is offered that the
// operator does not need.
func TestGroupNeedsChusen_SameNameTeamsKeyOnIdentity(t *testing.T) {
	group := []state.PlayerStanding{
		{Player: domain.Player{ID: "id-a", Name: "Tora", Dojo: "Tokyo"}},
		{Player: domain.Player{ID: "id-b", Name: "Tora", Dojo: "Osaka"}},
		{Player: domain.Player{ID: "id-c", Name: "Kuma", Dojo: "Kyoto"}},
	}
	dh := func(idx int, aID, aName, bID, bName, wID, wName string) state.MatchResult {
		return state.MatchResult{
			ID:    fmt.Sprintf("Pool A-DH-%d", idx),
			SideA: aName, SideAID: aID,
			SideB: bName, SideBID: bID,
			Winner: wName, WinnerID: wID,
			Status: state.MatchStatusCompleted,
		}
	}
	matches := []state.MatchResult{
		dh(0, "id-a", "Tora", "id-b", "Tora", "id-a", "Tora"), // Tokyo beats Osaka
		dh(1, "id-a", "Tora", "id-c", "Kuma", "id-a", "Tora"), // Tokyo beats Kuma
		dh(2, "id-b", "Tora", "id-c", "Kuma", "id-b", "Tora"), // Osaka beats Kuma
	}
	// Strict 2/1/0 order: Tokyo 2, Osaka 1, Kuma 0.
	assert.False(t, groupNeedsChusen(group, matches, nil),
		"a decided daihyosen order must not need a chusen just because two teams share a name")
}

// A pool correction made after a mixed competition's knockout has started can
// leave a qualifying tie the daihyosen cannot settle (here a three-way cycle).
// The chusen that settles it must be offered and accepted in knockout status,
// and seat the slot the correction sent back to its label; otherwise the
// knockout match that slot feeds can never be played. A chusen recorded by
// mistake then stays fixable like any rank override: named and refused while
// the old qualifier has fought, and, confirmed, reopened with the new one.
func TestChusen_KnockoutStatus_SeatsTheSlotAndStaysFixable(t *testing.T) {
	f := newRQFixture(t, "chusen-ko", 1, [][]string{{"Alpha", "Beta", "Gamma"}, {"Delta", "Epsilon"}},
		func(c *state.Competition) { c.Kind, c.TeamSize = "team", 2 })
	// Pool A-0: Alpha v Beta, A-1: Alpha v Gamma, A-2: Beta v Gamma.
	f.scorePool("Pool A-0", "Alpha")
	f.scorePool("Pool A-1", "Alpha")
	f.scorePool("Pool A-2", "Beta")
	f.scorePool("Pool B-0", "Delta")
	outcome, err := f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteKnockoutStarted, outcome)
	final, side := f.slot("Pool A-1st")
	name, _ := sideOf(final, side)
	require.Equal(t, "Alpha", name)

	// The correction: every Pool A encounter was drawn. The tied place goes
	// back to its label, and the daihyosen round is injected in knockout status.
	draw := func(matchID string) {
		m := loadPoolMatchByID(t, f.store, f.compID, matchID)
		require.NoError(t, f.write(matchID, &state.MatchResult{
			SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID,
			Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
		}))
	}
	for _, id := range []string{"Pool A-0", "Pool A-1", "Pool A-2"} {
		draw(id)
	}
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	final, _ = f.slot("Pool A-1st")
	name, _ = sideOf(final, side)
	require.Equal(t, "Pool A-1st", name, "a tied place is not a finisher")

	// The daihyosen cycles: Alpha > Beta, Beta > Gamma, Gamma > Alpha.
	scoreInjectedDH(t, f.eng, f.store, f.compID, func(sideA, sideB string) string {
		pair := map[string]bool{sideA: true, sideB: true}
		switch {
		case pair["Alpha"] && pair["Beta"]:
			return "Alpha"
		case pair["Beta"] && pair["Gamma"]:
			return "Beta"
		}
		return "Gamma"
	})
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	final, _ = f.slot("Pool A-1st")
	name, _ = sideOf(final, side)
	require.Equal(t, "Pool A-1st", name, "a cycle leaves the place undecided")
	comp, err := f.store.LoadCompetition(f.compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusKnockout, comp.Status)

	report, err := f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the chusen is offered in knockout status")
	assert.Equal(t, "Pool A", report.Pending[0].PoolName)
	assert.Len(t, report.Pending[0].Teams, 3)

	for rank, team := range []string{"Alpha", "Beta", "Gamma"} {
		changed, oerr := f.eng.OverridePoolRanks(f.compID, "Pool A", []RankOverride{{PlayerID: rqID(team), Rank: rank + 1}})
		require.NoError(t, oerr)
		assert.True(t, changed)
	}
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	final, _ = f.slot("Pool A-1st")
	name, id := sideOf(final, side)
	assert.Equal(t, "Alpha", name, "the chusen seats the slot")
	assert.Equal(t, rqID("Alpha"), id)
	report, err = f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	assert.Empty(t, report.Pending, "the recorded chusen clears the candidate")
	require.Len(t, report.Recorded, 1, "the recorded chusen is still reported in knockout status, so it can be changed")
	assert.Equal(t, []string{"Alpha", "Beta", "Gamma"}, chusenTeamNames(report.Recorded[0]))
	assert.Equal(t, []int{1, 2, 3}, report.Recorded[0].Ranks)

	// The final is fought, then the chusen turns out to have been misrecorded:
	// Alpha drew 3rd, so Beta (2nd) holds Pool A's 1st place.
	require.NoError(t, f.scoreKO(final.ID, "Alpha"))
	_, err = f.eng.OverridePoolRanks(f.compID, "Pool A", []RankOverride{{PlayerID: rqID("Alpha"), Rank: 3}})
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	require.Len(t, played.Blocking, 1)
	assert.Equal(t, final.ID, played.Blocking[0].ID)
	o, err := f.store.LoadOverrides(f.compID)
	require.NoError(t, err)
	assert.Equal(t, 1, o.PoolRanks["Pool A"]["id:"+rqID("Alpha")], "the refused override is taken back")

	var reopened []ReopenedMatch
	changed, err := f.eng.OverridePoolRanks(f.compID, "Pool A", []RankOverride{{PlayerID: rqID("Alpha"), Rank: 3}}, ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	assert.True(t, changed)
	require.Len(t, reopened, 1)
	assert.Equal(t, final.ID, reopened[0].ID)
	got := findBracketMatchInBracket(f.bracket(), final.ID)
	assert.Equal(t, state.MatchStatusScheduled, got.Status)
	name, id = sideOf(*got, side)
	assert.Equal(t, "Beta", name, "the corrected chusen seats the new 1st")
	assert.Equal(t, rqID("Beta"), id)
}

// Changing a recorded chusen is ONE write, answered for on the final order.
// Two qualifiers per pool, a three-way tie in Pool A recorded Alpha 1st, Beta
// 2nd, Gamma 3rd, and the knockout matches both of Pool A's places feed
// already fought. The lots were misread: the real order is Gamma, Beta,
// Alpha. Only 1st place changes hands, so only its match may be named.
//
// Set one rank at a time, the first write (Alpha 3rd) leaves Alpha and Gamma
// tied on 3 behind Beta: Beta read as 1st and 2nd place as moved too, so the
// operator was asked to reopen BOTH fought matches and, confirming, had the
// 2nd-place result cleared although Beta keeps 2nd. Declining left Alpha's
// new rank recorded on its own.
func TestOverridePoolRanks_ChangingAChusenNamesOnlyTheRealMove(t *testing.T) {
	f := newRQFixture(t, "chusen-change", 2, [][]string{{"Alpha", "Beta", "Gamma"}, {"Delta", "Epsilon"}},
		func(c *state.Competition) { c.Kind, c.TeamSize = "team", 2 })
	for _, id := range []string{"Pool A-0", "Pool A-1", "Pool A-2"} {
		m := loadPoolMatchByID(t, f.store, f.compID, id)
		require.NoError(t, f.write(id, &state.MatchResult{
			SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID,
			Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake),
		}))
	}
	f.scorePool("Pool B-0", "Delta")
	_, err := f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	// The daihyosen cycles: Alpha > Beta, Beta > Gamma, Gamma > Alpha.
	scoreInjectedDH(t, f.eng, f.store, f.compID, func(sideA, sideB string) string {
		pair := map[string]bool{sideA: true, sideB: true}
		switch {
		case pair["Alpha"] && pair["Beta"]:
			return "Alpha"
		case pair["Beta"] && pair["Gamma"]:
			return "Beta"
		}
		return "Gamma"
	})
	report, err := f.eng.ChusenStatus(f.compID)
	require.NoError(t, err)
	require.Len(t, report.Pending, 1, "the cycle needs a chusen")

	ranks := func(alpha, beta, gamma int) []RankOverride {
		return []RankOverride{{rqID("Alpha"), alpha}, {rqID("Beta"), beta}, {rqID("Gamma"), gamma}}
	}
	recorded := func() map[string]int {
		o, lerr := f.store.LoadOverrides(f.compID)
		require.NoError(t, lerr)
		return o.PoolRanks["Pool A"]
	}
	want := func(alpha, beta, gamma int) map[string]int {
		return map[string]int{"id:" + rqID("Alpha"): alpha, "id:" + rqID("Beta"): beta, "id:" + rqID("Gamma"): gamma}
	}

	changed, err := f.eng.OverridePoolRanks(f.compID, "Pool A", ranks(1, 2, 3))
	require.NoError(t, err)
	require.True(t, changed)
	outcome, err := f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	require.Equal(t, AutoCompleteKnockoutStarted, outcome)
	first, firstSide := f.slot("Pool A-1st")
	second, secondSide := f.slot("Pool A-2nd")
	require.NotEqual(t, first.ID, second.ID)
	name, _ := sideOf(first, firstSide)
	require.Equal(t, "Alpha", name)
	name, _ = sideOf(second, secondSide)
	require.Equal(t, "Beta", name)
	require.NoError(t, f.scoreKO(first.ID, "Alpha"))
	require.NoError(t, f.scoreKO(second.ID, "Beta"))

	// Refused: only 1st place moves, so only its match is named, and every
	// rank of the group stays as it was recorded.
	changed, err = f.eng.OverridePoolRanks(f.compID, "Pool A", ranks(3, 2, 1))
	assert.False(t, changed)
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	require.Len(t, played.Blocking, 1, "the 2nd-place match keeps Beta and is not named")
	assert.Equal(t, first.ID, played.Blocking[0].ID)
	require.Len(t, played.QualifierChange, 1)
	assert.Equal(t, 1, played.QualifierChange[0].Rank)
	assert.Equal(t, QualifierIdentity{Name: "Alpha", ID: rqID("Alpha")}, played.QualifierChange[0].From)
	assert.Equal(t, QualifierIdentity{Name: "Gamma", ID: rqID("Gamma")}, played.QualifierChange[0].To)
	assert.Equal(t, want(1, 2, 3), recorded(), "the refusal takes back every rank of the group")
	for _, id := range []string{first.ID, second.ID} {
		assert.Equal(t, state.MatchStatusCompleted, findBracketMatchInBracket(f.bracket(), id).Status, "nothing reopened before the operator confirms")
	}

	// Confirmed: the 1st-place match reopens with Gamma seated; the
	// 2nd-place match keeps its result.
	var reopened []ReopenedMatch
	changed, err = f.eng.OverridePoolRanks(f.compID, "Pool A", ranks(3, 2, 1), ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, want(3, 2, 1), recorded())
	require.Len(t, reopened, 1)
	assert.Equal(t, first.ID, reopened[0].ID)
	b := f.bracket()
	got := findBracketMatchInBracket(b, first.ID)
	assert.Equal(t, state.MatchStatusScheduled, got.Status)
	name, id := sideOf(*got, firstSide)
	assert.Equal(t, "Gamma", name)
	assert.Equal(t, rqID("Gamma"), id)
	kept := findBracketMatchInBracket(b, second.ID)
	assert.Equal(t, state.MatchStatusCompleted, kept.Status, "Beta keeps 2nd, so its fought match keeps its result")
	assert.Equal(t, "Beta", kept.Winner)
}
