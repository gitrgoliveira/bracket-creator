package engine

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFirstInQueueTime(t *testing.T) {
	// Standard case: one minute before the target court's earliest timed match.
	assert.Equal(t, "09:59", firstInQueueTime("11:00", "10:00"))
	assert.Equal(t, "09:59", firstInQueueTime("10:00", "10:00"))

	// No timed match on the target court (or only untimed matches): keep own time.
	assert.Equal(t, "10:30", firstInQueueTime("10:30", ""))
	assert.Equal(t, "", firstInQueueTime("", ""))

	// Own time already sorts before earliest in queue order: keep own time.
	assert.Equal(t, "09:15", firstInQueueTime("09:15", "10:00"))

	// Untimed moved match reads as "99:99" in queue order, so it is placed one
	// minute before earliest rather than kept as "".
	assert.Equal(t, "09:59", firstInQueueTime("", "10:00"))

	// Earliest at midnight cannot step back a minute: the match takes "00:00"
	// itself and ties for first, since nothing sorts earlier.
	assert.Equal(t, "00:00", firstInQueueTime("10:00", "00:00"))
	assert.Equal(t, "00:00", firstInQueueTime("09:00", "00:00"))
	assert.Equal(t, "00:00", firstInQueueTime("00:00", "00:00"))
	assert.Equal(t, "00:00", firstInQueueTime("", "00:00"))

	// Legacy non-canonical or unparseable earliest: keep own time.
	assert.Equal(t, "10:00", firstInQueueTime("10:00", "9:45"))
	assert.Equal(t, "", firstInQueueTime("", "9:45"))
	assert.Equal(t, "10:00", firstInQueueTime("10:00", "not-a-time"))
}

func TestLastInQueueTime(t *testing.T) {
	// Standard case: one minute after the target court's latest timed match.
	assert.Equal(t, "10:01", lastInQueueTime("09:00", "10:00"))
	assert.Equal(t, "10:01", lastInQueueTime("10:00", "10:00"))

	// No timed match on the target court (or only untimed matches): keep own time.
	assert.Equal(t, "09:30", lastInQueueTime("09:30", ""))
	assert.Equal(t, "", lastInQueueTime("", ""))

	// Own time already sorts after latest in queue order: keep own time.
	assert.Equal(t, "12:00", lastInQueueTime("12:00", "10:00"))

	// Untimed moved match reads as "99:99" in queue order, so it already sorts
	// after any timed match and keeps "".
	assert.Equal(t, "", lastInQueueTime("", "10:00"))

	// Latest at 23:59 has no later minute: the match takes "23:59" itself and
	// ties for last, since nothing sorts later.
	assert.Equal(t, "23:59", lastInQueueTime("10:00", "23:59"))
	assert.Equal(t, "23:59", lastInQueueTime("23:59", "23:59"))

	// Legacy non-canonical latest "9:45" sorts after "10:00" as a string, and
	// "09:46" would sort before it, so the match keeps its own time.
	assert.Equal(t, "09:00", lastInQueueTime("09:00", "9:45"))
	assert.Equal(t, "10:00", lastInQueueTime("10:00", "9:45"))
	assert.Equal(t, "09:00", lastInQueueTime("09:00", "not-a-time"))
}

func TestUpdateMatchCourt_SameCourtNoOpAndRefusalsKeepFileVersion(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "court-noop-versions"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "NoOp", Format: state.CompFormatMixed}))

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "Pool A-1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusRunning, Court: "B", ScheduledAt: "09:10"},
		{ID: "Pool A-2", SideA: "Eve", SideB: "Frank", Status: state.MatchStatusCompleted, Court: "B", ScheduledAt: "09:20", Winner: "Eve"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", MatchNumber: 1, DisplayRound: 2, SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
			{ID: "m-r1-1", MatchNumber: 2, DisplayRound: 2, SideA: "Carol", SideB: "Dave", Status: state.MatchStatusRunning, Court: "B", ScheduledAt: "10:10"},
		}},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: state.BronzeMatchID, DisplayRound: -1, SideA: "Eve", SideB: "Frank", Status: state.MatchStatusCompleted, Court: "B", ScheduledAt: "10:20", Winner: "Eve",
		},
	}))

	poolVer := store.FileVersion(compID, "pool-matches.csv")
	bracketVer := store.FileVersion(compID, "bracket.json")

	// Rule 1: same-court moves on pool, bracket round, and bronze matches, across statuses.
	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-0", "A", CourtMove{}))
	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-1", "B", CourtMove{}))
	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-1", "B", CourtMove{StopsRunning: true}))
	require.NoError(t, eng.UpdateMatchCourt(compID, "m-r1-0", "A", CourtMove{}))
	require.NoError(t, eng.UpdateMatchCourt(compID, "m-r1-1", "B", CourtMove{}))
	require.NoError(t, eng.UpdateMatchCourt(compID, state.BronzeMatchID, "B", CourtMove{}))

	assert.Equal(t, poolVer, store.FileVersion(compID, "pool-matches.csv"), "same-court move must not save pool-matches.csv")
	assert.Equal(t, bracketVer, store.FileVersion(compID, "bracket.json"), "same-court move must not save bracket.json")

	// Rule 2 refusal on running pool and bracket matches.
	require.Error(t, eng.UpdateMatchCourt(compID, "Pool A-1", "A", CourtMove{StopsRunning: false}))
	require.Error(t, eng.UpdateMatchCourt(compID, "m-r1-1", "A", CourtMove{StopsRunning: false}))

	assert.Equal(t, poolVer, store.FileVersion(compID, "pool-matches.csv"), "refused moves must not save pool-matches.csv")
	assert.Equal(t, bracketVer, store.FileVersion(compID, "bracket.json"), "refused moves must not save bracket.json")
}

