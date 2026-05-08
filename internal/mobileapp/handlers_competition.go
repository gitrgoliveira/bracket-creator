package mobileapp

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

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

		if err := store.SaveCompetition(&comp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// If players are provided in the request, save them too
		if len(comp.Players) > 0 {
			if err := store.SaveParticipants(comp.ID, comp.Players); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save participants: " + err.Error()})
				return
			}

			// Also extract and save seeds
			var assignments []domain.SeedAssignment
			for _, p := range comp.Players {
				if p.Seed > 0 {
					assignments = append(assignments, domain.SeedAssignment{
						Name:     p.Name,
						SeedRank: p.Seed,
					})
				}
			}
			if len(assignments) > 0 {
				if err := store.SaveSeeds(comp.ID, assignments); err != nil {
					fmt.Printf("Warning: failed to save seeds: %v\n", err)
				}
			}
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

		if err := store.SaveCompetition(&comp); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		// If players are provided in the request, save them too
		if len(comp.Players) > 0 {
			if err := store.SaveParticipants(id, comp.Players); err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save participants: " + err.Error()})
				return
			}

			// Also extract and save seeds
			var assignments []domain.SeedAssignment
			for _, p := range comp.Players {
				if p.Seed > 0 {
					assignments = append(assignments, domain.SeedAssignment{
						Name:     p.Name,
						SeedRank: p.Seed,
					})
				}
			}
			if len(assignments) > 0 {
				if err := store.SaveSeeds(id, assignments); err != nil {
					// We'll log the error but not fail the whole request as seeds might be incomplete during setup
					fmt.Printf("Warning: failed to save seeds: %v\n", err)
				}
			}
		}

		hub.Broadcast(EventTournamentUpdated, nil)
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

	r.POST("/competitions/:id/start", func(c *gin.Context) {
		id := c.Param("id")
		if err := eng.StartCompetition(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventCompetitionStarted, gin.H{"competitionId": id})
		hub.Broadcast(EventTournamentUpdated, nil)
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

		if err := store.SaveRankOverride(id, poolId, req.PlayerName, req.Rank); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusOK)
	})

	r.PUT("/competitions/:id/schedule", func(c *gin.Context) {
		id := c.Param("id")
		var entries []state.ScheduleEntry
		if err := c.ShouldBindJSON(&entries); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := store.SaveSchedule(id, entries); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventScheduleUpdated, nil)
		c.Status(http.StatusOK)
	})

	r.DELETE("/competitions/:id/overrides", func(c *gin.Context) {
		id := c.Param("id")
		if err := store.ResetOverrides(id); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusNoContent)
	})
}
