package mobileapp

// Clear withdrawal and reopen, at the HTTP boundary (operator ruling
// 2026-09-24: "Everything should be able to be fixed, in case of a wrong
// entry. The operator just needs to be aware of the consequences, if it
// affects downstream matches."). POST .../reopen accepts any completed match a
// withdrawal decided, individual included; a knockout match whose later round
// has its own result answers the shared downstream_knockout_played 409, and a
// retry with forceDownstreamReopen reopens both, names what it reopened and
// broadcasts every change.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	rwRyuID  = "11111111-1111-4111-8111-111111111111"
	rwToraID = "22222222-2222-4222-8222-222222222222"
	rwKumaID = "33333333-3333-4333-8333-333333333333"
)

// setupIndividualKnockoutWithdrawal: an individual knockout where Tora
// (shiro) did not appear against Ryu in round 1, so Ryu went through and has
// since WON the final against Kuma.
func setupIndividualKnockoutWithdrawal(t *testing.T) (*gin.Engine, *state.Store, *Hub) {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()
	const compID = "rw"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "rw", Kind: "individual", Status: state.CompStatusKnockout}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: rwRyuID, Name: "Ryu", Dojo: "DojoR"},
		{ID: rwToraID, Name: "Tora", Dojo: "DojoT"},
		{ID: rwKumaID, Name: "Kuma", Dojo: "DojoK"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{{ID: "m-r1-0", SideA: "Ryu", SideAID: rwRyuID, SideB: "Tora", SideBID: rwToraID, Status: state.MatchStatusRunning, MatchNumber: 1, DisplayRound: 2}},
		{{ID: "m-r2-0", SideB: "Kuma", SideBID: rwKumaID, MatchNumber: 2, DisplayRound: 1}},
	}}))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)

	w := postDecision(t, r, compID, "m-r1-0", map[string]any{"decision": "fusenpai", "decisionBy": "shiro"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NoError(t, store.UpdateBracket(compID, func(b *state.Bracket) error {
		f := &b.Rounds[1][0]
		require.Equal(t, "Ryu", f.SideA, "precondition: Ryu went through")
		f.Status, f.Winner, f.WinnerID, f.IpponsA = state.MatchStatusCompleted, "Ryu", rwRyuID, []string{"M", "K"}
		return nil
	}))
	return r, store, hub
}

func collectEvents(t *testing.T, hub *Hub) func() []string {
	t.Helper()
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
	return func() []string {
		hub.Unsubscribe(ch)
		<-done
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), events...)
	}
}

