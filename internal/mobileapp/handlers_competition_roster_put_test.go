package mobileapp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPUTCompetition_RosterPUTBypassesSettingsValidation pins the
// Copilot round-12 finding (#4): settings-specific validators
// (validateDateDMY, validateCompetitionCourts, empty-name check) used
// to run BEFORE the transform's branch decision, so a roster-only PUT
// from AdminParticipants (`{ ...c, players: np }` spread) carrying a
// stale settings field would fail with "date must be DD-MM-YYYY" even
// though the field was about to be ignored by the transform. Now those
// validators only run when comp.Players == nil (settings-only PUT).
func TestPUTCompetition_RosterPUTBypassesSettingsValidation(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Seed a competition with a NON-DMY date (simulating legacy state
	// from before the canonical-format cleanup landed). Direct
	// SaveCompetition bypasses the handler's validation so we can plant
	// the legacy shape.
	cid := "legacy-date-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:   cid,
		Name: "Legacy",
		Date: "2026-05-12", // ISO format, would fail validateDateDMY
	}))

	// Roster-only PUT, AdminParticipants spreads `{ ...c, players: np }`
	// where c.date is the on-disk legacy ISO date.
	body, _ := json.Marshal(state.Competition{
		ID:   cid,
		Name: "Legacy",
		Date: "2026-05-12", // stale ISO from c.date
		Players: []domain.Player{
			{ID: "p1-uuid", Name: "P1", Dojo: "D1"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code,
		"roster-only PUT must succeed even with non-DMY date in body: %s", w.Body.String())

	// Verify the roster landed and the legacy date is preserved on disk
	// (the PUT didn't touch the settings field, that's the whole point).
	parts, _ := store.LoadParticipants(cid, false)
	assert.Len(t, parts, 1, "roster must have landed")
	stored, _ := store.LoadCompetition(cid)
	assert.Equal(t, "2026-05-12", stored.Date, "legacy date untouched by roster PUT")
}

func TestPUTCompetition_SettingsOnlyResponseIncludesPlayers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "with-roster"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:                cid,
		Name:              "With Roster",
		Date:              "12-05-2026",
		HasParticipantIDs: true,
	}))
	// Use real UUID v4 IDs so the auto-detect / hinted loader recognises
	// the format and Names parse correctly on LoadParticipants.
	require.NoError(t, store.SaveParticipants(cid, []domain.Player{
		{ID: "11111111-1111-4111-8111-111111111111", Name: "Alice", Dojo: "Dojo X"},
		{ID: "22222222-2222-4222-8222-222222222222", Name: "Bob", Dojo: "Dojo Y"},
	}))

	// Settings-only PUT, body OMITS players. Just renaming.
	body := []byte(`{"id":"with-roster","name":"With Roster Renamed","date":"12-05-2026"}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	// Parse the response, the Players field must be a non-null array
	// reflecting the on-disk roster.
	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.NotNil(t, resp.Players, "Players must NOT be null in settings-only PUT response")
	assert.Len(t, resp.Players, 2, "Players must contain the on-disk roster")
	assert.Equal(t, "Alice", resp.Players[0].Name)
	assert.Equal(t, "Bob", resp.Players[1].Name)

	// Also verify the response body's JSON literally has a players array,
	// not the string "null", Go's nil slice serializes to "null", which
	// is the bug shape we're guarding against.
	assert.NotContains(t, w.Body.String(), `"players":null`,
		"response must not ship `players: null`, clients merge this into local state")
}

// mp-p7n: Copilot PR #185 round-3 finding, regenerating ids on save
// would orphan CompetitorStatus.PlayerID / team-lineup PlayerIDs that
// reference the original ids. Fix: keep the id verbatim on save; the loader
// (consulting Competition.HasParticipantIDs for the strip decision) handles
// any shape.
//
// This test pins the contract:
//   - The PUT body's non-UUID ids round-trip to the response intact
//     (no regeneration).
//   - Name/Dojo are correctly aligned in the response (no column shift
//     on load, the loader trusts HasParticipantIDs).
//   - A second PUT with the same body produces an idempotent round-trip
//     (ids don't churn).
//
// TestPUTCompetition_RosterPUT_NearDupWarningsAndTier1 pins the
// server-authoritative duplicate handling on the PRIMARY roster-import path
// (PUT /competitions/:id): a perfect (name,dojo) duplicate is rejected 409,
// and a near-duplicate pair is surfaced as non-blocking `warnings` in the
// response. (mp-ljry, Copilot round 2)
func TestPUTCompetition_RosterPUT_NearDupWarningsAndTier1(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "comp-ndw"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "NDW", Date: "12-05-2026",
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	// Perfect (name,dojo) duplicate → 409.
	dup := []byte(`{"id":"comp-ndw","name":"NDW","date":"12-05-2026","format":"knockout","kind":"individual","courts":["A"],
		"players":[{"name":"John Smith","dojo":"Wakaba"},{"name":"john  smith","dojo":"wakaba"}]}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(dup))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusConflict, w.Code, "perfect (name,dojo) dup on roster PUT must 409: %s", w.Body.String())

	// Near-duplicate (token-subset) → 200 with a warnings entry.
	near := []byte(`{"id":"comp-ndw","name":"NDW","date":"12-05-2026","format":"knockout","kind":"individual","courts":["A"],
		"players":[{"name":"Ana Maria Rossi","dojo":"Tora"},{"name":"Ana Rossi","dojo":"Wakaba"}]}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(near))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "near-dup roster PUT must succeed: %s", w.Body.String())

	var resp struct {
		Warnings []helper.NearDupWarning `json:"warnings"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Warnings, 1, "near-dup pair must produce exactly one warning")
	assert.Equal(t, "token-subset", resp.Warnings[0].Score)

	// Clean roster → warnings present as an empty array (consistent shape).
	clean := []byte(`{"id":"comp-ndw","name":"NDW","date":"12-05-2026","format":"knockout","kind":"individual","courts":["A"],
		"players":[{"name":"Shudokan A","dojo":"X"},{"name":"Shudokan B","dojo":"Y"}]}`)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(clean))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"warnings":[]`, "clean roster must serialize warnings as [] not null")
}

func TestPUTCompetition_RosterPUTPreservesIDsAndAlignsColumns(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "asddasd"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Asddasd", Date: "12-05-2026",
		WithZekkenName: false, HasParticipantIDs: true,
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	body := []byte(`{
		"id":"asddasd","name":"Asddasd","date":"12-05-2026",
		"format":"knockout","kind":"individual","courts":["A"],
		"withZekkenName":false,
		"players":[
			{"id":"asddasd-p1","name":"Aaron Adams","dojo":"Team Alpha"},
			{"id":"asddasd-p2","name":"Albus Blake","dojo":"Team Delta"}
		]
	}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Players, 2)

	// Ids preserved, regeneration would orphan dependent stores.
	assert.Equal(t, "asddasd-p1", resp.Players[0].ID,
		"non-UUID id must round-trip intact; regeneration orphans competitor_status refs")
	assert.Equal(t, "asddasd-p2", resp.Players[1].ID)

	// Name and Dojo correctly aligned, no column shift on load.
	assert.Equal(t, "Aaron Adams", resp.Players[0].Name)
	assert.Equal(t, "Team Alpha", resp.Players[0].Dojo)
	assert.Empty(t, resp.Players[0].Metadata,
		"Metadata must be empty, pre-fix column shift dumped Dojo into Metadata[0]")
	assert.Equal(t, "Albus Blake", resp.Players[1].Name)
	assert.Equal(t, "Team Delta", resp.Players[1].Dojo)

	// Idempotent second PUT, ids stay stable.
	body2 := fmt.Appendf(nil, `{
		"id":"asddasd","name":"Asddasd","date":"12-05-2026",
		"format":"knockout","kind":"individual","courts":["A"],
		"withZekkenName":false,
		"players":[
			{"id":%q,"name":"Aaron Adams","dojo":"Team Alpha"},
			{"id":%q,"name":"Albus Blake","dojo":"Team Delta"}
		]
	}`, resp.Players[0].ID, resp.Players[1].ID)
	w2 := httptest.NewRecorder()
	req2, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body2))
	req2.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w2, req2)
	require.Equal(t, http.StatusOK, w2.Code)
	var resp2 state.Competition
	require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))
	assert.Equal(t, "asddasd-p1", resp2.Players[0].ID, "ids must be stable across save round-trips")
}

