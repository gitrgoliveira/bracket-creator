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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "any-match")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "nonexistent")
	assert.NoError(t, err)
	assert.False(t, changed)
}

// TestAdvanceKachinuki_HikiwakeSideAExhausted covers the case where SideA
// has no replacement after a hikiwake but SideB does. Operator ruling:
// the fighter who just tied STAYS ON THE SLOT (under the taisho rule a
// drawing Taisho continues, with nothing to re-type), paired against
// SideB's next fighter; under plain exhaustion the operator gives the
// survivor the per-bout fusensho and Ends on that point (the walkover,
// spec 006 decision 2). It must NOT flag MatchEnded: that is the win
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
	assert.False(t, res.MatchEnded, "hikiwake leaves no decisive point; the appended bout expresses the outcome")
	assert.False(t, res.BothExhausted)
	require.NotNil(t, res.Next, "expected the next-bout slot")
	assert.Equal(t, 4, res.Next.Position)
	assert.Equal(t, "A-Chuken", res.Next.SideA, "the fighter who just tied stays on the slot")
	assert.Equal(t, "B-Fukusho", res.Next.SideB)
}

// TestAdvanceKachinuki_HikiwakeSideBExhausted mirrors the stays-on-slot
// contract for SideB having no replacement after a hikiwake.
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
	assert.False(t, res.MatchEnded, "hikiwake leaves no decisive point; the appended bout expresses the outcome")
	assert.False(t, res.BothExhausted)
	require.NotNil(t, res.Next, "expected the next-bout slot")
	assert.Equal(t, 4, res.Next.Position)
	assert.Equal(t, "A-Fukusho", res.Next.SideA)
	assert.Equal(t, "B-Chuken", res.Next.SideB, "the fighter who just tied stays on the slot")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, bracketMatchID)
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "m-bronze")
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

	changed, postLog, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout should have been appended")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Len(t, matches[0].SubResults, 3, "bout 3 should have been appended")
	assert.Equal(t, "A-Jiho", matches[0].SubResults[2].SideA, "A-Jiho stays as SideA winner")
	assert.Equal(t, "B-Senpo", matches[0].SubResults[2].SideB, "B-Senpo is next SideB")

	// mp-gmcg review E1: the returned postLog is the FULL post-append bout log,
	// so the handler echoes the appended pairing without re-reading the store.
	// It must match what was persisted.
	require.Len(t, postLog, 3, "postLog carries the appended bout")
	assert.Equal(t, matches[0].SubResults, postLog, "postLog equals the persisted bout log")
	assert.Equal(t, 3, postLog[2].Position, "appended bout carries its position")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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
		changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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
	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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
	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "SF1")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "B-Final")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "m-bronze")
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
		err := eng.ReopenKachinukiMatch("fixed", "P1-0", "operator error")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr, "non-kachinuki reopen must be a validation error (400)")
	})

	t.Run("pool match reopened: running, outcome cleared, bout log kept", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-pool", 3)
		require.NoError(t, store.SavePoolMatches("reopen-pool", []state.MatchResult{completedPool()}))
		require.NoError(t, eng.ReopenKachinukiMatch("reopen-pool", "P1-0", "  wrong winner recorded  "))

		matches, err := store.LoadPoolMatches("reopen-pool")
		require.NoError(t, err)
		require.Len(t, matches, 1)
		assert.Equal(t, state.MatchStatusRunning, matches[0].Status)
		assert.Empty(t, matches[0].Winner)
		assert.Empty(t, matches[0].WinnerID, "the resolved winner id must be cleared with the winner")
		assert.Empty(t, matches[0].Decision)
		require.Len(t, matches[0].SubResults, 1, "bout log kept")
		assert.Equal(t, "wrong winner recorded", matches[0].CorrectionReason,
			"the reopen reason is the audit trail for rewriting a finalized result, and must be stored trimmed")
	})

	t.Run("not completed", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-running", 3)
		m := completedPool()
		m.Status = state.MatchStatusRunning
		m.Winner = ""
		require.NoError(t, store.SavePoolMatches("reopen-running", []state.MatchResult{m}))
		err := eng.ReopenKachinukiMatch("reopen-running", "P1-0", "operator error")
		assert.ErrorIs(t, err, ErrReopenNotCompleted)
	})

	t.Run("match not found", func(t *testing.T) {
		eng, _, _ := setupKachinukiComp(t, "reopen-missing", 3)
		err := eng.ReopenKachinukiMatch("reopen-missing", "nope", "operator error")
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
		require.NoError(t, eng.ReopenKachinukiMatch("reopen-semi", "SF0", "taisho bout must be re-fought"))

		bracket, err := store.LoadBracket("reopen-semi")
		require.NoError(t, err)
		sf := bracket.Rounds[0][0]
		assert.Equal(t, state.MatchStatusRunning, sf.Status)
		assert.Empty(t, sf.Winner)
		assert.Empty(t, sf.Decision)
		require.Len(t, sf.SubResults, 1, "bout log kept")
		assert.Equal(t, "taisho bout must be re-fought", sf.CorrectionReason, "reopen reason persisted on the bracket match")
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
		err := eng.ReopenKachinukiMatch("reopen-blocked", "SF0", "operator error")
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
		require.NoError(t, eng.ReopenKachinukiMatch("reopen-bronze", "B0", "bronze scored on the wrong sheet"))

		bracket, err := store.LoadBracket("reopen-bronze")
		require.NoError(t, err)
		assert.Equal(t, state.MatchStatusRunning, bracket.ThirdPlaceMatch.Status)
		assert.Empty(t, bracket.ThirdPlaceMatch.Winner)
		require.Len(t, bracket.ThirdPlaceMatch.SubResults, 1, "bout log kept")
		assert.Equal(t, "bronze scored on the wrong sheet", bracket.ThirdPlaceMatch.CorrectionReason,
			"reopen reason persisted on the bronze match")
	})
}

// TestReopenKachinukiMatch_ReopenPending pins the one-tap reopen's audit
// carry-forward (mp-gmcg) across ALL THREE match homes: pool, bracket round,
// and the bronze (3rd-place) match, which is a SIBLING of Rounds and is
// exactly the branch a per-home copy of this rule would lose.
//
// Reopen no longer demands a justification, so a reason-less reopen must
// record that one is outstanding (ReopenPending); the score path then refuses
// to complete the match again without a correctionReason. A reopen that DID
// carry a reason is already justified and must leave nothing outstanding —
// otherwise volunteering a reason would be strictly worse than staying silent.
func TestReopenKachinukiMatch_ReopenPending(t *testing.T) {
	completedPool := func() state.MatchResult {
		return state.MatchResult{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
			Winner: "RedTeam", Decision: "kachinuki-exhaustion",
		}
	}
	completedBracket := func() *state.Bracket {
		return &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{{
					ID: "F0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
					Winner: "RedTeam", Decision: "kachinuki-exhaustion",
				}},
			},
			ThirdPlaceMatch: &state.BracketMatch{
				ID: "B0", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusCompleted,
				Winner: "Kuma", Decision: "kachinuki-exhaustion",
			},
		}
	}

	for _, tc := range []struct {
		name        string
		reason      string
		wantPending bool
	}{
		{"no reason: justification outstanding", "", true},
		{"whitespace-only reason: outstanding (trimmed to empty)", "   \t ", true},
		{"reason supplied: nothing outstanding", "ended one bout too early", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("pool", func(t *testing.T) {
				compID := "reopen-pending-pool"
				eng, store, _ := setupKachinukiComp(t, compID, 3)
				require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{completedPool()}))
				require.NoError(t, eng.ReopenKachinukiMatch(compID, "P1-0", tc.reason))

				matches, err := store.LoadPoolMatches(compID)
				require.NoError(t, err)
				require.Len(t, matches, 1)
				assert.Equal(t, tc.wantPending, matches[0].ReopenPending)
			})

			t.Run("bracket round", func(t *testing.T) {
				compID := "reopen-pending-bracket"
				eng, store, _ := setupKachinukiComp(t, compID, 3)
				require.NoError(t, store.SaveBracket(compID, completedBracket()))
				require.NoError(t, eng.ReopenKachinukiMatch(compID, "F0", tc.reason))

				bracket, err := store.LoadBracket(compID)
				require.NoError(t, err)
				assert.Equal(t, tc.wantPending, bracket.Rounds[0][0].ReopenPending)
			})

			t.Run("bronze", func(t *testing.T) {
				compID := "reopen-pending-bronze"
				eng, store, _ := setupKachinukiComp(t, compID, 3)
				require.NoError(t, store.SaveBracket(compID, completedBracket()))
				require.NoError(t, eng.ReopenKachinukiMatch(compID, "B0", tc.reason))

				bracket, err := store.LoadBracket(compID)
				require.NoError(t, err)
				require.NotNil(t, bracket.ThirdPlaceMatch)
				assert.Equal(t, tc.wantPending, bracket.ThirdPlaceMatch.ReopenPending)
			})
		})
	}
}

