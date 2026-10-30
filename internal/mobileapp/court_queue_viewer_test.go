package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

// The detail endpoint numbers the court-wide queue without the preview bracket,
// as the aggregate does: its placeholder bouts take no place and do not shift
// the competition's own pool bouts, and the Bracket tab still serves them.
func TestViewerDetail_PreviewBracketTakesNoCourtQueuePosition(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))

	comp := state.Competition{
		ID:        "mx",
		Name:      "Mixed",
		Format:    state.CompFormatMixed,
		Status:    state.CompStatusPools,
		StartTime: "09:00",
		Date:      "2026-11-01",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants("mx", nil))
	require.NoError(t, store.SavePoolMatches("mx", []state.MatchResult{
		{ID: "Pool A-0", Court: "A", SideA: "Aiko", SideB: "Bea", Status: state.MatchStatusScheduled, ScheduledAt: "09:10"},
		{ID: "Pool A-1", Court: "A", SideA: "Aiko", SideB: "Cleo", Status: state.MatchStatusScheduled, ScheduledAt: "09:20"},
	}))
	// Scheduled before the pool bouts, so a numbered preview bout would take
	// place 1 and push both pool bouts down.
	require.NoError(t, store.SaveBracket("mx", &state.Bracket{
		Preview: true,
		Rounds: [][]state.BracketMatch{{
			{ID: "m-r1-0", SideA: "Pool A-1st", SideB: "Pool B-2nd", Status: state.MatchStatusScheduled, Court: "A", ScheduledAt: "09:00"},
		}},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions/mx", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "viewer detail: %s", w.Body.String())
	var detail struct {
		PoolMatches []state.MatchResult `json:"poolMatches"`
		Bracket     *state.Bracket      `json:"bracket"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))

	require.Len(t, detail.PoolMatches, 2)
	assert.Equal(t, 1, detail.PoolMatches[0].QueuePosition)
	assert.Equal(t, 2, detail.PoolMatches[1].QueuePosition)
	require.NotNil(t, detail.Bracket, "the Bracket tab still serves the preview")
	assert.True(t, detail.Bracket.Preview)
	require.Len(t, detail.Bracket.Rounds, 1)
	assert.Zero(t, detail.Bracket.Rounds[0][0].QueuePosition)
}

// viewerQueueComp is the slice of a viewer competition payload the queue
// tests read: its id and its pool matches with their queuePosition.
type viewerQueueComp struct {
	Config      state.Competition   `json:"config"`
	PoolMatches []state.MatchResult `json:"poolMatches"`
}

func getViewerDetail(t *testing.T, r http.Handler, id string) viewerQueueComp {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions/"+id, nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "viewer detail %s: %s", id, w.Body.String())
	var detail viewerQueueComp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &detail))
	return detail
}

func getViewerAggregate(t *testing.T, r http.Handler) map[string]viewerQueueComp {
	t.Helper()
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "viewer competitions: %s", w.Body.String())
	var comps []viewerQueueComp
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &comps))
	byID := make(map[string]viewerQueueComp, len(comps))
	for _, c := range comps {
		byID[c.Config.ID] = c
	}
	return byID
}

func saveQueueComp(t *testing.T, store *state.Store, id string, status state.CompetitionStatus, matches []state.MatchResult) {
	t.Helper()
	comp := state.Competition{
		ID:        id,
		Name:      "Competition " + id,
		Format:    state.CompFormatMixed,
		Status:    status,
		StartTime: "09:00",
		Date:      "2026-11-01",
		Courts:    []string{"A"},
	}
	require.NoError(t, store.SaveCompetition(&comp))
	require.NoError(t, store.SaveParticipants(id, nil))
	require.NoError(t, store.SavePoolMatches(id, matches))
}

// TestViewerDetail_SetupCompetitionTakesNoCourtQueuePosition: a competition
// still in setup that holds matches a failed draw attempt left behind gets no
// queuePosition on either viewer endpoint, and those matches take no place
// from another competition's on the same court.
func TestViewerDetail_SetupCompetitionTakesNoCourtQueuePosition(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))

	saveQueueComp(t, store, "su", state.CompStatusSetup, []state.MatchResult{
		{ID: "Pool A-0", Court: "A", SideA: "Aiko", SideB: "Bea", Status: state.MatchStatusScheduled, ScheduledAt: "09:00"},
		{ID: "Pool A-1", Court: "A", SideA: "Aiko", SideB: "Cleo", Status: state.MatchStatusScheduled, ScheduledAt: "09:05"},
	})
	saveQueueComp(t, store, "po", state.CompStatusPools, []state.MatchResult{
		{ID: "Pool A-0", Court: "A", SideA: "Dai", SideB: "Eri", Status: state.MatchStatusScheduled, ScheduledAt: "09:10"},
	})

	detail := getViewerDetail(t, r, "su")
	require.Len(t, detail.PoolMatches, 2)
	for _, m := range detail.PoolMatches {
		assert.Zerof(t, m.QueuePosition, "detail: setup match %s", m.ID)
	}
	assert.Equal(t, 1, getViewerDetail(t, r, "po").PoolMatches[0].QueuePosition,
		"the setup competition's leftover matches take no place ahead of it")

	aggregate := getViewerAggregate(t, r)
	if su, ok := aggregate["su"]; ok {
		for _, m := range su.PoolMatches {
			assert.Zerof(t, m.QueuePosition, "aggregate: setup match %s", m.ID)
		}
	}
	require.Contains(t, aggregate, "po")
	assert.Equal(t, 1, aggregate["po"].PoolMatches[0].QueuePosition)
}

// TestViewerDetail_CourtQueueMatchesAggregate: the detail view, which loads
// the other competitions on its own, serves every match the same
// queuePosition the aggregate endpoint numbers it with.
func TestViewerDetail_CourtQueueMatchesAggregate(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))

	saveQueueComp(t, store, "a", state.CompStatusPools, []state.MatchResult{
		{ID: "Pool A-0", Court: "A", SideA: "Aiko", SideB: "Bea", Status: state.MatchStatusScheduled, ScheduledAt: "09:10"},
		{ID: "Pool A-1", Court: "A", SideA: "Aiko", SideB: "Cleo", Status: state.MatchStatusScheduled, ScheduledAt: "09:40"},
		{ID: "Pool A-2", Court: "A", SideA: "Bea", SideB: "Cleo", Status: state.MatchStatusCompleted, ScheduledAt: "09:00"},
	})
	saveQueueComp(t, store, "b", state.CompStatusPools, []state.MatchResult{
		{ID: "Pool A-0", Court: "A", SideA: "Dai", SideB: "Eri", Status: state.MatchStatusRunning, ScheduledAt: "09:05"},
		{ID: "Pool A-1", Court: "A", SideA: "Dai", SideB: "Fumi", Status: state.MatchStatusScheduled, ScheduledAt: "09:20"},
	})
	saveQueueComp(t, store, "c", state.CompStatusPools, []state.MatchResult{
		{ID: "Pool A-0", Court: "A", SideA: "Gen", SideB: "Hana", Status: state.MatchStatusScheduled, ScheduledAt: "09:15"},
		{ID: "Pool A-1", Court: "A", SideA: "Gen", SideB: "Ichi", Status: state.MatchStatusScheduled, ScheduledAt: "09:30"},
	})

	aggregate := getViewerAggregate(t, r)
	positions := 0
	for _, id := range []string{"a", "b", "c"} {
		want, ok := aggregate[id]
		require.Truef(t, ok, "aggregate holds %s", id)
		got := getViewerDetail(t, r, id)
		require.Len(t, got.PoolMatches, len(want.PoolMatches))
		for i, m := range got.PoolMatches {
			assert.Equalf(t, want.PoolMatches[i].ID, m.ID, "%s match order", id)
			assert.Equalf(t, want.PoolMatches[i].QueuePosition, m.QueuePosition, "%s %s", id, m.ID)
			if m.QueuePosition > 0 {
				positions++
			}
		}
	}
	assert.Equal(t, 5, positions, "every scheduled match on court A is queued, so the comparison is not vacuous")
}

// TestViewerDetail_PanicInCourtQueueLoadReturnsHTTP500: a panic while the
// detail view loads another competition for the court queue is answered as
// an internal error, never crashing the process and never swallowed.
func TestViewerDetail_PanicInCourtQueueLoadReturnsHTTP500(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "T", Password: "p"}))

	// The served competition holds a queued match, so the other
	// competitions are loaded at all.
	saveQueueComp(t, store, "served", state.CompStatusPools, []state.MatchResult{
		{ID: "Pool A-0", Court: "A", SideA: "Aiko", SideB: "Bea", Status: state.MatchStatusScheduled, ScheduledAt: "09:10"},
	})
	saveQueueComp(t, store, "other", state.CompStatusPools, []state.MatchResult{
		{ID: "Pool A-0", Court: "A", SideA: "Dai", SideB: "Eri", Status: state.MatchStatusScheduled, ScheduledAt: "09:20"},
	})

	swapViewerLoadCompetition(t, func(s *state.Store, compID string) (*state.Competition, error) {
		if compID == "other" {
			panic("simulated panic loading another competition")
		}
		return s.LoadCompetition(compID)
	})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions/served", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
}
