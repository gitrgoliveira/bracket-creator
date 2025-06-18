package cmd

import (
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"bufio"
	"bytes"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/cmd/version"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"

	"github.com/spf13/cobra"
)

type serveOptions struct {
	bindAddress string
	port        int
}

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
		log.Fatal(err)
		return err
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
		sanitize := c.PostForm("sanitize") == "on"
		determined := c.PostForm("determined") == "on"

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

		// Prepare output
		inMemoryBuffer := new(bytes.Buffer)
		inMemoryWriter := bufio.NewWriter(inMemoryBuffer)

		// Create tournament
		switch tournamentType {
		case "pools":
			o := &poolOptions{
				singleTree:  singleTree,
				sanitize:    sanitize,
				determined:  determined,
				teamMatches: teamMatches,
				roundRobin:  roundRobin,
				numPlayers:  playersPerPool,
				poolWinners: winnersPerPool,
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
				singleTree:  singleTree,
				sanitize:    sanitize,
				determined:  determined,
				teamMatches: teamMatches,
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
		inMemoryWriter.Flush()

		if inMemoryBuffer.Len() == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": "Failed to generate tournament data",
			})
			return
		}

		// Set response headers for file download
		filename := fmt.Sprintf("%s-%s.xlsx", tournamentType, time.Now().Format("2006-01-02"))
		c.Header("Content-Description", "File Transfer")
		c.Header("Content-Transfer-Encoding", "binary")
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s", filename))
		c.Header("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", inMemoryBuffer.Bytes())
	})

	err = r.Run(o.bindAddress + ":" + strconv.Itoa(o.port))
	if err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(newServeCmd())
}