// TestRevertMatchToQueue_ClearsReopenPending pins mp-gmcg review R1: a match
// reopened without a reason carries ReopenPending, and sending it back to the
// queue must CLEAR that audit debt. If it didn't, the requeued (now scheduled,
// empty) match would still owe a correctionReason for a result that no longer
// exists, and applyCorrectionReasonUnderTx would reject its next honest
// finalization demanding one. reopenBracketMatch's doc names this mirror
// obligation on RevertMatchToQueue; the two homes carry the flag separately.
func TestRevertMatchToQueue_ClearsReopenPending(t *testing.T) {
	t.Run("pool", func(t *testing.T) {
		compID := "revert-pending-pool"
		eng, store, _ := setupKachinukiComp(t, compID, 3)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
			Winner: "RedTeam", Decision: "kachinuki-exhaustion",
		}}))
		require.NoError(t, eng.ReopenKachinukiMatch(compID, "P1-0", "")) // reason-less → pending
		before, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.True(t, before[0].ReopenPending, "precondition: reason-less reopen sets the flag")

		require.NoError(t, eng.RevertMatchToQueue(compID, "P1-0"))
		after, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		assert.Equal(t, state.MatchStatusScheduled, after[0].Status)
		assert.False(t, after[0].ReopenPending, "requeue must clear the outstanding reopen debt")
	})

	t.Run("bracket", func(t *testing.T) {
		compID := "revert-pending-bracket"
		eng, store, _ := setupKachinukiComp(t, compID, 3)
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{{{
				ID: "F0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
				Winner: "RedTeam", Decision: "kachinuki-exhaustion",
			}}},
		}))
		require.NoError(t, eng.ReopenKachinukiMatch(compID, "F0", "")) // reason-less → pending
		before, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.True(t, before.Rounds[0][0].ReopenPending, "precondition")

		require.NoError(t, eng.RevertMatchToQueue(compID, "F0"))
		after, err := store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, state.MatchStatusScheduled, after.Rounds[0][0].Status)
		assert.False(t, after.Rounds[0][0].ReopenPending, "requeue must clear the outstanding reopen debt")
	})
}

// TestOverrideBracketWinner_ClearsReopenPending pins the related half of R1:
// override-winner is a second finalization path that bypasses
// applyCorrectionReasonUnderTx, so a reopened bracket match closed out via an
// override must also discharge ReopenPending, or the flag lingers indefinitely.
func TestOverrideBracketWinner_ClearsReopenPending(t *testing.T) {
	compID := "override-pending"
	eng, store, _ := setupKachinukiComp(t, compID, 3)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{{
			ID: "F0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
			Winner: "RedTeam", Decision: "kachinuki-exhaustion",
		}}},
	}))
	require.NoError(t, eng.ReopenKachinukiMatch(compID, "F0", "")) // reason-less → pending
	before, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.True(t, before.Rounds[0][0].ReopenPending, "precondition")

	applied, err := eng.OverrideBracketWinner(compID, "F0", "WhiteTeam", 0)
	require.NoError(t, err)
	require.True(t, applied)
	after, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "WhiteTeam", after.Rounds[0][0].Winner)
	assert.False(t, after.Rounds[0][0].ReopenPending, "an override discharges the reopen debt")
}

// TestReopenKachinukiMatch_DiscardsVerdictKeepsBoutLog pins the split that
// makes a reopen safe: the BOUT LOG is sacred, the ENCOUNTER VERDICT is not.
//
// Every fact about a bout that was actually fought lives on its
// SubMatchResult — who fought it, what they struck, hansoku, hantei, encho —
// so SubResults must survive a reopen untouched. Everything at match level is
// the discarded verdict ABOUT those bouts and must not: a reopened match that
// still advertises a winner, a default-win scoreline, a hantei/encho state, or
// a manual-override badge is describing a result the operator just threw away.
//
// Both homes are checked because they carry the verdict in different fields
// (MatchResult keeps ippons + WinnerID + rep nominations; BracketMatch keeps
// rendered ScoreA/ScoreB and IsOverridden) and drifted before.
func TestReopenKachinukiMatch_DiscardsVerdictKeepsBoutLog(t *testing.T) {
	boutLog := []state.SubMatchResult{
		{
			Position: 1, SideA: "R-1", SideB: "W-1",
			IpponsA: []string{"M"}, IpponsB: []string{"K"},
			HansokuB: 1, Winner: "R-1", Decision: "fought",
			DecidedByHantei: true, Encho: &state.EnchoMetadata{PeriodCount: 2},
		},
		{
			Position: 2, SideA: "R-1", SideB: "W-2",
			IpponsA: []string{}, IpponsB: []string{}, Decision: "hikiwake",
		},
	}
	hantei := true

	t.Run("pool", func(t *testing.T) {
		compID := "reopen-verdict-pool"
		eng, store, _ := setupKachinukiComp(t, compID, 3)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusCompleted,
			Winner: "RedTeam", WinnerID: "red-uuid",
			IpponsA: []string{"○", "○"}, IpponsB: []string{"M"},
			Decision: "kiken-voluntary", DecisionBy: "shiro", DecisionReason: "withdrew",
			Encho: &state.EnchoMetadata{PeriodCount: 1}, DecidedByHantei: &hantei,
			ResultSource: "admin", RepPlayerA: "R-2", RepPlayerB: "W-2",
			SubResults: boutLog,
		}}))
		require.NoError(t, eng.ReopenKachinukiMatch(compID, "P1-0", "ended too early"))

		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		require.Len(t, matches, 1)
		m := matches[0]

		// The bout log is untouched, in full.
		require.Len(t, m.SubResults, 2, "reopen must never drop a bout that was fought")
		assert.Equal(t, boutLog[0], m.SubResults[0], "bout 1 must survive byte-for-byte, encho and hantei included")
		assert.Equal(t, boutLog[1], m.SubResults[1], "including the drawn bout")
		assert.Equal(t, "R-1", m.SubResults[0].SideA, "who participated must always be recorded")

		// The verdict is gone.
		assert.Equal(t, state.MatchStatusRunning, m.Status)
		assert.Empty(t, m.Winner, "a running match must not advertise a winner")
		assert.Empty(t, m.WinnerID)
		assert.Empty(t, m.IpponsA, "the match-level default-win maru belonged to the discarded result")
		assert.Empty(t, m.IpponsB)
		assert.Empty(t, m.Decision)
		assert.Empty(t, m.DecisionBy)
		assert.Empty(t, m.DecisionReason)
		assert.Nil(t, m.Encho, "match-level encho described the bout the operator ended on")
		assert.Nil(t, m.DecidedByHantei)
		assert.Empty(t, m.ResultSource, "provenance of a result that no longer exists")
		assert.Empty(t, m.RepPlayerA)
		assert.Empty(t, m.RepPlayerB)
		assert.Equal(t, "ended too early", m.CorrectionReason, "the reopen's own reason is set, not cleared")
	})

	t.Run("bracket", func(t *testing.T) {
		compID := "reopen-verdict-bracket"
		eng, store, _ := setupKachinukiComp(t, compID, 3)
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{{
					ID: "F0", SideA: "RedTeam", SideB: "WhiteTeam",
					Status: state.MatchStatusCompleted, Winner: "RedTeam",
					ScoreA: "2", ScoreB: "1",
					Decision: "kiken-voluntary", DecisionBy: "shiro", DecisionReason: "withdrew",
					Encho: &state.EnchoMetadata{PeriodCount: 1}, DecidedByHantei: true,
					IsOverridden: true, ResultSource: "admin",
					SubResults: boutLog,
				}},
			},
		}))
		require.NoError(t, eng.ReopenKachinukiMatch(compID, "F0", "ended too early"))

		bracket, err := store.LoadBracket(compID)
		require.NoError(t, err)
		bm := bracket.Rounds[0][0]

		require.Len(t, bm.SubResults, 2, "reopen must never drop a bout that was fought")
		assert.Equal(t, boutLog[0], bm.SubResults[0])
		assert.Equal(t, boutLog[1], bm.SubResults[1])

		assert.Equal(t, state.MatchStatusRunning, bm.Status)
		assert.Empty(t, bm.Winner)
		assert.Empty(t, bm.ScoreA, "the rendered scoreline described the discarded result")
		assert.Empty(t, bm.ScoreB)
		assert.Empty(t, bm.Decision)
		assert.Empty(t, bm.DecisionBy)
		assert.Empty(t, bm.DecisionReason)
		assert.Nil(t, bm.Encho)
		assert.False(t, bm.DecidedByHantei)
		assert.False(t, bm.IsOverridden, "a reopened match is not carrying a manual override any more")
		assert.Empty(t, bm.ResultSource)
		assert.Equal(t, "ended too early", bm.CorrectionReason)
	})
}

