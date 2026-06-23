// Package mobileapp — see validation.go for the Validate() error
// pattern that request bodies use after JSON binding (Slice 0 / NFR-004).
//
// Pattern (used by `c.ShouldBindJSON(&req); req.Validate()`):
//
//  1. Define the body as a struct with explicit JSON tags.
//  2. Implement `Validate() error` on the struct (pointer receiver
//     when the struct is large) and return a typed `ValidationError`
//     describing the first failed field. Stop on the first failure —
//     handlers map ValidationError to HTTP 400 with the embedded message.
//  3. Handlers call `req.Validate()` immediately after `ShouldBindJSON`.
//     Anything more semantic (e.g. cross-resource lookups, store reads)
//     stays in the handler — Validate() handles only request-shape
//     invariants that don't need I/O.
//
// ScoreRequest is the example implementation. Other handler families
// will adopt the same pattern as later slices touch them.
package mobileapp

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// MaxLen* caps the byte length of persisted user-string fields. Picked
// loose enough that no realistic operator hits them, tight enough that
// abusive inputs are rejected fast on the write path. Defense-in-depth
// against unbounded YAML/CSV inflation — Findings #2 reinterpretation
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
	MaxLenRulesURL        = 500
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
	// Operator audit free-text (correction note, lineup-change note) shares the
	// same human-readable purpose and bound as DecisionReason.
	MaxLenCorrectionReason  = MaxLenDecisionReason
	MaxLenChangeReason      = MaxLenDecisionReason
	MaxLenEligibilityReason = 200
	MaxLenEntityID          = 64 // matches state.ValidateCompetitionID cap
	// MaxLenRevSession caps ScoreRequest.RevSession (an opaque session id, e.g.
	// a 36-char UUID; 64 leaves headroom).
	MaxLenRevSession = 64

	MaxLenSeedAssignmentName = 100

	// MaxLenMatchID caps the byte length of the "mid" path parameter accepted
	// by the score endpoint. Match IDs legitimately contain spaces (e.g.
	// "Pool A-1"), so a charset regex is inappropriate — a length cap is the
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
// Empty strings pass — required-field checks live separately so callers
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

// validateSubBout enforces FIK sub-bout invariants on a single SubMatchResult.
// Both encho and hantei are valid ONLY for the daihyosen representative bout
// (Position == -1): regular numbered bouts have fixed regulation time and are
// never decided by hantei. Hantei does NOT require encho, though — a tied
// daihyosen may be decided by judges directly (the encho gate was removed).
//
// The winner/tied-scoreline/decision checks here intentionally mirror the
// top-level DecidedByHantei block in ScoreRequest.Validate. Keep them in sync:
// the sub-bout variant adds the Position guards and omits the top-level-only
// Status/DecisionBy checks (SubMatchResult has no such fields).
func validateSubBout(prefix string, sr *state.SubMatchResult) error {
	// Encho period counts are bounded two ways. A negative count is never
	// valid on any bout (it would slip past the > 0 guards below and be
	// silently treated as "no encho", bypassing the cap check). On a
	// numbered bout, ANY non-zero count is rejected — a regular bout has
	// fixed regulation time and cannot go to overtime; only the daihyosen
	// representative bout (Position == -1) may carry encho.
	if sr.Encho != nil {
		if sr.Encho.PeriodCount < 0 {
			return &ValidationError{
				Field:   prefix + "encho",
				Message: "encho period count must not be negative",
			}
		}
		if sr.Position != -1 && sr.Encho.PeriodCount != 0 {
			return &ValidationError{
				Field:   prefix + "encho",
				Message: "encho is only valid for the daihyosen representative bout (position -1)",
			}
		}
	}
	if !sr.DecidedByHantei {
		return nil
	}
	if sr.Position != -1 {
		return &ValidationError{
			Field:   prefix + "decidedByHantei",
			Message: "hantei is only valid for the daihyosen representative bout (position -1)",
		}
	}
	if sr.Winner == "" {
		return &ValidationError{Field: prefix + "decidedByHantei", Message: "requires winner to be set"}
	}
	if len(sr.IpponsA) != len(sr.IpponsB) {
		return &ValidationError{Field: prefix + "decidedByHantei", Message: "requires a tied scoreline — ippon counts must be equal"}
	}
	switch sr.Decision {
	case "", "fought", "daihyosen":
		// compatible: daihyosen placeholders carry decision="daihyosen"
	default:
		return &ValidationError{
			Field:   prefix + "decidedByHantei",
			Message: fmt.Sprintf("incompatible with decision %q — hantei declares a winner from a tied bout; use '', 'fought', or 'daihyosen'", sr.Decision),
		}
	}
	return nil
}

