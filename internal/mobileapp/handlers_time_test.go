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
}

// clampClientModifiedAt rejects hostile/buggy client timestamps that would
// otherwise freeze a match against later legitimate writes (mp-y3nk, tri-review
// finding 4). Negative and far-future values fall back to 0 (unstamped ->
// arrival-order); a plausible value passes through unchanged.
func TestClampClientModifiedAt(t *testing.T) {
	now := time.Now().UnixMilli()
	assert.Equal(t, int64(0), clampClientModifiedAt(-1), "negative must clamp to 0")
	assert.Equal(t, int64(0), clampClientModifiedAt(now+modifiedAtMaxSkewMs+60_000), "far-future must clamp to 0")
	assert.Equal(t, int64(0), clampClientModifiedAt(0), "zero (unstamped) passes through as 0")
	assert.Equal(t, now-1000, clampClientModifiedAt(now-1000), "a recent past timestamp passes through")
	// A value inside the skew window is trusted (small legitimate clock drift).
	assert.Equal(t, now+1000, clampClientModifiedAt(now+1000), "a slightly-future value inside the skew window passes through")
}
