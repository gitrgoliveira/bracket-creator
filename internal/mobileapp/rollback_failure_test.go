package mobileapp

import (
	"bytes"
	"encoding/json"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// When a create path's save fails and its rollback cannot remove the
// competition either, the folder left on disk blocks a retry under the same id
// ("ID already exists"), so the operator must be told, in the answer and in
// the log, rather than the rollback failing silently.
//
// Both failures are made with one obstacle: a NON-EMPTY directory planted
// where the file the create writes must go, made read-only. The save cannot
// rename its file onto it, and DeleteCompetition's RemoveAll cannot empty it.

// plantStuckFile plants that obstacle at path and restores its mode at the
// end of the test, before the test router's cleanup removes the data folder.
func plantStuckFile(t *testing.T, path string) {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("root removes and writes through any folder mode")
	}
	require.NoError(t, os.MkdirAll(path, 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(path, "keep"), []byte("x"), 0o600))
	require.NoError(t, os.Chmod(path, 0o500))
	t.Cleanup(func() { assert.NoError(t, os.Chmod(path, 0o700)) })
}

// captureLog sends the standard logger to a buffer for the rest of the test.
func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(prev) })
	return &buf
}

func TestPOSTCompetition_RollbackFailureIsReported(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	const cid = "rollback-stuck-create"
	compDir := filepath.Join(tempDir, "competitions", cid)
	plantStuckFile(t, filepath.Join(compDir, "participants.csv"))
	logs := captureLog(t)

	body, err := json.Marshal(map[string]any{
		"id":      cid,
		"name":    "Rollback Stuck",
		"kind":    "individual",
		"format":  "mixed",
		"date":    "12-05-2026",
		"courts":  []string{"A"},
		"players": []map[string]any{{"Name": "Player 1", "Dojo": "Dojo A"}},
	})
	require.NoError(t, err)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/competitions", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "failed to save participants")
	assert.Contains(t, w.Body.String(), `competition \"`+cid+`\" could not be removed: delete its folder before trying again`,
		"the answer must say the half-saved competition is still on disk")
	assert.NotContains(t, w.Body.String(), tempDir, "filesystem paths stay in the log")
	assert.Contains(t, logs.String(), `rolling back competition "`+cid+`" failed`)
	assert.Equal(t, 1, strings.Count(logs.String(), "\n"), "the failure is logged once: "+logs.String())
	assert.DirExists(t, compDir, "the message must be true: the folder is still there")
}

func TestImportCompetition_RollbackFailureIsReported(t *testing.T) {
	for _, tc := range []struct {
		name, cid, obstacle, prefix string
	}{
		{"participants", "rollback-stuck-players", "participants.csv", "save participants: "},
		{"seeds", "rollback-stuck-seeds", "seeds.csv", "save seeds: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, store, _, _, tempDir := setupTestRouter(t)
			seedImportTournamentDate(t, store)
			compDir := filepath.Join(tempDir, "competitions", tc.cid)
			plantStuckFile(t, filepath.Join(compDir, tc.obstacle))
			logs := captureLog(t)

			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			manifestPart, err := writer.CreateFormFile("files", "manifest.yaml")
			require.NoError(t, err)
			_, err = manifestPart.Write([]byte(`
competitions:
  - id: "` + tc.cid + `"
    name: "Rollback Stuck"
    participants: "stuck-players.csv"
    seeds: "stuck-seeds.csv"
`))
			require.NoError(t, err)
			playersPart, err := writer.CreateFormFile("files", "stuck-players.csv")
			require.NoError(t, err)
			_, err = playersPart.Write([]byte("Player 1,Dojo A\nPlayer 2,Dojo B"))
			require.NoError(t, err)
			seedsPart, err := writer.CreateFormFile("files", "stuck-seeds.csv")
			require.NoError(t, err)
			_, err = seedsPart.Write([]byte("Rank,Name\n1,Player 1\n"))
			require.NoError(t, err)
			require.NoError(t, writer.Close())

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/tournament/import", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code)
			var resp map[string][]ImportResult
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			require.Len(t, resp["results"], 1)
			rowErr := resp["results"][0].Error
			assert.Contains(t, rowErr, tc.prefix)
			assert.Contains(t, rowErr, `rollback failed: competition "`+tc.cid+`" could not be removed, delete its folder before trying again`,
				"the row must say the half-written competition is still on disk")
			// The save's own error (before "; rollback failed") already named
			// its paths before this change; what the rollback adds names none.
			_, rollbackPart, found := strings.Cut(rowErr, "; rollback failed")
			require.True(t, found)
			assert.NotContains(t, rollbackPart, tempDir, "filesystem paths of the failed removal stay in the log")
			assert.Contains(t, logs.String(), `rolling back competition "`+tc.cid+`" failed`)
			assert.DirExists(t, compDir, "the message must be true: the folder is still there")
		})
	}
}

func TestRemoveFile(t *testing.T) {
	dir := t.TempDir()

	assert.NoError(t, removeFile(filepath.Join(dir, "absent")), "a file already gone counts as removed")

	present := filepath.Join(dir, "present")
	require.NoError(t, os.WriteFile(present, []byte("x"), 0o600))
	assert.NoError(t, removeFile(present))
	assert.NoFileExists(t, present)

	// A non-empty directory cannot be removed: the failure is answered, and
	// removeFileOrLog logs it.
	stuck := filepath.Join(dir, "stuck")
	require.NoError(t, os.MkdirAll(filepath.Join(stuck, "child"), 0o700))
	assert.Error(t, removeFile(stuck))
	logs := captureLog(t)
	removeFileOrLog(stuck)
	assert.Contains(t, logs.String(), "could not remove "+stuck+", it is left on disk")
}
