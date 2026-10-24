package mobileapp

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The push the kachinuki advance makes goes through the one match push door
// (broadcastMatchNow): it carries the stored match as the advance left it, with
// the appended bout, and is the LAST match_updated of the write, so a running
// push held in the window can never follow it with the older bout log.
func TestKachinukiAdvancePush_CarriesTheStoredMatchAndIsLast(t *testing.T) {
	const compID = "kachinuki-advance-push"
	r, store, hub := setupKachinukiScoreServerWithHub(t, compID)
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-0", SideA: "Ryu", SideB: "Tora", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1"}},
	}}))

	events := broadcastsDuring(t, hub, func() {
		w := putScore(t, r, compID, "P1-0", map[string]any{
			"sideA": "Ryu", "sideB": "Tora", "status": "running", "kachinukiBoutFinal": true,
			"subResults": []map[string]any{kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought")},
		})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
	var updates []string
	for _, e := range events {
		if strings.Contains(e, `"type":"match_updated"`) {
			updates = append(updates, e)
		}
	}
	require.NotEmpty(t, updates)
	last := updates[len(updates)-1]
	assert.Contains(t, last, `"matchId":"P1-0"`)
	assert.Contains(t, last, `"result"`, "the push carries the stored match")
	assert.Contains(t, last, `"position":2`, "the last push shows the bout the advance appended")
}
