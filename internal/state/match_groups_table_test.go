package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMatchGroups_SharedTable is the Go half of the shared Go/JS table of
// match write groups (bc-mrgc): a client names the groups its write changes
// by these exact strings, and the score handler refuses one it does not know
// (changedGroupsError), so the client's list (web-mobile/js/match_groups.jsx,
// pinned by match_groups_table.test.jsx) and this one must never drift.
func TestMatchGroups_SharedTable(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile(filepath.Join("testdata", "match_groups.json"))
	require.NoError(t, err, "shared Go/JS group table is missing")

	var table struct {
		ScalarGroups []string `json:"scalarGroups"`
		Bouts        []struct {
			Position int    `json:"position"`
			Group    string `json:"group"`
		} `json:"bouts"`
		Invalid []string `json:"invalid"`
	}
	require.NoError(t, json.Unmarshal(raw, &table))
	require.NotEmpty(t, table.ScalarGroups, "table parsed to no scalar groups: it would assert nothing")
	require.NotEmpty(t, table.Bouts, "table parsed to no bout groups: it would assert nothing")
	require.NotEmpty(t, table.Invalid, "table parsed to no invalid names: it would assert nothing")

	assert.Equal(t, table.ScalarGroups, ScalarGroups, "the scalar groups and their order")
	for _, g := range table.ScalarGroups {
		assert.True(t, ValidGroup(g), "%q is a group", g)
	}
	for _, b := range table.Bouts {
		assert.Equal(t, b.Group, BoutGroup(b.Position))
		pos, ok := ParseBoutGroup(b.Group)
		assert.True(t, ok, "%q names a bout", b.Group)
		assert.Equal(t, b.Position, pos)
		assert.True(t, ValidGroup(b.Group), "%q is a group", b.Group)
	}
	for _, g := range table.Invalid {
		assert.False(t, ValidGroup(g), "%q is not a group", g)
	}
}
