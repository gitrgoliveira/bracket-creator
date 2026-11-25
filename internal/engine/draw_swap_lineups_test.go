package engine

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- fixtures ---------------------------------------------------------------

func swapTeamComp(c *state.Competition) { c.Kind = "team"; c.TeamSize = 3 }

// teamMatchIDs lists the team matches that seat team in the stored draw, in
// match order.
func teamMatchIDs(t *testing.T, store *state.Store, compID, team string) []string {
	t.Helper()
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	draw := state.DrawMatchesFrom(loadSwapMatches(t, store, compID), bracket)
	return seatedMatches(state.TeamMatches(draw, comp.IsKnockoutEnabled()), team)
}

// saveSwapLineup stores team's lineup naming one fighter, for match (a
// match's own lineup) or, with match "", the team's starting lineup.
func saveSwapLineup(t *testing.T, store *state.Store, compID, team, match, fighter string) {
	t.Helper()
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: team, MatchID: match,
		Positions: map[domain.Position]string{domain.PositionNumbered(1): fighter},
	}, 3))
}

// storedLineups maps each stored lineup in force to its first fighter: a
// match's own lineup by (team, match), a team's starting lineup by (team, "").
func storedLineups(t *testing.T, store *state.Store, compID string) map[[2]string]string {
	t.Helper()
	lineups, err := store.LoadTeamLineups(compID)
	require.NoError(t, err)
	out := map[[2]string]string{}
	for _, l := range lineups {
		if l.MatchID == "" && l.Round != 0 {
			continue
		}
		out[[2]string{l.TeamID, l.MatchID}] = l.Positions[domain.PositionNumbered(1)]
	}
	return out
}

func lineupsFile(t *testing.T, dir, compID string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "lineups.yaml"))
	if err != nil && !os.IsNotExist(err) {
		require.NoError(t, err)
	}
	return string(raw)
}

// --- the mapping ------------------------------------------------------------

func TestSwapDrawPlaces_TeamMatchMap(t *testing.T) {
	assert.Equal(t, map[string]string{"a1": "b1", "a2": "b2"}, teamMatchMap([]string{"a1", "a2"}, []string{"b1", "b2"}),
		"the k-th match of the old place maps to the k-th of the new one")
	assert.Equal(t, map[string]string{"a1": "b1", "a2": "b2", "a3": ""}, teamMatchMap([]string{"a1", "a2", "a3"}, []string{"b1", "b2"}),
		"a playing order the new place has no match for maps to none")
	assert.Equal(t, map[string]string{"a1": "b1"}, teamMatchMap([]string{"a1"}, []string{"b1", "b2"}))
	assert.Equal(t, map[string]string{"ac": "bc"}, teamMatchMap([]string{"ab", "ac"}, []string{"ab", "bc"}),
		"a match the team still plays keeps its lineup and is left out of the pairing")
	assert.Empty(t, teamMatchMap([]string{"m0"}, []string{"m0"}))
}

// --- pools ------------------------------------------------------------------

