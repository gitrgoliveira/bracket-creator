package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSingleParticipantAddAndReplace(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-test-p"
	// withZekkenName=true so the explicit displayName values in the payload
	// are persisted as the bib/zekken column rather than stripped (the new
	// handler enforces "displayName only honored for zekken comps",  without
	// it, a 3-column row would be mis-parsed on the next load).
	comp := state.Competition{
		ID:             compID,
		Name:           "Test Competition",
		Status:         state.CompStatusSetup,
		WithZekkenName: true,
	}
	err := store.SaveCompetition(&comp)
	require.NoError(t, err)

	// 1. POST single participant (happy path)
	payload := map[string]interface{}{
		"name":        "Test Player",
		"displayName": "T. Player",
		"dojo":        "Test Dojo",
		"danGrade":    "3 Dan",
	}
	bodyBytes, _ := json.Marshal(payload)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var addedPlayer domain.Player
	err = json.Unmarshal(w.Body.Bytes(), &addedPlayer)
	require.NoError(t, err)

	assert.NotEmpty(t, addedPlayer.ID)
	// Bead spec + project convention: minted participant IDs must be UUIDv4
	// so the format-sniffer in loadParticipantsNoLock stays on a single
	// contract. Lock the format here to catch a future "compID-pX" regression.
	assert.True(t, helper.IsUUIDv4(addedPlayer.ID),
		"AddParticipant must mint UUIDv4 IDs, got %q", addedPlayer.ID)
	assert.Equal(t, "Test Player", addedPlayer.Name)
	assert.Equal(t, "T. Player", addedPlayer.DisplayName)
	assert.Equal(t, "Test Dojo", addedPlayer.Dojo)
	assert.Equal(t, []string{"3 Dan"}, addedPlayer.Metadata)

	// Verify player is stored in participants.csv,  reload with the same
	// withZekkenName=true used at save time so the column layout matches.
	storedPlayers, err := store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, storedPlayers, 1)
	assert.Equal(t, addedPlayer.ID, storedPlayers[0].ID)

	// 2. PUT replace participant (happy path)
	replacePayload := map[string]interface{}{
		"name":        "Updated Player Name",
		"displayName": "U. Player",
		"dojo":        "Updated Dojo",
		"danGrade":    "4 Dan",
	}
	replaceBytes, _ := json.Marshal(replacePayload)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+addedPlayer.ID, bytes.NewBuffer(replaceBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var updatedPlayer domain.Player
	err = json.Unmarshal(w.Body.Bytes(), &updatedPlayer)
	require.NoError(t, err)

	assert.Equal(t, addedPlayer.ID, updatedPlayer.ID)
	assert.Equal(t, "Updated Player Name", updatedPlayer.Name)
	assert.Equal(t, "U. Player", updatedPlayer.DisplayName)
	assert.Equal(t, "Updated Dojo", updatedPlayer.Dojo)
	assert.Equal(t, []string{"4 Dan"}, updatedPlayer.Metadata)

	// 3. Test 409 Conflict when started
	startedComp := comp
	startedComp.Status = state.CompStatusPools
	err = store.SaveCompetition(&startedComp)
	require.NoError(t, err)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+addedPlayer.ID, bytes.NewBuffer(replaceBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code)

	// 4. Test 404 when player does not exist (change status back to setup first)
	startedComp.Status = state.CompStatusSetup
	err = store.SaveCompetition(&startedComp)
	require.NoError(t, err)

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/nonexistent-id", bytes.NewBuffer(replaceBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestBlankNameRejected(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-blank-name"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Blank Name Test",
		Status: state.CompStatusSetup,
	}))

	for _, tc := range []struct {
		desc string
		body map[string]interface{}
	}{
		{"whitespace name on add", map[string]interface{}{"name": "   ", "dojo": "Dojo"}},
		{"empty name on add", map[string]interface{}{"name": "", "dojo": "Dojo"}},
		{"whitespace dojo on add", map[string]interface{}{"name": "Alice", "dojo": "   "}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			b, _ := json.Marshal(tc.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(b))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}

	// Add a valid participant to test PUT blank-name rejection.
	added, err := store.AddParticipant(compID, domain.Player{Name: "Alice", Dojo: "Dojo A"}, false)
	require.NoError(t, err)

	for _, tc := range []struct {
		desc string
		body map[string]interface{}
	}{
		{"whitespace name on replace", map[string]interface{}{"name": "   ", "dojo": "Dojo"}},
		{"whitespace dojo on replace", map[string]interface{}{"name": "Bob", "dojo": "   "}},
	} {
		t.Run(tc.desc, func(t *testing.T) {
			b, _ := json.Marshal(tc.body)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+added.ID, bytes.NewBuffer(b))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(w, req)
			assert.Equal(t, http.StatusBadRequest, w.Code)
		})
	}
}

