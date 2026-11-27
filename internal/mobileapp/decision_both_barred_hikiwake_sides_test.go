package mobileapp

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDecisionHandler_BothSidesBarredHikiwake_SideCheck pins that the
// both-barred draw judges the sides its writer saw like every other decision:
// a pairing that differs from the stored one (a draw swap since) is refused
// 409 side_mismatch and writes nothing, and the current pairing is recorded.
func TestDecisionHandler_BothSidesBarredHikiwake_SideCheck(t *testing.T) {
	poolMatch := func(t *testing.T, store *state.Store, compID string) state.MatchResult {
		t.Helper()
		matches, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range matches {
			if m.ID == "Pool A-2" {
				return m
			}
		}
		t.Fatal("Pool A-2 not found")
		return state.MatchResult{}
	}

	t.Run("a stale pairing is refused and nothing is written", func(t *testing.T) {
		compID := "both-barred-hikiwake-stale"
		r, store, _ := bothBarredPoolFixture(t, compID)
		m := poolMatch(t, store, compID)

		resp := postDecisionJSON(t, r, compID, "Pool A-2", DecisionRequest{
			Decision: "hikiwake", DecisionReason: "both withdrawn",
			SideAID: "swapped-in", SideBID: m.SideBID,
		})
		require.Equal(t, http.StatusConflict, resp.Code, resp.Body.String())
		var body map[string]any
		require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
		assert.Equal(t, "side_mismatch", body["error"])

		after := poolMatch(t, store, compID)
		assert.Equal(t, state.MatchStatusScheduled, after.Status, "a refused draw records nothing")
		assert.Empty(t, after.Decision)
		statuses, err := store.LoadCompetitorStatus(compID)
		require.NoError(t, err)
		assert.Len(t, statuses, 2, "no eligibility record is added")
	})

	t.Run("the current pairing is recorded", func(t *testing.T) {
		compID := "both-barred-hikiwake-current"
		r, store, _ := bothBarredPoolFixture(t, compID)
		m := poolMatch(t, store, compID)

		resp := postDecisionJSON(t, r, compID, "Pool A-2", DecisionRequest{
			Decision: "hikiwake", DecisionReason: "both withdrawn",
			SideAID: m.SideAID, SideBID: m.SideBID,
		})
		require.Equal(t, http.StatusOK, resp.Code, resp.Body.String())
		after := poolMatch(t, store, compID)
		assert.Equal(t, state.MatchStatusCompleted, after.Status)
		assert.Equal(t, "hikiwake", after.Decision)
	})
}
