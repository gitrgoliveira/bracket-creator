package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestQueuePositionDerivation verifies FR-025: per-court queue positions
// are derived (not stored) from a list of MatchResult values.
//
// Rule: live (running) matches and completed matches both have
// queuePosition = 0. Scheduled (non-completed) matches on the same court
// are numbered 1, 2, 3, ... in list order.
//
// This is a Red test — DeriveQueuePositions does not yet exist in the
// state package and the build must fail until the Green implementation
// (T034 family) lands.
func TestQueuePositionDerivation(t *testing.T) {
	input := []MatchResult{
		{ID: "m1", Status: MatchStatusRunning, Court: "A"},
		{ID: "m2", Status: MatchStatusScheduled, Court: "A"},
		{ID: "m3", Status: MatchStatusScheduled, Court: "A"},
		{ID: "m4", Status: MatchStatusCompleted, Court: "A"},
		{ID: "m5", Status: MatchStatusScheduled, Court: "A"},
	}

	got := DeriveQueuePositions(input)

	assert.Equal(t, []int{0, 1, 2, 0, 3}, got,
		"queue positions: live=0, scheduled counted in order, completed=0")
}
