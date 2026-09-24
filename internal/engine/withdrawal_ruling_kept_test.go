package engine

// bc-tmfn operator ruling 2026-09-24: "Save correction should just save what
// the operator enters." A match ended by a match-level withdrawal (kiken,
// fusenpai) is corrected through a score sheet, which says nothing about the
// withdrawal: the team sheet sends the bouts, the individual sheet the
// scoreline. What the operator entered must be saved and the recorded ruling,
// with every consequence it had, must stay exactly as recorded.
// preserveWithdrawalRuling is the owner; these pin it on both branches and
// pin the writes it must leave alone.

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	wrTeamA   = "Ryu"
	wrTeamB   = "Tora"
	wrTeamC   = "Kuma"
	wrTeamAID = "11111111-1111-4111-8111-111111111111"
	wrTeamBID = "22222222-2222-4222-8222-222222222222"
	wrTeamCID = "33333333-3333-4333-8333-333333333333"
)

// wrBout1 is bout 1, which Ryu's fighter won by `waza`.
func wrBout1(waza string) state.SubMatchResult {
	return state.SubMatchResult{Position: 1, SideA: "r1", SideB: "t1", Winner: "r1", IpponsA: []string{waza}}
}

// wrCorrection is what the team editor sends on Save correction: the sheet's
// bouts, match-level ippons [] and a winner DERIVED FROM THE BOUTS (Ryu, the
// team that withdrew), no decision.
func wrCorrection(id string) *state.MatchResult {
	return &state.MatchResult{
		ID: id, SideA: wrTeamA, SideB: wrTeamB,
		Winner: wrTeamA, IpponsA: []string{}, IpponsB: []string{},
		Status: state.MatchStatusCompleted, CorrectionReason: "Scoring error: wrong waza entered",
		SubResults: []state.SubMatchResult{wrBout1("K")},
	}
}

func wrSaveTeams(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: wrTeamAID, Name: wrTeamA, Dojo: "DojoR"},
		{ID: wrTeamBID, Name: wrTeamB, Dojo: "DojoT"},
		{ID: wrTeamCID, Name: wrTeamC, Dojo: "DojoK"},
	}))
}

// seedPoolWithdrawal builds a team league with Ryu v Tora running, bout 1
// fought, then Ryu (aka) withdraws through the real decision path.
func seedPoolWithdrawal(t *testing.T, decision string) (*Engine, *state.Store, string, string) {
	t.Helper()
	eng, store, dir := setupTestEngine(t)
	const compID = "wr-pool"
	createTestCompetition(t, store, compID, "league", 3, func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = 3
	})
	wrSaveTeams(t, store, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusRunning, SubResults: []state.SubMatchResult{wrBout1("M")},
	}}))
	_, _, err := eng.RecordDecision(compID, "Pool A-0", decision, "aka", "knee", nil, false)
	require.NoError(t, err)
	return eng, store, compID, dir
}

func wrPoolMatch(t *testing.T, store *state.Store, compID string) state.MatchResult {
	t.Helper()
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == "Pool A-0" {
			return m
		}
	}
	t.Fatalf("Pool A-0 not found")
	return state.MatchResult{}
}

func readStatusFile(t *testing.T, dir, compID string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "competitions", compID, "competitor-status.yaml"))
	require.NoError(t, err)
	return b
}

// assertRulingKept compares every field of the inherited unit.
func assertRulingKept(t *testing.T, before, after withdrawalRuling) {
	t.Helper()
	assert.Equal(t, before.Decision, after.Decision, "Decision")
	assert.Equal(t, before.DecisionBy, after.DecisionBy, "DecisionBy")
	assert.Equal(t, before.DecisionReason, after.DecisionReason, "DecisionReason")
	assert.Equal(t, before.Winner, after.Winner, "Winner")
	assert.Equal(t, before.WinnerID, after.WinnerID, "WinnerID")
	assert.Equal(t, before.IpponsA, after.IpponsA, "IpponsA")
	assert.Equal(t, before.IpponsB, after.IpponsB, "IpponsB")
	assert.Equal(t, before.Encho, after.Encho, "Encho")
}

