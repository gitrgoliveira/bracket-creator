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
// normalizePoolConfig zeroes ExtraQualifiers for every non-pool-fed format
// (league, playoffs, swiss; TestPUTCompetition_Swiss_StaleExtraQualifiers
// covers the swiss settings path) BEFORE ValidateExtraQualifiers runs, so a stray
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

// --- omitted-vs-explicit wire contract (review finding, second round) ---
//
// "" is BOTH the JSON zero value and the Standard selection, so the PUT body
// alone cannot say whether a client chose Standard or never knew the field
// existed. competitionUpdateRequest carries the key's presence to tell them
// apart. These three tests pin both halves of that contract plus the
// re-validation, because getting the first half right at the cost of the
// second (making Standard unreachable) would be worse than the bug.

// TestPUTCompetition_OmittedExtraQualifiers_KeepsStoredMode: a settings PUT
// that never mentions extraQualifiers must leave a stored non-standard mode
// alone. Before the fix this cleared it to Standard silently, and the next
// generate-draw ran the uniform builder with nothing logged anywhere.
// Red-verified: reverting the restore stores "".
func TestPUTCompetition_OmittedExtraQualifiers_KeepsStoredMode(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "eq-omitted-keeps"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:              cid,
		Name:            "EQ Omitted Keeps",
		Kind:            "individual",
		Format:          state.CompFormatMixed,
		Status:          state.CompStatusSetup,
		PoolSize:        3,
		PoolSizeMode:    "min",
		PoolWinners:     1,
		ExtraQualifiers: state.ExtraQualifiersFillBracket,
	}))

	// A client on the pre-LP-5a contract: every settings field it knows
	// about, and no extraQualifiers key at all.
	body, _ := json.Marshal(map[string]any{
		"id":           cid,
		"name":         "EQ Omitted Keeps (renamed)",
		"format":       state.CompFormatMixed,
		"kind":         "individual",
		"poolSize":     3,
		"poolSizeMode": "min",
		"poolWinners":  1,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	saved, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, state.ExtraQualifiersFillBracket, saved.ExtraQualifiers,
		"an omitted extraQualifiers must keep the stored mode, not silently clear it to Standard")
	assert.Equal(t, "EQ Omitted Keeps (renamed)", saved.Name, "the rest of the PUT must still apply")
}

// TestPUTCompetition_ExplicitStandard_ClearsStoredMode is the other half, and
// the reason the TeamMatchType trick could not simply be copied: an EXPLICIT
// "" is a real operator choice and must still switch a competition back to
// Standard. A fix that made omitted-keeps-stored by treating "" as absent
// would make Standard unreachable, which is worse than the bug it closes.
// Red-verified: treating "" as an omission leaves fill-bracket stored.
func TestPUTCompetition_ExplicitStandard_ClearsStoredMode(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "eq-explicit-standard"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:              cid,
		Name:            "EQ Explicit Standard",
		Kind:            "individual",
		Format:          state.CompFormatMixed,
		Status:          state.CompStatusSetup,
		PoolSize:        3,
		PoolSizeMode:    "min",
		PoolWinners:     1,
		ExtraQualifiers: state.ExtraQualifiersFillBracket,
	}))

	// What the SPA sends when the operator picks Standard: the key, empty.
	body, _ := json.Marshal(map[string]any{
		"id":              cid,
		"name":            "EQ Explicit Standard",
		"format":          state.CompFormatMixed,
		"kind":            "individual",
		"poolSize":        3,
		"poolSizeMode":    "min",
		"poolWinners":     1,
		"extraQualifiers": "",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	saved, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, state.ExtraQualifiersNone, saved.ExtraQualifiers,
		"an explicit \"\" is the Standard selection and must clear the stored mode")
}

// TestPUTCompetition_OmittedExtraQualifiers_RevalidatesAgainstNewPoolSizing
// pins the hazard the restore itself introduces. The settings block validates
// the INCOMING value, which is "" for an omitting client and always passes.
// Restoring a non-standard mode afterwards therefore pairs it with pool
// sizing nothing validated it against: here the PUT switches to maximum
// sizing, which ValidateExtraQualifiers forbids for a non-standard mode. The
// re-validation inside the transform must 400 rather than persist the pair.
// Red-verified: dropping the re-validation stores fill-bracket + "max".
func TestPUTCompetition_OmittedExtraQualifiers_RevalidatesAgainstNewPoolSizing(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "eq-omitted-revalidate"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:              cid,
		Name:            "EQ Omitted Revalidate",
		Kind:            "individual",
		Format:          state.CompFormatMixed,
		Status:          state.CompStatusSetup,
		PoolSize:        3,
		PoolSizeMode:    "min",
		PoolWinners:     1,
		ExtraQualifiers: state.ExtraQualifiersFillBracket,
	}))

	body, _ := json.Marshal(map[string]any{
		"id":           cid,
		"name":         "EQ Omitted Revalidate",
		"format":       state.CompFormatMixed,
		"kind":         "individual",
		"poolSize":     4,
		"poolSizeMode": "max", // fill-bracket requires minimum sizing
		"poolWinners":  1,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusBadRequest, w.Code,
		"restoring a non-standard mode onto maximum sizing must be refused, not persisted; response: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "extraQualifiers")

	saved, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, "min", saved.PoolSizeMode, "the refused PUT must not have persisted any of its changes")
}

// TestPUTCompetition_Swiss_StaleExtraQualifiers is the wedge the missing
// swiss case caused (review finding on this PR). A swiss record can hold a
// stale non-standard ExtraQualifiers (a hand-edited config.md, or an import
// manifest row with format: swiss + extra_qualifiers: larger-pools, which
// importCompetition validated without zeroing). The admin settings page PUTs
// its FULL local state, so the stale value rides along on every save; with
// pool_winners 0/absent, EffectivePoolWinners() reads 2 and
// ValidateExtraQualifiers 400s even a plain rename -- over a radio the UI
// only renders for mixed, so the operator has no control to clear it. The
// PUT must succeed and zero the value.
// Red-verified: removing CompFormatSwiss from normalizeExtraQualifiers's
// switch turns the 200 into a 400.
func TestPUTCompetition_Swiss_StaleExtraQualifiers(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "swiss-stale-eq"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:              cid,
		Name:            "Swiss Stale EQ",
		Kind:            "individual",
		Format:          state.CompFormatSwiss,
		SwissRounds:     4,
		Status:          state.CompStatusSetup,
		PoolSizeMode:    "min",
		ExtraQualifiers: state.ExtraQualifiersLargerPools, // stale; PoolWinners 0 -> effective 2 -> validate rejects the pair
	}))

	// The SPA PUTs its whole local state, so the stale value is in the body.
	body, _ := json.Marshal(map[string]any{
		"id":              cid,
		"name":            "Swiss Stale EQ (renamed)",
		"format":          state.CompFormatSwiss,
		"kind":            "individual",
		"swissRounds":     4,
		"poolSizeMode":    "min",
		"extraQualifiers": state.ExtraQualifiersLargerPools,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code,
		"a settings PUT on a swiss record with a stale extraQualifiers must not wedge; response: %s", w.Body.String())

	saved, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, state.ExtraQualifiersNone, saved.ExtraQualifiers,
		"the stale value must be zeroed, exactly as league/playoffs are")
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
