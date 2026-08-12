package engine

import (
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Operator ruling: "all results must be recorded into storage" — a recorded
// daihyosen hantei must never be erased by a writer that did not address it.
// preserveSubHantei runs at every SubResults replacement chokepoint; these
// tests exercise it end-to-end through the pool write path plus the unit
// truth table.
func TestPreserveSubHantei(t *testing.T) {
	dh := state.DaihyosenSubPosition
	storedSubs := func() []state.SubMatchResult {
		return []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M"}, Winner: "K1", Decision: "fought"},
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", IpponsA: []string{}, IpponsB: []string{},
				Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true)},
		}
	}

	t.Run("verdict-silent stale snapshot preserves the stored verdict", func(t *testing.T) {
		incoming := []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M", "K"}, Winner: "K1", Decision: "fought"},
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"}, // no verdict, no winner
		}
		preserveSubHantei(storedSubs(), incoming)
		require.True(t, incoming[1].HanteiDecided())
		assert.Equal(t, "Kyoto", incoming[1].Winner)
		// The unrelated bout correction is untouched.
		assert.Equal(t, []string{"M", "K"}, incoming[0].IpponsA)
	})

	t.Run("explicit false (withdrawal) passes through and clears", func(t *testing.T) {
		// NOTE: state.HanteiPtr is nil-for-false (built for the omitempty
		// projection), so an EXPLICIT false needs a real pointer.
		explicitFalse := false
		incoming := []state.SubMatchResult{
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen",
				DecidedByHantei: &explicitFalse},
		}
		preserveSubHantei(storedSubs(), incoming)
		require.NotNil(t, incoming[0].DecidedByHantei)
		assert.False(t, *incoming[0].DecidedByHantei)
		assert.Equal(t, "", incoming[0].Winner)
	})

	t.Run("a named winner on the incoming row stands", func(t *testing.T) {
		incoming := []state.SubMatchResult{
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen", Winner: "Osaka"},
		}
		preserveSubHantei(storedSubs(), incoming)
		assert.Nil(t, incoming[0].DecidedByHantei)
		assert.Equal(t, "Osaka", incoming[0].Winner)
	})

	t.Run("an untied incoming row cannot inherit the verdict", func(t *testing.T) {
		incoming := []state.SubMatchResult{
			{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen",
				IpponsA: []string{"M"}, IpponsB: []string{}},
		}
		preserveSubHantei(storedSubs(), incoming)
		assert.Nil(t, incoming[0].DecidedByHantei)
	})

	t.Run("no stored verdict: nothing to preserve", func(t *testing.T) {
		stored := []state.SubMatchResult{{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"}}
		incoming := []state.SubMatchResult{{Position: dh, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"}}
		preserveSubHantei(stored, incoming)
		assert.Nil(t, incoming[0].DecidedByHantei)
	})
}

// End-to-end through the pool write path: device B's stale snapshot (opened
// before the verdict was recorded) saves a correction and the stored verdict
// survives in storage.
func TestPoolWrite_StaleSnapshotKeepsHantei(t *testing.T) {
	dir, err := os.MkdirTemp("", "engine-subhantei-*")
	require.NoError(t, err)
	defer func() { _ = os.RemoveAll(dir) }()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	eng := New(store)

	compID := "sh1"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: compID, Name: "SH", Kind: "team", TeamSize: 3}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
		ID: "P1-1", SideA: "Kyoto", SideB: "Osaka", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M"}, Winner: "K1", Decision: "fought"},
			{Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka",
				Winner: "Kyoto", Decision: "daihyosen", DecidedByHantei: state.HanteiPtr(true)},
		},
	}}))

	// Device B never saw the verdict: its snapshot's daihyosen row is
	// verdict-silent; it corrects bout 1 only.
	patch := &state.MatchResult{
		SideA: "Kyoto", SideB: "Osaka", Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "K1", SideB: "O1", IpponsA: []string{"M", "K"}, Winner: "K1", Decision: "fought"},
			{Position: state.DaihyosenSubPosition, SideA: "Kyoto", SideB: "Osaka", Decision: "daihyosen"},
		},
	}
	require.NoError(t, eng.RecordMatchResult(compID, "P1-1", patch))

	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.Len(t, stored, 1)
	var dhRow *state.SubMatchResult
	for i := range stored[0].SubResults {
		if stored[0].SubResults[i].Position == state.DaihyosenSubPosition {
			dhRow = &stored[0].SubResults[i]
		}
	}
	require.NotNil(t, dhRow)
	assert.True(t, dhRow.HanteiDecided(), "stored verdict must survive the stale write")
	assert.Equal(t, "Kyoto", dhRow.Winner)
	// The correction itself landed.
	assert.Equal(t, []string{"M", "K"}, stored[0].SubResults[0].IpponsA)
}
