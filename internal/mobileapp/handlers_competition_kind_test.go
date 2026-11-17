package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateCompetitionEngiTeamExclusion pins Copilot #326: engi (individual
// PAIR paradigm) is mutually exclusive with team competitions. The admin UI
// hides the Engi toggle unless kind=individual, but the server must reject the
// contradictory combination so a hand-crafted POST can't create a team+engi
// comp that routes matches to the wrong scorer.
func TestCreateCompetitionEngiTeamExclusion(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("POST team + engi=true returns 400", func(t *testing.T) {
		comp := state.Competition{Name: "Team Engi", Kind: "team", TeamSize: 3, Engi: true}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		assert.Contains(t, w.Body.String(), "engi")
	})

	t.Run("POST individual + engi=true is accepted (not rejected by the exclusion)", func(t *testing.T) {
		comp := state.Competition{Name: "Indiv Engi", Kind: "individual", Engi: true, Format: "knockout", Date: "12-07-2026", StartTime: "09:00"}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.NotContains(t, w.Body.String(), "engi is only valid", "individual+engi must not trip the team-exclusion guard")
	})
}

func TestCreateCompetitionTeamSizeValidation(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("POST team with teamSize=1 returns 400", func(t *testing.T) {
		comp := state.Competition{
			Name:     "Team Size One",
			Kind:     "team",
			TeamSize: 1,
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("POST team with teamSize=0 returns 400", func(t *testing.T) {
		comp := state.Competition{
			Name:     "Team Size Zero",
			Kind:     "team",
			TeamSize: 0,
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("POST kind omitted with teamSize=1 returns 400", func(t *testing.T) {
		comp := state.Competition{
			Name:     "Ambiguous Size One",
			TeamSize: 1,
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("POST team with teamSize=2 succeeds", func(t *testing.T) {
		comp := state.Competition{
			Name:     "Team Size Two",
			Kind:     "team",
			TeamSize: 2,
		}
		body, _ := json.Marshal(comp)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusCreated, w.Code)
	})
}

func TestUpdateCompetitionTeamSizeValidation(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       "put-team-size-test",
		Name:     "PUT Team Size",
		Kind:     "team",
		TeamSize: 3,
		Status:   state.CompStatusSetup,
	}))

	t.Run("PUT team with teamSize=1 returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":     "PUT Team Size",
			"kind":     "team",
			"teamSize": 1,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/put-team-size-test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("PUT team with teamSize=0 returns 400", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":     "PUT Team Size",
			"kind":     "team",
			"teamSize": 0,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/put-team-size-test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "teamSize")
	})

	t.Run("PUT team with teamSize=2 succeeds", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"name":     "PUT Team Size",
			"kind":     "team",
			"teamSize": 2,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/put-team-size-test", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
	})
}

// TestUpdateCompetition_TeamMatchTypeLockedDrawReady verifies that changing
// teamMatchType while the competition is draw-ready returns 409 (A5, GAP 1).
// Changing this field after the draw is generated would make the running
// format inconsistent with the persisted bracket/pool structure.
func TestUpdateCompetition_TeamMatchTypeLockedDrawReady(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "team-match-type-lock"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            cid,
		Name:          "Team Match Type Lock",
		Format:        state.CompFormatMixed,
		Kind:          "team",
		TeamSize:      5,
		TeamMatchType: state.TeamMatchTypeFixed,
		Courts:        []string{"A"},
		PoolSize:      4,
		PoolWinners:   2,
		Status:        state.CompStatusDrawReady,
	}))

	t.Run("REJECT teamMatchType change from fixed to kachinuki while draw-ready", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":            cid,
			"name":          "Team Match Type Lock",
			"format":        state.CompFormatMixed,
			"kind":          "team",
			"teamSize":      5,
			"teamMatchType": state.TeamMatchTypeKachinuki, // changed from "fixed", output-affecting
			"courts":        []string{"A"},
			"poolSize":      4,
			"poolWinners":   2,
			"roundRobin":    false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusConflict, w.Code,
			"changing teamMatchType while draw-ready must return 409: %s", w.Body.String())

		// TeamMatchType must remain "fixed"; draw-ready gate must not mutate state.
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeFixed, stored.TeamMatchType,
			"teamMatchType must not change after 409 rejection")
		assert.Equal(t, state.CompStatusDrawReady, stored.Status,
			"status must remain draw-ready after 409 rejection")
	})

	t.Run("ALLOW teamMatchType unchanged while draw-ready", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":            cid,
			"name":          "Team Match Type Lock",
			"format":        state.CompFormatMixed,
			"kind":          "team",
			"teamSize":      5,
			"teamMatchType": state.TeamMatchTypeFixed, // same as stored, no change
			"courts":        []string{"A"},
			"poolSize":      4,
			"poolWinners":   2,
			"roundRobin":    false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code,
			"no output-affecting change must be allowed while draw-ready: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompStatusDrawReady, stored.Status,
			"status must remain draw-ready after allowed PUT")
	})
}

