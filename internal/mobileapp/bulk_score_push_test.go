package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A bulk-score batch larger than bulkScorePushEachMax pushes one
// match_updated naming only the competition, not one per entry, so a slow
// subscriber is not handed hundreds of frames; a small batch pushes one per
// landed entry.
func TestBulkScore_LargeBatchPushesOnce(t *testing.T) {
	for _, n := range []int{bulkScorePushEachMax, bulkScorePushEachMax + 1} {
		t.Run(fmt.Sprintf("%d entries", n), func(t *testing.T) {
			r, store, _, hub, tempDir := setupTestRouter(t)
			defer os.RemoveAll(tempDir)
			require.NoError(t, store.SaveCompetition(&state.Competition{ID: "bulkpush", Status: "pools", Courts: []string{"A"}}))
			matches := make([]state.MatchResult, n)
			entries := make([]map[string]any, n)
			for i := range matches {
				id := fmt.Sprintf("Pool A-%d", i)
				matches[i] = state.MatchResult{ID: id, SideA: "P1", SideB: "P2", Status: state.MatchStatusRunning}
				entries[i] = map[string]any{"id": id, "sideA": "P1", "sideB": "P2", "status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{}}
			}
			require.NoError(t, store.SavePoolMatches("bulkpush", matches))
			ch := hub.Subscribe()
			defer hub.Unsubscribe(ch)

			payload, _ := json.Marshal(entries)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions/bulkpush/matches/bulk-score", bytes.NewBuffer(payload))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			require.EqualValues(t, n, body["succeeded"], w.Body.String())

			named, bare := 0, 0
			for done := false; !done; {
				select {
				case msg := <-ch:
					evt := decodeHubEvent(t, msg)
					if evt.Type != EventMatchUpdated {
						continue
					}
					data, _ := evt.Data.(map[string]any)
					if _, ok := data["matchId"]; ok {
						named++
					} else {
						bare++
					}
				default:
					done = true
				}
			}
			if n > bulkScorePushEachMax {
				assert.Zero(t, named, "no push per entry")
				assert.Equal(t, 1, bare, "one push naming the competition")
			} else {
				assert.Equal(t, n, named, "one push per landed entry")
			}
		})
	}
}
