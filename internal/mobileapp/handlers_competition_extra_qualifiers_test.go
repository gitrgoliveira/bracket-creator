package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests for bc-qual LP-5a: the mobileapp PUT/POST competition handlers must
// call state.ValidateExtraQualifiers (internal/state/models.go) on
// Competition.ExtraQualifiers so an invalid combination (unknown value, or
// larger-pools/fill-bracket outside minimum-players-per-pool sizing / at
// poolWinners >= 2) is rejected with a 400 at SAVE time, not discovered
// later when the draw builder refuses to build (internal/engine/pools.go's
// own defense-in-depth ValidateExtraQualifiers call). ValidateExtraQualifiers
// itself is exhaustively tested in internal/state/models_test.go; these
// tests only pin that the HTTP layer actually calls it, maps a failure to
// 400 with the validator's message, and persists a valid value end-to-end
// (POST create, PUT settings-only, draw-ready output-affecting gate).

// TestPOSTCompetition_ExtraQualifiers_Validation table-drives the POST
// /competitions create path across the same value/poolSizeMode/poolWinners
// combinations state.TestValidateExtraQualifiers pins directly, confirming
// the HTTP layer surfaces the validator's own rejection rather than a
// generic bind error or a silent accept.
func TestPOSTCompetition_ExtraQualifiers_Validation(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	tests := []struct {
		name            string
		extraQualifiers string
		poolSizeMode    string
		poolWinners     int
		wantStatus      int
		wantErrContains string
	}{
		{
			name:            "standard (empty) is always valid, even at max sizing and 2 winners",
			extraQualifiers: "",
			poolSizeMode:    "max",
			poolWinners:     2,
			wantStatus:      http.StatusCreated,
		},
		{
			name:            "larger-pools rejected under max sizing",
			extraQualifiers: state.ExtraQualifiersLargerPools,
			poolSizeMode:    "max",
			poolWinners:     1,
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "minimum-players-per-pool sizing",
		},
		{
			name:            "larger-pools rejected at poolWinners=2 under min sizing",
			extraQualifiers: state.ExtraQualifiersLargerPools,
			poolSizeMode:    "min",
			poolWinners:     2,
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "pool winners",
		},
		{
			name:            "larger-pools accepted at min sizing, poolWinners=1",
			extraQualifiers: state.ExtraQualifiersLargerPools,
			poolSizeMode:    "min",
			poolWinners:     1,
			wantStatus:      http.StatusCreated,
		},
		{
			name:            "fill-bracket rejected under max sizing",
			extraQualifiers: state.ExtraQualifiersFillBracket,
			poolSizeMode:    "max",
			poolWinners:     1,
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "minimum-players-per-pool sizing",
		},
		{
			name:            "fill-bracket accepted at min sizing, poolWinners=1",
			extraQualifiers: state.ExtraQualifiersFillBracket,
			poolSizeMode:    "min",
			poolWinners:     1,
			wantStatus:      http.StatusCreated,
		},
		{
			name:            "unknown value rejected",
			extraQualifiers: "bogus-mode",
			poolSizeMode:    "min",
			poolWinners:     1,
			wantStatus:      http.StatusBadRequest,
			wantErrContains: "unknown extraQualifiers",
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			comp := state.Competition{
				ID:              "post-eq-" + string(rune('a'+i)),
				Name:            "Post EQ " + tc.name,
				Kind:            "individual",
				Format:          state.CompFormatMixed,
				PoolSize:        3,
				PoolSizeMode:    tc.poolSizeMode,
				PoolWinners:     tc.poolWinners,
				ExtraQualifiers: tc.extraQualifiers,
			}
			body, err := json.Marshal(comp)
			require.NoError(t, err)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)

			require.Equalf(t, tc.wantStatus, w.Code, "response: %s", w.Body.String())
			if tc.wantErrContains != "" {
				assert.Contains(t, w.Body.String(), tc.wantErrContains)
			}
			if tc.wantStatus == http.StatusCreated {
				saved, err := store.LoadCompetition(comp.ID)
				require.NoError(t, err)
				require.NotNil(t, saved)
				assert.Equal(t, tc.extraQualifiers, saved.ExtraQualifiers,
					"a successfully-created competition must persist the requested extraQualifiers verbatim")
			}
		})
	}
}

