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
	keys := make(dojoKeyCache)
	ids := newDojoIDCache(keys, 0)
	for i := range pools {
		for _, pl := range pools[i].Players {
			ids.of(pl.Dojo)
		}
	}
	ids.of("MUMEISHI")
	root, totalBits := buildDojoTree(qualifierSlots, targetSizes, placed, ids.numDojos())
	require.NotNil(t, root)
	for i := range pools {
		for _, pl := range pools[i].Players {
			recordDojoOccupancy(root, ids.of(pl.Dojo), qualifierSlots[i][0], totalBits, 0)
		}
	}

	best := pickDojoTreeAwarePool(pools, targetSizes, root, "MUMEISHI", ids.of("MUMEISHI"), qualifierSlots, keys)
	assert.Equal(t, 2, best,
		"a third, differently-cased member of the same dojo must be routed to the only dojo-free pool")
}