func TestWithdrawalRulingKept_PoolCorrection(t *testing.T) {
	for _, decision := range []string{"kiken-voluntary", "kiken-injury", "fusenpai"} {
		t.Run(decision, func(t *testing.T) {
			eng, store, compID, dir := seedPoolWithdrawal(t, decision)
			before := wrPoolMatch(t, store, compID)
			require.Equal(t, wrTeamB, before.Winner, "Ryu withdrew, so Tora won")
			statusBefore := readStatusFile(t, dir, compID)

			status, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", wrCorrection("Pool A-0"))
			require.NoError(t, err)
			assert.Nil(t, status, "no status change to broadcast: nobody changed the ruling")

			after := wrPoolMatch(t, store, compID)
			assertRulingKept(t, rulingOfMatch(&before), rulingOfMatch(&after))
			assert.Equal(t, domain.DefaultWinIppons(false), after.IpponsB, "the winner's maru survives")
			require.Len(t, after.SubResults, 1)
			assert.Equal(t, []string{"K"}, after.SubResults[0].IpponsA, "the bout edit is saved")
			assert.Equal(t, "Scoring error: wrong waza entered", after.CorrectionReason)
			assert.Equal(t, string(statusBefore), string(readStatusFile(t, dir, compID)),
				"the eligibility record is byte-for-byte unchanged, RecordedAt included")
		})
	}
}

// RecordMatchResult (writeMatchResult) is the other door with an eligibility
// side effect; it skips it on the same report.
func TestWithdrawalRulingKept_RecordMatchResultDoor(t *testing.T) {
	eng, store, compID, dir := seedPoolWithdrawal(t, "kiken-voluntary")
	before := wrPoolMatch(t, store, compID)
	statusBefore := readStatusFile(t, dir, compID)

	require.NoError(t, eng.RecordMatchResult(compID, "Pool A-0", wrCorrection("Pool A-0")))

	after := wrPoolMatch(t, store, compID)
	assertRulingKept(t, rulingOfMatch(&before), rulingOfMatch(&after))
	assert.Equal(t, string(statusBefore), string(readStatusFile(t, dir, compID)))
}

// A kiken-injury competitor reinstated by a doctor stays reinstated when the
// bouts of the match they withdrew from are corrected.
func TestWithdrawalRulingKept_ReinstatementSurvivesCorrection(t *testing.T) {
	eng, store, compID, _ := seedPoolWithdrawal(t, "kiken-injury")
	_, err := eng.ReinstateCompetitor(compID, wrTeamAID)
	require.NoError(t, err)

	_, err = eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", wrCorrection("Pool A-0"))
	require.NoError(t, err)

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.True(t, statuses[wrTeamAID].Eligible, "the correction must not undo the reinstatement")
}

// The individual editor's write carries no bout rows: it states the match WAS
// fought, so it replaces the ruling exactly as before. A team competition's
// -TB- rep bout is a single bout and follows the same rule.
func TestWithdrawalRulingKept_SingleBoutCorrectionKeepsTheRuling(t *testing.T) {
	for _, tc := range []struct {
		name, matchID string
		team          bool
		decision      string
	}{
		{name: "individual match", matchID: "Pool A-0"},
		{name: "individual match, draw toggle on", matchID: "Pool A-0", decision: "hikiwake"},
		{name: "tiebreaker rep bout in a team competition", matchID: "Pool A-TB-1", team: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, dir := setupTestEngine(t)
			const compID = "wr-ind"
			createTestCompetition(t, store, compID, "league", 3, func(c *state.Competition) {
				if tc.team {
					c.Kind = "team"
					c.TeamSize = 3
				}
			})
			wrSaveTeams(t, store, compID)
			// Ryu (aka) had struck a men and Tora a kote when Ryu withdrew.
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
				ID: tc.matchID, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
				IpponsA: []string{"M"}, IpponsB: []string{"K"},
				Status: state.MatchStatusRunning,
			}}))
			_, _, err := eng.RecordDecision(compID, tc.matchID, "kiken-voluntary", "aka", "knee", nil, false)
			require.NoError(t, err)
			ms, err := store.LoadPoolMatches(compID)
			require.NoError(t, err)
			before := ms[0]
			require.Equal(t, []string{"M"}, before.IpponsA, "precondition: the withdrawing side keeps what it struck (FIK Art. 32)")
			require.Equal(t, domain.DefaultWinIppons(false), before.IpponsB, "precondition: the winner holds the maru")
			statusBefore := readStatusFile(t, dir, compID)

			// The operator fixes Ryu's letter (it was a do, not a men). The sheet
			// echoes the maru back on Tora's side and says nothing about the
			// withdrawal: decision "" (or "hikiwake" with the draw toggle).
			status, err := eng.RecordMatchResultWithIneligibility(compID, tc.matchID, &state.MatchResult{
				ID: tc.matchID, SideA: wrTeamA, SideB: wrTeamB, Winner: wrTeamB, Decision: tc.decision,
				IpponsA: []string{"D"}, IpponsB: []string{"○", "○"},
				Status: state.MatchStatusCompleted, CorrectionReason: "Scoring error: it was a do",
			})
			require.NoError(t, err)
			assert.Nil(t, status, "no status change to broadcast: nobody changed the ruling")

			ms, err = store.LoadPoolMatches(compID)
			require.NoError(t, err)
			after := ms[0]
			assert.Equal(t, "kiken-voluntary", after.Decision, "the withdrawal is kept")
			assert.Equal(t, "aka", after.DecisionBy)
			assert.Equal(t, wrTeamB, after.Winner)
			assert.Equal(t, wrTeamBID, after.WinnerID)
			assert.Equal(t, []string{"D"}, after.IpponsA, "the withdrawing side's letters are what the operator entered")
			assert.Equal(t, domain.DefaultWinIppons(false), after.IpponsB, "the winner's maru is the ruling and stays")
			assert.Equal(t, string(statusBefore), string(readStatusFile(t, dir, compID)),
				"the eligibility record is byte-for-byte unchanged")
		})
	}
}