// TestUpdateCompetition_TeamMatchTypeDrawReadyOmittedAndLegacy verifies that
// the draw-ready teamMatchType guard uses effective-value comparison rather than
// raw wire comparison (Fix 5 regression coverage):
//   - PUT omitting teamMatchType on a draw-ready kachinuki competition must
//     succeed (omitted wire "" means "keep stored", not "change to fixed").
//   - PUT sending "fixed" against a stored legacy "" TeamMatchType on a
//     draw-ready competition must succeed (both represent the same effective
//     type: fixed).
//
// The guard-still-works case (explicit kachinuki against stored fixed returns
// 409) is already covered by TestUpdateCompetition_TeamMatchTypeLockedDrawReady
// and is not duplicated here.
func TestUpdateCompetition_TeamMatchTypeDrawReadyOmittedAndLegacy(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	// Subtest 1: omitting teamMatchType on a draw-ready kachinuki competition
	// must not trigger a 409 (false positive from raw "" != "kachinuki").
	t.Run("ALLOW omitted teamMatchType on draw-ready kachinuki competition", func(t *testing.T) {
		const cid = "draw-ready-kachinuki-omit"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:            cid,
			Name:          "Draw Ready Kachinuki",
			Format:        state.CompFormatMixed,
			Kind:          "team",
			TeamSize:      5,
			TeamMatchType: state.TeamMatchTypeKachinuki,
			Courts:        []string{"A"},
			PoolSize:      4,
			PoolWinners:   2,
			Status:        state.CompStatusDrawReady,
		}))

		// teamMatchType intentionally omitted so the wire value decodes to ""
		// (json omitempty). The effective meaning is "keep the stored value".
		body, _ := json.Marshal(map[string]any{
			"id":          cid,
			"name":        "Draw Ready Kachinuki",
			"format":      state.CompFormatMixed,
			"kind":        "team",
			"teamSize":    5,
			"courts":      []string{"A"},
			"poolSize":    4,
			"poolWinners": 2,
			"roundRobin":  false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code,
			"omitted teamMatchType on a draw-ready kachinuki competition must not return 409: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeKachinuki, stored.TeamMatchType,
			"TeamMatchType must remain kachinuki when the field was omitted from the PUT body")
		assert.Equal(t, state.CompStatusDrawReady, stored.Status,
			"status must remain draw-ready after the allowed PUT")
	})

	// Subtest 2: sending "fixed" against a stored legacy "" TeamMatchType on a
	// draw-ready competition must not 409 (both are the same effective type).
	t.Run("ALLOW explicit fixed against stored legacy empty TeamMatchType while draw-ready", func(t *testing.T) {
		const cid = "draw-ready-legacy-empty"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:            cid,
			Name:          "Draw Ready Legacy Empty",
			Format:        state.CompFormatMixed,
			Kind:          "team",
			TeamSize:      5,
			TeamMatchType: "", // legacy on-disk empty == fixed
			Courts:        []string{"A"},
			PoolSize:      4,
			PoolWinners:   2,
			Status:        state.CompStatusDrawReady,
		}))

		body, _ := json.Marshal(map[string]any{
			"id":            cid,
			"name":          "Draw Ready Legacy Empty",
			"format":        state.CompFormatMixed,
			"kind":          "team",
			"teamSize":      5,
			"teamMatchType": state.TeamMatchTypeFixed, // "fixed" is semantically equal to legacy ""
			"courts":        []string{"A"},
			"poolSize":      4,
			"poolWinners":   2,
			"roundRobin":    false,
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code,
			"explicit fixed against stored legacy empty must not return 409 while draw-ready: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeFixed, stored.TeamMatchType,
			"TeamMatchType must be persisted as fixed after a successful PUT")
		assert.Equal(t, state.CompStatusDrawReady, stored.Status,
			"status must remain draw-ready after the allowed PUT")
	})
}

