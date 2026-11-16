package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

const lotsSentence = "This replaces the lots you entered for 1st: Ryu 1st, Tora 2nd. The bout's result will decide the place."

// lotsRuledServer is an individual league whose tie-breaker bout for 1st
// (Pool A-TB-0, Ryu and Tora) was saved as a draw and whose lots the organiser
// entered, Ryu first.
func lotsRuledServer(t *testing.T) (*gin.Engine, *state.Store) {
	t.Helper()
	const compID = "lots"
	r, store, _ := withdrawalServer(t,
		&state.Competition{ID: compID, Name: compID, Kind: "individual", Format: state.CompFormatLeague, Status: state.CompStatusPools},
		func(store *state.Store) {
			require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Courts: []string{"A"}}))
			players := []helper.Player{
				{ID: rwRyuID, Name: "Ryu", Dojo: "DojoR"},
				{ID: rwToraID, Name: "Tora", Dojo: "DojoT"},
				{ID: rwKumaID, Name: "Kuma", Dojo: "DojoK"},
			}
			require.NoError(t, store.SavePools(compID, []helper.Pool{{PoolName: "Pool A", Players: players}}))
			require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
				{ID: "Pool A-0", SideA: "Ryu", SideAID: rwRyuID, SideB: "Tora", SideBID: rwToraID, Court: "A",
					Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)},
				{ID: "Pool A-1", SideA: "Ryu", SideAID: rwRyuID, SideB: "Kuma", SideBID: rwKumaID, Court: "A",
					Status: state.MatchStatusCompleted, Winner: "Ryu", WinnerID: rwRyuID, IpponsA: []string{"M"}},
				{ID: "Pool A-2", SideA: "Tora", SideAID: rwToraID, SideB: "Kuma", SideBID: rwKumaID, Court: "A",
					Status: state.MatchStatusCompleted, Winner: "Tora", WinnerID: rwToraID, IpponsA: []string{"M"}},
				{ID: "Pool A-TB-0", SideA: "Ryu", SideAID: rwRyuID, SideB: "Tora", SideBID: rwToraID, Court: "A",
					Status: state.MatchStatusCompleted, Decision: string(domain.DecisionHikiwake)},
			}))
			require.NoError(t, store.SaveOverrides(compID, &state.Overrides{PoolRanks: map[string]map[string]int{
				"Pool A": {"id:" + rwRyuID: 1, "id:" + rwToraID: 2},
			}}))
		})
	return r, store
}

func lotsStillStored(t *testing.T, store *state.Store) {
	t.Helper()
	overrides, err := store.LoadOverrides("lots")
	require.NoError(t, err)
	assert.Len(t, overrides.PoolRanks["Pool A"], 2, "the lots stay stored")
}

func tiebreakerRow(t *testing.T, store *state.Store) state.MatchResult {
	t.Helper()
	all, err := store.LoadPoolMatches("lots")
	require.NoError(t, err)
	for _, m := range all {
		if m.ID == "Pool A-TB-0" {
			return m
		}
	}
	t.Fatal("no tie-breaker row")
	return state.MatchResult{}
}

func requireLotsReplaced(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &parsed))
	assert.Equal(t, "lots_replaced", parsed["error"])
	assert.Equal(t, "Pool A-TB-0", parsed["matchId"])
	assert.Equal(t, lotsSentence, parsed["message"])
}

// A result that would give the drawn tie-breaker a winner over lots the
// organiser entered is answered 409 lots_replaced with the sentence naming the
// place and the order, and lands only with the request-only confirmLots; the
// lots stay stored either way.
func TestScoreHandler_ResultOverTheLotsNeedsConfirmLots(t *testing.T) {
	r, store := lotsRuledServer(t)
	fought := func(extra map[string]any) map[string]any {
		body := map[string]any{
			"status": "completed", "winner": "Tora", "winnerId": rwToraID, "ipponsB": []string{"M"},
			"correctionReason": "Fought after all",
		}
		for k, v := range extra {
			body[k] = v
		}
		return body
	}

	w := putScore(t, r, "lots", "Pool A-TB-0", fought(nil))
	requireLotsReplaced(t, w)
	assert.Empty(t, tiebreakerRow(t, store).Winner, "refused: the bout stays drawn")

	w = putScore(t, r, "lots", "Pool A-TB-0", fought(map[string]any{"confirmLots": true}))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "Tora", tiebreakerRow(t, store).Winner, "the bout's result decides the place")
	lotsStillStored(t, store)
}

// The decision door asks the same: a fusensho on the drawn bout is refused with
// the sentence until the request carries confirmLots.
func TestDecisionHandler_DecisionOverTheLotsNeedsConfirmLots(t *testing.T) {
	r, store := lotsRuledServer(t)
	decision := map[string]any{"decision": "fusensho", "decisionBy": "aka"}

	w := postDecision(t, r, "lots", "Pool A-TB-0", decision)
	requireLotsReplaced(t, w)
	assert.Empty(t, tiebreakerRow(t, store).Winner)

	decision["confirmLots"] = true
	w = postDecision(t, r, "lots", "Pool A-TB-0", decision)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotEmpty(t, tiebreakerRow(t, store).Winner)
	lotsStillStored(t, store)
}

// Fight it again is the same warning, through the reopen door.
func TestReopenHandler_BoutTheLotsSettledNeedsConfirmLots(t *testing.T) {
	r, store := lotsRuledServer(t)

	w := postReopenRaw(t, r, "lots", "Pool A-TB-0", []byte(`{"reason":"Fight it again"}`))
	requireLotsReplaced(t, w)
	assert.Equal(t, state.MatchStatusCompleted, tiebreakerRow(t, store).Status, "refused: nothing changed")

	w = postReopenRaw(t, r, "lots", "Pool A-TB-0", []byte(`{"reason":"Fight it again","confirmLots":true}`))
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.NotEqual(t, state.MatchStatusCompleted, tiebreakerRow(t, store).Status, "the bout is open again")
	lotsStillStored(t, store)
}
