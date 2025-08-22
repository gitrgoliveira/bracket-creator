package domain

// Match represents a match between two players
type Match struct {
	ID     string
	SideA  *Player
	SideB  *Player
	Winner *Player
}
