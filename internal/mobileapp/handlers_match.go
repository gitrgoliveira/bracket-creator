package mobileapp

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// annotateQueuePositions fills in MatchResult.QueuePosition for each
// element of matches in-place, delegating to state.DeriveQueuePositions
// for the per-court (status priority, ScheduledAt, original index) sort.
//
// FR-025, T036: queue positions are derived at serve time rather than
// persisted — a stored value would go stale the instant any match
// transitions and we'd have to recompute on every score write anyway.
// Match-list endpoints (handlers_viewer.go: GET /competitions and
// GET /competitions/:id) call this just before c.JSON so viewers see
// "next up: 3" without any background recomputation job. Score-write
// endpoints return a single MatchResult and intentionally do NOT
// annotate (a single match has no list ordering to derive against).
func annotateQueuePositions(matches []state.MatchResult) {
	if len(matches) == 0 {
		return
	}
	positions := state.DeriveQueuePositions(matches)
	for i := range matches {
		matches[i].QueuePosition = positions[i]
	}
}

// annotateBracketQueuePositions fills in BracketMatch.QueuePosition for each
// bracket match in-place. Non-scheduled matches are explicitly reset to 0 so
// any stale value previously persisted in bracket.json (or written by future
// code paths) cannot leak back out to clients via the omitempty JSON tag.
//
// The ordering basis matches the viewer's ScheduleViewer (web-mobile/js/
// viewer.jsx around the byCourt sort): pointers to all bracket matches are
// gathered, then sorted per-court by (status priority, ScheduledAt, round,
// position) before the per-court counter is incremented. This keeps the
// "Next up / N before yours" label consistent with the row order the viewer
// actually renders, even when bracket matches are scheduled out of round
// order (e.g., a finals court that started 30 minutes early).
func annotateBracketQueuePositions(b *state.Bracket) {
	if b == nil {
		return
	}

	// Group pointers per court, preserving the round/position pair as a
	// stable tie-break key. We can't sort b.Rounds itself — the bracket
	// tree structure is load-bearing for the renderer.
	type entry struct {
		m        *state.BracketMatch
		round    int
		position int
	}
	byCourt := make(map[string][]entry)
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			byCourt[m.Court] = append(byCourt[m.Court], entry{m: m, round: ri, position: mi})
		}
	}

	statusOrder := func(s state.MatchStatus) int {
		switch s {
		case state.MatchStatusRunning:
			return 0
		case state.MatchStatusScheduled:
			return 1
		default: // completed and any future status
			return 2
		}
	}

	for _, entries := range byCourt {
		sort.SliceStable(entries, func(i, j int) bool {
			oi, oj := statusOrder(entries[i].m.Status), statusOrder(entries[j].m.Status)
			if oi != oj {
				return oi < oj
			}
			// Empty scheduledAt sinks to the end (mirrors the JS
			// fallback to "99:99" in ScheduleViewer's sort).
			ai := entries[i].m.ScheduledAt
			aj := entries[j].m.ScheduledAt
			if ai == "" {
				ai = "99:99"
			}
			if aj == "" {
				aj = "99:99"
			}
			if ai != aj {
				return ai < aj
			}
			if entries[i].round != entries[j].round {
				return entries[i].round < entries[j].round
			}
			return entries[i].position < entries[j].position
		})

		counter := 0
		for _, e := range entries {
			if e.m.Status == state.MatchStatusScheduled {
				counter++
				e.m.QueuePosition = counter
			} else {
				e.m.QueuePosition = 0
			}
		}
	}
}

// enchoExceedsCap reports whether an encho block would exceed the
// competition's MaxEnchoPeriods cap. Returns false (within limit) when
// encho is unset, comp is nil, the cap is 0 (unlimited — FIK default),
// the count is within the cap, or force is set. T104/CHK029.
func enchoExceedsCap(encho *state.EnchoMetadata, comp *state.Competition, force bool) bool {
	if encho == nil || encho.PeriodCount <= 0 {
		return false
	}
	if comp == nil || comp.MaxEnchoPeriods <= 0 {
		return false
	}
	return encho.PeriodCount > comp.MaxEnchoPeriods && !force
}

// anySubBoutEnchoExceedsCap returns true if any sub-result's encho
// period count exceeds the competition cap. The same cap applies
// per-sub-bout because each bout is a standalone overtime bout.
func anySubBoutEnchoExceedsCap(subResults []state.SubMatchResult, comp *state.Competition, force bool) bool {
	for i := range subResults {
		if enchoExceedsCap(subResults[i].Encho, comp, force) {
			return true
		}
	}
	return false
}

