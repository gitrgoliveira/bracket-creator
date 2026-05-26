package mobileapp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
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
	// handler enforces "displayName only honored for zekken comps" — without
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

	// Verify player is stored in participants.csv — reload with the same
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

	// Seed the stored name — it must match what LoadSeeds returns after a reload.
	require.NoError(t, store.SaveSeeds(compID, []domain.SeedAssignment{{Name: "Alice Cooper", SeedRank: 1}}))

	// PUT with another non-canonical name — verify the seed is updated to Title-case.
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

	// Reload participants — seed merge must succeed (seed rank != 0).
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

	// 1. POST add duplicate → 409.
	dupAdd, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo X"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(dupAdd))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusConflict, w.Code, "POST add of duplicate name must return 409")

	// 2. PUT replace of Bob renaming to Alice → 409.
	dupReplace, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo B"})
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
	assert.Equal(t, http.StatusConflict, w.Code, "PUT replace renaming Bob→Alice must return 409")

	// 3. PUT renaming Alice to her OWN current name is a no-op rename and must succeed.
	sameName, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo A2"})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("PUT", "/api/competitions/"+compID+"/participants/"+first.ID, bytes.NewBuffer(sameName))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code, "PUT with unchanged name (dojo edit) must succeed")
}

// TestReplaceDoesNotInheritOldDisplayName ensures that replacing a participant
// with displayName:"" (the corrected JS payload) writes a clean 2-column CSV
// row, not a 3-column row that carries the old slot's stale SanitizeName value.
//
// Regression guard for the bug where the frontend was sending
// displayName: replaceTarget.displayName (the old player's auto-derived
// "A. SMITH") as part of the replace payload, causing saveParticipantsNoLock
// to emit "Alice Yamamoto, A. SMITH, Senbukan" instead of
// "Alice Yamamoto, Senbukan".
func TestReplaceDoesNotInheritOldDisplayName(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-replace-dn"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Replace DisplayName Test",
		Status: state.CompStatusSetup,
	}))

	// Add Alice Smith — Go will auto-derive displayName = "A. SMITH".
	addBody, _ := json.Marshal(map[string]interface{}{"name": "Alice Smith", "dojo": "Senbukan"})
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
		"dojo":        "Senbukan",
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
	assert.Equal(t, "Senbukan", players[0].Dojo)
	wantDisplay := helper.SanitizeName("Alice Yamamoto") // "A. YAMAMOTO"
	assert.Equal(t, wantDisplay, players[0].DisplayName,
		"displayName must be derived from the new name, not inherited from the old slot")
	assert.NotEqual(t, "A. SMITH", players[0].DisplayName,
		"stale A. SMITH from replaced slot must not carry over")
}

// TestBulkCheckIn exercises the POST /competitions/:id/participants/check-in-bulk endpoint.
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
		body, _ := json.Marshal(map[string]interface{}{"participant_ids": pids})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants/check-in-bulk", bytes.NewBuffer(body))
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
		body, _ := json.Marshal(map[string]interface{}{"participant_ids": []string{}})
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/competitions/nonexistent/participants/check-in-bulk", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})
}

func mustLoad(t *testing.T, store *state.Store, compID string) []domain.Player {
	t.Helper()
	players, err := store.LoadParticipants(compID, false)
	require.NoError(t, err)
	return players
}

// TestAddParticipant_DefaultsManualTag pins that an add via the single-add
// endpoint without an explicit tag gets "manual" — so rows added via this UI
// land in the same tag-filter bucket as rows the operator hand-edits into
// the paste-box import.
func TestAddParticipant_DefaultsManualTag(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	compID := "comp-manual-tag"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Manual Tag Test", Status: state.CompStatusSetup,
	}))

	body, _ := json.Marshal(map[string]interface{}{"name": "Alice", "dojo": "Dojo A"})
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var added domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &added))
	assert.Equal(t, "manual", added.Tag, "single-add without an explicit tag must default to manual")

	// An explicit tag must be respected (not overwritten by the default).
	body, _ = json.Marshal(map[string]interface{}{"name": "Bob", "dojo": "Dojo B", "tag": "registered"})
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/competitions/"+compID+"/participants", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)

	var bob domain.Player
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &bob))
	assert.Equal(t, "registered", bob.Tag, "explicit tag must override the manual default")
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

	// Add with explicit zekken — must survive a save/load round trip.
	body, _ := json.Marshal(map[string]interface{}{
		"name":        "Akira Tanaka",
		"displayName": "TANAKA",
		"dojo":        "Mumeishi",
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
	assert.Equal(t, "Mumeishi", loaded[0].Dojo)

	// Replace forwarding a new zekken.
	replBody, _ := json.Marshal(map[string]interface{}{
		"name":        "Akira Yamamoto",
		"displayName": "YAMAMOTO",
		"dojo":        "Senbukan",
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
	assert.Equal(t, "Senbukan", loaded[0].Dojo)

	// Replace with empty displayName — the backend must re-derive from the new
	// name (NOT inherit the previous "YAMAMOTO"), matching non-zekken behavior.
	replBody, _ = json.Marshal(map[string]interface{}{
		"name":        "Kenji Sato",
		"displayName": "",
		"dojo":        "Senbukan",
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
// (start-competition won the race) — never a 500 or a data-corrupt 200.
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
		assert.NoError(t, <-startErrCh, "iteration %d: status-flip goroutine must not error", i)
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
			require.NoError(t, err, "iteration %d: must be able to reload participants after 200", i)
			found := false
			for _, p := range reloaded {
				if p.ID == player.ID {
					assert.Equal(t, "Alice Renamed", p.Name,
						"iteration %d: persisted participant name must match request", i)
					found = true
					break
				}
			}
			assert.True(t, found, "iteration %d: replaced participant must still exist in roster", i)
		}
	}
}
