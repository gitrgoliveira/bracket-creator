// Package mobileapp — handlers_league_playoff.go owns the four operator
// endpoints for league play-off management (Phase 3b, mp-8rc9):
//
//	GET  /api/competitions/:cid/league-playoff/candidates  (public — no auth)
//	POST /api/competitions/:cid/league-playoff             (admin-gated)
//	DELETE /api/competitions/:cid/league-playoff           (admin-gated)
//	POST /api/competitions/:cid/league-playoff/finalize    (admin-gated)
//
// The GET read is public (registered via RegisterPublicLeaguePlayoffHandlers
// on the unauthenticated api group, mirroring RegisterPublicSwissHandlers).
// The three write/mutate operations require X-Tournament-Password and are
// registered via RegisterLeaguePlayoffHandlers on the adminSmallBody group.
//
// The design mirrors handlers_daihyosen.go: narrow consumer-boundary
// interfaces, request body caps enforced by the adminSmallBody group in
// server.go, and errors surfaced as typed engine errors (NotFoundError →
// 404, ValidationError → 400, conflict guards → 409).
package mobileapp

import (
	"errors"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// LeaguePlayoffEngine is the consumer-boundary view of *engine.Engine used
// by the league-playoff handler family. Methods are restricted to what these
// four endpoints actually call.
type LeaguePlayoffEngine interface {
	LeaguePlayoffCandidates(compID string) ([]engine.TiedGroup, error)
	GenerateLeaguePlayoffMatches(compID string, tiedTeamNames []string) ([]state.MatchResult, error)
	MaybeAutoCompletePools(compID string) (engine.AutoCompleteOutcome, error)
}

// LeaguePlayoffStore is the consumer-boundary view of *state.Store used by
// the league-playoff handler family.
type LeaguePlayoffStore interface {
	LoadCompetition(id string) (*state.Competition, error)
	LoadPoolMatches(id string) ([]state.MatchResult, error)
	SavePoolMatches(id string, matches []state.MatchResult) error
	UpdateCompetitionChanged(id string, transform func(current *state.Competition) (*state.Competition, error)) (bool, error)
}

// leaguePlayoffCandidateGroup is the JSON shape for one tied group returned
// by GET /league-playoff/candidates.
type leaguePlayoffCandidateGroup struct {
	// TeamNames holds the names of the tied teams in standings order.
	TeamNames []string `json:"teamNames"`
	// MinPosition is the 1-based best rank among the tied teams.
	MinPosition int `json:"minPosition"`
	// MaxPosition is the 1-based worst rank among the tied teams.
	MaxPosition int `json:"maxPosition"`
}

// leaguePlayoffRequest is the JSON body for POST /league-playoff.
// The operator selects exactly one tied group (by team names) to play off.
type leaguePlayoffRequest struct {
	// TeamNames is the set of team names for which to generate play-off
	// matches. Must match exactly one consequential candidate group from
	// LeaguePlayoffCandidates (order does not matter).
	TeamNames []string `json:"teamNames"`
}

// dedupedNameSet builds a presence set from names and reports whether the input
// contained any duplicate. Handlers reject duplicates up front so downstream
// group-size and pair-count comparisons (which use the deduped set) can't be
// fooled by a repeated team name.
func dedupedNameSet(names []string) (set map[string]bool, hadDuplicate bool) {
	set = make(map[string]bool, len(names))
	for _, n := range names {
		set[n] = true
	}
	return set, len(set) != len(names)
}

// RegisterPublicLeaguePlayoffHandlers wires the unauthenticated league-playoff
// read endpoint on the public api group.
//
//	GET /competitions/:id/league-playoff/candidates
//
// No Broadcaster is needed — this is a pure read.
// Callers pass *engine.Engine and *state.Store which satisfy the local
// interfaces by structural match.
func RegisterPublicLeaguePlayoffHandlers(r *gin.RouterGroup, eng LeaguePlayoffEngine, store LeaguePlayoffStore) {
	// GET /competitions/:id/league-playoff/candidates
	// Returns the consequential tied groups for this team-league competition.
	// 200 [] when no ties (or competition is not a team league).
	// 404 when the competition does not exist.
	r.GET("/competitions/:id/league-playoff/candidates", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		comp, err := store.LoadCompetition(id)
		if err != nil {
			// Public endpoint: don't leak internal store error strings.
			log.Printf("league-playoff candidates LoadCompetition(%s): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		// Short-circuit the finalized case: LeaguePlayoffCandidates returns []
		// once shared ranks are accepted, so skip the second standings load.
		if comp.LeaguePlayoffFinalized {
			c.JSON(http.StatusOK, gin.H{"candidates": []leaguePlayoffCandidateGroup{}, "finalized": true})
			return
		}

		candidates, err := eng.LeaguePlayoffCandidates(id)
		if err != nil {
			var notFound *engine.NotFoundError
			if errors.As(err, &notFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			// Public endpoint: opaque 500, log the real cause server-side.
			log.Printf("league-playoff candidates LeaguePlayoffCandidates(%s): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		out := make([]leaguePlayoffCandidateGroup, 0, len(candidates))
		for _, g := range candidates {
			names := make([]string, len(g.Teams))
			for i, t := range g.Teams {
				names[i] = t.Player.Name
			}
			out = append(out, leaguePlayoffCandidateGroup{
				TeamNames:   names,
				MinPosition: g.MinPosition,
				MaxPosition: g.MaxPosition,
			})
		}

		// Finalized flag: surface whether the operator has already accepted
		// shared ranks so the frontend can reflect that state.
		c.JSON(http.StatusOK, gin.H{
			"candidates": out,
			"finalized":  comp.LeaguePlayoffFinalized,
		})
	})
}

// RegisterLeaguePlayoffHandlers wires the three admin-gated league-playoff
// mutation endpoints. Callers pass *engine.Engine and *state.Store which
// satisfy the local interfaces by structural match.
func RegisterLeaguePlayoffHandlers(r *gin.RouterGroup, eng LeaguePlayoffEngine, store LeaguePlayoffStore, hub Broadcaster) {
	// POST /competitions/:id/league-playoff
	// Body: { "teamNames": ["TeamA", "TeamB", ...] }
	// Generates round-robin play-off matches for the selected tied group.
	// Validates that the selection matches exactly one candidate group.
	// 400 if the selection does not match any candidate group.
	// 409 if play-off matches for that group already exist.
	// 404 if the competition does not exist.
	r.POST("/competitions/:id/league-playoff", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		var req leaguePlayoffRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(req.TeamNames) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames must contain at least two teams"})
			return
		}

		// Validate the selection against LeaguePlayoffCandidates BEFORE calling
		// GenerateLeaguePlayoffMatches. The engine does not validate this
		// constraint itself — the handler is the gate.
		candidates, err := eng.LeaguePlayoffCandidates(id)
		if err != nil {
			var notFound *engine.NotFoundError
			if errors.As(err, &notFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		reqSet, hadDup := dedupedNameSet(req.TeamNames)
		if hadDup {
			c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames contains duplicate entries"})
			return
		}

		matched := false
		for _, g := range candidates {
			if len(g.Teams) != len(reqSet) {
				continue
			}
			groupSet := make(map[string]bool, len(g.Teams))
			for _, t := range g.Teams {
				groupSet[t.Player.Name] = true
			}
			allMatch := true
			for n := range reqSet {
				if !groupSet[n] {
					allMatch = false
					break
				}
			}
			if allMatch {
				matched = true
				break
			}
		}
		if !matched {
			c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames does not match any consequential tied group; check GET /league-playoff/candidates"})
			return
		}

		// Guard: refuse if DH matches for every requested pair already exist
		// (all pairs present = idempotent call that already completed).
		existing, err := store.LoadPoolMatches(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		pairsNeeded := len(reqSet) * (len(reqSet) - 1) / 2
		pairsExist := 0
		for _, m := range existing {
			if !engine.IsPoolDaihyosenMatchID(m.ID) {
				continue
			}
			// Check if this DH match is between two teams from the requested group.
			if reqSet[m.SideA] && reqSet[m.SideB] {
				pairsExist++
			}
		}
		if pairsExist >= pairsNeeded {
			c.JSON(http.StatusConflict, gin.H{"error": "playoff_matches_exist", "detail": "play-off matches for this group already exist; delete them first to regenerate"})
			return
		}

		injected, err := eng.GenerateLeaguePlayoffMatches(id, req.TeamNames)
		if err != nil {
			var notFound *engine.NotFoundError
			var validation *engine.ValidationError
			if errors.As(err, &notFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			if errors.As(err, &validation) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id})
		hub.Broadcast(EventScheduleUpdated, nil)

		c.JSON(http.StatusCreated, gin.H{"matches": injected})
	})

	// DELETE /competitions/:id/league-playoff
	// Body: { "teamNames": ["TeamA", "TeamB", ...] }
	// Removes UNSCORED play-off DH matches for the given group.
	// 400 if teamNames has duplicates or names only part of a play-off group.
	// 409 if any match for the group is in progress or has already been scored.
	// 404 if no play-off matches exist for the group.
	r.DELETE("/competitions/:id/league-playoff", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		var req leaguePlayoffRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(req.TeamNames) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames must contain at least two teams"})
			return
		}

		reqSet, hadDup := dedupedNameSet(req.TeamNames)
		if hadDup {
			c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames contains duplicate entries"})
			return
		}

		allMatches, err := store.LoadPoolMatches(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// Identify DH matches belonging to the requested group, and reject a
		// selection that splits a play-off group: a DH match with exactly one
		// side in reqSet means the operator named a partial group, which would
		// orphan the remaining round-robin bouts.
		var groupDH []state.MatchResult
		for _, m := range allMatches {
			if !engine.IsPoolDaihyosenMatchID(m.ID) {
				continue
			}
			inA, inB := reqSet[m.SideA], reqSet[m.SideB]
			if inA != inB {
				c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames does not cover a complete play-off group"})
				return
			}
			if inA && inB {
				groupDH = append(groupDH, m)
			}
		}
		if len(groupDH) == 0 {
			c.JSON(http.StatusNotFound, gin.H{"error": "no_playoff_matches", "detail": "no play-off matches found for this group"})
			return
		}

		// Guard: refuse removal if any match in the group is in progress or has
		// been scored — deleting a running DH match would orphan the operator's
		// open scoring session.
		for _, m := range groupDH {
			if m.Winner != "" || m.Status == state.MatchStatusCompleted || m.Status == state.MatchStatusRunning {
				c.JSON(http.StatusConflict, gin.H{"error": "playoff_match_scored", "detail": "one or more play-off matches for this group are in progress or have been scored; clear scores first"})
				return
			}
		}

		// Build the filtered matches list (remove the group's DH matches).
		dhIDs := make(map[string]bool, len(groupDH))
		for _, m := range groupDH {
			dhIDs[m.ID] = true
		}
		filtered := make([]state.MatchResult, 0, len(allMatches)-len(groupDH))
		for _, m := range allMatches {
			if !dhIDs[m.ID] {
				filtered = append(filtered, m)
			}
		}

		// Persist the filtered pool-match list. Race risk: a concurrent score
		// write could land between our Load and this Save, but the delete is
		// an operator-explicit action and we already guard against scored
		// matches above, so a last-write-wins outcome is acceptable here.
		// A future refactor can wrap this in WithTransaction for stronger
		// atomicity if needed.
		if err := store.SavePoolMatches(id, filtered); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id})
		hub.Broadcast(EventScheduleUpdated, nil)

		c.JSON(http.StatusOK, gin.H{"deleted": len(groupDH)})
	})

	// POST /competitions/:id/league-playoff/finalize
	// Operator accepts the current standings as final without running a
	// play-off. Sets LeaguePlayoffFinalized=true, which makes
	// LeaguePlayoffCandidates return [] on the next call, unblocking
	// MaybeAutoCompletePools to transition to CompStatusComplete.
	r.POST("/competitions/:id/league-playoff/finalize", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		var notFoundFlag bool
		var alreadyComplete bool
		_, err := store.UpdateCompetitionChanged(id, func(comp *state.Competition) (*state.Competition, error) {
			if comp == nil {
				notFoundFlag = true
				return nil, nil
			}
			if comp.Status == state.CompStatusComplete {
				alreadyComplete = true
				return nil, nil
			}
			comp.LeaguePlayoffFinalized = true
			return comp, nil
		})
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if notFoundFlag {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if alreadyComplete {
			c.JSON(http.StatusConflict, gin.H{"error": "already_complete", "detail": "competition is already complete"})
			return
		}

		// Trigger completion via MaybeAutoCompletePools. With
		// LeaguePlayoffFinalized=true, LeaguePlayoffCandidates now returns []
		// and the blocking gate passes through to the completion transition.
		outcome, autoErr := eng.MaybeAutoCompletePools(id)
		if autoErr != nil {
			log.Printf("MaybeAutoCompletePools(%s) after finalize: %v", id, autoErr)
			c.Header(AutoCompleteErrorHeader, AutoCompleteErrorValue)
		} else {
			switch outcome {
			case engine.AutoCompleteTransitioned:
				hub.Broadcast(EventCompetitionCompleted, gin.H{"competitionId": id})
				hub.Broadcast(EventScheduleUpdated, nil)
			case engine.AutoCompleteNoChange:
				// Not all matches complete yet — finalize flag is set but
				// the competition cannot transition until all matches finish.
				hub.Broadcast(EventScheduleUpdated, nil)
			default:
				hub.Broadcast(EventScheduleUpdated, nil)
			}
		}

		c.JSON(http.StatusOK, gin.H{"finalized": true})
	})
}
