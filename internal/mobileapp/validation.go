// Package mobileapp, see validation.go for the Validate() error
// pattern that request bodies use after JSON binding (Slice 0 / NFR-004).
//
// Pattern (used by `c.ShouldBindJSON(&req); req.Validate()`):
//
//  1. Define the body as a struct with explicit JSON tags.
//  2. Implement `Validate() error` on the struct (pointer receiver
//     when the struct is large) and return a typed `ValidationError`
//     describing the first failed field. Stop on the first failure,
//     handlers map ValidationError to HTTP 400 with the embedded message.
//  3. Handlers call `req.Validate()` immediately after `ShouldBindJSON`.
//     Anything more semantic (e.g. cross-resource lookups, store reads)
//     stays in the handler, Validate() handles only request-shape
//     invariants that don't need I/O.
//
// ScoreRequest is the example implementation. Other handler families
// will adopt the same pattern as later slices touch them.
package mobileapp

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MaxLen* caps the byte length of persisted user-string fields. Picked
// loose enough that no realistic operator hits them, tight enough that
// abusive inputs are rejected fast on the write path. Defense-in-depth
// against unbounded YAML/CSV inflation, Findings #2 reinterpretation
// from the security review (the recommended HTML-sanitization path was
// rejected; render-time encoding via Preact JSX is already in place).
const (
	MaxLenTournamentName     = 200
	MaxLenTournamentVenue    = 200
	MaxLenTournamentDate     = 10  // DD-MM-YYYY, also format-validated
	MaxLenTournamentPassword = 256 // not trimmed; cap prevents megabyte payloads
	MaxLenCeremonyBlock      = 16  // "1h30m" etc.

	// mp-ef3: public tournament info field caps.
	MaxLenPublicURL       = 500 // mp-s1gl: externally-shareable base URL
	MaxLenVenueAddress    = 300
	MaxLenVenueMapURL     = 500
	MaxLenDisplayTime     = 8 // "HH:MM" or "HH:MM:SS"
	MaxLenWebsiteURL      = 500
	MaxLenAwardsNote      = 500
	MaxLenInfoNotes       = 2000
	MaxLenContactLabel    = 50
	MaxLenContactValue    = 200
	MaxTournamentContacts = 10

	// MaxTournamentDurationDays is the upper bound on Tournament.DurationDays.
	// 30 days covers the longest conceivable multi-day open tournament.
	MaxTournamentDurationDays = 30

	MaxLenCompetitionName         = 200
	MaxLenCompetitionNumberPrefix = 3 // matches admin UI maxLength="3"
	MaxLenCompetitionStartTime    = 8 // "HH:MM"
	MaxLenCompetitionDate         = 10

	MaxLenPlayerName        = 100
	MaxLenPlayerDisplayName = 50 // physical zekken fabric-strip size
	MaxLenPlayerDojo        = 100
	MaxLenPlayerMetadata    = 200 // per entry
	MaxPlayerMetadataItems  = 16

	MaxLenMatchSide        = 100 // sideA / sideB / winner
	MaxLenMatchScheduledAt = 32

	MaxLenDecisionReason = 200
	// Operator audit free-text (correction note) shares the same human-readable
	// purpose and bound as DecisionReason.
	MaxLenCorrectionReason  = MaxLenDecisionReason
	MaxLenEligibilityReason = 200
	MaxLenEntityID          = 64 // matches state.ValidateCompetitionID cap
	// MaxLenRevSession caps ScoreRequest.RevSession (an opaque session id, e.g.
	// a 36-char UUID; 64 leaves headroom).
	MaxLenRevSession = 64

	MaxLenSeedAssignmentName = 100

	// MaxLenMatchID caps the byte length of the "mid" path parameter accepted
	// by the score endpoint. Match IDs legitimately contain spaces (e.g.
	// "Pool A-1"), so a charset regex is inappropriate, a length cap is the
	// right defense-in-depth guard against abusive keys growing runningRevStore
	// unbounded. 128 bytes covers any realistic match ID.
	MaxLenMatchID = 128

	// MaxBulkCheckInIDs is the upper bound on the participantIds array
	// accepted by POST /competitions/:id/participants/checkin-bulk. A
	// single per-comp write lock is held for the duration; 1000 is a
	// practical ceiling for tournament rosters (no real competition has
	// exceeded ~200).
	MaxBulkCheckInIDs = 1000

	// MaxFightingSpiritAwards is the upper bound on the number of fighting
	// spirit awards a competition may carry. 20 is a generous cap for
	// the typical ceremony (usually 1–3 honourees).
	MaxFightingSpiritAwards = 20
)

