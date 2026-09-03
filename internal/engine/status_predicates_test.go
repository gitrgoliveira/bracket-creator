package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
)

// TestCanStartCanGenerateDraw pins the two status predicates the engine's own
// StartCompetition/GenerateDraw switches gate on, which the mobileapp
// start/generate-draw pre-flights (ensureNumberPrefix) share so the two cannot
// drift: a status the engine accepts but the pre-flight skips would let a
// legacy competition reach the draw with an empty prefix.
func TestCanStartCanGenerateDraw(t *testing.T) {
	for _, tc := range []struct {
		status      state.CompetitionStatus
		start, draw bool
	}{
		{state.CompStatusSetup, true, true},
		{"", true, true},
		{state.CompStatusDrawReady, true, false},
		{state.CompStatusPools, false, false},
		{state.CompStatusPlayoffs, false, false},
		{state.CompStatusComplete, false, false},
		{state.CompStatusInvalid, false, false},
	} {
		t.Run(string(tc.status), func(t *testing.T) {
			assert.Equal(t, tc.start, CanStart(tc.status), "CanStart")
			assert.Equal(t, tc.draw, CanGenerateDraw(tc.status), "CanGenerateDraw")
		})
	}
}
