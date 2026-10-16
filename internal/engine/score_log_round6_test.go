package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// refusingPoolTx is a transaction whose pool-match write is refused with a
// validation error, so the match the replay derived cannot be written.
type refusingPoolTx struct{ state.StoreTx }

func (refusingPoolTx) UpdatePoolMatchByID(string, string, func(*state.MatchResult) error) (bool, error) {
	return true, validationErrorf("the replayed match is refused")
}

// bc-evts stage 2b review round 6: when the match the replay derived cannot be
// written, the refusal code is on the event's answer AND on the history line
// kept for it, so the line says why the tap changed nothing.
func TestRecordScoreEvents_AWriteRefusalIsOnTheHistoryLine(t *testing.T) {
	h := mmIndividual(t, false)
	ev := domain.ScoreEvent{ID: "e1", Session: "s1", Seq: 1, Stamp: mmT1, Kind: domain.ScoreIpponAdd, Side: "a", Letter: "M"}
	require.NoError(t, h.store.WithTransaction(h.compID, func(tx state.StoreTx) error {
		res, err := h.eng.RecordScoreEventsTx(refusingPoolTx{tx}, h.compID, h.matchID, ScoreEventsRequest{Session: "s1", Events: []domain.ScoreEvent{ev}})
		require.NoError(t, err)
		require.Error(t, res.WriteErr)
		assert.Equal(t, domain.ScoreOutcomeRefused, res.Answers[0].Outcome)
		assert.Equal(t, "invalid", res.Answers[0].Reason)
		assert.NotEmpty(t, res.Answers[0].Message)
		return nil
	}))
	var line *state.MatchHistoryEntry
	for _, e := range h.history(t) {
		if e.Event != nil && e.Event.ID == "e1" {
			line = &e
		}
	}
	require.NotNil(t, line, "the refused tap leaves a history line")
	assert.Equal(t, domain.ScoreOutcomeRefused, line.EventOutcome)
	assert.Equal(t, "invalid", line.Reason, "the line carries the refusal code")
}