// TestReopenKachinukiMatchCourtBusy pins the reopen court gate (mp-gmcg).
//
// NOTE ON THE FIXTURES: every OTHER reopen test builds its matches with NO
// Court set, so the gate never engaged and the bug survived review. These
// subtests set Court explicitly on BOTH matches, which is the whole point.
//
// Reopen flips the match back to RUNNING, and court exclusivity keys purely
// on `status == running` (courtOccupied). Reopening onto a busy
// court would leave two running matches there, and checkCourtExclusivityTx
// then rejects BOTH: the re-End of the reopened match AND every further
// score write to the genuinely live bout. So the reopen itself is refused.
func TestReopenKachinukiMatchCourtBusy(t *testing.T) {
	completedOnCourt := func(id, court string) state.MatchResult {
		return state.MatchResult{
			ID: id, SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
			Winner: "RedTeam", Decision: "kachinuki-exhaustion", Court: court,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		}
	}
	runningOnCourt := func(id, court string) state.MatchResult {
		return state.MatchResult{
			ID: id, SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusRunning, Court: court,
		}
	}

	t.Run("same competition: busy court rejects, the finished result is untouched", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-court-busy", 3)
		require.NoError(t, store.SavePoolMatches("reopen-court-busy", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
			runningOnCourt("P1-1", "A"),
		}))

		err := eng.ReopenKachinukiMatch("reopen-court-busy", "P1-0", "need more bouts")
		require.Error(t, err)
		var busy *CourtBusyError
		require.ErrorAs(t, err, &busy)
		assert.Equal(t, "A", busy.Court)
		assert.Equal(t, "P1-1", busy.MatchID, "the error must name the match holding the court")
		// The same-comp court scan (E4: courtFreeInCompTxWith reusing
		// findMatchHome's loaded pool matches) must still stamp the competition.
		assert.Equal(t, "reopen-court-busy", busy.CompID, "the same-comp occupant is in this competition")
		assert.ErrorIs(t, err, ErrCourtBusy, "reopen reuses the single court-busy sentinel")

		matches, lerr := store.LoadPoolMatches("reopen-court-busy")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "the reopen must not land")
		assert.Equal(t, "RedTeam", matches[0].Winner)
		assert.Empty(t, matches[0].CorrectionReason, "a rejected reopen must not stamp an audit reason")
		assert.Equal(t, state.MatchStatusRunning, matches[1].Status, "the live match keeps the court")
	})

	t.Run("same competition: a free court still reopens", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-court-free", 3)
		require.NoError(t, store.SavePoolMatches("reopen-court-free", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
			// Running, but on a DIFFERENT court: no conflict.
			runningOnCourt("P1-1", "B"),
		}))

		require.NoError(t, eng.ReopenKachinukiMatch("reopen-court-free", "P1-0", "need more bouts"))
		matches, lerr := store.LoadPoolMatches("reopen-court-free")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusRunning, matches[0].Status)
	})

	// mp-gmcg review E4: the reopen court scan reuses findMatchHome's loaded
	// pool matches AND loads the bracket when the home is a pool match. These
	// two cross-store cases exercise both branches: a bracket reopen must still
	// see a POOL occupant (reused pool slice), and a pool reopen must still see
	// a BRACKET occupant (the bracket the pool-home walk did not load).
	t.Run("bracket-home reopen is blocked by a POOL match on the same court", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-bracket-pool-busy", 3)
		require.NoError(t, store.SaveBracket("reopen-bracket-pool-busy", &state.Bracket{
			Rounds: [][]state.BracketMatch{{
				{ID: "SF0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
					Winner: "RedTeam", Decision: "kachinuki-exhaustion", Court: "A"},
			}},
		}))
		require.NoError(t, store.SavePoolMatches("reopen-bracket-pool-busy", []state.MatchResult{
			runningOnCourt("P1-0", "A"),
		}))

		err := eng.ReopenKachinukiMatch("reopen-bracket-pool-busy", "SF0", "need more bouts")
		var busy *CourtBusyError
		require.ErrorAs(t, err, &busy)
		assert.Equal(t, "P1-0", busy.MatchID, "a pool match holding the court blocks a bracket reopen")
		assert.Equal(t, "reopen-bracket-pool-busy", busy.CompID)
	})

	t.Run("pool-home reopen is blocked by a BRACKET match on the same court", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-pool-bracket-busy", 3)
		require.NoError(t, store.SavePoolMatches("reopen-pool-bracket-busy", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
		}))
		require.NoError(t, store.SaveBracket("reopen-pool-bracket-busy", &state.Bracket{
			Rounds: [][]state.BracketMatch{{
				{ID: "SF1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusRunning, Court: "A"},
			}},
		}))

		err := eng.ReopenKachinukiMatch("reopen-pool-bracket-busy", "P1-0", "need more bouts")
		var busy *CourtBusyError
		require.ErrorAs(t, err, &busy)
		assert.Equal(t, "SF1", busy.MatchID, "a bracket match holding the court blocks a pool reopen")
	})

	t.Run("bracket match on a busy court is refused too", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-court-busy-bracket", 3)
		require.NoError(t, store.SaveBracket("reopen-court-busy-bracket", &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{
						ID: "SF0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
						Winner: "RedTeam", Decision: "kachinuki-exhaustion", Court: "A",
					},
					{ID: "SF1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusRunning, Court: "A"},
				},
				{{ID: "F0", SideA: "RedTeam", SideB: "Winner of r2-m1", Status: state.MatchStatusScheduled}},
			},
		}))

		err := eng.ReopenKachinukiMatch("reopen-court-busy-bracket", "SF0", "need more bouts")
		var busy *CourtBusyError
		require.ErrorAs(t, err, &busy)
		assert.Equal(t, "SF1", busy.MatchID)

		bracket, lerr := store.LoadBracket("reopen-court-busy-bracket")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status, "the reopen must not land")
		assert.Equal(t, "RedTeam", bracket.Rounds[1][0].SideA, "the downstream slot must not be retracted")
	})

	t.Run("bronze match on a busy court is refused too", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-court-busy-bronze", 3)
		require.NoError(t, store.SaveBracket("reopen-court-busy-bronze", &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{{ID: "F0", SideA: "RedTeam", SideB: "Kuma", Status: state.MatchStatusRunning, Court: "A"}},
			},
			ThirdPlaceMatch: &state.BracketMatch{
				ID: "B0", SideA: "WhiteTeam", SideB: "Washi", Status: state.MatchStatusCompleted,
				Winner: "Washi", Decision: "kachinuki-exhaustion", Court: "A",
			},
		}))

		err := eng.ReopenKachinukiMatch("reopen-court-busy-bronze", "B0", "need more bouts")
		var busy *CourtBusyError
		require.ErrorAs(t, err, &busy)
		assert.Equal(t, "F0", busy.MatchID)

		bracket, lerr := store.LoadBracket("reopen-court-busy-bronze")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusCompleted, bracket.ThirdPlaceMatch.Status, "the reopen must not land")
	})

	t.Run("a running match in ANOTHER competition on the same court also rejects", func(t *testing.T) {
		// Courts are tournament-global, so the cross-competition half of the
		// gate (CheckCrossCompCourtBusy, run before the transaction) matters
		// just as much: two competitions sharing court A must not both put a
		// running match on it.
		eng, store, _ := setupKachinukiComp(t, "reopen-cross-a", 3)
		require.NoError(t, store.SavePoolMatches("reopen-cross-a", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
		}))
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "reopen-cross-b"}))
		require.NoError(t, store.SavePoolMatches("reopen-cross-b", []state.MatchResult{
			runningOnCourt("P9-0", "A"),
		}))

		err := eng.ReopenKachinukiMatch("reopen-cross-a", "P1-0", "need more bouts")
		var busy *CourtBusyError
		require.ErrorAs(t, err, &busy)
		assert.Equal(t, "reopen-cross-b", busy.CompID, "the conflicting competition must be named")

		matches, lerr := store.LoadPoolMatches("reopen-cross-a")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "the reopen must not land")
	})

	// mp-gmcg review: an UNREOPENABLE target (its winner already fed a fought
	// downstream) whose court is held by a running match in ANOTHER competition
	// must report the permanent ErrReopenDownstreamFought, NOT a transient
	// court_busy. Otherwise the admin remedy panel (which branches on
	// code=="court_busy") offers to requeue the court's occupant for a target that
	// can never reopen — a dead end. The plain-reopen entry pre-checks the result
	// preconditions before CheckCrossCompCourtBusy so the right 409 wins.
	t.Run("an unreopenable target is not masked by a cross-competition court_busy", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-cross-ds-a", 3)
		require.NoError(t, store.SaveBracket("reopen-cross-ds-a", &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{ID: "SF0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
						Winner: "RedTeam", Decision: "kachinuki-exhaustion", Court: "A"},
					{ID: "SF1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusCompleted, Winner: "Kuma"},
				},
				{
					// The final is already being fought — SF0's winner fed it.
					{ID: "F0", SideA: "RedTeam", SideB: "Kuma", Status: state.MatchStatusRunning,
						SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "K-1"}}},
				},
			},
		}))
		// Another competition holds SF0's court (A) with a running match.
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "reopen-cross-ds-b"}))
		require.NoError(t, store.SavePoolMatches("reopen-cross-ds-b", []state.MatchResult{
			runningOnCourt("P9-0", "A"),
		}))

		err := eng.ReopenKachinukiMatch("reopen-cross-ds-a", "SF0", "")
		require.ErrorIs(t, err, ErrReopenDownstreamFought, "the permanent reason must win over a transient court_busy")
		var busy *CourtBusyError
		require.NotErrorAs(t, err, &busy, "an unreopenable target must not surface as court_busy")

		bracket, lerr := store.LoadBracket("reopen-cross-ds-a")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status, "the target stays completed")
	})
}