// mp-p7n: uppercase-UUID id round-trips intact (no canonicalisation,
// no regeneration). The loader's HasParticipantIDs-based strip handles
// any shape, case isn't load-bearing for the id-strip decision once
// the flag is consulted.
func TestPUTCompetition_RosterPUTPreservesUppercaseUUID(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "upper-uuid"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Upper UUID", Date: "12-05-2026",
		WithZekkenName: false, HasParticipantIDs: true,
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	upperUUID := "85CDEB35-C066-4667-B7FD-43EBAE8A9F13"
	body := fmt.Appendf(nil, `{
		"id":"upper-uuid","name":"Upper UUID","date":"12-05-2026",
		"format":"knockout","kind":"individual","courts":["A"],
		"withZekkenName":false,
		"players":[{"id":%q,"name":"Aaron Adams","dojo":"Team Alpha"}]
	}`, upperUUID)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Players, 1)
	assert.Equal(t, upperUUID, resp.Players[0].ID,
		"client-supplied id must round-trip intact (case preserved, no regeneration)")
	assert.Equal(t, "Aaron Adams", resp.Players[0].Name)
	assert.Equal(t, "Team Alpha", resp.Players[0].Dojo)
}

// mp-p7n / Copilot PR #185 round-9: exercises the round-6
// 500-on-flag-flip-failure branch end-to-end, deterministically.
//
// Round-8 used a watcher-goroutine filesystem race to inject the
// failure between SaveParticipants and the flip; Copilot flagged that
// as nondeterministic (if the watcher lost the race it could observe
// participants.csv after the handler already returned 200, then assert
// 500). Replaced with a package-level test seam: flipHasParticipantIDs
// is a var, so the test swaps in a stub that returns an error with no
// timing dependency.
func TestPUTCompetition_RosterPUT_FlagFlipFailureReturns500(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	// Inject a deterministic flip failure; restore on exit. These
	// handler tests do not run in parallel, so mutating the package
	// var is safe.
	orig := flipHasParticipantIDs
	flipHasParticipantIDs = func(_ *state.Store, _ string) error {
		return fmt.Errorf("injected flag-flip failure")
	}
	defer func() { flipHasParticipantIDs = orig }()

	cid := "flip-fails"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Flip Fails", Date: "12-05-2026",
		HasParticipantIDs: false,
		Format:            state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	body, _ := json.Marshal(state.Competition{
		ID: cid, Name: "Flip Fails", Date: "12-05-2026",
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
		Players: []domain.Player{
			{ID: "flip-fails-p1", Name: "Aaron Adams", Dojo: "Team Alpha"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	// Round-6 contract: a flip failure surfaces as 500, NOT a 200 with
	// a stale flag.
	require.Equal(t, http.StatusInternalServerError, w.Code,
		"flip failure must surface as 500 (round-6 contract), got %d: %s", w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "HasParticipantIDs",
		"error body must mention HasParticipantIDs (round-6 flip-failure branch); got %s", w.Body.String())

	// The roster file itself DID land before the flip, confirm the
	// participants were saved (the failure is only in the metadata flip,
	// which is why the operator's retry is idempotent). Read with an
	// explicit HasIDs hint: a no-hint read would reproduce the column
	// shift precisely BECAUSE the flag flip failed (HasParticipantIDs is
	// still false on disk, so the loader auto-detects "no ids" for the
	// non-UUID first column). The hinted read proves the on-disk bytes
	// are correct and a successful retry (which lands the flip) recovers.
	trueP := true
	saved, lerr := store.LoadParticipantsOpt(cid, false, state.LoadParticipantsOpts{HasIDs: &trueP})
	require.NoError(t, lerr)
	require.Len(t, saved, 1)
	assert.Equal(t, "Aaron Adams", saved[0].Name)
	assert.Equal(t, "Team Alpha", saved[0].Dojo)
	assert.Equal(t, "flip-fails-p1", saved[0].ID)
}

// mp-p7n / Copilot PR #185 round-5: when the roster-PUT response
// re-loads participants, the deferred HasParticipantIDs=true flip is
// best-effort (failures only log). If the flip never lands, or simply
// hasn't landed yet, the loader's default branch reads the stale flag
// (false) and falls back to uuidRE-on-row-0, mis-classifying non-UUID
// ids as "no ids" and returning the column-shifted roster.
//
// Simulate the failure mode by forcing HasParticipantIDs=false on
// disk BEFORE the PUT, then asserting the response still surfaces
// correctly-aligned Name/Dojo. The handler now passes HasIDs=&true
// explicitly when re-loading a non-empty roster, so the loader trusts
// the call site (we just saved a non-empty roster, every row has an
// id) regardless of the metadata flag's state.
func TestPUTCompetition_RosterPUTResponseHardenedAgainstStaleFlag(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "stale-flag"
	// Note: HasParticipantIDs explicitly FALSE, simulating either the
	// pre-flip window (race) or a failed deferred flip (logged but not
	// surfaced). The reload must not trust this flag.
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Stale Flag", Date: "12-05-2026",
		WithZekkenName: false, HasParticipantIDs: false,
		Format: state.CompFormatKnockout, Kind: "individual", Courts: []string{"A"},
	}))

	body := []byte(`{
		"id":"stale-flag","name":"Stale Flag","date":"12-05-2026",
		"format":"knockout","kind":"individual","courts":["A"],
		"withZekkenName":false,
		"players":[
			{"id":"stale-flag-p1","name":"Aaron Adams","dojo":"Team Alpha"},
			{"id":"stale-flag-p2","name":"Albus Blake","dojo":"Team Delta"}
		]
	}`)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equalf(t, http.StatusOK, w.Code, "response: %s", w.Body.String())

	var resp state.Competition
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Players, 2)

	// Response MUST surface correctly-aligned Name/Dojo even if the
	// deferred HasParticipantIDs flip failed. Pre-fix, the reload
	// would have returned Name="Stale-Flag-P1", Dojo="Aaron Adams",
	// metadata=["Team Alpha"] (column shift).
	assert.Equal(t, "Aaron Adams", resp.Players[0].Name,
		"reload must use HasIDs=&true hint, not the stale comp flag, non-UUID id must be stripped from column 0")
	assert.Equal(t, "Team Alpha", resp.Players[0].Dojo)
	assert.Empty(t, resp.Players[0].Metadata)
	assert.Equal(t, "stale-flag-p1", resp.Players[0].ID,
		"original non-UUID id preserved across the round-trip")
	assert.Equal(t, "Aaron Adams", resp.Players[0].Name)
	assert.Equal(t, "Albus Blake", resp.Players[1].Name)
	assert.Equal(t, "Team Delta", resp.Players[1].Dojo)
}

