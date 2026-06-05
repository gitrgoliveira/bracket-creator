package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"testing/fstest"

	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVersionEndpoint(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "version-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)

	eng := engine.New(store)
	mockFS := fstest.MapFS{
		"web-mobile/index.html": {Data: []byte("<html><body>Mobile</body></html>")},
	}
	res := resources.NewResources(nil, mockFS)

	r, _ := NewRouter(store, eng, res, NewFileVerifier(store))

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/version", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp versionResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)

	assert.Equal(t, version.GetVersion(), resp.Version)
	assert.Equal(t, version.GetGitCommit(), resp.GitCommit)
	assert.Equal(t, version.GetBuildDate(), resp.BuildDate)
}
