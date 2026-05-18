// Package mobileapp — handlers_decision.go owns the POST
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
//   - decision MUST be one of kiken/fusenpai/fusensho/daihyosen.
//   - decisionBy is required and MUST be "shiro" or "aka".
//   - decisionReason ≤ 200 chars (contract).
func (r *DecisionRequest) Validate() error {
	switch r.Decision {
	case "kiken":
		r.Decision = "kiken-voluntary"
	case "kiken-voluntary", "kiken-injury", "fusenpai", "fusensho", "daihyosen":
		// ok — these are the decision types this endpoint creates.
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
// AFTER the tx returns — the auto-complete check itself takes the lock
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

		// T104/CHK029: enforce MaxEnchoPeriods cap on the encho block.
		// Same shape as the score handler — Force bypasses, 0 cap means
		// unlimited. Done BEFORE the tx so the read is cheap and we
		// don't take the lock when the request is going to 400 anyway.
		if !enforceEnchoCap(c, store, id, req.Encho, req.Force) {
			return
		}

		// T156: run the entire RecordDecision flow inside one
		// WithTransaction. The engine call chain — sides lookup, T103
		// downstream-match check, T105 concurrent-kiken pre-check,
		// pool/bracket match-write, ineligibility check-and-set, prior-
		// loser eligibility restore on undo — all use the same StoreTx
		// handle, so the per-comp lock is acquired exactly once for the
		// entire mutation.
		var (
			result *state.MatchResult
			status *domain.CompetitorStatus
			engErr error
		)
		txErr := tx.WithTransaction(id, func(stx state.StoreTx) error {
			result, status, engErr = eng.RecordDecisionTx(stx, id, mid, req.Decision, req.DecisionBy, req.DecisionReason, req.Encho, req.Force)
			// engine errors are normal failure modes (locked, ineligible,
			// not-found, validation) — return nil here so the tx
			// commits whatever partial writes K3-style rollback already
			// landed; engErr is mapped to the right HTTP status below.
			return nil
		})
		if txErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": txErr.Error()})
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
				// T105/CHK047: concurrent kiken — another operator already
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
				c.JSON(http.StatusInternalServerError, gin.H{"error": engErr.Error()})
			}
			return
		}

		hub.Broadcast(EventMatchUpdated, gin.H{
			"competitionId": id,
			"matchId":       mid,
			"result":        result,
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
