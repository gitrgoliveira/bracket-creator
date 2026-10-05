package engine

// A knockout correction whose new winner would change a side of a later match
// that is RUNNING is refused on every door, and force does not get past it
// (operator decision 2026-09-27). TestDownstreamKnockoutCorrection_RunningDownstreamRefuses
// (downstream_correction_guard_test.go) pins the score door; these pin the
// rest: the bronze a semifinal feeds, a running match past a bye, the
// override and engi doors, a feeder assertion overtaken by the real result,
// and /decision, where the refusal must come before the T103 lock.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedSemifinalsWithRunning is seedBronzeBracket's shape with the final and
// the bronze set to the statuses given; a match left completed keeps its own
// result, a running one has one ippon struck and no verdict.
func seedSemifinalsWithRunning(t *testing.T, store *state.Store, compID string, finalRunning, bronzeRunning bool) {
	t.Helper()
	seedBronzeBracket(t, store, compID)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	final := &b.Rounds[1][0]
	final.MatchNumber, final.DisplayRound, final.Court = 3, 1, "A"
	if finalRunning {
		final.Status, final.Winner, final.WinnerID = state.MatchStatusRunning, "", ""
		final.IpponsA = []string{"K"}
	}
	bronze := b.ThirdPlaceMatch
	bronze.Court = "B"
	if bronzeRunning {
		bronze.Status, bronze.Winner, bronze.WinnerID = state.MatchStatusRunning, "", ""
		bronze.IpponsA = []string{"K"}
	}
	require.NoError(t, store.SaveBracket(compID, b))
}

func correctSemifinal(t *testing.T, eng *Engine, store *state.Store, compID string, force bool) error {
	t.Helper()
	return inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{Force: force})
		return err
	})
}

func TestDownstreamKnockoutCorrection_RunningBronze(t *testing.T) {
	t.Run("a running bronze is named", func(t *testing.T) {
		for _, force := range []bool{false, true} {
			eng, store, _ := setupTestEngine(t)
			compID := "kcdg-running-bronze"
			seedSemifinalsWithRunning(t, store, compID, false, true)
			err := correctSemifinal(t, eng, store, compID, force)
			var runErr *DownstreamKnockoutRunningError
			require.ErrorAs(t, err, &runErr, "force=%v", force)
			assert.Equal(t, []string{"m-bronze"}, reopenedIDs(runErr.Running))
			assert.Equal(t, "The 3rd-place match is being fought now on Shiaijo B. Finish it or send it back to the queue, then save this correction again.",
				runErr.Error())
		}
	})

	t.Run("both running: both named, bronze first", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "kcdg-running-both"
		seedSemifinalsWithRunning(t, store, compID, true, true)
		err := correctSemifinal(t, eng, store, compID, false)
		var runErr *DownstreamKnockoutRunningError
		require.ErrorAs(t, err, &runErr)
		assert.Equal(t, []string{"m-bronze", "m-r2-0"}, reopenedIDs(runErr.Running))
		assert.Equal(t, "The 3rd-place match is being fought now on Shiaijo B and Match 3 (Final) on Shiaijo A. Finish them or send them back to the queue, then save this correction again.",
			runErr.Error())

		b, lerr := store.LoadBracket(compID)
		require.NoError(t, lerr)
		assert.Equal(t, "Alice", b.Rounds[0][0].Winner, "nothing saved")
		assert.Equal(t, "Alice", b.Rounds[1][0].SideA)
		assert.Equal(t, "Bob", b.ThirdPlaceMatch.SideA)
	})

	t.Run("running bronze with a played final: the running refusal, not the played one", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "kcdg-running-bronze-played-final"
		seedSemifinalsWithRunning(t, store, compID, false, true)
		err := correctSemifinal(t, eng, store, compID, false)
		var runErr *DownstreamKnockoutRunningError
		require.ErrorAs(t, err, &runErr)
		assert.NotErrorIs(t, err, ErrDownstreamKnockoutPlayed, "never ask to confirm a write that would then be refused")
	})
}

