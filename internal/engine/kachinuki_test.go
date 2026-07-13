package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupKachinukiComp builds an engine + store and saves a kachinuki
// competition with an empty pool-matches file, the setup every kachinuki
// advancement/export test shares. opts mutate the competition before it is
// saved (e.g. teamSize, Naginata, Format).
func setupKachinukiComp(t *testing.T, id string, teamSize int, opts ...func(*state.Competition)) (*Engine, *state.Store, *state.Competition) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	comp := &state.Competition{
		ID:            id,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      teamSize,
	}
	for _, o := range opts {
		o(comp)
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches(id, []state.MatchResult{}))
	return eng, store, comp
}

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
// engine returns BothExhausted=true (MatchEnded=false) so the caller
// can decide by phase: a pool encounter is finalized as a draw, a
// bracket encounter stays running until the operator adds a daihyosen.
// GAP 2b.
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

	assert.True(t, res.BothExhausted, "simultaneous exhaustion must set BothExhausted")
	assert.False(t, res.MatchEnded, "MatchEnded must remain false; caller decides by phase")
	assert.Equal(t, "", res.WinningSide, "no winner when both sides exhaust simultaneously")
	assert.Nil(t, res.Next, "no next bout")
}

// TestAdvanceKachinuki_SimultaneousExhaustionNoOp verifies that when both
// teams run out of players simultaneously after a hikiwake, the pure
// AdvanceKachinuki function returns BothExhausted=true with MatchEnded=false
// and Next=nil. The caller (MaybeAdvanceKachinuki) decides the outcome by
// phase: pool/league finalizes as a draw, bracket stays running for daihyosen.
// GAP 2b.
func TestAdvanceKachinuki_SimultaneousExhaustionNoOp(t *testing.T) {
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

	assert.True(t, res.BothExhausted, "simultaneous exhaustion must set BothExhausted=true")
	assert.False(t, res.MatchEnded, "MatchEnded must be false; caller decides by phase")
	assert.Equal(t, "", res.WinningSide, "no default winner when both sides exhaust simultaneously")
	assert.Nil(t, res.Next, "no next bout scheduled")
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
	compID := "advance-not-found"
	eng, _, _ := setupKachinukiComp(t, compID, 5)

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
// MaybeAdvanceKachinuki returns (false, nil), this covers the bracket lookup.
func TestMaybeAdvanceKachinuki_BracketPath(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-bracket"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}))

	// Create a bracket with a single match, no SubResults on BracketMatch.
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

// TestMaybeAdvanceKachinuki_BronzeFinalize verifies the Naginata 3rd-place
// (bronze) match — a sibling of bracket.Rounds, not an element of it — is
// found and finalized by the kachinuki advancement path, at parity with a
// Rounds bracket match.
func TestMaybeAdvanceKachinuki_BronzeFinalize(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-bronze"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Naginata:      true,
	}))

	// Bronze match with a single bout: R-Senpo beats W-Senpo. The winner stays
	// and SideB's queue is exhausted → AdvanceKachinuki ends the match (same
	// shape as TestMaybeAdvanceKachinuki_AppendsBout).
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "RedTeam", SideB: "WhiteTeam", Winner: "RedTeam", Status: state.MatchStatusCompleted}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:    "m-bronze",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-Senpo", SideB: "W-Senpo", Winner: "R-Senpo", Decision: "fought"},
			},
		},
	}))
	// No pool matches so findTeamMatch falls through to the bracket search.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "m-bronze")
	require.NoError(t, err)
	assert.True(t, changed, "bronze kachinuki match must be found and finalized")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	assert.Equal(t, state.MatchStatusCompleted, bracket.ThirdPlaceMatch.Status, "bronze finalized")
	assert.Equal(t, "RedTeam", bracket.ThirdPlaceMatch.Winner, "bronze winner mirrored from kachinuki outcome")
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

// A2 lineup integration tests -----------------------------------------------

// TestMaybeAdvanceKachinuki_RosterFromLineup verifies that when a round-scoped
// lineup is saved for both teams, kachinukiRemainingRoster uses it to build
// the full remaining roster rather than the bout-log-only fallback. This means
// when SideA-position-1 beats SideB-position-1, SideB still has positions 2+
// queued, so the engine appends the next bout instead of ending the match.
func TestMaybeAdvanceKachinuki_RosterFromLineup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-roster-from-lineup"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))
	// Round-scoped lineups at round 0 for both teams.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	// Bout 1: R-1 beats W-1. With lineup: remainingA=[R-1], remainingB=[W-2,W-3].
	// Without lineup: remainingB=[] (bout-log-only) → wrongly ends match.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout must be appended (W-2 is in queue from lineup)")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 2, "bout 2 must be appended")
	assert.Equal(t, "R-1", matches[0].SubResults[1].SideA, "R-1 stays as winner")
	assert.Equal(t, "W-2", matches[0].SubResults[1].SideB, "W-2 is next from lineup")
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "match must not be completed yet")
}

