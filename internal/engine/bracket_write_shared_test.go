package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These two pin invariants of applyBracketMatchResult that no test covered:
// both survived a mutation run against the whole suite. They were equally
// unpinned when the write existed as three hand-copied bodies; one shared
// function is what makes a single test protect the round path, the Tx path and
// the bronze fallback at once.

// A score must not rewrite the seeded pairing. The pool path has pinned this
// since scoring_test.go's TestRecordMatchResult_SideMismatch; the bracket path
// had not, so deleting the rejection left the suite green while a payload
// naming the wrong competitor overwrote the seeded match.
func TestBracketWrite_SideMismatchRejected(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	compID := "bm1"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Status = state.CompStatusPools
	})
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
		}},
	}))

	err := eng.RecordMatchResult(compID, "m-r1-0", &state.MatchResult{
		SideA: "Alice", SideB: "Mallory", // stored says Bob
		Winner: "Mallory", Status: state.MatchStatusCompleted,
	})
	assert.ErrorIs(t, err, ErrMatchSideMismatch)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", b.Rounds[0][0].SideB, "the seeded pairing must be untouched")
	assert.Empty(t, b.Rounds[0][0].Winner)
}

// A "running" write is for live-status display only: the next round must not be
// filled in until the match has a final result, or an operator who taps Start
// (which sends {status: "running"}) would seed the following round from a match
// still being fought.
func TestBracketWrite_RunningDoesNotPropagate(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	compID := "bm2"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Status = state.CompStatusPools
	})
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
				{ID: "m-r1-1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled},
			},
			{{ID: "m-r2-0", Status: state.MatchStatusScheduled}},
		},
	}))

	// Running, with a winner already showing: must NOT reach the next round.
	require.NoError(t, eng.RecordMatchResult(compID, "m-r1-0", &state.MatchResult{
		SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusRunning,
	}))
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Empty(t, b.Rounds[1][0].SideA, "a running match must not seed the next round")

	// Completing the same match propagates.
	require.NoError(t, eng.RecordMatchResult(compID, "m-r1-0", &state.MatchResult{
		SideA: "Alice", SideB: "Bob", Winner: "Alice", Status: state.MatchStatusCompleted,
	}))
	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Alice", b.Rounds[1][0].SideA)
}
