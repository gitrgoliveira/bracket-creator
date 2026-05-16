package domain

// Match represents a match between two players
type Match struct {
	ID     string
	SideA  *Player
	SideB  *Player
	Winner *Player
}

// MatchResult captures the outcome metadata of a match.
//
// Slice-1 minimal — Slice-3 will add Decision/DecisionBy/DecisionReason/etc. per T076.
type MatchResult struct {
	Encho *EnchoMetadata `json:"encho,omitempty" yaml:"encho,omitempty"`
}

// EnchoMetadata records overtime (encho) periods played to resolve a tied match.
type EnchoMetadata struct {
	PeriodCount int `json:"periodCount" yaml:"periodCount"`
	// Periods field deferred to Slice 3 if needed
}
