// Package mobileapp — handlers_league_tiebreak.go owns the four operator
// endpoints for league tie-breaker management (Phase 3b, mp-8rc9):
//
//	GET  /api/competitions/:cid/league-tiebreak/candidates  (public — no auth)
//	POST /api/competitions/:cid/league-tiebreak             (admin-gated)
//	DELETE /api/competitions/:cid/league-tiebreak           (admin-gated)
//	POST /api/competitions/:cid/league-tiebreak/finalize    (admin-gated)
//
// The GET read is public (registered via RegisterPublicLeagueTiebreakHandlers
// on the unauthenticated api group, mirroring RegisterPublicSwissHandlers).
// The three write/mutate operations require X-Tournament-Password and are
// registered via RegisterLeagueTiebreakHandlers on the adminSmallBody group.
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

// LeagueTiebreakEngine is the consumer-boundary view of *engine.Engine used
// by the league-tiebreak handler family. Methods are restricted to what these
// four endpoints actually call.
type LeagueTiebreakEngine interface {
	LeagueTiebreakCandidates(compID string) ([]engine.TiedGroup, error)
	GenerateLeagueTiebreakMatches(compID string, tiedTeamNames []string) ([]state.MatchResult, error)
	MaybeAutoCompletePools(compID string) (engine.AutoCompleteOutcome, error)
}

// LeagueTiebreakStore is the consumer-boundary view of *state.Store used by
// the league-tiebreak handler family.
type LeagueTiebreakStore interface {
	LoadCompetition(id string) (*state.Competition, error)
	LoadPoolMatches(id string) ([]state.MatchResult, error)
	SavePoolMatches(id string, matches []state.MatchResult) error
	UpdateCompetitionChanged(id string, transform func(current *state.Competition) (*state.Competition, error)) (bool, error)
	// WithTransaction holds the per-comp lock across a read-modify-write so the
	// DELETE handler's load→guard→filter→save can't lose a concurrent score
	// write that lands mid-sequence.
	WithTransaction(compID string, fn func(tx state.StoreTx) error) error
}

// leagueTiebreakCandidateGroup is the JSON shape for one tied group returned
// by GET /league-tiebreak/candidates.
type leagueTiebreakCandidateGroup struct {
	// TeamNames holds the names of the tied teams in standings order.
	TeamNames []string `json:"teamNames"`
	// MinPosition is the 1-based best rank among the tied teams.
	MinPosition int `json:"minPosition"`
	// MaxPosition is the 1-based worst rank among the tied teams.
	MaxPosition int `json:"maxPosition"`
}

