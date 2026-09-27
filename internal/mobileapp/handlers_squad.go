// Package mobileapp, handlers_squad.go owns the
// `/api/competitions/:id/team-members` and
// `/api/competitions/:id/teams/:tid/members[/:memberId]` endpoints
// (bc-tmid pass 1; DELETE added bc-pnum): a team's squad, the people on it,
// kept in its own per-competition store (internal/state/squad.go) rather
// than in Player.Metadata, the untyped array a team's roster row shares
// with an individual's dan grade -- see state.ErrDuplicateTeamMember's doc
// comment in participants.go for the data-loss history that store move
// exists to close.
//
// All four routes live on the admin group. In a self-run tournament ADD and
// RENAME are also open to a caller without the password, because the public
// score sheet names a bout's fighter through them (bc-dhas); such a caller may
// name a member who has no name yet but not rename one who has (see the PUT).
// The read and the name CLEAR stay main-password-gated
// (isSelfRunMainGatedConfigRoute, middleware.go): the public page reads team
// members from the viewer payload and never clears one.
//
// ADD is deliberately silent; RENAME and CLEAR are not, and the split is the
// point. The original rule was that squad edits are setup done by one
// organiser, not the concurrent multi-device traffic the lineup broadcast
// exists for, so a member added on one device is invisible to a second admin
// session until it remounts. That consequence is still accepted for ADD. On
// the public score sheet an add is followed by the match lineup PUT, which
// broadcasts, so the other devices refetch the members anyway.
//
// Rename and clear outgrew it. They now rewrite lineups.yaml as well
// (state.renameMemberInLineupsLocked), because a lineup position stores a
// display copy of the member's name that the score sheet and the export read.
// Every other writer of that file fires EventLineupUpdated, and without it the
// failure is not staleness but LOSS: a second admin holding a pre-rename
// lineup makes any unrelated inline pick, its write spreads the whole stale
// positions map, and the operator's correction is reverted on disk. So these
// two fire the EXISTING lineup event rather than a new squad one, which is why
// no squad reader needs a new subscriber.
//
// They fire it unconditionally, without asking whether a position actually
// changed. A spurious refetch costs one request; a missed one costs the
// rename. Same safe direction bumpFileVersion takes in the store.
//
// The public READ surfaces do not call these routes: the viewer, the court
// display and the streaming overlay read a team's squad from the viewer
// payload (handlers_viewer.go). They inherit the same consequence.
// Nothing here fires an event and the SPA has no data poll, so a squad
// edit reaches them only on their next payload fetch, which some OTHER
// broadcast triggers. Same trade, same reason: this is setup, and the
// label it feeds is enrichment beside a name that is already correct.
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

// RegisterSquadHandlers wires the GET/POST/PUT/DELETE squad endpoints under
// the admin group. comps is used only to turn an unknown competition id into
// a 404 instead of a confusing 500 from a write that can never land (the
// per-competition directory does not exist to write into); mirrors
// handlers_lineup.go's own comp == nil check.
//
// tl/verifier tell an anonymous self-run caller apart (selfRunAnonymous) for
// the rename's guard.
func RegisterSquadHandlers(r *gin.RouterGroup, store SquadStore, comps CompetitionStore, hub Broadcaster, tl TournamentLoader, verifier PasswordVerifier) {
	r.GET("/competitions/:id/team-members", func(c *gin.Context) {
		compID, ok := requireValidCompID(c)
		if !ok {
			return
		}
		if !requireExistingCompetition(c, comps, compID) {
			return
		}
		squads, err := store.LoadSquads(compID)
		if err != nil {
			internalError(c, err)
			return
		}
		c.JSON(http.StatusOK, gin.H{"teamMembers": squads})
	})

	r.POST("/competitions/:id/teams/:tid/members", func(c *gin.Context) {
		compID, teamID, ok := requireValidCompIDAndTeam(c)
		if !ok {
			return
		}
		if !requireExistingCompetition(c, comps, compID) {
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
		if !requireExistingCompetition(c, comps, compID) {
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
		// An anonymous self-run caller names a member from the public score
		// sheet, which only ever names one who has no name yet. Renaming one
		// who has a name stays with the organiser: the rename reaches every
		// stored lineup and every bout already fought that names the member,
		// finished matches included, which the score path's finished-match
		// rule keeps from an anonymous caller.
		anonymous, ok := selfRunAnonymous(c, tl, verifier)
		if !ok {
			return
		}
		if anonymous {
			named, err := teamMemberHasName(store, compID, teamID, memberID)
			if err != nil {
				internalError(c, err)
				return
			}
			if named {
				c.JSON(http.StatusConflict, gin.H{"error": "this team member already has a name; ask the tournament organizer to change it"})
				return
			}
		}
		if err := store.RenameTeamMember(compID, teamID, memberID, req.Name); err != nil {
			respondSquadWriteError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
		hub.Broadcast(EventLineupUpdated, gin.H{"competitionId": compID})
	})

	// DELETE is the operator's "removal": it clears the member's Name back
	// to "" and leaves ID and Index untouched (bc-pnum), so a bout already
	// fought that names this position keeps its meaning. Refused with 409
	// once the competition has started (state.ErrTeamMemberClearAfterStart):
	// the request itself is well-formed, it is the competition's state that
	// forbids it, the same reasoning classifyRosterWriteError's other 409s
	// already use.
	r.DELETE("/competitions/:id/teams/:tid/members/:memberId", func(c *gin.Context) {
		compID, teamID, ok := requireValidCompIDAndTeam(c)
		if !ok {
			return
		}
		if !requireExistingCompetition(c, comps, compID) {
			return
		}
		memberID := c.Param("memberId")
		if memberID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "member ID is required"})
			return
		}
		if err := store.ClearTeamMemberName(compID, teamID, memberID); err != nil {
			respondSquadWriteError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
		hub.Broadcast(EventLineupUpdated, gin.H{"competitionId": compID})
	})
}

// teamMemberHasName reports whether the member already has a name. A member
// or team it cannot find reads as unnamed, so the rename itself reports the
// missing member (respondSquadWriteError's 404) rather than this check.
func teamMemberHasName(store SquadStore, compID, teamID, memberID string) (bool, error) {
	squads, err := store.LoadSquads(compID)
	if err != nil {
		return false, err
	}
	for _, m := range squads[teamID] {
		if m.ID == memberID {
			return strings.TrimSpace(m.Name) != "", nil
		}
	}
	return false, nil
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

// respondSquadWriteError maps an AddTeamMember/RenameTeamMember/
// ClearTeamMemberName error to its HTTP status. ErrTeamNotFound (the team id
// names no participant), ErrTeamMemberNotFound, and
// ErrTeamMemberClearAfterStart are squad-specific; everything else reuses
// classifyRosterWriteError's existing sentinel table via
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
	if errors.Is(err, state.ErrTeamMemberClearAfterStart) {
		// 409, not 400: the request is well-formed, it is the competition's
		// current state (already started) that forbids it.
		c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		return
	}
	if respondRosterWriteError(c, err) {
		return
	}
	internalError(c, err)
}
