package domain

// Tournament represents the complete tournament structure
type Tournament struct {
	Name               string
	Pools              []Pool
	EliminationMatches []Match
}
