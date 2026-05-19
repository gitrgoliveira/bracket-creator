package domain

import (
	"errors"
	"time"
)

// CompetitorStatus tracks whether a player is eligible to compete after
// a kiken/fusenpai withdrawal recorded earlier in the tournament.
//
// FR-034, data-model §3.
type CompetitorStatus struct {
	PlayerID      string    `json:"playerId" yaml:"playerId"`
	Eligible      bool      `json:"eligible" yaml:"eligible"`
	Reinstateable bool      `json:"reinstateable,omitempty" yaml:"reinstateable,omitempty"`
	Reason        string    `json:"reason,omitempty" yaml:"reason,omitempty"`
	MatchID       string    `json:"matchId,omitempty" yaml:"matchId,omitempty"`
	RecordedAt    time.Time `json:"recordedAt" yaml:"recordedAt"`
}

// Sentinel errors so callers can switch on the validation failure
// without parsing strings.
var (
	ErrCompetitorStatusMissingPlayerID = errors.New("competitor_status: playerId must be set")
	ErrCompetitorStatusMissingReason   = errors.New("competitor_status: reason required when not eligible")
)

// Validate enforces FR-034: PlayerID must be set, and an ineligible
// status must carry a non-empty Reason.
func (c CompetitorStatus) Validate() error {
	if c.PlayerID == "" {
		return ErrCompetitorStatusMissingPlayerID
	}
	if !c.Eligible && c.Reason == "" {
		return ErrCompetitorStatusMissingReason
	}
	return nil
}