// TestUpdateCompetition_TeamMatchTypeLockedWhenStarted verifies that changing
// teamMatchType on a STARTED competition (status past setup, e.g. knockout)
// returns 409. Flipping fixed <-> kachinuki mid-tournament would desync the
// recorded bout structure from the scoring/advancement paradigm. Sibling of
// the Naginata/Engi started-guards; the draw-ready lock alone left started
// comps editable (UAT finding).
func TestUpdateCompetition_TeamMatchTypeLockedWhenStarted(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "team-match-type-started-lock"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            cid,
		Name:          "Team Match Type Started Lock",
		Format:        state.CompFormatMixed,
		Kind:          "team",
		TeamSize:      5,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		Courts:        []string{"A"},
		PoolSize:      4,
		PoolWinners:   2,
		Status:        state.CompStatusKnockout,
	}))

	basePayload := func(tmt state.TeamMatchType) map[string]any {
		return map[string]any{
			"id":            cid,
			"name":          "Team Match Type Started Lock",
			"format":        state.CompFormatMixed,
			"kind":          "team",
			"teamSize":      5,
			"teamMatchType": tmt,
			"courts":        []string{"A"},
			"poolSize":      4,
			"poolWinners":   2,
			"roundRobin":    false,
		}
	}
	put := func(payload map[string]any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("REJECT kachinuki to fixed flip on a started comp", func(t *testing.T) {
		w := put(basePayload(state.TeamMatchTypeFixed))
		assert.Equal(t, http.StatusConflict, w.Code,
			"changing teamMatchType on a started comp must return 409: %s", w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeKachinuki, stored.TeamMatchType,
			"teamMatchType must not change after 409 rejection")
	})

	t.Run("ALLOW save with unchanged teamMatchType on a started comp", func(t *testing.T) {
		w := put(basePayload(state.TeamMatchTypeKachinuki))
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())
	})

	t.Run("ALLOW save that omits teamMatchType (keep stored value)", func(t *testing.T) {
		payload := basePayload("")
		delete(payload, "teamMatchType")
		w := put(payload)
		assert.Equal(t, http.StatusOK, w.Code, w.Body.String())

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.TeamMatchTypeKachinuki, stored.TeamMatchType,
			"an omitted teamMatchType must keep the stored value, never reset it")
	})
}

