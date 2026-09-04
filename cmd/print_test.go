package cmd

import (
	"bytes"
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
	// need >=2 pools by invariant).
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

// TestPrintAllSkippedExitsCleanly covers a tournament-data directory whose
// only competition is Swiss (no static bracket to export -- Swiss export is
// not yet implemented). ExportTournamentWorkbooks skips it, leaving
// len(sources)==0 with err==nil; before the fix o.generatePDFs ran anyway
// and failed with pdf's "no source workbooks provided", a confusing error
// even though the warning above already explained the omission correctly.
// The command must instead print an informative line to stderr and exit
// nil: nothing failed, every competition was legitimately unexportable.
// Requires soffice: pdf.NewGenerator() is called after the export/skip step
// in run(), so soffice must be present to reach it.
func TestPrintAllSkippedExitsCleanly(t *testing.T) {
	if _, err := pdf.NewGenerator(); err != nil {
		t.Skipf("skipping: LibreOffice not available (%v)", err)
	}

	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          "swiss-only",
		Name:        "Swiss Only",
		Format:      state.CompFormatSwiss,
		SwissRounds: 2,
		Status:      "setup",
	}))

	cmd := newPrintCmd()
	stderr := &bytes.Buffer{}
	stdout := &bytes.Buffer{}
	cmd.SetErr(stderr)
	cmd.SetOut(stdout)
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--type=all",
		"--tournament-data=" + dir,
		"--output-dir=" + t.TempDir(),
	})

	err = cmd.Execute()
	require.NoError(t, err, "an all-skipped tournament must exit cleanly, not error; stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "no exportable competitions found")
	assert.Contains(t, stderr.String(), "swiss-only")
	assert.NotContains(t, stderr.String(), "no source workbooks provided",
		"must not leak the internal pdf-generator error string")
}

// TestPrintAllSkippedFlagNotSet pins today's default behaviour explicitly:
// when --fail-on-skip-all is not set (the zero value), an all-skipped
// tournament-data directory returns nil, with the per-competition warning
// still printed to stderr. The flag must never change this default. Does not
// require soffice: len(sources)==0 returns before pdf.NewGenerator() is
// reached in run().
func TestPrintAllSkippedFlagNotSet(t *testing.T) {
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          "swiss-only",
		Name:        "Swiss Only",
		Format:      state.CompFormatSwiss,
		SwissRounds: 2,
		Status:      "setup",
	}))

	cmd := newPrintCmd()
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--type=all",
		"--tournament-data=" + dir,
		"--output-dir=" + t.TempDir(),
	})

	err = cmd.Execute()
	require.NoError(t, err, "flag not set: all-skipped must still exit cleanly; stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "no exportable competitions found")
	assert.Contains(t, stderr.String(), "swiss-only")
}

// TestPrintAllSkippedWithFailOnSkipAll covers the opt-in: with
// --fail-on-skip-all set, an all-skipped tournament-data directory must
// return a non-nil error naming the skip count, while still printing the
// per-competition warnings to stderr -- the flag changes only the exit
// status, never the reporting. Does not require soffice, for the same reason
// as TestPrintAllSkippedFlagNotSet.
func TestPrintAllSkippedWithFailOnSkipAll(t *testing.T) {
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          "swiss-only",
		Name:        "Swiss Only",
		Format:      state.CompFormatSwiss,
		SwissRounds: 2,
		Status:      "setup",
	}))

	cmd := newPrintCmd()
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--type=all",
		"--tournament-data=" + dir,
		"--output-dir=" + t.TempDir(),
		"--fail-on-skip-all",
	})

	err = cmd.Execute()
	require.Error(t, err, "flag set: all-skipped must return an error; stderr=%s", stderr.String())
	assert.Contains(t, err.Error(), "skipped")
	assert.Contains(t, stderr.String(), "no exportable competitions found",
		"the flag changes only the exit status, never the reporting")
	assert.Contains(t, stderr.String(), "swiss-only")
}

