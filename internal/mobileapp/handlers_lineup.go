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
//
// The third parameter (`tx CompetitionTransactor`) is the T156 hook.
// The PUT body wraps its three store calls — load comp (for teamSize),
// set lineup, reload lineup (for the response) — in one
// WithTransaction so they all commit under a single per-comp lock
// acquire. The GET and DELETE paths stay on the lock-per-call form
// because they're single-operation flows where the extra primitive
// would just be ceremony. `*state.Store` satisfies all three
// interfaces (TeamLineupStore + CompetitionStore + CompetitionTransactor)
// so wiring stays drop-in.
// RegisterPublicLineupHandlers wires the read-only
// GET /competitions/:id/teams/:tid/lineups/:round endpoint on an
// unauthenticated router group. Lineup data (position assignments)
// is not sensitive — coaches and viewers can see who plays where —
// and the AdminLineup form needs to load the current lineup without
// holding admin credentials for the initial read.  PUT and DELETE
// remain on the admin group via RegisterLineupHandlers.
//
// Slice 7.B / T127.
func RegisterPublicLineupHandlers(r *gin.RouterGroup, store TeamLineupStore) {
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
}

// RegisterLineupHandlers wires the PUT/DELETE lineup endpoints under
// the admin (auth-protected) group. The corresponding GET is public
// and registered via RegisterPublicLineupHandlers.
//
// DELETE is manager-only per the spec — for now we rely on the
// existing AuthMiddleware (mounted on the admin router group in
// server.go) as the auth boundary; a richer role check lands when
// per-role auth is implemented.
//
// The third parameter (`tx CompetitionTransactor`) is the T156 hook.
// The PUT body wraps its three store calls — load comp (for teamSize),
// set lineup, reload lineup (for the response) — in one
// WithTransaction so they all commit under a single per-comp lock
// acquire. `*state.Store` satisfies all three interfaces
// (TeamLineupStore + CompetitionStore + CompetitionTransactor) so
// wiring stays drop-in.
func RegisterLineupHandlers(r *gin.RouterGroup, store TeamLineupStore, comps CompetitionStore, tx CompetitionTransactor) {
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

		lineup := domain.TeamLineup{
			TeamID:        teamID,
			CompetitionID: compID,
			Round:         round,
			Positions:     req.Positions,
		}

		// T156: load comp (for teamSize) + Set lineup + reload lineup (for
		// the response) all run under one WithTransaction acquire. Before
		// this migration the three calls each took their own per-comp
		// lock — a concurrent admin "force-lock round" between Set and
		// the reload could stamp LockedAt onto the response payload that
		// wasn't on the actual saved record. Same atomicity argument the
		// engine UpdatePoolMatchByID / UpdateBracket primitives already
		// make for their own multi-step flows.
		//
		// httpErr carries the (status, body) pair the response should
		// emit; we set it from inside the tx and write the response
		// AFTER the lock releases. Writing JSON while holding the lock
		// would let a slow consumer stall every other writer for the
		// same competition for the entire stream duration.
		type httpErr struct {
			status int
			body   gin.H
		}
		var respErr *httpErr
		var persistedLineup domain.TeamLineup
		txErr := tx.WithTransaction(compID, func(stx state.StoreTx) error {
			// TeamSize is competition-level: a 3-person team and a
			// 5-person team cannot coexist in the same competition. We
			// need it here to drive Validate(); not having a competition
			// is a 404.
			comp, err := stx.LoadCompetition(compID)
			if err != nil {
				respErr = &httpErr{status: http.StatusInternalServerError, body: gin.H{"error": err.Error()}}
				return nil
			}
			if comp == nil {
				respErr = &httpErr{status: http.StatusNotFound, body: gin.H{"error": "competition not found"}}
				return nil
			}
			teamSize := comp.TeamSize
			if teamSize <= 0 {
				respErr = &httpErr{
					status: http.StatusBadRequest,
					body:   gin.H{"error": "competition is not configured for team play (teamSize must be > 0)"},
				}
				return nil
			}

			if err := stx.SetTeamLineup(compID, lineup, teamSize); err != nil {
				switch {
				case errors.Is(err, state.ErrLineupLocked):
					respErr = &httpErr{status: http.StatusConflict, body: gin.H{"error": err.Error()}}
				case errors.Is(err, domain.ErrLineupMissingSenpo),
					errors.Is(err, domain.ErrLineupMissingTaisho),
					errors.Is(err, domain.ErrLineupTooManyMissing),
					errors.Is(err, domain.ErrLineupTeamSizeInvalid):
					respErr = &httpErr{status: http.StatusBadRequest, body: gin.H{"error": err.Error()}}
				default:
					// Generic dynamic validation messages (e.g.
					// "position X not allowed in N-person team") also map
					// to 400; same surface as the sentinel path.
					respErr = &httpErr{status: http.StatusBadRequest, body: gin.H{"error": err.Error()}}
				}
				return nil
			}
			// Reload after write so the response carries the persisted
			// CompetitionID (auto-stamped by Set) and any future
			// server-managed fields. This reload reads the same on-disk
			// state as the Set above because no concurrent writer can
			// have taken the per-comp lock between them.
			lineups, err := stx.LoadTeamLineups(compID)
			if err != nil {
				respErr = &httpErr{status: http.StatusInternalServerError, body: gin.H{"error": err.Error()}}
				return nil
			}
			key := fmt.Sprintf("%s-%d", teamID, round)
			if persisted, ok := lineups[key]; ok {
				persistedLineup = persisted
			} else {
				// Defensive: SetTeamLineup just succeeded, so the entry
				// MUST be present on reload. Falling back to the request
				// payload keeps the response shape sane if the
				// invariant is somehow violated.
				persistedLineup = lineup
			}
			return nil
		})
		if txErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
			return
		}
		if respErr != nil {
			c.JSON(respErr.status, respErr.body)
			return
		}
		c.JSON(http.StatusOK, persistedLineup)
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
