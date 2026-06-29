package cmd

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/mobileapp"

	"github.com/spf13/cobra"
)

type serveOptions struct {
	bindAddress string
	port        int
}

var downloadStatus sync.Map

func newServeCmd() *cobra.Command {
	o := &serveOptions{}

	cmd := &cobra.Command{
		Use:          "serve",
		Short:        "serves a web gui",
		SilenceUsage: true,
		RunE:         o.run,
	}

	bindAddress := os.Getenv("BIND_ADDRESS")
	if bindAddress == "" {
		bindAddress = "localhost" // default value
	}
	cmd.Flags().StringVarP(&o.bindAddress, "bind", "b", bindAddress, "bind address (env: BIND_ADDRESS)")

	portStr := os.Getenv("PORT")
	port := 8080 // default value
	if portStr != "" {
		var err error
		port, err = strconv.Atoi(portStr)
		if err != nil {
			fmt.Println("Warning: Invalid PORT environment variable. Using default.")
			port = 8080
		}
	}
	cmd.Flags().IntVarP(&o.port, "port", "p", port, "port number (env: PORT)")

	return cmd
}

func (o *serveOptions) run(cmd *cobra.Command, args []string) error {
	r := NewRouter()
	return r.Run(o.bindAddress + ":" + strconv.Itoa(o.port))
}

func NewRouter() *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	// Enable CORS
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Origin, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// Get web directory, fall back to the global helper.WebFs if appResources
	// has not been set (e.g. during integration tests that call NewRouter directly).
	var webFS fs.FS = helper.WebFs
	if res := GetResources(); res != nil {
		webFS = res.GetWebFS()
	}
	webDir, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Printf("Warning: web directory not found in WebFs, static files will not be served: %v", err)
	} else {
		// Serve static files
		r.StaticFS("/static", http.FS(webDir))
		// Serve index.html file directly from the root path
		r.GET("/", func(c *gin.Context) {
			data, err := fs.ReadFile(webDir, "index.html")
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not read index.html"})
				return
			}
			c.Data(http.StatusOK, "text/html; charset=utf-8", data)
		})
	}

	// Stateless schedule estimator, shared between the CLI web UI and
	// the mobile app frontend (T152a). Routed under /api so the same
	// fetch path works in both server modes. Uses gin.Engine.Group("/")
	// to obtain a RouterGroup pointer to pass to the shared registrar.
	mobileapp.RegisterScheduleHandlers(r.Group("/api"))

	// Setup API endpoints first
	r.GET("/api/status", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":    "ok",
			"version":   version.GetVersion(),
			"buildDate": version.GetBuildDate(),
			"goVersion": version.GetGoVersion(),
			"osArch":    version.GetOsArch(),
		})
	})

	r.GET("/api/download-status", func(c *gin.Context) {
		downloadToken := c.Query("token")
		if downloadToken == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "token is required"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"ready": consumeDownloadReady(downloadToken)})
	})

	r.POST("/api/parse-participants", func(c *gin.Context) {
		var req struct {
			PlayerList     string `json:"playerList"`
			WithZekkenName bool   `json:"withZekkenName"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
			return
		}

		rawEntries := strings.Split(req.PlayerList, "\n")
		if dups := helper.CheckDuplicateEntries(rawEntries); len(dups) > 0 {
			c.JSON(http.StatusBadRequest, gin.H{
				"error":      fmt.Sprintf("Duplicate participant entries: %s", strings.Join(dups, ", ")),
				"duplicates": dups,
			})
			return
		}
		players, err := helper.CreatePlayers(rawEntries, req.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		var participants []gin.H
		for _, p := range players {
			participants = append(participants, gin.H{
				"name":        p.Name,
				"displayName": p.DisplayName,
				"dojo":        p.Dojo,
			})
		}
		c.JSON(http.StatusOK, gin.H{"participants": participants})
	})

	// Add a redirect for POST requests to the root endpoint (for backward compatibility)
	r.POST("/", func(c *gin.Context) {
		c.Redirect(http.StatusMovedPermanently, "/create")
	})

	// Tournament generation endpoint, handler shared with the mobile-app
	// server (registered in cmd/mobile_app.go) so both run the same generator.
	r.POST("/create", createTournamentHandler)

	return r
}

func init() {
	rootCmd.AddCommand(newServeCmd())
}

// downloadReadyTTL bounds how long an unconsumed download-ready token lingers
// in downloadStatus. The serve web app consumes the token via the
// /api/download-status poll (consumeDownloadReady deletes it), but a client
// that navigates away, or the mobile-app server, which mounts /create without
// that poll endpoint, would otherwise leak the entry forever. The TTL keeps
// the map bounded regardless of which server runs the handler.
const downloadReadyTTL = 60 * time.Second

func markDownloadReady(token string) {
	downloadStatus.Store(token, true)
	time.AfterFunc(downloadReadyTTL, func() { downloadStatus.Delete(token) })
}

func consumeDownloadReady(token string) bool {
	if _, ok := downloadStatus.LoadAndDelete(token); ok {
		return true
	}

	return false
}