// TestRequeueBlockerAndReopenKachinuki pins the atomic court-busy remedy
// (mp-gmcg review A4): under ONE court-exclusivity lock, requeue the match
// holding the court, then reopen the target onto the freed court — so no peer
// can grab the court between the two steps the old two-call client flow made.
func TestRequeueBlockerAndReopenKachinuki(t *testing.T) {
	completedOnCourt := func(id, court string) state.MatchResult {
		return state.MatchResult{
			ID: id, SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
			Winner: "RedTeam", Decision: "kachinuki-exhaustion", Court: court,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		}
	}
	runningOnCourt := func(id, court string) state.MatchResult {
		return state.MatchResult{ID: id, SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusRunning, Court: court}
	}

	t.Run("frees the blocker then reopens the target, atomically", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "requeue-reopen", 3)
		require.NoError(t, store.SavePoolMatches("requeue-reopen", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
			runningOnCourt("P1-1", "A"),
		}))

		// A plain reopen is refused: court A is busy with P1-1.
		require.ErrorIs(t, eng.ReopenKachinukiMatch("requeue-reopen", "P1-0", ""), ErrCourtBusy)

		// Requeue the blocker + reopen the target in one atomic operation.
		require.NoError(t, eng.RequeueBlockerAndReopenKachinuki("requeue-reopen", "P1-0", "requeue-reopen", "P1-1", ""))

		byID := map[string]state.MatchResult{}
		matches, _ := store.LoadPoolMatches("requeue-reopen")
		for _, m := range matches {
			byID[m.ID] = m
		}
		assert.Equal(t, state.MatchStatusScheduled, byID["P1-1"].Status, "blocker requeued")
		assert.Equal(t, state.MatchStatusRunning, byID["P1-0"].Status, "target reopened onto the freed court")
		assert.True(t, byID["P1-0"].ReopenPending, "reason-less reopen leaves the audit obligation")
	})

	t.Run("blocker in a DIFFERENT competition on the same court", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rq-target", 3)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "rq-blocker", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
		}))
		require.NoError(t, store.SavePoolMatches("rq-target", []state.MatchResult{completedOnCourt("P1-0", "A")}))
		require.NoError(t, store.SavePoolMatches("rq-blocker", []state.MatchResult{runningOnCourt("B1-0", "A")}))

		require.NoError(t, eng.RequeueBlockerAndReopenKachinuki("rq-target", "P1-0", "rq-blocker", "B1-0", ""))

		assert.Equal(t, state.MatchStatusRunning, loadPoolMatchByID(t, store, "rq-target", "P1-0").Status)
		assert.Equal(t, state.MatchStatusScheduled, loadPoolMatchByID(t, store, "rq-blocker", "B1-0").Status)
	})

	// A competition runs across SEVERAL courts, so only the match holding the
	// TARGET's court is a blocker; a running sibling on another court is
	// untouched and never blocks the reopen.
	t.Run("same competition, multiple courts: only the same-court blocker is requeued", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rq-multicourt", 3)
		require.NoError(t, store.SavePoolMatches("rq-multicourt", []state.MatchResult{
			completedOnCourt("P1-0", "A"), // target on court A
			runningOnCourt("P1-1", "A"),   // blocker holding court A
			runningOnCourt("P1-2", "B"),   // sibling on court B — different court, NOT a blocker
		}))

		require.NoError(t, eng.RequeueBlockerAndReopenKachinuki("rq-multicourt", "P1-0", "rq-multicourt", "P1-1", ""))

		byID := map[string]state.MatchResult{}
		matches, _ := store.LoadPoolMatches("rq-multicourt")
		for _, m := range matches {
			byID[m.ID] = m
		}
		assert.Equal(t, state.MatchStatusScheduled, byID["P1-1"].Status, "the court-A blocker is requeued")
		assert.Equal(t, state.MatchStatusRunning, byID["P1-0"].Status, "the target reopens onto court A")
		assert.Equal(t, state.MatchStatusRunning, byID["P1-2"].Status, "the court-B sibling is untouched (different court)")
	})

	// mp-gmcg review R1: the blocker id is CLIENT-SUPPLIED and RevertMatchToQueue
	// is destructive. Naming a bystander running on a DIFFERENT court must be
	// rejected WITHOUT wiping it — otherwise the requeue commits, the reopen then
	// fails on the court's real occupant, and the operator sees only "court busy".
	t.Run("a blocker on a DIFFERENT court is rejected and left untouched", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rq-wrongcourt", 3)
		bystander := runningOnCourt("P1-2", "B")
		bystander.IpponsA = []string{"M"} // a partial score the wipe would clear
		bystander.SubResults = []state.SubMatchResult{
			{Position: 1, SideA: "K-1", SideB: "Wa-1", IpponsA: []string{"M"}},
		}
		require.NoError(t, store.SavePoolMatches("rq-wrongcourt", []state.MatchResult{
			completedOnCourt("P1-0", "A"), // target on court A
			runningOnCourt("P1-1", "A"),   // the REAL blocker holding court A
			bystander,                     // a bystander on court B
		}))

		// Client wrongly names the court-B bystander as the blocker.
		err := eng.RequeueBlockerAndReopenKachinuki("rq-wrongcourt", "P1-0", "rq-wrongcourt", "P1-2", "")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr, "a blocker on the wrong court is a client error")

		byID := map[string]state.MatchResult{}
		matches, _ := store.LoadPoolMatches("rq-wrongcourt")
		for _, m := range matches {
			byID[m.ID] = m
		}
		assert.Equal(t, state.MatchStatusRunning, byID["P1-2"].Status, "the bystander must NOT be requeued")
		assert.Equal(t, []string{"M"}, byID["P1-2"].IpponsA, "the bystander's score must NOT be wiped")
		assert.Len(t, byID["P1-2"].SubResults, 1, "the bystander's bout log must NOT be wiped")
		assert.Equal(t, state.MatchStatusRunning, byID["P1-1"].Status, "the real blocker is untouched")
		assert.Equal(t, state.MatchStatusCompleted, byID["P1-0"].Status, "the target must NOT reopen when the guard rejects")
	})

	// mp-gmcg review U2: the target's RESULT preconditions (completed +
	// downstream-not-fought) are checked read-only BEFORE the destructive revert,
	// so a target that cannot be reopened — its winner already fed a fought
	// knockout — does not cost the blocker its live on-court score. Without the
	// pre-check the revert committed first and the reopen then failed, wiping a
	// running match for nothing.
	t.Run("a target with a fought downstream is rejected WITHOUT wiping the blocker", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rq-downstream", 3)
		require.NoError(t, store.SaveBracket("rq-downstream", &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{ID: "SF0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
						Winner: "RedTeam", Decision: "kachinuki-exhaustion", Court: "A"},
					{ID: "SF1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusCompleted, Winner: "Kuma"},
				},
				{
					// The final is already being fought — SF0's winner fed it.
					{ID: "F0", SideA: "RedTeam", SideB: "Kuma", Status: state.MatchStatusRunning,
						SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "K-1"}}},
				},
			},
		}))
		// A blocker running on SF0's court (A), in another competition, with a
		// live score the destructive revert would clear.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "rq-ds-blocker", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
		}))
		blocker := runningOnCourt("B1-0", "A")
		blocker.IpponsA = []string{"M"}
		blocker.SubResults = []state.SubMatchResult{{Position: 1, SideA: "K-1", SideB: "Wa-1", IpponsA: []string{"M"}}}
		require.NoError(t, store.SavePoolMatches("rq-ds-blocker", []state.MatchResult{blocker}))

		err := eng.RequeueBlockerAndReopenKachinuki("rq-downstream", "SF0", "rq-ds-blocker", "B1-0", "")
		assert.ErrorIs(t, err, ErrReopenDownstreamFought)

		b := loadPoolMatchByID(t, store, "rq-ds-blocker", "B1-0")
		assert.Equal(t, state.MatchStatusRunning, b.Status, "blocker must NOT be requeued when the target can't reopen")
		assert.Equal(t, []string{"M"}, b.IpponsA, "blocker's score must NOT be wiped")
		assert.Len(t, b.SubResults, 1, "blocker's bout log must NOT be wiped")

		bracket, lerr := store.LoadBracket("rq-downstream")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status, "target stays completed")
	})

	// mp-gmcg review: the fought-downstream subtest above only exercises the
	// BRACKET branch of checkTargetReopenable. checkPoolReopenDownstreamTx
	// short-circuits unless Format == Mixed (kachinuki.go), so the pre-check's
	// POOL branch is otherwise dead in this whole test — a refactor that dropped
	// its wiring would pass every case. This pins it: a MIXED-format pool target
	// whose finisher already sits in a started knockout is refused via the pool
	// branch WITHOUT wiping the blocker.
	t.Run("a mixed-pool target with a started knockout downstream is rejected WITHOUT wiping the blocker", func(t *testing.T) {
		eng, store, compID := saveMixedKachinukiCompForReopenTest(t)
		scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
		scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")
		_, allResolved, err := eng.ResolveQualifiedPools(compID)
		require.NoError(t, err)
		require.True(t, allResolved)

		// Start the knockout leaf (A1 vs B1) → RUNNING: A1, Pool A-0's finisher,
		// is now committed to it, so reopening Pool A-0 must be refused.
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
			_, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, b.Rounds[0][0].ID, &state.MatchResult{
				SideA: "A1", SideB: "B1", Status: state.MatchStatusRunning,
			})
			return e
		}))

		// Give the completed Pool A-0 a court so requireBlockerHoldsCourt passes
		// and the flow reaches the pre-check (the fixture assigns none).
		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for i := range matches {
			if matches[i].ID == "Pool A-0" {
				matches[i].Court = "A"
			}
		}
		require.NoError(t, store.SavePoolMatches(compID, matches))

		// A blocker running on Pool A-0's court (A), in another competition, with
		// a live score the destructive revert would clear.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "rq-pool-blocker", TeamSize: 2, TeamMatchType: state.TeamMatchTypeKachinuki,
		}))
		blocker := runningOnCourt("B1-0", "A")
		blocker.IpponsA = []string{"M"}
		blocker.SubResults = []state.SubMatchResult{{Position: 1, SideA: "K-1", SideB: "Wa-1", IpponsA: []string{"M"}}}
		require.NoError(t, store.SavePoolMatches("rq-pool-blocker", []state.MatchResult{blocker}))

		err = eng.RequeueBlockerAndReopenKachinuki(compID, "Pool A-0", "rq-pool-blocker", "B1-0", "")
		assert.ErrorIs(t, err, ErrReopenDownstreamFought)

		bl := loadPoolMatchByID(t, store, "rq-pool-blocker", "B1-0")
		require.NotNil(t, bl)
		assert.Equal(t, state.MatchStatusRunning, bl.Status, "blocker must NOT be requeued when the pool target can't reopen")
		assert.Equal(t, []string{"M"}, bl.IpponsA, "blocker's score must NOT be wiped")
		assert.Len(t, bl.SubResults, 1, "blocker's bout log must NOT be wiped")
		assert.Equal(t, state.MatchStatusCompleted, loadPoolMatchByID(t, store, compID, "Pool A-0").Status, "pool target stays completed")
	})

	// mp-gmcg review: the ErrReopenNotCompleted arm of the pre-check had no
	// requeue-path coverage either. A target that is not completed must be
	// refused BEFORE the destructive revert, so the blocker keeps its score.
	t.Run("a target that is NOT completed is rejected WITHOUT wiping the blocker", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rq-notdone", 3)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "rq-notdone-blocker", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
		}))
		// Target is RUNNING (not completed) but holds court A.
		require.NoError(t, store.SavePoolMatches("rq-notdone", []state.MatchResult{runningOnCourt("P1-0", "A")}))
		blocker := runningOnCourt("B1-0", "A")
		blocker.IpponsA = []string{"M"}
		blocker.SubResults = []state.SubMatchResult{{Position: 1, SideA: "K-1", SideB: "Wa-1", IpponsA: []string{"M"}}}
		require.NoError(t, store.SavePoolMatches("rq-notdone-blocker", []state.MatchResult{blocker}))

		err := eng.RequeueBlockerAndReopenKachinuki("rq-notdone", "P1-0", "rq-notdone-blocker", "B1-0", "")
		assert.ErrorIs(t, err, ErrReopenNotCompleted)

		bl := loadPoolMatchByID(t, store, "rq-notdone-blocker", "B1-0")
		require.NotNil(t, bl)
		assert.Equal(t, state.MatchStatusRunning, bl.Status, "blocker must NOT be requeued when the target isn't completed")
		assert.Equal(t, []string{"M"}, bl.IpponsA, "blocker's score must NOT be wiped")
		assert.Len(t, bl.SubResults, 1, "blocker's bout log must NOT be wiped")
	})

	// A COMPLETED match never holds the court as "running", so it is not the
	// blocker (a plain reopen is the correct remedy); R1's guard rejects it
	// rather than destructively requeuing it.
	t.Run("a COMPLETED blocker is not running, so it is rejected and the target stays finished", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rq-done-blocker", 3)
		require.NoError(t, store.SavePoolMatches("rq-done-blocker", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
			completedOnCourt("P1-1", "A"),
		}))
		err := eng.RequeueBlockerAndReopenKachinuki("rq-done-blocker", "P1-0", "rq-done-blocker", "P1-1", "")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr)
		assert.Equal(t, state.MatchStatusCompleted, loadPoolMatchByID(t, store, "rq-done-blocker", "P1-0").Status,
			"the target must not reopen when the requeue is rejected")
	})

	t.Run("a non-kachinuki target is rejected", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "rq-fixed", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed,
		}))
		err := eng.RequeueBlockerAndReopenKachinuki("rq-fixed", "P1-0", "rq-fixed", "P1-1", "")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr)
	})

	t.Run("an unknown blocker errors and leaves the target finished", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rq-nf-blocker", 3)
		require.NoError(t, store.SavePoolMatches("rq-nf-blocker", []state.MatchResult{completedOnCourt("P1-0", "A")}))
		err := eng.RequeueBlockerAndReopenKachinuki("rq-nf-blocker", "P1-0", "rq-nf-blocker", "nope", "")
		require.Error(t, err)
		assert.Equal(t, state.MatchStatusCompleted, loadPoolMatchByID(t, store, "rq-nf-blocker", "P1-0").Status)
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
			// A non-nil but degenerate zero-period Encho block is NOT a real
			// marker (EnchoMetadata.On() is false), so an otherwise-empty
			// trailing row carrying only {PeriodCount:0} is still an unscored
			// placeholder and must strip. Guards against a client-supplied
			// zero block wedging a phantom bout into a completed record.
			name: "degenerate zero-period encho block still strips as unscored",
			in: []state.SubMatchResult{
				scored(1, "R"),
				{Position: 2, SideA: "R", SideB: "W", Encho: &state.EnchoMetadata{PeriodCount: 0}},
			},
			wantPos: []int{1},
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

// TestApplyKachinukiMerge_DerivesWinnerFromBoutLog is the C1/C3-fix pin
// (mp-gmcg review C3): "OPERATOR INPUT DETERMINES THE BOUT OUTCOME" was
// enforced only by the client HIDING the generic correction button —
// applyKachinukiMerge itself accepted whatever winner a completed
// kachinuki-exhaustion write carried. It must now derive the winner from the
// merged bout log's last scored bout, overriding a client value that
// disagrees with it.
func TestApplyKachinukiMerge_DerivesWinnerFromBoutLog(t *testing.T) {
	comp := &state.Competition{
		ID: "derive-win", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
	}

	t.Run("a wrong client-supplied winner is overridden by the last bout", func(t *testing.T) {
		result := &state.MatchResult{
			SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusCompleted, Decision: "kachinuki-exhaustion",
			// The client claims WhiteTeam won; the bout log's last scored bout
			// (position 2, R-2 the winner) says RedTeam actually won.
			Winner: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
				{Position: 2, SideA: "R-2", SideB: "W-2", IpponsA: []string{"M"}, Winner: "R-2", Decision: "fought"},
			},
		}
		applyKachinukiMerge(comp, nil, result)
		assert.Equal(t, "RedTeam", result.Winner, "the bout log, not the client's claim, decides the winner")
	})

	t.Run("an absent client winner is filled from the last bout", func(t *testing.T) {
		result := &state.MatchResult{
			SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusCompleted, Decision: "kachinuki-exhaustion",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "W-1", Decision: "fought"},
			},
		}
		applyKachinukiMerge(comp, nil, result)
		assert.Equal(t, "WhiteTeam", result.Winner)
	})

	t.Run("a matching client winner is left as is", func(t *testing.T) {
		result := &state.MatchResult{
			SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusCompleted, Decision: "kachinuki-exhaustion",
			Winner: "RedTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		}
		require.NoError(t, applyKachinukiMerge(comp, nil, result))
		assert.Equal(t, "RedTeam", result.Winner)
	})

	// mp-gmcg review R2: a kachinuki-exhaustion write whose last SCORED bout is
	// a DRAW is rejected. Exhaustion means the final pairing was decisive and
	// the loser had no replacement, so a tied last bout contradicts it. Without
	// this the arbitrary client winner would survive (deriveKachinukiWinner used
	// to return early on a winnerless last bout) and a bracket match would then
	// pass validateBracketCompletion on that fabricated winner.
	t.Run("a kachinuki-exhaustion ending on a tied last bout is rejected", func(t *testing.T) {
		result := &state.MatchResult{
			SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusCompleted, Decision: "kachinuki-exhaustion",
			Winner: "RedTeam", // a winner the bout log does not support
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
				{Position: 2, SideA: "R-2", SideB: "W-2", Decision: "hikiwake"}, // scored, but a draw
			},
		}
		err := applyKachinukiMerge(comp, nil, result)
		var verr *ValidationError
		require.ErrorAs(t, err, &verr, "a decisive-win decision on a tied last bout must be rejected")
	})

	// mp-gmcg review: a kachinuki-exhaustion write whose last SCORED bout names
	// a winner matching NEITHER competitor is rejected, not accepted verbatim. A
	// kachinuki bout persists the PLAYER name as its winner and never the team
	// name, so the winner can only match the sub's own sideA/sideB; a payload
	// that carries a winner + ippons but omits those sides (a bulk PUT /scores,
	// an offline flush) used to fall through the winner switch and keep the
	// client's match-level winner, which a bracket match's validateBracketCompletion
	// would then wave through on the non-empty check alone.
	t.Run("a kachinuki-exhaustion winner matching neither side is rejected", func(t *testing.T) {
		result := &state.MatchResult{
			SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusCompleted, Decision: "kachinuki-exhaustion",
			Winner: "WhiteTeam", // an arbitrary match-level winner the log can't attribute
			SubResults: []state.SubMatchResult{
				// A decisive bout (R-1 struck a point) but with NO sub sides, so
				// neither isWinForSide branch can bind the player-name winner.
				{Position: 1, IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		}
		err := applyKachinukiMerge(comp, nil, result)
		var verr *ValidationError
		require.ErrorAs(t, err, &verr, "an unattributable deciding-bout winner must be rejected")
	})

	// The reject is scoped to kachinuki-exhaustion: a genuine drawn encounter
	// carries decision "hikiwake", which is accepted on a tied last bout.
	t.Run("a hikiwake decision on a tied last bout is accepted", func(t *testing.T) {
		result := &state.MatchResult{
			SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusCompleted, Decision: "hikiwake",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", Decision: "hikiwake"},
			},
		}
		require.NoError(t, applyKachinukiMerge(comp, nil, result), "a hikiwake draw is legitimate")
	})

	// The critical guard: kiken/fusenpai/fusensho decisions reach this SAME
	// merge point (RecordDecisionTx -> RecordMatchResultWithIneligibilityTx ->
	// applyKachinukiMerge) with their OWN winner rule (the non-withdrawing
	// side), independent of the bout log. deriveKachinukiWinner must NOT touch
	// it, or a legitimate walkover (FIK Art. 32 — the withdrawing side keeps
	// its already-struck ippons and can be "ahead" on the last logged bout)
	// would be silently overturned.
	for _, decision := range []string{"kiken-voluntary", "kiken-injury", "fusenpai", "fusensho"} {
		t.Run("a "+decision+" decision's winner is untouched even when the bout log disagrees", func(t *testing.T) {
			result := &state.MatchResult{
				SideA: "RedTeam", SideB: "WhiteTeam",
				Status: state.MatchStatusCompleted, Decision: decision,
				// RedTeam withdraws (WhiteTeam wins by walkover), but the last
				// LOGGED bout was won by a RED player — exactly the case that
				// must NOT flip the winner back to RedTeam.
				Winner: "WhiteTeam",
				SubResults: []state.SubMatchResult{
					{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
				},
			}
			applyKachinukiMerge(comp, nil, result)
			assert.Equal(t, "WhiteTeam", result.Winner, "a walkover's winner must never be re-derived from the bout log")
		})
	}

	t.Run("a running write is never touched (derivation is completed-only)", func(t *testing.T) {
		result := &state.MatchResult{
			SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusRunning, Decision: "kachinuki-exhaustion", Winner: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"},
			},
		}
		applyKachinukiMerge(comp, nil, result)
		assert.Equal(t, "WhiteTeam", result.Winner, "no derivation before the match actually completes")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

		changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
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

	changed, _, err := eng.MaybeAdvanceKachinuki(compID, "B-Final")
	require.NoError(t, err)
	assert.False(t, changed, "bracket simultaneous exhaustion must leave the match running; operator resolves via daihyosen")

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, bracket.Rounds[0], 1)
	assert.NotEqual(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status, "bracket match must not be completed")
	assert.Empty(t, bracket.Rounds[0][0].Winner, "no winner assigned by the engine for bracket simultaneous exhaustion")
}

// saveMixedKachinukiCompForReopenTest builds a mixed KACHINUKI competition (two
// 1-winner pools feeding a single knockout leaf) for exercising the pool reopen
// downstream guard. Standings derive W/L straight from the match Winner
// (computeStandingsFrom), so A1/B1 rank first in their pools once their pool
// matches complete, regardless of the (absent) sub-bout detail.
func saveMixedKachinukiCompForReopenTest(t *testing.T) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-reopen-guard"

	pools := []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "A1"}, {Name: "A2"}}},
		{PoolName: "Pool B", Players: []helper.Player{{Name: "B1"}, {Name: "B2"}}},
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		Name:          compID,
		Kind:          "team",
		Format:        state.CompFormatMixed,
		Status:        state.CompStatusPools,
		Courts:        []string{"A"},
		StartTime:     "09:00",
		PoolWinners:   1,
		TeamSize:      2,
		TeamMatchType: state.TeamMatchTypeKachinuki,
	}))
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "A1"}, {Name: "A2"}, {Name: "B1"}, {Name: "B2"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", Status: state.MatchStatusScheduled},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", Status: state.MatchStatusScheduled},
	}))
	finals := helper.GenerateFinals(pools, 1)
	tree := helper.CreateBalancedTree(finals)
	helper.ApplyPoolAdjustments(tree)
	leaves := helper.TreeToLeafArray(tree)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	bracket, err := eng.buildBracketFromLeaves(comp, leaves)
	require.NoError(t, err)
	bracket.Preview = true
	require.NoError(t, store.SaveBracket(compID, bracket))
	return eng, store, compID
}

