package state

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

type Tournament struct {
	Name     string   `yaml:"name" json:"name"`
	Date     string   `yaml:"date" json:"date"`
	Venue    string   `yaml:"venue" json:"venue"`
	Courts   []string `yaml:"courts" json:"courts"`
	Password string   `yaml:"password" json:"password"`

	// DurationDays is the number of consecutive calendar days this
	// tournament spans, starting from Date (Day 1). Default 1 (single-day).
	// Maximum 30. Use Days() to obtain the derived per-day DD-MM-YYYY list.
	//
	// omitempty drops the field only when the value is 0 (Go's zero value),
	// never when it is 1 — so tournaments saved by this code always persist
	// an explicit duration_days. The omitempty matters in the reverse
	// direction: legacy tournament.md files predating this field carry no
	// duration_days key, so they deserialize to 0 and are migrated to 1 by
	// the load path (and ApplyTournamentDefaults).
	DurationDays int `yaml:"duration_days,omitempty" json:"durationDays,omitempty"`

	// AdminPassword gates destructive operations (spec 004 / mp-e21):
	// competition delete, draw/override/invalidate, and participant roster
	// mutations. It is a SEPARATE, higher-privilege credential from
	// Password (which gates the API as a whole).
	//
	// CRITICAL: it is WRITE-ONLY at the API boundary — `json:"-"` means it
	// is never emitted in any response AND never populated by binding a
	// request body into a Tournament. That is the opposite of Password
	// (which is a peer of the credential gating /tournament, so returning
	// it is harmless). AdminPassword is HIGHER privilege than that gate, so
	// leaking it via GET — or letting a main-password holder overwrite it
	// via the bulk PUT — would collapse the separation. It is set only via
	// the dedicated, elevated-gated PUT /api/auth/admin-password handler.
	// File mode only; in locked mode the env-var bcrypt hash is
	// authoritative and any on-disk value here is inert.
	AdminPassword string `yaml:"admin_password,omitempty" json:"-"`

	// Ceremony blocks expressed as human duration strings (e.g. "30m",
	// "1h"). When set, the auto-scheduler reserves a contiguous range
	// at the appropriate point in the day and skips match slots that
	// would land inside it. Optional; zero/empty means no block.
	// FR-056, R9, data-model §6.
	OpeningBlock string `yaml:"opening_block,omitempty" json:"openingBlock,omitempty"`
	LunchBlock   string `yaml:"lunch_block,omitempty" json:"lunchBlock,omitempty"`
	ClosingBlock string `yaml:"closing_block,omitempty" json:"closingBlock,omitempty"`

	// ClockToElapsedMultiplier scales the on-clock match duration to
	// "real elapsed minutes" — coin tosses, scoring transitions, salutes
	// and crossings, etc. Defaults to 1.5 via ApplyTournamentDefaults
	// when zero. FR-055, R9.
	ClockToElapsedMultiplier float64 `yaml:"clock_to_elapsed_multiplier,omitempty" json:"clockToElapsedMultiplier,omitempty"`

	// SlowestCourtBufferPct is the % buffer added when distributing total
	// elapsed minutes across N parallel courts — the slowest court usually
	// runs longer than the mean. Defaults to 10 via ApplyTournamentDefaults
	// when zero. FR-057, R9.
	SlowestCourtBufferPct int `yaml:"slowest_court_buffer_pct,omitempty" json:"slowestCourtBufferPct,omitempty"`

	// Mode selects the auth posture for the whole tournament, chosen at
	// creation and IMMUTABLE thereafter (mp-7h7). "officiated" (default)
	// gates the full admin surface behind X-Tournament-Password.
	// "self-run" inverts the boundary: constructive actions (scoring,
	// check-in, start/complete) are public; only destructive actions
	// (those already gated by RequireElevatedPassword / enforceElevated)
	// still require X-Admin-Password. omitempty means older tournament.md
	// files (Mode == "") normalise to "officiated" via ApplyTournamentDefaults.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`

	// Public tournament info fields (mp-ef3). All optional (omitempty).
	// Rendered read-only on the viewer home page; editable in the admin setup form.
	// PublicURL is the externally-shareable base URL for this tournament (mp-s1gl).
	// When set it overrides window.location.origin for QR codes and share links.
	PublicURL    string              `yaml:"public_url,omitempty" json:"publicURL,omitempty"`
	VenueAddress string              `yaml:"venue_address,omitempty" json:"venueAddress,omitempty"`
	VenueMapURL  string              `yaml:"venue_map_url,omitempty" json:"venueMapURL,omitempty"`
	OpeningTime  string              `yaml:"opening_time,omitempty" json:"openingTime,omitempty"`
	ClosingTime  string              `yaml:"closing_time,omitempty" json:"closingTime,omitempty"`
	RulesURL     string              `yaml:"rules_url,omitempty" json:"rulesURL,omitempty"`
	AwardsNote   string              `yaml:"awards_note,omitempty" json:"awardsNote,omitempty"`
	InfoNotes    string              `yaml:"info_notes,omitempty" json:"infoNotes,omitempty"`
	Contacts     []TournamentContact `yaml:"contacts,omitempty" json:"contacts,omitempty"`

	// Sponsors is the ordered list of sponsor logos to display on the
	// public viewer home and the /display TV/lobby surfaces (mp-c38).
	// Stored as omitempty so legacy tournament.md files without sponsors
	// round-trip cleanly (no `sponsors: []` key emitted).
	Sponsors []Sponsor `yaml:"sponsors,omitempty" json:"sponsors,omitempty"`

	// Theme holds optional branding overrides: custom accent colors and a
	// tournament logo (mp-scf). All fields are omitempty so existing
	// tournament.md files without a theme block round-trip cleanly.
	Theme *Theme `yaml:"theme,omitempty" json:"theme,omitempty"`
}

