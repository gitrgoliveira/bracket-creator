package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func retimeTournament(courts []string, pauses ...state.Pause) *state.Tournament {
	return &state.Tournament{Name: "Retime Cup", Date: "01-07-2026", DurationDays: 2, Courts: courts, Pauses: pauses}
}

// drawnForRetime draws a competition (day 1, 09:00) and returns it as stored.
func drawnForRetime(t *testing.T, format string, courts []string, players int, tourn *state.Tournament) (*Engine, *state.Store, *state.Competition) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveTournament(tourn))
	createTestCompetition(t, store, "retime", format, 4, func(c *state.Competition) {
		c.Courts = courts
		c.Date = "01-07-2026"
		c.StartTime = "09:00"
	})
	names := make([]string, players)
	for i := range names {
		names[i] = fmt.Sprintf("Fighter %d", i+1)
	}
	saveTestParticipants(t, store, "retime", names)
	require.NoError(t, eng.GenerateDraw("retime"))
	comp, err := store.LoadCompetition("retime")
	require.NoError(t, err)
	require.Equal(t, state.CompStatusDrawReady, comp.Status)
	return eng, store, comp
}

func movedTo(comp *state.Competition, date, start string) *state.Competition {
	cp := *comp
	cp.Date = date
	cp.StartTime = start
	return &cp
}

func retimeInTx(t *testing.T, eng *Engine, store *state.Store, oldComp, newComp *state.Competition) (RetimeOutcome, error) {
	t.Helper()
	in, err := eng.ReadRetimeInputs(oldComp, newComp)
	require.NoError(t, err)
	var out RetimeOutcome
	txErr := store.WithTransaction(oldComp.ID, func(tx state.StoreTx) error {
		var err error
		out, err = RetimeDrawTx(tx, oldComp, newComp, in)
		return err
	})
	return out, txErr
}

func storedTimes(t *testing.T, store *state.Store, comp *state.Competition) map[string]string {
	t.Helper()
	pool, err := store.LoadPoolMatches(comp.ID)
	require.NoError(t, err)
	var bracket *state.Bracket
	if comp.IsKnockoutEnabled() {
		bracket, err = store.LoadBracket(comp.ID)
		require.NoError(t, err)
	}
	out := map[string]string{}
	for _, r := range scheduledRows(pool, bracket) {
		out[r.id] = r.at
	}
	return out
}

func minutesOf(t *testing.T, at string) int {
	t.Helper()
	m, ok := state.ParseClockMinutes(at)
	require.True(t, ok, "time %q", at)
	return m
}

func TestRetime_SameInputsChangeNothingOnAFreshDraw(t *testing.T) {
	cases := []struct {
		format  string
		courts  []string
		players int
	}{
		{state.CompFormatMixed, []string{"A", "B"}, 8},
		{state.CompFormatKnockout, []string{"A", "B"}, 8},
		{state.CompFormatKnockout, []string{"A"}, 5},
		{state.CompFormatLeague, []string{"A"}, 5},
		{state.CompFormatLeague, []string{"A", "B"}, 6},
		{state.CompFormatLeague, []string{"A", "B", "C"}, 7},
		{state.CompFormatLeague, []string{"A", "B", "C", "D"}, 8},
		{state.CompFormatLeague, []string{"A", "B", "C", "D"}, 9},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("%s_%d_courts_%d_players", tc.format, len(tc.courts), tc.players), func(t *testing.T) {
			eng, store, comp := drawnForRetime(t, tc.format, tc.courts, tc.players,
				retimeTournament(tc.courts, state.Pause{Name: "Lunch", Start: "09:30", Length: "15m"}))
			out, err := eng.RetimePreview(comp, comp)
			require.NoError(t, err)
			assert.False(t, out.AnyChange, "a re-time at the same day and start must reproduce the draw: %+v", out.Changes)
			assert.Zero(t, out.NotFreshCount)
			assert.NotZero(t, out.MatchCount)
			assert.NotEmpty(t, out.Courts)
			before := storedTimes(t, store, comp)
			_, err = retimeInTx(t, eng, store, comp, comp)
			require.NoError(t, err)
			assert.Equal(t, before, storedTimes(t, store, comp))
		})
	}
}