// TestPOSTCompetition_ExtraQualifiers_ZeroedForNonPoolFormats verifies
// normalizePoolConfig zeroes ExtraQualifiers for league/playoffs (mirroring
// PoolSize/PoolWinners) BEFORE ValidateExtraQualifiers runs, so a stray
// non-standard value sent for a format with no pool phase is silently
// dropped rather than rejected or persisted: ExtraQualifiers only has
// meaning for "mixed" (internal/engine/pools.go gates its own use of it on
// comp.Format == state.CompFormatMixed).
func TestPOSTCompetition_ExtraQualifiers_ZeroedForNonPoolFormats(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	comp := state.Competition{
		ID:              "playoffs-eq-zeroed",
		Name:            "Playoffs EQ Zeroed",
		Kind:            "individual",
		Format:          state.CompFormatPlayoffs,
		ExtraQualifiers: state.ExtraQualifiersLargerPools, // meaningless for playoffs; must not survive
	}
	body, err := json.Marshal(comp)
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusCreated, w.Code, "response: %s", w.Body.String())

	saved, err := store.LoadCompetition(comp.ID)
	require.NoError(t, err)
	require.NotNil(t, saved)
	assert.Equal(t, state.ExtraQualifiersNone, saved.ExtraQualifiers,
		"ExtraQualifiers must be zeroed for a format with no pool phase, mirroring PoolSize/PoolWinners")
}

// TestPUTCompetition_ExtraQualifiers_SettingsOnlyRoundTrip verifies the
// settings-only PUT path: (a) ValidateExtraQualifiers is called there too,
// (b) a valid value round-trips through the settings merge
// (current.ExtraQualifiers = comp.ExtraQualifiers).
func TestPUTCompetition_ExtraQualifiers_SettingsOnlyRoundTrip(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "put-eq-roundtrip"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     cid,
		Name:   "Put EQ Roundtrip",
		Kind:   "individual",
		Format: state.CompFormatMixed,
		Status: state.CompStatusSetup,
	}))

	// Reject: larger-pools with the default (unset->2) poolWinners.
	body, _ := json.Marshal(map[string]any{
		"id":              cid,
		"name":            "Put EQ Roundtrip",
		"format":          state.CompFormatMixed,
		"kind":            "individual",
		"poolSizeMode":    "min",
		"poolSize":        3,
		"extraQualifiers": state.ExtraQualifiersLargerPools,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code, "response: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "pool winners")

	// Accept: larger-pools with poolWinners=1, min sizing. Must persist and
	// round-trip through the settings merge.
	body, _ = json.Marshal(map[string]any{
		"id":              cid,
		"name":            "Put EQ Roundtrip",
		"format":          state.CompFormatMixed,
		"kind":            "individual",
		"poolSizeMode":    "min",
		"poolSize":        3,
		"poolWinners":     1,
		"extraQualifiers": state.ExtraQualifiersLargerPools,
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	saved, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, state.ExtraQualifiersLargerPools, saved.ExtraQualifiers,
		"extraQualifiers must survive a settings-only PUT via the current.ExtraQualifiers = comp.ExtraQualifiers merge")

	// Switch back to standard: must also round-trip (not stick at the prior value).
	body, _ = json.Marshal(map[string]any{
		"id":              cid,
		"name":            "Put EQ Roundtrip",
		"format":          state.CompFormatMixed,
		"kind":            "individual",
		"poolSizeMode":    "min",
		"poolSize":        3,
		"poolWinners":     2,
		"extraQualifiers": "",
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	saved, err = store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, state.ExtraQualifiersNone, saved.ExtraQualifiers)
}

// TestPUTCompetition_ExtraQualifiers_DrawReadyOutputAffectingGate verifies
// ExtraQualifiers is included in the draw-ready outputAffectingChanged set
// (handlers_competition.go): changing it while status=draw-ready must be
// rejected with 409, the same as PoolSize/PoolWinners/PoolSizeMode,
// because it changes which draw builder runs (or, for fill-bracket, how
// many pools are even cut) and the generated draw would desync from the
// stored config if the change were allowed silently.
func TestPUTCompetition_ExtraQualifiers_DrawReadyOutputAffectingGate(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "draw-ready-eq-gate"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:              cid,
		Name:            "Draw Ready EQ Gate",
		Format:          state.CompFormatMixed,
		Kind:            "individual",
		Courts:          []string{"A"},
		PoolSize:        3,
		PoolSizeMode:    "min",
		PoolWinners:     1,
		ExtraQualifiers: state.ExtraQualifiersNone,
		Status:          state.CompStatusDrawReady,
	}))

	body, _ := json.Marshal(map[string]any{
		"id":              cid,
		"name":            "Draw Ready EQ Gate",
		"format":          state.CompFormatMixed,
		"kind":            "individual",
		"courts":          []string{"A"},
		"poolSize":        3,
		"poolSizeMode":    "min",
		"poolWinners":     1,
		"extraQualifiers": state.ExtraQualifiersLargerPools, // only field changed from stored
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code,
		"changing extraQualifiers while draw-ready must return 409: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "discard",
		"409 body must mention discarding the draw, same as the other output-affecting fields")

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, state.ExtraQualifiersNone, stored.ExtraQualifiers,
		"the rejected change must not mutate the stored value")
	assert.Equal(t, state.CompStatusDrawReady, stored.Status)
}
