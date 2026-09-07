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
// caller of applyDrawNumbers' I/O wrappers must agree on: a nil comp or an
// empty prefix means "do nothing, no I/O, no number" (ok=false), same as a
// DrawNone competition (no draw yet, or Swiss -- see engine.DrawSourceFor).
// Otherwise ok is true and needsBracket says which file the draw actually
// needs: DrawInBracket's DrawOrder, or pools.csv for DrawInPools. Shared by
// numbersFromDraw and numbersFromDrawWithBracket below so the rule cannot
// drift between the two.
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

// numbersFromDraw is applyDrawNumbers' I/O-performing wrapper (renamed from
// numbersFromPools, bc-pnum ruling 2 successor to PR #416 finding 3): a
// caller that wants players' Number field filled from the draw's
// assignment, and has loaded NEITHER pools nor the bracket for its own
// purposes, calls this rather than hand-rolling the format switch, the
// prefix/no-draw-yet skip, and the read itself. currentMatchPlayers
// (handlers_display.go) is that caller.
//
// A thin front door: it runs the shared guard (numberingApplies), loads the
// bracket itself ONLY when the format actually needs one, and delegates
// everything else -- the pools.csv read for every other format, and the
// merge itself -- to numbersFromDrawWithBracket, so neither function has to
// restate the guard chain or the format switch on its own.
//
// Skips ALL reads (pools or bracket) when the competition has no draw yet:
// pools.csv/bracket.json cannot exist yet for a competition that has never
// drawn, so the read is a guaranteed-empty stat -- and, more importantly,
// any bytes found at that path for such a competition are noise (a stray
// fixture/leftover from another run), not an operator-actionable file, so
// they must never surface as a data issue or a log line.
//
// On a genuine read/parse error the merge is skipped (numbers are never
// composed from a partial/corrupt read) and the error is returned so a
// caller that maintains a dataIssues list can fold it in; other callers log
// it directly.
//
// A caller that has ALREADY attempted to load the bracket for its own
// purposes (buildViewerCompetitionPayload, below) must NOT call this: doing
// so would re-attempt the identical read on a failure and report the same
// corrupt-file error a second time, once under its own brErr and once under
// this function's return. numbersFromDrawWithBracket is that caller's own
// door instead.
func numbersFromDraw(store *state.Store, comp *state.Competition, players []domain.Player) error {
	needsBracket, ok := numberingApplies(comp)
	if !ok {
		return nil
	}
	var bracket *state.Bracket
	if needsBracket {
		var err error
		bracket, err = store.LoadBracket(comp.ID)
		if err != nil {
			return err
		}
	}
	return numbersFromDrawWithBracket(store, comp, players, bracket)
}

// numbersFromDrawWithBracket is numbersFromDraw's sibling for a caller that
// has ALREADY attempted to load the bracket for its own purposes:
// buildViewerCompetitionPayload loads it unconditionally for the court-feed
// match check before numbering is ever computed. bracket is that read's
// result verbatim -- nil exactly when the read failed, which the caller
// reports itself (its own brErr), so this never retries it: retrying would
// both cost a second bracket.json read and report the identical corrupt-file
// error a second time in the payload's dataIssues.
//
// Only the playoffs branch can skip an I/O read this way (bracket is the
// ONLY file that format ever needs for numbering); every other format still
// performs its own pools.csv read here, since the caller has not loaded
// one. Same guard as numbersFromDraw (numberingApplies), which is also this
// function's own direct caller's gate -- numbersFromDraw delegates here
// after running it once, so stating it again costs nothing wrong, only a
// second cheap boolean check, and this function still needs its own copy
// for the callers that reach it directly (buildViewerCompetitionPayload).
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
	issues := viewerDataIssues(players, pools, poolMatches, pmErr, brErr, poolsErr)
	if len(issues) > 0 {
		payload["dataIssues"] = issues
	}
	return payload
}

// dataIssuesFrom collects the located file failures among errs, dropping
// everything else: a missing file, a permissions problem or a nil is not
// something an operator repairs with a text editor, so it gets no banner.
// Each entry carries "kind": "corrupt-file" explicitly (PR #416 finding 9),
// alongside missingParticipantIDsIssue's own "missing-ids" kind below, so a
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

// poolsMissingParticipantIDsIssue is missingParticipantIDsIssue's twin for
// pools.csv (drawn pool membership): a member with no participant id gets
// no player number and is unresolvable by every id-only standings/scoring/
// eligibility consumer (operator ruling bc-pnum). See
// helper.PoolsMissingParticipantIDsMessage for the composed wording and the
// "regenerate the draw" remedy (a participants.csv re-save, unlike the
// sibling issue above, does not touch pools.csv at all).
func poolsMissingParticipantIDsIssue(pools []helper.Pool) *gin.H {
	detail := helper.PoolsMissingParticipantIDsMessage(pools)
	if detail == "" {
		return nil
	}
	return &gin.H{
		"kind":   "missing-ids",
		"file":   "pools.csv",
		"detail": detail,
	}
}

