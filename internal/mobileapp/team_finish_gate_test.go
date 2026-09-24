package mobileapp

// bc-tmfn: a team match cannot be finished while a numbered bout has no
// result (operator ruling 2026-09-24: every bout of a team match is fought).
// These pin the server half of that gate on PUT .../score: who it refuses,
// what the refusal says, and every write it must leave alone.

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	finishGateMatchID = "Pool A-1"
	finishGateTeamA   = "Ryu"
	finishGateTeamB   = "Tora"
	finishGateTeamAID = "11111111-1111-4111-8111-111111111111"
	finishGateTeamBID = "22222222-2222-4222-8222-222222222222"
)

// setupTeamFinishServer builds a team competition of teamSize with one pool
// match Ryu v Tora (running, nothing recorded) and the score and decision
// handlers wired against a real store and engine.
func setupTeamFinishServer(t *testing.T, teamSize int, matchType state.TeamMatchType) (*gin.Engine, *state.Store) {
	t.Helper()
	r, store, _ := setupTeamFinishServerWithHub(t, teamSize, matchType)
	return r, store
}

// setupTeamFinishServerWithHub is setupTeamFinishServer that also hands back
// the hub, for a test that asserts what was broadcast.
func setupTeamFinishServerWithHub(t *testing.T, teamSize int, matchType state.TeamMatchType) (*gin.Engine, *state.Store, *Hub) {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()
	const compID = "tf"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Kind: "team", Format: state.CompFormatMixed, Status: state.CompStatusPools,
		TeamSize: teamSize, TeamMatchType: matchType,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: finishGateTeamAID, Name: finishGateTeamA, Dojo: "DojoR"},
		{ID: finishGateTeamBID, Name: finishGateTeamB, Dojo: "DojoT"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: finishGateMatchID, SideA: finishGateTeamA, SideAID: finishGateTeamAID,
		SideB: finishGateTeamB, SideBID: finishGateTeamBID, Status: state.MatchStatusRunning,
	}}))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)
	return r, store, hub
}

// saveFinishGateLineup saves a round-0 lineup for a team keyed by its
// participant id, naming a fighter at each listed bout and leaving every
// other position vacant.
func saveFinishGateLineup(t *testing.T, store *state.Store, teamID string, teamSize int, occupied ...int) {
	t.Helper()
	saveFinishGateLineupAt(t, store, teamID, teamSize, 0, occupied...)
}

// saveFinishGateLineupAt is saveFinishGateLineup for a given round.
func saveFinishGateLineupAt(t *testing.T, store *state.Store, teamID string, teamSize, round int, occupied ...int) {
	t.Helper()
	positions := map[domain.Position]string{}
	for _, bout := range occupied {
		pos, ok := domain.PositionForBout(teamSize, bout)
		require.True(t, ok)
		positions[pos] = teamID[:4] + "-fighter-" + string(pos)
	}
	require.NoError(t, store.SetTeamLineup("tf", domain.TeamLineup{TeamID: teamID, Round: round, Positions: positions}, teamSize))
}

// wonBout is a numbered bout Ryu won by a men.
func wonBout(position int) map[string]any {
	return map[string]any{
		"position": position, "sideA": "", "sideB": "",
		"ipponsA": []string{"M"}, "ipponsB": []string{},
		"winner": finishGateTeamA, "decision": "",
	}
}

// unfoughtBout is what the editor sends for a bout nobody has touched.
func unfoughtBout(position int) map[string]any {
	return map[string]any{
		"position": position, "sideA": "", "sideB": "",
		"ipponsA": []string{}, "ipponsB": []string{},
		"winner": "", "decision": "",
	}
}

func finishPayload(subs ...map[string]any) map[string]any {
	return map[string]any{
		"status": "completed", "winner": finishGateTeamA,
		"sideA": finishGateTeamA, "sideB": finishGateTeamB,
		"ipponsA": []string{}, "ipponsB": []string{},
		"subResults": subs,
	}
}

