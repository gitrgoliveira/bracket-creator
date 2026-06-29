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

// PathGraphRounds generates a two-round schedule for the n-1 adjacent matches
// of a path graph: (0,1), (1,2), …, (n-2, n-1).
//
// Round 0 contains even-indexed edges: (0,1),(2,3),…
// Round 1 contains odd-indexed edges:  (1,2),(3,4),… (omitted if empty).
//
// Returns nil for n < 2.
func PathGraphRounds(n int) [][]IntPair {
	if n < 2 {
		return nil
	}

	var round0, round1 []IntPair
	for i := 0; i < n-1; i++ {
		pair := IntPair{A: i, B: i + 1}
		if i%2 == 0 {
			round0 = append(round0, pair)
		} else {
			round1 = append(round1, pair)
		}
	}

	rounds := [][]IntPair{round0}
	if len(round1) > 0 {
		rounds = append(rounds, round1)
	}
	return rounds
}
