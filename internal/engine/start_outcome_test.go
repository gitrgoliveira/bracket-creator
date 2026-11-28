package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// A start the pass made is reported as a start whatever else the pass did,
// unless the same pass finished the competition or already reports a start.
func TestStartOutcome(t *testing.T) {
	all := []AutoCompleteOutcome{
		AutoCompleteNoChange,
		AutoCompleteTransitioned,
		AutoCompleteTiebreakInjected,
		AutoCompleteKnockoutStarted,
		AutoCompletePoolsResolved,
		AutoCompleteAwaitingLeagueTiebreak,
		AutoCompleteStarted,
	}
	for _, outcome := range all {
		assert.Equal(t, outcome, startOutcome(false, false, outcome), "no start leaves %d alone", outcome)
	}

	for _, flags := range []struct {
		name              string
		started, reopened bool
	}{
		{"started", true, false},
		{"reopened", false, true},
		{"both", true, true},
	} {
		for _, outcome := range []AutoCompleteOutcome{
			AutoCompleteNoChange,
			AutoCompleteTiebreakInjected,
			AutoCompletePoolsResolved,
			AutoCompleteAwaitingLeagueTiebreak,
			AutoCompleteStarted,
		} {
			assert.Equal(t, AutoCompleteStarted, startOutcome(flags.started, flags.reopened, outcome),
				"%s with outcome %d is reported as a start", flags.name, outcome)
		}
		for _, outcome := range []AutoCompleteOutcome{AutoCompleteTransitioned, AutoCompleteKnockoutStarted} {
			assert.Equal(t, outcome, startOutcome(flags.started, flags.reopened, outcome),
				"%s with outcome %d keeps the outcome", flags.name, outcome)
		}
	}
}
