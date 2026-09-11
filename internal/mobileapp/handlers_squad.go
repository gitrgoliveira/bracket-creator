// Package mobileapp, handlers_squad.go owns the
// `/api/competitions/:id/squads` and
// `/api/competitions/:id/teams/:tid/members[/:memberId]` endpoints
// (bc-tmid pass 1): a team's squad, the people on it, kept in its own
// per-competition store (internal/state/squad.go) rather than in
// Player.Metadata, the untyped array a team's roster row shares with an
// individual's dan grade -- see state.ErrDuplicateTeamMember's doc comment
// in participants.go for the data-loss history that store move exists to
// close.
//
// All three routes are admin-only, and stay main-password-gated even in
// self-run mode (isSelfRunMainGatedConfigRoute, middleware.go): squad
// management is organiser setup, not operational play, the same class as
// team lineup PUT/DELETE.
//
// Deliberately NO SSE broadcast, unlike the lineup handlers next door which
// fire EventLineupUpdated on every mutation. The consequence is real and
// accepted: a member added or renamed on one device is invisible to a second
// admin session already sitting in the lineup editor until it remounts.
// Squad edits are setup done by one organiser, not the concurrent
// multi-device traffic the lineup broadcast exists for, and adding an event
// means a new wire event plus a subscriber on every squad reader. Stated
// here because the omission otherwise reads as an oversight next to a
// sibling that does broadcast.
package mobileapp

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// SquadMemberRequest is the body for POST .../members and
// PUT .../members/:memberId. Both take only a name; the team (and, for
// PUT, the member) are pinned by the URL path -- Add mints the id/index,
// Rename keeps them.
type SquadMemberRequest struct {
	Name string `json:"name"`
}

// RegisterSquadHandlers wires the GET/POST/PUT squad endpoints under the
// admin group. comps is used only to turn an unknown competition id into a
// 404 instead of a confusing 500 from a write that can never land (the
// per-competition directory does not exist to write into); mirrors
// handlers_lineup.go's own comp == nil check.
func RegisterSquadHandlers(r *gin.RouterGroup, store SquadStore, comps CompetitionStore) {
	r.GET("/competitions/:id/squads", func(c *gin.Context) {
		compID, ok := requireValidCompID(c)
		if !ok {
			return
		}
		if !requireExistingCompetitionForSquad(c, comps, compID) {
			return
		}
		squads, err := store.LoadSquads(compID)
		if err != nil {
			internalError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"squads": squads})
	})

	r.POST("/competitions/:id/teams/:tid/members", func(c *gin.Context) {
		compID, teamID, ok := requireValidCompIDAndTeam(c)
		if !ok {
			return
		}
		if !requireExistingCompetitionForSquad(c, comps, compID) {
			return
		}
		var req SquadMemberRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
			return
		}
		member, err := store.AddTeamMember(compID, teamID, req.Name)
		if err != nil {
			respondSquadWriteError(c, err)
			return
		}
		c.JSON(http.StatusCreated, member)
	})

	r.PUT("/competitions/:id/teams/:tid/members/:memberId", func(c *gin.Context) {
		compID, teamID, ok := requireValidCompIDAndTeam(c)
		if !ok {
			return
		}
		if !requireExistingCompetitionForSquad(c, comps, compID) {
			return
		}
		memberID := c.Param("memberId")
		if memberID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "member ID is required"})
			return
		}
		var req SquadMemberRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if strings.TrimSpace(req.Name) == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
			return
		}
		if err := store.RenameTeamMember(compID, teamID, memberID, req.Name); err != nil {
			respondSquadWriteError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}

// requireExistingCompetitionForSquad 404s when compID names no competition,
// and 500s on an unreadable config.md. compID has already passed
// requireValidCompID (format only), so this is the existence check that
// turns a bad id into "competition not found" instead of falling through to
// a store write that would fail with a bare, unmappable I/O error (no
// competition directory to write squads.yaml into).
func requireExistingCompetitionForSquad(c *gin.Context, comps CompetitionStore, compID string) bool {
	comp, err := comps.LoadCompetition(compID)
	if err != nil {
		internalError(c, err)
		return false
	}
	if comp == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
		return false
	}
	return true
}

// requireValidCompIDAndTeam extracts (compID, teamID) from the URL, 400ing
// on an empty team id. Shared by all three readers of this path prefix: the
// squad handlers below and parseLineupParams/parseMatchLineupParams
// (handlers_lineup.go), which spelled the same check out by hand until this
// helper existed. The URL param is named :tid, matching
// parseLineupParams' own wildcard at this same path prefix
// ("/competitions/:id/teams/:tid/...") -- gin's router tree refuses two
// different wildcard names at one path position, so this MUST stay :tid,
// not :teamId, or route registration panics.
//
// This helper checks the SHAPE of teamID only (non-empty) and never that it
// names a real team; it is shared by callers that differ on that point. The
// squad path validates existence downstream, at state.AddTeamMember, because
// a member minted under a bogus id can never be removed. The lineup paths
// deliberately do not, because a lineup keyed on a bogus id is overwritable.
// teamID is never used as a filesystem path on either.
func requireValidCompIDAndTeam(c *gin.Context) (compID, teamID string, ok bool) {
	compID, ok = requireValidCompID(c)
	if !ok {
		return "", "", false
	}
	teamID = c.Param("tid")
	if teamID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "team ID is required"})
		return "", "", false
	}
	return compID, teamID, true
}

// respondSquadWriteError maps an AddTeamMember/RenameTeamMember error to its
// HTTP status. ErrTeamNotFound (the team id names no participant) and
// ErrTeamMemberNotFound are squad-specific (404 each); everything
// else reuses classifyRosterWriteError's existing sentinel table via
// respondRosterWriteError (errors.go) rather than a second hand-copied
// mapping -- state.ErrDuplicateTeamMember is already classified there as a
// 409, the same status every OTHER caller of that sentinel gets.
func respondSquadWriteError(c *gin.Context, err error) {
	if errors.Is(err, state.ErrTeamNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if errors.Is(err, state.ErrTeamMemberNotFound) {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	if respondRosterWriteError(c, err) {
		return
	}
	internalError(c, err)
}
