package mobileapp

import (
	"net/http"
	"sync"

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

		// Preserve ordering by pre-allocating a slot per competition ID.
		results := make([]*state.Competition, len(ids))
		var mu sync.Mutex
		var wg sync.WaitGroup

		for i, id := range ids {
			wg.Add(1)
			go func(idx int, compID string) {
				defer wg.Done()
				comp, _ := store.LoadCompetition(compID)
				if comp == nil {
					return
				}
				players, _ := store.LoadParticipants(compID, comp.WithZekkenName)
				comp.Players = players
				mu.Lock()
				results[idx] = comp
				mu.Unlock()
			}(i, id)
		}
		wg.Wait()

		comps := make([]*state.Competition, 0, len(ids))
		for _, comp := range results {
			if comp != nil {
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

		// Run all independent I/O concurrently.
		var (
			pools         any
			poolMatches   any
			standings     any
			bracket       any
			schedule      any
			reservedSlots []state.ReservedSlot

			playersErr, poolsErr, poolMatchesErr, standingsErr, bracketErr, scheduleErr error
		)

		var wg sync.WaitGroup
		wg.Add(6)
		go func() {
			defer wg.Done()
			p, e := store.LoadParticipants(id, comp.WithZekkenName)
			comp.Players = p
			playersErr = e
		}()
		go func() {
			defer wg.Done()
			pools, poolsErr = store.LoadPools(id)
		}()
		go func() {
			defer wg.Done()
			poolMatches, poolMatchesErr = store.LoadPoolMatches(id)
		}()
		go func() {
			defer wg.Done()
			standings, standingsErr = eng.CalculatePoolStandings(id)
		}()
		go func() {
			defer wg.Done()
			bracket, bracketErr = store.LoadBracket(id)
		}()
		go func() {
			defer wg.Done()
			schedule, scheduleErr = store.LoadSchedule(id)
		}()
		wg.Wait()

		reservedSlots, _ = store.LoadReservedSlots(id)

		for _, e := range []error{playersErr, poolsErr, poolMatchesErr, standingsErr, bracketErr, scheduleErr} {
			if e != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": e.Error()})
				return
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"config":        comp,
			"pools":         pools,
			"poolMatches":   poolMatches,
			"standings":     standings,
			"bracket":       bracket,
			"schedule":      schedule,
			"reservedSlots": reservedSlots,
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