// validateMaxLen returns a ValidationError when val exceeds max bytes.
// Empty strings pass, required-field checks live separately so callers
// can compose presence and length independently. Byte length (not rune
// count) is the right measure here: the cap is about disk/parse cost,
// which scales with bytes, not display width. Caller is responsible for
// trimming first if trimming applies.
func validateMaxLen(field, val string, max int) error {
	if len(val) > max {
		return &ValidationError{
			Field:   field,
			Message: fmt.Sprintf("must be <= %d characters", max),
		}
	}
	return nil
}

// The remedy clauses for a seeding refused at a WRITE boundary. Both callers
// are holding the whole list already, so unlike the draw's refusal there is no
// seeding panel to send them back to and nothing on disk to "clear" -- and the
// two differ because one is fixed in a request body and the other in a file.
const (
	seedGapRemedyPut    = "Send the complete seeding, or an empty list to clear it."
	seedGapRemedyImport = "Fix the seeds file or drop it from the manifest, then import again."
)

// seedRejection turns a domain.ValidateAssignments failure into the message a
// write boundary refuses with: the gap diagnosis plus that boundary's remedy
// when ranks are missing, and otherwise the validator's own words.
//
// The pass-through matters. A duplicate rank and a rank of 0 are not gaps, and
// helper.SeedGapDiagnosis returns "" for both precisely so that neither can be
// reported as one; err already describes them exactly.
func seedRejection(assignments []domain.SeedAssignment, err error, remedy string) string {
	if diagnosis := helper.SeedGapDiagnosis(assignments); diagnosis != "" {
		return diagnosis + " " + remedy
	}
	return err.Error()
}

// rejectSeedsOffRoster refuses a seeding that ranks a name no participant
// carries. domain.ValidateAssignments checks the ranks as a SET (contiguous
// from 1, no duplicates) and never looks at the roster, so a ghost name passed
// and produced a competition whose two views of one seeding disagreed: the file
// held a valid 1..N and drew, while every reader that merges seeds onto players
// by name saw only the survivors and read the ghost's rank as an unclosable gap.
//
// Name is the join key state.loadParticipants uses when it merges seeds.csv onto
// the roster, so matching on it here means this check and that merge can never
// disagree about who a rank belongs to.
//
// An empty roster is NOT treated as "everything is a ghost": seeds for a
// competition whose participants have not been written yet are refused only when
// there is a roster to contradict them, so a client that saves seeds before the
// roster still works and the draw's own validation remains the backstop.
func rejectSeedsOffRoster(store *state.Store, compID string, assignments []domain.SeedAssignment) error {
	if len(assignments) == 0 {
		return nil
	}
	// withZekkenName only affects column layout, not the Name field this reads.
	players, err := store.LoadParticipants(compID, false)
	if err != nil || len(players) == 0 {
		return nil
	}
	onRoster := make(map[string]bool, len(players))
	for _, p := range players {
		onRoster[p.Name] = true
	}
	var unknown []string
	for _, a := range assignments {
		if !onRoster[a.Name] {
			unknown = append(unknown, fmt.Sprintf("%q (rank %d)", a.Name, a.SeedRank))
		}
	}
	if len(unknown) == 0 {
		return nil
	}
	verb := "is"
	if len(unknown) > 1 {
		verb = "are"
	}
	return fmt.Errorf("seeds: %s %s not on this competition's roster; a seed rank must belong to a participant. %s",
		strings.Join(unknown, ", "), verb, seedGapRemedyPut)
}

// validateHTTPURL returns a ValidationError when val is non-empty and does not
// start with "http://" or "https://". These URL fields are rendered as raw href
// values in the viewer SPA; rejecting non-http(s) schemes at the write boundary
// prevents javascript: or data: URIs from reaching the public viewer page.
// Empty strings pass (the fields are optional).
func validateHTTPURL(field, val string) error {
	if val == "" {
		return nil
	}
	lower := strings.ToLower(val)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return &ValidationError{
			Field:   field,
			Message: "must start with http:// or https://",
		}
	}
	return nil
}

// validateURLHasHost rejects scheme-only values like "https://" that pass the
// prefix check in validateHTTPURL but have no host, which would produce a
// broken base URL after trailing-slash normalization (e.g. "https:").
// Empty strings pass (the field is optional).
func validateURLHasHost(field, val string) error {
	if val == "" {
		return nil
	}
	u, err := url.Parse(val)
	if err != nil || u.Host == "" {
		return &ValidationError{
			Field:   field,
			Message: "must include a host (e.g. https://my-tournament.example.com)",
		}
	}
	return nil
}