// TestMaybeAdvanceKachinuki_RosterFromLineup_ParticipantIDKeyed reproduces
// the real UI flow: the lineup editor saves lineups keyed by the team
// PARTICIPANT ID (teamIdOf(t) resolves to player.id, a UUID assigned by
// the store), while bracket/pool match sides carry the team NAME. The
// engine must translate the side name to the matching participant ID
// when looking up lineups (match on id OR name), otherwise every real
// lookup misses and the roster silently degrades to the bout-log-only
// path (GAP 2 stays broken in production).
func TestMaybeAdvanceKachinuki_RosterFromLineup_ParticipantIDKeyed(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-lineup-pid-keyed"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))

	// Team participants exactly as the store creates them: UUID id, team
	// name in Name. (state.LoadParticipants only treats the first CSV
	// column as an ID when it parses as UUID v4.)
	ryuID := helper.NewUUID4()
	toraID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: ryuID, Name: "Ryu", Dojo: "DojoR"},
		{ID: toraID, Name: "Tora", Dojo: "DojoT"},
	}))

	// Lineups keyed by the participant ID, exactly as the UI saves them
	// (NOT by team name).
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: ryuID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: toraID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	// Match sides carry the team NAME. Bout 1: R-1 beats W-1. With the
	// lineup resolved: remainingB=[W-2, W-3], so bout 2 must be appended.
	// If the id-keyed lineup lookup misses, the bout-log-only fallback
	// sees remainingB=[] and wrongly ends the match.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "Ryu",
			SideB: "Tora",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout must be appended (W-2 queued in the id-keyed lineup)")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 2, "lineup must resolve via participant id: bout 2 appended")
	assert.Equal(t, "R-1", matches[0].SubResults[1].SideA, "R-1 stays as winner")
	assert.Equal(t, "W-2", matches[0].SubResults[1].SideB, "W-2 is next from the id-keyed lineup")
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "match must not be completed yet")
}

// TestKachinukiRemainingRoster_IDKeyBeatsNameKey pins that when a team has BOTH
// an id-keyed and a name-keyed lineup at the same round, the participant-id
// lineup (the UI's storage key) wins. This guards the teamKeys id-first
// ordering against FindBestLineupAny's deterministic slice-order tie-break: a
// name-first order would let a legacy name-keyed lineup override the current
// id-keyed one and select the wrong roster.
func TestKachinukiRemainingRoster_IDKeyBeatsNameKey(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-idkey-wins", 3, func(c *state.Competition) { c.Format = state.CompFormatMixed })

	ryuID := helper.NewUUID4()
	toraID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(comp.ID, []domain.Player{
		{ID: ryuID, Name: "Ryu", Dojo: "DojoR"},
		{ID: toraID, Name: "Tora", Dojo: "DojoT"},
	}))

	// Authoritative id-keyed lineup for Ryu at round 0.
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: ryuID, Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	// Legacy name-keyed lineup for the SAME team + round, different roster.
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: "Ryu", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "X-1",
			domain.PositionNumbered(2): "X-2",
			domain.PositionNumbered(3): "X-3",
		},
	}, 3))

	parent := &state.MatchResult{ID: "P1-0", SideA: "Ryu", SideB: "Tora"}
	remainingA, _, ok := eng.kachinukiRemainingRoster(comp.ID, "P1-0", comp, parent, 0)
	require.True(t, ok, "lineup roster must resolve")
	assert.Equal(t, []string{"R-1", "R-2", "R-3"}, remainingA,
		"the participant-id-keyed lineup must win the same-round tie over the legacy name-keyed one")
}

// TestMaybeAdvanceKachinuki_CompletedMatchNoOp: a match that is already
// completed must never be advanced again. Corrections re-submit the
// bout log of a finished match; without this guard the engine would
// append a phantom next bout onto the completed result (the roster
// still shows W-2/W-3 remaining here, so advancement WOULD fire).
func TestMaybeAdvanceKachinuki_CompletedMatchNoOp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-completed-noop"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:       "P1-0",
			SideA:    "RedTeam",
			SideB:    "WhiteTeam",
			Status:   state.MatchStatusCompleted,
			Winner:   "RedTeam",
			Decision: "kachinuki-exhaustion",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.False(t, changed, "a completed match must never be advanced")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Len(t, matches[0].SubResults, 1, "no bout may be appended onto a completed match")
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status)
	assert.Equal(t, "RedTeam", matches[0].Winner)
}

// TestMaybeAdvanceKachinuki_FullExhaustion5v5 verifies that a full 5-person
// lineup is used to correctly detect exhaustion after the last player of one
// side is defeated.
func TestMaybeAdvanceKachinuki_FullExhaustion5v5(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-5v5-exhaustion"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Format:        state.CompFormatMixed,
	}))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "R-S", domain.PosJiho: "R-J", domain.PosChuken: "R-C",
			domain.PosFukusho: "R-F", domain.PosTaisho: "R-T",
		},
	}, 5))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "W-S", domain.PosJiho: "W-J", domain.PosChuken: "W-C",
			domain.PosFukusho: "W-F", domain.PosTaisho: "W-T",
		},
	}, 5))

	// R-S has beaten W-S, W-J, W-C, W-F. Now beats W-T (last of WhiteTeam).
	// After this bout: remainingA=[R-S], remainingB=[] → MatchEnded WinningSide=A.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-S", SideB: "W-S", Winner: "R-S", Decision: "fought"},
				{Position: 2, SideA: "R-S", SideB: "W-J", Winner: "R-S", Decision: "fought"},
				{Position: 3, SideA: "R-S", SideB: "W-C", Winner: "R-S", Decision: "fought"},
				{Position: 4, SideA: "R-S", SideB: "W-F", Winner: "R-S", Decision: "fought"},
				{Position: 5, SideA: "R-S", SideB: "W-T", Winner: "R-S", Decision: "fought"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "all WhiteTeam players retired; match must end")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status)
	assert.Equal(t, "RedTeam", matches[0].Winner, "RedTeam wins by exhausting WhiteTeam")
}

