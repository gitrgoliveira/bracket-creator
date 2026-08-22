package state

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

// A legacy decidedByHantei flag on a SAME-NAME pair can only be attributed by
// participant id: the winner name matches both sides, so the name path would
// resolve sideA-first and could mark the loser. The fold therefore goes
// through domain.AttributeWinnerSide like every other attribution site.
func TestLegacyHanteiFold_AttributesSameNamePairByID(t *testing.T) {
	t.Run("ids place the mark on B where names would pick A", func(t *testing.T) {
		m := &MatchResult{
			SideA: "Yuki Tanaka", SideB: "Yuki Tanaka", Winner: "Yuki Tanaka",
			SideAID: "id-a", SideBID: "id-b", WinnerID: "id-b",
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
			DecidedByHantei: boolPtr(true),
		}
		m.NormalizeLegacyHantei()
		assert.False(t, domain.ContainsHantei(m.IpponsA), "the loser's slice must stay markless")
		assert.True(t, domain.ContainsHantei(m.IpponsB), "the id names side B as the winner")
	})

	t.Run("id-less legacy rows keep the name path unchanged", func(t *testing.T) {
		m := &MatchResult{
			SideA: "Aiko", SideB: "Ben", Winner: "Ben",
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
			DecidedByHantei: boolPtr(true),
		}
		m.NormalizeLegacyHantei()
		assert.False(t, domain.ContainsHantei(m.IpponsA))
		assert.True(t, domain.ContainsHantei(m.IpponsB))
	})

	t.Run("ids present but naming neither side stays unattributable", func(t *testing.T) {
		m := &MatchResult{
			SideA: "Aiko", SideB: "Ben", Winner: "Aiko",
			SideAID: "id-a", SideBID: "id-b", WinnerID: "id-gone",
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
			DecidedByHantei: boolPtr(true),
		}
		m.NormalizeLegacyHantei()
		assert.False(t, domain.ContainsHantei(m.IpponsA), "drop, never guess")
		assert.False(t, domain.ContainsHantei(m.IpponsB))
	})
}

func boolPtr(v bool) *bool { return &v }
