package mobileapp

import (
	"bytes"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizePoolConfig_LeagueAndKnockout verifies the normalizePoolConfig
// pure function directly (no HTTP round-trip needed for the unit assertion).
func TestNormalizePoolConfig_LeagueAndKnockout(t *testing.T) {
	cases := []struct {
		name          string
		format        string
		inPoolSize    int
		inPoolWinners int
		wantPoolSize  int
		wantWinners   int
	}{
		{
			name:          "league zeroes poolSize and poolWinners",
			format:        state.CompFormatLeague,
			inPoolSize:    5,
			inPoolWinners: 2,
			wantPoolSize:  0,
			wantWinners:   0,
		},
		{
			name:          "knockout zeroes poolSize and poolWinners",
			format:        state.CompFormatKnockout,
			inPoolSize:    8,
			inPoolWinners: 3,
			wantPoolSize:  0,
			wantWinners:   0,
		},
		{
			name:          "mixed preserves poolSize and poolWinners",
			format:        state.CompFormatMixed,
			inPoolSize:    4,
			inPoolWinners: 2,
			wantPoolSize:  4,
			wantWinners:   2,
		},
		{
			name:          "swiss preserves poolSize and poolWinners",
			format:        state.CompFormatSwiss,
			inPoolSize:    6,
			inPoolWinners: 1,
			wantPoolSize:  6,
			wantWinners:   1,
		},
		{
			name:          "empty format preserves poolSize and poolWinners",
			format:        "",
			inPoolSize:    4,
			inPoolWinners: 2,
			wantPoolSize:  4,
			wantWinners:   2,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			comp := &state.Competition{
				Format:      tc.format,
				PoolSize:    tc.inPoolSize,
				PoolWinners: tc.inPoolWinners,
			}
			normalizePoolConfig(comp)
			assert.Equal(t, tc.wantPoolSize, comp.PoolSize, "PoolSize")
			assert.Equal(t, tc.wantWinners, comp.PoolWinners, "PoolWinners")
		})
	}
}

