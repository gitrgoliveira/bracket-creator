package mobileapp

import (
	"errors"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// boolPtr returns a pointer to b, allowing inline *bool literals in test structs.
func boolPtr(b bool) *bool { return &b }

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

// TestScoreRequestValidate_IpponCounts covers the best-of-3 invariants
// added by validateIpponCounts: max 2 ippons per side, and 2-2 is
// rejected (impossible because the bout ends at first to 2). 1-1 and
// 2-1 must remain valid (time-out draw / regulation winner).
func TestScoreRequestValidate_IpponCounts(t *testing.T) {
	tests := []struct {
		name      string
		req       ScoreRequest
		wantErr   bool
		wantField string
	}{
		{
			name: "0-0 ok (no ippons, scheduled / drawn-with-no-score)",
			req:  ScoreRequest{},
		},
		{
			name: "1-0 ok (regulation winner)",
			req:  ScoreRequest{IpponsA: []string{"M"}},
		},
		{
			name: "1-1 ok (timeout draw)",
			req:  ScoreRequest{IpponsA: []string{"M"}, IpponsB: []string{"K"}},
		},
		{
			name: "2-0 ok (regulation winner)",
			req:  ScoreRequest{IpponsA: []string{"M", "K"}},
		},
		{
			name: "2-1 ok (regulation winner)",
			req:  ScoreRequest{IpponsA: []string{"M", "K"}, IpponsB: []string{"D"}},
		},
		{
			name:      "2-2 rejected (impossible — bout ends at first to 2)",
			req:       ScoreRequest{IpponsA: []string{"M", "K"}, IpponsB: []string{"D", "T"}},
			wantErr:   true,
			wantField: "ippons",
		},
		{
			name:      "3-0 rejected (exceeds best-of-3 cap)",
			req:       ScoreRequest{IpponsA: []string{"M", "K", "D"}},
			wantErr:   true,
			wantField: "ipponsA",
		},
		{
			name:      "0-3 rejected (exceeds best-of-3 cap)",
			req:       ScoreRequest{IpponsB: []string{"M", "K", "D"}},
			wantErr:   true,
			wantField: "ipponsB",
		},
		{
			name: "sub-bout 1-1 ok",
			req: ScoreRequest{
				SubResults: []state.SubMatchResult{
					{Position: 0, IpponsA: []string{"M"}, IpponsB: []string{"K"}},
				},
			},
		},
		{
			name: "sub-bout 2-2 rejected (impossible in best-of-3)",
			req: ScoreRequest{
				SubResults: []state.SubMatchResult{
					{Position: 0, IpponsA: []string{"M", "K"}, IpponsB: []string{"D", "T"}},
				},
			},
			wantErr:   true,
			wantField: "subResults[0].ippons",
		},
		{
			name: "second sub-bout violates (index reflected in field)",
			req: ScoreRequest{
				SubResults: []state.SubMatchResult{
					{Position: 0, IpponsA: []string{"M"}},
					{Position: 1, IpponsA: []string{"M", "K", "D"}},
				},
			},
			wantErr:   true,
			wantField: "subResults[1].ipponsA",
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

// TestValidateBulkScoreLengths_IpponCounts confirms the bulk-score path
// rejects the same impossible 2-2 scoreline so the bulk endpoint
// stays in lockstep with ScoreRequest.Validate.
func TestValidateBulkScoreLengths_IpponCounts(t *testing.T) {
	r := &state.MatchResult{
		SideA:   "Alice",
		SideB:   "Bob",
		IpponsA: []string{"M", "K"},
		IpponsB: []string{"D", "T"},
	}
	err := validateBulkScoreLengths(r)
	require.Error(t, err, "bulk-score 2-2 must be rejected by validateBulkScoreLengths")
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "ippons", verr.Field)
}

// TestValidateBulkScoreLengths_SubResultIppons confirms sub-bout
// invariants are also enforced on the bulk path.
func TestValidateBulkScoreLengths_SubResultIppons(t *testing.T) {
	r := &state.MatchResult{
		SideA: "TeamA",
		SideB: "TeamB",
		SubResults: []state.SubMatchResult{
			{Position: 0, IpponsA: []string{"M"}},
			{Position: 1, IpponsA: []string{"M", "K"}, IpponsB: []string{"D", "T"}},
		},
	}
	err := validateBulkScoreLengths(r)
	require.Error(t, err)
	var verr *ValidationError
	require.True(t, errors.As(err, &verr))
	assert.Equal(t, "subResults[1].ippons", verr.Field)
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

// TestValidateMaxLen covers the persisted-string cap helper. Empty
// strings always pass — presence is enforced separately.
func TestValidateMaxLen(t *testing.T) {
	tests := []struct {
		name      string
		val       string
		max       int
		wantField string
	}{
		{name: "empty under cap: ok", val: "", max: 10},
		{name: "exactly at cap: ok", val: strings.Repeat("x", 10), max: 10},
		{name: "one over cap: rejected", val: strings.Repeat("x", 11), max: 10, wantField: "field"},
		{name: "wildly over cap: rejected", val: strings.Repeat("x", 100000), max: 10, wantField: "field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateMaxLen("field", tt.val, tt.max)
			if tt.wantField == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr))
			assert.Equal(t, tt.wantField, verr.Field)
		})
	}
}

// TestScoreRequestValidate_LengthCaps verifies the persisted-string
// caps in ScoreRequest.Validate. decisionReason was previously
// unbounded on the score endpoint (only DecisionRequest enforced
// the 200-char contract) — this confirms the gap closure.
func TestScoreRequestValidate_LengthCaps(t *testing.T) {
	tests := []struct {
		name      string
		req       ScoreRequest
		wantField string
	}{
		{
			name:      "sideA over 100 chars",
			req:       ScoreRequest{SideA: strings.Repeat("a", 101)},
			wantField: "sideA",
		},
		{
			name:      "sideB over 100 chars",
			req:       ScoreRequest{SideB: strings.Repeat("b", 101)},
			wantField: "sideB",
		},
		{
			name:      "winner over 100 chars",
			req:       ScoreRequest{Winner: strings.Repeat("w", 101)},
			wantField: "winner",
		},
		{
			name:      "scheduledAt over 32 chars",
			req:       ScoreRequest{ScheduledAt: strings.Repeat("t", 33)},
			wantField: "scheduledAt",
		},
		{
			name:      "decisionReason over 200 chars: closes pre-existing gap",
			req:       ScoreRequest{DecisionReason: strings.Repeat("r", 201)},
			wantField: "decisionReason",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr))
			assert.Equal(t, tt.wantField, verr.Field)
		})
	}
}