func TestSwapDrawPlaces_LineupsMoveWithTheTeam(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "swap-lineups"
	drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(6), swapTeamComp)
	pools := loadSwapPools(t, store, compID)
	require.Len(t, pools, 2)
	a, b := poolSwapSide(pools, 0, 0), poolSwapSide(pools, 1, 1)
	opponent := pools[0].Players[1].ID
	oldA, oldB := teamMatchIDs(t, store, compID, a.ID), teamMatchIDs(t, store, compID, b.ID)
	require.Len(t, oldA, 2)
	require.Len(t, oldB, 2)
	var vsA string
	for _, id := range teamMatchIDs(t, store, compID, opponent) {
		if slices.Contains(oldA, id) {
			vsA = id
		}
	}
	require.NotEmpty(t, vsA)

	saveSwapLineup(t, store, compID, a.ID, oldA[0], "A-first")
	saveSwapLineup(t, store, compID, a.ID, oldA[1], "A-second")
	saveSwapLineup(t, store, compID, a.ID, "", "A-start")
	saveSwapLineup(t, store, compID, b.ID, oldB[0], "B-first")
	saveSwapLineup(t, store, compID, opponent, vsA, "opponent")
	fileBefore := lineupsFile(t, dir, compID)

	dry, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, 3, dry.LineupsMoved, "the check says what the swap moves")
	assert.Zero(t, dry.LineupsRemoved)
	assert.Equal(t, fileBefore, lineupsFile(t, dir, compID), "a dry run moves no lineup")

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
	require.NoError(t, err)
	require.True(t, got.Applied)
	assert.Equal(t, 3, got.LineupsMoved)
	assert.Zero(t, got.LineupsRemoved)

	newA, newB := teamMatchIDs(t, store, compID, a.ID), teamMatchIDs(t, store, compID, b.ID)
	assert.Equal(t, oldB, newA, "the team plays the matches of the place it took")
	assert.Equal(t, oldA, newB)
	want := map[[2]string]string{
		{a.ID, newA[0]}: "A-first",
		{a.ID, newA[1]}: "A-second",
		{a.ID, ""}:      "A-start",
		{b.ID, newB[0]}: "B-first",
		{opponent, vsA}: "opponent",
	}
	assert.Equal(t, want, storedLineups(t, store, compID),
		"each lineup moves to the match the team plays in the same order; the opponent's and the starting lineup stay")

	inForce, err := eng.LineupInForce(compID, b.ID, newB[1])
	require.NoError(t, err)
	assert.Equal(t, "B-first", inForce.Lineup.Positions[domain.PositionNumbered(1)],
		"the team's next match carries the lineup that moved with it")
}

func TestSwapDrawPlaces_LineupsInsideOnePool(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-lineups-within"
	drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(6), swapTeamComp)
	pools := loadSwapPools(t, store, compID)
	a, b := poolSwapSide(pools, 0, 0), poolSwapSide(pools, 0, 2)
	oldA := teamMatchIDs(t, store, compID, a.ID)
	oldB := teamMatchIDs(t, store, compID, b.ID)
	var both, aOnly string
	for _, id := range oldA {
		if slices.Contains(oldB, id) {
			both = id
		} else {
			aOnly = id
		}
	}
	require.NotEmpty(t, both, "a round robin of three holds the two")
	require.NotEmpty(t, aOnly)
	saveSwapLineup(t, store, compID, a.ID, both, "A-vs-B")
	saveSwapLineup(t, store, compID, a.ID, aOnly, "A-other")
	saveSwapLineup(t, store, compID, b.ID, both, "B-vs-A")

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
	require.NoError(t, err)
	require.True(t, got.Applied)
	assert.Equal(t, 1, got.LineupsMoved, "the match holding both stays; only A's other lineup moves")
	assert.Zero(t, got.LineupsRemoved)

	newA := teamMatchIDs(t, store, compID, a.ID)
	require.Contains(t, newA, both)
	var aNew string
	for _, id := range newA {
		if id != both {
			aNew = id
		}
	}
	assert.Equal(t, map[[2]string]string{
		{a.ID, both}: "A-vs-B",
		{b.ID, both}: "B-vs-A",
		{a.ID, aNew}: "A-other",
	}, storedLineups(t, store, compID))
}

