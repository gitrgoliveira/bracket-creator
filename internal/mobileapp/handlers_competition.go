package mobileapp

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// flipHasParticipantIDs sets Competition.HasParticipantIDs=true after a
// successful non-empty roster save. It's a package var (not an inline
// closure) so tests can inject a deterministic failure without relying
// on filesystem-race timing, see
// TestPUTCompetition_RosterPUT_FlagFlipFailureReturns500. mp-p7n /
// Copilot PR #185 round-9.
var flipHasParticipantIDs = func(store *state.Store, id string) error {
	_, err := store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
		if current == nil {
			return nil, nil
		}
		current.HasParticipantIDs = true
		return current, nil
	})
	return err
}

// slugifyID derives a valid competition ID from a name: lowercase, non-alphanumeric
// runs become a single hyphen, leading/trailing hyphens stripped, max 64 chars.
func slugifyID(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	var sb strings.Builder
	prevHyphen := true
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
			prevHyphen = false
		} else if !prevHyphen {
			sb.WriteRune('-')
			prevHyphen = true
		}
	}
	result := strings.TrimRight(sb.String(), "-")
	if len(result) > 64 {
		result = strings.TrimRight(result[:64], "-")
	}
	return result
}

// errSaveParticipants marks a participants-save failure from
// saveCompetitionWithPlayers, so the POST handler can surface the safe
// "failed to save participants" label to the client (the wrapped cause, which
// may include a filesystem path, is logged, not returned).
var errSaveParticipants = errors.New("failed to save participants")

// saveCompetitionWithPlayers persists the competition config and, when players
// are present, saves participants and extracts seed assignments.
// Returns (true, nil) when the on-disk content changed, so callers can decide
// whether to broadcast.
//
// IMPORTANT: this function is intended for CREATE paths only (POST
// /competitions). On a SaveParticipants failure AFTER SaveCompetitionChanged
// succeeded, it rolls back by calling DeleteCompetition, without that
// rollback, the ID-collision check at the top of POST /competitions would
// block retries because the orphaned config.md is on disk but
// participants.csv isn't. Calling this on an UPDATE path would delete the
// existing record on participants-save failure, so callers updating an
// existing competition must use store.SaveParticipants directly (see the
// PUT /competitions/:id handler, which writes participants after the
// transform commits and treats save errors as retriable since PUT is
// idempotent, no ID-collision trap).
func saveCompetitionWithPlayers(comp *state.Competition, store *state.Store) (bool, error) {
	if len(comp.Players) > 0 {
		comp.HasParticipantIDs = true // participants.csv always written with UUID IDs
	}
	changed, err := store.SaveCompetitionChanged(comp)
	if err != nil {
		return false, err
	}
	if len(comp.Players) == 0 {
		return changed, nil
	}
	if err := store.SaveParticipants(comp.ID, comp.Players); err != nil {
		// Rollback: SaveCompetitionChanged wrote config.md, but
		// participants.csv didn't land. Without removing config.md the
		// caller's ID-collision check on retry would 400 "ID already
		// exists" even though the prior attempt failed. Mirror the
		// import handler's rollback pattern (handlers_import.go).
		_ = store.DeleteCompetition(comp.ID) // best-effort rollback
		return false, fmt.Errorf("%w: %w", errSaveParticipants, err)
	}
	assignments := extractSeeds(comp.Players)
	if err := store.SaveSeeds(comp.ID, assignments); err != nil {
		// SaveSeeds is best-effort by historical contract (the same
		// Printf-warning pattern is used in the PUT handler's
		// participants block). seeds.csv missing is recoverable,
		// the operator can re-set seeds without re-creating the
		// competition. No rollback to avoid surprising the caller
		// with a deleted record over a non-critical write.
		fmt.Printf("Warning: failed to save seeds: %v\n", err)
	}
	return changed, nil
}

func extractSeeds(players []domain.Player) []domain.SeedAssignment {
	var out []domain.SeedAssignment
	for _, p := range players {
		if p.Seed > 0 {
			out = append(out, domain.SeedAssignment{Name: p.Name, SeedRank: p.Seed})
		}
	}
	return out
}

// validateCompetitionDateInTournament checks that the competition's Date
// falls within the tournament's day range [Day 1 .. Day N]. When:
//   - comp.Date is empty (optional field) → skip, return nil.
//   - tourn is nil or tourn.Date is empty → skip (can't derive day list yet).
//   - comp.Date is in the derived day list → return nil.
//   - comp.Date is NOT in the derived day list → return a descriptive error.
//
// errcheck: no bare ignored returns, always propagated by callers.
func validateCompetitionDateInTournament(comp *state.Competition, tourn *state.Tournament) error {
	if comp.Date == "" || tourn == nil || tourn.Date == "" {
		return nil
	}
	days := tourn.Days()
	if len(days) == 0 {
		// Tournament date unparseable or DurationDays < 1, skip range check.
		return nil
	}
	for _, d := range days {
		if d == comp.Date {
			return nil
		}
	}
	return fmt.Errorf("date must be one of the tournament days (%s to %s)", days[0], days[len(days)-1])
}

// validateCompetitionFormat returns an HTTP status code + error
// message for invalid Format / PoolFormat values. Empty values are
// accepted (defaults applied on load). Unknown values return 400.
//
// FR-050a: swiss is now accepted; the caller must ALSO run
// validateSwissConfig when format == swiss to enforce swissRounds >= 1.
func validateCompetitionDurations(comp *state.Competition) error {
	if comp.PoolMatchDuration < 0 || comp.PlayoffMatchDuration < 0 || comp.MatchDuration < 0 {
		return fmt.Errorf("match duration must be >= 0")
	}
	return nil
}

func validateCompetitionFormat(format, poolFormat string) (int, error) {
	switch format {
	case "", state.CompFormatPlayoffs,
		state.CompFormatMixed, state.CompFormatLeague, state.CompFormatSwiss:
		// ok
	default:
		return http.StatusBadRequest, fmt.Errorf("unknown format %q", format)
	}
	switch poolFormat {
	case "", state.PoolFormatFull, state.PoolFormatPartial:
		// ok
	default:
		return http.StatusBadRequest, fmt.Errorf("unknown poolFormat %q", poolFormat)
	}
	return 0, nil
}

// normalizePoolConfig silently zeroes PoolSize and PoolWinners for formats
// that have no pool phase. League has a single implicit pool containing all
// participants (engine overrides PoolSize at start time); playoffs is a direct
// elimination bracket with no pools at all. Keeping these fields set for those
// formats is misleading and inconsistent, so we discard them on ingest rather
// than rejecting the request (the engine already ignores them at runtime, and
// the mixed format is the only one where they carry meaning).
//
// Called in both the POST (create) and PUT (update/merge) paths so the API is
// consistent regardless of how the client sends the competition.
func normalizePoolConfig(comp *state.Competition) {
	switch comp.Format {
	case state.CompFormatLeague, state.CompFormatPlayoffs:
		comp.PoolSize = 0
		comp.PoolWinners = 0
	}
}

// validateSwissConfig enforces FR-050a: when Format == swiss, SwissRounds
// must be at least 1. Returns nil for non-swiss competitions. The caller
// surfaces the error as HTTP 400.
func validateSwissConfig(comp *state.Competition) error {
	if comp.Format != state.CompFormatSwiss {
		return nil
	}
	if comp.SwissRounds < 1 {
		return fmt.Errorf("swiss format requires swissRounds >= 1")
	}
	return nil
}

// validateLeagueTiebreakConfig validates the league-tiebreak configuration knobs
// (LeagueTiebreakTopN, LeagueTwoThirdPlaces). These fields are only meaningful
// for team-league competitions; they are silently ignored for other formats/kinds
// and must not cause errors there. Returns nil for non-league or non-team comps.
//
// Kind == "team" is the canonical team marker: ValidateCompetitionTeamSize (run
// on every create/edit) enforces Kind == "team" ⟺ TeamSize >= 2, so a comp with
// TeamSize > 0 but Kind == "" cannot be persisted, the Kind check alone is
// sufficient to identify a team league here.
func validateLeagueTiebreakConfig(comp *state.Competition) error {
	if comp.Format != state.CompFormatLeague || comp.Kind != "team" {
		return nil
	}
	switch comp.LeagueTiebreakTopN {
	case 0, 3, 4:
		// 0 = unset (will default to 3 at draw time); 3 and 4 are the only
		// legal explicit values (per owner decision Q1).
	default:
		return fmt.Errorf("leagueTiebreakTopN must be 3 or 4 (got %d)", comp.LeagueTiebreakTopN)
	}
	return nil
}

