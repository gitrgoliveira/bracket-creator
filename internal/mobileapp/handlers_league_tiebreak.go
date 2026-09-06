// Package mobileapp, handlers_league_tiebreak.go owns the four operator
// endpoints for league tie-breaker management (Phase 3b, mp-8rc9):
//
//	GET  /api/competitions/:cid/league-tiebreak/candidates  (public, no auth)
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
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// LeagueTiebreakEngine is the consumer-boundary view of *engine.Engine used
// by the league-tiebreak handler family. Methods are restricted to what these
// four endpoints actually call.
type LeagueTiebreakEngine interface {
	LeagueTiebreakCandidates(compID string) ([]engine.TiedGroup, error)
	GenerateLeagueTiebreakMatches(compID string, tiedTeamNames []string, tiedTeamIDs []string) ([]state.MatchResult, error)
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

// leagueTiebreakTeamRef is the {id,name,dojo} identity shape for one team in
// a candidate group, mirroring the "teams" array GET /chusen-candidates
// already emits (handlers_competition.go) for the same reason: two teams may
// share a display name across dojos (a namesake collision reachable through
// the documented checkNewTeamNameCollisions restore hole), and TeamNames
// alone cannot disambiguate them for the POST selection below.
type leagueTiebreakTeamRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Dojo string `json:"dojo"`
}

// leagueTiebreakCandidateGroup is the JSON shape for one tied group returned
// by GET /league-tiebreak/candidates.
type leagueTiebreakCandidateGroup struct {
	// TeamNames holds the names of the tied teams in standings order. Kept
	// for backward compatibility; Teams (below) carries the same teams with
	// identity, and is what a client selecting a namesake-holding group
	// should use.
	TeamNames []string `json:"teamNames"`
	// Teams holds the same tied teams as TeamNames, with id/dojo alongside
	// each name (bc-idfx) so the operator's selection can name a team
	// unambiguously via POST .../league-tiebreak's teamIds.
	Teams []leagueTiebreakTeamRef `json:"teams"`
	// MinPosition is the 1-based best rank among the tied teams.
	MinPosition int `json:"minPosition"`
	// MaxPosition is the 1-based worst rank among the tied teams.
	MaxPosition int `json:"maxPosition"`
}

// leagueTiebreakRequest is the JSON body for POST /league-tiebreak.
// The operator selects exactly one tied group to tie-break, by team names
// (TeamNames, always required at this HTTP layer -- validated and length-
// checked on every request) or, when a namesake collision makes names
// ambiguous, by participant id (TeamIDs, optional; bc-idfx). When TeamIDs is
// present it is authoritative for both candidate-group matching HERE and
// group resolution in the engine (GenerateLeagueTiebreakMatches never reads
// TeamNames at all in that case, see that method's own doc comment).
// TeamNames being required is this HTTP layer's own rule, not a downstream
// dependency: idempotency dedup against existing DH rows is done by
// generatePoolDaihyosenMatches, which resolves existing rows against the
// already-resolved tied group, not against the raw request's TeamNames.
type leagueTiebreakRequest struct {
	// TeamNames is the set of team names for which to generate tie-breaker
	// matches. Must match exactly one consequential candidate group from
	// LeagueTiebreakCandidates (order does not matter).
	TeamNames []string `json:"teamNames"`
	// TeamIDs is the optional id-aware selection (bc-idfx): when provided,
	// it must be the same length as TeamNames and is used instead of names
	// to identify the candidate group and resolve the tied group.
	TeamIDs []string `json:"teamIds,omitempty"`
}

// dedupedStringSet builds a presence set from a string slice and reports
// whether the input contained any duplicate. Generic over what the strings
// ARE: callers below feed it both team names (dedup by display name) and
// team ids (dedup by participant id) -- a single name-suffixed helper for
// both would misdescribe the id call sites. Handlers reject duplicates up
// front so downstream group-size and pair-count comparisons (which use the
// deduped set) can't be fooled by a repeated entry.
func dedupedStringSet(values []string) (set map[string]bool, hadDuplicate bool) {
	set = make(map[string]bool, len(values))
	for _, v := range values {
		set[v] = true
	}
	return set, len(set) != len(values)
}

// hasBlankEntry reports whether values contains an empty or whitespace-only
// string. teamIds specifically: dedupedStringSet has no opinion on WHAT the
// strings are, so a single "" entry dedupes cleanly and passes straight
// through to the group-membership check as if "" were a real participant
// id -- and every id-less DH row (SideAID/SideBID both "") then matches
// BOTH sides of that check, group membership for a row that names no team
// at all. Reachable concretely on DELETE: a caller sends a single blank
// teamIds entry and every id-less DH row across every group reads as
// "in the selected group", so a request meant to select nothing deletes an
// unrelated group's bout instead (200 {"deleted":2} for zero real ids
// supplied). Checked wherever teamIds is used at all, POST and DELETE
// alike, before it reaches dedupedStringSet.
func hasBlankEntry(values []string) bool {
	for _, v := range values {
		if strings.TrimSpace(v) == "" {
			return true
		}
	}
	return false
}