func TestSeedRenamingUnderReplace(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-seed-rename"
	comp := state.Competition{
		ID:     compID,
		Name:   "Seed Rename Test",
		Status: state.CompStatusSetup,
	}
	err := store.SaveCompetition(&comp)
	require.NoError(t, err)

	// 1. Add participant
	player := domain.Player{
		Name: "Alice",
		Dojo: "Original Dojo",
	}
	added, err := store.AddParticipant(compID, player, false)
	require.NoError(t, err)

	// 2. Set seed for Alice
	seeds := []domain.SeedAssignment{
		{Name: "Alice", SeedRank: 1},
	}
	err = store.SaveSeeds(compID, seeds)
	require.NoError(t, err)

	// 3. PUT replace participant renaming Alice -> Alice Cooper
	replacePayload := map[string]interface{}{
		"name": "Alice Cooper",
		"dojo": "Cooper Dojo",
	}
	replaceBytes, _ := json.Marshal(replacePayload)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+added.ID, bytes.NewBuffer(replaceBytes))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	// 4. Verify seed is renamed in seeds.csv
	storedSeeds, err := store.LoadSeeds(compID)
	require.NoError(t, err)
	require.Len(t, storedSeeds, 1)
	assert.Equal(t, "Alice Cooper", storedSeeds[0].Name)
}

// TestNameTitleCaseCanonicalization verifies that names submitted in non-canonical
// casing (e.g. "alice cooper") are stored Title-cased so participants.csv and
// seeds.csv always carry the same form that CreatePlayers produces on load,
// preventing seed-merge mismatches.
func TestNameTitleCaseCanonicalization(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-title-case"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Title Case Test", Status: state.CompStatusSetup,
	}))

	// POST with a lower-cased name.
	body, _ := json.Marshal(map[string]interface{}{"name": "alice cooper", "dojo": "Test Dojo"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var added domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
	assert.Equal(t, "Alice Cooper", added.Name, "AddParticipant must Title-case the stored name")

	// Seed the stored name,  it must match what LoadSeeds returns after a reload.
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{{Name: "Alice Cooper", SeedRank: 1}}))

	// PUT with another non-canonical name,  verify the seed is updated to Title-case.
	replBody, _ := json.Marshal(map[string]interface{}{"name": "bob the builder", "dojo": "Builder Dojo"})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+added.ID, bytes.NewBuffer(replBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var updated domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &updated))
	assert.Equal(t, "Bob The Builder", updated.Name, "UpdateParticipant must Title-case the stored name")

	// Seed must be renamed to the Title-cased form.
	seeds, err := store.LoadSeeds(compID)
	require.NoError(t, err)
	require.Len(t, seeds, 1)
	assert.Equal(t, "Bob The Builder", seeds[0].Name, "seed name must match the Title-cased participant name")

	// Reload participants,  seed merge must succeed (seed rank != 0).
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	require.Len(t, players, 1)
	assert.Equal(t, "Bob The Builder", players[0].Name)
	assert.Equal(t, 1, players[0].Seed, "seed must merge after reload because names are canonical on both sides")
}

