package state

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

// A legacy sub-bout flag is folded into the mark by the SUB-BOUT rule: the
// member ids decide, and a display name both fighters share decides nothing.
// Two fighters on opposing teams may legally share a name, so the aka-first tie
// the match level takes on a winner naming both sides is a coin flip here.
func TestSubBoutLegacyHanteiFoldsByMemberID(t *testing.T) {
	t.Run("member ids place the mark on the winner's side", func(t *testing.T) {
		s := &SubMatchResult{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			SideAMemberID: "m-aka", SideBMemberID: "m-shiro", WinnerMemberID: "m-shiro",
			IpponsA: []string{}, IpponsB: []string{},
			DecidedByHantei: boolPtr(true),
		}
		s.normalizeLegacyHantei()
		assert.Empty(t, s.IpponsA, "the losing fighter's slots stay markless")
		assert.Equal(t, []string{domain.HanteiMark}, s.IpponsB,
			"the mark belongs to the fighter the member ids name as the winner")
	})

	t.Run("a shared name with no member ids places no mark", func(t *testing.T) {
		s := &SubMatchResult{
			Position: 1, SideA: "Yamada", SideB: "Yamada", Winner: "Yamada",
			IpponsA: []string{}, IpponsB: []string{},
			DecidedByHantei: boolPtr(true),
		}
		s.normalizeLegacyHantei()
		assert.Empty(t, s.IpponsA, "neither side may be guessed when the name names both")
		assert.Empty(t, s.IpponsB, "neither side may be guessed when the name names both")
	})

	t.Run("distinct names still fold by name", func(t *testing.T) {
		s := &SubMatchResult{
			Position: 1, SideA: "Sato", SideB: "Tanaka", Winner: "Tanaka",
			IpponsA: []string{}, IpponsB: []string{},
			DecidedByHantei: boolPtr(true),
		}
		s.normalizeLegacyHantei()
		assert.Empty(t, s.IpponsA)
		assert.Equal(t, []string{domain.HanteiMark}, s.IpponsB,
			"a row whose names separate the fighters keeps the pre-id behaviour")
	})
}
