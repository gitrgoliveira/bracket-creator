package mobileapp

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

func RegisterViewerHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine) {
	r.GET("/tournament", func(c *gin.Context) {
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t != nil {
			publicT := *t
			publicT.Password = ""
			c.JSON(http.StatusOK, publicT)
		} else {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		}
	})

	r.GET("/competitions", func(c *gin.Context) {
		ids, _ := store.ListCompetitions()
		comps := []*state.Competition{}
		for _, id := range ids {
			comp, _ := store.LoadCompetition(id)
			if comp != nil {
				players, _ := store.LoadParticipants(id, comp.WithZekkenName)
				comp.Players = players
				comps = append(comps, comp)
			}
		}
		c.JSON(http.StatusOK, comps)
	})

	r.GET("/competitions/:id", func(c *gin.Context) {
		id := c.Param("id")
		comp, err := store.LoadCompetition(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		players, err := store.LoadParticipants(id, comp.WithZekkenName)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		comp.Players = players

		// Return combined info: config, pools, poolMatches, standings, bracket, schedule
		pools, err := store.LoadPools(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		poolMatches, err := store.LoadPoolMatches(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		standings, err := eng.CalculatePoolStandings(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		bracket, err := store.LoadBracket(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		schedule, err := store.LoadSchedule(id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"config":      comp,
			"pools":       pools,
			"poolMatches": poolMatches,
			"standings":   standings,
			"bracket":     bracket,
			"schedule":    schedule,
		})
	})

	r.GET("/schedule", func(c *gin.Context) {
		ids, _ := store.ListCompetitions()
		allEntries := []state.ScheduleEntry{}
		for _, id := range ids {
			s, _ := store.LoadSchedule(id)
			allEntries = append(allEntries, s...)
		}
		c.JSON(http.StatusOK, allEntries)
	})
}