// TournamentContact is a single contact entry for attendees (mp-ef3).
// Label is a short description (e.g. "Email", "Phone") and Value is the
// contact detail (email address, phone number, URL, etc.).
type TournamentContact struct {
	Label string `yaml:"label" json:"label"`
	Value string `yaml:"value" json:"value"`
}

// Sponsor is a single sponsor logo entry. File is the server-generated
// random filename under tournament-data/sponsors/; Name is the alt text;
// Link is optional and, when set, makes the logo clickable on the viewer
// surface only (display surfaces never render anchors). See mp-c38.
type Sponsor struct {
	Name string `yaml:"name" json:"name"`
	File string `yaml:"file" json:"file"`
	Link string `yaml:"link,omitempty" json:"link,omitempty"`
}

// MaxSponsors is the per-tournament sponsor count cap (mp-c38). Realistic
// count is 1–4; 6 leaves headroom without enabling abuse.
const MaxSponsors = 6

// MaxSponsorNameLen and MaxSponsorLinkLen bound the metadata fields.
const (
	MaxSponsorNameLen = 80
	MaxSponsorLinkLen = 500
)

// Sentinel errors returned by ValidateSponsor so handlers can map them
// to specific HTTP status codes without string-matching.
var (
	ErrSponsorNameRequired = errors.New("name is required (1–80 chars)")
	ErrSponsorNameTooLong  = errors.New("name must be ≤80 chars")
	ErrSponsorLinkTooLong  = errors.New("link must be ≤500 chars")
	ErrSponsorLinkInvalid  = errors.New("link must be a valid http(s) URL")
)

// ValidateSponsor checks name length and link format. Centralises the
// rules so handlers, tests, and future import paths agree. Name/link
// must already be trimmed by the caller.
func ValidateSponsor(s Sponsor) error {
	if s.Name == "" {
		return ErrSponsorNameRequired
	}
	if len([]rune(s.Name)) > MaxSponsorNameLen {
		return ErrSponsorNameTooLong
	}
	if s.Link == "" {
		return nil
	}
	if len(s.Link) > MaxSponsorLinkLen {
		return ErrSponsorLinkTooLong
	}
	u, err := url.Parse(s.Link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return ErrSponsorLinkInvalid
	}
	return nil
}

// Theme holds per-tournament branding overrides (mp-scf). PrimaryColor and
// AccentSoftColor are CSS hex values (#rrggbb). WindowTitle overrides the
// browser tab/window title; it defaults to "Bracket Creator Mobile" when
// empty. LogoPath stores the uploaded logo filename under
// tournament-data/branding/; it is NOT exposed in JSON responses (the logo
// is served via GET /api/branding/logo instead).
// All fields are optional; omit the whole block for the default styling.
type Theme struct {
	PrimaryColor    string `yaml:"primary_color,omitempty"    json:"primaryColor,omitempty"`
	AccentSoftColor string `yaml:"accent_soft_color,omitempty" json:"accentSoftColor,omitempty"`
	WindowTitle     string `yaml:"window_title,omitempty"      json:"windowTitle,omitempty"`
	LogoPath        string `yaml:"logo_path,omitempty"         json:"-"` // disk filename; served via /api/branding/logo
}

const maxWindowTitleLen = 100

var hexColorRE = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ValidateTheme returns an error when any non-empty color field is not a
// valid 6-digit CSS hex value, or when WindowTitle exceeds 100 characters.
// An entirely nil/empty Theme is always valid.
func ValidateTheme(theme *Theme) error {
	if theme == nil {
		return nil
	}
	if theme.PrimaryColor != "" && !hexColorRE.MatchString(theme.PrimaryColor) {
		return errors.New("primaryColor must be a 6-digit hex color (e.g. #1d3557)")
	}
	if theme.AccentSoftColor != "" && !hexColorRE.MatchString(theme.AccentSoftColor) {
		return errors.New("accentSoftColor must be a 6-digit hex color (e.g. #e7eaf3)")
	}
	if len([]rune(theme.WindowTitle)) > maxWindowTitleLen {
		return fmt.Errorf("windowTitle must be %d characters or fewer", maxWindowTitleLen)
	}
	return nil
}

