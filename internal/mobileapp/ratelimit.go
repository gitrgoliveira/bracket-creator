package mobileapp

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

// Default per-IP rate limit constants. These are intentionally generous —
// no legitimate tournament user (viewer refreshing, admin scoring rapidly)
// comes close to 100 req/s from a single device. The per-IP layer exists
// to prevent a single misbehaving client from exhausting the global budget
// and starving everyone else.
const (
	DefaultPerIPRate  = 100 // requests per second per IP
	DefaultPerIPBurst = 200 // burst allowance per IP
	ipCleanupInterval = 3 * time.Minute
	ipIdleTimeout     = 5 * time.Minute
)

// ipEntry pairs a limiter with the last time it was accessed, so the
// cleanup goroutine can evict idle entries and keep the map bounded.
type ipEntry struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// perIPLimiter holds per-client rate limiters keyed by IP address.
// A background goroutine evicts entries that haven't been seen in
// ipIdleTimeout, preventing unbounded map growth from transient clients.
type perIPLimiter struct {
	mu      sync.Mutex
	clients map[string]*ipEntry
	rate    float64
	burst   int
	stop    chan struct{}
}

// newPerIPLimiter creates a per-IP rate limiter and starts the background
// cleanup goroutine. Call close() when the server shuts down.
func newPerIPLimiter(r float64, burst int) *perIPLimiter {
	p := &perIPLimiter{
		clients: make(map[string]*ipEntry),
		rate:    r,
		burst:   burst,
		stop:    make(chan struct{}),
	}
	go p.cleanupLoop()
	return p
}

// allow checks whether the request from the given IP is within its
// per-client budget. Creates a new limiter on first sight.
func (p *perIPLimiter) allow(ip string) bool {
	p.mu.Lock()
	entry, exists := p.clients[ip]
	if !exists {
		entry = &ipEntry{
			limiter: rate.NewLimiter(rate.Limit(p.rate), p.burst),
		}
		p.clients[ip] = entry
	}
	entry.lastSeen = time.Now()
	p.mu.Unlock()

	return entry.limiter.Allow()
}

// cleanupLoop periodically evicts IP entries that haven't been seen
// recently. This keeps memory bounded — even if 10,000 unique IPs
// hit the server in a burst, idle ones are reaped within minutes.
func (p *perIPLimiter) cleanupLoop() {
	ticker := time.NewTicker(ipCleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			p.mu.Lock()
			cutoff := time.Now().Add(-ipIdleTimeout)
			for ip, entry := range p.clients {
				if entry.lastSeen.Before(cutoff) {
					delete(p.clients, ip)
				}
			}
			p.mu.Unlock()
		case <-p.stop:
			return
		}
	}
}

// close stops the cleanup goroutine.
func (p *perIPLimiter) close() {
	close(p.stop)
}

// APIRateLimiter combines a global circuit breaker with per-IP fairness.
// Both layers are zero-config with sensible defaults:
//
//   - Global: prevents total server overload (default 5000 req/s).
//   - Per-IP: prevents a single client from starving others (default 100 req/s).
//
// A request must pass BOTH checks. The per-IP check runs first (cheaper
// to reject a known-bad client before touching the shared global bucket).
type APIRateLimiter struct {
	global *rate.Limiter
	perIP  *perIPLimiter
}

// NewAPIRateLimiter creates the two-layer rate limiter. The caller should
// call Close() on shutdown to stop the per-IP cleanup goroutine.
func NewAPIRateLimiter(globalRate float64, globalBurst int) *APIRateLimiter {
	return &APIRateLimiter{
		global: rate.NewLimiter(rate.Limit(globalRate), globalBurst),
		perIP:  newPerIPLimiter(DefaultPerIPRate, DefaultPerIPBurst),
	}
}

// Close stops the background cleanup goroutine.
func (rl *APIRateLimiter) Close() {
	rl.perIP.close()
}

// Middleware returns a gin.HandlerFunc that enforces both layers.
func (rl *APIRateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		// Per-IP first — reject known-bad clients cheaply before
		// consuming a token from the shared global bucket.
		if !rl.perIP.allow(ip) {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests",
			})
			return
		}

		// Global circuit breaker.
		if !rl.global.Allow() {
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"error": "too many requests",
			})
			return
		}

		c.Next()
	}
}
