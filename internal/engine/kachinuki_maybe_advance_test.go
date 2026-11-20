package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
// only. With no lineup saved and no team members, neither side fields anyone
// (operator decision K2), so after bout 1 SideB has nobody left and the pure
// core reports a match end — but team sizes are unregulated and
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

	// Bout 1: R-Senpo beats W-Senpo. Neither side fields anyone: remainingB=[]
	// → the pure core reports a match end, which the caller ignores.
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
		TeamID: redTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-Senpo",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: whiteTeamID, MatchID: "P1-0",
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
			ID:      "P1-0",
			SideA:   "RedTeam",
			SideAID: redTeamID,
			SideB:   "WhiteTeam",
			SideBID: whiteTeamID,
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
// (operator-led completion, mp-gmcg): with no lineup and no team members the
// sides field nobody (K2), so after bout 1 SideB has nobody left, an advisory
// verdict only, and the engine leaves the
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
	// and SideB fields nobody → the pure core reports a match end, which the
	// caller must treat as advisory (no finalize).
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
	saveOwnMatchLineup(t, store, compID, "P1-0", redTeamID, 5, "A-Senpo", "A-Jiho")
	saveOwnMatchLineup(t, store, compID, "P1-0", whiteTeamID, 5, "B-Senpo", "B-Chuken")
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:      "P1-0",
			SideA:   "RedTeam",
			SideAID: redTeamID,
			SideB:   "WhiteTeam",
			SideBID: whiteTeamID,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "A-Senpo", SideB: "B-Senpo", Winner: "B-Senpo", Decision: "fought"},
				{Position: 2, SideA: "A-Jiho", SideB: "B-Chuken", Winner: "A-Jiho", Decision: "fought"},
			},
		},
	}))

	changed, post, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	assert.True(t, changed, "next bout should have been appended")

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	assert.Len(t, matches[0].SubResults, 3, "bout 3 should have been appended")
	assert.Equal(t, "A-Jiho", matches[0].SubResults[2].SideA, "A-Jiho stays as SideA winner")
	assert.Equal(t, "B-Senpo", matches[0].SubResults[2].SideB, "B-Senpo is next SideB")

	// mp-gmcg review E1: the returned post.BoutLog is the FULL post-append bout log,
	// so the handler echoes the appended pairing without re-reading the store.
	// It must match what was persisted.
	require.Len(t, post.BoutLog, 3, "post.BoutLog carries the appended bout")
	assert.Equal(t, matches[0].SubResults, post.BoutLog, "post.BoutLog equals the persisted bout log")
	assert.Equal(t, 3, post.BoutLog[2].Position, "appended bout carries its position")
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
		TeamID: redTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: whiteTeamID, MatchID: "P1-0",
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
			SideAID:  redTeamID,
			SideB:    "WhiteTeam",
			SideBID:  whiteTeamID,
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
		TeamID: redTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PosSenpo: "R-S", domain.PosJiho: "R-J", domain.PosChuken: "R-C",
			domain.PosFukusho: "R-F", domain.PosTaisho: "R-T",
		},
	}, 5))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: whiteTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PosSenpo: "W-S", domain.PosJiho: "W-J", domain.PosChuken: "W-C",
			domain.PosFukusho: "W-F", domain.PosTaisho: "W-T",
		},
	}, 5))

	// Bout 1 scored: R-S beats W-S. The walk below scores each appended
	// bout in turn (R-S keeps winning) and re-runs advancement.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:      "P1-0",
			SideA:   "RedTeam",
			SideAID: redTeamID,
			SideB:   "WhiteTeam",
			SideBID: whiteTeamID,
			Status:  state.MatchStatusRunning,
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
	// After B-Senpo beats A-Senpo, A has nobody left (with no lineup and no
	// team members, neither side fields anyone: K2).
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
	// No lineup and no team members: neither side fields anyone (K2) → remainingB=[].
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
	// Each side fields its own match lineup:
	//   lineupA=[A-Senpo,A-Jiho], retiredA={A-Senpo} → remainingA=[A-Jiho]
	//   lineupB=[B-Senpo,B-Chuken], retiredB={B-Chuken} → remainingB=[B-Senpo]
	saveOwnMatchLineup(t, store, compID, "B-Final", redTeamID, 5, "A-Senpo", "A-Jiho")
	saveOwnMatchLineup(t, store, compID, "B-Final", whiteTeamID, 5, "B-Senpo", "B-Chuken")
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:      "B-Final",
					SideA:   "TeamA",
					SideAID: redTeamID,
					SideB:   "TeamB",
					SideBID: whiteTeamID,
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
	// Same lineups and bouts as BracketAppendsBout above.
	saveOwnMatchLineup(t, store, compID, "m-bronze", redTeamID, 5, "A-Senpo", "A-Jiho")
	saveOwnMatchLineup(t, store, compID, "m-bronze", whiteTeamID, 5, "B-Senpo", "B-Chuken")
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "SF1", SideA: "TeamA", SideB: "TeamB", Winner: "TeamA", Status: state.MatchStatusCompleted}},
		},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:      "m-bronze",
			SideA:   "TeamA",
			SideAID: redTeamID,
			SideB:   "TeamB",
			SideBID: whiteTeamID,
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