// TestMaybeAdvanceKachinuki_NoLineupFallback verifies that when no lineup is
// saved the function falls back to the bout-log-only approach without error.
func TestMaybeAdvanceKachinuki_NoLineupFallback(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-no-lineup-fallback"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))
	// No lineups saved.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	// Without lineup: knownB={W-1}, retiredB={W-1}, remainingB=[] → MatchEnded.
	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "bout-log-only fallback: W-1 retired, SideB empty → match ended")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status)
}

// TestMaybeAdvanceKachinuki_MatchScopedLineup verifies that a match-scoped
// lineup (keyed by matchID) takes precedence over the round-scoped lineup for
// the same team.
func TestMaybeAdvanceKachinuki_MatchScopedLineup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-match-scoped-lineup"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))

	// Round-scoped lineup (generic): R-1, R-2, R-3.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	// Match-scoped lineup (specific for "P1-0"): R-A, R-B, R-C.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-A",
			domain.PositionNumbered(2): "R-B",
			domain.PositionNumbered(3): "R-C",
		},
	}, 3))
	// WhiteTeam round-scoped.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	// Bout 1: W-1 beats R-A (from match-scoped lineup).
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-A", SideB: "W-1", Winner: "W-1", Decision: "fought"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout must be appended using match-scoped lineup")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches[0].SubResults, 2)
	// R-A retired; next from match-scoped roster is R-B (NOT R-2 from round-scoped).
	assert.Equal(t, "R-B", matches[0].SubResults[1].SideA, "R-B from match-scoped lineup, not R-2 from round-scoped")
	assert.Equal(t, "W-1", matches[0].SubResults[1].SideB, "W-1 stays as winner")
}

// TestMaybeAdvanceKachinuki_LatestRoundLineupFallback verifies AMENDMENT 1:
// when multiple round-scoped lineups exist for a team, the engine picks the
// highest round <= currentRound. For pool matches (currentRound=0), a round-1
// lineup must be ignored in favour of round-0.
func TestMaybeAdvanceKachinuki_LatestRoundLineupFallback(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-latest-round-fallback"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))

	// Round 0 lineup for RedTeam (pool phase).
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-Pool-1",
			domain.PositionNumbered(2): "R-Pool-2",
			domain.PositionNumbered(3): "R-Pool-3",
		},
	}, 3))
	// Round 1 lineup for RedTeam (bracket phase, should NOT be used for pool match).
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 1,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-Bracket-1",
			domain.PositionNumbered(2): "R-Bracket-2",
			domain.PositionNumbered(3): "R-Bracket-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	// Bout 1: R-Pool-1 beats W-1. With AMENDMENT 1 fallback, round-0 lineup is
	// used (not round-1) for this pool match → remainingA=[R-Pool-1], remainingB=[W-2,W-3].
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-Pool-1", SideB: "W-1", Winner: "R-Pool-1", Decision: "fought"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout must be appended (W-2 is in pool-round-0 lineup)")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches[0].SubResults, 2, "bout 2 must be appended")
	// R-Pool-1 stays; next from round-0 lineup is W-2.
	assert.Equal(t, "R-Pool-1", matches[0].SubResults[1].SideA)
	assert.Equal(t, "W-2", matches[0].SubResults[1].SideB, "round-0 lineup used (not bracket round-1 with different names)")
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
		Format:        state.CompFormatMixed,
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
					// No Winner, no Decision, bout still in progress
				},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.False(t, changed, "incomplete bout (no outcome) must not advance the match")
}

// TestMaybeAdvanceKachinuki_HikiwakeBothExhausted verifies that when both
// sides of a POOL match are exhausted simultaneously after a hikiwake,
// MaybeAdvanceKachinuki finalizes the encounter as a draw (Status=Completed,
// Decision="hikiwake", Winner=""). Daihyosen is knockout-only; pool encounters
// are legitimately drawn. GAP 2b.
func TestMaybeAdvanceKachinuki_HikiwakeBothExhausted(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-hikiwake-exhausted"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Format:        state.CompFormatMixed,
	}))
	// Single hikiwake bout, both players are retired; remaining rosters empty.
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
	assert.True(t, changed, "pool simultaneous exhaustion must finalize the encounter as a draw")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "pool match must be completed as a draw")
	assert.Equal(t, state.DecisionDraw, matches[0].Decision, "decision must be hikiwake (draw)")
	assert.Empty(t, matches[0].Winner, "no winner on a drawn encounter")
}

