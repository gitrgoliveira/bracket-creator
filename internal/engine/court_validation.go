package engine

import "fmt"

// SuggestedMaxCourts returns the recommended maximum number of courts
// for a single-pool competition with numPlayers players.
// Formula: floor(numPlayers/2) - 1, minimum 1.
// At this count, every player gets at least one rest slot between fights.
func SuggestedMaxCourts(numPlayers int) int {
	return max(1, numPlayers/2-1)
}

// ValidateCourtCount checks if numCourts is valid for a single-pool
// competition with numPlayers players.
//
// Returns error if numCourts > floor(numPlayers/2): courts would sit
// idle because there are not enough players to fill a round.
//
// The warning case (numCourts == floor(N/2), no rest between fights) is
// handled exclusively by the frontend — see admin_competition.jsx.
func ValidateCourtCount(numPlayers, numCourts int) error {
	hardCap := max(1, numPlayers/2)
	if numCourts > hardCap {
		return fmt.Errorf(
			"too many courts: %d courts for %d players exceeds maximum of %d (floor(N/2)); extra courts would sit idle",
			numCourts, numPlayers, hardCap,
		)
	}
	return nil
}
