package domain

// Pool represents a group of players in the tournament
type Pool struct {
	ID      string
	Name    string
	Players []Player
	Matches []Match
}