func finishGateStored(t *testing.T, store *state.Store) state.MatchResult {
	t.Helper()
	ms, err := store.LoadPoolMatches("tf")
	require.NoError(t, err)
	require.Len(t, ms, 1)
	return ms[0]
}

func TestTeamFinishGate_RefusesAndNamesTheBouts(t *testing.T) {
	cases := []struct {
		name string
		subs []map[string]any
		want string
	}{
		{
			name: "bouts missing from the payload",
			subs: []map[string]any{wonBout(1), wonBout(2), wonBout(3)},
			want: "Bout 4 (Fukusho) and Bout 5 (Taisho) have no result. Record a score, a Tie, or a Fusensho before finishing.",
		},
		{
			name: "an untouched row in the payload",
			subs: []map[string]any{wonBout(1), unfoughtBout(2), wonBout(3), wonBout(4), wonBout(5)},
			want: "Bout 2 (Jiho) has no result. Record a score, a Tie, or a Fusensho before finishing.",
		},
		{
			name: "a daihyosen row does not stand in for a numbered bout",
			subs: []map[string]any{wonBout(1), wonBout(2), wonBout(3), wonBout(4), {
				"position": state.DaihyosenSubPosition, "sideA": finishGateTeamA, "sideB": finishGateTeamB,
				"ipponsA": []string{"M"}, "ipponsB": []string{}, "winner": finishGateTeamA, "decision": "daihyosen",
			}},
			want: "Bout 5 (Taisho) has no result. Record a score, a Tie, or a Fusensho before finishing.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store := setupTeamFinishServer(t, 5, state.TeamMatchTypeFixed)
			w := putScore(t, r, "tf", finishGateMatchID, finishPayload(tc.subs...))
			require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
			var body map[string]string
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, tc.want, body["error"])
			assert.NotContains(t, w.Body.String(), "lineup", "the refusal must never send the operator to the lineup")
			assert.Equal(t, state.MatchStatusRunning, finishGateStored(t, store).Status, "a refused finish writes nothing")
		})
	}
}

func TestTeamFinishGate_AllowsEveryBoutWithAResult(t *testing.T) {
	r, store := setupTeamFinishServer(t, 3, state.TeamMatchTypeFixed)
	tie := map[string]any{
		"position": 2, "sideA": "", "sideB": "", "ipponsA": []string{}, "ipponsB": []string{},
		"winner": "", "decision": "hikiwake",
	}
	fusensho := map[string]any{
		"position": 3, "sideA": "", "sideB": "", "ipponsA": domain.DefaultWinIppons(false), "ipponsB": []string{},
		"winner": finishGateTeamA, "decision": "fusensho",
	}
	w := putScore(t, r, "tf", finishGateMatchID, finishPayload(wonBout(1), tie, fusensho))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, state.MatchStatusCompleted, finishGateStored(t, store).Status)
}

func TestTeamFinishGate_LeavesOtherWritesAlone(t *testing.T) {
	t.Run("a running write with bouts still to fight", func(t *testing.T) {
		r, _ := setupTeamFinishServer(t, 3, state.TeamMatchTypeFixed)
		p := finishPayload(wonBout(1), unfoughtBout(2), unfoughtBout(3))
		p["status"] = "running"
		p["winner"] = ""
		w := putScore(t, r, "tf", finishGateMatchID, p)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})
	t.Run("kachinuki, which ends on End match", func(t *testing.T) {
		r, store := setupTeamFinishServer(t, 3, state.TeamMatchTypeKachinuki)
		sub := wonBout(1)
		sub["sideA"], sub["sideB"], sub["winner"] = "R-1", "W-1", "R-1"
		p := finishPayload(sub)
		p["decision"] = "kachinuki-exhaustion"
		w := putScore(t, r, "tf", finishGateMatchID, p)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, state.MatchStatusCompleted, finishGateStored(t, store).Status)
	})
	t.Run("a withdrawal decision on a partly fought encounter", func(t *testing.T) {
		r, store := setupTeamFinishServer(t, 3, state.TeamMatchTypeFixed)
		p := finishPayload(wonBout(1))
		p["status"] = "running"
		p["winner"] = ""
		require.Equal(t, http.StatusOK, putScore(t, r, "tf", finishGateMatchID, p).Code)
		w := postDecision(t, r, "tf", finishGateMatchID, map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro"})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		assert.Equal(t, state.MatchStatusCompleted, finishGateStored(t, store).Status)
	})
}

