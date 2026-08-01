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

// TestMaybeAdvanceKachinuki_ExhaustedSnapshotNoOp verifies operator-led
// completion (mp-gmcg): an apparently-exhausted roster snapshot is ADVISORY
// only. With no lineup saved, the bout-log-only roster after bout 1 shows
// SideB empty (W-Senpo retired, nobody else ever seen), so the pure core
// reports MatchEnded (WinningSide=A) — but team sizes are unregulated and
// the snapshot may be incomplete, so the engine must NOT finalize. The
// stored match stays completely untouched; the operator ends the encounter
// with an explicit completed score write through the normal scoring path.
func TestMaybeAdvanceKachinuki_ExhaustedSnapshotNoOp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-append"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}))

	// Bout 1: R-Senpo beats W-Senpo. Bout-log-only roster: remainingB=[]
	// → the pure core reports MatchEnded, which the caller ignores.
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
	assert.False(t, changed, "an exhausted snapshot is advisory; the engine must not mutate the match")

	// The stored match must be completely untouched.
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status,
		"the engine must never auto-complete a kachinuki match")
	assert.Empty(t, matches[0].Winner, "no winner may be assigned by the engine")
	assert.Empty(t, matches[0].Decision, "no decision may be written by the engine")
	assert.Len(t, matches[0].SubResults, 1, "no bout may be appended off an exhausted snapshot")
}

// TestMaybeAdvanceKachinuki_IgnoresTrailingDaihyosen verifies advancement is
// driven by the last NUMBERED bout, not by a trailing daihyosen placeholder.
// mergeKachinukiSubResults orders the daihyosen (Position -1) row last; keying
// off the final slice element would advance off the rep bout (which is not a
// kachinuki bout) instead of the real numbered result.
func TestMaybeAdvanceKachinuki_IgnoresTrailingDaihyosen(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-skip-daihyosen"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
	}))
	// Saved lineups give WhiteTeam a non-empty remaining queue after bout 1,
	// so advancing off the numbered bout APPENDS the next pairing. That
	// append is the proof the daihyosen row was skipped (operator-led
	// completion, mp-gmcg: appending is the only mutation the engine makes).
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-Senpo",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "WhiteTeam", Round: 0,
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-Senpo",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))

	// Bout 1 (numbered) has a decisive outcome that should drive advancement.
	// A trailing daihyosen placeholder (Position -1) is unscored: if selection
	// keyed off the final slice element it would read the outcome-less rep row
	// and bail (false), leaving the numbered result unprocessed.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-Senpo", SideB: "W-Senpo", Winner: "R-Senpo", Decision: "fought"},
				{Position: state.DaihyosenSubPosition, SideA: "RedTeam", SideB: "WhiteTeam"},
			},
		},
	}))

	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "advancement must run off the numbered bout, not the trailing daihyosen row")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Len(t, matches[0].SubResults, 3, "the next bout must be appended off the numbered result")
	appended := matches[0].SubResults[2]
	assert.NotEqual(t, state.DaihyosenSubPosition, appended.Position, "the appended entry is a numbered kachinuki bout")
	assert.Equal(t, "R-Senpo", appended.SideA, "R-Senpo stays on as the bout-1 winner")
	assert.Equal(t, "W-2", appended.SideB, "W-2 is next from the WhiteTeam lineup")
	// The daihyosen placeholder itself must survive untouched.
	daihyosen := matches[0].SubResults[1]
	require.Equal(t, state.DaihyosenSubPosition, daihyosen.Position)
	assert.Empty(t, daihyosen.Winner, "the unscored daihyosen row must not be mutated")
	assert.Empty(t, daihyosen.Decision, "the unscored daihyosen row must not be mutated")
	// And the engine never finalizes: the encounter keeps running (mp-gmcg).
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status)
	assert.Empty(t, matches[0].Winner)
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
// runs out after a hikiwake but SideB still has players. Spec 006
// decision 2: the tie left no decisive point, so the engine appends a
// ONE-SIDED WALKOVER SLOT for SideB's next fighter (the operator gives
// them the per-bout fusensho and Ends on that point, or fills the empty
// side and fights on). It must NOT flag MatchEnded: that is the win
// path's verdict, and here there is no decisive point to End on.
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
	assert.False(t, res.MatchEnded, "hikiwake leaves no decisive point; the walkover bout expresses the win")
	assert.False(t, res.BothExhausted)
	require.NotNil(t, res.Next, "expected the walkover slot")
	assert.Equal(t, 4, res.Next.Position)
	assert.Equal(t, "", res.Next.SideA, "exhausted side stays empty on the walkover slot")
	assert.Equal(t, "B-Fukusho", res.Next.SideB)
}

