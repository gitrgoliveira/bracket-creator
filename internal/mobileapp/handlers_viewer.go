package mobileapp

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// mergePoolNumbersIntoPlayersSlice fills each player's ASSIGNED Number (e.g.
// "K1") in place. participants.csv never persists Number: the draw assigns it
// and persists it only in pools.csv, so every payload that shows a number
// derives it here at read time (mp-13y).
//
// Two sources, both "assigned by the draw":
//   - a pools.csv exists: the number is the one it holds, matched by id first
//     (HasParticipantIDs case) and by (name, dojo) -- the identity rule this
//     whole codebase uses, never bare name -- as the legacy fallback for
//     rosters drawn before ids existed;
//   - the competition is playoffs-only: its draw never writes pools.csv and
//     its number IS participant order under the prefix, composed through the
//     same helper the draw uses.
//
// comp supplies both NumberPrefix and the EFFECTIVE format
// (comp.EffectiveFormat(), never comp.Format directly: an unset Format ("")
// is standalone playoffs too, generation's default case falls to
// generatePlayoffs for it identically), so every caller derives the same
// answer from the same source instead of restating the caveat locally.
// players is still explicit -- separate from comp.Players -- because the
// court-overlay caller (currentMatchPlayers) loads its OWN roster slice
// rather than mutating the competition's.
//
// The playoffs-only branch below calls engine.NumberPlayoffsOnlyParticipants
// directly (a plain package-level function, its receiver was never
// read; mobileapp already imports engine, so the PlayoffsNumberingEngine
// consumer-boundary interface this used to thread through as an `eng`
// parameter bought nothing over the direct call), the SAME function the
// blank-template export's NumberedParticipantsFor calls -- ONE derivation,
// not two independent call sites invoking the shared
// helper.AssignPlayerNumbers primitive, which is what actually prevents the
// public payload and the printed Tags/Names-to-Print sheets from silently
// disagreeing.
//
// Any other format with no pools is left WITHOUT numbers: before the draw a
// pooled competition's competitors show no number anywhere (bc-pnum operator
// ruling), and a public surface never shows a number the draw has not
// assigned. Callers pass pools only from a SUCCESSFUL read:
// an unreadable pools.csv is reported, not treated as "no draw yet", so a
// corrupt file cannot make this compose numbers that contradict the draw on
// disk. No-op when comp is nil, its NumberPrefix is empty, or the roster is
// empty.
func mergePoolNumbersIntoPlayersSlice(comp *state.Competition, players []domain.Player, pools []helper.Pool) {
	if comp == nil || comp.NumberPrefix == "" || len(players) == 0 {
		return
	}
	if len(pools) == 0 {
		if comp.EffectiveFormat() != state.CompFormatPlayoffs {
			return
		}
		// Playoffs-only: nothing on disk carries a number, so derive it from
		// participant order under the current prefix. A prefix change is
		// therefore reflected on the next read with no file write.
		engine.NumberPlayoffsOnlyParticipants(comp, players)
		return
	}
	byID := make(map[string]string)
	for _, pool := range pools {
		for _, pp := range pool.Players {
			if pp.Number != "" && pp.ID != "" {
				byID[pp.ID] = pp.Number
			}
		}
	}
	// byNameDojo is built LAZILY, on the first roster row that misses
	// byID, rather than unconditionally up front. The vast majority of
	// payload builds are for a roster where every row carries an id (the
	// common, ids-everywhere case), so byID alone resolves every row and
	// this second pass over every pool -- plus two helper.CompetitorKey
	// normalisations per row -- was pure waste on every single payload
	// build. Deliberately NOT gated on "the roster has ids": a pool row may
	// carry a blank id while the roster row has one (or vice versa), and
	// that legacy (name, dojo) fallback must still be reachable per row, not
	// switched off for the whole call based on one row's shape.
	var byNameDojo map[string]string
	for i := range players {
		if players[i].Number != "" {
			continue
		}
		if n, ok := byID[players[i].ID]; ok && n != "" {
			players[i].Number = n
			continue
		}
		if byNameDojo == nil {
			// bc-pnum A4: keyed on (name, dojo) via the shared identity
			// primitive, not bare name -- two legal namesakes from
			// DIFFERENT dojos (allowed everywhere per this repo's identity
			// rule) used to collide in a name-only map, so the SECOND one
			// written silently overwrote the FIRST's number and both
			// entrants in the public payload showed the second's number.
			//
			// Accepted degradation, id-less legacy rosters only: a
			// competitor edited (dojo corrected/transferred) AFTER a legacy
			// draw with no participant IDs shows NO number rather than a
			// WRONG one. Neither key can survive the edit -- ID is blank on
			// both sides for legacy data, and the (name, dojo) key below is
			// now the participant's NEW dojo against the pool row's OLD one
			// -- so the lookup misses cleanly and the fall-through below
			// leaves Number empty. That is the correct failure direction: a
			// silent match on the wrong dojo (or on bare name, the exact A4
			// bug this key replaced) would misattribute someone else's
			// number instead.
			byNameDojo = make(map[string]string)
			for _, pool := range pools {
				for _, pp := range pool.Players {
					if pp.Number == "" {
						continue
					}
					byNameDojo[helper.CompetitorKey("", pp.Name, pp.Dojo)] = pp.Number
				}
			}
		}
		if n, ok := byNameDojo[helper.CompetitorKey("", players[i].Name, players[i].Dojo)]; ok && n != "" {
			players[i].Number = n
		}
	}
}