func TestTeamFinishGate_CorrectionIsNotExempt(t *testing.T) {
	r, store := setupTeamFinishServer(t, 3, state.TeamMatchTypeFixed)
	require.Equal(t, http.StatusOK, putScore(t, r, "tf", finishGateMatchID, finishPayload(wonBout(1), wonBout(2), wonBout(3))).Code)
	p := finishPayload(wonBout(1), wonBout(2), unfoughtBout(3))
	p["correctionReason"] = "wrong bout scored"
	w := putScore(t, r, "tf", finishGateMatchID, p)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Bout 3 has no result.")
	stored := finishGateStored(t, store)
	require.Len(t, stored.SubResults, 3)
	assert.Equal(t, finishGateTeamA, stored.SubResults[2].Winner, "the refused correction left the stored bout alone")
}

// A correction to the bouts of a match a withdrawal ended keeps the ruling
// (operator ruling 2026-09-24: "Save correction should just save what the
// operator enters"), so the bouts nobody fought after it do not refuse the
// save, on either door. The ruling, the winner it names and the eligibility
// record all stay as recorded, although the sheet's own winner is the team
// that withdrew.
func TestTeamFinishGate_CorrectionOverAWithdrawalIsSaved(t *testing.T) {
	for _, door := range []string{"score", "bulk-score"} {
		t.Run(door, func(t *testing.T) {
			r, store := setupTeamFinishServer(t, 3, state.TeamMatchTypeFixed)
			p := finishPayload(wonBout(1))
			p["status"], p["winner"] = "running", ""
			require.Equal(t, http.StatusOK, putScore(t, r, "tf", finishGateMatchID, p).Code)
			w := postDecision(t, r, "tf", finishGateMatchID, map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka"})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			statusesBefore, err := store.LoadCompetitorStatus("tf")
			require.NoError(t, err)
			require.False(t, statusesBefore[finishGateTeamAID].Eligible)

			fixed := wonBout(1)
			fixed["ipponsA"] = []string{"K"}
			c := finishPayload(fixed, unfoughtBout(2), unfoughtBout(3))
			c["correctionReason"] = "Scoring error: wrong waza entered"
			if door == "score" {
				w = putScore(t, r, "tf", finishGateMatchID, c)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			} else {
				c["id"] = finishGateMatchID
				body, merr := json.Marshal([]map[string]any{c})
				require.NoError(t, merr)
				req, rerr := http.NewRequest(http.MethodPost, "/api/competitions/tf/matches/bulk-score", bytes.NewBuffer(body))
				require.NoError(t, rerr)
				req.Header.Set("Content-Type", "application/json")
				w = httptest.NewRecorder()
				r.ServeHTTP(w, req)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				assert.Contains(t, w.Body.String(), `"succeeded":1`)
			}

			stored := finishGateStored(t, store)
			assert.Equal(t, "kiken-voluntary", stored.Decision)
			assert.Equal(t, "aka", stored.DecisionBy)
			assert.Equal(t, finishGateTeamB, stored.Winner, "Ryu withdrew; the sheet's own winner does not replace that")
			assert.Equal(t, finishGateTeamBID, stored.WinnerID)
			require.NotEmpty(t, stored.SubResults)
			assert.Equal(t, []string{"K"}, stored.SubResults[0].IpponsA, "the bout edit is saved")
			statusesAfter, err := store.LoadCompetitorStatus("tf")
			require.NoError(t, err)
			assert.Equal(t, statusesBefore, statusesAfter, "the eligibility record is untouched, RecordedAt included")
		})
	}
}

// Clear withdrawal and reopen (operator ruling 2026-09-24: "Everything should
// be able to be fixed, in case of a wrong entry"): a withdrawal recorded by
// mistake is removed by REOPENING the match, which puts it back to running
// with the bout fought before it kept, restores the team the withdrawal
// barred (broadcast as competitor_status_updated), and leaves the rest to the
// normal finish: the gate refuses while a bout has no result, and once every
// bout has one the match finishes as fought, carrying the reopen's reason.
func TestTeamFinishGate_ClearWithdrawalReopensTheMatch(t *testing.T) {
	r, store, hub := setupTeamFinishServerWithHub(t, 3, state.TeamMatchTypeFixed)
	p := finishPayload(wonBout(1))
	p["status"], p["winner"] = "running", ""
	require.Equal(t, http.StatusOK, putScore(t, r, "tf", finishGateMatchID, p).Code)
	w := postDecision(t, r, "tf", finishGateMatchID, map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var mu sync.Mutex
	var events []string
	ch := hub.Subscribe()
	require.NotNil(t, ch)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for e := range ch {
			mu.Lock()
			events = append(events, e.payload)
			mu.Unlock()
		}
	}()

	w = postReopen(t, r, "tf", finishGateMatchID, "Withdrawal recorded by mistake")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	hub.Unsubscribe(ch)
	<-done

	stored := finishGateStored(t, store)
	assert.Equal(t, state.MatchStatusRunning, stored.Status, "the match was never decided")
	assert.Equal(t, "", stored.Decision)
	assert.Equal(t, "", stored.Winner)
	require.NotEmpty(t, stored.SubResults, "the bout fought before the withdrawal is kept")
	assert.Equal(t, []string{"M"}, stored.SubResults[0].IpponsA)
	statuses, err := store.LoadCompetitorStatus("tf")
	require.NoError(t, err)
	assert.True(t, statuses[finishGateTeamAID].Eligible, "the team the withdrawal barred is eligible again")
	mu.Lock()
	var restored bool
	for _, e := range events {
		if strings.Contains(e, `"type":"competitor_status_updated"`) &&
			strings.Contains(e, `"playerId":"`+finishGateTeamAID+`"`) && strings.Contains(e, `"eligible":true`) {
			restored = true
		}
	}
	mu.Unlock()
	assert.True(t, restored, "the restore is broadcast; got %v", events)

	partial := finishPayload(wonBout(1), unfoughtBout(2), unfoughtBout(3))
	w = putScore(t, r, "tf", finishGateMatchID, partial)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Bout 2 and Bout 3 have no result.")

	w = putScore(t, r, "tf", finishGateMatchID, finishPayload(wonBout(1), wonBout(2), wonBout(3)))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored = finishGateStored(t, store)
	assert.Equal(t, state.MatchStatusCompleted, stored.Status)
	assert.Equal(t, finishGateTeamA, stored.Winner)
	assert.Equal(t, "Withdrawal recorded by mistake", stored.CorrectionReason, "the reopen's reason is the audit trail")
}

// A correction naming a decision the sheet cannot send ("fought") does not keep
// the withdrawal, so it is a fought finish and the gate asks for every bout.
// The exemption is read under the write's own lock (teamFinishRefusalUnderTx).
func TestTeamFinishGate_ADecisionThatReplacesTheWithdrawalIsGated(t *testing.T) {
	r, store := setupTeamFinishServer(t, 3, state.TeamMatchTypeFixed)
	p := finishPayload(wonBout(1))
	p["status"], p["winner"] = "running", ""
	require.Equal(t, http.StatusOK, putScore(t, r, "tf", finishGateMatchID, p).Code)
	w := postDecision(t, r, "tf", finishGateMatchID, map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	c := finishPayload(wonBout(1), unfoughtBout(2), unfoughtBout(3))
	c["decision"] = "fought"
	c["correctionReason"] = "Scoring error"
	w = putScore(t, r, "tf", finishGateMatchID, c)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "Bout 2 and Bout 3 have no result.")
	assert.Equal(t, "kiken-voluntary", finishGateStored(t, store).Decision, "the refused write left the withdrawal alone")
}

// teamFinishRefusalUnderTx exempts exactly the writes that keep the stored
// withdrawal, read from the in-tx snapshot.
func TestTeamFinishRefusalUnderTx(t *testing.T) {
	refusal := &ValidationError{Message: "Bout 2 has no result."}
	correction := &state.MatchResult{Status: state.MatchStatusCompleted}
	kiken := correctionCheck{StoredStatus: state.MatchStatusCompleted, StoredDecision: "kiken-voluntary"}
	fought := correctionCheck{StoredStatus: state.MatchStatusCompleted}
	reopened := correctionCheck{StoredStatus: state.MatchStatusRunning}

	assert.Nil(t, teamFinishRefusalUnderTx(nil, fought, correction), "no refusal to apply")
	assert.Nil(t, teamFinishRefusalUnderTx(refusal, kiken, correction), "a correction over a withdrawal keeps it")
	assert.Same(t, refusal, teamFinishRefusalUnderTx(refusal, fought, correction))
	assert.Same(t, refusal, teamFinishRefusalUnderTx(refusal, reopened, correction),
		"a match reopened since the refusal was computed is a fought finish")
	assert.Same(t, refusal, teamFinishRefusalUnderTx(refusal, kiken, &state.MatchResult{Status: state.MatchStatusCompleted, Decision: "fought"}))
}

func TestTeamFinishGate_Vacancies(t *testing.T) {
	cases := []struct {
		name      string
		lineupA   []int // occupied bouts; nil means no lineup saved
		lineupB   []int
		wantCode  int
		wantInErr string
	}{
		{name: "a position both sides leave vacant has no bout", lineupA: []int{1, 2}, lineupB: []int{1, 2}, wantCode: http.StatusOK},
		{name: "a vacancy on one side is still a bout", lineupA: []int{1, 2}, lineupB: []int{1, 2, 3}, wantCode: http.StatusBadRequest, wantInErr: "Bout 3 has no result."},
		{name: "no saved lineup counts as occupied", lineupA: nil, lineupB: []int{1, 2}, wantCode: http.StatusBadRequest, wantInErr: "Bout 3 has no result."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store := setupTeamFinishServer(t, 3, state.TeamMatchTypeFixed)
			if tc.lineupA != nil {
				saveFinishGateLineup(t, store, finishGateTeamAID, 3, tc.lineupA...)
			}
			if tc.lineupB != nil {
				saveFinishGateLineup(t, store, finishGateTeamBID, 3, tc.lineupB...)
			}
			w := putScore(t, r, "tf", finishGateMatchID, finishPayload(wonBout(1), wonBout(2)))
			require.Equal(t, tc.wantCode, w.Code, w.Body.String())
			if tc.wantInErr != "" {
				assert.Contains(t, w.Body.String(), tc.wantInErr)
				assert.NotContains(t, w.Body.String(), "lineup")
			}
		})
	}
}

