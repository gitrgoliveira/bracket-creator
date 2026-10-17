package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// bc-evts stage 3 review 12, Z3: the remaining doors the server orders by
// arrival (a match lineup's removal, a chusen's order, a reset of the
// overrides, a reinstatement) answer a late duplicate as recorded, like the
// court move and the lineup save of review 11 (Y8): the request the outbox
// gave up on lands after its resend and after the next write of the same
// target, and must not undo it.

func TestLateDuplicate_LineupRemovalIsAnsweredAsRecorded(t *testing.T) {
	f := newTeamWritesFixture(t, false)
	changed := []domain.Position{domain.PosSenpo}
	save := func(name, writeID string) LineupRequest {
		return LineupRequest{Positions: map[domain.Position]string{domain.PosSenpo: name}, Changed: &changed, WriteID: writeID}
	}
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, f.lineupPath("PoolA-0"), "main-pw", save("Ito", "w-save-1")).Code)
	require.Equal(t, http.StatusNoContent, f.send(http.MethodDelete, f.lineupPath("PoolA-0"), "main-pw", map[string]string{"writeId": "w-remove-1"}).Code)
	_, ok := f.savedLineup(t, "PoolA-0")
	require.False(t, ok, "the removal applied")
	require.Equal(t, http.StatusOK, f.send(http.MethodPut, f.lineupPath("PoolA-0"), "main-pw", save("Kato", "w-save-2")).Code)

	w := f.send(http.MethodDelete, f.lineupPath("PoolA-0"), "main-pw", map[string]string{"writeId": "w-remove-1"})
	require.Equal(t, http.StatusNoContent, w.Code, w.Body.String())
	l, ok := f.savedLineup(t, "PoolA-0")
	require.True(t, ok, "the late removal does not take the newer save away")
	assert.Equal(t, "Kato", l.Positions[domain.PosSenpo])
}

func TestLateDuplicate_ChusenOrderAndResetAreAnsweredAsRecorded(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	compID := "once-ranks"
	seedMixedCompWithSeatedKnockout(t, store, compID, state.MatchStatusScheduled)
	order := func(first, second, writeID string) map[string]any {
		return map[string]any{
			"ranks":   []map[string]any{rankEntry(first, 1), rankEntry(second, 2)},
			"writeId": writeID,
		}
	}
	put := func(body map[string]any) int {
		return sendJSON(t, r, http.MethodPut, "/api/competitions/"+compID+"/pools/Pool A/override-rank", body).Code
	}
	reset := func(writeID string) int {
		return sendJSON(t, r, http.MethodDelete, "/api/competitions/"+compID+"/overrides", map[string]string{"writeId": writeID}).Code
	}
	first := map[string]int{helper.CompetitorKey("a2-id", "", ""): 1, helper.CompetitorKey("a1-id", "", ""): 2}
	second := map[string]int{helper.CompetitorKey("a1-id", "", ""): 1, helper.CompetitorKey("a2-id", "", ""): 2}

	require.Equal(t, http.StatusOK, put(order("a2-id", "a1-id", "w-rank-1")))
	require.Equal(t, http.StatusOK, put(order("a1-id", "a2-id", "w-rank-2")))
	require.Equal(t, http.StatusOK, put(order("a2-id", "a1-id", "w-rank-1")), "a late duplicate is answered as recorded")
	assert.Equal(t, second, poolARanks(t, store, compID), "the late order does not go back on over the corrected one")
	assert.NotEqual(t, first, poolARanks(t, store, compID))

	require.Equal(t, http.StatusNoContent, reset("w-reset-1"))
	require.Empty(t, poolARanks(t, store, compID))
	require.Equal(t, http.StatusOK, put(order("a2-id", "a1-id", "w-rank-3")))
	require.Equal(t, http.StatusNoContent, reset("w-reset-1"), "a late reset is answered as recorded")
	assert.Equal(t, first, poolARanks(t, store, compID), "the late reset does not clear the newer order")
}

// countingEligibilityEngine counts the reinstatements the door applied.
type countingEligibilityEngine struct{ calls int }

func (e *countingEligibilityEngine) ReinstateCompetitor(_, playerID string) (*domain.CompetitorStatus, error) {
	e.calls++
	return &domain.CompetitorStatus{PlayerID: playerID, Eligible: true}, nil
}

func TestLateDuplicate_ReinstatementIsAnsweredAsRecorded(t *testing.T) {
	const pw = "pass"
	eng := &countingEligibilityEngine{}
	r, store := setupReinstateTestRouter(t, eng)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: pw}))
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))
	send := func(writeID string) int {
		body, err := json.Marshal(map[string]string{"writeId": writeID})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/api/competitions/c1/competitors/p1/reinstate", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tournament-Password", pw)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}
	require.Equal(t, http.StatusOK, send("w-reinstate-1"))
	require.Equal(t, 1, eng.calls)
	require.Equal(t, http.StatusOK, send("w-reinstate-1"), "a late duplicate is answered as recorded")
	assert.Equal(t, 1, eng.calls, "and the competitor is not reinstated again")
	require.Equal(t, http.StatusOK, send("w-reinstate-2"))
	assert.Equal(t, 2, eng.calls, "another write is applied")
}
