// Package mobileapp, handlers_decision.go owns the POST
// `/api/competitions/:cid/matches/:mid/decision` endpoint that auto-
// fills the scoreline for kiken/fusenpai/fusensho/daihyosen decisions
// (T090).
//
// All consumers go through the constructor-injected `ScoringEngine` /
// `Broadcaster` interfaces from deps.go rather than the concrete
// `*engine.Engine` / `*Hub` types (NFR-002).
package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// DecisionRequest is the body shape for `POST /api/competitions/:cid/matches/:mid/decision`.
//
// Per contracts/match-decisions.md §POST /decision the operator
// supplies only the decision-type metadata; the server auto-fills the
// scoreline and Winner based on decisionBy + encho.
//
// Force bypasses the decision-lock check (T103/CHK024) that prevents
// overwriting a prior kiken/fusenpai when a subsequent match for
// either participant has already started. The admin UI sets it after
// the operator confirms the override.
type DecisionRequest struct {
	Decision       string               `json:"decision"`
	DecisionBy     string               `json:"decisionBy"`
	DecisionReason string               `json:"decisionReason,omitempty"`
	Encho          *state.EnchoMetadata `json:"encho,omitempty"`
	Force          bool                 `json:"force,omitempty"`
	// ModifiedAt is the client's server-relative write stamp, the same one
	// /score carries (mp-y3nk). Sending it puts decision writes under the
	// timestamp last-write-wins guard instead of its unstamped bypass, and
	// gives a decision-completed match a recency the UI can order by: a match
	// closed while still `scheduled` (a queue row's Record default win) was
	// otherwise the one completion that carried no time at all (mp-jnvl).
	ModifiedAt int64 `json:"modifiedAt,omitempty"`
	// ForceDownstreamReopen bypasses bc-kcdg's downstream-knockout-correction
	// guard (engine.DownstreamKnockoutPlayedError, HTTP 409
	// downstream_knockout_played): a decision that changes an already-
	// propagated bracket winner while a downstream match carries a result of
	// its own is refused by default, as is a decision on a mixed
	// competition's POOL match that moves a qualifier the knockout already
	// played. Same field name and contract as every
	// other knockout-correction write (see scoreRequestBody.ForceDownstreamReopen
	// in handlers_match.go); a SEPARATE field from Force above, which answers
	// a different question (T103's decision-lock override): this maps to
	// engine.ForceOptions.Force on the RecordDecisionTxWithOptions call
	// below, never to the force parameter Force itself feeds.
	ForceDownstreamReopen bool `json:"forceDownstreamReopen"`
}

// Validate enforces request-shape invariants on a decision payload
// before the engine touches it.
//
//   - decision MUST be one of kiken-voluntary/kiken-injury/fusenpai/fusensho/daihyosen
//     (legacy "kiken" is remapped to "kiken-voluntary").
//   - decisionBy is required and MUST be "shiro" or "aka".
//   - decisionReason, 200 chars max (contract).
func (r *DecisionRequest) Validate() error {
	switch r.Decision {
	case "kiken":
		r.Decision = "kiken-voluntary"
	case "kiken-voluntary", "kiken-injury", "fusenpai", "fusensho", "daihyosen":
		// ok, these are the decision types this endpoint creates.
	case "":
		return &ValidationError{Field: "decision", Message: "required"}
	default:
		return &ValidationError{
			Field:   "decision",
			Message: fmt.Sprintf("unsupported on /decision endpoint: %q (use /score for fought/hikiwake)", r.Decision),
		}
	}
	if r.DecisionBy == "" {
		return &ValidationError{Field: "decisionBy", Message: "required"}
	}
	if r.DecisionBy != "shiro" && r.DecisionBy != "aka" {
		return &ValidationError{
			Field:   "decisionBy",
			Message: fmt.Sprintf("must be 'shiro' or 'aka', got %q", r.DecisionBy),
		}
	}
	if err := validateMaxLen("decisionReason", r.DecisionReason, MaxLenDecisionReason); err != nil {
		return err
	}
	return nil
}