// The gate resolves a pool match's lineup by the round the score sheet uses
// (the client's resolveRoundIndex: a pool, league or Swiss match's own Round),
// not by round 0, so the bouts it exempts are the ones the operator was shown.
// Round 5 is a lineup saved ahead for a later round: with the match read as
// round 0 nothing is at or below it and the lookup fell back to the highest
// round overall, which is round 5's lineup, not this match's.
func TestTeamFinishGate_PoolMatchUsesItsOwnRound(t *testing.T) {
	full := []int{1, 2, 3, 4, 5}
	noTaisho := []int{1, 2, 3, 4}
	cases := []struct {
		name     string
		rounds   map[int][]int // round -> occupied bouts, saved for both teams
		wantCode int
	}{
		{name: "round 3 leaves Taisho empty on both sides", rounds: map[int][]int{1: full, 3: noTaisho}, wantCode: http.StatusOK},
		{name: "Taisho empty in round 1 only", rounds: map[int][]int{1: noTaisho, 3: full}, wantCode: http.StatusBadRequest},
		{name: "round 3 leaves Taisho empty, a later round is full", rounds: map[int][]int{1: full, 3: noTaisho, 5: full}, wantCode: http.StatusOK},
		{name: "round 3 is full, a later round leaves Taisho empty", rounds: map[int][]int{1: noTaisho, 3: full, 5: noTaisho}, wantCode: http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r, store := setupTeamFinishServer(t, 5, state.TeamMatchTypeFixed)
			ms, err := store.LoadPoolMatches("tf")
			require.NoError(t, err)
			ms[0].Round = 3
			require.NoError(t, store.SavePoolMatches("tf", ms))
			for round, occupied := range tc.rounds {
				saveFinishGateLineupAt(t, store, finishGateTeamAID, 5, round, occupied...)
				saveFinishGateLineupAt(t, store, finishGateTeamBID, 5, round, occupied...)
			}
			w := putScore(t, r, "tf", finishGateMatchID, finishPayload(wonBout(1), wonBout(2), wonBout(3), wonBout(4)))
			require.Equal(t, tc.wantCode, w.Code, w.Body.String())
			if tc.wantCode == http.StatusBadRequest {
				assert.Contains(t, w.Body.String(), "Bout 5 (Taisho) has no result.")
			}
		})
	}
}