// TestAdvanceKachinuki_HikiwakeSideBExhausted mirrors the walkover-slot
// contract for SideB running out after a hikiwake.
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
	assert.False(t, res.MatchEnded, "hikiwake leaves no decisive point; the walkover bout expresses the win")
	assert.False(t, res.BothExhausted)
	require.NotNil(t, res.Next, "expected the walkover slot")
	assert.Equal(t, 4, res.Next.Position)
	assert.Equal(t, "A-Fukusho", res.Next.SideA)
	assert.Equal(t, "", res.Next.SideB, "exhausted side stays empty on the walkover slot")
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

// TestMaybeAdvanceKachinuki_BronzeExhaustedSnapshotNoOp verifies the Naginata
// 3rd-place (bronze) match — a sibling of bracket.Rounds, not an element of
// it — is found by the kachinuki advancement path but is NEVER auto-finalized
// (operator-led completion, mp-gmcg): the bout-log-only roster after bout 1
// shows SideB exhausted, an advisory verdict only, so the engine leaves the
// ThirdPlaceMatch completely untouched. The bronze APPEND branch is pinned
// separately by TestMaybeAdvanceKachinuki_BronzeAppendsBout.
func TestMaybeAdvanceKachinuki_BronzeExhaustedSnapshotNoOp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-bronze"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Naginata:      true,
	}))

	// Bronze match with a single bout: R-Senpo beats W-Senpo. The winner stays
	// and SideB's bout-log-only queue is exhausted → the pure core reports
	// MatchEnded, which the caller must treat as advisory (no finalize).
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
	assert.False(t, changed, "bronze exhausted snapshot is advisory; the engine must not mutate the match")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch)
	assert.NotEqual(t, state.MatchStatusCompleted, bracket.ThirdPlaceMatch.Status, "bronze must not be auto-completed")
	assert.Empty(t, bracket.ThirdPlaceMatch.Winner, "no winner may be assigned by the engine")
	assert.Empty(t, bracket.ThirdPlaceMatch.Decision, "no decision may be written by the engine")
	assert.Len(t, bracket.ThirdPlaceMatch.SubResults, 1, "no bout may be appended off an exhausted snapshot")
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

// TestMaybeAdvanceKachinuki_FullSequence5v5EndsWithOperator walks a full 5v5
// sequence bout by bout: R-S sweeps the WhiteTeam lineup, and after each
// scored bout the engine appends the next pairing from the saved lineups
// (pinning the append chain). After the FINAL bout (W-T, last of WhiteTeam,
// defeated) the roster snapshot is exhausted — but that verdict is advisory
// only (operator-led completion, mp-gmcg): the last advance is a no-op and
// the match stays running with the full bout log intact until the operator
// sends an explicit completed score write.
func TestMaybeAdvanceKachinuki_FullSequence5v5EndsWithOperator(t *testing.T) {
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

	// Bout 1 scored: R-S beats W-S. The walk below scores each appended
	// bout in turn (R-S keeps winning) and re-runs advancement.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:     "P1-0",
			SideA:  "RedTeam",
			SideB:  "WhiteTeam",
			Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-S", SideB: "W-S", Winner: "R-S", Decision: "fought"},
			},
		},
	}))

	whiteOrder := []string{"W-S", "W-J", "W-C", "W-F", "W-T"}
	for bout := 2; bout <= 5; bout++ {
		changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
		require.NoError(t, err, "bout %d", bout)
		require.True(t, changed, "bout %d must be appended from the lineups", bout)

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, matches, 1)
		require.Len(t, matches[0].SubResults, bout, "append chain must reach bout %d", bout)
		next := &matches[0].SubResults[bout-1]
		assert.Equal(t, "R-S", next.SideA, "bout %d: R-S stays on as the winner", bout)
		assert.Equal(t, whiteOrder[bout-1], next.SideB, "bout %d: next WhiteTeam fighter in lineup order", bout)
		assert.Empty(t, next.Winner, "bout %d is appended unscored", bout)
		assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "encounter keeps running mid-sequence")

		// Operator scores the appended bout: R-S wins again.
		next.Winner = "R-S"
		next.Decision = "fought"
		require.NoError(t, store.SavePoolMatches(compID, matches))
	}

	// W-T (last of WhiteTeam) is now defeated: the snapshot is exhausted,
	// but that is advisory only — the engine must NOT finalize (mp-gmcg).
	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.False(t, changed, "exhaustion is advisory; the operator ends the match explicitly")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Len(t, matches[0].SubResults, 5, "the full bout log must stay intact")
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "the engine must never auto-complete")
	assert.Empty(t, matches[0].Winner, "no winner may be assigned by the engine")
	assert.Empty(t, matches[0].Decision, "no decision may be written by the engine")
}