func TestRetime_LaterStartMovesEveryMatchByTheSameAmount(t *testing.T) {
	for _, format := range []string{state.CompFormatMixed, state.CompFormatKnockout, state.CompFormatLeague} {
		t.Run(format, func(t *testing.T) {
			eng, store, comp := drawnForRetime(t, format, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
			before := storedTimes(t, store, comp)
			newComp := movedTo(comp, comp.Date, "11:00")

			out, err := retimeInTx(t, eng, store, comp, newComp)
			require.NoError(t, err)
			require.True(t, out.AnyChange)
			after := storedTimes(t, store, comp)
			assert.Len(t, out.Changes, len(before))
			for _, ch := range out.Changes {
				assert.Equal(t, before[ch.MatchID], ch.Was)
				assert.Equal(t, after[ch.MatchID], ch.Now)
				assert.Equal(t, 120, minutesOf(t, ch.Now)-minutesOf(t, ch.Was), "match %s", ch.MatchID)
			}
			require.NotEmpty(t, out.Courts)
			for _, c := range out.Courts {
				assert.Equal(t, 120, c.NewStart-c.OldStart, "court %s start", c.Court)
				assert.Equal(t, 120, c.NewEnd-c.OldEnd, "court %s end", c.Court)
			}
			assert.Zero(t, out.NotFreshCount)
		})
	}
}

// The move preview judges a drawn competition's clashes on these windows.
func TestRetime_NewWindowsAreEachShiaijosNewSpan(t *testing.T) {
	out := RetimeOutcome{Courts: []RetimeCourt{
		{Court: "A", OldStart: 540, OldEnd: 600, NewStart: 660, NewEnd: 720},
		{Court: "B", OldStart: 545, OldEnd: 590, NewStart: 665, NewEnd: 710},
	}}
	assert.Equal(t, map[string]state.CourtWindow{
		"A": {Start: 660, End: 720},
		"B": {Start: 665, End: 710},
	}, out.NewWindows())
	assert.Empty(t, RetimeOutcome{}.NewWindows())
}

func TestRetime_APauseTheNewStartRunsIntoPushesMatchesPastIt(t *testing.T) {
	tourn := retimeTournament([]string{"A", "B"}, state.Pause{Name: "Lunch", Start: "12:00", Length: "1h"})
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, tourn)
	out, err := retimeInTx(t, eng, store, comp, movedTo(comp, comp.Date, "11:50"))
	require.NoError(t, err)
	require.True(t, out.AnyChange)
	pastPause := false
	for _, at := range storedTimes(t, store, comp) {
		m := minutesOf(t, at)
		assert.False(t, m >= 12*60 && m < 13*60, "no match starts inside the pause, got %s", at)
		if m >= 13*60 {
			pastPause = true
		}
	}
	assert.True(t, pastPause, "the pause pushes later matches to 13:00 or after")
}

func TestRetime_AnotherDayTakesThatDaysBreaks(t *testing.T) {
	tourn := retimeTournament([]string{"A", "B"}, state.Pause{Name: "Grading", Start: "09:30", Length: "30m", Day: 2})
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, tourn)
	out, err := retimeInTx(t, eng, store, comp, movedTo(comp, "02-07-2026", "09:00"))
	require.NoError(t, err)
	require.True(t, out.AnyChange, "day 2 has a pause that day 1 has not")
	for _, at := range storedTimes(t, store, comp) {
		m := minutesOf(t, at)
		assert.False(t, m >= 9*60+30 && m < 10*60, "no match starts inside day 2's pause, got %s", at)
	}
}

func TestRetime_AnotherDayAtTheSameStartChangesNoTime(t *testing.T) {
	eng, _, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	out, err := eng.RetimePreview(comp, movedTo(comp, "02-07-2026", "09:00"))
	require.NoError(t, err)
	assert.False(t, out.AnyChange)
}