// anySubBoutHasEncho reports whether any sub-result carries encho with at
// least one period. Used to decide whether the cap check needs to load the
// competition at all — ordinary team scoring (every bout has SubResults but
// no encho) must not pay that store load.
func anySubBoutHasEncho(subResults []state.SubMatchResult) bool {
	for i := range subResults {
		if subResults[i].Encho != nil && subResults[i].Encho.PeriodCount > 0 {
			return true
		}
	}
	return false
}

// enforceEnchoCap is the gin-handler wrapper around enchoExceedsCap for
// the single-result score / decision endpoints. Loads the competition
// once, checks the top-level encho and every sub-bout encho against the
// cap (writing 500 on store failure, 400 on cap exceeded).
// Returns true if the handler should continue.
func enforceEnchoCap(c *gin.Context, store CompetitionStore, id string, encho *state.EnchoMetadata, force bool) bool {
	return enforceEnchoCapWithSubs(c, store, id, encho, nil, force)
}

// enforceEnchoCapWithSubs is the variant used by the score endpoint. It
// checks both the top-level encho and each sub-result's encho against the
// competition cap in a single competition load.
func enforceEnchoCapWithSubs(c *gin.Context, store CompetitionStore, id string, encho *state.EnchoMetadata, subs []state.SubMatchResult, force bool) bool {
	needsCheck := (encho != nil && encho.PeriodCount > 0) || anySubBoutHasEncho(subs)
	if !needsCheck {
		return true
	}
	comp, err := store.LoadCompetition(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate encho limits"})
		return false
	}
	if enchoExceedsCap(encho, comp, force) || anySubBoutEnchoExceedsCap(subs, comp, force) {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "max_encho_exceeded",
			"limit": comp.MaxEnchoPeriods,
		})
		return false
	}
	return true
}

// tryAutoCompletePools runs the auto-complete check after a successful score
// write. The score itself has already been recorded, so we don't fail the
// request when the auto-complete check errors; instead we log full details
// server-side and set AutoCompleteErrorHeader to a generic sentinel so
// clients can detect the failure (and refresh) without us leaking
// internal store details. Broadcasts EventCompetitionCompleted when the
// transition actually happens.
//
// Takes the consumer-boundary interfaces (T014) so handler tests can
// stub the engine + hub without spinning up the full state/engine
// stack. Production code passes `*engine.Engine` and `*Hub` which
// satisfy the interfaces by structural match.
func tryAutoCompletePools(c *gin.Context, eng ScoringEngine, hub Broadcaster, compID string) {
	outcome, err := eng.MaybeAutoCompletePools(compID)
	if err != nil {
		log.Printf("MaybeAutoCompletePools(%s): %v", compID, err)
		c.Header(AutoCompleteErrorHeader, AutoCompleteErrorValue)
		return
	}
	switch outcome {
	case engine.AutoCompleteTransitioned:
		hub.Broadcast(EventCompetitionCompleted, gin.H{"competitionId": compID})
	case engine.AutoCompleteTiebreakInjected:
		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": compID})
		hub.Broadcast(EventScheduleUpdated, nil)
	case engine.AutoCompleteKnockoutStarted:
		// The LAST pool was just seeded → status moved pools → playoffs (only
		// knockout matches remain). Tell clients to reload the now-fully-live
		// competition.
		hub.Broadcast(EventCompetitionStarted, gin.H{"competitionId": compID})
		hub.Broadcast(EventScheduleUpdated, nil)
	case engine.AutoCompletePoolsResolved:
		// Some (not all) pools were seeded into the knockout, and/or tiebreakers
		// were injected. The bracket/schedule changed and newly-playable knockout
		// matches may now be live — refresh without a full status change.
		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": compID})
		hub.Broadcast(EventScheduleUpdated, nil)
	}
}

