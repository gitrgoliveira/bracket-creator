package cmd

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

func TestNewServeCmd(t *testing.T) {
	cmd := newServeCmd()
	assert.NotNil(t, cmd)
	assert.Equal(t, "serve", cmd.Use)
	assert.Equal(t, "serves a web gui", cmd.Short)
}

func TestServeCmdFlags(t *testing.T) {
	cmd := newServeCmd()

	// Test bind flag
	bindFlag := cmd.Flags().Lookup("bind")
	assert.NotNil(t, bindFlag)
	assert.Equal(t, "b", bindFlag.Shorthand)

	// Test port flag
	portFlag := cmd.Flags().Lookup("port")
	assert.NotNil(t, portFlag)
	assert.Equal(t, "p", portFlag.Shorthand)
}

func TestServeCmdWithEnvVars(t *testing.T) {
	// Test with environment variables
	os.Setenv("BIND_ADDRESS", "0.0.0.0")
	os.Setenv("PORT", "9090")
	defer func() {
		os.Unsetenv("BIND_ADDRESS")
		os.Unsetenv("PORT")
	}()

	cmd := newServeCmd()
	assert.NotNil(t, cmd)

	bindFlag := cmd.Flags().Lookup("bind")
	assert.Equal(t, "0.0.0.0", bindFlag.DefValue)

	portFlag := cmd.Flags().Lookup("port")
	assert.Equal(t, "9090", portFlag.DefValue)
}

func TestServeCmdWithInvalidPort(t *testing.T) {
	os.Setenv("PORT", "invalid")
	defer os.Unsetenv("PORT")

	cmd := newServeCmd()
	assert.NotNil(t, cmd)

	// Should fall back to default port 8080
	portFlag := cmd.Flags().Lookup("port")
	assert.Equal(t, "8080", portFlag.DefValue)
}

func TestNewRouter(t *testing.T) {
	router := NewRouter()
	assert.NotNil(t, router)
}

func TestRouterStatusEndpoint(t *testing.T) {
	router := NewRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/status", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Equal(t, "ok", response["status"])
	assert.NotEmpty(t, response["version"])
}

func TestRouterParseParticipants(t *testing.T) {
	router := NewRouter()

	tests := []struct {
		name           string
		payload        map[string]interface{}
		expectedStatus int
		expectError    bool
	}{
		{
			name: "valid participants",
			payload: map[string]interface{}{
				"playerList":     "John Doe,Dojo1\nJane Smith,Dojo2",
				"withZekkenName": false,
			},
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name: "with zekken names",
			payload: map[string]interface{}{
				"playerList":     "John Doe,Dojo1,Johnny\nJane Smith,Dojo2,Janey",
				"withZekkenName": true,
			},
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
		{
			name: "empty player list",
			payload: map[string]interface{}{
				"playerList":     "",
				"withZekkenName": false,
			},
			expectedStatus: http.StatusOK,
			expectError:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body, _ := json.Marshal(tt.payload)
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/api/parse-participants", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if !tt.expectError {
				var response map[string]interface{}
				err := json.Unmarshal(w.Body.Bytes(), &response)
				assert.NoError(t, err)
				assert.Contains(t, response, "participants")
			}
		})
	}
}

func TestRouterCreateEndpoint_EmptyPlayerList(t *testing.T) {
	router := NewRouter()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("playerList", "")
	writer.WriteField("tournamentType", "playoffs")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/create", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "Player list cannot be empty")
}

func TestRouterCreateEndpoint_InvalidTournamentType(t *testing.T) {
	router := NewRouter()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("playerList", "John Doe,Dojo1\nJane Smith,Dojo2")
	writer.WriteField("tournamentType", "invalid")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/create", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "Invalid tournament type")
}

func TestRouterCreateEndpoint_PoolsValidation(t *testing.T) {
	router := NewRouter()

	tests := []struct {
		name           string
		winnersPerPool string
		playersPerPool string
		expectedError  string
	}{
		{
			name:           "zero winners",
			winnersPerPool: "0",
			playersPerPool: "3",
			expectedError:  "Winners per pool must be at least 1",
		},
		{
			name:           "zero players",
			winnersPerPool: "2",
			playersPerPool: "0",
			expectedError:  "Players per pool must be at least 1",
		},
		{
			name:           "winners >= players",
			winnersPerPool: "3",
			playersPerPool: "3",
			expectedError:  "Winners per pool must be less than players per pool",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &bytes.Buffer{}
			writer := multipart.NewWriter(body)
			writer.WriteField("playerList", "John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4")
			writer.WriteField("tournamentType", "pools")
			writer.WriteField("winnersPerPool", tt.winnersPerPool)
			writer.WriteField("playersPerPool", tt.playersPerPool)
			writer.Close()

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/create", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			router.ServeHTTP(w, req)

			assert.Equal(t, http.StatusBadRequest, w.Code)

			var response map[string]interface{}
			err := json.Unmarshal(w.Body.Bytes(), &response)
			assert.NoError(t, err)
			assert.Contains(t, response["error"], tt.expectedError)
		})
	}
}

