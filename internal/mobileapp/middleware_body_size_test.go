package mobileapp

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func newBodySizeTestRouter(limit int64) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(MaxBodyBytes(limit))
	r.POST("/echo", func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"len": len(body)})
	})
	r.GET("/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	return r
}

func TestMaxBodyBytes_FastPath_RejectsByContentLength(t *testing.T) {
	r := newBodySizeTestRouter(100)

	// 200-byte body, advertised via Content-Length, with limit=100.
	body := strings.Repeat("x", 200)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/echo", bytes.NewBufferString(body))
	req.ContentLength = int64(len(body))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code,
		"oversized body should be rejected up-front via Content-Length")
	assert.Contains(t, w.Body.String(), "request body too large")
}

func TestMaxBodyBytes_DefensiveWrap_RejectsWhenContentLengthHidden(t *testing.T) {
	r := newBodySizeTestRouter(50)

	// 200-byte body but Content-Length explicitly cleared so the fast
	// path doesn't fire. MaxBytesReader must catch it during read.
	body := strings.Repeat("x", 200)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/echo", bytes.NewBufferString(body))
	req.ContentLength = -1 // unknown
	r.ServeHTTP(w, req)

	// MaxBytesReader surfaces as a read error from GetRawData → 500
	// from the test handler. The point of this test is just to verify
	// the body cap is enforced at the read layer, not just at
	// Content-Length. A real production handler using BindJSON would
	// see the same error and respond appropriately.
	assert.NotEqual(t, http.StatusOK, w.Code,
		"oversized body should not succeed even when Content-Length is hidden")
}

func TestMaxBodyBytes_HappyPath_UnderLimit(t *testing.T) {
	r := newBodySizeTestRouter(1000)

	body := strings.Repeat("x", 500)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/echo", bytes.NewBufferString(body))
	req.ContentLength = int64(len(body))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Body.String(), `"len":500`)
}

func TestMaxBodyBytes_SkipsBodylessMethods(t *testing.T) {
	r := newBodySizeTestRouter(1)

	// GET with a huge body (technically allowed by HTTP but the middleware
	// should skip the cap on GET since it has no semantic body). Limit=1
	// would reject if the middleware enforced on GET.
	body := strings.Repeat("x", 200)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/ping", bytes.NewBufferString(body))
	req.ContentLength = int64(len(body))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "GET should bypass body cap")
}

// TestMaxBodyBytes_FiresBeforeAuth pins the wiring contract used by
// server.go: the body cap middleware is installed BEFORE AuthMiddleware
// on each admin group, so an unauthenticated POST with an oversized
// Content-Length gets 413 — not 401. Regression test for the
// mp-663 Phase 3 acceptance criterion.
func TestMaxBodyBytes_FiresBeforeAuth(t *testing.T) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()

	// Order matches server.go: body cap, then auth.
	r.Use(MaxBodyBytes(100))
	r.Use(func(c *gin.Context) {
		// Stand-in for AuthMiddleware that always returns 401 — proves
		// the body cap fired first if the response is 413 instead.
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	})
	r.POST("/api/anything", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})

	body := strings.Repeat("x", 500)
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/anything", bytes.NewBufferString(body))
	req.ContentLength = int64(len(body))
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusRequestEntityTooLarge, w.Code,
		"oversized body must be rejected before auth runs; got %d (body: %s)", w.Code, w.Body.String())
}