// subBoutNeedsNumberedEnchoAllowance reports whether sr is a NUMBERED bout
// (not the position -1 daihyosen) carrying an encho marker — the exact shape
// rejected UNLESS the kachinuki numbered-bout allowance applies. THE one
// predicate shared by validateSubBout's gate (which enforces it) and
// anyNumberedBoutHasEncho (which pre-scans a payload to decide whether the
// competition even needs loading for the allowance): deriving both from one
// place means a future widening of the gate — hantei on a numbered bout is the
// flagged next candidate — is added HERE, so the pre-scan cannot silently stop
// matching and evaporate the exception (mp-gmcg review).
func subBoutNeedsNumberedEnchoAllowance(sr *state.SubMatchResult) bool {
	return sr.Position != state.DaihyosenSubPosition && sr.Encho.On()
}

// validateSubBout enforces FIK sub-bout invariants on a single SubMatchResult.
// Both encho and hantei are valid ONLY for the daihyosen representative bout
// (Position == -1): regular numbered bouts have fixed regulation time and are
// never decided by hantei. Hantei does NOT require encho, though, a tied
// daihyosen may be decided by judges directly (the encho gate was removed).
//
// EXCEPTION (mp-gmcg): in a KACHINUKI competition a tied pairing may be
// fought on in overtime on that same bout (daihyosen does not exist in
// kachinuki), so callers pass allowNumberedEncho=true — derived via
// allowNumberedEnchoFromStore (handlers_match.go). The exception applies in
// EVERY phase: whether the final pairing must produce a result (e.g. the
// taisho must be defeated) is operator discretion, never derivable from
// pool-vs-bracket. The hantei gate is NOT relaxed: kachinuki bouts are
// never decided by hantei.
//
// The winner and tied-scoreline checks here are the same rules the top-level
// DecidedByHantei block in ScoreRequest.Validate applies; both now call the
// shared domain predicates, so that pair stays in sync by construction rather
// than by comment. Two differences remain deliberate: the sub-bout variant adds
// the Position guards and omits the top-level-only Status/DecisionBy checks
// (SubMatchResult has no such fields), and it ALLOWS decision "daihyosen",
// which the match level rejects (see the note there).
func validateSubBout(prefix string, sr *state.SubMatchResult, allowNumberedEncho bool) error {
	// Encho period counts are bounded two ways. A negative count is never
	// valid on any bout (it would make Encho.On() read false below and be
	// silently treated as "no encho"). On a numbered bout, ANY non-zero
	// count is rejected unless the caller allows it (kachinuki, see above):
	// a regular bout has fixed regulation time and cannot go to overtime;
	// only the daihyosen representative bout (Position == -1) may carry encho.
	if sr.Encho != nil {
		if sr.Encho.PeriodCount < 0 {
			return &ValidationError{
				Field:   prefix + "encho",
				Message: "encho period count must not be negative",
			}
		}
		if !allowNumberedEncho && subBoutNeedsNumberedEnchoAllowance(sr) {
			return &ValidationError{
				Field:   prefix + "encho",
				Message: "encho is only valid for the daihyosen representative bout (position -1)",
			}
		}
	}
	if !sr.HanteiDecided() {
		return nil
	}
	if sr.Position != state.DaihyosenSubPosition {
		return &ValidationError{
			Field:   prefix + "decidedByHantei",
			Message: "hantei is only valid for the daihyosen representative bout (position -1)",
		}
	}
	if sr.Winner == "" {
		return &ValidationError{Field: prefix + "decidedByHantei", Message: "requires winner to be set"}
	}
	// Both halves of this gate are shared with the engine's preserveSubHantei
	// via domain, so a row this accepts and a row the engine may stamp can
	// never disagree. The tie test was raw len() here and placeholder-dropping
	// there, which is a difference an "•" or empty cell can express.
	if !domain.HanteiTiedScoreline(sr.IpponsA, sr.IpponsB) {
		return &ValidationError{Field: prefix + "decidedByHantei", Message: "requires a tied scoreline, ippon counts must be equal"}
	}
	if !domain.IsSubBoutHanteiCompatibleDecisionStr(sr.Decision) {
		return &ValidationError{
			Field:   prefix + "decidedByHantei",
			Message: fmt.Sprintf("incompatible with decision %q, hantei declares a winner from a tied bout; use '', 'fought', or 'daihyosen'", sr.Decision),
		}
	}
	return nil
}