func TestRetime_AMatchSetByHandCountsAsNotFreshAndIsReplaced(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	pool, err := store.LoadPoolMatches(comp.ID)
	require.NoError(t, err)
	handID := pool[0].ID
	pool[0].ScheduledAt = "16:45"
	require.NoError(t, store.SavePoolMatches(comp.ID, pool))

	out, err := eng.RetimePreview(comp, movedTo(comp, comp.Date, "10:00"))
	require.NoError(t, err)
	assert.Equal(t, 1, out.NotFreshCount)

	res, err := retimeInTx(t, eng, store, comp, movedTo(comp, comp.Date, "10:00"))
	require.NoError(t, err)
	var hand *MatchTimeChange
	for i := range res.Changes {
		if res.Changes[i].MatchID == handID {
			hand = &res.Changes[i]
		}
	}
	require.NotNil(t, hand, "the hand-set match is re-timed too")
	assert.Equal(t, "16:45", hand.Was)
	assert.NotEqual(t, "16:45", hand.Now)
}

func TestRetime_APauseAddedAfterTheDrawCountsTheMatchesItShifts(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	require.NoError(t, store.SaveTournament(retimeTournament([]string{"A", "B"}, state.Pause{Name: "Late", Start: "09:10", Length: "20m"})))
	out, err := eng.RetimePreview(comp, comp)
	require.NoError(t, err)
	assert.Positive(t, out.NotFreshCount)
	assert.True(t, out.AnyChange)
}

func TestRetime_PreviewWritesNothingAndMatchesTheSave(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	newComp := movedTo(comp, comp.Date, "13:15")
	before := storedTimes(t, store, comp)
	preview, err := eng.RetimePreview(comp, newComp)
	require.NoError(t, err)
	assert.Equal(t, before, storedTimes(t, store, comp), "the dry run writes nothing")
	saved, err := retimeInTx(t, eng, store, comp, newComp)
	require.NoError(t, err)
	assert.Equal(t, preview, saved)
}

// A match time is a bare "HH:MM", so a start late enough to carry the
// competition's own matches past 24:00 would write "00:xx" times that sort first
// in the court queue. It is refused, in the dry run and the save, and nothing is
// written.
func TestRetime_AStartThatRunsTheCompetitionPastMidnightIsRefused(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	before := storedTimes(t, store, comp)
	late := movedTo(comp, comp.Date, "23:50")

	_, err := eng.RetimePreview(comp, late)
	assert.ErrorIs(t, err, ErrRetimePastMidnight)
	_, err = retimeInTx(t, eng, store, comp, late)
	assert.ErrorIs(t, err, ErrRetimePastMidnight)
	assert.Equal(t, before, storedTimes(t, store, comp))

	_, err = eng.RetimePreview(comp, movedTo(comp, comp.Date, "20:00"))
	assert.NoError(t, err, "a start that finishes before midnight is not refused")
}

// A wrap that lands at or after the competition's own start reads as a normal
// morning time, so the start-minute check alone cannot see it: here a pause
// that runs through midnight sends the next match to 10:05 the next day, after
// a 10:00 start. The assigner's cursor still knows it left the day.
func TestRetime_AWrapPastTheCompetitionsOwnStartIsRefused(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	require.NoError(t, store.SaveTournament(retimeTournament([]string{"A", "B"},
		state.Pause{Name: "Overnight", Start: "10:05", Length: "24h"})))
	_, err := eng.RetimePreview(comp, movedTo(comp, comp.Date, "10:00"))
	assert.ErrorIs(t, err, ErrRetimePastMidnight)
}

