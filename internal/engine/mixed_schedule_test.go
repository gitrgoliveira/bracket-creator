package engine

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// numberedKnockoutTimes returns the ScheduledAt of every numbered knockout
// match, by court.
func numberedKnockoutTimes(b *state.Bracket) map[string][]time.Time {
	out := map[string][]time.Time{}
	for _, r := range b.Rounds {
		for _, m := range r {
			if m.MatchNumber > 0 && m.ScheduledAt != "" {
				out[m.Court] = append(out[m.Court], parseClockHHMM(m.ScheduledAt))
			}
		}
	}
	return out
}

func drawSchedule(t *testing.T, format string, courts []string, names []string) ([]state.MatchResult, *state.Bracket, *state.Competition, *state.Tournament) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	id := "bc-kosc-" + format
	createTestCompetition(t, store, id, format, 3, func(c *state.Competition) { c.Courts = courts })
	saveTestParticipants(t, store, id, names)
	require.NoError(t, eng.GenerateDraw(id))
	pm, err := store.LoadPoolMatches(id)
	require.NoError(t, err)
	b, err := store.LoadBracket(id)
	require.NoError(t, err)
	require.NotNil(t, b)
	comp, err := store.LoadCompetition(id)
	require.NoError(t, err)
	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	return pm, b, comp, tourn
}

var kosc6 = []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"}

// bc-kosc: in a pools + knockout draw every knockout bout is timed after its
// court's last pool bout.
func TestGenerateDraw_Mixed_KnockoutScheduledAfterPools(t *testing.T) {
	for name, courts := range map[string][]string{"one court": {"A"}, "two courts": {"A", "B"}} {
		t.Run(name, func(t *testing.T) {
			pm, b, comp, tourn := drawSchedule(t, state.CompFormatMixed, courts, kosc6)
			ends := poolPhaseEndByCourt(pm, comp, tourn)
			require.NotEmpty(t, ends)
			ko := numberedKnockoutTimes(b)
			require.NotEmpty(t, ko)
			for court, times := range ko {
				end, ok := ends[court]
				require.Truef(t, ok, "court %s has knockout bouts but no pool bouts", court)
				for _, ts := range times {
					assert.Falsef(t, ts.Before(end), "court %s knockout at %s starts before its pools end at %s", court, ts.Format("15:04"), end.Format("15:04"))
				}
			}
			assert.Lenf(t, ends, len(courts), "every court should hold pool bouts")
		})
	}
}

// Knockout-only competitions keep the day start.
func TestGenerateDraw_KnockoutOnly_StartsAtDayStart(t *testing.T) {
	_, b, _, _ := drawSchedule(t, state.CompFormatKnockout, []string{"A"}, kosc6)
	first := ""
	for _, r := range b.Rounds {
		for _, m := range r {
			if m.MatchNumber == 1 {
				first = m.ScheduledAt
			}
		}
	}
	assert.Equal(t, "09:00", first)
}

// bc-kosc: a tie-break bout added after the draw is appended after its
// court's last pool bout, where the knockout used to start; the knockout
// moves past it.
func TestInjectTiebreaker_MovesTheKnockoutPastTheNewBout(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	id := "bc-kosc-tb"
	createTestCompetition(t, store, id, state.CompFormatMixed, 3, func(c *state.Competition) { c.Courts = []string{"A"} })
	saveTestParticipants(t, store, id, kosc6)
	require.NoError(t, eng.GenerateDraw(id))

	pm, err := store.LoadPoolMatches(id)
	require.NoError(t, err)
	pool := ""
	for _, m := range pm {
		pn, ok := poolNameFromMatchID(m.ID)
		require.True(t, ok)
		if pool == "" {
			pool = pn
		}
		if pn != pool {
			continue
		}
		found, err := store.UpdatePoolMatchByID(id, m.ID, func(r *state.MatchResult) error {
			r.Status = state.MatchStatusCompleted
			r.Decision = "hikiwake"
			return nil
		})
		require.NoError(t, err)
		require.True(t, found)
	}

	injected, err := eng.InjectTiebreakerMatches(id)
	require.NoError(t, err)
	require.NotEmpty(t, injected, "a three-way draw for first needs a tie-break")

	pm, err = store.LoadPoolMatches(id)
	require.NoError(t, err)
	comp, err := store.LoadCompetition(id)
	require.NoError(t, err)
	tourn, err := store.LoadTournament()
	require.NoError(t, err)
	end := poolPhaseEndByCourt(pm, comp, tourn)["A"]
	b, err := store.LoadBracket(id)
	require.NoError(t, err)
	times := numberedKnockoutTimes(b)["A"]
	require.NotEmpty(t, times)
	for _, ts := range times {
		assert.Falsef(t, ts.Before(end), "knockout at %s starts before the tie-break ends at %s", ts.Format("15:04"), end.Format("15:04"))
	}
}

// pushKnockoutPastPools leaves a court alone when its knockout already starts
// after its pools (a time the operator set survives) and when a knockout
// match there has started; otherwise it keeps the order and gaps.
func TestPushKnockoutPastPools(t *testing.T) {
	comp := &state.Competition{StartTime: "09:00", KnockoutMatchDurationSeconds: 240, PoolMatchDurationSeconds: 240, Courts: []string{"A", "B", "C"}}
	pool := func(court, at string) state.MatchResult {
		return state.MatchResult{ID: "p", Court: court, ScheduledAt: at}
	}
	ko := func(id, court, at string, n int, st state.MatchStatus) state.BracketMatch {
		return state.BracketMatch{ID: id, Court: court, ScheduledAt: at, MatchNumber: n, Status: st}
	}
	b := &state.Bracket{Rounds: [][]state.BracketMatch{{
		ko("a2", "A", "10:30", 2, state.MatchStatusScheduled),
		ko("a1", "A", "10:00", 1, state.MatchStatusScheduled),
		ko("b1", "B", "11:00", 3, state.MatchStatusScheduled),
		ko("c1", "C", "09:30", 4, state.MatchStatusRunning),
		ko("c2", "C", "09:36", 5, state.MatchStatusScheduled),
	}}}
	// One pool slot is 4 * 1.5 = 6 minutes: A's pools end 10:06, B's 10:06.
	pm := []state.MatchResult{pool("A", "10:00"), pool("B", "10:00"), pool("C", "10:00")}
	require.True(t, pushKnockoutPastPools(b, comp, nil, pm))
	got := map[string]string{}
	for _, m := range b.Rounds[0] {
		got[m.ID] = m.ScheduledAt
	}
	assert.Equal(t, "10:06", got["a1"], "moved to the pool end")
	assert.Equal(t, "10:30", got["a2"], "already clear of a1, keeps its time")
	assert.Equal(t, "11:00", got["b1"], "already after its pools: left as set")
	assert.Equal(t, "09:36", got["c2"], "a court whose knockout has started is not touched")

	assert.False(t, pushKnockoutPastPools(b, comp, nil, pm), "nothing left to move")
}
