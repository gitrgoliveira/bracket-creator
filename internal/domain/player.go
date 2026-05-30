// Package domain defines the core domain models for the bracket creator
package domain

// Player represents a tournament participant. It is the canonical
// participant type across state, engine, mobileapp, and helper
// packages; internal/helper re-exports it under helper.Player as a
// type alias for rendering-side ergonomics (NFR-007).
type Player struct {
	ID          string   `json:"id,omitempty"` // stable UUID assigned at first persist
	Name        string   `json:"name"`
	DisplayName string   `json:"displayName"`
	Dojo        string   `json:"dojo"`
	Metadata    []string `json:"metadata,omitempty"`
	Tag         string   `json:"tag,omitempty"` // e.g. "manual", "registered", "transfer"
	CheckedIn   bool     `json:"checkedIn"`

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