// The bronze match takes the final's slot and pushes the final one slot later,
// after the rounds' own wrap check. A pause from that pushed slot running
// through midnight sends the final to the next day at a time after the start,
// so only the push itself can tell it left the day: here semifinals at 10:00
// and one slot on, the bronze match, then the final the next day.
func TestRetime_TheBronzeMatchPushingTheFinalPastMidnightIsRefused(t *testing.T) {
	courts := []string{"A"}
	eng, store, _ := setupTestEngine(t)
	tourn := retimeTournament(courts)
	require.NoError(t, store.SaveTournament(tourn))
	single := false
	createTestCompetition(t, store, "retime", state.CompFormatKnockout, 4, func(c *state.Competition) {
		c.Courts = courts
		c.Date = "01-07-2026"
		c.StartTime = "09:00"
		c.TwoThirdPlaces = &single
	})
	saveTestParticipants(t, store, "retime", []string{"Fighter 1", "Fighter 2", "Fighter 3", "Fighter 4"})
	require.NoError(t, eng.GenerateDraw("retime"))
	comp, err := store.LoadCompetition("retime")
	require.NoError(t, err)
	bracket, err := store.LoadBracket(comp.ID)
	require.NoError(t, err)
	require.NotNil(t, bracket.ThirdPlaceMatch, "a single third place draws a bronze match")

	out, err := eng.RetimePreview(comp, movedTo(comp, comp.Date, "10:00"))
	require.NoError(t, err, "with no pause in the way the same move is not refused")
	assert.True(t, out.AnyChange)

	_, slot := SlotMinutes(comp, tourn)
	pushed := 10*60 + 3*slot
	require.NoError(t, store.SaveTournament(retimeTournament(courts,
		state.Pause{Name: "Overnight", Start: fmt.Sprintf("%02d:%02d", pushed/60, pushed%60), Length: "24h"})))
	before := storedTimes(t, store, comp)
	_, err = eng.RetimePreview(comp, movedTo(comp, comp.Date, "10:00"))
	assert.ErrorIs(t, err, ErrRetimePastMidnight)
	_, err = retimeInTx(t, eng, store, comp, movedTo(comp, comp.Date, "10:00"))
	assert.ErrorIs(t, err, ErrRetimePastMidnight)
	assert.Equal(t, before, storedTimes(t, store, comp))
}

// A competition with no shiaijo of its own is drawn on the tournament's
// (InheritedDrawCourts), so its re-time reads the league's slots on them too.
func TestRetime_ALeagueOnTheTournamentsShiaijoCanBeRetimed(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatLeague, nil, 6, retimeTournament([]string{"A", "B"}))
	pool, err := store.LoadPoolMatches(comp.ID)
	require.NoError(t, err)
	require.Equal(t, "B", pool[1].Court, "the draw used the tournament's two shiaijo")
	comp.Courts = nil // as stored by a competition that never chose its own

	out, err := eng.RetimePreview(comp, movedTo(comp, comp.Date, "11:00"))
	require.NoError(t, err)
	assert.True(t, out.AnyChange)
	assert.Zero(t, out.NotFreshCount)
}

func TestRetime_RefusesACompetitionThatIsNotDrawReady(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	before := storedTimes(t, store, comp)
	started := *comp
	started.Status = state.CompStatusPools
	_, err := retimeInTx(t, eng, store, &started, movedTo(&started, started.Date, "13:00"))
	assert.ErrorIs(t, err, ErrRetimeNotDrawReady)
	assert.Equal(t, before, storedTimes(t, store, comp))
	_, err = eng.RetimePreview(&started, movedTo(&started, started.Date, "13:00"))
	assert.ErrorIs(t, err, ErrRetimeNotDrawReady)
}

func TestRetime_ALeagueWhoseOrderCannotBeReadBackWritesNothing(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatLeague, []string{"A", "B"}, 6, retimeTournament([]string{"A", "B"}))
	pool, err := store.LoadPoolMatches(comp.ID)
	require.NoError(t, err)
	pool[1].Court = "Z"
	require.NoError(t, store.SavePoolMatches(comp.ID, pool))
	before := storedTimes(t, store, comp)

	_, err = retimeInTx(t, eng, store, comp, movedTo(comp, comp.Date, "11:00"))
	assert.ErrorIs(t, err, ErrRetimeUnavailable)
	assert.Equal(t, before, storedTimes(t, store, comp))
	_, err = eng.RetimePreview(comp, movedTo(comp, comp.Date, "11:00"))
	assert.ErrorIs(t, err, ErrRetimeUnavailable)
}

func TestRetime_ALeaguePlayerTwiceInOneSlotCannotBeReadBack(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatLeague, []string{"A", "B"}, 6, retimeTournament([]string{"A", "B"}))
	pool, err := store.LoadPoolMatches(comp.ID)
	require.NoError(t, err)
	// Give the second match of the first slot the first match's player.
	pool[1].SideAID = pool[0].SideAID
	require.NoError(t, store.SavePoolMatches(comp.ID, pool))
	_, err = eng.RetimePreview(comp, movedTo(comp, comp.Date, "11:00"))
	assert.ErrorIs(t, err, ErrRetimeUnavailable)
}