// leagueTiebreakRequest is the JSON body for POST /league-tiebreak.
// The operator selects exactly one tied group (by team names) to tie-break.
type leagueTiebreakRequest struct {
	// TeamNames is the set of team names for which to generate tie-breaker
	// matches. Must match exactly one consequential candidate group from
	// LeagueTiebreakCandidates (order does not matter).
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

// RegisterPublicLeagueTiebreakHandlers wires the unauthenticated league-tiebreak
// read endpoint on the public api group.
//
//	GET /competitions/:id/league-tiebreak/candidates
//
// No Broadcaster is needed — this is a pure read.
// Callers pass *engine.Engine and *state.Store which satisfy the local
// interfaces by structural match.
func RegisterPublicLeagueTiebreakHandlers(r *gin.RouterGroup, eng LeagueTiebreakEngine, store LeagueTiebreakStore) {
	// GET /competitions/:id/league-tiebreak/candidates
	// Returns the consequential tied groups for this team-league competition.
	// 200 [] when no ties (or competition is not a team league).
	// 404 when the competition does not exist.
	r.GET("/competitions/:id/league-tiebreak/candidates", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		comp, err := store.LoadCompetition(id)
		if err != nil {
			// Public endpoint: don't leak internal store error strings.
			log.Printf("league-tiebreak candidates LoadCompetition(%s): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		// Short-circuit the finalized case: LeagueTiebreakCandidates returns []
		// once shared ranks are accepted, so skip the second standings load.
		if comp.LeagueTiebreakFinalized {
			c.JSON(http.StatusOK, gin.H{"candidates": []leagueTiebreakCandidateGroup{}, "finalized": true})
			return
		}

		candidates, err := eng.LeagueTiebreakCandidates(id)
		if err != nil {
			var notFound *engine.NotFoundError
			if errors.As(err, &notFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			// Public endpoint: opaque 500, log the real cause server-side.
			log.Printf("league-tiebreak candidates LeagueTiebreakCandidates(%s): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		out := make([]leagueTiebreakCandidateGroup, 0, len(candidates))
		for _, g := range candidates {
			names := make([]string, len(g.Teams))
			for i, t := range g.Teams {
				names[i] = t.Player.Name
			}
			out = append(out, leagueTiebreakCandidateGroup{
				TeamNames:   names,
				MinPosition: g.MinPosition,
				MaxPosition: g.MaxPosition,
			})
		}

		// Finalized flag: surface whether the operator has already accepted
		// shared ranks so the frontend can reflect that state.
		c.JSON(http.StatusOK, gin.H{
			"candidates": out,
			"finalized":  comp.LeagueTiebreakFinalized,
		})
	})
}

// RegisterLeagueTiebreakHandlers wires the three admin-gated league-tiebreak
// mutation endpoints. Callers pass *engine.Engine and *state.Store which
// satisfy the local interfaces by structural match.
func RegisterLeagueTiebreakHandlers(r *gin.RouterGroup, eng LeagueTiebreakEngine, store LeagueTiebreakStore, hub Broadcaster) {
	// POST /competitions/:id/league-tiebreak
	// Body: { "teamNames": ["TeamA", "TeamB", ...] }
	// Generates round-robin tie-breaker matches for the selected tied group.
	// Validates that the selection matches exactly one candidate group.
	// 400 if the selection does not match any candidate group.
	// 409 if tie-breaker matches for that group already exist.
	// 404 if the competition does not exist.
	r.POST("/competitions/:id/league-tiebreak", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		var req leagueTiebreakRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if len(req.TeamNames) < 2 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames must contain at least two teams"})
			return
		}

		// Guard: this endpoint applies only to team-league competitions.
		// Kind == "team" is the canonical team marker: ValidateCompetitionTeamSize (run
		// on every create/edit) enforces Kind == "team" ⟺ TeamSize >= 2, so the Kind
		// check alone is sufficient.
		postComp, err := store.LoadCompetition(id)
		if err != nil {
			log.Printf("league-tiebreak POST LoadCompetition(%s): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if postComp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if postComp.Format != state.CompFormatLeague || postComp.Kind != "team" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "league tie-breaker endpoints apply only to team-league competitions"})
			return
		}

		// Validate the selection against LeagueTiebreakCandidates BEFORE calling
		// GenerateLeagueTiebreakMatches. The engine does not validate this
		// constraint itself — the handler is the gate.
		candidates, err := eng.LeagueTiebreakCandidates(id)
		if err != nil {
			var notFound *engine.NotFoundError
			if errors.As(err, &notFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			log.Printf("league-tiebreak POST LeagueTiebreakCandidates(%s): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
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
			c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames does not match any consequential tied group; check GET /league-tiebreak/candidates"})
			return
		}

		// Guard: refuse if DH matches for every requested pair already exist
		// (all pairs present = idempotent call that already completed).
		existing, err := store.LoadPoolMatches(id)
		if err != nil {
			log.Printf("league-tiebreak POST LoadPoolMatches(%s): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
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
			c.JSON(http.StatusConflict, gin.H{"error": "tiebreak_matches_exist", "detail": "tie-breaker matches for this group already exist; delete them first to regenerate"})
			return
		}

		injected, err := eng.GenerateLeagueTiebreakMatches(id, req.TeamNames)
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
			log.Printf("league-tiebreak POST GenerateLeagueTiebreakMatches(%s): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id})
		hub.Broadcast(EventScheduleUpdated, nil)

		c.JSON(http.StatusCreated, gin.H{"matches": injected})
	})

	// DELETE /competitions/:id/league-tiebreak
	// Body: { "teamNames": ["TeamA", "TeamB", ...] }
	// Removes UNSCORED tie-breaker DH matches for the given group.
	// 400 if teamNames has duplicates or names only part of a tie-breaker group.
	// 409 if any match for the group is in progress or has already been scored.
	// 404 if no tie-breaker matches exist for the group.
	r.DELETE("/competitions/:id/league-tiebreak", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		var req leagueTiebreakRequest
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

		// The whole read-modify-write runs under the per-comp lock so a
		// concurrent score write can't land between the load and the save and be
		// lost when we rewrite the pool-match list.
		var (
			compMissing    bool
			notTeamLeague  bool
			partialGroup   bool
			noneFound      bool
			scoredConflict bool
			deleted        int
		)
		txErr := store.WithTransaction(id, func(stx state.StoreTx) error {
			comp, err := stx.LoadCompetition(id)
			if err != nil {
				return err
			}
			if comp == nil {
				compMissing = true
				return nil
			}
			// This endpoint is league-only. Without this guard an operator could
			// delete a MIXED team competition's auto-injected DH matches through
			// the league tie-breaker endpoint.
			if comp.Format != state.CompFormatLeague || comp.Kind != "team" {
				notTeamLeague = true
				return nil
			}

			allMatches, err := stx.LoadPoolMatches(id)
			if err != nil {
				return err
			}

			// Identify DH matches belonging to the requested group, and reject a
			// selection that splits a tie-breaker group: a DH match with exactly
			// one side in reqSet means the operator named a partial group, which
			// would orphan the remaining round-robin bouts.
			var groupDH []state.MatchResult
			for _, m := range allMatches {
				if !engine.IsPoolDaihyosenMatchID(m.ID) {
					continue
				}
				inA, inB := reqSet[m.SideA], reqSet[m.SideB]
				if inA != inB {
					partialGroup = true
					return nil
				}
				if inA && inB {
					groupDH = append(groupDH, m)
				}
			}
			if len(groupDH) == 0 {
				noneFound = true
				return nil
			}

			// Refuse removal if any match in the group is in progress or has been
			// scored — deleting a running DH match would orphan the operator's
			// open scoring session.
			for _, m := range groupDH {
				if m.Winner != "" || m.Status == state.MatchStatusCompleted || m.Status == state.MatchStatusRunning {
					scoredConflict = true
					return nil
				}
			}

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
			if err := stx.SavePoolMatches(id, filtered); err != nil {
				return err
			}
			deleted = len(groupDH)
			return nil
		})
		if txErr != nil {
			log.Printf("league-tiebreak DELETE WithTransaction(%s): %v", id, txErr)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		switch {
		case compMissing:
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		case notTeamLeague:
			c.JSON(http.StatusBadRequest, gin.H{"error": "league tie-breaker endpoints apply only to team-league competitions"})
			return
		case partialGroup:
			c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames does not cover a complete tie-breaker group"})
			return
		case noneFound:
			c.JSON(http.StatusNotFound, gin.H{"error": "no_tiebreak_matches", "detail": "no tie-breaker matches found for this group"})
			return
		case scoredConflict:
			c.JSON(http.StatusConflict, gin.H{"error": "tiebreak_match_scored", "detail": "one or more tie-breaker matches for this group are in progress or have been scored; clear scores first"})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id})
		hub.Broadcast(EventScheduleUpdated, nil)

		c.JSON(http.StatusOK, gin.H{"deleted": deleted})
	})

	// POST /competitions/:id/league-tiebreak/finalize
	// Operator accepts the current standings as final without running a
	// tie-breaker. Sets LeagueTiebreakFinalized=true, which makes
	// LeagueTiebreakCandidates return [] on the next call, unblocking
	// MaybeAutoCompletePools to transition to CompStatusComplete.
	r.POST("/competitions/:id/league-tiebreak/finalize", func(c *gin.Context) {
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
			comp.LeagueTiebreakFinalized = true
			return comp, nil
		})
		if err != nil {
			log.Printf("league-tiebreak finalize UpdateCompetitionChanged(%s): %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
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
		// LeagueTiebreakFinalized=true, LeagueTiebreakCandidates now returns []
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
