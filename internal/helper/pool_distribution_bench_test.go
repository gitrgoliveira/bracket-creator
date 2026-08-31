package helper

import (
	"fmt"
	"testing"
)

// benchClusteredDojoRoster builds nDojos*groupSize players, pasted DOJO BY
// DOJO (an operator's most common paste order), which is the shape that
// makes delayDojoMeetings' hill climb and improveDojoMeetings' repair loop
// do real work: every dojo's members start out adjacent.
func benchClusteredDojoRoster(nDojos, groupSize int) []Player {
	out := make([]Player, 0, nDojos*groupSize)
	for c := 0; c < nDojos; c++ {
		for i := 0; i < groupSize; i++ {
			out = append(out, Player{Name: fmt.Sprintf("C%02d_%03d", c, i), Dojo: fmt.Sprintf("Dojo%02d", c)})
		}
	}
	return out
}

// benchInterleavedDojoRoster builds the same population as
// benchClusteredDojoRoster (same nDojos, same groupSize) but round-robins
// dojo membership across roster order instead of clustering it, which
// spreads same-dojo pairs across the whole index range up front rather than
// starting them adjacent.
func benchInterleavedDojoRoster(nDojos, groupSize int) []Player {
	out := make([]Player, 0, nDojos*groupSize)
	for i := 0; i < groupSize; i++ {
		for c := 0; c < nDojos; c++ {
			out = append(out, Player{Name: fmt.Sprintf("I%02d_%03d", c, i), Dojo: fmt.Sprintf("Dojo%02d", c)})
		}
	}
	return out
}

// The StandardSeeding benchmarks below measure delayDojoMeetings' hill
// climb (P1: dojoSumMeetRounds/dojoSwapGain) at the sizes and dojo shapes
// bc-dojo-least-conflicted-pool's wave-2 measurement was asked to
// re-verify: 256 entrants at 16 dojos of 16 and 32 dojos of 8, plus the
// 64- and 128-entrant equivalents (same two dojo-count shapes, scaled) used
// for calibration. All are clustered (dojo-by-dojo) rosters, since that is
// the shape that gives the climb real work to do.

func BenchmarkStandardSeeding_64_16x4(b *testing.B) {
	roster := benchClusteredDojoRoster(16, 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

func BenchmarkStandardSeeding_64_32x2(b *testing.B) {
	roster := benchClusteredDojoRoster(32, 2)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

func BenchmarkStandardSeeding_128_16x8(b *testing.B) {
	roster := benchClusteredDojoRoster(16, 8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

func BenchmarkStandardSeeding_128_32x4(b *testing.B) {
	roster := benchClusteredDojoRoster(32, 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

func BenchmarkStandardSeeding_256_16x16(b *testing.B) {
	roster := benchClusteredDojoRoster(16, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

func BenchmarkStandardSeeding_256_32x8(b *testing.B) {
	roster := benchClusteredDojoRoster(32, 8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

// The BuildPoolPhaseTreeAware benchmarks below measure improveDojoMeetings
// (P2 caching) and earliestDojoMeeting (P3 occupied-pool collection) at
// poolSize=4 -- 256 players/64 pools is the wave-2 measurement's own
// target, with 64/16 and 128/32 players/pools as the calibration points.
// Both clustered and interleaved roster orders are measured: the repair
// loop's workload depends on how many round-1/near-round-1 collisions the
// dojo-tree descent leaves behind, which differs between the two orders.

func BenchmarkBuildPoolPhaseTreeAware_64_16x4_Clustered(b *testing.B) {
	roster := benchClusteredDojoRoster(16, 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := BuildPoolPhaseTreeAware(roster, 4, false, 4, 2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildPoolPhaseTreeAware_64_16x4_Interleaved(b *testing.B) {
	roster := benchInterleavedDojoRoster(16, 4)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := BuildPoolPhaseTreeAware(roster, 4, false, 4, 2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildPoolPhaseTreeAware_128_16x8_Clustered(b *testing.B) {
	roster := benchClusteredDojoRoster(16, 8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := BuildPoolPhaseTreeAware(roster, 4, false, 4, 2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildPoolPhaseTreeAware_128_16x8_Interleaved(b *testing.B) {
	roster := benchInterleavedDojoRoster(16, 8)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := BuildPoolPhaseTreeAware(roster, 4, false, 4, 2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildPoolPhaseTreeAware_256_16x16_Clustered(b *testing.B) {
	roster := benchClusteredDojoRoster(16, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := BuildPoolPhaseTreeAware(roster, 4, false, 4, 2); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkBuildPoolPhaseTreeAware_256_16x16_Interleaved(b *testing.B) {
	roster := benchInterleavedDojoRoster(16, 16)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, _, err := BuildPoolPhaseTreeAware(roster, 4, false, 4, 2); err != nil {
			b.Fatal(err)
		}
	}
}
