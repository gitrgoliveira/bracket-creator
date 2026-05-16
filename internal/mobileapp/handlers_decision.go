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
	case "kiken", "fusenpai", "fusensho", "daihyosen":
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
	if len(r.DecisionReason) > 200 {
		return &ValidationError{Field: "decisionReason", Message: "must be ≤ 200 characters"}
	}
	return nil
}

// RegisterDecisionHandlers wires the POST /decision endpoint via the
// consumer-boundary interfaces.
//
// T090, NFR-002.
//
// TODO(T156): wrap eng.RecordDecision + tryAutoCompletePools in a
// single state.Store.WithTransaction once the engine grows tx-aware
// variants. eng.RecordDecision internally calls
// RecordMatchResultWithIneligibility which acquires the per-comp lock
// via UpdatePoolMatchByID / UpdateBracket; nesting that inside a
// WithTransaction would deadlock (sync.RWMutex is non-recursive). The
// migration template lives in handlers_lineup.go (the PUT body); same
// scope-cut rationale as the score handler in handlers_match.go.
func RegisterDecisionHandlers(r *gin.RouterGroup, eng ScoringEngine, store CompetitionStore, hub Broadcaster) {
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
		// unlimited.
		if req.Encho != nil && req.Encho.PeriodCount > 0 {
			if comp, cerr := store.LoadCompetition(id); cerr == nil && comp != nil && comp.MaxEnchoPeriods > 0 {
				if req.Encho.PeriodCount > comp.MaxEnchoPeriods && !req.Force {
					c.JSON(http.StatusBadRequest, gin.H{
						"error": "max_encho_exceeded",
						"limit": comp.MaxEnchoPeriods,
					})
					return
				}
			}
		}

		result, status, err := eng.RecordDecision(id, mid, req.Decision, req.DecisionBy, req.DecisionReason, req.Encho, req.Force)
		if err != nil {
			// Map engine.ValidationError → 400, NotFoundError → 404,
			// IneligibleCompetitorError → 409 (FR-035),
			// ErrDecisionLocked → 409 (T103/CHK024).
			var alreadyIneligErr *engine.AlreadyIneligibleError
			var ineligErr *engine.IneligibleCompetitorError
			var engValErr *engine.ValidationError
			var engNotFoundErr *engine.NotFoundError
			switch {
			case errors.As(err, &alreadyIneligErr):
				// T105/CHK047: concurrent kiken — another operator already
				// recorded ineligibility for this player on a different match.
				c.JSON(http.StatusConflict, gin.H{
					"error":    "already_ineligible",
					"playerId": alreadyIneligErr.PlayerID,
					"matchId":  alreadyIneligErr.MatchID,
					"reason":   alreadyIneligErr.Reason,
				})
			case errors.As(err, &ineligErr):
				c.JSON(http.StatusConflict, gin.H{
					"error":    "ineligible_competitor",
					"playerId": ineligErr.PlayerID,
					"reason":   ineligErr.Reason,
				})
			case errors.Is(err, engine.ErrDecisionLocked):
				c.JSON(http.StatusConflict, gin.H{
					"error":  "decision_locked",
					"reason": err.Error(),
				})
			case errors.As(err, &engValErr):
				c.JSON(http.StatusBadRequest, gin.H{"error": engValErr.Error()})
			case errors.As(err, &engNotFoundErr):
				c.JSON(http.StatusNotFound, gin.H{"error": engNotFoundErr.Error()})
			default:
				c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
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
