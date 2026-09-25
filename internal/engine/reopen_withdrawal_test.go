package engine

// Operator ruling 2026-09-24: "Everything should be able to be fixed, in case
// of a wrong entry. The operator just needs to be aware of the consequences,
// if it affects downstream matches." A withdrawal (kiken, fusenpai) recorded
// by mistake is removed by REOPENING the match, whatever its format: a
// withdrawal means the opponent received the default score, so removing one
// means the match was never decided. It goes back to running with what was
// fought kept, the verdict cleared and the withdrawn side eligible again, and
// the operator finishes it normally. These pin that on the team (pool and
// knockout) and individual shapes, the downstream warn-and-proceed, and the
// gate that still refuses every other non-kachinuki match. The kachinuki
// reopen's own withdrawal tests are in kachinuki_reopen_withdrawal_test.go.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A fixed-order team pool match: running, bout kept, verdict and maru gone,
// eligible again, and the reason is the audit trail (nothing outstanding).
func TestReopenWithdrawal_TeamPoolMatch(t *testing.T) {
	for _, decision := range []string{"kiken-voluntary", "kiken-injury", "fusenpai"} {
		t.Run(decision, func(t *testing.T) {
			eng, store, compID, _ := seedPoolWithdrawal(t, decision)

			status, err := eng.ReopenMatch(compID, "Pool A-0", "  Withdrawal recorded by mistake  ")
			require.NoError(t, err)
			require.NotNil(t, status, "the restore is returned so the handler can broadcast it")
			assert.Equal(t, wrTeamAID, status.PlayerID)
			assert.True(t, status.Eligible)
			assert.True(t, wrEligible(t, store, compID, wrTeamAID), "a removed withdrawal bars nobody")

			m := wrPoolMatch(t, store, compID)
			assert.Equal(t, state.MatchStatusRunning, m.Status)
			assert.Equal(t, "", m.Decision)
			assert.Equal(t, "", m.DecisionBy)
			assert.Equal(t, "", m.DecisionReason)
			assert.Equal(t, "", m.Winner)
			assert.Equal(t, "", m.WinnerID)
			assert.Empty(t, m.IpponsB, "the default-win maru goes with the verdict")
			// bc-tmfn follow-up: the kiken padded bouts 2 and 3 (TeamSize 3);
			// reopening clears the verdict but does not strip those rows --
			// the same empty-position shape a fresh team match's Start write
			// already stores.
			require.Len(t, m.SubResults, 3, "the bout fought before the withdrawal is kept, plus the padded rows 2/3")
			assert.Equal(t, []string{"M"}, m.SubResults[0].IpponsA)
			assert.Equal(t, "Withdrawal recorded by mistake", m.CorrectionReason)
			assert.False(t, m.ReopenPending, "a reason was given, so nothing is owed")
		})
	}
}

// A reason-less reopen owes its reason on the next completion, as the
// kachinuki reopen always has.
func TestReopenWithdrawal_NoReasonLeavesItPending(t *testing.T) {
	eng, store, compID, _ := seedPoolWithdrawal(t, "fusenpai")
	_, err := eng.ReopenMatch(compID, "Pool A-0", "")
	require.NoError(t, err)
	assert.True(t, wrPoolMatch(t, store, compID).ReopenPending)
}

// seedIndividualWithdrawal: an individual league where Ryu (aka) had struck a
// men and Tora (shiro) a kote when Ryu withdrew, in overtime when encho is set.
func seedIndividualWithdrawal(t *testing.T, decision string, encho *state.EnchoMetadata) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	const compID = "rw-ind"
	createTestCompetition(t, store, compID, "league", 3)
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		IpponsA: []string{"M"}, IpponsB: []string{"K"}, HansokuB: 1,
		Status: state.MatchStatusRunning,
	}}))
	_, _, err := eng.RecordDecision(compID, "Pool A-0", decision, "aka", "", encho, false)
	require.NoError(t, err)
	require.False(t, wrEligible(t, store, compID, wrTeamAID), "precondition: the withdrawal barred Ryu")
	return eng, store, compID
}

