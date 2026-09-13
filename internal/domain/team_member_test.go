package domain_test

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

// TestSquadMemberLabel verifies the "T10.1"-style composition (bc-pnum:
// "make a team member's label available to the public surfaces") and its
// two blank-out cases: no team number yet, and a non-positive member
// index (every real member is minted with a 1-based index, so this
// branch only guards against a zero-value/defaulted struct).
func TestSquadMemberLabel(t *testing.T) {
	tests := []struct {
		name        string
		teamNumber  string
		memberIndex int
		want        string
	}{
		{"composes number, dot, index", "T10", 1, "T10.1"},
		{"double-digit index", "K3", 12, "K3.12"},
		{"blank team number returns blank", "", 1, ""},
		{"zero index returns blank", "T10", 0, ""},
		{"negative index returns blank", "T10", -1, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, domain.SquadMemberLabel(tc.teamNumber, tc.memberIndex))
		})
	}
}
