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
// "K1") from pools.csv, in place. participants.csv never persists Number:
// the draw assigns it and persists it only in pools.csv, so every pooled
// payload that shows a number derives it here at read time (mp-13y). A
// knockout-only (playoffs) competition's number lives in the bracket's
// DrawOrder instead, merged by engine.NumberKnockoutParticipants -- see
// applyDrawNumbers below, this function's sibling for that format.
//
// Matched by id ONLY (bc-pnum ruling 2: identity is the participant id,
// never bare name or (name, dojo) -- the (name, dojo) fallback this
// function used to carry for legacy id-less rosters was removed under that
// same ruling). A pools.csv row with no id, or a roster row with no id,
// contributes no number; surfacing that gap as an operator-visible notice
// is a separate concern, not this function's job.
//
// players is explicit -- separate from comp.Players -- because the
// court-overlay caller (currentMatchPlayers) loads its OWN roster slice
// rather than mutating the competition's. No-op when comp is nil, its
// NumberPrefix is empty, or the roster/pools list is empty.
func mergePoolNumbersIntoPlayersSlice(comp *state.Competition, players []domain.Player, pools []helper.Pool) {
	if comp == nil || comp.EffectiveNumberPrefix() == "" || len(players) == 0 {
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
	for i := range players {
		if players[i].Number != "" {
			continue
		}
		if n, ok := byID[players[i].ID]; ok && n != "" {
			players[i].Number = n
		}
	}
}

// applyDrawNumbers is the pure, no-I/O core of "numbers come from the draw"
// (bc-pnum ruling 2): given comp and the caller's own already-loaded pools
// and/or bracket (either or both nil when the caller has neither, or when
// engine.DrawSourceFor(comp) says the competition does not use that file),
// it fills players' Number field exactly as the draw assigned it --
// pools.csv for DrawInPools (mergePoolNumbersIntoPlayersSlice, id-only),
// the bracket's DrawOrder for DrawInBracket
// (engine.NumberKnockoutParticipants). No-op for DrawNone (no draw yet, or
// Swiss): a knockout-only competitor shows NO number before the draw,
// exactly like a pooled one (bc-pnum operator ruling) -- there is
// deliberately no participant-order or name-based fallback of any kind, and
// Swiss never assigns one at all.
func applyDrawNumbers(comp *state.Competition, players []domain.Player, pools []helper.Pool, bracket *state.Bracket) {
	if comp == nil || comp.EffectiveNumberPrefix() == "" {
		return
	}
	switch engine.DrawSourceFor(comp) {
	case engine.DrawInBracket:
		var drawOrder []string
		if bracket != nil {
			drawOrder = bracket.DrawOrder
		}
		engine.NumberKnockoutParticipants(comp, drawOrder, players)
	case engine.DrawInPools:
		mergePoolNumbersIntoPlayersSlice(comp, players, pools)
	}
}

// drawInPoolsFile reports whether comp's draw lives in pools.csv right now.
// Both public viewer payload builders gate their pools.csv read AND its
// error on this one predicate, which is what keeps a corrupt or stray
// pools.csv reported identically on the dashboard list and on the
// competition page: bytes found at that path for any other DrawSource are
// leftovers, not an operator-actionable file.
func drawInPoolsFile(comp *state.Competition) bool {
	return engine.DrawSourceFor(comp) == engine.DrawInPools
}

// numberingApplies is the ONE place that states the guard chain every
// caller of applyDrawNumbers' I/O wrapper must agree on: a nil comp or an
// empty prefix means "do nothing, no I/O, no number" (ok=false), same as a
// DrawNone competition (no draw yet, or Swiss -- see engine.DrawSourceFor).
// Otherwise ok is true and needsBracket says which file the draw actually
// needs: DrawInBracket's DrawOrder, or pools.csv for DrawInPools.
func numberingApplies(comp *state.Competition) (needsBracket, ok bool) {
	if comp == nil || comp.EffectiveNumberPrefix() == "" {
		return false, false
	}
	switch engine.DrawSourceFor(comp) {
	case engine.DrawInBracket:
		return true, true
	case engine.DrawInPools:
		return false, true
	default:
		return false, false
	}
}

// numbersFromDrawWithBracket is applyDrawNumbers' I/O-performing wrapper
// (bc-pnum ruling 2 successor to PR #416 finding 3): a caller that wants
// players' Number field filled from the draw's assignment calls this rather
// than hand-rolling the format switch, the prefix/no-draw-yet skip, and the
// read itself.
//
// bracket is the caller's own already-loaded read when it has one (nil
// otherwise, meaning "load it here only if the format needs it"):
// buildViewerCompetitionPayload (below) loads it unconditionally for the
// court-feed match check before numbering is ever computed, and
// currentMatchPlayers (handlers_display.go) holds one on its bracket-branch
// calls but not its pool-branch call. Passing bracket verbatim -- nil
// exactly when the caller's own read failed, which the caller reports
// itself -- means this never retries a failed read: retrying would both
// cost a second bracket.json read and report the identical corrupt-file
// error a second time.
//
// Only the playoffs branch can skip an I/O read this way (bracket is the
// ONLY file that format ever needs for numbering); every other format still
// performs its own pools.csv read here. Skips ALL reads (pools or bracket)
// when the competition has no draw yet: pools.csv/bracket.json cannot exist
// yet for a competition that has never drawn, so the read is a
// guaranteed-empty stat -- and, more importantly, any bytes found at that
// path for such a competition are noise (a stray fixture/leftover from
// another run), not an operator-actionable file, so they must never surface
// as a data issue or a log line.
//
// On a genuine read/parse error the merge is skipped (numbers are never
// composed from a partial/corrupt read) and the error is returned so a
// caller that maintains a dataIssues list can fold it in; other callers log
// it directly.
func numbersFromDrawWithBracket(store *state.Store, comp *state.Competition, players []domain.Player, bracket *state.Bracket) error {
	needsBracket, ok := numberingApplies(comp)
	if !ok {
		return nil
	}
	if needsBracket {
		applyDrawNumbers(comp, players, nil, bracket)
		return nil
	}
	pools, err := store.LoadPools(comp.ID)
	if err != nil {
		return err
	}
	applyDrawNumbers(comp, players, pools, nil)
	return nil
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
	// One gated pools.csv read serves both the number merge and the pools.csv
	// notice below, and its error is the one poolsErr that joins dataIssues:
	// the SAME shape the detail endpoint reports, so a corrupt pools.csv shows
	// on the dashboard list and on the competition page alike. It used to
	// reach the list only when a number prefix was set, because the read
	// lived inside the number merge, which skips without one, while the
	// detail endpoint reads pools.csv for its own payload regardless.
	// drawInPoolsFile is the one predicate both builders gate on: pools.csv
	// cannot exist before a draw, and a knockout-only or Swiss competition
	// never has one, so in those states any bytes at that path are leftovers
	// (a discarded draw, a hand-placed file), not an operator-actionable
	// file, and are neither read nor reported.
	var (
		pools    []helper.Pool
		poolsErr error
	)
	if drawInPoolsFile(comp) {
		pools, poolsErr = store.LoadPools(compID)
		if poolsErr != nil {
			// Reported, not merged: an unreadable pools.csv must show as
			// MISSING numbers, never as composed ones (D1). bc-pnum C4: only a
			// PARSE failure joins the payload's dataIssues below
			// (dataIssuesFrom -> state.AsCorruptFile, which corruptCSV
			// populates from a csv.ParseError specifically). A raw READ error
			// (permissions, I/O) is not something an operator repairs with a
			// text editor, and its message names the absolute path on disk,
			// which must never reach this PUBLIC payload -- it is logged
			// server-side only, here.
			log.Printf("mobileapp: viewer payload %s: load pools: %v", compID, poolsErr)
		}
	}
	// mp-13y: merge the draw's numbers onto the roster through the one
	// no-I/O owner (applyDrawNumbers, bc-pnum ruling 2): pools.csv for a
	// pooled format, the bracket's DrawOrder for a standalone knockout.
	// bracket is already loaded above for the court-feed check, so a
	// playoffs-format competition's number never re-reads (and, on a corrupt
	// file, never re-reports) bracket.json.
	applyDrawNumbers(comp, players, pools, bracket)

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
	issues := viewerDataIssues(comp, players, pools, poolMatches, pmErr, brErr, poolsErr)
	if len(issues) > 0 {
		payload["dataIssues"] = issues
	}
	return payload
}

// dataIssuesFrom collects the located file failures among errs, dropping
// everything else: a missing file, a permissions problem or a nil is not
// something an operator repairs with a text editor, so it gets no banner.
// Each entry carries "kind": "corrupt-file" explicitly (PR #416 finding 9),
// alongside missingIDsIssue's own "missing-ids" kind below, so a
// consumer partitions the list by reading the field rather than by an
// absent-vs-present convention (the SPA still reads an absent kind as
// corrupt-file too, for an older payload's sake; see data_integrity.jsx).
func dataIssuesFrom(errs ...error) []gin.H {
	var issues []gin.H
	for _, err := range errs {
		cf, ok := state.AsCorruptFile(err)
		if !ok {
			continue
		}
		issues = append(issues, gin.H{
			"kind":   "corrupt-file",
			"file":   cf.File,
			"line":   cf.Line,
			"column": cf.Column,
			"detail": cf.Detail,
		})
	}
	return issues
}

// missingIDsIssue builds a "missing-ids" dataIssues entry naming file, or
// nil when detail is empty. Shared by all three on-disk records that carry
// an id field a side is ever resolved from -- participants.csv (bc-pnum
// ruling 1b: every roster WRITE mints a UUID for an id-less row via
// marshalParticipantsCSV, so a row that still has none was loaded from a
// legacy file that predates that write and has simply never been re-saved),
// pools.csv (drawn pool membership: a member with no id gets no player
// number and is unresolvable by every id-only standings/scoring/eligibility
// consumer), and pool-matches.csv (a row missing a side id, or recording a
// winner with no WinnerID, is not counted in standings) -- all operator
// ruling bc-pnum. Nothing is broken and no write is refused in any of the
// three cases. participants.csv's remedy stays a re-save (marshalParticipantsCSV
// mints ids only on write); pools.csv and pool-matches.csv are instead
// repaired automatically at load time when their rows resolve unambiguously
// against the roster (state.upgradePoolParticipantIDsLocked,
// state.upgradePoolMatchSideIDsLocked), so this issue now only ever
// surfaces for the residue that repair could not resolve, whose remedy is a
// regenerated draw or a re-entered result. This carries its own "kind"
// rather than folding into the corrupt-file entries dataIssuesFrom builds,
// which the console renders with "a file could not be read" framing that
// would misdescribe all three.
//
// The detail message itself is composed by the caller, via
// helper.MissingParticipantIDsMessage (the SAME function the draw
// pre-flight, helper.ValidateNoMissingParticipantIDs, uses to build its
// refusal: this is advance warning of the same condition the draw later
// hard-refuses), helper.PoolsMissingParticipantIDsMessage, or
// engine.PoolMatchesMissingSideIDsMessage.
func missingIDsIssue(file, detail string) *gin.H {
	if detail == "" {
		return nil
	}
	return &gin.H{
		"kind":   "missing-ids",
		"file":   file,
		"detail": detail,
	}
}

// viewerDataIssues is the ONE place that assembles a competition's
// dataIssues list, so the aggregate (buildViewerCompetitionPayload) and the
// single-competition detail endpoint (GET /api/viewer/competitions/:id)
// always report the same issues for the same on-disk state (bc-pnum ruling
// 1e follow-up).
//
// It applies the drawInPoolsFile(comp) gate to pools/poolsErr itself:
// pools.csv cannot exist before a real draw and never exists for a
// knockout-only or Swiss competition, so any bytes found at that path in
// those states are leftovers (a discarded draw, a hand-placed file), not an
// operator-actionable pools.csv, and must surface neither a missing-ids
// notice nor a corrupt-file entry. A caller may still gate its OWN read of
// pools.csv on the same predicate as an I/O saving (buildViewerCompetitionPayload
// does); that is redundant with, not a substitute for, the gate here.
//
// pools and poolMatches are the RECORDS themselves (not just their load
// errors), since the pools.csv/pool-matches.csv notices need to inspect the
// rows for a missing id; a nil or empty slice reports no issue from that
// record. Deliberately takes only pmErr/brErr/poolsErr for the corrupt-file
// half: the detail endpoint's own playersErr and standingsErr are not
// passed in, even though standingsErr can carry the identical underlying
// fault as poolsErr (engine.CalculatePoolStandings's own internal LoadPools
// reads the same pools.csv) -- reporting both would either double the entry
// or require a dedup rule this function would then own alone.
func viewerDataIssues(comp *state.Competition, players []domain.Player, pools []helper.Pool, poolMatches []state.MatchResult, pmErr, brErr, poolsErr error) []gin.H {
	if !drawInPoolsFile(comp) {
		pools, poolsErr = nil, nil
	}
	issues := dataIssuesFrom(pmErr, brErr, poolsErr)
	if mi := missingIDsIssue("participants.csv", helper.MissingParticipantIDsMessage(players)); mi != nil {
		issues = append(issues, *mi)
	}
	if pi := missingIDsIssue("pools.csv", helper.PoolsMissingParticipantIDsMessage(pools)); pi != nil {
		issues = append(issues, *pi)
	}
	if mmi := missingIDsIssue("pool-matches.csv", engine.PoolMatchesMissingSideIDsMessage(poolMatches)); mmi != nil {
		issues = append(issues, *mmi)
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

			// bc-pnum: "make a team member's label available to the public
			// surfaces". Gated on the same team-competition discriminator the
			// rest of the codebase uses (Kind == "team" || TeamSize > 0, e.g.
			// Competition.IsKachinuki's own condition and the JS twin in
			// admin_schedule_lineup.jsx) so an individual competition never
			// attempts a squads.yaml read at all: this is a hot path (every
			// viewer/TV/streaming-overlay poll), and a file that can only
			// ever be empty for this shape of competition is not worth a
			// stat, let alone a read+parse.
			isTeamComp := comp.Kind == "team" || comp.TeamSize > 0

			// Run all independent I/O concurrently.
			var (
				pools       []helper.Pool
				poolMatches []state.MatchResult
				standings   any
				bracket     *state.Bracket
				squads      map[string][]domain.TeamMember

				playersErr, poolsErr, poolMatchesErr, standingsErr, bracketErr, squadsErr error
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
			if isTeamComp {
				safeGo(&wg, &panicRef, func() {
					squads, squadsErr = store.LoadSquads(id)
				})
			}
			wg.Wait()

			if p := panicRef.Load(); p != nil {
				return nil, p
			}

			// squadsErr is NOT folded into the strict degradedReads loop below
			// (state.AsCorruptFile-gated, everything else aborts the request):
			// unlike pools/poolMatches/bracket/standings, a squad label is
			// pure ENRICHMENT of an already-complete match row (it decorates a
			// fighter's name with "T10.1"; it drives no scoring, standings, or
			// bracket advancement), so a broken squads.yaml must never be able
			// to take the whole competition page down with it -- the same
			// "one bad cell cannot stop a tournament" principle this file's
			// own read-fault handling already applies elsewhere degrades
			// unconditionally here rather than escalating on an unwrapped
			// YAML parse error (state/squad.go does not wrap its yaml.Unmarshal
			// failures as *state.CorruptFileError the way the JSON/CSV readers
			// do, so treating it like the strict loop below would 500 the
			// whole page over a typo in one YAML file). A MISSING squads.yaml
			// is not an error at all (state.LoadSquads' own contract, mirroring
			// LoadTeamLineups): squadsErr is only ever non-nil for a genuine
			// read/parse fault, which is logged and then ignored.
			if squadsErr != nil {
				log.Printf("mobileapp: viewer payload %s: load squads: %v", id, squadsErr)
				squads = nil
			}

			// bc-pnum ruling 1e follow-up: a corrupt-file error (pools.csv,
			// pool-matches.csv, bracket.json, or one
			// engine.CalculatePoolStandings' own reads surfaces, e.g.
			// overrides.json) DEGRADES rather than failing the whole
			// detail request -- the aggregate has never failed the whole
			// payload for this class of fault, and the detail endpoint
			// failing here was exactly why the corrupt-pools-csv case
			// this pin covers never reached the operator: the request
			// 500'd before dataIssues (below) ever got a chance to name
			// it. participants.csv is deliberately NOT in this class: its
			// loader (state.LoadParticipantsOpt) returns either the raw
			// helper.ReadCSVFile error or a plain fmt.Errorf from
			// helper.CreatePlayersFromRecords (duplicate-entry, bad-format),
			// neither of which is ever a *state.CorruptFileError, so
			// state.AsCorruptFile(playersErr) is always false and a
			// participants.csv fault still aborts the request below
			// (already logged, via internalError, on that path) -- unchanged
			// from before this comment. A future change that starts wrapping
			// that loader with corruptCSV would be a deliberate decision, not
			// something this comment already sanctions. Any OTHER error (a
			// genuine I/O fault the operator cannot fix by editing a file)
			// still aborts, unchanged from before.
			//
			// Every degraded error is logged below, inside the continue branch,
			// naming the competition and which read hit it -- trading a loud
			// failure for a silent one was never the intent, only trading a 500
			// for a 200 that still leaves a server-side breadcrumb.
			//
			// standingsErr is deliberately the ONE of the five never folded into
			// viewerDataIssues (compare the call a few lines down, which passes
			// poolMatchesErr and bracketErr but not standingsErr): this endpoint
			// is the only one that ever computes standings at all -- the
			// aggregate (buildViewerCompetitionPayload) never calls
			// engine.CalculatePoolStandings -- so feeding standingsErr into the
			// SHARED viewerDataIssues builder would make the two surfaces report
			// different issues for the identical competition, which
			// viewerDataIssues' own doc comment (above) says they must never do.
			// The fault standingsErr actually carries here (a corrupt
			// overrides.json, via CalculatePoolStandings' own internal
			// LoadOverrides) already has a loud, actionable operator channel of
			// its own: EVERY write path answers 422 naming the file with a
			// repair instruction (respondIfCorruptOverrides, errors.go),
			// including the DELETE .../overrides repair door. This read surface
			// degrading, plus the log line below, is the deliberate answer for a
			// GET -- not an oversight.
			degradedReads := []struct {
				name string
				err  error
			}{
				{"participants", playersErr},
				{"pools", poolsErr},
				{"pool matches", poolMatchesErr},
				{"standings", standingsErr},
				{"bracket", bracketErr},
			}
			for _, dr := range degradedReads {
				if dr.err == nil {
					continue
				}
				if _, ok := state.AsCorruptFile(dr.err); ok {
					log.Printf("mobileapp: viewer payload %s: load %s: %v", id, dr.name, dr.err)
					continue
				}
				return nil, dr.err
			}

			// FR-025, T036: derive per-court queue position at serve time,
			// see annotateQueuePositions for rationale.
			annotateQueuePositions(poolMatches)
			annotateBracketQueuePositions(bracket)

			// mp-13y: merge assigned competitor Number from the draw onto
			// comp.Players so the numberPrefix-derived "K1", "K2", … surface
			// on the TV display, streaming overlay, and viewer card. `pools`
			// and `bracket` are already loaded above (both are also this
			// payload's own fields), so this calls the pure, no-I/O
			// applyDrawNumbers directly rather than numbersFromDrawWithBracket
			// (bc-pnum ruling 2 successor to PR #416 finding 3), which would
			// re-read pools.csv/bracket.json a second time. A read error
			// degrades (above) rather than aborting, so `pools`/`bracket` may
			// be empty/nil here; applyDrawNumbers is a no-op over either,
			// matching the aggregate's own tolerance for the same fault.
			applyDrawNumbers(comp, comp.Players, pools, bracket)

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
			//
			// `pools`/`poolsErr` are passed RAW: viewerDataIssues applies the
			// drawInPoolsFile gate itself now, so this endpoint (which, unlike
			// the aggregate, always loads pools.csv unconditionally a few lines
			// up for the "pools" payload field and the number merge) does not
			// need its own gated copy just to keep the two callers' dataIssues
			// output agreeing.
			payload := gin.H{
				"config":      comp,
				"pools":       pools,
				"poolMatches": poolMatches,
				"standings":   standings,
				"bracket":     bracket,
			}
			// "squads" is present only for a team competition (isTeamComp,
			// above) and omitted entirely for an individual one, rather than
			// carrying an always-empty {} -- a client can treat the key's
			// absence as "this competition has no squads to resolve" without
			// inspecting comp.kind/comp.teamSize itself. Keyed by the TEAM's
			// participant id, matching the admin endpoint's shape
			// (GET /api/competitions/:id/squads, handlers_squad.go) exactly,
			// so a client resolves a bout row's sideAMemberId/sideBMemberId
			// (state.SubMatchResult) to {id, index, name} without a second,
			// admin-gated call this public surface could never make anyway.
			if isTeamComp {
				payload["squads"] = squads
			}
			if issues := viewerDataIssues(comp, comp.Players, pools, poolMatches, poolMatchesErr, bracketErr, poolsErr); len(issues) > 0 {
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
