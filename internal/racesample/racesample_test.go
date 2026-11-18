package racesample

import (
	"slices"
	"testing"
)

func TestSampleOf_KeepsADeterministicSubsetInOrder(t *testing.T) {
	all := make([]int, 100)
	for i := range all {
		all[i] = i
	}
	got := SampleOf(all, 10)
	if len(got) != 10 || !slices.IsSorted(got) || len(slices.Compact(slices.Clone(got))) != 10 {
		t.Fatalf("SampleOf(100, 10) = %v, want 10 distinct indices in order", got)
	}
	if again := SampleOf(all, 10); !slices.Equal(got, again) {
		t.Fatalf("SampleOf is not deterministic: %v then %v", got, again)
	}
	if short := Sample(all[:5], 10); !slices.Equal(short, all[:5]) {
		t.Fatalf("Sample of fewer than keep = %v, want all of them", short)
	}
	if n := len(Sample(all, 10)); (Enabled && n != 10) || (!Enabled && n != 100) {
		t.Fatalf("Sample(100, 10) kept %d (race %v)", n, Enabled)
	}
}
