package helper

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidateCourtPairing sweeps the shiaijo-count rule across the whole
// operator-plausible range: 1 shiaijo is VALID (it draws as two half-blocks
// acting as partner courts), every even count is valid, and every odd count
// above 1 is rejected because the draw cannot pair its court regions.
func TestValidateCourtPairing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		n     int
		valid bool
	}{
		{1, true}, // a single-shiaijo competition is explicitly allowed
		{2, true},
		{3, false},
		{4, true},
		{5, false},
		{6, true},
		{7, false},
		{8, true},
	}

	for _, tt := range tests {
		t.Run(fmt.Sprintf("courts=%d", tt.n), func(t *testing.T) {
			t.Parallel()
			err := ValidateCourtPairing(tt.n)
			if tt.valid {
				assert.NoErrorf(t, err, "%d shiaijo must be accepted", tt.n)
				return
			}
			require.Errorf(t, err, "%d shiaijo must be rejected", tt.n)
			msg := err.Error()
			assert.Contains(t, msg, fmt.Sprintf("got %d", tt.n),
				"the message must name the rejected count")
			assert.Contains(t, msg, fmt.Sprintf("use %d or %d, or 1", tt.n-1, tt.n+1),
				"the message must name the nearest valid counts AND 1")
			assert.Contains(t, msg, "partner",
				"the message must explain the court-pairing reason")
		})
	}
}

// TestValidateCourtPairingMessageNeverReadsAsMinimumTwo pins the wording
// requirement that outlives any rephrasing: a 1-shiaijo competition is legal,
// so the error must never tell an operator they need at least two courts.
func TestValidateCourtPairingMessageNeverReadsAsMinimumTwo(t *testing.T) {
	t.Parallel()

	err := ValidateCourtPairing(3)
	require.Error(t, err)
	msg := strings.ToLower(err.Error())
	assert.Contains(t, msg, "1 or an even number",
		"the rule must be stated as '1 or an even number'")
	assert.Contains(t, msg, ", or 1",
		"1 must be offered as a valid answer")
	assert.NotContains(t, msg, "at least 2",
		"the rule is not a minimum: a single-shiaijo competition is valid")
	assert.NotContains(t, msg, "at least two",
		"the rule is not a minimum: a single-shiaijo competition is valid")
}

// TestValidateCourtPairingNonPositive documents the division of labour with
// ValidateCourts: 0 and negative counts are that validator's business (and
// the competition-level validators read an empty court list as "inherit the
// tournament's courts"), so the pairing rule stays silent about them rather
// than emitting a confusing "use -1 or 1" suggestion.
func TestValidateCourtPairingNonPositive(t *testing.T) {
	t.Parallel()

	assert.NoError(t, ValidateCourtPairing(0))
	assert.NoError(t, ValidateCourtPairing(-3))
	assert.Error(t, ValidateCourts(0), "0 courts stays ValidateCourts' rejection")
}

// TestValidateCourtPairingWithinLabelCap checks the two validators compose at
// the top of the range: 25 is a legal label count but an illegal allocation,
// and its suggestion (26) is still inside the A-Z cap, so an operator can act
// on it without hitting the other error.
func TestValidateCourtPairingWithinLabelCap(t *testing.T) {
	t.Parallel()

	require.NoError(t, ValidateCourts(25), "25 is within the A-Z label cap")
	err := ValidateCourtPairing(25)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "use 24 or 26, or 1")
	assert.NoError(t, ValidateCourts(26), "the suggested 26 must itself be legal")
}
