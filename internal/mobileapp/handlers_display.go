package mobileapp

import (
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RegisterDisplayHandlers wires the public, no-auth court-scoped surfaces:
//   - GET /api/viewer/court/:court/current, a one-shot polled view of the
//     currently-running match on a court, for non-browser streaming
//     integrations (vMix, OBS plugins, scoreboards).
//   - GET /api/viewer/court/:court/matches, the operator console's court feed:
//     every competition with a real match physically placed on the court, each
//     with its full {config, poolMatches, bracket} payload.
//
// Browser clients otherwise use SSE (/api/events); these polled surfaces exist
// for clients that cannot subscribe (and to right-size the operator console).
//
// Per NFR-002 the handler depends on the *state.Store concrete type (the
// same boundary handlers_viewer.go uses); there is no snug-fit interface
// in deps.go that covers the OTHER read methods we need here, and inventing
// one for a single consumer would be premature. If a second polled surface
// lands later we can hoist a DisplayStore interface then. Number merging
// (currentMatchPlayers) reaches engine.NumberKnockoutParticipants through
// numbersFromDrawWithBracket (handlers_viewer.go, same package) exactly as
// the viewer payload does (that shared helper calls the engine function
// directly -- a plain package-level function, not threaded through as a
// parameter -- so this file needs no engine reference of its own), the SAME
// derivation the blank-template export uses, so this surface's numbers
// cannot silently disagree with either of those.
func RegisterDisplayHandlers(r *gin.RouterGroup, store *state.Store) {
	// P2 (mp-9afd style): singleflight group for the court-scoped match feed,
	// mirroring the sf in RegisterViewerHandlers for GET /competitions.
	// Unlike the constant "competitions" key, the key here includes the
	// court, different courts are different payloads and must not collapse
	// together.
	sf := newViewerSingleFlight()

	r.GET("/court/:court/current", func(c *gin.Context) {
		// Streaming clients poll this on a 1-2s cadence; resolveCourt pins the
		// no-store + CORS headers so the polled-surface guarantee survives
		// router refactors, normalises the: court param, and writes the 503/404.
		court, ok := resolveCourt(c, store)
		if !ok {
			return
		}

		// Scan competitions in listing order; the first running match on
		// this court wins. Two running matches on the same court would
		// already be a tournament-data error (one court runs one match
		// at a time per R8) and we surface whichever appears first. Within a
		// competition we check pool matches then the bracket, the same order
		// state.RunningMatchOnCourt uses, so a running KNOCKOUT bout is just
		// as "current" as a pool bout (mp-9h1f follow-up: the prior code
		// scanned only poolMatches, so a running elimination match read as idle).
		//
		// A failing competition list is a real 500, not an idle court: the
		// overlay must be able to distinguish "nothing running" from "backend
		// broken" (same contract as the sibling /matches feed). Per-competition
		// read faults below follow the same soft-degrade contract as
		// buildViewerCompetitionPayload (skip the broken comp, log the cause)
		// and use the same log.Printf mechanism so the operator greps one
		// format for every soft-degrade breadcrumb.
		ids, err := store.ListCompetitions()
		if err != nil {
			internalError(c, err)
			return
		}
		for _, compID := range ids {
			comp, err := store.LoadCompetition(compID)
			if err != nil {
				log.Printf("mobileapp: court current %s: load competition: %v", compID, err)
			}
			if comp == nil {
				continue
			}

			poolMatches, err := store.LoadPoolMatches(compID)
			if err != nil {
				log.Printf("mobileapp: court current %s: load pool matches: %v", compID, err)
			}
			for _, m := range poolMatches {
				if !strings.EqualFold(m.Court, court) || m.Status != state.MatchStatusRunning {
					continue
				}
				// No bracket loaded yet on this branch (it is loaded further
				// below, only once the pool scan comes up empty), so numbering
				// falls back to its own pools.csv read for a pooled competition.
				players := currentMatchPlayers(store, comp, nil)
				zek := comp.EffectiveWithZekkenName()
				// Pool daihyosen/tiebreaker rep bouts carry team names in
				// sideA/sideB; the representative fighter for each side lives in
				// RepPlayerA/B. Empty for every regular match (mp-62vr).
				//
				// A pool MatchResult always carries a SideAID/SideBID field
				// (even a legacy row where it is empty), so this is the id
				// class: buildSideByID, never buildSideByIDOrName -- a name
				// fallback here could silently resolve to the wrong same-name
				// competitor across dojos.
				c.JSON(http.StatusOK, currentMatchPayload(court, comp,
					buildSideByID(m.SideA, m.SideAID, players, zek),
					buildSideByID(m.SideB, m.SideBID, players, zek),
					m.IpponsA, m.IpponsB, m.HansokuA, m.HansokuB,
					phaseFromMatchID(m.ID), m.RepPlayerA, m.RepPlayerB))
				return
			}

			// Bracket/knockout: a running elimination bout carries the ippon
			// arrays natively (BracketMatch.IpponsA/B/HansokuA/B), the same
			// shape a pool MatchResult carries, so no decode is needed.
			// Elimination matches have no representative-bout fighters.
			bracket, err := store.LoadBracket(compID)
			if err != nil {
				log.Printf("mobileapp: court current %s: load bracket: %v", compID, err)
			}
			if bracket == nil {
				continue
			}
			for _, round := range bracket.Rounds {
				for _, bm := range round {
					if !strings.EqualFold(bm.Court, court) || bm.Status != state.MatchStatusRunning {
						continue
					}
					// bracket is already loaded (below the pool scan above), so
					// pass it through rather than letting numbersFromDrawWithBracket
					// read bracket.json a second time.
					players := currentMatchPlayers(store, comp, bracket)
					zek := comp.EffectiveWithZekkenName()
					// bc-brid: BracketMatch carries a per-side id when its own
					// row is stamped; buildSideByIDOrName prefers it and falls
					// back to the pre-bc-brid name match for a bye, an
					// unresolved feeder, or an unrepaired legacy row.
					c.JSON(http.StatusOK, currentMatchPayload(court, comp,
						buildSideByIDOrName(bm.SideA, bm.SideAID, players, zek),
						buildSideByIDOrName(bm.SideB, bm.SideBID, players, zek),
						bm.IpponsA, bm.IpponsB, bm.HansokuA, bm.HansokuB,
						phaseFromMatchID(bm.ID), "", ""))
					return
				}
			}
			// ThirdPlaceMatch (single-3rd bronze, bc-3rdp) is a sibling of Rounds, not
			// inside it. A running bronze bout must appear as "current" on its
			// court so the OBS/vMix overlay stays live (Finding 1).
			if bm := bracket.ThirdPlaceMatch; bm != nil &&
				strings.EqualFold(bm.Court, court) &&
				bm.Status == state.MatchStatusRunning {
				// bracket is already loaded (above, this branch's own bm comes
				// from it), so pass it through rather than a second read.
				players := currentMatchPlayers(store, comp, bracket)
				zek := comp.EffectiveWithZekkenName()
				ipponsA, hansokuA := bm.IpponsA, bm.HansokuA
				ipponsB, hansokuB := bm.IpponsB, bm.HansokuB
				// phaseFromMatchID(bm.ID) would return "m" for the bronze
				// match's single-hyphen "m-bronze" ID: not meaningful. This
				// branch already knows it's the bronze match, so pass a
				// stable literal label instead of deriving one from the ID
				// (matches the "3rd Place Match" label kachinuki_export.go
				// uses for the same match).
				// ThirdPlaceMatch is a BracketMatch too, so the same
				// id-preferring, name-falling-back resolution applies
				// (buildSideByIDOrName, matching the round branch above).
				c.JSON(http.StatusOK, currentMatchPayload(court, comp,
					buildSideByIDOrName(bm.SideA, bm.SideAID, players, zek),
					buildSideByIDOrName(bm.SideB, bm.SideBID, players, zek),
					ipponsA, ipponsB, hansokuA, hansokuB,
					"3rd Place Match", "", ""))
				return
			}
		}

		// No running match on this court.
		c.JSON(http.StatusOK, gin.H{"court": court, "status": "idle"})
	})

	// GET /api/viewer/court/:court/matches, the court-scoped match feed for
	// the operator console. Returns {court, competitions:[…]} where each entry
	// is the SAME public per-competition payload the aggregate GET /competitions
	// returns ({config, poolMatches, bracket}), but ONLY for competitions that
	// have at least one real (both-sides-resolved) match; pool or non-preview
	// bracket, physically placed on this court right now.
	//
	// The operator console is court-first AND cross-competition by design: the
	// running bout stays put across comps (AC7), the switch nudge watches other
	// comps on the court (AC6), and Submit+Next advances within the submitted
	// comp; all need every competition on the court, not just the selected one.
	// Keying on actual match placement (not comp.courts config) keeps the page
	// correct after an operator MOVES a match to another shiaijo. The per-comp
	// match data is full (not court-filtered) so client-side derivations like
	// "Match N of M" pool counts stay correct. The right-sizing is by
	// competition COUNT (only comps on this court, not the whole tournament).
	r.GET("/court/:court/matches", func(c *gin.Context) {
		// resolveCourt is a per-request concern (validates + normalizes the
		// :court param against the current tournament) and must run outside
		// sf.Do, only the expensive payload build is collapsed.
		court, ok := resolveCourt(c, store)
		if !ok {
			return
		}

		// P2 (mp-9afd style): collapse concurrent builds for the SAME court to
		// O(1) per in-flight window (court-scoped key: see the sf declaration
		// comment). On panic inside the elected build, sf.Do returns an error
		// and all waiters receive it; mapped to 500.
		data, err := sf.Do("court-matches:"+court, func() ([]byte, error) {
			// Propagate a failing competition list so serveSingleFlightJSON
			// maps it to a 500 (matching GET /competitions): swallowing it
			// here would serve an empty-but-200 board to every collapsed
			// waiter while the backend is actually broken.
			// Shared concurrent builder (same as GET /competitions); the
			// court filter does the setup-skip and "real match on this
			// court" gating inside the single per-comp load (no separate
			// presence pre-check that would re-read poolMatches/bracket).
			// comps is non-nil even when empty, so zero competitions
			// marshals as [] not null (the wire shape the SPA and the
			// regression test pin).
			comps, err := buildViewerCompetitionPayloads(store, court)
			if err != nil {
				return nil, err
			}
			return json.Marshal(gin.H{"court": court, "competitions": comps})
		})

		serveSingleFlightJSON(c, data, err)
	})
}

// bracketPlaceholderRE / poolOriginPlaceholderRE use the same patterns as the
// BRACKET_PLACEHOLDER_RE / POOL_ORIGIN_PLACEHOLDER_RE constants in
// web-mobile/js/admin_helpers.jsx (the Go and JS identifiers differ in casing).
// A side matching either is an unresolved placeholder ("Winner of r2-m1",
// "Pool A-1st"), not a real fighter.
var (
	bracketPlaceholderRE    = regexp.MustCompile(`^Winner of r\d+-m\d+$`)
	poolOriginPlaceholderRE = regexp.MustCompile(`^Pool .+-\d+(st|nd|rd|th)$`)
)

// courtMatchSidesReal mirrors hasBothSides (web-mobile/js/admin_helpers.jsx):
// a match counts only when both sides are present AND neither is a bracket
// "Winner of…" or pool-origin "Pool A-1st" placeholder, so the
// court→competitions index lists exactly the competitions the operator selector
// derives from the aggregate today. One intentional difference from the JS
// helper: this Go side additionally TrimSpace-normalizes each name before the
// empty/placeholder checks. That only widens the empty-side guard (a
// whitespace-only side reads as absent) and never changes which real names
// pass, so the two predicates agree on every real match; they are not
// byte-for-byte identical implementations.
func courtMatchSidesReal(a, b string) bool {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" || b == "" {
		return false
	}
	if bracketPlaceholderRE.MatchString(a) || bracketPlaceholderRE.MatchString(b) {
		return false
	}
	if poolOriginPlaceholderRE.MatchString(a) || poolOriginPlaceholderRE.MatchString(b) {
		return false
	}
	return true
}

