package helper

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDojoKey_NormalizesCaseAndDiacritics pins bc-drwx item 3's own
// primitive: dojoKey must treat two spellings of one dojo -- differing only
// in case (the reported repro: "Mumeishi" vs "mumeishi") or in diacritics --
// as the SAME key, while leaving already-consistent spellings (and genuinely
// different dojos) untouched.
func TestDojoKey_NormalizesCaseAndDiacritics(t *testing.T) {
	assert.Equal(t, dojoKey("Mumeishi"), dojoKey("mumeishi"),
		"differing case of the same dojo must normalize to one key")
	assert.Equal(t, dojoKey("Müller Dojo"), dojoKey("muller dojo"),
		"a diacritic-only spelling difference must normalize to one key")
	assert.NotEqual(t, dojoKey("Mumeishi"), dojoKey("OtherDojo"),
		"genuinely different dojos must not collide")
}

// TestCountDojoInPool_NormalizesSpelling is the exact repro from bc-drwx
// item 3: countDojoInPool's own doc comment claims to be "the one place"
// dojo comparison changes, but it used to compare the raw, as-typed string,
// so "Mumeishi" and "mumeishi" were counted as two different dojos and both
// spellings could land in the very same pool that identical spelling would
// have kept apart.
func TestCountDojoInPool_NormalizesSpelling(t *testing.T) {
	pool := Pool{Players: []Player{{Name: "Alice", Dojo: "Mumeishi"}}}
	keys := make(dojoKeyCache)
	assert.Equal(t, 1, countDojoInPool(pool, "mumeishi", keys),
		"a differently-cased spelling of the same dojo must still count")
	assert.Equal(t, 0, countDojoInPool(pool, "SomeOtherDojo", keys),
		"a genuinely different dojo must not count")
}

// TestDojoTreeDescent_NormalizesSpelling pins the SAME repro one level up,
// through the actual dojo-tree descent (recordDojoOccupancy/
// chooseDojoTreePool/pickDojoTreeAwarePool, pool_distribution_tree_aware.go)
// rather than countDojoInPool alone: that descent keeps its own
// dojoNode.dojoCount maps, keyed independently of countDojoInPool, so a fix
// scoped to only ONE of the two raw-comparison sites would still leave this
// path buggy. Two pools already hold one member each of the SAME dojo under
// two different spellings; a THIRD member of that dojo must be routed away
// from both -- exactly as it would if every member spelled the dojo
// identically -- because the tree only sees that as "avoid this dojo" when
// the two spellings normalize to one key.
func TestDojoTreeDescent_NormalizesSpelling(t *testing.T) {
	// 3 pools of 2, all winner-only qualifierSlots (poolWinners=1): pool 0
	// and pool 1 already hold one member each of "Mumeishi"/"mumeishi";
	// pool 2 is dojo-free. A third same-dojo member must land in pool 2,
	// the only one not already holding the dojo.
	pools := []Pool{
		{PoolName: "Pool A", Players: []Player{{Name: "M1", Dojo: "Mumeishi"}}},
		{PoolName: "Pool B", Players: []Player{{Name: "M2", Dojo: "mumeishi"}}},
		{PoolName: "Pool C", Players: []Player{{Name: "Other", Dojo: "OtherDojo"}}},
	}
	targetSizes := []int{2, 2, 2}
	qualifierSlots := [][]int{{0}, {1}, {2}}

	placed := []int{1, 1, 1}
	// roster flattens pools' players plus a synthetic "MUMEISHI"-dojo entry
	// (a third spelling of the same dojo, never actually seated anywhere)
	// so newDojoIDCacheFor interns every spelling this test resolves below
	// up front, same as every other caller of that helper.
	var roster []Player
	for i := range pools {
		roster = append(roster, pools[i].Players...)
	}
	roster = append(roster, Player{Dojo: "MUMEISHI"})
	ids, _ := newDojoIDCacheFor(roster)
	root, totalBits := buildDojoTree(qualifierSlots, targetSizes, placed, ids.numDojos())
	require.NotNil(t, root)
	for i := range pools {
		for _, pl := range pools[i].Players {
			recordDojoOccupancy(root, ids.of(pl.Dojo), qualifierSlots[i][0], totalBits, 0)
		}
	}

	// counts mirrors assignUnseededByDojoTree's own dense per-pool dojo
	// tally (bc-pnum review(d)): pickDojoTreeAwarePool now reads pool
	// membership from this rather than rescanning pools[i].Players itself.
	counts := make([][]int, len(pools))
	for i := range pools {
		counts[i] = make([]int, ids.numDojos())
		for _, pl := range pools[i].Players {
			counts[i][ids.of(pl.Dojo)]++
		}
	}

	var dojoPoolIndicesBuf []int
	best := pickDojoTreeAwarePool(pools, targetSizes, root, ids.of("MUMEISHI"), qualifierSlots, counts, &dojoPoolIndicesBuf)
	assert.Equal(t, 2, best,
		"a third, differently-cased member of the same dojo must be routed to the only dojo-free pool")
}

