package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"testing/fstest"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRouter(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "server-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	store, err := state.NewStore(tempDir)
	require.NoError(t, err)

	eng := engine.New(store)

	// Mock FS
	mockFS := fstest.MapFS{
		"web-mobile/index.html": {Data: []byte("<html><body>Mobile</body></html>")},
		"web-mobile/main.js":    {Data: []byte("console.log('hello')")},
	}
	res := resources.NewResources(nil, mockFS)

	r := NewRouter(store, eng, res)

	// Test Health check
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "ok")

	// Test CORS
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("OPTIONS", "/health", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))

	// Test Static file serving
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/main.js", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "console.log")

	// Test SPA Fallback
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/some/spa/route", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), "Mobile")

	// Test API 404 (should not fallback to index.html)
	w = httptest.NewRecorder()
	req, _ = http.NewRequest("GET", "/api/not-exists", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "API route not found")

	w = httptest.NewRecorder()
	req, _ = http.NewRequest("POST", "/api/not-exists", nil)
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusNotFound, w.Code)
	assert.Contains(t, w.Body.String(), "API route not found")
}
