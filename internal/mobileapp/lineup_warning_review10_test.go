package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// refusedCarryAfterP10 lays out the start-warning case: P1-1's own lineup is a
// copy made at 40 holding a position the team size has no place for (kept
// from older data), and P1-0's own lineup a copy made at 50. A write that
// moves P1-0 out of scheduled at 30 keeps P1-0's lineup from 30 on, so P1-1
// reads it and cannot be worked out again.
func refusedCarryAfterP10(t *testing.T, e *carryEnv) {
	t.Helper()
	p1, p2, p5 := domain.PositionNumbered(1), domain.PositionNumbered(2), domain.PositionNumbered(5)
	e.save("", 1, "Mori", 35)
	require.NoError(t, e.store.SetTeamLineup(e.comp, domain.TeamLineup{TeamID: e.id["Taka"], MatchID: "P1-1",
		Positions: map[domain.Position]string{p1: "Mori", p2: "Ueda", p5: "Endo"},
		Stamps:    map[domain.Position]int64{p1: e.base + 35, p2: e.base - 1000, p5: e.base + 40},
		CopiedAt:  e.base + 40, Own: []domain.Position{p5}}, 6))
	e.save("P1-0", 2, "Doi", 50)
}

const refusedP11Warning = "but the lineup carried to P1-1 could not be worked out again " +
	"(a position the team size has no place for); it was kept as it was. Check that match's lineup."

func warningOf(t *testing.T, rec interface{ Bytes() []byte }) string {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Bytes(), &body))
	w, _ := body["warning"].(string)
	return w
}

