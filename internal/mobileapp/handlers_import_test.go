package mobileapp

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
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
    format: "pools"
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

	// Padded YAML string fields persisted unchanged before this fix —
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
    format: "  pools  "
    pool_size_mode: "  min  "
    number_prefix: "  A  "
    start_time: "  09:00  "
    date: "  2026-05-12  "
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
		assert.Equal(t, "pools", stored.Format, "Format should be trimmed")
		assert.Equal(t, "min", stored.PoolSizeMode, "PoolSizeMode should be trimmed")
		assert.Equal(t, "A", stored.NumberPrefix, "NumberPrefix should be trimmed")
		assert.Equal(t, "09:00", stored.StartTime, "StartTime should be trimmed")
		assert.Equal(t, "2026-05-12", stored.Date, "Date should be trimmed")

		// The API response (ImportResult) must also reflect the trimmed
		// Name, not the raw manifest value. Pre-fix: res.Name = entry.Name
		// passed the padded "  Padded Cup  " back to the client while the
		// stored record was "Padded Cup" — admin UI then showed two
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
	// name trims to "" — without an explicit guard, that would persist
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
    format: "pools"
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
    format: "pools"
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
    format: "pools"
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
