// Package domain defines the core domain models for the bracket creator
package domain

// Player represents a tournament participant
type Player struct {
	ID           string
	Name         string
	DisplayName  string
	Dojo         string
	PoolPosition int64
	Seed         int
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