// RegisterMatchHandlers wires up the score / quick-score / bulk-score /
// court / override-winner / time endpoints under the admin group.
//
// The score endpoint is the Slice 0 / NFR-002 demonstration of the
// interface-based dependency injection pattern (T017): it consumes
// `ScoringEngine` and `Broadcaster` rather than the concrete
// `*engine.Engine` and `*Hub`, plus the `ScoreRequest.Validate()`
// pattern (T015 / NFR-004) for request-shape validation.
//
// The remaining endpoints in this file still hold concrete pointers
// (the function signature accepts the concrete `*engine.Engine` for
// methods not yet on the interface). Later slices migrate those one at
// a time; the concrete `*engine.Engine` remains a drop-in
// implementation of `ScoringEngine` so the `tryAutoCompletePools` and
// score endpoint paths can already accept the interface today.
func RegisterMatchHandlers(r *gin.RouterGroup, eng *engine.Engine, store CompetitionStore, tx CompetitionTransactor, hub *Hub, verifier PasswordVerifier, tl TournamentLoader) {
	r.POST("/competitions/:id/matches/bulk-score", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		var results []state.MatchResult
		if err := c.ShouldBindJSON(&results); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Defense-in-depth: bulk-score writes straight to disk via
		// RecordMatchResult, bypassing ScoreRequest.Validate's length
		// caps. Reuse the same caps here so a 1MB sideA/winner can't
		// land. Per-result rejection keeps the partial-success semantics
		// (good entries still succeed, bad ones surface in `errors`).
		type scoreError struct {
			MatchID string `json:"matchId"`
			Error   string `json:"error"`
		}
		var errs []scoreError
		// Only successfully-recorded results go into the SSE broadcast so
		// clients never patch with values the engine rejected.
		var successful []state.MatchResult
		// Collect eligibility changes so we can broadcast
		// EventCompetitorStatusUpdated for every kiken/fusenpai result in
		// the batch — mirrors the single-score handler (T085/T092).
		var eligibilityUpdates []*domain.CompetitorStatus

		comp, err := store.LoadCompetition(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to validate encho limits"})
			return
		}
		force := c.Query("force") == "true"

		for i := range results {
			// T104/CHK029: enforce MaxEnchoPeriods cap on bulk-score payload
			// (top-level and each sub-bout independently).
			if enchoExceedsCap(results[i].Encho, comp, force) || anySubBoutEnchoExceedsCap(results[i].SubResults, comp, force) {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: "max_encho_exceeded"})
				continue
			}

			if err := validateBulkScoreLengths(&results[i]); err != nil {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: err.Error()})
				continue
			}

			// mp-ic5b: the correction-reason gate and the write run under the
			// same per-comp lock so the status read is race-free against a
			// concurrent PUT /score. Per-result transactions preserve the
			// existing {succeeded, errors[]} partial-success response shape.
			results[i].CorrectionReason = strings.TrimSpace(results[i].CorrectionReason)
			var capturedStatus *domain.CompetitorStatus
			if err := tx.WithTransaction(id, func(stx state.StoreTx) error {
				if results[i].Status != state.MatchStatusCompleted {
					results[i].CorrectionReason = ""
				} else {
					existing := lookupMatchStatusUnderTx(stx, id, results[i].ID)
					if existing == state.MatchStatusCompleted {
						if results[i].CorrectionReason == "" {
							return errors.New("correcting a completed match result requires a non-empty correctionReason")
						}
					} else {
						results[i].CorrectionReason = "" // first finalization — not a correction
					}
				}
				status, err := eng.RecordMatchResultWithIneligibilityTx(stx, id, results[i].ID, &results[i])
				if err == nil {
					capturedStatus = status
				}
				return err
			}); err != nil {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: err.Error()})
				continue
			}
			successful = append(successful, results[i])
			if capturedStatus != nil {
				eligibilityUpdates = append(eligibilityUpdates, capturedStatus)
			}
		}

		if len(successful) > 0 {
			hub.Broadcast(EventMatchUpdated, gin.H{
				"competitionId": id,
				"results":       matchesForBroadcast(successful),
			})
			tryAutoCompletePools(c, eng, hub, id)
		}
		for _, status := range eligibilityUpdates {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        status,
			})
		}
		c.JSON(http.StatusOK, gin.H{"succeeded": len(successful), "errors": errs})
	})

	r.PUT("/competitions/:id/matches/:mid/quick-score", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		var req struct {
			SideA     string `json:"sideA"`
			SideB     string `json:"sideB"`
			TeamAWins int    `json:"teamAWins"`
			TeamBWins int    `json:"teamBWins"`
			Draws     int    `json:"draws"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.SideA == "" || req.SideB == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sideA and sideB are required"})
			return
		}
		if err := validateMaxLen("sideA", req.SideA, MaxLenMatchSide); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validateMaxLen("sideB", req.SideB, MaxLenMatchSide); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		const maxBouts = 100
		if req.TeamAWins < 0 || req.TeamBWins < 0 || req.Draws < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "win/draw counts must be non-negative"})
			return
		}
		if req.TeamAWins > maxBouts || req.TeamBWins > maxBouts || req.Draws > maxBouts {
			c.JSON(http.StatusBadRequest, gin.H{"error": "individual bout count exceeds maximum"})
			return
		}
		total := req.TeamAWins + req.TeamBWins + req.Draws
		if total > maxBouts {
			c.JSON(http.StatusBadRequest, gin.H{"error": "total bout count exceeds maximum"})
			return
		}

		// Determine team winner per kendo rules: most individual wins wins.
		// winnerSide records the WINNING SIDE (not just the name) so the
		// engine can stamp WinnerID even when both sides share a name —
		// the name alone can't tell two same-name participants apart.
		winner := ""
		winnerSide := ""
		switch {
		case req.TeamAWins > req.TeamBWins:
			winner = req.SideA
			winnerSide = "A"
		case req.TeamBWins > req.TeamAWins:
			winner = req.SideB
			winnerSide = "B"
		}

		// Synthesise SubResults so standings IV/IL/IT counts are correct.
		// Sub-bout SideA/SideB are left empty — individual bout sides are
		// unknown in quick-score mode (no lineup). Winner attribution in
		// computeStandings uses `sub.Winner == m.SideA` (the match-level
		// name); the `sub.Winner == sub.SideA` fallback is guarded against
		// the "" == "" false-positive.
		subResults := make([]state.SubMatchResult, 0, total)
		pos := 1
		for range req.TeamAWins {
			subResults = append(subResults, state.SubMatchResult{Position: pos, Winner: req.SideA})
			pos++
		}
		for range req.TeamBWins {
			subResults = append(subResults, state.SubMatchResult{Position: pos, Winner: req.SideB})
			pos++
		}
		for range req.Draws {
			subResults = append(subResults, state.SubMatchResult{Position: pos})
			pos++
		}

		result := state.MatchResult{
			ID:         mid,
			SideA:      req.SideA,
			SideB:      req.SideB,
			Winner:     winner,
			WinnerSide: winnerSide,
			Status:     state.MatchStatusCompleted,
			SubResults: subResults,
		}
		if err := eng.RecordMatchResult(id, mid, &result); err != nil {
			if errors.Is(err, engine.ErrMatchSideMismatch) {
				c.JSON(http.StatusConflict, gin.H{
					"error":   "side_mismatch",
					"message": "The submitted competitors don't match this match's pairing. Reload and try again.",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id, "matchId": mid})
		tryAutoCompletePools(c, eng, hub, id)
		c.JSON(http.StatusOK, result)
	})

	// Score endpoint — Slice 0 demonstration of the interface-DI +
	// Validate() pattern (T015 / T017 / NFR-002 / NFR-004). Calls go
	// through ScoringEngine and Broadcaster (the consumer-boundary
	// interfaces from deps.go) rather than the concrete types, and the
	// request body is validated via ScoreRequest.Validate() before any
	// engine call. The closure captures `*engine.Engine` / `*Hub` and
	// adapts them to the interfaces at the call boundary — same wire
	// behaviour as before. T156 added the CompetitionTransactor `tx`
	// parameter so the match-write + ineligibility-write + lineup-freeze
	// commit under one per-comp lock acquire.
	registerScoreHandler(r, eng, store, tx, hub, verifier, tl)

	r.PUT("/competitions/:id/matches/:mid/court", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")

		var req struct {
			Court string `json:"court"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Cap defensively — the tournament-level validateCourtLabels
		// enforces single-char labels but per-match court strings have
		// historically accepted longer values in engine tests (e.g.
		// "Court Z"). 32 is generous enough not to break any real
		// caller while rejecting abusive payloads.
		if err := validateMaxLen("court", req.Court, MaxLenMatchScheduledAt); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.UpdateMatchCourt(id, mid, req.Court); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"court":         req.Court,
		})

		c.Status(http.StatusOK)
	})

	r.PUT("/competitions/:id/matches/:mid/override-winner", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		var req struct {
			WinnerName string `json:"winnerName"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Trim whitespace from the winner name. Downstream comparisons
		// (m.Winner == m.SideA / m.SideB in engine/scoring.go and
		// engine/ranking.go) are exact-string equality, so a padded
		// "  Foo  " won't match the canonical "Foo" from the roster —
		// bracket math silently breaks. The JS prompt site at
		// admin_competition.jsx now trims client-side, but a
		// hand-crafted API call could still hit this. Mirrors the
		// override-rank handler's TrimSpace pattern.
		winnerName := strings.TrimSpace(req.WinnerName)
		if winnerName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "winnerName is required"})
			return
		}
		if err := validateMaxLen("winnerName", winnerName, MaxLenMatchSide); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.OverrideBracketWinner(id, mid, winnerName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusOK)
	})

	r.PUT("/competitions/:id/matches/:mid/time", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		var req struct {
			ScheduledAt string `json:"scheduledAt"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := validateMaxLen("scheduledAt", req.ScheduledAt, MaxLenMatchScheduledAt); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.UpdateMatchTime(id, mid, req.ScheduledAt); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventScheduleUpdated, nil)
		c.Status(http.StatusOK)
	})
}