// TestValidateBulkScoreLengths covers the bulk-score helper. The
// bulk-score endpoint bypasses ScoreRequest.Validate's caps because
// it writes raw state.MatchResult through RecordMatchResult — without
// this helper a 1MB sideA could land on disk via bulk-score even
// after the score endpoint's caps were added.
func TestValidateBulkScoreLengths(t *testing.T) {
	tests := []struct {
		name      string
		mr        state.MatchResult
		wantField string
	}{
		{
			name: "valid result: ok",
			mr: state.MatchResult{
				SideA: "Alice", SideB: "Bob", Winner: "Alice",
			},
		},
		{
			name:      "sideA over cap",
			mr:        state.MatchResult{SideA: strings.Repeat("a", 101)},
			wantField: "sideA",
		},
		{
			name:      "scheduledAt over cap",
			mr:        state.MatchResult{ScheduledAt: strings.Repeat("s", 33)},
			wantField: "scheduledAt",
		},
		{
			name:      "decisionReason over cap",
			mr:        state.MatchResult{DecisionReason: strings.Repeat("r", 201)},
			wantField: "decisionReason",
		},
		{
			name: "subResult sideB over cap",
			mr: state.MatchResult{
				SubResults: []state.SubMatchResult{
					{Position: 1, SideB: strings.Repeat("b", 101)},
				},
			},
			wantField: "subResults[0].sideB",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateBulkScoreLengths(&tt.mr)
			if tt.wantField == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr))
			assert.Equal(t, tt.wantField, verr.Field)
		})
	}
}

