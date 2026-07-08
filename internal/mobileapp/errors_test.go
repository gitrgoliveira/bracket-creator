package mobileapp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

// TestInternalError verifies the generic-500 helper: it always returns HTTP 500,
// never leaks the underlying error string to the client, and uses a caller-
// supplied safe label when one is given.
func TestInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)

	// A wrapped error carrying a filesystem path that must NOT reach the client.
	sensitive := errors.New("open /srv/tournament-data/competitions/abc/pools.csv: permission denied")

	t.Run("no public message -> generic body, no leak", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/competitions/abc/export-results", nil)

		internalError(c, sensitive)

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "internal error")
		assert.NotContains(t, w.Body.String(), "pools.csv",
			"the raw error (incl. filesystem paths) must never be echoed to the client")
		assert.NotContains(t, w.Body.String(), "permission denied")
	})

	t.Run("safe public message is surfaced, error still hidden", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPut, "/api/competitions/abc", nil)

		internalError(c, sensitive, "failed to save participants")

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		assert.Contains(t, w.Body.String(), "failed to save participants")
		assert.NotContains(t, w.Body.String(), "pools.csv")
	})

	t.Run("empty public message falls back to generic", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/x", nil)

		internalError(c, sensitive, "")

		assert.Contains(t, w.Body.String(), "internal error")
	})
}
