package mobileapp

import (
	"errors"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
			require.Truef(t, errors.As(err, &verr), "want *ValidationError, got %T", err)
			assert.Equal(t, tt.wantField, verr.Field)
		})
	}
}

// TestScoreRequestValidate_IpponCounts covers the best-of-3 invariants
// added by validateIppons: max 2 ippons per side, and 2-2 is
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
			name:      "2-2 rejected (impossible, bout ends at first to 2)",
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
			require.Truef(t, errors.As(err, &verr), "want *ValidationError, got %T", err)
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
	err := validateBulkScoreLengths(r, false)
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
	err := validateBulkScoreLengths(r, false)
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

// TestValidateDecision_RequiresDecisionBy verifies that every default-win
// decision the top-level validator accepts (including the legacy "kiken"
// alias, which the whitelist normalizes) demands decisionBy.
func TestValidateDecision_RequiresDecisionBy(t *testing.T) {
	for _, decision := range []string{"kiken", "fusenpai"} {
		t.Run(decision, func(t *testing.T) {
			req := ScoreRequest{Decision: decision}
			err := req.Validate()
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr))
			assert.Equal(t, "decisionBy", verr.Field)
		})
	}
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

// TestRequireWinnerForDecision_EmptyWinner verifies that a default win
// with a valid scoreline but no Winner field is rejected (kiken and
// fusenpai share the merged validator arm).
func TestRequireWinnerForDecision_EmptyWinner(t *testing.T) {
	for _, decision := range []string{"kiken", "fusenpai"} {
		t.Run(decision, func(t *testing.T) {
			req := ScoreRequest{
				Decision:   decision,
				DecisionBy: "shiro",
				IpponsA:    []string{"M", "K"},
			}
			err := req.Validate()
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr))
			assert.Equal(t, "winner", verr.Field)
		})
	}
}

