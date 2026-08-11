package mobileapp

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The operator console's seed-warning surface (R2/D7 of specs/007-ekc-draw).
//
// A seeding constraint the configuration cannot satisfy never refuses the
// draw; the operator is told what was relaxed. Two surfaces carry that, both
// admin-gated and both computed from the same engine call: the generate-draw
// response and the competition detail GET carry it as drawWarnings (the result
// of the action the operator just took), and GET
// /competitions/:id/draw-warnings serves it on its own, which is what the
// console reads because it loads a competition through the PUBLIC viewer
// detail. Nothing is persisted, so a discarded and regenerated draw can never
// serve a stale warning.

// seedWarningComp creates a mixed competition of numPlayers entrants in pools
// of four, with the first numSeeds of them seeded 1..numSeeds.
func seedWarningComp(t *testing.T, store *state.Store, id string, numPlayers, numSeeds int, courts []string) {
	t.Helper()
	players := make([]domain.Player, numPlayers)
	seeds := make([]domain.SeedAssignment, 0, numSeeds)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%02d", i+1), Dojo: fmt.Sprintf("Dojo %02d", i+1)}
		if i < numSeeds {
			seeds = append(seeds, domain.SeedAssignment{Name: players[i].Name, Dojo: players[i].Dojo, SeedRank: i + 1})
		}
	}
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           id,
		Name:         id,
		Format:       state.CompFormatMixed,
		PoolSize:     4,
		PoolSizeMode: "max",
		PoolWinners:  2,
		Courts:       courts,
		Status:       state.CompStatusSetup,
	}))
	require.NoError(t, store.SaveParticipants(id, players))
	require.NoError(t, store.SaveSeeds(id, seeds))
}

func drawWarningsOf(t *testing.T, body []byte) []string {
	t.Helper()
	var payload struct {
		DrawWarnings []string `json:"drawWarnings"`
	}
	require.NoError(t, json.Unmarshal(body, &payload))
	return payload.DrawWarnings
}

func TestDrawWarningsOnCompetitionResponses(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// 12 entrants in pools of 4 gives 3 pools, which cannot host 4 seeds:
	// two seeds may never share a pool, so rank 4 is ignored (R2).
	cid := "seed-warn-comp"
	seedWarningComp(t, store, cid, 12, 4, []string{"A", "B"})

	var generated []string
	t.Run("generate-draw reports what was relaxed", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/generate-draw", nil)
		r.ServeHTTP(w, req)
		require.Equalf(t, http.StatusOK, w.Code,
			"a seeding constraint that cannot be met is a warning, never a refusal: %s", w.Body.String())

		generated = drawWarningsOf(t, w.Body.Bytes())
		require.NotEmpty(t, generated, "4 seeds over 3 pools must warn")
		assert.Contains(t, generated[0], "Seed 4 ignored")
	})

	t.Run("competition detail carries the same warnings", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/api/competitions/"+cid, nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, generated, drawWarningsOf(t, w.Body.Bytes()),
			"the operator coming back to the page must read what generate-draw told them")

		// The wrapper must not have swallowed the competition itself.
		var comp state.Competition
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &comp))
		assert.Equal(t, cid, comp.ID)
		assert.Equal(t, state.CompStatusDrawReady, comp.Status)
	})
}

// TestNoDrawWarningsWithoutSeeds pins the quiet case: an unseeded competition
// is a normal configuration and its detail response carries no warnings at all
// (the field is omitted, so the console renders nothing).
func TestNoDrawWarningsWithoutSeeds(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "seed-quiet-comp"
	seedWarningComp(t, store, cid, 20, 0, []string{"A", "B"})

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/generate-draw", nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, drawWarningsOf(t, w.Body.Bytes()))
	assert.NotContains(t, w.Body.String(), "drawWarnings")

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/competitions/"+cid, nil)
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, drawWarningsOf(t, w.Body.Bytes()))
}

// TestDrawWarningsEndpoint covers the surface the operator console actually
// reads. The console loads a competition through the PUBLIC viewer detail, so a
// field on the admin competition record would never reach the page that has to
// show it; this admin-gated endpoint is what does.
func TestDrawWarningsEndpoint(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	warningsAt := func(path string) (int, []string) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", path, nil)
		r.ServeHTTP(w, req)
		var payload struct {
			Warnings []string `json:"warnings"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &payload)
		return w.Code, payload.Warnings
	}

	cid := "seed-endpoint-comp"
	seedWarningComp(t, store, cid, 12, 4, []string{"A", "B"})

	t.Run("before the draw: 200 and nothing to report", func(t *testing.T) {
		code, warnings := warningsAt("/api/competitions/" + cid + "/draw-warnings")
		assert.Equal(t, http.StatusOK, code, "no draw yet is normal, not an error")
		assert.Empty(t, warnings)
	})

	t.Run("after the draw: what was relaxed", func(t *testing.T) {
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+cid+"/generate-draw", nil)
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		code, warnings := warningsAt("/api/competitions/" + cid + "/draw-warnings")
		assert.Equal(t, http.StatusOK, code)
		require.Len(t, warnings, 1)
		assert.Contains(t, warnings[0], "Seed 4 ignored")
	})

	t.Run("unknown competition: 404", func(t *testing.T) {
		code, _ := warningsAt("/api/competitions/no-such-comp/draw-warnings")
		assert.Equal(t, http.StatusNotFound, code)
	})
}
