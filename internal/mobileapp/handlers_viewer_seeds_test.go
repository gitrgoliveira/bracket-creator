package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestViewerAggregateCarriesSeeds pins that the AGGREGATE competitions list
// merges seeds onto its players, matching the single-competition detail
// endpoint. It used to load WithSeeds: false, and the admin SPA renders
// AdminCompetition off this aggregate object until the detail fetch lands --
// permanently, if that fetch fails -- so the fill-bracket settings preview,
// whose supply comes from the roster's seed ranks, briefly showed the
// UNSEEDED pool cut: a different pool COUNT, not a missing annotation
// (code-review round, cross-file tracer).
func TestViewerAggregateCarriesSeeds(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: "seeded-agg", Name: "Seeded", Format: state.CompFormatMixed,
		PoolSize: 3, PoolSizeMode: "min", PoolWinners: 1, Courts: []string{"A"},
	}))
	require.NoError(t, store.SaveParticipants("seeded-agg", []domain.Player{
		{Name: "Alice", Dojo: "D1"}, {Name: "Bob", Dojo: "D2"}, {Name: "Carol", Dojo: "D3"},
	}))
	require.NoError(t, store.SaveSeeds("seeded-agg", []domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1},
		{Name: "Bob", SeedRank: 2},
	}))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/viewer/competitions", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var comps []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &comps))
	require.Len(t, comps, 1)
	config, ok := comps[0]["config"].(map[string]any)
	require.True(t, ok, "aggregate rows nest the competition under config")
	players, ok := config["players"].([]any)
	require.True(t, ok)
	seeds := map[string]float64{}
	for _, p := range players {
		pm := p.(map[string]any)
		if s, ok := pm["seed"].(float64); ok && s > 0 {
			seeds[pm["name"].(string)] = s
		}
	}
	assert.Equal(t, map[string]float64{"Alice": 1, "Bob": 2}, seeds,
		"the aggregate list must carry the same seeds the detail endpoint serves, or every consumer that falls back to it renders the unseeded state")
}