// TestMaybeAdvanceKachinuki_SimultaneousExhaustionStaysRunning verifies that
// when both teams of a POOL match are exhausted simultaneously after a hikiwake,
// MaybeAdvanceKachinuki finalizes the pool encounter as a draw (changed=true,
// Status=Completed, Decision="hikiwake", Winner=""). The name is preserved for
// historical context; see also TestMaybeAdvanceKachinuki_BracketSimultaneousExhaustionStaysRunning
// for the bracket path that does leave the match running. GAP 2b.
func TestMaybeAdvanceKachinuki_SimultaneousExhaustionStaysRunning(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-simultaneous-exhaustion"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Format:        state.CompFormatMixed,
	}))
	// Single hikiwake bout, both players are the last on their side.
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
					Decision: state.DecisionDraw, // hikiwake; both retire
				},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "pool simultaneous exhaustion must finalize the encounter as a draw")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "pool draw must be completed")
	assert.Equal(t, state.DecisionDraw, matches[0].Decision, "decision must be hikiwake")
	assert.Empty(t, matches[0].Winner, "no winner on a drawn pool encounter")
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
		Format:        state.CompFormatMixed,
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

// A4 bracket bout-append and winner propagation tests -------------------------

// TestMaybeAdvanceKachinuki_BracketPropagatesWinner verifies that when a bracket
// kachinuki match ends, propagateBracketWinner is called so the winning team
// advances to the next round. GAP 4 (A4).
func TestMaybeAdvanceKachinuki_BracketPropagatesWinner(t *testing.T) {
	compID := "kachinuki-bracket-propagates-winner"
	eng, store, _ := setupKachinukiComp(t, compID, 5)

	// 2-round bracket: Round 0 = [SF1, SF2], Round 1 = [Final].
	// SF1: TeamA vs TeamB. Bout 1: A-Senpo beats B-Senpo.
	// Bout-log-only fallback: knownB={B-Senpo}, retiredB={B-Senpo} → remainingB=[].
	// AdvanceKachinuki → MatchEnded=true, WinningSide=A → Winner=TeamA.
	// propagateBracketWinner must feed TeamA into Final.SideA.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:    "SF1",
					SideA: "TeamA",
					SideB: "TeamB",
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "A-Senpo", Decision: "fought"},
					},
				},
				{
					ID:    "SF2",
					SideA: "TeamC",
					SideB: "TeamD",
				},
			},
			{
				{ID: "Final", SideA: "", SideB: ""},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "SF1")
	require.NoError(t, err)
	assert.True(t, changed, "SF1 should have been finalized")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status, "SF1 marked completed")
	assert.Equal(t, "TeamA", bracket.Rounds[0][0].Winner, "SF1 winner is TeamA")
	// propagateBracketWinner must have fed TeamA into the Final's SideA slot.
	assert.Equal(t, "TeamA", bracket.Rounds[1][0].SideA, "Final SideA must be populated from SF1 winner")
}

// TestMaybeAdvanceKachinuki_BracketAppendsBout verifies that when a bracket
// kachinuki match is still running (not exhausted), the next bout is appended
// to BracketMatch.SubResults. GAP 4 (A4).
func TestMaybeAdvanceKachinuki_BracketAppendsBout(t *testing.T) {
	compID := "kachinuki-bracket-appends-bout"
	eng, store, _ := setupKachinukiComp(t, compID, 5)

	// Single-round bracket (the final). After bout 2 both sides still have
	// players: remainingA=[A-Jiho], remainingB=[B-Senpo] → Next (bout 3 pairing).
	// Bout-log-only fallback:
	//   knownA={A-Senpo,A-Jiho}, retiredA={A-Senpo} → remainingA=[A-Jiho]
	//   knownB={B-Senpo,B-Chuken}, retiredB={B-Chuken} → remainingB=[B-Senpo]
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:    "B-Final",
					SideA: "TeamA",
					SideB: "TeamB",
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "B-Senpo", Decision: "fought"},
						{Position: 2, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "A-Jiho", Decision: "fought"},
					},
				},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "B-Final")
	require.NoError(t, err)
	assert.True(t, changed, "next bout must be appended to BracketMatch.SubResults")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, bracket.Rounds[0][0].SubResults, 3, "bout 3 must be appended to BracketMatch.SubResults")
	assert.Equal(t, "A-Jiho", bracket.Rounds[0][0].SubResults[2].SideA, "A-Jiho stays as SideA winner")
	assert.Equal(t, "B-Senpo", bracket.Rounds[0][0].SubResults[2].SideB, "B-Senpo is next from SideB")
	assert.Equal(t, 3, bracket.Rounds[0][0].SubResults[2].Position, "position must be 3")
}