// RegisterDecisionHandlers wires the POST /decision endpoint via the
// consumer-boundary interfaces.
//
// T090, NFR-002. T156: under WithTransaction so the match-write +
// ineligibility-write + (on undo) prior-loser eligibility restore all
// commit under ONE per-comp lock acquire instead of 3+ separate ones.
// `tx` (CompetitionTransactor) is the new dependency for that migration;
// `eng` exposes the tx-aware RecordDecisionTx the closure dispatches to.
//
// SSE broadcasts and the optional tryAutoCompletePools post-write run
// AFTER the tx returns, the auto-complete check itself takes the lock
// internally via UpdateCompetitionChanged, so running it inside the tx
// would deadlock (non-reentrant mutex). Holding the tx open across an
// SSE broadcast would let a slow consumer stall every other writer for
// the same competition.
func RegisterDecisionHandlers(r *gin.RouterGroup, eng ScoringEngine, store CompetitionStore, tx CompetitionTransactor, hub Broadcaster) {
	r.POST("/competitions/:id/matches/:mid/decision", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		mid := c.Param("mid")

		var req DecisionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// A stamp implausibly far in the server's future is refused outright,
		// before either write path below opens a transaction, exactly as PUT
		// /score refuses one: a decision competes on timestamps, so neither
		// honouring nor zeroing such a stamp is safe (modifiedAtRefuseSkewMs).
		// Nothing is written. It runs ahead of the both-barred carve-out,
		// whose write competes on timestamps too.
		if serverNowMs, aheadMs, refuse := clientClockSkew(req.ModifiedAt); refuse {
			respondClockSkew(c, serverNowMs, aheadMs)
			return
		}
		// A negative stamp is garbage; the clamp turns it into the unstamped
		// bypass, which is always safe (mp-y3nk).
		req.ModifiedAt = clampClientModifiedAt(req.ModifiedAt)
		// bc-cse item 10: the ONE hikiwake shape this endpoint accepts, ahead
		// of Validate() (which otherwise 400s every hikiwake -- "use /score
		// for fought/hikiwake"). Any hikiwake that is not this exact
		// both-barred pool/league shape falls through unchanged to that same
		// 400.
		if handleBothSidesBarredHikiwake(c, eng, store, tx, hub, id, mid, req) {
			return
		}
		if err := req.Validate(); err != nil {
			var verr *ValidationError
			if errors.As(err, &verr) {
				c.JSON(http.StatusBadRequest, gin.H{"error": verr.Error()})
				return
			}
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Engi competitions decide bouts by flag counts, not ippon waza.
		// Kiken/fusenpai/fusensho decisions make no sense in that paradigm
		// (there are no ippons to forfeit). Reject explicitly so the client
		// gets a clear error instead of a silent no-op (Finding 9).
		comp, loadErr := store.LoadCompetition(id)
		if loadErr != nil {
			internalError(c, loadErr)
			return
		}
		if comp != nil && comp.Engi {
			c.JSON(http.StatusBadRequest, gin.H{"error": "engi competitions do not support kiken/fusenpai decisions; use flag scoring instead"})
			return
		}

		// T156: run the entire RecordDecision flow inside one
		// WithTransaction. The engine call chain, sides lookup, T103
		// downstream-match check, T105 concurrent-kiken pre-check,
		// pool/bracket match-write, ineligibility check-and-set, prior-
		// loser eligibility restore on undo, all use the same StoreTx
		// handle, so the per-comp lock is acquired exactly once for the
		// entire mutation.
		var (
			result *state.MatchResult
			status *domain.CompetitorStatus
			engErr error
			// bc-kcdg: reopenedDownstream collects the IDs of any downstream
			// bracket match reopened by a forced correction, populated only
			// when req.ForceDownstreamReopen actually unblocked one.
			reopenedDownstream []engine.ReopenedMatch
		)
		reason := strings.TrimSpace(req.DecisionReason)
		txErr := tx.WithTransaction(id, func(stx state.StoreTx) error {
			// A match reopened without a reason (ReopenPending) is ended here
			// like any other: never refused for a missing reason (operator
			// ruling 2026-09-25: a match can be reopened without any reason,
			// and nothing is gated on that). The write below discharges the
			// flag and keeps the decision's reason, if any, as the correction
			// reason. The read is in-tx, so it is race-free against a
			// concurrent finalization, and it fails CLOSED on a load error.
			//
			// ReopenPending is set by engine.ReopenMatch, which since bc-tmfn
			// reopens a match of any format that a withdrawal decided, so the
			// read is not kachinuki-only.
			snap, _, snapErr := matchSnapshotOrErr(stx, id, mid, "reopen-pending")
			if snapErr != nil {
				return snapErr
			}
			// Pass the TRIMMED reason (not req.DecisionReason): it is persisted
			// into DecisionReason, and dischargeReopenPendingUnderTx stores the
			// same trimmed value into CorrectionReason below — feeding the raw one
			// here left the two audit fields on one record disagreeing byte-for-
			// byte on padding. Mirrors the score path's up-front TrimSpace of
			// CorrectionReason (mp-gmcg review).
			// bc-kcdg/bc-cse finding 5: RecordDecisionTxWithOptions keeps
			// req.Force (T103 decision-lock override) and
			// req.ForceDownstreamReopen (the bc-kcdg downstream-knockout-
			// correction guard) as two independent confirmations -- setting
			// one does not silently grant the other -- and surfaces the
			// reopened downstream match ids so they can be broadcast below.
			result, status, engErr = eng.RecordDecisionTxWithOptions(stx, id, mid, req.Decision, req.DecisionBy, reason, req.Encho, req.Force,
				engine.ForceOptions{Force: req.ForceDownstreamReopen, Reopened: &reopenedDownstream}, req.ModifiedAt)
			if result != nil && result.ResultSource == "" {
				result.ResultSource = "admin"
			}
			// Discharge only behind a write that actually landed: the closure
			// returns nil on engErr (the transaction commits regardless, engErr
			// is surfaced below), so an unguarded call would clear the
			// obligation for a rejected decision.
			if engErr == nil && snap.ReopenPending {
				// A decision can complete a POOL or a bracket match, so pass the
				// snapshot's own home: a pool match still takes the pool arm, a
				// bracket match skips the guaranteed-miss pool probe (mp-gmcg review).
				return dischargeReopenPendingUnderTx(stx, id, mid, reason, snap.InBracket)
			}
			return nil
		})
		if txErr != nil {
			internalError(c, txErr)
			return
		}
		if engErr != nil {
			// bc-cse finding 3: shared with handleBothSidesBarredHikiwake's
			// own write below, so both doors answer the sentinels they have
			// in common (superseded, downstream_knockout_running/played,
			// corrupt overrides) identically rather than the carve-out
			// falling through respondEngineError's 500 default for them.
			respondDecisionEngineError(c, store, id, mid, engErr)
			return
		}

		stampWithdrawnStatus(store, id, result)
		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchPtrForBroadcast(result),
		})
		// bc-kcdg: each reopened match is distinct from the one the decision
		// just corrected; broadcast it too so a client watching only that
		// court/match learns its verdict was cleared (mirrors /score,
		// /override-winner, and /quick-score).
		broadcastReopenedDownstream(hub, id, reopenedDownstream)
		if status != nil {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        status,
			})
		}
		// A decision always closes the match it rules on, so what the
		// after-write check needs (which match, left completed) is known
		// without reading the returned result.
		tryAutoCompletePoolsAfterWrite(c, eng, hub, id, state.MatchResult{ID: mid, Status: state.MatchStatusCompleted})

		c.JSON(http.StatusOK, result)
	})
}