// resolveCourt is the shared preamble for the court-scoped display surfaces. It
// pins the no-store + CORS headers (streaming clients poll on a 1-2s cadence and
// must never be cached), normalises the: court param, validates it against the
// active tournament, and writes the error response on failure. Returns
// (court, true) on success; ("", false) after writing a 503 (no tournament) or
// 404 (unknown court), in which case the handler must return immediately.
func resolveCourt(c *gin.Context, store *state.Store) (string, bool) {
	c.Header("Cache-Control", "no-store")
	c.Header("Access-Control-Allow-Origin", "*")

	court := strings.ToUpper(strings.TrimSpace(c.Param("court")))

	tour, err := store.LoadTournament()
	if err != nil || tour == nil {
		// 503 distinguishes "tournament not loaded yet" from a genuine 4xx,
		// clients can retry without reporting it as an error to the operator.
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no_active_tournament"})
		return "", false
	}

	for _, ct := range tour.Courts {
		if strings.EqualFold(ct, court) {
			return court, true
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "court_not_found", "court": court})
	return "", false
}

// matchesPresentOnCourt reports whether the given (already-loaded) pool matches
// or non-preview bracket contain at least one real match (both sides resolved,
// see courtMatchSidesReal) physically placed on the court. A preview bracket
// (mixed-comp placeholder structure) is skipped, it is read-only and never
// played, mirroring the aggregate viewer's preview strip. Pure (no store reads)
// so buildViewerCompetitionPayload can gate the court feed off the same
// poolMatches/bracket it already loaded, without a second read.
func matchesPresentOnCourt(poolMatches []state.MatchResult, bracket *state.Bracket, court string) bool {
	for _, m := range poolMatches {
		if strings.EqualFold(m.Court, court) && courtMatchSidesReal(m.SideA, m.SideB) {
			return true
		}
	}
	if bracket != nil && !bracket.Preview {
		for _, round := range bracket.Rounds {
			for _, bm := range round {
				if strings.EqualFold(bm.Court, court) && courtMatchSidesReal(bm.SideA, bm.SideB) {
					return true
				}
			}
		}
		// ThirdPlaceMatch (single-3rd bronze, bc-3rdp) is a sibling of Rounds; a
		// bronze-only court must not be excluded from the court feed (Finding 4).
		if bm := bracket.ThirdPlaceMatch; bm != nil &&
			strings.EqualFold(bm.Court, court) &&
			courtMatchSidesReal(bm.SideA, bm.SideB) {
			return true
		}
	}
	return false
}

