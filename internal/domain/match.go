package domain

// Match represents a match between two players
type Match struct {
	ID     string
	SideA  *Player
	SideB  *Player
	Winner *Player
}

// Side identifies which side of a match (Shiro = white, Aka = red).
//
// FR-032, data-model §2.
type Side string

const (
	SideShiro Side = "shiro"
	SideAka   Side = "aka"
)

// MatchResult captures the outcome metadata of a match.
//
// FR-030, FR-032, data-model §2.
type MatchResult struct {
	Decision       Decision       `json:"decision,omitempty" yaml:"decision,omitempty"`
	DecisionBy     Side           `json:"decisionBy,omitempty" yaml:"decisionBy,omitempty"`
	DecisionReason string         `json:"decisionReason,omitempty" yaml:"decisionReason,omitempty"`
	Encho          *EnchoMetadata `json:"encho,omitempty" yaml:"encho,omitempty"`
}

// EnchoMetadata records overtime (encho) periods played to resolve a tied match.
type EnchoMetadata struct {
	PeriodCount int `json:"periodCount" yaml:"periodCount"`
	// Periods field deferred to Slice 3 if needed
}