// poolMatchesMissingSideIDsIssue is missingParticipantIDsIssue's twin for
// pool-matches.csv: a row missing a side id, or recording a winner with no
// WinnerID, is not counted in standings (operator ruling bc-pnum). See
// engine.PoolMatchesMissingSideIDsMessage for the composed wording and the
// "re-enter the result" remedy.
func poolMatchesMissingSideIDsIssue(matches []state.MatchResult) *gin.H {
	detail := engine.PoolMatchesMissingSideIDsMessage(matches)
	if detail == "" {
		return nil
	}
	return &gin.H{
		"kind":   "missing-ids",
		"file":   "pool-matches.csv",
		"detail": detail,
	}
}

// viewerDataIssues is the ONE place that assembles a competition's
// dataIssues list: the corrupt-file errors among pmErr/brErr/poolsErr,
// folded together with the missing-ids advisories for all THREE on-disk
// records that carry an id field a side is ever resolved from
// (participants.csv, pools.csv, pool-matches.csv). Both public viewer
// payload builders call it with the identical shape -- the aggregate
// (buildViewerCompetitionPayload, above) and the single-competition detail
// endpoint (GET /api/viewer/competitions/:id, below) -- so a given
// competition's issues read the same on the dashboard list and on the
// competition overview, never present on one and silently dropped on the
// other (bc-pnum ruling 1e follow-up: the overview reads
// detail.config.dataIssues once the detail has loaded, which used to have
// no such field at all because the detail endpoint never computed one).
//
// pools and poolMatches are the RECORDS themselves (not just their load
// errors), since the pools.csv/pool-matches.csv notices need to inspect the
// rows for a missing id, not merely know whether the read succeeded; a nil
// or empty slice (competition not yet drawn, or a failed load already
// logged by the caller) simply reports no issue from that record.
//
// pools and poolsErr are NOT unconditionally whatever the caller happened to
// load: BOTH callers gate them on drawInPoolsFile(comp) (above), passing nil
// for both while no pools.csv draw exists (still draw-ready, or a knockout-
// only or Swiss competition). pools.csv cannot exist before a real draw and
// never exists for those formats, so any bytes found at that path in those
// states are leftovers (a discarded draw, a hand-placed file) rather than an
// operator-actionable pools.csv, and must surface neither a "no id in the
// pool draw" notice nor a corrupt-file entry. This function does not and
// cannot enforce that gate itself -- it only sees what it was handed -- so
// the two callers' output is identical only as long as both apply the one
// predicate; see buildViewerCompetitionPayload's read (above) and the
// GET /api/viewer/competitions/:id call site (below).
//
// Deliberately takes only pmErr/brErr/poolsErr for the CORRUPT-FILE half,
// not every error a caller might have: the detail endpoint's own playersErr
// and standingsErr are NOT passed in, even though standingsErr can carry the
// identical underlying fault as poolsErr (engine.CalculatePoolStandings's
// own internal LoadPools reads the same pools.csv) -- reporting both would
// either double the entry or require a dedup rule this function would then
// own alone. Passing exactly the three-error shape keeps the two callers'
// output IDENTICAL by construction for the same on-disk state, given the
// shared drawInPoolsFile gate on pools/poolsErr; a caller with an error
// source the other builder does not have is a caller that has drifted from
// the contract, not one that needs a wider signature.
func viewerDataIssues(players []domain.Player, pools []helper.Pool, poolMatches []state.MatchResult, pmErr, brErr, poolsErr error) []gin.H {
	issues := dataIssuesFrom(pmErr, brErr, poolsErr)
	if mi := missingParticipantIDsIssue(players); mi != nil {
		issues = append(issues, *mi)
	}
	if pi := poolsMissingParticipantIDsIssue(pools); pi != nil {
		issues = append(issues, *pi)
	}
	if mmi := poolMatchesMissingSideIDsIssue(poolMatches); mmi != nil {
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

			// mp-13y: merge assigned competitor Number from the draw onto
			// comp.Players so the numberPrefix-derived "K1", "K2", … surface
			// on the TV display, streaming overlay, and viewer card. `pools`
			// and `bracket` are already loaded above (both are also this
			// payload's own fields), so this calls the pure, no-I/O
			// applyDrawNumbers directly rather than numbersFromDraw
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
			// poolsForIssues / poolsErrForIssues mirror the aggregate's gate
			// (drawInPoolsFile, shared): `pools` itself is loaded unconditionally
			// a few lines up because the "pools" payload field and the number
			// merge need whatever is there, but the pools.csv notice and its
			// corrupt-file entry must not fire over LEFTOVER bytes from a state
			// in which no pools.csv draw exists (still draw-ready, a knockout-
			// only or Swiss competition). Without this, a stray or corrupt
			// pools.csv showed on the detail endpoint only, since the aggregate
			// never reads the file in those states, contradicting
			// viewerDataIssues' contract that the two callers agree.
			poolsForIssues, poolsErrForIssues := pools, poolsErr
			if !drawInPoolsFile(comp) {
				poolsForIssues, poolsErrForIssues = nil, nil
			}
			payload := gin.H{
				"config":      comp,
				"pools":       pools,
				"poolMatches": poolMatches,
				"standings":   standings,
				"bracket":     bracket,
			}
			if issues := viewerDataIssues(comp.Players, poolsForIssues, poolMatches, poolMatchesErr, bracketErr, poolsErrForIssues); len(issues) > 0 {
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
