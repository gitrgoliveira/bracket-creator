package cmd

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"bufio"
	"bytes"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"

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
	cmd.Flags().StringVarP(&o.bindAddress, "bind", "b", bindAddress, "bind address")

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
	cmd.Flags().IntVarP(&o.port, "port", "p", port, "port number")

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

	// Get web directory
	webDir, err := fs.Sub(helper.WebFs, "web")
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

		players, err := helper.CreatePlayers(strings.Split(req.PlayerList, "\n"), req.WithZekkenName)
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

	// Set up tournament creation endpoint
	r.POST("/create", func(c *gin.Context) {
		text := c.PostForm("playerList")
		if text == "" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Player list cannot be empty",
			})
			return
		}

		// Parse form values
		singleTree := c.PostForm("singleTree") == "on"
		withZekkenName := c.PostForm("withZekkenName") == "on"
		determined := c.PostForm("determined") == "on"
		// Mirror defaults to true. Since checkboxes only send a value when checked, we use the
		// 'form_submitted' hidden field to distinguish between an unchecked box (mirror=false)
		// and an API call/initial load where the parameter is missing (mirror=true).
		mirror := c.PostForm("form_submitted") == "" || c.PostForm("mirror") == "on"

		teamMatches, err := strconv.Atoi(c.PostForm("teamMatches"))
		if err != nil {
			teamMatches = 0
		}

		tournamentType := c.PostForm("tournamentType")
		if tournamentType != "pools" && tournamentType != "playoffs" {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid tournament type",
			})
			return
		}

		winnersPerPool, err := strconv.Atoi(c.PostForm("winnersPerPool"))
		if err != nil {
			winnersPerPool = 2
		}

		playersPerPool, err := strconv.Atoi(c.PostForm("playersPerPool"))
		if err != nil {
			playersPerPool = 3
		}

		// Validate pool settings
		if tournamentType == "pools" {
			if winnersPerPool <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "Winners per pool must be at least 1",
				})
				return
			}

			if playersPerPool <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "Players per pool must be at least 1",
				})
				return
			}

			if winnersPerPool >= playersPerPool {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": "Winners per pool must be less than players per pool",
				})
				return
			}
		}

		roundRobin := c.PostForm("roundRobin") == "on"
		poolSizeMode := c.PostForm("poolSizeMode")

		// Parse courts (number of Shiaijo)
		courts, err := strconv.Atoi(c.PostForm("courts"))
		if err != nil || courts < 1 {
			courts = 2
		}

		// Parse seeds if provided
		var seedAssignments []domain.SeedAssignment
		seedsJSON := c.PostForm("seeds")
		if seedsJSON != "" {
			err := json.Unmarshal([]byte(seedsJSON), &seedAssignments)
			if err != nil {
				log.Printf("failed to parse seeds JSON: %s", err.Error())
				c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid seed assignments format"})
				return
			}
		}

		// Prepare output
		inMemoryBuffer := new(bytes.Buffer)
		inMemoryWriter := bufio.NewWriter(inMemoryBuffer)

		// Create tournament
		switch tournamentType {
		case "pools":
			var numPlayers, maxPlayers int
			if poolSizeMode == "max" {
				maxPlayers = playersPerPool
			} else {
				numPlayers = playersPerPool
			}

			o := &poolOptions{
				singleTree:      singleTree,
				withZekkenName:  withZekkenName,
				determined:      determined,
				teamMatches:     teamMatches,
				roundRobin:      roundRobin,
				numPlayers:      numPlayers,
				maxPlayers:      maxPlayers,
				poolWinners:     winnersPerPool,
				courts:          courts,
				mirror:          mirror,
				SeedAssignments: seedAssignments,
			}
			o.outputWriter = inMemoryWriter

			err := o.createPools(strings.Split(text, "\n"))
			if err != nil {
				log.Printf("failed to create pools: %s", err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("Failed to create pools: %s", err.Error()),
				})
				return
			}

		case "playoffs":
			o := &playoffOptions{
				singleTree:      singleTree,
				withZekkenName:  withZekkenName,
				determined:      determined,
				teamMatches:     teamMatches,
				courts:          courts,
				mirror:          mirror,
				SeedAssignments: seedAssignments,
			}

			o.outputWriter = inMemoryWriter

			err := o.createPlayoffs(strings.Split(text, "\n"))
			if err != nil {
				log.Printf("failed to create playoffs: %s", err.Error())
				c.JSON(http.StatusInternalServerError, gin.H{
					"error": fmt.Sprintf("Failed to create playoffs: %s", err.Error()),
				})
				return
			}
		default:
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "Invalid tournament type",
			})
			return
		}

		// Ensure data is written to the buffer
		if err := inMemoryWriter.Flush(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": fmt.Sprintf("Failed to flush buffer: %s", err.Error()),
			})
			return
		}

		if inMemoryBuffer.Len() == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to generate tournament data",
			})
			return
		}

		// Set response headers for file download
		filename := fmt.Sprintf("%s-%s.xlsx", tournamentType, time.Now().Format("2006-01-02"))

		// Mark the download as ready so the client can detect when download starts
		downloadToken := c.PostForm("downloadToken")
		if downloadToken != "" {
			markDownloadReady(downloadToken)
		}

		c.Header("Content-Description", "File Transfer")
		c.Header("Content-Transfer-Encoding", "binary")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", inMemoryBuffer.Bytes())
	})

	return r
}

func init() {
	rootCmd.AddCommand(newServeCmd())
}

func markDownloadReady(token string) {
	downloadStatus.Store(token, true)
}

func consumeDownloadReady(token string) bool {
	if _, ok := downloadStatus.LoadAndDelete(token); ok {
		return true
	}

	return false
}