// TestPUTCompetition_DefersHasParticipantIDsOnSaveFailure pins the
// Copilot round-12 finding (#1): the transform used to flip
// HasParticipantIDs=true BEFORE the post-transform SaveParticipants
// call. If SaveParticipants then failed (disk full, EISDIR, etc.) the
// config carried HasParticipantIDs=true while participants.csv
// retained the OLD non-UUID format, the HasIDs-hinted loader would
// then misparse the file. The flag flip is now deferred to AFTER
// SaveParticipants succeeds.
func TestPUTCompetition_DefersHasParticipantIDsOnSaveFailure(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	cid := "save-fails-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: cid, Name: "Save Fails", HasParticipantIDs: false,
	}))

	// Plant a directory where participants.csv should be, the
	// SaveParticipants -> WriteFile call will fail with EISDIR.
	plantedDir := filepath.Join(tempDir, "competitions", cid, "participants.csv")
	require.NoError(t, os.MkdirAll(plantedDir, 0o700))

	body, _ := json.Marshal(state.Competition{
		ID:   cid,
		Name: "Save Fails",
		Players: []domain.Player{
			{ID: "p1-uuid", Name: "P1", Dojo: "D1"},
		},
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+cid, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusInternalServerError, w.Code,
		"save failure must surface as 500 (got %s): %s", w.Code, w.Body.String())

	// HasParticipantIDs must NOT have been flipped to true, the file
	// save failed, so the metadata flag stays in sync with the still-
	// missing file.
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.False(t, stored.HasParticipantIDs,
		"HasParticipantIDs must NOT flip to true when SaveParticipants fails")
}