// TestMaybeAdvanceKachinuki_BronzeAppendsBout verifies that the ThirdPlaceMatch
// (bronze) bout-append path mirrors the Rounds path: next bout is appended to
// ThirdPlaceMatch.SubResults when the match is still running. GAP 4 (A4).
func TestMaybeAdvanceKachinuki_BronzeAppendsBout(t *testing.T) {
	compID := "kachinuki-bronze-appends-bout"
	eng, store, _ := setupKachinukiComp(t, compID, 5, func(c *state.Competition) { c.Naginata = true })

	// Bronze match has 2 bouts; both sides still have remaining players after bout 2.
	// Same bout-log-only scenario as BracketAppendsBout above.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "SF1", SideA: "TeamA", SideB: "TeamB", Winner: "TeamA", Status: state.MatchStatusCompleted}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:    "m-bronze",
			SideA: "TeamA",
			SideB: "TeamB",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "B-Senpo", Decision: "fought"},
				{Position: 2, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "A-Jiho", Decision: "fought"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "m-bronze")
	require.NoError(t, err)
	assert.True(t, changed, "next bout must be appended to ThirdPlaceMatch.SubResults")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	require.Len(t, bracket.ThirdPlaceMatch.SubResults, 3, "bout 3 must be appended to ThirdPlaceMatch.SubResults")
	assert.Equal(t, "A-Jiho", bracket.ThirdPlaceMatch.SubResults[2].SideA, "A-Jiho stays as SideA winner")
	assert.Equal(t, "B-Senpo", bracket.ThirdPlaceMatch.SubResults[2].SideB, "B-Senpo is next from SideB")
	assert.Equal(t, 3, bracket.ThirdPlaceMatch.SubResults[2].Position, "position must be 3")
}

// TestFindTeamMatch_BronzeRoundIndex pins that the 3rd-place (bronze) match
// resolves to round index len(Rounds), not 0, so round-scoped lineup lookup
// prefers the bronze's own stage (matching the client's
// derivedBracket.rounds.length). A regular bracket match keeps its own rIdx.
func TestFindTeamMatch_BronzeRoundIndex(t *testing.T) {
	compID := "kachinuki-bronze-round-index"
	eng, store, _ := setupKachinukiComp(t, compID, 5, func(c *state.Competition) { c.Naginata = true })

	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "SF1", SideA: "TeamA", SideB: "TeamB"}, {ID: "SF2", SideA: "TeamC", SideB: "TeamD"}},
			{{ID: "F1", SideA: "TeamA", SideB: "TeamC"}},
		},
		ThirdPlaceMatch: &state.BracketMatch{ID: "m-bronze", SideA: "TeamB", SideB: "TeamD"},
	}))

	_, isBracket, roundIdx, err := eng.findTeamMatch(compID, "m-bronze")
	require.NoError(t, err)
	assert.True(t, isBracket, "bronze is a bracket match")
	assert.Equal(t, 2, roundIdx, "bronze round index is len(Rounds)=2, not 0")

	_, _, sfRound, err := eng.findTeamMatch(compID, "SF1")
	require.NoError(t, err)
	assert.Equal(t, 0, sfRound, "a first-round bracket match keeps rIdx 0")
}

// TestMergeKachinukiSubResults pins the by-position merge semantics the
// score-write entry points rely on (ACID: a partial client log must
// never destroy server-appended bouts).
func TestMergeKachinukiSubResults(t *testing.T) {
	t.Run("incoming overwrites same position, stored extras preserved", func(t *testing.T) {
		stored := []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", Decision: "hikiwake"},
			{Position: 2, SideA: "R-2", SideB: "W-2"},
		}
		incoming := []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
		}
		out := mergeKachinukiSubResults(stored, incoming)
		require.Len(t, out, 2)
		assert.Equal(t, "fought", out[0].Decision, "incoming bout 1 wins")
		assert.Equal(t, "R-1", out[0].Winner)
		assert.Equal(t, 2, out[1].Position, "stored placeholder preserved")
		assert.Equal(t, "R-2", out[1].SideA)
	})

	t.Run("empty incoming preserves the full stored log", func(t *testing.T) {
		stored := []state.SubMatchResult{
			{Position: 1, Winner: "R-1", Decision: "fought"},
			{Position: 2},
		}
		out := mergeKachinukiSubResults(stored, nil)
		require.Len(t, out, 2)
		assert.Equal(t, 1, out[0].Position)
		assert.Equal(t, 2, out[1].Position)
	})

	t.Run("daihyosen (-1) merges and sorts last", func(t *testing.T) {
		stored := []state.SubMatchResult{
			{Position: -1, SideA: "Ryu", SideB: "Tora", Decision: "daihyosen"},
			{Position: 1, Winner: "R-1", Decision: "fought"},
		}
		incoming := []state.SubMatchResult{
			{Position: 2, SideA: "R-1", SideB: "W-2"},
			{Position: -1, SideA: "Ryu", SideB: "Tora", Winner: "Ryu", Decision: "daihyosen"},
		}
		out := mergeKachinukiSubResults(stored, incoming)
		require.Len(t, out, 3)
		assert.Equal(t, 1, out[0].Position)
		assert.Equal(t, 2, out[1].Position)
		assert.Equal(t, -1, out[2].Position, "daihyosen sorts last")
		assert.Equal(t, "Ryu", out[2].Winner, "incoming daihyosen wins")
	})

	t.Run("full log in the patch behaves like a plain replace (corrections)", func(t *testing.T) {
		stored := []state.SubMatchResult{
			{Position: 1, Winner: "R-1", Decision: "fought"},
			{Position: 2, Winner: "W-2", Decision: "fought"},
		}
		incoming := []state.SubMatchResult{
			{Position: 1, Winner: "W-1", Decision: "fought"},
			{Position: 2, Winner: "W-2", Decision: "fought"},
		}
		out := mergeKachinukiSubResults(stored, incoming)
		require.Len(t, out, 2)
		assert.Equal(t, "W-1", out[0].Winner)
		assert.Equal(t, "W-2", out[1].Winner)
	})
}

