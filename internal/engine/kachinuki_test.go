package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestKachinukiWinnerAdvances covers the FR-044 happy path: when one
// side's player wins a bout, that player stays on and faces the head
// of the opposing side's remaining queue.
//
// T123.
func TestKachinukiWinnerAdvances(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 1,
		SideA:    "A-Senpo",
		SideB:    "B-Senpo",
		Winner:   "A-Senpo",
		Decision: "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		// SideA still has 4 more; SideB lost their Senpo so the next
		// un-retired opponent is Jiho.
		SideA: []string{"A-Jiho", "A-Chuken", "A-Fukusho", "A-Taisho"},
		SideB: []string{"B-Jiho", "B-Chuken", "B-Fukusho", "B-Taisho"},
	})

	require.NotNil(t, res.Next, "expected a follow-up bout, got match-ended")
	assert.False(t, res.MatchEnded)
	assert.Equal(t, 2, res.Next.Position, "bout position should increment by one")
	// SideA player stays, so the new bout still has SideA = A-Senpo.
	assert.Equal(t, "A-Senpo", res.Next.SideA)
	assert.Equal(t, "B-Jiho", res.Next.SideB)
}

// TestKachinukiSideBWinnerSwapsRole verifies the symmetric path: when
// the SideB player wins, they remain SideB on the next bout (the
// stayer's side role is preserved so admins reading subResults can
// still tell which team a name belongs to).
func TestKachinukiSideBWinnerSwapsRole(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 1,
		SideA:    "A-Senpo",
		SideB:    "B-Senpo",
		Winner:   "B-Senpo",
		Decision: "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    []string{"A-Jiho", "A-Chuken"},
		SideB:    []string{"B-Jiho", "B-Chuken"},
	})

	require.NotNil(t, res.Next)
	assert.Equal(t, 2, res.Next.Position)
	assert.Equal(t, "A-Jiho", res.Next.SideA)
	assert.Equal(t, "B-Senpo", res.Next.SideB)
}

// TestKachinukiHikiwakeRetiresBoth covers FR-044 hikiwake semantics:
// a draw retires BOTH players and the next pair from each remaining
// queue takes the court.
//
// T124.
func TestKachinukiHikiwakeRetiresBoth(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 1,
		SideA:    "A-Senpo",
		SideB:    "B-Senpo",
		Winner:   "",
		Decision: string(domain.DecisionHikiwake),
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		// Both Senpos retired; the caller already stripped them from
		// these remaining queues. Next bout should pair A-Jiho with
		// B-Jiho.
		SideA: []string{"A-Jiho", "A-Chuken"},
		SideB: []string{"B-Jiho", "B-Chuken"},
	})

	require.NotNil(t, res.Next, "expected next bout after hikiwake")
	assert.False(t, res.MatchEnded)
	assert.Equal(t, 2, res.Next.Position)
	assert.Equal(t, "A-Jiho", res.Next.SideA)
	assert.Equal(t, "B-Jiho", res.Next.SideB)
}

// TestKachinukiExhaustionEndsMatch covers FR-044 + T137: when one side
// has no remaining un-retired players, the other side wins by
// exhaustion (DecisionKachinukiExhaustion).
func TestKachinukiExhaustionEndsMatch(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 4,
		SideA:    "A-Fukusho",
		SideB:    "B-Taisho",
		Winner:   "A-Fukusho",
		Decision: "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		// SideA still has Taisho left; SideB is exhausted.
		SideA: []string{"A-Taisho"},
		SideB: []string{},
	})

	assert.True(t, res.MatchEnded, "side B exhausted should end the match")
	assert.Equal(t, "A", res.WinningSide)
	assert.Equal(t, string(domain.DecisionKachinukiExhaustion), res.Decision)
	assert.Nil(t, res.Next)
}

// TestKachinukiHikiwakeExhaustsLast covers the edge case where a
// hikiwake retires the last player on each side simultaneously. The
// engine ends the match and logs (default WinningSide=A is a
// reviewer-flag — admins are expected to override).
func TestKachinukiHikiwakeExhaustsLast(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 5,
		SideA:    "A-Taisho",
		SideB:    "B-Taisho",
		Decision: string(domain.DecisionHikiwake),
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    []string{},
		SideB:    []string{},
	})

	assert.True(t, res.MatchEnded)
	assert.Equal(t, string(domain.DecisionKachinukiExhaustion), res.Decision)
	assert.Equal(t, "A", res.WinningSide, "simultaneous hikiwake exhaustion defaults to A for admin review")
}