// TestValidateDecision_KikenBadScorelline verifies that kiken requires a
// 2-0 (or 1-0 in encho) scoreline.
func TestValidateDecision_KikenBadScoreline(t *testing.T) {
	req := ScoreRequest{
		Decision:   "kiken",
		DecisionBy: "shiro",
		// No ippons, fails winningScoreline check
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

// TestValidateDecision_KachinukiExhaustionOk verifies that
// kachinuki-exhaustion is accepted (engine-generated value, not
// operator-entered).
func TestValidateDecision_KachinukiExhaustionOk(t *testing.T) {
	req := ScoreRequest{Decision: "kachinuki-exhaustion"}
	assert.NoError(t, req.Validate())
}

// TestValidateMaxLen covers the persisted-string cap helper. Empty
// strings always pass, presence is enforced separately.
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
// the 200-char contract), this confirms the gap closure.
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
		{
			name:      "correctionReason over 200 chars",
			req:       ScoreRequest{CorrectionReason: strings.Repeat("c", 201)},
			wantField: "correctionReason",
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

// TestReasonCaps_ValidateTrimmedValue verifies the audit free-text length caps
// are applied to the TRIMMED value, matching the write path which persists
// strings.TrimSpace(reason). A reason that is within the cap once normalized
// must not be rejected for surrounding whitespace.
func TestReasonCaps_ValidateTrimmedValue(t *testing.T) {
	atCap := strings.Repeat("c", MaxLenCorrectionReason)
	padded := "  " + atCap + "   " // over the cap raw, within it once trimmed

	t.Run("ScoreRequest.Validate correctionReason", func(t *testing.T) {
		req := ScoreRequest{CorrectionReason: padded}
		require.NoError(t, req.Validate(),
			"correctionReason within cap after trim must not be rejected")
	})
	t.Run("validateBulkScoreLengths correctionReason", func(t *testing.T) {
		require.NoError(t, validateBulkScoreLengths(&state.MatchResult{CorrectionReason: padded}, false),
			"bulk correctionReason within cap after trim must not be rejected")
	})
}

// TestValidateBulkScoreLengths covers the bulk-score helper. The
// bulk-score endpoint bypasses ScoreRequest.Validate's caps because
// it writes raw state.MatchResult through RecordMatchResult, without
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
			name:      "correctionReason over cap",
			mr:        state.MatchResult{CorrectionReason: strings.Repeat("c", 201)},
			wantField: "correctionReason",
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
			err := validateBulkScoreLengths(&tt.mr, false)
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
		source      string
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
			name:      "source over 200 chars",
			source:    strings.Repeat("t", 201),
			wantField: "source",
		},
		{
			name:       "valid registration source: ok",
			playerName: "Alice",
			source:     "registered",
		},
		{
			name:       "valid source any case: ok (normalized elsewhere)",
			playerName: "Alice",
			source:     "Manual",
		},
		{
			name:       "legacy 'reserved' accepted (aliased to manual)",
			playerName: "Alice",
			source:     "reserved",
		},
		{
			name:       "unknown registration source rejected",
			playerName: "Alice",
			source:     "vip",
			wantField:  "source",
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
			err := validatePlayerLengths(tt.playerName, tt.displayName, tt.dojo, tt.source, tt.metadata)
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
// Reason on ineligible) but not length, this fills that gap.
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

// TestSuneIpponRoundTrips confirms that "S" (Sune, shin strike, valid in
// Naginata) is accepted by validateIppons and ScoreRequest.Validate.
// The server's ippon-count validator is letter-agnostic (counts only, it
// does not filter by allowed letter); so "S" must not cause a validation
// error regardless of competition type.
func TestSuneIpponRoundTrips(t *testing.T) {
	t.Run("S in ipponsA passes validateIppons", func(t *testing.T) {
		err := validateIppons("", []string{"S"}, []string{})
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
		err := validateIppons("", []string{"S", "S", "S"}, []string{})
		require.Error(t, err, "three ippons must be rejected")
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "ipponsA", verr.Field)
	})
}

// TestScoreRequestValidate_Hantei covers the request-shape rules for a
// judges'-decision verdict. The verdict is the domain.HanteiMark entry in the
// WINNER's ippon slice (operator ruling 2026-08-21), not the legacy
// DecidedByHantei *bool, so every subtest here supplies the mark directly in
// IpponsA/IpponsB rather than going through the legacy flag. The one
// exception is the dedicated legacy-acceptance subtest at the end (rule:
// tests that specifically pin the offline-queue compat channel may still
// send the flag). Error field names moved from "decidedByHantei" to "ippons"
// (validateHanteiMarkPlacement), since the mark now lives inside the ippons
// the request already carries.
func TestScoreRequestValidate_Hantei(t *testing.T) {
	encho1 := &state.EnchoMetadata{PeriodCount: 1}
	// A 0-0 hantei: the winner's slice holds just the mark, per the "a hantei
	// only needs a free slot" reasoning in domain.HanteiMark's doc comment.
	winnerMark := []string{domain.HanteiMark}

	t.Run("valid hantei: completed with winner and encho", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			Encho:   encho1,
			IpponsA: winnerMark, IpponsB: []string{},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("valid hantei: no status supplied (partial update) with encho", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Encho:   encho1,
			IpponsA: winnerMark, IpponsB: []string{},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("invalid hantei: no winner", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob",
			Status:  state.MatchStatusCompleted,
			Encho:   encho1,
			IpponsA: winnerMark, IpponsB: []string{},
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "ippons", verr.Field)
	})

	t.Run("invalid hantei: status is running, not completed", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusRunning,
			Encho:   encho1,
			IpponsA: winnerMark, IpponsB: []string{},
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "ippons", verr.Field)
	})

	t.Run("valid hantei: no encho set (encho is not required)", func(t *testing.T) {
		// Encho was decoupled from hantei, a tied match may be taken straight
		// to a judges' decision without an overtime period.
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: winnerMark, IpponsB: []string{},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("valid hantei: encho period count is zero", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			Encho:   &state.EnchoMetadata{PeriodCount: 0},
			IpponsA: winnerMark, IpponsB: []string{},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("valid hantei: tied 1-1 scoreline", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			Encho:   encho1,
			IpponsA: []string{"M", domain.HanteiMark},
			IpponsB: []string{"K"},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("invalid hantei: non-tied scoreline (0-1)", func(t *testing.T) {
		// A hantei match can only ever be 0-0 or 1-1 (sanbon-shobu ends at 2,
		// so a side with 2 real ippons already won without judges): there is
		// no structurally valid way to combine 2 real ippons with the mark on
		// the same side (that would be 3 entries, rejected by the per-side
		// cap before the tie check even runs). This shape — mark alone
		// against one real ippon — untied 0-vs-1 without hitting that cap.
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			Encho:   encho1,
			IpponsA: winnerMark,
			IpponsB: []string{"K"},
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "ippons", verr.Field)
		assert.Contains(t, verr.Message, "tied scoreline")
	})

	for _, decision := range []string{"hikiwake", "kiken-voluntary", "kiken-injury", "fusenpai", "daihyosen", "kachinuki-exhaustion"} {
		decision := decision
		t.Run("invalid hantei: decision "+decision+" incompatible", func(t *testing.T) {
			req := ScoreRequest{
				SideA: "Alice", SideB: "Bob", Winner: "Alice",
				Status:   state.MatchStatusCompleted,
				Encho:    encho1,
				Decision: decision,
				IpponsA:  winnerMark, IpponsB: []string{},
			}
			err := req.Validate()
			require.Error(t, err)
			var verr *ValidationError
			require.True(t, errors.As(err, &verr))
			assert.Equal(t, "ippons", verr.Field)
		})
	}

	t.Run("invalid hantei: decisionBy set", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:     state.MatchStatusCompleted,
			Encho:      encho1,
			DecisionBy: "aka",
			IpponsA:    winnerMark, IpponsB: []string{},
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "ippons", verr.Field)
	})

	t.Run("invalid hantei: decisionReason set", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:         state.MatchStatusCompleted,
			Encho:          encho1,
			DecisionReason: "injury",
			IpponsA:        winnerMark, IpponsB: []string{},
		}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.True(t, errors.As(err, &verr))
		assert.Equal(t, "ippons", verr.Field)
	})

	t.Run("valid hantei: decision fought is compatible", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:   state.MatchStatusCompleted,
			Encho:    encho1,
			Decision: "fought",
			IpponsA:  winnerMark, IpponsB: []string{},
		}
		assert.NoError(t, req.Validate())
	})

	// Legacy-acceptance pins (rule: the offline queue can replay a pre-ruling
	// payload for hours after a binary upgrade, so ScoreRequest.Validate must
	// still accept the *bool channel and fold it into the mark).
	t.Run("legacy decidedByHantei=true is normalized into the mark", func(t *testing.T) {
		req := ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
		}
		require.NoError(t, req.Validate())
		assert.True(t, domain.ContainsHantei(req.IpponsA),
			"the legacy flag must fold into the winner's ippon slice")
		assert.Nil(t, req.DecidedByHantei, "normalization clears the legacy field")
	})

	t.Run("legacy decidedByHantei=false is always valid", func(t *testing.T) {
		req := ScoreRequest{DecidedByHantei: boolPtr(false)}
		assert.NoError(t, req.Validate())
	})
}