// POST .../bulk-score can complete a team match too, so it runs the same gate
// per entry. A refused entry is reported in errors[] (the endpoint is always
// 200, partial success), names the match and the bouts, and writes nothing.
func TestTeamFinishGate_BulkScore(t *testing.T) {
	bulk := func(t *testing.T, r *gin.Engine, entry map[string]any) (int, map[string]any) {
		t.Helper()
		entry["id"] = finishGateMatchID
		body, err := json.Marshal([]map[string]any{entry})
		require.NoError(t, err)
		req, err := http.NewRequest(http.MethodPost, "/api/competitions/tf/matches/bulk-score", bytes.NewBuffer(body))
		require.NoError(t, err)
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		var resp map[string]any
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		return w.Code, resp
	}

	t.Run("an entry finishing with an unfought bout is refused", func(t *testing.T) {
		r, store := setupTeamFinishServer(t, 5, state.TeamMatchTypeFixed)
		code, resp := bulk(t, r, finishPayload(wonBout(1), wonBout(2), wonBout(3), wonBout(4)))
		require.Equal(t, http.StatusOK, code)
		assert.EqualValues(t, 0, resp["succeeded"])
		errs, ok := resp["errors"].([]any)
		require.True(t, ok)
		require.Len(t, errs, 1)
		entry := errs[0].(map[string]any)
		assert.Equal(t, finishGateMatchID, entry["matchId"])
		assert.Equal(t, "Bout 5 (Taisho) has no result. Record a score, a Tie, or a Fusensho before finishing.", entry["error"])
		assert.Equal(t, state.MatchStatusRunning, finishGateStored(t, store).Status, "a refused entry writes nothing")
	})

	t.Run("an entry with every bout fought is recorded", func(t *testing.T) {
		r, store := setupTeamFinishServer(t, 3, state.TeamMatchTypeFixed)
		code, resp := bulk(t, r, finishPayload(wonBout(1), wonBout(2), wonBout(3)))
		require.Equal(t, http.StatusOK, code)
		assert.EqualValues(t, 1, resp["succeeded"])
		assert.Equal(t, state.MatchStatusCompleted, finishGateStored(t, store).Status)
	})
}

