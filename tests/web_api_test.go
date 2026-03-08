package tests

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/cmd"
	"github.com/stretchr/testify/assert"
)

func TestAPI_ParseParticipants(t *testing.T) {
	err := os.Chdir("..")
	assert.NoError(t, err)
	defer os.Chdir("tests")

	router := cmd.NewRouter()

	w := httptest.NewRecorder()
	body := `{"playerList": "Jane Doe, Dojo1\nJohn Smith, Dojo2"}`
	req, _ := http.NewRequest("POST", "/api/parse-participants", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string][]map[string]string
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)

	assert.Len(t, resp["participants"], 2)
	assert.Equal(t, "Jane Doe", resp["participants"][0]["name"])
	assert.Equal(t, "Dojo1", resp["participants"][0]["dojo"])
}

func TestAPI_CreateWithSeeds(t *testing.T) {
	err := os.Chdir("..")
	assert.NoError(t, err)
	defer os.Chdir("tests")

	router := cmd.NewRouter()

	w := httptest.NewRecorder()

	form := url.Values{}
	form.Add("playerList", "Jane Doe, Dojo1\nJohn Smith, Dojo2")
	form.Add("tournamentType", "playoffs")
	form.Add("seeds", `[{"Name": "Jane Doe", "SeedRank": 1}]`)

	req, _ := http.NewRequest("POST", "/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	router.ServeHTTP(w, req)

	// Since we are in test and helper.TemplateFile is empty, it should try fallback to disk
	// If it fails fallback too, it might error. But from root it should find template.xlsx.
	// Oh wait, tests/ subpackage - Chdir might be needed or it finds it in root if we run go test ./...
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", w.Header().Get("Content-Type"))
}

func TestAPI_CreateWithMissingSeed(t *testing.T) {
	err := os.Chdir("..")
	assert.NoError(t, err)
	defer os.Chdir("tests")

	router := cmd.NewRouter()

	w := httptest.NewRecorder()

	form := url.Values{}
	form.Add("playerList", "Jane Doe, Dojo1")
	form.Add("tournamentType", "playoffs")
	form.Add("seeds", `[{"Name": "John Smith", "SeedRank": 1}]`)

	req, _ := http.NewRequest("POST", "/create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "seeded participant not found")
}
