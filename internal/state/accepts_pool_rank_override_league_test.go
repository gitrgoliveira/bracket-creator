package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A finished league keeps its lots correctable (decision F2 (a)); a finished
// competition of any other format does not take a pool order by hand.
func TestCompetition_AcceptsPoolRankOverride_FinishedLeague(t *testing.T) {
	assert.True(t, Competition{Format: CompFormatLeague, Status: CompStatusComplete}.AcceptsPoolRankOverride())
	for _, format := range []string{CompFormatMixed, CompFormatKnockout, CompFormatSwiss, ""} {
		assert.False(t, Competition{Format: format, Status: CompStatusComplete}.AcceptsPoolRankOverride(), "format=%q", format)
	}
	assert.False(t, Competition{Format: CompFormatLeague, Status: CompStatusKnockout}.AcceptsPoolRankOverride())
	assert.False(t, Competition{Format: CompFormatLeague, Status: CompStatusInvalid}.AcceptsPoolRankOverride())
}