func loadPoolMatchByID(t *testing.T, store *state.Store, compID, matchID string) *state.MatchResult {
	t.Helper()
	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for i := range matches {
		if matches[i].ID == matchID {
			return &matches[i]
		}
	}
	return nil
}

// TestReopenKachinukiPoolMatch_DownstreamKnockoutStarted_Rejected pins the
// pool-branch parity with the bracket branch (mp-gmcg): reopening a pool match
// whose current finisher already sits in a STARTED knockout match is refused
// with ErrReopenDownstreamFought, so a later re-End cannot strand the displaced
// finisher in the bracket. The score path's mp-e2k1 guard cannot catch that,
// because by re-End time the reopened match is excluded from the standings
// baseline it compares against.
func TestReopenKachinukiPoolMatch_DownstreamKnockoutStarted_Rejected(t *testing.T) {
	eng, store, compID := saveMixedKachinukiCompForReopenTest(t)

	scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
	scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)

	// Start the knockout leaf (A1 vs B1) → RUNNING: A1 is now committed to it.
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Len(t, b.Rounds, 1)
	require.Len(t, b.Rounds[0], 1)
	knockoutMatchID := b.Rounds[0][0].ID
	txErr := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, knockoutMatchID, &state.MatchResult{
			SideA: "A1", SideB: "B1", Status: state.MatchStatusRunning,
		})
		return e
	})
	require.NoError(t, txErr)

	// Reopening Pool A-0 (whose finisher A1 sits in the running knockout) is refused.
	err = eng.ReopenKachinukiMatch(compID, "Pool A-0", "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrReopenDownstreamFought)

	// The pool match stays completed with its original winner (not reopened).
	poolA0 := loadPoolMatchByID(t, store, compID, "Pool A-0")
	require.NotNil(t, poolA0)
	assert.Equal(t, state.MatchStatusCompleted, poolA0.Status)
	assert.Equal(t, "A1", poolA0.Winner)
	assert.False(t, poolA0.ReopenPending)
}

