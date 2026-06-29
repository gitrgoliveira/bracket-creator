package mobileapp

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegisterImportHandlers(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	t.Run("Import Successful", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Add manifest.yaml
		manifestPart, err := writer.CreateFormFile("files", "manifest.yaml")
		require.NoError(t, err)
		manifestPart.Write([]byte(`
competitions:
  - id: "comp-1"
    name: "Competition 1"
    kind: "individual"
    format: "mixed"
    courts: ["A", "B"]
    participants: "players.csv"
    seeds: "seeds.csv"
`))

		// Add players.csv
		playersPart, err := writer.CreateFormFile("files", "players.csv")
		require.NoError(t, err)
		playersPart.Write([]byte(`Player 1,Dojo A
Player 2,Dojo B
`))

		// Add seeds.csv
		seedsPart, err := writer.CreateFormFile("files", "seeds.csv")
		require.NoError(t, err)
		seedsPart.Write([]byte(`Rank,Name
1,Player 1
2,Player 2
`))

		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)

		var resp map[string][]ImportResult
		err = json.Unmarshal(w.Body.Bytes(), &resp)
		require.NoError(t, err)
		require.Len(t, resp["results"], 1)
		assert.Equal(t, "comp-1", resp["results"][0].ID)
		assert.Equal(t, 2, resp["results"][0].ParticipantCount)
		assert.Equal(t, 2, resp["results"][0].SeedCount)
		assert.Empty(t, resp["results"][0].Error)
	})

	t.Run("Import with Base Name Matching", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)

		// Use nested paths to test baseName matching
		manifestPart, err := writer.CreateFormFile("files", "folder/manifest.yaml")
		require.NoError(t, err)
		manifestPart.Write([]byte(`
competitions:
  - id: "comp-2"
    name: "Competition 2"
    participants: "data/players.csv"
`))

		playersPart, err := writer.CreateFormFile("files", "data/players.csv")
		require.NoError(t, err)
		playersPart.Write([]byte(`Player 3,Dojo C`))

		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string][]ImportResult
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, 1, resp["results"][0].ParticipantCount)
	})

	t.Run("Missing Manifest", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		writer.CreateFormFile("files", "something.txt")
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "no manifest.yaml or manifest.json found")
	})

	t.Run("Invalid Manifest YAML", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`: invalid yaml`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "cannot parse manifest.yaml")
	})

	t.Run("Empty Manifest", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`competitions: []`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusBadRequest, w.Code)
		assert.Contains(t, w.Body.String(), "manifest defines no competitions")
	})

	t.Run("Import Error - Invalid ID", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "INVALID ID!!!"
    name: "Bad ID"
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code) // The handler returns 200 even if individual imports fail
		var resp map[string][]ImportResult
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.NotEmpty(t, resp["results"][0].Error)
	})

	t.Run("Import Error - Missing Participants File", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "comp-missing"
    name: "Missing File"
    participants: "missing.csv"
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		var resp map[string][]ImportResult
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp["results"][0].Error, "not found in upload")
	})

	t.Run("Import Error - Missing ID", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - name: "Missing ID"
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		var resp map[string][]ImportResult
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Equal(t, "missing id", resp["results"][0].Error)
	})

	t.Run("Import Error - Participants Parse Error", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "comp-bad-part"
    name: "Bad Participants"
    participants: "bad.csv"
`))
		playersPart, _ := writer.CreateFormFile("files", "bad.csv")
		// Duplicate names trigger a validation error in helper.CreatePlayers
		playersPart.Write([]byte("Player A,Dojo A\nPlayer A,Dojo A"))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		var resp map[string][]ImportResult
		json.Unmarshal(w.Body.Bytes(), &resp)
		assert.Contains(t, resp["results"][0].Error, "parse participants")
	})

	// Pre-fix, the seeds block silently swallowed three shapes:
	// (1) missing seeds file, (2) parseSeedsBytes returning err != nil
	// (currently unreachable but dead branch), and (3) empty parse.
	// Only (3) is now soft, the other two surface as per-row res.Error,
	// matching the participants block's pattern. The user no longer sees
	// SeedCount=0 with no error message when they named a file that
	// wasn't in the upload.
	t.Run("Import Error - Missing Seeds File", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "comp-missing-seeds"
    name: "Missing Seeds"
    participants: "players-only.csv"
    seeds: "absent-seeds.csv"
`))
		playersPart, _ := writer.CreateFormFile("files", "players-only.csv")
		playersPart.Write([]byte("Player 1,Dojo A\nPlayer 2,Dojo B"))
		// NOTE: no seeds file added to the multipart, the manifest names
		// "absent-seeds.csv" but it never lands in the upload. Pre-fix
		// this returned res.SeedCount=0 with no error; post-fix it must
		// surface "not found in upload" so the user knows the import
		// partially failed.
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string][]ImportResult
		json.Unmarshal(w.Body.Bytes(), &resp)
		require.Len(t, resp["results"], 1)
		assert.Contains(t, resp["results"][0].Error, "seeds file",
			"missing seeds file must surface a clear per-row error")
		assert.Contains(t, resp["results"][0].Error, "not found in upload",
			"error must indicate the missing-file failure mode")
		// Critical retry-safety check: the parse step happens BEFORE the
		// rename-lock save, so a missing-seeds failure must leave the
		// disk clean (no config.md, no participants.csv). The user can
		// fix the manifest and retry without hitting the ID-collision
		// guard.
		stored, _ := store.LoadCompetition("comp-missing-seeds")
		assert.Nil(t, stored, "missing-seeds failure must not leave a half-written competition on disk")
	})

	// Empty seeds parse (header-only file, all rows malformed) is the
	// soft path, no error, SeedCount=0. Symmetric with how the
	// participants block treats an empty roster file. Pin so a future
	// refactor that hardens empty-parse to an error doesn't silently
	// break legitimate "no seeds yet" imports.
	t.Run("Import Soft - Empty Seeds File", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "comp-empty-seeds"
    name: "Empty Seeds"
    participants: "players-empty-seeds.csv"
    seeds: "empty-seeds.csv"
`))
		playersPart, _ := writer.CreateFormFile("files", "players-empty-seeds.csv")
		playersPart.Write([]byte("Player 1,Dojo A"))
		// Header-only seeds file: parseSeedsBytes returns an empty slice.
		seedsPart, _ := writer.CreateFormFile("files", "empty-seeds.csv")
		seedsPart.Write([]byte("Rank,Name\n"))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string][]ImportResult
		json.Unmarshal(w.Body.Bytes(), &resp)
		require.Len(t, resp["results"], 1)
		assert.Empty(t, resp["results"][0].Error, "header-only seeds file must NOT error")
		assert.Equal(t, 0, resp["results"][0].SeedCount)
		assert.Equal(t, 1, resp["results"][0].ParticipantCount,
			"participants should still save when seeds file is empty")
	})

	// Rollback contract: when SaveCompetition succeeds but a downstream
	// per-row save (SaveParticipants or SaveSeeds) fails on I/O, the
	// half-written competition MUST be rolled off disk. Pre-fix, the
	// row error was surfaced but the config.md stayed, and the
	// ID-collision guard on retry then rejected the same manifest with
	// "competition ID %q already exists", so the operator couldn't
	// recover without manual file deletion.
	//
	// To induce a SaveSeeds I/O failure deterministically, pre-create
	// a DIRECTORY at the future seeds.csv path. `os.WriteFile` to a
	// path that exists as a directory returns "is a directory" (EISDIR)
	// reliably across platforms. SaveCompetition (config.md) and
	// SaveParticipants (participants.csv) still succeed because their
	// target paths are clear; only SaveSeeds collides.
	t.Run("Import Rollback On SaveSeeds I/O Failure", func(t *testing.T) {
		const cid = "comp-rollback-seeds"
		// Plant a directory where SaveSeeds wants to write a file.
		// Creates both the parent comp directory AND the leaf
		// "seeds.csv" as a directory inside.
		seedsObstacle := filepath.Join(tempDir, "competitions", cid, "seeds.csv")
		require.NoError(t, os.MkdirAll(seedsObstacle, 0o700))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "` + cid + `"
    name: "Rollback Test"
    participants: "rollback-players.csv"
    seeds: "rollback-seeds.csv"
`))
		playersPart, _ := writer.CreateFormFile("files", "rollback-players.csv")
		playersPart.Write([]byte("Player 1,Dojo A\nPlayer 2,Dojo B"))
		seedsPart, _ := writer.CreateFormFile("files", "rollback-seeds.csv")
		seedsPart.Write([]byte("Rank,Name\n1,Player 1\n"))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		var resp map[string][]ImportResult
		json.Unmarshal(w.Body.Bytes(), &resp)
		require.Len(t, resp["results"], 1)
		assert.Contains(t, resp["results"][0].Error, "save seeds:",
			"SaveSeeds I/O failure must surface a per-row error")

		// Rollback assertion: the entire comp directory must be gone,
		// so a retry of the same manifest (after the operator removes
		// the obstacle) passes the ID-collision guard.
		stored, _ := store.LoadCompetition(cid)
		assert.Nil(t, stored, "rollback must remove config.md so retry passes the ID-collision guard")

		// Defense-in-depth: the planted directory should also be gone
		// (DeleteCompetition does RemoveAll on the comp dir).
		_, statErr := os.Stat(filepath.Join(tempDir, "competitions", cid))
		assert.True(t, os.IsNotExist(statErr),
			"rollback must remove the entire comp directory, got stat err=%v", statErr)
	})

	// Padded YAML string fields persisted unchanged before this fix,
	// the import handler bypasses the POST/PUT trim in
	// handlers_competition.go and writes via SaveCompetitionChanged
	// directly. Pin the contract so all three write paths stay aligned.
	t.Run("Import Trims Padded String Fields", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "comp-trim"
    name: "  Padded Cup  "
    kind: "  individual  "
    format: "  mixed  "
    pool_size_mode: "  min  "
    number_prefix: "  A  "
    start_time: "  09:00  "
    date: "  12-05-2026  "
    courts: ["A"]
    participants: "trim.csv"
`))
		playersPart, _ := writer.CreateFormFile("files", "trim.csv")
		playersPart.Write([]byte("Player 1,Dojo A"))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		stored, err := store.LoadCompetition("comp-trim")
		require.NoError(t, err)
		require.NotNil(t, stored)
		assert.Equal(t, "Padded Cup", stored.Name, "Name should be trimmed")
		assert.Equal(t, "individual", stored.Kind, "Kind should be trimmed")
		assert.Equal(t, "mixed", stored.Format, "Format should be trimmed")
		assert.Equal(t, "min", stored.PoolSizeMode, "PoolSizeMode should be trimmed")
		assert.Equal(t, "A", stored.NumberPrefix, "NumberPrefix should be trimmed")
		assert.Equal(t, "09:00", stored.StartTime, "StartTime should be trimmed")
		assert.Equal(t, "12-05-2026", stored.Date, "Date should be trimmed")

		// The API response (ImportResult) must also reflect the trimmed
		// Name, not the raw manifest value. Pre-fix: res.Name = entry.Name
		// passed the padded "  Padded Cup  " back to the client while the
		// stored record was "Padded Cup", admin UI then showed two
		// different names for the same competition.
		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Equal(t, "Padded Cup", resp.Results[0].Name,
			"ImportResult.Name should reflect the trimmed value to match the persisted record")
	})

	// Cross-file guard symmetry with handlers_competition.go (POST + PUT)
	// and handlers_tournament.go. A manifest entry with whitespace-only
	// name trims to "", without an explicit guard, that would persist
	// as Competition.Name = "" and render a blank card in the admin UI.
	// The error is surfaced per-row in ImportResult.Error rather than
	// HTTP-failing the whole batch (matches existing behavior for missing
	// IDs / invalid IDs / save errors).
	t.Run("Whitespace-Only Name Rejected", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "blank-name-import"
    name: "   "
    kind: "individual"
    format: "mixed"
    courts: ["A"]
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Equal(t, "competition name is required", resp.Results[0].Error,
			"whitespace-only Name should land in ImportResult.Error, not on disk")

		// Confirm the competition was not persisted.
		stored, _ := store.LoadCompetition("blank-name-import")
		assert.Nil(t, stored, "blank-name-import should not have been persisted")
	})

	// Cross-file guard symmetry with handlers_competition.go POST + PUT.
	// The import handler now wraps SaveCompetition in
	// WithCompetitionRenameLock + checkUniqueCompName so a manifest
	// row whose name collides with an existing competition lands the
	// uniqueness error in result.Error (per-row, doesn't abort batch)
	// rather than silently creating a duplicate.
	t.Run("Duplicate Name Across Import And Existing Comp Rejected", func(t *testing.T) {
		// Pre-existing competition.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:   "existing-cup",
			Name: "Cup Name",
		}))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "duplicate-cup"
    name: "Cup Name"
    kind: "individual"
    format: "mixed"
    courts: ["A"]
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Contains(t, resp.Results[0].Error, "already exists",
			"duplicate name should land in ImportResult.Error (not silent duplicate)")

		// Confirm the duplicate-id comp was NOT persisted (the
		// existing one is untouched).
		stored, _ := store.LoadCompetition("duplicate-cup")
		assert.Nil(t, stored, "duplicate-cup should not have been persisted")
		existing, _ := store.LoadCompetition("existing-cup")
		require.NotNil(t, existing, "existing-cup must still exist")
		assert.Equal(t, "Cup Name", existing.Name, "existing comp's name must be untouched")
	})

	// Copilot round-4 finding on PR #104: the import path checked name
	// uniqueness but NOT ID uniqueness, so a manifest entry with an
	// existing comp.ID but a different comp.Name would silently
	// overwrite the existing competition (its name was unique, but
	// SaveCompetition writes by ID). Mirrors the ID-collision guard
	// already in POST /competitions and CreatePlayoff.
	t.Run("Duplicate ID Across Import And Existing Comp Rejected", func(t *testing.T) {
		// Pre-existing competition with a known name.
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:   "id-collide",
			Name: "Original Name",
		}))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "id-collide"
    name: "Different Name"
    kind: "individual"
    format: "mixed"
    courts: ["A"]
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Contains(t, resp.Results[0].Error, "already exists",
			"duplicate ID should land in ImportResult.Error (not silent overwrite)")

		// Confirm the original competition's Name was NOT clobbered.
		existing, _ := store.LoadCompetition("id-collide")
		require.NotNil(t, existing, "id-collide must still exist")
		assert.Equal(t, "Original Name", existing.Name,
			"existing comp's name must be untouched by the colliding-ID import")
	})

	// mp-yin4: the import path must enforce number-prefix uniqueness just like
	// POST/PUT, a manifest row with a duplicate NumberPrefix must land a
	// per-row error and not be persisted.
	t.Run("Duplicate Number Prefix Across Import And Existing Comp Rejected", func(t *testing.T) {
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID: "pfx-import-existing", Name: "Pfx Import Existing", NumberPrefix: "I",
		}))

		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "pfx-import-dup"
    name: "Pfx Import Dup"
    number_prefix: "I"
    kind: "individual"
    format: "mixed"
    courts: ["A"]
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Contains(t, resp.Results[0].Error, "number prefix",
			"duplicate prefix should land in ImportResult.Error")

		stored, _ := store.LoadCompetition("pfx-import-dup")
		assert.Nil(t, stored, "pfx-import-dup must not have been persisted")
	})

	// Date must be DD-MM-YYYY. Non-canonical formats (e.g. ISO YYYY-MM-DD)
	// land a per-row error rather than persisting the bad date, matches
	// the POST/PUT 400 contract in handlers_competition.go.
	t.Run("Non-DMY Date Rejected Per Row", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "iso-date-import"
    name: "ISO Date Import"
    kind: "individual"
    format: "mixed"
    date: "2026-05-12"
    courts: ["A"]
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)

		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Contains(t, resp.Results[0].Error, "date must be DD-MM-YYYY",
			"ISO-format date should land in ImportResult.Error (not persist)")

		// Confirm the bad-date comp was NOT persisted.
		stored, _ := store.LoadCompetition("iso-date-import")
		assert.Nil(t, stored, "iso-date-import must not have been persisted")
	})

	// Cross-file guard symmetry: POST /competitions and PUT /competitions/:id
	// call validateCompetitionCourts to reject empty / multi-character /
	// >26-court manifests. Pre-fix, the import path bypassed this check,
	// so a manifest row could persist court labels that no other write
	// path would accept. Two failure modes to cover: multi-character label
	// (court="AA"), and >26 courts.
	t.Run("Invalid Courts Rejected Per Row", func(t *testing.T) {
		// Multi-character court label
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "bad-court-label"
    name: "Bad Court Label"
    kind: "individual"
    format: "mixed"
    courts: ["AA"]
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Contains(t, resp.Results[0].Error, "courts",
			"multi-character court label should be rejected by validateCompetitionCourts")
		stored, _ := store.LoadCompetition("bad-court-label")
		assert.Nil(t, stored, "bad-court-label must not have been persisted")

		// Too many courts (>26)
		body2 := &bytes.Buffer{}
		writer2 := multipart.NewWriter(body2)
		manifestPart2, _ := writer2.CreateFormFile("files", "manifest.yaml")
		manifestPart2.Write([]byte(`
competitions:
  - id: "too-many-courts"
    name: "Too Many Courts"
    kind: "individual"
    format: "mixed"
    courts: ["A","B","C","D","E","F","G","H","I","J","K","L","M","N","O","P","Q","R","S","T","U","V","W","X","Y","Z","AA"]
`))
		writer2.Close()
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("POST", "/api/tournament/import", body2)
		req2.Header.Set("Content-Type", writer2.FormDataContentType())
		r.ServeHTTP(w2, req2)
		assert.Equal(t, http.StatusOK, w2.Code)
		var resp2 struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w2.Body.Bytes(), &resp2))
		require.Len(t, resp2.Results, 1)
		assert.Contains(t, resp2.Results[0].Error, "courts",
			">26 courts should be rejected by validateCompetitionCourts")
		stored2, _ := store.LoadCompetition("too-many-courts")
		assert.Nil(t, stored2, "too-many-courts must not have been persisted")

		// Duplicate court labels. Same cross-file symmetry: POST/PUT
		// /competitions reject `["A","A"]`; the import path must too,
		// or a manifest can persist court labels that no REST call
		// would accept and that collapse the frontend's `byCourt`
		// bucketing.
		bodyDup := &bytes.Buffer{}
		writerDup := multipart.NewWriter(bodyDup)
		manifestPartDup, _ := writerDup.CreateFormFile("files", "manifest.yaml")
		manifestPartDup.Write([]byte(`
competitions:
  - id: "dup-courts-import"
    name: "Dup Courts Import"
    kind: "individual"
    format: "mixed"
    courts: ["A", "A"]
`))
		writerDup.Close()
		wDup := httptest.NewRecorder()
		reqDup, _ := http.NewRequest("POST", "/api/tournament/import", bodyDup)
		reqDup.Header.Set("Content-Type", writerDup.FormDataContentType())
		r.ServeHTTP(wDup, reqDup)
		assert.Equal(t, http.StatusOK, wDup.Code)
		var respDup struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(wDup.Body.Bytes(), &respDup))
		require.Len(t, respDup.Results, 1)
		assert.Contains(t, respDup.Results[0].Error, "duplicate court label",
			"duplicate court labels should be rejected by validateCompetitionCourts")
		storedDup, _ := store.LoadCompetition("dup-courts-import")
		assert.Nil(t, storedDup, "dup-courts-import must not have been persisted")
	})

	// validateCompetitionFormat cross-file guard symmetry: a Swiss
	// competition imported WITHOUT swissRounds must error per-row
	// rather than persisting (FR-050a). This mirrors the
	// validateCompetitionCourts and validateDateDMY per-row guards
	// already in importCompetition. T181: swiss is now an accepted
	// format value, the validation moves to swissRounds >= 1.
	t.Run("Swiss Missing Rounds Rejected Per Row", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "swiss-no-rounds-import"
    name: "Swiss No Rounds Import"
    kind: "individual"
    format: "swiss"
    courts: ["A"]
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Contains(t, resp.Results[0].Error, "swissRounds",
			"swiss without swissRounds should land in ImportResult.Error (not persist)")
		stored, _ := store.LoadCompetition("swiss-no-rounds-import")
		assert.Nil(t, stored, "swiss-no-rounds-import must not have been persisted")
	})

	t.Run("Invalid Non-Swiss Format Rejected Per Row", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "bad-format-import"
    name: "Bad Format Import"
    kind: "individual"
    format: "roundrobin"
    courts: ["A"]
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Contains(t, resp.Results[0].Error, "format:",
			"unknown non-swiss format should land in ImportResult.Error (not persist)")
		stored, _ := store.LoadCompetition("bad-format-import")
		assert.Nil(t, stored, "bad-format-import must not have been persisted")
	})

	// validateDateDMY year-range enforcement: a manifest with a date
	// outside minDateYear..maxDateYear (mirroring JS MIN_YEAR/MAX_YEAR)
	// must error per-row rather than persisting a year the admin
	// Settings UI then refuses to display or save against.
	t.Run("Year Out Of Range Rejected Per Row", func(t *testing.T) {
		body := &bytes.Buffer{}
		writer := multipart.NewWriter(body)
		manifestPart, _ := writer.CreateFormFile("files", "manifest.yaml")
		manifestPart.Write([]byte(`
competitions:
  - id: "year-out-of-range-import"
    name: "Year Out Of Range Import"
    kind: "individual"
    format: "mixed"
    date: "01-01-1800"
    courts: ["A"]
`))
		writer.Close()

		w := httptest.NewRecorder()
		req, _ := http.NewRequest("POST", "/api/tournament/import", body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		r.ServeHTTP(w, req)
		require.Equal(t, http.StatusOK, w.Code)
		var resp struct {
			Results []ImportResult `json:"results"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		require.Len(t, resp.Results, 1)
		assert.Contains(t, resp.Results[0].Error, "date year must be between",
			"out-of-range year should land in ImportResult.Error (not persist)")
		stored, _ := store.LoadCompetition("year-out-of-range-import")
		assert.Nil(t, stored, "year-out-of-range-import must not have been persisted")
	})
}

