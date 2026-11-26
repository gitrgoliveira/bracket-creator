package mobileapp

// bc-sseq FR5: a write the merge held answers with the match as it is stored,
// read inside the write's own transaction, so the device that is told "Not
// applied" can show the stored match in the same render as the note. Each
// door is pinned here or beside its own superseded test (the representative
// bout's add and remove in handlers_daihyosen_test.go, the both-barred draw in
// decision_both_barred_hikiwake_test.go).

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// answerMatch reads the `match` a superseded answer carries.
func answerMatch(t *testing.T, out map[string]any) map[string]any {
	t.Helper()
	m, ok := out["match"].(map[string]any)
	require.True(t, ok, "the superseded answer carries the stored match: %v", out)
	return m
}

func TestSupersededAnswer_ScoreCarriesTheStoredMatch(t *testing.T) {
	const compID = "superseded-match-score"
	r, store := mergeServer(t, compID)
	now := time.Now().UnixMilli()
	mergeScore(t, r, compID, map[string]any{
		"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{},
		"changed": []string{"points"}, "modifiedAt": now - 10_000,
	})

	t.Run("a newer change is stored", func(t *testing.T) {
		out := mergeScore(t, r, compID, map[string]any{
			"status": "running", "ipponsA": []string{"D"}, "ipponsB": []string{},
			"changed": []string{"points"}, "modifiedAt": now - 30_000,
			"rev": 7, "revSession": "s-old",
		})
		require.Equal(t, false, out["applied"])
		m := answerMatch(t, out)
		stored := mergeStored(t, store, compID)
		assert.Equal(t, "Pool A-0", m["id"])
		assert.Equal(t, []any{"M"}, m["ipponsA"], "the stored point, not the held one")
		assert.EqualValues(t, stored.ModifiedAt, m["modifiedAt"])
		assert.EqualValues(t, stored.ScoreVersion, m["scoreVersion"], "the version the client orders copies by")
		assert.Equal(t, "A", m["court"])
		assert.NotContains(t, m, "rev", "write-ordering metadata is never echoed")
		assert.NotContains(t, m, "revSession")
	})

	t.Run("the match is closed", func(t *testing.T) {
		mergeScore(t, r, compID, map[string]any{
			"status": "completed", "ipponsA": []string{"M"}, "ipponsB": []string{}, "winner": mgAlice,
			"modifiedAt": now - 5_000,
		})
		out := mergeScore(t, r, compID, map[string]any{
			"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{"K"},
			"modifiedAt": now - 1_000,
		})
		require.Equal(t, state.HeldReasonMatchClosed, out["heldReason"])
		m := answerMatch(t, out)
		assert.Equal(t, string(state.MatchStatusCompleted), m["status"], "the finish the write lost to")
		assert.Equal(t, mgAlice, m["winner"])
		assert.Equal(t, []any{}, m["ipponsB"], "the turned-away point is not on the stored match")
	})
}

func TestSupersededAnswer_DecisionCarriesTheStoredMatch(t *testing.T) {
	const compID = "superseded-match-decision"
	r, _ := mergeServer(t, compID)
	now := time.Now().UnixMilli()
	mergeScore(t, r, compID, map[string]any{
		"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{}, "modifiedAt": now - 10_000,
	})
	w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/Pool A-0/decision", map[string]any{
		"decision": "kiken-voluntary", "decisionBy": "aka", "modifiedAt": now - 20_000,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Equal(t, false, out["applied"])
	m := answerMatch(t, out)
	assert.Equal(t, string(state.MatchStatusRunning), m["status"], "the withdrawal never landed")
	assert.Equal(t, []any{"M"}, m["ipponsA"])
	assert.Equal(t, "", m["decision"], "the stored match has no decision")
}

func TestSupersededAnswer_BulkScoreEntryCarriesTheStoredMatch(t *testing.T) {
	const compID = "superseded-match-bulk"
	r, _ := mergeServer(t, compID)
	now := time.Now().UnixMilli()
	mergeScore(t, r, compID, map[string]any{
		"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{},
		"changed": []string{"points"}, "modifiedAt": now - 10_000,
	})
	w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/bulk-score", []map[string]any{
		{"id": "Pool A-0", "sideA": mgAlice, "sideB": mgBob, "status": "running", "ipponsA": []string{"D"}, "ipponsB": []string{},
			"changed": []string{"points"}, "modifiedAt": now - 30_000},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out struct {
		Errors []struct {
			Reason string         `json:"reason"`
			Match  map[string]any `json:"match"`
		} `json:"errors"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out.Errors, 1)
	assert.Equal(t, "superseded", out.Errors[0].Reason)
	require.NotNil(t, out.Errors[0].Match, "the superseded entry carries the stored match")
	assert.Equal(t, "Pool A-0", out.Errors[0].Match["id"])
	assert.Equal(t, []any{"M"}, out.Errors[0].Match["ipponsA"])
}

// storedMatchForAnswer projects the stored match as the doors' applied answers
// are: a bracket match carries its court and schedule, and a public caller is
// answered without the audit notes, as the broadcast is.
func TestStoredMatchForAnswer_Projection(t *testing.T) {
	const compID = "superseded-match-projection"
	_, store := mergeServer(t, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID: "B1", SideA: mgAlice, SideAID: mgAliceID, SideB: mgBob, SideBID: mgBobID,
		Status: state.MatchStatusCompleted, Winner: mgAlice, WinnerID: mgAliceID,
		IpponsA: []string{"M", "K"}, Court: "A", ScheduledAt: "10:30",
		CorrectionReason: "typed on the wrong match", ModifiedAt: 1_000,
	}}}}))

	read := func(matchID string, public bool) *state.MatchResult {
		var m *state.MatchResult
		require.NoError(t, store.WithTransaction(compID, func(stx state.StoreTx) error {
			m = storedMatchForAnswer(stx, compID, matchID, public)
			return nil
		}))
		return m
	}

	t.Run("a bracket match carries its court and schedule", func(t *testing.T) {
		m := read("B1", false)
		require.NotNil(t, m)
		assert.Equal(t, "A", m.Court)
		assert.Equal(t, "10:30", m.ScheduledAt)
		assert.Equal(t, []string{"M", "K"}, m.IpponsA)
		assert.Equal(t, mgAliceID, m.WinnerID)
		assert.EqualValues(t, 1_000, m.ModifiedAt)
		assert.Equal(t, "typed on the wrong match", m.CorrectionReason, "the organiser reads the audit note")
	})

	t.Run("a public caller is answered without the audit notes", func(t *testing.T) {
		m := read("B1", true)
		require.NotNil(t, m)
		assert.Empty(t, m.CorrectionReason)
		assert.Equal(t, "A", m.Court)
	})

	t.Run("a pool match", func(t *testing.T) {
		m := read("Pool A-0", false)
		require.NotNil(t, m)
		assert.Equal(t, "A", m.Court)
		assert.Equal(t, mgAliceID, m.SideAID)
	})

	t.Run("an unknown match answers none", func(t *testing.T) {
		assert.Nil(t, read("B9", false))
	})
}