func TestReopenHandler_WithdrawalWithAPlayedFinalWarnsThenProceeds(t *testing.T) {
	r, store, hub := setupIndividualKnockoutWithdrawal(t)

	w := postReopen(t, r, "rw", "m-r1-0", "Withdrawal recorded by mistake")
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var refusal map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &refusal))
	assert.Equal(t, "downstream_knockout_played", refusal["error"])
	assert.Equal(t, "m-r2-0", refusal["blockingMatchId"])
	assert.Equal(t, "Ryu", refusal["displaced"])
	b, err := store.LoadBracket("rw")
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[0][0].Status, "nothing changes until the operator confirms")
	assert.Equal(t, state.MatchStatusCompleted, b.Rounds[1][0].Status)

	stop := collectEvents(t, hub)
	body, err := json.Marshal(map[string]any{"reason": "Withdrawal recorded by mistake", "forceDownstreamReopen": true})
	require.NoError(t, err)
	w = postReopenRaw(t, r, "rw", "m-r1-0", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	events := stop()

	var resp struct {
		ReopenedMatches []struct {
			ID     string `json:"id"`
			Number int    `json:"number"`
			Label  string `json:"label"`
		} `json:"reopenedMatches"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.ReopenedMatches, 1)
	assert.Equal(t, "m-r2-0", resp.ReopenedMatches[0].ID)
	assert.Equal(t, 2, resp.ReopenedMatches[0].Number)
	assert.Equal(t, "Match 2 (Final)", resp.ReopenedMatches[0].Label, "the words the reopened notice shows")

	b, err = store.LoadBracket("rw")
	require.NoError(t, err)
	r1, final := b.Rounds[0][0], b.Rounds[1][0]
	assert.Equal(t, state.MatchStatusRunning, r1.Status)
	assert.Equal(t, "", r1.Decision)
	assert.Empty(t, r1.IpponsA, "Ryu's maru went with the verdict")
	assert.Equal(t, state.MatchStatusScheduled, final.Status)
	assert.Empty(t, final.Winner)
	assert.Equal(t, "Winner of r2-m0", final.SideA)
	statuses, err := store.LoadCompetitorStatus("rw")
	require.NoError(t, err)
	assert.True(t, statuses[rwToraID].Eligible, "Tora can compete again")

	joined := strings.Join(events, "\n")
	assert.Contains(t, joined, `"matchId":"m-r1-0"`)
	assert.Contains(t, joined, `"matchId":"m-r2-0"`, "the reopened final is broadcast too")
	assert.Contains(t, joined, `"type":"competitor_status_updated"`)
	assert.Contains(t, joined, `"playerId":"`+rwToraID+`"`)
}

// An individual pool match a withdrawal decided reopens with the struck
// letters kept; a fought individual match is still refused with 400.
func TestReopenHandler_IndividualPoolWithdrawal(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	eng := engine.New(store)
	hub := NewHub()
	const compID = "rwp"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "rwp", Kind: "individual", Format: state.CompFormatLeague, Status: state.CompStatusPools}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: rwRyuID, Name: "Ryu", Dojo: "DojoR"},
		{ID: rwToraID, Name: "Tora", Dojo: "DojoT"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Ryu", SideAID: rwRyuID, SideB: "Tora", SideBID: rwToraID, IpponsA: []string{"M"}, Status: state.MatchStatusRunning},
		{ID: "Pool A-1", SideA: "Tora", SideAID: rwToraID, SideB: "Ryu", SideBID: rwRyuID, Status: state.MatchStatusCompleted,
			Winner: "Tora", WinnerID: rwToraID, IpponsA: []string{"M", "M"}},
	}))
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)
	w := postDecision(t, r, compID, "Pool A-0", map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	w = postReopen(t, r, compID, "Pool A-0", "Withdrawal recorded by mistake")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	ms, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	assert.Equal(t, state.MatchStatusRunning, ms[0].Status)
	assert.Equal(t, []string{"M"}, ms[0].IpponsA, "what Ryu struck before the withdrawal is kept")
	assert.Empty(t, ms[0].IpponsB)

	w = postReopen(t, r, compID, "Pool A-1", "Scoring error")
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "correct other results via the score editor")
}

// A withdrawal cleared with no reason owes one on the next completion, and a
// decision is one: POST /decision reads ReopenPending for every format now,
// not only kachinuki, so re-recording a withdrawal on the reopened match
// without a reason is refused rather than leaving the flag set on a
// completed match.
func TestDecisionHandler_ReopenedWithdrawalOwesItsReason(t *testing.T) {
	r, store, _ := setupIndividualKnockoutWithdrawal(t)
	require.NoError(t, store.UpdateBracket("rw", func(b *state.Bracket) error {
		f := &b.Rounds[1][0]
		f.Status, f.Winner, f.WinnerID, f.IpponsA = state.MatchStatusScheduled, "", "", nil
		return nil
	}))
	w := postReopenRaw(t, r, "rw", "m-r1-0", []byte(`{}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	b, err := store.LoadBracket("rw")
	require.NoError(t, err)
	require.True(t, b.Rounds[0][0].ReopenPending)

	w = postDecision(t, r, "rw", "m-r1-0", map[string]any{"decision": "fusenpai", "decisionBy": "aka"})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "decisionReason")

	w = postDecision(t, r, "rw", "m-r1-0", map[string]any{"decision": "fusenpai", "decisionBy": "aka", "decisionReason": "It was Yamada who did not appear"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	b, err = store.LoadBracket("rw")
	require.NoError(t, err)
	assert.False(t, b.Rounds[0][0].ReopenPending, "the reason discharged it")
}

// withdrawalServer wires the match and decision handlers over a fresh store
// seeded by seed, for the tests below that drive a whole reopen through HTTP.
func withdrawalServer(t *testing.T, comp *state.Competition, seed func(store *state.Store)) (*gin.Engine, *state.Store, *Hub) {
	t.Helper()
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(comp.ID, []domain.Player{
		{ID: rwRyuID, Name: "Ryu", Dojo: "DojoR"},
		{ID: rwToraID, Name: "Tora", Dojo: "DojoT"},
		{ID: rwKumaID, Name: "Kuma", Dojo: "DojoK"},
	}))
	seed(store)
	hub := NewHub()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	eng := engine.New(store)
	RegisterMatchHandlers(admin, eng, store, store, hub, NewFileVerifier(store), store)
	RegisterDecisionHandlers(admin, eng, store, store, hub)
	return r, store, hub
}

// TestReopen_OlderQueuedWriteIsRefusedAfterward: a reopen stamps the match
// with the server's now, so a write stamped BEFORE it cannot undo it. The
// case: a Save correction made after the withdrawal was recorded, held in an
// offline queue, replays after "Clear withdrawal and reopen". Without the
// stamp the match kept the decision's time, the older write won
// last-write-wins and completed the running match again. With it the write is
// refused as superseded (200 {"applied": false}, the refusal contract) and the
// match stays running. Every reopen door stamps: the plain reopen on each
// home, the kachinuki reopen, and the court-busy remedy.
func TestReopen_OlderQueuedWriteIsRefusedAfterward(t *testing.T) {
	decidedAt := time.Now().Add(-time.Hour).UnixMilli()
	queuedAt := decidedAt + 60_000

	type door struct {
		name    string
		serve   func(t *testing.T) (*gin.Engine, *state.Store, string)
		matchID string
		reopen  func(t *testing.T, r *gin.Engine, compID string) *httptest.ResponseRecorder
		stale   map[string]any
		status  func(t *testing.T, store *state.Store, compID string) state.MatchStatus
	}
	individualCorrection := map[string]any{
		"sideA": "Ryu", "sideB": "Tora", "winner": "Tora",
		"ipponsA": []string{"M"}, "ipponsB": []string{"○", "○"},
		"status": "completed", "correctionReason": "Scoring error: wrong waza entered",
		"modifiedAt": queuedAt,
	}
	poolStatus := func(t *testing.T, store *state.Store, compID string) state.MatchStatus {
		ms, err := store.LoadPoolMatches(compID)
		require.NoError(t, err)
		for _, m := range ms {
			if m.ID == "Pool A-0" || m.ID == "P1-0" {
				return m.Status
			}
		}
		t.Fatalf("match not found")
		return ""
	}
	bracketStatus := func(t *testing.T, store *state.Store, compID string) state.MatchStatus {
		b, err := store.LoadBracket(compID)
		require.NoError(t, err)
		return b.Rounds[0][0].Status
	}
	reopen := func(matchID string) func(t *testing.T, r *gin.Engine, compID string) *httptest.ResponseRecorder {
		return func(t *testing.T, r *gin.Engine, compID string) *httptest.ResponseRecorder {
			return postReopen(t, r, compID, matchID, "Withdrawal recorded by mistake")
		}
	}
	// individualPool: Ryu (aka) withdraws from Pool A-0 at decidedAt. With
	// blocker, the court's next match is then running on court A: Tora v
	// Kuma, both eligible (Ryu is the one the kiken barred).
	individualPool := func(blocker bool) func(t *testing.T) (*gin.Engine, *state.Store, string) {
		return func(t *testing.T) (*gin.Engine, *state.Store, string) {
			const compID = "rw-lww-pool"
			r, store, _ := withdrawalServer(t, &state.Competition{ID: compID, Name: compID, Kind: "individual", Format: state.CompFormatLeague, Status: state.CompStatusPools},
				func(store *state.Store) {
					require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
						{ID: "Pool A-0", SideA: "Ryu", SideAID: rwRyuID, SideB: "Tora", SideBID: rwToraID, Court: "A", IpponsA: []string{"M"}, Status: state.MatchStatusRunning},
					}))
				})
			w := postDecision(t, r, compID, "Pool A-0", map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka", "modifiedAt": decidedAt})
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			if blocker {
				ms, err := store.LoadPoolMatches(compID)
				require.NoError(t, err)
				require.NoError(t, store.SavePoolMatches(compID, append(ms, state.MatchResult{
					ID: "Pool A-1", SideA: "Tora", SideAID: rwToraID, SideB: "Kuma", SideBID: rwKumaID, Court: "A", Status: state.MatchStatusRunning,
				})))
			}
			return r, store, compID
		}
	}
	doors := []door{
		{name: "reopen, pool match", serve: individualPool(false), matchID: "Pool A-0",
			reopen: reopen("Pool A-0"), stale: individualCorrection, status: poolStatus},
		{name: "requeue-blocker-and-reopen", serve: individualPool(true), matchID: "Pool A-0",
			reopen: func(t *testing.T, r *gin.Engine, compID string) *httptest.ResponseRecorder {
				return postRequeueAndReopen(t, r, compID, "Pool A-0", map[string]any{
					"blockerCompId": compID, "blockerMatchId": "Pool A-1", "reason": "Withdrawal recorded by mistake",
				})
			},
			stale: individualCorrection, status: poolStatus},
		{name: "reopen, knockout match",
			serve: func(t *testing.T) (*gin.Engine, *state.Store, string) {
				const compID = "rw-lww-ko"
				r, store, _ := withdrawalServer(t, &state.Competition{ID: compID, Name: compID, Kind: "individual", Status: state.CompStatusKnockout},
					func(store *state.Store) {
						require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
							{{ID: "m-r1-0", SideA: "Ryu", SideAID: rwRyuID, SideB: "Tora", SideBID: rwToraID, Court: "A", IpponsA: []string{"M"}, Status: state.MatchStatusRunning, MatchNumber: 1}},
							{{ID: "m-r2-0", SideB: "Kuma", SideBID: rwKumaID, MatchNumber: 2}},
						}}))
					})
				w := postDecision(t, r, compID, "m-r1-0", map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka", "modifiedAt": decidedAt})
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				return r, store, compID
			},
			matchID: "m-r1-0", reopen: reopen("m-r1-0"), stale: individualCorrection, status: bracketStatus},
		{name: "kachinuki reopen",
			serve: func(t *testing.T) (*gin.Engine, *state.Store, string) {
				const compID = "rw-lww-kachinuki"
				r, store := setupKachinukiScoreServer(t, compID)
				players, err := store.LoadParticipants(compID, false)
				require.NoError(t, err)
				ids := map[string]string{}
				for _, p := range players {
					ids[p.Name] = p.ID
				}
				require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
					ID: "P1-0", SideA: "Ryu", SideAID: ids["Ryu"], SideB: "Tora", SideBID: ids["Tora"], Court: "A", Status: state.MatchStatusRunning,
					SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1", IpponsA: []string{"M"}, Winner: "R-1", Decision: "fought"}},
				}}))
				w := postDecision(t, r, compID, "P1-0", map[string]any{"decision": "kiken-voluntary", "decisionBy": "aka", "modifiedAt": decidedAt})
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				return r, store, compID
			},
			matchID: "P1-0", reopen: reopen("P1-0"),
			stale: map[string]any{
				"sideA": "Ryu", "sideB": "Tora", "winner": "Ryu", "status": "completed", "decision": "kachinuki-exhaustion",
				"subResults":       []map[string]any{kachinukiSub(1, "R-1", "W-1", []string{"M", "K"}, "R-1", "fought")},
				"correctionReason": "Scoring error: wrong waza entered", "modifiedAt": queuedAt,
			},
			status: poolStatus},
	}
	for _, d := range doors {
		t.Run(d.name, func(t *testing.T) {
			r, store, compID := d.serve(t)
			w := d.reopen(t, r, compID)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			require.Equal(t, state.MatchStatusRunning, d.status(t, store, compID), "precondition: reopened")

			w = putScore(t, r, compID, d.matchID, d.stale)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
			assert.Equal(t, false, body["applied"], "a write stamped before the reopen must not land; got %s", w.Body.String())
			assert.Equal(t, "superseded", body["reason"])
			assert.Equal(t, state.MatchStatusRunning, d.status(t, store, compID), "the reopened match stays running")
		})
	}
}