// TestDuplicateNameRejection pins the bead acceptance criterion that
// add/replace must reject a name already in the roster with 409. Without
// the guard, name-keyed lookups (seeds, lineups) would silently key on
// whichever row happens to come first in the CSV.
func TestDuplicateNameRejection(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-dup-name"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Dup Name Test",
		Status: state.CompStatusSetup,
	}))

	// Seed two participants directly via the store helper.
	first, err := store.AddParticipant(compID, domain.Player{Name: "Alice", Dojo: "Dojo A"}, false)
	require.NoError(t, err)
	_, err = store.AddParticipant(compID, domain.Player{Name: "Bob", Dojo: "Dojo B"}, false)
	require.NoError(t, err)

	// 1. POST add duplicate,  same (name, dojo) → 409. Different dojo with same
	// name is allowed (two real people at different clubs), so we must use the
	// SAME dojo as Alice to trigger the conflict.
	dupAdd, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo A"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(dupAdd))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code, "POST add of duplicate name+dojo must return 409")

	// 2. PUT replace: move Bob to Alice's dojo AND rename → same (name,dojo) → 409.
	dupReplace, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo A"})
	bobID := ""
	for _, p := range mustLoad(t, store, compID) {
		if p.Name == "Bob" {
			bobID = p.ID
			break
		}
	}
	require.NotEmpty(t, bobID)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+bobID, bytes.NewBuffer(dupReplace))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code, "PUT replace renaming Bob→Alice with same dojo must return 409")

	// 3. PUT renaming Alice to her OWN current name is a no-op rename and must succeed.
	sameName, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo A2"})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+first.ID, bytes.NewBuffer(sameName))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "PUT with unchanged name (dojo edit) must succeed")
}

// TestBatchPostDuplicateNameDojo_409 covers the batch (players array) POST
// path: a perfect (name, dojo) duplicate within one request is rejected with
// 409, while two same-named competitors at different dojos are accepted. This
// complements TestDuplicateNameRejection (single-add) and the state-level
// TestSaveParticipants_RejectsDuplicateNameDojo. (mp-ljry, Copilot round 2)
func TestBatchPostDuplicateNameDojo_409(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-batch-dup"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Batch Dup Test",
		Status: state.CompStatusSetup,
	}))

	// Perfect (name, dojo) duplicate in one batch (case/whitespace variant) → 409.
	dup, _ := json.Marshal(map[string]any{"players": []map[string]string{
		{"name": "Alice Smith", "dojo": "Wakaba"},
		{"name": "alice  smith", "dojo": "wakaba"},
	}})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(dup))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code, "batch POST with duplicate (name,dojo) must return 409")

	// Same name at DIFFERENT dojos in one batch is allowed.
	ok, _ := json.Marshal(map[string]any{"players": []map[string]string{
		{"name": "Alice Smith", "dojo": "Wakaba"},
		{"name": "Alice Smith", "dojo": "Tora"},
	}})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(ok))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "same name at different dojos must be accepted in a batch")
}

// TestBatchPostBlankDojo_400 covers the batch (players array) POST path: a row
// with a blank dojo must be rejected with 400, mirroring the single-add path
// (which already returns "dojo must not be blank"). Without this, a misformatted
// roster paste,  e.g. a two-column "Name, Dojo" line in a zekken competition,
// which parseParticipantLines maps to {displayName: dojo, dojo: ""},  would be
// silently accepted, persisting a competitor with no dojo while the UI reports
// success. (invalid-submission acceptance bug)
func TestBatchPostBlankDojo_400(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-batch-blank-dojo"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Batch Blank Dojo Test",
		Status: state.CompStatusSetup,
	}))

	// A blank dojo on any row must reject the whole batch with 400.
	blank, _ := json.Marshal(map[string]any{"players": []map[string]string{
		{"name": "Alice Smith", "dojo": "Wakaba"},
		{"name": "Bob Jones", "dojo": ""},
	}})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(blank))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "batch POST with a blank dojo must return 400")

	// A whitespace-only dojo is equally invalid (trimmed to empty).
	ws, _ := json.Marshal(map[string]any{"players": []map[string]string{
		{"name": "Carol White", "dojo": "   "},
	}})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(ws))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "batch POST with a whitespace-only dojo must return 400")

	// A blank name is likewise rejected.
	blankName, _ := json.Marshal(map[string]any{"players": []map[string]string{
		{"name": "  ", "dojo": "Wakaba"},
	}})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(blankName))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code, "batch POST with a blank name must return 400")

	// Nothing should have been persisted by any of the rejected batches.
	players := mustLoad(t, store, compID)
	assert.Empty(t, players, "no participants should be saved when a batch is rejected")
}