func TestUpdateMatchCourt_WaitingMoveLandsLast(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	comp1 := "wait-comp-1"
	comp2 := "wait-comp-2"
	compKo := "wait-comp-ko"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: comp1, Name: "Comp 1", Format: state.CompFormatLeague}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: comp2, Name: "Comp 2", Format: state.CompFormatLeague}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compKo, Name: "Comp KO", Format: state.CompFormatKnockout}))

	require.NoError(t, store.SavePoolMatches(comp1, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "Pool A-1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:15"},
		{ID: "Pool A-untimed", SideA: "Eve", SideB: "Frank", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: ""},
	}))
	// Another competition has a later scheduled match on court B and only an
	// untimed match on court C.
	require.NoError(t, store.SavePoolMatches(comp2, []state.MatchResult{
		{ID: "Pool B-0", SideA: "Grace", SideB: "Heidi", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "11:30"},
		{ID: "Pool C-untimed", SideA: "Ivan", SideB: "Judy", Status: state.MatchStatusScheduled, Court: "C", ScheduledAt: ""},
	}))
	require.NoError(t, store.SaveBracket(compKo, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", MatchNumber: 1, DisplayRound: 1, SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:30"},
		}},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: state.BronzeMatchID, DisplayRound: -1, SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:45",
		},
	}))

	// Moving Pool A-0 from A to B places it after comp2's 11:30 match on B -> 11:31.
	require.NoError(t, eng.UpdateMatchCourt(comp1, "Pool A-0", "B", CourtMove{}))
	pm := loadPoolMatchByID(t, store, comp1, "Pool A-0")
	assert.Equal(t, "B", pm.Court)
	assert.Equal(t, "11:31", pm.ScheduledAt)
	assert.Empty(t, historyLen(t, store, comp1, "Pool A-0"), "waiting move writes no history line")

	// Moving bracket round match m-r1-0 to B places it after 11:31 -> 11:32.
	require.NoError(t, eng.UpdateMatchCourt(compKo, "m-r1-0", "B", CourtMove{}))
	b, err := store.LoadBracket(compKo)
	require.NoError(t, err)
	assert.Equal(t, "B", b.Rounds[0][0].Court)
	assert.Equal(t, "11:32", b.Rounds[0][0].ScheduledAt)
	assert.Empty(t, historyLen(t, store, compKo, "m-r1-0"))

	// Moving bronze match to C (which holds only an untimed waiting match) keeps its own time.
	require.NoError(t, eng.UpdateMatchCourt(compKo, state.BronzeMatchID, "C", CourtMove{}))
	b, err = store.LoadBracket(compKo)
	require.NoError(t, err)
	assert.Equal(t, "C", b.ThirdPlaceMatch.Court)
	assert.Equal(t, "09:45", b.ThirdPlaceMatch.ScheduledAt)

	// Moving an untimed waiting match to B keeps "".
	require.NoError(t, eng.UpdateMatchCourt(comp1, "Pool A-untimed", "B", CourtMove{}))
	untimed := loadPoolMatchByID(t, store, comp1, "Pool A-untimed")
	assert.Equal(t, "B", untimed.Court)
	assert.Equal(t, "", untimed.ScheduledAt)
}

