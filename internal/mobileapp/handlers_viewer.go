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
		// Each goroutine writes to a unique index so no mutex is needed;
		// wg.Wait() provides the happens-before for reads below.
		results := make([]any, len(ids))
		var wg sync.WaitGroup

		for i, id := range ids {
			wg.Add(1)
			go func(idx int, compID string) {
				defer wg.Done()
				comp, _ := store.LoadCompetition(compID)
				if comp == nil {
					return
				}
				// Only pass HasIDs=true hint; false means unset so auto-detect
				// runs for competitions created before the flag existed AND
				// for the narrow window where a deferred HasParticipantIDs
				// flip fails after SaveParticipants succeeded (file has UUIDs
				// but flag is still false on disk). Pre-fix this site passed
				// `&hasIDs` (non-nil false) which bypassed auto-detect and
				// misparsed UUID-prefix rows as plain Name fields. Mirrors
				// the detail-view pattern at line ~101 and the engine load
				// pattern in StartCompetition.
				var hasIDsHint *bool
				if comp.HasParticipantIDs {
					t := true
					hasIDsHint = &t
				}
				players, _ := store.LoadParticipantsOpt(compID, comp.WithZekkenName, state.LoadParticipantsOpts{WithSeeds: false, HasIDs: hasIDsHint})
				comp.Players = players

				// Global views like Scoring/Schedule need matches and brackets
				poolMatches, _ := store.LoadPoolMatches(compID)
				bracket, _ := store.LoadBracket(compID)

				// FR-025, T036: derive per-court queue position at serve time
				// so viewers see "Next up: 3" without persisting a value that
				// would go stale the moment any match transitions.
				annotateQueuePositions(poolMatches)

				results[idx] = gin.H{
					"config":      comp,
					"poolMatches": poolMatches,
					"bracket":     bracket,
				}
			}(i, id)
		}
		wg.Wait()

		comps := make([]any, 0, len(ids))
		for _, comp := range results {
			if comp != nil {
				comps = append(comps, comp)
			}
		}
		c.JSON(http.StatusOK, comps)
	})

	r.GET("/competitions/:id", func(c *gin.Context) {
		// Validate the :id like the admin handlers do — pre-fix, an
		// invalid ID here returned 500 (LoadCompetition's internal
		// ValidateCompetitionID surfaced as a generic error response)
		// while the OpenAPI spec on the CompetitionId parameter
		// documents 400 for invalid IDs. Aligning to 400 makes the
		// spec accurate and matches the path-traversal-defense
		// rationale documented in the spec.
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
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
			poolMatches   []state.MatchResult
			standings     any
			bracket       any
			schedule      any
			reservedSlots []state.ReservedSlot

			playersErr, poolsErr, poolMatchesErr, standingsErr, bracketErr, scheduleErr, reservedSlotsErr error
		)

		var wg sync.WaitGroup
		wg.Add(7)
		go func() {
			defer wg.Done()
			// Only pass HasIDs=true hint; false means unset so auto-detect
			// still runs for competitions created before the flag existed.
			var hasIDsHint *bool
			if comp.HasParticipantIDs {
				t := true
				hasIDsHint = &t
			}
			p, e := store.LoadParticipantsOpt(id, comp.WithZekkenName, state.LoadParticipantsOpts{
				WithSeeds: true,
				HasIDs:    hasIDsHint,
			})
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
		go func() {
			defer wg.Done()
			reservedSlots, reservedSlotsErr = store.LoadReservedSlots(id)
		}()
		wg.Wait()

		for _, e := range []error{playersErr, poolsErr, poolMatchesErr, standingsErr, bracketErr, scheduleErr, reservedSlotsErr} {
			if e != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": e.Error()})
				return
			}
		}

		// FR-025, T036: derive per-court queue position at serve time —
		// see annotateQueuePositions for rationale.
		annotateQueuePositions(poolMatches)

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
		// Pre-allocate one slot per competition so goroutines write to unique
		// indices without a mutex. wg.Wait() provides the happens-before for
		// the reads below.
		perComp := make([][]state.ScheduleEntry, len(ids))
		var wg sync.WaitGroup
		for i, id := range ids {
			wg.Add(1)
			go func(idx int, compID string) {
				defer wg.Done()
				s, _ := store.LoadSchedule(compID)
				perComp[idx] = s
			}(i, id)
		}
		wg.Wait()
		allEntries := []state.ScheduleEntry{}
		for _, s := range perComp {
			allEntries = append(allEntries, s...)
		}
		c.JSON(http.StatusOK, allEntries)
	})
}