// An individual match is a single bout: its match-level scoreline IS the
// fight, so the letters the withdrawing side struck (kept by the withdrawal,
// FIK Art. 32) stay, the winner's maru goes, and overtime stays.
func TestReopenWithdrawal_IndividualMatchKeepsWhatWasFought(t *testing.T) {
	encho := &state.EnchoMetadata{PeriodCount: 1}
	eng, store, compID := seedIndividualWithdrawal(t, "kiken-injury", encho)
	before := wrPoolMatch(t, store, compID)
	require.Equal(t, []string{"M"}, before.IpponsA)
	require.Equal(t, domain.DefaultWinIppons(true), before.IpponsB)

	status, err := eng.ReopenMatch(compID, "Pool A-0", "Withdrawal recorded by mistake")
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, wrTeamAID, status.PlayerID)
	assert.True(t, wrEligible(t, store, compID, wrTeamAID))

	m := wrPoolMatch(t, store, compID)
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, "", m.Decision)
	assert.Equal(t, "", m.Winner)
	assert.Equal(t, []string{"M"}, m.IpponsA, "the withdrawing side's struck letter is kept")
	assert.Empty(t, m.IpponsB, "the winner's maru was the verdict; its letters before the withdrawal were replaced when it was recorded")
	require.NotNil(t, m.Encho, "the bout was in overtime and still is")
	assert.Equal(t, 1, m.Encho.PeriodCount)
}

// The knockout shape, with the downstream warn-and-proceed: a final that has
// its own result is the operator's call. Refused (naming it) without force,
// reopened for re-entry with force; either way the retraction lands only
// once the operator has been asked.
func TestReopenWithdrawal_KnockoutDownstreamIsWarnAndProceed(t *testing.T) {
	eng, store, compID, _, matchID := seedBracketWithdrawal(t, false)
	before, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Equal(t, state.MatchStatusCompleted, before.Rounds[1][0].Status, "precondition: Tora won the final")

	_, err = eng.ReopenMatch(compID, matchID, "Withdrawal recorded by mistake")
	require.ErrorIs(t, err, ErrDownstreamKnockoutPlayed)
	var dkp *DownstreamKnockoutPlayedError
	require.ErrorAs(t, err, &dkp)
	assert.Equal(t, "m-r2-0", dkp.BlockingMatchID)
	assert.Equal(t, wrTeamB, dkp.Displaced)
	unchanged, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, before, unchanged, "a refused reopen leaves the bracket untouched")
	assert.False(t, wrEligible(t, store, compID, wrTeamAID), "and the withdrawal in force")

	var reopened []ReopenedMatch
	status, err := eng.ReopenMatch(compID, matchID, "Withdrawal recorded by mistake", ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.True(t, wrEligible(t, store, compID, wrTeamAID))
	require.Len(t, reopened, 1)
	assert.Equal(t, "m-r2-0", reopened[0].ID)

	after, err := store.LoadBracket(compID)
	require.NoError(t, err)
	r1 := after.Rounds[0][0]
	assert.Equal(t, state.MatchStatusRunning, r1.Status)
	assert.Equal(t, "", r1.Decision)
	// bc-tmfn follow-up: the kiken padded bouts 2 and 3 (TeamSize 3); reopening
	// clears the verdict but does not strip those rows.
	require.Len(t, r1.SubResults, 3, "the bout fought before the withdrawal is kept, plus the padded rows 2/3")
	final := after.Rounds[1][0]
	assert.Equal(t, state.MatchStatusScheduled, final.Status, "the final is reopened for re-entry")
	assert.Empty(t, final.Winner)
	assert.Empty(t, final.SubResults, "its bouts belonged to a pairing no longer in it")
	assert.Equal(t, "Winner of r2-m0", final.SideA, "the retracted slot waits for this match again")
	assert.Equal(t, wrTeamC, final.SideB)
}

// A downstream match in progress is not something a confirmation can settle:
// someone is fighting it. Refused outright, force or not.
func TestReopenWithdrawal_RunningDownstreamIsRefused(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "rw-ko-running"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "rw", Kind: "individual", Status: state.CompStatusKnockout}))
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID, Status: state.MatchStatusRunning}},
		{{ID: "m-r2-0", SideB: wrTeamC, SideBID: wrTeamCID}},
	}}))
	_, _, err := eng.RecordDecision(compID, "m-r1-0", "fusenpai", "shiro", "", nil, false)
	require.NoError(t, err)
	require.NoError(t, store.UpdateBracket(compID, func(b *state.Bracket) error {
		b.Rounds[1][0].Status = state.MatchStatusRunning
		b.Rounds[1][0].IpponsA = []string{"M"}
		return nil
	}))

	for _, force := range []bool{false, true} {
		_, err = eng.ReopenMatch(compID, "m-r1-0", "Withdrawal recorded by mistake", ForceOptions{Force: force})
		// bc-cse: m-r2-0 is RUNNING, so this is DownstreamKnockoutRunningError
		// now, not the bare ErrReopenDownstreamFought sentinel.
		require.ErrorIs(t, err, ErrDownstreamKnockoutRunning, "force=%v", force)
	}
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[0][0].Status)
	assert.Equal(t, state.MatchStatusRunning, b.Rounds[1][0].Status)
}