// TestReplaceDoesNotInheritOldDisplayName ensures that replacing a participant
// with displayName:"" (the corrected JS payload) writes a clean 2-column CSV
// row, not a 3-column row that carries the old slot's stale SanitizeName value.
//
// Regression guard for the bug where the frontend was sending
// displayName: replaceTarget.displayName (the old player's auto-derived
// "A. SMITH") as part of the replace payload, causing saveParticipantsNoLock
// to emit "Alice Yamamoto, A. SMITH, Raizan" instead of
// "Alice Yamamoto, Raizan".
func TestReplaceDoesNotInheritOldDisplayName(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-replace-dn"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Replace DisplayName Test",
		Status: state.CompStatusSetup,
	}))

	// Add Alice Smith,  Go will auto-derive displayName = "A. SMITH".
	addBody, _ := json.Marshal(map[string]interface{}{"name": "Alice Smith", "dojo": "Raizan"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(addBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
	var alice domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &alice))

	// Replace Alice Smith with Alice Yamamoto, explicitly clearing displayName
	// to "" as the corrected frontend does.
	replBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Alice Yamamoto",
		"dojo":        "Raizan",
		"displayName": "",
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+alice.ID, bytes.NewBuffer(replBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	// Load and verify: displayName must be the SanitizeName of the NEW name,
	// not the old "A. SMITH" inherited from the replaced slot.
	players := mustLoad(t, store, compID)
	require.Len(t, players, 1)
	assert.Equal(t, "Alice Yamamoto", players[0].Name)
	assert.Equal(t, "Raizan", players[0].Dojo)
	wantDisplay := helper.SanitizeName("Alice Yamamoto") // "A. YAMAMOTO"
	assert.Equal(t, wantDisplay, players[0].DisplayName,
		"displayName must be derived from the new name, not inherited from the old slot")
	assert.NotEqual(t, "A. SMITH", players[0].DisplayName,
		"stale A. SMITH from replaced slot must not carry over")
}

// TestBulkCheckIn exercises the POST /competitions/:id/participants/checkin-bulk endpoint.
func TestBulkCheckIn(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-bulk-checkin"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Bulk Check-in Test", Status: state.CompStatusSetup,
	}))

	// Seed five participants via the store directly.
	names := []string{"Alice", "Bob", "Carol", "Dave", "Eve"}
	added := make([]domain.Player, 0, len(names))
	for _, n := range names {
		p, err := store.AddParticipant(compID, domain.Player{Name: n, Dojo: "Dojo"}, false)
		require.NoError(t, err)
		added = append(added, *p)
	}

	doPost := func(t *testing.T, pids []string) *httptest.ResponseRecorder {
		t.Helper()
		body, _ := json.Marshal(map[string]interface{}{"participantIds": pids})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants/checkin-bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	t.Run("empty array returns zero counts", func(t *testing.T) {
		w := doPost(t, []string{})
		require.Equal(t, http.StatusOK, w.Code)
		var res state.BulkCheckInResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
		assert.Equal(t, 0, res.CheckedIn)
		assert.Equal(t, 0, res.AlreadyCheckedIn)
		assert.Empty(t, res.NotFound)
	})

	t.Run("checks in 3 unchecked participants", func(t *testing.T) {
		pids := []string{added[0].ID, added[1].ID, added[2].ID}
		w := doPost(t, pids)
		require.Equal(t, http.StatusOK, w.Code)
		var res state.BulkCheckInResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
		assert.Equal(t, 3, res.CheckedIn)
		assert.Equal(t, 0, res.AlreadyCheckedIn)
		assert.Empty(t, res.NotFound)

		// Verify persisted state.
		players, err := store.LoadParticipants(compID, false)
		require.NoError(t, err)
		checkedByID := make(map[string]bool, len(players))
		for _, p := range players {
			checkedByID[p.ID] = p.CheckedIn
		}
		assert.True(t, checkedByID[added[0].ID], "Alice must be checked in")
		assert.True(t, checkedByID[added[1].ID], "Bob must be checked in")
		assert.True(t, checkedByID[added[2].ID], "Carol must be checked in")
		assert.False(t, checkedByID[added[3].ID], "Dave must NOT be checked in yet")
	})

	t.Run("already-checked-in participants counted separately", func(t *testing.T) {
		// Alice, Bob, Carol already checked in from previous sub-test; check in Alice again + Dave (new).
		pids := []string{added[0].ID, added[3].ID}
		w := doPost(t, pids)
		require.Equal(t, http.StatusOK, w.Code)
		var res state.BulkCheckInResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
		assert.Equal(t, 1, res.CheckedIn)
		assert.Equal(t, 1, res.AlreadyCheckedIn)
		assert.Empty(t, res.NotFound)
	})

	t.Run("unknown pid appears in not_found", func(t *testing.T) {
		pids := []string{added[4].ID, "00000000-0000-0000-0000-000000000099"}
		w := doPost(t, pids)
		require.Equal(t, http.StatusOK, w.Code)
		var res state.BulkCheckInResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
		assert.Equal(t, 1, res.CheckedIn)
		assert.Equal(t, 0, res.AlreadyCheckedIn)
		assert.Equal(t, []string{"00000000-0000-0000-0000-000000000099"}, res.NotFound)
	})

	t.Run("competition not found returns 404", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"participantIds": []string{}})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/nonexistent/participants/checkin-bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("oversized array returns 400", func(t *testing.T) {
		over := make([]string, MaxBulkCheckInIDs+1)
		for i := range over {
			over[i] = added[0].ID
		}
		w := doPost(t, over)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("oversized individual pid returns 400", func(t *testing.T) {
		w := doPost(t, []string{string(make([]byte, MaxLenEntityID+1))})
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("duplicate pids counted only once", func(t *testing.T) {
		// Eve (added[4]) already checked in from "unknown pid" sub-test.
		// Send her PID twice,  must count as 1 already_checked_in, not 2.
		pids := []string{added[4].ID, added[4].ID}
		w := doPost(t, pids)
		require.Equal(t, http.StatusOK, w.Code)
		var res state.BulkCheckInResult
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
		assert.Equal(t, 0, res.CheckedIn)
		assert.Equal(t, 1, res.AlreadyCheckedIn)
		assert.Empty(t, res.NotFound)
	})

	t.Run("no file write when nothing toggles", func(t *testing.T) {
		csvPath := filepath.Join(tempDir, "competitions", compID, "participants.csv")

		// Snapshot mtime before a no-op call (empty array).
		before, err := os.Stat(csvPath)
		require.NoError(t, err)

		w := doPost(t, []string{})
		require.Equal(t, http.StatusOK, w.Code)

		after, err := os.Stat(csvPath)
		require.NoError(t, err)
		assert.Equal(t, before.ModTime(), after.ModTime(), "participants.csv must not be written for an empty-array call")

		// All participants already checked in,  file must also not be written.
		// At this point Alice, Bob, Carol, Dave, Eve are all checked in.
		allPIDs := make([]string, len(added))
		for i, p := range added {
			allPIDs[i] = p.ID
		}
		before2, err := os.Stat(csvPath)
		require.NoError(t, err)

		w = doPost(t, allPIDs)
		require.Equal(t, http.StatusOK, w.Code)

		after2, err := os.Stat(csvPath)
		require.NoError(t, err)
		assert.Equal(t, before2.ModTime(), after2.ModTime(), "participants.csv must not be written when all participants are already checked-in")
	})
}

// spyBroadcaster counts Broadcast calls for asserting SSE fan-out behaviour.
type spyBroadcaster struct {
	mu    sync.Mutex
	calls int
}

func (s *spyBroadcaster) Broadcast(_ EventType, _ any) {
	s.mu.Lock()
	s.calls++
	s.mu.Unlock()
}

func (s *spyBroadcaster) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// TestBulkCheckIn_BroadcastBehaviour verifies the conditional SSE broadcast:
// exactly one event when at least one participant is toggled, zero events when
// all participants are already checked-in.
func TestBulkCheckIn_BroadcastBehaviour(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "mobileapp-test-broadcast-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)

	spy := &spyBroadcaster{}
	gin.SetMode(gin.TestMode)
	r := gin.New()
	admin := r.Group("/api")
	RegisterParticipantHandlers(admin, store, engine.New(store), spy, NewFileElevatedVerifier(store))

	compID := "comp-broadcast-test"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Broadcast Test", Status: state.CompStatusSetup,
	}))
	p, err := store.AddParticipant(compID, domain.Player{Name: "Alice", Dojo: "Dojo"}, false)
	require.NoError(t, err)

	doPost := func(pids []string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"participantIds": pids})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants/checkin-bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}

	// First call: toggles Alice → exactly 1 broadcast.
	w := doPost([]string{p.ID})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, spy.count(), "one broadcast expected when a participant is newly checked-in")

	// Second call: Alice already checked-in → zero additional broadcasts.
	w = doPost([]string{p.ID})
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, 1, spy.count(), "no additional broadcast expected when participant is already checked-in")
}

