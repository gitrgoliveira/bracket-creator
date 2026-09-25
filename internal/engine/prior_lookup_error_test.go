package engine

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failFirstPoolLoad is a StoreTx whose FIRST LoadPoolMatches fails and whose
// later ones succeed, so the engi seam's prior lookup cannot read the stored
// match while the write that follows it could.
type failFirstPoolLoad struct {
	state.StoreTx
	failed bool
}

func (f *failFirstPoolLoad) LoadPoolMatches(compID string) ([]state.MatchResult, error) {
	if !f.failed {
		f.failed = true
		return nil, errors.New("pool-matches.csv could not be read")
	}
	return f.StoreTx.LoadPoolMatches(compID)
}

// A pool write in a mixed engi competition reads the stored match first, so a
// refusal from the knockout check can put the pool row back. A prior it cannot
// read is an error, never a nil prior: with no prior there is nothing to roll
// back to, and the write went ahead unguarded while the error was discarded.
func TestEngiMixedPoolWrite_UnreadablePriorIsAnError(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "engi-prior-unreadable"
	setupEngiComp(t, store, compID, state.CompFormatMixed)
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alice", Dojo: "Dojo Alice"}, {Name: "Bob", Dojo: "Dojo Bob"}}},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A",
	}}))

	err := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, werr := eng.RecordMatchResultWithIneligibilityTx(&failFirstPoolLoad{StoreTx: tx}, compID, "Pool A-0",
			&state.MatchResult{FlagsA: 3, FlagsB: 0, Status: state.MatchStatusCompleted})
		return werr
	})
	require.Error(t, err, "the prior lookup's error must reach the caller")

	ms, lerr := store.LoadPoolMatches(compID)
	require.NoError(t, lerr)
	require.Len(t, ms, 1)
	assert.Equal(t, state.MatchStatusRunning, ms[0].Status, "nothing is written when the prior cannot be read")
	assert.Zero(t, ms[0].FlagsA+ms[0].FlagsB)
}

// The same rule on the kendo path of the same entry point, where the prior
// also feeds K3's rollback and the kachinuki merge.
func TestMatchWrite_UnreadablePriorIsAnError(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "kendo-prior-unreadable"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "p", Kind: "individual", Format: state.CompFormatLeague, Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Alice", SideB: "Bob", Status: state.MatchStatusRunning, Court: "A",
	}}))

	err := store.WithTransaction(compID, func(tx state.StoreTx) error {
		_, werr := eng.RecordMatchResultWithIneligibilityTx(&failFirstPoolLoad{StoreTx: tx}, compID, "Pool A-0",
			&state.MatchResult{SideA: "Alice", SideB: "Bob", Winner: "Alice", IpponsA: []string{"M", "K"}, Status: state.MatchStatusCompleted})
		return werr
	})
	require.Error(t, err, "the prior lookup's error must reach the caller")

	ms, lerr := store.LoadPoolMatches(compID)
	require.NoError(t, lerr)
	require.Len(t, ms, 1)
	assert.Equal(t, state.MatchStatusRunning, ms[0].Status, "nothing is written when the prior cannot be read")
}