// respondDecisionEngineError maps engErr from a write on the /decision
// endpoint to its HTTP response. It is the ONE switch both the main decision
// flow above and handleBothSidesBarredHikiwake's own write below call (bc-cse
// finding 3): before this was extracted, the carve-out mapped its errors
// through the generic respondEngineError, which only classifies
// *engine.NotFoundError (404) and *engine.ValidationError (400) and defaults
// everything else -- including engine.ErrMatchSuperseded,
// *engine.DownstreamKnockoutPlayedError, *engine.DownstreamKnockoutRunningError,
// and state.ErrCorruptOverrides -- to a 500. The SPA's offline write queue
// retries 5xx forever (the mp-q8c6 poisoned-queue pattern), so a superseded
// carve-out write could never win a retry, and the other three are terminal,
// operator-actionable conflicts that must never look like a server fault.
func respondDecisionEngineError(c *gin.Context, store CompetitionStore, compID, matchID string, engErr error) {
	// Map engine.ValidationError → 400, NotFoundError → 404,
	// IneligibleCompetitorError → 409 (FR-035),
	// ErrDecisionLocked → 409 (T103/CHK024).
	var alreadyIneligErr *engine.AlreadyIneligibleError
	var ineligErr *engine.IneligibleCompetitorError
	var engNotFoundErr *engine.NotFoundError
	switch {
	case errors.Is(engErr, engine.ErrMatchSuperseded):
		// bc-lww1. REACHABLE since mp-jnvl: the SPA stamps decision
		// writes (api_client.recordDecision) and RecordDecisionTx puts
		// that stamp on its MatchResult, so ApplyByTimestamp no longer
		// takes the unstamped bypass and a decision can lose to a newer
		// stored result -- the same way a score write can. An unstamped
		// decision (an older client, or an engine-internal caller) still
		// takes the bypass and always applies. Mapping it was already
		// right for the reason the two daihyosen paths are: this is the
		// LAST arm a future writer would remember to add, and the
		// default below is internalError -> 500. The SPA queues
		// /decision as a terminal write (_enqueueTerminalWrite, kind
		// 'decision') and retries 5xx indefinitely, so an unmapped
		// supersede here would not merely mis-report a dropped write,
		// it would poison the offline queue with one that can never
		// succeed.
		respondSuperseded(c)
	case errors.As(engErr, &alreadyIneligErr):
		// T105/CHK047: concurrent kiken, another operator already
		// recorded ineligibility for this player on a different
		// match. bc-rawm/bc-cse: reasonHuman is ONE operator
		// sentence naming the match and the remedy, not the raw
		// kendo-term reason.
		c.JSON(http.StatusConflict, gin.H{
			"error":       "already_ineligible",
			"playerId":    alreadyIneligErr.PlayerID,
			"matchId":     alreadyIneligErr.MatchID,
			"reason":      alreadyIneligErr.Reason,
			"reasonHuman": reasonHumanForBarredCompetitor(store, compID, matchID, alreadyIneligErr.PlayerID, alreadyIneligErr.Reason, alreadyIneligErr.MatchID, alreadyIneligErr.Decision),
		})
	case errors.As(engErr, &ineligErr):
		// Simultaneous tells the simultaneity gate's sentence
		// (already complete in Reason) apart from a barred-status
		// refusal -- even one whose own MatchID is empty (a status
		// set directly via POST /competitor-status) -- exactly as
		// the /score handler discriminates.
		reasonHuman := ineligErr.Reason
		switch {
		case ineligErr.Simultaneous:
			// reasonHuman stays ineligErr.Reason, already complete.
		case ineligErr.BothSidesBarred:
			// bc-cse item 10: neither side has an opponent to hand the
			// default win to, so the single-sided builder's remedy
			// clauses do not apply.
			reasonHuman = bothSidesBarredReasonHuman(store, compID, matchID)
		default:
			reasonHuman = reasonHumanForBarredCompetitor(store, compID, matchID, ineligErr.PlayerID, ineligErr.Reason, ineligErr.MatchID, ineligErr.Decision)
		}
		c.JSON(http.StatusConflict, gin.H{
			"error":       "ineligible_competitor",
			"playerId":    ineligErr.PlayerID,
			"reason":      ineligErr.Reason,
			"reasonHuman": reasonHuman,
		})
	case errors.Is(engErr, engine.ErrDecisionLocked):
		c.JSON(http.StatusConflict, gin.H{
			"error":  "decision_locked",
			"reason": engErr.Error(),
		})
	case respondIfDownstreamKnockoutRunning(c, engErr):
		// A decision on a mixed competition's POOL match that would
		// move a qualifier out of a knockout match being fought now:
		// terminal, not confirmable (see
		// respondIfDownstreamKnockoutRunning's doc comment).
	case respondIfDownstreamKnockoutPlayed(c, engErr):
		// bc-kcdg: this decision would change an already-propagated
		// bracket winner while a downstream match carries a result of
		// its own. Fixed wire contract, shared with every other
		// knockout-correction write (see
		// respondIfDownstreamKnockoutPlayed's doc comment);
		// respondIfDownstreamKnockoutPlayed already wrote the response.
		// Retry with forceDownstreamReopen:true once confirmed. A
		// decision on a mixed competition's POOL match reaches this too
		// when it moves a qualifier the knockout already played
		// (qualifierChange names who moves).
	case errors.As(engErr, &engNotFoundErr):
		c.JSON(http.StatusNotFound, gin.H{"error": engNotFoundErr.Error()})
	default:
		// engine.ValidationError → 400 and a corrupt overrides.json → 422
		// (computeStandingsFrom, reached via RecordDecisionTx ->
		// RecordMatchResultWithIneligibilityTx's pool requalification
		// check)
		// both fall through respondIfEngineWriteError.
		if respondIfEngineWriteError(c, engErr) {
			return
		}
		internalError(c, engErr)
	}
}

