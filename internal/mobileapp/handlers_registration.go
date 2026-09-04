package mobileapp

import (
	"errors"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RegisterPublicRegistrationHandlers registers public (no-auth) self-registration
// endpoints on the given router group (mp-e5j). These routes are only functional
// in self-run tournaments; they return 404 for officiated tournaments (defense-in-depth).
//
// Routes:
//
//	GET  /api/register/competitions/:id, competition metadata for the registration form
//	POST /api/register/competitions/:id, register a new participant (tag="registered")
func RegisterPublicRegistrationHandlers(r *gin.RouterGroup, store *state.Store, hub Broadcaster) {
	r.GET("/register/competitions/:id", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load tournament"})
			return
		}
		if t == nil || t.Mode != state.TournamentModeSelfRun {
			c.JSON(http.StatusNotFound, gin.H{"error": "registration is not available for this competition"})
			return
		}

		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		if comp.Kind == "team" {
			c.JSON(http.StatusNotFound, gin.H{"error": "registration is not available for this competition"})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"id":             comp.ID,
			"name":           comp.Name,
			"withZekkenName": comp.WithZekkenName,
			"status":         comp.Status,
		})
	})

	r.POST("/register/competitions/:id", MaxBodyBytes(DefaultMaxBodyBytes), func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load tournament"})
			return
		}
		if t == nil || t.Mode != state.TournamentModeSelfRun {
			c.JSON(http.StatusNotFound, gin.H{"error": "registration is not available for this competition"})
			return
		}

		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		if comp.Kind == "team" {
			c.JSON(http.StatusNotFound, gin.H{"error": "registration is not available for this competition"})
			return
		}

		if comp.Status != state.CompStatusSetup && comp.Status != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "registration is closed for this competition"})
			return
		}

		var req struct {
			Name        string `json:"name"`
			DisplayName string `json:"displayName"`
			Dojo        string `json:"dojo"`
			DanGrade    string `json:"danGrade"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			log.Printf("registration: invalid JSON body for %s: %v", id, err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
			return
		}

		name := strings.TrimSpace(req.Name)
		dojo := strings.TrimSpace(req.Dojo)
		if name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "name must not be blank"})
			return
		}
		if dojo == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "dojo must not be blank"})
			return
		}

		// Strip displayName unless the zekken column is enabled, to avoid CSV
		// mis-parse. Engi pairs store both member names combined in
		// Player.Name, so DisplayName is only the zekken column value.
		displayName := strings.TrimSpace(req.DisplayName)
		if !comp.EffectiveWithZekkenName() {
			displayName = ""
		}

		var metadata []string
		if dg := strings.TrimSpace(req.DanGrade); dg != "" {
			metadata = []string{dg}
		}

		if err := validatePlayerLengths(name, displayName, dojo, "registered", metadata); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		player := domain.Player{
			Name:        name,
			DisplayName: displayName,
			Dojo:        dojo,
			Metadata:    metadata,
			Source:      "registered",
		}

		addedPlayer, err := store.AddParticipant(id, player, comp.EffectiveWithZekkenName())
		if err != nil {
			// ErrDuplicateName gets a friendlier public-facing message here
			// (this is the unauthenticated self-registration endpoint), so it
			// is checked BEFORE respondRosterWriteError -- that shared
			// classifier (errors.go) always answers with err.Error()
			// verbatim, which would leak the raw "same name and dojo" wording
			// to a walk-up competitor instead of this endpoint's guidance.
			if errors.Is(err, state.ErrDuplicateName) {
				c.JSON(http.StatusConflict, gin.H{"error": "A participant with this name is already registered. If this is you, no action needed. If not, try including your dojo name."})
				return
			}
			// ErrBlankDojo is also checked BEFORE respondRosterWriteError, for a
			// different reason than ErrDuplicateName above: the write-floor
			// guard (state.saveParticipantsNoLock) scans the WHOLE stored
			// roster, not just this incoming player, and this registrant's own
			// dojo was already validated non-blank above. So when it fires
			// here, it is naming a blank dojo left by ANOTHER, pre-existing
			// (legacy-imported or hand-edited) row -- server-side data damage,
			// not something this registrant did wrong. respondRosterWriteError
			// would answer 400 with err.Error() verbatim, which is that OTHER
			// competitor's name leaked to an anonymous walk-up, and it would
			// block every registration until an operator repairs the row, with
			// no signal that repair is needed. Answer 500-class with a generic
			// public message instead, and log the offending row so the
			// operator can find and fix it.
			if errors.Is(err, state.ErrBlankDojo) {
				internalError(c, err, "registration is temporarily unavailable, please contact an organiser")
				return
			}
			if respondRosterWriteError(c, err) {
				return
			}
			if errors.Is(err, state.ErrCompetitionNotInSetup) {
				c.JSON(http.StatusConflict, gin.H{"error": "registration is closed for this competition"})
				return
			}
			log.Printf("registration: failed to add participant to %s: %v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to register participant"})
			return
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, addedPlayer)
	})
}