func TestUpdateMatchCourt_TwoWaitingMovesInARowLandInOrder(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "two-waiting-in-row"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Order"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		{ID: "Pool A-1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:05"},
		{ID: "Pool B-0", SideA: "Eve", SideB: "Frank", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
	}))

	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-0", "B", CourtMove{}))
	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-1", "B", CourtMove{}))

	m0 := loadPoolMatchByID(t, store, compID, "Pool A-0")
	m1 := loadPoolMatchByID(t, store, compID, "Pool A-1")
	assert.Equal(t, "10:01", m0.ScheduledAt)
	assert.Equal(t, "10:02", m1.ScheduledAt)
}

func TestUpdateMatchCourt_FinishedMatchKeepsTime(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "finished-keeps-time"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Finished"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A", ScheduledAt: "09:00"},
		{ID: "Pool B-0", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:30"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", MatchNumber: 1, DisplayRound: 1, SideA: "Alice", SideB: "Bob", Status: state.MatchStatusCompleted, Winner: "Alice", Court: "A", ScheduledAt: "09:15"},
		}},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: state.BronzeMatchID, DisplayRound: -1, SideA: "Carol", SideB: "Dave", Status: state.MatchStatusCompleted, Winner: "Carol", Court: "A", ScheduledAt: "09:20",
		},
	}))

	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-0", "B", CourtMove{}))
	require.NoError(t, eng.UpdateMatchCourt(compID, "m-r1-0", "B", CourtMove{}))
	require.NoError(t, eng.UpdateMatchCourt(compID, state.BronzeMatchID, "B", CourtMove{}))

	pm := loadPoolMatchByID(t, store, compID, "Pool A-0")
	assert.Equal(t, "B", pm.Court)
	assert.Equal(t, "09:00", pm.ScheduledAt, "finished pool match keeps its time")
	assert.Empty(t, historyLen(t, store, compID, "Pool A-0"))

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "B", b.Rounds[0][0].Court)
	assert.Equal(t, "09:15", b.Rounds[0][0].ScheduledAt, "finished bracket match keeps its time")
	assert.Equal(t, "B", b.ThirdPlaceMatch.Court)
	assert.Equal(t, "09:20", b.ThirdPlaceMatch.ScheduledAt, "finished bronze match keeps its time")
}

func TestUpdateMatchCourt_WaitingPoolBoutPushesOwnKnockout(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "waiting-pool-pushes-ko"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Courts = []string{"A", "B"}
		c.StartTime = "09:00"
	})
	saveTestParticipants(t, store, compID, []string{"P1", "P2", "P3", "P4", "P5", "P6"})
	require.NoError(t, eng.GenerateDraw(compID))

	pms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var moved state.MatchResult
	for _, m := range pms {
		if m.Court == "A" {
			moved = m
			break
		}
	}
	require.NotEmpty(t, moved.ID)

	// The competition's own knockout is no queue neighbour of its pool bouts:
	// the bout lands after B's pool bouts, and the knockout follows the pools.
	var latestOnB string
	for _, m := range pms {
		if m.Court == "B" && m.Status == state.MatchStatusScheduled && m.ScheduledAt > latestOnB {
			latestOnB = m.ScheduledAt
		}
	}
	require.NotEmpty(t, latestOnB, "court B must have a scheduled pool bout")
	bBefore, err := store.LoadBracket(compID)
	require.NoError(t, err)
	knockoutOnB := false
	for _, r := range bBefore.Rounds {
		for _, bm := range r {
			knockoutOnB = knockoutOnB || (bm.Court == "B" && bm.MatchNumber > 0)
		}
	}
	require.True(t, knockoutOnB, "court B must have a knockout match")

	require.NoError(t, eng.UpdateMatchCourt(compID, moved.ID, "B", CourtMove{}))

	pmAfter := loadPoolMatchByID(t, store, compID, moved.ID)
	assert.Equal(t, "B", pmAfter.Court)
	assert.Equal(t, lastInQueueTime(moved.ScheduledAt, latestOnB), pmAfter.ScheduledAt,
		"the waiting pool bout lands one minute after the latest pool bout waiting on B")

	bAfter, err := store.LoadBracket(compID)
	require.NoError(t, err)
	for _, r := range bAfter.Rounds {
		for _, bm := range r {
			if bm.Court == "B" && bm.MatchNumber > 0 {
				assert.Greater(t, bm.ScheduledAt, pmAfter.ScheduledAt,
					"moveKnockoutPastPools pushes B's knockout past the moved pool bout")
			}
		}
	}
}