// handleBothSidesBarredHikiwake handles bc-cse item 10's ONE accepted
// exception on POST /decision: {"decision":"hikiwake"} for a SCHEDULED pool
// or league match (engine.IsPoolMatchID covers both id shapes) whose BOTH
// sides are already barred elsewhere (engine.BarredSides). Neither
// competitor can fight, and there is no opponent to hand a default win to
// (both are equally unable to show up), so the encounter is recorded as a
// completed DRAW with no winner and no points. It writes no eligibility
// status of its own -- hikiwake is not a withdrawal decision
// (domain.IsWithdrawalDecisionStr excludes it), so
// RecordMatchResultWithIneligibilityTx's recordIneligibilityFromDecision
// never fires for it -- and it never goes through StartMatchTx, which
// would refuse it on either side's existing bar.
//
// A match can be reopened without a reason (operator ruling), and this door
// is never refused for one. It does settle the reopen, through
// dischargeReopenPendingUnderTx in the same transaction as the write:
// a match reopened without a reason carries ReopenPending (engine.
// reopenPoolMatch; a fusensho whose barred side is still barred reopens to
// scheduled, which is how a both-barred match can carry it), and the draw
// clears it and keeps the request's reason, if any, as the correction reason.
//
// Returns true when it fully answered the request (success, or a failure of
// ITS OWN write once the shape qualified); false when the shape does not
// qualify, so the caller falls through to the ordinary /decision flow --
// which, for any OTHER hikiwake, is req.Validate()'s existing 400 ("use
// /score for fought/hikiwake"), unchanged from today.
func handleBothSidesBarredHikiwake(c *gin.Context, eng ScoringEngine, store CompetitionStore, txr CompetitionTransactor, hub Broadcaster, compID, matchID string, req DecisionRequest) bool {
	if req.Decision != "hikiwake" || !engine.IsPoolMatchID(matchID) {
		return false
	}
	reason := strings.TrimSpace(req.DecisionReason)
	var (
		applied  bool
		result   state.MatchResult
		writeErr error
	)
	txErr := txr.WithTransaction(compID, func(stx state.StoreTx) error {
		poolMatches, err := stx.LoadPoolMatches(compID)
		if err != nil {
			return err
		}
		var m *state.MatchResult
		for i := range poolMatches {
			if poolMatches[i].ID == matchID {
				m = &poolMatches[i]
				break
			}
		}
		if m == nil || m.Status != state.MatchStatusScheduled {
			return nil
		}
		statuses, err := stx.LoadCompetitorStatus(compID)
		if err != nil {
			return err
		}
		a, b := engine.BarredSides(statuses, matchID, m.SideAID, m.SideBID)
		if a == nil || b == nil {
			return nil
		}
		write := &state.MatchResult{
			ID: matchID, SideA: m.SideA, SideB: m.SideB, SideAID: m.SideAID, SideBID: m.SideBID,
			Status: state.MatchStatusCompleted, Decision: "hikiwake", DecisionReason: reason,
			ModifiedAt: req.ModifiedAt,
		}
		if _, werr := eng.RecordMatchResultWithIneligibilityTx(stx, compID, matchID, write); werr != nil {
			writeErr = werr
			return nil
		}
		result = *write
		applied = true
		if m.ReopenPending {
			return dischargeReopenPendingUnderTx(stx, compID, matchID, reason, false)
		}
		return nil
	})
	if txErr != nil {
		internalError(c, txErr)
		return true
	}
	if !applied {
		if writeErr != nil {
			// The shape qualified (both barred, scheduled pool/league match)
			// but the write itself failed on its own terms -- answered
			// directly rather than silently dropped to the 400 fallback,
			// which would misreport this as "unsupported decision". Routed
			// through the same respondDecisionEngineError the main decision
			// flow uses (bc-cse finding 3), not the generic respondEngineError:
			// this write can lose the timestamp LWW race (engine.ErrMatchSuperseded)
			// or trip a downstream-knockout guard exactly like the main flow,
			// and respondEngineError's 500 default for those would poison the
			// SPA's offline write queue instead of answering them terminally.
			respondDecisionEngineError(c, store, compID, matchID, writeErr)
			return true
		}
		return false
	}
	hub.Broadcast(EventMatchUpdated, gin.H{
		"competitionId": compID,
		"results":       matchesForBroadcast([]state.MatchResult{result}),
	})
	tryAutoCompletePoolsAfterWrite(c, eng, hub, compID, result)
	c.JSON(http.StatusOK, result)
	return true
}