func TestSwapDrawPlaces_LineupWithNoMatchAtTheNewPlaceIsRemoved(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-lineups-smaller"
	drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(7), swapTeamComp)
	pools := loadSwapPools(t, store, compID)
	require.Len(t, pools, 2)
	big, small := 0, 1
	if len(pools[1].Players) > len(pools[0].Players) {
		big, small = 1, 0
	}
	require.Equal(t, len(pools[small].Players)+1, len(pools[big].Players), "fixture: pools of four and three")
	a, b := poolSwapSide(pools, big, 0), poolSwapSide(pools, small, 0)
	oldA, oldB := teamMatchIDs(t, store, compID, a.ID), teamMatchIDs(t, store, compID, b.ID)
	require.Len(t, oldA, 3)
	require.Len(t, oldB, 2)
	for i, id := range oldA {
		saveSwapLineup(t, store, compID, a.ID, id, []string{"A1", "A2", "A3"}[i])
	}
	for i, id := range oldB {
		saveSwapLineup(t, store, compID, b.ID, id, []string{"B1", "B2"}[i])
	}

	dry, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b, DryRun: true})
	require.NoError(t, err)
	assert.Equal(t, 4, dry.LineupsMoved)
	assert.Equal(t, 1, dry.LineupsRemoved, "the check says a lineup goes before the swap")

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
	require.NoError(t, err)
	assert.Equal(t, 4, got.LineupsMoved)
	assert.Equal(t, 1, got.LineupsRemoved)
	newA, newB := teamMatchIDs(t, store, compID, a.ID), teamMatchIDs(t, store, compID, b.ID)
	require.Len(t, newA, 2)
	require.Len(t, newB, 3)
	assert.Equal(t, map[[2]string]string{
		{a.ID, newA[0]}: "A1",
		{a.ID, newA[1]}: "A2",
		{b.ID, newB[0]}: "B1",
		{b.ID, newB[1]}: "B2",
	}, storedLineups(t, store, compID), "A's third lineup has no match at the smaller pool and is removed")
}

// --- knockout ---------------------------------------------------------------

func TestSwapDrawPlaces_KnockoutLineupsMoveWithTheTeam(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-lineups-ko"
	drawDojoComp(t, eng, store, compID, state.CompFormatKnockout, 3, uniqueDojos(8), swapTeamComp)
	before := loadBracket(t, store, compID)
	m0, m1 := before.Rounds[0][0], before.Rounds[0][1]
	saveSwapLineup(t, store, compID, m0.SideAID, m0.ID, "A")
	saveSwapLineup(t, store, compID, m0.SideBID, m0.ID, "opponent")
	saveSwapLineup(t, store, compID, m1.SideAID, m1.ID, "B")

	got, err := eng.SwapDrawPlaces(compID, SwapRequest{A: SwapSide{ID: m0.SideAID, Place: swapSlot(0)}, B: SwapSide{ID: m1.SideAID, Place: swapSlot(2)}})
	require.NoError(t, err)
	assert.Equal(t, 2, got.LineupsMoved)
	assert.Equal(t, map[[2]string]string{
		{m0.SideAID, m1.ID}: "A",
		{m1.SideAID, m0.ID}: "B",
		{m0.SideBID, m0.ID}: "opponent",
	}, storedLineups(t, store, compID), "the first-round lineup moves to the new first-round match")

	// Two opponents trading sides both still play the match: nothing moves.
	after := loadBracket(t, store, compID).Rounds[0][0]
	flip, err := eng.SwapDrawPlaces(compID, SwapRequest{A: SwapSide{ID: after.SideAID, Place: swapSlot(0)}, B: SwapSide{ID: after.SideBID, Place: swapSlot(1)}})
	require.NoError(t, err)
	require.True(t, flip.Applied)
	assert.Zero(t, flip.LineupsMoved)
	assert.Equal(t, map[[2]string]string{
		{m0.SideAID, m1.ID}: "A",
		{m1.SideAID, m0.ID}: "B",
		{m0.SideBID, m0.ID}: "opponent",
	}, storedLineups(t, store, compID))
}

// --- the round-lineup settlement ----------------------------------------------