// currentMatchPlayers loads the participant slice used to enrich a court's
// current-match payload (DisplayName/Dojo/number). LoadParticipantsOpt is the
// canonical read so we pick up DisplayName/Dojo even on legacy competitions
// that predate the HasParticipantIDs flag. mp-13y: when a numberPrefix is
// configured, merge the draw-derived numbers onto the slice so buildSide
// can include "number" in the polled OBS/vMix overlay payload.
//
// bracket is the caller's own already-loaded read when it has one (nil
// otherwise): the pool branch above has not loaded one at the point it
// calls this, but both bracket-branch callers (the round scan and the
// ThirdPlaceMatch check) already hold it, so passing it through here lets
// numbersFromDrawWithBracket skip a second bracket.json read for a
// playoffs-format competition.
func currentMatchPlayers(store *state.Store, comp *state.Competition, bracket *state.Bracket) []domain.Player {
	// the load error used to be discarded outright (`players, _ :=...`)
	// while the pools load just below already logs its own. Logged, not
	// returned/surfaced: the overlay must not vanish over an unreadable
	// participants.csv, it degrades to whatever LoadParticipantsOpt still
	// managed to return (possibly nil/empty), same contract as the pools
	// branch below.
	players, plErr := store.LoadParticipantsOpt(comp.ID, comp.EffectiveWithZekkenName(), state.LoadParticipantsOpts{WithSeeds: false, HasIDs: comp.ParticipantIDsHint()})
	if plErr != nil {
		log.Printf("mobileapp: court current %s: load participants: %v", comp.ID, plErr)
	}
	// numbersFromDrawWithBracket (bc-pnum ruling 2 successor to PR #416
	// finding 3) owns the prefix/no-draw-yet skip and the format-specific
	// read (pools.csv when needed; bracket is threaded through instead of
	// re-read). An unreadable file is reported, not merged, so the overlay
	// shows MISSING numbers, never composed ones (D1). The log line says
	// "load draw", not "load pools": for a playoffs-format competition the
	// file behind this error is bracket.json, not pools.csv.
	if err := numbersFromDrawWithBracket(store, comp, players, bracket); err != nil {
		log.Printf("mobileapp: court current %s: load draw: %v", comp.ID, err)
	}
	return players
}

