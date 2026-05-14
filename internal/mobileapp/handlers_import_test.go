package mobileapp

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

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
