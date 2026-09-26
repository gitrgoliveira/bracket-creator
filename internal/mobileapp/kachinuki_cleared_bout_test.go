package mobileapp

import (
	"net/http"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-kclr: the server half of taking a mark back on a kachinuki bout. The
// merge (engine.mergeKachinukiSubResults) keeps any stored row a payload
// omits, so the editor now sends a bout the operator cleared as an explicit
// empty row (kachinukiRowCleared, admin_scoring_team.jsx). These pin that such
// a row is stored as cleared, on a running write and on End match.

func TestScoreHandler_KachinukiClearedBoutIsStored(t *testing.T) {
	compID := "kachinuki-cleared-bout"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}}},
	}}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA": "Ryu", "sideB": "Tora", "status": "running",
		"subResults": []map[string]any{kachinukiSub(1, "R-1", "W-1", []string{}, "", "")},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	m := loadPoolMatch(t, store, compID, "P1-0")
	require.Len(t, m.SubResults, 1)
	assert.Empty(t, m.SubResults[0].IpponsA, "the point taken back must not survive the write")
	assert.Equal(t, "R-1", m.SubResults[0].SideA, "the pairing stays")
}

func TestScoreHandler_KachinukiEndAfterClearingTheCurrentBout(t *testing.T) {
	compID := "kachinuki-end-after-clear"
	r, store := setupKachinukiScoreServer(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M", "K"}, Winner: "R-1", Decision: "fought"},
			// A stray point on the current bout, autosaved, then taken back.
			{Position: 2, SideA: "R-1", SideB: "W-2", IpponsB: []string{"K"}},
		},
	}}))

	w := putScore(t, r, compID, "P1-0", map[string]any{
		"sideA": "Ryu", "sideB": "Tora", "winner": "Ryu", "status": "completed", "decision": "kachinuki-exhaustion",
		"subResults": []map[string]any{
			kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought"),
			kachinukiSub(2, "R-1", "W-2", []string{}, "", ""),
		},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	m := loadPoolMatch(t, store, compID, "P1-0")
	assert.Equal(t, state.MatchStatusCompleted, m.Status)
	assert.Equal(t, "Ryu", m.Winner)
	require.Len(t, m.SubResults, 1, "the cleared current bout is an unscored trailing row, stripped on End match")
	assert.Equal(t, 1, m.SubResults[0].Position)
}