// validateBulkScoreLengths enforces persisted-string caps on a single
// MatchResult before it lands in the engine. Used by the bulk-score
// endpoint, which writes through RecordMatchResult and so bypasses
// ScoreRequest.Validate's checks. Same caps as ScoreRequest.Validate
// so the per-result and per-endpoint enforcement stays in lockstep.
// allowNumberedEncho mirrors validateSubBout's kachinuki exception; the
// bulk handler derives it from the competition it already loads.
func validateBulkScoreLengths(r *state.MatchResult, allowNumberedEncho bool) error {
	if err := validateMaxLen("sideA", r.SideA, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("sideB", r.SideB, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("winner", r.Winner, MaxLenMatchSide); err != nil {
		return err
	}
	// Daihyosen/tiebreaker rep-player names (mp-62vr) are competitor names,
	// cap them like the match sides to keep the appended pool-matches.csv
	// columns bounded.
	if err := validateMaxLen("repPlayerA", r.RepPlayerA, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("repPlayerB", r.RepPlayerB, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("scheduledAt", r.ScheduledAt, MaxLenMatchScheduledAt); err != nil {
		return err
	}
	if err := validateMaxLen("decisionReason", r.DecisionReason, MaxLenDecisionReason); err != nil {
		return err
	}
	// Cap the TRIMMED value: the write path persists strings.TrimSpace(reason),
	// so a reason within the cap once normalized must not be rejected for
	// trailing/leading whitespace.
	if err := validateMaxLen("correctionReason", strings.TrimSpace(r.CorrectionReason), MaxLenCorrectionReason); err != nil {
		return err
	}
	if err := validateIppons("", r.IpponsA, r.IpponsB); err != nil {
		return err
	}
	if r.FlagsA < 0 {
		return &ValidationError{Field: "flagsA", Message: "must not be negative"}
	}
	if r.FlagsB < 0 {
		return &ValidationError{Field: "flagsB", Message: "must not be negative"}
	}
	for i := range r.SubResults {
		sr := &r.SubResults[i]
		prefix := fmt.Sprintf("subResults[%d].", i)
		if err := validateMaxLen(prefix+"sideA", sr.SideA, MaxLenMatchSide); err != nil {
			return err
		}
		if err := validateMaxLen(prefix+"sideB", sr.SideB, MaxLenMatchSide); err != nil {
			return err
		}
		if err := validateMaxLen(prefix+"winner", sr.Winner, MaxLenMatchSide); err != nil {
			return err
		}
		if err := validateIppons(prefix, sr.IpponsA, sr.IpponsB); err != nil {
			return err
		}
		if err := validateSubBout(prefix, sr, allowNumberedEncho); err != nil {
			return err
		}
	}
	return nil
}

// validatePlayerRequired rejects a roster entry with a blank name or dojo.
// A blank dojo is the silent-corruption signature of a misformatted roster
// row (e.g. a two-column "Name, Dojo" line in a zekken competition, which the
// SPA parser maps to {displayName: dojo, dojo: ""}). Shared by every endpoint
// that persists an embedded roster, POST /participants (batch), POST
// /competitions, and the roster-PUT branch of PUT /competitions/:id, so the
// same malformed payload is rejected identically regardless of entry point.
func validatePlayerRequired(name, dojo string) error {
	if strings.TrimSpace(name) == "" {
		return &ValidationError{Message: "name must not be blank"}
	}
	if strings.TrimSpace(dojo) == "" {
		return &ValidationError{Message: "dojo must not be blank"}
	}
	return nil
}

// validatePlayerLengths enforces caps on every persisted string of a
// participant. Shared between the participants handler (live UI write)
// and the import handler (manifest upload) so a malformed CSV or JSON
// payload from either path is rejected with the same error shape. The
// Metadata slice is also count-capped, 16 entries is generous given
// the current schema (Dan, Grade, optional flags) but rejects abusive
// payloads that would inflate participants.csv into the megabytes.
func validatePlayerLengths(name, displayName, dojo, source string, metadata []string) error {
	if err := validateMaxLen("name", name, MaxLenPlayerName); err != nil {
		return err
	}
	if err := validateMaxLen("displayName", displayName, MaxLenPlayerDisplayName); err != nil {
		return err
	}
	if err := validateMaxLen("dojo", dojo, MaxLenPlayerDojo); err != nil {
		return err
	}
	if err := validateMaxLen("source", source, MaxLenPlayerMetadata); err != nil {
		return err
	}
	// A non-empty registration source must be a recognised value. Validate the
	// CANONICAL form (CanonicalRegistrationSource trims, lower-cases, and aliases
	// the legacy "reserved" → "manual") so every endpoint accepts the same set
	// uniformly, including legacy inputs, while still rejecting truly unknown
	// values (the CSV loader only recognises these tokens, so an unexpected value
	// would shift into Metadata on reload).
	if source != "" && !helper.IsRegistrationSource(helper.CanonicalRegistrationSource(source)) {
		return &ValidationError{
			Field:   "source",
			Message: `must be one of "manual", "registered", "transfer" (case-insensitive; legacy "reserved" accepted as "manual")`,
		}
	}
	if len(metadata) > MaxPlayerMetadataItems {
		return &ValidationError{
			Field:   "metadata",
			Message: fmt.Sprintf("must contain <= %d entries", MaxPlayerMetadataItems),
		}
	}
	for i, entry := range metadata {
		if err := validateMaxLen(fmt.Sprintf("metadata[%d]", i), entry, MaxLenPlayerMetadata); err != nil {
			return err
		}
	}
	return nil
}

// Validator is the contract every request body should satisfy after
// JSON binding. Validate() returns nil when the body is well-formed
// against its own shape rules; ValidationError when it isn't.
type Validator interface {
	Validate() error
}

// ValidationError is a typed error returned by Validate() so handlers
// can distinguish shape errors (400) from store / engine errors (500).
// Handlers map ValidationError directly to a 400 with the Message body.
type ValidationError struct {
	// Field is the JSON field name that failed validation, or "" when
	// the failure spans multiple fields.
	Field string
	// Message is the operator-facing reason, designed to be returned
	// verbatim in the HTTP 400 response body.
	Message string
}

func (e *ValidationError) Error() string {
	if e.Field == "" {
		return e.Message
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

// ScoreRequest is the body shape for `PUT /api/competitions/:id/matches/:mid/score`.
// It is the minimal example implementation of the Validator pattern (T015).
//
// Defined as a named type whose underlying type is state.MatchResult so
// the JSON shape is identical to the pre-Slice-0 endpoint (clients send
// MatchResult fields at the top level), no client-side change. The
// named type lets us hang Validate() off it without polluting state
// (which is a pure-data package).
//
// As later slices add decision-type / encho fields (see Slice 3 FR-031,
// T077), the matching Validate() rules land here.
type ScoreRequest state.MatchResult

// Validate enforces request-shape invariants on a score payload before
// the engine touches it. Rules deliberately match the existing engine
// guards so behaviour is unchanged:
//
//   - Status, when set, must be one of the documented MatchStatus values.
//   - Winner, when set alongside both sides, must name one of the sides
//     (a Winner that names neither side would silently miscount in
//     standings).
//   - Decision (T077, FR-031, contracts/match-decisions.md):
//     value must be one of fought/hikiwake/kiken/fusenpai/fusensho/
//     daihyosen/kachinuki-exhaustion (or empty).
//     kiken/fusenpai require decisionBy and a winning-side scoreline
//     (2-0 in regulation, 1-0 in encho). fusensho is only
//     valid on a per-bout SubResult, not on a top-level score request.
func (r *ScoreRequest) Validate() error {
	return r.validateWithOptions(false)
}

// validateWithOptions is Validate with the kachinuki bout-level encho
// exception threaded through (mp-gmcg): the score handler passes
// allowNumberedEncho=true when the target competition is kachinuki
// (allowNumberedEnchoFromStore) — any phase; whether a tied pairing must be
// fought to a result is operator discretion. Every other caller keeps
// the strict daihyosen-only gate via Validate().
func (r *ScoreRequest) validateWithOptions(allowNumberedEncho bool) error {
	if r.Status != "" {
		switch r.Status {
		case state.MatchStatusScheduled, state.MatchStatusRunning, state.MatchStatusCompleted:
			// ok
		default:
			return &ValidationError{
				Field:   "status",
				Message: fmt.Sprintf("must be one of scheduled/running/completed, got %q", r.Status),
			}
		}
	}
	// Length caps, defense-in-depth against unbounded YAML/CSV bloat.
	// `decisionReason` was previously bounded only in DecisionRequest.Validate
	// (200 char contract); folding it in here closes the bulk-score gap
	// where a 1MB reason could land on disk via PUT /matches/:mid/score.
	if err := validateMaxLen("sideA", r.SideA, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("sideB", r.SideB, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("winner", r.Winner, MaxLenMatchSide); err != nil {
		return err
	}
	// Daihyosen/tiebreaker rep-player names (mp-62vr) are competitor names,
	// cap them like the match sides to keep the appended pool-matches.csv
	// columns bounded.
	if err := validateMaxLen("repPlayerA", r.RepPlayerA, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("repPlayerB", r.RepPlayerB, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("scheduledAt", r.ScheduledAt, MaxLenMatchScheduledAt); err != nil {
		return err
	}
	if err := validateMaxLen("decisionReason", r.DecisionReason, MaxLenDecisionReason); err != nil {
		return err
	}
	// Cap the TRIMMED value: the write path persists strings.TrimSpace(reason),
	// so a reason within the cap once normalized must not be rejected for
	// trailing/leading whitespace.
	if err := validateMaxLen("correctionReason", strings.TrimSpace(r.CorrectionReason), MaxLenCorrectionReason); err != nil {
		return err
	}
	if err := validateMaxLen("revSession", r.RevSession, MaxLenRevSession); err != nil {
		return err
	}
	// rev is a client-supplied monotonic counter. rev==0 is the intentional
	// "unversioned" opt-out (guard skipped); a NEGATIVE rev would likewise slip
	// past the Rev>0 gate, letting a stale running write clobber newer state, so
	// reject it outright.
	if r.Rev < 0 {
		return &ValidationError{Field: "rev", Message: "must not be negative"}
	}
	// Engi referee flag counts must be non-negative. The {1,3,5}-total
	// completed-match invariant is enforced by the engine (engiValidTotal);
	// here we only reject the impossible negatives at the HTTP boundary,
	// since a running/partial save may legitimately carry a 0 total.
	if r.FlagsA < 0 {
		return &ValidationError{Field: "flagsA", Message: "must not be negative"}
	}
	if r.FlagsB < 0 {
		return &ValidationError{Field: "flagsB", Message: "must not be negative"}
	}
	// Winner, when supplied, must name one of the two sides. Empty
	// winner is permitted (draw or pre-completion update). We only
	// check when both sides AND winner are present in the request,
	// the engine's preserve-on-empty fallback handles the side-omitted
	// case.
	if r.Winner != "" && r.SideA != "" && r.SideB != "" {
		if r.Winner != r.SideA && r.Winner != r.SideB {
			return &ValidationError{
				Field:   "winner",
				Message: fmt.Sprintf("must equal sideA or sideB, got %q", r.Winner),
			}
		}
	}
	// Best-of-3 ippon invariants on the top-level scoreline.
	if err := validateIppons("", r.IpponsA, r.IpponsB); err != nil {
		return err
	}
	// Same invariants on each sub-bout (team-match positions).
	for i := range r.SubResults {
		sr := &r.SubResults[i]
		prefix := fmt.Sprintf("subResults[%d].", i)
		if err := validateIppons(prefix, sr.IpponsA, sr.IpponsB); err != nil {
			return err
		}
		if err := validateSubBout(prefix, sr, allowNumberedEncho); err != nil {
			return err
		}
	}
	// DecidedByHantei records a referee judges' decision that declares a winner
	// from a tied bout. A winner must be present, the status (if supplied) must
	// be completed, and the scoreline must be tied (equal ippon counts). Encho
	// is NOT required: operators may take a tied match straight to hantei
	// without an overtime period (the encho gate was removed deliberately).
	// The winner and tied-scoreline checks below are the SAME rules
	// validateSubBout applies, so both call the shared domain predicates rather
	// than spelling them out twice (the tie test used to be a raw len() here
	// against a placeholder-dropping count there, so ["M","•"] against ["M","K"]
	// read tied to one enforcer and untied to the other). The decision check is
	// deliberately NARROWER than the sub-bout one; see the note on it below.
	if r.DecidedByHantei != nil && *r.DecidedByHantei {
		if r.Winner == "" {
			return &ValidationError{
				Field:   "decidedByHantei",
				Message: "requires winner to be set",
			}
		}
		if r.Status != "" && r.Status != state.MatchStatusCompleted {
			return &ValidationError{
				Field:   "decidedByHantei",
				Message: "only valid on completed matches",
			}
		}
		if !domain.HanteiTiedScoreline(r.IpponsA, r.IpponsB) {
			return &ValidationError{
				Field:   "decidedByHantei",
				Message: "requires a tied scoreline, ippon counts must be equal",
			}
		}
		// Hantei is a referee judges' decision that produces a winner from a
		// tied bout. Any other special decision (hikiwake=draw, kiken=withdrawal,
		// fusenpai=no-show, daihyosen=rep-bout…) is semantically incompatible,
		// persisting both would render contradictory suffixes like "Kiken (E) HT".
		// Only the neutral values ("" and "fought") are allowed alongside hantei.
		//
		// The MATCH-level predicate, which is the sub-bout one minus "daihyosen";
		// the narrowing and its reason live on the domain function. It used to be
		// a hand-written switch here, which meant this enforcer and the engine's
		// hanteiStillHolds spelled the same rule two different ways — and the
		// engine's spelling was the WIDER one, so it could stamp a match-level
		// verdict onto a decision this rejects.
		if !domain.IsMatchHanteiCompatibleDecisionStr(r.Decision) {
			return &ValidationError{
				Field:   "decidedByHantei",
				Message: fmt.Sprintf("incompatible with decision %q, hantei declares a winner from a tied bout; use '' or 'fought'", r.Decision),
			}
		}
		if r.DecisionBy != "" {
			return &ValidationError{
				Field:   "decidedByHantei",
				Message: "decisionBy must be empty when decidedByHantei is true",
			}
		}
		if r.DecisionReason != "" {
			return &ValidationError{
				Field:   "decidedByHantei",
				Message: "decisionReason must be empty when decidedByHantei is true",
			}
		}
	}
	return r.validateDecision()
}

// validateDecision enforces the FR-031 / contracts/match-decisions.md
// rules. Splitting it out keeps Validate() at a glance.
func (r *ScoreRequest) validateDecision() error {
	switch r.Decision {
	case "kiken":
		r.Decision = "kiken-voluntary"
	case "", "fought", "hikiwake", "kiken-voluntary", "kiken-injury", "fusenpai", "fusensho", "daihyosen", "kachinuki-exhaustion":
		// ok
	default:
		return &ValidationError{
			Field:   "decision",
			Message: fmt.Sprintf("unknown decision %q", r.Decision),
		}
	}
	if r.DecisionBy != "" && r.DecisionBy != "shiro" && r.DecisionBy != "aka" {
		return &ValidationError{
			Field:   "decisionBy",
			Message: fmt.Sprintf("must be 'shiro' or 'aka', got %q", r.DecisionBy),
		}
	}
	switch r.Decision {
	case "kiken-voluntary", "kiken-injury", "fusenpai":
		if r.DecisionBy == "" {
			return &ValidationError{Field: "decisionBy", Message: fmt.Sprintf("required when decision is %s", r.Decision)}
		}
		// The required default-win scoreline is exactly what the recorder
		// fills (domain.DefaultWinIppons keyed on the shared encho
		// predicate): the full pair in regulation, the single deciding
		// point in encho.
		need := len(domain.DefaultWinIppons(r.Encho.On()))
		if !winningScoreline(r.IpponsA, r.IpponsB, need) {
			return &ValidationError{
				Field:   "scoreline",
				Message: fmt.Sprintf("%s requires %d-0 scoreline", r.Decision, need),
			}
		}
		if err := r.requireWinnerForDecision(); err != nil {
			return err
		}
	case "fusensho":
		return &ValidationError{
			Field:   "decision",
			Message: "fusensho is only valid on a per-bout sub-result, not a top-level match",
		}
	}
	return nil
}

// winningScoreline reports whether exactly one of the two ippon slices
// has `n` entries while the other is empty (i.e. an n-0 result).
func winningScoreline(ipponsA, ipponsB []string, n int) bool {
	a := len(ipponsA)
	b := len(ipponsB)
	return (a == n && b == 0) || (a == 0 && b == n)
}

// maxIpponsPerSide is the kendo best-of-3 cap: each fighter can score
// at most 2 ippons in regulation (the bout ends when one side reaches
// 2). 2-2 is therefore an impossible scoreline, the match would have
// ended at 2-1 before either side could score a third.
const maxIpponsPerSide = 2

// validateIppons is the ONE gate every ippon slice crosses on its way in, from
// both the ScoreRequest path and the bulk-score path. It enforces the
// best-of-3 invariants on a single match (or sub-bout) tally, and the shape of
// the entries themselves. Rules:
//
//   - each entry is a single character (domain.IpponFitsScoreCodec)
//   - len(ipponsA) ≤ 2 and len(ipponsB) ≤ 2
//   - NOT (scoring(ipponsA) == 2 && scoring(ipponsB) == 2)  , the 2-2 ban
//
// Field is the JSON-field prefix used in error messages (e.g. "" for a
// top-level match, "subResults[i]." for a sub-bout). Kiken/fusenpai
// scorelines are also bounded by these rules, their own n-0 check in
// validateDecision is strictly tighter (n ≤ 2) so this passes through.
//
// (Named validateIpponCounts until the entry-shape rule joined it, at which
// point the name described half the job.)
func validateIppons(field string, ipponsA, ipponsB []string) error {
	// Entry SHAPE first: a multi-rune entry is malformed whatever the counts
	// are, and the two failures it caused are both worse than a count error.
	if err := ipponEntriesWellFormed(field+"ipponsA", ipponsA); err != nil {
		return err
	}
	if err := ipponEntriesWellFormed(field+"ipponsB", ipponsB); err != nil {
		return err
	}
	if len(ipponsA) > maxIpponsPerSide {
		return &ValidationError{
			Field:   field + "ipponsA",
			Message: fmt.Sprintf("at most %d ippons per side (best-of-3), got %d", maxIpponsPerSide, len(ipponsA)),
		}
	}
	if len(ipponsB) > maxIpponsPerSide {
		return &ValidationError{
			Field:   field + "ipponsB",
			Message: fmt.Sprintf("at most %d ippons per side (best-of-3), got %d", maxIpponsPerSide, len(ipponsB)),
		}
	}
	// The two checks above and the one below count DIFFERENTLY, on purpose.
	//
	// The per-side caps are STRUCTURAL: a side has two slots, so an array with
	// more entries than that is malformed whatever the entries are, and raw
	// len() is the right measure.
	//
	// This one is SEMANTIC: "both sides scored 2" is a claim about POINTS, and
	// an unfilled "•" placeholder or an empty cell is not a point (the same rule
	// domain.CountScoringIppons states for the engine, the store and the hantei
	// tie gate). Counting slots here rejected legal scorelines: ["M","•"]
	// against ["K","D"] is 1-2, an ordinary win, but read as 2-2 and refused
	// with a message about a rule it does not break. Unreachable from the UI —
	// both editors strip the placeholder before sending — but this is the wire
	// boundary, and it now agrees with every other counter in the codebase.
	if domain.CountScoringIppons(ipponsA) == maxIpponsPerSide &&
		domain.CountScoringIppons(ipponsB) == maxIpponsPerSide {
		return &ValidationError{
			Field:   field + "ippons",
			Message: "both sides cannot have 2 ippons (best-of-3 ends at first to 2)",
		}
	}
	return nil
}

// ipponEntriesWellFormed rejects a multi-rune ippon entry on one side. The
// reasoning lives on domain.IpponFitsScoreCodec, beside the codec whose
// precondition this is; the short version is that FormatScore joins with no
// separator, so "MHt" comes back as three ippons — and on a POOL match the
// two-rune "Ht" is the verdict ENCODING, so accepting one from a client let a
// payload forge a judges' decision that appeared on the next reload.
//
// field is the fully-qualified JSON field name, e.g. "subResults[0].ipponsA".
func ipponEntriesWellFormed(field string, ippons []string) error {
	for _, v := range ippons {
		if !domain.IpponFitsScoreCodec(v) {
			// Says SHAPE, not vocabulary, because that is what is enforced. The
			// letters are deliberately open (naginata adds "S", and the round-trip
			// tests pin letter-agnosticism), so naming a closed set here would
			// promise a rule that does not exist and mislead whoever hits it.
			return &ValidationError{
				Field: field,
				Message: fmt.Sprintf(
					"each ippon mark is a single character (e.g. a waza letter, %q or %q); got %q",
					domain.DefaultWinIppon, domain.IpponPlaceholder, v),
			}
		}
	}
	return nil
}

// requireWinnerForDecision enforces that Winner is set when a kiken/
// fusenpai is recorded, the engine's eligibility side effect uses
// Winner as the canonical surviving side. Without this, a bulk-score
// or hand-crafted request could record an ineligibility against the
// wrong player.
func (r *ScoreRequest) requireWinnerForDecision() error {
	if r.Winner == "" {
		return &ValidationError{
			Field:   "winner",
			Message: fmt.Sprintf("required when decision is %s (names the surviving side)", r.Decision),
		}
	}
	return nil
}

// AsMatchResult returns the underlying state.MatchResult value so the
// score handler can forward it to the engine. The conversion is a
// zero-cost type conversion (identical underlying layout).
func (r *ScoreRequest) AsMatchResult() *state.MatchResult {
	mr := state.MatchResult(*r)
	return &mr
}

// IsSelfRunReportableDecision reports whether the given decision value is
// permitted for participant self-reporting in self-run tournaments (i.e.
// when no valid admin password is present on the request).
//
// Allowed at the top level: "" (none), "fought", "hikiwake". These are
// factual observations a participant can make without referee authority.
// fusensho is only valid on sub-results (ScoreRequest.Validate rejects it
// at the top level), so it's not listed here.
//
// Rejected: "kiken-voluntary", "kiken-injury", "fusenpai", "daihyosen",
// "kachinuki-exhaustion", "fusensho", referee/operator rulings with
// eligibility side-effects or official designation requirements. Also
// rejected when decidedByHantei is explicitly true (judges' panel decision).
func IsSelfRunReportableDecision(decision string, decidedByHantei *bool) bool {
	if decidedByHantei != nil && *decidedByHantei {
		return false
	}
	switch decision {
	case "", "fought", "hikiwake":
		return true
	default:
		return false
	}
}

// IsSelfRunReportableSubDecision validates a sub-bout decision for self-run
// anonymous callers. Allowed: "" (none), "fought", "hikiwake", "fusensho"
// (per-bout forfeiture is a factual observation). Rejected: kiken variants,
// fusenpai, daihyosen, kachinuki-exhaustion, decidedByHantei=true. Also
// rejects position == -1 (daihyosen representative bout placeholder).
func IsSelfRunReportableSubDecision(decision string, decidedByHantei bool, position int) bool {
	if position == state.DaihyosenSubPosition {
		return false
	}
	if decidedByHantei {
		return false
	}
	switch decision {
	case "", "fought", "hikiwake", "fusensho":
		return true
	default:
		return false
	}
}