// TestMaybeAdvanceKachinuki_NoLineupFallback verifies that when no lineup is
// saved the function falls back to the bout-log-only roster without error.
// The fallback snapshot (knownB={W-1}, retiredB={W-1} → remainingB=[]) reads
// as MatchEnded, but that verdict is advisory only (operator-led completion,
// mp-gmcg): with team sizes unregulated the bout log may not have seen the
// whole roster, so the engine must leave the match untouched.
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

	// Without lineup: remainingB=[] → advisory MatchEnded → no-op.
	changed, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.False(t, changed, "bout-log-only exhaustion is advisory; the engine must not mutate the match")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "the engine must never auto-complete")
	assert.Empty(t, matches[0].Winner)
	assert.Empty(t, matches[0].Decision)
	assert.Len(t, matches[0].SubResults, 1, "no bout may be appended off the exhausted fallback snapshot")
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
// sides of a POOL match look simultaneously exhausted after a hikiwake, the
// BothExhausted verdict is ADVISORY only (operator-led completion, mp-gmcg):
// the engine no longer auto-finalizes the pool draw. The match is left
// completely untouched; the operator records the draw explicitly through the
// normal scoring path.
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
	assert.False(t, changed, "BothExhausted is advisory; the engine must not finalize the pool draw")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "the engine must never auto-complete")
	assert.Empty(t, matches[0].Decision, "no draw decision may be written by the engine")
	assert.Empty(t, matches[0].Winner, "no winner may be assigned by the engine")
	assert.Len(t, matches[0].SubResults, 1, "the bout log must stay intact")
}

// TestMaybeAdvanceKachinuki_SimultaneousExhaustionStaysRunning verifies that
// when both teams of a POOL match look simultaneously exhausted after a
// hikiwake, the encounter literally STAYS RUNNING (operator-led completion,
// mp-gmcg): the match starts in MatchStatusRunning and the advisory
// BothExhausted verdict must preserve that status verbatim — no completion,
// no draw decision, no winner. The operator records the draw explicitly. See
// TestMaybeAdvanceKachinuki_BracketSimultaneousExhaustionStaysRunning for the
// bracket twin.
func TestMaybeAdvanceKachinuki_SimultaneousExhaustionStaysRunning(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-simultaneous-exhaustion"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Format:        state.CompFormatMixed,
	}))
	// Single hikiwake bout, both players are the last on their side. The
	// match is explicitly Running so the test can pin status preservation.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:     "P1-0",
			SideA:  "RedTeam",
			SideB:  "WhiteTeam",
			Status: state.MatchStatusRunning,
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
	assert.False(t, changed, "simultaneous exhaustion is advisory; the engine must not mutate the match")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Equal(t, state.MatchStatusRunning, matches[0].Status, "the pool encounter must stay running for the operator")
	assert.Empty(t, matches[0].Decision, "no draw decision may be written by the engine")
	assert.Empty(t, matches[0].Winner, "no winner on the untouched encounter")
	assert.Len(t, matches[0].SubResults, 1, "the bout log must stay intact")
}

