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

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// retiredAdminPasswordHashEnv held the bcrypt hash of the destructive-ops
// password in locked mode. That password was removed (operator ruling
// 2026-10-12: an admin-level action needs the admin sign-in alone), so the
// variable is read only to say it is ignored: a deployment that still sets
// it starts and works exactly as one that does not.
const retiredAdminPasswordHashEnv = "TOURNAMENT_ADMIN_PASSWORD_HASH"

// mobileWebRoot is the embedded-resources subdirectory the mobile-app SPA is
// served from. Shared between the fs.Sub call below and FrontendBundleMissing
// (frontend_bundle.go) so the two can't drift on which root they check.
const mobileWebRoot = "web-mobile"

// noteRetiredAdminPasswordHash logs, once per router, that
// TOURNAMENT_ADMIN_PASSWORD_HASH is set but no longer used. It never fails
// startup: the value is simply ignored.
func noteRetiredAdminPasswordHash() {
	if os.Getenv(retiredAdminPasswordHashEnv) != "" {
		slog.Info("mobile-app: " + retiredAdminPasswordHashEnv +
			" is set but no longer used; admin actions need only the admin password")
	}
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

	noteRetiredAdminPasswordHash()

	// Enable CORS
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Tournament-Password, X-Confirm-Password")
		// Every answer names the build that gave it (bc-evts F2).
		c.Writer.Header().Set(BuildHeader, AppBuild())
		c.Writer.Header().Set("Access-Control-Expose-Headers", BuildHeader)

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
	// GET /competitions/:id/teams/:tid/lineups/:round,
	// .../match-lineups/:matchId and .../lineup-in-force/:matchId, lineup
	// assignments are visible to coaches and spectators; AdminLineup loads
	// them before the operator has entered the admin password.
	RegisterPublicEligibilityHandlers(api, store)
	RegisterPublicLineupHandlers(api, store, store, eng)
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
	RegisterAuthConfigHandlers(api, verifier, scheduleEnabled)

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
	// Deleting a competition asks for the password again (X-Confirm-Password);
	// this middleware judges only that one route, after the sign-in.
	adminSmallBody.Use(RequireConfirmPasswordForDelete(verifier))
	RegisterCompetitionHandlers(adminSmallBody, store, eng, hub)
	RegisterParticipantHandlers(adminSmallBody, store, eng, hub)
	RegisterMatchHandlers(adminSmallBody, eng, store, store, hub, verifier, store)
	RegisterMatchHistoryHandler(adminSmallBody, store)
	RegisterDecisionHandlers(adminSmallBody, eng, store, store, hub)
	RegisterEligibilityHandlers(adminSmallBody, store, hub)
	RegisterReinstateHandler(adminSmallBody, eng, hub)
	RegisterLineupHandlers(adminSmallBody, store, store, store, hub, store, verifier)
	RegisterSquadHandlers(adminSmallBody, store, store, hub, store, verifier)
	RegisterDaihyosenHandlers(adminSmallBody, eng, store, hub, store, verifier)
	RegisterLeagueTiebreakHandlers(adminSmallBody, eng, store, hub)
	RegisterSwissHandlers(adminSmallBody, store, eng, hub)

	// PDF export, POST body is effectively empty (type in URL param only);
	// uses DefaultMaxBodyBytes for consistency with the other admin JSON tier.
	RegisterPrintHandlers(adminSmallBody, eng)
	RegisterExportResultsHandlers(adminSmallBody, store, eng)

	adminLargeBody := adminGroup(r, MaxImportBodyBytes, verifier, store)
	RegisterImportHandlers(adminLargeBody, store, eng, hub)

	// Sponsor uploads (mp-c38), multipart logo upload needs envelope
	// headroom for the file plus boundary/form-field overhead; so it gets
	// its own 2 MB group separate from the 1 MB JSON tier. DELETE rides
	// on the same group; a DELETE sends no body, and one that did would be
	// capped like a POST (MaxBodyBytes).
	adminSponsorBody := adminGroup(r, SponsorMaxBodyBytes, verifier, store)
	RegisterSponsorHandlers(adminSponsorBody, store, hub)

	// Tournament branding logo (mp-scf), same 2 MB envelope as sponsors.
	adminBrandingBody := adminGroup(r, BrandingMaxBodyBytes, verifier, store)
	RegisterBrandingHandlers(adminBrandingBody, store, hub)

	// Static files & SPA Fallback
	mobileFS := res.GetMobileWebFS()
	subFS, err := fs.Sub(mobileFS, mobileWebRoot)
	if err != nil {
		log.Printf("Warning: web-mobile directory not found: %v", err)
	} else {
		// One validator per ROUTER, computed on first use rather than at
		// startup, and captured by the handler below. sync.OnceValue keeps the
		// laziness (nothing hashes unless a static asset is actually requested)
		// without the package-level Once+string this used to need: those made a
		// per-router value process-global, so a second FS root or an alternate
		// asset dir would silently share one validator, and every test touching
		// the headers had to reset package state or leak a consumed memo into
		// whatever ran next.
		assetETag := sync.OnceValue(func() string { return buildAssetETag(subFS) })
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
					serveIndexHTML(c, data, store, assetPrefix(assetETag()))
					return
				}
			}

			// A module under a build's own directory, /dist/<prefix>/ (see
			// staticCacheControlScoped): the current build's is served from
			// dist/ with the prefix stripped, and any other is a 404, never the
			// page, which a module import cannot use.
			scoped := false
			if rest, ok := strings.CutPrefix(filePath, "dist/"); ok {
				if seg, tail, found := strings.Cut(rest, "/"); found && isAssetPrefixSegment(seg) {
					if current := assetPrefix(assetETag()); current == "" || seg != current {
						c.String(http.StatusNotFound, "Not found")
						return
					}
					filePath, scoped = "dist/"+tail, true
					c.Request.URL.Path = "/" + filePath
				}
			}

			// Check if file exists in FS
			_, err := fs.Stat(subFS, filePath)
			if err == nil {
				// File exists, serve it. http.ServeContent honours the ETag we
				// set here against the request's If-None-Match, so an unchanged
				// build answers 304 without a body.
				setStaticCacheHeaders(c, assetETag(), filePath, scoped)
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
					setStaticCacheHeaders(c, assetETag(), rewritten, scoped)
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
					serveIndexHTML(c, data, store, assetPrefix(assetETag()))
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
// It also names the build's own module directory (assetPrefix), served
// immutable (staticCacheControlScoped); the ETag turns any revalidation into a
// 0-byte 304.
//
// Before this, http.FileServer over embed.FS emitted no Cache-Control, no ETag
// and no Last-Modified (embed's zero modtime makes ServeContent omit it), so a
// browser had nothing to revalidate against and re-downloaded the whole
// front-end on EVERY page load — measured at 1.17 MB across 69 files, none
// served from cache. The "?v=N" query strings on some script tags were busting
// a cache that never existed.
func buildAssetETag(subFS fs.FS) string {
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
		// No validator is better than a wrong one: returning "" makes
		// setStaticCacheHeaders skip the ETag, so responses simply go back
		// to being uncacheable rather than cacheable-and-stale.
		log.Printf("mobileapp: hashing /dist for the asset ETag failed, assets will not be cacheable: %v", err)
		return ""
	}
	return fmt.Sprintf("%q", hex.EncodeToString(h.Sum(nil))[:16])
}

