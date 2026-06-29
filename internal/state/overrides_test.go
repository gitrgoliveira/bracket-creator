package state

import (
	"os"
	"path/filepath"
	"testing"

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

	// 1. Load empty overrides (doesn't exist)
	overrides, err := store.LoadOverrides(compID)
	require.NoError(t, err)
	require.NotNil(t, overrides)
	assert.Empty(t, overrides.PoolRanks)
	assert.Empty(t, overrides.Winners)

	// 2. Save rank override
	err = store.SaveRankOverride(compID, "Pool A", "Alice", 1)
	require.NoError(t, err)

	// 3. Load overrides after save
	overrides, err = store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Equal(t, 1, overrides.PoolRanks["Pool A"]["Alice"])

	// 4. Save winner override
	err = store.SaveWinnerOverride(compID, "Match-1", "Bob")
	require.NoError(t, err)

	// 5. Load overrides after save
	overrides, err = store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Equal(t, "Bob", overrides.Winners["Match-1"])
	assert.Equal(t, 1, overrides.PoolRanks["Pool A"]["Alice"])

	// 6. Reset overrides
	err = store.ResetOverrides(compID)
	require.NoError(t, err)

	// 7. Load overrides after reset
	overrides, err = store.LoadOverrides(compID)
	require.NoError(t, err)
	assert.Empty(t, overrides.PoolRanks)
	assert.Empty(t, overrides.Winners)
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
	changed1, err := store.SaveRankOverrideChanged(compID, "Pool1", "Alice", 1)
	require.NoError(t, err)
	assert.True(t, changed1)

	// Saving the same value again should return false (no change)
	changed2, err := store.SaveRankOverrideChanged(compID, "Pool1", "Alice", 1)
	require.NoError(t, err)
	assert.False(t, changed2)
}
