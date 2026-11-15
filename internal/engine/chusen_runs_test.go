package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// runRows is a tied group of level rows (same Points), in the order given.
func runRows(ids ...string) []state.PlayerStanding {
	rows := make([]state.PlayerStanding, len(ids))
	for i, id := range ids {
		rows[i] = state.PlayerStanding{Player: domain.Player{ID: id, Name: id}, Points: 3}
	}
	return rows
}

// tbBout is the tie-break bout n between a and b: won by winner, or drawn
// (nobody fought it) when winner is empty.
func tbBout(n int, a, b, winner string) state.MatchResult {
	return state.MatchResult{
		ID: fmt.Sprintf("Pool A-TB-%d", n), SideA: a, SideAID: a, SideB: b, SideBID: b,
		Status: state.MatchStatusCompleted, Winner: winner, WinnerID: winner,
	}
}

func inForceIDs(rows []state.PlayerStanding, inForce map[string]int) []string {
	var ids []string
	for _, r := range rows {
		if _, ok := lookupPoolRankOverride(inForce, r.Player.ID); ok {
			ids = append(ids, r.Player.ID)
		}
	}
	return ids
}

// Lots rank a run the fought bouts left level, one run at a time: once a bout of
// one run has a winner, that run's lots stop applying and the bout decides its
// places, while another run of the same Points group keeps its lots.
func TestPoolRanksInForce_JudgedOnEachRun(t *testing.T) {
	rows := runRows("a", "b", "c", "d")
	// a and b beat c and d; a-b and c-d were drawn (barred), so each pair is a run.
	round := func(abWinner string) []state.MatchResult {
		return []state.MatchResult{
			tbBout(0, "a", "b", abWinner), tbBout(1, "c", "d", ""),
			tbBout(2, "a", "c", "a"), tbBout(3, "a", "d", "a"),
			tbBout(4, "b", "c", "b"), tbBout(5, "b", "d", "b"),
		}
	}
	recorded := map[string]int{
		helper.CompetitorKey("b", "", ""): 1, helper.CompetitorKey("a", "", ""): 2,
		helper.CompetitorKey("d", "", ""): 3, helper.CompetitorKey("c", "", ""): 4,
	}

	assert.Equal(t, []string{"a", "b", "c", "d"}, inForceIDs(rows, poolRanksInForce(rows, round(""), false, recorded)),
		"two level runs, both ranked: every lot is in force")
	assert.Equal(t, []string{"c", "d"}, inForceIDs(rows, poolRanksInForce(rows, round("a"), false, recorded)),
		"a won bout separates its run: only the run still level keeps its lots")
}

// The lots order a run inside its own places: a competitor the bouts put clear of
// it keeps the place they gave, whatever rank the lots recorded for it.
func TestApplyPoolRanksInForce_OrdersWithinARun(t *testing.T) {
	rows := runRows("a", "b", "c", "d")
	rows[0].TieRun, rows[1].TieRun, rows[2].TieRun, rows[3].TieRun = 0, 1, 2, 2
	applyPoolRanksInForce(rows, map[string]int{
		helper.CompetitorKey("c", "", ""): 4, helper.CompetitorKey("d", "", ""): 3,
	})
	var order []string
	for _, r := range rows {
		order = append(order, r.Player.ID)
	}
	assert.Equal(t, []string{"a", "b", "d", "c"}, order)

	// A rank that would put a run's member above a competitor the bouts placed
	// higher stays inside the run.
	rows = runRows("a", "b", "c", "d")
	rows[0].TieRun, rows[1].TieRun, rows[2].TieRun, rows[3].TieRun = 0, 1, 2, 2
	applyPoolRanksInForce(rows, map[string]int{
		helper.CompetitorKey("c", "", ""): 2, helper.CompetitorKey("d", "", ""): 1,
	})
	order = nil
	for _, r := range rows {
		order = append(order, r.Player.ID)
	}
	assert.Equal(t, []string{"a", "b", "d", "c"}, order, "lots never move a member out of its run")
}

// A circle left level at 3rd to 5th inside a group that started at 1st is judged
// on its own places: with joint 3rd places awarded it shares 3rd and owes no
// lots, though the group's best place is 1st.
func TestGroupNeedsChusen_JudgesEachRunOnItsOwnPlaces(t *testing.T) {
	group := runRows("a", "b", "c", "d", "e")
	// a beats everyone, b beats c, d and e, and c, d and e form a circle.
	matches := []state.MatchResult{
		tbBout(0, "a", "b", "a"), tbBout(1, "a", "c", "a"), tbBout(2, "a", "d", "a"), tbBout(3, "a", "e", "a"),
		tbBout(4, "b", "c", "b"), tbBout(5, "b", "d", "b"), tbBout(6, "b", "e", "b"),
		tbBout(7, "c", "d", "c"), tbBout(8, "d", "e", "d"), tbBout(9, "c", "e", "e"),
	}
	positions := []int{0, 1, 2, 3, 4}
	belowTheBand := func(runPositions []int, _ []state.PlayerStanding) bool { return runPositions[0] < 2 }

	assert.True(t, groupNeedsChusen(group, positions, matches, inEveryBand), "in the band the circle owes lots")
	assert.False(t, groupNeedsChusen(group, positions, matches, belowTheBand), "a circle at 3rd to 5th shares 3rd and owes none")
	assert.False(t, groupSettledByChusen(group, positions, matches, belowTheBand), "and has none recorded")

	for i := 2; i < 5; i++ {
		group[i].IsOverridden = true
	}
	assert.False(t, groupNeedsChusen(group, positions, matches, inEveryBand), "lots over the circle settle it")
	assert.True(t, groupSettledByChusen(group, positions, matches, inEveryBand), "and they stay correctable")
}