func TestUpdateMatchCourt_RunningMoveConfirmed(t *testing.T) {
	t.Run("pool match", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "run-confirm-pool"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "RunPool"}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{
				ID:               "Pool A-0",
				SideA:            "Alice",
				SideAID:          "id-a",
				SideB:            "Bob",
				SideBID:          "id-b",
				Status:           state.MatchStatusRunning,
				Court:            "B",
				ScheduledAt:      "10:30",
				IpponsA:          []string{"M"},
				HansokuB:         1,
				Encho:            &state.EnchoMetadata{PeriodCount: 1},
				FlagsA:           2,
				SubResults:       []state.SubMatchResult{{Position: 1, SideA: "A1", SideB: "B1", IpponsA: []string{"K"}}},
				RepPlayerA:       "Alice-rep",
				RepPlayerB:       "Bob-rep",
				Winner:           "Alice",
				WinnerID:         "id-a",
				Decision:         "fought",
				DecisionBy:       "A",
				DecisionReason:   "some reason",
				ResultSource:     "admin",
				CorrectionReason: "some correction",
				ReopenPending:    true,
				GroupStamps:      map[string]int64{state.GroupPoints: 500, state.GroupResult: 600},
			},
			{
				ID:          "Pool B-0",
				SideA:       "Carol",
				SideB:       "Dave",
				Status:      state.MatchStatusScheduled,
				Court:       "A",
				ScheduledAt: "10:00",
			},
		}))

		err := eng.UpdateMatchCourt(compID, "Pool A-0", "A", CourtMove{
			StopsRunning: true,
			WriteID:      "w-pool-run-1",
		})
		require.NoError(t, err)

		m := loadPoolMatchByID(t, store, compID, "Pool A-0")
		assert.Equal(t, state.MatchStatusScheduled, m.Status)
		assert.Equal(t, "A", m.Court)
		assert.Equal(t, "09:59", m.ScheduledAt, "placed one minute before court A's earliest waiting match")
		// Score and its stamps are kept.
		assert.Equal(t, []string{"M"}, m.IpponsA)
		assert.Equal(t, 1, m.HansokuB)
		require.NotNil(t, m.Encho)
		assert.Equal(t, 1, m.Encho.PeriodCount)
		assert.Equal(t, 2, m.FlagsA)
		require.Len(t, m.SubResults, 1)
		assert.Equal(t, "Alice-rep", m.RepPlayerA)
		assert.Equal(t, "Bob-rep", m.RepPlayerB)
		assert.Equal(t, int64(500), m.GroupStamp(state.GroupPoints), "points stamp kept")
		assert.Greater(t, m.GroupStamp(state.GroupResult), int64(600), "result group stamped at server time")
		// Verdict and audit notes are cleared.
		assert.Empty(t, m.Winner)
		assert.Empty(t, m.WinnerID)
		assert.Empty(t, m.Decision)
		assert.Empty(t, m.DecisionBy)
		assert.Empty(t, m.DecisionReason)
		assert.Empty(t, m.ResultSource)
		assert.Empty(t, m.CorrectionReason)
		assert.False(t, m.ReopenPending)

		history, err := store.LoadMatchHistory(compID, "Pool A-0")
		require.NoError(t, err)
		require.Len(t, history, 1)
		assert.Equal(t, doorRequeue, history[0].Door)
		assert.Equal(t, "A", history[0].Court)
		assert.Equal(t, m.GroupStamp(state.GroupResult), history[0].Stamp)
		assert.Equal(t, state.HistoryOutcomeApplied, history[0].Outcomes[state.GroupResult])
		assert.Equal(t, 1, countLoggedWriteID(t, store, compID, "Pool A-0", "w-pool-run-1"))
	})

	t.Run("bracket round and bronze matches", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "run-confirm-bracket"
		require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "RunBracket"}))
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{
			Rounds: [][]state.BracketMatch{{
				{
					ID:               "m-r1-0",
					MatchNumber:      1,
					DisplayRound:     2,
					SideA:            "Alice",
					SideAID:          "id-a",
					SideB:            "Bob",
					SideBID:          "id-b",
					Status:           state.MatchStatusRunning,
					Court:            "B",
					ScheduledAt:      "11:00",
					IpponsA:          []string{"K"},
					HansokuA:         1,
					Encho:            &state.EnchoMetadata{PeriodCount: 2},
					FlagsB:           1,
					Winner:           "Alice",
					WinnerID:         "id-a",
					Decision:         "fought",
					IsOverridden:     true,
					ResultSource:     "admin",
					CorrectionReason: "fix",
					ReopenPending:    true,
					GroupStamps:      map[string]int64{state.GroupPoints: 700, state.GroupResult: 800},
				},
				{
					ID:          "m-r1-1",
					MatchNumber: 2,
					SideA:       "Carol",
					SideB:       "Dave",
					Status:      state.MatchStatusScheduled,
					Court:       "A",
					ScheduledAt: "10:15",
				},
			}},
			ThirdPlaceMatch: &state.BracketMatch{
				ID:            state.BronzeMatchID,
				DisplayRound:  -1,
				SideA:         "Eve",
				SideB:         "Frank",
				Status:        state.MatchStatusRunning,
				Court:         "B",
				ScheduledAt:   "11:30",
				IpponsB:       []string{"D"},
				Winner:        "Frank",
				ReopenPending: true,
				GroupStamps:   map[string]int64{state.GroupPoints: 750, state.GroupResult: 850},
			},
		}))

		require.NoError(t, eng.UpdateMatchCourt(compID, "m-r1-0", "A", CourtMove{
			StopsRunning: true,
			WriteID:      "w-brk-run-1",
		}))
		require.NoError(t, eng.UpdateMatchCourt(compID, state.BronzeMatchID, "A", CourtMove{
			StopsRunning: true,
			WriteID:      "w-bronze-run-1",
		}))

		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		r0 := b.Rounds[0][0]
		assert.Equal(t, state.MatchStatusScheduled, r0.Status)
		assert.Equal(t, "A", r0.Court)
		assert.Equal(t, "10:14", r0.ScheduledAt)
		assert.Equal(t, []string{"K"}, r0.IpponsA)
		assert.Equal(t, 1, r0.HansokuA)
		assert.Equal(t, int64(700), r0.GroupStamp(state.GroupPoints))
		assert.Empty(t, r0.Winner)
		assert.Empty(t, r0.WinnerID)
		assert.Empty(t, r0.Decision)
		assert.False(t, r0.IsOverridden)
		assert.Empty(t, r0.ResultSource)
		assert.Empty(t, r0.CorrectionReason)
		assert.False(t, r0.ReopenPending)

		// The bronze moved second, so it steps one minute before 10:14 -> 10:13.
		br := b.ThirdPlaceMatch
		assert.Equal(t, state.MatchStatusScheduled, br.Status)
		assert.Equal(t, "A", br.Court)
		assert.Equal(t, "10:13", br.ScheduledAt)
		assert.Equal(t, []string{"D"}, br.IpponsB)
		assert.Empty(t, br.Winner)
		assert.False(t, br.ReopenPending)

		hRound, err := store.LoadMatchHistory(compID, "m-r1-0")
		require.NoError(t, err)
		require.Len(t, hRound, 1)
		assert.Equal(t, doorRequeue, hRound[0].Door)
		assert.Equal(t, "A", hRound[0].Court)
		assert.Equal(t, 1, countLoggedWriteID(t, store, compID, "m-r1-0", "w-brk-run-1"))

		hBronze, err := store.LoadMatchHistory(compID, state.BronzeMatchID)
		require.NoError(t, err)
		require.Len(t, hBronze, 1)
		assert.Equal(t, doorRequeue, hBronze[0].Door)
		assert.Equal(t, "A", hBronze[0].Court)
		assert.Equal(t, 1, countLoggedWriteID(t, store, compID, state.BronzeMatchID, "w-bronze-run-1"))
	})
}

