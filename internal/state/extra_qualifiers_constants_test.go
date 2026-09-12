package state

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
)

// TestExtraQualifiersConstantsMatchHelper pins bc-drwx item 13:
// state.Competition.ExtraQualifiers* and helper.QualifierMode* are two
// independently-declared sets of string constants for the SAME three-value
// vocabulary ("" / "larger-pools" / "fill-bracket") -- duplicated rather
// than imported because internal/helper cannot import internal/state
// (internal/state already imports internal/helper for
// Competition.QualifiersForPool's helper.Pool parameter, so the reverse
// import would cycle; see helper's qualifierMode doc comment). A typo in
// either set would silently desynchronize what the app validates
// (state.ValidateExtraQualifiers) from what the tree-aware distributor
// actually dispatches on (helper.treeAwareQualifierSlots), with no compiler
// error to catch it -- this test is the one place that comparison is made,
// so a future edit to either set that drifts from the other fails HERE
// rather than as a silently-misrouted draw.
func TestExtraQualifiersConstantsMatchHelper(t *testing.T) {
	assert.Equal(t, ExtraQualifiersNone, helper.QualifierModeStandard,
		"the standard/default mode's empty-string sentinel must match")
	assert.Equal(t, ExtraQualifiersLargerPools, helper.QualifierModeLargerPools,
		"the larger-pools mode string must match")
	assert.Equal(t, ExtraQualifiersFillBracket, helper.QualifierModeFillBracket,
		"the fill-bracket mode string must match")
}
