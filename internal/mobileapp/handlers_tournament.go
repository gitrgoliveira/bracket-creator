package mobileapp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// validateDateDMY validates that `date` is either empty or a syntactically
// AND semantically valid day in DD-MM-YYYY format. Uses Go's time-parsing
// reference layout `02-01-2006` which catches both shape errors and
// out-of-range days (Feb 31, 32-01-2026, etc.). Also enforces the same
// year range that the frontend validator applies (see admin_helpers.jsx
// MIN_YEAR/MAX_YEAR — kept in lockstep with helper.MinDateYear /
// helper.MaxDateYear) so direct API callers can't persist a year the UI
// then refuses to display or save against. Shared helper used by
// tournament + competition + import write paths to keep the canonical
// format invariant in one place.
func validateDateDMY(date string) error {
	if date == "" {
		return nil
	}
	parsed, err := time.Parse("02-01-2006", date)
	if err != nil {
		return fmt.Errorf("date must be DD-MM-YYYY")
	}
	if year := parsed.Year(); year < helper.MinDateYear || year > helper.MaxDateYear {
		return fmt.Errorf("date year must be between %d and %d", helper.MinDateYear, helper.MaxDateYear)
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
	seen := make(map[string]bool, len(courts))
	for i, label := range courts {
		if label == "" {
			return fmt.Errorf("courts[%d]: court label cannot be empty", i)
		}
		// Reject whitespace-only labels. Pre-fix, the `label == ""`
		// check above and the single-character check below both passed
		// for a label like " " (single space) — `label != ""` is true
		// and `len([]rune(" ")) == 1`. The label then propagated to
		// disk and became a React `key={cc}` value, schedule
		// `byCourt[m.court]` bucket key, and filter value. Visually
		// blank but structurally distinct from "" — broke the admin's
		// court-filter dropdown and produced an unnamed schedule
		// column. The admin UI's auto-generated A,B,C... labels can't
		// produce whitespace, so this is defense-in-depth against
		// direct API/import payloads.
		if strings.TrimSpace(label) == "" {
			return fmt.Errorf("courts[%d]: court label cannot be whitespace-only", i)
		}
		// Spec: single-character labels. The bracket-generator's
		// CourtLabel helper produces "A"..."Z" exactly. Multi-character
		// labels (e.g. "AA") would break downstream Excel layout and
		// the viewer's "shiaijo" abbreviation.
		if len([]rune(label)) != 1 {
			return fmt.Errorf("courts[%d]: court label %q must be a single character", i, label)
		}
		// Reject duplicate labels. The frontend uses court labels as
		// identity keys: `<div key={cc}>` per-court rendering, filter
		// values, the schedule view's `byCourt[m.court]` bucket map.
		// Duplicates collapse the byCourt map (two courts' matches end
		// up in one lane) and trigger React duplicate-key warnings.
		// The admin UI's AdminEditTournament generates courts via
		// `Array.from({length: n}, (_, i) => String.fromCharCode(65 + i))`
		// so duplicates can't arise via the form, but direct API/import
		// payloads bypass that — defense-in-depth at the validator.
		if seen[label] {
			return fmt.Errorf("courts[%d]: duplicate court label %q", i, label)
		}
		seen[label] = true
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

// validateTournamentDurationDays returns an error when durationDays is
// outside the accepted range. 0 is allowed (treated as "default to 1" by
// ApplyTournamentDefaults); values 1–MaxTournamentDurationDays are
// accepted; negative values and values > MaxTournamentDurationDays are
// rejected with a clear 400-bound error.
func validateTournamentDurationDays(durationDays int) error {
	if durationDays < 0 || durationDays > MaxTournamentDurationDays {
		return fmt.Errorf("durationDays must be between 1 and %d", MaxTournamentDurationDays)
	}
	return nil
}

// errModeImmutable is the sentinel the PUT /tournament transform returns
// when the caller tries to change the tournament Mode field after creation
// (mp-7h7). Mode is immutable — flipping it mid-event would either
// suddenly expose the admin surface (officiated → self-run) or lock
// participants out mid-scoring (self-run → officiated).
var errModeImmutable = errors.New("tournament mode cannot be changed after creation")

// errSelfRunRequiresAdminPassword is the sentinel returned when a caller
// tries to create or save a self-run tournament in file mode without an
// elevated (admin) password configured. In self-run mode the main-gate
// is skipped, so if enforceElevated's GateActive()==false path (file mode,
// no admin password) were reached the destructive routes would be fully
// public — violating the self-run invariant that destructive calls still
// require X-Admin-Password. (mp-7h7 fail-open fix)
var errSelfRunRequiresAdminPassword = errors.New("self-run tournaments require an admin (destructive-ops) password to be set; configure it before switching to self-run mode or set it in the same request")

// trimPublicInfoFields trims all optional public tournament info string fields
// and normalises the Contacts slice: each entry's Label/Value is trimmed and
// all-empty entries are dropped. Called identically by the PUT and POST
// /tournament handlers. Count validation (>MaxTournamentContacts) is enforced
// by validateTournamentLengths as a 400 error rather than silently truncating.
func trimPublicInfoFields(t *state.Tournament) {
	t.VenueAddress = strings.TrimSpace(t.VenueAddress)
	t.VenueMapURL = strings.TrimSpace(t.VenueMapURL)
	t.OpeningTime = strings.TrimSpace(t.OpeningTime)
	t.ClosingTime = strings.TrimSpace(t.ClosingTime)
	t.RulesURL = strings.TrimSpace(t.RulesURL)
	t.AwardsNote = strings.TrimSpace(t.AwardsNote)
	t.InfoNotes = strings.TrimSpace(t.InfoNotes)
	if len(t.Contacts) > 0 {
		filtered := make([]state.TournamentContact, 0, len(t.Contacts))
		for _, ct := range t.Contacts {
			ct.Label = strings.TrimSpace(ct.Label)
			ct.Value = strings.TrimSpace(ct.Value)
			if ct.Label != "" || ct.Value != "" {
				filtered = append(filtered, ct)
			}
		}
		t.Contacts = filtered
	}
}

// validateTournamentLengths enforces the persisted-string caps from
// validation.go on every string field of t. Called after trim and
// after the required-field checks so error messages report the
// post-trim length the client actually persisted. Returns the first
// *ValidationError on failure for direct mapping to HTTP 400.
func validateTournamentLengths(t *state.Tournament) error {
	if err := validateMaxLen("name", t.Name, MaxLenTournamentName); err != nil {
		return err
	}
	if err := validateMaxLen("venue", t.Venue, MaxLenTournamentVenue); err != nil {
		return err
	}
	if err := validateMaxLen("date", t.Date, MaxLenTournamentDate); err != nil {
		return err
	}
	if err := validateMaxLen("password", t.Password, MaxLenTournamentPassword); err != nil {
		return err
	}
	if err := validateMaxLen("openingBlock", t.OpeningBlock, MaxLenCeremonyBlock); err != nil {
		return err
	}
	if err := validateMaxLen("lunchBlock", t.LunchBlock, MaxLenCeremonyBlock); err != nil {
		return err
	}
	if err := validateMaxLen("closingBlock", t.ClosingBlock, MaxLenCeremonyBlock); err != nil {
		return err
	}
	// mp-ef3: public tournament info fields.
	if err := validateMaxLen("venueAddress", t.VenueAddress, MaxLenVenueAddress); err != nil {
		return err
	}
	if err := validateMaxLen("venueMapURL", t.VenueMapURL, MaxLenVenueMapURL); err != nil {
		return err
	}
	if err := validateHTTPSURL("venueMapURL", t.VenueMapURL); err != nil {
		return err
	}
	if err := validateMaxLen("openingTime", t.OpeningTime, MaxLenDisplayTime); err != nil {
		return err
	}
	if err := validateMaxLen("closingTime", t.ClosingTime, MaxLenDisplayTime); err != nil {
		return err
	}
	if err := validateMaxLen("rulesURL", t.RulesURL, MaxLenRulesURL); err != nil {
		return err
	}
	if err := validateHTTPSURL("rulesURL", t.RulesURL); err != nil {
		return err
	}
	if err := validateMaxLen("awardsNote", t.AwardsNote, MaxLenAwardsNote); err != nil {
		return err
	}
	if err := validateMaxLen("infoNotes", t.InfoNotes, MaxLenInfoNotes); err != nil {
		return err
	}
	if len(t.Contacts) > MaxTournamentContacts {
		return &ValidationError{
			Field:   "contacts",
			Message: fmt.Sprintf("must contain <= %d entries", MaxTournamentContacts),
		}
	}
	for i, ct := range t.Contacts {
		prefix := fmt.Sprintf("contacts[%d]", i)
		if err := validateMaxLen(prefix+".label", ct.Label, MaxLenContactLabel); err != nil {
			return err
		}
		if err := validateMaxLen(prefix+".value", ct.Value, MaxLenContactValue); err != nil {
			return err
		}
	}
	return nil
}

func RegisterTournamentHandlers(r *gin.RouterGroup, store *state.Store, hub *Hub, verifier PasswordVerifier) {
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
		// In locked mode the on-disk Password is irrelevant (auth comes
		// from the env-var bcrypt hash). Strip it from the response so
		// the admin UI doesn't show a stored-but-unused value that
		// would mislead the operator about what credential actually
		// authenticates them. Mirrors the public viewer handler's
		// password-strip step (handlers_viewer.go).
		if verifier != nil && verifier.RedactStoredPassword() {
			publicT := *t
			publicT.Password = ""
			c.JSON(http.StatusOK, publicT)
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
		trimPublicInfoFields(&t)

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

		if err := validateTournamentLengths(&t); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		if err := validateTournamentDurationDays(t.DurationDays); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Default 0 → 1 before persisting so round-trips are consistent.
		if t.DurationDays == 0 {
			t.DurationDays = 1
		}

		if err := validateCourts(t.Courts); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Validate Mode before entering the update transform. Mode is
		// immutable after creation (mp-7h7) — the transform enforces that
		// invariant — but it must also be a known value so an invalid string
		// never lands on disk via a direct API call. Empty is accepted here
		// (the transform normalises "" → preserved-stored-value or
		// "officiated" for a new record). ValidateTournamentMode is the
		// canonical check; POST already calls it for the same reason.
		if err := state.ValidateTournamentMode(t.Mode); err != nil {
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
		//
		// Verifiers that mark the on-disk Password as non-authoritative
		// (today: the bcrypt locked verifier) auth from the env-var hash;
		// the on-disk Password field is irrelevant. If the admin sent a
		// non-empty Password in locked mode it would silently never take
		// effect — surface a 400 explaining the situation rather than
		// pretending the rotation worked. Empty Password is fine
		// (the SPA's `password: pass || undefined` pattern hits this
		// path when the operator is just editing name/venue/courts).
		locked := verifier != nil && verifier.RedactStoredPassword()
		if locked && t.Password != "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "password rotation is disabled in locked mode; restart with a new TOURNAMENT_PASSWORD_HASH"})
			return
		}
		// Track whether the persisted password actually changed so we
		// can fire EventPasswordReset (in addition to
		// EventTournamentUpdated) and other admin sessions log out
		// immediately instead of waiting for their next write to 401.
		passwordChanged := false
		changed, err := store.UpdateTournamentChanged(&t, func(current, desired *state.Tournament) error {
			// Preserve the write-only elevated password (spec 004). It has
			// json:"-", so the bound body always leaves desired.AdminPassword
			// == "" — without copying it forward, a routine name/venue save
			// would silently wipe the elevated credential. It is changed only
			// via PUT /api/auth/admin-password, never here.
			if current != nil {
				desired.AdminPassword = current.AdminPassword
			}

			// Immutability of Mode (mp-7h7): Mode is chosen once at creation
			// and cannot be changed via PUT. Mirror the write-only AdminPassword
			// preservation pattern: when the incoming body sends Mode == ""
			// (omitempty), preserve the current stored value. When the body
			// sends a non-empty Mode that differs from the stored Mode, reject.
			//
			// Fix 3329406568: normalize current.Mode="" to "officiated" before
			// comparing so an idempotent PUT sending mode:"officiated" against a
			// legacy tournament (empty Mode on disk) is accepted rather than
			// rejected as a mutation ("" != "officiated").
			if current != nil {
				if desired.Mode == "" {
					// Preserve on omit — backward-compat and "no change" intent.
					desired.Mode = current.Mode
				} else {
					// Normalize legacy empty current mode before comparing.
					currentMode := current.Mode
					if currentMode == "" {
						currentMode = state.TournamentModeOfficiated
					}
					if desired.Mode != currentMode {
						return errModeImmutable
					}
				}
			}
			// Normalize empty Mode so new tournaments always have a canonical value.
			if desired.Mode == "" {
				desired.Mode = state.TournamentModeOfficiated
			}

			if locked {
				// Reset passwordChanged to false defensively — the
				// non-empty case is already rejected above, but keep
				// the bookkeeping consistent in case the guard is
				// ever loosened.
				passwordChanged = false
				if current != nil {
					desired.Password = current.Password
				}
				return nil
			}
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
			if current == nil || current.Password != desired.Password {
				passwordChanged = true
			}

			// Fail-open guard for self-run + file mode (mp-7h7): if the
			// tournament is self-run, the main-gate is skipped, so
			// destructive routes rely SOLELY on RequireElevatedPassword /
			// enforceElevated. enforceElevated allows requests when
			// GateActive()==false (file mode, no AdminPassword set). Without
			// this guard, a self-run tournament with no admin password would
			// leave destructive routes fully public. Locked mode always fails
			// closed (GateActive always true → 503 when unconfigured) so we
			// must NOT apply this guard in locked mode: there, the on-disk
			// AdminPassword is always empty/inert (the env-var bcrypt hash is
			// authoritative), so guarding on desired.AdminPassword == ""
			// would wrongly reject every PUT to a locked-mode self-run
			// tournament (e.g. a routine venue edit). The `locked` flag is
			// captured from the enclosing handler scope.
			//
			// desired.AdminPassword was preserved from current above, so it
			// reflects the current on-disk value.
			if !locked && desired.Mode == state.TournamentModeSelfRun && desired.AdminPassword == "" {
				return errSelfRunRequiresAdminPassword
			}

			return nil
		})
		if errors.Is(err, errPasswordRequired) {
			c.JSON(http.StatusBadRequest, gin.H{"error": errPasswordRequired.Error()})
			return
		}
		if errors.Is(err, errModeImmutable) {
			c.JSON(http.StatusBadRequest, gin.H{"error": errModeImmutable.Error()})
			return
		}
		if errors.Is(err, errSelfRunRequiresAdminPassword) {
			c.JSON(http.StatusBadRequest, gin.H{"error": errSelfRunRequiresAdminPassword.Error()})
			return
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
			if passwordChanged {
				// Mirror the /reset endpoint: rotating the credential
				// via the admin PUT path must also log other admin
				// sessions out so their cached bc_password isn't
				// silently stale. The originatorId field on the body
				// would be welcome here too (and we'd echo it on the
				// event), but the admin PUT body doesn't carry one —
				// every admin tab including the originator will
				// re-prompt, which is acceptable since the operator
				// just typed the new credential and can type it again.
				hub.Broadcast(EventPasswordReset, passwordResetEventData{})
			}
		}
		// In locked mode the on-disk Password is irrelevant and must not
		// leak through any response surface. GET strips it; apply the
		// same redaction here so an admin GET-after-PUT can't differ
		// from a plain GET. Copy `t` before mutating — the
		// UpdateTournamentChanged transform stashed the same struct in
		// the store's cache, so a direct `t.Password = ""` would also
		// blank the cached password and cause subsequent LoadTournament
		// calls (in tests OR in middleware) to see an empty password
		// where the file on disk still has the real value.
		if locked {
			publicT := t
			publicT.Password = ""
			c.JSON(http.StatusOK, publicT)
			return
		}
		c.JSON(http.StatusOK, t)
	})

	r.POST("/tournament", func(c *gin.Context) {
		var t state.Tournament
		// ShouldBindBodyWith caches the raw request body so we can bind it a
		// SECOND time below into a separate struct. This lets us read a
		// transient `adminPassword` field from the SAME POST body WITHOUT
		// weakening the json:"-" invariant on Tournament.AdminPassword (which
		// deliberately can never be populated by binding a request body).
		// Creation is the one safe place to accept the elevated credential in
		// the body: there is no existing credential to protect at bootstrap,
		// and it lets self-run creation be atomic (the fail-open guard never
		// sees a self-run tournament persisted without an admin password). PUT
		// keeps the json:"-" preserve-only behaviour (rotation stays on
		// PUT /api/auth/admin-password).
		if err := c.ShouldBindBodyWith(&t, binding.JSON); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Second bind into a local struct to capture the transient admin
		// (destructive-ops) password. Tournament.AdminPassword has json:"-"
		// so it is never populated by the bind above — this is the only way
		// to read it from the body at creation time. A bind failure here is
		// impossible (the first bind already validated the JSON), but handle
		// it for safety.
		var extra struct {
			AdminPassword string `json:"adminPassword"`
		}
		if err := c.ShouldBindBodyWith(&extra, binding.JSON); err != nil {
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
		trimPublicInfoFields(&t)

		// Same empty-after-trim guard as the PUT handler.
		if t.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tournament name is required"})
			return
		}

		if err := validateTournamentLengths(&t); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Same DD-MM-YYYY guard as the PUT handler.
		if err := validateDateDMY(t.Date); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Same DurationDays guard as the PUT handler.
		if err := validateTournamentDurationDays(t.DurationDays); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if t.DurationDays == 0 {
			t.DurationDays = 1
		}

		if err := validateCourts(t.Courts); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Validate and normalize the Mode field (mp-7h7). POST is the creation
		// endpoint — Mode is accepted here and becomes immutable thereafter.
		// Empty maps to "officiated" (default, backward compat).
		if err := state.ValidateTournamentMode(t.Mode); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if t.Mode == "" {
			t.Mode = state.TournamentModeOfficiated
		}

		// Reject empty Password on POST (initial setup) in file mode.
		// AuthMiddleware allows POST /api/tournament unauthenticated
		// when the tournament is uninitialized — this is the bootstrap
		// entry point. If Password == "" lands on disk, AuthMiddleware's
		// `password != t.Password` check vacuously passes for any
		// request with an empty `X-Tournament-Password` header (empty
		// == empty), exposing every /api/* endpoint unauthenticated.
		// The PUT handler's preserve-stored-on-empty guard above
		// can't reach this state on update — but POST is how that
		// state would land in the first place, so block it here.
		// Note: Password is NOT trimmed (passwords may intentionally
		// contain whitespace; auth check is exact-string match).
		//
		// Locked mode: the on-disk Password is irrelevant — auth comes
		// from the env-var bcrypt hash. The SPA's CreateTournament form
		// detects locked mode via /api/auth-config and labels the
		// password field as the env-var credential, sending it as
		// X-Tournament-Password (the middleware verifies the header,
		// the body's password is discarded here). The stored value is
		// read back as empty via GET /tournament so the admin doesn't
		// see a stale credential after bootstrap.
		// Load the existing tournament (if any) for two purposes:
		//   1. Locked mode: preserve the on-disk password so a later
		//      file-mode rollback can recover it.
		//   2. File mode: detect a password change so we can broadcast
		//      EventPasswordReset and clear stale admin sessions.
		existingForPost, loadErrPost := store.LoadTournament()
		if loadErrPost != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": loadErrPost.Error()})
			return
		}

		if verifier != nil && verifier.RedactStoredPassword() {
			// Locked mode: the on-disk Password is not authoritative.
			// For a true first bootstrap (no tournament.md on disk yet),
			// store "" so GET /tournament returns empty — no stale
			// credential visible. For re-POSTs against an existing record,
			// preserve whatever is currently stored so a later file-mode
			// rollback can recover the original credential.
			if existingForPost != nil {
				t.Password = existingForPost.Password
			} else {
				t.Password = ""
			}
		} else if t.Password == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tournament password is required"})
			return
		}

		// Preserve the write-only elevated password (spec 004) across a
		// re-bootstrap POST. It has json:"-" so the bound body never carries
		// it; without this a re-POST over an existing record would wipe the
		// elevated credential. On a true first bootstrap (existingForPost ==
		// nil) it stays "" — the operator sets it later via Settings.
		if existingForPost != nil {
			t.AdminPassword = existingForPost.AdminPassword
		}

		// Fix 3329416172: Mode is immutable after creation (mp-7h7). On a
		// re-bootstrap POST (existingForPost != nil) preserve the stored Mode
		// so a caller cannot flip an officiated tournament to self-run or vice
		// versa via POST. Normalise legacy empty to "officiated" so the on-disk
		// record gains the canonical value on the next write. On a true first
		// bootstrap (existingForPost == nil), Mode was already validated and
		// normalised above — no change needed.
		if existingForPost != nil {
			existingMode := existingForPost.Mode
			if existingMode == "" {
				existingMode = state.TournamentModeOfficiated
			}
			t.Mode = existingMode
		}

		// Atomic admin-password set at creation (mp-7h7). In file mode the
		// admin (destructive-ops) password is stored as PLAINTEXT in
		// Tournament.AdminPassword (fileElevatedVerifier.Verify does an
		// exact-string compare). When the body carried an `adminPassword`
		// (read into `extra` above) and no admin password is already on disk,
		// set it now — BEFORE the fail-open guard and before
		// SaveTournamentChanged — so a self-run tournament is persisted WITH
		// the credential in a single request. This is what makes self-run
		// creation possible via the real API (the json:"-" invariant blocks
		// the field from being bound directly into Tournament).
		//
		// Locked mode: the env-var bcrypt hash is the authoritative elevated
		// credential; any body adminPassword is inert, so ignore it (do not
		// write a plaintext value that would never be consulted).
		//
		// Fix 3329406566: validate adminPassword length before copy. The field
		// is transient (not in Tournament) so validateTournamentLengths never
		// sees it; enforce MaxLenTournamentPassword here.
		fileMode := verifier == nil || !verifier.RedactStoredPassword()
		// Fix 3331061367: validate adminPassword length whenever the field is
		// present in the body, regardless of mode or whether an existing
		// credential is already stored. This enforces the OpenAPI maxLength:256
		// contract consistently: locked mode and re-bootstrap with an existing
		// password would otherwise accept and silently discard an oversized
		// value, giving callers no feedback that their request violates the
		// contract. Only persist the value when in file mode with no existing
		// elevated credential (locked mode uses TOURNAMENT_ADMIN_PASSWORD_HASH;
		// re-bootstrap preserves the existing credential).
		if extra.AdminPassword != "" {
			if err := validateMaxLen("adminPassword", extra.AdminPassword, MaxLenTournamentPassword); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			if fileMode && t.AdminPassword == "" {
				t.AdminPassword = extra.AdminPassword
			}
		}

		// Fail-open guard for self-run + file mode (mp-7h7).
		// In self-run mode the main-gate is skipped, so destructive routes
		// rely SOLELY on RequireElevatedPassword / enforceElevated.
		// enforceElevated GateActive()==false (file mode, no admin password)
		// means destructive routes would be fully public — violating the
		// invariant. Reject creation of a self-run tournament in file mode
		// unless an AdminPassword is set: either already on disk (preserved
		// from existingForPost) or supplied in this request's body (set just
		// above). Locked mode always fails closed (GateActive always true →
		// 503) so no guard needed there.
		if fileMode && t.Mode == state.TournamentModeSelfRun && t.AdminPassword == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": errSelfRunRequiresAdminPassword.Error()})
			return
		}

		if _, err := store.SaveTournamentChanged(&t); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		// In file mode, broadcast EventPasswordReset when an existing
		// tournament's password is OVERWRITTEN (re-bootstrap with a new
		// credential) so other logged-in admin sessions clear their
		// stale cached credentials. Mirrors the PUT handler's behavior.
		//
		// We deliberately do NOT broadcast on the first-time bootstrap
		// (`existingForPost == nil`). The creating tab already subscribed
		// to SSE before submit, and the create-tournament flow calls
		// `onCreated(t, pass)` to mark itself authenticated with the
		// just-typed password. An empty-originator `password_reset`
		// broadcast would race that, and the SPA's SSE handler — which
		// has no originatorId to ignore (the POST body doesn't carry
		// one) — would clear the freshly-cached credential and kick
		// the user straight back to AuthModal.
		//
		// In locked mode, t.Password is always set to the pre-existing
		// on-disk value above, so it never changes via POST and we skip
		// the broadcast.
		if (verifier == nil || !verifier.RedactStoredPassword()) &&
			existingForPost != nil &&
			t.Password != existingForPost.Password {
			hub.Broadcast(EventPasswordReset, passwordResetEventData{})
		}
		// In locked mode the on-disk Password is not authoritative; strip it
		// from the response so callers don't cache a stale file-mode credential
		// that would never authenticate against the env-var bcrypt hash.
		// Mirrors the GET and PUT redaction in the same verifier.RedactStoredPassword()
		// branch above.
		if verifier != nil && verifier.RedactStoredPassword() {
			publicT := t
			publicT.Password = ""
			c.JSON(http.StatusCreated, publicT)
			return
		}
		c.JSON(http.StatusCreated, t)
	})
}
