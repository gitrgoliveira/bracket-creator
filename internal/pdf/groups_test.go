package pdf

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSheetSpecMatches(t *testing.T) {
	tests := []struct {
		name  string
		spec  sheetSpec
		sheet string
		want  bool
	}{
		{"exact hit", exact("data"), "data", true},
		{"exact miss on prefix", exact("Tags"), "Tags A", false},
		{"prefix hit base", prefix("Tags"), "Tags", true},
		{"prefix hit suffixed", prefix("Names to Print"), "Names to Print B", true},
		{"prefix hit tree", prefix("Tree"), "Tree 12", true},
		{"prefix miss", prefix("Tree"), "Time Estimator", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.spec.matches(tt.sheet))
		})
	}
}

func TestGroupResolveSheets(t *testing.T) {
	// Mirrors real generator output: Names/Tree are split into suffixed sheets.
	present := []string{
		"data", "Time Estimator", "Pool Draw", "Pool Matches",
		"Elimination Matches", "Tree 1", "Tree 2",
		"Names to Print A", "Names to Print B", "Tags",
	}

	cases := map[string][]string{
		"registration": {"data"},
		"names":        {"Names to Print A", "Names to Print B"},
		"tags":         {"Tags"},
		"pools-trees":  {"Pool Draw", "Tree 1", "Tree 2"},
		"full-bracket": {"Pool Draw", "Pool Matches", "Elimination Matches", "Tree 1", "Tree 2"},
	}

	for typ, want := range cases {
		t.Run(typ, func(t *testing.T) {
			g, ok := GroupByType(typ)
			require.Truef(t, ok, "group %q must exist", typ)
			assert.Equal(t, want, g.resolveSheets(present))
		})
	}
}

func TestGroupResolveSheetsPreservesGroupOrder(t *testing.T) {
	// Even if the workbook lists Tree before Pool Draw, the group's declared
	// sheet order wins (Pool Draw first, then Trees).
	present := []string{"Tree 1", "Pool Draw", "Tree 2"}
	g, _ := GroupByType("pools-trees")
	assert.Equal(t, []string{"Pool Draw", "Tree 1", "Tree 2"}, g.resolveSheets(present))
}

func TestPageNumbersOnlyOnBracketGroups(t *testing.T) {
	want := map[string]bool{
		"registration": false,
		"names":        false,
		"tags":         false,
		"pools-trees":  true,
		"full-bracket": true,
	}
	for _, g := range Groups {
		assert.Equalf(t, want[g.Type], g.PageNumbers, "group %q page-number flag", g.Type)
	}
}

func TestOnlyTagsSkipsTeamWorkbooks(t *testing.T) {
	for _, g := range Groups {
		assert.Equalf(t, g.Type == "tags", g.SkipTeamWorkbooks, "group %q team-skip flag", g.Type)
	}
}

func TestProfileInstallURI(t *testing.T) {
	tests := []struct {
		name string
		path string
		want string
	}{
		// POSIX absolute paths produce file:// + path (three slashes total
		// because the path begins with '/').
		{"posix tmp", "/tmp/lo-profile-abc", "file:///tmp/lo-profile-abc"},
		{"posix nested", "/var/folders/x/y/T/lo-profile-123", "file:///var/folders/x/y/T/lo-profile-123"},
		// Windows absolute paths: after filepath.ToSlash the drive letter is
		// first, so an extra leading slash gives the correct file:///C:/... form
		// (RFC 8089 §2). os.MkdirTemp on Windows produces forward-slashed paths
		// after ToSlash, so we test that form here.
		{"windows forward slashes", "C:/Temp/lo-profile-xyz", "file:///C:/Temp/lo-profile-xyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, profileInstallURI(tt.path))
		})
	}
}
