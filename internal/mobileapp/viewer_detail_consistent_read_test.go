package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A write that commits while a competition detail read is under way must
// never leave the answer half before and half after it. The seam lands a
// real kiken between the detail's concurrent loads (pool matches read
// before it) and its competitor-status read (after it): read as it was
// built, the answer showed Pool A-0 still scheduled while Pool A-1 already
// carried Alice's bar from that very kiken. The detail is one read of one
// state, so it is built again when a write landed during the build.
func TestViewerCompetitionDetail_WriteDuringTheBuildIsNeverHalfShown(t *testing.T) {
	tempDir := t.TempDir()
	store, err := state.NewStore(tempDir)
	require.NoError(t, err)
	eng := engine.New(store)

	compID := "detail-consistent-read"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatMixed, Status: state.CompStatusPools,
	}))
	aliceID, bobID, carolID := helper.NewUUID4(), helper.NewUUID4(), helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: aliceID, Name: "Alice", Dojo: "A"},
		{ID: bobID, Name: "Bob", Dojo: "B"},
		{ID: carolID, Name: "Carol", Dojo: "C"},
	}))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alice", SideB: "Bob", SideAID: aliceID, SideBID: bobID, Court: "A", Status: state.MatchStatusScheduled},
		{ID: "Pool A-1", SideA: "Alice", SideB: "Carol", SideAID: aliceID, SideBID: carolID, Court: "A", Status: state.MatchStatusScheduled},
	}))

	builds := 0
	viewerDetailAfterConcurrentLoads = func(id string) {
		builds++
		if builds != 1 {
			return
		}
		_, _, derr := eng.RecordDecision(id, "Pool A-0", "kiken-injury", "aka", "test", nil)
		assert.NoError(t, derr)
	}
	t.Cleanup(func() { viewerDetailAfterConcurrentLoads = nil })

	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterViewerHandlers(r.Group("/api/viewer"), store, eng)

	w := httptest.NewRecorder()
	req, err := http.NewRequest(http.MethodGet, "/api/viewer/competitions/"+compID, nil)
	require.NoError(t, err)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	matches := map[string]map[string]any{}
	for _, raw := range body["poolMatches"].([]any) {
		m := raw.(map[string]any)
		matches[m["id"].(string)] = m
	}

	barred, _ := matches["Pool A-1"]["ineligibleSides"].(map[string]any)
	require.NotNil(t, barred, "the answer is read after the kiken: Pool A-1 carries Alice's bar, got %+v", matches["Pool A-1"])
	assert.Equal(t, "kiken-injury", barred["a"])
	assert.Equal(t, "completed", matches["Pool A-0"]["status"],
		"the kiken that barred Alice must show on Pool A-0 in the same answer")
	assert.Equal(t, 2, builds, "the build that a write overlapped is made again, once")
}