// TestMaybeAdvanceKachinuki_MatchEndedAdvisoryNoOp verifies that a MatchEnded
// verdict for the SideB winner (SideA snapshot exhausted) is ADVISORY only
// (operator-led completion, mp-gmcg): the engine must not complete the pool
// match, assign WhiteTeam the win, or touch the bout log. Symmetric twin of
// TestMaybeAdvanceKachinuki_ExhaustedSnapshotNoOp (which pins WinningSide=A).
func TestMaybeAdvanceKachinuki_MatchEndedAdvisoryNoOp(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-match-ended"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Format:        state.CompFormatMixed,
	}))
	// After B-Senpo beats A-Senpo, A's bout-log-only roster is empty.
	// With kachinukiRemainingRoster: knownA={A-Senpo}, retiredA={A-Senpo},
	// remainingA=[]. knownB={B-Senpo}, retiredB={}, remainingB=[B-Senpo].
	// AdvanceKachinuki sees SideA=[] → WinningSide="B" → MatchEnded=true,
	// which the caller must log and otherwise ignore.
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
	assert.False(t, changed, "MatchEnded is advisory; the engine must not mutate the match")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "the engine must never auto-complete")
	assert.Empty(t, matches[0].Winner, "no winner may be assigned by the engine")
	assert.Empty(t, matches[0].Decision, "no decision may be written by the engine")
	assert.Len(t, matches[0].SubResults, 1, "the bout log must stay intact")
}

// A4 bracket bout-append tests ------------------------------------------------

// TestMaybeAdvanceKachinuki_BracketNoAutoFinalize verifies that a bracket
// kachinuki match whose roster snapshot reads as exhausted is NOT finalized
// by the engine and, in particular, NO winner is propagated to the next
// round (operator-led completion, mp-gmcg): the operator's explicit
// completed score write owns finalization and propagation.
func TestMaybeAdvanceKachinuki_BracketNoAutoFinalize(t *testing.T) {
	compID := "kachinuki-bracket-propagates-winner"
	eng, store, _ := setupKachinukiComp(t, compID, 5)

	// 2-round bracket: Round 0 = [SF1, SF2], Round 1 = [Final].
	// SF1: TeamA vs TeamB. Bout 1: A-Senpo beats B-Senpo.
	// Bout-log-only fallback: knownB={B-Senpo}, retiredB={B-Senpo} → remainingB=[].
	// AdvanceKachinuki → MatchEnded=true (advisory only): the engine must not
	// complete SF1 nor feed TeamA into the Final.
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
	assert.False(t, changed, "an exhausted bracket snapshot is advisory; the engine must not finalize SF1")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.NotEqual(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status, "SF1 must not be auto-completed")
	assert.Empty(t, bracket.Rounds[0][0].Winner, "no winner may be assigned by the engine")
	assert.Len(t, bracket.Rounds[0][0].SubResults, 1, "the bout log must stay intact")
	// No propagation: the Final's slots must stay empty until the operator
	// completes SF1 through the scoring path.
	assert.Empty(t, bracket.Rounds[1][0].SideA, "Final SideA must not be populated by this call")
	assert.Empty(t, bracket.Rounds[1][0].SideB, "Final SideB must not be populated by this call")
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

	t.Run("malformed negative position (< -1) is dropped, not sorted first", func(t *testing.T) {
		// Real bouts are non-negative; the daihyosen is -1. A Position < -1 is
		// malformed and must not be preserved-and-sorted ahead of real bouts,
		// mirroring the defensive skip in the aggregates.
		stored := []state.SubMatchResult{
			{Position: 1, Winner: "R-1", Decision: "fought"},
			{Position: -2, Winner: "bogus", Decision: "fought"},
		}
		out := mergeKachinukiSubResults(stored, nil)
		require.Len(t, out, 1, "the -2 row is dropped")
		assert.Equal(t, 1, out[0].Position)
	})
}

