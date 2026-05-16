package engine

import (
	"errors"
	"testing"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestStartMatchBlockedByIneligibleCompetitor verifies FR-035: when a
// participant has CompetitorStatus{Eligible: false} in the store,
// engine.StartMatch must return *IneligibleCompetitorError matching
// errors.Is(err, ErrIneligibleCompetitor) so the score handler can
// reply 409 with the player/reason.
func TestStartMatchBlockedByIneligibleCompetitor(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "elig-blocked"

	createTestCompetition(t, store, compID, "pools", 2)

	// Seed participants with explicit UUIDs — state.LoadParticipants
	// only treats the first column as an ID when it parses as UUID v4.
	aliceID := helper.NewUUID4()
	bobID := helper.NewUUID4()
	players := []helper.Player{
		{ID: aliceID, Name: "Alice", Dojo: "DojoA"},
		{ID: bobID, Name: "Bob", Dojo: "DojoB"},
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	matches := []state.MatchResult{{
		ID:     "Pool A-0",
		SideA:  "Alice",
		SideB:  "Bob",
		Status: state.MatchStatusScheduled,
	}}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	require.NoError(t, store.SetCompetitorStatus(compID, domain.CompetitorStatus{
		PlayerID:   aliceID,
		Eligible:   false,
		Reason:     "kiken at m_prev",
		MatchID:    "m_prev",
		RecordedAt: time.Now().UTC(),
	}))

	err := eng.StartMatch(compID, "Pool A-0")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrIneligibleCompetitor), "want errors.Is == ErrIneligibleCompetitor, got %v", err)

	var ineligErr *IneligibleCompetitorError
	require.ErrorAs(t, err, &ineligErr)
	assert.Equal(t, aliceID, ineligErr.PlayerID)
	assert.Equal(t, "kiken at m_prev", ineligErr.Reason)
}
