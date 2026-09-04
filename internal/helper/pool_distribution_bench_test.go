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

// benchLopsidedDojoRoster builds bigDojoCount dojos of bigGroupSize members
// each (pasted clustered), followed by `singletons` players each from their
// own unique dojo. This is the shape that made the pre-memo climb's
// worst-pair rescan/candidate-confirmation tail the WORST at a given N: a
// small number of oversized dojos means the SAME one or two slots recur as
// worstA/worstB across many stuck-and-excluded iterations of one
// generation (every other same-dojo pair in that generation belongs to one
// of the few big dojos too), which is exactly the repeat pattern the
// slotBest generation memo (delayDojoMeetings, seed.go) targets.
func benchLopsidedDojoRoster(bigDojoCount, bigGroupSize, singletons int) []Player {
	out := make([]Player, 0, bigDojoCount*bigGroupSize+singletons)
	for c := 0; c < bigDojoCount; c++ {
		for i := 0; i < bigGroupSize; i++ {
			out = append(out, Player{Name: fmt.Sprintf("Big%d_%03d", c, i), Dojo: fmt.Sprintf("BigDojo%d", c)})
		}
	}
	for i := 0; i < singletons; i++ {
		out = append(out, Player{Name: fmt.Sprintf("Solo%03d", i), Dojo: fmt.Sprintf("SoloDojo%03d", i)})
	}
	return out
}

// BenchmarkStandardSeeding_256_2x96Plus64Singletons and
// BenchmarkStandardSeeding_256_2x128 mirror the two lopsided shapes the
// wave-2 review measured the slotBest memo against (2 large dojos + many
// singletons; 2 large dojos with no singletons at all), re-verified
// independently here rather than trusted from the review alone.

func BenchmarkStandardSeeding_256_2x96Plus64Singletons(b *testing.B) {
	roster := benchLopsidedDojoRoster(2, 96, 64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

func BenchmarkStandardSeeding_256_2x128(b *testing.B) {
	roster := benchLopsidedDojoRoster(2, 128, 0)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

// benchSingleDojoRoster builds a roster where EVERY entrant shares one dojo
// (or the roster the CLI's legacy no-dojo-column parser used to default
// every blank dojo to "NA" -- the same shape from delayDojoMeetings' own
// point of view, since it only ever sees the string).
func benchSingleDojoRoster(n int) []Player {
	out := make([]Player, n)
	for i := range out {
		out[i] = Player{Name: fmt.Sprintf("P%04d", i), Dojo: "OneDojo"}
	}
	return out
}

// BenchmarkStandardSeeding_SingleDojo_64/128/256 are bc-drwx item 2's own
// worst case: with only ONE dojo in the whole roster, no cross-dojo swap
// partner can EVER exist (dojoSwapGain's candidate filter rejects same-dojo
// targets unconditionally), so the pre-fix delayDojoMeetings paid O(N^4) --
// C(N,2) same-dojo pairs, excluded one at a time, each exclusion re-paying a
// fresh O(N^2) worst-pair rescan -- to discover, the slow way, that the
// whole climb was a no-op from the start. See delayDojoMeetings' own doc
// comment for the early-out this measures and the before/after numbers.
func BenchmarkStandardSeeding_SingleDojo_64(b *testing.B) {
	roster := benchSingleDojoRoster(64)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

func BenchmarkStandardSeeding_SingleDojo_128(b *testing.B) {
	roster := benchSingleDojoRoster(128)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		StandardSeeding(roster)
	}
}

func BenchmarkStandardSeeding_SingleDojo_256(b *testing.B) {
	roster := benchSingleDojoRoster(256)
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