// TestRecordMatchResultWithIneligibility_KachinukiMerge covers the
// NON-TX twin's merge block: a partial kachinuki patch must preserve the
// stored appended placeholder (same contract the tx twin enforces for
// the live /score path).
func TestRecordMatchResultWithIneligibility_KachinukiMerge(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-nontx-merge"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Decision: "hikiwake"},
				{Position: 2, SideA: "R-2", SideB: "W-2"},
			},
		},
	}))

	result := &state.MatchResult{
		SideA:  "RedTeam",
		SideB:  "WhiteTeam",
		Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
		},
	}
	_, err := eng.RecordMatchResultWithIneligibility(compID, "P1-0", result)
	require.NoError(t, err)

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 2, "stored placeholder must survive the partial write")
	assert.Equal(t, "fought", matches[0].SubResults[0].Decision)
	assert.Equal(t, "R-2", matches[0].SubResults[1].SideA)
}

// TestCheckKachinukiPrematureCompletion pins every bypass branch of the
// completed-write safety net.
func TestCheckKachinukiPrematureCompletion(t *testing.T) {
	setup := func(t *testing.T, compID string, matchStatus state.MatchStatus) (*Engine, *state.Store) {
		t.Helper()
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:            compID,
			TeamMatchType: state.TeamMatchTypeKachinuki,
			TeamSize:      3,
			Format:        state.CompFormatMixed,
		}))
		require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
			TeamID: "RedTeam", Round: 0,
			Positions: map[domain.Position]string{
				domain.PositionNumbered(1): "R-1",
				domain.PositionNumbered(2): "R-2",
				domain.PositionNumbered(3): "R-3",
			},
		}, 3))
		require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
			TeamID: "WhiteTeam", Round: 0,
			Positions: map[domain.Position]string{
				domain.PositionNumbered(1): "W-1",
				domain.PositionNumbered(2): "W-2",
				domain.PositionNumbered(3): "W-3",
			},
		}, 3))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: matchStatus,
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
				},
			},
		}))
		return eng, store
	}

	t.Run("both rosters remaining rejects", func(t *testing.T) {
		eng, _ := setup(t, "premature-both-remaining", state.MatchStatusRunning)
		err := eng.CheckKachinukiPrematureCompletion("premature-both-remaining", "P1-0", &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "RedTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		})
		assert.ErrorIs(t, err, ErrKachinukiPrematureCompletion)
	})

	t.Run("non-completed write passes", func(t *testing.T) {
		eng, _ := setup(t, "premature-running-ok", state.MatchStatusRunning)
		assert.NoError(t, eng.CheckKachinukiPrematureCompletion("premature-running-ok", "P1-0", &state.MatchResult{
			Status: state.MatchStatusRunning,
		}))
	})

	t.Run("withdrawal decision passes", func(t *testing.T) {
		eng, _ := setup(t, "premature-kiken-ok", state.MatchStatusRunning)
		assert.NoError(t, eng.CheckKachinukiPrematureCompletion("premature-kiken-ok", "P1-0", &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "RedTeam", Decision: "kiken-voluntary",
		}))
	})

	t.Run("daihyosen sub-result passes", func(t *testing.T) {
		eng, _ := setup(t, "premature-daihyosen-ok", state.MatchStatusRunning)
		assert.NoError(t, eng.CheckKachinukiPrematureCompletion("premature-daihyosen-ok", "P1-0", &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "RedTeam",
			SubResults: []state.SubMatchResult{
				{Position: -1, SideA: "RedTeam", SideB: "WhiteTeam", Winner: "RedTeam", Decision: "daihyosen"},
			},
		}))
	})

	t.Run("correction of a completed match passes", func(t *testing.T) {
		eng, _ := setup(t, "premature-correction-ok", state.MatchStatusCompleted)
		assert.NoError(t, eng.CheckKachinukiPrematureCompletion("premature-correction-ok", "P1-0", &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "RedTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		}))
	})

	t.Run("exhausted side passes", func(t *testing.T) {
		eng, _ := setup(t, "premature-exhausted-ok", state.MatchStatusRunning)
		// Incoming log retires the whole WhiteTeam roster: W-1..W-3 all lost.
		assert.NoError(t, eng.CheckKachinukiPrematureCompletion("premature-exhausted-ok", "P1-0", &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "RedTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
				{Position: 2, SideA: "R-1", SideB: "W-2", Winner: "R-1", Decision: "fought"},
				{Position: 3, SideA: "R-1", SideB: "W-3", Winner: "R-1", Decision: "fought"},
			},
		}))
	})

	t.Run("unknown match passes through to the 404 path", func(t *testing.T) {
		eng, _ := setup(t, "premature-unknown-match", state.MatchStatusRunning)
		assert.NoError(t, eng.CheckKachinukiPrematureCompletion("premature-unknown-match", "no-such-match", &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "RedTeam",
		}))
	})

	t.Run("non-kachinuki competition passes", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "premature-fixed-comp", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed,
		}))
		assert.NoError(t, eng.CheckKachinukiPrematureCompletion("premature-fixed-comp", "P1-0", &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "RedTeam",
		}))
	})
}