// TestScoreRequestValidate_SubBoutHantei pins the sub-bout hantei rules. As
// with the match-level twin, the verdict is domain.HanteiMark inside the
// WINNER's ippon slice, so fixtures inject it directly rather than through
// the legacy DecidedByHantei *bool — sending the flag alone hits two edges
// the new model doesn't have an equivalent for: normalizeLegacyHantei DROPS
// an unattributable (winner-less) verdict rather than guessing a side, and
// AppendHantei onto an already-full 2-entry ippon slice grows it to 3 (a
// STRUCTURAL cap violation, a different error than the one being pinned).
// Both are why "without winner" and "non-tied scoreline" below place the mark
// directly rather than round-tripping it through the flag.
func TestScoreRequestValidate_SubBoutHantei(t *testing.T) {
	enchoOne := &state.EnchoMetadata{PeriodCount: 1}
	mark := domain.HanteiMark

	// Hantei is only valid on the daihyosen representative bout (Position == -1).
	t.Run("invalid: hantei on regular bout position", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA",
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].ippons", verr.(*ValidationError).Field)
	})

	t.Run("invalid: encho on regular bout position", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", "K"}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: enchoOne,
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].encho", verr.(*ValidationError).Field)
	})

	t.Run("invalid: negative encho on regular bout position", func(t *testing.T) {
		// A negative period count would slip past the > 0 guard and be
		// silently treated as "no encho". It must be rejected outright.
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M"}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: &state.EnchoMetadata{PeriodCount: -1},
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].encho", verr.(*ValidationError).Field)
		assert.Contains(t, verr.Error(), "must not be negative")
	})

	t.Run("invalid: negative encho on daihyosen position", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: state.DaihyosenSubPosition, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M"}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: &state.EnchoMetadata{PeriodCount: -3},
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].encho", verr.(*ValidationError).Field)
		assert.Contains(t, verr.Error(), "must not be negative")
	})

	t.Run("valid: zero encho on regular bout position is a no-op", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", "K"}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: &state.EnchoMetadata{PeriodCount: 0},
				},
			},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("valid: daihyosen hantei with winner, encho, tied scoreline", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: state.DaihyosenSubPosition, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: enchoOne,
				},
			},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("invalid: daihyosen hantei without winner", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: state.DaihyosenSubPosition, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "", Encho: enchoOne,
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].ippons", verr.(*ValidationError).Field)
	})

	t.Run("valid: daihyosen hantei without encho (encho not required)", func(t *testing.T) {
		// Encho was decoupled from hantei, a tied daihyosen may be decided by
		// judges without an overtime period.
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: nil,
				},
			},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("invalid: daihyosen hantei with non-tied scoreline", func(t *testing.T) {
		// The mark occupies a slot, and sanbon-shobu caps a side at 2 entries,
		// so a hantei sub-bout can only be 0-0 or 1-1: this puts the mark alone
		// on the winner's side (0 real ippons) against 2 real ippons on the
		// other, untied without hitting that structural cap.
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{mark}, IpponsB: []string{"D", "T"},
					Winner: "TeamA", Encho: enchoOne,
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].ippons", verr.(*ValidationError).Field)
		assert.Contains(t, verr.Error(), "tied scoreline")
	})

	t.Run("invalid: daihyosen hantei incompatible with hikiwake decision", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA", Decision: "hikiwake", Encho: enchoOne,
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].ippons", verr.(*ValidationError).Field)
	})

	t.Run("valid: daihyosen hantei with decision daihyosen is compatible", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA", Decision: "daihyosen", Encho: enchoOne,
				},
			},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("valid: daihyosen hantei with decision fought is compatible", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA", Decision: "fought", Encho: enchoOne,
				},
			},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("valid: daihyosen 0-0 tied encho decided by hantei", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{mark}, IpponsB: []string{},
					Winner: "TeamA", Encho: enchoOne,
				},
			},
		}
		assert.NoError(t, req.Validate())
	})

	t.Run("error prefix uses correct subResults index", func(t *testing.T) {
		req := ScoreRequest{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: enchoOne,
				},
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					// invalid: non-tied scoreline (0 real vs 2 real, same
					// cap-avoiding shape as the standalone test above)
					IpponsA: []string{mark}, IpponsB: []string{"K", "D"},
					Winner: "TeamA", Encho: enchoOne,
				},
			},
		}
		verr := req.Validate()
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[1].ippons", verr.(*ValidationError).Field)
	})
}