// TestRetiredPlayersFromBoutLog verifies the helper that callers use
// to derive remaining rosters from the persisted SubResults log.
func TestRetiredPlayersFromBoutLog(t *testing.T) {
	boutLog := []state.SubMatchResult{
		{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "A-Senpo", Decision: "fought"},
		{Position: 2, SideA: "A-Senpo", SideB: "B-Jiho", Decision: string(domain.DecisionHikiwake)},
		{Position: 3, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "B-Chuken", Decision: "fought"},
	}
	retiredA, retiredB := RetiredPlayersFromBoutLog(boutLog, "Team A", "Team B")

	// A-Senpo retired on the hikiwake in bout 2; A-Jiho retired in bout 3.
	assert.Contains(t, retiredA, "A-Senpo")
	assert.Contains(t, retiredA, "A-Jiho")
	assert.NotContains(t, retiredA, "A-Chuken", "A-Chuken never played, not retired")
	// B-Senpo retired in bout 1; B-Jiho retired in bout 2 (hikiwake).
	assert.Contains(t, retiredB, "B-Senpo")
	assert.Contains(t, retiredB, "B-Jiho")
	assert.NotContains(t, retiredB, "B-Chuken", "B-Chuken won bout 3 and is still on the court")
}

// TestFilterRemaining smoke-tests the order-preserving filter.
func TestFilterRemaining(t *testing.T) {
	roster := []string{"A-Senpo", "A-Jiho", "A-Chuken", "A-Fukusho", "A-Taisho"}
	retired := map[string]struct{}{"A-Senpo": {}, "A-Chuken": {}}
	got := FilterRemaining(roster, retired)
	assert.Equal(t, []string{"A-Jiho", "A-Fukusho", "A-Taisho"}, got)
}

// TestAdvanceKachinukiUnrecognizedOutcome guards the defensive branch:
// a Winner that names neither side returns the zero result so callers
// fall back to manual scheduling instead of producing a wrong pair.
func TestAdvanceKachinukiUnrecognizedOutcome(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 1,
		SideA:    "A-Senpo",
		SideB:    "B-Senpo",
		Winner:   "Someone Else",
		Decision: "fought",
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    []string{"A-Jiho"},
		SideB:    []string{"B-Jiho"},
	})
	assert.Nil(t, res.Next)
	assert.False(t, res.MatchEnded)
}

// TestDescribeKachinukiResult smoke-tests the stringer (used by handler logs).
func TestDescribeKachinukiResult(t *testing.T) {
	assert.Contains(t, describeKachinukiResult(AdvanceKachinukiResult{MatchEnded: true, WinningSide: "A", Decision: string(domain.DecisionKachinukiExhaustion)}), "MatchEnded")
	assert.Contains(t, describeKachinukiResult(AdvanceKachinukiResult{Next: &state.SubMatchResult{Position: 3, SideA: "x", SideB: "y"}}), "Next")
	assert.Equal(t, "no-op", describeKachinukiResult(AdvanceKachinukiResult{}))
}

// TestMaybeAdvanceKachinuki_NonKachinuki verifies the no-op fast-path:
// a competition whose TeamMatchType is not kachinuki must return
// (false, nil) immediately.
func TestMaybeAdvanceKachinuki_NonKachinuki(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-noop"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeFixed,
		TeamSize:      5,
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "any-match")
	assert.NoError(t, err)
	assert.False(t, changed)
}

// TestMaybeAdvanceKachinuki_NoSubResults verifies that a kachinuki
// match that has no sub-results yet returns (false, nil) without
// appending a bout or mutating the match.
func TestMaybeAdvanceKachinuki_NoSubResults(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-no-sub"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}))

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusScheduled},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	assert.NoError(t, err)
	assert.False(t, changed, "no sub-results means nothing to advance")
}

// TestMaybeAdvanceKachinuki_AppendsBout verifies the happy path: when
// the last bout has a winner the function appends the next bout.
func TestMaybeAdvanceKachinuki_AppendsBout(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-append"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}))

	// Bout 1: R-Senpo beats W-Senpo. Remaining: W-Jiho is still in,
	// so AdvanceKachinuki should produce a next bout for position 2.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-Senpo", SideB: "W-Senpo", Winner: "R-Senpo", Decision: "fought"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	// The remaining queues from the bout log are empty (only one bout, one
	// player each side, winner stays → SideB queue has no one left from
	// bout log). AdvanceKachinuki sees WinningSide=A and empty SideB queue
	// → MatchEnded. So changed should be true (ended state persisted).
	assert.True(t, changed)

	// Verify the match was updated.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status,
		"match should be completed when SideB queue is exhausted")
}

// TestMaybeAdvanceKachinuki_MatchNotFound verifies that requesting
// advancement for an unknown match ID returns (false, nil).
func TestMaybeAdvanceKachinuki_MatchNotFound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-not-found"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}))

	// Save empty pool — no matches.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "nonexistent")
	assert.NoError(t, err)
	assert.False(t, changed)
}

// TestAdvanceKachinuki_HikiwakeSideAExhausted covers the case where SideA
// runs out after a hikiwake but SideB still has players (WinningSide=B).
func TestAdvanceKachinuki_HikiwakeSideAExhausted(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 3,
		SideA:    "A-Chuken",
		SideB:    "B-Chuken",
		Decision: string(domain.DecisionHikiwake),
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    []string{},                        // SideA exhausted
		SideB:    []string{"B-Fukusho", "B-Taisho"}, // SideB still has players
	})
	assert.True(t, res.MatchEnded)
	assert.Equal(t, "B", res.WinningSide)
	assert.Equal(t, string(domain.DecisionKachinukiExhaustion), res.Decision)
}