// enforceSelfRunPolicy applies the self-run decision allowlist when the
// tournament is in self-run mode and the request carries no valid admin
// password. Returns the resultSource string ("admin" or "self-reported")
// and true on success; writes the HTTP error response and returns "",
// false when the request should be rejected.
//
// The finalized-result guard is NOT checked here — it must run inside
// WithTransaction to prevent TOCTOU races between concurrent anonymous
// submissions. See checkFinalizedUnderTx.
//
// Called after ScoreRequest.Validate() so the request is structurally valid.
//
// In officiated mode this is a pass-through that returns "admin", true.
// On LoadTournament error the function fails closed (500).
func enforceSelfRunPolicy(c *gin.Context, tl TournamentLoader, verifier PasswordVerifier, req *ScoreRequest) (string, bool) {
	t, err := tl.LoadTournament()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load tournament config"})
		return "", false
	}
	if t == nil || t.Mode != "self-run" {
		return "admin", true
	}

	// Self-run mode: check whether the caller has a valid admin password.
	ok, verr := verifier.Verify(c.GetHeader("X-Tournament-Password"))
	if verr != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "auth verification failed"})
		return "", false
	}
	if ok {
		return "admin", true
	}

	// Anonymous caller in self-run mode: enforce decision allowlist on
	// the top-level decision AND every sub-result decision.
	if !IsSelfRunReportableDecision(req.Decision, req.DecidedByHantei) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "decision type not allowed in self-run mode without admin password"})
		return "", false
	}
	for i := range req.SubResults {
		sub := &req.SubResults[i]
		if !IsSelfRunReportableSubDecision(sub.Decision, sub.DecidedByHantei, sub.Position) {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("subResults[%d]: decision type not allowed in self-run mode without admin password", i)})
			return "", false
		}
	}

	return "self-reported", true
}

