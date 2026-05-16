package state

import (
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
)

type Tournament struct {
	Name     string   `yaml:"name" json:"name"`
	Date     string   `yaml:"date" json:"date"`
	Venue    string   `yaml:"venue" json:"venue"`
	Courts   []string `yaml:"courts" json:"courts"`
	Password string   `yaml:"password" json:"password"`
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

	// Legacy single-phase duration. Captured at unmarshal time and used by
	// ApplyCompetitionDefaults to populate the per-phase fields above when
	// they are zero. Not persisted on save — only here so older YAML files
	// round-trip through the new schema.
	MatchDuration int `yaml:"match_duration,omitempty" json:"matchDuration,omitempty"`

	Players []helper.Player `yaml:"-" json:"players"`
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
	// Swiss intentionally not added in v1 — deferred per A-5.

	PoolFormatFull    = "full"
	PoolFormatPartial = "partial"
)

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
	Player           helper.Player `json:"player"`
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