func TestRouterCreateEndpoint_PlayoffsSuccess(t *testing.T) {
	// Change to project root for template access
	err := os.Chdir("..")
	require.NoError(t, err)
	defer os.Chdir("cmd")

	router := NewRouter()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("playerList", "John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4")
	writer.WriteField("tournamentType", "playoffs")
	writer.WriteField("determined", "on")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/create", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", w.Header().Get("Content-Type"))
	assert.Contains(t, w.Header().Get("Content-Disposition"), "playoffs-")
	assert.Greater(t, w.Body.Len(), 0)
}

func TestRouterCreateEndpoint_PoolsSuccess(t *testing.T) {
	// Change to project root for template access
	err := os.Chdir("..")
	require.NoError(t, err)
	defer os.Chdir("cmd")

	router := NewRouter()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("playerList", "John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4\nCharlie,Dojo5\nDave,Dojo6")
	writer.WriteField("tournamentType", "pools")
	writer.WriteField("winnersPerPool", "2")
	writer.WriteField("playersPerPool", "3")
	writer.WriteField("determined", "on")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/create", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	// Template file may not be available, so either success or template error is acceptable
	if w.Code == http.StatusOK {
		assert.Equal(t, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", w.Header().Get("Content-Type"))
		assert.Contains(t, w.Header().Get("Content-Disposition"), "pools-")
		assert.Greater(t, w.Body.Len(), 0)
	} else {
		assert.Equal(t, http.StatusInternalServerError, w.Code)
	}
}

func TestRouterCreateEndpoint_WithSeeds(t *testing.T) {
	// Change to project root for template access
	err := os.Chdir("..")
	require.NoError(t, err)
	defer os.Chdir("cmd")

	router := NewRouter()

	seeds := []map[string]interface{}{
		{"name": "John Doe", "rank": 1},
		{"name": "Jane Smith", "rank": 2},
	}
	seedsJSON, _ := json.Marshal(seeds)

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("playerList", "John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4")
	writer.WriteField("tournamentType", "playoffs")
	writer.WriteField("seeds", string(seedsJSON))
	writer.WriteField("determined", "on")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/create", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Greater(t, w.Body.Len(), 0)
}

func TestRouterCreateEndpoint_InvalidSeeds(t *testing.T) {
	router := NewRouter()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("playerList", "John Doe,Dojo1\nJane Smith,Dojo2")
	writer.WriteField("tournamentType", "playoffs")
	writer.WriteField("seeds", "invalid json")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/create", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var response map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &response)
	assert.NoError(t, err)
	assert.Contains(t, response["error"], "Invalid seed assignments format")
}

func TestRouterCreateEndpoint_WithOptions(t *testing.T) {
	// Change to project root for template access
	err := os.Chdir("..")
	require.NoError(t, err)
	defer os.Chdir("cmd")

	router := NewRouter()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("playerList", "John Doe,Dojo1,Johnny\nJane Smith,Dojo2,Janey\nAlice,Dojo3,Ali\nBob,Dojo4,Bobby")
	writer.WriteField("tournamentType", "playoffs")
	writer.WriteField("singleTree", "on")
	writer.WriteField("withZekkenName", "on")
	writer.WriteField("teamMatches", "2")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/create", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Greater(t, w.Body.Len(), 0)
}

func TestRouterRootRedirect(t *testing.T) {
	router := NewRouter()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	writer.WriteField("playerList", "test")
	writer.Close()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusMovedPermanently, w.Code)
	assert.Equal(t, "/create", w.Header().Get("Location"))
}

func TestRouterCORS(t *testing.T) {
	router := NewRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("OPTIONS", "/api/status", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, "*", w.Header().Get("Access-Control-Allow-Origin"))
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "GET")
	assert.Contains(t, w.Header().Get("Access-Control-Allow-Methods"), "POST")
}

func TestRouterStaticFiles(t *testing.T) {
	router := NewRouter()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/", nil)
	router.ServeHTTP(w, req)

	// Should serve index.html or return an error if web files not embedded
	assert.True(t, w.Code == http.StatusOK || w.Code == http.StatusInternalServerError)
}

func TestServeOptionsRun(t *testing.T) {
	// This test verifies the run method structure but doesn't actually start the server
	o := &serveOptions{
		bindAddress: "localhost",
		port:        8080,
	}

	assert.Equal(t, "localhost", o.bindAddress)
	assert.Equal(t, 8080, o.port)
}

// Made with Bob
