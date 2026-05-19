package mobileapp

import (
	"io/fs"
	"log"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/resources"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func NewRouter(store *state.Store, eng *engine.Engine, res *resources.Resources, verifier PasswordVerifier) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	if verifier == nil {
		verifier = NewFileVerifier(store)
	}

	// Enable CORS
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, X-Tournament-Password")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	hub := NewHub()

	// Health check
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// SSE Events endpoint
	r.GET("/api/events", hub.HandleEvents())

	// Public viewer endpoints
	viewer := r.Group("/api/viewer")
	{
		RegisterViewerHandlers(viewer, store, eng)
		RegisterDisplayHandlers(viewer, store)
	}

	// Stateless schedule estimator — no auth, no state-store access.
	// Registered directly under /api so the path matches the canonical
	// CLI web-server route exactly (T147a, T152a). Shared by both
	// `make run` and `make run-mobile` frontends.
	api := r.Group("/api")
	RegisterScheduleHandlers(api)

	// Public read-only endpoints for resources whose GET is unauthenticated
	// (same contract as /api/viewer/*). The write paths for each are on the
	// admin group below.
	//
	// GET /competitions/:id/competitor-status — eligibility state is
	// derivable from public match results; viewer/display surfaces need it
	// without admin credentials.
	// GET /competitions/:id/teams/:tid/lineups/:round — lineup assignments
	// are visible to coaches and spectators; AdminLineup loads them before
	// the operator has entered the admin password.
	RegisterPublicEligibilityHandlers(api, store)
	RegisterPublicLineupHandlers(api, store)
	RegisterPublicSwissHandlers(api, store, eng)

	// Public password-reset + auth-config endpoints. Both must live
	// outside the admin group: /reset is the recovery path for a
	// forgotten admin password (so requiring the password to use it
	// would be useless), and /auth-config lets the SPA discover whether
	// reset is enabled (locked mode disables it). Both 404 / return
	// inert payloads when locked mode is active — see handlers_reset.go
	// and handlers_auth_config.go.
	RegisterResetHandlers(api, store, verifier, hub)
	RegisterAuthConfigHandlers(api, verifier)

	// Admin API endpoints (protected)
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(verifier, store))
	{
		RegisterTournamentHandlers(admin, store, hub, verifier)
		RegisterImportHandlers(admin, store, hub)
		RegisterCompetitionHandlers(admin, store, eng, hub)
		RegisterParticipantHandlers(admin, store)
		RegisterMatchHandlers(admin, eng, store, store, hub)
		RegisterDecisionHandlers(admin, eng, store, store, hub)
		RegisterEligibilityHandlers(admin, store, hub)
		RegisterReinstateHandler(admin, eng, hub)
		RegisterLineupHandlers(admin, store, store, store)
		RegisterDaihyosenHandlers(admin, eng, store, hub)
		RegisterSwissHandlers(admin, store, eng, hub)
	}

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
			// strings — so a browser's `import "./X.jsx"` falls through to
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

	return r
}
