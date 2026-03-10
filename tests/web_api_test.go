package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/cmd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAPI_ParseParticipants(t *testing.T) {
	restoreDir := ensureRepoRoot(t)
	defer restoreDir()

	router := cmd.NewRouter()

	w := httptest.NewRecorder()
	// Test without Zekken Name
	body := `{"playerList": "Jane Doe, Dojo1\nJohn Smith, Dojo2", "withZekkenName": false}`
	req, err := http.NewRequest("POST", "/api/parse-participants", strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string][]map[string]string
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Len(t, resp["participants"], 2)
	assert.Equal(t, "Jane Doe", resp["participants"][0]["name"])
	assert.Equal(t, "J. DOE", resp["participants"][0]["displayName"])
	assert.Equal(t, "Dojo1", resp["participants"][0]["dojo"])

	// Test with Zekken Name
	w2 := httptest.NewRecorder()
	body2 := `{"playerList": "Jane Doe, ジェーン, Dojo1", "withZekkenName": true}`
	req2, err := http.NewRequest("POST", "/api/parse-participants", strings.NewReader(body2))
	require.NoError(t, err)
	req2.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w2, req2)
	assert.Equal(t, http.StatusOK, w2.Code)

	var resp2 map[string][]map[string]string
	err = json.Unmarshal(w2.Body.Bytes(), &resp2)
	require.NoError(t, err)

	assert.Len(t, resp2["participants"], 1)
	assert.Equal(t, "Jane Doe", resp2["participants"][0]["name"])
	assert.Equal(t, "ジェーン", resp2["participants"][0]["displayName"])
	assert.Equal(t, "Dojo1", resp2["participants"][0]["dojo"])
}

func TestAPI_CreateWithSeeds(t *testing.T) {
	restoreDir := ensureRepoRoot(t)
	defer restoreDir()

	router := cmd.NewRouter()

	w := httptest.NewRecorder()

	form := url.Values{}
	form.Add("playerList", "Jane Doe, Dojo1\nJohn Smith, Dojo2")
	form.Add("tournamentType", "playoffs")
	form.Add("seeds", `[{"Name": "Jane Doe", "SeedRank": 1}]`)

	req, err := http.NewRequest("POST", "/create", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	router.ServeHTTP(w, req)

	// Since we are in test and helper.TemplateFile is empty, it should try fallback to disk
	// If it fails fallback too, it might error. But from root it should find template.xlsx.
	// Oh wait, tests/ subpackage - Chdir might be needed or it finds it in root if we run go test ./...
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", w.Header().Get("Content-Type"))
}

func TestAPI_CreateWithMissingSeed(t *testing.T) {
	restoreDir := ensureRepoRoot(t)
	defer restoreDir()

	router := cmd.NewRouter()

	w := httptest.NewRecorder()

	form := url.Values{}
	form.Add("playerList", "Jane Doe, Dojo1")
	form.Add("tournamentType", "playoffs")
	form.Add("seeds", `[{"Name": "John Smith", "SeedRank": 1}]`)

	req, err := http.NewRequest("POST", "/create", strings.NewReader(form.Encode()))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "seeded participant not found")
}

func ensureRepoRoot(t *testing.T) func() {
	t.Helper()
	// No longer needed - template.xlsx is loaded in TestMain
	// This function kept for backward compatibility
	return func() {}
}
