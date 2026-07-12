package cmd

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

// createTournamentHandler generates a tournament Excel workbook from a posted
// roster + settings and streams it back as an .xlsx download. It is the single
// source of truth behind both the `serve` web app (POST /create) and the
// mobile-app server (POST /create), so the in-app "Download .xlsx" button runs
// the EXACT same generator the standalone web form does, building pools and
// matches in one pass (helper.CreatePool*Matches → helper.PrintPoolMatches),
// which keeps the player↔match pointer link the scoring/ranking formulas rely
// on. The engine's stored-pool export path (internal/engine) cannot, because a
// store round-trip severs that link and the W/L/T/RANK formulas collapse to 0.
//
// Stateless: it reads everything from the request body and writes nothing to
// the state store, so it is safe to mount unauthenticated alongside the
// mobile-app's stateful APIs.
func createTournamentHandler(c *gin.Context) {
	text := c.PostForm("playerList")
	if text == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "Player list cannot be empty",
		})
		return
	}
	// Normalize line endings before splitting: textarea/form submissions
	// commonly arrive CRLF, which would otherwise leave a trailing "\r" on
	// every entry, bypassing the exact-match duplicate check and turning a
	// blank "\r" line into a phantom entry. The downstream field parser
	// TrimSpace's individual columns, but the entry-level dedup does not.
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	entries := strings.Split(text, "\n")

	// Parse form values
	singleTree := c.PostForm("singleTree") == "on"
	withZekkenName := c.PostForm("withZekkenName") == "on"
	engi := c.PostForm("engi") == "on"
	naginata := c.PostForm("naginata") == "on"
	determined := c.PostForm("determined") == "on"
	titlePrefix := c.PostForm("titlePrefix")
	numberPrefix := c.PostForm("numberPrefix")

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
	poolFormat := c.PostForm("poolFormat") // "partial" → path-graph; else legacy roundRobin switch
	poolSizeMode := c.PostForm("poolSizeMode")

	// Parse courts (number of Shiaijo)
	courts, err := strconv.Atoi(c.PostForm("courts"))
	if err != nil || courts < 1 {
		courts = 2
	}
	if err := helper.ValidateCourts(courts); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Reject duplicate participant entries up front so the user sees a
	// clear error instead of silently dropped rows in the spreadsheet.
	if dups := helper.CheckDuplicateEntries(entries); len(dups) > 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Duplicate participant entries: %s", strings.Join(dups, ", ")),
		})
		return
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
			engi:            engi,
			naginata:        naginata,
			determined:      determined,
			teamMatches:     teamMatches,
			roundRobin:      roundRobin,
			poolFormat:      poolFormat,
			numPlayers:      numPlayers,
			maxPlayers:      maxPlayers,
			poolWinners:     winnersPerPool,
			courts:          courts,
			titlePrefix:     titlePrefix,
			numberPrefix:    numberPrefix,
			SeedAssignments: seedAssignments,
		}
		o.outputWriter = inMemoryWriter

		err := o.createPools(entries)
		if err != nil {
			// Generation failures here are overwhelmingly caused by invalid
			// request input (pool-size/winners constraints, participant
			// validation), so report 400 rather than 500, the body carries
			// the specific reason for the caller to surface.
			log.Printf("failed to create pools: %s", err.Error())
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Failed to create pools: %s", err.Error()),
			})
			return
		}

	case "playoffs":
		o := &playoffOptions{
			singleTree:      singleTree,
			withZekkenName:  withZekkenName,
			naginata:        naginata,
			engi:            engi,
			determined:      determined,
			teamMatches:     teamMatches,
			courts:          courts,
			titlePrefix:     titlePrefix,
			numberPrefix:    numberPrefix,
			SeedAssignments: seedAssignments,
		}

		o.outputWriter = inMemoryWriter

		err := o.createPlayoffs(entries)
		if err != nil {
			// As with pools, playoff generation failures are typically
			// request-caused (invalid roster, seed validation), report 400.
			log.Printf("failed to create playoffs: %s", err.Error())
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf("Failed to create playoffs: %s", err.Error()),
			})
			return
		}
	}
	// No default: tournamentType is validated to be "pools" or "playoffs" by
	// the guard near the top of this handler.

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
}
