package mobileapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

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
			require.True(t, errors.As(err, &verr), "want *ValidationError, got %T", err)
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
