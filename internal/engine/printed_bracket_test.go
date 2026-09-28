package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestPrintedBracket pins how the workbook names a stored bracket match: the
// Elimination Matches title over its block, with the round counted from the
// first round printed; a side an earlier match decides as "M n"; the 3rd-place
// sides as the matches feeding the final; and nothing for a bye or for data
// that records no match number or feeder.
func TestPrintedBracket(t *testing.T) {
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "r1-0", SideA: "Pool A-1st", SideB: "Pool B-2nd", MatchNumber: 1, DisplayRound: 3},
				{ID: "r1-1", SideA: "Ann", SideB: "", Hidden: true},
				{ID: "r1-2", SideA: "Pool B-1st", SideB: "Pool A-2nd", MatchNumber: 2, DisplayRound: 3},
				{ID: "r1-3", SideA: "Cid", SideB: "Dan", MatchNumber: 3, DisplayRound: 3},
			},
			{
				{ID: "r2-0", SideA: "Winner of r3-m0", SideB: "Ann", MatchNumber: 4, DisplayRound: 2, Feeders: []string{"r1-0", ""}},
				{ID: "r2-1", SideA: "Kodokan", SideB: "Winner of r3-m3", MatchNumber: 5, DisplayRound: 2, Feeders: []string{"r1-2", "r1-3"}},
			},
			{
				{ID: "r3-0", SideA: "Winner of r2-m0", SideB: "Winner of r2-m1", MatchNumber: 6, DisplayRound: 1, Feeders: []string{"r2-0", "r2-1"}},
			},
		},
		ThirdPlaceMatch: &state.BracketMatch{ID: state.BronzeMatchID, SideA: "", SideB: "Kodokan"},
	}

	printed := PrintedBracket(bracket)

	cases := []struct {
		id   string
		want PrintedBracketMatch
	}{
		{"r1-0", PrintedBracketMatch{Title: helper.EliminationMatchTitle(1, 1), SideA: "Pool A-1st", SideB: "Pool B-2nd"}},
		{"r1-1", PrintedBracketMatch{SideA: "Ann"}},
		{"r1-3", PrintedBracketMatch{Title: helper.EliminationMatchTitle(1, 3), SideA: "Cid", SideB: "Dan"}},
		{"r2-0", PrintedBracketMatch{Title: helper.EliminationMatchTitle(2, 4), SideA: helper.MatchRefLabel(1), SideB: "Ann"}},
		{"r2-1", PrintedBracketMatch{Title: helper.EliminationMatchTitle(2, 5), SideA: "Kodokan", SideB: helper.MatchRefLabel(3)}},
		{"r3-0", PrintedBracketMatch{Title: helper.EliminationMatchTitle(3, 6), SideA: helper.MatchRefLabel(4), SideB: helper.MatchRefLabel(5)}},
		{state.BronzeMatchID, PrintedBracketMatch{Title: helper.ThirdPlaceLabel, SideA: helper.MatchRefLabel(4), SideB: "Kodokan"}},
	}
	for _, tc := range cases {
		t.Run(tc.id, func(t *testing.T) {
			assert.Equal(t, tc.want, printed[tc.id])
		})
	}

	t.Run("a bracket stored before match numbers and feeders", func(t *testing.T) {
		legacy := PrintedBracket(&state.Bracket{Rounds: [][]state.BracketMatch{
			{{ID: "a", SideA: "Ann", SideB: "Bea"}},
			{{ID: "b", SideA: "Winner of r2-m0", SideB: "Cid"}},
		}})
		assert.Equal(t, PrintedBracketMatch{SideA: "Ann", SideB: "Bea"}, legacy["a"])
		assert.Equal(t, PrintedBracketMatch{SideA: "", SideB: "Cid"}, legacy["b"], "an unknown feeder leaves the side blank")
	})

	t.Run("a competitor named like the start of a feeder label", func(t *testing.T) {
		named := PrintedBracket(&state.Bracket{Rounds: [][]state.BracketMatch{
			{{ID: "a", SideA: "Ann", SideB: "Bea", MatchNumber: 1, DisplayRound: 2}},
			{{ID: "b", SideA: "Winner of Kyushu", SideB: "Winner of r2-m0", MatchNumber: 2, DisplayRound: 1, Feeders: []string{"x", "a"}}},
		}})
		assert.Equal(t, PrintedBracketMatch{Title: helper.EliminationMatchTitle(2, 2), SideA: "Winner of Kyushu", SideB: helper.MatchRefLabel(1)}, named["b"])
	})

	t.Run("no bracket", func(t *testing.T) {
		assert.Empty(t, PrintedBracket(nil))
	})
}
