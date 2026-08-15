package mobileapp

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

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
	// GET /api/time: server clock for offset learning (timestamp reconciliation).
	RegisterTimeHandlers(api)

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
	RegisterPublicLeagueHandlers(api, eng)
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
	RegisterExportResultsHandlers(adminSmallBody, store, eng)

	adminLargeBody := adminGroup(r, MaxImportBodyBytes, verifier, store)
	RegisterImportHandlers(adminLargeBody, store, hub, elevated)

	// Sponsor uploads (mp-c38), multipart logo upload needs envelope
	// headroom for the file plus boundary/form-field overhead; so it gets
	// its own 2 MB group separate from the 1 MB JSON tier. DELETE rides
	// on the same group (DELETE skips the cap by method anyway).
	adminSponsorBody := adminGroup(r, SponsorMaxBodyBytes, verifier, store)
	RegisterSponsorHandlers(adminSponsorBody, store, hub)

	// Tournament branding logo (mp-scf), same 2 MB envelope as sponsors.
	adminBrandingBody := adminGroup(r, BrandingMaxBodyBytes, verifier, store)
	RegisterBrandingHandlers(adminBrandingBody, store, hub)

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

			// index.html gets server-rendered link-preview meta tags injected
			// from live tournament state (mp-p9o8). Handle it before the generic
			// file server so the static bytes are never served bare.
			if filePath == "index.html" {
				if data, rerr := fs.ReadFile(subFS, "index.html"); rerr == nil {
					serveIndexHTML(c, data, store)
					return
				}
			}

			// Check if file exists in FS
			_, err := fs.Stat(subFS, filePath)
			if err == nil {
				// File exists, serve it. http.ServeContent honours the ETag we
				// set here against the request's If-None-Match, so an unchanged
				// build answers 304 without a body.
				setStaticCacheHeaders(c, subFS, filePath)
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
					setStaticCacheHeaders(c, subFS, rewritten)
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
					serveIndexHTML(c, data, store)
					return
				}
			}

			c.String(http.StatusNotFound, "Not found")
		})
	}

	return r, hub, apiLimiter
}

// buildAssetETag is the validator for the compiled front-end under /dist/.
//
// Derived from the CONTENT of the embedded assets, not from the build version.
// version.GetVersion() reads an embedded string that on a development branch is
// the branch name: it does not change when you rebuild, so using it here would
// serve a developer stale JavaScript after every recompile — the exact failure
// this policy exists to prevent. Hashing the bytes is correct in development
// and in release alike, and stays stable across restarts of the same binary.
//
// Computed once per process, lazily, on the first /dist/ request: one walk and
// SHA-256 over ~1.2 MB, single-digit milliseconds, off the startup path.
//
// Paired with Cache-Control: max-age (see staticAssetMaxAge) plus the ETag, so
// the two regimes compose: inside the window a repeat load makes NO request at
// all, and after it the ETag turns a re-download into a 0-byte 304.
//
// Before this, http.FileServer over embed.FS emitted no Cache-Control, no ETag
// and no Last-Modified (embed's zero modtime makes ServeContent omit it), so a
// browser had nothing to revalidate against and re-downloaded the whole
// front-end on EVERY page load — measured at 1.17 MB across 69 files, none
// served from cache. The "?v=N" query strings on some script tags were busting
// a cache that never existed.
func buildAssetETag(subFS fs.FS) string {
	assetETagOnce.Do(func() {
		h := sha256.New()
		// WalkDir is deterministic (lexical order), so the same tree always
		// hashes to the same value.
		err := fs.WalkDir(subFS, "dist", func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, rerr := fs.ReadFile(subFS, path)
			if rerr != nil {
				return rerr
			}
			_, _ = h.Write([]byte(path))
			_, _ = h.Write(b)
			return nil
		})
		if err != nil {
			// No validator is better than a wrong one: leaving it empty makes
			// setStaticCacheHeaders skip the ETag, so responses simply go back
			// to being uncacheable rather than cacheable-and-stale.
			log.Printf("mobileapp: hashing /dist for the asset ETag failed, assets will not be cacheable: %v", err)
			return
		}
		assetETag = fmt.Sprintf("%q", hex.EncodeToString(h.Sum(nil))[:16])
	})
	return assetETag
}

// staticAssetMaxAge is how long a client may reuse a compiled asset WITHOUT
// asking. It is the deliberate trade in this policy.
//
// Why not 0 (revalidate always, the safest setting): correctness would be
// perfect, but every page load still costs one conditional request per asset —
// 69 of them here. On a LAN that is invisible; on cellular or a hotel-grade
// operator laptop at ~100ms round-trip over HTTP/1.1 (six connections), it is
// roughly a dozen serialised waves before anything renders. Operators and
// spectators reported that latency as a real problem, so the window exists to
// remove those round trips entirely for a revisit.
//
// Why not hours: an asset cached under max-age is used WITHOUT asking, so a
// server upgraded mid-event is invisible to an already-loaded client until the
// window expires. Five minutes bounds that to something an operator would not
// notice, while covering the cases that actually recur — a tab reopened, a
// second console window, a spectator returning to the schedule between matches.
//
// The ETag still backs it: once the window lapses the client revalidates and
// gets a 0-byte 304 unless the bundle really changed, so this never costs a
// re-download. Content-hashed filenames plus "immutable" would remove the
// window entirely, but esbuild here runs without --bundle and does not rewrite
// import specifiers, so hashing the names would break every `./x.jsx` import.
const staticAssetMaxAge = 5 * time.Minute

// setStaticCacheHeaders applies the revalidation policy to a compiled asset.
// Only /dist/ is covered: index.html is deliberately no-store (it carries
// server-rendered meta tags), and uploaded branding/sponsor images set their
// own policy in their handlers.
func setStaticCacheHeaders(c *gin.Context, subFS fs.FS, filePath string) {
	if !strings.HasPrefix(filePath, "dist/") {
		return
	}
	etag := buildAssetETag(subFS)
	if etag == "" {
		return
	}
	c.Header("Cache-Control", fmt.Sprintf("public, max-age=%d", int(staticAssetMaxAge.Seconds())))
	c.Header("ETag", etag)
}

// assetETag caches the content hash of the embedded /dist tree for the life of
// the process; see buildAssetETag.
var (
	assetETagOnce sync.Once
	assetETag     string
)
