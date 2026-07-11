package mobileapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// mergePoolNumbersIntoPlayersSlice, copy the assigned competitor Number
// (e.g. "K1") from pools.csv onto the given players slice in place.
// participants.csv does NOT persist Number, it is assigned at draw time by
// AssignPlayerNumbers and only persisted into pools.csv. Without this merge
// the viewer API never carries the numberPrefix-derived numbers, so
// TV/overlay/viewer surfaces can't render them (mp-13y). No-op when
// numberPrefix is empty or either slice is empty. Match by id first
// (HasParticipantIDs case), fall back to name.
//
// For playoffs-only competitions (format == "playoffs") the engine assigns
// numbers in-memory but has no pools.csv to persist them. In that case assign
// numbers sequentially (1-N in participant order), matching generatePlayoffs.
func mergePoolNumbersIntoPlayersSlice(numberPrefix string, players []domain.Player, pools []helper.Pool, format string) {
	if numberPrefix == "" || len(players) == 0 {
		return
	}
	if len(pools) == 0 {
		if format != state.CompFormatPlayoffs {
			return
		}
		// Playoffs-only: numbers were assigned in memory by generatePlayoffs
		// but never written to disk. Re-derive them from participant order.
		for i := range players {
			if players[i].Number == "" {
				players[i].Number = fmt.Sprintf("%s%d", numberPrefix, i+1)
			}
		}
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

// mergePoolNumbersIntoPlayers, thin wrapper that operates on a Competition
// pointer. Existing call sites that hold a *Competition keep their idiomatic
// form; the work happens in the slice-typed helper below.
func mergePoolNumbersIntoPlayers(comp *state.Competition, pools []helper.Pool) {
	if comp == nil {
		return
	}
	mergePoolNumbersIntoPlayersSlice(comp.NumberPrefix, comp.Players, pools, comp.Format)
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

// buildViewerCompetitionPayload assembles the public per-competition viewer
// payload ({config, poolMatches, bracket}) shared by the aggregate
// GET /competitions and the court-scoped GET /court/:court/matches. It applies
// the identical participant/number merge, preview-bracket strip, queue-position
// annotation, and audit-field redaction so every PUBLIC surface sees the same
// non-sensitive data. Returns nil when the competition cannot be loaded.
//
// courtFilter scopes the result for the court feed: when non-empty, the comp is
// included ONLY if it is not in setup AND has at least one real match physically
// on that court (matchesPresentOnCourt). The gate runs off the same
// poolMatches/bracket this function already loads, no second read. The
// aggregate passes "" (no filter).
func buildViewerCompetitionPayload(store *state.Store, compID, courtFilter string) gin.H {
	comp, _ := viewerLoadCompetition(store, compID)
	if comp == nil {
		return nil
	}
	// A setup competition exposes no public matches (parity with compMatches in
	// viewer_utils.jsx, which returns [] for setup), so it never appears on the
	// court feed. The aggregate (courtFilter == "") still includes it.
	if courtFilter != "" && comp.Status == state.CompStatusSetup {
		return nil
	}

	// Global views like Scoring/Schedule need matches and brackets.
	poolMatches, _ := store.LoadPoolMatches(compID)
	bracket, _ := store.LoadBracket(compID)

	// Court feed: drop comps with no real match on the requested court. Checked
	// on the RAW bracket (before the preview strip below) so a preview bracket
	// never qualifies a comp for a court.
	if courtFilter != "" && !matchesPresentOnCourt(poolMatches, bracket, courtFilter) {
		return nil
	}

	// Only pass HasIDs=true hint; false means unset so auto-detect runs for
	// competitions created before the flag existed AND for the narrow window
	// where a deferred HasParticipantIDs flip fails after SaveParticipants
	// succeeded (file has UUIDs but flag is still false on disk).
	var hasIDsHint *bool
	if comp.HasParticipantIDs {
		t := true
		hasIDsHint = &t
	}
	players, _ := store.LoadParticipantsOpt(compID, comp.WithZekkenName, state.LoadParticipantsOpts{WithSeeds: false, HasIDs: hasIDsHint})
	comp.Players = players
	// mp-13y: merge numberPrefix-derived numbers from pools.csv. Skip the
	// pools.csv read entirely when no prefix is configured (the common case).
	if comp.NumberPrefix != "" {
		pools, _ := store.LoadPools(compID)
		mergePoolNumbersIntoPlayers(comp, pools)
	}

	// mp-9dz: a preview bracket carries pool-origin placeholders ("Pool A-1st")
	// with assigned times. It MUST NOT leak into the public match-list payloads
	// (Find-My-Matches / Watchlist / global schedule / TV / operator console),
	// which treat every bracket match as a real, scheduled bout.
	if bracket != nil && bracket.Preview {
		bracket = nil
	}

	// FR-025, T036: derive per-court queue position at serve time.
	annotateQueuePositions(poolMatches)
	annotateBracketQueuePositions(bracket)

	// Redact operator-only audit fields before this PUBLIC payload.
	stripMatchesAudit(poolMatches)
	stripBracketAudit(bracket)

	return gin.H{
		"config":      comp,
		"poolMatches": poolMatches,
		"bracket":     bracket,
	}
}

func RegisterViewerHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine) {
	// P2 (mp-9afd): singleflight group for the two expensive viewer read
	// endpoints. Created once per router setup and shared by all requests
	// via closure capture. Collapses concurrent identical builds (e.g. the
	// 1000-viewer SSE fan-out storm on every ippon) to O(1) actual builds
	// per in-flight window without serving stale data, the key is removed
	// as soon as the elected caller's fn returns, so each new wave
	// re-executes.
	sf := newViewerSingleFlight()

	r.GET("/tournament", func(c *gin.Context) {
		t, err := store.LoadTournament()
		if err != nil {
			// Recorded on the context (not returned to the caller) so the
			// root cause is still visible in server logs.
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		if t != nil {
			publicT := *t
			publicT.Password = ""
			c.JSON(http.StatusOK, publicT)
		} else {
			// No tournament configured yet is a normal bootstrap state, not an
			// error: return 200 with a null body so the SPA opens the create-
			// tournament gate without the browser logging a console 404.
			// fetchTournament (api_client.jsx) treats a null payload as "no
			// tournament" exactly like it did the old 404.
			c.JSON(http.StatusOK, nil)
		}
	})

	r.GET("/competitions", func(c *gin.Context) {
		// P2 (mp-9afd): collapse concurrent builds to O(1) per in-flight
		// window. The key is constant, all callers want the same payload.
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
					// Shared per-comp builder (also used by the court-scoped
					// /court/:court/matches feed). A nil payload (comp failed to
					// load) leaves results[idx] as a nil `any` so the collect
					// loop below skips it, assigning a nil gin.H directly would
					// box into a non-nil interface and slip past that filter.
					if payload := buildViewerCompetitionPayload(store, compID, ""); payload != nil {
						results[idx] = payload
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
		// Validate the: id like the admin handlers do, pre-fix, an
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

			// FR-025, T036: derive per-court queue position at serve time,
			// see annotateQueuePositions for rationale.
			annotateQueuePositions(poolMatches)
			annotateBracketQueuePositions(bracket)

			// mp-13y: merge assigned competitor Number from pools.csv onto
			// comp.Players so the numberPrefix-derived "K1", "K2", … surface
			// on the TV display, streaming overlay, and viewer card.
			mergePoolNumbersIntoPlayers(comp, pools)

			// Redact operator-only audit fields before this PUBLIC payload.
			stripMatchesAudit(poolMatches)
			stripBracketAudit(bracket)

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