// The gate is unchanged for every other match: a completed non-kachinuki
// match decided by neither a withdrawal nor a default win is corrected, not
// reopened. fusensho is deliberately NOT in this list since bc-cse: see
// TestReopenWithdrawal_FusenshoAcceptedRestoresNobody for its own (accepted)
// case.
func TestReopenWithdrawal_OtherMatchesAreStillRefused(t *testing.T) {
	for _, decision := range []string{"", "fought", "hikiwake", "daihyosen"} {
		t.Run("decision "+decision, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			const compID = "rw-refused"
			createTestCompetition(t, store, compID, "league", 3)
			wrSaveTeams(t, store, compID)
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
				ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
				Status: state.MatchStatusCompleted, Winner: wrTeamA, WinnerID: wrTeamAID,
				IpponsA: []string{"M"}, Decision: decision,
			}}))
			_, err := eng.ReopenMatch(compID, "Pool A-0", "reason")
			var verr *ValidationError
			require.ErrorAs(t, err, &verr)
			assert.Equal(t, state.MatchStatusCompleted, wrPoolMatch(t, store, compID).Status)
		})
	}
	t.Run("a withdrawal-decided match that is not completed", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		const compID = "rw-running"
		createTestCompetition(t, store, compID, "league", 3)
		wrSaveTeams(t, store, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: wrTeamA, SideB: wrTeamB, Status: state.MatchStatusRunning,
		}}))
		_, err := eng.ReopenMatch(compID, "Pool A-0", "reason")
		require.ErrorIs(t, err, ErrReopenNotCompleted)
	})
}

// bc-cse: a match-level fusensho (default win) is now reopenable, exactly
// like a withdrawal -- but fusensho never recorded a CompetitorStatus for
// anyone (domain.IsWithdrawalDecisionStr excludes it, recordIneligibilityFromDecision
// only fires for a withdrawal), so reopening one restores nobody's
// eligibility: there was nothing to restore.
func TestReopenWithdrawal_FusenshoAcceptedRestoresNobody(t *testing.T) {
	eng, store, compID, _ := seedPoolWithdrawal(t, "fusensho")

	status, err := eng.ReopenMatch(compID, "Pool A-0", "Default win recorded by mistake")
	require.NoError(t, err, "fusensho must be reopenable (bc-cse)")
	assert.Nil(t, status, "fusensho barred nobody, so nothing is restored")

	m := wrPoolMatch(t, store, compID)
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, "", m.Decision)
	assert.Equal(t, "", m.DecisionBy)
	assert.Equal(t, "", m.Winner)
	assert.Empty(t, m.IpponsB, "the default-win maru goes with the verdict")
	assert.Equal(t, "Default win recorded by mistake", m.CorrectionReason)
}

// The warn-and-proceed is the reopen's one rule, so a kachinuki reopen whose
// final has its own result is asked about too, and proceeds on force.
func TestReopenKachinuki_ClosedDownstreamIsWarnAndProceed(t *testing.T) {
	eng, store, _ := setupKachinukiComp(t, "rw-kachi-ko", 3)
	require.NoError(t, store.SaveBracket("rw-kachi-ko", &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{
				ID: "SF0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted,
				Winner: "RedTeam", Decision: "kachinuki-exhaustion",
				SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"}},
			},
			{ID: "SF1", SideA: "Kuma", SideB: "Washi", Status: state.MatchStatusCompleted, Winner: "Kuma", IpponsA: []string{"M"}},
		},
		{
			{
				ID: "F0", SideA: "RedTeam", SideB: "Kuma", Status: state.MatchStatusCompleted, Winner: "Kuma",
				Decision:   "kachinuki-exhaustion",
				SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "K-1", IpponsB: []string{"M"}, Winner: "K-1", Decision: "fought"}},
			},
		},
	}}))
	_, err := eng.ReopenMatch("rw-kachi-ko", "SF0", "")
	require.ErrorIs(t, err, ErrDownstreamKnockoutPlayed)

	var reopened []ReopenedMatch
	_, err = eng.ReopenMatch("rw-kachi-ko", "SF0", "", ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	require.Len(t, reopened, 1)
	b, err := store.LoadBracket("rw-kachi-ko")
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusRunning, b.Rounds[0][0].Status)
	assert.Equal(t, state.MatchStatusScheduled, b.Rounds[1][0].Status)
	assert.Equal(t, "Winner of r2-m0", b.Rounds[1][0].SideA)
}