// Tournament mode constants (mp-7h7).
const (
	TournamentModeOfficiated = "officiated" // default: main-gate on all admin routes
	TournamentModeSelfRun    = "self-run"   // main-gate skipped; elevated gate stays
)

// ValidateTournamentMode reports whether the given mode value is acceptable.
// Empty string is treated as "officiated" (backward compatibility). Only
// the two defined constants are accepted; any other value returns an error.
func ValidateTournamentMode(mode string) error {
	switch mode {
	case "", TournamentModeOfficiated, TournamentModeSelfRun:
		return nil
	default:
		return fmt.Errorf("unknown tournament mode %q (expected %q or %q)",
			mode, TournamentModeOfficiated, TournamentModeSelfRun)
	}
}

// ApplyTournamentDefaults fills zero-valued schedule-estimator tuning
// fields on t with their canonical defaults: ClockToElapsedMultiplier=1.5,
// SlowestCourtBufferPct=10, and DurationDays=1. Idempotent; safe to call
// repeatedly.
// Also normalizes empty Mode to TournamentModeOfficiated so that
// tournament.md files predating mp-7h7 behave as officiated tournaments.
// FR-055, FR-057, R9.
func ApplyTournamentDefaults(t *Tournament) {
	if t == nil {
		return
	}
	if t.ClockToElapsedMultiplier == 0 {
		t.ClockToElapsedMultiplier = 1.5
	}
	if t.SlowestCourtBufferPct == 0 {
		t.SlowestCourtBufferPct = 10
	}
	if t.DurationDays == 0 {
		t.DurationDays = 1
	}
	if t.Mode == "" {
		t.Mode = TournamentModeOfficiated
	}
	if len(t.Courts) == 0 {
		// Default to at least one court ("A") for legacy or malformed configs
		t.Courts = []string{"A"}
	}
}

// Days returns the ordered list of DD-MM-YYYY calendar day strings
// covered by the tournament, derived from Date + DurationDays. The list
// has exactly DurationDays entries (Day 1 = Date, Day 2 = Date+1, …).
//
// Edge cases — never panics:
//   - If Date is empty or unparseable, returns nil (no day list available).
//   - If DurationDays < 1, returns nil.
//
// Consumers should call ApplyTournamentDefaults before Days() to ensure
// DurationDays has its correct minimum of 1.
func (t *Tournament) Days() []string {
	if t == nil || t.DurationDays < 1 || t.Date == "" {
		return nil
	}
	base, err := time.Parse("02-01-2006", t.Date)
	if err != nil {
		return nil
	}
	days := make([]string, t.DurationDays)
	for i := range days {
		days[i] = base.AddDate(0, 0, i).Format("02-01-2006")
	}
	return days
}

// FightingSpiritAward is an optional, per-competition individual honour
// (敢闘賞 / kantōshō) independent of the placement podium. Recipient is a
// plain name string for parity with the rest of the system (no UUID ref) —
// it is display-only and matches nothing.
type FightingSpiritAward struct {
	Title         string `yaml:"title" json:"title"`
	RecipientName string `yaml:"recipient_name" json:"recipientName"`
	RecipientDojo string `yaml:"recipient_dojo,omitempty" json:"recipientDojo,omitempty"`
}

