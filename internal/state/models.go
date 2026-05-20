package state

import (
	"fmt"
	"time"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
)

type Tournament struct {
	Name     string   `yaml:"name" json:"name"`
	Date     string   `yaml:"date" json:"date"`
	Venue    string   `yaml:"venue" json:"venue"`
	Courts   []string `yaml:"courts" json:"courts"`
	Password string   `yaml:"password" json:"password"`

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

	// Check-in window configuration (HH:MM strings on the tournament day).
	// Informational only; the head table can still manually toggle check-ins
	// after the window closes.
	CheckInWindowStart string `yaml:"check_in_window_start,omitempty" json:"checkInWindowStart,omitempty"`
	CheckInWindowEnd   string `yaml:"check_in_window_end,omitempty" json:"checkInWindowEnd,omitempty"`
}

// ApplyTournamentDefaults fills zero-valued schedule-estimator tuning
// fields on t with their canonical defaults: ClockToElapsedMultiplier=1.5
// and SlowestCourtBufferPct=10. Idempotent; safe to call repeatedly.
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

type CompetitionStatus string

const (
	CompStatusSetup    CompetitionStatus = "setup"
	CompStatusPools    CompetitionStatus = "pools"
	CompStatusPlayoffs CompetitionStatus = "playoffs"
	CompStatusComplete CompetitionStatus = "completed"
	CompStatusInvalid  CompetitionStatus = "invalid"
)

type MatchStatus string

const (
	MatchStatusScheduled MatchStatus = "scheduled"
	MatchStatusRunning   MatchStatus = "running"
	MatchStatusCompleted MatchStatus = "completed"
)

// Competition.Format values.
const (
	CompFormatPools    = "pools"
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

// DecisionDraw is the canonical value for a tied (hikiwake) match.
const DecisionDraw = "hikiwake"

// IsDraw reports whether a match decision string represents a draw.
func IsDraw(decision string) bool {
	return decision == DecisionDraw
}

type SubMatchResult struct {
	Position int      `json:"position"`
	SideA    string   `json:"sideA"`
	SideB    string   `json:"sideB"`
	IpponsA  []string `json:"ipponsA"`
	IpponsB  []string `json:"ipponsB"`
	HansokuA int      `json:"hansokuA"`
	HansokuB int      `json:"hansokuB"`
	Winner   string   `json:"winner"`
	Decision string   `json:"decision"`
}

type MatchResult struct {
	ID             string           `json:"id"`
	SideA          string           `json:"sideA"` // Player/Team Name
	SideB          string           `json:"sideB"`
	Winner         string           `json:"winner"`
	IpponsA        []string         `json:"ipponsA"` // M, K, D, T, H
	IpponsB        []string         `json:"ipponsB"`
	HansokuA       int              `json:"hansokuA"`
	HansokuB       int              `json:"hansokuB"`
	Decision       string           `json:"decision"`
	DecisionBy     string           `json:"decisionBy,omitempty"`
	DecisionReason string           `json:"decisionReason,omitempty"`
	Status         MatchStatus      `json:"status"`
	Court          string           `json:"court"`
	ScheduledAt    string           `json:"scheduledAt"`
	SubResults     []SubMatchResult `json:"subResults,omitempty"`
	Encho          *EnchoMetadata   `json:"encho,omitempty" yaml:"encho,omitempty"`
	QueuePosition  int              `json:"queuePosition,omitempty" yaml:"-"`
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
	ScoreA       string `json:"scoreA"`
	ScoreB       string `json:"scoreB"`
	IsOverridden bool   `json:"isOverridden"`
	// Decision-type metadata mirrors MatchResult so an elimination-stage
	// kiken/fusenpai/encho is reconstructable from bracket.json alone
	// (label rendering, Excel export, SSE replays).
	Decision       string         `json:"decision,omitempty"`
	DecisionBy     string         `json:"decisionBy,omitempty"`
	DecisionReason string         `json:"decisionReason,omitempty"`
	Encho          *EnchoMetadata `json:"encho,omitempty"`
}

type Bracket struct {
	Rounds [][]BracketMatch `json:"rounds"`
}

// ReservedSlot represents a placeholder participant that will be resolved to
// the actual player who achieves a given rank in another competition.
type ReservedSlot struct {
	ID            string `json:"id"`            // unique slot ID
	ParticipantID string `json:"participantID"` // ID of the placeholder in participants.csv
	SourceCompID  string `json:"sourceCompID"`
	SourceRank    int    `json:"sourceRank"`
}

type Announcement struct {
	Message   string    `json:"message" yaml:"message"`
	SentAt    time.Time `json:"sentAt" yaml:"sent_at"`
	ExpiresAt time.Time `json:"expiresAt" yaml:"expires_at"`
}
