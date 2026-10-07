package mobileapp

// bc-mrgc: the write doors' answers to the merge (engine.mergeMatchWrite).
// A write applied in part answers as applied plus heldGroups; one whose every
// change was held keeps the 200 {applied:false, reason:"superseded"} shape the
// SPA's queue and banners key on, plus heldGroups; and either way the held
// values are in the match's history, which a superseded write's transaction
// must commit.

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

const (
	mgAlice   = "Alice"
	mgBob     = "Bob"
	mgAliceID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	mgBobID   = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

// mergeServer is a running individual pool match on court A, Alice v Bob,
// plus a second match on the same court.
func mergeServer(t *testing.T, compID string) (http.Handler, *state.Store) {
	t.Helper()
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual", Format: state.CompFormatLeague,
		Status: state.CompStatusPools, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: mgAliceID, Name: mgAlice, Dojo: "D1"},
		{ID: mgBobID, Name: mgBob, Dojo: "D2"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: mgAlice, SideAID: mgAliceID, SideB: mgBob, SideBID: mgBobID, Court: "A", Status: state.MatchStatusRunning},
		{ID: "Pool A-1", SideA: mgBob, SideAID: mgBobID, SideB: mgAlice, SideBID: mgAliceID, Court: "A", Status: state.MatchStatusScheduled},
	}))
	runningRevStore.Delete(compID + ":Pool A-0")
	return r, store
}

