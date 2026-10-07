package engine

import (
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// PrintedBracketMatch is how the Elimination Matches sheet prints one stored
// bracket match, for the workbook writers that name it from the stored
// bracket rather than from the sheet's own formulas.
type PrintedBracketMatch struct {
	// Title is the header over the match's block: helper.EliminationMatchTitle
	// ("Round 2 - Match 3") with the round counted from the first round as
	// the sheet counts it, or helper.ThirdPlaceLabel for the 3rd-place match.
	// Empty for a match the sheet prints no block for (a bye) and for a
	// bracket stored before match numbers were.
	Title string
	// SideA and SideB are the match's sides: a competitor, or a pool
	// placeholder such as "Pool A-1st", as stored; a side an earlier match
	// decides, by that match's number, helper.MatchRefLabel's "M 3". Blank
	// for a bye's empty side, or when the feeding match is not recorded.
	SideA, SideB string
}

// PrintedBracket returns, by match id, how the Elimination Matches sheet
// prints every stored bracket match, the 3rd-place match included, whose
// sides are the losers of the two matches feeding the final.
func PrintedBracket(bracket *state.Bracket) map[string]PrintedBracketMatch {
	out := map[string]PrintedBracketMatch{}
	if bracket == nil {
		return out
	}
	// DisplayRound counts from the final (1), so the first round printed is
	// the highest one stored.
	numberByID := map[string]int{}
	firstRound := 0
	for _, round := range bracket.Rounds {
		for _, bm := range round {
			numberByID[bm.ID] = bm.MatchNumber
			firstRound = max(firstRound, bm.DisplayRound)
		}
	}
	side := func(name string, feeders []string, i int) string {
		if name != "" && !helper.IsWinnerOfPlaceholder(name) {
			return name
		}
		if i < len(feeders) {
			if n := numberByID[feeders[i]]; n > 0 {
				return helper.MatchRefLabel(n)
			}
		}
		return ""
	}
	for _, round := range bracket.Rounds {
		for _, bm := range round {
			printed := PrintedBracketMatch{SideA: side(bm.SideA, bm.Feeders, 0), SideB: side(bm.SideB, bm.Feeders, 1)}
			if bm.MatchNumber > 0 && bm.DisplayRound > 0 {
				printed.Title = helper.EliminationMatchTitle(firstRound-bm.DisplayRound+1, bm.MatchNumber)
			}
			out[bm.ID] = printed
		}
	}
	if bm := bracket.ThirdPlaceMatch; bm != nil {
		var finalFeeders []string
		if n := len(bracket.Rounds); n > 0 && len(bracket.Rounds[n-1]) > 0 {
			finalFeeders = bracket.Rounds[n-1][0].Feeders
		}
		out[bm.ID] = PrintedBracketMatch{
			Title: helper.ThirdPlaceLabel,
			SideA: side(bm.SideA, finalFeeders, 0),
			SideB: side(bm.SideB, finalFeeders, 1),
		}
	}
	return out
}
