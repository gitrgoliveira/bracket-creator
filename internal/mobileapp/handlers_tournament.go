package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// validateDateDMY validates that `date` is either empty or a syntactically
// AND semantically valid day in DD-MM-YYYY format. Uses Go's time-parsing
// reference layout `02-01-2006` which catches both shape errors and
// out-of-range days (Feb 31, 32-01-2026, etc.). Shared helper used by
// tournament + competition + import write paths to keep the canonical
// format invariant in one place.
func validateDateDMY(date string) error {
	if date == "" {
		return nil
	}
	if _, err := time.Parse("02-01-2006", date); err != nil {
		return fmt.Errorf("date must be DD-MM-YYYY")
	}
	return nil
}

// validateCourtLabels checks that each entry in courts is a non-empty
// single character (the spec-documented format — see Tournament.courts
// in specs/openapi.yaml). Used as a shared check for both tournament
// and competition courts. Caller decides whether empty courts is
// acceptable: validateCourts rejects empty (tournament must have at
// least one court to run anything); validateCompetitionCourts accepts
// empty (the engine applies a 1-court default for competitions whose
// Courts list is empty, allowing tournament-wide courts to be the
// implicit default).
func validateCourtLabels(courts []string) error {
	if len(courts) > helper.MaxCourts {
		return fmt.Errorf("courts must be <= %d (Shiaijo are labelled A–Z), got %d", helper.MaxCourts, len(courts))
	}
	for i, label := range courts {
		if label == "" {
			return fmt.Errorf("courts[%d]: court label cannot be empty", i)
		}
		// Spec: single-character labels. The bracket-generator's
		// CourtLabel helper produces "A"..."Z" exactly. Multi-character
		// labels (e.g. "AA") would break downstream Excel layout and
		// the viewer's "shiaijo" abbreviation.
		if len([]rune(label)) != 1 {
			return fmt.Errorf("courts[%d]: court label %q must be a single character", i, label)
		}
	}
	return nil
}

// validateCourts is the strict tournament-level check: between 1 and
// helper.MaxCourts (26, the A–Z labelling cap) entries, each a single
// non-empty character. Direct API callers can't bypass the admin UI's
// per-form checks (admin_setup.jsx AdminEditTournament caps at 26
// client-side, but a hand-crafted POST /tournament with 50 courts or
// multi-character labels was previously persisted as-is).
func validateCourts(courts []string) error {
	if err := helper.ValidateCourts(len(courts)); err != nil {
		return err
	}
	return validateCourtLabels(courts)
}

// validateCompetitionCourts is the looser competition-level check:
// 0..helper.MaxCourts entries, each (when present) a single non-empty
// character. Empty is allowed because the engine defaults a
// competition with no Courts to 1 court — this matches the existing
// import handler's `if len(comp.Courts) == 0 { comp.Courts = []string{"A"} }`
// fallback semantics and the engine generators' `if numCourts == 0 { numCourts = 1 }`
// behavior. The label and cap invariants from validateCourtLabels
// still apply when courts are explicitly provided.
func validateCompetitionCourts(courts []string) error {
	return validateCourtLabels(courts)
}

// errPasswordRequired is the sentinel the PUT /tournament transform
// returns when the desired Password is empty AND the stored Password
// is also empty (or no record exists yet). It propagates back through
// UpdateTournamentChanged unchanged so the handler can map it to a
// 400 response. Using a typed sentinel rather than an inline error
// keeps the handler's errors.Is check stable across refactors.
var errPasswordRequired = errors.New("tournament password is required")

