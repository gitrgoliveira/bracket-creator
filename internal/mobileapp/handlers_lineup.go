// Package mobileapp, handlers_lineup.go owns the
// `/api/competitions/:cid/teams/:tid/lineups/:round` endpoints
// (Slice 7.B / T127).
//
// GET returns the lineup for a (team, round) tuple, PUT sets/replaces
// it, DELETE removes it. Lineups are always editable, including while
// a match is running or completed (mp-q722).
//
// All store I/O goes through the TeamLineupStore + CompetitionStore
// interfaces (deps.go) rather than the concrete *state.Store
// (NFR-002). The handler needs CompetitionStore to look up the
// competition's TeamSize, which drives the FIK back-fill validation
// inside TeamLineup.Validate.
package mobileapp

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// lineupSetStatus maps a SetTeamLineup error to the right HTTP status. Domain
// lineup validation failures (bad positions, missing senpo/taisho, disqualifying
// vacancies, bad team size) all carry the "team_lineup:" prefix and are client
// errors (400). Anything else is a server fault (YAML parse / disk I/O) and must
// be a 500 so a real failure is not misreported as a bad request. compID is
// already validated upstream by requireValidCompID, so ValidateCompetitionID
// cannot be the source here.
//
// CONTRACT: this classification relies on the "team_lineup:" prefix being at
// position 0 of the message. The store path (SetTeamLineup -> setTeamLineupLocked
// and the domain ValidatePositions in internal/domain/team_lineup.go) returns
// these validation errors UNWRAPPED (plain sentinels / %q-%v fmt.Errorf, never
// %w). Do NOT wrap a lineup validation error with added context before it reaches
// here, or it would fall through to 500. If wrapping becomes necessary, switch
// this to a typed error checked via errors.As instead of a prefix match.
func lineupSetStatus(err error) int {
	if strings.HasPrefix(err.Error(), "team_lineup:") {
		return http.StatusBadRequest
	}
	return http.StatusInternalServerError
}

// LineupRequest is the body for PUT /lineups/:round and the match-scoped
// PUT /match-lineups/:matchId. We accept only the positions map; teamID,
// round/matchID, and compID are pinned by the URL path.
type LineupRequest struct {
	Positions map[domain.Position]string `json:"positions"`
}

// RegisterLineupHandlers wires the GET/PUT/DELETE lineup endpoints
// under the admin group. Slice 7.B / T127.
//
// DELETE is manager-only per the spec; for now we rely on the
// existing AuthMiddleware (mounted on the admin router group in
// server.go) as the auth boundary. A richer role check lands when
// per-role auth is implemented.
//
// The third parameter (`tx CompetitionTransactor`) is the T156 hook.
// The PUT body wraps its three store calls; load comp (for teamSize),
// set lineup, reload lineup (for the response), all run in one
// WithTransaction so they all commit under a single per-comp lock
// acquire. The GET and DELETE paths stay on the lock-per-call form
// because they're single-operation flows where the extra primitive
// would just be ceremony. `*state.Store` satisfies all three
// interfaces (TeamLineupStore + CompetitionStore + CompetitionTransactor)
// so wiring stays drop-in.
// RegisterPublicLineupHandlers wires the read-only
// GET /competitions/:id/teams/:tid/lineups/:round endpoint on an
// unauthenticated router group. Lineup data (position assignments)
// is not sensitive, coaches and viewers can see who plays where,
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
			internalError(c, err)
			return
		}
		key := fmt.Sprintf("%s-%d", teamID, round)
		lineup, found := lineups[key]
		if !found && c.Query("fallback") == "best" {
			// Best-effort mode, the client-side twin of AMENDMENT 1: the
			// scoring modal asks for the match's own round index, but
			// operators typically save one round-0 lineup for the whole
			// day, so a knockout final (round 1+) would 404 and leave the
			// modal without names. Resolve via the FindBestLineup round
			// tiers (highest round <= requested, else highest overall;
			// match-scoped entries are skipped by passing an empty
			// matchID). Default behavior without the param stays exact +
			// 404 so the lineup editor's "no lineup submitted for THIS
			// round" semantics are untouched.
			lineup, found = state.FindBestLineup(lineups, teamID, "", round)
		}
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "no lineup submitted for this team and round"})
			return
		}
		c.JSON(http.StatusOK, lineup)
	})

	// Match-scoped read (mp-825). 404 lets the caller fall back to the
	// round-scoped endpoint above.
	r.GET("/competitions/:id/teams/:tid/match-lineups/:matchId", func(c *gin.Context) {
		compID, teamID, matchID, ok := parseMatchLineupParams(c)
		if !ok {
			return
		}
		lineups, err := store.LoadTeamLineups(compID)
		if err != nil {
			internalError(c, err)
			return
		}
		lineup, found := findMatchLineup(lineups, teamID, matchID)
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "no lineup submitted for this team and match"})
			return
		}
		c.JSON(http.StatusOK, lineup)
	})
}