func TestDownstreamKnockoutCorrection_RunningPastABye(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-running-past-bye"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg", Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}},
			},
			{
				// A bye the draw resolved off Alice: nobody fought it.
				{ID: "m-r2-0", SideA: "Alice", SideB: "", SideAID: "alice",
					Winner: "Alice", WinnerID: "alice", Status: state.MatchStatusCompleted},
			},
			{
				{ID: "m-r3-0", SideA: "Alice", SideB: "Dave", SideAID: "alice", SideBID: "dave",
					Status: state.MatchStatusRunning, IpponsB: []string{"K"},
					MatchNumber: 3, DisplayRound: 1, Court: "C"},
			},
		},
	}))

	err := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", correctR1ToBob("fix"), ForceOptions{})
		return e
	})
	var runErr *DownstreamKnockoutRunningError
	require.ErrorAs(t, err, &runErr)
	assert.Equal(t, []string{"m-r3-0"}, reopenedIDs(runErr.Running), "the first match past the bye, the one being fought")

	b, lerr := store.LoadBracket(compID)
	require.NoError(t, lerr)
	assert.Equal(t, "Alice", b.Rounds[2][0].SideA, "nothing saved")
}

func TestDownstreamKnockoutCorrection_UnchangedWinnerOverRunningNextApplies(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-running-same-winner"
	seedRunningNextRound(t, store, compID)

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", &state.MatchResult{
			ID: "m-r1-0", SideA: "Alice", SideB: "Bob",
			Winner: "Alice", IpponsA: []string{"M", "K"},
			Status: state.MatchStatusCompleted, CorrectionReason: "scoreline typo",
		}, ForceOptions{})
		return err
	}), "a correction that keeps the winner moves nobody in the running match")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, []string{"M", "K"}, b.Rounds[0][0].IpponsA)
	assert.Equal(t, state.MatchStatusRunning, b.Rounds[1][0].Status)
	assert.Equal(t, "Alice", b.Rounds[1][0].SideA)
}

func TestDownstreamKnockoutCorrection_StaleWriteOverRunningNextIsSuperseded(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-running-stale"
	seedRunningNextRound(t, store, compID)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	b.Rounds[0][0].ModifiedAt = 10_000
	require.NoError(t, store.SaveBracket(compID, b))

	stale := correctR1ToBob("fix")
	stale.ModifiedAt = 5_000
	err = inTx(t, store, compID, func(tx state.StoreTx) error {
		_, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", stale, ForceOptions{})
		return e
	})
	require.ErrorIs(t, err, ErrMatchSuperseded, "a stale write is reported as superseded, not as the running refusal")
	var runErr *DownstreamKnockoutRunningError
	assert.NotErrorAs(t, err, &runErr)
}

func TestOverrideBracketWinner_RunningDownstreamRefuses(t *testing.T) {
	for _, force := range []bool{false, true} {
		eng, store, _ := setupTestEngine(t)
		compID := "kcdg-override-running"
		seedRunningNextRound(t, store, compID)

		var reopened []ReopenedMatch
		applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", "Bob", 0, ForceOptions{Force: force, Reopened: &reopened})
		var runErr *DownstreamKnockoutRunningError
		require.ErrorAs(t, err, &runErr, "force=%v", force)
		assert.False(t, applied)
		assert.Equal(t, []string{"m-r2-0"}, reopenedIDs(runErr.Running))
		assert.Empty(t, reopened)

		b, lerr := store.LoadBracket(compID)
		require.NoError(t, lerr)
		assert.Equal(t, "Alice", b.Rounds[0][0].Winner)
		assert.Equal(t, "Alice", b.Rounds[1][0].SideA)
	}
}

