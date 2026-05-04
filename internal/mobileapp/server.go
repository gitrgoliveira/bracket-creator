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

func NewRouter(store *state.Store, eng *engine.Engine, res *resources.Resources) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

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
	}

	// Admin API endpoints (protected)
	admin := r.Group("/api")
	admin.Use(AuthMiddleware(store))
	{
		RegisterTournamentHandlers(admin, store, hub)
		RegisterCompetitionHandlers(admin, store, eng, hub)
		RegisterParticipantHandlers(admin, store)
		RegisterMatchHandlers(admin, store, eng, hub)
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
