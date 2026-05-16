// Package pool will hold pool-generation algorithms extracted from internal/helper.
// Stub created to unblock Slice 1 partial round-robin work (NFR-001, US11).
package pool

// Player is the minimal pool-scoped player identity. The wider Player
// type with name/dojo/grade lives in internal/helper for Excel-rendering
// reasons; this struct deliberately stays small so pool generation can
// be tested in isolation (NFR-001, NFR-007 incremental adoption).
type Player struct {
	ID string
}

// Match is a single head-to-head fixture between two players inside a
// pool. SideA/SideB point into the Player slice passed to the generator
// so callers can correlate match results back to the original entries.
type Match struct {
	SideA *Player
	SideB *Player
}

// GenerateAdjacentPairings produces a partial round-robin: each player
// faces only their immediate neighbour in the input order. For N players
// the result is N-1 matches in the sequence (0,1), (1,2), ..., (N-2,N-1).
//
// Use case (FR-052, R7): "league" / "partial-pool" competitions where a
// full round-robin would be too long; participants get a guaranteed pair
// of bouts (one with the player above, one with the player below) plus
// the two end-players who only get one. The list ordering is therefore
// load-bearing — the caller (Slice 1.D / Slice 6) is responsible for
// sorting by rank/seed before invoking this.
//
// Returns nil when fewer than 2 players are passed (no valid pairing).
func GenerateAdjacentPairings(players []Player) []Match {
	if len(players) < 2 {
		return nil
	}
	matches := make([]Match, 0, len(players)-1)
	for i := 0; i < len(players)-1; i++ {
		matches = append(matches, Match{
			SideA: &players[i],
			SideB: &players[i+1],
		})
	}
	return matches
}