// TestPOSTCompetition_LeaguePoolConfigNormalized verifies that creating a
// league competition with poolSize / poolWinners set results in them being
// silently zeroed on disk (API normalises, does not reject).
func TestPOSTCompetition_LeaguePoolConfigNormalized(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	body, _ := json.Marshal(state.Competition{
		Name:        "League Cup",
		Format:      state.CompFormatLeague,
		Kind:        "individual",
		PoolSize:    6,
		PoolWinners: 3,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	// Verify the stored competition has zeroed pool config.
	stored, err := store.LoadCompetition("league-cup")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 0, stored.PoolSize, "league PoolSize must be zeroed on create")
	assert.Equal(t, 0, stored.PoolWinners, "league PoolWinners must be zeroed on create")
}

// TestPUTCompetition_LeaguePoolConfigNormalized verifies that updating a
// league competition with poolSize / poolWinners set results in them being
// silently zeroed on disk (settings PUT normalises, does not reject).
func TestPUTCompetition_LeaguePoolConfigNormalized(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed a league competition (with zeroed pool config on disk).
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     "league-edit",
		Name:   "League Edit",
		Format: state.CompFormatLeague,
		Kind:   "individual",
	}))

	// PUT with poolSize / poolWinners set, they must be zeroed on save.
	body, _ := json.Marshal(state.Competition{
		ID:          "league-edit",
		Name:        "League Edit",
		Format:      state.CompFormatLeague,
		Kind:        "individual",
		PoolSize:    7,
		PoolWinners: 2,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/league-edit", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	stored, err := store.LoadCompetition("league-edit")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 0, stored.PoolSize, "league PoolSize must be zeroed on PUT")
	assert.Equal(t, 0, stored.PoolWinners, "league PoolWinners must be zeroed on PUT")
}

// TestPUTCompetition_LeagueWritesTwoThirdPlacesNotTheLegacyField verifies that
// a league's settings PUT stores twoThirdPlaces (bc-3rdp) and never writes the
// legacy leagueTwoThirdPlaces, which is read-only now. The league has no
// tie-break band to set: it is always the top three places.
func TestPUTCompetition_LeagueWritesTwoThirdPlacesNotTheLegacyField(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "lp-setup",
		Name:     "LP Setup",
		Format:   state.CompFormatLeague,
		Kind:     "team",
		TeamSize: 5,
		Status:   state.CompStatusSetup,
	}))
	twoThirdPlacesOn := true
	body, _ := json.Marshal(state.Competition{
		ID: "lp-setup", Name: "LP Setup", Format: state.CompFormatLeague,
		Kind: "team", TeamSize: 5, TwoThirdPlaces: &twoThirdPlacesOn,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/lp-setup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err := store.LoadCompetition("lp-setup")
	require.NoError(t, err)
	require.NotNil(t, stored.TwoThirdPlaces)
	assert.True(t, *stored.TwoThirdPlaces)
	assert.False(t, stored.LeagueTwoThirdPlaces, "legacy field is never written by this PUT any more")
}

// TestPUTCompetition_TwoThirdPlacesImmutableAfterStart (bc-3rdp) verifies
// twoThirdPlaces can be changed before the competition starts but is
// rejected (400) once it has progressed past setup, mirroring the sibling
// Naginata guard: the bronze match is built at draw
// time, so flipping the rule afterward would add or remove a bronze match
// while results may already be in flight.
func TestPUTCompetition_TwoThirdPlacesImmutableAfterStart(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	trueVal := true
	falseVal := false

	// Pre-start (setup): a change is accepted.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:             "tp-setup",
		Name:           "TP Setup",
		Format:         state.CompFormatKnockout,
		Kind:           "individual",
		Status:         state.CompStatusSetup,
		TwoThirdPlaces: &trueVal,
	}))
	body, _ := json.Marshal(state.Competition{
		ID: "tp-setup", Name: "TP Setup", Format: state.CompFormatKnockout,
		Kind: "individual", TwoThirdPlaces: &falseVal,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/tp-setup", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	stored, err := store.LoadCompetition("tp-setup")
	require.NoError(t, err)
	require.NotNil(t, stored.TwoThirdPlaces)
	assert.False(t, *stored.TwoThirdPlaces, "pre-start change must persist")

	// Started (pools): a change is rejected with 400, on-disk value unchanged.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:             "tp-started",
		Name:           "TP Started",
		Format:         state.CompFormatKnockout,
		Kind:           "individual",
		Status:         state.CompStatusPools,
		TwoThirdPlaces: &trueVal,
	}))
	body, _ = json.Marshal(state.Competition{
		ID: "tp-started", Name: "TP Started", Format: state.CompFormatKnockout,
		Kind: "individual", TwoThirdPlaces: &falseVal,
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/tp-started", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	stored, err = store.LoadCompetition("tp-started")
	require.NoError(t, err)
	require.NotNil(t, stored.TwoThirdPlaces)
	assert.True(t, *stored.TwoThirdPlaces, "started competition's twoThirdPlaces must be unchanged")
}

// TestPUTCompetition_MixedPoolConfigPreserved verifies that a mixed
// competition retains its poolSize / poolWinners after a settings PUT.
func TestPUTCompetition_MixedPoolConfigPreserved(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          "mixed-edit",
		Name:        "Mixed Edit",
		Format:      state.CompFormatMixed,
		Kind:        "individual",
		PoolSize:    5,
		PoolWinners: 2,
	}))

	body, _ := json.Marshal(state.Competition{
		ID:          "mixed-edit",
		Name:        "Mixed Edit",
		Format:      state.CompFormatMixed,
		Kind:        "individual",
		PoolSize:    4,
		PoolWinners: 1,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/mixed-edit", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	stored, err := store.LoadCompetition("mixed-edit")
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, 4, stored.PoolSize, "mixed PoolSize must be preserved")
	assert.Equal(t, 1, stored.PoolWinners, "mixed PoolWinners must be preserved")
}

