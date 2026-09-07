package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
)

func TestPoolMatchesMissingSideIDsMessage(t *testing.T) {
	t.Run("empty when every match already carries its ids", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "a1", SideB: "Bob", SideBID: "b1",
				Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: "a1"},
		}
		assert.Empty(t, PoolMatchesMissingSideIDsMessage(matches))
	})

	t.Run("empty for no matches at all", func(t *testing.T) {
		assert.Empty(t, PoolMatchesMissingSideIDsMessage(nil))
		assert.Empty(t, PoolMatchesMissingSideIDsMessage([]state.MatchResult{}))
	})

	t.Run("a bye row (one side blank) is excluded, never counted", func(t *testing.T) {
		// A bye: SideB is empty (nobody to carry an id), and there is no
		// winner recorded either. Must not be counted just because SideBID
		// is also empty -- the SideB=="" guard excludes it.
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "a1", SideB: "", SideBID: ""},
		}
		assert.Empty(t, PoolMatchesMissingSideIDsMessage(matches), "a bye row must not be counted")
	})

	t.Run("a drawn (hikiwake) row with no winner is excluded, never counted", func(t *testing.T) {
		// Both sides carry ids; no Winner is recorded (a draw), so the
		// Winner!="" guard excludes the winner-id check even though
		// WinnerID is empty too.
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "a1", SideB: "Bob", SideBID: "b1",
				Status: state.MatchStatusCompleted, Decision: "hikiwake"},
		}
		assert.Empty(t, PoolMatchesMissingSideIDsMessage(matches), "a hikiwake row with no Winner must not be counted")
	})

	t.Run("counts one match missing SideAID", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "", SideB: "Bob", SideBID: "b1"},
		}
		msg := PoolMatchesMissingSideIDsMessage(matches)
		assert.Contains(t, msg, "1 match(es)")
		assert.Contains(t, msg, "not counted in standings")
		assert.Contains(t, msg, "re-enter the results once the sides have ids")
	})

	t.Run("counts one match missing SideBID", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "a1", SideB: "Bob", SideBID: ""},
		}
		assert.Contains(t, PoolMatchesMissingSideIDsMessage(matches), "1 match(es)")
	})

	t.Run("counts one match with a winner but no WinnerID", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "a1", SideB: "Bob", SideBID: "b1",
				Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: ""},
		}
		assert.Contains(t, PoolMatchesMissingSideIDsMessage(matches), "1 match(es)")
	})

	t.Run("one issue per match: a row missing BOTH a side id and the winner id counts once, not twice", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "", SideB: "Bob", SideBID: "b1",
				Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: ""},
		}
		assert.Contains(t, PoolMatchesMissingSideIDsMessage(matches), "1 match(es)",
			"a single row with multiple missing ids must still count as ONE affected match")
	})

	t.Run("counts every affected match across a mixed batch", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "a1", SideB: "Bob", SideBID: "b1",
				Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: "a1"}, // clean
			{ID: "Pool A-1", SideA: "Carol", SideAID: "", SideB: "Dave", SideBID: "d1"}, // missing SideAID
			{ID: "Pool A-2", SideA: "Eve", SideAID: "e1", SideB: "", SideBID: ""},       // bye, excluded
			{ID: "Pool A-3", SideA: "Fay", SideAID: "f1", SideB: "Gus", SideBID: "g1",
				Status: state.MatchStatusCompleted, Winner: "Fay", WinnerID: ""}, // missing WinnerID
		}
		assert.Contains(t, PoolMatchesMissingSideIDsMessage(matches), "2 match(es)")
	})
}