// TestValidatePlayerLengths covers the shared participant cap helper
// used by handlers_participants.go, handlers_competition.go (roster
// PUT), and handlers_import.go (manifest upload).
func TestValidatePlayerLengths(t *testing.T) {
	tests := []struct {
		name        string
		playerName  string
		displayName string
		dojo        string
		tag         string
		metadata    []string
		wantField   string
	}{
		{name: "all valid: ok", playerName: "Alice", dojo: "Dojo A"},
		{
			name:       "name over 100 chars",
			playerName: strings.Repeat("a", 101),
			wantField:  "name",
		},
		{
			name:        "displayName over 50 chars (physical zekken cap)",
			displayName: strings.Repeat("z", 51),
			wantField:   "displayName",
		},
		{
			name:      "dojo over 100 chars",
			dojo:      strings.Repeat("d", 101),
			wantField: "dojo",
		},
		{
			name:      "tag over 200 chars",
			tag:       strings.Repeat("t", 201),
			wantField: "tag",
		},
		{
			name:      "metadata > 16 entries",
			metadata:  make([]string, 17),
			wantField: "metadata",
		},
		{
			name:      "single metadata entry over 200 chars",
			metadata:  []string{strings.Repeat("m", 201)},
			wantField: "metadata[0]",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePlayerLengths(tt.playerName, tt.displayName, tt.dojo, tt.tag, tt.metadata)
			if tt.wantField == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr))
			assert.Equal(t, tt.wantField, verr.Field)
		})
	}
}

// TestCompetitorStatusRequestValidate verifies the eligibility request
// caps. domain.CompetitorStatus.Validate covers presence (PlayerID,
// Reason on ineligible) but not length — this fills that gap.
func TestCompetitorStatusRequestValidate(t *testing.T) {
	tests := []struct {
		name      string
		req       CompetitorStatusRequest
		wantField string
	}{
		{name: "valid: ok", req: CompetitorStatusRequest{PlayerID: "p1", Reason: "kiken"}},
		{
			name:      "playerId over 64 chars",
			req:       CompetitorStatusRequest{PlayerID: strings.Repeat("p", 65)},
			wantField: "playerId",
		},
		{
			name:      "matchId over 64 chars",
			req:       CompetitorStatusRequest{MatchID: strings.Repeat("m", 65)},
			wantField: "matchId",
		},
		{
			name:      "reason over 200 chars",
			req:       CompetitorStatusRequest{Reason: strings.Repeat("r", 201)},
			wantField: "reason",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantField == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr))
			assert.Equal(t, tt.wantField, verr.Field)
		})
	}
}

// TestSuneIpponRoundTrips confirms that "S" (Sune — shin strike, valid in
// Naginata) is accepted by validateIpponCounts and ScoreRequest.Validate.
// The server's ippon-count validator is letter-agnostic (counts only — it
// does not filter by allowed letter), so "S" must not cause a validation
// error regardless of competition type.
func TestSuneIpponRoundTrips(t *testing.T) {
	t.Run("S in ipponsA passes validateIpponCounts", func(t *testing.T) {
		err := validateIpponCounts("", []string{"S"}, []string{})
		assert.NoError(t, err, "Sune ippon must pass the count-only validator")
	})

	t.Run("S-K scoreline passes ScoreRequest.Validate", func(t *testing.T) {
		req := ScoreRequest{
			SideA:   "Alice",
			SideB:   "Bob",
			Winner:  "Alice",
			IpponsA: []string{"S", "K"},
		}
		assert.NoError(t, req.Validate(), "S-K scoreline must validate correctly")
	})

	t.Run("S in sub-result passes ScoreRequest.Validate", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{Position: 0, IpponsA: []string{"S"}, IpponsB: []string{}},
			},
		}
		assert.NoError(t, req.Validate(), "S in sub-result must validate correctly")
	})

	t.Run("three S ippons still exceeds best-of-3 cap", func(t *testing.T) {
		err := validateIpponCounts("", []string{"S", "S", "S"}, []string{})
		require.Error(t, err, "three ippons must be rejected")
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "ipponsA", verr.Field)
	})
}

