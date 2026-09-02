package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportCompetitionXlsx_RejectsSwiss pins bc-yuy8 Phase 2a: Swiss has no
// pools and no static bracket, so ExportCompetitionXlsx must reject it with
// the shared sentinel BEFORE doing any rendering work, rather than emitting
// an effectively empty workbook (measured pre-fix: sheets present but the
// data sheet held only 6 header cells and no participants anywhere).
func TestExportCompetitionXlsx_RejectsSwiss(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	createTestCompetition(t, store, "swiss-export", state.CompFormatSwiss, 4, func(c *state.Competition) {
		c.SwissRounds = 3
	})
	saveTestParticipants(t, store, "swiss-export", []string{"Alice", "Bob", "Charlie", "Dave"})

	data, err := eng.ExportCompetitionXlsx("swiss-export")
	require.Error(t, err)
	assert.Nil(t, data)
	assert.ErrorIs(t, err, ErrSwissExportUnsupported)
}

// TestExportTournamentWorkbooks_SkipsSwissAndReportsIt is the mixed-set case:
// one Swiss competition must not abort the whole print booklet for every
// OTHER competition in the tournament. It is skipped and reported back to
// the caller (via the second return value) rather than erroring or being
// silently dropped.
func TestExportTournamentWorkbooks_SkipsSwissAndReportsIt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	createTestCompetition(t, store, "swiss-comp", state.CompFormatSwiss, 4, func(c *state.Competition) {
		c.Name = "Swiss Comp"
		c.SwissRounds = 2
	})
	saveTestParticipants(t, store, "swiss-comp", []string{"Alice", "Bob", "Charlie", "Dave"})

	createTestCompetition(t, store, "league-comp", state.CompFormatLeague, 3, func(c *state.Competition) {
		c.Name = "League Comp"
	})
	saveTestParticipants(t, store, "league-comp", []string{"Eve", "Frank", "Grace"})
	require.NoError(t, eng.StartCompetition("league-comp"))

	tmpDir := t.TempDir()
	sources, skipped, err := eng.ExportTournamentWorkbooks(tmpDir, "swiss-comp", "league-comp")
	require.NoError(t, err, "one Swiss competition must not abort the whole batch")

	require.Len(t, sources, 1, "the renderable competition must still be exported")
	assert.Equal(t, "League Comp", sources[0].Title)

	require.Len(t, skipped, 1, "the Swiss competition must be reported as skipped, not silently dropped")
	assert.Equal(t, "swiss-comp", skipped[0].ID)
	assert.Equal(t, "Swiss Comp", skipped[0].Name)
	assert.NotEmpty(t, skipped[0].Reason)
}
