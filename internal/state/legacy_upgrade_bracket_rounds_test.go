package state_test

// legacy_upgrade_bracket_rounds_test.go pins the bc-tmfn load-time repair for
// bracket.json: a bracket drawn by v2.0.0 or v2.1.0 carries rounds and match
// numbers from the old classification (a pair beside an empty pair one round
// early), and EnsureLegacyUpgraded rewrites them from the bracket's own stored
// Feeders, once, leaving everything else as it was. The input is the file a
// v2.1.0 binary wrote (testdata/bracket_v2.1.0_knockout5.json), placed on disk
// byte for byte.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLegacyBracketRoundsUpgrade_V21BracketIsCorrectedOnLoadOnce(t *testing.T) {
	dir, _ := newLegacyUpgradeFixture(t)
	v21, err := os.ReadFile(filepath.FromSlash(v21FiveEntrantFixture))
	require.NoError(t, err)
	path := filepath.Join(dir, "competitions", "c1", "bracket.json")
	require.NoError(t, os.WriteFile(path, v21, 0o600))

	first := freshLegacyUpgradeStore(t, dir)
	served, err := first.LoadBracket("c1")
	require.NoError(t, err)

	upgraded, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NotEqual(t, string(v21), string(upgraded), "the v2.1 rounds must be rewritten on disk")
	assert.NotZero(t, first.FileVersion("c1", "bracket.json"),
		"the rewrite goes through saveBracketLocked, which bumps the bracket's file version")

	// The file on disk equals the v2.1 file with exactly these four fields
	// changed: sides, ids, winners, the recorded P4 v P5 result, courts,
	// scheduled times, Hidden rows, feeders and the draw order are identical.
	var want state.Bracket
	require.NoError(t, json.Unmarshal(v21, &want))
	for id, rn := range map[string][2]int{
		"m-r1-0": {2, 2},
		"m-r1-3": {3, 1},
		"m-r2-1": {2, 3},
		"m-r3-0": {1, 4},
	} {
		m := matchByID(t, &want, id)
		m.DisplayRound, m.MatchNumber = rn[0], rn[1]
	}
	var onDisk state.Bracket
	require.NoError(t, json.Unmarshal(upgraded, &onDisk))
	assert.Equal(t, want, onDisk)
	// And the read that triggered it serves the corrected bracket, not a
	// cached copy of the old one.
	assert.Equal(t, &want, served)

	// A later process loads the corrected file and writes nothing.
	second := freshLegacyUpgradeStore(t, dir)
	_, err = second.LoadBracket("c1")
	require.NoError(t, err)
	again, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(upgraded), string(again), "an already-corrected bracket must not be rewritten")
	assert.Zero(t, second.FileVersion("c1", "bracket.json"), "no write, so no version bump")
}

// A bracket whose Feeders cannot be walked is left byte for byte as stored:
// the repair never guesses at a round.
func TestLegacyBracketRoundsUpgrade_UnwalkableBracketIsLeftAsStored(t *testing.T) {
	dir, _ := newLegacyUpgradeFixture(t)
	var b state.Bracket
	v21, err := os.ReadFile(filepath.FromSlash(v21FiveEntrantFixture))
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(v21, &b))
	m := matchByID(t, &b, "m-r1-1") // a real bout no match names as a feeder
	m.SideA, m.SideB, m.Hidden = "X", "Y", false
	// Ids of its own, so the side-id repair that runs first has nothing to do
	// and any byte that moves would be this pass's.
	m.SideAID, m.SideBID = "x-id", "y-id"
	raw, err := json.MarshalIndent(&b, "", "  ")
	require.NoError(t, err)
	path := filepath.Join(dir, "competitions", "c1", "bracket.json")
	require.NoError(t, os.WriteFile(path, raw, 0o600))

	fresh := freshLegacyUpgradeStore(t, dir)
	_, err = fresh.LoadBracket("c1")
	require.NoError(t, err)
	after, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, string(raw), string(after))
	assert.Zero(t, fresh.FileVersion("c1", "bracket.json"))
}