func TestSwapDrawPlaces_RoundLineupPairsMoveWithTheTeam(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "swap-lineups-given"
	drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(6), swapTeamComp)
	pools := loadSwapPools(t, store, compID)
	a, b := poolSwapSide(pools, 0, 0), poolSwapSide(pools, 1, 1)
	other := pools[0].Players[1].ID
	oldA, oldB := teamMatchIDs(t, store, compID, a.ID), teamMatchIDs(t, store, compID, b.ID)
	oldOther := teamMatchIDs(t, store, compID, other)
	sorted := func(ids []string) []string { out := slices.Clone(ids); slices.Sort(out); return out }

	// A tournament an older release recorded: A and B are legacy teams with a
	// round lineup, and the settlement gave them (and C) their matches.
	for _, team := range []string{a.ID, b.ID} {
		require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
			TeamID: team, Round: 1, Positions: map[domain.Position]string{domain.PositionNumbered(1): "round-1"},
		}, 3))
	}
	for i, id := range oldA {
		saveSwapLineup(t, store, compID, a.ID, id, []string{"A1", "A2"}[i])
	}
	for i, id := range oldB {
		saveSwapLineup(t, store, compID, b.ID, id, []string{"B1", "B2"}[i])
	}
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.RoundLineupsConverted = false
	comp.RoundLineupsLegacy = sorted([]string{a.ID, b.ID})
	comp.RoundLineupsGiven = map[string][]string{a.ID: sorted(oldA), b.ID: sorted(oldB), other: sorted(oldOther)}
	require.NoError(t, store.SaveCompetition(comp))
	lineupsBefore := storedLineups(t, store, compID)

	_, err = eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
	require.NoError(t, err)
	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{a.ID: sorted(oldB), b.ID: sorted(oldA), other: sorted(oldOther)}, comp.RoundLineupsGiven,
		"the settled pairs move with the team, so nothing is given twice or withheld")
	newA := teamMatchIDs(t, store, compID, a.ID)
	assert.Equal(t, "A1", storedLineups(t, store, compID)[[2]string{a.ID, newA[0]}], "the settlement gave no reading over the moved lineup")

	// Swap back: every lineup and every pair is where it was.
	pools = loadSwapPools(t, store, compID)
	_, err = eng.SwapDrawPlaces(compID, SwapRequest{A: poolSwapSide(pools, 0, 0), B: poolSwapSide(pools, 1, 1)})
	require.NoError(t, err)
	comp, err = store.LoadCompetition(compID)
	require.NoError(t, err)
	assert.Equal(t, map[string][]string{a.ID: sorted(oldA), b.ID: sorted(oldB), other: sorted(oldOther)}, comp.RoundLineupsGiven)
	assert.Equal(t, lineupsBefore, storedLineups(t, store, compID))
}

// --- one transaction ----------------------------------------------------------

func TestSwapDrawPlaces_ALineupThatCannotMoveLeavesEverythingWhereItWas(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "swap-lineups-fail"
	drawDojoComp(t, eng, store, compID, state.CompFormatMixed, 3, uniqueDojos(6), swapTeamComp)
	pools := loadSwapPools(t, store, compID)
	a, b := poolSwapSide(pools, 0, 0), poolSwapSide(pools, 1, 1)
	oldA := teamMatchIDs(t, store, compID, a.ID)
	saveSwapLineup(t, store, compID, b.ID, teamMatchIDs(t, store, compID, b.ID)[0], "B1")
	// A position a three-person team has no place for, kept from older data:
	// the store refuses to write it again, so the move fails.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: a.ID, MatchID: oldA[1],
		Positions: map[domain.Position]string{domain.PosJiho: "A-jiho"},
	}, 5))
	draw, lineups := drawFiles(t, dir, compID), lineupsFile(t, dir, compID)

	_, err := eng.SwapDrawPlaces(compID, SwapRequest{A: a, B: b})
	require.Error(t, err)
	assert.Equal(t, draw, drawFiles(t, dir, compID), "the draw files did not change")
	assert.Equal(t, lineups, lineupsFile(t, dir, compID), "every lineup stayed where it was")
	assert.Equal(t, pools, loadSwapPools(t, store, compID))
}