// TestUpdateCompetition_MixedFormatRequiresUsablePoolSize verifies bc-symm's
// fix for a settings PUT that flips format to mixed without a usable pool
// size. normalizePoolConfig zeroes PoolSize/PoolWinners on the way TO
// league/knockout, but pre-fix nothing required a pool size on the way BACK
// to mixed, so a league/knockout -> mixed switch with an omitted/zero
// poolSize stored PoolSize 0 and the failure only surfaced much later at
// draw time (engine/pools.go: "pool size must be at least 1").
func TestUpdateCompetition_MixedFormatRequiresUsablePoolSize(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "league-to-mixed-no-poolsize"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "League To Mixed",
		Format: state.CompFormatLeague,
		Courts: []string{"A"},
		Status: state.CompStatusSetup,
	}))

	t.Run("REJECT flip to mixed with no usable pool size", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":     cid,
			"name":   "League To Mixed",
			"format": state.CompFormatMixed,
			"courts": []string{"A"},
			// poolSize intentionally omitted -> decodes to 0
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code,
			"a flip to mixed with no usable pool size must be rejected at write time: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompFormatLeague, stored.Format, "rejected write must not touch the stored record")
	})

	t.Run("ALLOW flip to mixed WITH a usable pool size", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":       cid,
			"name":     "League To Mixed",
			"format":   state.CompFormatMixed,
			"courts":   []string{"A"},
			"poolSize": 4,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompFormatMixed, stored.Format)
		assert.Equal(t, 4, stored.PoolSize)
	})
}

// TestCompetition_PoolWinnersBound pins the save-time bound on poolWinners on
// both doors. Before it, any value saved, and the calendar and the estimate
// then built a qualifier skeleton of pools x poolWinners leaves until the
// server ran out of memory. The PUT check is change-scoped like the pool-size
// guard, so a stored value past it stays editable for unrelated fields.
func TestCompetition_PoolWinnersBound(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)
	send := func(method, path string, body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest(method, path, bytes.NewBuffer(raw))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("POST refuses more winners than a pool holds", func(t *testing.T) {
		w := send("POST", "/api/competitions", map[string]any{
			"id": "pw-create", "name": "PW Create", "format": state.CompFormatMixed, "courts": []string{"A"},
			"poolSize": 3, "poolSizeMode": "max", "poolWinners": 300,
		})
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "poolWinners must be at most 3 for pools of 3 (got 300)")
		stored, err := store.LoadCompetition("pw-create")
		require.NoError(t, err)
		assert.Nil(t, stored, "a refused create persists nothing")
	})

	const cid = "pw-update"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "PW Update", Format: state.CompFormatMixed, Courts: []string{"A"},
		PoolSize: 3, PoolSizeMode: "max", PoolWinners: 300, Status: state.CompStatusSetup,
	}))
	base := map[string]any{"id": cid, "name": "PW Update", "format": state.CompFormatMixed, "courts": []string{"A"},
		"poolSize": 3, "poolSizeMode": "max", "poolWinners": 300}

	t.Run("PUT leaves a stored value editable for unrelated fields", func(t *testing.T) {
		body := maps.Clone(base)
		body["name"] = "PW Renamed"
		w := send("PUT", "/api/competitions/"+cid, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("PUT refuses a change that lands past the bound", func(t *testing.T) {
		body := maps.Clone(base)
		body["name"] = "PW Renamed"
		body["poolWinners"] = 299
		w := send("PUT", "/api/competitions/"+cid, body)
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "poolWinners must be at most 3")
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, 300, stored.PoolWinners, "a refused write leaves the record as it was")
	})

	t.Run("PUT accepts a change back inside the bound", func(t *testing.T) {
		body := maps.Clone(base)
		body["name"] = "PW Renamed"
		body["poolWinners"] = 2
		w := send("PUT", "/api/competitions/"+cid, body)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, 2, stored.PoolWinners)
	})
}