// TestValidateBulkScoreLengths_SubBoutHantei pins the bulk-import path.
// validateBulkScoreLengths has no NormalizeLegacyHantei call of its own (only
// parsePoolMatchesRecords, LoadBracket, and ScoreRequest.validateWithOptions
// are conversion sites — state/legacy_hantei.go), so the legacy
// DecidedByHantei *bool is genuinely inert here: fixtures place the mark in
// ippons directly, exactly as the single-score twin's tests do.
func TestValidateBulkScoreLengths_SubBoutHantei(t *testing.T) {
	enchoOne := &state.EnchoMetadata{PeriodCount: 1}
	mark := domain.HanteiMark

	t.Run("invalid: hantei on regular position rejected on bulk path", func(t *testing.T) {
		r := &state.MatchResult{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA",
				},
			},
		}
		verr := validateBulkScoreLengths(r, false)
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].ippons", verr.(*ValidationError).Field)
	})

	t.Run("invalid: encho on regular position rejected on bulk path", func(t *testing.T) {
		r := &state.MatchResult{
			SubResults: []state.SubMatchResult{
				{
					Position: 1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", "K"}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: enchoOne,
				},
			},
		}
		verr := validateBulkScoreLengths(r, false)
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].encho", verr.(*ValidationError).Field)
	})

	t.Run("valid: daihyosen hantei accepted on bulk path", func(t *testing.T) {
		r := &state.MatchResult{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: enchoOne,
				},
			},
		}
		assert.NoError(t, validateBulkScoreLengths(r, false))
	})

	t.Run("invalid: daihyosen hantei without winner rejected on bulk path", func(t *testing.T) {
		r := &state.MatchResult{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "", Encho: enchoOne,
				},
			},
		}
		verr := validateBulkScoreLengths(r, false)
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].ippons", verr.(*ValidationError).Field)
	})

	t.Run("valid: daihyosen hantei without encho accepted on bulk path", func(t *testing.T) {
		r := &state.MatchResult{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
					Winner: "TeamA", Encho: nil,
				},
			},
		}
		assert.NoError(t, validateBulkScoreLengths(r, false))
	})

	t.Run("invalid: daihyosen hantei with non-tied scoreline rejected on bulk path", func(t *testing.T) {
		r := &state.MatchResult{
			SubResults: []state.SubMatchResult{
				{
					Position: -1, SideA: "TeamA", SideB: "TeamB",
					IpponsA: []string{mark}, IpponsB: []string{"D", "T"},
					Winner: "TeamA", Encho: enchoOne,
				},
			},
		}
		verr := validateBulkScoreLengths(r, false)
		require.IsType(t, &ValidationError{}, verr)
		assert.Equal(t, "subResults[0].ippons", verr.(*ValidationError).Field)
	})
}

