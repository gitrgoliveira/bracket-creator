package state

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// bc-dnst renamed the team-member store from squads.yaml to
// team-members.yaml, and its root key from `squads` to `members`. Operator
// rule: a storage change carries a migration path on load covering the last
// two releases. v2.0.0 wrote the shape below; v1.1.0 and earlier stored no
// team members at all, so this is the whole history to carry.
//
// The fixture bytes are written by hand rather than produced by the current
// code ON PURPOSE: the current code can no longer emit this shape, so a
// fixture derived from it would pin nothing. These are the bytes v2.0.0 left
// on disk, and they stay that way forever.
const v200SquadsYAML = `squads:
    c1-p1:
        - id: 11111111-1111-4111-8111-111111111111
          index: 1
          name: Haruki Tanaka
        - id: 22222222-2222-4222-8222-222222222222
          index: 2
          name: ""
`

func TestLegacyUpgrade_AdoptsV200SquadsFile(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte(v200SquadsYAML), 0o600))

	// Precondition: nothing is readable under the current name yet. Without
	// the migration this is what the whole tournament would see.
	_, statErr := os.Stat(filepath.Join(dir, teamMembersFilename))
	require.True(t, os.IsNotExist(statErr), "precondition: the new file must not exist yet")

	s.EnsureLegacyUpgraded(id)

	members, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, members["c1-p1"], 2, "both members recorded by v2.0.0 must survive the rename")
	assert.Equal(t, "Haruki Tanaka", members["c1-p1"][0].Name)
	assert.Equal(t, "11111111-1111-4111-8111-111111111111", members["c1-p1"][0].ID,
		"ids must carry over: a lineup position and a fought bout resolve by them")
	assert.Equal(t, 2, members["c1-p1"][1].Index, "a blank-named slot keeps its number")

	// Converged, not dual-read: exactly one shape is live afterwards.
	data, err := os.ReadFile(filepath.Join(dir, teamMembersFilename))
	require.NoError(t, err)
	assert.Contains(t, string(data), "members:", "the new file carries the new root key")
	_, statErr = os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.True(t, os.IsNotExist(statErr), "the old file is removed once the new one is safely written")
}

// A competition already on the current name must not be clobbered by a stale
// squads.yaml left beside it: the current file wins and the old one is left
// untouched rather than overwriting live data.
func TestLegacyUpgrade_CurrentFileWinsOverStaleLegacyFile(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)

	// Written directly in the CURRENT shape: this test is about which file
	// wins, so it needs a live team-members.yaml and nothing else.
	const current = "members:\n    c1-p1:\n        - id: 99999999-9999-4999-8999-999999999999\n          index: 1\n          name: Current Member\n"
	require.NoError(t, os.WriteFile(filepath.Join(dir, teamMembersFilename), []byte(current), 0o600))
	before, err := s.LoadSquads(id)
	require.NoError(t, err)
	require.Len(t, before["c1-p1"], 1)

	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte(v200SquadsYAML), 0o600))
	s.EnsureLegacyUpgraded(id)

	after, err := s.LoadSquads(id)
	require.NoError(t, err)
	assert.Equal(t, before["c1-p1"], after["c1-p1"], "the live file must be untouched")
	_, statErr := os.Stat(filepath.Join(dir, legacySquadsFilename))
	assert.False(t, os.IsNotExist(statErr), "the stale file is left alone, not consumed")
}

// A file at the legacy path that is not v2.0.0's shape (no `squads` key) must
// not be read as "this competition has no members" and written over the new
// name as an empty list.
func TestLegacyUpgrade_IgnoresAForeignFileAtTheLegacyPath(t *testing.T) {
	s, id := newTeamMemberTestStore(t, "team", 3, false)
	dir := filepath.Join(s.GetFolder(), "competitions", id)
	require.NoError(t, os.WriteFile(filepath.Join(dir, legacySquadsFilename), []byte("something: else\n"), 0o600))

	s.EnsureLegacyUpgraded(id)

	_, statErr := os.Stat(filepath.Join(dir, teamMembersFilename))
	assert.True(t, os.IsNotExist(statErr), "no new file should have been minted from a foreign document")
}
