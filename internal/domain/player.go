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
	Source      string   `json:"source,omitempty"` // registration source, how the entry was created: registered (self-registered), manual (operator-added), transfer (imported)
	CheckedIn   bool     `json:"checkedIn"`

	PoolPosition int64 `json:"-"` // internal: used for Excel output and pool-draw ordering; not serialised to JSON because the value is inconsistently indexed across producer paths (0-based in handlers, 1-based in helper). Draw order on the wire is conveyed by pool.players array ordering.
	Seed         int   `json:"seed"`
	// Number is the player's "tag", their assigned competitor number / on-court
	// identifier (zekken-style), optionally prefixed via the competition's
	// numberPrefix (e.g. "A1"). This is distinct from the registration Source field.
	Number string `json:"number,omitempty"` // e.g. "K1", assigned when --number-prefix is set
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