// TestCreateCompetition_MixedFormatRequiresUsablePoolSize is the POST twin of
// TestUpdateCompetition_MixedFormatRequiresUsablePoolSize above, and the two
// doors deliberately differ: the PUT guard is change-scoped to protect a
// stored value from a lockout, while POST authors a brand-new record with
// nothing to inherit and so runs unconditionally.
//
// What POST does NOT do is refuse an OMITTED pool size. That body
// (`{"name","format":"mixed","courts"}`) returned 201 for the whole life of
// the endpoint, four other tests in this package send it, and the identical
// competition arriving through /api/tournament/import lands fine because
// that door has always defaulted PoolSize. Both doors now read the same
// defaultPoolSize constant, so an omission cannot succeed at one and fail
// at the other. The guard keeps the case a caller STATES and the draw could
// never build.
func TestCreateCompetition_MixedFormatRequiresUsablePoolSize(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("DEFAULT an omitted pool size rather than refusing the create", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":     "create-mixed-no-poolsize",
			"name":   "Create Mixed No PoolSize",
			"format": state.CompFormatMixed,
			"courts": []string{"A"},
			// poolSize intentionally omitted -> decodes to 0
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code,
			"an omitted pool size must be defaulted, as /api/tournament/import has always defaulted it: %s", w.Body.String())

		stored, err := store.LoadCompetition("create-mixed-no-poolsize")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, defaultPoolSize, stored.PoolSize,
			"and it must be the SAME default the import door uses, or the two authoring doors answer one body differently")
	})

	t.Run("REJECT a stated pool size the draw could never build", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":       "create-mixed-negative-poolsize",
			"name":     "Create Mixed Negative PoolSize",
			"format":   state.CompFormatMixed,
			"courts":   []string{"A"},
			"poolSize": -1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code,
			"a stated impossible pool size is the caller's own value and must be refused: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "mixed format requires a pool size of at least 1")

		stored, err := store.LoadCompetition("create-mixed-negative-poolsize")
		require.NoError(t, err)
		assert.Nil(t, stored, "a rejected create must persist nothing")
	})

	// The default is scoped to "mixed" on this door: normalizePoolConfig
	// runs first and zeroes PoolSize for league/knockout, and it must stay
	// zeroed -- a bare bracket has no pool phase to size.
	t.Run("does not default a pool size for a format that has no pool phase", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":     "create-knockout-no-poolsize",
			"name":   "Create Knockout No PoolSize",
			"format": state.CompFormatKnockout,
			"courts": []string{"A"},
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		stored, err := store.LoadCompetition("create-knockout-no-poolsize")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, 0, stored.PoolSize, "normalizePoolConfig zeroed this; the default must not undo it")
	})

	t.Run("ALLOW create with mixed format and a usable pool size", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":       "create-mixed-with-poolsize",
			"name":     "Create Mixed With PoolSize",
			"format":   state.CompFormatMixed,
			"courts":   []string{"A"},
			"poolSize": 4,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		stored, err := store.LoadCompetition("create-mixed-with-poolsize")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, 4, stored.PoolSize)
	})
}