// TestReopenKachinukiMatch pins the sanctioned reopen path (mp-gmcg,
// spec 006 decision 4): a completed kachinuki match returns to running
// with the match-level outcome cleared and the bout log intact.
// Kachinuki-only; bracket reopens retract an unfought propagated winner
// (next-round slot back to its "Winner of rX-mY" placeholder, bronze
// loser slot back to empty) and reject when downstream already fought.
func TestReopenKachinukiMatch(t *testing.T) {
	completedPool := func() state.MatchResult {
		return state.MatchResult{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
			Winner: "RedTeam", WinnerID: "red-id", Decision: "kachinuki-exhaustion",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		}
	}

	t.Run("non-kachinuki competition rejected with ValidationError", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "fixed", TeamSize: 3}))
		require.NoError(t, store.SavePoolMatches("fixed", []state.MatchResult{completedPool()}))
		err := eng.ReopenKachinukiMatch("fixed", "P1-0")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr, "non-kachinuki reopen must be a validation error (400)")
	})

	t.Run("pool match reopened: running, outcome cleared, bout log kept", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-pool", 3)
		require.NoError(t, store.SavePoolMatches("reopen-pool", []state.MatchResult{completedPool()}))
		require.NoError(t, eng.ReopenKachinukiMatch("reopen-pool", "P1-0"))

		matches, err := store.LoadPoolMatches("reopen-pool")
		require.NoError(t, err)
		require.Len(t, matches, 1)
		assert.Equal(t, state.MatchStatusRunning, matches[0].Status)
		assert.Empty(t, matches[0].Winner)
		assert.Empty(t, matches[0].WinnerID, "the resolved winner id must be cleared with the winner")
		assert.Empty(t, matches[0].Decision)
		require.Len(t, matches[0].SubResults, 1, "bout log kept")
	})

	t.Run("not completed", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-running", 3)
		m := completedPool()
		m.Status = state.MatchStatusRunning
		m.Winner = ""
		require.NoError(t, store.SavePoolMatches("reopen-running", []state.MatchResult{m}))
		err := eng.ReopenKachinukiMatch("reopen-running", "P1-0")
		assert.ErrorIs(t, err, ErrReopenNotCompleted)
	})

	t.Run("match not found", func(t *testing.T) {
		eng, _, _ := setupKachinukiComp(t, "reopen-missing", 3)
		err := eng.ReopenKachinukiMatch("reopen-missing", "nope")
		var nfErr *NotFoundError
		assert.ErrorAs(t, err, &nfErr)
	})

	t.Run("semifinal reopen retracts the final slot AND the bronze loser", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-semi", 3)
		require.NoError(t, store.SaveBracket("reopen-semi", &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{
						ID: "SF0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
						Winner: "RedTeam", Decision: "kachinuki-exhaustion",
						SubResults: []state.SubMatchResult{
							{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
						},
					},
					{ID: "SF1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusScheduled},
				},
				{
					{ID: "F0", SideA: "RedTeam", SideB: "Winner of r2-m1", Status: state.MatchStatusScheduled},
				},
			},
			ThirdPlaceMatch: &state.BracketMatch{
				ID: "B0", SideA: "WhiteTeam", SideB: "", Status: state.MatchStatusScheduled,
			},
		}))
		require.NoError(t, eng.ReopenKachinukiMatch("reopen-semi", "SF0"))

		bracket, err := store.LoadBracket("reopen-semi")
		require.NoError(t, err)
		sf := bracket.Rounds[0][0]
		assert.Equal(t, state.MatchStatusRunning, sf.Status)
		assert.Empty(t, sf.Winner)
		assert.Empty(t, sf.Decision)
		require.Len(t, sf.SubResults, 1, "bout log kept")
		assert.Equal(t, "Winner of r2-m0", bracket.Rounds[1][0].SideA, "final slot retracted to the generation placeholder")
		assert.Equal(t, "Winner of r2-m1", bracket.Rounds[1][0].SideB, "sibling slot untouched")
		assert.Empty(t, bracket.ThirdPlaceMatch.SideA, "bronze loser slot retracted")
	})

	t.Run("downstream fought rejects the reopen and leaves the bracket untouched", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-blocked", 3)
		require.NoError(t, store.SaveBracket("reopen-blocked", &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{
						ID: "SF0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
						Winner: "RedTeam", Decision: "kachinuki-exhaustion",
					},
					{ID: "SF1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusCompleted, Winner: "Kuma"},
				},
				{
					{
						ID: "F0", SideA: "RedTeam", SideB: "Kuma", Status: state.MatchStatusRunning,
						SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "K-1"}},
					},
				},
			},
		}))
		err := eng.ReopenKachinukiMatch("reopen-blocked", "SF0")
		assert.ErrorIs(t, err, ErrReopenDownstreamFought)

		bracket, lerr := store.LoadBracket("reopen-blocked")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status)
		assert.Equal(t, "RedTeam", bracket.Rounds[1][0].SideA, "downstream pairing untouched")
	})

	t.Run("bronze match reopens with no downstream checks", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-bronze", 3)
		require.NoError(t, store.SaveBracket("reopen-bronze", &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{{ID: "F0", SideA: "RedTeam", SideB: "Kuma", Status: state.MatchStatusScheduled}},
			},
			ThirdPlaceMatch: &state.BracketMatch{
				ID: "B0", SideA: "WhiteTeam", SideB: "Washi", Status: state.MatchStatusCompleted,
				Winner: "Washi", Decision: "kachinuki-exhaustion",
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "W-1", SideB: "X-1", IpponsB: []string{"M"}, Winner: "X-1", Decision: "fought"},
				},
			},
		}))
		require.NoError(t, eng.ReopenKachinukiMatch("reopen-bronze", "B0"))

		bracket, err := store.LoadBracket("reopen-bronze")
		require.NoError(t, err)
		assert.Equal(t, state.MatchStatusRunning, bracket.ThirdPlaceMatch.Status)
		assert.Empty(t, bracket.ThirdPlaceMatch.Winner)
		require.Len(t, bracket.ThirdPlaceMatch.SubResults, 1, "bout log kept")
	})
}