func TestUpdateMatchCourt_RunningMoveNotConfirmedRefused(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "run-unconfirmed"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Unconfirmed"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "B", ScheduledAt: "09:00"},
		{ID: "Pool A-1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusRunning, Court: "", ScheduledAt: "09:10"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", MatchNumber: 3, DisplayRound: 1, SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "B", ScheduledAt: "10:00"},
		}},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: state.BronzeMatchID, DisplayRound: -1, SideA: "Carol", SideB: "Dave", Status: state.MatchStatusRunning, Court: "B", ScheduledAt: "10:10",
		},
	}))

	// Pool match on Shiaijo B -> Shiaijo A.
	err := eng.UpdateMatchCourt(compID, "Pool A-0", "A", CourtMove{StopsRunning: false})
	var refused *CourtMoveRefusedError
	require.True(t, errors.As(err, &refused))
	assert.Equal(t, "Pool A · Match 1 was started on Shiaijo B before your move reached the server, so it was not moved. Move it again to stop it there and put it first on Shiaijo A.", refused.Sentence())
	assert.Equal(t, refused.Sentence(), err.Error())
	assert.Empty(t, historyLen(t, store, compID, "Pool A-0"))

	// An unassigned court is not named.
	err = eng.UpdateMatchCourt(compID, "Pool A-1", "A", CourtMove{StopsRunning: false})
	require.True(t, errors.As(err, &refused))
	assert.Equal(t, "Pool A · Match 2 was started before your move reached the server, so it was not moved. Move it again to stop it there and put it first on Shiaijo A.", refused.Sentence())

	// A move off every shiaijo names none to put it on.
	err = eng.UpdateMatchCourt(compID, "Pool A-0", "", CourtMove{StopsRunning: false})
	require.True(t, errors.As(err, &refused))
	assert.Equal(t, "Pool A · Match 1 was started on Shiaijo B before your move reached the server, so it was not moved. Move it again to stop it there and leave it without a shiaijo.", refused.Sentence())

	// Bracket round match.
	err = eng.UpdateMatchCourt(compID, "m-r1-0", "A", CourtMove{StopsRunning: false})
	require.True(t, errors.As(err, &refused))
	assert.Equal(t, "Match 3 (Final) was started on Shiaijo B before your move reached the server, so it was not moved. Move it again to stop it there and put it first on Shiaijo A.", refused.Sentence())
	assert.Empty(t, historyLen(t, store, compID, "m-r1-0"))

	// Bronze match (starts lowercase in MatchLabel, SentenceCase uppercases "The").
	err = eng.UpdateMatchCourt(compID, state.BronzeMatchID, "A", CourtMove{StopsRunning: false})
	require.True(t, errors.As(err, &refused))
	assert.Equal(t, "The 3rd-place match was started on Shiaijo B before your move reached the server, so it was not moved. Move it again to stop it there and put it first on Shiaijo A.", refused.Sentence())
}