// currentMatchPayload builds the GET /court/:court/current "current" body
// shared by the pool and bracket branches. repA/repB are the
// representative-bout fighters for a pool daihyosen (empty for regular and
// bracket matches). sideA/sideB are pre-built by the caller via
// buildSideByID (pool: id-only, no name fallback -- operator ruling
// bc-pnum) or buildSideByIDOrName (bracket/ThirdPlaceMatch: prefers the id
// but falls back to name for a bye, an unresolved feeder, or an unrepaired
// legacy row) -- the CALLER picks the resolver, because only it knows
// which record class it read the match from; this function must not
// re-derive that choice from whether an id string happens to be empty.
func currentMatchPayload(court string, comp *state.Competition, sideA, sideB gin.H,
	ipponsA, ipponsB []string, hansokuA, hansokuB int,
	phase, repA, repB string) gin.H {
	return gin.H{
		"court":  court,
		"status": "current",
		"competition": gin.H{
			"id":   comp.ID,
			"name": comp.Name,
		},
		"phase": phase,
		"sideA": sideA,
		"sideB": sideB,
		// Normalize nil → [] so the JSON encodes empty arrays, not null: the
		// contract models ipponsA/ipponsB as arrays and overlay clients assume
		// []. A nil slice reaches here from an unscored pool match or a
		// bracket match with no ippons yet (BracketMatch.IpponsA/IpponsB stay
		// nil until scored).
		"ipponsA":    emptyIfNil(ipponsA),
		"ipponsB":    emptyIfNil(ipponsB),
		"hansokuA":   hansokuA,
		"hansokuB":   hansokuB,
		"repPlayerA": repA,
		"repPlayerB": repB,
	}
}