// TestFindTeamMatch_Bronze pins that the 3rd-place (bronze) match, a sibling of
// bracket.Rounds rather than an element of it, is found like any bracket match.
func TestFindTeamMatch_Bronze(t *testing.T) {
	compID := "kachinuki-bronze-lookup"
	eng, store, _ := setupKachinukiComp(t, compID, 5, func(c *state.Competition) { c.Naginata = true })

	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{{ID: "SF1", SideA: "TeamA", SideB: "TeamB"}, {ID: "SF2", SideA: "TeamC", SideB: "TeamD"}},
			{{ID: "F1", SideA: "TeamA", SideB: "TeamC"}},
		},
		ThirdPlaceMatch: &state.BracketMatch{ID: "m-bronze", SideA: "TeamB", SideB: "TeamD"},
	}))

	bronze, err := eng.findTeamMatch(compID, "m-bronze")
	require.NoError(t, err)
	require.NotNil(t, bronze, "the 3rd-place match is a sibling of Rounds and is still found")
	assert.True(t, bronze.IsBracket, "bronze is a bracket match")
	assert.Equal(t, "TeamB", bronze.Result.SideA)

	sf1, err := eng.findTeamMatch(compID, "SF1")
	require.NoError(t, err)
	require.NotNil(t, sf1)
	assert.True(t, sf1.IsBracket)
	require.NotNil(t, sf1.Bracket, "a bracket match comes with the bracket it was found in")
	assert.Equal(t, "m-bronze", sf1.Bracket.ThirdPlaceMatch.ID)
}

// TestFindTeamMatch_PoolMatchComesWithThePoolMatchesItWasFoundIn pins that the
// walk hands back what it loaded, so the lineup rule is built from it rather
// than from a second read of the draw: the pool matches, and no bracket, since
// the walk finds a pool match before it reads one.
func TestFindTeamMatch_PoolMatchComesWithThePoolMatchesItWasFoundIn(t *testing.T) {
	compID := "kachinuki-pool-lookup"
	eng, store, _ := setupKachinukiComp(t, compID, 5)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "TeamA", SideAID: "a", SideB: "TeamB", SideBID: "b"},
		{ID: "Pool A-1", SideA: "TeamA", SideAID: "a", SideB: "TeamC", SideBID: "c"},
	}))

	found, err := eng.findTeamMatch(compID, "Pool A-1")

	require.NoError(t, err)
	require.NotNil(t, found)
	assert.False(t, found.IsBracket)
	assert.Equal(t, "TeamC", found.Result.SideB)
	assert.Len(t, found.PoolMatches, 2, "every pool match the walk loaded, not just the one found")
	assert.Nil(t, found.Bracket, "a pool match is found before the bracket is read")

	missing, err := eng.findTeamMatch(compID, "no-such-match")
	require.NoError(t, err)
	assert.Nil(t, missing, "nil is reserved for an id in neither store")
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
		TeamID: redTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: whiteTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "W-1",
			domain.PositionNumbered(2): "W-2",
			domain.PositionNumbered(3): "W-3",
		},
	}, 3))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID: "P1-0", SideA: "RedTeam", SideAID: redTeamID, SideB: "WhiteTeam", SideBID: whiteTeamID, Status: state.MatchStatusRunning,
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
		TeamID: redTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "R-1",
			domain.PositionNumbered(2): "R-2",
			domain.PositionNumbered(3): "R-3",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: whiteTeamID, MatchID: "P1-0",
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
			ID:      "P1-0",
			SideA:   "RedTeam",
			SideAID: redTeamID,
			SideB:   "WhiteTeam",
			SideBID: whiteTeamID,
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

