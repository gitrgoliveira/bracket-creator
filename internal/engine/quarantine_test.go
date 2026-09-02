package engine

import (
	"fmt"
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
func TestQuarantineCorruptBracket_RefusesKnockoutRatherThanInventingADraw(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	path := quarantineComp(t, store, eng, dir, "knockout-q", state.CompFormatKnockout)
	breakBracket(t, path)

	_, err := eng.QuarantineCorruptBracket("knockout-q")
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

// The three recovery outcomes, driven through the real entry point for every
// format the app can hold, rather than restated as an expression that would
// happily agree with a broken gate.
//
// This is the test that would have caught the hole it now guards: the refusal
// used to be a hand-written `Format == knockout || Format == ""`, so an
// UNRECOGNISED format -- a typo in a hand-edited config.md -- passed it,
// reached the quarantine, and had its bracket moved aside with nothing built to
// replace it. That format takes the draw pipeline's default branch and gets a
// standalone knockout bracket, so the file it lost was the only record of its
// draw. The table says which formats those are; this says the engine agrees.
func TestQuarantineCorruptBracket_OutcomeFollowsTheFormatTable(t *testing.T) {
	for i, tc := range loadFormatDrawsBracketTable(t) {
		t.Run(tc.Why, func(t *testing.T) {
			eng, store, dir := setupTestEngine(t)
			id := fmt.Sprintf("fmt-%d", i)
			path := quarantineComp(t, store, eng, dir, id, tc.Format)
			breakBracket(t, path)

			res, err := eng.QuarantineCorruptBracket(id)

			switch {
			case tc.RebuildableFromPools:
				require.NoError(t, err, "format %q rebuilds from pools.csv", tc.Format)
				require.NotNil(t, res)
				assert.True(t, res.Rebuilt,
					"format %q rebuilds, so the caller must be able to say so", tc.Format)
			case !tc.DrawsBracket:
				require.NoError(t, err,
					"format %q draws no bracket, so its bracket.json is vestigial and moving it "+
						"aside is the whole repair", tc.Format)
				require.NotNil(t, res)
				assert.False(t, res.Rebuilt,
					"format %q has no knockout stage, so nothing was rebuilt and Rebuilt must not "+
						"claim otherwise -- the console words the operator's confirmation from it",
					tc.Format)
				assert.Contains(t, res.QuarantinedAs, "bracket.json.corrupt-")
			default:
				var validation *ValidationError
				require.ErrorAs(t, err, &validation,
					"format %q draws its bracket directly, so quarantining it would discard the "+
						"only record of the draw and must be refused", tc.Format)
				assert.Nil(t, res)
				assert.FileExists(t, path, "a refused quarantine leaves the file exactly where it was")
			}
		})
	}
}
