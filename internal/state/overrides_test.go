package state

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverrides(t *testing.T) {
	dir, err := os.MkdirTemp("", "overrides-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "comp-overrides"

	// The competition has to exist: saving overrides no longer creates the
	// competition directory, because doing so let a save that landed after
	// DeleteCompetition resurrect it as a config-less orphan. See
	// TestSaveOverridesDoesNotResurrectDeletedCompetition.
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Comp Overrides"}))

	// 1. Load empty overrides (doesn't exist)
	overrides, err := store.LoadOverrides(compID)
	require.NoError(t, err)
	require.NotNil(t, overrides)
	assert.Empty(t, overrides.PoolRanks)
	assert.Empty(t, overrides.Winners)

	// 2. Save rank override, keyed by participant id (bc-pnum: playerID is
	// the only identity SaveRankOverridesChanged accepts); the key is
	// helper.CompetitorKey("alice-id", "", ""), i.e. "id:alice-id" -- see
	// Overrides.PoolRanks' doc comment. Same-name-different-dojo identity is
	// verified in engine's TestCalculatePoolStandings_Override_SameNameDifferentDojo;
	// this test is plumbing only.
	aliceKey := helper.CompetitorKey("alice-id", "", "")
	err = store.SaveRankOverride(compID, "Pool A", "alice-id", 1)
	require.NoError(t, err)

	// 3. Load overrides after save
	overrides, err = store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Equal(t, 1, overrides.PoolRanks["Pool A"][aliceKey])

	// 4. Save winner override
	err = store.SaveWinnerOverride(compID, "Match-1", "Bob")
	require.NoError(t, err)

	// 5. Load overrides after save
	overrides, err = store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", overrides.Winners["Match-1"])
	assert.Equal(t, 1, overrides.PoolRanks["Pool A"][aliceKey])

	// 6. Reset overrides
	err = store.ResetOverrides(compID)
	require.NoError(t, err)

	// 7. Load overrides after reset
	overrides, err = store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Empty(t, overrides.PoolRanks)
	assert.Empty(t, overrides.Winners)
}

// RestoreRankOverrides is the undo of a refused override group: it puts back
// the value each competitor held before the change, or removes the key when
// there was none, in one write.
func TestRestoreRankOverrides(t *testing.T) {
	dir, err := os.MkdirTemp("", "overrides-restore-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "comp-restore"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Comp Restore"}))
	key := helper.CompetitorKey("alice-id", "", "")
	bobKey := helper.CompetitorKey("bob-id", "", "")
	ranks := func() map[string]int {
		o, lerr := store.LoadOverrides(compID)
		require.NoError(t, lerr)
		return o.PoolRanks["Pool A"]
	}

	require.NoError(t, store.SaveRankOverride(compID, "Pool A", "alice-id", 2))
	require.NoError(t, store.RestoreRankOverrides(compID, "Pool A", map[string]PriorRank{"alice-id": {}}))
	assert.NotContains(t, ranks(), key, "no prior override: the key is removed")

	require.NoError(t, store.SaveRankOverride(compID, "Pool A", "alice-id", 3))
	require.NoError(t, store.RestoreRankOverrides(compID, "Pool B", map[string]PriorRank{"alice-id": {Rank: 1, Present: true}}))
	o, err := store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Equal(t, 1, o.PoolRanks["Pool B"][key], "a prior override is put back, creating its pool's map")
	assert.Equal(t, 3, ranks()[key], "another pool's override is untouched")

	// A group: each member goes back to what IT held, present or not.
	_, err = store.SaveRankOverridesChanged(compID, "Pool A", map[string]int{"alice-id": 1, "bob-id": 2})
	require.NoError(t, err)
	require.NoError(t, store.RestoreRankOverrides(compID, "Pool A", map[string]PriorRank{
		"alice-id": {Rank: 3, Present: true},
		"bob-id":   {},
	}))
	assert.Equal(t, map[string]int{key: 3}, ranks(), "alice back to 3, bob's new key removed")
	assert.NotContains(t, ranks(), bobKey)
}

// SaveRankOverridesChanged writes a whole group in one write and reports a
// change only when some rank in it actually moved.
func TestSaveRankOverridesChanged_Group(t *testing.T) {
	dir, err := os.MkdirTemp("", "overrides-group-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)
	store, err := NewStore(dir)
	require.NoError(t, err)
	compID := "comp-group"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "Comp Group"}))
	ranks := func() map[string]int {
		o, lerr := store.LoadOverrides(compID)
		require.NoError(t, lerr)
		return o.PoolRanks["Pool A"]
	}

	changed, err := store.SaveRankOverridesChanged(compID, "Pool A", map[string]int{"a-id": 1, "b-id": 2, "c-id": 3})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, map[string]int{"id:a-id": 1, "id:b-id": 2, "id:c-id": 3}, ranks())

	changed, err = store.SaveRankOverridesChanged(compID, "Pool A", map[string]int{"a-id": 1, "b-id": 2, "c-id": 3})
	require.NoError(t, err)
	assert.False(t, changed, "the same order again changes nothing")

	changed, err = store.SaveRankOverridesChanged(compID, "Pool A", map[string]int{"a-id": 3, "b-id": 2, "c-id": 1})
	require.NoError(t, err)
	assert.True(t, changed)
	assert.Equal(t, map[string]int{"id:a-id": 3, "id:b-id": 2, "id:c-id": 1}, ranks())
}