// TestUpdateCompetition_FormatKindLockedWhenStarted verifies bc-symm's
// post-start gate on Format and Kind (mirroring the existing
// Naginata/Engi/TeamMatchType started-guards): once a competition has left
// setup status, current.Format and current.Kind steer live scoring paths
// (comp.Format is read in engine/scoring_tx.go's mixed-pool rescoring gate;
// comp.Kind is read throughout internal/engine's scheduling/scoring/PDF
// paths), so flipping either after results already exist must be refused.
//
// Pre-fix, ONLY the draw-ready 409 protected these two fields; a competition
// that had progressed past draw-ready (pools/knockout/completed) accepted a
// format or kind change with a plain 200.
//
// The third subtest is the regression that matters most: the SPA has never
// had editors for format/kind and echoes the stored values back unchanged on
// every single settings save, so the gate must fire ONLY on an actual
// change, never unconditionally.
func TestUpdateCompetition_FormatKindLockedWhenStarted(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "format-kind-started-lock"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          cid,
		Name:        "Format Kind Started Lock",
		Format:      state.CompFormatMixed,
		Kind:        "individual",
		Courts:      []string{"A"},
		PoolSize:    4,
		PoolWinners: 2,
		Status:      state.CompStatusKnockout, // started
	}))

	basePayload := func(overrides map[string]any) map[string]any {
		p := map[string]any{
			"id":          cid,
			"name":        "Format Kind Started Lock",
			"format":      state.CompFormatMixed,
			"kind":        "individual",
			"courts":      []string{"A"},
			"poolSize":    4,
			"poolWinners": 2,
		}
		for k, v := range overrides {
			p[k] = v
		}
		return p
	}
	put := func(payload map[string]any) *httptest.ResponseRecorder {
		body, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("REJECT format change on a started comp", func(t *testing.T) {
		w := put(basePayload(map[string]any{"format": state.CompFormatLeague}))
		assert.Equal(t, http.StatusConflict, w.Code,
			"changing format on a started comp must return 409: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, state.CompFormatMixed, stored.Format, "format must not change after 409 rejection")
	})

	t.Run("REJECT kind change on a started comp", func(t *testing.T) {
		w := put(basePayload(map[string]any{"kind": "team", "teamSize": 5}))
		assert.Equal(t, http.StatusConflict, w.Code,
			"changing kind on a started comp must return 409: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, "individual", stored.Kind, "kind must not change after 409 rejection")
	})

	t.Run("ALLOW save that echoes unchanged format/kind on a started comp", func(t *testing.T) {
		w := put(basePayload(nil))
		assert.Equal(t, http.StatusOK, w.Code,
			"a save that does not change format/kind must succeed on a started comp: %s", w.Body.String())
	})
}

// TestUpdateCompetition_KachinukiZeroTeamSizeCannotPersist verifies bc-symm's
// fix for a settings PUT that flips a team-kachinuki competition to
// individual (teamSize 0) while omitting teamMatchType. Pre-fix,
// state.ValidateTeamMatchType ran on the WIRE teamMatchType ("" passes
// vacuously for any teamSize) BEFORE the "inherit stored teamMatchType when
// omitted" step, so the write passed validation, then inherited the stored
// "kachinuki" and persisted it alongside teamSize 0 -- exactly the pair
// ValidateTeamMatchType exists to forbid (inert at runtime, since
// IsKachinuki() requires teamSize >= 2, but every LATER settings PUT would
// echo that same pair back on the wire and now fail the WIRE-value check,
// locking the operator out of the settings screen entirely).
//
// The re-check COERCES rather than rejects, which is what this asserts. The
// value it judges is one the write INHERITED, never one a client sent (a
// sent value has already met the wire check in the same branch), so a 400
// would answer the operator's PUT with an error naming a field their
// request never mentioned. Resetting to the fixed default lands exactly
// the record the operator asked for -- an individual competition -- and
// leaves nothing illegal on disk. See the sibling test below for the case
// where rejecting would have been an outright lockout.
func TestUpdateCompetition_KachinukiZeroTeamSizeCannotPersist(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "kachinuki-zero-lockout"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            cid,
		Name:          "Kachinuki Zero Lockout",
		Format:        state.CompFormatMixed,
		Kind:          "team",
		TeamSize:      5,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		Courts:        []string{"A"},
		PoolSize:      4,
		PoolWinners:   2,
		Status:        state.CompStatusSetup,
	}))

	// Flip kind to individual and zero teamSize, omitting teamMatchType --
	// exactly what a kind editor does once it stops presenting the
	// team-only controls.
	body, _ := json.Marshal(map[string]any{
		"id":          cid,
		"name":        "Kachinuki Zero Lockout",
		"format":      state.CompFormatMixed,
		"kind":        "individual",
		"teamSize":    0,
		"courts":      []string{"A"},
		"poolSize":    4,
		"poolWinners": 2,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"the operator asked for an individual competition and may have it; the inherited kachinuki is the server's problem to resolve, not theirs: %s", w.Body.String())

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, state.TeamMatchTypeFixed, stored.TeamMatchType,
		"the pair (kachinuki, teamSize 0) is what ValidateTeamMatchType forbids; it must not reach disk by way of the inherit")
	assert.Equal(t, 0, stored.TeamSize)
	assert.Equal(t, "individual", stored.Kind)

	require.NoError(t, state.ValidateTeamMatchType(stored.TeamMatchType, stored.TeamSize),
		"whatever the route, the record left behind must satisfy the rule the re-check enforces")
}

// The lockout the coercion above exists to prevent, driven from the state a
// PRE-FIX server could leave on disk: (teamMatchType kachinuki, teamSize 0).
// Rejecting an inherited value there is not a one-off 400 -- it is
// permanent. Every later settings PUT from a client that omits
// teamMatchType (a non-SPA API caller, or a browser tab cached from before
// the field shipped) inherits the same stored pair and fails the same way,
// over a field the request never mentions, with nothing on the settings
// screen able to repair it.
//
// CLAUDE.md states the rule this pins: a write answers for what it
// introduces, not for what it inherited. Same shape as stripInvalidHantei
// (engine), which strips an inherited mark and logs rather than
// blaming the operator for state already on disk.
func TestUpdateCompetition_InheritedIllegalTeamMatchTypeIsRepairedNotRejected(t *testing.T) {
	r, store, _, _, _ := setupTestRouter(t)

	const cid = "kachinuki-zero-stored"
	// SaveCompetition writes straight through, so this is the record a
	// pre-fix PUT would have produced -- not a shape any handler accepts
	// today.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            cid,
		Name:          "Stored Illegal Pair",
		Format:        state.CompFormatMixed,
		Kind:          "individual",
		TeamSize:      0,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		Courts:        []string{"A"},
		PoolSize:      4,
		PoolWinners:   2,
		Status:        state.CompStatusSetup,
	}))

	// A rename. Nothing about teams, nothing about match format.
	body, _ := json.Marshal(map[string]any{
		"id":          cid,
		"name":        "Renamed",
		"format":      state.CompFormatMixed,
		"kind":        "individual",
		"teamSize":    0,
		"courts":      []string{"A"},
		"poolSize":    4,
		"poolWinners": 2,
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code,
		"a rename must not 400 over a stored value the request never mentioned: %s", w.Body.String())

	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Equal(t, "Renamed", stored.Name, "the edit the operator actually asked for must land")
	assert.Equal(t, state.TeamMatchTypeFixed, stored.TeamMatchType,
		"and the write repairs what it inherited on its way past, so the NEXT save is an ordinary one")
}

