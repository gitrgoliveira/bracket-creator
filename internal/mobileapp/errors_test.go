package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
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

// A file the operator can repair is the one internal failure worth naming. The
// rest of internalError's contract still applies to it: nothing that names a
// path on disk may reach the client.
func TestInternalErrorNamesACorruptFile(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("a located parse failure is reported with its position", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodPost, "/api/matches/R1-1/score", nil)

		internalError(c, fmt.Errorf("loading bracket: %w", &state.CorruptFileError{
			File: "bracket.json", Line: 47, Column: 12,
			Detail: "invalid character 'x' after object key:value pair",
		}))

		assert.Equal(t, http.StatusInternalServerError, w.Code)
		body := w.Body.String()
		assert.Contains(t, body, "corrupt_file", "the SPA keys on the code, not the prose")
		assert.Contains(t, body, "bracket.json")
		assert.Contains(t, body, "47")
		assert.Contains(t, body, "invalid character")
		assert.NotContains(t, body, "internal error",
			"a repairable file must not be reported as an opaque failure")
	})

	t.Run("an I/O failure is NOT reported as corruption", func(t *testing.T) {
		// It is not something a text editor fixes, and its message carries the
		// absolute path this helper exists to withhold. The store only wraps
		// genuine parse failures, so such an error arrives here unwrapped and
		// falls through to the generic body.
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request, _ = http.NewRequest(http.MethodGet, "/api/competitions/abc", nil)

		internalError(c, errors.New("read /srv/tournament-data/competitions/abc/bracket.json: input/output error"))

		body := w.Body.String()
		assert.Contains(t, body, "internal error")
		assert.NotContains(t, body, "corrupt_file")
		assert.NotContains(t, body, "/srv/tournament-data")
	})
}
