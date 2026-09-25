package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seedMixedCompWithSeatedKnockout builds a minimal mixed competition: two
// 2-player pools (PoolWinners=1), both pool matches scored (A1 and B1 win),
// and a knockout whose one match (Match 1) seats "Pool A-1st" and "Pool
// B-1st" as A1 vs B1, in koStatus. Completed means A1 has already won it.
// Re-scoring "Pool A-0" for A2 then moves Pool A's 1st place out from under
// that match.
func seedMixedCompWithSeatedKnockout(t *testing.T, store *state.Store, compID string, koStatus state.MatchStatus) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                compID,
		Name:              compID,
		Format:            state.CompFormatMixed,
		Status:            state.CompStatusKnockout,
		Courts:            []string{"A"},
		PoolWinners:       1,
		HasParticipantIDs: true,
	}))
	players := []domain.Player{
		{ID: "a1-id", Name: "A1", Dojo: "Dojo A1"}, {ID: "a2-id", Name: "A2", Dojo: "Dojo A2"},
		{ID: "b1-id", Name: "B1", Dojo: "Dojo B1"}, {ID: "b2-id", Name: "B2", Dojo: "Dojo B2"},
	}
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{players[0], players[1]}},
		{PoolName: "Pool B", Players: []helper.Player{players[2], players[3]}},
	}))
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "A1", SideB: "A2", SideAID: "a1-id", SideBID: "a2-id",
			Winner: "A1", WinnerID: "a1-id", IpponsA: []string{"M", "M"}, Status: state.MatchStatusCompleted},
		{ID: "Pool B-0", SideA: "B1", SideB: "B2", SideAID: "b1-id", SideBID: "b2-id",
			Winner: "B1", WinnerID: "b1-id", IpponsA: []string{"M", "M"}, Status: state.MatchStatusCompleted},
	}))
	ko := state.BracketMatch{
		ID: "m-r1-0", MatchNumber: 1, DisplayRound: 1, Court: "A",
		PlaceholderA: "Pool A-1st", PlaceholderB: "Pool B-1st",
		SideA: "A1", SideB: "B1", SideAID: "a1-id", SideBID: "b1-id",
		Status: koStatus,
	}
	if koStatus == state.MatchStatusCompleted {
		ko.Winner, ko.WinnerID, ko.IpponsA = "A1", "a1-id", []string{"M"}
	}
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{ko}}}))
}

// poolFixDoors are the four write doors a pool correction can come through,
// each sending the same correction: Pool A-0 now won by A2. force adds the
// operator's confirmation.
var poolFixDoors = []struct {
	name string
	send func(t *testing.T, r *gin.Engine, compID string, force bool) *httptest.ResponseRecorder
}{
	{"score", func(t *testing.T, r *gin.Engine, compID string, force bool) *httptest.ResponseRecorder {
		return sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/matches/Pool A-0/score", map[string]any{
			"sideA": "A1", "sideB": "A2", "winner": "A2", "ipponsB": []string{"M"},
			"status": "completed", "correctionReason": "scoresheet was misread", "forceDownstreamReopen": force,
		})
	}},
	{"quick-score", func(t *testing.T, r *gin.Engine, compID string, force bool) *httptest.ResponseRecorder {
		return sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/matches/Pool A-0/quick-score", map[string]any{
			"sideA": "A1", "sideB": "A2", "teamAWins": 0, "teamBWins": 1, "forceDownstreamReopen": force,
		})
	}},
	{"decision", func(t *testing.T, r *gin.Engine, compID string, force bool) *httptest.ResponseRecorder {
		// aka (SideA, A1) withdraws, so A2 wins.
		return sendJSON(t, r, http.MethodPost, "/api/competitions/"+compID+"/matches/Pool A-0/decision", map[string]any{
			"decision": "kiken-voluntary", "decisionBy": "aka", "forceDownstreamReopen": force,
		})
	}},
}

