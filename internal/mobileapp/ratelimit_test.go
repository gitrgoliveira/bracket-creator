package mobileapp

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

func TestAPIRateLimiter(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("Per-IP Burst Exhaustion", func(t *testing.T) {
		// generous global, strict per-IP
		limiter := NewAPIRateLimiter(1000, 1000)
		// override per-IP for test: 0 refill rate, burst 2
		limiter.perIP.close() // Close the original one to avoid leak
		limiter.perIP = newPerIPLimiter(0, 2)
		t.Cleanup(limiter.Close)

		r := gin.New()
		r.Use(limiter.Middleware())
		r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

		// Client A: First 2 requests should pass (burst = 2)
		for i := 0; i < 2; i++ {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.RemoteAddr = "1.1.1.1:1234"
			r.ServeHTTP(w, req)
			assert.Equal(t, 200, w.Code)
			assert.Equal(t, "ok", w.Body.String())
		}

		// Client A: 3rd request immediately should fail (burst exhausted)
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "1.1.1.1:1234"
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)

		// Client B: Even though A is exhausted, B should still pass (IP isolation)
		w2 := httptest.NewRecorder()
		req2, _ := http.NewRequest("GET", "/test", nil)
		req2.RemoteAddr = "2.2.2.2:1234"
		r.ServeHTTP(w2, req2)
		assert.Equal(t, 200, w2.Code)
		assert.Equal(t, "ok", w2.Body.String())
	})

	t.Run("Global Burst Exhaustion", func(t *testing.T) {
		// strict global (burst 3), generous per-IP (default)
		limiter := NewAPIRateLimiter(0, 3)
		t.Cleanup(limiter.Close)

		r := gin.New()
		r.Use(limiter.Middleware())
		r.GET("/test", func(c *gin.Context) { c.String(200, "ok") })

		// Send 3 requests from different IPs (should pass)
		for i := 0; i < 3; i++ {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/test", nil)
			req.RemoteAddr = string(rune('1'+i)) + ".1.1.1:1234"
			r.ServeHTTP(w, req)
			assert.Equal(t, 200, w.Code)
			assert.Equal(t, "ok", w.Body.String())
		}

		// 4th request from a fresh IP fails because global limit is exhausted
		w := httptest.NewRecorder()
		req, _ := http.NewRequest("GET", "/test", nil)
		req.RemoteAddr = "9.9.9.9:1234"
		r.ServeHTTP(w, req)
		assert.Equal(t, http.StatusTooManyRequests, w.Code)
	})

	t.Run("Idempotent Close", func(t *testing.T) {
		limiter := NewAPIRateLimiter(100, 100)
		assert.NotPanics(t, func() {
			limiter.Close()
		})
		assert.NotPanics(t, func() {
			limiter.Close()
		})
	})
}
