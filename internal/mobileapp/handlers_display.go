package mobileapp

import (
	"net/http"
	"regexp"
	"strconv"
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
// in deps.go that covers the read methods we need here, and inventing one
// for a single consumer would be premature. If a second polled surface
// lands later we can hoist a DisplayStore interface then.
func RegisterDisplayHandlers(r *gin.RouterGroup, store *state.Store) {
	r.GET("/court/:court/current", func(c *gin.Context) {
		// Streaming clients poll this on a 1-2s cadence; resolveCourt pins the
		// no-store + CORS headers so the polled-surface guarantee survives
		// router refactors, normalises the :court param, and writes the 503/404.
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
		ids, _ := store.ListCompetitions()
		for _, compID := range ids {
			comp, _ := store.LoadCompetition(compID)
			if comp == nil {
				continue
			}

			poolMatches, _ := store.LoadPoolMatches(compID)
			for _, m := range poolMatches {
				if !strings.EqualFold(m.Court, court) || m.Status != state.MatchStatusRunning {
					continue
				}
				players := currentMatchPlayers(store, comp)
				// Pool daihyosen/tiebreaker rep bouts carry team names in
				// sideA/sideB; the representative fighter for each side lives in
				// RepPlayerA/B. Empty for every regular match (mp-62vr).
				c.JSON(http.StatusOK, currentMatchPayload(court, comp, players,
					m.SideA, m.SideB, m.IpponsA, m.IpponsB, m.HansokuA, m.HansokuB,
					phaseFromMatchID(m.ID), m.RepPlayerA, m.RepPlayerB))
				return
			}

			// Bracket/knockout: a running elimination bout persists its running
			// score as the formatted ScoreA/ScoreB string (engine.formatScore),
			// not the ippon arrays a pool MatchResult carries. parseScore turns
			// it back into the {ippons, hansoku} shape the contract returns.
			// Elimination matches have no representative-bout fighters.
			bracket, _ := store.LoadBracket(compID)
			if bracket == nil {
				continue
			}
			for _, round := range bracket.Rounds {
				for _, bm := range round {
					if !strings.EqualFold(bm.Court, court) || bm.Status != state.MatchStatusRunning {
						continue
					}
					players := currentMatchPlayers(store, comp)
					ipponsA, hansokuA := parseScore(bm.ScoreA)
					ipponsB, hansokuB := parseScore(bm.ScoreB)
					c.JSON(http.StatusOK, currentMatchPayload(court, comp, players,
						bm.SideA, bm.SideB, ipponsA, ipponsB, hansokuA, hansokuB,
						phaseFromMatchID(bm.ID), "", ""))
					return
				}
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
		court, ok := resolveCourt(c, store)
		if !ok {
			return
		}

		ids, _ := store.ListCompetitions()
		comps := make([]gin.H, 0, len(ids))
		for _, compID := range ids {
			// The court filter does the setup-skip and "real match on this
			// court" gating inside the single per-comp load (no separate
			// presence pre-check that would re-read poolMatches/bracket).
			if payload := buildViewerCompetitionPayload(store, compID, court); payload != nil {
				comps = append(comps, payload)
			}
		}

		c.JSON(http.StatusOK, gin.H{"court": court, "competitions": comps})
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
// must never be cached), normalises the :court param, validates it against the
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
	}
	return false
}

// currentMatchPlayers loads the participant slice used to enrich a court's
// current-match payload (DisplayName/Dojo/number). LoadParticipantsOpt is the
// canonical read so we pick up DisplayName/Dojo even on legacy competitions
// that predate the HasParticipantIDs flag. mp-13y: when a numberPrefix is
// configured, merge the pools.csv-derived numbers onto the slice so buildSide
// can include "number" in the polled OBS/vMix overlay payload; the pools.csv
// read is skipped entirely otherwise (the common case).
func currentMatchPlayers(store *state.Store, comp *state.Competition) []domain.Player {
	var hasIDsHint *bool
	if comp.HasParticipantIDs {
		t := true
		hasIDsHint = &t
	}
	players, _ := store.LoadParticipantsOpt(comp.ID, comp.WithZekkenName, state.LoadParticipantsOpts{WithSeeds: false, HasIDs: hasIDsHint})
	if comp.NumberPrefix != "" {
		pools, _ := store.LoadPools(comp.ID)
		mergePoolNumbersIntoPlayersSlice(comp.NumberPrefix, players, pools, comp.Format)
	}
	return players
}

// currentMatchPayload builds the GET /court/:court/current "current" body shared
// by the pool and bracket branches. repA/repB are the representative-bout
// fighters for a pool daihyosen (empty for regular and bracket matches).
func currentMatchPayload(court string, comp *state.Competition, players []domain.Player,
	sideAName, sideBName string, ipponsA, ipponsB []string, hansokuA, hansokuB int,
	phase, repA, repB string) gin.H {
	return gin.H{
		"court":  court,
		"status": "current",
		"competition": gin.H{
			"id":   comp.ID,
			"name": comp.Name,
		},
		"phase": phase,
		"sideA": buildSide(sideAName, players, comp.WithZekkenName),
		"sideB": buildSide(sideBName, players, comp.WithZekkenName),
		// Normalize nil → [] so the JSON encodes empty arrays, not null: the
		// contract models ipponsA/ipponsB as arrays and overlay clients assume
		// []. A nil slice reaches here from an unscored pool match or a bracket
		// score with no ippons ("(H2)" or "" via parseScore, which stays
		// nil-returning so its unit tests hold).
		"ipponsA":    emptyIfNil(ipponsA),
		"ipponsB":    emptyIfNil(ipponsB),
		"hansokuA":   hansokuA,
		"hansokuB":   hansokuB,
		"repPlayerA": repA,
		"repPlayerB": repB,
	}
}

// emptyIfNil returns a non-nil slice so JSON encodes [] rather than null.
// Applied at the response boundary (currentMatchPayload) so parseScore can keep
// returning nil (its unit tests pin that) while the wire stays array-typed.
func emptyIfNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// parseScore is the inverse of engine.formatScore (internal/engine/scoring.go):
// "MK (H1)" → (["M","K"], 1), "MK" → (["M","K"], 0), "(H1)" → (nil, 1). Ippon
// letters are single runes (M/K/D/T/H/S or the ○ default-win marker), so
// splitting the non-hansoku remainder on runes recovers the slice. Used to fill
// the ippon/hansoku fields for a RUNNING bracket match, which persists its
// running score as the formatted ScoreA/ScoreB string rather than the ippon
// arrays a pool MatchResult carries.
func parseScore(s string) ([]string, int) {
	s = strings.TrimSpace(s)
	hansoku := 0
	if i := strings.LastIndex(s, "(H"); i >= 0 {
		if j := strings.Index(s[i:], ")"); j >= 0 {
			if n, err := strconv.Atoi(s[i+2 : i+j]); err == nil {
				hansoku = n
			}
			s = strings.TrimSpace(s[:i])
		}
	}
	var ippons []string
	for _, r := range s {
		if r == ' ' {
			continue
		}
		ippons = append(ippons, string(r))
	}
	return ippons, hansoku
}

// buildSide turns a participant name (which is what MatchResult.SideA/SideB
// carries, see state/models.go) into the per-side payload defined by the
// court-current contract. When the participants list cannot resolve the name
// we fall back to a name-only side so the overlay can still render
// "Player vs Player" rather than blanking out.
func buildSide(name string, players []domain.Player, withZekkenName bool) gin.H {
	displayName := name
	dojo := ""
	playerID := ""
	number := ""
	for i := range players {
		if players[i].Name == name {
			if withZekkenName && players[i].DisplayName != "" {
				displayName = players[i].DisplayName
			}
			dojo = players[i].Dojo
			playerID = players[i].ID
			number = players[i].Number
			break
		}
	}
	return gin.H{
		"playerId":    playerID,
		"name":        name,
		"displayName": displayName,
		"dojo":        dojo,
		"number":      number,
	}
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
