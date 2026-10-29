package mobileapp

import (
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findPayloadMatch(payloads []any, compID, matchID string) *state.MatchResult {
	for _, p := range payloads {
		gh, ok := p.(gin.H)
		if !ok {
			continue
		}
		cfg, ok := gh["config"].(*state.Competition)
		if !ok || cfg == nil || cfg.ID != compID {
			continue
		}
		pms, ok := gh["poolMatches"].([]state.MatchResult)
		if !ok {
			continue
		}
		for i := range pms {
			if pms[i].ID == matchID {
				return &pms[i]
			}
		}
	}
	return nil
}

func TestViewerPayload_CourtQueueAcrossCompetitions(t *testing.T) {
	_, store, _, _, _ := setupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))

	compA := state.Competition{
		ID:        "a",
		Name:      "Competition A",
		Format:    state.CompFormatMixed,
		Status:    state.CompStatusPools,
		StartTime: "09:00",
		Date:      "2026-11-01",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(&compA))
	require.NoError(t, store.SaveParticipants("a", nil))

	compB := state.Competition{
		ID:        "b",
		Name:      "Competition B",
		Format:    state.CompFormatMixed,
		Status:    state.CompStatusPools,
		StartTime: "09:00",
		Date:      "2026-11-01",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(&compB))
	require.NoError(t, store.SaveParticipants("b", nil))

	matchesA := []state.MatchResult{
		{
			ID:          "a-m1",
			Court:       "A",
			SideA:       "Aiko",
			SideB:       "Bea",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "09:20",
		},
		{
			ID:          "a-m2",
			Court:       "A",
			SideA:       "Aiko",
			SideB:       "Bea",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "09:30",
		},
	}
	require.NoError(t, store.SavePoolMatches("a", matchesA))

	matchesB := []state.MatchResult{
		{
			ID:          "b-m1",
			Court:       "A",
			SideA:       "Aiko",
			SideB:       "Bea",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "09:10",
		},
	}
	require.NoError(t, store.SavePoolMatches("b", matchesB))

	payloads, err := buildViewerCompetitionPayloads(store, "")
	require.NoError(t, err)

	bm1 := findPayloadMatch(payloads, "b", "b-m1")
	require.NotNil(t, bm1)
	assert.Equal(t, 1, bm1.QueuePosition)

	am1 := findPayloadMatch(payloads, "a", "a-m1")
	require.NotNil(t, am1)
	assert.Equal(t, 2, am1.QueuePosition)

	am2 := findPayloadMatch(payloads, "a", "a-m2")
	require.NotNil(t, am2)
	assert.Equal(t, 3, am2.QueuePosition)

	courtPayloads, err := buildViewerCompetitionPayloads(store, "A")
	require.NoError(t, err)

	bm1Feed := findPayloadMatch(courtPayloads, "b", "b-m1")
	require.NotNil(t, bm1Feed)
	assert.Equal(t, 1, bm1Feed.QueuePosition)

	am1Feed := findPayloadMatch(courtPayloads, "a", "a-m1")
	require.NotNil(t, am1Feed)
	assert.Equal(t, 2, am1Feed.QueuePosition)

	am2Feed := findPayloadMatch(courtPayloads, "a", "a-m2")
	require.NotNil(t, am2Feed)
	assert.Equal(t, 3, am2Feed.QueuePosition)
}

func TestViewerPayload_CourtQueueOtherDay(t *testing.T) {
	_, store, _, _, _ := setupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))

	compA := state.Competition{
		ID:        "a",
		Name:      "Competition A",
		Format:    state.CompFormatMixed,
		Status:    state.CompStatusPools,
		StartTime: "09:00",
		Date:      "2026-11-01",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(&compA))
	require.NoError(t, store.SaveParticipants("a", nil))

	compB := state.Competition{
		ID:        "b",
		Name:      "Competition B",
		Format:    state.CompFormatMixed,
		Status:    state.CompStatusPools,
		StartTime: "09:00",
		Date:      "2026-11-02",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(&compB))
	require.NoError(t, store.SaveParticipants("b", nil))

	matchesA := []state.MatchResult{
		{
			ID:          "a-m1",
			Court:       "A",
			SideA:       "Aiko",
			SideB:       "Bea",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "09:20",
		},
		{
			ID:          "a-m2",
			Court:       "A",
			SideA:       "Aiko",
			SideB:       "Bea",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "09:30",
		},
	}
	require.NoError(t, store.SavePoolMatches("a", matchesA))

	matchesB := []state.MatchResult{
		{
			ID:          "b-m1",
			Court:       "A",
			SideA:       "Aiko",
			SideB:       "Bea",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "09:10",
		},
	}
	require.NoError(t, store.SavePoolMatches("b", matchesB))

	payloads, err := buildViewerCompetitionPayloads(store, "")
	require.NoError(t, err)

	am1 := findPayloadMatch(payloads, "a", "a-m1")
	require.NotNil(t, am1)
	assert.Equal(t, 1, am1.QueuePosition)

	am2 := findPayloadMatch(payloads, "a", "a-m2")
	require.NotNil(t, am2)
	assert.Equal(t, 2, am2.QueuePosition)

	bm1 := findPayloadMatch(payloads, "b", "b-m1")
	require.NotNil(t, bm1)
	assert.Equal(t, 1, bm1.QueuePosition)
}