// seedBracketWithdrawal builds a team knockout: Ryu v Tora in round 1 (bout 1
// fought, then Ryu withdraws), feeding Tora into the final against Kuma,
// which Tora has since WON. The correction's bout-derived winner (Ryu) differs
// from the recorded one, so a correction that failed to keep the ruling would
// meet the downstream guard.
func seedBracketWithdrawal(t *testing.T, bronze bool) (*Engine, *state.Store, string, string, string) {
	t.Helper()
	eng, store, dir := setupTestEngine(t)
	const compID = "wr-ko"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "wr", Kind: "team", TeamSize: 3, Status: state.CompStatusKnockout,
	}))
	wrSaveTeams(t, store, compID)
	matchID := "m-r1-0"
	if bronze {
		matchID = "m-bronze"
	}
	r1 := state.BracketMatch{ID: matchID, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusRunning, SubResults: []state.SubMatchResult{wrBout1("M")}}
	b := &state.Bracket{Rounds: [][]state.BracketMatch{{r1}, {{ID: "m-r2-0", SideB: wrTeamC, SideBID: wrTeamCID}}}}
	if bronze {
		b = &state.Bracket{
			Rounds:          [][]state.BracketMatch{{{ID: "m-r1-0", SideA: wrTeamC, SideB: "Other", Status: state.MatchStatusCompleted, Winner: wrTeamC}}},
			ThirdPlaceMatch: &r1,
		}
	}
	require.NoError(t, store.SaveBracket(compID, b))
	_, _, err := eng.RecordDecision(compID, matchID, "kiken-voluntary", "aka", "knee", nil, false)
	require.NoError(t, err)
	if !bronze {
		_, err = eng.RecordMatchResultWithIneligibility(compID, "m-r2-0", &state.MatchResult{
			ID: "m-r2-0", SideA: wrTeamB, SideB: wrTeamC, Winner: wrTeamB, Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "t1", SideB: "k1", Winner: "t1", IpponsA: []string{"M"}}},
		})
		require.NoError(t, err)
	}
	return eng, store, compID, dir, matchID
}

func wrBracketMatch(t *testing.T, store *state.Store, compID string, bronze bool) state.BracketMatch {
	t.Helper()
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	if bronze {
		return *b.ThirdPlaceMatch
	}
	return b.Rounds[0][0]
}