// tiebreakSelectionField names which request field a selection/duplicate
// error refers to: POST and DELETE both point the operator at teamIds once
// useIDs is in play, since that is the only field able to disambiguate a
// namesake pair.
func tiebreakSelectionField(useIDs bool) string {
	if useIDs {
		return "teamIds"
	}
	return "teamNames"
}

// parseTiebreakSelection binds and validates the {teamNames, teamIds}
// selection body shared by POST and DELETE .../league-tiebreak (M4): length
// parity, blank teamIds entries, and the teamNames/teamIds duplicate checks
// used to be three near-identical blocks, one hand-copied into each
// handler, that had already drifted on wording between the two. This is the
// ONE parse+validate both now share.
//
// On success returns the bound request, whether teamIds selection is in
// play, the deduped team-NAME set (always populated -- POST needs it for
// the idempotency dedup against existing DH rows even when selecting by id,
// see its own comment), and the deduped team-ID set (nil when teamIds was
// not supplied). On any validation failure the JSON error response has
// already been written and ok is false; the caller must return immediately.
func parseTiebreakSelection(c *gin.Context) (req leagueTiebreakRequest, useIDs bool, names, ids map[string]bool, ok bool) {
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return leagueTiebreakRequest{}, false, nil, nil, false
	}
	if len(req.TeamNames) < 2 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "teamNames must contain at least two teams"})
		return leagueTiebreakRequest{}, false, nil, nil, false
	}
	// teamIds (bc-idfx) is optional, but when present it must line up 1:1
	// with teamNames -- the pair is what generatePoolDaihyosenMatches /
	// GenerateLeagueTiebreakMatches ultimately need per team.
	useIDs = len(req.TeamIDs) > 0
	if useIDs && len(req.TeamIDs) != len(req.TeamNames) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "teamIds, when provided, must have the same length as teamNames"})
		return leagueTiebreakRequest{}, false, nil, nil, false
	}
	if useIDs && hasBlankEntry(req.TeamIDs) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "teamIds entries must be non-empty; omit teamIds entirely to select by name"})
		return leagueTiebreakRequest{}, false, nil, nil, false
	}

	var hadDup bool
	names, hadDup = dedupedStringSet(req.TeamNames)
	// A namesake collision (two tied teams sharing a display name across
	// dojos) makes duplicate NAMES the expected, legal shape once teamIds
	// disambiguates them -- the id set's own uniqueness check below is what
	// actually guards this path, so the name-based duplicate rejection only
	// applies when there is nothing to fall back on.
	if !useIDs && hadDup {
		c.JSON(http.StatusBadRequest, gin.H{"error": tiebreakSelectionField(false) + " contains duplicate entries; use teamIds to disambiguate teams that share a name"})
		return leagueTiebreakRequest{}, false, nil, nil, false
	}
	if useIDs {
		ids, hadDup = dedupedStringSet(req.TeamIDs)
		if hadDup {
			c.JSON(http.StatusBadRequest, gin.H{"error": "teamIds contains duplicate entries"})
			return leagueTiebreakRequest{}, false, nil, nil, false
		}
	}
	return req, useIDs, names, ids, true
}

// inGroup returns the per-DH-row membership test shared by POST's
// pairsExist count and DELETE's group-collection loop (M4).
// generatePoolDaihyosenMatches only began stamping SideAID/SideBID on
// 2026-08-29; a DH row written before that carries blank ids. Under
// teamIds selection, a request-level "use ids for everything" decision made
// every id-less row invisible to both handlers (POST undercounted
// pairsExist and bypassed its own 409; DELETE's membership test read every
// id-less row as OUT of the group and answered 404 no_tiebreak_matches even
// though the SPA, which still matches by name, showed the group as
// present). The fix is ROW-level, not request-level: THIS row's own
// SideAID/SideBID decide whether it is judged by id or falls back to the
// name sets, so a tied group that mixes an old id-less row with a newer
// id-carrying one (regenerated after a participant edit) is handled
// correctly row by row rather than picking one basis for the whole request.
func inGroup(useIDs bool, ids, names map[string]bool) func(m state.MatchResult) (inA, inB bool) {
	return func(m state.MatchResult) (inA, inB bool) {
		if useIDs && m.SideAID != "" && m.SideBID != "" {
			return ids[m.SideAID], ids[m.SideBID]
		}
		return names[m.SideA], names[m.SideB]
	}
}