// TestCreateCompetition_RejectsIllegalPoolSizeMode pins the POST half of
// state.ValidatePoolSizeMode (operator decision 2026-11-08): a mode other
// than "max" or "min" is refused, not run as minimum sizing. An omitted
// mode stays legal.
func TestCreateCompetition_RejectsIllegalPoolSizeMode(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	post := func(body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(raw))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("REJECT an unrecognised mode", func(t *testing.T) {
		w := post(map[string]any{
			"id": "create-illegal-mode", "name": "Create Illegal Mode",
			"format": "mixed", "poolSize": 3, "poolSizeMode": "maxx",
		})
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `poolSizeMode: unknown poolSizeMode \"maxx\"`)

		stored, err := store.LoadCompetition("create-illegal-mode")
		require.NoError(t, err)
		assert.Nil(t, stored, "a rejected create must persist nothing")
	})

	for _, mode := range []string{"", "max", " min "} {
		t.Run("ALLOW "+strconv.Quote(mode), func(t *testing.T) {
			id := "create-mode-" + strings.TrimSpace(mode)
			w := post(map[string]any{
				"id": id, "name": "Create Mode " + strings.TrimSpace(mode),
				"format": "mixed", "poolSize": 3, "poolSizeMode": mode,
			})
			require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
			stored, err := store.LoadCompetition(id)
			require.NoError(t, err)
			require.NotNil(t, stored)
			assert.Equal(t, strings.TrimSpace(mode), stored.PoolSizeMode)
		})
	}
}

// TestUpdateCompetition_PoolSizeModeGuard is the change-scoped pair for
// state.ValidatePoolSizeMode on PUT, the same shape as
// TestUpdateCompetition_KindGuard: a change to an illegal mode is refused,
// but a save that echoes an already-illegal stored mode back unchanged
// still lands, so a hand-edited config.md never locks the settings screen.
func TestUpdateCompetition_PoolSizeModeGuard(t *testing.T) {
	put := func(r http.Handler, cid string, body map[string]any) *httptest.ResponseRecorder {
		raw, _ := json.Marshal(body)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(raw))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("REJECT a change to an illegal mode", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		const cid = "mode-guard-change"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Mode Guard Change", Format: state.CompFormatMixed,
			PoolSize: 3, PoolSizeMode: "max", Status: state.CompStatusSetup,
		}))

		w := put(r, cid, map[string]any{
			"id": cid, "name": "Mode Guard Change", "format": "mixed",
			"poolSize": 3, "poolSizeMode": "maxx",
		})
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `poolSizeMode: unknown poolSizeMode \"maxx\"`)

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, "max", stored.PoolSizeMode, "the rejected write must not land")
	})

	t.Run("REJECT an illegal mode as 400 ahead of the draw-ready 409", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		const cid = "mode-guard-draw-ready"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Mode Guard Draw Ready", Format: state.CompFormatMixed,
			PoolSize: 3, PoolSizeMode: "max", Status: state.CompStatusDrawReady,
		}))

		w := put(r, cid, map[string]any{
			"id": cid, "name": "Mode Guard Draw Ready", "format": "mixed",
			"poolSize": 3, "poolSizeMode": "maxx",
		})
		require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), `poolSizeMode: unknown poolSizeMode \"maxx\"`)

		// A valid change to the mode is the draw-ready refusal, so the 400
		// above is the order of the checks and not a missing guard.
		w = put(r, cid, map[string]any{
			"id": cid, "name": "Mode Guard Draw Ready", "format": "mixed",
			"poolSize": 3, "poolSizeMode": "min",
		})
		require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	})

	t.Run("ALLOW a save that echoes an ALREADY-illegal stored mode unchanged (lockout regression)", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		const cid = "mode-guard-lockout"
		// SaveCompetition bypasses the handler, as a hand-edited config.md
		// or a record from before the guard would.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: cid, Name: "Mode Guard Lockout", Format: state.CompFormatMixed,
			PoolSize: 3, PoolSizeMode: "maxx", Date: "12-05-2026",
			StartTime: "09:00", Status: state.CompStatusSetup,
		}))

		w := put(r, cid, map[string]any{
			"id": cid, "name": "Mode Guard Lockout", "format": "mixed",
			"poolSize": 3, "poolSizeMode": "maxx",
			"date": "12-05-2026", "startTime": "10:30",
		})
		require.Equal(t, http.StatusOK, w.Code,
			"a save that does not change an already-illegal mode must succeed: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "maxx", stored.PoolSizeMode, "the illegal mode is preserved, not silently corrected")
		assert.Equal(t, "10:30", stored.StartTime, "the unrelated edit must land")
	})
}