func TestUpdateMatchCourt_ConfirmedOverNonRunningMovesAsWaitingOrFinished(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "confirmed-non-running"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "NonRunning"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-base", SideA: "Grace", SideB: "Heidi", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "10:30"},
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "09:00"},
		{ID: "Pool A-1", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusCompleted, Winner: "Carol", Court: "B", ScheduledAt: "09:10"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", MatchNumber: 1, DisplayRound: 2, SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
		}},
		ThirdPlaceMatch: &state.BracketMatch{
			ID: state.BronzeMatchID, DisplayRound: -1, SideA: "Carol", SideB: "Dave", Status: state.MatchStatusCompleted, Winner: "Carol", Court: "B", ScheduledAt: "10:10",
		},
	}))

	// Confirmed move over scheduled pool match lands last on target court A (10:30 + 1m = 10:31).
	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-0", "A", CourtMove{StopsRunning: true}))
	pm := loadPoolMatchByID(t, store, compID, "Pool A-0")
	assert.Equal(t, "A", pm.Court)
	assert.Equal(t, "10:31", pm.ScheduledAt)
	assert.Equal(t, state.MatchStatusScheduled, pm.Status)
	assert.Empty(t, historyLen(t, store, compID, "Pool A-0"), "waiting move writes no requeue history line")

	// Confirmed move over completed pool match keeps its time and result.
	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-1", "A", CourtMove{StopsRunning: true}))
	pm = loadPoolMatchByID(t, store, compID, "Pool A-1")
	assert.Equal(t, "A", pm.Court)
	assert.Equal(t, "09:10", pm.ScheduledAt)
	assert.Equal(t, state.MatchStatusCompleted, pm.Status)
	assert.Equal(t, "Carol", pm.Winner)
	assert.Empty(t, historyLen(t, store, compID, "Pool A-1"))

	// Confirmed move over scheduled bracket match lands last on target court A (latest is now 10:31 -> 10:32).
	require.NoError(t, eng.UpdateMatchCourt(compID, "m-r1-0", "A", CourtMove{StopsRunning: true}))
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bm := b.MatchByID("m-r1-0")
	require.NotNil(t, bm)
	assert.Equal(t, "A", bm.Court)
	assert.Equal(t, "10:32", bm.ScheduledAt)
	assert.Equal(t, state.MatchStatusScheduled, bm.Status)
	assert.Empty(t, historyLen(t, store, compID, "m-r1-0"))

	// Confirmed move over completed bronze match keeps its time and result.
	require.NoError(t, eng.UpdateMatchCourt(compID, state.BronzeMatchID, "A", CourtMove{StopsRunning: true}))
	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, b.ThirdPlaceMatch)
	assert.Equal(t, "A", b.ThirdPlaceMatch.Court)
	assert.Equal(t, "10:10", b.ThirdPlaceMatch.ScheduledAt)
	assert.Equal(t, state.MatchStatusCompleted, b.ThirdPlaceMatch.Status)
	assert.Equal(t, "Carol", b.ThirdPlaceMatch.Winner)
	assert.Empty(t, historyLen(t, store, compID, state.BronzeMatchID))
}