func TestWithdrawalRulingKept_BracketCorrection(t *testing.T) {
	for _, bronze := range []bool{false, true} {
		name := "round"
		if bronze {
			name = "bronze"
		}
		t.Run(name, func(t *testing.T) {
			eng, store, compID, dir, matchID := seedBracketWithdrawal(t, bronze)
			before := wrBracketMatch(t, store, compID, bronze)
			require.Equal(t, wrTeamB, before.Winner)
			finalBefore, err := store.LoadBracket(compID)
			require.NoError(t, err)
			statusBefore := readStatusFile(t, dir, compID)

			var reopened []ReopenedMatch
			err = inTx(t, store, compID, func(tx state.StoreTx) error {
				status, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, matchID, wrCorrection(matchID), ForceOptions{Reopened: &reopened})
				assert.Nil(t, status)
				return e
			})
			require.NoError(t, err, "a correction that keeps the winner must not meet the downstream guard")
			assert.Empty(t, reopened)

			after := wrBracketMatch(t, store, compID, bronze)
			assertRulingKept(t, rulingOfBracketMatch(&before), rulingOfBracketMatch(&after))
			require.Len(t, after.SubResults, 1)
			assert.Equal(t, []string{"K"}, after.SubResults[0].IpponsA, "the bout edit is saved")
			assert.Equal(t, "Scoring error: wrong waza entered", after.CorrectionReason)
			assert.Equal(t, string(statusBefore), string(readStatusFile(t, dir, compID)))
			if !bronze {
				b, lerr := store.LoadBracket(compID)
				require.NoError(t, lerr)
				assert.Equal(t, finalBefore.Rounds[1][0], b.Rounds[1][0], "the final Tora won is untouched")
			}
		})
	}
}

// KeepsWithdrawalRuling: every clause is load-bearing.
func TestKeepsWithdrawalRuling(t *testing.T) {
	correction := func(mut func(*state.MatchResult)) *state.MatchResult {
		r := &state.MatchResult{Status: state.MatchStatusCompleted, SubResults: []state.SubMatchResult{wrBout1("M")}}
		if mut != nil {
			mut(r)
		}
		return r
	}
	cases := []struct {
		name           string
		storedStatus   state.MatchStatus
		storedDecision string
		incoming       *state.MatchResult
		want           bool
	}{
		{"bout correction over a kiken", state.MatchStatusCompleted, "kiken-voluntary", correction(nil), true},
		{"over a legacy kiken", state.MatchStatusCompleted, "kiken", correction(nil), true},
		{"over a fusenpai", state.MatchStatusCompleted, "fusenpai", correction(nil), true},
		{"a hikiwake-mapped sheet still keeps it", state.MatchStatusCompleted, "kiken-injury", correction(func(r *state.MatchResult) { r.Decision = "hikiwake" }), true},
		{"stored fought", state.MatchStatusCompleted, "", correction(nil), false},
		{"stored per-bout default win is not a withdrawal", state.MatchStatusCompleted, "fusensho", correction(nil), false},
		{"stored not completed", state.MatchStatusRunning, "kiken-voluntary", correction(nil), false},
		{"incoming not completed", state.MatchStatusCompleted, "kiken-voluntary", correction(func(r *state.MatchResult) { r.Status = state.MatchStatusScheduled }), false},
		{"an individual correction (no bout rows) keeps it", state.MatchStatusCompleted, "kiken-voluntary", correction(func(r *state.MatchResult) { r.SubResults = nil }), true},
		{"an individual correction with the draw toggle keeps it", state.MatchStatusCompleted, "fusenpai", correction(func(r *state.MatchResult) {
			r.SubResults = nil
			r.Decision = "hikiwake"
		}), true},
		{"incoming is a re-decision", state.MatchStatusCompleted, "kiken-voluntary", correction(func(r *state.MatchResult) { r.Decision = "fusenpai" }), false},
		// The allowlist: every decision a score sheet cannot send replaces the
		// ruling. fusensho and daihyosen arrive from POST /decision with the
		// prior bout rows attached (preserveLoserScore), which a denylist kept.
		{"incoming fusensho replaces it", state.MatchStatusCompleted, "kiken-voluntary", correction(func(r *state.MatchResult) { r.Decision = "fusensho" }), false},
		{"incoming daihyosen replaces it", state.MatchStatusCompleted, "kiken-injury", correction(func(r *state.MatchResult) { r.Decision = "daihyosen" }), false},
		{"incoming kachinuki-exhaustion replaces it", state.MatchStatusCompleted, "fusenpai", correction(func(r *state.MatchResult) { r.Decision = "kachinuki-exhaustion" }), false},
		{"incoming fought replaces it", state.MatchStatusCompleted, "kiken-voluntary", correction(func(r *state.MatchResult) { r.Decision = "fought" }), false},
		{"incoming ippon-shobu replaces it", state.MatchStatusCompleted, "kiken-voluntary", correction(func(r *state.MatchResult) { r.Decision = "ippon-shobu" }), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, KeepsWithdrawalRuling(tc.storedStatus, tc.storedDecision, tc.incoming))
		})
	}
}

