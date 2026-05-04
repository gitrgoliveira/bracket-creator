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
	ID             string          `yaml:"id" json:"id"`
	Name           string          `yaml:"name" json:"name"`
	Kind           string          `yaml:"kind" json:"kind"`
	Format         string          `yaml:"format" json:"format"`
	TeamSize       int             `yaml:"team_size" json:"teamSize"`
	PoolSize       int             `yaml:"pool_size" json:"poolSize"`
	PoolSizeMode   string          `yaml:"pool_size_mode" json:"poolSizeMode"`
	PoolWinners    int             `yaml:"pool_winners" json:"poolWinners"`
	RoundRobin     bool            `yaml:"round_robin" json:"roundRobin"`
	Courts         []string        `yaml:"courts" json:"courts"`
	StartTime      string          `yaml:"start_time" json:"startTime"`
	Status         string          `yaml:"status" json:"status"`
	Mirror         bool            `yaml:"mirror" json:"mirror"`
	WithZekkenName bool            `yaml:"with_zekken_name" json:"withZekkenName"`
	Players        []helper.Player `yaml:"-" json:"players"`
}

type MatchStatus string

const (
	MatchStatusScheduled MatchStatus = "scheduled"
	MatchStatusRunning   MatchStatus = "running"
	MatchStatusCompleted MatchStatus = "completed"
)

type MatchResult struct {
	ID          string      `json:"id"`
	SideA       string      `json:"sideA"` // Player Name
	SideB       string      `json:"sideB"` // Player Name
	Winner      string      `json:"winner"`
	IpponsA     []string    `json:"ipponsA"` // M, K, D, T, H
	IpponsB     []string    `json:"ipponsB"`
	HansokuA    int         `json:"hansokuA"`
	HansokuB    int         `json:"hansokuB"`
	Decision    string      `json:"decision"` // hantei, etc
	Status      MatchStatus `json:"status"`
	Court       string      `json:"court"`
	ScheduledAt string      `json:"scheduledAt"`
}

type PlayerStanding struct {
	Player      helper.Player `json:"player"`
	Wins        int           `json:"wins"`
	Losses      int           `json:"losses"`
	Draws       int           `json:"draws"`
	IpponsGiven int           `json:"ipponsGiven"`
	IpponsTaken int           `json:"ipponsTaken"`
	Points      int           `json:"points"`
	Rank        int           `json:"rank"`
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
	ScoreA string `json:"scoreA"`
	ScoreB string `json:"scoreB"`
}

type Bracket struct {
	Rounds [][]BracketMatch `json:"rounds"`
}
