package engine

// bc-kheb (operator ruling 2026-09-24): one kachinuki bout fought on in encho
// does not put the encounter in overtime. (E) lives on that bout's own row
// (SubMatchResult.Encho), and the encounter's match-level Encho is never
// stored: the write chokepoint every production score write reaches
// (RecordMatchResultWithIneligibilityTx -> applyKachinukiMerge) strips it on
// both homes, and a fixed-order team match keeps its own.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// enchoWrite is a running write whose bout 1 was won in one period of encho,
// carrying the match-level count an older client mirrored from it.
func enchoWrite(id string) *state.MatchResult {
	return &state.MatchResult{
		ID: id, SideA: wrTeamA, SideB: wrTeamB, Status: state.MatchStatusRunning,
		Encho: &state.EnchoMetadata{PeriodCount: 2},
		SubResults: []state.SubMatchResult{{
			Position: 1, SideA: "r1", SideB: "t1", Winner: "r1", IpponsA: []string{"M"},
			Encho: &state.EnchoMetadata{PeriodCount: 1},
		}},
	}
}

// markerSet keeps the load repair out of these tests, so only the write
// path can be what clears the field.
func markerSet(c *state.Competition) { c.KachinukiEncounterEnchoCleared = true }

func TestRecordMatchResultWithIneligibility_KachinukiDropsEncounterEncho(t *testing.T) {
	t.Run("pool", func(t *testing.T) {
		const compID = "kenc-pool"
		eng, store, _ := setupKachinukiComp(t, compID, 3, markerSet)
		wrSaveTeams(t, store, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusRunning,
		}}))
		_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", enchoWrite("Pool A-0"))
		require.NoError(t, err)

		m := wrPoolMatch(t, store, compID)
		assert.Nil(t, m.Encho, "a kachinuki encounter stores no match-level overtime")
		require.Len(t, m.SubResults, 1)
		require.NotNil(t, m.SubResults[0].Encho, "the bout keeps its own overtime")
		assert.Equal(t, 1, m.SubResults[0].Encho.PeriodCount)
	})

	t.Run("bracket", func(t *testing.T) {
		const compID = "kenc-ko"
		eng, store, _ := setupKachinukiComp(t, compID, 3, markerSet)
		wrSaveTeams(t, store, compID)
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
			ID: "m-r1-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusRunning,
		}}}}))
		_, err := eng.RecordMatchResultWithIneligibility(compID, "m-r1-0", enchoWrite("m-r1-0"))
		require.NoError(t, err)

		bracket, err := store.LoadBracket(compID)
		require.NoError(t, err)
		bm := bracket.Rounds[0][0]
		assert.Nil(t, bm.Encho, "a kachinuki encounter stores no match-level overtime")
		require.Len(t, bm.SubResults, 1)
		require.NotNil(t, bm.SubResults[0].Encho, "the bout keeps its own overtime")
		assert.Equal(t, 1, bm.SubResults[0].Encho.PeriodCount)
	})

	// Scoping guard: the rule is kachinuki's. A fixed-order team match's
	// match-level overtime is stored as before.
	t.Run("fixed-order team match keeps it", func(t *testing.T) {
		const compID = "kenc-fixed"
		eng, store, _ := setupKachinukiComp(t, compID, 3, func(c *state.Competition) {
			c.TeamMatchType = state.TeamMatchTypeFixed
		})
		wrSaveTeams(t, store, compID)
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusRunning,
		}}))
		w := enchoWrite("Pool A-0")
		w.SubResults[0].Encho = nil // numbered-bout encho is kachinuki's; this pins the match level only
		_, err := eng.RecordMatchResultWithIneligibility(compID, "Pool A-0", w)
		require.NoError(t, err)

		m := wrPoolMatch(t, store, compID)
		require.NotNil(t, m.Encho)
		assert.Equal(t, 2, m.Encho.PeriodCount)
	})
}
