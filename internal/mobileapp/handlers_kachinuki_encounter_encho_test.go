package mobileapp

// bc-kheb (operator ruling 2026-09-24): a kachinuki encounter carries no
// match-level overtime. A decision on one is the write that computes the
// winner's default-win circles FROM the encho it is given, before the engine's
// write chokepoint strips it, so the handler drops an older client's encho
// first: the winner gets the two regulation circles, and nothing stores (E)
// on the encounter.

import (
	"net/http"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDecisionHandler_KachinukiDropsEncounterEncho(t *testing.T) {
	compID := "kachinuki-decision-encho"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought",
				Encho: &state.EnchoMetadata{PeriodCount: 1}},
		},
	}}))

	w := postDecision(t, r, compID, "P1-0", map[string]any{
		"decision": "fusenpai", "decisionBy": "aka",
		"encho": map[string]any{"periodCount": 1},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Nil(t, m.Encho, "a kachinuki encounter stores no match-level overtime")
	winnerIppons := m.IpponsB
	if m.Winner == m.SideA {
		winnerIppons = m.IpponsA
	}
	assert.Equal(t, domain.DefaultWinIppons(false), winnerIppons,
		"the default win is the regulation two circles, not the one an encounter encho would give")
	require.Len(t, m.SubResults, 1)
	require.NotNil(t, m.SubResults[0].Encho, "the bout keeps its own overtime")
	assert.Equal(t, 1, m.SubResults[0].Encho.PeriodCount)
}