// TestValidateBulkScoreLengths_MatchLevelHantei is the match-level twin of
// TestValidateBulkScoreLengths_SubBoutHantei. Before this fix,
// validateBulkScoreLengths ran only mark placement + tied-scoreline (via an
// inline ContainsHantei block), omitting the completed-status check, the
// compatible-decision check, and the DecisionBy/DecisionReason-empty checks
// that ScoreRequest.validateWithOptions runs on the single-score path. A row
// shaped {winner, ippons carrying the mark, tied, decision: hikiwake} (or one
// naming decisionBy) therefore passed bulk with a nil error while the single
// endpoint 400s the identical payload, and the batch response counted the row
// as succeeded — the engine's stripInvalidHantei then silently discarded the
// verdict bulk had just accepted, with no error surfaced anywhere. Both paths
// now share validateMatchHantei, so they must reject the same payload with
// the exact same message.
func TestValidateBulkScoreLengths_MatchLevelHantei(t *testing.T) {
	mark := domain.HanteiMark

	t.Run("mark + incompatible decision (hikiwake) rejected identically on both paths", func(t *testing.T) {
		mr := state.MatchResult{
			SideA: "TeamA", SideB: "TeamB", Winner: "TeamA",
			IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
			Decision: "hikiwake", Status: state.MatchStatusCompleted,
		}

		bulkErr := validateBulkScoreLengths(&mr, false)
		require.Error(t, bulkErr, "bulk must reject a mark alongside an incompatible decision")

		sr := ScoreRequest(mr)
		singleErr := sr.Validate()
		require.Error(t, singleErr, "single-score path must reject the identical payload")

		assert.Equal(t, singleErr.Error(), bulkErr.Error(), "bulk and single must reject with the SAME message")
	})

	t.Run("mark + decisionBy rejected identically on both paths", func(t *testing.T) {
		mr := state.MatchResult{
			SideA: "TeamA", SideB: "TeamB", Winner: "TeamA",
			IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
			DecisionBy: "shiro", Status: state.MatchStatusCompleted,
		}

		bulkErr := validateBulkScoreLengths(&mr, false)
		require.Error(t, bulkErr, "bulk must reject a mark alongside a decisionBy audit field")

		sr := ScoreRequest(mr)
		singleErr := sr.Validate()
		require.Error(t, singleErr, "single-score path must reject the identical payload")

		assert.Equal(t, singleErr.Error(), bulkErr.Error(), "bulk and single must reject with the SAME message")
	})

	t.Run("mark on a non-completed status rejected identically on both paths", func(t *testing.T) {
		mr := state.MatchResult{
			SideA: "TeamA", SideB: "TeamB", Winner: "TeamA",
			IpponsA: []string{"M", mark}, IpponsB: []string{"K"},
			Status: state.MatchStatusRunning,
		}

		bulkErr := validateBulkScoreLengths(&mr, false)
		require.Error(t, bulkErr, "bulk must reject a mark on a still-running match")

		sr := ScoreRequest(mr)
		singleErr := sr.Validate()
		require.Error(t, singleErr, "single-score path must reject the identical payload")

		assert.Equal(t, singleErr.Error(), bulkErr.Error(), "bulk and single must reject with the SAME message")
	})
}