// errResultFinalized is a sentinel returned by checkFinalizedUnderTx to
// signal that the match is already finalized and the anonymous overwrite
// should be rejected with 409.
var errResultFinalized = errors.New("result_finalized")

// checkFinalizedUnderTx runs inside WithTransaction (under the per-comp
// lock) so it's safe from TOCTOU races. Returns errResultFinalized when
// an anonymous caller tries to write to a completed match. Fails closed:
// a load error rejects the request rather than allowing an overwrite.
func checkFinalizedUnderTx(stx state.StoreTx, compID, matchID string) error {
	poolMatches, err := stx.LoadPoolMatches(compID)
	if err != nil {
		return fmt.Errorf("finalized guard: load pool matches: %w", err)
	}
	for i := range poolMatches {
		if poolMatches[i].ID == matchID && isMatchFinalized(&poolMatches[i]) {
			return errResultFinalized
		}
	}
	bracket, err := stx.LoadBracket(compID)
	if err != nil {
		return fmt.Errorf("finalized guard: load bracket: %w", err)
	}
	if bracket != nil {
		for _, round := range bracket.Rounds {
			for i := range round {
				if round[i].ID == matchID {
					mr := bracketMatchToResult(&round[i])
					if isMatchFinalized(mr) {
						return errResultFinalized
					}
				}
			}
		}
	}
	return nil
}

// bracketMatchToResult projects the fields of a BracketMatch that the
// finalized guard cares about into a MatchResult so the guard can use a
// uniform type.
func bracketMatchToResult(bm *state.BracketMatch) *state.MatchResult {
	return &state.MatchResult{
		ID:       bm.ID,
		Winner:   bm.Winner,
		Decision: bm.Decision,
		Status:   bm.Status,
	}
}

