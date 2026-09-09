// internal/state/comp_path_containment_test.go pins the compPath
// containment guarantee: no compID (an HTTP route parameter in
// production, so callable with arbitrary attacker input) and no
// caller-supplied filename can make compPath build a path outside
// "<folder>/competitions".
package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCompPath_Containment drives compPath directly with the compID and
// path segments that, before the fix, could walk a joined path outside
// the competitions directory. For every case it asserts the NEW result
// stays contained. It also computes what the PRE-FIX implementation
// (filepath.Clean(filepath.Join(folder, "competitions", compID, parts...)))
// would have produced, so a case that used to escape is asserted to
// differ from that escaping value, and a legitimate case is asserted to
// still resolve to the exact same value (no behavior change for real
// competition ids).
func TestCompPath_Containment(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()

	base := filepath.Clean(filepath.Join(store.folder, "competitions"))
	sep := string(filepath.Separator)

	tests := []struct {
		name  string
		id    string
		parts []string
	}{
		{name: "valid id, no parts", id: "comp1"},
		{name: "valid id with filename part", id: "comp1", parts: []string{"config.md"}},
		{name: "dotdot", id: ".."},
		{name: "dotdot dotdot", id: "../.."},
		{name: "dotdot into etc", id: "../../etc"},
		{name: "embedded traversal", id: "a/../../b"},
		{name: "slash in id", id: "foo/bar"},
		{name: "empty id", id: ""},
		{name: "hidden dotfile id", id: ".hidden"},
		{name: "65-char id", id: strings.Repeat("a", 65)},
		{name: "valid id, escaping parts", id: "comp1", parts: []string{"..", "..", "etc", "passwd"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			isValid := ValidateCompetitionID(tc.id) == nil

			oldSegments := append([]string{store.folder, "competitions", tc.id}, tc.parts...)
			oldVulnerable := filepath.Clean(filepath.Join(oldSegments...))
			oldContained := oldVulnerable == base || strings.HasPrefix(oldVulnerable, base+sep)

			got := store.compPath(tc.id, tc.parts...)

			// The core guarantee, unconditional: every result stays under base.
			gotContained := got == base || strings.HasPrefix(got, base+sep)
			assert.Truef(t, gotContained, "compPath(%q, %v) = %q, want a path under %q", tc.id, tc.parts, got, base)

			switch {
			case !oldContained:
				// The pre-fix implementation would have escaped the competitions
				// dir for this input. The new result must not land on that
				// outside path.
				assert.NotEqual(t, oldVulnerable, got,
					"compPath(%q, %v) must not resolve to the outside path %q the pre-fix implementation produced", tc.id, tc.parts, oldVulnerable)
			case isValid:
				// A legitimate, non-escaping id/parts combination must still
				// resolve exactly as before: no behavior change for real callers.
				assert.Equal(t, oldVulnerable, got,
					"compPath(%q, %v) changed behavior for a valid, non-escaping input", tc.id, tc.parts)
			}
		})
	}
}

// TestFileMtime_DoesNotEscapeTournamentFolder exercises the guarantee
// through an exported method rather than compPath directly. FileMtime
// takes an unvalidated compID (it has no ValidateCompetitionID call of
// its own), so before the fix "../.." would walk out of the competitions
// dir and one level above the tournament folder, exactly where this test
// plants a file.
func TestFileMtime_DoesNotEscapeTournamentFolder(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "tournament-data")
	require.NoError(t, os.MkdirAll(dataDir, 0700))

	store, err := NewStore(dataDir)
	require.NoError(t, err)

	// Sits one level above dataDir -- exactly where the pre-fix
	// compPath("../..", "outside-secret.txt") would have pointed.
	outside := filepath.Join(root, "outside-secret.txt")
	require.NoError(t, os.WriteFile(outside, []byte("secret"), 0600))

	got := store.FileMtime("../..", "outside-secret.txt")
	assert.Equal(t, int64(0), got, "FileMtime must not stat a file outside the tournament folder")
}
