package helper

// CreatePartialPoolMatches fills each pool's Matches slice with
// adjacent-neighbour pairings only, for N players it produces N-1
// matches in the sequence (0,1), (1,2), ..., (N-2,N-1). This is the
// "partial round-robin" / league format selected via
// state.Competition.PoolFormat == "partial".
//
// FR-052 / R7: full round-robin grows O(N²) so even modest pools become
// untenably long; partial pools give every player exactly two bouts
// (one with the neighbour above, one with the neighbour below) except
// the two endpoints who get one each.
//
// Pre-condition: pools[i].Players must be sorted in the order the
// pairings should follow (typically seed/rank order, the caller in
// engine/pools.go applies PoolSeeding before passing them in).
func CreatePartialPoolMatches(pools []Pool) {
	for i := range pools {
		p := &pools[i]
		if len(p.Players) < 2 {
			continue
		}

		roundLookup := buildRoundLookup(PathGraphRounds(len(p.Players)))

		for k := 0; k < len(p.Players)-1; k++ {
			round := 0
			if r, ok := roundLookup[IntPair{A: k, B: k + 1}]; ok {
				round = r
			}
			p.Matches = append(p.Matches, Match{
				SideA: &p.Players[k],
				SideB: &p.Players[k+1],
				Round: round,
			})
		}
	}
}