func RegisterTournamentHandlers(r *gin.RouterGroup, store *state.Store, hub *Hub) {
	r.GET("/tournament", func(c *gin.Context) {
		t, err := store.LoadTournament()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if t == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "tournament not initialized"})
			return
		}
		c.JSON(http.StatusOK, t)
	})

	r.PUT("/tournament", func(c *gin.Context) {
		var t state.Tournament
		if err := c.ShouldBindJSON(&t); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Trim string fields so padded input from direct API callers
		// doesn't persist with surrounding whitespace. Date is included
		// for cross-file guard symmetry with handlers_import.go (which
		// trims competition.Date) and handlers_competition.go (which
		// trims the same competition string fields uniformly). Password
		// is NOT trimmed — the user may intentionally use leading/
		// trailing whitespace, and the auth header check is exact-string
		// match.
		t.Name = strings.TrimSpace(t.Name)
		t.Venue = strings.TrimSpace(t.Venue)
		t.Date = strings.TrimSpace(t.Date)

		// Reject non-empty Date that doesn't match the canonical DD-MM-YYYY
		// shape (or semantically invalid days like Feb 31). The frontend
		// converts the HTML date picker's ISO output to DMY before sending;
		// direct API callers must send DMY directly. See validateDateDMY.
		if err := validateDateDMY(t.Date); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Reject whitespace-only names. The current EditTournament UI
		// (admin_setup.jsx) validates trimmed name client-side before
		// submit; this is defense-in-depth against direct API callers
		// (curl etc.). Without this guard, the trim above silently
		// persists Name == "" — admin UI then shows a blank tournament
		// title and the persisted record fails the documented "tournament
		// has a name" invariant.
		// Cross-file guard symmetry with the POST handler below and
		// the competition write paths in
		// handlers_competition.go + handlers_import.go.
		if t.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tournament name is required"})
			return
		}

		if err := validateCourts(t.Courts); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Preserve the stored Password when the incoming body omits it
		// or sends "". The frontend AdminEditTournament uses
		// `password: pass || undefined` (admin_setup.jsx:89) so an
		// admin who edits the name/venue without changing the password
		// sends a JSON body with the password field omitted — Go's
		// ShouldBindJSON then leaves t.Password == "". Without the
		// preserve step, that save would clobber the stored password
		// with "", and AuthMiddleware's `password != t.Password` check
		// would then vacuously pass for an empty `X-Tournament-Password`
		// header — exposing every /api/* endpoint unauthenticated.
		//
		// The load + preserve + save sequence runs under the store's
		// write lock via UpdateTournamentChanged. The earlier
		// implementation (separate LoadTournament + SaveTournamentChanged
		// calls) had a TOCTOU window: two concurrent PUTs, one with
		// empty Password (intent: keep) and one with a new password
		// (intent: change), could race so that the empty-password PUT's
		// late save overwrote the change-password PUT's earlier save —
		// silently losing the password change. The atomic primitive
		// closes that window.
		changed, err := store.UpdateTournamentChanged(&t, func(current, desired *state.Tournament) error {
			if desired.Password == "" && current != nil {
				desired.Password = current.Password
			}
			// Defense-in-depth: if after the preserve step the password
			// is STILL empty (a fresh PUT against a never-initialized
			// tournament, or an operator who manually edited
			// tournament.md), reject. An empty stored Password is the
			// exact precondition for the AuthMiddleware vacuous-pass
			// scenario described above (also blocked at the middleware
			// itself — see middleware.go).
			if desired.Password == "" {
				return errPasswordRequired
			}
			return nil
		})
		if errors.Is(err, errPasswordRequired) {
			c.JSON(http.StatusBadRequest, gin.H{"error": errPasswordRequired.Error()})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.JSON(http.StatusOK, t)
	})

	r.POST("/tournament", func(c *gin.Context) {
		var t state.Tournament
		if err := c.ShouldBindJSON(&t); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// See PUT handler above. The current CreateTournament UI in
		// app.jsx trims client-side before submit; this is defense-in-depth
		// against direct API callers (curl etc.) sending padded values —
		// the server-side trim is the canonical defense layer so persisted
		// records are always canonical.
		t.Name = strings.TrimSpace(t.Name)
		t.Venue = strings.TrimSpace(t.Venue)
		t.Date = strings.TrimSpace(t.Date)

		// Same empty-after-trim guard as the PUT handler.
		if t.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tournament name is required"})
			return
		}

		// Same DD-MM-YYYY guard as the PUT handler.
		if err := validateDateDMY(t.Date); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := validateCourts(t.Courts); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Reject empty Password on POST (initial setup). AuthMiddleware
		// allows POST /api/tournament unauthenticated when the
		// tournament is uninitialized — this is the bootstrap entry
		// point. If Password == "" lands on disk, AuthMiddleware's
		// `password != t.Password` check vacuously passes for any
		// request with an empty `X-Tournament-Password` header (empty
		// == empty), exposing every /api/* endpoint unauthenticated.
		// The PUT handler's preserve-stored-on-empty guard above
		// can't reach this state on update — but POST is how that
		// state would land in the first place, so block it here.
		// Note: Password is NOT trimmed (passwords may intentionally
		// contain whitespace; auth check is exact-string match).
		if t.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tournament password is required"})
			return
		}

		if _, err := store.SaveTournamentChanged(&t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusCreated, t)
	})
}