// TestIsSelfRunReportableDecision covers the allowlist and rejection cases
// for participant self-reporting in self-run tournaments.
//
// The second parameter used to be the legacy *bool decidedByHantei tri-state;
// it is now hanteiDecided, a plain bool computed from
// MatchResult.HanteiDecided() (the domain.HanteiMark presence test) at the
// call site (handlers_match.go). There is no third "nil" state any more: the
// mark either is or is not in the ippons.
func TestIsSelfRunReportableDecision(t *testing.T) {
	tests := []struct {
		name          string
		decision      string
		hanteiDecided bool
		want          bool
	}{
		{name: "empty decision allowed", decision: "", want: true},
		{name: "fought allowed", decision: "fought", want: true},
		{name: "hikiwake allowed", decision: "hikiwake", want: true},
		{name: "fusensho rejected at top level", decision: "fusensho", want: false},
		{name: "kiken-voluntary rejected", decision: "kiken-voluntary", want: false},
		{name: "kiken-injury rejected", decision: "kiken-injury", want: false},
		{name: "fusenpai rejected", decision: "fusenpai", want: false},
		{name: "daihyosen rejected", decision: "daihyosen", want: false},
		{name: "kachinuki-exhaustion rejected", decision: "kachinuki-exhaustion", want: false},
		{name: "unknown decision rejected", decision: "magic", want: false},
		{name: "hanteiDecided=true rejects even allowed decision", decision: "fought", hanteiDecided: true, want: false},
		{name: "hanteiDecided=false is ok", decision: "fought", hanteiDecided: false, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsSelfRunReportableDecision(tc.decision, tc.hanteiDecided)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestIsSelfRunReportableSubDecision(t *testing.T) {
	tests := []struct {
		name            string
		decision        string
		decidedByHantei bool
		position        int
		want            bool
	}{
		{name: "empty sub-decision allowed", decision: "", position: 1, want: true},
		{name: "fought sub-decision allowed", decision: "fought", position: 1, want: true},
		{name: "hikiwake sub-decision allowed", decision: "hikiwake", position: 1, want: true},
		{name: "fusensho sub-decision allowed", decision: "fusensho", position: 1, want: true},
		{name: "kiken-voluntary sub rejected", decision: "kiken-voluntary", position: 1, want: false},
		{name: "daihyosen sub rejected", decision: "daihyosen", position: 1, want: false},
		{name: "position -1 (daihyosen slot) rejected", decision: "", position: -1, want: false},
		{name: "decidedByHantei true sub rejected", decision: "fought", decidedByHantei: true, position: 1, want: false},
		{name: "position 0 allowed", decision: "fought", position: 0, want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := IsSelfRunReportableSubDecision(tc.decision, tc.decidedByHantei, tc.position)
			assert.Equal(t, tc.want, got)
		})
	}
}

// TestScoreRequestValidate_RevSessionCap verifies that an over-long revSession
// (> MaxLenRevSession=64) is rejected with a 400-mapped ValidationError, while
// empty (legacy clients) and UUID-length (36 chars) values pass.
func TestScoreRequestValidate_RevSessionCap(t *testing.T) {
	t.Run("empty revSession passes (legacy clients)", func(t *testing.T) {
		req := ScoreRequest{RevSession: ""}
		assert.NoError(t, req.Validate())
	})

	t.Run("UUID-length revSession passes", func(t *testing.T) {
		req := ScoreRequest{RevSession: "550e8400-e29b-41d4-a716-446655440000"}
		assert.NoError(t, req.Validate())
	})

	t.Run("over-long revSession rejected", func(t *testing.T) {
		req := ScoreRequest{RevSession: strings.Repeat("x", 65)}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.Truef(t, errors.As(err, &verr), "want *ValidationError, got %T", err)
		assert.Equal(t, "revSession", verr.Field)
	})
}

// TestScoreRequestValidate_RevNonNegative verifies that a negative rev is
// rejected (it would slip past the handler's Rev>0 guard and let a stale running
// write clobber newer state), while rev==0 (unversioned opt-out) and positive
// revs pass.
func TestScoreRequestValidate_RevNonNegative(t *testing.T) {
	t.Run("rev=0 (unversioned) passes", func(t *testing.T) {
		req := ScoreRequest{Rev: 0}
		assert.NoError(t, req.Validate())
	})

	t.Run("positive rev passes", func(t *testing.T) {
		req := ScoreRequest{Rev: 42}
		assert.NoError(t, req.Validate())
	})

	t.Run("negative rev rejected", func(t *testing.T) {
		req := ScoreRequest{Rev: -1}
		err := req.Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.Truef(t, errors.As(err, &verr), "want *ValidationError, got %T", err)
		assert.Equal(t, "rev", verr.Field)
	})
}

// TestScoreRequestValidate_FlagsNonNegative verifies the HTTP-layer guard
// rejects negative engi referee-flag counts on the field that carries them.
// Zero (a running/partial save) and valid positive counts pass; the {1,3,5}
// completed-match total is enforced by the engine, not here.
func TestScoreRequestValidate_FlagsNonNegative(t *testing.T) {
	t.Run("zero flags pass (running/partial)", func(t *testing.T) {
		assert.NoError(t, (&ScoreRequest{FlagsA: 0, FlagsB: 0}).Validate())
	})
	t.Run("valid positive flags pass", func(t *testing.T) {
		assert.NoError(t, (&ScoreRequest{FlagsA: 3, FlagsB: 2}).Validate())
	})
	t.Run("negative flagsA rejected", func(t *testing.T) {
		err := (&ScoreRequest{FlagsA: -1}).Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.Truef(t, errors.As(err, &verr), "want *ValidationError, got %T", err)
		assert.Equal(t, "flagsA", verr.Field)
	})
	t.Run("negative flagsB rejected", func(t *testing.T) {
		err := (&ScoreRequest{FlagsB: -1}).Validate()
		require.Error(t, err)
		var verr *ValidationError
		require.Truef(t, errors.As(err, &verr), "want *ValidationError, got %T", err)
		assert.Equal(t, "flagsB", verr.Field)
	})
	t.Run("negative flagsA rejected in bulk validator", func(t *testing.T) {
		err := validateBulkScoreLengths(&state.MatchResult{FlagsA: -1}, false)
		require.Error(t, err)
		var verr *ValidationError
		require.Truef(t, errors.As(err, &verr), "want *ValidationError, got %T", err)
		assert.Equal(t, "flagsA", verr.Field)
	})
}

// TestValidateDecision_EnchoScoreline pins that the required default-win
// scoreline follows the recorder's fill (domain.DefaultWinIppons keyed on
// the shared Encho.On predicate): the full pair in regulation, the single
// deciding point in encho — for the kiken variants and fusenpai alike.
func TestValidateDecision_EnchoScoreline(t *testing.T) {
	for _, decision := range []string{"kiken-voluntary", "fusenpai"} {
		t.Run(decision, func(t *testing.T) {
			build := func(ippons []string, encho *state.EnchoMetadata) *ScoreRequest {
				return &ScoreRequest{
					SideA:      "Alice",
					SideB:      "Bob",
					Decision:   decision,
					DecisionBy: "shiro",
					IpponsA:    ippons,
					Winner:     "Alice",
					Encho:      encho,
				}
			}
			rejectScoreline := func(t *testing.T, req *ScoreRequest, why string) {
				t.Helper()
				err := req.Validate()
				require.Error(t, err, why)
				var verr *ValidationError
				require.True(t, errors.As(err, &verr), why)
				assert.Equal(t, "scoreline", verr.Field, why)
			}
			assert.NoError(t, build([]string{"○"}, &state.EnchoMetadata{PeriodCount: 1}).Validate(), "1-0 in encho accepted")
			rejectScoreline(t, build([]string{"○", "○"}, &state.EnchoMetadata{PeriodCount: 1}), "2-0 in encho rejected")
			rejectScoreline(t, build([]string{"○"}, nil), "1-0 in regulation rejected")
			rejectScoreline(t, build([]string{"○"}, &state.EnchoMetadata{PeriodCount: 0}), "degenerate periodCount-0 block is not encho")
		})
	}
}

// The match-level tie gate must count ippons the way the engine does. It used
// to compare raw len(), while validateSubBout (its declared twin) and the
// engine's preserveSubHantei both drop the "•" unfilled-slot placeholder — so
// one scoreline could read tied to one enforcer and untied to the other.
func TestScoreRequestValidate_HanteiTieGateIgnoresPlaceholders(t *testing.T) {
	req := func(a, b []string) ScoreRequest {
		return ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:          state.MatchStatusCompleted,
			DecidedByHantei: boolPtr(true),
			IpponsA:         a, IpponsB: b,
		}
	}

	t.Run("tied on real ippons, untied on raw length, is accepted", func(t *testing.T) {
		// 1 real ippon each. Raw len says 2 != 1 and would reject.
		r := req([]string{"M", "•"}, []string{"K"})
		assert.NoError(t, r.Validate())
	})

	t.Run("an empty trailing cell counts the same way", func(t *testing.T) {
		r := req([]string{"M", ""}, []string{"K"})
		assert.NoError(t, r.Validate())
	})

	t.Run("untied on real ippons is still rejected", func(t *testing.T) {
		// 0 real against 1. Raw len would call this tied (1 == 1) and accept.
		// Kept to one slot per side so validateIppons' 2-2 rule (which
		// does count raw slots) cannot be what rejects it.
		r := req([]string{"•"}, []string{"K"})
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tied scoreline")
	})

	t.Run("the sub-bout twin agrees on the same scoreline", func(t *testing.T) {
		sub := &state.SubMatchResult{
			Position: state.DaihyosenSubPosition,
			SideA:    "Alice", SideB: "Bob", Winner: "Alice",
			Decision:        "daihyosen",
			IpponsA:         []string{"M", "•"},
			IpponsB:         []string{"K"},
			DecidedByHantei: state.HanteiExplicit(true),
		}
		assert.NoError(t, validateSubBout("subResults[0].", sub, false))
	})
}

// validateIppons counts two ways on purpose: the per-side caps are
// STRUCTURAL (a side has two slots), the 2-2 rule is SEMANTIC (a claim about
// points). Counting slots for the second rejected legal scorelines.
func TestValidateIpponCounts_TwoTwoRuleCountsPointsNotSlots(t *testing.T) {
	t.Run("a placeholder does not make a scoreline 2-2", func(t *testing.T) {
		// 1-2 on real ippons: an ordinary win, previously refused as "2-2".
		assert.NoError(t, validateIppons("", []string{"M", "•"}, []string{"K", "D"}))
	})

	t.Run("an empty cell does not either", func(t *testing.T) {
		assert.NoError(t, validateIppons("", []string{"M", ""}, []string{"K", "D"}))
	})

	t.Run("a real 2-2 is still rejected", func(t *testing.T) {
		err := validateIppons("", []string{"M", "K"}, []string{"D", "T"})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "both sides cannot have 2 ippons")
	})

	t.Run("the per-side cap stays structural", func(t *testing.T) {
		// Three slots is malformed whatever the entries are: only one is a
		// real ippon, and it is still refused.
		err := validateIppons("", []string{"M", "•", "•"}, nil)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "at most 2 ippons per side")
	})

	t.Run("the wire gate agrees with the domain counter", func(t *testing.T) {
		// The property that keeps this aligned: whenever the domain counter
		// says both sides scored 2, the gate refuses; otherwise it does not.
		for _, tc := range [][2][]string{
			{{"M", "K"}, {"D", "T"}},
			{{"M", "•"}, {"K", "D"}},
			{{"M"}, {"K"}},
			{{}, {}},
			{{"○", "○"}, {"M", "K"}},
		} {
			a, b := tc[0], tc[1]
			wantReject := domain.CountScoringIppons(a) == 2 && domain.CountScoringIppons(b) == 2
			err := validateIppons("", a, b)
			assert.Equal(t, wantReject, err != nil, "ipponsA=%v ipponsB=%v", a, b)
		}
	})
}