// TestAdvanceKachinuki_HikiwakeSideBExhausted covers the case where SideB
// runs out after a hikiwake but SideA still has players (WinningSide=A).
func TestAdvanceKachinuki_HikiwakeSideBExhausted(t *testing.T) {
	bout := state.SubMatchResult{
		Position: 3,
		SideA:    "A-Chuken",
		SideB:    "B-Chuken",
		Decision: string(domain.DecisionHikiwake),
	}
	res := AdvanceKachinuki(AdvanceKachinukiInput{
		LastBout: bout,
		SideA:    []string{"A-Fukusho", "A-Taisho"}, // SideA still has players
		SideB:    []string{},                        // SideB exhausted
	})
	assert.True(t, res.MatchEnded)
	assert.Equal(t, "A", res.WinningSide)
	assert.Equal(t, string(domain.DecisionKachinukiExhaustion), res.Decision)
}

// TestMaybeAdvanceKachinuki_BracketPath verifies that findTeamMatch exercises
// the bracket search path. BracketMatch has no SubResults, so
// MaybeAdvanceKachinuki returns (false, nil) — this covers the bracket lookup.
func TestMaybeAdvanceKachinuki_BracketPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-bracket"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}))

	// Create a bracket with a single match — no SubResults on BracketMatch.
	bracketMatchID := "B1"
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: bracketMatchID, SideA: "RedTeam", SideB: "WhiteTeam"},
			},
		},
	}))
	// No pool matches so findTeamMatch falls through to the bracket search.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, bracketMatchID)
	require.NoError(t, err)
	// BracketMatch has no SubResults → early return false.
	assert.False(t, changed)
}

// TestMaybeAdvanceKachinuki_AppendsBoutNextRound verifies the case where
// the last bout has a winner AND both sides still have players, so the
// engine appends the next bout rather than ending the match.
func TestMaybeAdvanceKachinuki_AppendsBoutNextRound(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-next-bout"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}))

	// Bout 1: B-Senpo beats A-Senpo (A-Senpo retires, B-Senpo stays).
	// Bout 2: A-Jiho beats B-Chuken (B-Chuken retires, A-Jiho stays).
	// After bout 2: remainingA=[A-Jiho], remainingB=[B-Senpo] (won bout 1).
	// → AdvanceKachinuki returns out.Next (next bout, not match-ended).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "B-Senpo", Decision: "fought"},
				{Position: 2, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "A-Jiho", Decision: "fought"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout should have been appended")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Len(t, matches[0].SubResults, 3, "bout 3 should have been appended")
	assert.Equal(t, "A-Jiho", matches[0].SubResults[2].SideA, "A-Jiho stays as SideA winner")
	assert.Equal(t, "B-Senpo", matches[0].SubResults[2].SideB, "B-Senpo is next SideB")
}

// TestMaybeAdvanceKachinuki_NoOutcome verifies that a SubResult with no
// Winner and no Decision (bout still in progress) returns (false, nil)
// immediately without appending anything.
func TestMaybeAdvanceKachinuki_NoOutcome(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-no-outcome"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Format:        state.CompFormatPools,
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{
					Position: 1,
					SideA:    "A-Senpo",
					SideB:    "B-Senpo",
					// No Winner, no Decision — bout still in progress
				},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.False(t, changed, "incomplete bout (no outcome) must not advance the match")
}

// TestMaybeAdvanceKachinuki_HikiwakeBothExhausted verifies that when both
// sides are exhausted after a hikiwake (empty remaining rosters),
// MaybeAdvanceKachinuki returns (false, nil) — the "no-op" path when
// AdvanceKachinuki cannot determine a next action.
func TestMaybeAdvanceKachinuki_HikiwakeBothExhausted(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-hikiwake-exhausted"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Format:        state.CompFormatPools,
	}))
	// Single hikiwake bout — both players are retired; remaining rosters empty.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{
					Position: 1,
					SideA:    "A-Senpo",
					SideB:    "B-Senpo",
					Decision: state.DecisionDraw, // hikiwake
				},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "both exhausted → match ended (WinningSide=A default) → changed=true")
}

// TestMaybeAdvanceKachinuki_MatchEndedPoolUpdate verifies that when one side
// is exhausted (the winning side "has won"), MaybeAdvanceKachinuki marks the
// pool match as completed and returns (true, nil).
func TestMaybeAdvanceKachinuki_MatchEndedPoolUpdate(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-match-ended"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Format:        state.CompFormatPools,
	}))
	// After B-Senpo beats A-Senpo, A's remaining roster is empty → match ends.
	// With kachinukiRemainingRoster: knownA={A-Senpo}, retiredA={A-Senpo},
	// remainingA=[]. knownB={B-Senpo}, retiredB={}, remainingB=[B-Senpo].
	// AdvanceKachinuki sees SideA=[] → WinningSide="B" → MatchEnded=true.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{
					Position: 1,
					SideA:    "A-Senpo",
					SideB:    "B-Senpo",
					Winner:   "B-Senpo",
					Decision: "fought",
				},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "match should have been marked completed")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status)
	assert.Equal(t, "WhiteTeam", matches[0].Winner)
}