// staticCacheControlScoped is the caching policy for a compiled module served
// from the build's own directory, /dist/<prefix>/, where <prefix> is the content
// hash of the whole front-end (assetPrefix, buildAssetETag: never commit.txt,
// which two development builds of one commit share). The page is no-store and
// names every module there (withAssetPrefix), and every import inside a module
// is relative, so a module's imports resolve under the same prefix: a module at
// such a path never changes, and a load makes no request for one it holds
// (bc-evts stage 3 review 3, F3). That keeps both rulings: the page and its code
// always come from one build (operator decision S2 a; within the old five-minute
// window a reload paired the new page with the previous build's modules, review
// 2 R7), and a load costs no round of conditional requests (mp-6o1a, 34d358a2:
// "no-cache" made one per module, 69 per load, on a slow link). A prefix that is
// not the current build's is a 404 (NewRouterWithHub), never the page.
//
// staticCacheControlUnscoped is the policy for a plain /dist/ path, which only a
// page whose content hash could not be made names: the old five-minute window.

// setStaticCacheHeaders applies the caching policy to a compiled asset: scoped
// when it was asked for under the build's own directory.
// Only /dist/ is covered: index.html is deliberately no-store (it carries
// server-rendered meta tags), and uploaded branding/sponsor images set their
// own policy in their handlers.
func setStaticCacheHeaders(c *gin.Context, etag, filePath string, scoped bool) {
	if !strings.HasPrefix(filePath, "dist/") {
		return
	}
	if etag == "" {
		return
	}
	if scoped {
		c.Header("Cache-Control", staticCacheControlScoped)
	} else {
		c.Header("Cache-Control", staticCacheControlUnscoped)
	}
	c.Header("ETag", etag)
}

// assetPrefixSegmentLen is the length of a build-scoped module directory's
// name: buildAssetETag's 16 hex characters.
const assetPrefixSegmentLen = 16

// assetPrefix is the name of the build's own module directory, /dist/<prefix>/:
// the content hash of the compiled front-end (buildAssetETag) without its
// quotes, "" when it could not be made.
func assetPrefix(etag string) string {
	return strings.Trim(etag, `"`)
}

// isAssetPrefixSegment reports whether a path segment under /dist/ has the
// shape of a build's module directory (16 lowercase hex characters). web-mobile/dist
// has no subdirectories of its own, so such a segment is never a real folder.
func isAssetPrefixSegment(seg string) bool {
	if len(seg) != assetPrefixSegmentLen {
		return false
	}
	for _, r := range seg {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

const (
	staticCacheControlScoped   = "public, max-age=31536000, immutable"
	staticCacheControlUnscoped = "public, max-age=300"
)