func mustLoad(t *testing.T, store *state.Store, compID string) []domain.Player {
	t.Helper()
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	return players
}

// TestAddParticipant_DefaultsManualSource pins that an add via the single-add
// endpoint without an explicit source gets "manual",  so rows added via this UI
// land in the same source-filter bucket as rows the operator hand-edits into
// the paste-box import.
func TestAddParticipant_DefaultsManualSource(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-manual-source"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Manual Source Test", Status: state.CompStatusSetup,
	}))

	body, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo A"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var added domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
	assert.Equal(t, "manual", added.Source, "single-add without an explicit source must default to manual")

	// An explicit source must be respected (not overwritten by the default).
	body, _ = json.Marshal(map[string]interface{}{"name": "Bob", "dojo": "Dojo B", "source": "registered"})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var bob domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &bob))
	assert.Equal(t, "registered", bob.Source, "explicit source must override the manual default")
}

// TestZekkenAddAndReplace covers the previously-missing path for
// withZekkenName=true comps: the operator must be able to set / change the
// zekken via the single-add endpoint and the replace endpoint without
// losing it to auto-derivation.
func TestZekkenAddAndReplace(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-zekken"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:             compID,
		Name:           "Zekken Test",
		Status:         state.CompStatusSetup,
		WithZekkenName: true,
	}))

	// Add with explicit zekken,  must survive a save/load round trip.
	body, _ := json.Marshal(map[string]interface{}{
		"name":        "Akira Tanaka",
		"displayName": "TANAKA",
		"dojo":        "Gyokusen",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var added domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
	assert.Equal(t, "TANAKA", added.DisplayName, "operator-supplied zekken must persist through add")

	loaded, err := store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "TANAKA", loaded[0].DisplayName, "zekken must round-trip through participants.csv")
	assert.Equal(t, "Gyokusen", loaded[0].Dojo)

	// Replace forwarding a new zekken.
	replBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Akira Yamamoto",
		"displayName": "YAMAMOTO",
		"dojo":        "Raizan",
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+added.ID, bytes.NewBuffer(replBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	loaded, err = store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "YAMAMOTO", loaded[0].DisplayName, "operator-supplied zekken must persist through replace")
	assert.Equal(t, "Akira Yamamoto", loaded[0].Name)
	assert.Equal(t, "Raizan", loaded[0].Dojo)

	// Replace with empty displayName,  the backend must re-derive from the new
	// name (NOT inherit the previous "YAMAMOTO"), matching non-zekken behavior.
	replBody, _ = json.Marshal(map[string]interface{}{
		"name":        "Kenji Sato",
		"displayName": "",
		"dojo":        "Raizan",
	})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+added.ID, bytes.NewBuffer(replBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	loaded, err = store.LoadParticipants(compID, true)
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, helper.SanitizeName("Kenji Sato"), loaded[0].DisplayName,
		"empty displayName must be re-derived from the new name, not inherited")
	assert.NotEqual(t, "YAMAMOTO", loaded[0].DisplayName)
}