// TestCheckKachinukiPrematureCompletion_MergesPartialIncoming guards the
// pre-check against a partial or stale client log on a completed write. The
// write path merges sub-results by position, so exhaustion must be judged on
// the MERGED log, not the incoming log alone: a client that omits earlier
// server-appended bouts must not trip a false 409 when the committed (merged)
// log would show exhaustion.
func TestCheckKachinukiPrematureCompletion_MergesPartialIncoming(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "premature-merge", 3, func(c *state.Competition) { c.Format = state.CompFormatMixed })
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1", domain.PositionNumbered(2): "R-2", domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(comp.ID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1", domain.PositionNumbered(2): "W-2", domain.PositionNumbered(3): "W-3",
		},
	}, 3))
	// Server already holds the FULL exhausting log: R-1 beat W-1..W-3, so
	// WhiteTeam is fully retired and completion is legitimate.
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
				{Position: 2, SideA: "R-1", SideB: "W-2", Winner: "R-1", Decision: "fought"},
				{Position: 3, SideA: "R-1", SideB: "W-3", Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	// The client submits a PARTIAL completed write carrying only the last
	// bout (a stale/short log). Judged alone this looks like W-1/W-2 remain
	// (a false 409); merged with the stored log WhiteTeam is exhausted.
	err := eng.CheckKachinukiPrematureCompletion(comp.ID, "P1-0", &state.MatchResult{
		Status: state.MatchStatusCompleted, Winner: "RedTeam",
		SubResults: []state.SubMatchResult{
			{Position: 3, SideA: "R-1", SideB: "W-3", Winner: "R-1", Decision: "fought"},
		},
	})
	assert.NoError(t, err, "merged log shows WhiteTeam exhausted; completion must be allowed, not falsely rejected")
}

// TestMaybeAdvanceKachinuki_NamelessBoutNoOp: identity is required for
// retirement math. A bout that carries an outcome but EMPTY side names
// (UAT: the final's bootstrapped bout 1 was submitted as a nameless
// hikiwake because the round-1 lineup GET 404ed) must NOT advance:
// pre-fix the engine retired nobody and appended senpo-vs-senpo as
// bout 2, shifting the whole sequence by one.
func TestMaybeAdvanceKachinuki_NamelessBoutNoOp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-nameless-bout"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "", SideB: "", Decision: "hikiwake"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.False(t, changed, "a nameless bout must not advance the sequence")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Len(t, matches[0].SubResults, 1, "no bout may be appended off a nameless outcome")
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status)
	assert.Empty(t, matches[0].Winner)
}

// TestMaybeAdvanceKachinuki_FallbackRosterFirstAppearanceOrder verifies that
// the bout-log fallback returns the remaining roster in first-appearance order
// rather than nondeterministic map-iteration order (Fix 2). The last bout is
// a hikiwake, so advanceAfterHikiwake picks the HEAD of each remaining queue:
// the SideA head reveals which player was ordered first.
//
// Setup: WHITE has a saved lineup (deterministic queue). RED has NO lineup, so
// it falls back to the bout log. Bouts 1 and 2 each have a different RED
// player as SideA winning (contrived, to make both non-retired and produce a
// 2-element fallback queue). Bout 3 is a hikiwake that retires R-X. After the
// hikiwake, RED fallback remaining must be [R-B, R-A] (R-B appeared first in
// bout 1). Running 20 iterations guards against accidentally passing due to
// lucky Go map-iteration order.
func TestMaybeAdvanceKachinuki_FallbackRosterFirstAppearanceOrder(t *testing.T) {
	for i := range 20 {
		eng, store, _ := setupTestEngine(t)
		const compID = "kachinuki-fallback-order"

		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:            compID,
			TeamMatchType: state.TeamMatchTypeKachinuki,
			TeamSize:      3,
			Format:        state.CompFormatMixed,
		}))
		// Lineup saved only for WHITE; RED falls back to the bout log.
		require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
			TeamID: "WhiteTeam", Round: 0,
			Positions: map[domain.Position]string{
				domain.PositionNumbered(1): "W-1",
				domain.PositionNumbered(2): "W-2",
				domain.PositionNumbered(3): "W-3",
			},
		}, 3))
		// SubResults: bouts 1 and 2 are contrived (two different RED players win
		// against stub WHITE names not in the saved lineup) so R-B and R-A both
		// appear non-retired, with R-B first in bout-log order. Bout 3 is a
		// hikiwake that retires R-X (third RED player). The retirement map for RED
		// after RetiredPlayersFromBoutLog is {R-X}, leaving [R-B, R-A] as the
		// first-appearance-ordered remaining fallback queue.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam",
				Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "R-B", SideB: "W-stub1", Winner: "R-B", Decision: "fought"},
					{Position: 2, SideA: "R-A", SideB: "W-stub2", Winner: "R-A", Decision: "fought"},
					{Position: 3, SideA: "R-X", SideB: "W-stub3", Decision: string(domain.DecisionHikiwake)},
				},
			},
		}))

		changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
		require.NoError(t, err, "iteration %d", i)
		require.True(t, changed, "iteration %d: hikiwake must advance the sequence", i)

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, matches[0].SubResults, 4, "iteration %d: next bout must be appended", i)
		next := matches[0].SubResults[3]
		assert.Equal(t, "R-B", next.SideA,
			"iteration %d: R-B (first appearance in bout log) must be chosen as the next RED fighter; got %q", i, next.SideA)
		assert.Equal(t, "W-1", next.SideB,
			"iteration %d: W-1 must be chosen as the next WHITE fighter (head of saved lineup)", i)
	}
}

