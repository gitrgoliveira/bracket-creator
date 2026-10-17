// Package mobileapp, handlers_eligibility.go owns the
// `/api/competitions/:cid/competitor-status` endpoints (T091).
//
// GET returns every persisted status entry for the competition; POST
// sets (or replaces) a single entry. The frontend subscribes to the
// SSE `competitor_status_updated` event (T092) to invalidate cached
// match-list state when a status changes.
//
// All consumers go through CompetitorStatusStore + Broadcaster
// (deps.go) rather than the concrete *state.Store / *Hub (NFR-002).
package mobileapp

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
)

// CompetitorStatusRequest is the body for POST /competitor-status.
// Mirrors domain.CompetitorStatus on the wire.
type CompetitorStatusRequest struct {
	PlayerID string `json:"playerId"`
	Eligible bool   `json:"eligible"`
	Reason   string `json:"reason,omitempty"`
	MatchID  string `json:"matchId,omitempty"`
}

// Validate enforces persisted-string caps on the request shape. The
// domain.CompetitorStatus.Validate path covers presence (PlayerID,
// Reason on ineligible) but not length, this fills that gap so a
// 1MB reason can't bloat competitor_status.yaml.
func (r *CompetitorStatusRequest) Validate() error {
	if err := validateMaxLen("playerId", r.PlayerID, MaxLenEntityID); err != nil {
		return err
	}
	if err := validateMaxLen("matchId", r.MatchID, MaxLenEntityID); err != nil {
		return err
	}
	if err := validateMaxLen("reason", r.Reason, MaxLenEligibilityReason); err != nil {
		return err
	}
	return nil
}

func (r *CompetitorStatusRequest) toDomain() domain.CompetitorStatus {
	return domain.CompetitorStatus{
		PlayerID: r.PlayerID,
		Eligible: r.Eligible,
		Reason:   r.Reason,
		MatchID:  r.MatchID,
	}
}

// RegisterPublicEligibilityHandlers wires the read-only
// GET /competitions/:id/competitor-status endpoint on an unauthenticated
// router group. This mirrors the other /api/competitions/:id/* viewer
// GETs: eligibility state is not sensitive (it's derivable from the
// public match results) and the display/viewer surfaces need it without
// admin credentials. The write path (POST) stays on the admin group via
// RegisterEligibilityHandlers.
//
// T091, FR-034.
func RegisterPublicEligibilityHandlers(r *gin.RouterGroup, store CompetitorStatusStore) {
	r.GET("/competitions/:id/competitor-status", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		statuses, err := store.LoadCompetitorStatus(id)
		if err != nil {
			internalError(c, err)
			return
		}
		// Return as a slice so the response shape is stable / orderable
		// even if Go map iteration order isn't.
		out := make([]domain.CompetitorStatus, 0, len(statuses))
		for _, st := range statuses {
			out = append(out, st)
		}
		c.JSON(http.StatusOK, gin.H{"statuses": out})
	})
}

// RegisterEligibilityHandlers wires the write-only
// POST /competitions/:id/competitor-status endpoint on the admin
// (auth-protected) router group. The corresponding GET is public and
// registered via RegisterPublicEligibilityHandlers.
//
// T091, FR-034.
func RegisterEligibilityHandlers(r *gin.RouterGroup, store CompetitorStatusStore, hub Broadcaster) {
	r.POST("/competitions/:id/competitor-status", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		var req CompetitorStatusRequest
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
		status := req.toDomain()
		if err := store.SetCompetitorStatus(id, status); err != nil {
			// domain.CompetitorStatus.Validate returns sentinel errors
			// for shape failures; map to 400.
			if errors.Is(err, domain.ErrCompetitorStatusMissingPlayerID) ||
				errors.Is(err, domain.ErrCompetitorStatusMissingReason) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			internalError(c, err)
			return
		}
		// Reload after write so the broadcast carries the persisted
		// RecordedAt (defaulted server-side when the caller left it
		// zero).
		statuses, err := store.LoadCompetitorStatus(id)
		if err == nil {
			if st, ok := statuses[status.PlayerID]; ok {
				status = st
			}
		}
		hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
			"competitionId": id,
			"status":        status,
		})
		c.JSON(http.StatusOK, status)
	})
}

// RegisterReinstateHandler wires POST /competitions/:id/competitors/:pid/reinstate
// on the admin (auth-protected) router group. Restores eligibility for
// a competitor who was withdrawn via kiken-injury (FIK Art. 30).
// Voluntary kiken (Art. 31) and fusenpai statuses are not reinstateable.
func RegisterReinstateHandler(r *gin.RouterGroup, eng EligibilityEngine, hub Broadcaster) {
	// A reinstatement is ordered by arrival: one given up on at the deadline
	// can land after the withdrawal recorded since, and would lift it. A late
	// duplicate of one already applied is answered as recorded (writeOnce, Z3).
	once := newWriteOnce()
	r.POST("/competitions/:id/competitors/:pid/reinstate", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		pid := c.Param("pid")
		if pid == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "playerId is required"})
			return
		}
		// The optional {writeId} body, as the other doors ordered by arrival.
		tag, ok := bindOptionalWriteTag(c)
		if !ok {
			return
		}

		var status *domain.CompetitorStatus
		applied := false
		ran := once.do("reinstate\x1f"+id+"\x1f"+pid, tag.WriteID, func() bool {
			var err error
			status, err = eng.ReinstateCompetitor(id, pid)
			if err != nil {
				var engValErr *engine.ValidationError
				if errors.As(err, &engValErr) {
					c.JSON(http.StatusConflict, gin.H{"error": engValErr.Error()})
					return false
				}
				internalError(c, err)
				return false
			}
			applied = true
			return true
		})
		if !ran {
			// A late duplicate: the competitor is not reinstated again and
			// nothing is announced. The status they hold now is the
			// announcements' to say, so the answer names the competitor only.
			c.JSON(http.StatusOK, gin.H{"playerId": pid})
			return
		}
		if !applied {
			return
		}

		hub.Broadcast(EventCompetitorStatusUpdated, gin.H{
			"competitionId": id,
			"status":        status,
		})
		c.JSON(http.StatusOK, status)
	})
}
