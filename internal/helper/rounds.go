package helper

// IntPair represents a match pairing by player index (A < B always).
type IntPair struct {
	A, B int
}

// CircleMethodRounds generates a full round-robin schedule for n players using
// the circle (polygon) method. Player 0 is fixed; players 1..N-1 rotate.
//
// For even n:   n-1 rounds, n/2 matches per round, n*(n-1)/2 total.
// For odd n:    n rounds, (n-1)/2 matches per round (one bye per round),
//
//	n*(n-1)/2 total.
//
// Returns nil for n < 2.
func CircleMethodRounds(n int) [][]IntPair {
	if n < 2 {
		return nil
	}

	// If n is odd, introduce a ghost player so we have an even table size.
	ghost := -1
	N := n
	if n%2 == 1 {
		ghost = n // ghost index is n (one past the real players)
		N = n + 1
	}

	numRounds := N - 1
	matchesPerRound := N / 2
	rounds := make([][]IntPair, 0, numRounds)

	// Standard circle method:
	//   Position 0 is fixed (player 0).
	//   Positions 1..N-2 rotate clockwise each round.
	//   In round r the player at rotation position k is: 1 + (r+k) % (N-1).
	//   Pairing: position p is matched against position (N-1-p).
	//   p=0 (fixed) is matched against position N-1.
	for r := range numRounds {
		matches := make([]IntPair, 0, matchesPerRound)

		for p := range matchesPerRound {
			var a, b int
			if p == 0 {
				// Fixed player 0 faces the player at the last rotation slot.
				a = 0
				b = 1 + (r+N-2)%(N-1)
			} else {
				// Mirror positions across the circle.
				a = 1 + (r+p-1)%(N-1)
				b = 1 + (r+N-2-p)%(N-1)
			}

			// Skip any pair that involves the ghost player.
			if a == ghost || b == ghost {
				continue
			}

			// Normalise so A < B.
			if a > b {
				a, b = b, a
			}
			matches = append(matches, IntPair{A: a, B: b})
		}

		rounds = append(rounds, matches)
	}

	return rounds
}
