package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		err := reopenErr(eng, "fixed", "P1-0", "operator error")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr, "non-kachinuki reopen must be a validation error (400)")
	})

	t.Run("pool match reopened: running, outcome cleared, bout log kept", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-pool", 3)
		require.NoError(t, store.SavePoolMatches("reopen-pool", []state.MatchResult{completedPool()}))
		require.NoError(t, reopenErr(eng, "reopen-pool", "P1-0", "  wrong winner recorded  "))

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
		err := reopenErr(eng, "reopen-running", "P1-0", "operator error")
		assert.ErrorIs(t, err, ErrReopenNotCompleted)
	})

	t.Run("match not found", func(t *testing.T) {
		eng, _, _ := setupKachinukiComp(t, "reopen-missing", 3)
		err := reopenErr(eng, "reopen-missing", "nope", "operator error")
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
		require.NoError(t, reopenErr(eng, "reopen-semi", "SF0", "taisho bout must be re-fought"))

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
		err := reopenErr(eng, "reopen-blocked", "SF0", "operator error")
		// bc-cse: F0 is RUNNING, so this is DownstreamKnockoutRunningError
		// now, not the bare ErrReopenDownstreamFought sentinel.
		assert.ErrorIs(t, err, ErrDownstreamKnockoutRunning)

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
		require.NoError(t, reopenErr(eng, "reopen-bronze", "B0", "bronze scored on the wrong sheet"))

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
				require.NoError(t, reopenErr(eng, compID, "P1-0", tc.reason))

				matches, err := store.LoadPoolMatches(compID)
				require.NoError(t, err)
				require.Len(t, matches, 1)
				assert.Equal(t, tc.wantPending, matches[0].ReopenPending)
			})

			t.Run("bracket round", func(t *testing.T) {
				compID := "reopen-pending-bracket"
				eng, store, _ := setupKachinukiComp(t, compID, 3)
				require.NoError(t, store.SaveBracket(compID, completedBracket()))
				require.NoError(t, reopenErr(eng, compID, "F0", tc.reason))

				bracket, err := store.LoadBracket(compID)
				require.NoError(t, err)
				assert.Equal(t, tc.wantPending, bracket.Rounds[0][0].ReopenPending)
			})

			t.Run("bronze", func(t *testing.T) {
				compID := "reopen-pending-bronze"
				eng, store, _ := setupKachinukiComp(t, compID, 3)
				require.NoError(t, store.SaveBracket(compID, completedBracket()))
				require.NoError(t, reopenErr(eng, compID, "B0", tc.reason))

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
// exists, and the handler's applyCorrectionReason would reject its next honest
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
		require.NoError(t, reopenErr(eng, compID, "P1-0", "")) // reason-less → pending
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
		require.NoError(t, reopenErr(eng, compID, "F0", "")) // reason-less → pending
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
// its own IpponsA/IpponsB and IsOverridden) and drifted before.
func TestReopenKachinukiMatch_DiscardsVerdictKeepsBoutLog(t *testing.T) {
	// boutLog[0]'s hantei is recorded as domain.HanteiMark inside the
	// WINNER's (R-1's) ippon slice — the mark IS the record (operator ruling
	// 2026-08-21) — rather than the legacy DecidedByHantei *bool.
	boutLog := []state.SubMatchResult{
		{
			Position: 1, SideA: "R-1", SideB: "W-1",
			IpponsA: []string{"M", domain.HanteiMark}, IpponsB: []string{"K"},
			HansokuB: 1, Winner: "R-1", Decision: "fought",
			Encho: &state.EnchoMetadata{PeriodCount: 2},
		},
		{
			Position: 2, SideA: "R-1", SideB: "W-2",
			IpponsA: []string{}, IpponsB: []string{}, Decision: "hikiwake",
		},
	}

	t.Run("pool", func(t *testing.T) {
		compID := "reopen-verdict-pool"
		// markerSet: the load repair (bc-kheb) would clear the stored Encho
		// before the reopen ran, and the Encho assertion would pin nothing.
		eng, store, _ := setupKachinukiComp(t, compID, 3, markerSet)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam",
			Status: state.MatchStatusCompleted,
			Winner: "RedTeam", WinnerID: "red-uuid",
			IpponsA: []string{"○", "○"}, IpponsB: []string{"M"},
			HansokuA: 1, HansokuB: 2,
			Decision: "kiken-voluntary", DecisionBy: "shiro", DecisionReason: "withdrew",
			Encho:        &state.EnchoMetadata{PeriodCount: 1},
			ResultSource: "admin", RepPlayerA: "R-2", RepPlayerB: "W-2",
			SubResults: boutLog,
		}}))
		require.NoError(t, reopenErr(eng, compID, "P1-0", "ended too early"))

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
		assert.Zero(t, m.HansokuA, "the outstanding-foul count belonged to the discarded result, same as the ippons it discharges into")
		assert.Zero(t, m.HansokuB)
		assert.Empty(t, m.Decision)
		assert.Empty(t, m.DecisionBy)
		assert.Empty(t, m.DecisionReason)
		assert.Nil(t, m.Encho, "match-level encho described the bout the operator ended on")
		assert.Nil(t, m.DecidedByHantei, "legacy read-only field: never raised by a loader")
		assert.Empty(t, m.ResultSource, "provenance of a result that no longer exists")
		assert.Empty(t, m.RepPlayerA)
		assert.Empty(t, m.RepPlayerB)
		assert.Equal(t, "ended too early", m.CorrectionReason, "the reopen's own reason is set, not cleared")
	})

	t.Run("bracket", func(t *testing.T) {
		compID := "reopen-verdict-bracket"
		eng, store, _ := setupKachinukiComp(t, compID, 3, markerSet)
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{{
					ID: "F0", SideA: "RedTeam", SideB: "WhiteTeam",
					Status: state.MatchStatusCompleted, Winner: "RedTeam",
					IpponsA: []string{"○", "○"}, IpponsB: []string{"○"},
					HansokuA: 1, HansokuB: 2,
					Decision: "kiken-voluntary", DecisionBy: "shiro", DecisionReason: "withdrew",
					Encho:        &state.EnchoMetadata{PeriodCount: 1},
					IsOverridden: true, ResultSource: "admin",
					SubResults: boutLog,
				}},
			},
		}))
		require.NoError(t, reopenErr(eng, compID, "F0", "ended too early"))

		bracket, err := store.LoadBracket(compID)
		require.NoError(t, err)
		bm := bracket.Rounds[0][0]

		require.Len(t, bm.SubResults, 2, "reopen must never drop a bout that was fought")
		assert.Equal(t, boutLog[0], bm.SubResults[0])
		assert.Equal(t, boutLog[1], bm.SubResults[1])

		assert.Equal(t, state.MatchStatusRunning, bm.Status)
		assert.Empty(t, bm.Winner)
		assert.Empty(t, bm.IpponsA, "the scoreline described the discarded result")
		assert.Empty(t, bm.IpponsB)
		assert.Zero(t, bm.HansokuA, "reachable via NormalizeLegacy's score-string fold on a pre-migration file, and must not survive a reopen")
		assert.Zero(t, bm.HansokuB)
		assert.Empty(t, bm.Decision)
		assert.Empty(t, bm.DecisionBy)
		assert.Empty(t, bm.DecisionReason)
		assert.Nil(t, bm.Encho)
		assert.False(t, bm.DecidedByHantei, "legacy read-only field: production code never writes it")
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
	t.Run("same competition: busy court rejects, the finished result is untouched", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-court-busy", 3)
		require.NoError(t, store.SavePoolMatches("reopen-court-busy", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
			runningOnCourt("P1-1", "A"),
		}))

		err := reopenErr(eng, "reopen-court-busy", "P1-0", "need more bouts")
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

		require.NoError(t, reopenErr(eng, "reopen-court-free", "P1-0", "need more bouts"))
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

		err := reopenErr(eng, "reopen-bracket-pool-busy", "SF0", "need more bouts")
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

		err := reopenErr(eng, "reopen-pool-bracket-busy", "P1-0", "need more bouts")
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

		err := reopenErr(eng, "reopen-court-busy-bracket", "SF0", "need more bouts")
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

		err := reopenErr(eng, "reopen-court-busy-bronze", "B0", "need more bouts")
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

		err := reopenErr(eng, "reopen-cross-a", "P1-0", "need more bouts")
		var busy *CourtBusyError
		require.ErrorAs(t, err, &busy)
		assert.Equal(t, "reopen-cross-b", busy.CompID, "the conflicting competition must be named")

		matches, lerr := store.LoadPoolMatches("reopen-cross-a")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusCompleted, matches[0].Status, "the reopen must not land")
	})

	// mp-gmcg review: an UNREOPENABLE target (its winner already fed a fought
	// downstream) whose court is held by a running match in ANOTHER competition
	// must report the permanent downstream reason, NOT a transient
	// court_busy. Otherwise the admin remedy panel (which branches on
	// code=="court_busy") offers to requeue the court's occupant for a target that
	// can never reopen — a dead end. The plain-reopen entry pre-checks the result
	// preconditions before CheckCrossCompCourtBusy so the right 409 wins.
	t.Run("an unreopenable target is not masked by a cross-competition court_busy", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "reopen-cross-ds-a", 3)
		require.NoError(t, store.SaveBracket("reopen-cross-ds-a", foughtDownstreamBracket()))
		// Another competition holds SF0's court (A) with a running match.
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: "reopen-cross-ds-b"}))
		require.NoError(t, store.SavePoolMatches("reopen-cross-ds-b", []state.MatchResult{
			runningOnCourt("P9-0", "A"),
		}))

		err := reopenErr(eng, "reopen-cross-ds-a", "SF0", "")
		// bc-cse: foughtDownstreamBracket's F0 is RUNNING, so this is
		// DownstreamKnockoutRunningError now, not the bare sentinel.
		require.ErrorIs(t, err, ErrDownstreamKnockoutRunning, "the permanent reason must win over a transient court_busy")
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
	t.Run("frees the blocker then reopens the target, atomically", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "requeue-reopen", 3)
		require.NoError(t, store.SavePoolMatches("requeue-reopen", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
			runningOnCourt("P1-1", "A"),
		}))

		// A plain reopen is refused: court A is busy with P1-1.
		require.ErrorIs(t, reopenErr(eng, "requeue-reopen", "P1-0", ""), ErrCourtBusy)

		// Requeue the blocker + reopen the target in one atomic operation.
		require.NoError(t, requeueReopenErr(eng, "requeue-reopen", "P1-0", "requeue-reopen", "P1-1", ""))

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

		require.NoError(t, requeueReopenErr(eng, "rq-target", "P1-0", "rq-blocker", "B1-0", ""))

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

		require.NoError(t, requeueReopenErr(eng, "rq-multicourt", "P1-0", "rq-multicourt", "P1-1", ""))

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
		bystander := scoredBlocker("P1-2", "B") // court-B bystander with a score the wipe would clear
		require.NoError(t, store.SavePoolMatches("rq-wrongcourt", []state.MatchResult{
			completedOnCourt("P1-0", "A"), // target on court A
			runningOnCourt("P1-1", "A"),   // the REAL blocker holding court A
			bystander,                     // a bystander on court B
		}))

		// Client wrongly names the court-B bystander as the blocker.
		err := requeueReopenErr(eng, "rq-wrongcourt", "P1-0", "rq-wrongcourt", "P1-2", "")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr, "a blocker on the wrong court is a client error")

		byID := map[string]state.MatchResult{}
		matches, _ := store.LoadPoolMatches("rq-wrongcourt")
		for _, m := range matches {
			byID[m.ID] = m
		}
		assertBlockerIntact(t, store, "rq-wrongcourt", "P1-2") // the wrongly-named bystander must NOT be wiped
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
		require.NoError(t, store.SaveBracket("rq-downstream", foughtDownstreamBracket()))
		// A blocker running on SF0's court (A), in another competition, with a
		// live score the destructive revert would clear.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "rq-ds-blocker", TeamSize: 3, TeamMatchType: state.TeamMatchTypeKachinuki,
		}))
		require.NoError(t, store.SavePoolMatches("rq-ds-blocker", []state.MatchResult{scoredBlocker("B1-0", "A")}))

		err := requeueReopenErr(eng, "rq-downstream", "SF0", "rq-ds-blocker", "B1-0", "")
		// bc-cse: foughtDownstreamBracket's F0 is RUNNING, so this is
		// DownstreamKnockoutRunningError now, not the bare sentinel.
		assert.ErrorIs(t, err, ErrDownstreamKnockoutRunning)

		assertBlockerIntact(t, store, "rq-ds-blocker", "B1-0")

		bracket, lerr := store.LoadBracket("rq-downstream")
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusCompleted, bracket.Rounds[0][0].Status, "target stays completed")
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
		require.NoError(t, store.SavePoolMatches("rq-notdone-blocker", []state.MatchResult{scoredBlocker("B1-0", "A")}))

		err := requeueReopenErr(eng, "rq-notdone", "P1-0", "rq-notdone-blocker", "B1-0", "")
		assert.ErrorIs(t, err, ErrReopenNotCompleted)

		assertBlockerIntact(t, store, "rq-notdone-blocker", "B1-0")
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
		err := requeueReopenErr(eng, "rq-done-blocker", "P1-0", "rq-done-blocker", "P1-1", "")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr)
		assert.Equal(t, state.MatchStatusCompleted, loadPoolMatchByID(t, store, "rq-done-blocker", "P1-0").Status,
			"the target must not reopen when the requeue is rejected")
	})

	t.Run("a non-kachinuki target no withdrawal decided is rejected WITHOUT wiping the blocker", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "rq-fixed", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed,
		}))
		require.NoError(t, store.SavePoolMatches("rq-fixed", []state.MatchResult{
			completedOnCourt("P1-0", "A"),
			scoredBlocker("P1-1", "A"),
		}))
		err := requeueReopenErr(eng, "rq-fixed", "P1-0", "rq-fixed", "P1-1", "")
		var verr *ValidationError
		require.ErrorAs(t, err, &verr)
		assert.Equal(t, state.MatchStatusRunning, loadPoolMatchByID(t, store, "rq-fixed", "P1-1").Status,
			"the gate runs before the destructive requeue, so the blocker keeps its live score")
		assert.Equal(t, state.MatchStatusCompleted, loadPoolMatchByID(t, store, "rq-fixed", "P1-0").Status)
	})

	t.Run("an unknown blocker errors and leaves the target finished", func(t *testing.T) {
		eng, store, _ := setupKachinukiComp(t, "rq-nf-blocker", 3)
		require.NoError(t, store.SavePoolMatches("rq-nf-blocker", []state.MatchResult{completedOnCourt("P1-0", "A")}))
		err := requeueReopenErr(eng, "rq-nf-blocker", "P1-0", "rq-nf-blocker", "nope", "")
		require.Error(t, err)
		assert.Equal(t, state.MatchStatusCompleted, loadPoolMatchByID(t, store, "rq-nf-blocker", "P1-0").Status)
	})
}

