package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/pdf"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, dir, name string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644))
}

func TestCollectWorkbooks(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "individual_men.xlsx")
	writeFile(t, dir, "individual_women.xlsx")
	writeFile(t, dir, "team_registrations.xlsx") // team by name heuristic
	writeFile(t, dir, "notes.txt")               // ignored: not xlsx
	writeFile(t, dir, "~$individual_men.xlsx")   // ignored: Excel lock file

	got, err := collectWorkbooks(dir, nil)
	require.NoError(t, err)
	require.Len(t, got, 3, "only the three real .xlsx files")

	// Sorted by path; assert team detection.
	byBase := map[string]bool{}
	for _, s := range got {
		byBase[filepath.Base(s.Path)] = s.IsTeam
	}
	assert.False(t, byBase["individual_men.xlsx"])
	assert.False(t, byBase["individual_women.xlsx"])
	assert.True(t, byBase["team_registrations.xlsx"], "filename containing 'team' is a team workbook")
}

func TestCollectWorkbooksExplicitTeamFile(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "groupA.xlsx") // no "team" in name
	writeFile(t, dir, "groupB.xlsx")

	got, err := collectWorkbooks(dir, []string{"groupA.xlsx"})
	require.NoError(t, err)
	require.Len(t, got, 2)

	for _, s := range got {
		if filepath.Base(s.Path) == "groupA.xlsx" {
			assert.True(t, s.IsTeam, "explicit --team-file marks the workbook as team")
		} else {
			assert.False(t, s.IsTeam)
		}
	}
}

func TestCollectWorkbooksEmptyDir(t *testing.T) {
	_, err := collectWorkbooks(t.TempDir(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no .xlsx files")
}

func TestCollectWorkbooksSorted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, dir, "c.xlsx")
	writeFile(t, dir, "a.xlsx")
	writeFile(t, dir, "b.xlsx")
	got, err := collectWorkbooks(dir, nil)
	require.NoError(t, err)
	require.Len(t, got, 3)
	assert.Equal(t, "a.xlsx", filepath.Base(got[0].Path))
	assert.Equal(t, "b.xlsx", filepath.Base(got[1].Path))
	assert.Equal(t, "c.xlsx", filepath.Base(got[2].Path))
}

// runPrintCmd is a helper that executes the print command with the given args
// and returns the error (if any). It does NOT require soffice.
func runPrintCmd(t *testing.T, args ...string) error {
	t.Helper()
	cmd := newPrintCmd()
	cmd.SetArgs(args)
	// Suppress usage on errors so test output is clean.
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	return cmd.Execute()
}

// TestPrintInputModeValidation checks that the mutual-exclusion logic on
// --input / --tournament-data is enforced without requiring soffice.
func TestPrintInputModeValidation(t *testing.T) {
	dir := t.TempDir()

	tests := []struct {
		name        string
		args        []string
		wantErrFrag string
	}{
		{
			name:        "neither flag set",
			args:        []string{"--type=names"},
			wantErrFrag: "provide exactly one of --input",
		},
		{
			name:        "both flags set",
			args:        []string{"--type=names", "--input=" + dir, "--tournament-data=" + dir},
			wantErrFrag: "mutually exclusive",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := runPrintCmd(t, tc.args...)
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tc.wantErrFrag),
				"expected error to contain %q, got: %v", tc.wantErrFrag, err)
		})
	}
}

// setupTestTournamentData builds a minimal tournament-data directory with one
// started competition and returns the data dir path.
func setupTestTournamentData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	store, err := state.NewStore(dir)
	require.NoError(t, err)

	// League format: this fixture tests Print/PDF store construction, not
	// mixed-knockout semantics. League produces a single pool (mixed would
	// need ≥2 pools by invariant).
	comp := &state.Competition{
		ID:           "test-comp",
		Name:         "Test Competition",
		Kind:         "individual",
		Format:       state.CompFormatLeague,
		PoolSize:     3,
		PoolSizeMode: "min",
		PoolWinners:  2,
		RoundRobin:   true,
		Courts:       []string{"A"},
		StartTime:    "09:00",
		Status:       "setup",
	}
	require.NoError(t, store.SaveCompetition(comp))

	players := []domain.Player{
		{Name: "Alice", Dojo: "DojoA"},
		{Name: "Bob", Dojo: "DojoB"},
		{Name: "Carol", Dojo: "DojoC"},
		{Name: "Dave", Dojo: "DojoD"},
	}
	require.NoError(t, store.SaveParticipants("test-comp", players))

	eng := engine.New(store)
	require.NoError(t, eng.StartCompetition("test-comp"))

	return dir
}

// TestPrintTournamentDataStoreConstruction verifies that --tournament-data builds
// the store+engine correctly and reaches the PDF-generation step (soffice gate).
// When soffice is absent the test is skipped rather than failed.
func TestPrintTournamentDataStoreConstruction(t *testing.T) {
	// Check soffice availability first.
	_, err := pdf.NewGenerator()
	if err != nil {
		t.Skipf("skipping: LibreOffice not available (%v)", err)
	}

	dataDir := setupTestTournamentData(t)
	outDir := t.TempDir()

	err = runPrintCmd(t,
		"--type=all",
		"--tournament-data="+dataDir,
		"--output-dir="+outDir,
	)
	require.NoError(t, err)
}