func TestEngiCorrection_RunningDownstreamRefuses(t *testing.T) {
	for _, force := range []bool{false, true} {
		const compID = "engi-running-downstream"
		eng, store := setupStartComp(t, &state.Competition{ID: compID, Name: "Engi KO", Format: state.CompFormatKnockout, Courts: []string{"A"}, Engi: true})
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
			{{ID: "m-r1-0", SideA: "Pair One", SideB: "Pair Two", SideAID: "p1", SideBID: "p2",
				Winner: "Pair One", WinnerID: "p1", Status: state.MatchStatusCompleted, FlagsA: 2, FlagsB: 1}},
			{{ID: "m-r2-0", SideA: "Pair One", SideB: "Pair Three", SideAID: "p1", SideBID: "p3",
				Status: state.MatchStatusRunning, FlagsA: 1, MatchNumber: 2, DisplayRound: 1, Court: "A"}},
		}}))

		_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0",
			&state.MatchResult{Status: state.MatchStatusCompleted, FlagsA: 1, FlagsB: 2, CorrectionReason: "fix"},
			ForceOptions{Force: force})
		var runErr *DownstreamKnockoutRunningError
		require.ErrorAs(t, err, &runErr, "force=%v", force)
		assert.Equal(t, []string{"m-r2-0"}, reopenedIDs(runErr.Running))

		b, lerr := store.LoadBracket(compID)
		require.NoError(t, lerr)
		assert.Equal(t, "Pair One", b.Rounds[0][0].Winner)
		assert.Equal(t, "Pair One", b.Rounds[1][0].SideA)
	}
}

// TestFeederAssertion_RealResultMustAgreeWhileTheFinalRuns: the operator
// asserts an unsynced feeder's winner so the final can start (Run now). If the
// feeder's own result then arrives naming the OTHER competitor while that
// final is being fought, it is a winner change and is refused; one that
// agrees with the assertion applies.
func TestFeederAssertion_RealResultMustAgreeWhileTheFinalRuns(t *testing.T) {
	setup := func(t *testing.T) (*Engine, *state.Store, string) {
		eng, store, _ := setupTestEngine(t)
		compID := "kcdg-feeder-assertion"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "kcdg", Status: state.CompStatusKnockout,
		}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
						Status: state.MatchStatusScheduled, MatchNumber: 1, DisplayRound: 2, Court: "B"},
					{ID: "m-r1-1", SideA: "Carol", SideB: "Dave", SideAID: "carol", SideBID: "dave",
						Winner: "Carol", WinnerID: "carol", Status: state.MatchStatusCompleted,
						IpponsA: []string{"M"}, MatchNumber: 2, DisplayRound: 2, Court: "A"},
				},
				{
					{ID: "m-r2-0", SideA: "Winner of r0-m0", SideB: "Carol", SideBID: "carol",
						Status: state.MatchStatusScheduled, MatchNumber: 3, DisplayRound: 1, Court: "A"},
				},
			},
		}))
		applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", "Alice", 1_000)
		require.NoError(t, err)
		require.True(t, applied)
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.Equal(t, "Alice", b.Rounds[1][0].SideA)
		b.Rounds[1][0].Status = state.MatchStatusRunning
		require.NoError(t, store.SaveBracket(compID, b))
		return eng, store, compID
	}
	feederResult := func(winner string) *state.MatchResult {
		r := &state.MatchResult{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", Winner: winner,
			Status: state.MatchStatusCompleted, ModifiedAt: 2_000}
		if winner == "Alice" {
			r.IpponsA = []string{"M"}
		} else {
			r.IpponsB = []string{"M"}
		}
		return r
	}

	t.Run("naming the other competitor is refused", func(t *testing.T) {
		eng, store, compID := setup(t)
		err := inTx(t, store, compID, func(tx state.StoreTx) error {
			_, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", feederResult("Bob"))
			return e
		})
		var runErr *DownstreamKnockoutRunningError
		require.ErrorAs(t, err, &runErr)
		assert.Equal(t, []string{"m-r2-0"}, reopenedIDs(runErr.Running))
		b, lerr := store.LoadBracket(compID)
		require.NoError(t, lerr)
		assert.Equal(t, "Alice", b.Rounds[1][0].SideA)
	})

	t.Run("agreeing with the assertion applies", func(t *testing.T) {
		eng, store, compID := setup(t)
		require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
			_, e := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", feederResult("Alice"))
			return e
		}))
		b, lerr := store.LoadBracket(compID)
		require.NoError(t, lerr)
		assert.Equal(t, state.MatchStatusRunning, b.Rounds[1][0].Status)
		assert.Equal(t, "Alice", b.Rounds[1][0].SideA)
	})
}

