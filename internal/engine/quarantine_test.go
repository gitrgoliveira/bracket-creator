package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

const brokenBracket = "{\n  \"rounds\": [\n    [{\"id\": \"R1-1\"x}]\n  ]\n}\n"

func quarantineComp(t *testing.T, store *state.Store, eng *Engine, dir, id, format string) string {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: id, Name: "Q", Kind: "individual", Format: format,
		PoolSize: 3, PoolSizeMode: "min", PoolWinners: 2,
		Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
	}))
	// Eight, because a mixed (Pools + Knockout) competition needs at least two
	// pools to have a knockout stage at all.
	saveTestParticipants(t, store, id, []string{
		"Alice", "Bob", "Charlie", "Dave", "Erin", "Frank", "Grace", "Heidi",
	})
	require.NoError(t, eng.StartCompetition(id))
	return filepath.Join(dir, "competitions", id, "bracket.json")
}

func breakBracket(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(brokenBracket), 0600))
}

// The whole point of the action: the operator's bytes are moved aside, never
// deleted, because a file that will not parse is the only record of what it
// described and nothing can say how much that is.
func TestQuarantineCorruptBracket_KeepsTheBrokenFile(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	path := quarantineComp(t, store, eng, dir, "mixed-q", state.CompFormatMixed)
	breakBracket(t, path)

	res, err := eng.QuarantineCorruptBracket("mixed-q")
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.True(t, res.Rebuilt, "a pool-fed knockout keeps its draw in pools.csv, so it rebuilds")
	assert.Contains(t, res.QuarantinedAs, "bracket.json.corrupt-")

	kept, err := os.ReadFile(filepath.Join(filepath.Dir(path), res.QuarantinedAs)) // #nosec G304
	require.NoError(t, err)
	assert.Equal(t, brokenBracket, string(kept),
		"the operator's bytes survive verbatim, for a later repair by hand")

	// And the competition is scoreable again.
	rebuilt, err := store.LoadBracket("mixed-q")
	require.NoError(t, err, "the fresh bracket parses")
	assert.NotEmpty(t, rebuilt.Rounds)
}

// The guard that stops this being a general bracket-wipe.
func TestQuarantineCorruptBracket_RefusesAReadableBracket(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	path := quarantineComp(t, store, eng, dir, "mixed-ok", state.CompFormatMixed)

	before, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)

	_, err = eng.QuarantineCorruptBracket("mixed-ok")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "readable bracket")

	after, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	assert.Equal(t, before, after, "a healthy bracket is untouched")
}

// For a direct-elimination competition the bracket file IS the draw. Rebuilding
// it from today's roster would not restore the tournament, it would invent a
// different one that disagrees with the sheet already printed and on the wall.
func TestQuarantineCorruptBracket_RefusesPlayoffsRatherThanInventingADraw(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	path := quarantineComp(t, store, eng, dir, "playoffs-q", state.CompFormatPlayoffs)
	breakBracket(t, path)

	_, err := eng.QuarantineCorruptBracket("playoffs-q")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "only record of its draw")
	assert.Contains(t, err.Error(), "Repair the file")

	after, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	assert.Equal(t, brokenBracket, string(after),
		"and it refuses BEFORE moving anything, so the file is exactly where the operator left it")
}

// A league has no knockout stage, so its bracket.json is vestigial: moving it
// aside is the whole repair and there is nothing to rebuild.
func TestQuarantineCorruptBracket_LeagueHasNothingToRebuild(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	id := "league-q"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: id, Name: "Q", Kind: "individual", Format: state.CompFormatLeague,
		Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
	}))
	saveTestParticipants(t, store, id, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(id))
	path := filepath.Join(dir, "competitions", id, "bracket.json")
	breakBracket(t, path)

	res, err := eng.QuarantineCorruptBracket(id)
	require.NoError(t, err)
	assert.False(t, res.Rebuilt)
	assert.NotEmpty(t, res.QuarantinedAs)
	_, statErr := os.Stat(path)
	assert.True(t, os.IsNotExist(statErr), "the broken file is out of the way")
}