// A match the operator moved to another shiaijo after the draw leaves the
// fill-from-the-first-shiaijo pattern, so the slot boundaries cannot be read
// back: A,B,A,B becomes A,B,B,B and every later match would be re-timed a slot
// late. It is refused instead.
func TestRetime_ALeagueMatchMovedToAnotherShiaijoCannotBeReadBack(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatLeague, []string{"A", "B"}, 6, retimeTournament([]string{"A", "B"}))
	pool, err := store.LoadPoolMatches(comp.ID)
	require.NoError(t, err)
	require.Equal(t, "A", pool[2].Court)
	pool[2].Court = "B"
	require.NoError(t, store.SavePoolMatches(comp.ID, pool))
	before := storedTimes(t, store, comp)

	_, err = retimeInTx(t, eng, store, comp, movedTo(comp, comp.Date, "11:00"))
	assert.ErrorIs(t, err, ErrRetimeUnavailable)
	assert.Equal(t, before, storedTimes(t, store, comp))
}

func TestRecoverLeagueSlots_RefusesASideWithNoParticipantId(t *testing.T) {
	_, err := recoverLeagueSlots([]state.MatchResult{
		{ID: "Pool A-0", SideAID: "p0", SideBID: "", Court: "A"},
	}, []string{"A"})
	assert.ErrorIs(t, err, ErrRetimeUnavailable)
}

func TestRestoreMatchTimesTx_PutsEveryMatchBackExactly(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	before := storedTimes(t, store, comp)
	out, err := retimeInTx(t, eng, store, comp, movedTo(comp, comp.Date, "12:20"))
	require.NoError(t, err)
	require.NotEmpty(t, out.Changes)
	require.NotEqual(t, before, storedTimes(t, store, comp))

	require.NoError(t, store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		return RestoreMatchTimesTx(tx, comp, out.Changes)
	}))
	assert.Equal(t, before, storedTimes(t, store, comp))
}

func TestRestoreMatchTimesTx_RefusesWhenAMatchWasChangedSince(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	out, err := retimeInTx(t, eng, store, comp, movedTo(comp, comp.Date, "12:20"))
	require.NoError(t, err)
	pool, err := store.LoadPoolMatches(comp.ID)
	require.NoError(t, err)
	pool[0].ScheduledAt = "23:50"
	require.NoError(t, store.SavePoolMatches(comp.ID, pool))
	before := storedTimes(t, store, comp)

	err = store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		return RestoreMatchTimesTx(tx, comp, out.Changes)
	})
	assert.ErrorIs(t, err, ErrUndoStale)
	assert.Equal(t, before, storedTimes(t, store, comp), "nothing is written")
}

func TestRestoreMatchTimesTx_UnknownMatchEmptyListAndStartedCompetition(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	before := storedTimes(t, store, comp)

	err := store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		return RestoreMatchTimesTx(tx, comp, []MatchTimeChange{{MatchID: "no-such-match", Was: "09:00", Now: "10:00"}})
	})
	assert.ErrorIs(t, err, ErrRestoreUnknownMatch)

	require.NoError(t, store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		return RestoreMatchTimesTx(tx, comp, []MatchTimeChange{})
	}))

	// A time to put back must be empty or an HH:MM clock: it is written as it
	// comes, so nothing else may reach the store.
	var someID string
	for id := range before {
		someID = id
		break
	}
	for _, bad := range []string{"9am", "25:00", "09:60", "09:00:00"} {
		err = store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
			return RestoreMatchTimesTx(tx, comp, []MatchTimeChange{{MatchID: someID, Was: bad, Now: before[someID]}})
		})
		assert.ErrorIs(t, err, ErrRestoreBadTime, bad)
	}
	assert.Equal(t, before, storedTimes(t, store, comp), "nothing is written")

	// One match listed twice is ambiguous (which time goes back?): refused
	// whole, even when both entries would otherwise pass.
	err = store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		return RestoreMatchTimesTx(tx, comp, []MatchTimeChange{
			{MatchID: someID, Was: "", Now: before[someID]},
			{MatchID: someID, Was: "08:00", Now: before[someID]},
		})
	})
	assert.ErrorIs(t, err, ErrRestoreDuplicateMatch)
	assert.Equal(t, before, storedTimes(t, store, comp), "nothing is written")

	started := *comp
	started.Status = state.CompStatusPools
	err = store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		return RestoreMatchTimesTx(tx, &started, []MatchTimeChange{{MatchID: "x", Was: "a", Now: "b"}})
	})
	assert.ErrorIs(t, err, ErrRetimeNotDrawReady)
	assert.Equal(t, before, storedTimes(t, store, comp))
	_ = eng
}