func TestUpdateMatchCourt_RefusedPoolMoveLeavesKnockoutTimesUnchanged(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "refused-pool-ko-unchanged"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Courts = []string{"A", "B"}
		c.StartTime = "09:00"
	})
	saveTestParticipants(t, store, compID, []string{"P1", "P2", "P3", "P4", "P5", "P6"})
	require.NoError(t, eng.GenerateDraw(compID))

	pms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, pms)
	// Put the first pool match at a late time and mark it running so moving it
	// without StopsRunning is refused.
	targetID := pms[0].ID
	otherCourt := "A"
	if pms[0].Court == "A" {
		otherCourt = "B"
	}
	pms[0].Status = state.MatchStatusRunning
	pms[0].ScheduledAt = "18:00"
	require.NoError(t, store.SavePoolMatches(compID, pms))

	bBefore, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bracketVer := store.FileVersion(compID, "bracket.json")

	err = eng.UpdateMatchCourt(compID, targetID, otherCourt, CourtMove{StopsRunning: false})
	require.Error(t, err)

	bAfter, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, bBefore, bAfter, "refused pool move must not push knockout times")
	assert.Equal(t, bracketVer, store.FileVersion(compID, "bracket.json"))
}

func TestUpdateMatchCourt_ResendByWriteIDAnsweredAsRecorded(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "resend-write-id"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Resend"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "B", ScheduledAt: "10:00"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", MatchNumber: 1, DisplayRound: 1, SideA: "Carol", SideB: "Dave", Status: state.MatchStatusRunning, Court: "B", ScheduledAt: "10:00"},
		}},
	}))

	mvPool := CourtMove{StopsRunning: true, WriteID: "w-dup-pool"}
	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-0", "A", mvPool))
	mvBrk := CourtMove{StopsRunning: true, WriteID: "w-dup-brk"}
	require.NoError(t, eng.UpdateMatchCourt(compID, "m-r1-0", "A", mvBrk))

	// Move each match on to court "B" with a plain CourtMove{} (no WriteID).
	// The match is now waiting; a waiting move writes no history line.
	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-0", "B", CourtMove{}))
	require.NoError(t, eng.UpdateMatchCourt(compID, "m-r1-0", "B", CourtMove{}))

	poolVer := store.FileVersion(compID, "pool-matches.csv")
	bracketVer := store.FileVersion(compID, "bracket.json")

	// Resend the original mvPool and mvBrk to court "A" after the matches have
	// moved on to court "B". Rule 0 answers nil and writes nothing because the
	// WriteID is already logged. Rule 1 cannot answer because each match sits
	// on "B", not "A".
	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-0", "A", mvPool))
	require.NoError(t, eng.UpdateMatchCourt(compID, "m-r1-0", "A", mvBrk))

	pm := loadPoolMatchByID(t, store, compID, "Pool A-0")
	assert.Equal(t, "B", pm.Court, "pool match must still sit on court B")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	bm := b.MatchByID("m-r1-0")
	require.NotNil(t, bm)
	assert.Equal(t, "B", bm.Court, "bracket match must still sit on court B")

	assert.Equal(t, poolVer, store.FileVersion(compID, "pool-matches.csv"))
	assert.Equal(t, bracketVer, store.FileVersion(compID, "bracket.json"))
	assert.Equal(t, 1, historyLen(t, store, compID, "Pool A-0"))
	assert.Equal(t, 1, historyLen(t, store, compID, "m-r1-0"))
	assert.Equal(t, 1, countLoggedWriteID(t, store, compID, "Pool A-0", "w-dup-pool"))
	assert.Equal(t, 1, countLoggedWriteID(t, store, compID, "m-r1-0", "w-dup-brk"))
}

func TestUpdateMatchCourt_MoveToEmptyCourt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "move-empty-court"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "EmptyCourt"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-wait", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:30"},
		{ID: "Pool A-run", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusRunning, Court: "A", ScheduledAt: "09:45", IpponsA: []string{"M"}},
	}))

	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-wait", "", CourtMove{}))
	w := loadPoolMatchByID(t, store, compID, "Pool A-wait")
	assert.Equal(t, "", w.Court)
	assert.Equal(t, "09:30", w.ScheduledAt, "waiting move to empty court keeps its time")

	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-run", "", CourtMove{StopsRunning: true, WriteID: "w-empty-court"}))
	r := loadPoolMatchByID(t, store, compID, "Pool A-run")
	assert.Equal(t, "", r.Court)
	assert.Equal(t, state.MatchStatusScheduled, r.Status)
	assert.Equal(t, "09:45", r.ScheduledAt, "running move to empty court keeps its time")
	assert.Equal(t, []string{"M"}, r.IpponsA)

	h, err := store.LoadMatchHistory(compID, "Pool A-run")
	require.NoError(t, err)
	require.Len(t, h, 1)
	assert.Equal(t, doorRequeue, h[0].Door)
	assert.Equal(t, "", h[0].Court, "requeue line for move to empty court carries no court")
}

