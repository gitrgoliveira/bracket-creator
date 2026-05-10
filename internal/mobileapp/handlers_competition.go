package mobileapp

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// saveCompetitionWithPlayers persists the competition config and, when players
// are present, saves participants and extracts seed assignments.
// Returns (true, nil) when the on-disk content changed, so callers can decide
// whether to broadcast.
func saveCompetitionWithPlayers(comp *state.Competition, store *state.Store) (bool, error) {
	if len(comp.Players) > 0 {
		comp.HasParticipantIDs = true // participants.csv always written with UUID IDs
	}
	changed, err := store.SaveCompetitionChanged(comp)
	if err != nil {
		return false, err
	}
	if len(comp.Players) == 0 {
		return changed, nil
	}
	if err := store.SaveParticipants(comp.ID, comp.Players); err != nil {
		return false, fmt.Errorf("failed to save participants: %w", err)
	}
	if assignments := extractSeeds(comp.Players); len(assignments) > 0 {
		if err := store.SaveSeeds(comp.ID, assignments); err != nil {
			fmt.Printf("Warning: failed to save seeds: %v\n", err)
		}
	}
	return changed, nil
}

func extractSeeds(players []helper.Player) []domain.SeedAssignment {
	var out []domain.SeedAssignment
	for _, p := range players {
		if p.Seed > 0 {
			out = append(out, domain.SeedAssignment{Name: p.Name, SeedRank: p.Seed})
		}
	}
	return out
}

func RegisterCompetitionHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine, hub *Hub) {
	r.GET("/competitions", func(c *gin.Context) {
		ids, err := store.ListCompetitions()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		var comps []*state.Competition
		for _, id := range ids {
			comp, err := store.LoadCompetition(id)
			if err == nil && comp != nil {
				comps = append(comps, comp)
			}
		}
		c.JSON(http.StatusOK, comps)
	})

	r.POST("/competitions", func(c *gin.Context) {
		var comp state.Competition
		if err := c.ShouldBindJSON(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if comp.ID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "competition ID is required"})
			return
		}

		if _, err := saveCompetitionWithPlayers(&comp, store); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusCreated, comp)
	})

	r.GET("/competitions/:id", func(c *gin.Context) {
		id := c.Param("id")
		comp, err := store.LoadCompetition(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		c.JSON(http.StatusOK, comp)
	})

	r.PUT("/competitions/:id", func(c *gin.Context) {
		id := c.Param("id")
		var comp state.Competition
		if err := c.ShouldBindJSON(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		comp.ID = id // ensure ID matches URL

		changed, err := saveCompetitionWithPlayers(&comp, store)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.JSON(http.StatusOK, comp)
	})

	r.DELETE("/competitions/:id", func(c *gin.Context) {
		id := c.Param("id")
		comp, err := store.LoadCompetition(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}

		// Only allow delete in setup status (as per plan)
		if comp.Status != "setup" && comp.Status != "" {
			c.JSON(http.StatusForbidden, gin.H{"error": "cannot delete competition that has already started"})
			return
		}

		if err := store.DeleteCompetition(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusNoContent)
	})

	r.GET("/competitions/:id/reserved-slots", func(c *gin.Context) {
		id := c.Param("id")
		slots, err := store.LoadReservedSlots(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, slots)
	})

	r.POST("/competitions/:id/reserved-slots", func(c *gin.Context) {
		id := c.Param("id")
		var req struct {
			SourceCompID string `json:"sourceCompID"`
			SourceRank   int    `json:"sourceRank"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.SourceCompID == "" || req.SourceRank < 1 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "sourceCompID and sourceRank (>= 1) are required"})
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		slot, err := store.AddReservedSlot(id, req.SourceCompID, req.SourceRank, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusCreated, slot)
	})

	r.DELETE("/competitions/:id/reserved-slots/:slotID", func(c *gin.Context) {
		id := c.Param("id")
		slotID := c.Param("slotID")
		comp, err := store.LoadCompetition(id)
		if err != nil || comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if err := store.RemoveReservedSlot(id, slotID, comp.WithZekkenName); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusNoContent)
	})

	r.POST("/competitions/:id/start", func(c *gin.Context) {
		id := c.Param("id")
		if err := eng.StartCompetition(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventCompetitionStarted, gin.H{"competitionId": id})
		c.Status(http.StatusOK)
	})

	r.GET("/competitions/:id/export", func(c *gin.Context) {
		id := c.Param("id")
		data, err := eng.ExportCompetitionXlsx(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		filename := fmt.Sprintf("bracket-%s.xlsx", id)
		c.Header("Content-Description", "File Transfer")
		c.Header("Content-Disposition", "attachment; filename="+filename)
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
	})

	r.PUT("/competitions/:id/pools/:poolId/override-rank", func(c *gin.Context) {
		id := c.Param("id")
		poolId := c.Param("poolId")
		var req struct {
			PlayerName string `json:"playerName"`
			Rank       int    `json:"rank"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		changed, err := store.SaveRankOverrideChanged(id, poolId, req.PlayerName, req.Rank)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.Status(http.StatusOK)
	})

	r.PUT("/competitions/:id/schedule", func(c *gin.Context) {
		id := c.Param("id")
		var entries []state.ScheduleEntry
		if err := c.ShouldBindJSON(&entries); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		changed, err := store.SaveScheduleChanged(id, entries)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventScheduleUpdated, nil)
		}
		c.Status(http.StatusOK)
	})

	r.DELETE("/competitions/:id/overrides", func(c *gin.Context) {
		id := c.Param("id")
		changed, err := store.ResetOverridesChanged(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.Status(http.StatusNoContent)
	})
}