// The confirmed reopen takes the reopened downstream match's own winner back
// out of a round beyond it that nobody has touched, as a forced correction
// does: the final no longer names RedTeam once the semifinal RedTeam won is
// reopened.
func TestReopenKachinuki_ForcedReopenRetractsFromAnUntouchedRound(t *testing.T) {
	const compID = "rw-kachi-3r"
	eng, store, _ := setupKachinukiComp(t, compID, 3)
	bout := func(a, b string) []state.SubMatchResult {
		return []state.SubMatchResult{{Position: 1, SideA: a, SideB: b, IpponsA: []string{"M"}, Winner: a, Decision: "fought"}}
	}
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "QF0", SideA: "RedTeam", SideB: "WhiteTeam", Status: state.MatchStatusCompleted, Winner: "RedTeam",
			Decision: "kachinuki-exhaustion", SubResults: bout("R-1", "W-1")}},
		{{ID: "SF0", SideA: "RedTeam", SideB: "Kuma", Status: state.MatchStatusCompleted, Winner: "RedTeam",
			Decision: "kachinuki-exhaustion", SubResults: bout("R-1", "K-1")}},
		{{ID: "F0", SideA: "RedTeam", SideB: "Washi", Status: state.MatchStatusScheduled}},
	}}))

	var reopened []ReopenedMatch
	_, err := eng.ReopenMatch(compID, "QF0", "", ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	require.Equal(t, []string{"SF0"}, reopenedIDs(reopened))

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusRunning, b.Rounds[0][0].Status)
	assert.Equal(t, state.MatchStatusScheduled, b.Rounds[1][0].Status)
	final := b.Rounds[2][0]
	assert.Equal(t, winnerOfPlaceholder(len(b.Rounds)-1, 0), final.SideA,
		"the untouched final waits for the reopened semifinal instead of naming RedTeam")
	assert.Equal(t, "Washi", final.SideB)
}

// A team competition's -DH- rep bout is a single bout: the match carries no
// bout rows and its match-level scoreline IS the bout. Reopening a withdrawal
// that decided it keeps who fought it (RepPlayerA/B) with the letters struck,
// the same as the fight itself: a reopen that cleared them lost the fighters'
// names while keeping their points.
func TestReopenWithdrawal_TeamRepBoutKeepsWhoFought(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "rw-rep"
	createTestCompetition(t, store, compID, "league", 3, func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = 3
	})
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-DH-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		RepPlayerA: "r2", RepPlayerB: "t2", IpponsA: []string{"M"},
		Status: state.MatchStatusRunning,
	}}))
	_, _, err := eng.RecordDecision(compID, "Pool A-DH-0", "kiken-voluntary", "aka", "knee", nil, false)
	require.NoError(t, err)
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Equal(t, "kiken-voluntary", ms[0].Decision)
	require.Equal(t, "r2", ms[0].RepPlayerA, "precondition: the decision kept who fought")

	_, err = eng.ReopenMatch(compID, "Pool A-DH-0", "Withdrawal recorded by mistake")
	require.NoError(t, err)
	ms, err = store.LoadPoolMatches(compID)
	require.NoError(t, err)
	m := ms[0]
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	assert.Equal(t, []string{"M"}, m.IpponsA, "the struck letter is kept")
	assert.Equal(t, "r2", m.RepPlayerA, "who fought for Ryu is kept with the letter they struck")
	assert.Equal(t, "t2", m.RepPlayerB, "who fought for Tora is kept")
}

// seedKnockoutFinalWithdrawal is an individual knockout: round 1 Ryu v Tora,
// the final Tora (or whoever round 1 sends through) v Kuma. Round 1 is
// settled by first(eng), then Kuma withdraws from the final through the real
// decision path, which bars Kuma.
func seedKnockoutFinalWithdrawal(t *testing.T, first func(eng *Engine, compID string)) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	const compID = "rw-final"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "rw", Kind: "individual", Status: state.CompStatusKnockout}))
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID, Status: state.MatchStatusRunning, MatchNumber: 1}},
		{{ID: "m-r2-0", SideB: wrTeamC, SideBID: wrTeamCID, MatchNumber: 2}},
	}}))
	first(eng, compID)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.Equal(t, wrTeamB, b.Rounds[1][0].SideA, "precondition: Tora went through to the final")
	_, _, err = eng.RecordDecision(compID, "m-r2-0", "kiken-voluntary", "shiro", "knee", nil, false)
	require.NoError(t, err)
	require.False(t, wrEligible(t, store, compID, wrTeamCID), "precondition: the final's withdrawal barred Kuma")
	return eng, store, compID
}