func TestSaveOverrides_InvalidDir(t *testing.T) {
	// Try to save to a directory that cannot be created
	dir, err := os.MkdirTemp("", "overrides-fail-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	// Create a file where the specific competition directory should be,
	// forcing MkdirAll to fail.
	compID := "some-comp"
	err = os.WriteFile(filepath.Join(dir, "competitions", compID), []byte("file"), 0600)
	require.NoError(t, err)

	o := &Overrides{
		PoolRanks: make(map[string]map[string]int),
		Winners:   make(map[string]string),
	}

	err = store.SaveOverrides(compID, o)
	assert.Error(t, err)
}

func TestLoadOverrides_InvalidJSON(t *testing.T) {
	dir, err := os.MkdirTemp("", "overrides-fail-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "invalid-json-comp"
	compDir := filepath.Join(dir, "competitions", compID)
	err = os.MkdirAll(compDir, 0700)
	require.NoError(t, err)

	path := filepath.Join(compDir, "overrides.json")
	err = os.WriteFile(path, []byte("{invalid json"), 0600)
	require.NoError(t, err)

	_, err = store.LoadOverrides(compID)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrCorruptOverrides), "a JSON parse failure must be wrapped in ErrCorruptOverrides so callers can recognise and repair it")

	// bc-pnum FIX 1: the same error must ALSO satisfy AsCorruptFile, located,
	// so every reader that degrades on an operator-repairable file (e.g. the
	// public viewer detail endpoint) recognises it too. Before the fix,
	// ErrCorruptOverrides was a plain errors.New sentinel wrapped with %w, not
	// a *CorruptFileError, so AsCorruptFile could never match it.
	cf, ok := AsCorruptFile(err)
	require.True(t, ok, "a corrupt overrides.json must be a located CorruptFileError, not just a bare sentinel")
	assert.Equal(t, "overrides.json", cf.File)
	assert.NotZero(t, cf.Line, "a JSON syntax error must resolve to a line an operator can open")
}

// TestResetOverridesForce_RepairsCorruptFile is PR #416 finding 10: every
// OTHER override writer (SaveRankOverridesChanged, SaveWinnerOverride,
// ResetOverridesChanged) goes through modifyOverridesChanged, which LOADS
// the file first, so none of them can repair a corrupt overrides.json --
// including "reset", which one might expect to be the escape hatch.
// ResetOverridesForce is the one write that does not parse first.
func TestResetOverridesForce_RepairsCorruptFile(t *testing.T) {
	dir, err := os.MkdirTemp("", "overrides-repair-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "repair-comp"
	compDir := filepath.Join(dir, "competitions", compID)
	require.NoError(t, os.MkdirAll(compDir, 0700))
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "overrides.json"), []byte("{not valid json"), 0600))

	// Precondition: the file is genuinely corrupt, and the ordinary reset
	// path (load-then-save) fails identically to a plain load.
	_, err = store.LoadOverrides(compID)
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrCorruptOverrides))
	_, err = store.ResetOverridesChanged(compID)
	require.Error(t, err, "the ordinary reset path loads first and must fail the same way on a corrupt file")

	// The repair door succeeds without reading the corrupt bytes at all.
	require.NoError(t, store.ResetOverridesForce(compID))

	o, err := store.LoadOverrides(compID)
	require.NoError(t, err, "the file must be readable again after the repair")
	assert.Empty(t, o.PoolRanks)
	assert.Empty(t, o.Winners)
}

// TestLoadOverridesLocked_NilFieldInit verifies that loading a valid but empty
// JSON object ("{}") initialises PoolRanks and Winners to non-nil empty maps
// instead of leaving them nil. This exercises the nil-init guard on lines
// 43-48 of overrides.go.
func TestLoadOverridesLocked_NilFieldInit(t *testing.T) {
	dir, err := os.MkdirTemp("", "overrides-nilinit-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "nil-init-comp"
	compDir := filepath.Join(dir, "competitions", compID)
	require.NoError(t, os.MkdirAll(compDir, 0700))
	// Write an empty JSON object; Unmarshal leaves both map fields nil.
	require.NoError(t, os.WriteFile(filepath.Join(compDir, "overrides.json"), []byte("{}"), 0600))

	o, err := store.LoadOverrides(compID)
	require.NoError(t, err)
	require.NotNil(t, o)
	assert.NotNil(t, o.PoolRanks, "PoolRanks must be non-nil after loading {}")
	assert.NotNil(t, o.Winners, "Winners must be non-nil after loading {}")
	assert.Empty(t, o.PoolRanks)
	assert.Empty(t, o.Winners)
}

// TestModifyOverridesChanged_NoChange verifies bytes.Equal early-exit branch:
// saving the same rank value twice returns changed=false on the second call.
func TestModifyOverridesChanged_NoChange(t *testing.T) {
	dir, err := os.MkdirTemp("", "overrides-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(dir)

	store, err := NewStore(dir)
	require.NoError(t, err)

	compID := "no-change-comp"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID, Name: "No Change"}))

	// First save sets a rank
	changed1, err := store.SaveRankOverridesChanged(compID, "Pool1", map[string]int{"alice-id": 1})
	require.NoError(t, err)
	assert.True(t, changed1)

	// Saving the same value again should return false (no change)
	changed2, err := store.SaveRankOverridesChanged(compID, "Pool1", map[string]int{"alice-id": 1})
	require.NoError(t, err)
	assert.False(t, changed2)
}
