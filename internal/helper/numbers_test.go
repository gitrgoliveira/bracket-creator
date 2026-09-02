package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAssignPlayerNumbers(t *testing.T) {
	t.Run("basic numbering with prefix", func(t *testing.T) {
		players := makePlayers(3)

		next := AssignPlayerNumbers(players, "A", 1)

		require.Equal(t, 4, next)
		assert.Equal(t, "A1", players[0].Number)
		assert.Equal(t, "A2", players[1].Number)
		assert.Equal(t, "A3", players[2].Number)
	})

	t.Run("empty prefix produces bare numbers", func(t *testing.T) {
		players := makePlayers(2)

		next := AssignPlayerNumbers(players, "", 1)

		require.Equal(t, 3, next)
		assert.Equal(t, "1", players[0].Number)
		assert.Equal(t, "2", players[1].Number)
	})

	t.Run("empty slice returns start unchanged and mutates nothing", func(t *testing.T) {
		var players []Player

		next := AssignPlayerNumbers(players, "A", 5)

		assert.Equal(t, 5, next)
		assert.Empty(t, players)
	})

	t.Run("chaining continues sequence across slices without gaps or duplicates", func(t *testing.T) {
		// pool1's own numbering (A1..A3) is pinned by the first subtest;
		// here only the continuation matters: the returned counter feeds the
		// next slice with no gap or duplicate.
		pool1 := makePlayers(3)
		pool2 := makePlayers(2)

		next := AssignPlayerNumbers(pool1, "A", 1)
		require.Equal(t, 4, next)

		next = AssignPlayerNumbers(pool2, "A", next)
		require.Equal(t, 6, next)

		assert.Equal(t, "A4", pool2[0].Number)
		assert.Equal(t, "A5", pool2[1].Number)
	})

	t.Run("non-1 start value", func(t *testing.T) {
		players := makePlayers(2)

		next := AssignPlayerNumbers(players, "K", 10)

		require.Equal(t, 12, next)
		assert.Equal(t, "K10", players[0].Number)
		assert.Equal(t, "K11", players[1].Number)
	})
}

// TestNumberPools pins the shape G1 keeps unchanged: ONE counter running
// straight through the pools in the order they're given (their published
// court order at every real call site), restarting nowhere.
func TestNumberPools(t *testing.T) {
	t.Run("counter runs through pools with no restart", func(t *testing.T) {
		pools := []Pool{
			{PoolName: "Pool A", Players: makePlayers(4)},
			{PoolName: "Pool B", Players: makePlayers(3)},
		}

		NumberPools(pools, "K")

		assert.Equal(t, "K1", pools[0].Players[0].Number)
		assert.Equal(t, "K4", pools[0].Players[3].Number)
		assert.Equal(t, "K5", pools[1].Players[0].Number, "second pool must continue the counter, not restart at 1")
		assert.Equal(t, "K7", pools[1].Players[2].Number)
	})

	t.Run("no pools is a no-op", func(t *testing.T) {
		var pools []Pool
		assert.NotPanics(t, func() { NumberPools(pools, "K") })
	})
}

// TestDefaultNumberPrefix covers the derivation itself: initials, escalating
// disambiguation against the taken set (bare initial, then progressively more
// of them, then a numeric suffix), the length cap, the no-ASCII-letters
// fallback, and case-insensitive comparison against taken.
func TestDefaultNumberPrefix(t *testing.T) {
	tests := []struct {
		name     string
		compName string
		taken    []string
		want     string
	}{
		{
			name:     "bare initial when nothing taken",
			compName: "Kendo Open",
			taken:    nil,
			want:     "K",
		},
		{
			name:     "escalates to two initials when the bare one collides",
			compName: "Kendo Open",
			taken:    []string{"K"},
			want:     "KO",
		},
		{
			name:     "escalates to a numeric suffix once initials are exhausted",
			compName: "Kendo Open",
			taken:    []string{"K", "KO"},
			want:     "KO2",
		},
		{
			name:     "numeric suffix climbs past a taken KO2",
			compName: "Kendo Open",
			taken:    []string{"K", "KO", "KO2"},
			want:     "KO3",
		},
		{
			name:     "initials cap at MaxNumberPrefixLen (3)",
			compName: "Kendo Open Regional",
			taken:    nil,
			want:     "K",
		},
		{
			name:     "a 3-letter initials stem trims to fit a numeric suffix under the cap",
			compName: "Kendo Open Regional",
			taken:    []string{"K", "KO", "KOR"},
			want:     "KO2",
		},
		{
			name:     "taken comparison is case-insensitive",
			compName: "Kendo Open",
			taken:    []string{"k"},
			want:     "KO",
		},
		{
			name:     "empty name falls back to the kendo default",
			compName: "",
			taken:    nil,
			want:     DefaultNumberPrefixFallback,
		},
		{
			name:     "a name with no ASCII letters falls back to the kendo default",
			compName: "東京 2026",
			taken:    nil,
			want:     DefaultNumberPrefixFallback,
		},
		{
			name:     "punctuation and digits are word separators, not letters",
			compName: "Kendo - Open (Senior)",
			taken:    []string{"K"},
			want:     "KO",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DefaultNumberPrefix(tc.compName, tc.taken)
			assert.Equal(t, tc.want, got)
			assert.LessOrEqualf(t, len(got), MaxNumberPrefixLen, "derived prefix %q must never exceed the length cap", got)
		})
	}
}