type Competition struct {
	ID                   string            `yaml:"id" json:"id"`
	Name                 string            `yaml:"name" json:"name"`
	Kind                 string            `yaml:"kind" json:"kind"`
	Format               string            `yaml:"format" json:"format"`
	PoolFormat           string            `yaml:"pool_format,omitempty" json:"poolFormat,omitempty"` // "full" (default) | "partial"
	TeamSize             int               `yaml:"team_size" json:"teamSize"`
	PoolSize             int               `yaml:"pool_size" json:"poolSize"`
	PoolSizeMode         string            `yaml:"pool_size_mode" json:"poolSizeMode"`
	PoolWinners          int               `yaml:"pool_winners" json:"poolWinners"`
	RoundRobin           bool              `yaml:"round_robin" json:"roundRobin"`
	Courts               []string          `yaml:"courts" json:"courts"`
	StartTime            string            `yaml:"start_time" json:"startTime"`
	Date                 string            `yaml:"date" json:"date"`
	Status               CompetitionStatus `yaml:"status" json:"status"`
	Mirror               bool              `yaml:"mirror" json:"mirror"`
	WithZekkenName       bool              `yaml:"with_zekken_name" json:"withZekkenName"`
	NumberPrefix         string            `yaml:"number_prefix,omitempty" json:"numberPrefix,omitempty"`
	HasParticipantIDs    bool              `yaml:"has_participant_ids,omitempty" json:"hasParticipantIDs,omitempty"`
	PoolMatchDuration    int               `yaml:"pool_match_duration,omitempty" json:"poolMatchDuration,omitempty"`
	PlayoffMatchDuration int               `yaml:"playoff_match_duration,omitempty" json:"playoffMatchDuration,omitempty"`
	// MaxEnchoPeriods caps how many encho (overtime) periods one match
	// may run before the operator must call daihyosen. Zero means
	// unlimited (FIK general default). T104, CHK029.
	MaxEnchoPeriods int `yaml:"max_encho_periods,omitempty" json:"maxEnchoPeriods,omitempty"`

	// TeamMatchType selects the team-match format (FR-044). Empty value
	// is treated as TeamMatchTypeFixed for backward compatibility — all
	// N×1 bouts are pre-scheduled by position. TeamMatchTypeKachinuki
	// schedules only the first bout; subsequent bouts are derived
	// dynamically from prior bout outcomes ("winner stays on"). See
	// engine/kachinuki.go for the advancement semantics. Ignored when
	// TeamSize is 0 (individual competitions).
	TeamMatchType TeamMatchType `yaml:"team_match_type,omitempty" json:"teamMatchType,omitempty"`

	// Legacy single-phase duration. Captured at unmarshal time and used by
	// ApplyCompetitionDefaults to populate the per-phase fields above when
	// they are zero. Not persisted on save — only here so older YAML files
	// round-trip through the new schema.
	MatchDuration int `yaml:"match_duration,omitempty" json:"matchDuration,omitempty"`

	// SwissRounds is the number of rounds played in a Swiss-format
	// competition (FR-050a). Ignored when Format != CompFormatSwiss.
	// Persisted so resuming a Swiss tournament reads the same round
	// budget on subsequent loads.
	SwissRounds int `yaml:"swiss_rounds,omitempty" json:"swissRounds,omitempty"`

	// SwissCurrentRound tracks which round has been generated so far
	// (FR-050d). 0 = not started; the value increments after each
	// successful GenerateSwissRound. Used by the "Generate next round"
	// gate to refuse re-generation of an in-progress round.
	SwissCurrentRound int `yaml:"swiss_current_round,omitempty" json:"swissCurrentRound,omitempty"`

	// Naginata selects the Naginata ippon set for this competition.
	// When true, the score editor offers an extra "S" (Sune) button
	// in addition to the standard M/K/D/T/H set. Default false = Kendo.
	Naginata bool `yaml:"naginata,omitempty" json:"naginata"`

	CheckInEnabled bool `yaml:"check_in_enabled,omitempty" json:"checkInEnabled,omitempty"`

	FightingSpiritAwards []FightingSpiritAward `yaml:"fighting_spirit_awards,omitempty" json:"fightingSpiritAwards,omitempty"`

	// LeagueTiebreakTopN is the highest finishing position (rank) that warrants
	// an operator-initiated tie-breaker when teams are tied at the end of league
	// play. Meaningful only for team-league competitions (Format == "league" &&
	// TeamSize > 0). Allowed values are 3 (default) and 4.
	//
	// A "consequential" tied group is one whose position range intersects the
	// band [1..LeagueTiebreakTopN] — i.e. its best (lowest-numbered) position is
	// ≤ TopN. For example, with TopN=3 a tie involving teams at positions 1–2 is
	// consequential (it affects who wins or is runner-up), but a tie at positions
	// 4–5 is not. When the best-placed team in a tied group finishes WORSE than
	// position TopN (its top position is strictly greater than TopN, so the whole
	// group sits below the band), the group is non-consequential and no
	// tie-breaker is needed.
	//
	// The kendo convention of two joint 3rd places interacts here: when
	// LeagueTwoThirdPlaces is true and TopN is 3 or 4, a tied group that sits
	// ENTIRELY at position 3 or below does not need a 3rd-vs-4th decider —
	// both teams are awarded 3rd place and the group is non-consequential.
	// Zero is treated as the default (3) for a team league (Format == "league"
	// && Kind == "team") at draw time.
	LeagueTiebreakTopN int `yaml:"league_tiebreak_top_n,omitempty" json:"leagueTiebreakTopN,omitempty"`

	// LeagueTwoThirdPlaces controls whether two joint 3rd places are awarded
	// when multiple teams tie at the 3rd-position boundary. Meaningful only for
	// team-league competitions.
	//
	// When true, a tied group whose ENTIRE position range falls at position ≥ 3
	// (i.e. all tied teams are at 3rd place or below) is treated as
	// non-consequential — both receive 3rd place and no tie-breaker is required.
	// This implements the standard kendo convention where there is no bronze
	// match; two teams can share 3rd.
	//
	// When false (default), all ties within [1..LeagueTiebreakTopN] are
	// consequential and may require a tie-breaker.
	LeagueTwoThirdPlaces bool `yaml:"league_two_third_places,omitempty" json:"leagueTwoThirdPlaces,omitempty"`

	// LeagueTiebreakFinalized is set to true by the operator via
	// POST /api/competitions/:id/league-tiebreak/finalize to accept the
	// current standings as final without running a tie-breaker. When true,
	// LeagueTiebreakCandidates returns an empty slice (even if consequential
	// ties remain), which allows MaybeAutoCompletePools to transition the
	// competition to CompStatusComplete. Phase 3b.
	LeagueTiebreakFinalized bool `yaml:"league_tiebreak_finalized,omitempty" json:"leagueTiebreakFinalized,omitempty"`

	Players []domain.Player `yaml:"-" json:"players"`
}

