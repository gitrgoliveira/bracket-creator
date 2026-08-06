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
			result    *state.MatchResult
			status    *domain.CompetitorStatus
			engErr    error
			reasonErr *ValidationError
		)
		reason := strings.TrimSpace(req.DecisionReason)
		txErr := tx.WithTransaction(id, func(stx state.StoreTx) error {
			// mp-gmcg: a reason-less kachinuki reopen DEFERS its audit
			// justification to whatever finalizes the match next. PUT /score
			// collects it (applyCorrectionReasonUnderTx); this endpoint is the
			// OTHER way to finalize a match, so it has to collect it too — a
			// kiken recorded on a reopened encounter is still a result
			// replacing one the operator had already declared final.
			//
			// Checked in-tx, before the engine write, so the read is race-free
			// against a concurrent finalization and a rejection costs no write.
			// matchSnapshotOrErr fails CLOSED on a load error (unlike the
			// best-effort matchSnapshotFor), so a dropped read can't finalize on
			// an assumed-false ReopenPending and silently discard the mandatory
			// reopen audit reason.
			//
			// The read is KACHINUKI-ONLY: ReopenPending is set exclusively by
			// ReopenKachinukiMatch (which rejects non-kachinuki), so a
			// non-kachinuki match can never carry it. Skip the whole (2-file)
			// snapshot read for the common non-kachinuki decision — comp is
			// already loaded above, so the gate itself costs no read (mp-gmcg
			// review E3). snap stays zero-valued (ReopenPending false), so the
			// checks below are correctly no-ops.
			var snap matchSnapshot
			if isKachinukiComp(comp) {
				var snapErr error
				snap, _, snapErr = matchSnapshotOrErr(stx, id, mid, "reopen-pending")
				if snapErr != nil {
					return snapErr
				}
			}
			if snap.ReopenPending && reason == "" {
				reasonErr = &ValidationError{Field: "decisionReason", Message: ReopenNeedsReasonMessage}
				return nil
			}
			result, status, engErr = eng.RecordDecisionTx(stx, id, mid, req.Decision, req.DecisionBy, req.DecisionReason, req.Encho, req.Force)
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
		if reasonErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": reasonErr.Error()})
			return
		}
		if engErr != nil {
			// Map engine.ValidationError → 400, NotFoundError → 404,
			// IneligibleCompetitorError → 409 (FR-035),
			// ErrDecisionLocked → 409 (T103/CHK024).
			var alreadyIneligErr *engine.AlreadyIneligibleError
			var ineligErr *engine.IneligibleCompetitorError
			var engValErr *engine.ValidationError
			var engNotFoundErr *engine.NotFoundError
			switch {
			case errors.As(engErr, &alreadyIneligErr):
				// T105/CHK047: concurrent kiken, another operator already
				// recorded ineligibility for this player on a different match.
				// U1: reasonHuman carries the volunteer-readable gloss
				// alongside the raw kendo-term reason.
				c.JSON(http.StatusConflict, gin.H{
					"error":       "already_ineligible",
					"playerId":    alreadyIneligErr.PlayerID,
					"matchId":     alreadyIneligErr.MatchID,
					"reason":      alreadyIneligErr.Reason,
					"reasonHuman": domain.ResolveReasonHuman(alreadyIneligErr.Reason),
				})
			case errors.As(engErr, &ineligErr):
				c.JSON(http.StatusConflict, gin.H{
					"error":       "ineligible_competitor",
					"playerId":    ineligErr.PlayerID,
					"reason":      ineligErr.Reason,
					"reasonHuman": domain.ResolveReasonHuman(ineligErr.Reason),
				})
			case errors.Is(engErr, engine.ErrDecisionLocked):
				c.JSON(http.StatusConflict, gin.H{
					"error":  "decision_locked",
					"reason": engErr.Error(),
				})
			case errors.As(engErr, &engValErr):
				c.JSON(http.StatusBadRequest, gin.H{"error": engValErr.Error()})
			case errors.As(engErr, &engNotFoundErr):
				c.JSON(http.StatusNotFound, gin.H{"error": engNotFoundErr.Error()})
			default:
				internalError(c, engErr)
			}
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        matchPtrForBroadcast(result),
		})
		if status != nil {
			hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
				"competitionId": id,
				"status":        status,
			})
		}
		tryAutoCompletePools(c, eng, hub, id)

		c.JSON(http.StatusOK, result)
	})
}
