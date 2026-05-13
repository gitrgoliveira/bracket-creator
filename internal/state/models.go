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
	ID                string            `yaml:"id" json:"id"`
	Name              string            `yaml:"name" json:"name"`
	Kind              string            `yaml:"kind" json:"kind"`
	Format            string            `yaml:"format" json:"format"`
	TeamSize          int               `yaml:"team_size" json:"teamSize"`
	PoolSize          int               `yaml:"pool_size" json:"poolSize"`
	PoolSizeMode      string            `yaml:"pool_size_mode" json:"poolSizeMode"`
	PoolWinners       int               `yaml:"pool_winners" json:"poolWinners"`
	RoundRobin        bool              `yaml:"round_robin" json:"roundRobin"`
	Courts            []string          `yaml:"courts" json:"courts"`
	StartTime         string            `yaml:"start_time" json:"startTime"`
	Date              string            `yaml:"date" json:"date"`
	Status            CompetitionStatus `yaml:"status" json:"status"`
	Mirror            bool              `yaml:"mirror" json:"mirror"`
	WithZekkenName    bool              `yaml:"with_zekken_name" json:"withZekkenName"`
	NumberPrefix      string            `yaml:"number_prefix,omitempty" json:"numberPrefix,omitempty"`
	HasParticipantIDs bool              `yaml:"has_participant_ids,omitempty" json:"hasParticipantIDs,omitempty"`
	Players           []helper.Player   `yaml:"-" json:"players"`
}

type CompetitionStatus string

const (
	CompStatusSetup    CompetitionStatus = "setup"
	CompStatusPending  CompetitionStatus = "pending"
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

// DecisionDraw is the canonical value for a tied (hikiwake) match. The
// legacy spelling "hikewake" (missing the 'i') was used historically and is
// still accepted by IsDraw() for backward compatibility on existing data,
// but all new writes use this canonical spelling.
const (
	DecisionDraw       = "hikiwake"
	decisionDrawLegacy = "hikewake"
)

// IsDraw reports whether a match decision string represents a draw.
// Accepts both the canonical "hikiwake" and the legacy "hikewake".
func IsDraw(decision string) bool {
	return decision == DecisionDraw || decision == decisionDrawLegacy
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
	ID          string           `json:"id"`
	SideA       string           `json:"sideA"` // Player/Team Name
	SideB       string           `json:"sideB"`
	Winner      string           `json:"winner"`
	IpponsA     []string         `json:"ipponsA"` // M, K, D, T, H
	IpponsB     []string         `json:"ipponsB"`
	HansokuA    int              `json:"hansokuA"`
	HansokuB    int              `json:"hansokuB"`
	Decision    string           `json:"decision"`
	Status      MatchStatus      `json:"status"`
	Court       string           `json:"court"`
	ScheduledAt string           `json:"scheduledAt"`
	SubResults  []SubMatchResult `json:"subResults,omitempty"`
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