// TestReopenKachinukiPoolMatch_KnockoutNotStarted_Allowed is the companion: with
// the knockout resolved but NOT started, reopening a pool match succeeds — there
// is no started bracket match to strand a finisher in — mirroring the bracket
// branch resetting an unfought downstream slot.
func TestReopenKachinukiPoolMatch_KnockoutNotStarted_Allowed(t *testing.T) {
	eng, store, compID := saveMixedKachinukiCompForReopenTest(t)

	scorePoolMatchTx(t, eng, store, compID, "Pool A-0", "A1", "A2", "A1")
	scorePoolMatchTx(t, eng, store, compID, "Pool B-0", "B1", "B2", "B1")

	_, allResolved, err := eng.ResolveQualifiedPools(compID)
	require.NoError(t, err)
	require.True(t, allResolved)
	// The knockout leaf is resolved but left SCHEDULED (never started).

	err = eng.ReopenKachinukiMatch(compID, "Pool A-0", "scored the wrong bout")
	require.NoError(t, err)

	poolA0 := loadPoolMatchByID(t, store, compID, "Pool A-0")
	require.NotNil(t, poolA0)
	assert.Equal(t, state.MatchStatusRunning, poolA0.Status)
	assert.Empty(t, poolA0.Winner)
	assert.Equal(t, "scored the wrong bout", poolA0.CorrectionReason)
}