// ApplyCompetitionDefaults fills zero-valued per-phase durations from the
// legacy MatchDuration field. Idempotent; safe to call repeatedly.
//
// FR-054, NFR-025, R9: old config.md files predating per-phase durations
// carry only `match_duration`. We MUST preserve their schedule estimates.
func ApplyCompetitionDefaults(c *Competition) {
	if c == nil {
		return
	}
	if c.PoolMatchDuration == 0 && c.MatchDuration > 0 {
		c.PoolMatchDuration = c.MatchDuration
	}
	if c.PlayoffMatchDuration == 0 && c.MatchDuration > 0 {
		c.PlayoffMatchDuration = c.MatchDuration
	}
}

// IsPlayoffEnabled reports whether this competition runs a knockout/playoff
// phase. League and pure-pools formats do not; mixed and playoffs do.
//
// FR-050, FR-051: when Format == "league", the UI must hide playoff-bracket
// affordances and present pool standings as final.
func (c Competition) IsPlayoffEnabled() bool {
	switch c.Format {
	case CompFormatPlayoffs, CompFormatMixed:
		return true
	default:
		return false
	}
}

// EffectivePoolWinners returns the number of finishers each pool promotes to the
// knockout, defaulting to 2 when unset (<=0). Single source of truth for the
// qualifier count so the draw-time validation (pools.go), preview-bracket build
// (bracket.go), incremental seeding (knockout.go), and schedule estimation
// (estimate_schedule.go) cannot drift from one another.
func (c Competition) EffectivePoolWinners() int {
	if c.PoolWinners > 0 {
		return c.PoolWinners
	}
	return 2
}

type CompetitionStatus string

const (
	CompStatusSetup     CompetitionStatus = "setup"
	CompStatusDrawReady CompetitionStatus = "draw-ready"
	CompStatusPools     CompetitionStatus = "pools"
	CompStatusPlayoffs  CompetitionStatus = "playoffs"
	CompStatusComplete  CompetitionStatus = "completed"
	CompStatusInvalid   CompetitionStatus = "invalid"
)

type MatchStatus string

const (
	MatchStatusScheduled MatchStatus = "scheduled"
	MatchStatusRunning   MatchStatus = "running"
	MatchStatusCompleted MatchStatus = "completed"
)

// Competition.Format values.
const (
	CompFormatPlayoffs = "playoffs"
	CompFormatMixed    = "mixed"  // FR-050
	CompFormatLeague   = "league" // FR-050
	CompFormatSwiss    = "swiss"  // FR-050, FR-050a (US13)

	PoolFormatFull    = "full"
	PoolFormatPartial = "partial"
)

// TeamMatchType selects the team-match format. FR-044.
//
//   - TeamMatchTypeFixed: every N×1 bout is scheduled up-front by
//     position (Senpo×Senpo, Jiho×Jiho, …). This is the historical
//     default and the empty value resolves to it for backward compat.
//   - TeamMatchTypeKachinuki: only the first bout is scheduled. After
//     each bout, the winner stays on and faces the next un-retired
//     player from the losing side; on a hikiwake both retire. The team
//     match ends when one side has no remaining un-retired players.
//     See engine/kachinuki.go.
type TeamMatchType string

const (
	TeamMatchTypeFixed     TeamMatchType = "fixed"
	TeamMatchTypeKachinuki TeamMatchType = "kachinuki"
)

// ValidateTeamMatchType returns nil when the value is acceptable on the
// given Competition (empty == fixed default, kachinuki requires
// TeamSize >= 2). FR-044.
func ValidateTeamMatchType(t TeamMatchType, teamSize int) error {
	switch t {
	case "", TeamMatchTypeFixed:
		return nil
	case TeamMatchTypeKachinuki:
		if teamSize < 2 {
			return fmt.Errorf("kachinuki requires teamSize >= 2")
		}
		return nil
	default:
		return fmt.Errorf("unknown teamMatchType %q (expected %q or %q)",
			t, TeamMatchTypeFixed, TeamMatchTypeKachinuki)
	}
}