// TestReplaceParticipant_ConcurrentStartRace verifies that the PUT replace
// handler serialises the status check and the participant write under one
// lock acquire (mp-0lc). A goroutine that concurrently flips status to
// CompStatusPools must produce either a 200 (replace won the race) or a 409
// (start-competition won the race),  never a 500 or a data-corrupt 200.
func TestReplaceParticipant_ConcurrentStartRace(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const compID = "race-replace"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Race", Status: state.CompStatusSetup,
	}))

	addBody, _ := json.Marshal(map[string]interface{}{
		"name": "Alice Smith", "dojo": "Dojo A",
	})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(addBody))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var player domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &player))

	const iterations = 30
	for i := 0; i < iterations; i++ {
		// Reset competition to setup before each iteration.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: compID, Name: "Race", Status: state.CompStatusSetup,
		}))

		var wg sync.WaitGroup
		wg.Add(2)

		var replCode int
		var replBody []byte
		go func() {
			defer wg.Done()
			body, _ := json.Marshal(map[string]interface{}{
				"name": "Alice Renamed", "dojo": "Dojo A",
			})
			rec := httptest.NewRecorder()
			req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+player.ID, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(rec, req)
			replCode = rec.Code
			replBody = rec.Body.Bytes()
		}()

		startErrCh := make(chan error, 1)
		go func() {
			defer wg.Done()
			startErrCh <- store.SaveCompetition(&state.Competition{
				ID: compID, Name: "Race", Status: state.CompStatusPools,
			})
		}()

		wg.Wait()
		assert.NoErrorf(t, <-startErrCh, "iteration %d: status-flip goroutine must not error", i)
		assert.True(t, replCode == http.StatusOK || replCode == http.StatusConflict,
			"iteration %d: replace must return 200 or 409, got %d", i, replCode)

		// When replace succeeded, verify the persisted roster is readable
		// and the returned player name matches what was requested.
		if replCode == http.StatusOK {
			var returned domain.Player
			require.NoError(t, json.Unmarshal(replBody, &returned),
				"iteration %d: 200 body must be a valid Player JSON", i)
			assert.Equal(t, "Alice Renamed", returned.Name,
				"iteration %d: returned player name must match request", i)

			reloaded, err := store.LoadParticipants(compID, false)
			require.NoErrorf(t, err, "iteration %d: must be able to reload participants after 200", i)
			found := false
			for _, p := range reloaded {
				if p.ID == player.ID {
					assert.Equal(t, "Alice Renamed", p.Name,
						"iteration %d: persisted participant name must match request", i)
					found = true
					break
				}
			}
			assert.Truef(t, found, "iteration %d: replaced participant must still exist in roster", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Whitespace-only danGrade must not persist as a blank metadata entry
// Tests both POST (add) and PUT (replace) paths,  mirrors the registration
// handler's analogous test (TestRegistration_POST_WhitespaceDanGrade_NotPersisted).
// ---------------------------------------------------------------------------

func TestParticipants_WhitespaceDanGrade_NotPersisted(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	const compID = "comp-ws-dan-admin"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Whitespace Dan Test",
		Status: state.CompStatusSetup,
	}))

	t.Run("POST add,  whitespace danGrade not persisted", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":     "Alice Ws",
			"dojo":     "Dojo",
			"danGrade": "   ",
		}
		b, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		players, err := store.LoadParticipants(compID, false)
		require.NoError(t, err)
		require.Len(t, players, 1)
		assert.Empty(t, players[0].Metadata, "whitespace-only danGrade must not persist")
	})

	t.Run("PUT replace,  whitespace danGrade not persisted", func(t *testing.T) {
		players, err := store.LoadParticipants(compID, false)
		require.NoError(t, err)
		require.Len(t, players, 1)
		pid := players[0].ID

		payload := map[string]interface{}{
			"name":     "Alice Ws",
			"dojo":     "Dojo",
			"danGrade": "  ",
		}
		b, _ := json.Marshal(payload)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+pid, bytes.NewBuffer(b))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)

		require.Equal(t, http.StatusOK, w.Code)
		updated, err := store.LoadParticipants(compID, false)
		require.NoError(t, err)
		require.Len(t, updated, 1)
		assert.Empty(t, updated[0].Metadata, "whitespace-only danGrade must not persist on replace")
	})
}

