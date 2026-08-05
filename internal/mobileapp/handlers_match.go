package mobileapp

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// modifiedAtMaxSkewMs bounds how far into the future a client-supplied
// server-relative ModifiedAt may be before it is clamped to 0. A legitimate
// write is stamped at action time (at or before "now" in the server frame), so
// 2 seconds covers normal stamp-to-evaluate network jitter while still
// rejecting a buggy or hostile far-future value that would freeze a match by
// making every subsequent legitimate write look "older" and be dropped.
const modifiedAtMaxSkewMs = 2 * 1000

// clampClientModifiedAt sanitises a client-supplied ModifiedAt for the
// timestamp last-write-wins guard (mp-y3nk). A negative value, or one more
// than modifiedAtMaxSkewMs into the future, is untrustworthy: honouring it
// would let a client FREEZE a match by making every subsequent legitimate
// write look "older" and be dropped. Such values fall back to 0, which the
// guard treats as unstamped (arrival-order) and is always safe.
func clampClientModifiedAt(v int64) int64 {
	if v < 0 || v > time.Now().UnixMilli()+modifiedAtMaxSkewMs {
		return 0
	}
	return v
}

// annotateQueuePositions fills in MatchResult.QueuePosition for each
// element of matches in-place, delegating to state.DeriveQueuePositions
// for the per-court (status priority, ScheduledAt, original index) sort.
//
// FR-025, T036: queue positions are derived at serve time rather than
// persisted, a stored value would go stale the instant any match
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
	// stable tie-break key. We can't sort b.Rounds itself, the bracket
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
	// ThirdPlaceMatch (Naginata bronze) is a sibling of Rounds. The bronze is
	// conventionally played JUST BEFORE the final (viewer_awards.jsx: "the
	// bronze is normally played first"), so slot it into the final's round with
	// a position sentinel of -1: on their shared court, when scheduledAt is
	// blank/equal, it sorts after the semifinals but before the final.
	if b.ThirdPlaceMatch != nil {
		finalRound := len(b.Rounds) - 1
		byCourt[b.ThirdPlaceMatch.Court] = append(byCourt[b.ThirdPlaceMatch.Court],
			entry{m: b.ThirdPlaceMatch, round: finalRound, position: -1})
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

// anyNumberedBoutHasEncho reports whether any NUMBERED sub-result (a real
// team bout, not the position -1 daihyosen) carries an encho marker. Used
// by the score endpoints to decide whether the kachinuki numbered-bout
// encho exception needs the competition loaded at all: ordinary payloads
// carry no numbered-bout encho and must not pay the store read.
func anyNumberedBoutHasEncho(subResults []state.SubMatchResult) bool {
	for i := range subResults {
		sr := &subResults[i]
		// Encho.On() is the single "did this happen in encho" predicate
		// (CLAUDE.md); validateSubBout uses the same one, so an open-coded
		// nil+PeriodCount chain here could drift from it.
		if sr.Position != state.DaihyosenSubPosition && sr.Encho.On() {
			return true
		}
	}
	return false
}

// isKachinukiComp reports whether comp is a kachinuki TEAM competition,
// mirroring the engine's dispatch gate (TeamSize >= 2 + kachinuki type).
func isKachinukiComp(comp *state.Competition) bool {
	return comp != nil && comp.TeamSize >= 2 && comp.TeamMatchType == state.TeamMatchTypeKachinuki
}

// allowNumberedEnchoFor is the single decision point for the kachinuki
// bout-level encho exception (mp-gmcg): a tied kachinuki pairing may be
// fought on in overtime on that same bout, so the daihyosen-only encho
// gate in validateSubBout is relaxed for kachinuki competitions — in
// EVERY phase. Whether the final pairing must produce a result (e.g. the
// taisho must be defeated) is OPERATOR DISCRETION, never derivable from
// pool-vs-bracket (operator ruling, 2026-08-01, reverting an earlier
// bracket-only scoping that hard-coded the rule by phase — the exact
// thing spec 006's problem statement forbids). The encounter-level
// no-draw rule for brackets lives in validateBracketCompletion, not
// here. Both the single-score and bulk-score paths must route through
// this helper rather than re-deriving the rule.
func allowNumberedEnchoFor(comp *state.Competition) bool {
	return isKachinukiComp(comp)
}

// allowNumberedEnchoFromStore resolves the numbered-bout encho gate for
// compID: it loads the competition and applies allowNumberedEnchoFor, but
// ONLY when the payload actually carries a numbered-bout encho
// (hasNumberedEncho, from anyNumberedBoutHasEncho). An ordinary payload
// carries none and must not pay the store read on the hot scoring path.
//
// FAIL CLOSED: any load failure keeps the STRICT daihyosen-only gate. The
// error is logged rather than swallowed (errcheck) and rather than returned:
// a load failure must not surface to the operator as a 400 blaming the
// payload ("encho is only valid for the daihyosen representative bout
// (position -1)"), which is what an ignored error produced. Both the
// single-score and bulk-score paths route through here so neither the gate
// nor its diagnosability can drift from the other.
func allowNumberedEnchoFromStore(store CompetitionStore, compID string, hasNumberedEncho bool) bool {
	if !hasNumberedEncho {
		return false
	}
	comp, err := store.LoadCompetition(compID)
	if err != nil {
		log.Printf("LoadCompetition(%s) for the numbered-bout encho gate: %v; keeping the strict gate", compID, err)
		return false
	}
	return allowNumberedEnchoFor(comp)
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
		// matches may now be live, refresh without a full status change.
		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": compID})
		hub.Broadcast(EventScheduleUpdated, nil)
	case engine.AutoCompleteAwaitingLeagueTiebreak:
		// All regular team-league pool matches are complete but consequential ties
		// remain, the operator must either generate tie-breaker matches or finalize
		// shared ranks via the league-tiebreak endpoints (Phase 3b). Broadcast both
		// EventMatchUpdated (reload standings) and EventScheduleUpdated (display
		// the "tie-breaker required" banner) as documented on AutoCompleteAwaitingLeagueTiebreak.
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
		// the batch, mirrors the single-score handler (T085/T092).
		var eligibilityUpdates []*domain.CompetitorStatus

		// Kachinuki relaxes the numbered-bout encho gate. Every result in the
		// batch belongs to the same competition, so the gate is resolved ONCE
		// (allowNumberedEnchoFromStore also keeps the fail-closed behaviour and
		// the load-error log): scan the batch first so an import that carries no
		// bout-level encho at all never pays the store read.
		batchHasNumberedEncho := false
		for i := range results {
			if anyNumberedBoutHasEncho(results[i].SubResults) {
				batchHasNumberedEncho = true
				break
			}
		}
		allowNumberedEncho := allowNumberedEnchoFromStore(store, id, batchHasNumberedEncho)

		for i := range results {
			// Reject a hostile/buggy far-future or negative client timestamp so
			// it cannot freeze the match against later legitimate writes (mp-y3nk).
			results[i].ModifiedAt = clampClientModifiedAt(results[i].ModifiedAt)

			if err := validateBulkScoreLengths(&results[i], allowNumberedEncho); err != nil {
				errs = append(errs, scoreError{MatchID: results[i].ID, Error: err.Error()})
				continue
			}

			// mp-62vr: rep-player names belong only on a pool daihyosen/tiebreaker
			// rep bout. Strip them from regular matches so a crafted bulk payload
			// can't persist stale rep metadata (mirrors the single-score path).
			if !engine.IsPoolDaihyosenMatchID(results[i].ID) && !engine.IsTiebreakerMatchID(results[i].ID) {
				results[i].RepPlayerA = ""
				results[i].RepPlayerB = ""
			}

			// mp-ic5b: the correction-reason gate and the write run under the
			// same per-comp lock so the status read is race-free against a
			// concurrent PUT /score. Per-result transactions preserve the
			// existing {succeeded, errors[]} partial-success response shape.
			// Court exclusivity (mp-95mg) is intentionally NOT enforced here:
			// bulk-score is an admin batch-import/correction tool, not a live
			// match-start path. It also bypasses StartMatchTx for the same
			// reason, admin corrections are meant to override normal flow.
			results[i].CorrectionReason = strings.TrimSpace(results[i].CorrectionReason)
			var capturedStatus *domain.CompetitorStatus
			if err := tx.WithTransaction(id, func(stx state.StoreTx) error {
				// Correction-reason audit policy (require a reason when the write
				// rewrites a result the operator already declared final — a
				// completed -> completed overwrite, or the re-End of a match
				// reopened without one — otherwise carry the STORED reason
				// forward). The rule itself lives in
				// applyCorrectionReasonUnderTx, shared with the single-score path
				// so the two cannot drift; only the error SHAPE differs here
				// (partial-success entries carry a plain message).
				check, snapErr := applyCorrectionReasonUnderTx(stx, id, results[i].ID, &results[i])
				if snapErr != nil {
					return snapErr
				}
				if check.Reject != nil {
					return errors.New(check.Reject.Message)
				}
				status, err := eng.RecordMatchResultWithIneligibilityTx(stx, id, results[i].ID, &results[i])
				if err != nil {
					return err
				}
				capturedStatus = status
				if check.ClearBracketReopenPending {
					return dischargeReopenPendingUnderTx(stx, id, results[i].ID, "")
				}
				return nil
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

		// Engi competitions use flag-based scoring, not ippon tallies.
		// Quick-score builds an ippon-style result, which bypasses the
		// engi dispatch and would corrupt standings for flag-scored bouts.
		comp, err := store.LoadCompetition(id)
		if err != nil {
			internalError(c, err)
			return
		}
		if comp != nil && comp.Engi {
			c.JSON(http.StatusBadRequest, gin.H{"error": "quick-score is not supported for engi competitions"})
			return
		}
		// Kachinuki matches are scored one bout at a time via the score
		// endpoint (server-appended winner-stays bout log). Quick-score
		// synthesises a positional log and writes it wholesale through the
		// plain RecordMatchResult path, which has no kachinuki merge and no
		// premature-completion check, so a single call would destroy a live
		// winner-stays sequence. Same incompatibility class as engi: reject.
		// isKachinukiComp, not a bare TeamMatchType test: the sequence this
		// guard protects only exists when the engine actually runs kachinuki
		// advancement, which requires TeamSize >= 2 (MaybeAdvanceKachinuki
		// returns early below that). One spelling of "is this kachinuki"
		// across the package, matching the engine's dispatch gate.
		if isKachinukiComp(comp) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "quick-score is not supported for kachinuki competitions; score bouts individually"})
			return
		}

		// Determine team winner per kendo rules: most individual wins wins.
		// winnerSide records the WINNING SIDE (not just the name) so the
		// engine can stamp WinnerID even when both sides share a name,
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
		// Sub-bout SideA/SideB are left empty, individual bout sides are
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
			// A tied quick-score on a bracket team match produces a
			// Completed write with no winner, which validateBracketCompletion
			// rejects as *engine.ValidationError; map it to 400 so the caller
			// sees "resolve via daihyosen first" instead of a generic 500.
			var engValErr *engine.ValidationError
			if errors.As(err, &engValErr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": engValErr.Error()})
				return
			}
			internalError(c, err)
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{"competitionId": id, "matchId": mid})
		tryAutoCompletePools(c, eng, hub, id)
		c.JSON(http.StatusOK, result)
	})

	// Score endpoint, Slice 0 demonstration of the interface-DI +
	// Validate() pattern (T015 / T017 / NFR-002 / NFR-004). Calls go
	// through ScoringEngine and Broadcaster (the consumer-boundary
	// interfaces from deps.go) rather than the concrete types, and the
	// request body is validated via ScoreRequest.Validate() before any
	// engine call. The closure captures `*engine.Engine` / `*Hub` and
	// adapts them to the interfaces at the call boundary, same wire
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
		// Cap defensively, the tournament-level validateCourtLabels
		// enforces single-char labels but per-match court strings have
		// historically accepted longer values in engine tests (e.g.
		// "Court Z"). 32 is generous enough not to break any real
		// caller while rejecting abusive payloads.
		if err := validateMaxLen("court", req.Court, MaxLenMatchScheduledAt); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.UpdateMatchCourt(id, mid, req.Court); err != nil {
			internalError(c, err)
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"court":         req.Court,
		})

		c.Status(http.StatusOK)
	})

	// POST /competitions/:id/matches/:mid/revert-to-queue
	// Reverts a running match back to the scheduled (queued) state, discarding
	// any partial score so the operator can restart the correct bout. Idempotent
	// for already-scheduled matches. Completed matches return 409 (use the score
	// editor to correct a recorded result); an unknown match id returns 404.
	//
	// Intentionally NOT in isSelfRunMainGatedConfigRoute: revert-to-queue is
	// operational play (same as start-match), not organiser configuration, so it
	// stays accessible to court operators in self-run mode.
	r.POST("/competitions/:id/matches/:mid/revert-to-queue", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := eng.RevertMatchToQueue(id, mid); err != nil {
			if errors.Is(err, engine.ErrMatchAlreadyCompleted) {
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
				return
			}
			var notFoundErr *engine.NotFoundError
			if errors.As(err, &notFoundErr) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			internalError(c, err)
			return
		}

		// The match just left the running state, so its rev-guard high-water
		// mark is dead. Drop it (mirrors the score handler's `!isRunning`
		// cleanup) so a later re-start of this match, whose client may reuse the
		// same RevSession or reset its rev counter, is not wrongly dropped as
		// stale against a leftover mark.
		runningRevStore.Delete(id + ":" + mid)

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
		})

		c.Status(http.StatusOK)
	})

	// POST /competitions/:id/matches/:mid/reopen
	// Sanctioned "Reopen match" for a COMPLETED kachinuki team match
	// (mp-gmcg, spec 006 decision 4): status back to running, match-level
	// winner/decision cleared, bout log kept, so the operator can add more
	// bouts and later End match again. A dedicated endpoint rather than a
	// score-write flag so the score path's stale-write guard (a plain
	// running write against a completed match silently no-ops) stays fully
	// intact.
	//
	// Body: {"reason": "<why>"} — OPTIONAL, and an absent body is equally
	// valid, so reopening is ONE TAP. Requiring the justification here was
	// too much friction for the case it is most needed in: an operator who
	// ended a match by mistake, at a shiaijo, mid-session, had to compose a
	// reason before they could get back in. Rewriting a finalized result is
	// still audited — a reason-less reopen sets state.MatchResult.ReopenPending
	// and the score path then refuses to complete the match again without a
	// correctionReason (see applyCorrectionReasonUnderTx). The record is
	// written later than the action it justifies; it is not lost.
	//
	// Non-kachinuki competitions get 400 (their only sanctioned edit of a
	// finished result remains the correction path), as does an over-long
	// reason; a non-completed match, a bracket match whose winner already
	// feeds a fought downstream match, or a BUSY COURT gets 409.
	r.POST("/competitions/:id/matches/:mid/reopen", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		var body struct {
			Reason string `json:"reason"`
		}
		// io.EOF is an EMPTY body, which is the one-tap case: the client sends
		// no JSON at all. Only a malformed body is a 400 — an operator tapping
		// Reopen must never be blocked on request shape.
		if err := c.ShouldBindJSON(&body); err != nil && !errors.Is(err, io.EOF) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		reason := strings.TrimSpace(body.Reason)
		// Only a SUPPLIED reason is length-checked; empty is the sanctioned
		// one-tap shape and is handled by the ReopenPending flag instead.
		if reason != "" {
			if err := validateMaxLen("reason", reason, MaxLenCorrectionReason); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}

		// Self-run mode: reopening a finalized result is an organiser action
		// (anonymous participants are blocked from overwriting finalized
		// results on the score path via checkFinalizedUnderTx; an ungated
		// reopen would be a trivial bypass). The gate lives in the CENTRAL
		// allowlist, isSelfRunMainGatedConfigRoute (middleware.go), same as
		// its sibling override-winner — NOT hand-rolled here, so it shares
		// AuthMiddleware's verification mechanics including the F4
		// empty-stored-password fail-closed branch. Pinned by
		// TestSelfRun_ReopenRequiresMainPassword.

		// WithCourtExclusivityLock mirrors the score path: the engine's
		// cross-competition court check runs before its own per-comp
		// transaction, so without the tournament-level lock a concurrent
		// match-start in another competition could pass its check and commit
		// between the two, re-creating the very two-running-matches-on-one-
		// court wedge the reopen court gate exists to prevent.
		err := tx.WithCourtExclusivityLock(func() error {
			return eng.ReopenKachinukiMatch(id, mid, reason)
		})
		if err != nil {
			var notFoundErr *engine.NotFoundError
			var validationErr *engine.ValidationError
			var courtBusyErr *engine.CourtBusyError
			switch {
			case errors.Is(err, engine.ErrReopenNotCompleted),
				errors.Is(err, engine.ErrReopenDownstreamFought):
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case errors.As(err, &courtBusyErr):
				// Same 409 shape as the score path so clients have one
				// court_busy branch to handle (mp-gmcg).
				respondCourtBusy(c, courtBusyErr, "reopening this one")
			case errors.As(err, &notFoundErr):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validationErr):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				internalError(c, err)
			}
			return
		}

		// The match re-entered the running state; any rev-guard high-water
		// mark from its previous life is dead (mirrors revert-to-queue).
		runningRevStore.Delete(id + ":" + mid)

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
		})

		c.Status(http.StatusOK)
	})

	// DELETE /competitions/:id/matches/:mid/kachinuki-bout
	//
	// mp-gmcg: remove a trailing UNSCORED kachinuki bout appended by mistake
	// ([Record bout] / [Add next bout]). This is the explicit operator undo for
	// the empty appended pairing that otherwise only vanishes implicitly on the
	// End-match strip. It targets a regular numbered bout, NOT a daihyosen
	// (which does not exist in kachinuki); the engine reuses the same
	// trailing-unscored strip the completed write applies, so the removable set
	// is identical in both places.
	//
	// Not court-gated (a running match stays running). It IS self-run
	// main-gated (isSelfRunMainGatedConfigRoute, middleware.go): removing a
	// bout is an organiser correction, the same class as reopen/override-winner,
	// NOT the participant score path — which self-gates via enforceSelfRunPolicy
	// (this handler has no such in-handler check), so without the allowlist
	// entry a self-run pass-through would let an anonymous spectator delete a
	// live pairing (mp-gmcg review F2).
	r.DELETE("/competitions/:id/matches/:mid/kachinuki-bout", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		updated, err := eng.RemoveTrailingKachinukiBout(id, mid)
		if err != nil {
			var notFoundErr *engine.NotFoundError
			var validationErr *engine.ValidationError
			switch {
			case errors.Is(err, engine.ErrNoRemovableBout), errors.Is(err, engine.ErrRemoveBoutNotRunning):
				c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			case errors.As(err, &notFoundErr):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validationErr):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				internalError(c, err)
			}
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchPtrForBroadcast(updated),
		})

		// Project the 200 body the same way as the broadcast: the caller only
		// needs the (shortened) subResults to re-sync its local log, not the
		// audit free-text (correctionReason/decisionReason) or another client's
		// revSession (mp-gmcg review F2).
		c.JSON(http.StatusOK, gin.H{"result": matchPtrForBroadcast(updated)})
	})

	r.PUT("/competitions/:id/matches/:mid/override-winner", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		var req struct {
			WinnerName string `json:"winnerName"`
			// ModifiedAt is the client's server-relative timestamp for
			// last-write-wins reconciliation (mp-y3nk); 0 when unstamped.
			ModifiedAt int64 `json:"modifiedAt"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Trim whitespace from the winner name. Downstream comparisons
		// (m.Winner == m.SideA / m.SideB in engine/scoring.go and
		// engine/ranking.go) are exact-string equality, so a padded
		// "  Foo  " won't match the canonical "Foo" from the roster,
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

		// Engi competitions decide bouts by referee flag counts. A manual
		// winner override sets Winner without FlagsA/FlagsB, leaving a
		// completed engi match with a 0-0 flag total that violates the
		// {1,3,5} invariant. Reject it (mirrors the quick-score / decision
		// guards) so flag scoring stays the only engi result path. Fail
		// CLOSED on a load error, like those siblings: a transient fault must
		// not let an engi override slip through into inconsistent state.
		comp, loadErr := store.LoadCompetition(id)
		if loadErr != nil {
			internalError(c, loadErr)
			return
		}
		if comp != nil && comp.Engi {
			c.JSON(http.StatusBadRequest, gin.H{"error": "override-winner is not supported for engi competitions; use flag scoring instead"})
			return
		}

		applied, err := eng.OverrideBracketWinner(id, mid, winnerName, clampClientModifiedAt(req.ModifiedAt))
		if err != nil {
			// Map engine client-errors to their proper status so the offline
			// terminal-write replay never treats a permanent 4xx (unknown match,
			// or a feeder not yet finished) as a retryable failure and wedge sync
			// (mp-y3nk). Mirrors the RevertMatchToQueue mapping above.
			var notFoundErr *engine.NotFoundError
			var validationErr *engine.ValidationError
			switch {
			case errors.As(err, &notFoundErr):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validationErr):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				internalError(c, err)
			}
			return
		}

		if applied {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.JSON(http.StatusOK, gin.H{"applied": applied})
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
			internalError(c, err)
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
// The finalized-result guard is NOT checked here, it must run inside
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

	// Anonymous self-run writes are treated as unversioned: clear any client-
	// supplied rev/revSession so they don't engage the same-session rev-guard at
	// all and simply apply (last-write-wins among peers). A participant has no
	// need to order their own writes, and this keeps a crafted revSession from
	// interfering with another session's rev ordering.
	req.Rev = 0
	req.RevSession = ""
	return "self-reported", true
}

// errResultFinalized is a sentinel returned by checkFinalizedUnderTx to
// signal that the match is already finalized and the anonymous overwrite
// should be rejected with 409.
var errResultFinalized = errors.New("result_finalized")

// checkFinalizedUnderTx runs inside WithTransaction (under the per-comp
// lock) so it's safe from TOCTOU races. Returns errResultFinalized when
// an anonymous caller tries to write to a completed match. Fails closed:
// a load error rejects the request rather than allowing an overwrite,
// which is why it reads the snapshot through the fail-closed
// matchSnapshotOrErr rather than the best-effort matchSnapshotFor.
func checkFinalizedUnderTx(stx state.StoreTx, compID, matchID string) error {
	snap, found, err := matchSnapshotOrErr(stx, compID, matchID, "finalized")
	if err != nil {
		return err
	}
	if found && isMatchFinalized(snap.Status) {
		return errResultFinalized
	}
	return nil
}

// isMatchFinalized reports whether the given stored status represents a
// concluded match. Any completed match is finalized, anonymous callers must
// not overwrite it regardless of whether a winner was explicitly recorded.
func isMatchFinalized(status state.MatchStatus) bool {
	return status == state.MatchStatusCompleted
}

// matchStores is the minimal read surface a match lookup needs: the two
// files a match can live in. BOTH state.StoreTx (in-transaction, per-comp
// lock already held) and CompetitionStore (plain loads, each taking the lock
// itself) satisfy it, so the transactional and the non-transactional lookups
// share ONE traversal instead of a hand-rolled walk per projection.
type matchStores interface {
	LoadPoolMatches(compID string) ([]state.MatchResult, error)
	LoadBracket(compID string) (*state.Bracket, error)
}

// matchSnapshot is the stored state of a match as this file's guards read
// it: Status drives the correction / stale-write / finalized gates,
// CorrectionReason carries the kachinuki reopen justification forward (see
// applyCorrectionReasonUnderTx), ReopenPending says a reason-less reopen
// still owes one, and SubResults echoes the post-advance kachinuki bout log
// back to the open score editor.
//
// InBracket is the match's HOME rather than its content: the pool write is a
// whole-struct overwrite that carries ReopenPending itself, while the bracket
// write copies field by field and deliberately never reads the flag off the
// client-supplied payload, so only a bracket match needs the separate
// dischargeReopenPendingUnderTx pass. lookupMatchSnapshot already knows which
// store it found the match in, so reporting it costs nothing and saves the
// clear path a second traversal.
type matchSnapshot struct {
	Status           state.MatchStatus
	CorrectionReason string
	ReopenPending    bool
	SubResults       []state.SubMatchResult
	InBracket        bool
}

// lookupMatchSnapshot is THE traversal of the three homes a match ID can
// have — the pool-matches CSV, the bracket rounds, and the bracket's
// ThirdPlaceMatch — searched in that order. Lookup is by ID equality, not
// prefix: match-ID shapes vary across formats and fixtures.
//
// The bronze (3rd-place) match is a SIBLING of Rounds, not an element of it,
// so a rounds-only loop never reaches it. Forgetting that branch is the bug
// this single traversal exists to make impossible: MaybeAdvanceKachinuki
// appends bouts to the bronze like any other bracket match, and a per-caller
// copy of the walk that omitted it left a kachinuki bronze "Record bout"
// echoing the pre-advance log, with the appended pairing never reaching the
// open editor.
//
// found=false means the ID is in neither store; callers treat an unknown
// match as "nothing recorded yet" (the engine rejects it via errMatchNotFound
// on the actual score write, so there is nothing to fail here). err is the
// FIRST load failure seen, and the walk continues past it so a best-effort
// caller can still find a bracket match when the pool CSV is unreadable.
// Callers do not open-code this: a fail-closed audit gate goes through
// matchSnapshotOrErr (surfaces err), a best-effort caller through
// matchSnapshotFor (drops it).
func lookupMatchSnapshot(s matchStores, compID, matchID string) (matchSnapshot, bool, error) {
	var loadErr error
	poolMatches, err := s.LoadPoolMatches(compID)
	if err != nil {
		loadErr = fmt.Errorf("load pool matches: %w", err)
	}
	for i := range poolMatches {
		if poolMatches[i].ID == matchID {
			return matchSnapshot{
				Status:           poolMatches[i].Status,
				CorrectionReason: poolMatches[i].CorrectionReason,
				ReopenPending:    poolMatches[i].ReopenPending,
				SubResults:       poolMatches[i].SubResults,
			}, true, loadErr
		}
	}
	bracket, err := s.LoadBracket(compID)
	if err != nil && loadErr == nil {
		loadErr = fmt.Errorf("load bracket: %w", err)
	}
	if bracket != nil {
		for _, round := range bracket.Rounds {
			for i := range round {
				if round[i].ID == matchID {
					return bracketMatchSnapshot(&round[i]), true, loadErr
				}
			}
		}
		if bracket.ThirdPlaceMatch != nil && bracket.ThirdPlaceMatch.ID == matchID {
			return bracketMatchSnapshot(bracket.ThirdPlaceMatch), true, loadErr
		}
	}
	return matchSnapshot{}, false, loadErr
}

// bracketMatchSnapshot projects a BracketMatch into the same shape as a pool
// MatchResult so lookupMatchSnapshot returns one type across both stores.
func bracketMatchSnapshot(bm *state.BracketMatch) matchSnapshot {
	return matchSnapshot{
		Status:           bm.Status,
		CorrectionReason: bm.CorrectionReason,
		ReopenPending:    bm.ReopenPending,
		SubResults:       bm.SubResults,
		InBracket:        true,
	}
}

// matchSnapshotFor is the best-effort form of lookupMatchSnapshot, for the
// callers where a load failure is indistinguishable from "nothing recorded
// yet": every one of them re-reads on the next request and the authoritative
// rejection happens on the engine write.
func matchSnapshotFor(s matchStores, compID, matchID string) (matchSnapshot, bool) {
	snap, found, _ := lookupMatchSnapshot(s, compID, matchID)
	return snap, found
}

// matchSnapshotOrErr is the fail-CLOSED counterpart to matchSnapshotFor: it
// SURFACES a load error (wrapped as "<guardLabel> guard: …") instead of
// swallowing it, so an in-transaction audit gate aborts rather than acting on a
// best-effort empty snapshot. A new fail-closed gate must reach for THIS, not
// matchSnapshotFor — reading a match's ReopenPending/status under a dropped load
// error is the exact hole mp-gmcg closed. The caller keeps its own found/err
// handling (the return shapes legitimately differ: a sentinel, a bare error, a
// (correctionCheck{}, err)); this owns only the shared load-and-wrap.
func matchSnapshotOrErr(s matchStores, compID, matchID, guardLabel string) (matchSnapshot, bool, error) {
	snap, found, err := lookupMatchSnapshot(s, compID, matchID)
	if err != nil {
		return matchSnapshot{}, false, fmt.Errorf("%s guard: %w", guardLabel, err)
	}
	return snap, found, nil
}

// respondCourtBusy writes the shared 409 court_busy body. The court-
// exclusivity gate fires on three paths (score start, score reopen, and
// kachinuki reopen); `action` names what the operator was attempting (e.g.
// "starting a new one") so the sentence reads naturally. Keeping the
// {error,court,matchId,compId,message} shape in ONE place means every client
// court_busy branch stays in lockstep when the wire contract moves.
func respondCourtBusy(c *gin.Context, err *engine.CourtBusyError, action string) {
	c.JSON(http.StatusConflict, gin.H{
		"error":   "court_busy",
		"court":   err.Court,
		"matchId": err.MatchID,
		"compId":  err.CompID,
		"message": fmt.Sprintf("Court %s already has a running match (%s). Finish that match before %s.", err.Court, err.MatchID, action),
	})
}

// matchStatusFromStore is the non-transactional status projection of
// matchSnapshotFor: it reads a match's current status via plain store loads
// so a correction (a completed -> completed overwrite) can be detected BEFORE
// the transaction begins, letting the pre-tx court gate skip it. A benign
// TOCTOU with a concurrent status change only decides whether the
// (spurious-for-corrections) court check runs; the audit-critical
// correction-REASON requirement still uses the race-free in-tx read.
func matchStatusFromStore(store CompetitionStore, compID, matchID string) state.MatchStatus {
	snap, _ := matchSnapshotFor(store, compID, matchID)
	return snap.Status
}

// correctionCheck is applyCorrectionReasonUnderTx's verdict.
//
//   - StoredStatus is the match's status as stored, returned so the caller's
//     stale-after-complete guard doesn't have to look the match up again.
//   - Reject is non-nil when the write must be refused; the caller renders it
//     in its own error shape (a 400 on the single-score path, a
//     partial-success entry on the bulk path).
//   - ClearBracketReopenPending asks the caller to run
//     dischargeReopenPendingUnderTx AFTER a successful engine write. Only bracket
//     matches need it: the pool write is a whole-struct overwrite that carries
//     r.ReopenPending straight through.
type correctionCheck struct {
	StoredStatus              state.MatchStatus
	Reject                    *ValidationError
	ClearBracketReopenPending bool
}

// applyCorrectionReasonUnderTx applies the correction-reason audit policy to
// r and returns the match's STORED status. Runs inside WithTransaction
// (caller MUST hold the per-comp lock via the supplied StoreTx), so the
// is-completed read is race-free against a concurrent score write.
//
// The policy is shared by the single-score and bulk-score paths so the audit
// rule cannot drift between them (same reason applyKachinukiMerge is shared
// by the locked and tx scoring paths); each caller wraps the Reject verdict
// in its own error shape.
//
// A completion must be JUSTIFIED in two cases, and both demand exactly the
// same thing (a non-empty CorrectionReason), because both rewrite a result
// the operator had already declared final:
//
//   - completed -> completed is a CORRECTION: overwriting an already-finalized
//     result requires a reason for traceability. This applies to ANY decision
//     type, including a withdrawal (kiken/fusenpai) submitted via /score;
//     exempting those would let a finalized result be overwritten with no
//     audit reason.
//   - the match carries ReopenPending: it was reopened with no reason
//     (mp-gmcg), so its stored status is `running` and the completion looks
//     like a first finalization — but the finalized result it replaces was
//     discarded by that reopen. The justification reopen no longer asks for is
//     collected HERE, on a step the operator was already taking.
//
// In both cases Reject reports a missing reason and r is left otherwise
// untouched; on acceptance the supplied reason is kept and ReopenPending is
// discharged (nothing is outstanding once the record exists).
//
// Anything else (a genuine first finalization, or a running/scheduled write)
// is not a rewrite. A client-supplied reason is meaningless there and is
// dropped, but the reason STORED on the match must survive: the kachinuki
// reopen path records the operator's justification there, and blanking it
// would erase the audit trail at the very write it was recorded to justify.
// Carrying it on the non-completed writes too is what keeps it alive for POOL
// matches, whose write is a whole-struct overwrite (`*r = *result` in
// engine/scoring_tx.go) rather than the bracket path's set-if-non-empty.
//
// ReopenPending is SERVER-OWNED and is re-stamped from the stored value on
// every write, before any of the above. state.MatchResult binds straight from
// the request body, so a client could otherwise plant the flag on an unrelated
// match or — the damaging direction — clear its own outstanding justification
// by sending `reopenPending: false`. Re-stamping is also what keeps the flag
// alive across a pool match's running writes, which would otherwise blank it
// through the same whole-struct overwrite.
//
// Returning the stored status keeps this the transaction's ONLY match lookup:
// StoreTx loads deliberately bypass the file cache (state.LoadPoolMatchesLocked
// / loadBracketLocked), so a second walk is a real os.Open plus a full CSV
// parse (and, for a bracket match, an os.ReadFile plus a whole-bracket
// json.Unmarshal) while the per-comp WRITE lock is held. The caller's
// stale-after-complete guard reads the returned status instead.
func applyCorrectionReasonUnderTx(stx state.StoreTx, compID, matchID string, r *state.MatchResult) (correctionCheck, error) {
	// matchSnapshotOrErr fails CLOSED on a load error: the correction-reason
	// gate below must not pass on an assumed-false ReopenPending, silently
	// finalizing without the mandatory reason (or dropping a client-supplied
	// CorrectionReason) — the audit hole this gate exists to close (mp-gmcg).
	snap, _, err := matchSnapshotOrErr(stx, compID, matchID, "correction-reason")
	if err != nil {
		return correctionCheck{}, err
	}
	r.ReopenPending = snap.ReopenPending
	if r.Status == state.MatchStatusCompleted && (snap.Status == state.MatchStatusCompleted || snap.ReopenPending) {
		if r.CorrectionReason == "" {
			return correctionCheck{StoredStatus: snap.Status, Reject: missingCorrectionReasonError(snap)}, nil
		}
		// The justification has landed: the reopen is no longer outstanding.
		r.ReopenPending = false
		return correctionCheck{
			StoredStatus:              snap.Status,
			ClearBracketReopenPending: snap.InBracket && snap.ReopenPending,
		}, nil
	}
	// Non-completing write: pin the reason to the STORED one. This is not just a
	// carry-forward — it also refuses a client-supplied reason on a running
	// write, so the audit note can only ever change through the completing
	// correction branch above. The engine twin (recordMatchResultTx in
	// scoring_tx.go: `if result.CorrectionReason == "" { ... = r.CorrectionReason }`)
	// independently carries the stored reason across its whole-struct overwrite
	// for callers that DON'T pass through here; the two are deliberately kept
	// separate (this one is stricter) — do not collapse one into the other
	// without re-checking that a running write still cannot rewrite the note
	// (mp-gmcg review F5).
	r.CorrectionReason = snap.CorrectionReason
	return correctionCheck{StoredStatus: snap.Status}, nil
}

// missingCorrectionReasonError names the CAUSE, not just the rule. Both
// branches reject on the same field with the same HTTP shape, but an operator
// re-ending a match they reopened one tap ago has no idea they are performing
// a "correction" — telling them the reopen is what asks for the reason is the
// difference between a fixable prompt and a dead end.
func missingCorrectionReasonError(snap matchSnapshot) *ValidationError {
	msg := "correcting a completed match result requires a non-empty correctionReason"
	if snap.Status != state.MatchStatusCompleted && snap.ReopenPending {
		msg = ReopenNeedsReasonMessage
	}
	return &ValidationError{Field: "correctionReason", Message: msg}
}

// ReopenNeedsReasonMessage is the operator-facing text for "you reopened this
// match, so finalizing it again has to say why". It lives here as a constant
// because a match can be finalized through EITHER endpoint — PUT /score
// (applyCorrectionReasonUnderTx, above) or POST /decision (kiken/fusenpai,
// handlers_decision.go) — and the two report it on DIFFERENT fields
// (correctionReason vs decisionReason, each endpoint's own audit field). The
// FIELD differs, the rule does not, so the wording is shared rather than
// duplicated: an operator who meets this on one endpoint should not be told
// something subtly different on the other.
const ReopenNeedsReasonMessage = "this match was reopened; ending it again requires a reason"

// dischargeReopenPendingUnderTx clears a match's stored ReopenPending flag
// after a completion that carried its justification, and — when reason is
// non-empty — records that justification in CorrectionReason. Runs inside the
// caller's WithTransaction, AFTER the engine write has succeeded: the
// finalizing handlers commit the transaction even when the engine rejects the
// write (engErr is surfaced afterwards), so discharging beforehand would
// persist a discharge for a write that never landed.
//
// BOTH match homes, because both finalizing endpoints reach both:
//
//   - PUT /score passes reason "" and is gated on
//     correctionCheck.ClearBracketReopenPending, so only a BRACKET match ever
//     arrives from that path. Its reason is already stored (the engine write
//     copies CorrectionReason set-if-non-empty) and a POOL match is already
//     handled by the whole-struct overwrite (`*r = *result`) carrying the
//     cleared r.ReopenPending.
//   - POST /decision passes the operator's decisionReason and needs BOTH arms.
//     RecordDecisionTx builds its own result struct, so the pool overwrite
//     blanks ReopenPending with no audit record (a SILENT discharge) while the
//     bracket write, being field-by-field, never copies the flag and leaves it
//     stuck on a completed match. Same rule, two opposite failures; one
//     explicit pass fixes both.
//
// CorrectionReason is set-if-empty so the "" caller cannot blank a stored
// reason, and so a reason already recorded by the engine write wins over a
// re-statement here.
//
// recordBracketMatchResult* deliberately does NOT copy ReopenPending from the
// payload — that field is client-supplied and a score write must not be able
// to move a server-owned audit flag — which is why the flag needs this
// explicit pass rather than riding along with the result.
func dischargeReopenPendingUnderTx(stx state.StoreTx, compID, matchID, reason string) error {
	// Pool first, mirroring lookupMatchSnapshot's order. UpdatePoolMatchByID
	// reports found=false for a bracket match, which is the fall-through.
	found, err := stx.UpdatePoolMatchByID(compID, matchID, func(r *state.MatchResult) {
		r.ReopenPending = false
		if reason != "" && r.CorrectionReason == "" {
			r.CorrectionReason = reason
		}
	})
	if err != nil {
		return fmt.Errorf("discharge reopen flag: pool match: %w", err)
	}
	if found {
		return nil
	}
	bracket, err := stx.LoadBracket(compID)
	if err != nil {
		return fmt.Errorf("discharge reopen flag: load bracket: %w", err)
	}
	if bracket == nil {
		return nil
	}
	// One closure rather than the mutation written at both homes: the rounds
	// loop and the bronze branch must not drift (same reasoning as the pool
	// mutate above, and as ReopenKachinukiMatch's `guard`).
	discharge := func(bm *state.BracketMatch) error {
		bm.ReopenPending = false
		if reason != "" && bm.CorrectionReason == "" {
			bm.CorrectionReason = reason
		}
		return stx.SaveBracket(compID, bracket)
	}
	for rIdx := range bracket.Rounds {
		for mIdx := range bracket.Rounds[rIdx] {
			if bracket.Rounds[rIdx][mIdx].ID == matchID {
				return discharge(&bracket.Rounds[rIdx][mIdx])
			}
		}
	}
	// The bronze (3rd-place) match is a SIBLING of Rounds, not an element of
	// it; the loop above never reaches it (see lookupMatchSnapshot).
	if bm := bracket.ThirdPlaceMatch; bm != nil && bm.ID == matchID {
		return discharge(bm)
	}
	return nil
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
// complete-pools post-writes deliberately run AFTER the tx, both
// reach for other per-comp locked operations (UpdatePoolMatchByID,
// UpdateCompetitionChanged) which would deadlock inside the tx, and
// they're already structured as best-effort side effects with their
// own non-fatal failure-handling. Bulk-score handler is intentionally
// NOT migrated: the partial-success error array semantics need a
// per-result tx (or a different commit shape) and that's out of scope
// for this slice.
// runningRev is the per-match value stored in runningRevStore: the highest
// Rev seen WITHIN a given client scoring session (RevSession). A write whose
// RevSession differs from the stored one is treated as last-write-wins
// (concurrent operators, multiple operators may score one shiaijo). The
// completed-match regression guard is the real protection against a stale
// running write reverting a finished result.
type runningRev struct {
	Session string
	Rev     int64
}

// runningRevStore is a process-lifetime, in-memory map that tracks the
// highest client-side revision number (Rev) seen for each running-status
// write on a given match. Key is "compID:matchID". Value is a runningRev
// (session + rev).
//
// C2 rev-guard: when a "running" write arrives with a Rev that is lower
// than the stored high-water mark WITHIN THE SAME RevSession, we silently
// no-op it (return 200). This prevents out-of-order delivery from a
// reconnect flush overwriting a more-recent in-flight write. Writes from a
// DIFFERENT RevSession are treated as last-write-wins, multiple operators
// may legitimately score the same shiaijo concurrently. The completed-match
// regression guard (staleAfterComplete, inside the tx) is the authoritative
// protection: it ensures a running write never reverts a finished match
// regardless of session. Only "running" writes are gated, completed writes
// and Rev==0 (unversioned) writes always proceed so the guard never blocks
// explicit operator submits or legacy clients.
//
// The map is process-scoped and therefore reset on server restart; the
// on-disk state is the ground truth. A mis-ordered running-write that
// slips through after a restart is harmless: the operator's explicit
// Finish is the authoritative write and carries no rev constraint.
var runningRevStore sync.Map

// scoreRequestBody wraps ScoreRequest with transient, request-only
// fields. These bind from the JSON body but are NOT part of
// state.MatchResult, so they can never be persisted.
type scoreRequestBody struct {
	ScoreRequest
	// KachinukiBoutFinal marks an explicit operator "record bout" submit
	// on a kachinuki team match. Only a flagged write triggers
	// MaybeAdvanceKachinuki; unflagged running writes (autosave fires
	// mid-bout, where a 1-0 lead already sets the sub winner) and
	// completed writes (corrections, daihyosen finishes) skip
	// advancement entirely.
	KachinukiBoutFinal bool `json:"kachinukiBoutFinal"`
}

func registerScoreHandler(r *gin.RouterGroup, eng ScoringEngine, store CompetitionStore, tx CompetitionTransactor, hub Broadcaster, verifier PasswordVerifier, tl TournamentLoader) {
	// C3: coalesce high-frequency "running" match_updated broadcasts to ≤4/s
	// per match. Completed writes always proceed (isRunning=false).
	coalescer := newMatchBroadcastCoalescer()

	r.PUT("/competitions/:id/matches/:mid/score", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")
		if err := validateMaxLen("matchId", mid, MaxLenMatchID); err != nil {
			// Use the shared validateMaxLen helper for a consistent
			// ValidationError-style body ("matchId: must be <= N characters")
			// that includes the limit, matching the other mobileapp handlers.
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Composite key used by the rev-guard and broadcast coalescer; hoisted
		// here so all four use-sites share a single allocation (FINDING 3).
		matchKey := id + ":" + mid

		var body scoreRequestBody
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		req := body.ScoreRequest
		// Kachinuki exception (mp-gmcg): a tied kachinuki pairing may be
		// fought on in overtime on that same bout, in ANY phase — whether the
		// final pairing must produce a result is operator discretion
		// (allowNumberedEnchoFor), so the daihyosen-only encho gate in
		// validateSubBout is relaxed for kachinuki competitions. The
		// competition is loaded only when the payload actually carries a
		// bout-level encho, keeping the hot scoring path free of the read.
		// Fail closed: a load failure keeps the strict gate (400 below) and is
		// logged by allowNumberedEnchoFromStore, which the bulk-score path
		// shares so the gate and its diagnostics stay identical.
		allowNumberedEncho := allowNumberedEnchoFromStore(store, id, anyNumberedBoutHasEncho(req.SubResults))
		if err := req.validateWithOptions(allowNumberedEncho); err != nil {
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

		// mp-ba3: self-run decision allowlist + result provenance. Runs
		// after Validate() so the request is structurally valid.
		resultSource, ok := enforceSelfRunPolicy(c, tl, verifier, &req)
		if !ok {
			return
		}

		// mp-62vr: rep-player names are meaningful only on a pool
		// daihyosen/tiebreaker rep bout (the frontend gates the dropdowns on
		// the same id shape via repIsTeam). Strip them from any regular match
		// so a crafted authenticated payload can't persist stale rep metadata
		// onto a normal pool/bracket result.
		if !engine.IsPoolDaihyosenMatchID(mid) && !engine.IsTiebreakerMatchID(mid) {
			req.RepPlayerA = ""
			req.RepPlayerB = ""
		}

		result := req.AsMatchResult()
		// Reject a hostile/buggy far-future or negative client timestamp so it
		// cannot freeze the match against later legitimate writes (mp-y3nk).
		result.ModifiedAt = clampClientModifiedAt(result.ModifiedAt)
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

		// NOTE (mp-gmcg): kachinuki completion is operator-led. A completed
		// write on a kachinuki match is always an explicit operator action
		// ("End match") and is accepted even when the roster snapshot shows
		// players remaining: team sizes are unregulated and the
		// taisho-defeated rule legitimately ends a match early, so the app
		// cannot second-guess the shiaijo. The former premature-completion
		// 409 gate was removed with the engine's auto-finalize.

		// C2 rev-guard: drop stale "running" autosave writes that arrive
		// out of order after a reconnect flush.
		//
		// Only gated when:
		//   - status is "running" (autosave writes; completed writes always win)
		//   - the incoming Rev > 0 (client opted in; Rev==0 means unversioned)
		//   - RevSession is non-empty (the guard is scoped to a session; a Rev
		//     without a session can't be safely compared, so treat it as
		//     unversioned and always proceed rather than collapse mixed clients
		//     into the "" session and wrongly drop a reload starting at rev=1)
		//
		// Same-session ordering: if the stored high-water mark for this match
		// (within the same session) is already > the incoming Rev, the write is
		// stale, return 200 so the client doesn't surface an error but skip the
		// engine write entirely. A higher-or-equal rev advances the mark.
		//
		// Different sessions (concurrent operators): last-write-wins. Multiple
		// operators may legitimately score the same shiaijo simultaneously. The
		// completed-match regression guard (staleAfterComplete, inside the tx)
		// is the real protection against a running write reverting a finished match.
		if result.Status == state.MatchStatusRunning && result.Rev > 0 && result.RevSession != "" {
			incoming := runningRev{Session: result.RevSession, Rev: result.Rev}
			for {
				existing, loaded := runningRevStore.LoadOrStore(matchKey, incoming)
				if !loaded {
					break // first running write for this key
				}
				stored := existing.(runningRev)
				// Same session: a lower rev is a stale out-of-order delivery (e.g.
				// a reconnect flush). Drop it. DIFFERENT sessions are concurrent
				// operators (multiple operators may score one shiaijo), last write
				// wins; the completed-match regression guard below still prevents a
				// running write from reverting a finished match.
				if stored.Session == incoming.Session && result.Rev < stored.Rev {
					c.JSON(http.StatusOK, gin.H{"stale": true})
					return
				}
				if runningRevStore.CompareAndSwap(matchKey, existing, incoming) {
					break
				}
				// Lost the CAS race, retry.
			}
		}

		isWithdrawal := domain.IsKikenDecisionStr(result.Decision) || result.Decision == "fusenpai"

		// isCorrection: a completed -> completed overwrite (the operator is
		// fixing an already-finished result, e.g. via the shiaijo console's or
		// the Scores page's "Correct" affordance). Such a write never places or
		// keeps the match on the court, so BOTH court-exclusivity gates below
		// must skip it; otherwise correcting a completed match while ANOTHER
		// match runs on the same court is wrongly rejected with court_busy,
		// which blocks the operator from going back to correct any match. The
		// pre-tx store read is a benign TOCTOU (see matchStatusFromStore); a
		// running/scheduled write is never a correction, so the && short-circuit
		// skips the read on the hot live-scoring path. A first finalization
		// (running -> completed) is NOT a correction, so the start gate still
		// runs there and its eligibility/simultaneity checks are preserved.
		isCorrection := result.Status == state.MatchStatusCompleted &&
			matchStatusFromStore(store, id, mid) == state.MatchStatusCompleted

		// FR-035: WithCourtExclusivityLock serializes the cross-competition
		// court-busy check + per-competition write under a tournament-level
		// mutex so two concurrent match-starts on the same court in different
		// competitions can't both pass the cross-comp check before either
		// commits (TOCTOU). Withdrawal decisions skip the court gate, operators
		// must record kiken/fusenpai regardless of court state.
		var (
			engStatus          *domain.CompetitorStatus
			engErr             error
			staleAfterComplete bool
		)
		txErr := tx.WithCourtExclusivityLock(func() error {
			if !isWithdrawal && !isCorrection {
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
				// Correction audit: a write that rewrites a result the operator
				// already declared final requires a non-empty CorrectionReason for
				// traceability — either a completed -> completed overwrite, or the
				// re-End of a match reopened without a reason (mp-gmcg). A genuine
				// first finalization needs no reason but must carry the STORED one
				// forward. All of that rule lives in
				// applyCorrectionReasonUnderTx, shared with the bulk-score path. It
				// runs inside the tx so the is-completed read is race-free (same
				// lock), and the status it returns is reused by the stale-write
				// guard below rather than looked up a second time.
				check, snapErr := applyCorrectionReasonUnderTx(stx, id, mid, result)
				if snapErr != nil {
					return snapErr
				}
				existingStatus := check.StoredStatus
				if check.Reject != nil {
					engErr = check.Reject
					return nil
				}
				// Bracket integrity: a running- OR scheduled-status write must
				// never revert an already-completed match (e.g. a stale autosave
				// queued before Finish and flushed afterward, or a requeue write
				// that raced with completion). Applies to ALL callers (the
				// self-reported finalized guard above only covers anonymous mode).
				// Empty-status writes are legitimate completions/corrections and
				// are not caught here. There is no sanctioned way to send a
				// COMPLETED match back to the queue: the revert-to-queue endpoint
				// reverts only a running match and rejects a completed one (409).
				// A finished result is corrected via the score editor, not requeued.
				// No-op it as a stale write so the client's flush discards it.
				// existingStatus is the status read by the correction gate a few
				// lines up: same transaction, same lock, and nothing between the two
				// touches the store, so re-reading it would only re-parse the
				// pool-matches CSV / bracket JSON (StoreTx loads bypass the cache).
				if (result.Status == state.MatchStatusRunning || result.Status == state.MatchStatusScheduled) &&
					existingStatus == state.MatchStatusCompleted {
					staleAfterComplete = true
					return nil
				}
				if !isWithdrawal && !isCorrection {
					if err := eng.StartMatchTx(stx, id, mid); err != nil {
						engErr = err
						return nil
					}
				}
				engStatus, engErr = eng.RecordMatchResultWithIneligibilityTx(stx, id, mid, result)
				// engErr is a normal application-level signal (AlreadyIneligible
				// → 409, validation/not-found → other codes); we surface it
				// after the tx returns. The score-write inside the tx already
				// includes the K3 rollback for the AlreadyIneligible path,
				// returning nil here commits whatever final state the engine
				// settled on.
				if engErr == nil && check.ClearBracketReopenPending {
					// Only after the write actually landed: this branch commits
					// even when engErr is set, so discharging the outstanding
					// justification any earlier would clear it for a rejected write.
					return dischargeReopenPendingUnderTx(stx, id, mid, "")
				}
				return nil
			})
		})
		if txErr != nil {
			// txErr carries errors from CheckCrossCompCourtBusy (cross-comp
			// court conflict or match-not-found) or from the WithTransaction
			// infrastructure itself (WAL commit failure, etc.).
			var courtBusyErr *engine.CourtBusyError
			if errors.As(txErr, &courtBusyErr) {
				respondCourtBusy(c, courtBusyErr, "starting a new one")
				return
			}
			var notFoundErr *engine.NotFoundError
			if errors.As(txErr, &notFoundErr) {
				// Match not found, drop any rev-guard entry this request created so
				// fabricated match IDs can't grow runningRevStore unbounded (mirrors
				// the engErr NotFoundError path; scoring is unauthenticated in
				// self-run mode).
				runningRevStore.Delete(matchKey)
				c.JSON(http.StatusNotFound, gin.H{"error": txErr.Error()})
				return
			}
			internalError(c, txErr)
			return
		}
		if staleAfterComplete {
			// If this was a running-status write with a Rev/RevSession, the
			// rev-guard above stored a high-water mark in runningRevStore
			// (LoadOrStore) before we discovered, inside the transaction, that the
			// match is already completed; drop it now. For a scheduled-status
			// write the rev-guard never ran, so this Delete is a safe no-op
			// (sync.Map Delete on a missing key does nothing). Either way, no
			// future write can legitimately supersede a completed match, so
			// retaining the entry would leak map memory.
			runningRevStore.Delete(matchKey)
			c.JSON(http.StatusOK, gin.H{"stale": true})
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
				// pairing, refuse rather than rewrite match identity.
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
				respondCourtBusy(c, courtBusyErr, "starting a new one")
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
				// The match doesn't exist, drop any rev-guard entry this request
				// created so fabricated match IDs can't grow runningRevStore
				// unbounded (scoring is unauthenticated in self-run mode).
				runningRevStore.Delete(matchKey)
				c.JSON(http.StatusNotFound, gin.H{"error": engErr.Error()})
				return
			}
			var valErr *ValidationError
			if errors.As(engErr, &valErr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": valErr.Error()})
				return
			}
			// engine.ValidationError is a DISTINCT type from the handler-layer
			// ValidationError above; without this mapping an engine precondition
			// rejection (e.g. validateBracketCompletion's "cannot mark completed
			// with no winner; resolve via daihyosen first") surfaced as a 500,
			// which the client write-queue treats as transient and retries
			// (mp-q8c6 poisoned-queue pattern) instead of showing the message.
			var engValErr *engine.ValidationError
			if errors.As(engErr, &engValErr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": engValErr.Error()})
				return
			}
			internalError(c, engErr)
			return
		}

		// Broadcast match update with the full (post-merge) result so
		// SSE consumers see the same payload they'd see on a re-fetch.
		// C3: coalesce high-frequency running-status broadcasts (first-wins
		// within 250ms); completed writes always broadcast unconditionally.
		isRunning := result.Status == state.MatchStatusRunning
		// Bound runningRevStore: once a match leaves the running state its rev
		// high-water mark is dead (the guard only gates running writes), so drop
		// the entry to keep the process-lifetime map from growing without bound
		// across many matches/competitions. A later correction that re-opens the
		// match starts a fresh session anyway.
		if !isRunning {
			runningRevStore.Delete(matchKey)
		}
		if coalescer.Allow(matchKey, isRunning) {
			hub.Broadcast(EventMatchUpdated, gin.H{
				"competitionId": id,
				"matchId":       mid,
				"result":        matchPtrForBroadcast(result),
			})
		}
		// T085/T092, when a kiken or fusenpai is recorded, the engine
		// persisted a CompetitorStatus for the losing player; surface
		// it so admin clients can invalidate cached match lists.
		if engStatus != nil {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        engStatus,
			})
		}
		// T135, kachinuki post-score advancement. Gated on the transient
		// kachinukiBoutFinal request flag: only an explicit operator
		// "record bout" submit may advance. Unflagged running writes are
		// mid-bout autosaves (a 1-0 lead already sets the sub winner, so
		// advancing on them would fire while the bout is still being
		// fought) and completed writes are corrections/daihyosen finishes.
		// Runs OUTSIDE the tx because MaybeAdvanceKachinuki calls
		// UpdatePoolMatchByID / UpdateBracket which acquire the per-comp
		// lock themselves; nesting under our tx would deadlock. A
		// non-fatal error here doesn't fail the request: the operator's
		// bout score is already on disk; surfacing a 500 would lead them
		// to retry and double-record. Mirrors the recordIneligibility
		// non-fatal pattern.
		if body.KachinukiBoutFinal {
			if advanced, kerr := eng.MaybeAdvanceKachinuki(id, mid); kerr != nil {
				log.Printf("engine.MaybeAdvanceKachinuki(%s, %s): %v", id, mid, kerr)
			} else if advanced {
				// Echo the POST-advance bout log so the open score editor can
				// render the appended pairing immediately (Record bout →
				// next bout appears, mp-gmcg): the `result` echoed below
				// predates the append, and SSE only refreshes the match
				// LIST, not the host's open-match snapshot. Best-effort: on
				// a load failure the pre-advance log is echoed and the next
				// refresh reconciles.
				if snap, ok := matchSnapshotFor(store, id, mid); ok {
					result.SubResults = snap.SubResults
				}
				hub.Broadcast(EventMatchUpdated, gin.H{
					"competitionId": id,
					"matchId":       mid,
				})
			}
		}
		tryAutoCompletePools(c, eng, hub, id)

		// Don't echo internal write-ordering metadata back in the response.
		result.Rev = 0
		result.RevSession = ""
		c.JSON(http.StatusOK, result)
	})
}