// TestCreateCompetition_RejectsIllegalKind pins the POST-side half of
// state.ValidateCompetitionKind. POST authors a brand-new record, so unlike
// the PUT guard (change-scoped, see TestUpdateCompetition_KindGuard below)
// nothing here is inherited and the check runs unconditionally: a
// hand-crafted "banana" kind must never reach disk, since Kind == "team" is
// the marker engine code uses to route team-vs-individual scoring/generation
// (ValidateCompetitionTeamSize's doc comment names the same split) and an
// unrecognised value silently fell through every one of those checks and ran
// as individual with no error anywhere.
func TestCreateCompetition_RejectsIllegalKind(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("REJECT an unrecognised kind", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":   "create-illegal-kind",
			"name": "Create Illegal Kind",
			"kind": "banana",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code,
			"an unrecognised kind must be rejected on create: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "unknown kind")

		stored, err := store.LoadCompetition("create-illegal-kind")
		require.NoError(t, err)
		assert.Nil(t, stored, "a rejected create must persist nothing")
	})

	t.Run("ALLOW empty kind (legacy/import default meaning individual)", func(t *testing.T) {
		body, _ := json.Marshal(map[string]any{
			"id":   "create-empty-kind",
			"name": "Create Empty Kind",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
		stored, err := store.LoadCompetition("create-empty-kind")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "", stored.Kind)
	})
}

// TestUpdateCompetition_KindGuard is the CRITICAL SCOPING pair for
// state.ValidateCompetitionKind on PUT: reject an illegal kind ONLY when the
// value actually CHANGES, never when an already-illegal stored value is
// merely echoed back.
//
// The lockout sub-test is the important one. The settings page always
// round-trips the stored kind on every save (it has no reason to touch a
// field it never renders an editor for, and even once it does, an unrelated
// save still echoes the CURRENT value back). A competition whose config.md
// was hand-edited (or written before this guard existed) to carry an
// unrecognised kind must stay editable for every unrelated field, or an
// unconditional check here would 400 every future save and permanently lock
// the operator out of the settings screen -- the same lockout shape
// CLAUDE.md's "a write answers for what it introduces, not for what it
// inherited" rule exists to prevent, and one this codebase has already had
// to fix twice.
func TestUpdateCompetition_KindGuard(t *testing.T) {
	t.Run("REJECT a change to an illegal kind", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		const cid = "kind-guard-change"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:     cid,
			Name:   "Kind Guard Change",
			Kind:   "individual",
			Status: state.CompStatusSetup,
		}))

		body, _ := json.Marshal(map[string]any{
			"id":   cid,
			"name": "Kind Guard Change",
			"kind": "banana",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusBadRequest, w.Code,
			"changing to an unrecognised kind must be rejected: %s", w.Body.String())
		assert.Contains(t, w.Body.String(), "unknown kind")

		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		assert.Equal(t, "individual", stored.Kind, "the rejected write must not land")
	})

	t.Run("ALLOW a save that echoes an ALREADY-illegal stored kind unchanged (lockout regression)", func(t *testing.T) {
		r, store, _, _, tempDir := setupTestRouter(t)
		defer os.RemoveAll(tempDir)

		const cid = "kind-guard-lockout"
		// Simulates a hand-edited config.md, or a record written before this
		// guard existed: SaveCompetition writes straight to disk, bypassing
		// the handler's own validation, exactly the way a pre-existing
		// on-disk illegal value would arise in production.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:        cid,
			Name:      "Kind Guard Lockout",
			Kind:      "banana",
			Date:      "12-05-2026",
			StartTime: "09:00",
			Status:    state.CompStatusSetup,
		}))

		// The settings page echoes the stored (illegal) kind back verbatim
		// while editing an entirely unrelated field (StartTime here).
		body, _ := json.Marshal(map[string]any{
			"id":        cid,
			"name":      "Kind Guard Lockout",
			"kind":      "banana",
			"date":      "12-05-2026",
			"startTime": "10:30",
		})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code,
			"a save that does not change an already-illegal kind must succeed: %s", w.Body.String())
		stored, err := store.LoadCompetition(cid)
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "banana", stored.Kind, "the illegal kind is preserved, not silently corrected")
		assert.Equal(t, "10:30", stored.StartTime, "the unrelated edit must land")
	})
}