// TestRecordDecisionTx_RunningDownstreamRefusesBeforeTheLock: a wrong-side
// kiken on a semifinal, the final started, then the kiken re-recorded on the
// other side. The write is refused because the final is being fought, and
// that refusal comes FIRST: the operator is never asked to confirm the T103
// decision lock for a write that would then be refused anyway.
func TestRecordDecisionTx_RunningDownstreamRefusesBeforeTheLock(t *testing.T) {
	for _, t103Force := range []bool{false, true} {
		eng, store, _ := setupTestEngine(t)
		compID := "kcdg-decision-running"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "kcdg", Status: state.CompStatusKnockout,
		}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{
					{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
						Status: state.MatchStatusRunning, MatchNumber: 1, DisplayRound: 2, Court: "A"},
					{ID: "m-r1-1", SideA: "Carol", SideB: "Dave", SideAID: "carol", SideBID: "dave",
						Winner: "Carol", WinnerID: "carol", Status: state.MatchStatusCompleted,
						IpponsA: []string{"M"}, MatchNumber: 2, DisplayRound: 2, Court: "A"},
				},
				{
					{ID: "m-r2-0", SideA: "Winner of r0-m0", SideB: "Carol", SideBID: "carol",
						Status: state.MatchStatusScheduled, MatchNumber: 3, DisplayRound: 1, Court: "A"},
				},
			},
		}))
		// The hasty kiken: Alice (aka) recorded as withdrawing, so Bob goes on.
		require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
			_, _, e := eng.RecordDecisionTx(tx, compID, "m-r1-0", "kiken-voluntary", "aka", "", nil, false)
			return e
		}))
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.Equal(t, "Bob", b.Rounds[1][0].SideA)
		b.Rounds[1][0].Status = state.MatchStatusRunning
		require.NoError(t, store.SaveBracket(compID, b))

		// The correction: it was Bob (shiro) who withdrew.
		err = inTx(t, store, compID, func(tx state.StoreTx) error {
			_, _, e := eng.RecordDecisionTx(tx, compID, "m-r1-0", "kiken-voluntary", "shiro", "", nil, t103Force)
			return e
		})
		var runErr *DownstreamKnockoutRunningError
		require.ErrorAs(t, err, &runErr, "t103Force=%v", t103Force)
		assert.NotErrorIs(t, err, ErrDecisionLocked, "the refusal comes before the lock (t103Force=%v)", t103Force)
		assert.Equal(t, []string{"m-r2-0"}, reopenedIDs(runErr.Running))
		assert.Equal(t, "Match 3 (Final) is being fought now on Shiaijo A. Finish it or send it back to the queue, then save this correction again.",
			runErr.Error())

		b, err = store.LoadBracket(compID)
		require.NoError(t, err)
		assert.Equal(t, "Bob", b.Rounds[0][0].Winner, "nothing saved (t103Force=%v)", t103Force)
		assert.Equal(t, "Bob", b.Rounds[1][0].SideA)
		assert.Equal(t, state.MatchStatusRunning, b.Rounds[1][0].Status)
	}
}

func TestPropagatedDownstream_RunningOrder(t *testing.T) {
	final := &state.BracketMatch{ID: "final", Status: state.MatchStatusRunning}
	bronze := &state.BracketMatch{ID: "bronze", Status: state.MatchStatusRunning}
	assert.Equal(t, []*state.BracketMatch{bronze, final}, propagatedDownstream{bronze: bronze, next: final}.running(), "bronze first")
	bronze.Status = state.MatchStatusCompleted
	assert.Equal(t, []*state.BracketMatch{final}, propagatedDownstream{bronze: bronze, next: final}.running())
	final.Status = state.MatchStatusScheduled
	assert.Empty(t, propagatedDownstream{bronze: bronze, next: final}.running())
	assert.Empty(t, propagatedDownstream{}.running(), "past the final there is nothing")
}