// TestPrintPartialSkipWithFailOnSkipAll is the case most likely to be got
// wrong: --fail-on-skip-all must NOT trip when only SOME competitions were
// skipped, because a booklet was still produced from the rest. Combines a
// Swiss competition (always skipped -- no static bracket to export) with a
// League competition (exportable) in the same tournament-data directory.
// Requires soffice to reach full PDF generation, since sources is non-empty
// and run() proceeds past the early return.
func TestPrintPartialSkipWithFailOnSkipAll(t *testing.T) {
	if _, err := pdf.NewGenerator(); err != nil {
		t.Skipf("skipping: LibreOffice not available (%v)", err)
	}

	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:          "swiss-only",
		Name:        "Swiss Only",
		Format:      state.CompFormatSwiss,
		SwissRounds: 2,
		Status:      "setup",
	}))

	comp := &state.Competition{
		ID:           "league-comp",
		Name:         "League Competition",
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
	require.NoError(t, store.SaveParticipants("league-comp", players))
	eng := engine.New(store)
	require.NoError(t, eng.StartCompetition("league-comp"))

	cmd := newPrintCmd()
	stderr := &bytes.Buffer{}
	cmd.SetErr(stderr)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SilenceErrors = true
	cmd.SilenceUsage = true
	cmd.SetArgs([]string{
		"--type=all",
		"--tournament-data=" + dir,
		"--output-dir=" + t.TempDir(),
		"--fail-on-skip-all",
	})

	err = cmd.Execute()
	require.NoError(t, err, "partial skip must still succeed even with --fail-on-skip-all; stderr=%s", stderr.String())
	assert.Contains(t, stderr.String(), "swiss-only", "the skipped competition is still warned about")
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

// TestGeneratePDFsValidation exercises the early-exit validation branches of
// generatePDFs without requiring LibreOffice. A nil generator is safe here
// because all tested branches return before any gen.* call.
func TestGeneratePDFsValidation(t *testing.T) {
	tmpDir := t.TempDir()
	tests := []struct {
		name        string
		opts        printOptions
		wantErrFrag string
	}{
		{
			name:        "all type with --output set",
			opts:        printOptions{pdfType: "all", output: filepath.Join(tmpDir, "out.pdf"), outputDir: ""},
			wantErrFrag: "--output is not valid with --type=all",
		},
		{
			name:        "all type without --output-dir",
			opts:        printOptions{pdfType: "all"},
			wantErrFrag: "--type=all requires --output-dir",
		},
		{
			name:        "single type without any output flag",
			opts:        printOptions{pdfType: "names"},
			wantErrFrag: "provide --output",
		},
		{
			name:        "single type with both output flags",
			opts:        printOptions{pdfType: "names", output: filepath.Join(tmpDir, "out.pdf"), outputDir: tmpDir},
			wantErrFrag: "--output and --output-dir are mutually exclusive",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newPrintCmd()
			err := tc.opts.generatePDFs(cmd, nil, nil, "test-label")
			require.Error(t, err)
			assert.True(t, strings.Contains(err.Error(), tc.wantErrFrag),
				"expected error to contain %q, got: %v", tc.wantErrFrag, err)
		})
	}
}

// TestPrintUnknownType checks that an unrecognised --type is rejected before
// any soffice call (exercises the run() validation branch).
func TestPrintUnknownType(t *testing.T) {
	dir := t.TempDir()
	// write a dummy xlsx so collectWorkbooks doesn't fail first
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.xlsx"), []byte("x"), 0o644))
	err := runPrintCmd(t, "--type=bogus", "--input="+dir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown --type")
}

// TestPrintSofficeNotFound checks the ErrSofficeNotFound branch in run()
// by pointing LIBREOFFICE_PATH at a non-existent binary. LocateSoffice only
// treats LIBREOFFICE_PATH as authoritative when it resolves to an executable;
// when it doesn't, the test skips if LibreOffice is still found via $PATH.
func TestPrintSofficeNotFound(t *testing.T) {
	t.Setenv("LIBREOFFICE_PATH", "/nonexistent-soffice-binary-abc123")

	// Ensure soffice is really absent on this run.
	if _, err := pdf.NewGenerator(); err == nil {
		t.Skip("LibreOffice found on PATH despite LIBREOFFICE_PATH override; skipping")
	}

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "a.xlsx"), []byte("x"), 0o644))
	err := runPrintCmd(t, "--type=names", "--input="+dir, "--output-dir="+t.TempDir())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LibreOffice")
}

// findExampleXLSXForCmd returns an example XLSX from the repo root, skipping
// if none is found. The cmd package is one directory below the repo root.
func findExampleXLSXForCmd(t *testing.T) string {
	t.Helper()
	candidate := filepath.Join("..", "playoffs-example-medium.xlsx")
	if _, err := os.Stat(candidate); err != nil {
		t.Skipf("example workbook not found at %s: %v", candidate, err)
	}
	abs, err := filepath.Abs(candidate)
	require.NoError(t, err)
	return abs
}

// TestPrintInputDirSingleType exercises the --input + single-type path through
// generatePDFs (including the output rename at the end). Requires soffice.
func TestPrintInputDirSingleType(t *testing.T) {
	if _, err := pdf.NewGenerator(); err != nil {
		t.Skipf("skipping: LibreOffice not available (%v)", err)
	}
	xlsx := findExampleXLSXForCmd(t)
	dir := t.TempDir()
	// Copy the example xlsx into a temp dir so collectWorkbooks finds it.
	xlsxDst := filepath.Join(dir, "example.xlsx")
	data, err := os.ReadFile(xlsx) // #nosec G304, test-only read of a repo asset
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(xlsxDst, data, 0o644))

	outDir := t.TempDir()

	err = runPrintCmd(t,
		"--type=registration",
		"--input="+dir,
		"--output-dir="+outDir,
	)
	require.NoError(t, err)
}

// TestPrintInputDirSingleTypeWithOutput exercises the --output rename path in
// generatePDFs (when --output is given instead of --output-dir for a single type).
func TestPrintInputDirSingleTypeWithOutput(t *testing.T) {
	if _, err := pdf.NewGenerator(); err != nil {
		t.Skipf("skipping: LibreOffice not available (%v)", err)
	}
	xlsx := findExampleXLSXForCmd(t)
	dir := t.TempDir()
	xlsxDst := filepath.Join(dir, "example.xlsx")
	data, err := os.ReadFile(xlsx) // #nosec G304, test-only read of a repo asset
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(xlsxDst, data, 0o644))

	outFile := filepath.Join(t.TempDir(), "out.pdf")

	err = runPrintCmd(t,
		"--type=registration",
		"--input="+dir,
		"--output="+outFile,
	)
	require.NoError(t, err)
	_, statErr := os.Stat(outFile)
	assert.NoError(t, statErr, "output file should exist")
}
