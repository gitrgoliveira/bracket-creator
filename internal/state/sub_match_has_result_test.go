package state

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
)

// TestSubMatchResult_HasResult pins the Go twin of subBoutHasBeenPlayed
// (bc-tmfn): each kind of recorded input counts, and a row of placeholders
// or empty cells does not.
func TestSubMatchResult_HasResult(t *testing.T) {
	cases := []struct {
		name string
		sub  SubMatchResult
		want bool
	}{
		{"an untouched row", SubMatchResult{Position: 1, IpponsA: []string{}, IpponsB: []string{}}, false},
		{"unfilled slots only", SubMatchResult{Position: 1, IpponsA: []string{domain.IpponPlaceholder, ""}}, false},
		{"a winner", SubMatchResult{Position: 1, Winner: "A"}, true},
		{"a Tie", SubMatchResult{Position: 1, Decision: DecisionDraw}, true},
		{"a fusensho", SubMatchResult{Position: 1, Decision: string(domain.DecisionFusensho)}, true},
		{"a point for A", SubMatchResult{Position: 1, IpponsA: []string{"M"}}, true},
		{"a point for B", SubMatchResult{Position: 1, IpponsB: []string{"K"}}, true},
		{"the hantei mark", SubMatchResult{Position: 1, IpponsB: []string{domain.HanteiMark}}, true},
		{"a foul on A", SubMatchResult{Position: 1, HansokuA: 1}, true},
		{"a foul on B", SubMatchResult{Position: 1, HansokuB: 1}, true},
		{"an overtime", SubMatchResult{Position: 1, Encho: &EnchoMetadata{PeriodCount: 1}}, true},
		{"a zero-period encho block", SubMatchResult{Position: 1, Encho: &EnchoMetadata{}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, tc.sub.HasResult())
		})
	}
}