// TestStripTrailingUnscoredKachinukiBouts pins the completed-write strip
// (mp-gmcg): MaybeAdvanceKachinuki auto-appends the next pairing after each
// scored bout, so an operator's "End match" leaves an abandoned unscored
// placeholder at the tail. The server strips only TRAILING unscored rows,
// stopping at the first scored row or any Position <= 0 sentinel.
func TestStripTrailingUnscoredKachinukiBouts(t *testing.T) {
	scored := func(pos int, winner string) state.SubMatchResult {
		return state.SubMatchResult{Position: pos, SideA: "R", SideB: "W", IpponsA: []string{"M"}, Winner: winner, Decision: "fought"}
	}
	placeholder := func(pos int) state.SubMatchResult {
		return state.SubMatchResult{Position: pos, SideA: "R", SideB: "W"}
	}

	tests := []struct {
		name    string
		in      []state.SubMatchResult
		wantPos []int
	}{
		{
			name:    "trailing unscored placeholder stripped",
			in:      []state.SubMatchResult{scored(1, "R"), placeholder(2)},
			wantPos: []int{1},
		},
		{
			name:    "trailing scored bout kept",
			in:      []state.SubMatchResult{scored(1, "R"), scored(2, "W")},
			wantPos: []int{1, 2},
		},
		{
			name:    "interior unscored row kept, only the tail strips",
			in:      []state.SubMatchResult{scored(1, "R"), placeholder(2), scored(3, "W"), placeholder(4)},
			wantPos: []int{1, 2, 3},
		},
		{
			name: "tied final bout with hikiwake decision kept",
			in: []state.SubMatchResult{
				scored(1, "R"),
				{Position: 2, SideA: "R-2", SideB: "W-2", Decision: "hikiwake"},
			},
			wantPos: []int{1, 2},
		},
		{
			name:    "multiple trailing unscored rows all stripped back to the last scored bout",
			in:      []state.SubMatchResult{scored(1, "R"), placeholder(2), placeholder(3)},
			wantPos: []int{1},
		},
		{
			name:    "log with only unscored rows strips to empty",
			in:      []state.SubMatchResult{placeholder(1), placeholder(2)},
			wantPos: []int{},
		},
		{
			name: "trailing daihyosen sentinel stops the walk (legacy row never deleted)",
			in: []state.SubMatchResult{
				scored(1, "R"),
				{Position: state.DaihyosenSubPosition, SideA: "Ryu", SideB: "Tora", Winner: "Ryu", Decision: "daihyosen"},
			},
			wantPos: []int{1, state.DaihyosenSubPosition},
		},
		{
			name: "unscored trailing row BEFORE a sentinel is protected too (walk stops entirely)",
			in: []state.SubMatchResult{
				scored(1, "R"),
				placeholder(2),
				{Position: state.DaihyosenSubPosition, SideA: "Ryu", SideB: "Tora", Winner: "Ryu", Decision: "daihyosen"},
			},
			wantPos: []int{1, 2, state.DaihyosenSubPosition},
		},
		{
			name: "encho marker counts as scored",
			in: []state.SubMatchResult{
				scored(1, "R"),
				{Position: 2, SideA: "R", SideB: "W", Encho: &state.EnchoMetadata{PeriodCount: 1}},
			},
			wantPos: []int{1, 2},
		},
		{
			name: "hansoku-only bout counts as scored",
			in: []state.SubMatchResult{
				scored(1, "R"),
				{Position: 2, SideA: "R", SideB: "W", HansokuB: 1},
			},
			wantPos: []int{1, 2},
		},
		{
			name: "placeholder-dot ippons are still unscored",
			in: []state.SubMatchResult{
				scored(1, "R"),
				{Position: 2, SideA: "R", SideB: "W", IpponsA: []string{"•", ""}, IpponsB: []string{""}},
			},
			wantPos: []int{1},
		},
		{
			name:    "empty log stays empty",
			in:      nil,
			wantPos: []int{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := stripTrailingUnscoredKachinukiBouts(tt.in)
			gotPos := make([]int, 0, len(out))
			for _, s := range out {
				gotPos = append(gotPos, s.Position)
			}
			assert.Equal(t, tt.wantPos, gotPos)
		})
	}
}

