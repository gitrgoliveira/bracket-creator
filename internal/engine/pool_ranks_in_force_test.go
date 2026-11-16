package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestCalculatePoolStandings_LotsGiveWayToADecidedOrder: a drawing of lots
// orders only a tie that is still level on points and that no fought bout has
// settled (poolRanksInForce). Gamma and Delta drew for 3rd and the lots put
// Delta 3rd; when a correction or a re-fought representative bout decides the
// order instead, the lots are ignored, nothing is marked overridden, and the
// result stands.
func TestCalculatePoolStandings_LotsGiveWayToADecidedOrder(t *testing.T) {
	order := func(t *testing.T, eng *Engine, compID string) ([]string, map[string]bool) {
		t.Helper()
		eng.standingsCache.Delete(compID)
		eng.standingsFlight.Delete(compID)
		st, err := eng.CalculatePoolStandings(compID)
		require.NoError(t, err)
		var names []string
		overridden := map[string]bool{}
		for _, s := range st["Pool A"] {
			names = append(names, s.Player.Name)
			if s.IsOverridden {
				overridden[s.Player.Name] = true
			}
		}
		return names, overridden
	}
	lotsDeltaFirst := func(t *testing.T, store *state.Store, compID string) {
		t.Helper()
		pools, err := store.LoadPools(compID)
		require.NoError(t, err)
		ids := map[string]string{}
		for _, p := range pools[0].Players {
			ids[p.Name] = p.ID
		}
		_, err = store.SaveRankOverridesChanged(compID, "Pool A", map[string]int{ids["Delta"]: 3, ids["Gamma"]: 4})
		require.NoError(t, err)
	}
	teams := []string{"Alpha", "Beta", "Gamma", "Delta"}

	t.Run("while the tie stands the lots order it", func(t *testing.T) {
		const compID = "lots-in-force"
		matches := append(fourTeamOneTiedPair(), teamPoolMatch("Pool A-DH-0", "A", "Gamma", "Delta", ""))
		eng, store := setupTeamPoolWinners(t, compID, teams, 4, matches)
		lotsDeltaFirst(t, store, compID)
		names, overridden := order(t, eng, compID)
		assert.Equal(t, []string{"Alpha", "Beta", "Delta", "Gamma"}, names)
		assert.Equal(t, map[string]bool{"Delta": true, "Gamma": true}, overridden)
	})

	t.Run("a correction that splits the tie", func(t *testing.T) {
		const compID = "lots-after-correction"
		matches := fourTeamOneTiedPair()
		matches[5] = teamPoolMatch("Pool A-5", "A", "Gamma", "Delta", "Gamma") // the draw corrected to a Gamma win
		eng, store := setupTeamPoolWinners(t, compID, teams, 4, matches)
		lotsDeltaFirst(t, store, compID)
		names, overridden := order(t, eng, compID)
		assert.Equal(t, []string{"Alpha", "Beta", "Gamma", "Delta"}, names, "the corrected result decides 3rd, not the lots drawn for the old tie")
		assert.Empty(t, overridden)
	})

	t.Run("a re-fought representative bout that names a winner", func(t *testing.T) {
		const compID = "lots-after-refought-bout"
		matches := append(fourTeamOneTiedPair(), teamPoolMatch("Pool A-DH-0", "A", "Gamma", "Delta", "Gamma"))
		eng, store := setupTeamPoolWinners(t, compID, teams, 4, matches)
		lotsDeltaFirst(t, store, compID)
		names, overridden := order(t, eng, compID)
		assert.Equal(t, []string{"Alpha", "Beta", "Gamma", "Delta"}, names, "the fought bout decides 3rd, not the lots drawn when it was drawn")
		assert.Empty(t, overridden)
	})
}
