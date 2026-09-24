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

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
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

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
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

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
	assert.Empty(t, cands, "a chusen recorded as a full rank override clears the candidate")
}

// TestChusenCandidates_NonTeamHasNone: individual (non-team) competitions never
// surface chusen candidates (chusen here resolves team-pool daihyosen cycles).
func TestChusenCandidates_NonTeamHasNone(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "chusen-indiv", Name: "Individual", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"}, TeamSize: 0,
	}))
	cands, err := eng.ChusenCandidates("chusen-indiv")
	require.NoError(t, err)
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

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
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

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
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

	cands, err := eng.ChusenCandidates(compID)
	require.NoError(t, err)
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

	cands, err := f.eng.ChusenCandidates(f.compID)
	require.NoError(t, err)
	require.Len(t, cands, 1, "the chusen is offered in knockout status")
	assert.Equal(t, "Pool A", cands[0].PoolName)
	assert.Len(t, cands[0].Teams, 3)

	for rank, team := range []string{"Alpha", "Beta", "Gamma"} {
		changed, oerr := f.eng.OverridePoolRank(f.compID, "Pool A", rqID(team), rank+1)
		require.NoError(t, oerr)
		assert.True(t, changed)
	}
	_, err = f.eng.MaybeAutoCompletePools(f.compID)
	require.NoError(t, err)
	final, _ = f.slot("Pool A-1st")
	name, id := sideOf(final, side)
	assert.Equal(t, "Alpha", name, "the chusen seats the slot")
	assert.Equal(t, rqID("Alpha"), id)
	cands, err = f.eng.ChusenCandidates(f.compID)
	require.NoError(t, err)
	assert.Empty(t, cands, "the recorded chusen clears the candidate")

	// The final is fought, then the chusen turns out to have been misrecorded:
	// Alpha drew 3rd, so Beta (2nd) holds Pool A's 1st place.
	require.NoError(t, f.scoreKO(final.ID, "Alpha"))
	_, err = f.eng.OverridePoolRank(f.compID, "Pool A", rqID("Alpha"), 3)
	var played *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &played)
	require.Len(t, played.Blocking, 1)
	assert.Equal(t, final.ID, played.Blocking[0].ID)
	o, err := f.store.LoadOverrides(f.compID)
	require.NoError(t, err)
	assert.Equal(t, 1, o.PoolRanks["Pool A"]["id:"+rqID("Alpha")], "the refused override is taken back")

	var reopened []ReopenedMatch
	changed, err := f.eng.OverridePoolRank(f.compID, "Pool A", rqID("Alpha"), 3, ForceOptions{Force: true, Reopened: &reopened})
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