// TestApplyKachinukiMerge_StripOnCompletedOnly pins WHERE the strip runs:
// applyKachinukiMerge is the single chokepoint both scoring twins funnel
// through, and it strips only when the incoming write is completed. A
// running write (autosave, record-bout) must keep the appended placeholder.
func TestApplyKachinukiMerge_StripOnCompletedOnly(t *testing.T) {
	comp := &state.Competition{
		ID:            "strip-choke",
		TeamSize:      3,
		TeamMatchType: state.TeamMatchTypeKachinuki,
	}
	prior := &state.MatchResult{
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			{Position: 2, SideA: "R-1", SideB: "W-2"}, // server-appended, never fought
		},
	}

	t.Run("completed write strips the merged trailing placeholder", func(t *testing.T) {
		result := &state.MatchResult{
			Status: state.MatchStatusCompleted,
			Winner: "Ryu",
			SubResults: []state.SubMatchResult{
				// Stale client omits the appended bout 2; the merge restores it,
				// then the completed-status strip removes it again.
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		}
		applyKachinukiMerge(comp, prior, result)
		require.Len(t, result.SubResults, 1, "the abandoned placeholder must not survive a completed write")
		assert.Equal(t, 1, result.SubResults[0].Position)
	})

	t.Run("running write keeps the merged placeholder", func(t *testing.T) {
		result := &state.MatchResult{
			Status: state.MatchStatusRunning,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		}
		applyKachinukiMerge(comp, prior, result)
		require.Len(t, result.SubResults, 2, "a running write must preserve the server-appended bout")
		assert.Equal(t, 2, result.SubResults[1].Position)
	})

	t.Run("non-kachinuki competition is untouched", func(t *testing.T) {
		fixed := &state.Competition{ID: "fixed", TeamSize: 3}
		result := &state.MatchResult{
			Status:     state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{{Position: 1}, {Position: 2}},
		}
		applyKachinukiMerge(fixed, prior, result)
		assert.Len(t, result.SubResults, 2, "no merge and no strip outside kachinuki")
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

// TestMaybeAdvanceKachinuki_PoolSimultaneousExhaustionNoOp pins that the pool
// BothExhausted auto-draw is REMOVED (operator-led completion, mp-gmcg): a
// hikiwake that retires the last lineup player on both sides of a POOL match
// leaves the encounter untouched — still running, no draw decision, no
// winner, full bout log intact. The operator records the draw explicitly
// through the normal scoring path.
func TestMaybeAdvanceKachinuki_PoolSimultaneousExhaustionNoOp(t *testing.T) {
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
	// After bout 3: remainingA=[], remainingB=[] → BothExhausted, an advisory
	// verdict the engine logs and otherwise ignores.
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
	assert.False(t, changed, "the pool BothExhausted auto-draw is removed; the engine must not mutate the match")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.NotEqual(t, state.MatchStatusCompleted, matches[0].Status, "the engine must never auto-complete the pool draw")
	assert.Empty(t, matches[0].Decision, "no draw decision may be written by the engine")
	assert.Empty(t, matches[0].Winner, "no winner on the untouched pool encounter")
	assert.Len(t, matches[0].SubResults, 3, "the full bout log must stay intact")
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
