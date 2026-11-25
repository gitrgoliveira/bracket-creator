package engine

import (
	"errors"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// drawReadyHidden generates the draw of a fresh competition of format and
// answers the competition as stored.
func drawReadyHidden(t *testing.T, format string, opts ...func(*state.Competition)) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	const compID = "draw-vis"
	createTestCompetition(t, store, compID, format, 4, opts...)
	saveTestParticipants(t, store, compID, namedPlayers(8))
	require.NoError(t, eng.GenerateDraw(compID))
	return eng, store, compID
}

// A generated draw starts hidden, in every format, so a draft is never on the
// public pages before the organiser publishes it.
func TestGenerateDraw_StoresTheDraftAsHidden(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format string
		opts   func(*state.Competition)
	}{
		{"knockout", state.CompFormatKnockout, nil},
		{"mixed", state.CompFormatMixed, nil},
		{"league", state.CompFormatLeague, nil},
		{"swiss", state.CompFormatSwiss, func(c *state.Competition) { c.SwissRounds = 3 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, store, id := drawReadyHidden(t, tc.format, tc.opts)
			c := loadComp(t, store, id)
			assert.Equal(t, state.CompStatusDrawReady, c.Status)
			assert.True(t, c.DrawHidden, "a generated draw is stored hidden")
			assert.True(t, c.DrawHiddenIn(&state.Tournament{}), "an officiated tournament hides it from the public")
		})
	}
}

// Generate stores the field in a self-run tournament too; DrawHiddenIn reads
// the mode at read time and hides nothing there.
func TestGenerateDraw_SelfRunStoresHiddenButHidesNothing(t *testing.T) {
	_, store, id := drawReadyHidden(t, state.CompFormatKnockout)
	c := loadComp(t, store, id)
	assert.True(t, c.DrawHidden)
	assert.False(t, c.DrawHiddenIn(&state.Tournament{Mode: state.TournamentModeSelfRun}))
}

// The one-click start (Start on a setup competition) generates and starts in
// one go: the competition is public at once, so the draw is never stored as
// hidden.
func TestStartCompetition_FromSetupDoesNotStoreAHiddenDraw(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "one-click"
	createTestCompetition(t, store, compID, state.CompFormatKnockout, 4)
	saveTestParticipants(t, store, compID, namedPlayers(8))
	require.NoError(t, eng.StartCompetition(compID))
	c := loadComp(t, store, compID)
	assert.Equal(t, state.CompStatusKnockout, c.Status)
	assert.False(t, c.DrawHidden)
}

// Starting a hidden draft leaves the stored field alone; a started
// competition is public whatever is stored.
func TestStartCompetition_AHiddenDraftIsPublicOnceStarted(t *testing.T) {
	eng, store, id := drawReadyHidden(t, state.CompFormatKnockout)
	require.NoError(t, eng.StartCompetition(id))
	c := loadComp(t, store, id)
	assert.Equal(t, state.CompStatusKnockout, c.Status)
	assert.True(t, c.DrawHidden, "the start paths do not touch the field")
	assert.False(t, c.DrawHiddenIn(&state.Tournament{}), "a started competition is public")
}

// Discard draw clears the field in the same write that resets the status, so
// a competition back in setup carries no stale hidden flag and the next draw
// hides itself.
func TestDiscardDraw_ClearsDrawHidden(t *testing.T) {
	eng, store, id := drawReadyHidden(t, state.CompFormatMixed)
	require.NoError(t, eng.DiscardDraw(id))
	c := loadComp(t, store, id)
	assert.Equal(t, state.CompStatusSetup, c.Status)
	assert.False(t, c.DrawHidden)

	// A discarded PUBLISHED draft regenerates hidden.
	require.NoError(t, eng.GenerateDraw(id))
	assert.True(t, loadComp(t, store, id).DrawHidden)
}

// Publish and Unpublish flip the field; asking for the state already stored
// writes nothing and reports no change.
func TestSetDrawHidden_FlipsAndIsIdempotent(t *testing.T) {
	eng, store, id := drawReadyHidden(t, state.CompFormatKnockout)

	changed, err := eng.SetDrawHidden(id, true)
	require.NoError(t, err)
	assert.False(t, changed, "hiding a hidden draft changes nothing")

	before := store.FileVersion(id, "config.md")
	changed, err = eng.SetDrawHidden(id, false)
	require.NoError(t, err)
	assert.True(t, changed, "publish clears the field")
	assert.False(t, loadComp(t, store, id).DrawHidden)
	assert.False(t, loadComp(t, store, id).DrawHiddenIn(&state.Tournament{}))
	assert.NotEqual(t, before, store.FileVersion(id, "config.md"), "a publish is a config write")

	mid := store.FileVersion(id, "config.md")
	changed, err = eng.SetDrawHidden(id, false)
	require.NoError(t, err)
	assert.False(t, changed, "publishing a published draft changes nothing")
	assert.Equal(t, mid, store.FileVersion(id, "config.md"), "nothing is written")

	changed, err = eng.SetDrawHidden(id, true)
	require.NoError(t, err)
	assert.True(t, changed, "unpublish sets the field again")
	assert.True(t, loadComp(t, store, id).DrawHidden)
}

// Only a draw waiting to start can be published or hidden: setup and a
// started competition are refused with the typed refusal, and nothing is
// written.
func TestSetDrawHidden_RefusedOffDrawReady(t *testing.T) {
	t.Run("setup", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		createTestCompetition(t, store, "in-setup", state.CompFormatKnockout, 4)
		for _, hidden := range []bool{true, false} {
			changed, err := eng.SetDrawHidden("in-setup", hidden)
			assert.False(t, changed)
			require.Error(t, err)
			assert.True(t, errors.Is(err, ErrDrawNotReady))
			var ve *ValidationError
			assert.True(t, errors.As(err, &ve))
		}
		assert.False(t, loadComp(t, store, "in-setup").DrawHidden)
	})
	t.Run("started", func(t *testing.T) {
		eng, store, id := drawReadyHidden(t, state.CompFormatKnockout)
		require.NoError(t, eng.StartCompetition(id))
		changed, err := eng.SetDrawHidden(id, false)
		assert.False(t, changed)
		require.ErrorIs(t, err, ErrDrawNotReady)
		assert.True(t, loadComp(t, store, id).DrawHidden, "the refused write stored nothing")
	})
	t.Run("missing", func(t *testing.T) {
		eng, _, _ := setupTestEngine(t)
		_, err := eng.SetDrawHidden("nobody", true)
		require.Error(t, err)
		assert.False(t, errors.Is(err, ErrDrawNotReady))
	})
}