// keptWithdrawalScoreline: the winner's maru is the ruling's, the withdrawing
// side's letters are the write's (through struckIppons), except where the
// write is a team sheet, the ruling names no side, or the write omits the side.
func TestKeptWithdrawalScoreline(t *testing.T) {
	maru := domain.DefaultWinIppons(false)
	stored := func(by string) withdrawalRuling {
		if by == "shiro" {
			return withdrawalRuling{DecisionBy: by, IpponsA: maru, IpponsB: []string{"M"}}
		}
		return withdrawalRuling{DecisionBy: by, IpponsA: []string{"M"}, IpponsB: maru}
	}
	for _, tc := range []struct {
		name         string
		stored       withdrawalRuling
		incoming     *state.MatchResult
		wantA, wantB []string
	}{
		{"aka withdrew: A from the write, B the maru",
			stored("aka"), &state.MatchResult{IpponsA: []string{"D"}, IpponsB: []string{"K"}},
			[]string{"D"}, maru},
		{"shiro withdrew: B from the write, A the maru",
			stored("shiro"), &state.MatchResult{IpponsA: []string{"K"}, IpponsB: []string{"K", "D"}},
			maru, []string{"K", "D"}},
		{"an echoed maru or hantei mark is not a strike",
			stored("aka"), &state.MatchResult{IpponsA: []string{"○", domain.HanteiMark, "M"}},
			[]string{"M"}, maru},
		{"an explicit [] clears the letters",
			stored("aka"), &state.MatchResult{IpponsA: []string{}},
			nil, maru},
		{"an omitted side keeps the stored letters",
			stored("aka"), &state.MatchResult{},
			[]string{"M"}, maru},
		{"a team sheet keeps the stored scoreline",
			stored("aka"), &state.MatchResult{IpponsA: []string{}, SubResults: []state.SubMatchResult{wrBout1("M")}},
			[]string{"M"}, maru},
		{"a ruling with no side keeps the stored scoreline",
			stored(""), &state.MatchResult{IpponsA: []string{"D"}},
			[]string{"M"}, maru},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a, b := keptWithdrawalScoreline(tc.stored, tc.incoming)
			assert.Equal(t, tc.wantA, a)
			assert.Equal(t, tc.wantB, b)
		})
	}
}

// A restore replays a trusted snapshot and inherits nothing.
func TestPreserveWithdrawalRuling_RestoreInheritsNothing(t *testing.T) {
	stored := withdrawalRuling{Status: state.MatchStatusCompleted, Decision: "kiken-voluntary", Winner: wrTeamB}
	result := &state.MatchResult{Status: state.MatchStatusCompleted, Winner: wrTeamA, SubResults: []state.SubMatchResult{wrBout1("M")}}
	assert.False(t, preserveWithdrawalRuling(stored, result, matchWriteRestore))
	assert.Equal(t, wrTeamA, result.Winner)
	assert.True(t, preserveWithdrawalRuling(stored, result, matchWriteForward))
	assert.Equal(t, wrTeamB, result.Winner)
}

// Recovery path (1): a withdrawal recorded against the wrong team is fixed by
// re-deciding it for the other side (POST .../decision), which this change
// does not touch: the bouts fought before it survive (preserveLoserScore),
// the other team becomes the winner, and the wrongly withdrawn team is
// eligible again (RecordDecisionTx's MatchID-keyed restore), including after
// a bout correction has been saved in between.
func TestWithdrawalRulingKept_OtherSideStillFixesAWrongWithdrawal(t *testing.T) {
	eng, store, compID, _ := seedPoolWithdrawal(t, "kiken-voluntary")
	_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", wrCorrection("Pool A-0"))
	require.NoError(t, err)

	_, status, err := eng.RecordDecision(compID, "Pool A-0", "kiken-voluntary", "shiro", "wrong team", nil, false)
	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, wrTeamAID, status.PlayerID)
	assert.True(t, status.Eligible, "the wrongly withdrawn team is eligible again")

	m := wrPoolMatch(t, store, compID)
	assert.Equal(t, wrTeamA, m.Winner)
	assert.Equal(t, "shiro", m.DecisionBy)
	require.Len(t, m.SubResults, 1)
	assert.Equal(t, []string{"K"}, m.SubResults[0].IpponsA, "the corrected bout survives the re-decision")
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.False(t, statuses[wrTeamBID].Eligible, "the team that actually withdrew is now the ineligible one")
}