func sendJSON(t *testing.T, r *gin.Engine, method, url string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	require.NoError(t, err)
	req, err := http.NewRequest(method, url, bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func assertPoolA0WonBy(t *testing.T, store *state.Store, compID, winner string) {
	t.Helper()
	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	for _, m := range stored {
		if m.ID == "Pool A-0" {
			assert.Equal(t, winner, m.Winner)
			return
		}
	}
	t.Fatal("Pool A-0 not found")
}

// Every pool-write door answers a correction that moves a qualifier the
// knockout already played with the confirmable 409 downstream_knockout_played,
// carrying qualifierChange so the operator is told who moves; nothing lands.
func TestPoolCorrection_QualifierMove_409Shape(t *testing.T) {
	for _, door := range poolFixDoors {
		t.Run(door.name, func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			compID := "rq-409-" + door.name
			seedMixedCompWithSeatedKnockout(t, store, compID, state.MatchStatusCompleted)

			w := door.send(t, r, compID, false)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			var resp struct {
				Error           string `json:"error"`
				MatchID         string `json:"matchId"`
				BlockingMatchID string `json:"blockingMatchId"`
				BlockingMatches []struct {
					ID     string `json:"id"`
					Number int    `json:"number"`
					Label  string `json:"label"`
				} `json:"blockingMatches"`
				QualifierChange []struct {
					Pool  string `json:"pool"`
					Rank  int    `json:"rank"`
					Place string `json:"place"`
					From  struct{ Name, ID string }
					To    struct{ Name, ID string }
				} `json:"qualifierChange"`
				Message string `json:"message"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, "downstream_knockout_played", resp.Error)
			assert.Equal(t, "Pool A-0", resp.MatchID)
			assert.Equal(t, "m-r1-0", resp.BlockingMatchID)
			require.Len(t, resp.BlockingMatches, 1)
			assert.Equal(t, 1, resp.BlockingMatches[0].Number)
			// The words the dialog shows: the round keeps "Match 1" from
			// reading as the pool match being corrected (Pool A · Match 1).
			assert.Equal(t, "Match 1 (Final)", resp.BlockingMatches[0].Label)
			assert.Contains(t, resp.Message, "Match 1 (Final)", "the server's message names it the same way")
			require.Len(t, resp.QualifierChange, 1)
			qc := resp.QualifierChange[0]
			assert.Equal(t, "Pool A", qc.Pool)
			assert.Equal(t, 1, qc.Rank)
			assert.Equal(t, "1st", qc.Place)
			assert.Equal(t, "A1", qc.From.Name)
			assert.Equal(t, "a1-id", qc.From.ID)
			assert.Equal(t, "A2", qc.To.Name)
			assert.Equal(t, "a2-id", qc.To.ID)
			assert.NotEmpty(t, resp.Message)
			assertPoolA0WonBy(t, store, compID, "A1")
		})
	}
}

// bulk-score is always 200 with per-entry results, so the same refusal rides
// in the entry's Reason.
func TestBulkScore_QualifierMove_ReasonCodes(t *testing.T) {
	for _, tc := range []struct {
		koStatus state.MatchStatus
		reason   string
	}{
		{state.MatchStatusCompleted, "downstream_knockout_played"},
		{state.MatchStatusRunning, "downstream_knockout_running"},
	} {
		t.Run(string(tc.koStatus), func(t *testing.T) {
			r, store, _, _, _ := setupTestRouter(t)
			compID := "rq-bulk-" + string(tc.koStatus)
			seedMixedCompWithSeatedKnockout(t, store, compID, tc.koStatus)

			w := sendJSON(t, r, http.MethodPost, "/api/competitions/"+compID+"/matches/bulk-score", []map[string]any{{
				"id": "Pool A-0", "sideA": "A1", "sideB": "A2", "winner": "A2", "ipponsB": []string{"M"},
				"status": "completed", "correctionReason": "scoresheet was misread",
			}})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var resp struct {
				Succeeded int `json:"succeeded"`
				Errors    []struct {
					MatchID string `json:"matchId"`
					Reason  string `json:"reason"`
				} `json:"errors"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, 0, resp.Succeeded)
			require.Len(t, resp.Errors, 1)
			assert.Equal(t, "Pool A-0", resp.Errors[0].MatchID)
			assert.Equal(t, tc.reason, resp.Errors[0].Reason)
			assertPoolA0WonBy(t, store, compID, "A1")
		})
	}
}

// A knockout match being fought refuses the correction at every door, and the
// operator's confirmation does not get past it (a 409, never a 5xx the
// offline queue would retry forever).
func TestPoolCorrection_RunningKnockout_409Terminal(t *testing.T) {
	for _, door := range poolFixDoors {
		for _, force := range []bool{false, true} {
			t.Run(door.name, func(t *testing.T) {
				r, store, _, _, _ := setupTestRouter(t)
				compID := "rq-run-" + door.name
				seedMixedCompWithSeatedKnockout(t, store, compID, state.MatchStatusRunning)

				w := door.send(t, r, compID, force)
				require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
				var resp struct {
					Error          string `json:"error"`
					MatchID        string `json:"matchId"`
					RunningMatches []struct {
						ID     string `json:"id"`
						Number int    `json:"number"`
						Label  string `json:"label"`
					} `json:"runningMatches"`
					Message string `json:"message"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				assert.Equal(t, "downstream_knockout_running", resp.Error, "force=%v", force)
				assert.Equal(t, "Pool A-0", resp.MatchID)
				require.Len(t, resp.RunningMatches, 1)
				assert.Equal(t, "m-r1-0", resp.RunningMatches[0].ID)
				assert.Equal(t, "Match 1 (Final)", resp.RunningMatches[0].Label)
				assert.Equal(t, "Match 1 (Final) is being fought now. Finish it or send it back to the queue, then save again.", resp.Message)
				assertPoolA0WonBy(t, store, compID, "A1")
			})
		}
	}
}

// Confirmed, the correction lands, the played match is reopened with the new
// qualifier seated in it, and it is announced: its own match_updated (a client
// watching only that court must hear its verdict was cleared) and, on /score,
// the reopenedMatches receipt.
func TestPoolCorrection_Forced_ReopensAndBroadcasts(t *testing.T) {
	for _, door := range poolFixDoors {
		t.Run(door.name, func(t *testing.T) {
			r, store, _, hub, _ := setupTestRouter(t)
			compID := "rq-force-" + door.name
			seedMixedCompWithSeatedKnockout(t, store, compID, state.MatchStatusCompleted)

			stop := collectEvents(t, hub)
			w := door.send(t, r, compID, true)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			events := stop()

			if door.name == "score" {
				var resp struct {
					ReopenedMatches []struct {
						ID     string `json:"id"`
						Number int    `json:"number"`
						Label  string `json:"label"`
					} `json:"reopenedMatches"`
				}
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
				require.Len(t, resp.ReopenedMatches, 1)
				assert.Equal(t, "m-r1-0", resp.ReopenedMatches[0].ID)
				assert.Equal(t, 1, resp.ReopenedMatches[0].Number)
				assert.Equal(t, "Match 1 (Final)", resp.ReopenedMatches[0].Label)
			}
			announced := false
			for _, e := range events {
				if strings.Contains(e, `"matchId":"m-r1-0"`) && strings.Contains(e, string(EventMatchUpdated)) {
					announced = true
				}
			}
			assert.True(t, announced, "the reopened knockout match gets its own match_updated: %v", events)

			assertPoolA0WonBy(t, store, compID, "A2")
			b, err := store.LoadBracket(compID)
			require.NoError(t, err)
			ko := b.Rounds[0][0]
			assert.Equal(t, state.MatchStatusScheduled, ko.Status)
			assert.Empty(t, ko.Winner)
			assert.Equal(t, "A2", ko.SideA)
			assert.Equal(t, "a2-id", ko.SideAID)
		})
	}
}

// Once the knockout has started, the after-write auto-complete runs the pool
// pass only for a completed pool write, so every door has to hand it the match
// it wrote. A pool draw corrected in knockout status leaves Pool A's 1st place
// tied, and the same request must inject the tie-break that settles it; a door
// that passed nothing would skip the pass and leave the place undecided.
func TestPoolCorrection_KnockoutStatusDrawInjectsTieBreak(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	compID := "rq-ko-tie"
	seedMixedCompWithSeatedKnockout(t, store, compID, state.MatchStatusScheduled)

	w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/matches/Pool A-0/score", map[string]any{
		"sideA": "A1", "sideB": "A2", "winner": "", "decision": "hikiwake",
		"status": "completed", "correctionReason": "it was a draw",
	})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	matches, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	var tieBreaks []string
	for _, m := range matches {
		if strings.Contains(m.ID, "-TB-") {
			tieBreaks = append(tieBreaks, m.ID)
		}
	}
	assert.NotEmpty(t, tieBreaks, "the correcting write itself injects the tie-break for the tied place")
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	assert.Equal(t, "Pool A-1st", b.Rounds[0][0].SideA, "the tied place waits on its label")
}

// PUT .../override-rank (the chusen door) moves a pool's order without a match
// write, and answers for the knockout exactly as a pool correction does: the
// confirmable 409 naming who moves, the refused override taken back; the
// terminal 409 for a match being fought; and, confirmed, the fought match
// reopened, announced and repainted, with the receipt naming it. Before, it
// saved the override with no answer, and the next resolver pass seated A1 in
// the played match and in A1's new place at once.
//
// It runs in both statuses a mixed competition's pool order is open in: the
// pools stage, and the knockout stage, where a pool correction can reopen a
// tie only a chusen settles and a wrong chusen must stay fixable. The door
// used to refuse the knockout stage with a 409, whatever the order did.
func TestPoolRankOverride_AnswersForTheKnockout(t *testing.T) {
	for _, compStatus := range []state.CompetitionStatus{state.CompStatusPools, state.CompStatusKnockout} {
		t.Run(string(compStatus), func(t *testing.T) {
			testPoolRankOverrideAnswersForTheKnockout(t, compStatus)
		})
	}
}

func testPoolRankOverrideAnswersForTheKnockout(t *testing.T, compStatus state.CompetitionStatus) {
	seed := func(t *testing.T, store *state.Store, compID string, koStatus state.MatchStatus) {
		seedMixedCompWithSeatedKnockout(t, store, compID, koStatus)
		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		comp.Status = compStatus
		require.NoError(t, store.SaveCompetition(comp))
	}
	send := func(t *testing.T, r *gin.Engine, compID string, force bool) *httptest.ResponseRecorder {
		return sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool A/override-rank", map[string]any{
			"playerId": "a2-id", "rank": 1, "forceDownstreamReopen": force,
		})
	}
	noOverride := func(t *testing.T, store *state.Store, compID string) {
		o, err := store.LoadOverrides(compID)
		require.NoError(t, err)
		assert.Empty(t, o.PoolRanks["Pool A"], "the refused override is taken back")
	}

	t.Run("played", func(t *testing.T) {
		r, store, _, hub, _ := setupTestRouter(t)
		compID := "rq-rank-played"
		seed(t, store, compID, state.MatchStatusCompleted)

		w := send(t, r, compID, false)
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
		var refusal struct {
			Error           string `json:"error"`
			MatchID         string `json:"matchId"`
			BlockingMatches []struct {
				ID string `json:"id"`
			} `json:"blockingMatches"`
			QualifierChange []struct {
				Place string
				From  struct{ Name string }
				To    struct{ Name string }
			} `json:"qualifierChange"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
		assert.Equal(t, "downstream_knockout_played", refusal.Error)
		assert.Empty(t, refusal.MatchID, "no match is being corrected")
		require.Len(t, refusal.BlockingMatches, 1)
		assert.Equal(t, "m-r1-0", refusal.BlockingMatches[0].ID)
		require.Len(t, refusal.QualifierChange, 1)
		assert.Equal(t, "A1", refusal.QualifierChange[0].From.Name)
		assert.Equal(t, "A2", refusal.QualifierChange[0].To.Name)
		noOverride(t, store, compID)

		stop := collectEvents(t, hub)
		w = send(t, r, compID, true)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		events := stop()
		var receipt struct {
			ReopenedMatches []struct {
				ID     string `json:"id"`
				Number int    `json:"number"`
				Label  string `json:"label"`
			} `json:"reopenedMatches"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &receipt))
		require.Len(t, receipt.ReopenedMatches, 1)
		assert.Equal(t, "m-r1-0", receipt.ReopenedMatches[0].ID)
		assert.Equal(t, 1, receipt.ReopenedMatches[0].Number)
		assert.Equal(t, "Match 1 (Final)", receipt.ReopenedMatches[0].Label)
		announced := false
		for _, e := range events {
			if strings.Contains(e, `"matchId":"m-r1-0"`) && strings.Contains(e, string(EventMatchUpdated)) {
				announced = true
			}
		}
		assert.True(t, announced, "the reopened knockout match gets its own match_updated: %v", events)
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		ko := b.Rounds[0][0]
		assert.Equal(t, state.MatchStatusScheduled, ko.Status)
		assert.Empty(t, ko.Winner)
		assert.Equal(t, "A2", ko.SideA)
		assert.Equal(t, "a2-id", ko.SideAID)
	})

	t.Run("running", func(t *testing.T) {
		for _, force := range []bool{false, true} {
			r, store, _, _, _ := setupTestRouter(t)
			compID := fmt.Sprintf("rq-rank-running-%v", force)
			seed(t, store, compID, state.MatchStatusRunning)
			w := send(t, r, compID, force)
			require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
			assert.Contains(t, w.Body.String(), `"error":"downstream_knockout_running"`, "force=%v", force)
			noOverride(t, store, compID)
		}
	})
}

// A pool correction made after a mixed competition's knockout has started can
// leave Pool A's 1st place in a daihyosen cycle that only a chusen settles,
// with the knockout match that place feeds waiting on its label. The chusen
// doors must work in knockout status: GET .../chusen-candidates offers the
// tie, PUT .../override-rank accepts each position, and the request that
// records the last one seats the slot. Before, the candidates came back empty
// and the override was refused with a 409, so that match could never be played.
func TestChusen_KnockoutStatus_AcceptedAndSeatsTheSlot(t *testing.T) {
	r, store, eng, _, _ := setupTestRouter(t)
	compID := "chusen-ko-door"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Format: state.CompFormatMixed, Status: state.CompStatusKnockout,
		Kind: "team", TeamSize: 2, Courts: []string{"A"}, PoolWinners: 1, HasParticipantIDs: true,
	}))
	team := func(name string) domain.Player {
		return domain.Player{ID: strings.ToLower(name) + "-id", Name: name, Dojo: "Dojo " + name}
	}
	alpha, beta, gamma, delta, epsilon := team("Alpha"), team("Beta"), team("Gamma"), team("Delta"), team("Epsilon")
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{alpha, beta, gamma}},
		{PoolName: "Pool B", Players: []helper.Player{delta, epsilon}},
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{alpha, beta, gamma, delta, epsilon}))
	drawn := func(id string, a, b domain.Player) state.MatchResult {
		return state.MatchResult{ID: id, SideA: a.Name, SideB: b.Name, SideAID: a.ID, SideBID: b.ID, Court: "A",
			Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)}
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		drawn("Pool A-0", alpha, beta), drawn("Pool A-1", alpha, gamma), drawn("Pool A-2", beta, gamma),
		{ID: "Pool B-0", SideA: "Delta", SideB: "Epsilon", SideAID: delta.ID, SideBID: epsilon.ID, Court: "A",
			Winner: "Delta", WinnerID: delta.ID, Status: state.MatchStatusCompleted},
	}))
	_, err := eng.InjectPoolDaihyosenMatches(compID)
	require.NoError(t, err)
	all, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	beats := map[string]string{"Alpha": "Beta", "Beta": "Gamma", "Gamma": "Alpha"}
	dh := 0
	for i := range all {
		if !strings.Contains(all[i].ID, "-DH-") {
			continue
		}
		dh++
		all[i].Status = state.MatchStatusCompleted
		if beats[all[i].SideA] == all[i].SideB {
			all[i].Winner, all[i].WinnerID = all[i].SideA, all[i].SideAID
		} else {
			all[i].Winner, all[i].WinnerID = all[i].SideB, all[i].SideBID
		}
	}
	require.Equal(t, 3, dh)
	require.NoError(t, store.SavePoolMatches(compID, all))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
		ID: "m-r1-0", MatchNumber: 1, DisplayRound: 1, Court: "A",
		PlaceholderA: "Pool A-1st", PlaceholderB: "Pool B-1st",
		SideA: "Pool A-1st", SideB: "Delta", SideBID: delta.ID,
		Status: state.MatchStatusScheduled,
	}}}}))

	candidates := func() int {
		t.Helper()
		w := sendJSON(t, r, http.MethodGet, "/api/competitions/"+compID+"/chusen-candidates", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var body struct {
			Candidates []struct {
				PoolName string `json:"poolName"`
			} `json:"candidates"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
		return len(body.Candidates)
	}
	require.Equal(t, 1, candidates(), "the tie is offered in knockout status")

	for rank, p := range []domain.Player{alpha, beta, gamma} {
		w := sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool A/override-rank", map[string]any{
			"playerId": p.ID, "rank": rank + 1,
		})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	}

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	ko := b.Rounds[0][0]
	assert.Equal(t, "Alpha", ko.SideA, "the chusen seats the slot the knockout match waited on")
	assert.Equal(t, alpha.ID, ko.SideAID)
	assert.Equal(t, 0, candidates(), "the recorded chusen clears the candidate")
}
