package mobileapp

import (
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// A court feed leaves out a competition with no match on that court, and
// builds no name mask for it: the mask is rebuilt after every score write, and
// the feed is polled for every competition, shown or not.
func TestViewerCourtFeed_BuildsNoMaskForACompetitionItLeavesOut(t *testing.T) {
	fx := newPNFixture(t, state.PublicNamesHidden)
	var builds atomic.Int32
	fx.pn.masks.build = func(string) (*nameMask, error) {
		builds.Add(1)
		return failClosedMask(fx.comp), nil
	}

	feed, err := buildViewerCompetitionPayloads(fx.store, "A", fx.pn, audience{})
	require.NoError(t, err)
	assert.Empty(t, feed, "the competition has no match on the court")
	assert.Zero(t, builds.Load(), "no mask for a competition the feed leaves out")

	list, err := buildViewerCompetitionPayloads(fx.store, "", fx.pn, audience{})
	require.NoError(t, err)
	assert.Len(t, list, 1)
	assert.EqualValues(t, 1, builds.Load(), "the list shows the competition, so it builds the mask")
}
