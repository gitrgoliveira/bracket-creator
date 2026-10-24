package mobileapp

import (
	"math/rand/v2"
	"slices"
	"testing"
)

// raceSample is the set of orderings a permutation gate drives. Without the
// race detector (make go/test) it is every ordering. Under it (CI's coverage
// run, -race) the gates would run past the package timeout, and the detector
// looks for data races, which any ordering exercises as well as all of them,
// so it is at most keep orderings: a fixed-seed random choice, the same on
// every run, kept in the order given. A fixed stride is not used because the
// gates pick their families by the ordering's index modulo small radices, and
// a stride would always land on the same residues and skip whole families.
func raceSample[T any](all []T, keep int) []T {
	if !raceEnabled || len(all) <= keep {
		return all
	}
	return sampleOf(all, keep)
}

// sampleOf is keep of all, chosen by a fixed seed, in all's order.
func sampleOf[T any](all []T, keep int) []T {
	idx := rand.New(rand.NewPCG(0x5eed, uint64(len(all)))).Perm(len(all))[:keep]
	slices.Sort(idx)
	out := make([]T, 0, keep)
	for _, i := range idx {
		out = append(out, all[i])
	}
	return out
}

func TestSampleOf_KeepsADeterministicSubsetInOrder(t *testing.T) {
	all := make([]int, 100)
	for i := range all {
		all[i] = i
	}
	got := sampleOf(all, 10)
	if len(got) != 10 || !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != 10 {
		t.Fatalf("sampleOf(100, 10) = %v, want 10 distinct indices in order", got)
	}
	if again := sampleOf(all, 10); !slices.Equal(got, again) {
		t.Fatalf("sampleOf is not deterministic: %v then %v", got, again)
	}
	if short := raceSample(all[:5], 10); !slices.Equal(short, all[:5]) {
		t.Fatalf("raceSample of fewer than keep = %v, want all of them", short)
	}
	if n := len(raceSample(all, 10)); (raceEnabled && n != 10) || (!raceEnabled && n != 100) {
		t.Fatalf("raceSample(100, 10) kept %d (race %v)", n, raceEnabled)
	}
}
