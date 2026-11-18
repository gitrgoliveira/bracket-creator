// Package racesample is the one owner of how many of a permutation gate's
// orderings run under the race detector. It holds no test code, so the gates in
// internal/mobileapp and internal/mobileapp/kachinukigate share it.
package racesample

import (
	"math/rand/v2"
	"slices"
)

// Sample is the set of orderings a permutation gate drives. Without the race
// detector (make go/test) it is every ordering. Under it (CI's coverage run,
// -race) the gates would run past the package timeout, and the detector looks
// for data races, which any ordering exercises as well as all of them, so it is
// at most keep orderings: a fixed-seed random choice, the same on every run,
// kept in the order given. A fixed stride is not used because the gates pick
// their families by the ordering's index modulo small radices, and a stride
// would always land on the same residues and skip whole families.
func Sample[T any](all []T, keep int) []T {
	if !Enabled || len(all) <= keep {
		return all
	}
	return SampleOf(all, keep)
}

// SampleOf is keep of all, chosen by a fixed seed, in all's order.
func SampleOf[T any](all []T, keep int) []T {
	idx := rand.New(rand.NewPCG(0x5eed, uint64(len(all)))).Perm(len(all))[:keep]
	slices.Sort(idx)
	out := make([]T, 0, keep)
	for _, i := range idx {
		out = append(out, all[i])
	}
	return out
}
