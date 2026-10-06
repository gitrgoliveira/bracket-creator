package state

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestDrawMatchPlace pins the order a team plays its matches in, the one the
// lineup rule and the round-lineup settlement share: the pool phase in pool-match
// number, a Swiss team's rounds in round order, the knockout after it by round
// then position, the 3rd-place match last.
func TestDrawMatchPlace(t *testing.T) {
	draw := []DrawMatch{
		{ID: "r1-m0", Knockout: true, Round: 1, Index: 0},
		{ID: "Pool A-10"},
		{ID: "bronze", Knockout: true, Round: 2},
		{ID: "r0-m1", Knockout: true, Round: 0, Index: 1},
		{ID: "Pool A-2"},
		{ID: "Swiss-R2-0"},
		{ID: "r0-m0", Knockout: true, Round: 0, Index: 0},
		{ID: "Swiss-R1-5"},
		{ID: "Pool A-DH-0"},
		{ID: "Pool A-TB-3"},
		{ID: ""},
	}

	var team []string
	var order []MatchPlace
	for seq, m := range draw {
		if place, ok := m.Place(seq); ok {
			team = append(team, m.ID)
			order = append(order, place)
		}
	}
	sorted := slices.Clone(team)
	slices.SortFunc(sorted, func(a, b string) int {
		return order[slices.Index(team, a)].Compare(order[slices.Index(team, b)])
	})

	assert.Equal(t, []string{"Pool A-2", "Pool A-10", "Swiss-R1-5", "Swiss-R2-0", "r0-m0", "r0-m1", "r1-m0", "bronze"}, sorted,
		"pool numbers compare as numbers; a pool match has Swiss round 0, so it comes before any Swiss round")
	assert.NotContains(t, team, "Pool A-DH-0", "a representative bout is one individual bout, not a team match")
	assert.NotContains(t, team, "Pool A-TB-3", "nor is a tiebreaker")
	assert.NotContains(t, team, "", "a match with no id cannot be named")
}

func TestMatchPlaceCompareBreaksTieBySeq(t *testing.T) {
	a, _ := DrawMatch{ID: "Pool A-0"}.Place(0)
	b, _ := DrawMatch{ID: "Pool A-0"}.Place(1)

	assert.Negative(t, a.Compare(b))
	assert.Positive(t, b.Compare(a))
	assert.Zero(t, a.Compare(a))
}

func TestDrawMatchSeats(t *testing.T) {
	m := DrawMatch{ID: "m", SideAID: "a", SideBID: "b"}

	assert.True(t, m.Seats("a"))
	assert.True(t, m.Seats("b"))
	assert.False(t, m.Seats("c"))
	assert.False(t, m.Seats(""), "a side with no id seats nobody")
	assert.False(t, DrawMatch{SideAID: "a"}.Seats(""), "and an empty id is nobody's")
}

func TestParseSwissMatchRoundAndPoolPhaseMatchNumber(t *testing.T) {
	for id, want := range map[string]int{"Swiss-R1-0": 1, "Swiss-R12-3": 12} {
		n, ok := ParseSwissMatchRound(id)
		assert.True(t, ok, id)
		assert.Equal(t, want, n, id)
	}
	for _, id := range []string{"Pool A-0", "Swiss-R0-1", "Swiss-Rx-1", "Swiss-R2", ""} {
		_, ok := ParseSwissMatchRound(id)
		assert.False(t, ok, id)
	}

	for id, want := range map[string]int{"Pool A-0": 1, "Pool A-9": 10, "Swiss-R2-4": 5, "Pool A-DH-1": 0, "Pool A-TB-1": 0, "nodash": 0, "Pool A-x": 0} {
		assert.Equal(t, want, PoolPhaseMatchNumber(id), id)
	}
}