// TestReopenKachinukiPoolMatch_DownstreamKnockoutStarted_FinishDecides pins
// the pool reopen's rule: reopening a pool match moves no knockout slot (the
// pool is incomplete while it is open), so it is allowed even while its
// finisher sits in a started knockout match. What the reopen leads to is
// decided when it is FINISHED: the finishing write runs the requalification
// rule against the bracket, so the same result is silent and a changed one is
// refused here, because the knockout match it reaches is being fought.
func TestReopenKachinukiPoolMatch_DownstreamKnockoutStarted_FinishDecides(t *testing.T) {
	eng, store, compID := startedMixedKnockout(t)

	finish := func(winner string) error {
		r := &state.MatchResult{SideA: "A1", SideB: "A2", Winner: winner, IpponsA: []string{"M"}, Status: state.MatchStatusCompleted}
		if winner == "A2" {
			r.IpponsA, r.IpponsB = nil, []string{"M"}
		}
		var werr error
		require.NoError(t, store.WithTransaction(compID, func(tx state.StoreTx) error {
			_, werr = eng.RecordMatchResultWithIneligibilityTx(tx, compID, "Pool A-0", r)
			return nil
		}))
		return werr
	}

	require.NoError(t, reopenErr(eng, compID, "Pool A-0", "scored the wrong bout"))
	assert.Equal(t, state.MatchStatusRunning, loadPoolMatchByID(t, store, compID, "Pool A-0").Status)

	// Same result: nothing in the knockout moves, so nothing is said.
	require.NoError(t, finish("A1"))
	assert.Equal(t, state.MatchStatusCompleted, loadPoolMatchByID(t, store, compID, "Pool A-0").Status)

	// A different result would move A1 out of the knockout match being fought.
	require.NoError(t, reopenErr(eng, compID, "Pool A-0", "scored the wrong bout again"))
	err := finish("A2")
	require.ErrorIs(t, err, ErrDownstreamKnockoutRunning)
	poolA0 := loadPoolMatchByID(t, store, compID, "Pool A-0")
	assert.Equal(t, state.MatchStatusRunning, poolA0.Status, "the refused finish must leave the reopened match as it was")
	assert.Empty(t, poolA0.Winner)
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

	err = reopenErr(eng, compID, "Pool A-0", "scored the wrong bout")
	require.NoError(t, err)

	poolA0 := loadPoolMatchByID(t, store, compID, "Pool A-0")
	require.NotNil(t, poolA0)
	assert.Equal(t, state.MatchStatusRunning, poolA0.Status)
	assert.Empty(t, poolA0.Winner)
	assert.Equal(t, "scored the wrong bout", poolA0.CorrectionReason)
}