// TestReopenHandler_ForceReopenedFinalWithdrawalIsRestoredAndBroadcast: the
// final a confirmed reopen reopens was itself decided by Kuma's kiken. That
// withdrawal goes with its verdict, so Kuma is eligible again and the restore
// is broadcast beside the final's own match_updated, exactly as for the match
// the operator reopened.
func TestReopenHandler_ForceReopenedFinalWithdrawalIsRestoredAndBroadcast(t *testing.T) {
	const compID = "rw-final-kiken"
	r, store, hub := withdrawalServer(t, &state.Competition{ID: compID, Name: compID, Kind: "individual", Status: state.CompStatusKnockout},
		func(store *state.Store) {
			require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
				{{ID: "m-r1-0", SideA: "Ryu", SideAID: rwRyuID, SideB: "Tora", SideBID: rwToraID, Status: state.MatchStatusRunning, MatchNumber: 1}},
				{{ID: "m-r2-0", SideB: "Kuma", SideBID: rwKumaID, MatchNumber: 2}},
			}}))
		})
	w := postDecision(t, r, compID, "m-r1-0", map[string]any{"decision": "fusenpai", "decisionBy": "shiro"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	w = postDecision(t, r, compID, "m-r2-0", map[string]any{"decision": "kiken-voluntary", "decisionBy": "shiro", "decisionReason": "knee"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	require.False(t, statuses[rwKumaID].Eligible, "precondition: the final's kiken barred Kuma")

	stop := collectEvents(t, hub)
	body, err := json.Marshal(map[string]any{"reason": "Withdrawal recorded by mistake", "forceDownstreamReopen": true})
	require.NoError(t, err)
	w = postReopenRaw(t, r, compID, "m-r1-0", body)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	events := stop()

	statuses, err = store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	assert.True(t, statuses[rwKumaID].Eligible, "Kuma can compete in the reopened final")
	var kumaRestored bool
	for _, e := range events {
		kumaRestored = kumaRestored || (strings.Contains(e, `"type":"competitor_status_updated"`) &&
			strings.Contains(e, `"playerId":"`+rwKumaID+`"`) && strings.Contains(e, `"eligible":true`))
	}
	assert.True(t, kumaRestored, "Kuma's restore is broadcast; got %v", events)
}
