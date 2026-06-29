package mobileapp

import (
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// AdminPasswordHashEnv is the env var holding the bcrypt hash of the
// elevated (destructive-ops) password in locked mode (spec 004 / mp-e21).
// It is the elevated-credential analogue of TOURNAMENT_PASSWORD_HASH.
const AdminPasswordHashEnv = "TOURNAMENT_ADMIN_PASSWORD_HASH"

// defaultElevatedVerifier derives the elevated-password verifier from the
// main verifier's mode (spec 004). File mode reads the write-only
// Tournament.AdminPassword from the store (no env var); locked mode reads
// the bcrypt hash from TOURNAMENT_ADMIN_PASSWORD_HASH, falling back to the
// fail-closed unconfigured verifier (503 on gated endpoints) when the env
// var is absent or malformed. Reading the env here, rather than threading
// an explicit param through NewRouter, keeps the router signature stable
// for the many existing callers; file-mode tests never touch the env.
func defaultElevatedVerifier(verifier PasswordVerifier, store *state.Store) ElevatedVerifier {
	if verifier != nil && verifier.Mode() == "locked" {
		if v, err := NewBcryptElevatedVerifier(os.Getenv(AdminPasswordHashEnv)); err == nil {
			return v
		}
		slog.Warn("mobile-app: locked mode without a valid " + AdminPasswordHashEnv +
			"; destructive operations will return 503 until it is set")
		return NewLockedUnconfiguredElevatedVerifier()
	}
	return NewFileElevatedVerifier(store)
}

// NewRouter wires the mobile-app gin engine. The returned *gin.Engine
// is the HTTP handler; the returned *Hub is exposed so the caller
// (cmd/mobile_app.go) can call Hub.Close() from a graceful-shutdown
// hook, without that, http.Server.Shutdown would block forever on
// the long-lived SSE goroutines. The returned *APIRateLimiter should
// also be closed on shutdown to stop the per-IP cleanup goroutine.
func NewRouter(store *state.Store, eng *engine.Engine, res *resources.Resources, verifier PasswordVerifier) (*gin.Engine, *Hub, *APIRateLimiter) {
	return NewRouterWithHub(store, eng, res, verifier, NewHub(), false)
}

// NewRouterWithHub is the testable / configurable variant, pass a
// pre-built Hub (e.g. one with NewHubWithLimits) instead of constructing
// the default. cmd/mobile_app.go uses this to apply the SSE_MAX_CLIENTS
// override; tests use it to inject a small-capacity hub.
// scheduleEnabled is sourced from ENABLE_TOURNAMENT_SCHEDULE (mp-fwce) and
// forwarded to RegisterAuthConfigHandlers.
func NewRouterWithHub(store *state.Store, eng *engine.Engine, res *resources.Resources, verifier PasswordVerifier, hub *Hub, scheduleEnabled bool) (*gin.Engine, *Hub, *APIRateLimiter) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	if verifier == nil {
		verifier = NewFileVerifier(store)
	}

	// Elevated (destructive-ops) password verifier, spec 004 / mp-e21.
	// Derived from the main verifier's mode; see defaultElevatedVerifier.
	elevated := defaultElevatedVerifier(verifier, store)

	// Enable CORS
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Tournament-Password, X-Admin-Password")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	rateLimitVal := 5000.0
	if val, exists := os.LookupEnv("API_RATE_LIMIT"); exists {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil && parsed > 0 {
			rateLimitVal = parsed
		} else {
			slog.Warn("mobile-app: invalid API_RATE_LIMIT (must be > 0), falling back to default", "val", val)
		}
	}

	burstVal := 10000
	if val, exists := os.LookupEnv("API_RATE_LIMIT_BURST"); exists {
		if parsed, err := strconv.Atoi(val); err == nil && parsed > 0 {
			burstVal = parsed
		} else {
			slog.Warn("mobile-app: invalid API_RATE_LIMIT_BURST (must be > 0), falling back to default", "val", val)
		}
	}

	slog.Info("mobile-app: api rate limit configured", "globalRate", rateLimitVal, "globalBurst", burstVal, "perIPRate", DefaultPerIPRate, "perIPBurst", DefaultPerIPBurst)

	// Two-layer rate limiting for API endpoints:
	//   1. Per-IP: prevents a single client from starving others (100 req/s default)
	//   2. Global: circuit breaker for total server capacity (5000 req/s default)
	// Both layers are zero-config with automatic cleanup of idle IP entries.
	apiLimiter := NewAPIRateLimiter(rateLimitVal, burstVal)
	apiRateLimiter := apiLimiter.Middleware()
	r.Use(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/api/") {
			apiRateLimiter(c)
		} else {
			c.Next()
		}
	})

	// SSE Events endpoint
	r.GET("/api/events", hub.HandleEvents())

	// Public viewer endpoints
	viewer := r.Group("/api/viewer")
	{
		RegisterViewerHandlers(viewer, store, eng)
		RegisterDisplayHandlers(viewer, store)
	}

	// Stateless schedule estimator, no auth, no state-store access.
	// Registered directly under /api so the path matches the canonical
	// CLI web-server route exactly (T147a, T152a). Shared by both
	// `make run` and `make run-mobile` frontends.
	api := r.Group("/api")
	RegisterScheduleHandlers(api)
	RegisterVersionHandlers(api)

	// Public read-only endpoints for resources whose GET is unauthenticated
	// (same contract as /api/viewer/*). The write paths for each are on the
	// admin group below.
	//
	// GET /competitions/:id/competitor-status, eligibility state is
	// derivable from public match results; viewer/display surfaces need it
	// without admin credentials.
	// GET /competitions/:id/teams/:tid/lineups/:round, lineup assignments
	// are visible to coaches and spectators; AdminLineup loads them before
	// the operator has entered the admin password.
	RegisterPublicEligibilityHandlers(api, store)
	RegisterPublicLineupHandlers(api, store)
	RegisterPublicSwissHandlers(api, store, eng)
	RegisterPublicAnnouncementHandlers(api, store)
	RegisterPublicRegistrationHandlers(api, store, hub)
	RegisterPublicSponsorHandlers(api, store)
	RegisterPublicBrandingHandlers(api, store)
	RegisterPublicLeagueTiebreakHandlers(api, eng, store)

	// Public password-reset + auth-config endpoints. Both must live
	// outside the admin group; /reset is the recovery path for a
	// forgotten admin password (so requiring the password to use it
	// would be useless), and /auth-config lets the SPA discover whether
	// reset is enabled (locked mode disables it). Both 404 / return
	// inert payloads when locked mode is active, see handlers_reset.go
	// and handlers_auth_config.go.
	RegisterResetHandlers(api, store, verifier, hub)
	RegisterAuthConfigHandlers(api, verifier, elevated, scheduleEnabled)

	// Admin API endpoints (protected). Split into three sub-groups by
	// expected body size so the body cap fires BEFORE AuthMiddleware at
	// the right granularity for each endpoint tier:
	//
	//   adminTinyBody  (4 KB), /tournament/announce
	//   adminSmallBody (1 MB), all other admin JSON endpoints
	//   adminLargeBody (64 MB), /tournament/import (CSV upload)
	//
	// Use adminGroup() to wire each group; it enforces the cap→auth ordering
	// so new groups can't accidentally reverse it.
	adminTinyBody := adminGroup(r, AnnouncementMaxBodyBytes, verifier, store)
	RegisterAnnouncementHandlers(adminTinyBody, store, hub)

	adminSmallBody := adminGroup(r, DefaultMaxBodyBytes, verifier, store)
	RegisterTournamentHandlers(adminSmallBody, store, hub, verifier)
	RegisterAdminPasswordHandler(adminSmallBody, store, elevated)
	RegisterCompetitionHandlers(adminSmallBody, store, eng, hub, elevated)
	RegisterParticipantHandlers(adminSmallBody, store, eng, hub, elevated)
	RegisterMatchHandlers(adminSmallBody, eng, store, store, hub, verifier, store)
	RegisterDecisionHandlers(adminSmallBody, eng, store, store, hub)
	RegisterEligibilityHandlers(adminSmallBody, store, hub)
	RegisterReinstateHandler(adminSmallBody, eng, hub)
	RegisterLineupHandlers(adminSmallBody, store, store, store, hub)
	RegisterDaihyosenHandlers(adminSmallBody, eng, store, hub)
	RegisterLeagueTiebreakHandlers(adminSmallBody, eng, store, hub)
	RegisterSwissHandlers(adminSmallBody, store, eng, hub)

	// PDF export, POST body is effectively empty (type in URL param only);
	// uses DefaultMaxBodyBytes for consistency with the other admin JSON tier.
	RegisterPrintHandlers(adminSmallBody, eng)

	adminLargeBody := adminGroup(r, MaxImportBodyBytes, verifier, store)
	RegisterImportHandlers(adminLargeBody, store, hub, elevated)

	// Sponsor uploads (mp-c38), multipart logo upload needs envelope
	// headroom for the file plus boundary/form-field overhead; so it gets
	// its own 2 MB group separate from the 1 MB JSON tier. DELETE rides
	// on the same group (DELETE skips the cap by method anyway).
	adminSponsorBody := adminGroup(r, SponsorMaxBodyBytes, verifier, store)
	RegisterSponsorHandlers(adminSponsorBody, store)

	// Tournament branding logo (mp-scf), same 2 MB envelope as sponsors.
	adminBrandingBody := adminGroup(r, BrandingMaxBodyBytes, verifier, store)
	RegisterBrandingHandlers(adminBrandingBody, store)

	// Static files & SPA Fallback
	mobileFS := res.GetMobileWebFS()
	subFS, err := fs.Sub(mobileFS, "web-mobile")
	if err != nil {
		log.Printf("Warning: web-mobile directory not found: %v", err)
	} else {
		// Custom handler to serve from embedded FS with SPA fallback
		r.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path

			// API routes should not fallback to index.html
			if strings.HasPrefix(path, "/api/") {
				c.JSON(http.StatusNotFound, gin.H{"error": "API route not found"})
				return
			}

			// Try to serve file from embedded FS
			filePath := strings.TrimPrefix(path, "/")
			if filePath == "" {
				filePath = "index.html"
			}

			// Check if file exists in FS
			_, err := fs.Stat(subFS, filePath)
			if err == nil {
				// File exists, serve it
				fileServer := http.FileServer(http.FS(subFS))
				fileServer.ServeHTTP(c.Writer, c.Request)
				return
			}

			// Browser-build rewrite: source .jsx files (web-mobile/js/*.jsx)
			// import siblings via `./X.jsx` paths. esbuild compiles to
			// .js (web-mobile/dist/*.js) but does NOT rewrite the import
			// strings, so a browser's `import "./X.jsx"` falls through to
			// here looking for a non-existent `dist/X.jsx`. Map to the
			// compiled `.js` sibling. Without this rewrite the SPA fails
			// to mount because every entry chunk has an unresolved
			// `.jsx` import. Vitest tests pass because Node-side resolves
			// `.jsx` to the source file directly.
			if strings.HasPrefix(filePath, "dist/") && strings.HasSuffix(filePath, ".jsx") {
				rewritten := strings.TrimSuffix(filePath, ".jsx") + ".js"
				if _, err := fs.Stat(subFS, rewritten); err == nil {
					c.Request.URL.Path = "/" + rewritten
					http.FileServer(http.FS(subFS)).ServeHTTP(c.Writer, c.Request)
					return
				}
			}

			// If it's a sub-route (SPA), serve index.html
			// (but only if it doesn't look like a file request with an extension)
			ext := filepath.Ext(filePath)
			if ext == "" || ext == ".html" {
				data, err := fs.ReadFile(subFS, "index.html")
				if err == nil {
					c.Data(http.StatusOK, "text/html; charset=utf-8", data)
					return
				}
			}

			c.String(http.StatusNotFound, "Not found")
		})
	}

	return r, hub, apiLimiter
}
