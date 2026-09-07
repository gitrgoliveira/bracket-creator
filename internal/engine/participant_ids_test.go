package engine

import (
	"fmt"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
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

	t.Run("names one match missing SideAID as SideA vs SideB", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "", SideB: "Bob", SideBID: "b1"},
		}
		msg := PoolMatchesMissingSideIDsMessage(matches)
		assert.Contains(t, msg, "Alice vs Bob")
		assert.Contains(t, msg, "not counted in standings")
		assert.Contains(t, msg, "re-enter the results once the sides have ids")
		// At or under helper.MaxNamedRows, the count is not restated (mirrors
		// the two helper notices' own shape): the "N match(es), including"
		// prefix only appears once the affected count exceeds what is named.
		assert.NotContains(t, msg, "match(es)")
	})

	t.Run("names one match missing SideBID", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "a1", SideB: "Bob", SideBID: ""},
		}
		assert.Contains(t, PoolMatchesMissingSideIDsMessage(matches), "Alice vs Bob")
	})

	t.Run("names one match with a winner but no WinnerID", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "a1", SideB: "Bob", SideBID: "b1",
				Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: ""},
		}
		assert.Contains(t, PoolMatchesMissingSideIDsMessage(matches), "Alice vs Bob")
	})

	t.Run("one issue per match: a row missing BOTH a side id and the winner id names it once, not twice", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "", SideB: "Bob", SideBID: "b1",
				Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: ""},
		}
		msg := PoolMatchesMissingSideIDsMessage(matches)
		assert.Equal(t, 1, strings.Count(msg, "Alice vs Bob"),
			"a single row with multiple missing ids must still be named ONCE, not once per missing id")
	})

	t.Run("names every affected match across a mixed batch, at or under the naming cap", func(t *testing.T) {
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "a1", SideB: "Bob", SideBID: "b1",
				Status: state.MatchStatusCompleted, Winner: "Alice", WinnerID: "a1"}, // clean
			{ID: "Pool A-1", SideA: "Carol", SideAID: "", SideB: "Dave", SideBID: "d1"}, // missing SideAID
			{ID: "Pool A-2", SideA: "Eve", SideAID: "e1", SideB: "", SideBID: ""},       // bye, excluded
			{ID: "Pool A-3", SideA: "Fay", SideAID: "f1", SideB: "Gus", SideBID: "g1",
				Status: state.MatchStatusCompleted, Winner: "Fay", WinnerID: ""}, // missing WinnerID
		}
		msg := PoolMatchesMissingSideIDsMessage(matches)
		assert.Contains(t, msg, "Carol vs Dave")
		assert.Contains(t, msg, "Fay vs Gus")
	})

	t.Run("more than MaxNamedRows affected matches: named rows plus a total count", func(t *testing.T) {
		var matches []state.MatchResult
		for i := 0; i < helper.MaxNamedRows+2; i++ {
			matches = append(matches, state.MatchResult{
				ID: fmt.Sprintf("Pool A-%d", i), SideA: fmt.Sprintf("Side%dA", i), SideAID: "",
				SideB: fmt.Sprintf("Side%dB", i), SideBID: fmt.Sprintf("b%d", i),
			})
		}
		msg := PoolMatchesMissingSideIDsMessage(matches)
		assert.Contains(t, msg, fmt.Sprintf("%d match(es), including", helper.MaxNamedRows+2),
			"once the affected count exceeds the naming cap, the total is restated with the notice's own noun")
		// Only the first MaxNamedRows are individually named.
		assert.Contains(t, msg, "Side0A vs Side0B")
		assert.NotContains(t, msg, fmt.Sprintf("Side%dA vs Side%dB", helper.MaxNamedRows, helper.MaxNamedRows),
			"a row past the naming cap is not individually named")
	})

	t.Run("one blank side names only the side that is present, never a dangling vs", func(t *testing.T) {
		// A bye-shaped row: SideB is legitimately empty, but SideA itself
		// has no id, so the row is still affected.
		matches := []state.MatchResult{
			{ID: "Pool A-0", SideA: "Alice", SideAID: "", SideB: ""},
		}
		msg := PoolMatchesMissingSideIDsMessage(matches)
		assert.Contains(t, msg, "Alice")
		assert.NotContains(t, msg, "Alice vs ", "must not print a dangling \"vs\" when SideB is blank")
		assert.NotContains(t, msg, " vs ")
	})

	t.Run("both sides blank falls back to the match id", func(t *testing.T) {
		// Only reachable via a hand-edited pool-matches.csv: a Winner name
		// recorded with no WinnerID and no side names at all.
		matches := []state.MatchResult{
			{ID: "Pool A-7", SideA: "", SideB: "", Winner: "Ghost"},
		}
		msg := PoolMatchesMissingSideIDsMessage(matches)
		assert.Contains(t, msg, "Pool A-7")
		assert.NotContains(t, msg, " vs ", "must not print a bare \" vs \" when neither side is named")
	})
}
