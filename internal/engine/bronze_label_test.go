package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
)

// The 3rd-place match is numbered neither by assignBracketMatchNumbers (which
// walks Bracket.Rounds) nor by the printed tree, so without a name of its own
// MatchLabel's id fallback would put "m-bronze" in front of an operator.
func TestMatchLabel_NamesTheBronzeRatherThanItsID(t *testing.T) {
	assert.Equal(t, "the 3rd-place match", MatchLabel(ReopenedMatch{ID: state.BronzeMatchID}))
	assert.Equal(t, "Match 4", MatchLabel(ReopenedMatch{ID: state.BronzeMatchID, Number: 4}),
		"a number, when one exists, still wins")
	assert.Equal(t, "Match 7", MatchLabel(ReopenedMatch{ID: "m-r3-0", Number: 7}))
	assert.Equal(t, "m-r3-0", MatchLabel(ReopenedMatch{ID: "m-r3-0"}),
		"an unnumbered, unnamed match still falls back to its id")
}
