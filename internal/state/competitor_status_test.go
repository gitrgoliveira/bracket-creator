package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestParseCompetitorStatusBytes_Empty covers the empty-input fast path.
func TestParseCompetitorStatusBytes_Empty(t *testing.T) {
	m, err := parseCompetitorStatusBytes(nil)
	require.NoError(t, err)
	assert.Empty(t, m)
}

// TestParseCompetitorStatusBytes_MalformedYAML covers the yaml.Unmarshal
// error path.
func TestParseCompetitorStatusBytes_MalformedYAML(t *testing.T) {
	_, err := parseCompetitorStatusBytes([]byte(":\t:bad yaml:"))
	assert.Error(t, err)
}

// TestSetCompetitorStatus_RoundTrip verifies load-set-load for a single
// status entry.
func TestSetCompetitorStatus_RoundTrip(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	compID := "cs-rt"
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	status := domain.CompetitorStatus{
		PlayerID: "player-1",
		Eligible: false,
		MatchID:  "M1",
		Reason:   "injury",
	}
	require.NoError(t, store.SetCompetitorStatus(compID, status))

	statuses, err := store.LoadCompetitorStatus(compID)
	require.NoError(t, err)
	got, ok := statuses["player-1"]
	require.True(t, ok)
	assert.False(t, got.Eligible)
	assert.Equal(t, "M1", got.MatchID)
}

// TestLoadCompetitorStatus_MalformedFile covers the parseCompetitorStatusBytes
// error path via loadCompetitorStatusLocked.
func TestLoadCompetitorStatus_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	compID := "cs-bad"
	store, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	// Write invalid YAML directly to the status file to trigger parse error.
	path := filepath.Join(dir, "competitions", compID, "competitor-status.yaml")
	require.NoError(t, os.WriteFile(path, []byte(":\t:bad yaml:"), 0o600))

	_, err = store.LoadCompetitorStatus(compID)
	assert.Error(t, err)
}

// TestCopyReservedSlots_Nil verifies that copyReservedSlots(nil) returns nil.
func TestCopyReservedSlots_Nil(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	assert.Nil(t, store.copyReservedSlots(nil))
}

// TestParseReservedSlotsFile_NullJSON covers the "slots == nil" guard in
// parseReservedSlotsFile: when the JSON is "null", json.Unmarshal sets the
// slice to nil, and the guard should convert it to an empty (non-nil) slice.
func TestParseReservedSlotsFile_NullJSON(t *testing.T) {
	dir := t.TempDir()
	compID := "null-slots"
	store, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	path := filepath.Join(dir, "competitions", compID, "reserved-slots.json")
	require.NoError(t, os.WriteFile(path, []byte("null"), 0o600))

	slots, err := store.loadReservedSlotsLocked(compID)
	require.NoError(t, err)
	assert.NotNil(t, slots, "null JSON must produce non-nil empty slice")
	assert.Empty(t, slots)
}

// TestLoadReservedSlotsLocked_MalformedJSON covers the parseReservedSlotsFile
// error path inside loadReservedSlotsLocked.
func TestLoadReservedSlotsLocked_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	compID := "rs-bad"
	store, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&Competition{ID: compID}))

	path := filepath.Join(dir, "competitions", compID, "reserved-slots.json")
	require.NoError(t, os.WriteFile(path, []byte("{not valid json"), 0o600))

	_, err = store.loadReservedSlotsLocked(compID)
	assert.Error(t, err)
}
