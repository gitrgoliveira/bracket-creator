package mobileapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDecisionRequestValidate covers all shape-validation paths in
// DecisionRequest.Validate().
func TestDecisionRequestValidate(t *testing.T) {
	tests := []struct {
		name      string
		req       DecisionRequest
		wantErr   bool
		wantField string
	}{
		{
			name: "kiken with shiro: ok",
			req:  DecisionRequest{Decision: "kiken", DecisionBy: "shiro"},
		},
		{
			name: "fusenpai with aka: ok",
			req:  DecisionRequest{Decision: "fusenpai", DecisionBy: "aka"},
		},
		{
			name: "fusensho with shiro: ok",
			req:  DecisionRequest{Decision: "fusensho", DecisionBy: "shiro"},
		},
		{
			name: "daihyosen with aka: ok",
			req:  DecisionRequest{Decision: "daihyosen", DecisionBy: "aka"},
		},
		{
			name:      "empty decision is required",
			req:       DecisionRequest{},
			wantErr:   true,
			wantField: "decision",
		},
		{
			name:      "fought is unsupported on /decision endpoint",
			req:       DecisionRequest{Decision: "fought"},
			wantErr:   true,
			wantField: "decision",
		},
		{
			name:      "hikiwake is unsupported on /decision endpoint",
			req:       DecisionRequest{Decision: "hikiwake"},
			wantErr:   true,
			wantField: "decision",
		},
		{
			name:      "missing decisionBy",
			req:       DecisionRequest{Decision: "kiken"},
			wantErr:   true,
			wantField: "decisionBy",
		},
		{
			name:      "invalid decisionBy (not shiro or aka)",
			req:       DecisionRequest{Decision: "kiken", DecisionBy: "red"},
			wantErr:   true,
			wantField: "decisionBy",
		},
		{
			name:      "decisionReason over 200 characters",
			req:       DecisionRequest{Decision: "kiken", DecisionBy: "shiro", DecisionReason: strings.Repeat("x", 201)},
			wantErr:   true,
			wantField: "decisionReason",
		},
		{
			name: "decisionReason exactly 200 characters: ok",
			req:  DecisionRequest{Decision: "kiken", DecisionBy: "shiro", DecisionReason: strings.Repeat("x", 200)},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if !tt.wantErr {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var verr *ValidationError
			require.Truef(t, errors.As(err, &verr), "want *ValidationError, got %T", err)
			assert.Equal(t, tt.wantField, verr.Field)
		})
	}
}

// TestDecisionHandler_InvalidJSON verifies that a malformed JSON body returns 400.
func TestDecisionHandler_InvalidJSON(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

	req := httptest.NewRequest(http.MethodPost,
		"/api/competitions/c1/matches/m1/decision",
		bytes.NewBufferString("{bad-json"))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestDecisionHandler_ValidationError verifies that a request with an
// unsupported decision type (fought) returns 400.
func TestDecisionHandler_ValidationError(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

	body, _ := json.Marshal(DecisionRequest{Decision: "fought"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/competitions/c1/matches/m1/decision",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestDecisionHandler_MatchNotFound verifies that a valid kiken request for
// a non-existent match returns 404.
func TestDecisionHandler_MatchNotFound(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{ID: "c1"}))

	body, _ := json.Marshal(DecisionRequest{Decision: "kiken", DecisionBy: "shiro"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/competitions/c1/matches/no-such-match/decision",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

// TestDecisionHandler_EngiGuard is a Finding 9 regression test: kiken and
// fusenpai decisions make no sense for engi competitions (no ippons), so the
// endpoint must return 400 before touching the engine.
func TestDecisionHandler_EngiGuard(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "engi-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:   cid,
		Engi: true,
	}))

	for _, dec := range []string{"kiken", "kiken-voluntary", "kiken-injury", "fusenpai", "fusensho", "daihyosen"} {
		body, _ := json.Marshal(DecisionRequest{Decision: dec, DecisionBy: "shiro"})
		req := httptest.NewRequest(http.MethodPost,
			"/api/competitions/"+cid+"/matches/m1/decision",
			bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		assert.Equalf(t, http.StatusBadRequest, w.Code,
			"decision %q on engi comp must return 400; body: %s", dec, w.Body.String())
		assert.Contains(t, w.Body.String(), "engi",
			"error message must mention engi for decision %q", dec)
	}
}

// TestDecisionHandler_NonEngiUnaffected verifies that a non-engi competition
// is not blocked by the engi guard and proceeds normally (404 expected because
// there is no such match, not 400 from the guard).
func TestDecisionHandler_NonEngiUnaffected(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "kendo-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:   cid,
		Engi: false,
	}))

	body, _ := json.Marshal(DecisionRequest{Decision: "kiken", DecisionBy: "shiro"})
	req := httptest.NewRequest(http.MethodPost,
		"/api/competitions/"+cid+"/matches/no-such-match/decision",
		bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	// The guard must NOT fire for non-engi; expect 404 (match not found), not 400.
	assert.Equal(t, http.StatusNotFound, w.Code,
		"non-engi competition must not be blocked by engi guard; expected 404 from engine")
}

// TestDecisionHandler_CorruptOverrides_TerminalError is bc-pnum gap 3, the
// decision-endpoint sibling of TestScoreHandler_CorruptOverrides_TerminalErrorThenRepairable
// (handlers_match_test.go): RecordDecisionTx writes through
// RecordMatchResultWithIneligibilityTx, which for a MIXED competition's pool
// match re-score runs the mp-e2k1 guard's computeStandingsFrom call -- the
// same LoadOverrides call the score handler's corrupt-overrides fix already
// covered. Before this fix the decision handler's error switch had no branch
// for state.ErrCorruptOverrides, so it fell through to the default
// internalError (500) arm; the SPA's offline write queue retries 5xx
// forever, so a genuinely corrupt file would poison the queue rather than
// surface once. The fix adds a case calling the shared
// respondIfCorruptOverrides helper (errors.go), matching the mapping every
// other LoadOverrides-reaching handler now has.
func TestDecisionHandler_CorruptOverrides_TerminalError(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "corrupt-ov-decision"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, PoolWinners: 2, Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Alice", Dojo: "DojoA"}, {Name: "Bob", Dojo: "DojoB"},
		}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusScheduled},
	}))

	// Corrupt overrides.json directly: the competition directory must already
	// exist, which SaveCompetition above guaranteed.
	overridesPath := filepath.Join(store.GetFolder(), "competitions", compID, "overrides.json")
	require.NoError(t, os.WriteFile(overridesPath, []byte("{not valid json"), 0o600))

	body, _ := json.Marshal(DecisionRequest{Decision: "kiken", DecisionBy: "aka"})
	// http.NewRequest (not httptest.NewRequest): the match id "Pool A-0"
	// contains a literal space, and httptest.NewRequest builds the request by
	// parsing a raw "METHOD target HTTP/1.0" line, where an unescaped space
	// in target breaks the line into the wrong number of fields. http.NewRequest
	// goes through url.Parse instead, which tolerates the literal space,
	// matching TestScoreHandler_CorruptOverrides_TerminalErrorThenRepairable's
	// identical request construction (handlers_match_test.go).
	req, err := http.NewRequest(http.MethodPost,
		"/api/competitions/"+compID+"/matches/Pool A-0/decision",
		bytes.NewBuffer(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusUnprocessableEntity, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "corrupt_overrides")

	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	assert.Equal(t, state.MatchStatusScheduled, stored[0].Status, "the rejected write must not have landed")
}
