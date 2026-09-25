package engine

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestBracketOrderOfPlayFollowsMatchNumbers: on every shiaijo a freshly drawn
// knockout is scheduled in match-number order, so Match 1 is played before
// Match 2 and a round before the next. The storage order is not the order of
// play: in a five-player draw the pair beside the empty pair sits in the
// first storage row but is a semifinal, numbered after the quarterfinal.
func TestBracketOrderOfPlayFollowsMatchNumbers(t *testing.T) {
	for n := 3; n <= 24; n++ {
		for _, courts := range []int{1, 2, 4} {
			t.Run(fmt.Sprintf("%d_entrants_%d_shiaijo", n, courts), func(t *testing.T) {
				eng, store, _ := setupTestEngine(t)
				const compID = "order-of-play"
				createTestCompetition(t, store, compID, state.CompFormatKnockout, 0, func(c *state.Competition) {
					c.Courts = courtLabels(courts)
				})
				require.NoError(t, store.SaveParticipants(compID, makePlayers(n)))
				require.NoError(t, eng.GenerateDraw(compID))
				bracket, err := store.LoadBracket(compID)
				require.NoError(t, err)

				byNumber := map[int]*state.BracketMatch{}
				maxNumber := 0
				for ri := range bracket.Rounds {
					for mi := range bracket.Rounds[ri] {
						if m := &bracket.Rounds[ri][mi]; m.MatchNumber > 0 && m.Status != state.MatchStatusCompleted {
							byNumber[m.MatchNumber] = m
							maxNumber = max(maxNumber, m.MatchNumber)
						}
					}
				}
				require.NotEmpty(t, byNumber, "the draw holds matches to play")
				last := map[string]*state.BracketMatch{}
				for num := 1; num <= maxNumber; num++ {
					m, ok := byNumber[num]
					if !ok {
						continue
					}
					if prev := last[m.Court]; prev != nil {
						require.Less(t, prev.ScheduledAt, m.ScheduledAt,
							"shiaijo %s: Match %d (%s) must be scheduled after Match %d (%s)",
							m.Court, m.MatchNumber, m.ScheduledAt, prev.MatchNumber, prev.ScheduledAt)
					}
					last[m.Court] = m
				}
			})
		}
	}
}

// TestBracketTimesTheOperatorMovedSurviveAReload: moving a match up the court
// queue swaps two times (UpdateMatchTime) and marks nothing else on the
// match. In a five-player draw, moving Match 2 above Match 1 leaves the
// court's times rising in storage order, which is exactly what the old
// scheduler left behind. The draw records its bracket as TimesSettled, so
// the load-time repair of that old scheduling never takes the move back.
func TestBracketTimesTheOperatorMovedSurviveAReload(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	const compID = "moved-times"
	createTestCompetition(t, store, compID, state.CompFormatKnockout, 0, func(c *state.Competition) {
		c.Courts = courtLabels(1)
	})
	require.NoError(t, store.SaveParticipants(compID, makePlayers(5)))
	require.NoError(t, eng.GenerateDraw(compID))
	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.True(t, bracket.TimesSettled, "a draw scheduled in match-number order owes the repair nothing")

	var m1, m2 *state.BracketMatch
	for ri := range bracket.Rounds {
		for mi := range bracket.Rounds[ri] {
			switch m := &bracket.Rounds[ri][mi]; m.MatchNumber {
			case 1:
				m1 = m
			case 2:
				m2 = m
			}
		}
	}
	require.NotNil(t, m1)
	require.NotNil(t, m2)
	first, second := m1.ScheduledAt, m2.ScheduledAt
	require.Less(t, first, second, "fixture: Match 1 is played first")

	// The operator moves Match 2 up the queue: the console swaps the times.
	require.NoError(t, eng.UpdateMatchTime(compID, m2.ID, first))
	require.NoError(t, eng.UpdateMatchTime(compID, m1.ID, second))

	reloaded, err := state.NewStore(dir)
	require.NoError(t, err)
	got, err := reloaded.LoadBracket(compID)
	require.NoError(t, err)
	byID := map[string]string{}
	for ri := range got.Rounds {
		for _, m := range got.Rounds[ri] {
			byID[m.ID] = m.ScheduledAt
		}
	}
	require.Equal(t, first, byID[m2.ID], "Match 2 keeps the time it was moved to")
	require.Equal(t, second, byID[m1.ID], "Match 1 keeps the time it was moved to")
}