// RegisterPublicLeagueTiebreakHandlers wires the unauthenticated league-tiebreak
// read endpoint on the public api group.
//
//	GET /competitions/:id/league-tiebreak/candidates
//
// No Broadcaster is needed, this is a pure read.
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
			teams := make([]leagueTiebreakTeamRef, len(g.Teams))
			for i, t := range g.Teams {
				names[i] = t.Player.Name
				teams[i] = leagueTiebreakTeamRef{ID: t.Player.ID, Name: t.Player.Name, Dojo: t.Player.Dojo}
			}
			out = append(out, leagueTiebreakCandidateGroup{
				TeamNames:   names,
				Teams:       teams,
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

		req, useTeamIDs, reqSet, reqIDSet, ok := parseTiebreakSelection(c)
		if !ok {
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
		// constraint itself, the handler is the gate.
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

		// The candidate-group match is by id (unambiguous) when teamIds was
		// supplied, else by name -- see leagueTiebreakRequest's doc comment.
		matched := false
		for _, g := range candidates {
			if useTeamIDs {
				if len(g.Teams) != len(reqIDSet) {
					continue
				}
				groupSet := make(map[string]bool, len(g.Teams))
				for _, t := range g.Teams {
					groupSet[t.Player.ID] = true
				}
				allMatch := true
				for id := range reqIDSet {
					if !groupSet[id] {
						allMatch = false
						break
					}
				}
				if allMatch {
					matched = true
					break
				}
				continue
			}
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
			c.JSON(http.StatusBadRequest, gin.H{"error": tiebreakSelectionField(useTeamIDs) + " does not match any consequential tied group; check GET /league-tiebreak/candidates"})
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
		// groupSize/pairsExist must use the SAME identity basis the group was
		// selected by: reqSet collapses a legitimate namesake pair (two
		// entries sharing one display name) down to ONE name, which would
		// under-count both the pairs needed and the pairs already scored.
		groupSize := len(reqSet)
		if useTeamIDs {
			groupSize = len(reqIDSet)
		}
		pairsNeeded := groupSize * (groupSize - 1) / 2
		pairsExist := 0
		// M4: inGroup decides membership per DH ROW (id when THIS row carries
		// both ids, else the name fallback), not once for the whole request
		// -- see inGroup's own doc comment for why a request-level decision
		// under-counted pairsExist for a legacy id-less row.
		inG := inGroup(useTeamIDs, reqIDSet, reqSet)
		for _, m := range existing {
			if !engine.IsPoolDaihyosenMatchID(m.ID) {
				continue
			}
			if inA, inB := inG(m); inA && inB {
				pairsExist++
			}
		}
		if pairsExist >= pairsNeeded {
			c.JSON(http.StatusConflict, gin.H{"error": "tiebreak_matches_exist", "detail": "tie-breaker matches for this group already exist; delete them first to regenerate"})
			return
		}

		injected, err := eng.GenerateLeagueTiebreakMatches(id, req.TeamNames, req.TeamIDs)
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

		_, useTeamIDs, reqSet, dedupedIDSet, ok := parseTiebreakSelection(c)
		if !ok {
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
			// one side in the request set means the operator named a partial
			// group, which would orphan the remaining round-robin bouts.
			// M4: inGroup decides membership per DH ROW (id when THIS row
			// carries both ids, else the name fallback), the SAME closure
			// POST's pairsExist loop uses -- see inGroup's own doc comment.
			var groupDH []state.MatchResult
			inG := inGroup(useTeamIDs, dedupedIDSet, reqSet)
			for _, m := range allMatches {
				if !engine.IsPoolDaihyosenMatchID(m.ID) {
					continue
				}
				inA, inB := inG(m)
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
			// scored, deleting a running DH match would orphan the operator's
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
			c.JSON(http.StatusBadRequest, gin.H{"error": tiebreakSelectionField(useTeamIDs) + " does not cover a complete tie-breaker group"})
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
		var notTeamLeague bool
		var alreadyComplete bool
		_, err := store.UpdateCompetitionChanged(id, func(comp *state.Competition) (*state.Competition, error) {
			if comp == nil {
				notFoundFlag = true
				return nil, nil
			}
			if comp.Format != state.CompFormatLeague || comp.Kind != "team" {
				notTeamLeague = true
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
		if notTeamLeague {
			c.JSON(http.StatusBadRequest, gin.H{"error": "league tie-breaker endpoints apply only to team-league competitions"})
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
				// Not all matches complete yet, finalize flag is set but
				// the competition cannot transition until all matches finish.
				hub.Broadcast(EventScheduleUpdated, nil)
			default:
				hub.Broadcast(EventScheduleUpdated, nil)
			}
		}

		c.JSON(http.StatusOK, gin.H{"finalized": true})
	})
}
