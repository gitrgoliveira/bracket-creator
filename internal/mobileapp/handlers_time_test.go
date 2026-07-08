package mobileapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// GET /api/time is the clock-offset source for timestamp reconciliation
// (mp-y3nk): a client stamps writes in server-relative time so an offline
// court's changes reconcile against other courts without trusting each
// tablet's local clock. The endpoint is stateless and unauthenticated.
func TestTimeHandler_ReturnsServerNowMillis(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	RegisterTimeHandlers(r.Group("/api"))

	before := time.Now().UnixMilli()
	req := httptest.NewRequest(http.MethodGet, "/api/time", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	after := time.Now().UnixMilli()

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		NowMs int64 `json:"nowMs"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// The reported time must fall within the window the request was served in.
	assert.GreaterOrEqual(t, resp.NowMs, before, "nowMs must be >= the time just before the request")
	assert.LessOrEqual(t, resp.NowMs, after, "nowMs must be <= the time just after the request")
	// no-store: a cached nowMs would skew every derived clock offset (mp-y3nk).
	assert.Equal(t, "no-store", w.Header().Get("Cache-Control"), "/api/time must be non-cacheable")
}
