// Package domain defines the core domain models for the bracket creator
package domain

// Player represents a tournament participant.
//
// Wire format: JSON tags MUST mirror helper.Player exactly so the two
// types serialise identically — domain.Player is intended as a drop-in
// successor to helper.Player for code outside internal/helper and
// internal/excel (NFR-007). See internal/helper/domainconv.go for
// boundary conversion helpers between the two; the converters live
// helper-side to avoid a helper → domain → helper import cycle.
type Player struct {
	ID          string   `json:"id,omitempty"` // stable UUID assigned at first persist
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Dojo        string   `json:"dojo"`
	Metadata    []string `json:"metadata,omitempty"`
	Tag         string   `json:"tag,omitempty"` // e.g. "manual", "registered", "transfer", "reserved"

	PoolPosition int64  `json:"-"`
	Seed         int    `json:"seed"`
	Number       string `json:"number,omitempty"` // e.g. "K1" — assigned when --number-prefix is set
}

// Matches checks if the player's name exactly matches the given name (case-sensitive)
func (p *Player) Matches(name string) bool {
	return p.Name == name
}

// MatchWinner represents a player who has won a match
type MatchWinner struct {
	PlayerID string
	MatchID  string
}
