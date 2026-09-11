package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCanStartCanGenerateDraw pins the two status predicates the engine's own
// StartCompetition/GenerateDraw switches gate on (bc-pnum: moved here from
// internal/engine so a squad write, internal/state, could gate on the same
// precondition without an upward import), which the mobileapp
// start/generate-draw pre-flights (ensureNumberPrefix) share so the two cannot
// drift: a status the engine accepts but the pre-flight skips would let a
// legacy competition reach the draw with an empty prefix.
func TestCanStartCanGenerateDraw(t *testing.T) {
	for _, tc := range []struct {
		status      CompetitionStatus
		start, draw bool
	}{
		{CompStatusSetup, true, true},
		{"", true, true},
		{CompStatusDrawReady, true, false},
		{CompStatusPools, false, false},
		{CompStatusPlayoffs, false, false},
		{CompStatusComplete, false, false},
		{CompStatusInvalid, false, false},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			assert.Equal(t, tc.start, CanStart(tc.status), "CanStart")
			assert.Equal(t, tc.draw, CanGenerateDraw(tc.status), "CanGenerateDraw")
		})
	}
}