// A bulk-score entry whose write leaves a carried lineup that cannot follow it
// tells the operator, as /score does: the entry's warning rides in the
// answer's `warnings`, never only in the log (stage 6 review 10, m3).
func TestBulkScore_ACarriedLineupThatCannotFollowTheEntryIsTold(t *testing.T) {
	e := newCarryEnv(t, true)
	refusedCarryAfterP10(t, e)
	rec := e.do(http.MethodPost, "/api/competitions/"+e.comp+"/matches/bulk-score", []map[string]any{{
		"id": "P1-0", "sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": e.id["Kuma"],
		"status": "running", "ipponsA": []string{}, "ipponsB": []string{},
		"changed": []string{state.GroupResult}, "modifiedAt": e.base + 30, "writeId": "bulk-P1-0",
	}})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, state.MatchStatusRunning, e.stored("P1-0").Status, "the entry stands")
	var body struct {
		Succeeded int `json:"succeeded"`
		Warnings  []struct {
			MatchID string `json:"matchId"`
			Warning string `json:"warning"`
		} `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, 1, body.Succeeded, rec.Body.String())
	require.Len(t, body.Warnings, 1, "the entry's warning is in the answer: %s", rec.Body.String())
	require.Equal(t, "P1-0", body.Warnings[0].MatchID)
	require.Equal(t, "The match was started, "+refusedP11Warning, body.Warnings[0].Warning)
}

// A decision recorded on a match that had not started pins the lineups as a
// start does, but the operator is told the decision was recorded, never that
// the match was started (stage 6 review 10, m8).
func TestKachinukiDecisionFromScheduled_TheWarningSaysTheDecisionWasRecorded(t *testing.T) {
	e := newCarryEnv(t, true)
	hub := NewHub()
	t.Cleanup(hub.Close)
	admin := e.r.Group("/api")
	admin.Use(AuthMiddleware(NewFileVerifier(e.store), e.store))
	RegisterDecisionHandlers(admin, e.eng, e.store, e.store, hub)
	refusedCarryAfterP10(t, e)
	rec := e.do(http.MethodPost, "/api/competitions/"+e.comp+"/matches/"+url.PathEscape("P1-0")+"/decision", map[string]any{
		"decision": "kiken-voluntary", "decisionBy": "shiro", "modifiedAt": e.base + 30, "writeId": "decide-P1-0",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, state.MatchStatusCompleted, e.stored("P1-0").Status, "the decision stands")
	w := warningOf(t, rec.Body)
	require.NotContains(t, w, "started", rec.Body.String())
	require.Equal(t, "The decision was recorded, "+refusedP11Warning, w)
}

// A Finish straight from scheduled is told as a finish (stage 6 review 10, m8).
func TestKachinukiFinishFromScheduled_TheWarningSaysTheMatchWasFinished(t *testing.T) {
	e := newCarryEnv(t, true)
	refusedCarryAfterP10(t, e)
	rec := e.do(http.MethodPut, "/api/competitions/"+e.comp+"/matches/"+url.PathEscape("P1-0")+"/score", map[string]any{
		"sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": e.id["Kuma"],
		"status": "completed", "winner": "Taka", "winnerId": e.id["Taka"], "decision": "kachinuki-exhaustion",
		"subResults": []map[string]any{
			{"position": 1, "sideA": "Doi", "sideB": "K-1", "ipponsA": []string{"M", "K"}, "ipponsB": []string{}, "winner": "Doi", "decision": "fought"},
		},
		"modifiedAt": e.base + 30, "writeId": "finish-P1-0",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	require.Equal(t, state.MatchStatusCompleted, e.stored("P1-0").Status, "the finish stands")
	w := warningOf(t, rec.Body)
	require.NotContains(t, w, "started", rec.Body.String())
	require.Equal(t, "The match was finished, "+refusedP11Warning, w)
}

// With both teams' lineups kept at the start, the operator is told in ONE
// sentence naming every carried lineup that could not follow, never two
// near-identical sentences joined (stage 6 review 10, m8).
func TestKachinukiStart_BothTeamsAnchoredAreToldInOneSentence(t *testing.T) {
	e := newCarryEnv(t, true)
	// Kuma meets Washi after P1-0.
	require.NoError(t, e.store.SavePoolMatches(e.comp, []state.MatchResult{
		{ID: "P1-0", SideA: "Taka", SideB: "Kuma", SideAID: e.id["Taka"], SideBID: e.id["Kuma"], Status: state.MatchStatusScheduled},
		{ID: "P1-1", SideA: "Taka", SideB: "Washi", SideAID: e.id["Taka"], SideBID: e.id["Washi"], Status: state.MatchStatusScheduled},
		{ID: "P1-2", SideA: "Taka", SideB: "Inu", SideAID: e.id["Taka"], SideBID: e.id["Inu"], Status: state.MatchStatusScheduled},
		{ID: "P1-3", SideA: "Kuma", SideB: "Washi", SideAID: e.id["Kuma"], SideBID: e.id["Washi"], Status: state.MatchStatusScheduled},
	}))
	refusedCarryAfterP10(t, e)
	p1, p2, p5 := domain.PositionNumbered(1), domain.PositionNumbered(2), domain.PositionNumbered(5)
	kuma := e.id["Kuma"]
	require.NoError(t, e.store.SetTeamLineup(e.comp, domain.TeamLineup{TeamID: kuma,
		Positions: map[domain.Position]string{p1: "Kubo"}, Stamps: map[domain.Position]int64{p1: e.base - 1000}}, 3))
	// Kuma's P1-3 lineup, a copy made at 40 holding a position the team size
	// has no place for; its P1-0 lineup a copy made at 50.
	require.NoError(t, e.store.SetTeamLineup(e.comp, domain.TeamLineup{TeamID: kuma, MatchID: "P1-3",
		Positions: map[domain.Position]string{p1: "Kubo", p5: "Kido"},
		Stamps:    map[domain.Position]int64{p1: e.base - 1000, p5: e.base + 40},
		CopiedAt:  e.base + 40, Own: []domain.Position{p5}}, 6))
	require.NoError(t, e.store.SetTeamLineup(e.comp, domain.TeamLineup{TeamID: kuma, MatchID: "P1-0",
		Positions: map[domain.Position]string{p1: "Kubo", p2: "Kane"},
		Stamps:    map[domain.Position]int64{p1: e.base - 1000, p2: e.base + 50},
		CopiedAt:  e.base + 50, Own: []domain.Position{p2}}, 3))
	rec := e.do(http.MethodPut, "/api/competitions/"+e.comp+"/matches/"+url.PathEscape("P1-0")+"/score", map[string]any{
		"sideA": "Taka", "sideB": "Kuma", "sideAId": e.id["Taka"], "sideBId": kuma,
		"status": "running", "startOnly": true, "ipponsA": []string{}, "ipponsB": []string{},
		"changed": []string{state.GroupResult}, "modifiedAt": e.base + 30, "writeId": "start-P1-0",
	})
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	w := warningOf(t, rec.Body)
	require.Equal(t, 1, strings.Count(w, "The match was started"), "one sentence: %s", w)
	require.Equal(t, "The match was started, but the lineups carried to P1-1 and P1-3 could not be worked out again "+
		"(a position the team size has no place for); they were kept as they were. Check those matches' lineups.", w)
}
