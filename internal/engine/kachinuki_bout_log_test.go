package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
