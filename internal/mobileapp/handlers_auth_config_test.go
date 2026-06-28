package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthConfigHandler_ScheduleEnabledFlag(t *testing.T) {
	for _, tc := range []struct {
		name            string
		scheduleEnabled bool
	}{
		{"scheduleEnabled=true", true},
		{"scheduleEnabled=false", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tempDir, err := os.MkdirTemp("", "auth-config-test-*")
			require.NoError(t, err)
			t.Cleanup(func() { os.RemoveAll(tempDir) })

			store, err := state.NewStore(tempDir)
			require.NoError(t, err)

			gin.SetMode(gin.TestMode)
			r := gin.New()
			api := r.Group("/api")
			verifier := NewFileVerifier(store)
			elevated := NewFileElevatedVerifier(store)
			RegisterAuthConfigHandlers(api, verifier, elevated, tc.scheduleEnabled)

			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/api/auth-config", nil)
			r.ServeHTTP(w, req)

			require.Equal(t, http.StatusOK, w.Code,
				"GET /api/auth-config must return 200: %s", w.Body.String())

			var resp authConfigResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
			assert.Equal(t, tc.scheduleEnabled, resp.ScheduleEnabled,
				"scheduleEnabled in response must match the flag passed to RegisterAuthConfigHandlers")
		})
	}
}
