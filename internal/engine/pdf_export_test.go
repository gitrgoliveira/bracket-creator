package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// zipMagic is the leading 4 bytes of an XLSX (a ZIP archive).
var zipMagic = []byte{0x50, 0x4b, 0x03, 0x04}

// TestExportTournamentWorkbooks_ExplicitIDs verifies that passing explicit
// compIDs writes one .xlsx per requested competition into tmpDir and returns
// a SourceWorkbook per comp with the correct Path/Title/IsTeam.
func TestExportTournamentWorkbooks_ExplicitIDs(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	createTestCompetition(t, store, "comp-a", "league", 3)
	saveTestParticipants(t, store, "comp-a", []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition("comp-a"))

	// comp-b has no Name, so the title must fall back to the comp ID.
	compB := &state.Competition{
		ID: "comp-b", Format: "league", PoolSize: 3, RoundRobin: true,
		Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
	}
	require.NoError(t, store.SaveCompetition(compB))
	saveTestParticipants(t, store, "comp-b", []string{"Dave", "Eve", "Frank"})
	require.NoError(t, eng.StartCompetition("comp-b"))

	tmpDir := t.TempDir()
	sources, err := eng.ExportTournamentWorkbooks(tmpDir, "comp-a", "comp-b")
	require.NoError(t, err)
	require.Len(t, sources, 2)

	assert.Equal(t, filepath.Join(tmpDir, "comp-a.xlsx"), sources[0].Path)
	assert.Equal(t, "Test Competition", sources[0].Title, "comp-a has a Name, must use it as Title")
	assert.False(t, sources[0].IsTeam)

	assert.Equal(t, filepath.Join(tmpDir, "comp-b.xlsx"), sources[1].Path)
	assert.Equal(t, "comp-b", sources[1].Title, "comp-b has no Name, must fall back to the comp ID")
	assert.False(t, sources[1].IsTeam)

	for _, src := range sources {
		data, err := os.ReadFile(src.Path)
		require.NoErrorf(t, err, "expected workbook file to exist at %s", src.Path)
		require.NotEmpty(t, data, "workbook file must be non-empty")
		assert.Equal(t, zipMagic, data[:4], "workbook must be a valid ZIP/XLSX")
	}
}

// TestExportTournamentWorkbooks_AllCompetitions verifies that omitting
// compIDs exports every competition returned by store.ListCompetitions.
func TestExportTournamentWorkbooks_AllCompetitions(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	createTestCompetition(t, store, "comp-x", "league", 3)
	saveTestParticipants(t, store, "comp-x", []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition("comp-x"))

	createTestCompetition(t, store, "comp-y", "league", 3)
	saveTestParticipants(t, store, "comp-y", []string{"Dave", "Eve", "Frank"})
	require.NoError(t, eng.StartCompetition("comp-y"))

	tmpDir := t.TempDir()
	sources, err := eng.ExportTournamentWorkbooks(tmpDir)
	require.NoError(t, err)
	require.Len(t, sources, 2, "empty compIDs must export ALL competitions")

	gotIDs := map[string]bool{}
	for _, src := range sources {
		base := filepath.Base(src.Path)
		gotIDs[base[:len(base)-len(".xlsx")]] = true
		data, err := os.ReadFile(src.Path)
		require.NoError(t, err)
		assert.NotEmpty(t, data)
	}
	assert.True(t, gotIDs["comp-x"])
	assert.True(t, gotIDs["comp-y"])
}

// TestExportTournamentWorkbooks_NoCompetitions verifies that with zero
// competitions on disk and no explicit compIDs, the "no competitions to
// export" error is returned.
func TestExportTournamentWorkbooks_NoCompetitions(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	tmpDir := t.TempDir()
	sources, err := eng.ExportTournamentWorkbooks(tmpDir)
	require.Error(t, err)
	assert.Nil(t, sources)
	assert.Contains(t, err.Error(), "no competitions to export")
}

// TestExportTournamentWorkbooks_UnknownCompID verifies that an explicit,
// unknown comp ID surfaces as a *NotFoundError (the HTTP 404 sentinel).
func TestExportTournamentWorkbooks_UnknownCompID(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	tmpDir := t.TempDir()
	sources, err := eng.ExportTournamentWorkbooks(tmpDir, "does-not-exist")
	require.Error(t, err)
	assert.Nil(t, sources)
	var nfe *NotFoundError
	assert.ErrorAs(t, err, &nfe, "unknown compID must return NotFoundError")
}

// TestExportTournamentWorkbooks_IsTeamFlag verifies IsTeam is true when
// either TeamSize > 0 or Kind == "team", and false otherwise.
func TestExportTournamentWorkbooks_IsTeamFlag(t *testing.T) {
	tests := []struct {
		name       string
		comp       *state.Competition
		playerName []string
		wantTeam   bool
	}{
		{
			name: "individual comp is not a team",
			comp: &state.Competition{
				ID: "indiv", Kind: "individual", Format: "league", PoolSize: 3,
				RoundRobin: true, Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
			},
			playerName: []string{"Alice", "Bob", "Charlie"},
			wantTeam:   false,
		},
		{
			name: "TeamSize > 0 marks IsTeam true",
			comp: &state.Competition{
				ID: "team-size", Kind: "individual", Format: "league", PoolSize: 3,
				TeamSize: 3, RoundRobin: true, Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
			},
			playerName: []string{"TeamA", "TeamB", "TeamC"},
			wantTeam:   true,
		},
		{
			name: "Kind == team marks IsTeam true",
			comp: &state.Competition{
				ID: "team-kind", Kind: "team", Format: "league", PoolSize: 3,
				RoundRobin: true, Courts: []string{"A"}, StartTime: "09:00", Status: "setup",
			},
			playerName: []string{"TeamA", "TeamB", "TeamC"},
			wantTeam:   true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			require.NoError(t, store.SaveCompetition(tc.comp))
			saveTestParticipants(t, store, tc.comp.ID, tc.playerName)
			require.NoError(t, eng.StartCompetition(tc.comp.ID))

			tmpDir := t.TempDir()
			sources, err := eng.ExportTournamentWorkbooks(tmpDir, tc.comp.ID)
			require.NoError(t, err)
			require.Len(t, sources, 1)
			assert.Equal(t, tc.wantTeam, sources[0].IsTeam)
		})
	}
}