// TestPutParticipant_DrawReady_Succeeds verifies that PUT /participants/:pid
// returns 200 (not 409) when the competition is in draw-ready state, and that
// the change cascades through pools.csv.
func TestPutParticipant_DrawReady_Succeeds(t *testing.T) {
	r, store, eng, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-draw-ready-put"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           compID,
		Name:         "Draw-Ready PUT Test",
		Status:       state.CompStatusSetup,
		Format:       state.CompFormatMixed,
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Kind:         "individual",
	}))

	// Add 6 participants so the pool generator has enough players (pool size ≥ 2).
	names := []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"}
	players := make([]domain.Player, len(names))
	for i, n := range names {
		players[i] = domain.Player{Name: n, Dojo: "Dojo" + string(rune('A'+i))}
	}
	require.NoError(t, store.SaveParticipants(compID, players))

	// Find Alice's ID.
	saved, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	aliceID := ""
	for _, p := range saved {
		if p.Name == "Alice" {
			aliceID = p.ID
			break
		}
	}
	require.NotEmpty(t, aliceID, "Alice must have a UUID after save")

	// Generate the draw so we reach draw-ready state.
	require.NoError(t, eng.GenerateDraw(compID))

	// Verify draw-ready.
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	require.Equal(t, state.CompStatusDrawReady, comp.Status)

	// PUT Alice → Alicia while draw is pending.
	payload := map[string]any{"name": "Alicia", "dojo": "DojoA"}
	body, _ := json.Marshal(payload)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+aliceID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equalf(t, http.StatusOK, w.Code, "PUT in draw-ready state must return 200, not 409; body: %s", w.Body.String())

	// Response must include the updated player.
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "Alicia", resp["name"])

	// pools.csv must reflect the new name.
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	aliciaInPools := false
	for _, p := range pools {
		for _, pl := range p.Players {
			if pl.Name == "Alicia" {
				aliciaInPools = true
			}
			assert.NotEqual(t, "Alice", pl.Name, "old name must not remain in pools")
		}
	}
	assert.True(t, aliciaInPools, "Alicia must appear in pools after cascade")
}