// A downstream match a confirmed reopen or correction reopens loses its
// verdict, and when that verdict was a withdrawal the competitor it barred
// is eligible again (restoreForceReopened), exactly as on the match the
// operator acted on. Before, the reopened final kept Kuma barred, so it could
// never be started again. Pinned on every door that reopens downstream: the
// reopen itself, a forced score correction and a forced winner override.
func TestForceReopenedDownstreamWithdrawalRestoresEligibility(t *testing.T) {
	fought := func(eng *Engine, compID string) {
		_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", &state.MatchResult{
			ID: "m-r1-0", SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamB,
			IpponsB: []string{"M", "K"}, Status: state.MatchStatusCompleted,
		})
		require.NoError(t, err)
	}
	cases := []struct {
		name   string
		first  func(eng *Engine, compID string)
		reopen func(t *testing.T, eng *Engine, compID string) []ReopenedMatch
	}{
		{
			name: "clear withdrawal and reopen",
			first: func(eng *Engine, compID string) {
				_, _, err := eng.RecordDecision(compID, "m-r1-0", "fusenpai", "aka", "", nil, false)
				require.NoError(t, err)
			},
			reopen: func(t *testing.T, eng *Engine, compID string) []ReopenedMatch {
				var reopened []ReopenedMatch
				_, err := eng.ReopenMatch(compID, "m-r1-0", "Withdrawal recorded by mistake", ForceOptions{Force: true, Reopened: &reopened})
				require.NoError(t, err)
				return reopened
			},
		},
		{
			name:  "forced score correction",
			first: fought,
			reopen: func(t *testing.T, eng *Engine, compID string) []ReopenedMatch {
				var reopened []ReopenedMatch
				_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", &state.MatchResult{
					ID: "m-r1-0", SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamA,
					IpponsA: []string{"M", "K"}, IpponsB: []string{}, Status: state.MatchStatusCompleted,
					CorrectionReason: "Scoring error: wrong side",
				}, ForceOptions{Force: true, Reopened: &reopened})
				require.NoError(t, err)
				return reopened
			},
		},
		{
			name:  "forced winner override",
			first: fought,
			reopen: func(t *testing.T, eng *Engine, compID string) []ReopenedMatch {
				var reopened []ReopenedMatch
				applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", wrTeamA, 0, ForceOptions{Force: true, Reopened: &reopened})
				require.NoError(t, err)
				require.True(t, applied)
				return reopened
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, compID := seedKnockoutFinalWithdrawal(t, tc.first)
			reopened := tc.reopen(t, eng, compID)

			require.Len(t, reopened, 1)
			assert.Equal(t, "m-r2-0", reopened[0].ID)
			assert.True(t, wrEligible(t, store, compID, wrTeamCID), "the final's withdrawal is gone, so Kuma can compete again")
			require.NotNil(t, reopened[0].Restored, "the restore is reported for the handler to broadcast")
			assert.Equal(t, wrTeamCID, reopened[0].Restored.PlayerID)
			b, err := store.LoadBracket(compID)
			require.NoError(t, err)
			assert.Equal(t, state.MatchStatusScheduled, b.Rounds[1][0].Status)
			assert.Empty(t, b.Rounds[1][0].Decision)
		})
	}
}

// A downstream match that no withdrawal decided has nothing to restore.
func TestForceReopenedDownstreamWithoutWithdrawalRestoresNothing(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "rw-final-fought"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "rw", Kind: "individual", Status: state.CompStatusKnockout}))
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID, Status: state.MatchStatusRunning, MatchNumber: 1}},
		{{ID: "m-r2-0", SideB: wrTeamC, SideBID: wrTeamCID, MatchNumber: 2}},
	}}))
	_, _, err := eng.RecordDecision(compID, "m-r1-0", "fusenpai", "aka", "", nil, false)
	require.NoError(t, err)
	_, err = eng.RecordMatchResultWithIneligibility(compID, "m-r2-0", &state.MatchResult{
		ID: "m-r2-0", SideA: wrTeamB, SideB: wrTeamC, Winner: wrTeamB,
		IpponsA: []string{"M", "K"}, Status: state.MatchStatusCompleted,
	})
	require.NoError(t, err)
	var reopened []ReopenedMatch
	_, err = eng.ReopenMatch(compID, "m-r1-0", "Withdrawal recorded by mistake", ForceOptions{Force: true, Reopened: &reopened})
	require.NoError(t, err)
	require.Len(t, reopened, 1)
	assert.Nil(t, reopened[0].Restored)
}
