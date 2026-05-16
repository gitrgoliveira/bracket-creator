// Package mobileapp — handlers_lineup.go owns the
// `/api/competitions/:cid/teams/:tid/lineups/:round` endpoints
// (Slice 7.B / T127).
//
// GET returns the lineup for a (team, round) tuple, PUT sets/replaces
// it, DELETE removes it. The lineup is mutable up until the round's
// first match goes live — once frozen, subsequent PUTs return 409 with
// ErrLineupLocked (FR-040, FR-041, R4 / CHK012).
//
// All store I/O goes through the TeamLineupStore + CompetitionStore
// interfaces (deps.go) rather than the concrete *state.Store
// (NFR-002). The handler needs CompetitionStore to look up the
// competition's TeamSize, which drives the FIK back-fill validation
// inside TeamLineup.Validate.
package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// LineupRequest is the body for PUT /lineups/:round. We accept the
// positions map as the only required field — teamID/round/compID are
// pinned by the URL path, and LockedAt is server-managed (the engine
// stamps it when the round's first match goes live).
type LineupRequest struct {
	Positions map[domain.Position]string `json:"positions"`
}

// RegisterLineupHandlers wires the GET/PUT/DELETE lineup endpoints
// under the admin group. Slice 7.B / T127.
//
// DELETE is manager-only per the spec — for now we rely on the
// existing AuthMiddleware (mounted on the admin router group in
// server.go) as the auth boundary; a richer role check lands when
// per-role auth is implemented.
func RegisterLineupHandlers(r *gin.RouterGroup, store TeamLineupStore, comps CompetitionStore) {
	r.GET("/competitions/:id/teams/:tid/lineups/:round", func(c *gin.Context) {
		compID, teamID, round, ok := parseLineupParams(c)
		if !ok {
			return
		}
		lineups, err := store.LoadTeamLineups(compID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		key := fmt.Sprintf("%s-%d", teamID, round)
		lineup, found := lineups[key]
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "no lineup submitted for this team and round"})
			return
		}
		c.JSON(http.StatusOK, lineup)
	})

	r.PUT("/competitions/:id/teams/:tid/lineups/:round", func(c *gin.Context) {
		compID, teamID, round, ok := parseLineupParams(c)
		if !ok {
			return
		}
		var req LineupRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// TeamSize is competition-level: a 3-person team and a 5-person
		// team cannot coexist in the same competition. We need it here
		// to drive Validate(); not having a competition is a 404.
		comp, err := comps.LoadCompetition(compID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		teamSize := comp.TeamSize
		if teamSize <= 0 {
			// Validate() would catch this, but we surface a clearer
			// message: a non-team competition shouldn't have lineups.
			c.JSON(http.StatusBadRequest, gin.H{
				"error": "competition is not configured for team play (teamSize must be > 0)",
			})
			return
		}

		lineup := domain.TeamLineup{
			TeamID:        teamID,
			CompetitionID: compID,
			Round:         round,
			Positions:     req.Positions,
		}

		if err := store.SetTeamLineup(compID, lineup, teamSize); err != nil {
			switch {
			case errors.Is(err, state.ErrLineupLocked):
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case errors.Is(err, domain.ErrLineupMissingSenpo),
				errors.Is(err, domain.ErrLineupMissingTaisho),
				errors.Is(err, domain.ErrLineupTooManyMissing),
				errors.Is(err, domain.ErrLineupTeamSizeInvalid):
				// Shape errors from Validate land here — 400 with the
				// sentinel message so the UI can render it directly.
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				// Fall-through: the generic "position X not allowed in
				// N-person team" / "with 1 vacancy the missing position
				// must be Jiho" messages are dynamically formatted and
				// don't have a sentinel — but they're still validation
				// failures, surfaced as 400.
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			}
			return
		}
		// Reload after write so the response carries the persisted
		// CompetitionID (auto-stamped by Set) and any future
		// server-managed fields.
		lineups, err := store.LoadTeamLineups(compID)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		key := fmt.Sprintf("%s-%d", teamID, round)
		if persisted, ok := lineups[key]; ok {
			lineup = persisted
		}
		c.JSON(http.StatusOK, lineup)
	})

	r.DELETE("/competitions/:id/teams/:tid/lineups/:round", func(c *gin.Context) {
		compID, teamID, round, ok := parseLineupParams(c)
		if !ok {
			return
		}
		if err := store.DeleteTeamLineup(compID, teamID, round); err != nil {
			if errors.Is(err, state.ErrLineupLocked) {
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.Status(http.StatusNoContent)
	})
}

// parseLineupParams extracts (compID, teamID, round) from the URL and
// writes a 400 response when round can't be parsed as int. compID
// goes through requireValidCompID to enforce the
// ValidateCompetitionID character whitelist.
//
// teamID is treated as opaque — there's no team-management surface
// yet, so we don't impose a regex (the on-disk file is keyed by the
// composite string and never used as a filesystem path). When team
// management lands a real validator can be added here.
func parseLineupParams(c *gin.Context) (compID, teamID string, round int, ok bool) {
	compID, ok = requireValidCompID(c)
	if !ok {
		return "", "", 0, false
	}
	teamID = c.Param("tid")
	if teamID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "team ID is required"})
		return "", "", 0, false
	}
	roundStr := c.Param("round")
	round, err := strconv.Atoi(roundStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "round must be an integer"})
		return "", "", 0, false
	}
	if round < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "round must be non-negative"})
		return "", "", 0, false
	}
	return compID, teamID, round, true
}