func TestParseSeedsBytes(t *testing.T) {
	t.Run("Different Formats", func(t *testing.T) {
		data := []byte("rank,name\n1,Player A\nPlayer B,2\n,Ignored\nInvalid,Rank\n")
		seeds, err := parseSeedsBytes(data)
		assert.NoError(t, err)
		assert.Len(t, seeds, 2)
		assert.Equal(t, "Player A", seeds[0].Name)
		assert.Equal(t, 1, seeds[0].SeedRank)
		assert.Equal(t, "Player B", seeds[1].Name)
		assert.Equal(t, 2, seeds[1].SeedRank)
	})
}

// TestImportCompetition_InheritsTournamentCourts locks in that the manifest
// importer applies the same court invariant as the POST/PUT handlers: a row
// that omits courts inherits the tournament's courts via
// resolveCompetitionCourts (it used to hardcode a single "A").
func TestImportCompetition_InheritsTournamentCourts(t *testing.T) {
	store, err := state.NewStore(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, store.SaveTournament(&state.Tournament{
		Name: "T", Date: "11-06-2026", Courts: []string{"A", "B"},
	}))

	t.Run("omitted courts inherit the tournament's courts", func(t *testing.T) {
		entry := ImportManifestComp{ID: "imp-no-courts", Name: "No Courts", Date: "11-06-2026"}
		res := importCompetition(store, entry, map[string][]byte{})
		require.Emptyf(t, res.Error, "import should succeed: %s", res.Error)
		comp, err := store.LoadCompetition("imp-no-courts")
		require.NoError(t, err)
		assert.Equal(t, []string{"A", "B"}, comp.Courts)
	})

	t.Run("explicit manifest courts are preserved", func(t *testing.T) {
		entry := ImportManifestComp{ID: "imp-one-court", Name: "One Court", Date: "11-06-2026", Courts: []string{"B"}}
		res := importCompetition(store, entry, map[string][]byte{})
		require.Emptyf(t, res.Error, "import should succeed: %s", res.Error)
		comp, err := store.LoadCompetition("imp-one-court")
		require.NoError(t, err)
		assert.Equal(t, []string{"B"}, comp.Courts)
	})
}
