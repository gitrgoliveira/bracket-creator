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
