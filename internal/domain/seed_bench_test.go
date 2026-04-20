package domain

import (
	"fmt"
	"testing"
)

func BenchmarkAssignSeeds(b *testing.B) {
	sizes := []int{10, 100, 1000, 5000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("Size_%d", size), func(b *testing.B) {
			players := make([]Player, size)
			for i := 0; i < size; i++ {
				players[i] = Player{Name: fmt.Sprintf("Player%d", i), Seed: i + 1}
			}

			assignments := make([]SeedAssignment, size/2)
			for i := 0; i < size/2; i++ {
				assignments[i] = SeedAssignment{Name: fmt.Sprintf("Player%d", i), SeedRank: size - i}
			}

			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				// We need to copy players so we don't pollute state across iterations
				b.StopTimer()
				playersCopy := make([]Player, size)
				copy(playersCopy, players)
				b.StartTimer()

				_ = AssignSeeds(playersCopy, assignments)
			}
		})
	}
}