func TestScoreRequestValidate_DecidedByHantei(t *testing.T) {
	encho1 := &state.EnchoMetadata{PeriodCount: 1}

	t.Run("valid hantei: completed with winner and encho", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
			Encho:           encho1,
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("valid hantei: no status supplied (partial update) with encho", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			DecidedByHantei: boolPtr(true),
			Encho:           encho1,
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("invalid hantei: no winner", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
			Encho:           encho1,
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "decidedByHantei", verr.Field)
	})

	t.Run("invalid hantei: status is running, not completed", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			Status:          state.MatchStatusRunning,
			DecidedByHantei: boolPtr(true),
			Encho:           encho1,
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "decidedByHantei", verr.Field)
	})

	t.Run("invalid hantei: no encho set", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "decidedByHantei", verr.Field)
	})

	t.Run("invalid hantei: encho period count is zero", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
			Encho:           &state.EnchoMetadata{PeriodCount: 0},
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "decidedByHantei", verr.Field)
	})

	t.Run("valid hantei: tied 1-1 scoreline", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
			Encho:           encho1,
			IpponsA:         []string{"M"},
			IpponsB:         []string{"K"},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("invalid hantei: non-tied scoreline (2-0)", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
			Encho:           encho1,
			IpponsA:         []string{"M", "K"},
			IpponsB:         nil,
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "decidedByHantei", verr.Field)
	})

	for _, decision := range []string{"hikiwake", "kiken-voluntary", "kiken-injury", "fusenpai", "daihyosen", "kachinuki-exhaustion"} {
		decision := decision
		t.Run("invalid hantei: decision "+decision+" incompatible", func(t *testing.T) {
			req := ScoreRequest{
				SideA:           "Alice",
				SideB:           "Bob",
				Winner:          "Alice",
				Status:          state.MatchStatusCompleted,
				DecidedByHantei: boolPtr(true),
				Encho:           encho1,
				Decision:        decision,
			}
			err := req.Validate()
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr))
			assert.Equal(t, "decidedByHantei", verr.Field)
		})
	}

	t.Run("invalid hantei: decisionBy set", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
			Encho:           encho1,
			DecisionBy:      "aka",
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "decidedByHantei", verr.Field)
	})

	t.Run("invalid hantei: decisionReason set", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
			Encho:           encho1,
			DecisionReason:  "injury",
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "decidedByHantei", verr.Field)
	})

	t.Run("valid hantei: decision fought is compatible", func(t *testing.T) {
		req := ScoreRequest{
			SideA:           "Alice",
			SideB:           "Bob",
			Winner:          "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
			Encho:           encho1,
			Decision:        "fought",
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("decidedByHantei false is always valid", func(t *testing.T) {
		req := ScoreRequest{DecidedByHantei: boolPtr(false)}
		assert.NoError(t, req.Validate())
	})
}

func TestScoreRequestValidate_SubBoutDecidedByHantei(t *testing.T) {
	enchoOne := &state.EnchoMetadata{PeriodCount: 1}

	t.Run("valid: sub-bout hantei with winner, encho, tied scoreline", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M"}, IpponsB: []string{"K"},
					Winner: "TeamA", DecidedByHantei: true, Encho: enchoOne,
				},
			},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("invalid: sub-bout hantei without winner", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M"}, IpponsB: []string{"K"},
					Winner: "", DecidedByHantei: true, Encho: enchoOne,
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].decidedByHantei", verr.(*ValidationError).Field)
	})

	t.Run("invalid: sub-bout hantei without encho", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M"}, IpponsB: []string{"K"},
					Winner: "TeamA", DecidedByHantei: true, Encho: nil,
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].decidedByHantei", verr.(*ValidationError).Field)
	})

	t.Run("invalid: sub-bout hantei with non-tied scoreline", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", "K"}, IpponsB: []string{"D"},
					Winner: "TeamA", DecidedByHantei: true, Encho: enchoOne,
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].decidedByHantei", verr.(*ValidationError).Field)
	})

	t.Run("invalid: sub-bout hantei incompatible with hikiwake decision", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M"}, IpponsB: []string{"K"},
					Winner: "TeamA", Decision: "hikiwake", DecidedByHantei: true, Encho: enchoOne,
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].decidedByHantei", verr.(*ValidationError).Field)
	})

	t.Run("valid: sub-bout hantei with decision fought is compatible", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M"}, IpponsB: []string{"K"},
					Winner: "TeamA", Decision: "fought", DecidedByHantei: true, Encho: enchoOne,
				},
			},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("valid: 0-0 tied encho decided by hantei", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: nil, IpponsB: nil,
					Winner: "TeamA", DecidedByHantei: true, Encho: enchoOne,
				},
			},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("error prefix uses correct subResults index", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M"}, IpponsB: []string{"K"},
					Winner: "TeamA", DecidedByHantei: true, Encho: enchoOne,
				},
				{
					Position: 2, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M"}, IpponsB: []string{"K"},
					Winner: "TeamA", DecidedByHantei: true, Encho: nil, // invalid: no encho
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[1].decidedByHantei", verr.(*ValidationError).Field)
	})
}
