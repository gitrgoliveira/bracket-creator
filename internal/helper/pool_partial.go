package helper

import (
	"github.com/gitrgoliveira/bracket-creator/internal/helper/pool"
)

// CreatePartialPoolMatches fills each pool's Matches slice with
// adjacent-neighbour pairings only — for N players it produces N-1
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
// pairings should follow (typically seed/rank order — the caller in
// engine/pools.go applies PoolSeeding before passing them in).
func CreatePartialPoolMatches(pools []Pool) {
	for i := range pools {
		p := &pools[i]
		// Convert helper.Player → pool.Player (only ID needed for pairing)
		ids := make([]pool.Player, len(p.Players))
		for j, pl := range p.Players {
			ids[j] = pool.Player{ID: pl.Name}
		}
		sub := pool.GenerateAdjacentPairings(ids)
		// Re-map back to helper.Match pointing at the original p.Players
		// entries — preserve the address invariant the rest of the
		// helper package (Excel rendering, etc.) relies on.
		for k := range sub {
			p.Matches = append(p.Matches, Match{
				SideA: &p.Players[k],
				SideB: &p.Players[k+1],
			})
		}
	}
}