// findMatchLineup scans the loaded lineup map for the match-scoped entry
// matching (teamID, matchID), avoiding any dependency on the store's
// internal key format. Returns the lineup and whether it was found.
func findMatchLineup(lineups map[string]domain.TeamLineup, teamID, matchID string) (domain.TeamLineup, bool) {
	for _, l := range lineups {
		if l.MatchID == matchID && l.TeamID == teamID {
			return l, true
		}
	}
	return domain.TeamLineup{}, false
}

// RegisterLineupHandlers wires the PUT/DELETE lineup endpoints under
// the admin (auth-protected) group. The corresponding GET is public
// and registered via RegisterPublicLineupHandlers.
//
// DELETE is manager-only per the spec; for now we rely on the
// existing AuthMiddleware (mounted on the admin router group in
// server.go) as the auth boundary. A richer role check lands when
// per-role auth is implemented.
//
// The `tx CompetitionTransactor` parameter is the T156 hook.
// The PUT body wraps its three store calls; load comp (for teamSize),
// set lineup, reload lineup (for the response), all in one
// WithTransaction so they all commit under a single per-comp lock
// acquire. `hub Broadcaster` receives an EventLineupUpdated after
// each successful write so SSE clients can re-fetch lineup data.
// `*state.Store` satisfies the first three interfaces (TeamLineupStore +
// CompetitionStore + CompetitionTransactor); the SSE hub satisfies
// Broadcaster and is wired separately in production.
func RegisterLineupHandlers(r *gin.RouterGroup, store TeamLineupStore, comps CompetitionStore, tx CompetitionTransactor, hub Broadcaster) {
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
		// the response) all run under one WithTransaction acquire. Same
		// atomicity argument the engine UpdatePoolMatchByID / UpdateBracket
		// primitives already make for their own multi-step flows.
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
				log.Printf("mobileapp: PUT /competitions/%s/teams/%s/lineups: LoadCompetition: %v", compID, teamID, err)
				respErr = &httpErr{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
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
				// Domain validation errors ("team_lineup:" prefix) are 400; a
				// YAML/disk fault is a 500 (see lineupSetStatus).
				respErr = &httpErr{status: lineupSetStatus(err), body: gin.H{"error": err.Error()}}
				return nil
			}
			// Reload after write so the response carries the persisted
			// CompetitionID (auto-stamped by Set) and any future
			// server-managed fields. This reload reads the same on-disk
			// state as the Set above because no concurrent writer can
			// have taken the per-comp lock between them.
			lineups, err := stx.LoadTeamLineups(compID)
			if err != nil {
				log.Printf("mobileapp: PUT /competitions/%s/teams/%s/lineups: LoadTeamLineups: %v", compID, teamID, err)
				respErr = &httpErr{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
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
			internalError(c, txErr)
			return
		}
		if respErr != nil {
			c.JSON(respErr.status, respErr.body)
			return
		}
		c.JSON(http.StatusOK, persistedLineup)
		hub.Broadcast(EventLineupUpdated, gin.H{"competitionId": compID})
	})

	r.DELETE("/competitions/:id/teams/:tid/lineups/:round", func(c *gin.Context) {
		compID, teamID, round, ok := parseLineupParams(c)
		if !ok {
			return
		}
		if err := store.DeleteTeamLineup(compID, teamID, round); err != nil {
			internalError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
		hub.Broadcast(EventLineupUpdated, gin.H{"competitionId": compID})
	})

	// Match-scoped PUT/DELETE (mp-825). Mirrors the round-scoped flow but
	// keys the lineup by matchID so successive encounters lock and edit
	// independently.
	r.PUT("/competitions/:id/teams/:tid/match-lineups/:matchId", func(c *gin.Context) {
		compID, teamID, matchID, ok := parseMatchLineupParams(c)
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
			MatchID:       matchID,
			Positions:     req.Positions,
		}

		type httpErr struct {
			status int
			body   gin.H
		}
		var respErr *httpErr
		var persistedLineup domain.TeamLineup
		txErr := tx.WithTransaction(compID, func(stx state.StoreTx) error {
			comp, err := stx.LoadCompetition(compID)
			if err != nil {
				log.Printf("mobileapp: PUT /competitions/%s/teams/%s/match-lineups/%s: LoadCompetition: %v", compID, teamID, matchID, err)
				respErr = &httpErr{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
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
				// Domain validation errors ("team_lineup:" prefix) are 400; a
				// YAML/disk fault is a 500 (see lineupSetStatus).
				respErr = &httpErr{status: lineupSetStatus(err), body: gin.H{"error": err.Error()}}
				return nil
			}
			lineups, err := stx.LoadTeamLineups(compID)
			if err != nil {
				log.Printf("mobileapp: PUT /competitions/%s/teams/%s/match-lineups/%s: LoadTeamLineups: %v", compID, teamID, matchID, err)
				respErr = &httpErr{status: http.StatusInternalServerError, body: gin.H{"error": "internal error"}}
				return nil
			}
			if persisted, found := findMatchLineup(lineups, teamID, matchID); found {
				persistedLineup = persisted
			} else {
				// Defensive: Set just succeeded, so the entry must be
				// present on reload; fall back to the request payload.
				persistedLineup = lineup
			}
			return nil
		})
		if txErr != nil {
			internalError(c, txErr)
			return
		}
		if respErr != nil {
			c.JSON(respErr.status, respErr.body)
			return
		}
		c.JSON(http.StatusOK, persistedLineup)
		hub.Broadcast(EventLineupUpdated, gin.H{"competitionId": compID})
	})

	r.DELETE("/competitions/:id/teams/:tid/match-lineups/:matchId", func(c *gin.Context) {
		compID, teamID, matchID, ok := parseMatchLineupParams(c)
		if !ok {
			return
		}
		if err := store.DeleteTeamLineupForMatch(compID, teamID, matchID); err != nil {
			internalError(c, err)
			return
		}
		c.Status(http.StatusNoContent)
		hub.Broadcast(EventLineupUpdated, gin.H{"competitionId": compID})
	})
}

// parseLineupParams extracts (compID, teamID, round) from the URL and
// writes a 400 response when round can't be parsed as int. compID
// goes through requireValidCompID to enforce the
// ValidateCompetitionID character whitelist.
//
// teamID is treated as opaque; there's no team-management surface
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

// parseMatchLineupParams extracts (compID, teamID, matchID) from the URL
// for the match-scoped lineup endpoints (mp-825). matchID is opaque
// (like teamID); it's never used as a filesystem path, only as a map
// key and a lookup against persisted match IDs, so no regex is imposed
// beyond non-empty.
func parseMatchLineupParams(c *gin.Context) (compID, teamID, matchID string, ok bool) {
	compID, ok = requireValidCompID(c)
	if !ok {
		return "", "", "", false
	}
	teamID = c.Param("tid")
	if teamID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "team ID is required"})
		return "", "", "", false
	}
	matchID = c.Param("matchId")
	if matchID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "match ID is required"})
		return "", "", "", false
	}
	return compID, teamID, matchID, true
}
