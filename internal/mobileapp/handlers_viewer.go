package mobileapp

import (
	"encoding/json"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// mergePoolNumbersIntoPlayersSlice — copy the assigned competitor Number
// (e.g. "K1") from pools.csv onto the given players slice in place.
// participants.csv does NOT persist Number — it is assigned at draw time by
// AssignPlayerNumbers and only persisted into pools.csv. Without this merge
// the viewer API never carries the numberPrefix-derived numbers, so
// TV/overlay/viewer surfaces can't render them (mp-13y). No-op when
// numberPrefix is empty or either slice is empty. Match by id first
// (HasParticipantIDs case), fall back to name.
func mergePoolNumbersIntoPlayersSlice(numberPrefix string, players []domain.Player, pools []helper.Pool) {
	if numberPrefix == "" || len(pools) == 0 || len(players) == 0 {
		return
	}
	byID := make(map[string]string)
	byName := make(map[string]string)
	for _, pool := range pools {
		for _, pp := range pool.Players {
			if pp.Number == "" {
				continue
			}
			if pp.ID != "" {
				byID[pp.ID] = pp.Number
			}
			byName[pp.Name] = pp.Number
		}
	}
	for i := range players {
		if players[i].Number != "" {
			continue
		}
		if n, ok := byID[players[i].ID]; ok && n != "" {
			players[i].Number = n
			continue
		}
		if n, ok := byName[players[i].Name]; ok && n != "" {
			players[i].Number = n
		}
	}
}

// mergePoolNumbersIntoPlayers — thin wrapper that operates on a Competition
// pointer. Existing call sites that hold a *Competition keep their idiomatic
// form; the work happens in the slice-typed helper below.
func mergePoolNumbersIntoPlayers(comp *state.Competition, pools []helper.Pool) {
	if comp == nil {
		return
	}
	mergePoolNumbersIntoPlayersSlice(comp.NumberPrefix, comp.Players, pools)
}

// viewerLoadCompetition is the store.LoadCompetition call used by the
// public viewer goroutines. It is a package-level variable so panic-
// recovery tests can swap it for a function that panics, exercising the
// safeGo wiring end-to-end without needing to corrupt on-disk state. The
// other 8 spawned goroutines also use safeGo, so a panic in any of them
// is caught by the same mechanism; this hook just gives the integration
// test something deterministic to trip.
var viewerLoadCompetition = func(store *state.Store, compID string) (*state.Competition, error) {
	return store.LoadCompetition(compID)
}

func RegisterViewerHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine) {
	// P2 (mp-9afd): singleflight group for the two expensive viewer read
	// endpoints. Created once per router setup and shared by all requests
	// via closure capture. Collapses concurrent identical builds (e.g. the
	// 1000-viewer SSE fan-out storm on every ippon) to O(1) actual builds
	// per in-flight window without serving stale data — the key is removed
	// as soon as the elected caller's fn returns, so each new wave
	// re-executes.
	sf := newViewerSingleFlight()

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
		// P2 (mp-9afd): collapse concurrent builds to O(1) per in-flight
		// window. The key is constant — all callers want the same payload.
		// On panic inside the elected build, sf.Do returns an error and
		// all waiters receive it; we map that to 500 below.
		data, err := sf.Do("competitions", func() ([]byte, error) {
			ids, err := store.ListCompetitions()
			if err != nil {
				return nil, err
			}

			// Preserve ordering by pre-allocating a slot per competition ID.
			// Each goroutine writes to a unique index so no mutex is needed;
			// wg.Wait() provides the happens-before for reads below.
			results := make([]any, len(ids))
			var wg sync.WaitGroup
			var panicRef atomic.Pointer[recoveredPanic]

			for i, id := range ids {
				idx, compID := i, id
				safeGo(&wg, &panicRef, func() {
					comp, _ := viewerLoadCompetition(store, compID)
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
					// mp-13y: merge numberPrefix-derived numbers from pools.csv.
					// Skip the pools.csv read entirely when no prefix is
					// configured — the common case, and the list endpoint
					// otherwise loads pools for every competition on every hit.
					if comp.NumberPrefix != "" {
						pools, _ := store.LoadPools(compID)
						mergePoolNumbersIntoPlayers(comp, pools)
					}

					// mp-9dz: a preview bracket on a mixed source carries pool-
					// origin placeholders ("Pool A-1st") with scheduled times
					// assigned by assignBracketMatchSlots. It MUST NOT leak into
					// the aggregate viewer payload that feeds Find-My-Matches /
					// Watchlist / global schedule / TV displays — those treat
					// every bracket match as a real, scheduled bout. Strip it
					// here so only the per-competition detail endpoint (which
					// powers the Bracket — preview tab) sees it.
					if bracket != nil && bracket.Preview {
						bracket = nil
					}

					// FR-025, T036: derive per-court queue position at serve time
					// so viewers see "Next up: 3" without persisting a value that
					// would go stale the moment any match transitions.
					annotateQueuePositions(poolMatches)
					annotateBracketQueuePositions(bracket)

					results[idx] = gin.H{
						"config":      comp,
						"poolMatches": poolMatches,
						"bracket":     bracket,
					}
				})
			}
			wg.Wait()

			if p := panicRef.Load(); p != nil {
				return nil, p
			}

			comps := make([]any, 0, len(ids))
			for _, comp := range results {
				if comp != nil {
					comps = append(comps, comp)
				}
			}
			return json.Marshal(comps)
		})

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", data)
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

		// P2 (mp-9afd): collapse concurrent detail-view builds for the
		// same competition to O(1) per in-flight window. Key includes the
		// comp id so parallel requests for different competitions are
		// independent.
		data, err := sf.Do("competition:"+id, func() ([]byte, error) {
			comp, err := store.LoadCompetition(id)
			if err != nil {
				return nil, err
			}
			if comp == nil {
				// Signal not-found so the handler can return 404.
				return nil, errNotFound
			}

			// Run all independent I/O concurrently.
			var (
				pools       []helper.Pool
				poolMatches []state.MatchResult
				standings   any
				bracket     *state.Bracket
				schedule    any

				playersErr, poolsErr, poolMatchesErr, standingsErr, bracketErr, scheduleErr error
			)

			var wg sync.WaitGroup
			var panicRef atomic.Pointer[recoveredPanic]
			safeGo(&wg, &panicRef, func() {
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
			})
			safeGo(&wg, &panicRef, func() {
				pools, poolsErr = store.LoadPools(id)
			})
			safeGo(&wg, &panicRef, func() {
				poolMatches, poolMatchesErr = store.LoadPoolMatches(id)
			})
			safeGo(&wg, &panicRef, func() {
				standings, standingsErr = eng.CalculatePoolStandings(id)
			})
			safeGo(&wg, &panicRef, func() {
				bracket, bracketErr = store.LoadBracket(id)
			})
			safeGo(&wg, &panicRef, func() {
				schedule, scheduleErr = store.LoadSchedule(id)
			})
			wg.Wait()

			if p := panicRef.Load(); p != nil {
				return nil, p
			}

			for _, e := range []error{playersErr, poolsErr, poolMatchesErr, standingsErr, bracketErr, scheduleErr} {
				if e != nil {
					return nil, e
				}
			}

			// FR-025, T036: derive per-court queue position at serve time —
			// see annotateQueuePositions for rationale.
			annotateQueuePositions(poolMatches)
			annotateBracketQueuePositions(bracket)

			// mp-13y: merge assigned competitor Number from pools.csv onto
			// comp.Players so the numberPrefix-derived "K1", "K2", … surface
			// on the TV display, streaming overlay, and viewer card.
			mergePoolNumbersIntoPlayers(comp, pools)

			return json.Marshal(gin.H{
				"config":      comp,
				"pools":       pools,
				"poolMatches": poolMatches,
				"standings":   standings,
				"bracket":     bracket,
				"schedule":    schedule,
			})
		})

		if errors.Is(err, errNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		c.Data(http.StatusOK, "application/json; charset=utf-8", data)
	})

	r.GET("/schedule", func(c *gin.Context) {
		ids, err := store.ListCompetitions()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		// Pre-allocate one slot per competition so goroutines write to unique
		// indices without a mutex. wg.Wait() provides the happens-before for
		// the reads below.
		perComp := make([][]state.ScheduleEntry, len(ids))
		var wg sync.WaitGroup
		var panicRef atomic.Pointer[recoveredPanic]
		for i, id := range ids {
			idx, compID := i, id
			safeGo(&wg, &panicRef, func() {
				s, _ := store.LoadSchedule(compID)
				perComp[idx] = s
			})
		}
		wg.Wait()
		if p := panicRef.Load(); p != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		allEntries := []state.ScheduleEntry{}
		for _, s := range perComp {
			allEntries = append(allEntries, s...)
		}
		c.JSON(http.StatusOK, allEntries)
	})
}