// An ippon entry must be a single character. The rule is domain's
// (IpponFitsScoreCodec, stated beside the FormatScore/ParseScore pair whose
// precondition it is); this pins that the wire boundary actually enforces it,
// on BOTH the ScoreRequest path and the bulk path, at match level and on a
// sub-bout.
//
// Two things went wrong while nothing enforced it. A bracket match persists
// each side as ONE joined string, so ["M","Ht"] became "MHt" and decoded back
// as three ippons, one of them the hansoku letter — into the display surfaces
// and into the K3 rollback snapshot built from the same decode. And on a POOL
// match, domain.HanteiMark IS the storage encoding for a verdict
// as an ippon entry: before the placement rules a client-supplied "Ht" was
// written to pool-matches.csv verbatim and read back as a
// genuine recorded judges' decision — a verdict forged by a payload that never
// set the flag. Being two runes, it is refused by the same rule as any other
// multi-character entry.
func TestIpponEntriesMustBeSingleCharacters(t *testing.T) {
	// Semantic flip (rule 4): the mark is no longer "smuggled in as a point" —
	// domain.HanteiMark IS the verdict record (operator ruling 2026-08-21),
	// and domain.IpponFitsScoreCodec deliberately admits it as the ONE
	// multi-rune token the codec understands. validateIppons therefore
	// accepts it structurally; PLACEMENT (at most one mark, only in the
	// winner's slice, tied scoreline) is a separate rule
	// (validateHanteiMarkPlacement), exercised by TestScoreRequestValidate_Hantei
	// and TestScoreRequestValidate_SubBoutHantei.
	t.Run("the hantei mark is a legitimate ippon-slice entry, not a smuggled point", func(t *testing.T) {
		assert.NoError(t, validateIppons("", []string{"M", domain.HanteiMark}, []string{"K"}))
	})

	t.Run("any OTHER multi-character entry, on either side, is still rejected", func(t *testing.T) {
		assert.Error(t, validateIppons("", []string{"MK"}, nil))
		assert.Error(t, validateIppons("", nil, []string{"MK"}))
		assert.Error(t, validateIppons("", []string{"(H1)"}, nil), "the codec's hansoku suffix is not an ippon")
	})

	t.Run("every legal entry still passes", func(t *testing.T) {
		// Waza letters (S is naginata), the default-win maru, the unfilled-slot
		// placeholder, and the empty cell the editors can send.
		for _, v := range []string{"M", "K", "D", "T", "H", "S", domain.DefaultWinIppon, domain.IpponPlaceholder, ""} {
			assert.NoErrorf(t, validateIppons("", []string{v}, nil), "entry %q", v)
		}
	})

	t.Run("a correctly-placed mark is accepted on the ScoreRequest path", func(t *testing.T) {
		r := &ScoreRequest{
			SideA: "Alice", SideB: "Bob", Winner: "Alice",
			Status:  state.MatchStatusCompleted,
			IpponsA: []string{domain.HanteiMark}, IpponsB: []string{},
		}
		assert.NoError(t, r.Validate())
	})

	t.Run("a mark not attributable to the winner is rejected on the ScoreRequest path", func(t *testing.T) {
		// This fixture never sets SideA/SideB, so the mark in IpponsA cannot be
		// attributed to Winner "Alice": rejected by validateHanteiMarkPlacement,
		// not by the (now-gone) single-character check.
		r := &ScoreRequest{
			Winner: "Alice", Status: state.MatchStatusCompleted,
			IpponsA: []string{domain.HanteiMark}, IpponsB: []string{"K"},
		}
		err := r.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "hantei mark belongs in the winner's ippon list")
	})

	t.Run("rejected on the bulk path when the sub-bout mark is on a regular position", func(t *testing.T) {
		r := &state.MatchResult{
			SideA: "Kyoto", SideB: "Osaka", Winner: "Kyoto",
			SubResults: []state.SubMatchResult{{
				Position: 1, SideA: "K1", SideB: "O1",
				IpponsA: []string{domain.HanteiMark},
			}},
		}
		err := validateBulkScoreLengths(r, false)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "daihyosen representative bout")
	})
}