// failingVacancies answers the vacancy question with an error, which must
// exempt nothing rather than let the finish through.
type failingVacancies struct{}

func (failingVacancies) TeamBoutsWithNoFighter(string, string) (map[int]bool, error) {
	return map[int]bool{3: true}, os.ErrPermission
}

func TestRefuseUnfinishedTeamFinish_Scope(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "ind", Format: state.CompFormatMixed}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "team", Kind: "team", Format: state.CompFormatMixed, TeamSize: 3}))
	partial := &state.MatchResult{Status: state.MatchStatusCompleted, SubResults: []state.SubMatchResult{{Position: 1, Winner: "A"}}}

	verr, err := refuseUnfinishedTeamFinish(store, stubScoringEngine{}, "ind", "Pool A-1", partial)
	require.NoError(t, err)
	assert.Nil(t, verr, "an individual competition has no bouts to gate")

	for _, id := range []string{"Pool A-DH-1", "Pool A-TB-1"} {
		verr, err = refuseUnfinishedTeamFinish(store, stubScoringEngine{}, "team", id, partial)
		require.NoError(t, err)
		assert.Nil(t, verr, "%s is a single rep bout, not a team encounter", id)
	}

	withdrawal := *partial
	withdrawal.Decision = string(domain.DecisionFusenpai)
	verr, err = refuseUnfinishedTeamFinish(store, stubScoringEngine{}, "team", "Pool A-1", &withdrawal)
	require.NoError(t, err)
	assert.Nil(t, verr, "a withdrawal is not a fought finish")

	verr, err = refuseUnfinishedTeamFinish(store, failingVacancies{}, "team", "Pool A-1", partial)
	require.NoError(t, err)
	require.NotNil(t, verr)
	assert.Equal(t, "Bout 2 and Bout 3 have no result. Record a score, a Tie, or a Fusensho before finishing.", verr.Error(),
		"a vacancy lookup that failed exempts nothing")

	verr, err = refuseUnfinishedTeamFinish(store, stubScoringEngine{}, "missing", "Pool A-1", partial)
	require.NoError(t, err)
	assert.Nil(t, verr, "a competition that does not exist is left to the engine write to report")

	_, err = refuseUnfinishedTeamFinish(store, stubScoringEngine{}, "bad/id", "Pool A-1", partial)
	assert.Error(t, err, "an unreadable competition is an error, not a pass")
}

