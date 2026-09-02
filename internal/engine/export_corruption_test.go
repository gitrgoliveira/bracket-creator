package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// breakTournament replaces tournament.md with a DIRECTORY, forcing
// LoadTournament's os.ReadFile call to fail with a real I/O error.
//
// A malformed YAML front matter does NOT make LoadTournament error: its
// parseFrontMatter failure path is a deliberate silent fallback to a default
// Tournament{} (internal/state/tournament.go, the "If it's not a
// front-matter file, return a default tournament" branch), not an error
// return. So "corrupt" here has to mean genuinely unreadable, which a
// directory in the file's place reliably reproduces on every OS regardless
// of the test's own privilege level (a permission-bit trick would silently
// pass for a root-run test).
func breakTournament(t *testing.T, dir string) {
	t.Helper()
	path := filepath.Join(dir, "tournament.md")
	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, os.Mkdir(path, 0o755))
}

// TestExportCompetitionXlsx_CorruptBracket_FailsForEveryFormat pins mp-yuy8
// criterion 4: ExportCompetitionXlsx now loads the stored bracket
// unconditionally and strictly, for every format -- not just naginata and
// pure-playoffs competitions, which already loaded it strictly before this
// change. Before this fix, a Mixed (pool-fed, non-naginata) competition's
// corrupt bracket.json was swallowed by the best-effort `else` branch and the
// export silently continued with a nil bracket, banding sheets by the draw's
// regions instead of the live shiaijo each bout is on.
func TestExportCompetitionXlsx_CorruptBracket_FailsForEveryFormat(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	path := quarantineComp(t, store, eng, dir, "mixed-corrupt-bracket", state.CompFormatMixed)
	breakBracket(t, path)

	data, err := eng.ExportCompetitionXlsx("mixed-corrupt-bracket")
	require.Error(t, err, "a corrupt bracket.json must abort the export rather than silently banding by the draw's regions")
	assert.Nil(t, data)
}

// TestExportCompetitionXlsx_CorruptTournament_Fails pins mp-yuy8 criterion 6:
// a corrupt (unreadable) tournament.md now aborts the export rather than
// CompetitionCourts silently degrading to the competition's own court list.
func TestExportCompetitionXlsx_CorruptTournament_Fails(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "export-corrupt-tournament"
	createTestCompetition(t, store, compID, "league", 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})
	require.NoError(t, eng.StartCompetition(compID))
	require.NoError(t, store.SaveTournament(&state.Tournament{Name: "Cup", Courts: []string{"A"}}))

	breakTournament(t, dir)

	data, err := eng.ExportCompetitionXlsx(compID)
	require.Error(t, err, "a corrupt tournament.md must abort the export rather than silently printing positional court labels")
	assert.Nil(t, data)
}

// TestExportCompetitionXlsx_MissingBracketAndTournament_Succeeds guards
// against over-strictness: a competition that has never been drawn (no
// bracket.json) and a folder with no tournament.md at all (the bootstrap
// window before POST /tournament) must still export cleanly. Both loaders
// treat "does not exist" as (nil/empty, nil), never an error.
func TestExportCompetitionXlsx_MissingBracketAndTournament_Succeeds(t *testing.T) {
	eng, store, dir := setupTestEngine(t)
	compID := "export-missing-bracket-tournament"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave", "Eve", "Frank"})
	// Deliberately NOT calling StartCompetition/GenerateDraw: no pools.csv, no
	// bracket.json. Deliberately NOT calling SaveTournament: no tournament.md.
	require.NoFileExists(t, filepath.Join(dir, "tournament.md"))
	require.NoFileExists(t, filepath.Join(dir, "competitions", compID, "bracket.json"))

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	requireZipHeader(t, data)
}