// validateCompetitionLengths enforces the persisted-string caps from
// validation.go on the settings-relevant string fields of comp. Called
// after trim. Returns the first *ValidationError on failure.
func validateCompetitionLengths(comp *state.Competition) error {
	if err := validateMaxLen("name", comp.Name, MaxLenCompetitionName); err != nil {
		return err
	}
	if err := validateMaxLen("numberPrefix", comp.NumberPrefix, MaxLenCompetitionNumberPrefix); err != nil {
		return err
	}
	if err := validateMaxLen("startTime", comp.StartTime, MaxLenCompetitionStartTime); err != nil {
		return err
	}
	if err := validateMaxLen("date", comp.Date, MaxLenCompetitionDate); err != nil {
		return err
	}
	return nil
}

// checkUniqueCompFields verifies that name and prefix are both unique across all
// competitions except excludeID in a single store pass. Returns (infraErr,
// validationErr): infraErr is non-nil when the store cannot be queried (caller
// should 500); validationErr is non-nil when a collision is detected (caller
// should 400). Empty prefix is exempt from the uniqueness check.
func checkUniqueCompFields(store *state.Store, name, prefix, excludeID string) (error, error) {
	prefix = strings.TrimSpace(prefix)
	ids, err := store.ListCompetitions()
	if err != nil {
		return fmt.Errorf("list competitions: %w", err), nil
	}
	for _, existingID := range ids {
		if existingID == excludeID {
			continue
		}
		existing, err := store.LoadCompetition(existingID)
		if err != nil {
			return fmt.Errorf("load competition %s: %w", existingID, err), nil
		}
		if existing == nil {
			continue
		}
		if strings.EqualFold(existing.Name, name) {
			return nil, fmt.Errorf("competition name %q already exists", name)
		}
		if prefix != "" && existing.NumberPrefix != "" &&
			strings.EqualFold(strings.TrimSpace(existing.NumberPrefix), prefix) {
			return nil, fmt.Errorf("number prefix %q already used by competition %q", prefix, existing.Name)
		}
	}
	return nil, nil
}

func RegisterCompetitionHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine, hub *Hub, elevated ElevatedVerifier) {
	r.GET("/competitions", func(c *gin.Context) {
		ids, err := store.ListCompetitions()
		if err != nil {
			internalError(c, err)
			return
		}

		comps := make([]*state.Competition, 0)
		for _, id := range ids {
			comp, err := store.LoadCompetition(id)
			if err == nil && comp != nil {
				comps = append(comps, comp)
			}
		}
		c.JSON(http.StatusOK, comps)
	})

	r.POST("/competitions", func(c *gin.Context) {
		var comp state.Competition
		if err := c.ShouldBindJSON(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		comp.Name = strings.TrimSpace(comp.Name)
		// Trim NumberPrefix too so untrimmed input from the SETTINGS edit
		// path can't land as "  A" / participants becoming "  A1" / etc.
		// Mirrors the comp.Name trim above (and the frontend trim in
		// admin_competition.jsx saveNow + admin_setup.jsx create).
		comp.NumberPrefix = strings.TrimSpace(comp.NumberPrefix)
		// Trim the remaining string fields too. Cross-file guard symmetry
		// with handlers_import.go (which trims all 7 string fields). The
		// admin UI sends these via dropdowns / time / date inputs that
		// don't pad, but a hand-crafted POST could send "  individual  "
		// as Kind, downstream switch statements would silently fall
		// through to "unknown kind" semantics.
		comp.Kind = strings.TrimSpace(comp.Kind)
		comp.Format = strings.TrimSpace(comp.Format)
		comp.PoolFormat = strings.TrimSpace(comp.PoolFormat)
		comp.PoolSizeMode = strings.TrimSpace(comp.PoolSizeMode)
		comp.StartTime = strings.TrimSpace(comp.StartTime)
		comp.Date = strings.TrimSpace(comp.Date)

		// Populate per-phase durations from the legacy MatchDuration field
		// for callers that still send `matchDuration` only. Idempotent on
		// modern callers that send both per-phase values.
		state.ApplyCompetitionDefaults(&comp)

		// Reject whitespace-only Name. The admin_setup.jsx Create form
		// validates this client-side (deriveCompetitionName + the
		// empty-name check), but hand-crafted POSTs with an explicit
		// `id` bypass the slugifyID empty-name fallback below, so
		// without this guard, an explicit-ID request with Name="   "
		// lands as Name="" on disk and renders a blank competition
		// card. Cross-file guard symmetry with handlers_tournament.go
		// (which rejects empty Name on PUT and POST).
		if comp.Name == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "competition name is required"})
			return
		}

		if err := validateCompetitionLengths(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Engi (individual PAIR paradigm) is mutually exclusive with team
		// competitions: engi is never a team (the score editor skips the team
		// check for engi and the scoring backend treats engi as non-team). The
		// admin UI hides the Engi toggle unless kind=individual, but reject the
		// contradictory combination here so a hand-crafted POST can't create a
		// team comp with engi=true and route its matches to the wrong scorer.
		if comp.Engi && (comp.Kind == "team" || comp.TeamSize > 0) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "engi is only valid for individual competitions, not team"})
			return
		}

		// POST /competitions can land with an embedded roster via
		// saveCompetitionWithPlayers, same required-field and length
		// caps as the roster-PUT branch and POST /participants.
		for i, p := range comp.Players {
			if err := validatePlayerRequired(p.Name, p.Dojo); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("players[%d]: %s", i, err.Error())})
				return
			}
			if err := validatePlayerLengths(p.Name, p.DisplayName, p.Dojo, p.Source, p.Metadata); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("players[%d]: %s", i, err.Error())})
				return
			}
			comp.Players[i].Source = helper.CanonicalRegistrationSource(p.Source)
		}

		// Reject non-canonical Date format. See validateDateDMY in
		// handlers_tournament.go for the canonical-format rationale.
		if err := validateDateDMY(comp.Date); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Load the tournament to (a) default the competition date to Day 1
		// and (b) enforce the date-in-range constraint. Tournament load can
		// return nil when no tournament.md exists yet (new setup); both steps
		// skip gracefully in that case.
		createTourn, err := store.LoadTournament()
		if err != nil {
			internalError(c, err)
			return
		}
		// Default the competition date to the tournament's Day 1 when the
		// client sends an empty date. Pre-fix: the frontend seeded the date
		// from tournament.date directly (JS), but the backend defaulted to
		// today when it was empty, using Day 1 is the correct multi-day
		// behaviour and also keeps server and client in sync.
		if comp.Date == "" && createTourn != nil && createTourn.Date != "" {
			comp.Date = createTourn.Date
		}

		// Reject a competition date that falls outside the tournament's day
		// range. Skipped when tournament has no date configured.
		if err := validateCompetitionDateInTournament(&comp, createTourn); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Cross-file guard symmetry with POST/PUT /tournament: same
		// label + cap check via validateCompetitionCourts (looser than
		// the tournament version: empty courts are allowed here because
		// they are immediately resolved to the tournament's courts via
		// resolveCompetitionCourts on the next line, so every match ends
		// up with a real court label). Defense against direct API callers
		// sending multi-character labels.
		if err := validateCompetitionCourts(comp.Courts); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "courts: " + err.Error()})
			return
		}

		// Guarantee >=1 court: empty competition courts inherit the
		// tournament's courts so every match carries a real court label
		// (otherwise the per-court Shiaijo operator view can't surface them).
		comp.Courts = resolveCompetitionCourts(comp.Courts, createTourn)

		// Reject negative per-phase or legacy durations.
		if err := validateCompetitionDurations(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Format / PoolFormat enum-style validation. Empty values are
		// accepted; unknown values 400. FR-050a: swiss is accepted but
		// validateSwissConfig must additionally enforce swissRounds >= 1.
		if code, err := validateCompetitionFormat(comp.Format, comp.PoolFormat); err != nil {
			c.JSON(code, gin.H{"error": err.Error()})
			return
		}

		// Zero out pool-config fields that have no meaning for the chosen
		// format (league / playoffs). The engine already ignores them at
		// start time; this keeps the persisted config consistent with what
		// the engine actually uses. See normalizePoolConfig for full rationale.
		normalizePoolConfig(&comp)

		// FR-050a: swiss-specific config validation.
		if err := validateSwissConfig(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// League tie-breaker config validation (leagueTiebreakTopN must be 0/3/4).
		if err := validateLeagueTiebreakConfig(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// TeamMatchType enum-style validation. Empty == fixed (default);
		// "kachinuki" requires TeamSize >= 2. FR-044.
		if err := state.ValidateTeamMatchType(comp.TeamMatchType, comp.TeamSize); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Team competitions require at least 2 members per team.
		if err := state.ValidateCompetitionTeamSize(comp.Kind, comp.TeamSize); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Derive ID from name BEFORE acquiring the rename lock, the ID
		// derivation has no concurrency concern (pure function of Name)
		// and an empty derived ID should fast-fail without holding the
		// global mutex.
		if comp.ID == "" {
			comp.ID = slugifyID(comp.Name)
			if comp.ID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "competition ID is required (could not derive one from name)"})
				return
			}
		}
		// Validate the derived OR caller-supplied ID upfront with a 400.
		// Without this, a non-empty but invalid ID (e.g. "../../etc/passwd"
		// or "foo bar") would skip the derive block, then LoadCompetition
		// would silently drop the validation error (`_, _:=`), and the
		// SaveCompetitionChanged inside saveCompetitionWithPlayers would
		// surface "invalid competition ID" as a 500, masking malformed
		// input as a server failure. The middleware.requireValidCompID
		// helper does the equivalent check for routes that take: id in
		// the URL; this is the body-supplied-id sibling.
		if err := state.ValidateCompetitionID(comp.ID); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		// Atomic uniqueness-check + save under the global
		// competition-rename mutex. Closes the AB-BA window where two
		// concurrent POSTs (or PUT renames) to the same new name both
		// passed checkUniqueCompFields (each seeing the other still had
		// its old name) and both landed. See state.Store
		// WithCompetitionRenameLock for full rationale.
		//
		// Also checks ID uniqueness: pre-fix, a POST with an existing
		// `id` but different `name` passed checkUniqueCompFields (the
		// name was unique) and then SaveCompetitionChanged silently
		// overwrote the existing competition. POST is documented as
		// CREATE, so an existing ID is a 409 / 400 case.
		var validationErr, idErr error
		lockErr := store.WithCompetitionRenameLock(func() error {
			if existing, _ := store.LoadCompetition(comp.ID); existing != nil {
				idErr = fmt.Errorf("competition ID %q already exists", comp.ID)
				return nil
			}
			var infraErr error
			infraErr, validationErr = checkUniqueCompFields(store, comp.Name, comp.NumberPrefix, "")
			if infraErr != nil {
				return infraErr
			}
			if validationErr != nil {
				return nil
			}
			_, saveErr := saveCompetitionWithPlayers(&comp, store)
			return saveErr
		})
		err = lockErr
		if idErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": idErr.Error()})
			return
		}
		if validationErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": validationErr.Error()})
			return
		}
		if err != nil {
			if errors.Is(err, state.ErrReservedName) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			if errors.Is(err, errSaveParticipants) {
				// Safe label to the client; the wrapped cause is logged, not returned.
				internalError(c, err, "failed to save participants")
				return
			}
			internalError(c, err)
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusCreated, comp)
	})

	r.GET("/competitions/:id", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil {
			internalError(c, err)
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		c.JSON(http.StatusOK, comp)
	})

	// GET /competitions/:id/schedule/estimate, pre-draw schedule estimate
	// for a specific competition. Read-only; main-password gated (registered
	// under adminGroup via RegisterCompetitionHandlers) but does NOT require
	// elevated auth. Returns a ScheduleEstimate JSON body; 404 for unknown
	// competition. mp-zoh Phase 3.
	r.GET("/competitions/:id/schedule/estimate", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		estimate, err := eng.EstimateScheduleForCompetition(id)
		if err != nil {
			var notFound *engine.NotFoundError
			var validation *engine.ValidationError
			if errors.As(err, &notFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			if errors.As(err, &validation) {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			internalError(c, err)
			return
		}
		c.JSON(http.StatusOK, estimate)
	})

	// GET /competitions/:id/schedule/clashes, court (shiaijo) scheduling
	// conflicts between this competition and every other one (same day, shared
	// court, overlapping time windows). Read-only, main-password gated. Returns
	// a (possibly empty) ClashWarning array; 404 for an unknown competition.
	// Non-blocking by design: the SPA surfaces these as a warning after a
	// settings save / create, it does not reject the save. (mp-4a52)
	r.GET("/competitions/:id/schedule/clashes", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		clashes, err := eng.DetectClashesForCompetition(id)
		if err != nil {
			var notFound *engine.NotFoundError
			if errors.As(err, &notFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			internalError(c, err)
			return
		}
		c.JSON(http.StatusOK, clashes)
	})

	r.PUT("/competitions/:id", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		var comp state.Competition
		if err := c.ShouldBindJSON(&comp); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Reject mismatched body.ID rather than silently overriding it.
		// Pre-fix, a caller doing `PUT /api/competitions/comp-a` with
		// `{"id":"comp-b",...}` would have its body.ID silently ignored
		// (the line below set comp.ID = id from the URL). That accepted
		// malformed input as valid; the safer contract is to surface
		// the mismatch.
		if comp.ID != "" && comp.ID != id {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("competition ID mismatch: URL %q vs body %q", id, comp.ID)})
			return
		}
		comp.ID = id // ensure ID matches URL (also for empty-body case)

		// Elevated-password gate for the ROSTER-mutation path (spec 004 /
		// mp-e21; Copilot PR #193). This handler doubles as a roster writer:
		// a non-nil Players field below triggers SaveParticipants/SaveSeeds.
		// That is the SPA's PRIMARY roster flow (paste/import, seed edits via
		// API.updateCompetition), so gating only the dedicated
		// POST/PUT /participants endpoints would leave the gate bypassable.
		// Route-level middleware can't see Players (it runs before binding),
		// so enforce inline now that the body is decoded. Settings-only PUTs
		// (Players == nil) are unaffected and stay single-factor.
		if comp.Players != nil && !enforceElevated(c, elevated) {
			return
		}

		comp.Name = strings.TrimSpace(comp.Name)
		// See POST handler comment, same trim is needed here so the
		// SETTINGS edit path can't persist whitespace-padded prefixes.
		comp.NumberPrefix = strings.TrimSpace(comp.NumberPrefix)
		// Cross-file guard symmetry with handlers_import.go and the POST
		// handler above. The admin UI uses dropdowns for Kind/Format/
		// PoolSizeMode and date/time pickers for StartTime/Date, none
		// of which produce padded values, but defense-in-depth against
		// hand-crafted PUT requests.
		comp.Kind = strings.TrimSpace(comp.Kind)
		comp.Format = strings.TrimSpace(comp.Format)
		comp.PoolFormat = strings.TrimSpace(comp.PoolFormat)
		comp.PoolSizeMode = strings.TrimSpace(comp.PoolSizeMode)
		comp.StartTime = strings.TrimSpace(comp.StartTime)
		comp.Date = strings.TrimSpace(comp.Date)

		// Roster-PUT length caps. Settings-only PUT's caps are gated
		// below behind `comp.Players == nil`; the roster-PUT path takes
		// the other branch in the transform and skips them, so check
		// player fields here unconditionally when a roster is being
		// saved. Mirrors the POST /participants validation in
		// handlers_participants.go.
		if comp.Players != nil {
			for i, p := range comp.Players {
				if err := validatePlayerRequired(p.Name, p.Dojo); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("players[%d]: %s", i, err.Error())})
					return
				}
				if err := validatePlayerLengths(p.Name, p.DisplayName, p.Dojo, p.Source, p.Metadata); err != nil {
					c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("players[%d]: %s", i, err.Error())})
					return
				}
				comp.Players[i].Source = helper.CanonicalRegistrationSource(p.Source)
			}
		}

		// Settings-specific validations, gate on comp.Players == nil
		// (settings-only PUT). Roster-only PUTs (comp.Players != nil)
		// carry a stale-snapshot of settings fields from the frontend
		// (AdminParticipants spreads `{ ...c, players: np }` over a
		// possibly outdated `c`), and those fields are IGNORED downstream
		// of the transform branch decision. Pre-fix, an on-disk
		// legacy/stale settings value (e.g. a pre-DMY-canonical Date
		// like `2026-05-12`) made the roster save fail with
		// "date must be DD-MM-YYYY" even though the date wasn't being
		// edited, the validators ran before the branch decision and
		// rejected on a field the request was about to ignore. Moving
		// these behind `comp.Players == nil` keeps the defense-in-depth
		// against bad settings PUTs and unblocks roster saves on legacy
		// state.
		if comp.Players == nil {
			// Reject whitespace-only Name (see POST handler above). The
			// admin SETTINGS edit path (AdminSettings.saveNow in
			// admin_competition.jsx) empty-checks the name client-side
			// first, defense-in-depth for direct API callers.
			if comp.Name == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "competition name is required"})
				return
			}

			if err := validateCompetitionLengths(&comp); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// Reject non-canonical Date format.
			if err := validateDateDMY(comp.Date); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// Enforce the competition-date-in-tournament-range constraint.
			// Load tournament outside the rename lock to avoid holding the
			// lock during I/O. Tournament load is read-only and idempotent,
			// so the window between load and the lock acquisition is safe
			// (the worst case is a missed tournament date update, which
			// would just skip the range check, a harmless skip vs. a
			// deadlock is the right trade-off).
			putTourn, putTournErr := store.LoadTournament()
			if putTournErr != nil {
				internalError(c, putTournErr)
				return
			}
			if err := validateCompetitionDateInTournament(&comp, putTourn); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// Cross-file guard symmetry with POST handler + POST/PUT /tournament:
			// validateCompetitionCourts label + cap check (empty allowed
			// because the engine applies a 1-court default for competitions).
			if err := validateCompetitionCourts(comp.Courts); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "courts: " + err.Error()})
				return
			}

			// Guarantee >=1 court: a settings PUT that clears courts (or a
			// direct API caller sending none) inherits the tournament's
			// courts so every match carries a real court label. The transform
			// below copies comp.Courts onto current for settings-only PUTs.
			comp.Courts = resolveCompetitionCourts(comp.Courts, putTourn)

			// Reject negative per-phase or legacy durations.
			if err := validateCompetitionDurations(&comp); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// Format / PoolFormat enum-style validation. Empty values are
			// accepted; unknown values 400. FR-050a: swiss requires the
			// additional swissRounds >= 1 check below.
			if code, err := validateCompetitionFormat(comp.Format, comp.PoolFormat); err != nil {
				c.JSON(code, gin.H{"error": err.Error()})
				return
			}

			// Zero out pool-config fields that have no meaning for the
			// chosen format. Mirrors the same call in the POST handler.
			normalizePoolConfig(&comp)

			// FR-050a: swiss-specific config validation.
			if err := validateSwissConfig(&comp); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// League tie-breaker config validation (leagueTiebreakTopN must be 0/3/4).
			if err := validateLeagueTiebreakConfig(&comp); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// TeamMatchType enum-style validation. Empty == fixed (default);
			// "kachinuki" requires TeamSize >= 2. FR-044. Settings-only PUT
			// (comp.Players == nil): roster-only PUTs carry a stale
			// snapshot of TeamMatchType from the frontend which is
			// ignored downstream (the transform only copies whitelisted
			// settings fields), matching the gate logic for the other
			// validators in this block.
			if err := state.ValidateTeamMatchType(comp.TeamMatchType, comp.TeamSize); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}

			// Team competitions require at least 2 members per team.
			if err := state.ValidateCompetitionTeamSize(comp.Kind, comp.TeamSize); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
		}

		// Atomic uniqueness-check + 404-on-missing + settings-only merge
		// + save under the global competition-rename mutex.
		//
		// Three invariants enforced here:
		//
		// 1. AB-BA rename race closure: two concurrent PUTs renaming
		//    different competitions to the same new name both passed
		//    checkUniqueCompFields pre-fix (each seeing the other still
		//    had its old name) and both landed. The dedicated rename
		//    mutex (different from any per-comp lock) serializes the
		//    check+save for uniqueness. An earlier attempt folded the
		//    check into UpdateCompetitionChanged's per-comp transform,
		//    deadlocked AB-BA because each goroutine held its own
		//    comp's per-comp write lock and tried to read-lock the
		//    other to do the check.
		//
		// 2. 404 on missing: pre-fix, saveCompetitionWithPlayers
		//    would CREATE the record if id didn't exist on disk,
		//    contradicting the OpenAPI-documented 404 response and
		//    surprising clients that expected idempotent "edit only".
		//    UpdateCompetitionChanged's transform now returns
		//    notFoundFlag for current == nil.
		//
		// 3. Settings-only merge for non-participants fields: pre-fix,
		//    the PUT body REPLACED the whole config, including
		//    Status / HasParticipantIDs that AdminSettings doesn't
		//    manage. If the JSON omitted Status (e.g. the new
		//    admin_competition.jsx saveNow whitelist that genuinely
		//    sends settings-only), the saved record would have
		//    status="" / hasParticipantIDs=false, reverting server-side
		//    start / participant changes. The transform copies ONLY the
		//    settings fields from the body onto current, so Status /
		//    HasParticipantIDs stay as they are on disk regardless of
		//    body contents. Defense-in-depth for direct API callers too.
		//
		// AdminParticipants STILL uses this same PUT to save the roster
		// (`{ ...c, players: np }`): when the body has Players, we run
		// the participants/seeds save AFTER the transform commits and
		// set HasParticipantIDs=true (saveParticipants writes UUID rows).
		var validationErr error
		var notFoundFlag bool
		var drawReadyFlag bool
		var changed bool
		err := store.WithCompetitionRenameLock(func() error {
			var updateErr error
			changed, updateErr = store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
				if current == nil {
					notFoundFlag = true
					return nil, nil
				}
				// Roster-only PUT: when the body carries a Players field
				// (present, possibly empty), treat the request as
				// participants-only and DON'T touch settings. The
				// AdminParticipants flow sends `{ ...c, players: np }`
				// where `c` is a (potentially stale) frontend snapshot
				// of the competition; copying every settings field from
				// that snapshot onto a fresh `current` would revert any
				// concurrent settings change that landed after the
				// participants page loaded its `c` snapshot (e.g. an
				// admin in another tab adjusts poolSize / courts /
				// startTime). The trailing SaveParticipants block runs
				// regardless via the post-transform gate
				// `if comp.Players != nil`.
				//
				// Settings updates use AdminSettings which OMITS the
				// players field (decodes to nil in Go) and takes the
				// settings-merge branch below. With this branch split,
				// settings-PUT and roster-PUT no longer step on each
				// other's writes.
				if comp.Players != nil {
					if current.Status == state.CompStatusDrawReady {
						drawReadyFlag = true
						return nil, nil
					}
					// Roster-only PUT, do NOT flip HasParticipantIDs
					// here. Pre-fix, the transform committed the flag
					// (HasParticipantIDs=true) BEFORE the post-transform
					// SaveParticipants call. If that save failed (disk
					// full, EISDIR, etc.) the config on disk would carry
					// HasParticipantIDs=true while participants.csv
					// retained the OLD non-UUID format, and the
					// list-view's HasIDs hint would then misparse the
					// file (trying to extract UUID prefix from each
					// non-UUID row). Defer the flag flip to the
					// post-transform block AFTER SaveParticipants
					// succeeds; see the participants/seeds save block
					// below for the deferred flip.
					return current, nil
				}

				// Settings-only PUT (Players field absent in body).
				// Draw-ready gate: the draw artifacts (pools.csv /
				// bracket.json) were generated from the current config.
				// Mutating output-affecting fields while draw-ready would
				// leave config.md inconsistent with those artifacts when
				// StartCompetition runs. Fields that do NOT reach the Excel
				// generator (Name, Date, StartTime, CheckInEnabled) stay
				// editable in draw-ready and are applied below. NOTE:
				// NumberPrefix and WithZekkenName DO reach the generator
				// (player numbers / name columns) and are gated below.
				// Naginata and Engi are NOT editable in draw-ready either: the
				// `started` guard below (current.Status != setup) treats
				// draw-ready as started, so a change to them is rejected. This
				// mirrors the participant/seed 409s in handlers_participants.go.
				if current.Status == state.CompStatusDrawReady {
					// Compare the EFFECTIVE (about-to-be-applied) values
					// directly, no zero/empty sentinels. The settings merge
					// below is a full replace, so a caller that sets an
					// output-affecting field TO its zero/empty value (e.g.
					// format:"" or poolFormat:"", both accepted by
					// validateCompetitionFormat) would otherwise slip past a
					// sentinel guard and corrupt the draw. The real client
					// (admin_competition_settings.jsx finalNext) always PUTs
					// the full config with current values for untouched fields,
					// so a cosmetic-only edit never trips this. comp.Courts was
					// already defaulted to >=1 court (tournament fallback) in the
					// settings-validation block above. ApplyCompetitionDefaults
					// touches only match-duration fields, so comparing here
					// (pre-defaults) matches the merged result for these fields.
					outputAffectingChanged :=
						comp.PoolSize != current.PoolSize ||
							comp.PoolWinners != current.PoolWinners ||
							comp.PoolSizeMode != current.PoolSizeMode ||
							strings.Join(comp.Courts, ",") != strings.Join(current.Courts, ",") ||
							comp.Format != current.Format ||
							comp.PoolFormat != current.PoolFormat ||
							comp.RoundRobin != current.RoundRobin ||
							comp.Mirror != current.Mirror ||
							comp.TeamSize != current.TeamSize ||
							comp.Kind != current.Kind ||
							// NumberPrefix and WithZekkenName reach the Excel generator
							// (POST /create: numberPrefix → player numbers, withZekkenName
							// → name columns), so changing them while draw-ready desyncs
							// config from the generated output.
							comp.NumberPrefix != current.NumberPrefix ||
							comp.WithZekkenName != current.WithZekkenName
					if outputAffectingChanged {
						drawReadyFlag = true
						return nil, nil
					}
				}
				// Existence first, uniqueness second. Pre-fix order ran
				// checkUniqueCompFields BEFORE the transform, so a PUT to
				// a missing: id whose body Name happened to collide with
				// an existing competition would 400 "name already exists"
				// instead of the documented 404 missing. Folding the
				// check into the transform, after current == nil, is
				// safe under WithCompetitionRenameLock: the rename mutex
				// serializes rename ops, so the LoadCompetition calls on
				// OTHER comp IDs that checkUniqueCompFields performs can't
				// race a concurrent rename of those comps (see store.go
				// "Lock ordering note" on WithCompetitionRenameLock).
				var infraErr error
				infraErr, validationErr = checkUniqueCompFields(store, comp.Name, comp.NumberPrefix, id)
				if infraErr != nil {
					return nil, infraErr
				}
				if validationErr != nil {
					return nil, nil
				}
				// Populate per-phase durations from legacy MatchDuration
				// when only the legacy field was supplied. Idempotent.
				// Runs INSIDE the transform so we can copy the resolved
				// per-phase values straight into `current` below.
				state.ApplyCompetitionDefaults(&comp)

				// Settings-only merge. Status, Players, and
				// HasParticipantIDs are deliberately not copied from
				// the body. Status is managed via dedicated endpoints
				// (start/complete/invalidate). Players is persisted
				// separately to participants.csv (see post-transform
				// block below). HasParticipantIDs is auto-managed,
				// only set to true in the roster-only branch above when
				// participants are being saved.
				current.Name = comp.Name
				current.Date = comp.Date
				current.StartTime = comp.StartTime
				current.PoolSize = comp.PoolSize
				current.PoolWinners = comp.PoolWinners
				current.PoolSizeMode = comp.PoolSizeMode
				// comp.Courts was already defaulted to >=1 court (tournament
				// fallback) in the settings-validation block above.
				current.Courts = comp.Courts
				current.RoundRobin = comp.RoundRobin
				current.WithZekkenName = comp.WithZekkenName
				current.TeamSize = comp.TeamSize
				current.NumberPrefix = comp.NumberPrefix
				current.Format = comp.Format
				current.PoolFormat = comp.PoolFormat
				current.Kind = comp.Kind
				current.Mirror = comp.Mirror
				current.PoolMatchDuration = comp.PoolMatchDuration
				current.PlayoffMatchDuration = comp.PlayoffMatchDuration
				current.MatchDuration = comp.MatchDuration
				current.TeamMatchType = comp.TeamMatchType
				// FR-050a: swiss round budget is admin-editable from
				// settings until the competition starts (the engine
				// gates StartCompetition on Status=setup). After start,
				// the field is read-only via the same Status gate.
				current.SwissRounds = comp.SwissRounds
				// Naginata (3rd-place play-off) is only settable before the
				// competition starts. Changing it after start would add or remove a
				// bronze match while results are already in flight. Reject rather than
				// silently ignoring it (Finding 8; mirrors the Engi guard below).
				started := current.Status != state.CompStatusSetup && current.Status != ""
				if started && comp.Naginata != current.Naginata {
					validationErr = fmt.Errorf("naginata can only be changed before the competition starts")
					return nil, nil
				}
				current.Naginata = comp.Naginata
				// Engi (flag-scoring paradigm) is only settable before the
				// competition starts. Flipping it mid-tournament switches the
				// scoring paradigm and corrupts recorded results. Reject a
				// change rather than silently ignoring it.
				if started && comp.Engi != current.Engi {
					validationErr = fmt.Errorf("engi can only be changed before the competition starts")
					return nil, nil
				}
				// Engi (individual PAIR paradigm) is mutually exclusive with team
				// competitions (engi is never a team). Kind/TeamSize were already
				// applied above, so reject rather than persist a contradictory
				// team+engi state that would route matches to the wrong scorer.
				if comp.Engi && (comp.Kind == "team" || comp.TeamSize > 0) {
					validationErr = fmt.Errorf("engi is only valid for individual competitions, not team")
					return nil, nil
				}
				current.Engi = comp.Engi
				current.CheckInEnabled = comp.CheckInEnabled
				// League tie-breaker config (Phase 3b) is only settable pre-start.
				// Once the competition has started (status past setup) the
				// consequential-tie set is in play, so changing leagueTiebreakTopN
				// or leagueTwoThirdPlaces could re-block or unblock completion and
				// change which ties already-played tie-breakers were meant to resolve.
				// Reject a change rather than silently ignoring it. The draw-ready
				// state already returned early above; the PUT validator enforces
				// LeagueTiebreakTopN ∈ {0,3,4}. LeagueTiebreakFinalized is managed by
				// the finalize endpoint, never here.
				if started && (comp.LeagueTiebreakTopN != current.LeagueTiebreakTopN ||
					comp.LeagueTwoThirdPlaces != current.LeagueTwoThirdPlaces) {
					validationErr = fmt.Errorf("leagueTiebreakTopN and leagueTwoThirdPlaces can only be changed before the competition starts")
					return nil, nil
				}
				current.LeagueTiebreakTopN = comp.LeagueTiebreakTopN
				current.LeagueTwoThirdPlaces = comp.LeagueTwoThirdPlaces
				return current, nil
			})
			return updateErr
		})
		// 404 before 400, with the uniqueness check now inside the
		// transform (after current == nil), notFoundFlag and validationErr
		// are mutually exclusive. Order kept defensive in case either flag
		// escapes the transform unexpectedly.
		if notFoundFlag {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if drawReadyFlag {
			c.JSON(http.StatusConflict, gin.H{"error": "cannot modify output-affecting settings (format, courts, pool size/winners/mode, pool format, round-robin, mirror, team size, kind, number prefix, zekken display) while a draw is pending; discard the draw first"})
			return
		}
		if validationErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": validationErr.Error()})
			return
		}
		if err != nil {
			internalError(c, err)
			return
		}
		// Participants/seeds save (separate file), runs whenever the
		// PUT body PRESENT (non-nil) Players field, including an
		// explicit empty `players: []` payload to CLEAR the roster.
		// AdminParticipants uses `{ ...c, players: np }` to either
		// populate or clear; AdminSettings's saveNow allowlist OMITS
		// the players field entirely (decodes to nil in Go), so it
		// skips this block.
		//
		// nil vs empty matters: a `null` / omitted players field
		// (settings-only PUT) decodes to nil and must NOT touch
		// participants.csv. An explicit `[]` from "clear roster"
		// decodes to a non-nil zero-length slice and DOES need to
		// land an empty participants.csv + clear seeds.csv. Pre-fix
		// `len > 0` collapsed both into "skip," leaving the prior
		// roster on disk even though the UI reported "Saved 0
		// participants."
		participantsChanged := false
		// Tier-2 near-duplicate warnings (non-blocking). Computed server-side
		// so the PUT roster path, the SPA's primary import flow, is the
		// authoritative source; attached to the response below.
		nearDupWarnings := []helper.NearDupWarning{}
		if comp.Players != nil {
			entries := make([][2]string, len(comp.Players))
			for i, p := range comp.Players {
				entries[i] = [2]string{p.Name, p.Dojo}
			}
			nearDupWarnings = helper.FindNearDupWarnings(entries)
			if err := store.SaveParticipants(id, comp.Players); err != nil {
				// Tier-1: a perfect (name, dojo) duplicate is a client error,
				// not a server fault, surface it as 409 so the operator sees
				// which entry collided.
				if errors.Is(err, state.ErrDuplicateName) {
					c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
					return
				}
				if errors.Is(err, state.ErrReservedName) {
					c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
					return
				}
				internalError(c, err, "failed to save participants")
				return
			}
			assignments := extractSeeds(comp.Players)
			if err := store.SaveSeeds(id, assignments); err != nil {
				fmt.Printf("Warning: failed to save seeds: %v\n", err)
			}
			// Deferred HasParticipantIDs flip, runs ONLY after the
			// participants file lands successfully. See the roster-only
			// branch in the transform above for the pre-fix bug shape
			// (flag committed before save → mismatch on disk when save
			// failed). A second transform here is cheap (metadata-only)
			// and runs under the per-comp lock, so subsequent loads see
			// a consistent (flag, file) pair.
			//
			// mp-p7n / Copilot PR #185 round-6: this flip is now part
			// of the roster-write contract, not best-effort. With
			// loadParticipantsNoLock's default branch keyed off
			// Competition.HasParticipantIDs, a stale `false` flag on
			// disk causes every subsequent no-hint reader (viewer
			// list/detail, engine StartCompetition, etc.) to fall back
			// to uuidRE-on-row-0 and mis-classify preserved non-UUID
			// ids as "no ids" → column shift. The previous "log and
			// continue" rationale was based on the older readers that
			// derived the hint per-record from uuidRE; that's no
			// longer how the load works. If the flip fails, return
			// 500 so the operator retries (idempotent, the same body
			// re-applied will re-save the file and re-attempt the
			// flip).
			if len(comp.Players) > 0 {
				if fierr := flipHasParticipantIDs(store, id); fierr != nil {
					internalError(c, fierr, "roster saved but failed to update HasParticipantIDs flag; retry the request (idempotent)")
					return
				}
			}
			participantsChanged = true
		}
		if changed || participantsChanged {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		// Re-load to return the actual on-disk state (with non-settings
		// fields preserved from current) rather than the partial body.
		// LoadCompetition doesn't repopulate Players from participants.csv,
		// so we ALWAYS need to populate Players on the response, otherwise
		// admin.jsx's updateCompetition merges `{ ...c, ...updated }` with
		// `updated.players: null` (Go nil slice → JSON null), wiping the
		// frontend's local roster and crashing render paths that read
		// `c.players.length`.
		updated, _ := store.LoadCompetition(id)
		if updated == nil {
			c.JSON(http.StatusOK, comp)
			return
		}
		if comp.Players != nil {
			// Roster-PUT: re-load from disk so the response reflects what
			// actually landed in participants.csv, the canonical on-disk
			// shape after the save round-trip (merged seeds, tag column,
			// any empty-id rows that the saver minted a fresh UUID for).
			// mp-p7n: client-supplied ids (UUID or not) are preserved
			// verbatim on save, and the loader strips column 0 by trusting
			// HasParticipantIDs, so the re-loaded ids match what the client
			// sent, no normalisation churn.
			//
			// AdminParticipants's clear-roster path sends [] and the
			// re-loaded roster will also be [] (LoadParticipants returns
			// an empty slice for an empty file), so the cleared-roster
			// contract still holds.
			//
			// mp-p7n: pass HasIDs=&true explicitly when we just
			// persisted a non-empty roster. We can rely on the
			// HasParticipantIDs flag being already set on disk by
			// this point (round-6 made the flip part of the contract,
			// flip failures return 500 above and never reach this
			// reload), so the loader's default branch would resolve
			// the same way. The explicit hint is purely declarative:
			// it pins the call-site invariant ("we just wrote a non-
			// empty roster, every row has an id in column 0") at
			// the reader, so future refactors that move or weaken
			// the flip guarantee can't silently regress this reload
			// to the no-hint auto-detect path.
			loadOpts := state.LoadParticipantsOpts{WithSeeds: true}
			if len(comp.Players) > 0 {
				trueP := true
				loadOpts.HasIDs = &trueP
			}
			if players, lerr := store.LoadParticipantsOpt(id, updated.WithZekkenName, loadOpts); lerr == nil {
				updated.Players = players
			} else {
				fmt.Printf("Warning: PUT /api/competitions/%s, failed to re-load participants for roster-PUT response (falling back to request body): %v\n", id, lerr)
				updated.Players = comp.Players // fallback: echo body
			}
		} else {
			// Settings-only PUT, load the on-disk roster for the
			// response so the merge doesn't push null into local state.
			// Falling back to an empty slice on load failure is safer
			// than nil (which JSON-encodes as null).
			if players, lerr := store.LoadParticipants(id, updated.WithZekkenName); lerr == nil {
				updated.Players = players
			} else {
				fmt.Printf("Warning: failed to load participants for settings-PUT response: %v\n", lerr)
				updated.Players = []domain.Player{}
			}
		}
		// Embed the competition fields inline and add the near-duplicate
		// warnings (empty array on the settings-only path). The embedded
		// pointer promotes the competition's JSON fields to the top level,
		// so existing clients see the same shape plus a `warnings` field.
		c.JSON(http.StatusOK, struct {
			*state.Competition
			Warnings []helper.NearDupWarning `json:"warnings"`
		}{Competition: updated, Warnings: nearDupWarnings})
	})

	r.DELETE("/competitions/:id", RequireElevatedPassword(elevated), func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		// If the config loads cleanly, gate on status. If it doesn't load
		// (corrupt or unparseable config.md), fall through to delete so the
		// operator can recover from a broken competition.
		if comp, err := store.LoadCompetition(id); err == nil && comp != nil {
			switch comp.Status {
			case state.CompStatusPools, state.CompStatusPlayoffs:
				c.JSON(http.StatusConflict, gin.H{"error": "competition is in progress; mark it invalid before deleting"})
				return
			}
		}
		if err := store.DeleteCompetition(id); err != nil {
			internalError(c, err)
			return
		}

		hub.Broadcast(EventTournamentUpdated, nil)
		c.Status(http.StatusNoContent)
	})

	r.POST("/competitions/:id/invalidate", RequireElevatedPassword(elevated), func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		// Atomic Load + Status check + Save. Pre-fix, the
		// LoadCompetition + saveCompetitionWithPlayers sequence had
		// a TOCTOU window: a concurrent
		// MaybeAutoCompletePools (triggered by a score-save from the
		// last pool match) could move Status to "complete" between
		// our read and write, admin's "invalidate" would then
		// silently revert to "complete".
		var compOut *state.Competition
		var statusErr error
		var notFoundFlag bool
		changed, err := store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
			if current == nil {
				notFoundFlag = true
				return nil, nil
			}
			if current.Status != state.CompStatusPools && current.Status != state.CompStatusPlayoffs {
				statusErr = fmt.Errorf("only in-progress competitions can be invalidated (current status: %q)", current.Status)
				return nil, nil
			}
			current.Status = state.CompStatusInvalid
			compOut = current
			return current, nil
		})
		if notFoundFlag {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if statusErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": statusErr.Error()})
			return
		}
		if err != nil {
			internalError(c, err)
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.JSON(http.StatusOK, compOut)
	})

	r.POST("/competitions/:id/start", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		if err := eng.StartCompetition(id); err != nil {
			var notFound *engine.NotFoundError
			var validation *engine.ValidationError
			switch {
			case errors.As(err, &notFound):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validation):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				internalError(c, err)
			}
			return
		}

		comp, err := store.LoadCompetition(id)
		if err != nil {
			internalError(c, err, "competition started but failed to load updated state")
			return
		}

		hub.Broadcast(EventCompetitionStarted, gin.H{"competitionId": id})

		// A pools competition that generated zero matches (e.g. single
		// participant) has nothing to score, so trip the auto-complete check
		// at start time. The non-zero case will trip via score handlers.
		// Same sanitized-header contract as tryAutoCompletePools, see
		// AutoCompleteErrorHeader/Value in hub.go.
		if outcome, err := eng.MaybeAutoCompletePools(id); err != nil {
			log.Printf("MaybeAutoCompletePools(%s) after start: %v", id, err)
			c.Header(AutoCompleteErrorHeader, AutoCompleteErrorValue)
		} else if outcome == engine.AutoCompleteTransitioned {
			hub.Broadcast(EventCompetitionCompleted, gin.H{"competitionId": id})
			// Reflect the auto-complete in the response body so the caller doesn't
			// see a stale "pools" status. The persisted file is already updated.
			comp.Status = state.CompStatusComplete
		}

		c.JSON(http.StatusOK, comp)
	})

	r.POST("/competitions/:id/generate-draw", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		if err := eng.GenerateDraw(id); err != nil {
			var notFound *engine.NotFoundError
			var validation *engine.ValidationError
			switch {
			case errors.As(err, &notFound):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validation):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				internalError(c, err)
			}
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil {
			internalError(c, err, "draw generated but failed to load updated state")
			return
		}
		if comp == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "draw generated but competition no longer exists"})
			return
		}
		hub.Broadcast(EventDrawGenerated, gin.H{"competitionId": id})
		c.JSON(http.StatusOK, comp)
	})

	r.DELETE("/competitions/:id/draw", RequireElevatedPassword(elevated), func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		if err := eng.DiscardDraw(id); err != nil {
			var notFound *engine.NotFoundError
			var validation *engine.ValidationError
			switch {
			case errors.As(err, &notFound):
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			case errors.As(err, &validation):
				c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			default:
				internalError(c, err)
			}
			return
		}
		hub.Broadcast(EventDrawDiscarded, gin.H{"competitionId": id})
		c.Status(http.StatusNoContent)
	})

	// Elevated-gated: completing a competition is IRREVERSIBLE (invalidate
	// rejects a completed comp), like DELETE /competitions/:id and
	// /invalidate. In file mode without an admin password the gate is a no-op.
	r.POST("/competitions/:id/complete", RequireElevatedPassword(elevated), func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		// Full-bracket-completion gate + atomic status transition, all under ONE
		// per-comp write lock via WithTransaction. A bracket-format competition
		// (playoffs, or a mixed comp's knockout) must have EVERY must-play match
		// completed -- all rounds plus the naginata bronze (ThirdPlaceMatch, a
		// sibling of Rounds) -- before it can be sealed, else the Awards podium
		// would show an unplayed final/bronze. The JS "Complete competition"
		// button enforces this (bracketFullyComplete), but a direct API call
		// would bypass it, so the server is the authority.
		//
		// Reading the bracket via tx.LoadBracket INSIDE the transaction closes a
		// TOCTOU window: a bare LoadBracket + a separate status write could
		// otherwise observe a stale "incomplete" snapshot (a concurrent
		// final-match score landing between the two) and spuriously reject
		// completion. tx.LoadBracket reuses the already-held lock; the public
		// Store.LoadBracket would deadlock the non-reentrant mutex.
		//
		// len(Rounds) > 0 skips bracketless formats (league/swiss/pools complete
		// via their own path -- no bracket.json, which LoadBracket maps to an
		// empty bracket + nil error). Completion is IRREVERSIBLE, so a genuine
		// I/O/parse fault fails CLOSED (500) rather than sealing with unknown
		// bracket state.
		var compOut *state.Competition
		var statusErr, bracketFault error
		var notFoundFlag bool
		txErr := store.WithTransaction(id, func(tx state.StoreTx) error {
			current, lerr := tx.LoadCompetition(id)
			if lerr != nil {
				return lerr
			}
			if current == nil {
				notFoundFlag = true
				return nil
			}
			if current.Status != state.CompStatusPools && current.Status != state.CompStatusPlayoffs {
				statusErr = fmt.Errorf("competition cannot be completed from status %q", current.Status)
				return nil
			}
			bracket, berr := tx.LoadBracket(id)
			if berr != nil {
				bracketFault = berr
				return nil
			}
			if bracket != nil && len(bracket.Rounds) > 0 && !bracketFullyComplete(bracket) {
				statusErr = fmt.Errorf("all bracket matches must be completed before this competition can be finished")
				return nil
			}
			current.Status = state.CompStatusComplete
			compOut = current
			return tx.SaveCompetition(current)
		})
		if notFoundFlag {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if bracketFault != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to load bracket"})
			return
		}
		if statusErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": statusErr.Error()})
			return
		}
		if txErr != nil {
			internalError(c, txErr)
			return
		}
		// Reaching here means the status transitioned (pools/playoffs -> complete),
		// always a real content change, so broadcast unconditionally.
		hub.Broadcast(EventTournamentUpdated, nil)
		c.JSON(http.StatusOK, compOut)
	})

	r.GET("/competitions/:id/export", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		data, err := eng.ExportCompetitionXlsx(id)
		if err != nil {
			internalError(c, err)
			return
		}

		filename := fmt.Sprintf("bracket-%s.xlsx", id)
		c.Header("Content-Description", "File Transfer")
		c.Header("Content-Disposition", "attachment; filename="+filename)
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
	})

	r.PUT("/competitions/:id/pools/:poolId/override-rank", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		poolId := c.Param("poolId")
		var req struct {
			PlayerName string `json:"playerName"`
			Rank       int    `json:"rank"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		// Defense-in-depth: the JS client already guards isNaN/<=0, but a stale
		// or hand-crafted request could persist garbage rank values. Reject
		// non-positive ranks here. Trim whitespace from the player name so
		// "   " doesn't slip through the empty check and so padded names
		// don't create keys that miss later lookups.
		playerName := strings.TrimSpace(req.PlayerName)
		if playerName == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "playerName is required"})
			return
		}
		if err := validateMaxLen("playerName", playerName, MaxLenPlayerName); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if req.Rank <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "rank must be a positive integer"})
			return
		}
		// Absolute overflow guard, defense-in-depth against weird
		// stale-pool or LoadPools-error edge cases. The real semantic
		// validation against the pool's actual size happens below.
		if req.Rank > helper.MaxRankOverride {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("rank must be a positive integer ≤ %d", helper.MaxRankOverride)})
			return
		}
		comp, err := store.LoadCompetition(id)
		if err != nil {
			internalError(c, err, "failed to load competition")
			return
		}
		if comp == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if comp.Status != state.CompStatusPools {
			c.JSON(http.StatusConflict, gin.H{"error": "rank overrides only accepted while competition is in pools stage"})
			return
		}
		// Pool-size validation: rank within a pool is bounded by the
		// number of players in that pool. Load the comp's pools and
		// look up the target pool by name (the URL: poolId matches
		// Pool.PoolName). Pre-fix, the only check was an absolute 1000
		// cap which let a stale/hand-crafted request store
		// rank=500 against a 4-player pool, meaningless override
		// values were silently accepted. Cost: one LoadPools per
		// override request. Rank overrides are rare admin actions, so
		// the extra read is negligible.
		pools, err := store.LoadPools(id)
		if err != nil {
			internalError(c, err, "failed to load pools")
			return
		}
		var targetPool *helper.Pool
		for i := range pools {
			if pools[i].PoolName == poolId {
				targetPool = &pools[i]
				break
			}
		}
		if targetPool == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("pool %q not found in competition %q", poolId, id)})
			return
		}
		poolSize := len(targetPool.Players)
		if req.Rank > poolSize {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("rank %d exceeds pool size %d", req.Rank, poolSize)})
			return
		}

		changed, err := store.SaveRankOverrideChanged(id, poolId, playerName, req.Rank)
		if err != nil {
			internalError(c, err)
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.Status(http.StatusOK)
	})

	// GET /competitions/:id/chusen-candidates
	// Consequential team-pool ties the daihyosen left undetermined (a cycle /
	// all-drawn); the operator settles each by chusen (drawing lots), recorded via
	// the override-rank endpoint. Admin-gated: RegisterCompetitionHandlers is
	// mounted under the authenticated router group, so callers must send the
	// tournament password header (unlike the public league-tiebreak candidates).
	r.GET("/competitions/:id/chusen-candidates", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		candidates, err := eng.ChusenCandidates(id)
		if err != nil {
			var notFound *engine.NotFoundError
			if errors.As(err, &notFound) {
				c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
				return
			}
			// Recorded on the context (not returned to the caller) so the
			// root cause is still visible in server logs, consistent with the
			// other opaque-500 handlers in this PR.
			_ = c.Error(err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
			return
		}
		out := make([]gin.H, 0, len(candidates))
		for _, g := range candidates {
			names := make([]string, len(g.Teams))
			for i, t := range g.Teams {
				names[i] = t.Player.Name
			}
			out = append(out, gin.H{"poolName": g.PoolName, "teamNames": names, "minPosition": g.MinPosition})
		}
		c.JSON(http.StatusOK, gin.H{"candidates": out})
	})

	r.PUT("/competitions/:id/schedule", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		var entries []state.ScheduleEntry
		if err := c.ShouldBindJSON(&entries); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		changed, err := store.SaveScheduleChanged(id, entries)
		if err != nil {
			internalError(c, err)
			return
		}
		if changed {
			hub.Broadcast(EventScheduleUpdated, nil)
		}
		c.Status(http.StatusOK)
	})

	r.DELETE("/competitions/:id/overrides", RequireElevatedPassword(elevated), func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}
		changed, err := store.ResetOverridesChanged(id)
		if err != nil {
			internalError(c, err)
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.Status(http.StatusNoContent)
	})

	r.PUT("/competitions/:id/awards", RequireElevatedPassword(elevated), func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		// Pointer slice distinguishes "field omitted" (nil) from "explicit
		// empty array" ([]). The field is documented required (OpenAPI
		// required: [fightingSpiritAwards]); a body of `{}` must 400 rather
		// than silently clear the list, while an explicit [] clears it.
		var body struct {
			FightingSpiritAwards *[]state.FightingSpiritAward `json:"fightingSpiritAwards"`
		}
		if err := c.ShouldBindJSON(&body); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if body.FightingSpiritAwards == nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "fightingSpiritAwards is required (use an empty array to clear all awards)"})
			return
		}

		awards := *body.FightingSpiritAwards
		if len(awards) > MaxFightingSpiritAwards {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("fightingSpiritAwards must not exceed %d entries", MaxFightingSpiritAwards)})
			return
		}

		// Validate and trim each award before persisting.
		trimmed := make([]state.FightingSpiritAward, 0, len(awards))
		for i, a := range awards {
			title := strings.TrimSpace(a.Title)
			recipientName := strings.TrimSpace(a.RecipientName)
			recipientDojo := strings.TrimSpace(a.RecipientDojo)

			if title == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("fightingSpiritAwards[%d]: title is required", i)})
				return
			}
			if recipientName == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("fightingSpiritAwards[%d]: recipientName is required", i)})
				return
			}
			if err := validateMaxLen("title", title, MaxLenPlayerName); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("fightingSpiritAwards[%d]: %s", i, err.Error())})
				return
			}
			if err := validateMaxLen("recipientName", recipientName, MaxLenPlayerName); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("fightingSpiritAwards[%d]: %s", i, err.Error())})
				return
			}
			if err := validateMaxLen("recipientDojo", recipientDojo, MaxLenPlayerDojo); err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("fightingSpiritAwards[%d]: %s", i, err.Error())})
				return
			}
			trimmed = append(trimmed, state.FightingSpiritAward{
				Title:         title,
				RecipientName: recipientName,
				RecipientDojo: recipientDojo,
			})
		}

		var compOut *state.Competition
		var notFoundFlag bool
		changed, err := store.UpdateCompetitionChanged(id, func(current *state.Competition) (*state.Competition, error) {
			if current == nil {
				notFoundFlag = true
				return nil, nil
			}
			current.FightingSpiritAwards = trimmed
			compOut = current
			return current, nil
		})
		if notFoundFlag {
			c.JSON(http.StatusNotFound, gin.H{"error": "competition not found"})
			return
		}
		if err != nil {
			internalError(c, err)
			return
		}
		if changed {
			hub.Broadcast(EventTournamentUpdated, nil)
		}
		c.JSON(http.StatusOK, compOut)
	})
}

// bracketFullyComplete reports whether every must-play match in the bracket --
// all round matches plus the naginata bronze (ThirdPlaceMatch, a sibling of
// Rounds) -- is completed. It mirrors the JS bracketFullyComplete /
// isRequiredBracketMatch gate (web-mobile/js/admin_helpers.jsx) so the server is
// the authority for POST /complete: a direct API call can't seal a competition
// with an unplayed final/bronze even if a client shows the button prematurely
// (an SSE match_updated carries only the scored match, so a downstream match can
// transiently still hold a "Winner of rX-mY" placeholder side). Returns false for
// a bracket with no must-play matches, so callers guard on len(Rounds) > 0 to
// skip bracketless formats (league/swiss/pools complete via their own path).
func bracketFullyComplete(b *state.Bracket) bool {
	if b == nil {
		return false
	}
	required := 0
	for ri := range b.Rounds {
		for mi := range b.Rounds[ri] {
			m := &b.Rounds[ri][mi]
			if !isRequiredBracketMatch(m) {
				continue
			}
			required++
			if m.Status != state.MatchStatusCompleted {
				return false
			}
		}
	}
	if isRequiredBracketMatch(b.ThirdPlaceMatch) {
		required++
		if b.ThirdPlaceMatch.Status != state.MatchStatusCompleted {
			return false
		}
	}
	return required > 0
}

// isRequiredBracketMatch reports whether a bracket match must be played before
// the competition is complete: both sides are named -- INCLUDING an unresolved
// "Winner of rX-mY" / "Pool A-1st" placeholder that propagation hasn't filled in
// yet (required, just not yet playable) -- and it is not a structural bye (one
// empty side) or a Hidden empty-vs-empty phantom. Keeping placeholder-sided
// matches is what stops the completion gate firing while a downstream match is
// still waiting on its feeder.
func isRequiredBracketMatch(m *state.BracketMatch) bool {
	if m == nil || m.Hidden {
		return false
	}
	return strings.TrimSpace(m.SideA) != "" && strings.TrimSpace(m.SideB) != ""
}