// TestMaybeAdvanceKachinuki_AdvancesOffABoutFoughtEntirelyByNumber pins the
// identity guard's widening from two names to names-OR-member-ids.
//
// Before bc-dnst the guard refused whenever both side NAMES were empty. A
// fresh team's squad slots are numbered and unnamed by design, so two such
// slots can legitimately meet: the row then carries member ids and no names
// at all, and the old condition read that as "no side identity" and stopped
// the encounter dead, mid-sequence, with nothing appended.
//
// Reverting the guard to the two-name form left the ENTIRE repo green, which
// is how this gap was found: no test fought a bout by number alone. The
// assertions are `changed` AND the appended pairing: a refusal and an
// exhausted roster both report changed=false, so `changed` alone distinguishes
// the guard from nothing else -- it is the pairing that proves the roster was
// actually consulted, and that the winner stayed on.
func TestMaybeAdvanceKachinuki_AdvancesOffABoutFoughtEntirelyByNumber(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "advance-by-number"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      3,
	}))
	// Both teams field blank numbered slots: ids present, names absent.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: redTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "",
			domain.PositionNumbered(2): "",
		},
		MemberIDs: map[domain.Position]string{
			domain.PositionNumbered(1): "red-1",
			domain.PositionNumbered(2): "red-2",
		},
	}, 3))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: whiteTeamID, MatchID: "P1-0",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(1): "",
			domain.PositionNumbered(2): "",
		},
		MemberIDs: map[domain.Position]string{
			domain.PositionNumbered(1): "white-1",
			domain.PositionNumbered(2): "white-2",
		},
	}, 3))

	// The only bout: red-1 beats white-1, both fielded by number. Neither
	// side carries a NAME, which is precisely what the old guard refused.
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:      "P1-0",
			SideA:   "RedTeam",
			SideAID: redTeamID,
			SideB:   "WhiteTeam",
			SideBID: whiteTeamID,
			SubResults: []state.SubMatchResult{
				{
					Position:       1,
					SideA:          "",
					SideB:          "",
					SideAMemberID:  "red-1",
					SideBMemberID:  "white-1",
					WinnerMemberID: "red-1",
					Winner:         "RedTeam",
					Decision:       "fought",
				},
			},
		},
	}))

	changed, post, err := eng.MaybeAdvanceKachinuki(compID, "P1-0")
	require.NoError(t, err)
	require.True(t, changed,
		"a bout fought entirely by squad number identifies both sides through their member ids; the encounter must keep advancing")
	require.Len(t, post.BoutLog, 2, "the next pairing is appended after the scored bout")
	assert.Equal(t, 2, post.BoutLog[1].Position, "the appended row is bout 2")
	// WHO is paired matters as much as that a row appeared: red-1 won and must
	// stay on, white-2 is the next unretired White member. Asserting only the
	// position would pass for an append that paired the wrong two fighters,
	// which is the failure winner-stays-on exists to prevent.
	assert.Equal(t, "red-1", post.BoutLog[1].SideAMemberID, "the winner stays on, by id")
	assert.Equal(t, "white-2", post.BoutLog[1].SideBMemberID, "the next White member comes in, by id")
}
