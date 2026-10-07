package mobileapp

// bc-kheb (operator ruling 2026-09-24): a kachinuki encounter carries no
// match-level overtime. A decision on one is the write that computes the
// winner's default-win circles FROM the encho it is given, before the engine's
// write chokepoint strips it, so the engine's decision write drops an older
// client's encho first: the winner gets the two regulation circles, and
// nothing stores (E) on the encounter.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
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

// A kachinuki competition created by this release starts with the load
// repair's marker set, since its writes never store encounter overtime; any
// other competition starts without it, so one switched to kachinuki later is
// still repaired once.
func TestCreateCompetition_KachinukiStartsWithTheEnchoRepairMarker(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	for _, tc := range []struct {
		id     string
		body   map[string]any
		marked bool
	}{
		{"create-kachinuki", map[string]any{"id": "create-kachinuki", "name": "Create Kachinuki", "kind": "team",
			"teamSize": 3, "teamMatchType": state.TeamMatchTypeKachinuki, "format": state.CompFormatKnockout}, true},
		{"create-fixed-order", map[string]any{"id": "create-fixed-order", "name": "Create Fixed Order", "kind": "team",
			"teamSize": 3, "format": state.CompFormatKnockout}, false},
	} {
		body, err := json.Marshal(tc.body)
		require.NoError(t, err)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		comp, err := store.LoadCompetition(tc.id)
		require.NoError(t, err)
		assert.Equal(t, tc.marked, comp.KachinukiEncounterEnchoCleared, tc.id)
	}
}