// emptyIfNil returns a non-nil slice so JSON encodes [] rather than null.
// Applied at the response boundary (currentMatchPayload) so the pool/bracket
// read paths can keep returning nil for "nothing scored yet" while the wire
// stays array-typed.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// findPlayerByID returns the roster player whose ID matches id, or nil when
// id is empty or no player matches. Empty is refused outright rather than
// matching an equally-empty Player.ID field, which would resolve a
// never-stamped side to an arbitrary equally-id-less roster row.
func findPlayerByID(players []domain.Player, id string) *domain.Player {
	if id == "" {
		return nil
	}
	for i := range players {
		if players[i].ID == id {
			return &players[i]
		}
	}
	return nil
}

// findPlayerByName returns the roster player whose Name matches name, or
// nil when no player matches.
func findPlayerByName(players []domain.Player, name string) *domain.Player {
	for i := range players {
		if players[i].Name == name {
			return &players[i]
		}
	}
	return nil
}

// sidePayload builds the per-side payload the court-current contract
// defines, from name (always present, even when p is nil) and the matched
// roster player p, or nil when unresolved -- dojo/playerId/number stay
// blank in that case, rather than fabricating a value.
func sidePayload(name string, p *domain.Player, withZekkenName bool) gin.H {
	displayName := name
	dojo := ""
	playerID := ""
	number := ""
	if p != nil {
		if withZekkenName && p.DisplayName != "" {
			displayName = p.DisplayName
		}
		dojo = p.Dojo
		playerID = p.ID
		number = p.Number
	}
	return gin.H{
		"playerId":    playerID,
		"name":        name,
		"displayName": displayName,
		"dojo":        dojo,
		"number":      number,
	}
}

