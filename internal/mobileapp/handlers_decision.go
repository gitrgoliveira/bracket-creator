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
type DecisionRequest struct {
	Decision       string               `json:"decision"`
	DecisionBy     string               `json:"decisionBy"`
	DecisionReason string               `json:"decisionReason,omitempty"`
	Encho          *state.EnchoMetadata `json:"encho,omitempty"`
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
func RegisterDecisionHandlers(r *gin.RouterGroup, eng ScoringEngine, hub Broadcaster) {
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

		result, status, err := eng.RecordDecision(id, mid, req.Decision, req.DecisionBy, req.DecisionReason, req.Encho)
		if err != nil {
			// Map engine.ValidationError → 400, NotFoundError → 404,
			// IneligibleCompetitorError → 409 (FR-035).
			var ineligErr *engine.IneligibleCompetitorError
			switch {
			case errors.As(err, &ineligErr):
				c.JSON(http.StatusConflict, gin.H{
					"error":    "ineligible_competitor",
					"playerId": ineligErr.PlayerID,
					"reason":   ineligErr.Reason,
				})
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
