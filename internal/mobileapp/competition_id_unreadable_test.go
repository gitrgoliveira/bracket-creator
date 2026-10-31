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

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A competition whose config.md cannot be read still holds its id: a create
// under that id, by POST or by an import row, is refused with a sentence that
// names the id and no path, and the file's bytes are left as they were.

const unreadableConfig = "not front matter at all"

// plantUnreadableConfig writes an unparseable config.md for id and answers its
// path.
func plantUnreadableConfig(t *testing.T, dataDir, id string) string {
	t.Helper()
	dir := filepath.Join(dataDir, "competitions", id)
	require.NoError(t, os.MkdirAll(dir, 0o700))
	path := filepath.Join(dir, "config.md")
	require.NoError(t, os.WriteFile(path, []byte(unreadableConfig), 0o600))
	return path
}

func assertConfigUntouched(t *testing.T, path string) {
	t.Helper()
	after, err := bctest.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, unreadableConfig, string(after), "the unreadable competition's settings are kept as they were")
}

func TestPOSTCompetition_RefusesAnIDHeldByAnUnreadableCompetition(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	const cid = "held-unreadable"
	path := plantUnreadableConfig(t, tempDir, cid)
	logs := captureLog(t)

	body, err := json.Marshal(map[string]any{
		"id":     cid,
		"name":   "Held Unreadable",
		"kind":   "individual",
		"format": "mixed",
		"date":   "12-05-2026",
		"courts": []string{"A"},
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, codeCompetitionIDUnreadable, resp["code"])
	assert.Equal(t, `competition ID "held-unreadable" is held by a competition whose settings cannot be read; repair or remove it`, resp["error"])
	assert.NotContains(t, w.Body.String(), tempDir, "no server path reaches the client")
	assert.Contains(t, logs.String(), "missing front matter delimiter", "the read error itself is logged")
	assertConfigUntouched(t, path)
}

// A fault READING config.md (here a folder in its place) is the server's, not
// the competition's settings: a 500 with no code and no path, and nothing is
// saved over it.
func TestPOSTCompetition_AnIDThatCannotBeCheckedIsAServerError(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	const cid = "held-by-a-folder"
	path := filepath.Join(tempDir, "competitions", cid, "config.md")
	require.NoError(t, os.MkdirAll(path, 0o700))
	logs := captureLog(t)

	body, err := json.Marshal(map[string]any{
		"id":     cid,
		"name":   "Held By A Folder",
		"kind":   "individual",
		"format": "mixed",
		"date":   "12-05-2026",
		"courts": []string{"A"},
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	require.Equal(t, http.StatusInternalServerError, w.Code, w.Body.String())
	assert.NotContains(t, w.Body.String(), codeCompetitionIDUnreadable, "a read fault is not the unreadable-settings refusal")
	assert.NotContains(t, w.Body.String(), tempDir, "no server path reaches the client")
	assert.Contains(t, logs.String(), "is a directory", "the read error itself is logged")
	assert.DirExists(t, path, "nothing is saved over it")
}

func TestImportCompetition_RefusesAnIDHeldByAnUnreadableCompetition(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	seedImportTournamentDate(t, store)
	const cid = "held-unreadable"
	path := plantUnreadableConfig(t, tempDir, cid)
	logs := captureLog(t)

	res := postImportManifest(t, r, `
competitions:
  - id: "`+cid+`"
    name: "Held Unreadable"
`, nil)

	assert.Equal(t, `competition ID "held-unreadable" is held by a competition whose settings cannot be read; repair or remove it`, res.Error)
	assert.NotContains(t, res.Error, tempDir, "no server path reaches the client")
	assert.Contains(t, logs.String(), "missing front matter delimiter", "the read error itself is logged")
	assertConfigUntouched(t, path)
}

// A fault reading config.md (here a folder in its place) is the server's, not
// the competition's settings: the row says the id could not be checked, and
// names no path.
func TestImportCompetition_AnIDThatCannotBeCheckedIsRefused(t *testing.T) {
	r, store, _, _, tempDir := setupTestRouter(t)
	seedImportTournamentDate(t, store)
	const cid = "held-by-a-folder"
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "competitions", cid, "config.md"), 0o700))
	logs := captureLog(t)

	res := postImportManifest(t, r, `
competitions:
  - id: "`+cid+`"
    name: "Held By A Folder"
`, nil)

	assert.Equal(t, "the competition ID could not be checked", res.Error)
	assert.Contains(t, logs.String(), "is a directory", "the read error itself is logged")
	assert.DirExists(t, filepath.Join(tempDir, "competitions", cid, "config.md"), "nothing is saved over it")
}

// A roster rule the save refuses names the participant, so the import row
// shows it as it is rather than the plain "could not be saved". The import's
// own parser refuses a blank dojo or a duplicate row before the save ("parse
// participants: ..."), so the save's refusal is made here directly and handed
// to importSaveFailure as the import hands it.
func TestImportSaveFailure_RosterRuleNamesTheParticipant(t *testing.T) {
	_, store, _, _, tempDir := setupTestRouter(t)
	const cid = "blank-dojo"
	require.NoError(t, store.SaveCompetition(&state.Competition{ID: cid, Courts: []string{"A"}}))
	saveErr := store.SaveParticipantsRestored(cid, []domain.Player{{Name: "Player One", Dojo: ""}})
	require.ErrorIs(t, saveErr, state.ErrBlankDojo)
	logs := captureLog(t)

	msg := importSaveFailure(store, cid, "the participants could not be saved", saveErr)

	assert.Equal(t, saveErr.Error(), msg, "the roster rule is shown as it is")
	assert.Contains(t, msg, "Player One", "the row error names the participant the rule refused")
	assert.NotContains(t, msg, tempDir, "no server path reaches the client")
	assert.Empty(t, logs.String(), "a roster rule is the operator's to fix, not a server fault to log")
	stored, err := store.LoadCompetition(cid)
	require.NoError(t, err)
	assert.Nil(t, stored, "the half-imported competition is rolled back")
}

// postImportManifest imports one manifest with its named files and answers
// the one row the import reports.
func postImportManifest(t *testing.T, r http.Handler, manifest string, files map[string]string) ImportResult {
	t.Helper()
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("files", "manifest.yaml")
	require.NoError(t, err)
	_, err = part.Write([]byte(manifest))
	require.NoError(t, err)
	for name, content := range files {
		part, err := writer.CreateFormFile("files", name)
		require.NoError(t, err)
		_, err = part.Write([]byte(content))
		require.NoError(t, err)
	}
	require.NoError(t, writer.Close())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/tournament/import", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var resp map[string][]ImportResult
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp["results"], 1)
	return resp["results"][0]
}