// TestRemoveTrailingKachinukiBout covers the operator undo for a bout appended
// by mistake (mp-gmcg): a RUNNING kachinuki encounter drops its trailing
// UNSCORED bout, reusing the same strip the End-match write applies so the
// removable set is identical in both places. A scored trailing bout, a
// completed match, and a non-kachinuki competition are all rejected rather than
// silently no-op'd. It targets a regular numbered bout, never a daihyosen.
func TestRemoveTrailingKachinukiBout(t *testing.T) {
	scored := state.SubMatchResult{Position: 1, SideA: "R1", SideB: "W1", Winner: "R1", Decision: "fought"}
	appended := state.SubMatchResult{Position: 2, SideA: "R1", SideB: "W2"} // unscored placeholder

	t.Run("removes a trailing unscored bout from a running pool match", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rm-pool", 5)
		require.NoError(t, store.SavePoolMatches("rm-pool", []state.MatchResult{
			{ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{scored, appended}},
		}))

		updated, err := eng.RemoveTrailingKachinukiBout("rm-pool", "P1-0")
		require.NoError(t, err)
		require.NotNil(t, updated)
		require.Len(t, updated.SubResults, 1, "the trailing unscored bout is dropped")
		assert.Equal(t, 1, updated.SubResults[0].Position)
		assert.Equal(t, state.MatchStatusRunning, updated.Status, "the match stays running")

		// Persisted, not just returned.
		stored := loadPoolMatchByID(t, store, "rm-pool", "P1-0")
		require.NotNil(t, stored)
		require.Len(t, stored.SubResults, 1)
	})

	t.Run("removes a trailing unscored bout from a running bracket match", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rm-bracket", 5)
		require.NoError(t, store.SaveBracket("rm-bracket", &state.Bracket{
			Rounds: [][]state.BracketMatch{{
				{ID: "B1", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
					SubResults: []state.SubMatchResult{scored, appended}},
			}},
		}))

		updated, err := eng.RemoveTrailingKachinukiBout("rm-bracket", "B1")
		require.NoError(t, err)
		require.NotNil(t, updated)
		require.Len(t, updated.SubResults, 1)

		bracket, err := store.LoadBracket("rm-bracket")
		require.NoError(t, err)
		require.Len(t, bracket.Rounds[0][0].SubResults, 1, "the strip is persisted on the bracket match")
	})

	// The bronze (3rd-place) match is a SIBLING of Rounds, not an element of it,
	// so a rounds-only walk never reaches it. Pins the bronze home before the
	// findMatchHome extraction (mp-gmcg review F6).
	t.Run("removes a trailing unscored bout from the bronze match", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rm-bronze", 5)
		require.NoError(t, store.SaveBracket("rm-bronze", &state.Bracket{
			ThirdPlaceMatch: &state.BracketMatch{
				ID: "m-bronze", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{scored, appended},
			},
		}))

		updated, err := eng.RemoveTrailingKachinukiBout("rm-bronze", "m-bronze")
		require.NoError(t, err)
		require.NotNil(t, updated)
		require.Len(t, updated.SubResults, 1)

		bracket, err := store.LoadBracket("rm-bronze")
		require.NoError(t, err)
		require.NotNil(t, bracket.ThirdPlaceMatch)
		require.Len(t, bracket.ThirdPlaceMatch.SubResults, 1, "the strip is persisted on the bronze match")
	})

	t.Run("rejects when the trailing bout is already scored", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rm-scored", 5)
		require.NoError(t, store.SavePoolMatches("rm-scored", []state.MatchResult{
			{ID: "P1-0", Status: state.MatchStatusRunning, SubResults: []state.SubMatchResult{scored}},
		}))

		_, err := eng.RemoveTrailingKachinukiBout("rm-scored", "P1-0")
		require.ErrorIs(t, err, ErrNoRemovableBout)

		stored := loadPoolMatchByID(t, store, "rm-scored", "P1-0")
		require.Len(t, stored.SubResults, 1, "a scored bout is never removed")
	})

	t.Run("rejects a completed match (reopen it first)", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rm-done", 5)
		require.NoError(t, store.SavePoolMatches("rm-done", []state.MatchResult{
			{ID: "P1-0", Status: state.MatchStatusCompleted, SubResults: []state.SubMatchResult{scored, appended}},
		}))

		_, err := eng.RemoveTrailingKachinukiBout("rm-done", "P1-0")
		require.ErrorIs(t, err, ErrRemoveBoutNotRunning)
	})

	t.Run("rejects a non-kachinuki competition", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "rm-fixed", TeamSize: 5, TeamMatchType: state.TeamMatchTypeFixed,
		}))
		require.NoError(t, store.SavePoolMatches("rm-fixed", []state.MatchResult{
			{ID: "P1-0", Status: state.MatchStatusRunning, SubResults: []state.SubMatchResult{scored, appended}},
		}))

		_, err := eng.RemoveTrailingKachinukiBout("rm-fixed", "P1-0")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr)
	})

	t.Run("not found for an unknown match", func(t *testing.T) {
		eng, _, _ := setupKachinukiComp(t, "rm-nf", 5)
		_, err := eng.RemoveTrailingKachinukiBout("rm-nf", "nope")
		var nferr *NotFoundError
		require.ErrorAs(t, err, &nferr)
	})

	// mp-gmcg review F3: the strip must floor at one bout and remove only the
	// single trailing pairing, never the whole trailing run.
	t.Run("refuses to empty a running match whose only bout is unscored", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rm-floor", 5)
		bootstrap := state.SubMatchResult{Position: 1, SideA: "R1", SideB: "W1"} // unscored bout 1
		require.NoError(t, store.SavePoolMatches("rm-floor", []state.MatchResult{
			{ID: "P1-0", Status: state.MatchStatusRunning, SubResults: []state.SubMatchResult{bootstrap}},
		}))

		_, err := eng.RemoveTrailingKachinukiBout("rm-floor", "P1-0")
		require.ErrorIs(t, err, ErrNoRemovableBout)

		stored := loadPoolMatchByID(t, store, "rm-floor", "P1-0")
		require.Len(t, stored.SubResults, 1, "the last remaining bout is never stripped")
	})

	t.Run("removes only the single trailing bout when two are appended", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rm-single", 5)
		appended2 := state.SubMatchResult{Position: 3, SideA: "R1", SideB: "W3"} // second unscored
		require.NoError(t, store.SavePoolMatches("rm-single", []state.MatchResult{
			{ID: "P1-0", Status: state.MatchStatusRunning,
				SubResults: []state.SubMatchResult{scored, appended, appended2}},
		}))

		updated, err := eng.RemoveTrailingKachinukiBout("rm-single", "P1-0")
		require.NoError(t, err)
		require.Len(t, updated.SubResults, 2, "only the last trailing bout is dropped, not both")
		assert.Equal(t, 2, updated.SubResults[1].Position, "the first appended bout survives")

		stored := loadPoolMatchByID(t, store, "rm-single", "P1-0")
		require.Len(t, stored.SubResults, 2)
	})
}