// ValidateCompetitionTeamSize rejects invalid teamSize values. Engine code
// uses both TeamSize > 0 and Kind == "team" in different paths; an inconsistent
// pair (e.g. kind="individual" with teamSize=3) causes misclassification:
//   - negative values are always invalid
//   - teamSize == 1 is always invalid (would flag as team via TeamSize > 0)
//   - non-team kinds require teamSize == 0
//   - kind == "team" requires teamSize >= 2
func ValidateCompetitionTeamSize(kind string, teamSize int) error {
	if teamSize < 0 {
		return fmt.Errorf("teamSize must be non-negative")
	}
	if teamSize == 1 {
		return fmt.Errorf("teamSize 1 is invalid: use 0 for individual competitions or >= 2 for teams")
	}
	if kind != "team" && teamSize > 0 {
		return fmt.Errorf("non-team competitions require teamSize == 0 (got %d)", teamSize)
	}
	if kind == "team" && teamSize < 2 {
		return fmt.Errorf("team competitions require teamSize >= 2")
	}
	return nil
}

// DecisionDraw is the canonical value for a tied (hikiwake) match.
const DecisionDraw = "hikiwake"

// IsDraw reports whether a match decision string represents a draw.
func IsDraw(decision string) bool {
	return decision == DecisionDraw
}

type SubMatchResult struct {
	Position        int            `json:"position"`
	SideA           string         `json:"sideA"`
	SideB           string         `json:"sideB"`
	IpponsA         []string       `json:"ipponsA"`
	IpponsB         []string       `json:"ipponsB"`
	HansokuA        int            `json:"hansokuA"`
	HansokuB        int            `json:"hansokuB"`
	Winner          string         `json:"winner"`
	Decision        string         `json:"decision"`
	DecidedByHantei bool           `json:"decidedByHantei,omitempty" yaml:"decided_by_hantei,omitempty"`
	Encho           *EnchoMetadata `json:"encho,omitempty"           yaml:"encho,omitempty"`
}

type MatchResult struct {
	ID     string `json:"id"`
	SideA  string `json:"sideA"` // Player/Team Name
	SideB  string `json:"sideB"`
	Winner string `json:"winner"`
	// SideAID/SideBID/WinnerID carry the participant UUID for each side and
	// the winner when available. Sides are stored by name everywhere else,
	// but a name is not unique within a competition — two participants from
	// different dojos may share a name (CheckDuplicateEntriesByNameDojo only
	// rejects same-name AND same-dojo). These ids let consumers (e.g. the
	// league matrix) cross-reference a match cell to the right row/column
	// player AND tell apart the winner when two identical-name players meet.
	// Purely additive metadata: all Go scoring/standings logic still keys on
	// name, and these stay empty for legacy data, so behavior is unchanged
	// when ids are absent. omitempty + append-only CSV columns keep old
	// files/readers fully compatible.
	SideAID  string `json:"sideAId,omitempty"`
	SideBID  string `json:"sideBId,omitempty"`
	WinnerID string `json:"winnerId,omitempty"`
	// WinnerSide is a transient hint ("A"/"B") set by scoring handlers that
	// know the winning SIDE unambiguously (e.g. quick-score, where the
	// winner is decided by ippon counts, not a name). writeMatchResult uses
	// it to resolve WinnerID from the stored side ids even when both sides
	// share a name. Never persisted (json/CSV omit) — it only carries the
	// side decision from the handler to the id-resolution step.
	WinnerSide     string           `json:"-" yaml:"-"`
	IpponsA        []string         `json:"ipponsA"` // M, K, D, T, H
	IpponsB        []string         `json:"ipponsB"`
	HansokuA       int              `json:"hansokuA"`
	HansokuB       int              `json:"hansokuB"`
	Decision       string           `json:"decision"`
	DecisionBy     string           `json:"decisionBy,omitempty"`
	DecisionReason string           `json:"decisionReason,omitempty"`
	Status         MatchStatus      `json:"status"`
	Court          string           `json:"court"`
	Round          int              `json:"round" yaml:"round"`
	ScheduledAt    string           `json:"scheduledAt"`
	SubResults     []SubMatchResult `json:"subResults,omitempty"`
	Encho          *EnchoMetadata   `json:"encho,omitempty" yaml:"encho,omitempty"`
	QueuePosition  int              `json:"queuePosition,omitempty" yaml:"-"`
	// DecidedByHantei is true when the winner was declared by referee
	// hantei on a tied bout (FIK Article 7-5 / 29-6). Overtime is not a
	// precondition: a tied scoreline may be taken straight to a judges'
	// decision. Distinguishes a judges' decision from an ippon-derived win
	// for stats, audit, and display. Zero value omitted from the wire.
	//
	// Pointer semantics at the API boundary: when a client omits the
	// field (nil) on a BRACKET-match score request, the engine preserves
	// whatever value is already stored; when the client explicitly sends
	// true or false the engine applies it. This prevents a re-score that
	// doesn't mention the flag from silently clearing a previously-
	// recorded hantei decision.
	//
	// Preserve-on-nil applies ONLY to bracket matches (see
	// engine/scoring.go:recordBracketMatchResult and
	// engine/scoring_tx.go's bracket commit branch — both gate the
	// assignment on result.DecidedByHantei != nil). Pool matches are
	// merged with `*r = *result`, so a nil pointer there will clear any
	// stored value. This is acceptable in practice because FIK rules
	// don't permit hantei in pool play (see persistence caveat below).
	//
	// On READ paths that project BracketMatch.DecidedByHantei (bool) back
	// into MatchResult for SSE / HTTP responses, use HanteiPtr below so
	// non-hantei matches emit nil (omitempty), keeping the wire payload
	// minimal and signalling "no hantei" by absence rather than an
	// explicit false.
	//
	// Persistence caveat: pool matches are stored in pool-matches.csv,
	// whose column layout does NOT include this field — so a hantei
	// decision on a pool match survives in-memory and on the SSE wire,
	// but does NOT survive a server restart. Bracket matches are stored
	// in bracket.json, which serializes the full struct, so the flag
	// survives there. See BracketMatch.DecidedByHantei for the mirror;
	// pool-level hantei is a rare-enough case (FIK doesn't normally
	// allow it in pool play) that the gap is acceptable. The yaml tag
	// is retained for future YAML-serialised contexts.
	DecidedByHantei *bool `json:"decidedByHantei,omitempty" yaml:"decided_by_hantei,omitempty"`
	// ResultSource records how the result was submitted: "admin" (operator with
	// password), "self-reported" (participant in self-run mode), or "" (legacy/
	// unset). Set by the score handler; omitted from wire when empty.
	ResultSource string `json:"resultSource,omitempty" yaml:"result_source,omitempty"`
	// CorrectionReason is a mandatory audit justification when an operator
	// overwrites a completed match result. Format: "<category>: <note>" (e.g.
	// "Scoring error: wrong waza entered"). Only required on corrections
	// (completed → completed); omitted on first completions. Append-only CSV
	// column (rec index 19, the 20th column, after the original 19) and the
	// bracket.json field keep older files fully compatible.
	CorrectionReason string `json:"correctionReason,omitempty" yaml:"correction_reason,omitempty"`
	// Rev is a client-monotonic revision counter carried on "running"-status
	// autosave writes. The server uses it (scoped to RevSession) to drop stale
	// in-flight writes that arrive out of order after a reconnect flush
	// (C2 rev-guard). It is wire-only: never serialized to CSV (no column) nor
	// to bracket.json (which uses BracketMatch), and yaml:"-" keeps it out of
	// config persistence. A plain value copy of MatchResult carries it in
	// memory, which is harmless. Defaulting to 0 (omitempty) means a payload
	// without the field is treated as unversioned and always proceeds.
	Rev int64 `json:"rev,omitempty" yaml:"-"`
	// RevSession identifies the client scoring session (one per page load,
	// random). The rev-guard compares Rev ONLY within the same RevSession; a
	// write from a new session (page reload / different device) always takes
	// over rather than being dropped as stale against a high-water mark left
	// by a prior session. Same wire-only persistence guarantees as Rev.
	RevSession string `json:"revSession,omitempty" yaml:"-"`
}

