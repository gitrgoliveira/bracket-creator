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