// A confirmed running move onto a court whose earliest waiting match is at
// "00:00" cannot step back a minute, so the moved match takes "00:00" itself
// and ties for first rather than keeping its own later time.
func TestUpdateMatchCourt_RunningMoveOntoMidnightQueueTiesForFirst(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "move-running-midnight"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Midnight"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-run", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A", ScheduledAt: "10:00", IpponsA: []string{"M"}},
		{ID: "Pool A-b0", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "00:00"},
		{ID: "Pool A-b1", SideA: "Eve", SideB: "Frank", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "09:00"},
	}))

	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-run", "B", CourtMove{StopsRunning: true, WriteID: "w-midnight"}))
	r := loadPoolMatchByID(t, store, compID, "Pool A-run")
	assert.Equal(t, "B", r.Court)
	assert.Equal(t, state.MatchStatusScheduled, r.Status)
	assert.Equal(t, "00:00", r.ScheduledAt, "ties with the 00:00 match for first, never behind the 09:00 one")
	assert.Equal(t, []string{"M"}, r.IpponsA)
}

// A waiting match moved onto a court whose latest waiting match is at "23:59"
// has no later minute to take, so it takes "23:59" itself and ties for last
// rather than keeping its own earlier time.
func TestUpdateMatchCourt_WaitingMoveOntoLastMinuteQueueTiesForLast(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "move-waiting-last-minute"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "LastMinute"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-wait", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "10:00"},
		{ID: "Pool A-b0", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "12:00"},
		{ID: "Pool A-b1", SideA: "Eve", SideB: "Frank", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "23:59"},
	}))

	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-wait", "B", CourtMove{}))
	w := loadPoolMatchByID(t, store, compID, "Pool A-wait")
	assert.Equal(t, "B", w.Court)
	assert.Equal(t, state.MatchStatusScheduled, w.Status)
	assert.Equal(t, "23:59", w.ScheduledAt, "ties with the 23:59 match for last, never ahead of the 12:00 one")
}

func TestUpdateMatchCourt_BoundsReadErrorKeepsOwnTime(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "bounds-err-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "BoundsErr"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-wait", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:30"},
		{ID: "Pool A-run", SideA: "Carol", SideB: "Dave", Status: state.MatchStatusRunning, Court: "A", ScheduledAt: "09:45"},
		{ID: "Pool B-0", SideA: "Eve", SideB: "Frank", Status: state.MatchStatusScheduled, Court: "B", ScheduledAt: "10:00"},
	}))

	// Create a sibling competition directory whose pool-matches.csv is a
	// directory so CourtQueueBounds fails when scanning competitions.
	badCompDir := filepath.Join(dir, "competitions", "zzz-corrupt")
	require.NoError(t, os.MkdirAll(filepath.Join(badCompDir, "pool-matches.csv"), 0o755))

	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-wait", "B", CourtMove{}))
	w := loadPoolMatchByID(t, store, compID, "Pool A-wait")
	assert.Equal(t, "B", w.Court)
	assert.Equal(t, "09:30", w.ScheduledAt, "bounds read error keeps the waiting match's own time")

	require.NoError(t, eng.UpdateMatchCourt(compID, "Pool A-run", "B", CourtMove{StopsRunning: true}))
	r := loadPoolMatchByID(t, store, compID, "Pool A-run")
	assert.Equal(t, "B", r.Court)
	assert.Equal(t, state.MatchStatusScheduled, r.Status)
	assert.Equal(t, "09:45", r.ScheduledAt, "bounds read error keeps the running match's own time")
}

func TestUpdateMatchCourt_UnknownMatchReturnsNotFoundError(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "unknown-match-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Unknown"}))

	err := eng.UpdateMatchCourt(compID, "does-not-exist", "A", CourtMove{})
	var nf *NotFoundError
	require.ErrorAs(t, err, &nf)
}

func TestUpdateMatchCourt_RevertMatchToQueueCarriesNoCourt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "revert-no-court-field"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "Revert"}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A"},
	}))

	require.NoError(t, eng.RevertMatchToQueue(compID, "Pool A-0"))
	h, err := store.LoadMatchHistory(compID, "Pool A-0")
	require.NoError(t, err)
	require.Len(t, h, 1)
	assert.Equal(t, doorRequeue, h[0].Door)
	assert.Empty(t, h[0].Court, "RevertMatchToQueue history line must carry no court")
}