// validateBulkScoreLengths enforces persisted-string caps on a single
// MatchResult before it lands in the engine. Used by the bulk-score
// endpoint, which writes through RecordMatchResult and so bypasses
// ScoreRequest.Validate's checks. Same caps as ScoreRequest.Validate
// so the per-result and per-endpoint enforcement stays in lockstep.
func validateBulkScoreLengths(r *state.MatchResult) error {
	if err := validateMaxLen("sideA", r.SideA, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("sideB", r.SideB, MaxLenMatchSide); err != nil {
		return err
	}
	if err := validateMaxLen("winner", r.Winner, MaxLenMatchSide); err != nil {
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
	if err := validateIpponCounts("", r.IpponsA, r.IpponsB); err != nil {
		return err
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
		if err := validateIpponCounts(prefix, sr.IpponsA, sr.IpponsB); err != nil {
			return err
		}
		if err := validateSubBout(prefix, sr); err != nil {
			return err
		}
	}
	return nil
}

// validatePlayerLengths enforces caps on every persisted string of a
// participant. Shared between the participants handler (live UI write)
// and the import handler (manifest upload) so a malformed CSV or JSON
// payload from either path is rejected with the same error shape. The
// Metadata slice is also count-capped — 16 entries is generous given
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
// MatchResult fields at the top level) — no client-side change. The
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
//     (2-0 in regulation, 1-0 in encho for kiken). fusensho is only
//     valid on a per-bout SubResult, not on a top-level score request.
func (r *ScoreRequest) Validate() error {
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
	// Length caps — defense-in-depth against unbounded YAML/CSV bloat.
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
	// Winner, when supplied, must name one of the two sides. Empty
	// winner is permitted (draw or pre-completion update). We only
	// check when both sides AND winner are present in the request —
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
	if err := validateIpponCounts("", r.IpponsA, r.IpponsB); err != nil {
		return err
	}
	// Same invariants on each sub-bout (team-match positions).
	for i := range r.SubResults {
		sr := &r.SubResults[i]
		prefix := fmt.Sprintf("subResults[%d].", i)
		if err := validateIpponCounts(prefix, sr.IpponsA, sr.IpponsB); err != nil {
			return err
		}
		if err := validateSubBout(prefix, sr); err != nil {
			return err
		}
	}
	// DecidedByHantei records a referee judges' decision that declares a winner
	// from a tied bout. A winner must be present, the status (if supplied) must
	// be completed, and the scoreline must be tied (equal ippon counts). Encho
	// is NOT required: operators may take a tied match straight to hantei
	// without an overtime period (the encho gate was removed deliberately).
	// The winner/tied/decision checks below mirror validateSubBout; keep both
	// in sync.
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
		if len(r.IpponsA) != len(r.IpponsB) {
			return &ValidationError{
				Field:   "decidedByHantei",
				Message: "requires a tied scoreline — ippon counts must be equal",
			}
		}
		// Hantei is a referee judges' decision that produces a winner from a
		// tied bout. Any other special decision (hikiwake=draw, kiken=withdrawal,
		// fusenpai=no-show, daihyosen=rep-bout…) is semantically incompatible —
		// persisting both would render contradictory suffixes like "Kiken (E) HT".
		// Only the neutral values ("" and "fought") are allowed alongside hantei.
		switch r.Decision {
		case "", "fought":
			// compatible: normal fight decided by judges
		default:
			return &ValidationError{
				Field:   "decidedByHantei",
				Message: fmt.Sprintf("incompatible with decision %q — hantei declares a winner from a tied bout; use '' or 'fought'", r.Decision),
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
	case "kiken-voluntary", "kiken-injury":
		if r.DecisionBy == "" {
			return &ValidationError{Field: "decisionBy", Message: fmt.Sprintf("required when decision is %s", r.Decision)}
		}
		need := 2
		if r.Encho != nil {
			need = 1
		}
		if !winningScoreline(r.IpponsA, r.IpponsB, need) {
			return &ValidationError{
				Field:   "scoreline",
				Message: fmt.Sprintf("%s requires %d-0 scoreline", r.Decision, need),
			}
		}
		if err := r.requireWinnerForDecision(r.Decision); err != nil {
			return err
		}
	case "fusenpai":
		if r.DecisionBy == "" {
			return &ValidationError{Field: "decisionBy", Message: "required when decision is fusenpai"}
		}
		if !winningScoreline(r.IpponsA, r.IpponsB, 2) {
			return &ValidationError{Field: "scoreline", Message: "fusenpai requires 2-0 scoreline"}
		}
		if err := r.requireWinnerForDecision("fusenpai"); err != nil {
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
// 2). 2-2 is therefore an impossible scoreline — the match would have
// ended at 2-1 before either side could score a third.
const maxIpponsPerSide = 2

// validateIpponCounts enforces the best-of-3 ippon invariants on a
// single match (or sub-bout) tally. Rules:
//
//   - len(ipponsA) ≤ 2 and len(ipponsB) ≤ 2
//   - NOT (len(ipponsA) == 2 && len(ipponsB) == 2)   — the 2-2 ban
//
// Field is the JSON-field prefix used in error messages (e.g. "" for a
// top-level match, "subResults[i]." for a sub-bout). Kiken/fusenpai
// scorelines are also bounded by these rules — their own n-0 check in
// validateDecision is strictly tighter (n ≤ 2) so this passes through.
func validateIpponCounts(field string, ipponsA, ipponsB []string) error {
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
	if len(ipponsA) == maxIpponsPerSide && len(ipponsB) == maxIpponsPerSide {
		return &ValidationError{
			Field:   field + "ippons",
			Message: "both sides cannot have 2 ippons (best-of-3 ends at first to 2)",
		}
	}
	return nil
}

// requireWinnerForDecision enforces that Winner is set when a kiken/
// fusenpai is recorded — the engine's eligibility side effect uses
// Winner as the canonical surviving side. Without this, a bulk-score
// or hand-crafted request could record an ineligibility against the
// wrong player.
func (r *ScoreRequest) requireWinnerForDecision(label string) error {
	if r.Winner == "" {
		return &ValidationError{
			Field:   "winner",
			Message: fmt.Sprintf("required when decision is %s (names the surviving side)", label),
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
// "kachinuki-exhaustion", "fusensho" — referee/operator rulings with
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
	if position == -1 {
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