// TestCheckKachinukiPrematureCompletion_EmptyDaihyosenRejected is the
// regression test for Fix 4: a completed kachinuki write whose only
// daihyosen sub-result carries NO winner (an unscored placeholder) must
// still be rejected when both teams still have players remaining. The old
// code bypassed the guard on ANY Position=-1 sub-result regardless of
// whether a winner had been recorded.
func TestCheckKachinukiPrematureCompletion_EmptyDaihyosenRejected(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "premature-daihyosen-no-winner"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
			},
		},
	}))

	t.Run("unscored daihyosen placeholder is rejected", func(t *testing.T) {
		// Position=-1 sub-result but Winner="" means the daihyosen has not yet
		// been played. Both teams still have R-2/R-3 and W-2/W-3 remaining, so
		// this must be rejected as a premature completion.
		err := eng.CheckKachinukiPrematureCompletion(compID, "P1-0", &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "RedTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
				{Position: -1, SideA: "RedTeam", SideB: "WhiteTeam", Winner: "", Decision: "daihyosen"},
			},
		})
		assert.ErrorIs(t, err, ErrKachinukiPrematureCompletion,
			"an unscored daihyosen placeholder must not bypass the premature-completion guard")
	})

	t.Run("winner-carrying daihyosen still passes", func(t *testing.T) {
		// Sanity-check: a Position=-1 sub-result WITH a winner is the legitimate
		// completion path and must still return nil.
		err := eng.CheckKachinukiPrematureCompletion(compID, "P1-0", &state.MatchResult{
			Status: state.MatchStatusCompleted, Winner: "RedTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"},
				{Position: -1, SideA: "RedTeam", SideB: "WhiteTeam", Winner: "RedTeam", Decision: "daihyosen"},
			},
		})
		assert.NoError(t, err,
			"a winner-carrying daihyosen sub-result must still allow the completion")
	})
}

// TestMaybeAdvanceKachinuki_PoolSimultaneousExhaustionDraw is the primary
// regression test for the pool/league draw finalization fix. A hikiwake that
// retires the last player on both sides in a POOL match must produce a
// completed, winner-less encounter (Status=Completed, Decision="hikiwake",
// Winner=""). Daihyosen is knockout-only; pool encounters are legitimately
// drawn. GAP 2b.
func TestMaybeAdvanceKachinuki_PoolSimultaneousExhaustionDraw(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-pool-draw-finalize"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
		Format:        state.CompFormatMixed,
	}))
	// Lineups: 3-person rosters. Bouts 1 and 2 produce hikiwake,
	// both sides retire their last player in bout 3 simultaneously.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))
	// Bout 3 is the last for both teams (R-3 vs W-3) and ends in hikiwake.
	// After bout 3: remainingA=[], remainingB=[] → BothExhausted → pool draw.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Decision: state.DecisionDraw},
				{Position: 2, SideA: "R-2", SideB: "W-2", Decision: state.DecisionDraw},
				{Position: 3, SideA: "R-3", SideB: "W-3", Decision: state.DecisionDraw},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "pool simultaneous exhaustion must finalize the encounter as a draw")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "pool match must be completed")
	assert.Equal(t, state.DecisionDraw, matches[0].Decision, "decision must be hikiwake (draw)")
	assert.Empty(t, matches[0].Winner, "no winner on a drawn pool encounter")
}

// TestMaybeAdvanceKachinuki_BracketSimultaneousExhaustionStaysRunning verifies
// that a BRACKET kachinuki match where a hikiwake retires both teams' last
// players simultaneously is left RUNNING (changed=false). Daihyosen is the
// operator-driven resolution path for bracket ties; the engine must not
// finalize the bracket match automatically. GAP 2b.
func TestMaybeAdvanceKachinuki_BracketSimultaneousExhaustionStaysRunning(t *testing.T) {
	compID := "kachinuki-bracket-simultaneous-exhaustion"
	eng, store, _ := setupKachinukiComp(t, compID, 3)

	// Single-round bracket final: bout 3 ends in hikiwake exhausting both sides.
	// remainingA=[], remainingB=[] → BothExhausted → bracket stays running.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:    "B-Final",
					SideA: "RedTeam",
					SideB: "WhiteTeam",
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "R-1", SideB: "W-1", Decision: state.DecisionDraw},
						{Position: 2, SideA: "R-2", SideB: "W-2", Decision: state.DecisionDraw},
						{Position: 3, SideA: "R-3", SideB: "W-3", Decision: state.DecisionDraw},
					},
				},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "B-Final")
	require.NoError(t, err)
	assert.False(t, changed, "bracket simultaneous exhaustion must leave the match running; operator resolves via daihyosen")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, bracket.Rounds[0], 1)
	assert.NotEqual(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status, "bracket match must not be completed")
	assert.Empty(t, bracket.Rounds[0][0].Winner, "no winner assigned by the engine for bracket simultaneous exhaustion")
}
