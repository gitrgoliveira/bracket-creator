package state

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestQueuePositionDerivation verifies FR-025: per-court queue positions
// are derived (not stored) from a list of MatchResult values.
//
// Positions are assigned in (status priority, scheduledAt, original index)
// order within each court, matching ScheduleViewer and the JS SSE recompute.
// Running and completed matches receive 0.
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
		"queue positions: running=0, scheduled counted in (scheduledAt,idx) order, completed=0")
}

// TestQueuePositionDerivation_ScheduledAtOrdering verifies that within a court
// the queue counter increments in scheduledAt order, not slice order.
// A match at 09:00 that appears after a match at 10:00 in the slice must
// receive a lower (earlier) queue position, consistent with ScheduleViewer
// and _orderByCourtKey in patch.jsx.
func TestQueuePositionDerivation_ScheduledAtOrdering(t *testing.T) {
	input := []MatchResult{
		{ID: "late", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
		{ID: "early", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
	}

	got := DeriveQueuePositions(input)

	// "early" should be position 1, "late" should be position 2,
	// regardless of their slice order.
	assert.Equal(t, []int{2, 1}, got,
		"scheduledAt ordering: earlier time wins lower queue position regardless of slice order")
}

// TestQueuePositionDerivation_MultipleCourts verifies independent per-court counters.
func TestQueuePositionDerivation_MultipleCourts(t *testing.T) {
	input := []MatchResult{
		{ID: "a1", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "b1", Status: MatchStatusScheduled, Court: "B", ScheduledAt: "09:30"},
		{ID: "a2", Status: MatchStatusScheduled, Court: "A", ScheduledAt: "09:30"},
		{ID: "b2", Status: MatchStatusRunning, Court: "B"},
	}

	got := DeriveQueuePositions(input)

	assert.Equal(t, []int{1, 1, 2, 0}, got,
		"per-court counters are independent; running gets 0")
}

func TestRunningMatchOnCourt_FreeCourt(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusScheduled, Court: "A"},
	}))

	occ, err := store.RunningMatchOnCourt("A", "")
	require.NoError(t, err)
	assert.Nil(t, occ, "scheduled match should not block the court")
}

func TestRunningMatchOnCourt_BusySameCourt(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusRunning, Court: "A"},
	}))

	occ, err := store.RunningMatchOnCourt("A", "")
	require.NoError(t, err)
	require.NotNil(t, occ)
	assert.Equal(t, "comp1", occ.CompID)
	assert.Equal(t, "m1", occ.MatchID)
}

func TestRunningMatchOnCourt_SkipCompID(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusRunning, Court: "A"},
	}))

	// With skipCompID=comp1, the running match in comp1 should be ignored.
	occ, err := store.RunningMatchOnCourt("A", "comp1")
	require.NoError(t, err)
	assert.Nil(t, occ, "skipCompID competition should be excluded from scan")
}

func TestRunningMatchOnCourt_CrossCompetition(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	// comp1 has a running match on court A; comp2 wants to start on court A.
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusRunning, Court: "A"},
	}))
	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp2"}))
	require.NoError(t, store.SavePoolMatches("comp2", []MatchResult{
		{ID: "m2", Status: MatchStatusScheduled, Court: "A"},
	}))

	// Scanning from comp2's perspective (skipCompID=comp2) should find comp1's match.
	occ, err := store.RunningMatchOnCourt("A", "comp2")
	require.NoError(t, err)
	require.NotNil(t, occ)
	assert.Equal(t, "comp1", occ.CompID)
	assert.Equal(t, "m1", occ.MatchID)
}

func TestRunningMatchOnCourt_BracketMatch(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	bracket := &Bracket{
		Rounds: [][]BracketMatch{
			{{ID: "bm1", Status: MatchStatusRunning, Court: "B"}},
		},
	}
	require.NoError(t, store.SaveBracket("comp1", bracket))

	occ, err := store.RunningMatchOnCourt("B", "")
	require.NoError(t, err)
	require.NotNil(t, occ)
	assert.Equal(t, "comp1", occ.CompID)
	assert.Equal(t, "bm1", occ.MatchID)
}

func TestRunningMatchOnCourt_EmptyCourtIsNeverBusy(t *testing.T) {
	dir, err := os.MkdirTemp("", "court-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	require.NoError(t, store.SaveCompetition(&Competition{ID: "comp1"}))
	require.NoError(t, store.SavePoolMatches("comp1", []MatchResult{
		{ID: "m1", Status: MatchStatusRunning, Court: ""},
	}))

	occ, err := store.RunningMatchOnCourt("", "")
	require.NoError(t, err)
	assert.Nil(t, occ, "empty court string should never be considered busy")
}