// HanteiPtr returns &b when b is true, nil otherwise. Use on READ paths
// that project a BracketMatch.DecidedByHantei (bool) into a MatchResult
// (which uses *bool with omitempty) so the wire payload OMITS the field
// for non-hantei matches rather than emitting an explicit "false".
// Always assigning &bm.DecidedByHantei would leak a non-nil pointer for
// every non-hantei match, defeating the omitempty contract.
func HanteiPtr(b bool) *bool {
	if !b {
		return nil
	}
	return &b
}

// EnchoMetadata records overtime / sudden-death periods played in a
// match. Read/persisted only in Slice 1; the score endpoint accepts it
// but does not yet act on it. Slice 3 (T076) will wire it into the
// decision logic.
//
// FR-032
type EnchoMetadata struct {
	PeriodCount int `json:"periodCount" yaml:"periodCount"`
}

// Clone returns a deep copy of the encho metadata, or nil if e is nil.
// Used by the match/bracket copy paths so cached state never shares an
// Encho pointer with a returned value.
func (e *EnchoMetadata) Clone() *EnchoMetadata {
	if e == nil {
		return nil
	}
	c := *e
	return &c
}

// cloneSubResults deep-copies a sub-result slice so cached state never shares
// the IpponsA/IpponsB slices or nested Encho pointers with a returned value.
// Used by both the pool match copy path (copyMatchResults) and the bracket
// copy path (copyBracket) — keep them aligned. Returns nil for a nil input so
// the omitempty/preserve semantics round-trip unchanged.
func cloneSubResults(subs []SubMatchResult) []SubMatchResult {
	if subs == nil {
		return nil
	}
	out := make([]SubMatchResult, len(subs))
	for i, sr := range subs {
		out[i] = sr
		if sr.IpponsA != nil {
			out[i].IpponsA = make([]string, len(sr.IpponsA))
			copy(out[i].IpponsA, sr.IpponsA)
		}
		if sr.IpponsB != nil {
			out[i].IpponsB = make([]string, len(sr.IpponsB))
			copy(out[i].IpponsB, sr.IpponsB)
		}
		out[i].Encho = sr.Encho.Clone()
	}
	return out
}

