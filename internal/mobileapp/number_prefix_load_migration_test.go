package mobileapp

// number_prefix_load_migration_test.go pins that a competition which appears
// on disk AFTER the process started is never served without a number prefix.
//
// The mobile-app command runs engine.MigrateNumberPrefixes once at startup,
// which covers everything present when it booted. On a laptop at a venue a
// competition can arrive later -- a folder restored from a backup, or copied
// across from another machine -- and before this, it was listed with no
// prefix and stayed that way until the next restart.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// writeCompetitionFolderDirectly drops a competition on disk behind the
// running store's back, which is what restoring a backup looks like.
func writeCompetitionFolderDirectly(t *testing.T, dir, id, name string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Join(dir, "competitions", id), 0o700))
	cfg := "---\nid: " + id + "\nname: " + name + "\nformat: playoffs\ncourts:\n  - A\nstatus: setup\n---\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, "competitions", id, "config.md"), []byte(cfg), 0o600))
}

func TestNumberPrefixAssignedOnLoadForACompetitionThatArrivedLater(t *testing.T) {
	r, _, _, _, tempDir := setupTestRouter(t)
	defer os.RemoveAll(tempDir)

	body := `{"name":"Legacy Cup","date":"12-09-2026","venue":"Crystal Palace NSC","courts":["A"],"password":"pfx-pass"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tournament", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())

	writeCompetitionFolderDirectly(t, tempDir, "kendo-open", "Kendo Open")

	cfgPath := filepath.Join(tempDir, "competitions", "kendo-open", "config.md")
	raw, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "number_prefix", "fixture guard: it really arrives without one")

	req = httptest.NewRequest(http.MethodGet, "/api/competitions", nil)
	req.Header.Set("X-Tournament-Password", "pfx-pass")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	var comps []struct {
		ID           string `json:"id"`
		NumberPrefix string `json:"numberPrefix"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &comps))
	require.Len(t, comps, 1)
	assert.Equal(t, "kendo-open", comps[0].ID)
	assert.Equal(t, "K", comps[0].NumberPrefix,
		"the listing derives the prefix rather than serving the competition without one")

	raw, err = os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "number_prefix: K",
		"and persists it, so the derivation is not repeated on every request")
}