// buildSideByID resolves a match side BY ID ONLY (operator ruling bc-pnum).
// id is the side's participant id from a record that carries a per-side id
// FIELD -- a POOL match's SideAID/SideBID (state.MatchResult has that
// field, even when its value happens to be empty, e.g. a legacy row
// stamped before SideAID/SideBID existed).
//
// An empty id -- foreign, stale, or simply never stamped -- resolves to NO
// roster player (findPlayerByID's own guard) rather than falling back to a
// name match, which could silently pick a different competitor sharing the
// same display name across dojos (bc-pnum review finding 2; the old single
// buildSide fell back to a name scan whenever id == "", which a legacy
// id-less POOL row could reach just as easily as a genuinely id-less
// record). Only buildSideByIDOrName may fall back to a name match -- see
// its own doc comment for which record class (the bracket) that is, and why.
func buildSideByID(name, id string, players []domain.Player, withZekkenName bool) gin.H {
	return sidePayload(name, findPlayerByID(players, id), withZekkenName)
}

// buildSideByIDOrName is buildSideByID's bracket counterpart (bc-brid).
// BracketMatch now carries a per-side id when its own row is stamped (see
// BracketMatch.SideAID's own doc comment for the writers that stamp it and
// the three shapes that carry none: a bye, an unresolved "Winner of ..."
// feeder, or an unrepaired legacy row), so this prefers the id exactly like
// buildSideByID, but falls back to the pre-bc-brid name match
// (findPlayerByName) when the id is empty or does not resolve to a roster
// player -- "this ADDS ids, it does not remove name handling" (bc-brid's own
// instruction for every identity-critical reader). When the roster cannot
// resolve either, this still returns a name-only side so the overlay can
// render "Player vs Player" rather than blanking out.
//
// Deliberately NOT folded into buildSideByID itself: a pool MatchResult's
// id-less residue (a row an earlier repair could not resolve) has ALWAYS
// rendered unresolved via buildSideByID, with NO name fallback -- pool
// sides are resolved BY ID ONLY (operator ruling bc-pnum), precisely to
// avoid the same same-name misattribution this function's own fallback
// re-admits for the bracket's residue. Widening buildSideByID itself would
// regress that deliberate pool ruling; keeping this a separate function is
// what lets the bracket keep its (necessarily weaker, but pre-existing)
// name tolerance without touching the pool's stricter one.
func buildSideByIDOrName(name, id string, players []domain.Player, withZekkenName bool) gin.H {
	p := findPlayerByID(players, id)
	if p == nil {
		p = findPlayerByName(players, name)
	}
	return sidePayload(name, p, withZekkenName)
}

// phaseFromMatchID derives a human-readable phase label from a match ID.
// Pool match IDs are formatted "PoolName-Idx" by engine/pools.go; we strip
// the suffix and return the pool name verbatim. Bracket matches will land
// in a later slice, for now we return the raw ID as a fallback so the
// field is never empty for a current match (the contract documents the field
// is always present on current payloads).
func phaseFromMatchID(id string) string {
	if i := strings.LastIndex(id, "-"); i > 0 {
		return id[:i]
	}
	return id
}