type PlayerStanding struct {
	Player           domain.Player `json:"player"`
	Wins             int           `json:"wins"`
	Losses           int           `json:"losses"`
	Draws            int           `json:"draws"`
	IpponsGiven      int           `json:"ipponsGiven"`
	IpponsTaken      int           `json:"ipponsTaken"`
	Points           int           `json:"points"`
	ScoreSummary     string        `json:"scoreSummary,omitempty"`
	Rank             int           `json:"rank"`
	IsOverridden     bool          `json:"isOverridden"`
	IndividualWins   int           `json:"individualWins,omitempty"`
	IndividualLosses int           `json:"individualLosses,omitempty"`
	IndividualDraws  int           `json:"individualDraws,omitempty"`
	PointsWon        int           `json:"pointsWon,omitempty"`
	PointsLost       int           `json:"pointsLost,omitempty"`
}

type BracketMatch struct {
	ID          string      `json:"id"`
	SideA       string      `json:"sideA"`
	SideB       string      `json:"sideB"`
	Winner      string      `json:"winner"`
	Status      MatchStatus `json:"status"`
	Court       string      `json:"court"`
	ScheduledAt string      `json:"scheduledAt"`
	// Additional fields from design
	ScoreA        string `json:"scoreA"`
	ScoreB        string `json:"scoreB"`
	IsOverridden  bool   `json:"isOverridden"`
	QueuePosition int    `json:"queuePosition,omitempty"`
	// Decision-type metadata mirrors MatchResult so an elimination-stage
	// kiken/fusenpai/encho is reconstructable from bracket.json alone
	// (label rendering, Excel export, SSE replays).
	Decision       string         `json:"decision,omitempty"`
	DecisionBy     string         `json:"decisionBy,omitempty"`
	DecisionReason string         `json:"decisionReason,omitempty"`
	Encho          *EnchoMetadata `json:"encho,omitempty"`
	// DecidedByHantei mirrors MatchResult.DecidedByHantei for bracket reads.
	// YAML tag included for parity with MatchResult and future YAML-serialised contexts.
	DecidedByHantei bool `json:"decidedByHantei,omitempty" yaml:"decided_by_hantei,omitempty"`
	// SubResults persists per-bout results for team bracket matches so the
	// score editor can restore hantei state and bout-level detail on re-open.
	SubResults []SubMatchResult `json:"subResults,omitempty"`
	// ResultSource mirrors MatchResult.ResultSource for bracket matches.
	ResultSource string `json:"resultSource,omitempty"`
	// CorrectionReason mirrors MatchResult.CorrectionReason for bracket
	// matches, persisted in bracket.json for audit. Set when an operator
	// overwrites a completed result (correction), omitted on first completion.
	CorrectionReason string `json:"correctionReason,omitempty"`
	// Display metadata (mp-7f2w) — additive, computed at generation time so the
	// viewer can render the bracket with the SAME effective-round columns as the
	// printed Excel Tree sheet (matches grouped by depth-from-root, structural
	// byes skipping a column) instead of the balanced pow2 rounds. These fields
	// are purely for rendering; the resolution/scoring/scheduling logic continues
	// to use the positional ID + "Winner of rX-mY" scheme unchanged.
	//
	// DisplayRound is the effective round counted from the final (1 = Final,
	// 2 = Semifinal, …). 0 means unset — either a legacy bracket generated before
	// this field existed (viewer falls back to positional rendering) or a Hidden
	// phantom match. Hidden marks a structural-bye match (empty-vs-empty dead
	// match, or a latent bye where one side never had an opponent) that is not a
	// real bout and must not be drawn as a match card. Feeders holds the IDs of
	// the two real feeder matches whose winners meet here, in [A, B] order; an
	// empty string means that side is a seeded entrant / bye (no connector line).
	DisplayRound int      `json:"displayRound,omitempty"`
	Hidden       bool     `json:"hidden,omitempty"`
	Feeders      []string `json:"feeders,omitempty"`
}

type Bracket struct {
	Rounds [][]BracketMatch `json:"rounds"`
	// Preview marks a bracket whose leaves are pool-origin PLACEHOLDERS
	// (e.g. "Pool A 1st") rather than resolved players. It is generated on a
	// mixed (Pools + Knockout) competition at draw time so the operator can
	// see the elimination structure that the pools feed — mirroring the Excel
	// Tree sheet. A preview bracket is read-only: the actual knockout is
	// played in the separate playoffs competition created from this source.
	Preview bool `json:"preview,omitempty"`
}

type Announcement struct {
	ID        string    `json:"id" yaml:"id"`
	Message   string    `json:"message" yaml:"message"`
	SentAt    time.Time `json:"sentAt" yaml:"sent_at"`
	ExpiresAt time.Time `json:"expiresAt" yaml:"expires_at"`
}
