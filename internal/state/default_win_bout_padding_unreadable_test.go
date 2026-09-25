package state

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLegacyDefaultWinPaddingNeverOverwritesAnUnreadableCell guards the
// bc-tmfn data-loss finding: a pool match closed by a default win (kiken)
// whose SubResults cell failed to parse must NOT be padded by the legacy
// upgrade pass. Padding turns the (empty, because unreadable) SubResults
// into a non-empty slice of placeholder rows, and savePoolMatchesLocked's
// SubResults column only falls back to the retained raw bytes when
// SubResults is still empty -- so padding it destroys the organiser's only
// copy of the corrupt cell forever.
func TestLegacyDefaultWinPaddingNeverOverwritesAnUnreadableCell(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C", TeamSize: 3}))

	team := MatchResult{
		ID: "Pool A-1", SideA: "Kenshikan", SideB: "Sanshukan", Winner: "Kenshikan",
		Status: MatchStatusCompleted, Decision: "kiken",
		SubResults: []SubMatchResult{
			{Position: 1, SideA: "Tanaka", SideB: "Suzuki", IpponsA: []string{"M"}, Winner: "Tanaka"},
		},
	}
	require.NoError(t, s.SavePoolMatches("c", []MatchResult{team}))

	path := s.compPath("c", "pool-matches.csv")
	mangleCell(t, path, `""position"":1`, `""position"":1x`)

	// Cold reload: LoadPoolMatches runs EnsureLegacyUpgraded, whose last step
	// is upgradeTeamDefaultWinBoutPaddingLocked. Before the fix this padded
	// the now-empty SubResults with 3 placeholder positions and immediately
	// rewrote the file, destroying the corrupt cell's bytes.
	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadPoolMatches("c")
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Empty(t, loaded[0].SubResults, "an unreadable cell must not be padded")
	assert.True(t, loaded[0].SubResultsUnreadable, "and must still say so")

	raw, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(raw), "Tanaka",
		"the malformed cell's bytes must survive the legacy default-win padding pass")
	assert.Contains(t, string(raw), `""position"":1x`,
		"and must survive VERBATIM, not partially rewritten by padding")
}