// TestPutParticipant_DrawReady_DojoConflictWarning verifies that when a PUT in
// draw-ready state introduces a dojo conflict, the response includes a warnings
// field alongside the player data.
func TestPutParticipant_DrawReady_DojoConflictWarning(t *testing.T) {
	r, store, eng, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-draw-dojo-warn"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:           compID,
		Name:         "Dojo Conflict Warning Test",
		Status:       state.CompStatusSetup,
		Format:       state.CompFormatMixed,
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Kind:         "individual",
	}))

	// 6 players, each from a unique dojo so the generator can place them freely.
	names := []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"}
	players := make([]domain.Player, len(names))
	for i, n := range names {
		players[i] = domain.Player{Name: n, Dojo: "Dojo" + string(rune('A'+i))}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.GenerateDraw(compID))

	// After draw, find a pool-mate of Alice and change their dojo to Alice's
	// dojo. This deterministically creates a conflict regardless of pool layout.
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	var aliceDojo, targetName, targetDojo string
	for _, p := range pools {
		aliceInPool := false
		for _, pl := range p.Players {
			if pl.Name == "Alice" {
				aliceDojo = pl.Dojo
				aliceInPool = true
			}
		}
		if aliceInPool {
			for _, pl := range p.Players {
				if pl.Name != "Alice" {
					targetName = pl.Name
					targetDojo = pl.Dojo
					break
				}
			}
			break
		}
	}
	require.NotEmpty(t, targetName, "must find a pool-mate of Alice")
	require.NotEqual(t, aliceDojo, targetDojo, "pool-mate must have a different dojo")

	saved, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	targetID := ""
	for _, p := range saved {
		if p.Name == targetName {
			targetID = p.ID
			break
		}
	}
	require.NotEmpty(t, targetID)

	// Replace the pool-mate with a new participant from Alice's dojo.
	payload := map[string]any{"name": "Grace", "dojo": aliceDojo}
	body, _ := json.Marshal(payload)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+targetID, bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equalf(t, http.StatusOK, w.Code, "PUT must succeed even with dojo conflict; body: %s", w.Body.String())

	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// Dojo conflict warning MUST be present,  we deterministically created one.
	ws, ok := resp["warnings"]
	require.True(t, ok, "warnings must be present when dojo conflict exists")
	wsSlice, ok := ws.([]any)
	require.True(t, ok, "warnings must be a JSON array")
	require.NotEmpty(t, wsSlice, "at least one dojo conflict warning expected")
	assert.Contains(t, wsSlice[0], "dojo conflict")

	// Grace must be in pools.
	poolsAfter, err := store.LoadPools(compID)
	require.NoError(t, err)
	graceFound := false
	for _, p := range poolsAfter {
		for _, pl := range p.Players {
			if pl.Name == "Grace" {
				graceFound = true
			}
		}
	}
	assert.True(t, graceFound, "Grace must appear in pools after cascade")
}