// TestUnfinishedTeamBoutsMessage_SharedTable is the Go half of the shared
// copy table; the JS half reads the same file (unfinished_team_bouts.test.jsx).
func TestUnfinishedTeamBoutsMessage_SharedTable(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "unfinished_team_bouts.json"))
	require.NoError(t, err)
	var table struct {
		Cases []struct {
			TeamSize int    `json:"teamSize"`
			Bouts    []int  `json:"bouts"`
			Message  string `json:"message"`
		} `json:"cases"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.Cases)
	for _, c := range table.Cases {
		assert.Equal(t, c.Message, unfinishedTeamBoutsMessage(c.TeamSize, c.Bouts))
	}
	assert.Empty(t, unfinishedTeamBoutsMessage(5, nil))
}

func TestUnfinishedTeamBouts(t *testing.T) {
	subs := []state.SubMatchResult{
		{Position: 1, Winner: "A"},
		{Position: 2},
		{Position: state.DaihyosenSubPosition, Winner: "A", Decision: "daihyosen"},
		{Position: 4, Decision: "hikiwake"},
	}
	assert.Equal(t, []int{2, 3, 5}, unfinishedTeamBouts(subs, 5, nil))
	assert.Equal(t, []int{2, 5}, unfinishedTeamBouts(subs, 5, map[int]bool{3: true}))
}