// mergePoolNumbersIntoPlayers, thin wrapper that operates on a Competition
// pointer's own roster. Existing call sites that hold a *Competition keep
// their idiomatic form; the work happens in the slice-typed helper above.
func mergePoolNumbersIntoPlayers(comp *state.Competition, pools []helper.Pool) {
	if comp == nil {
		return
	}
	mergePoolNumbersIntoPlayersSlice(comp, comp.Players, pools)
}

// viewerLoadCompetition is the store.LoadCompetition call used by the
// public viewer goroutines. It is a package-level variable so tests can
// swap it without corrupting on-disk state: panic-recovery tests substitute
// a panicking load (exercising the safeGo wiring end-to-end), and the
// court-feed singleflight test substitutes a slow load to hold a build
// in-flight. The other 8 spawned goroutines also use safeGo, so a panic in
// any of them is caught by the same mechanism; this hook just gives the
// integration tests something deterministic to trip.
var viewerLoadCompetition = func(store *state.Store, compID string) (*state.Competition, error) {
	return store.LoadCompetition(compID)
}

// buildViewerCompetitionPayloads lists competitions and builds each public
// per-comp payload concurrently: one safeGo goroutine per comp writing to a
// unique index of a pre-allocated results slice (no mutex needed; wg.Wait
// provides the happens-before), so the wall-clock cost is the slowest single
// build, not the sum. Shared by GET /competitions (courtFilter "") and the
// court feed GET /court/:court/matches. Non-nil payloads are returned in
// listing order; the returned slice is non-nil even when empty so callers
// marshal [] rather than null.
func buildViewerCompetitionPayloads(store *state.Store, courtFilter string) ([]any, error) {
	ids, err := store.ListCompetitions()
	if err != nil {
		return nil, err
	}

	results := make([]any, len(ids))
	var wg sync.WaitGroup
	var panicRef atomic.Pointer[recoveredPanic]
	for i, id := range ids {
		idx, compID := i, id
		safeGo(&wg, &panicRef, func() {
			// A nil payload (comp filtered out or failed to load) leaves
			// results[idx] as a nil `any` so the collect loop below skips
			// it; assigning a nil gin.H directly would box into a non-nil
			// interface and slip past that filter.
			if payload := buildViewerCompetitionPayload(store, compID, courtFilter); payload != nil {
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
	return comps, nil
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
	// Per-comp read faults degrade to skipping (or thinning) the comp rather
	// than failing the whole viewer payload — the availability trade for the
	// public list surfaces — but every failed load below is logged so a
	// corrupt competition leaves a server-side breadcrumb instead of
	// silently vanishing from (or thinning on) every board.
	comp, err := viewerLoadCompetition(store, compID)
	if err != nil {
		log.Printf("mobileapp: viewer payload %s: load competition: %v", compID, err)
	}
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
	poolMatches, pmErr := store.LoadPoolMatches(compID)
	if pmErr != nil {
		log.Printf("mobileapp: viewer payload %s: load pool matches: %v", compID, pmErr)
	}
	bracket, brErr := store.LoadBracket(compID)
	if brErr != nil {
		log.Printf("mobileapp: viewer payload %s: load bracket: %v", compID, brErr)
	}

	// Court feed: drop comps with no real match on the requested court. Checked
	// on the RAW bracket (before the preview strip below) so a preview bracket
	// never qualifies a comp for a court.
	if courtFilter != "" && !matchesPresentOnCourt(poolMatches, bracket, courtFilter) {
		return nil
	}

	// WithSeeds: true, matching the single-competition detail endpoint below.
	// The admin SPA renders AdminCompetition off THIS aggregate object until
	// (and permanently, if) the detail fetch fails (admin.jsx's
	// `detail?.config || c` fallback), and the fill-bracket settings preview
	// derives its supply from the roster's seed ranks -- an aggregate without
	// them briefly showed the UNSEEDED pool cut, a different pool COUNT, not
	// just a missing annotation. Seeds leak nothing the detail endpoint does
	// not already serve publicly, and the load is cached per mtime.
	players, plErr := store.LoadParticipantsOpt(compID, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{WithSeeds: true, HasIDs: comp.ParticipantIDsHint()})
	if plErr != nil {
		log.Printf("mobileapp: viewer payload %s: load participants: %v", compID, plErr)
	}
	comp.Players = players
	// mp-13y: merge numberPrefix-derived numbers from pools.csv. Skip the
	// pools.csv read entirely when no prefix is configured -- bc-pnum G2
	// means that is now the rare case (a legacy competition that predates
	// the never-empty-prefix rule and has not yet had a chance to heal it,
	// see RenumberCompetitors/G7), not the common one.
	var poolsErr error
	if comp.NumberPrefix != "" {
		var pools []helper.Pool
		pools, poolsErr = store.LoadPools(compID)
		if poolsErr != nil {
			// Reported, not merged: an unreadable pools.csv must show as
			// MISSING numbers, never as composed ones (D1). bc-pnum C4: only a
			// PARSE failure joins the payload's dataIssues below (dataIssuesFrom
			// -> state.AsCorruptFile, which corruptCSV populates from a
			// csv.ParseError specifically). A raw READ error (permissions, I/O)
			// is not something an operator repairs with a text editor, and its
			// message names the absolute path on disk, which must never reach
			// this PUBLIC payload -- it is logged server-side only, here.
			log.Printf("mobileapp: viewer payload %s: load pools: %v", compID, poolsErr)
		} else {
			mergePoolNumbersIntoPlayers(comp, pools)
		}
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

	payload := gin.H{
		"config":      comp,
		"poolMatches": poolMatches,
		"bracket":     bracket,
	}
	// Both loads above already SWALLOW their error into a log and carry on with
	// whatever they got, which is right -- one unreadable file must not blank a
	// whole competition view. But it left the operator with a silently
	// half-empty competition and no way to learn why. Carry the located reason
	// so the console can say which file is broken and where, and so "the
	// bracket is missing" and "the bracket file will not parse" stop looking
	// identical. This is the aggregate the admin SPA renders AdminCompetition
	// off (see the comment above the participant load), which is why it is the
	// right place for it despite the endpoint being public: the audience gate
	// is at render time.
	//
	// bc-pnum ruling 1b widened this beyond parser syntax: a legacy
	// participants.csv that predates the id-minting write path loads fine (no
	// parse error) but leaves some rows with no stable id, which is exactly
	// the kind of operator-actionable, per-competition data problem this list
	// exists for. viewerDataIssues folds that in alongside the corrupt-file
	// errors, and is the ONE place both this aggregate and the single-
	// competition detail endpoint build the list from (bc-pnum ruling 1e
	// follow-up), so the two surfaces never disagree about what a given
	// competition's issues are.
	issues := viewerDataIssues(players, pmErr, brErr, poolsErr)
	if len(issues) > 0 {
		payload["dataIssues"] = issues
	}
	return payload
}

// dataIssuesFrom collects the located file failures among errs, dropping
// everything else: a missing file, a permissions problem or a nil is not
// something an operator repairs with a text editor, so it gets no banner.
// Each entry's "kind" is implicitly "corrupt-file" (the SPA defaults an
// entry with no "kind" field to that reading; see data_integrity.jsx).
func dataIssuesFrom(errs ...error) []gin.H {
	var issues []gin.H
	for _, err := range errs {
		cf, ok := state.AsCorruptFile(err)
		if !ok {
			continue
		}
		issues = append(issues, gin.H{
			"file":   cf.File,
			"line":   cf.Line,
			"column": cf.Column,
			"detail": cf.Detail,
		})
	}
	return issues
}

// missingParticipantIDsIssue reports a loaded roster that still has id-less
// rows (bc-pnum ruling 1b). Every roster WRITE mints a UUID for an id-less row
// (marshalParticipantsCSV is the one chokepoint every persistence path
// funnels through); a row that still has none was loaded from a legacy
// participants.csv that predates that write and has simply never been
// re-saved since ids existed. Nothing is broken and no write is refused --
// the remedy is a re-save, not a repair -- so this is reported with its own
// "kind" rather than folded into the corrupt-file entries dataIssuesFrom
// builds, which the console renders with "a file could not be read" framing
// that would misdescribe this case.
//
// The message itself is composed by helper.MissingParticipantIDsMessage, the
// SAME function the draw pre-flight (helper.ValidateNoMissingParticipantIDs,
// called from internal/engine's runDrawPipeline, bc-pnum ruling 1c) uses to
// build its refusal: this is advance warning of the same condition the draw
// later hard-refuses, so the two surfaces must say the exact same thing
// about it. Returns nil when every row already has an id.
func missingParticipantIDsIssue(players []domain.Player) *gin.H {
	detail := helper.MissingParticipantIDsMessage(players)
	if detail == "" {
		return nil
	}
	return &gin.H{
		"kind":   "missing-ids",
		"file":   "participants.csv",
		"detail": detail,
	}
}

// viewerDataIssues is the ONE place that assembles a competition's
// dataIssues list: the corrupt-file errors among pmErr/brErr/poolsErr,
// folded together with the missing-participant-ids advisory. Both public
// viewer payload builders call it with the identical three-error shape --
// the aggregate (buildViewerCompetitionPayload, above) and the single-
// competition detail endpoint (GET /api/viewer/competitions/:id, below) --
// so a given competition's issues read the same on the dashboard list and
// on the competition overview, never present on one and silently dropped
// on the other (bc-pnum ruling 1e follow-up: the overview reads
// detail.config.dataIssues once the detail has loaded, which used to have
// no such field at all because the detail endpoint never computed one).
//
// Deliberately takes only pmErr/brErr/poolsErr, not every error a caller
// might have: the detail endpoint's own playersErr and standingsErr are
// NOT passed in, even though standingsErr can carry the identical
// underlying fault as poolsErr (engine.CalculatePoolStandings's own
// internal LoadPools reads the same pools.csv) -- reporting both would
// either double the entry or require a dedup rule this function would then
// own alone. Passing exactly the three-error shape keeps the two callers'
// output IDENTICAL by construction for the same on-disk state, which is
// the property this extraction exists for; a caller with an error source
// the other builder does not have is a caller that has drifted from the
// contract, not one that needs a wider signature.
func viewerDataIssues(players []domain.Player, pmErr, brErr, poolsErr error) []gin.H {
	issues := dataIssuesFrom(pmErr, brErr, poolsErr)
	if mi := missingParticipantIDsIssue(players); mi != nil {
		issues = append(issues, *mi)
	}
	return issues
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
			comps, err := buildViewerCompetitionPayloads(store, "")
			if err != nil {
				return nil, err
			}
			return json.Marshal(comps)
		})

		serveSingleFlightJSON(c, data, err)
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

				playersErr, poolsErr, poolMatchesErr, standingsErr, bracketErr error
			)

			var wg sync.WaitGroup
			var panicRef atomic.Pointer[recoveredPanic]
			safeGo(&wg, &panicRef, func() {
				p, e := store.LoadParticipantsOpt(id, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{
					WithSeeds: true,
					HasIDs:    comp.ParticipantIDsHint(),
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
			wg.Wait()

			if p := panicRef.Load(); p != nil {
				return nil, p
			}

			// bc-pnum ruling 1e follow-up: a corrupt-file error (pools.csv,
			// pool-matches.csv, bracket.json, participants.csv, or one
			// engine.CalculatePoolStandings' own reads surfaces, e.g.
			// overrides.json) DEGRADES rather than failing the whole
			// detail request -- the aggregate has never failed the whole
			// payload for this class of fault, and the detail endpoint
			// failing here was exactly why the corrupt-pools-csv case
			// this pin covers never reached the operator: the request
			// 500'd before dataIssues (below) ever got a chance to name
			// it. Any OTHER error (a genuine I/O fault the operator
			// cannot fix by editing a file) still aborts, unchanged from
			// before.
			for _, e := range []error{playersErr, poolsErr, poolMatchesErr, standingsErr, bracketErr} {
				if e == nil {
					continue
				}
				if _, ok := state.AsCorruptFile(e); ok {
					continue
				}
				return nil, e
			}

			// FR-025, T036: derive per-court queue position at serve time,
			// see annotateQueuePositions for rationale.
			annotateQueuePositions(poolMatches)
			annotateBracketQueuePositions(bracket)

			// mp-13y: merge assigned competitor Number from pools.csv onto
			// comp.Players so the numberPrefix-derived "K1", "K2", … surface
			// on the TV display, streaming overlay, and viewer card. A pools
			// read error degrades (above) rather than aborting, so `pools`
			// may be empty here; mergePoolNumbersIntoPlayers is a no-op over
			// an empty slice, matching the aggregate's own tolerance for the
			// same fault.
			mergePoolNumbersIntoPlayers(comp, pools)

			// Redact operator-only audit fields before this PUBLIC payload.
			stripMatchesAudit(poolMatches)
			stripBracketAudit(bracket)

			// viewerDataIssues (above) is the SAME function and the SAME
			// three-error shape buildViewerCompetitionPayload calls, so this
			// competition's dataIssues read identically here and on the
			// aggregate. Sibling of "config", exactly like the aggregate's
			// own payload shape; the SPA maps it onto detail.config.dataIssues
			// (api_client.jsx normalizeCompetitionDetail) since that is the
			// object AdminCompetitionOverview actually renders.
			payload := gin.H{
				"config":      comp,
				"pools":       pools,
				"poolMatches": poolMatches,
				"standings":   standings,
				"bracket":     bracket,
			}
			if issues := viewerDataIssues(comp.Players, poolMatchesErr, bracketErr, poolsErr); len(issues) > 0 {
				payload["dataIssues"] = issues
			}
			return json.Marshal(payload)
		})

		if errors.Is(err, errNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}
		serveSingleFlightJSON(c, data, err)
	})
}
