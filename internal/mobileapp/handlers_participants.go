package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func RegisterParticipantHandlers(r *gin.RouterGroup, store *state.Store, hub Broadcaster) {
	r.GET("/competitions/:id/participants", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		players, err := store.LoadParticipants(id, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, players)
	})

	r.POST("/competitions/:id/participants", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		if comp.Status != state.CompStatusSetup && comp.Status != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "cannot modify participants after competition has started"})
			return
		}

		var req struct {
			Players []struct {
				Name        string   `json:"name"`
				DisplayName string   `json:"displayName"`
				Dojo        string   `json:"dojo"`
				Metadata    []string `json:"metadata"`
				Tag         string   `json:"tag"`
			} `json:"players"`
			Name        string   `json:"name"`
			DisplayName string   `json:"displayName"`
			Dojo        string   `json:"dojo"`
			Metadata    []string `json:"metadata"`
			DanGrade    string   `json:"danGrade"`
			Tag         string   `json:"tag"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if len(req.Players) == 0 && req.Name != "" {
			// Single player add workflow
			metadata := req.Metadata
			if len(metadata) == 0 && req.DanGrade != "" {
				metadata = []string{req.DanGrade}
			}

			if err := validatePlayerLengths(req.Name, req.DisplayName, req.Dojo, req.Tag, metadata); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			player := domain.Player{
				Name:        req.Name,
				DisplayName: req.DisplayName,
				Dojo:        req.Dojo,
				Metadata:    metadata,
				Tag:         req.Tag,
			}

			addedPlayer, err := store.AddParticipant(id, player, comp.WithZekkenName)
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to add participant: " + err.Error()})
				return
			}

			hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
			c.JSON(http.StatusOK, addedPlayer)
			return
		}

		// Per-player length caps — defense-in-depth against unbounded
		// participants.csv inflation. Reject the whole batch on the
		// first offender (matches the all-or-nothing semantics
		// SaveParticipants already enforces on write).
		for i, p := range req.Players {
			if err := validatePlayerLengths(p.Name, p.DisplayName, p.Dojo, p.Tag, p.Metadata); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("players[%d]: %s", i, err.Error())})
				return
			}
		}

		// Load existing participants so we can preserve check-in state for
		// players that survive the edit (matched by name). A full roster
		// replacement via this endpoint must not silently clear check-ins
		// that were already recorded.
		existing, err := store.LoadParticipants(id, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load participants: " + err.Error()})
			return
		}
		checkedInByName := make(map[string]bool, len(existing))
		for _, ep := range existing {
			checkedInByName[strings.ToLower(strings.TrimSpace(ep.Name))] = ep.CheckedIn
		}

		players := make([]domain.Player, 0, len(req.Players))
		for i, p := range req.Players {
			players = append(players, domain.Player{
				Name:         p.Name,
				DisplayName:  p.DisplayName,
				Dojo:         p.Dojo,
				Metadata:     p.Metadata,
				Tag:          p.Tag,
				PoolPosition: int64(i),
				CheckedIn:    checkedInByName[strings.ToLower(strings.TrimSpace(p.Name))],
			})
		}

		if err := store.SaveParticipants(id, players); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save participants: " + err.Error()})
			return
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, players)
	})

	r.PUT("/competitions/:id/participants/:pid", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		pid := c.Param("pid")

		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		if comp.Status != state.CompStatusSetup && comp.Status != "" {
			c.JSON(http.StatusConflict, gin.H{"error": "cannot modify participants after competition has started"})
			return
		}

		var req struct {
			Name        string   `json:"name"`
			DisplayName string   `json:"displayName"`
			Dojo        string   `json:"dojo"`
			Metadata    []string `json:"metadata"`
			DanGrade    string   `json:"danGrade"`
			Tag         string   `json:"tag"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		metadata := req.Metadata
		if len(metadata) == 0 && req.DanGrade != "" {
			metadata = []string{req.DanGrade}
		}

		if err := validatePlayerLengths(req.Name, req.DisplayName, req.Dojo, req.Tag, metadata); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		updatedPlayer, err := store.UpdateParticipant(id, pid, comp.WithZekkenName, func(p *domain.Player) error {
			p.Name = req.Name
			p.DisplayName = req.DisplayName
			p.Dojo = req.Dojo
			p.Metadata = metadata
			p.Tag = req.Tag
			return nil
		})

		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, state.ErrParticipantNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, updatedPlayer)
	})

	r.GET("/competitions/:id/seeds", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		seeds, err := store.LoadSeeds(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, seeds)
	})

	r.PUT("/competitions/:id/seeds", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		var assignments []domain.SeedAssignment
		if err := c.ShouldBindJSON(&assignments); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Cross-file guard symmetry with handlers_import.go's seed
		// validation: reject oversized names so seeds.csv can't grow
		// unbounded.
		for i, sa := range assignments {
			if err := validateMaxLen(fmt.Sprintf("seeds[%d].name", i), sa.Name, MaxLenSeedAssignmentName); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}

		if err := store.SaveSeeds(id, assignments); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, assignments)
	})

	r.PUT("/competitions/:id/participants/:pid/checkin", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		pid := c.Param("pid")

		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		updatedPlayer, err := store.UpdateParticipant(id, pid, comp.WithZekkenName, func(p *domain.Player) error {
			p.CheckedIn = true
			return nil
		})

		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, state.ErrParticipantNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, updatedPlayer)
	})

	r.DELETE("/competitions/:id/participants/:pid/checkin", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		pid := c.Param("pid")

		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		updatedPlayer, err := store.UpdateParticipant(id, pid, comp.WithZekkenName, func(p *domain.Player) error {
			p.CheckedIn = false
			return nil
		})

		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, state.ErrParticipantNotFound) {
				status = http.StatusNotFound
			}
			c.JSON(status, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventParticipantsUpdated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, updatedPlayer)
	})
}
