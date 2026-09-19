package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A decision now carries the client's write stamp (mp-jnvl), so it is subject
// to the same clock-skew refusal PUT /score has: a stamp implausibly far in the
// server's future can be neither trusted nor zeroed (zero is the
// unconditional-apply bypass), so the write is refused outright.
//
// The refusal must be HTTP 200 with applied:false, never a 4xx/5xx: the SPA's
// offline queue retries 5xx forever, and a skewed write can never win a retry
// while the clock stays wrong (the mp-q8c6 poisoned-queue pattern).
func TestDecisionHandler_RefusesAFarFutureStamp(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

	body, _ := json.Marshal(DecisionRequest{
		Decision:   "kiken",
		DecisionBy: "shiro",
		// Well beyond modifiedAtRefuseSkewMs.
		ModifiedAt: time.Now().Add(time.Hour).UnixMilli(),
	})
	req := httptest.NewRequest(http.MethodPost,
		"/api/competitions/c1/matches/m1/decision", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var got map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, false, got["applied"])
	assert.Equal(t, "clock_skew", got["reason"])
	// Refused BEFORE the transaction, so the match is never even looked up:
	// a skewed write must not be reported as "not found" either.
	assert.NotContains(t, w.Body.String(), "not found")
}

// A stamp the server can believe is passed through to the engine rather than
// being dropped at the boundary. A negative one is garbage and is clamped to
// the unstamped bypass instead of freezing the match against later writes.
func TestDecisionRequest_CarriesTheStampField(t *testing.T) {
	var req DecisionRequest
	require.NoError(t, json.Unmarshal([]byte(`{"decision":"kiken","decisionBy":"aka","modifiedAt":1700000000000}`), &req))
	assert.Equal(t, int64(1_700_000_000_000), req.ModifiedAt)

	assert.Zero(t, clampClientModifiedAt(-1), "a negative stamp clamps to the unstamped bypass")

	_, _, refuse := clientClockSkew(time.Now().Add(-time.Hour).UnixMilli())
	assert.False(t, refuse, "a past stamp is never a refusal")
}