// isMatchFinalized reports whether the given result represents a concluded
// match. Any completed match is finalized — anonymous callers must not
// overwrite it regardless of whether a winner was explicitly recorded.
func isMatchFinalized(r *state.MatchResult) bool {
	return r.Status == state.MatchStatusCompleted
}

// lookupMatchStatusUnderTx reads the current status of matchID from
// the pool-matches CSV or bracket JSON (in that order) without taking
// any additional lock (caller MUST hold the per-comp lock via
// WithTransaction). Returns the empty MatchStatus "" when the match
// cannot be found in either store — callers treat an unknown match as
// "not yet completed" (the engine will reject it via errMatchNotFound
// on the actual score write, so we don't need to fail here).
func lookupMatchStatusUnderTx(stx state.StoreTx, compID, matchID string) state.MatchStatus {
	poolMatches, err := stx.LoadPoolMatches(compID)
	if err == nil {
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				return poolMatches[i].Status
			}
		}
	}
	bracket, err := stx.LoadBracket(compID)
	if err == nil && bracket != nil {
		for _, round := range bracket.Rounds {
			for i := range round {
				if round[i].ID == matchID {
					return round[i].Status
				}
			}
		}
	}
	return ""
}

// registerScoreHandler wires the `PUT /competitions/:id/matches/:mid/score`
// endpoint via the consumer-boundary interfaces (T014/T017) instead of
// the concrete `*engine.Engine` / `*Hub`. This is the Slice 0
// demonstration of the interface-DI pattern (NFR-002): handler tests
// can drive this code path with a stub ScoringEngine + Broadcaster, no
// temp dirs, no real engine wiring.
//
// Behaviour is identical to the pre-Slice-0 version except for the new
// ScoreRequest.Validate() call, which surfaces a 400 with the field
// name when the body is malformed against its own shape rules
// (Status outside the documented enum, Winner not naming either side).
// The engine's preserve-on-empty-side fallback continues to handle the
// "client sends scoring fields only" case.
//
// T156: the match-write + ineligibility-check-and-set + (T128) lineup-
// freeze now run inside one Store.WithTransaction so they all commit
// under a single per-comp lock acquire. The kachinuki advance + auto-
// complete-pools post-writes deliberately run AFTER the tx — both
// reach for other per-comp locked operations (UpdatePoolMatchByID,
// UpdateCompetitionChanged) which would deadlock inside the tx, and
// they're already structured as best-effort side effects with their
// own non-fatal failure-handling. Bulk-score handler is intentionally
// NOT migrated: the partial-success error array semantics need a
// per-result tx (or a different commit shape) and that's out of scope
// for this slice.
func registerScoreHandler(r *gin.RouterGroup, eng ScoringEngine, store CompetitionStore, tx CompetitionTransactor, hub Broadcaster, verifier PasswordVerifier, tl TournamentLoader) {
	r.PUT("/competitions/:id/matches/:mid/score", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")

		var req ScoreRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if err := req.Validate(); err != nil {
			// Map ValidationError → 400 with the validator's message.
			// Engine errors below remain 500 (they surface I/O / state
			// failures, not request-shape errors).
			var verr *ValidationError
			if errors.As(err, &verr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error()})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// T104/CHK029: enforce MaxEnchoPeriods cap. The cap is a
		// per-competition setting; 0 means unlimited (FIK default). The
		// operator can override by sending ?force=true after confirming
		// the warning banner — the UI's job is to surface that prompt
		// when the cap is reached.
		if !enforceEnchoCapWithSubs(c, store, id, req.Encho, req.SubResults, c.Query("force") == "true") {
			return
		}

		// mp-ba3: self-run decision allowlist + result provenance. Runs
		// after Validate() so the request is structurally valid.
		resultSource, ok := enforceSelfRunPolicy(c, tl, verifier, &req)
		if !ok {
			return
		}

		result := req.AsMatchResult()
		result.ResultSource = resultSource
		// Normalize the audit reason once, before validation and the engine
		// write, so a whitespace-only reason can't satisfy the correction gate
		// and the persisted value never carries leading/trailing whitespace.
		result.CorrectionReason = strings.TrimSpace(result.CorrectionReason)
		// A correctionReason is meaningful only on a correction (an overwrite of
		// an already-completed result). A non-completed write can never be one,
		// so don't persist a client-supplied reason there.
		if result.Status != state.MatchStatusCompleted {
			result.CorrectionReason = ""
		}

		isWithdrawal := domain.IsKikenDecisionStr(result.Decision) || result.Decision == "fusenpai"

		// FR-035: WithCourtExclusivityLock serializes the cross-competition
		// court-busy check + per-competition write under a tournament-level
		// mutex so two concurrent match-starts on the same court in different
		// competitions can't both pass the cross-comp check before either
		// commits (TOCTOU). Withdrawal decisions skip the court gate — operators
		// must record kiken/fusenpai regardless of court state.
		var (
			engStatus *domain.CompetitorStatus
			engErr    error
		)
		txErr := tx.WithCourtExclusivityLock(func() error {
			if !isWithdrawal {
				if err := eng.CheckCrossCompCourtBusy(id, mid); err != nil {
					return err
				}
			}
			// T156: run the score write + ineligibility update + lineup-freeze
			// inside a single per-comp lock acquire via WithTransaction. The
			// engine's RecordMatchResultWithIneligibilityTx dispatches every
			// store call through `stx`, so no internal call re-acquires the
			// lock (non-reentrant; nesting would deadlock).
			//
			// FR-035: intra-competition eligibility and court gate. Checks that
			// no OTHER match in compID's own pool/bracket is running on the same
			// court, plus participant ineligibility. Withdrawal decisions bypass
			// so operators can record kiken on matches with ineligible participants.
			return tx.WithTransaction(id, func(stx state.StoreTx) error {
				// mp-ba3: finalized guard runs under the per-comp lock to
				// prevent TOCTOU races between concurrent anonymous submissions.
				if resultSource == "self-reported" {
					if err := checkFinalizedUnderTx(stx, id, mid); err != nil {
						engErr = err
						return nil
					}
				}
				// Correction audit: overwriting an already-completed result
				// (completed -> completed) is a correction and requires a non-empty
				// CorrectionReason for traceability. This applies to ANY decision
				// type, including a withdrawal (kiken/fusenpai) submitted via /score
				// — exempting those would let a finalized result be overwritten with
				// no audit reason. The check runs inside the tx so the is-completed
				// read is race-free (same lock). A first finalization (existing
				// status is not completed) needs no reason.
				if result.Status == state.MatchStatusCompleted {
					existing := lookupMatchStatusUnderTx(stx, id, mid)
					if existing == state.MatchStatusCompleted {
						// Overwriting a finalized result is a correction — require a reason.
						if result.CorrectionReason == "" {
							engErr = &ValidationError{
								Field:   "correctionReason",
								Message: "correcting a completed match result requires a non-empty correctionReason",
							}
							return nil
						}
					} else {
						// First finalization — not a correction. The contract says the
						// reason is omitted here, so drop any client-supplied value.
						result.CorrectionReason = ""
					}
				}
				if !isWithdrawal {
					if err := eng.StartMatchTx(stx, id, mid); err != nil {
						engErr = err
						return nil
					}
				}
				engStatus, engErr = eng.RecordMatchResultWithIneligibilityTx(stx, id, mid, result)
				// engErr is a normal application-level signal (AlreadyIneligible
				// → 409, validation/not-found → other codes); we surface it
				// after the tx returns. The score-write inside the tx already
				// includes the K3 rollback for the AlreadyIneligible path —
				// returning nil here commits whatever final state the engine
				// settled on.
				return nil
			})
		})
		if txErr != nil {
			// txErr carries errors from CheckCrossCompCourtBusy (cross-comp
			// court conflict or match-not-found) or from the WithTransaction
			// infrastructure itself (WAL commit failure, etc.).
			var courtBusyErr *engine.CourtBusyError
			if errors.As(txErr, &courtBusyErr) {
				c.JSON(http.StatusConflict, gin.H{
					"error":   "court_busy",
					"court":   courtBusyErr.Court,
					"matchId": courtBusyErr.MatchID,
					"compId":  courtBusyErr.CompID,
					"message": fmt.Sprintf("Court %s already has a running match (%s). Finish that match before starting a new one.", courtBusyErr.Court, courtBusyErr.MatchID),
				})
				return
			}
			var notFoundErr *engine.NotFoundError
			if errors.As(txErr, &notFoundErr) {
				c.JSON(http.StatusNotFound, gin.H{"error": txErr.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
			return
		}
		if engErr != nil {
			if errors.Is(engErr, errResultFinalized) {
				c.JSON(http.StatusConflict, gin.H{
					"error":   "result_finalized",
					"message": "This match result has already been reported. Contact the tournament organizer to correct it.",
				})
				return
			}
			if errors.Is(engErr, engine.ErrMatchSideMismatch) {
				// The payload named competitors that differ from the stored
				// pairing — refuse rather than rewrite match identity.
				c.JSON(http.StatusConflict, gin.H{
					"error":   "side_mismatch",
					"message": "The submitted competitors don't match this match's pairing. Reload and try again.",
				})
				return
			}
			var ineligErr *engine.IneligibleCompetitorError
			if errors.As(engErr, &ineligErr) {
				// U1: reasonHuman alongside the raw kendo-term reason
				// so operator UIs can show "withdrew from match m_12"
				// instead of "kiken at m_12".
				c.JSON(http.StatusConflict, gin.H{
					"error":       "ineligible_competitor",
					"playerId":    ineligErr.PlayerID,
					"reason":      ineligErr.Reason,
					"reasonHuman": domain.ResolveReasonHuman(ineligErr.Reason),
				})
				return
			}
			var alreadyIneligErr *engine.AlreadyIneligibleError
			if errors.As(engErr, &alreadyIneligErr) {
				c.JSON(http.StatusConflict, gin.H{
					"error":       "already_ineligible",
					"playerId":    alreadyIneligErr.PlayerID,
					"matchId":     alreadyIneligErr.MatchID,
					"reason":      alreadyIneligErr.Reason,
					"reasonHuman": domain.ResolveReasonHuman(alreadyIneligErr.Reason),
				})
				return
			}
			var courtBusyErr *engine.CourtBusyError
			if errors.As(engErr, &courtBusyErr) {
				c.JSON(http.StatusConflict, gin.H{
					"error":   "court_busy",
					"court":   courtBusyErr.Court,
					"matchId": courtBusyErr.MatchID,
					"compId":  courtBusyErr.CompID,
					"message": fmt.Sprintf("Court %s already has a running match (%s). Finish that match before starting a new one.", courtBusyErr.Court, courtBusyErr.MatchID),
				})
				return
			}
			var downstreamKnockoutErr *engine.DownstreamKnockoutScoredError
			if errors.As(engErr, &downstreamKnockoutErr) {
				c.JSON(http.StatusConflict, gin.H{
					"error":    "downstream_knockout_scored",
					"pool":     downstreamKnockoutErr.Pool,
					"finisher": downstreamKnockoutErr.Finisher,
					"matchId":  downstreamKnockoutErr.MatchID,
					"message":  downstreamKnockoutErr.Error(),
				})
				return
			}
			var notFoundEngErr *engine.NotFoundError
			if errors.As(engErr, &notFoundEngErr) {
				c.JSON(http.StatusNotFound, gin.H{"error": engErr.Error()})
				return
			}
			var valErr *ValidationError
			if errors.As(engErr, &valErr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": valErr.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": engErr.Error()})
			return
		}

		// Broadcast match update with the full (post-merge) result so
		// SSE consumers see the same payload they'd see on a re-fetch.
		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchPtrForBroadcast(result),
		})
		// T085/T092 — when a kiken or fusenpai is recorded, the engine
		// persisted a CompetitorStatus for the losing player; surface
		// it so admin clients can invalidate cached match lists.
		if engStatus != nil {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        engStatus,
			})
		}
		// T135 — kachinuki post-score advancement. Runs OUTSIDE the tx
		// because MaybeAdvanceKachinuki calls UpdatePoolMatchByID /
		// UpdateBracket which acquire the per-comp lock themselves;
		// nesting under our tx would deadlock. A non-fatal error here
		// doesn't fail the request: the operator's bout score is
		// already on disk; surfacing a 500 would lead them to retry
		// and double-record. Mirrors the recordIneligibility non-fatal
		// pattern.
		if advanced, kerr := eng.MaybeAdvanceKachinuki(id, mid); kerr != nil {
			log.Printf("engine.MaybeAdvanceKachinuki(%s, %s): %v", id, mid, kerr)
		} else if advanced {
			hub.Broadcast(EventMatchUpdated, gin.H{
				"competitionId": id,
				"matchId":       mid,
			})
		}
		tryAutoCompletePools(c, eng, hub, id)

		c.JSON(http.StatusOK, result)
	})
}