// A change whose Now is empty is no change a re-time answered (every re-time
// gives every match a time): it is refused, never matched against a match that
// holds no time, which would schedule it.
func TestRestoreMatchTimesTx_RefusesAnEmptyNow(t *testing.T) {
	_, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	pool, err := store.LoadPoolMatches(comp.ID)
	require.NoError(t, err)
	require.NotEmpty(t, pool)
	pool[0].ScheduledAt = ""
	require.NoError(t, store.SavePoolMatches(comp.ID, pool))
	before := storedTimes(t, store, comp)
	require.Empty(t, before[pool[0].ID])

	err = store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		return RestoreMatchTimesTx(tx, comp, []MatchTimeChange{{MatchID: pool[0].ID, Was: "10:00", Now: ""}})
	})
	assert.ErrorIs(t, err, ErrRestoreBadTime)
	assert.Equal(t, before, storedTimes(t, store, comp), "nothing is written")
}

// The court floors are read before the competition's lock: another competition
// changed between that read and the transaction (here drawn on the same day and
// shiaijo) may hold the shiaijo the stale floors call free, so the re-time is
// refused and nothing is written.
func TestRetime_AnotherCompetitionChangedAfterTheFloorsWereReadIsRefused(t *testing.T) {
	eng, store, comp := drawnForRetime(t, state.CompFormatMixed, []string{"A", "B"}, 8, retimeTournament([]string{"A", "B"}))
	createTestCompetition(t, store, "early", state.CompFormatMixed, 4, func(c *state.Competition) {
		c.Courts = []string{"A", "B"}
		c.Date = "01-07-2026"
		c.StartTime = "08:00"
	})
	saveTestParticipants(t, store, "early", []string{"E1", "E2", "E3", "E4", "E5", "E6", "E7", "E8"})
	before := storedTimes(t, store, comp)

	newComp := movedTo(comp, comp.Date, "10:00")
	in, err := eng.ReadRetimeInputs(comp, newComp)
	require.NoError(t, err)
	require.NoError(t, eng.GenerateDraw("early"))

	err = store.WithTransaction(comp.ID, func(tx state.StoreTx) error {
		_, err := RetimeDrawTx(tx, comp, newComp, in)
		return err
	})
	assert.ErrorIs(t, err, ErrRetimeFloorChanged)
	assert.Equal(t, before, storedTimes(t, store, comp), "nothing is written")

	// Read again, the same move goes through.
	out, err := retimeInTx(t, eng, store, comp, newComp)
	require.NoError(t, err)
	assert.True(t, out.AnyChange)
}

// The slots read back from a stored league equal the ones the scheduler made,
// idle slots included.
func TestRecoverLeagueSlots_EqualsSchedulerSlots(t *testing.T) {
	for courtCount := 1; courtCount <= 4; courtCount++ {
		for players := 3; players <= 9; players++ {
			courts := []string{"A", "B", "C", "D"}[:courtCount]
			t.Run(fmt.Sprintf("%d_courts_%d_players", courtCount, players), func(t *testing.T) {
				var input []state.MatchResult
				for i := 0; i < players; i++ {
					for j := i + 1; j < players; j++ {
						input = append(input, state.MatchResult{
							ID:      fmt.Sprintf("Pool A-%d", len(input)),
							SideAID: fmt.Sprintf("p%d", i), SideBID: fmt.Sprintf("p%d", j),
							Status: state.MatchStatusScheduled,
						})
					}
				}
				ordered, slots := scheduleLeagueSlots(input, courts)
				got, err := recoverLeagueSlots(ordered, courts)
				require.NoError(t, err)
				assert.Equal(t, slots, got)
			})
		}
	}
}