func mergeScore(t *testing.T, r http.Handler, compID string, body map[string]any) map[string]any {
	t.Helper()
	body["sideA"], body["sideB"] = mgAlice, mgBob
	w := serveJSON(r, "PUT", "/api/competitions/"+compID+"/matches/Pool A-0/score", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func mergeStored(t *testing.T, store *state.Store, compID string) state.MatchResult {
	t.Helper()
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range ms {
		if m.ID == "Pool A-0" {
			return m
		}
	}
	t.Fatal("Pool A-0 not found")
	return state.MatchResult{}
}

func TestScoreHandler_AnswersWhatTheMergeHeld(t *testing.T) {
	const compID = "merge-score"
	r, store := mergeServer(t, compID)
	now := time.Now().UnixMilli()

	mergeScore(t, r, compID, map[string]any{
		"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{},
		"changed": []string{"points"}, "modifiedAt": now - 10_000,
	})

	t.Run("applied in part", func(t *testing.T) {
		out := mergeScore(t, r, compID, map[string]any{
			"status": "running", "ipponsA": []string{"K"}, "ipponsB": []string{},
			"encho":   map[string]any{"periodCount": 1},
			"changed": []string{"points", "encho"}, "modifiedAt": now - 20_000,
		})
		assert.NotContains(t, out, "applied", "it applied: the answer is the stored result")
		assert.Equal(t, []any{"points"}, out["heldGroups"])
		m := mergeStored(t, store, compID)
		assert.Equal(t, []string{"M"}, m.IpponsA, "the newer point stands")
		assert.True(t, m.Encho.On(), "the overtime applied")
	})

	t.Run("everything held", func(t *testing.T) {
		out := mergeScore(t, r, compID, map[string]any{
			"status": "running", "ipponsA": []string{"D"}, "ipponsB": []string{},
			"changed": []string{"points"}, "modifiedAt": now - 30_000,
		})
		assert.Equal(t, false, out["applied"])
		assert.Equal(t, "superseded", out["reason"])
		assert.Equal(t, []any{"points"}, out["heldGroups"])
		assert.Contains(t, out["message"], "kept in the match's history")
		history, err := store.LoadMatchHistory(compID, "Pool A-0")
		require.NoError(t, err)
		require.Len(t, history, 3, "the superseded write's transaction committed its entry")
		assert.Equal(t, state.HistoryOutcomeHeld, history[2].Outcomes["points"])
		assert.JSONEq(t, `{"ipponsA":["D"],"ipponsB":[],"hansokuA":0,"hansokuB":0}`, string(history[2].Held["points"]))
	})

	t.Run("an unknown group is refused", func(t *testing.T) {
		body := map[string]any{"sideA": mgAlice, "sideB": mgBob, "status": "running", "changed": []string{"pointz"}}
		w := serveJSON(r, "PUT", "/api/competitions/"+compID+"/matches/Pool A-0/score", body)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	})
}

// R3 through the door: a correction typed on a board after the match
// finished applies to the finished match, which stays finished and takes no
// court, even with the court's next match running.
func TestScoreHandler_RunningCorrectionAfterTheFinishApplies(t *testing.T) {
	const compID = "merge-r3"
	r, store := mergeServer(t, compID)
	now := time.Now().UnixMilli()
	mergeScore(t, r, compID, map[string]any{
		"status": "completed", "ipponsA": []string{"M"}, "ipponsB": []string{}, "winner": mgAlice,
		"modifiedAt": now - 20_000,
	})
	// The court's next match is under way.
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	ms[1].Status = state.MatchStatusRunning
	require.NoError(t, store.SavePoolMatches(compID, ms))

	out := mergeScore(t, r, compID, map[string]any{
		"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{"K", "D"},
		"modifiedAt": now - 10_000,
	})
	assert.NotContains(t, out, "applied", "no court_busy, no stale: it applied; got %v", out)
	m := mergeStored(t, store, compID)
	assert.Equal(t, state.MatchStatusCompleted, m.Status, "the match stays finished")
	assert.Equal(t, mgBob, m.Winner, "worked out again from the points")
	assert.Equal(t, mgBobID, m.WinnerID)
}

// A decision made before a point that is already stored is held whole, and
// the answer names the groups.
func TestDecisionHandler_HeldDecisionAnswersHeldGroups(t *testing.T) {
	const compID = "merge-decision"
	r, store := mergeServer(t, compID)
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
	assert.Equal(t, false, out["applied"])
	// encho is not listed: the decision carries no overtime over a match with
	// none, an echo of the stored value and no loss (bc-mrgc phase 3).
	assert.ElementsMatch(t, []any{"result", "points"}, out["heldGroups"])
	m := mergeStored(t, store, compID)
	assert.Equal(t, state.MatchStatusRunning, m.Status)
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	st, barred := statuses[mgAliceID]
	assert.True(t, !barred || st.Eligible, "the withdrawal never landed")
	history, err := store.LoadMatchHistory(compID, "Pool A-0")
	require.NoError(t, err)
	require.NotEmpty(t, history)
	assert.Equal(t, "decision", history[len(history)-1].Door)
}

func TestBulkScore_ReportsHeldGroups(t *testing.T) {
	const compID = "merge-bulk"
	r, store := mergeServer(t, compID)
	now := time.Now().UnixMilli()
	mergeScore(t, r, compID, map[string]any{
		"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{},
		"changed": []string{"points"}, "modifiedAt": now - 10_000,
	})
	w := serveJSON(r, "POST", "/api/competitions/"+compID+"/matches/bulk-score", []map[string]any{
		{"id": "Pool A-0", "sideA": mgAlice, "sideB": mgBob, "status": "running", "ipponsA": []string{"K"}, "ipponsB": []string{},
			"encho": map[string]any{"periodCount": 1}, "changed": []string{"points", "encho"}, "modifiedAt": now - 20_000},
		{"id": "Pool A-0", "sideA": mgAlice, "sideB": mgBob, "status": "running", "ipponsA": []string{"D"}, "ipponsB": []string{},
			"changed": []string{"points"}, "modifiedAt": now - 30_000},
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out struct {
		Succeeded int `json:"succeeded"`
		Errors    []struct {
			MatchID    string   `json:"matchId"`
			Reason     string   `json:"reason"`
			HeldGroups []string `json:"heldGroups"`
		} `json:"errors"`
		HeldGroups []struct {
			MatchID    string   `json:"matchId"`
			HeldGroups []string `json:"heldGroups"`
		} `json:"heldGroups"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, 1, out.Succeeded)
	require.Len(t, out.HeldGroups, 1, "the entry applied in part names what it held")
	assert.Equal(t, []string{"points"}, out.HeldGroups[0].HeldGroups)
	require.Len(t, out.Errors, 1)
	assert.Equal(t, "superseded", out.Errors[0].Reason)
	assert.Equal(t, []string{"points"}, out.Errors[0].HeldGroups)
	history, err := store.LoadMatchHistory(compID, "Pool A-0")
	require.NoError(t, err)
	assert.Len(t, history, 3, "both entries recorded, the superseded one included")
}

// A start write (startOnly) changes the status alone: the score the stored
// match holds is kept (keepQueuedScore), so its history entry names the
// result group and nothing else, and its empty payload score is no change an
// operator made (bc-mrgc phase 3, item 2).
func TestScoreHandler_StartWriteHistoryNamesOnlyTheResult(t *testing.T) {
	const compID = "merge-start"
	r, store := mergeServer(t, compID)
	now := time.Now().UnixMilli()
	mergeScore(t, r, compID, map[string]any{
		"status": "running", "ipponsA": []string{"M"}, "ipponsB": []string{},
		"changed": []string{"points"}, "modifiedAt": now - 10_000,
	})
	out := mergeScore(t, r, compID, map[string]any{
		"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
		"modifiedAt": now - 5_000,
	})
	assert.NotContains(t, out, "applied", "the start applied; got %v", out)
	m := mergeStored(t, store, compID)
	assert.Equal(t, []string{"M"}, m.IpponsA, "the stored score is kept")
	history, err := store.LoadMatchHistory(compID, "Pool A-0")
	require.NoError(t, err)
	require.Len(t, history, 2)
	start := history[1]
	assert.Equal(t, []string{state.GroupResult}, start.Changed, "a start changes the result group alone")
	assert.Len(t, start.Outcomes, 1)
	assert.Empty(t, start.Held)
}

// An override-winner older than the stored result is not applied; it is kept
// in the match's history with the winner it named and answered like any
// other fully held write: applied:false, reason superseded, and the result
// group it held (bc-mrgc phase 3, item 4).
func TestOverrideWinner_HeldAnswersHeldGroups(t *testing.T) {
	const compID = "merge-override"
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "", Courts: []string{"A"}}))
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "individual", Format: state.CompFormatKnockout,
		Status: state.CompStatusKnockout, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: mgAliceID, Name: mgAlice, Dojo: "D1"},
		{ID: mgBobID, Name: mgBob, Dojo: "D2"},
	}))
	now := time.Now().UnixMilli()
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID: "m-r1-0", SideA: mgAlice, SideAID: mgAliceID, SideB: mgBob, SideBID: mgBobID,
		Status: state.MatchStatusCompleted, Winner: mgAlice, WinnerID: mgAliceID,
		ModifiedAt: now - 5_000, MatchNumber: 1, Court: "A",
	}}}}))

	w := serveJSON(r, "PUT", "/api/competitions/"+compID+"/matches/m-r1-0/override-winner", map[string]any{
		"winnerName": mgBob, "modifiedAt": now - 20_000,
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var out map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	assert.Equal(t, false, out["applied"])
	assert.Equal(t, "superseded", out["reason"], "the reason the client tells a supersede from a clock refusal by")
	assert.Equal(t, []any{"result"}, out["heldGroups"])

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, mgAlice, b.MatchByID("m-r1-0").Winner, "the newer result stands")
	history, err := store.LoadMatchHistory(compID, "m-r1-0")
	require.NoError(t, err)
	require.NotEmpty(t, history)
	last := history[len(history)-1]
	assert.Equal(t, "override-winner", last.Door)
	assert.Equal(t, state.HistoryOutcomeHeld, last.Outcomes[state.GroupResult])
	assert.Contains(t, string(last.Held[state.GroupResult]), mgBob)
}