// TestDojoIDCache_Contract pins dojoIDCache's four load-bearing properties
// (bc-pnum review): every hot loop in pool_distribution_tree_aware.go and
// seed.go indexes a []int/[][]int by the ids this type mints, so a
// regression in any one of these four would eventually surface as an
// out-of-range index or a silently-wrong dojo-spread count somewhere
// downstream, never as an obvious failure at the interning site itself.
func TestDojoIDCache_Contract(t *testing.T) {
	t.Run("dense 0..n-1 in first-seen order", func(t *testing.T) {
		ids, _ := newDojoIDCacheFor([]Player{
			{Dojo: "Charlie"}, {Dojo: "Alpha"}, {Dojo: "Bravo"},
		})
		assert.Equal(t, 0, ids.of("Charlie"))
		assert.Equal(t, 1, ids.of("Alpha"))
		assert.Equal(t, 2, ids.of("Bravo"))
		assert.Equal(t, 3, ids.numDojos())
	})

	t.Run("stable across re-resolution", func(t *testing.T) {
		ids, _ := newDojoIDCacheFor([]Player{{Dojo: "Alpha"}, {Dojo: "Bravo"}})
		first := ids.of("Alpha")
		for i := 0; i < 5; i++ {
			assert.Equal(t, first, ids.of("Alpha"),
				"re-resolving the same dojo must return the same id every time")
		}
	})

	t.Run("spelling collapse", func(t *testing.T) {
		ids, _ := newDojoIDCacheFor([]Player{{Dojo: "Mumeishi"}})
		want := ids.of("Mumeishi")
		assert.Equal(t, want, ids.of("MUMEISHI"),
			"an all-caps spelling must collapse to the same id")
		assert.Equal(t, want, ids.of("  mumeishi  "),
			"a lowercase, whitespace-padded spelling must collapse to the same id")
	})

	t.Run("blank-safe", func(t *testing.T) {
		require.NotPanics(t, func() {
			ids, _ := newDojoIDCacheFor([]Player{{Dojo: ""}, {Dojo: "   "}})
			assert.Equal(t, ids.of(""), ids.of("   "),
				"an empty dojo and a whitespace-only one must collapse to the same id")
			assert.Equal(t, 1, ids.numDojos(),
				"both blank spellings must intern to ONE id, not two")
		})
	})

	t.Run("value copy shares the interning table", func(t *testing.T) {
		// dojoIDCache is a struct of two maps (reference types), passed by
		// value everywhere in this package (matching dojoKeyCache's own
		// value-type convention) -- a plain Go assignment must still share
		// the SAME underlying interning table, not fork it, or a caller
		// that copies the value (e.g. by passing it into a function that
		// takes it by value) would silently stop seeing ids minted through
		// the other copy.
		original, _ := newDojoIDCacheFor([]Player{{Dojo: "Alpha"}})
		dup := original
		mintedViaDup := dup.of("Bravo")
		assert.Equal(t, mintedViaDup, original.of("Bravo"),
			"an id minted through a copy must be visible through the original")
		assert.Equal(t, 2, original.numDojos())
		assert.Equal(t, 2, dup.numDojos())
	})
}
