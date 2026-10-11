package engine

// K3 refuses a withdrawal whose loser ANOTHER match already made ineligible,
// and a refusal must leave the bracket exactly as it found it. The pre-write
// half (refuseConcurrentWithdrawal) used to resolve the loser from a probe copy
// of the payload whose side NAMES were still empty, so a `/score`-shaped
// withdrawal that names a winner but no sides, no winnerSide and no winnerId
// resolved no loser there: the write went on, propagateBracketWinner re-seated
// the next round's side (its representative pick cleared, its stamp moved), and
// only THEN did the post-write check raise AlreadyIneligibleError, whose
// rollback restores the written match row alone. The probe now takes the stored
// pairing's names, so the loser resolves before anything is written.

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// k3Bracket stores rpSetup's team knockout with Ryu (who advanced into m-r2-0
// side A, holding a representative pick) already barred by "other-match".
func k3Bracket(t *testing.T) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, compID := rpSetup(t, false)
	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID: wrTeamAID, Eligible: false, Reason: "kiken", MatchID: "other-match",
	}))
	return eng, store, compID
}

// k3Payload is the hand-built withdrawal: Ryu withdraws (kiken), so the winner
// is Tora. It carries a winner NAME and nothing that names a side.
func k3Payload() *state.MatchResult {
	return &state.MatchResult{
		Status:           state.MatchStatusCompleted,
		Decision:         string(domain.DecisionKikenVoluntary),
		Winner:           wrTeamB,
		CorrectionReason: "Wrong side withdrew",
	}
}

func TestK3_ARefusedWithdrawalNeverReseatsTheNextRound(t *testing.T) {
	eng, store, compID := k3Bracket(t)
	priorNext := rpNext(t, store, compID)
	priorBracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	priorFirst := *priorBracket.MatchByID("m-r1-0")

	err = inTx(t, store, compID, func(tx state.StoreTx) error {
		_, werr := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", k3Payload(), ForceOptions{})
		return werr
	})
	var already *AlreadyIneligibleError
	require.ErrorAs(t, err, &already, "the withdrawal is refused")
	assert.Equal(t, wrTeamAID, already.PlayerID)
	assert.Equal(t, "other-match", already.MatchID)

	next := rpNext(t, store, compID)
	assert.Equal(t, wrTeamA, next.SideA, "the refusal left the next round's side A where it was")
	assert.Equal(t, wrTeamAID, next.SideAID)
	a, c := next.RepPicks()
	assert.Equal(t, rpPickA, a, "and the representative pick on it")
	assert.Equal(t, rpPickC, c)
	assert.Equal(t, mmT1, next.GroupStamp(state.GroupRepPickA), "dated as it was, not moved by a re-seat that was then refused")
	assert.Equal(t, priorNext.ModifiedAt, next.ModifiedAt, "the next match was not written at all")

	after, err := store.LoadBracket(compID)
	require.NoError(t, err)
	first := after.MatchByID("m-r1-0")
	assert.Equal(t, priorFirst.Winner, first.Winner, "the refused match keeps its recorded winner")
	assert.Equal(t, priorFirst.Status, first.Status)
	assert.Equal(t, priorFirst.Decision, first.Decision)

	for _, id := range []string{rpNextID, "m-r1-0"} {
		entries, herr := store.LoadMatchHistory(compID, id)
		require.NoError(t, herr)
		assert.Empty(t, entries, "a refused write leaves no history line on %s, a re-seat's included", id)
	}
}

// The same payload with the loser NOT barred is an ordinary correction: the
// probe's names must not turn every withdrawal into a refusal.
func TestK3_AWithdrawalNobodyBarredStillLands(t *testing.T) {
	eng, store, compID := rpSetup(t, false)
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, werr := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", k3Payload(), ForceOptions{})
		return werr
	}))
	next := rpNext(t, store, compID)
	assert.Equal(t, wrTeamB, next.SideA, "Tora advanced in Ryu's place")
}
