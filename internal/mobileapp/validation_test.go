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

// TestValidateDecision_UnknownDecision verifies that an unrecognized decision
// string is rejected.
func TestValidateDecision_UnknownDecision(t *testing.T) {
	req := ScoreRequest{Decision: "mystery"}
	err := req.Validate()
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "decision", verr.Field)
}

// TestValidateDecision_InvalidDecisionBy verifies that a decisionBy value
// other than "shiro" or "aka" is rejected.
func TestValidateDecision_InvalidDecisionBy(t *testing.T) {
	req := ScoreRequest{Decision: "hikiwake", DecisionBy: "blue"}
	err := req.Validate()
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "decisionBy", verr.Field)
}

// TestValidateDecision_KikenRequiresDecisionBy verifies that kiken without
// decisionBy returns an error on the decisionBy field.
func TestValidateDecision_KikenRequiresDecisionBy(t *testing.T) {
	req := ScoreRequest{Decision: "kiken"}
	err := req.Validate()
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "decisionBy", verr.Field)
}

// TestValidateDecision_FusenpaiRequiresDecisionBy verifies that fusenpai
// without decisionBy returns an error on the decisionBy field.
func TestValidateDecision_FusenpaiRequiresDecisionBy(t *testing.T) {
	req := ScoreRequest{Decision: "fusenpai"}
	err := req.Validate()
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "decisionBy", verr.Field)
}

// TestValidateDecision_FusenshoTopLevel verifies that fusensho is rejected at
// the top-level score endpoint (only valid on sub-results).
func TestValidateDecision_FusenshoTopLevel(t *testing.T) {
	req := ScoreRequest{Decision: "fusensho"}
	err := req.Validate()
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "decision", verr.Field)
}

// TestRequireWinnerForDecision_EmptyWinner verifies that kiken with a valid
// scoreline but no Winner field is rejected.
func TestRequireWinnerForDecision_EmptyWinner(t *testing.T) {
	req := ScoreRequest{
		Decision:   "kiken",
		DecisionBy: "shiro",
		IpponsA:    []string{"M", "K"},
	}
	err := req.Validate()
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "winner", verr.Field)
}

// TestValidateDecision_KikenBadScorelline verifies that kiken requires a
// 2-0 (or 1-0 in encho) scoreline.
func TestValidateDecision_KikenBadScoreline(t *testing.T) {
	req := ScoreRequest{
		Decision:   "kiken",
		DecisionBy: "shiro",
		// No ippons — fails winningScoreline check
	}
	err := req.Validate()
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "scoreline", verr.Field)
}

// TestValidateDecision_FusenpaiValidFull verifies that a complete fusenpai
// request (decisionBy + 2-0 scoreline + winner) passes validation.
func TestValidateDecision_FusenpaiValidFull(t *testing.T) {
	req := ScoreRequest{
		SideA:      "Alice",
		SideB:      "Bob",
		Decision:   "fusenpai",
		DecisionBy: "shiro",
		IpponsA:    []string{"M", "K"},
		Winner:     "Alice",
	}
	assert.NoError(t, req.Validate())
}

// TestValidateDecision_FusenpaiNoWinner verifies that fusenpai with a valid
// scoreline but no Winner is rejected.
func TestValidateDecision_FusenpaiNoWinner(t *testing.T) {
	req := ScoreRequest{
		Decision:   "fusenpai",
		DecisionBy: "shiro",
		IpponsA:    []string{"M", "K"},
	}
	err := req.Validate()
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "winner", verr.Field)
}

// TestValidateDecision_KachinukiExhaustionOk verifies that
// kachinuki-exhaustion is accepted (engine-generated value, not
// operator-entered).
func TestValidateDecision_KachinukiExhaustionOk(t *testing.T) {
	req := ScoreRequest{Decision: "kachinuki-exhaustion"}
	assert.NoError(t, req.Validate())
}
