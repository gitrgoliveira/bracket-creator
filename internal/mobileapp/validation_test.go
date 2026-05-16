package mobileapp

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestScoreRequestValidate covers the request-shape rules in
// ScoreRequest.Validate (Slice 0 / T015 / NFR-004). Slice 3 (T077)
// extends this with decision-type validation; rules here are the
// minimal Slice-0 set and must remain stable as later slices add to it.
func TestScoreRequestValidate(t *testing.T) {
	tests := []struct {
		name      string
		req       ScoreRequest
		wantErr   bool
		wantField string
	}{
		{
			name: "empty status: ok (engine preserve-on-empty handles it)",
			req:  ScoreRequest{},
		},
		{
			name: "status scheduled: ok",
			req:  ScoreRequest{Status: state.MatchStatusScheduled},
		},
		{
			name: "status running: ok",
			req:  ScoreRequest{Status: state.MatchStatusRunning},
		},
		{
			name: "status completed: ok",
			req:  ScoreRequest{Status: state.MatchStatusCompleted},
		},
		{
			name:      "unknown status: rejected on the status field",
			req:       ScoreRequest{Status: "garbage"},
			wantErr:   true,
			wantField: "status",
		},
		{
			name:      "legacy alias 'complete' not accepted (frontend serializer maps it first)",
			req:       ScoreRequest{Status: "complete"},
			wantErr:   true,
			wantField: "status",
		},
		{
			name: "winner matches sideA: ok",
			req:  ScoreRequest{SideA: "Alice", SideB: "Bob", Winner: "Alice"},
		},
		{
			name: "winner matches sideB: ok",
			req:  ScoreRequest{SideA: "Alice", SideB: "Bob", Winner: "Bob"},
		},
		{
			name: "empty winner with both sides set: ok (draw / pre-completion)",
			req:  ScoreRequest{SideA: "Alice", SideB: "Bob", Winner: ""},
		},
		{
			name: "winner with sideB omitted: ok (engine preserve-on-empty handles it)",
			req:  ScoreRequest{SideA: "Alice", SideB: "", Winner: "Alice"},
		},
		{
			name: "winner with sideA omitted: ok (engine preserve-on-empty handles it)",
			req:  ScoreRequest{SideA: "", SideB: "Bob", Winner: "Bob"},
		},
		{
			name:      "winner names neither side with both sides present: rejected on winner field",
			req:       ScoreRequest{SideA: "Alice", SideB: "Bob", Winner: "Charlie"},
			wantErr:   true,
			wantField: "winner",
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

// TestScoreRequestAsMatchResult exercises the zero-cost conversion the
// score handler uses to forward a validated request to the engine.
// Both directions of conversion must round-trip without losing fields.
func TestScoreRequestAsMatchResult(t *testing.T) {
	original := state.MatchResult{
		ID:       "m1",
		SideA:    "Alice",
		SideB:    "Bob",
		Winner:   "Alice",
		Status:   state.MatchStatusCompleted,
		IpponsA:  []string{"M", "K"},
		IpponsB:  []string{"D"},
		HansokuA: 1,
		HansokuB: 0,
	}
	req := ScoreRequest(original)
	mr := req.AsMatchResult()
	assert.Equal(t, original.ID, mr.ID)
	assert.Equal(t, original.SideA, mr.SideA)
	assert.Equal(t, original.SideB, mr.SideB)
	assert.Equal(t, original.Winner, mr.Winner)
	assert.Equal(t, original.Status, mr.Status)
	assert.Equal(t, original.IpponsA, mr.IpponsA)
	assert.Equal(t, original.IpponsB, mr.IpponsB)
	assert.Equal(t, original.HansokuA, mr.HansokuA)
	assert.Equal(t, original.HansokuB, mr.HansokuB)
}

// TestValidationErrorFormat covers the typed error's two presentation
// modes (with and without a Field). Handlers map ValidationError to
// HTTP 400 with the verr.Error() string as the user-facing message.
func TestValidationErrorFormat(t *testing.T) {
	t.Run("with field", func(t *testing.T) {
		err := &ValidationError{Field: "status", Message: "must be one of …"}
		assert.Equal(t, "status: must be one of …", err.Error())
	})
	t.Run("without field", func(t *testing.T) {
		err := &ValidationError{Message: "top-level shape error"}
		assert.Equal(t, "top-level shape error", err.Error())
	})
}