// TestRecordDecisionTx_StaleDecisionOverRunningNextIsSuperseded: a kiken
// queued offline replays after another device scored the semifinal the other
// way and the final started. The decision is older than the stored match, so
// it is reported superseded, exactly as a stale score write is, never as the
// running refusal whose advice (save again) would overwrite the newer result.
func TestRecordDecisionTx_StaleDecisionOverRunningNextIsSuperseded(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kcdg-decision-stale"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "kcdg", Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", SideA: "Alice", SideB: "Bob", SideAID: "alice", SideBID: "bob",
					Winner: "Bob", WinnerID: "bob", Status: state.MatchStatusCompleted,
					IpponsB: []string{"M"}, ModifiedAt: 10_000, MatchNumber: 1, DisplayRound: 2, Court: "A"},
				{ID: "m-r1-1", SideA: "Carol", SideB: "Dave", SideAID: "carol", SideBID: "dave",
					Winner: "Carol", WinnerID: "carol", Status: state.MatchStatusCompleted,
					IpponsA: []string{"M"}, MatchNumber: 2, DisplayRound: 2, Court: "A"},
			},
			{
				{ID: "m-r2-0", SideA: "Bob", SideAID: "bob", SideB: "Carol", SideBID: "carol",
					Status: state.MatchStatusRunning, IpponsA: []string{"K"}, MatchNumber: 3, DisplayRound: 1, Court: "A"},
			},
		},
	}))

	// The stale kiken: Bob (shiro) withdrawing, which would make Alice the winner.
	err := inTx(t, store, compID, func(tx state.StoreTx) error {
		_, _, e := eng.RecordDecisionTx(tx, compID, "m-r1-0", "kiken-voluntary", "shiro", "", nil, false, 5_000)
		return e
	})
	require.ErrorIs(t, err, ErrMatchSuperseded, "a stale decision is reported as superseded, not as the running refusal")
	var runErr *DownstreamKnockoutRunningError
	assert.NotErrorAs(t, err, &runErr)

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", b.Rounds[0][0].Winner, "the newer result stands")
	assert.Equal(t, "Bob", b.Rounds[1][0].SideA)
}

// TestDownstreamKnockoutRunningError_SharedMessages: the server's sentence and
// the SPA's (write_result.jsx) are pinned by ONE table both languages load,
// testdata/downstream_running_messages.json, so a replayed refusal and a
// direct one read alike on both doors (a saved correction and a reopen) and
// for one match or several ("A, B and C"). Literal copies in each language's
// tests are how the reopen wording drifted apart.
func TestDownstreamKnockoutRunningError_SharedMessages(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "downstream_running_messages.json"))
	require.NoError(t, err, "shared Go/JS message table is missing")

	var table struct {
		Cases []struct {
			Name      string `json:"name"`
			Reopening bool   `json:"reopening"`
			Running   []struct {
				ID           string `json:"id"`
				Number       int    `json:"number"`
				DisplayRound int    `json:"displayRound"`
				Court        string `json:"court"`
				Label        string `json:"label"`
			} `json:"running"`
			Message string `json:"message"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases, "message table parsed to zero cases: it would assert nothing")

	for _, tc := range table.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			running := make([]ReopenedMatch, 0, len(tc.Running))
			for _, r := range tc.Running {
				m := ReopenedMatch{ID: r.ID, Number: r.Number, DisplayRound: r.DisplayRound, Court: r.Court}
				require.Equal(t, r.Label, MatchLabel(m), "the fixture's wire label must be the one the server sends")
				running = append(running, m)
			}
			assert.Equal(t, tc.Message,
				(&DownstreamKnockoutRunningError{Running: running, Reopening: tc.Reopening}).Error())
		})
	}
}
