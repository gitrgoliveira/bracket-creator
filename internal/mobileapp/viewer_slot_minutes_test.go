package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func findViewerPayloadComp(payloads []any, compID string) gin.H {
	for _, p := range payloads {
		gh, ok := p.(gin.H)
		if !ok {
			continue
		}
		cfg, ok := gh["config"].(*state.Competition)
		if ok && cfg != nil && cfg.ID == compID {
			return gh
		}
	}
	return nil
}

func TestViewerPayload_SlotMinutesPerCompetition(t *testing.T) {
	router, store, _, _, _ := setupTestRouter(t)

	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name:                     "T",
		Password:                 "p",
		ClockToElapsedMultiplier: 2.0,
	}))

	compA := state.Competition{
		ID:                           "a",
		Name:                         "Competition A",
		Format:                       state.CompFormatMixed,
		Status:                       state.CompStatusPools,
		StartTime:                    "09:00",
		Date:                         "2026-11-01",
		Courts:                       []string{"A"},
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 360,
	}
	require.NoError(t, store.SaveCompetition(&compA))
	require.NoError(t, store.SaveParticipants("a", nil))

	matchesA := []state.MatchResult{
		{
			ID:          "a-m1",
			Court:       "A",
			SideA:       "Aiko",
			SideB:       "Bea",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "09:00",
		},
	}
	require.NoError(t, store.SavePoolMatches("a", matchesA))

	compB := state.Competition{
		ID:                           "b",
		Name:                         "Competition B",
		Format:                       state.CompFormatMixed,
		Status:                       state.CompStatusPools,
		StartTime:                    "09:00",
		Date:                         "2026-11-01",
		Courts:                       []string{"A"},
		PoolMatchDurationSeconds:     300,
		KnockoutMatchDurationSeconds: 480,
	}
	require.NoError(t, store.SaveCompetition(&compB))
	require.NoError(t, store.SaveParticipants("b", nil))

	matchesB := []state.MatchResult{
		{
			ID:          "b-m1",
			Court:       "A",
			SideA:       "Ken",
			SideB:       "Dan",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "09:30",
		},
	}
	require.NoError(t, store.SavePoolMatches("b", matchesB))

	tournament, err := store.LoadTournament()
	require.NoError(t, err)

	// 1. Check aggregate and court-scoped feeds for comp "a" and comp "b"
	payloadsAll, err := buildViewerCompetitionPayloads(store, "")
	require.NoError(t, err)

	payloadsCourtA, err := buildViewerCompetitionPayloads(store, "A")
	require.NoError(t, err)

	for _, payloads := range [][]any{payloadsAll, payloadsCourtA} {
		pA := findViewerPayloadComp(payloads, "a")
		require.NotNil(t, pA)
		cfgA := pA["config"].(*state.Competition)
		expPoolA, expKoA := engine.SlotMinutes(cfgA, tournament)
		assert.Equal(t, expPoolA, pA["slotMinutes"])
		assert.Equal(t, expKoA, pA["knockoutSlotMinutes"])

		pB := findViewerPayloadComp(payloads, "b")
		require.NotNil(t, pB)
		cfgB := pB["config"].(*state.Competition)
		expPoolB, expKoB := engine.SlotMinutes(cfgB, tournament)
		assert.Equal(t, expPoolB, pB["slotMinutes"])
		assert.Equal(t, expKoB, pB["knockoutSlotMinutes"])

		// Comp a and comp b differ
		assert.NotEqual(t, pA["slotMinutes"], pB["slotMinutes"])
		assert.NotEqual(t, pA["knockoutSlotMinutes"], pB["knockoutSlotMinutes"])

		// Within comp a, pool and knockout slots differ
		assert.NotEqual(t, pA["slotMinutes"], pA["knockoutSlotMinutes"])

		// Exact numbers for "a": 180s * 2.0 = 6 min, 360s * 2.0 = 12 min
		assert.Equal(t, 6, pA["slotMinutes"])
		assert.Equal(t, 12, pA["knockoutSlotMinutes"])
	}

	// 2. A knockout-format competition (Format state.CompFormatKnockout)
	compKO := state.Competition{
		ID:                           "ko",
		Name:                         "Knockout Comp",
		Format:                       state.CompFormatKnockout,
		Status:                       state.CompStatusKnockout,
		StartTime:                    "09:00",
		Date:                         "2026-11-01",
		Courts:                       []string{"A"},
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 360,
	}
	require.NoError(t, store.SaveCompetition(&compKO))
	require.NoError(t, store.SaveParticipants("ko", nil))
	matchesKO := []state.MatchResult{
		{
			ID:          "ko-m1",
			Court:       "A",
			SideA:       "Aiko",
			SideB:       "Bea",
			Status:      state.MatchStatusScheduled,
			ScheduledAt: "09:00",
		},
	}
	require.NoError(t, store.SavePoolMatches("ko", matchesKO))

	payloadsAfterKO, err := buildViewerCompetitionPayloads(store, "")
	require.NoError(t, err)
	pKO := findViewerPayloadComp(payloadsAfterKO, "ko")
	require.NotNil(t, pKO)
	assert.Equal(t, pKO["knockoutSlotMinutes"], pKO["slotMinutes"])
	assert.Equal(t, 12, pKO["slotMinutes"])
	assert.Equal(t, 12, pKO["knockoutSlotMinutes"])

	// 3. Detail endpoint GET /api/viewer/competitions/a returns the same slotMinutes and knockoutSlotMinutes
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions/a", nil)
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var detailA map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detailA))
	pAAll := findViewerPayloadComp(payloadsAll, "a")
	require.NotNil(t, pAAll)
	assert.Equal(t, float64(pAAll["slotMinutes"].(int)), detailA["slotMinutes"])
	assert.Equal(t, float64(pAAll["knockoutSlotMinutes"].(int)), detailA["knockoutSlotMinutes"])

	// 4. Team competition (Kind "team", TeamSize 0) gets 5-person default slot
	compTeam := state.Competition{
		ID:                           "team",
		Name:                         "Team Comp",
		Kind:                         "team",
		TeamSize:                     0,
		Format:                       state.CompFormatMixed,
		Status:                       state.CompStatusPools,
		StartTime:                    "09:00",
		Date:                         "2026-11-01",
		Courts:                       []string{"A"},
		PoolMatchDurationSeconds:     180,
		KnockoutMatchDurationSeconds: 360,
	}
	require.NoError(t, store.SaveCompetition(&compTeam))
	require.NoError(t, store.SaveParticipants("team", nil))
	payloadsWithTeam, err := buildViewerCompetitionPayloads(store, "")
	require.NoError(t, err)
	pTeam := findViewerPayloadComp(payloadsWithTeam, "team")
	require.NotNil(t, pTeam)

	teamCopy := compTeam
	teamCopy.TeamSize = 5
	expTeamPool, expTeamKo := engine.SlotMinutes(&teamCopy, tournament)
	assert.Equal(t, expTeamPool, pTeam["slotMinutes"])
	assert.Equal(t, expTeamKo, pTeam["knockoutSlotMinutes"])

	// 5. The slot is never stored: store.LoadCompetition("a") contains no "slotMinutes" substring
	loadedA, err := store.LoadCompetition("a")
	require.NoError(t, err)
	require.NotNil(t, loadedA)
	bytesA, err := json.Marshal(loadedA)
	require.NoError(t, err)
	assert.NotContains(t, string(bytesA), "slotMinutes")
}
