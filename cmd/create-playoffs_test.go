package cmd

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
)

func TestPlayoffOptionsRun_Success(t *testing.T) {
	// Create a temporary input file
	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	_, err = tmpInput.WriteString("John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4\n")
	require.NoError(t, err)
	tmpInput.Close()

	// Create a temporary output file
	tmpOutput, err := os.CreateTemp("", "output-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpOutput.Name())
	tmpOutput.Close()

	o := &playoffOptions{
		filePath:   tmpInput.Name(),
		outputPath: tmpOutput.Name(),
		determined: true,
		courts:     2,
	}

	err = o.run(nil, nil)
	assert.NoError(t, err)
}

func TestPlayoffOptionsRun_WithSeeds(t *testing.T) {
	// Create a temporary input file
	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	_, err = tmpInput.WriteString("John Doe,Dojo1\nJane Smith,Dojo2\nAlice,Dojo3\nBob,Dojo4\n")
	require.NoError(t, err)
	tmpInput.Close()

	// Create a temporary seeds file
	tmpSeeds, err := os.CreateTemp("", "seeds-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpSeeds.Name())
	_, err = tmpSeeds.WriteString("Name,Rank\nJohn Doe,1\nJane Smith,2\n")
	require.NoError(t, err)
	tmpSeeds.Close()

	// Create a temporary output file
	tmpOutput, err := os.CreateTemp("", "output-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpOutput.Name())
	tmpOutput.Close()

	o := &playoffOptions{
		filePath:   tmpInput.Name(),
		outputPath: tmpOutput.Name(),
		seedsPath:  tmpSeeds.Name(),
		determined: true,
		courts:     2,
	}

	err = o.run(nil, nil)
	assert.NoError(t, err)
}

func TestCreatePlayoffs_WithSeeds(t *testing.T) {

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	// Path relative to cmd/ directory
	seedsPath := filepath.Join("..", "tests", "fixtures", "winners.csv")

	o := &playoffOptions{
		outputWriter:   writer,
		outputPath:     "dummy.xlsx",
		seedsPath:      seedsPath,
		withZekkenName: false,
	}

	entries := []string{
		"Jane Doe,Dojo1",
		"John Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
	}

	// Create playoffs
	err := o.createPlayoffs(entries)

	// Ensure no error because seeds path is valid and names match
	assert.NoError(t, err)

	err = writer.Flush()
	assert.NoError(t, err)

	// Buffer should contain excel data
	assert.Greater(t, b.Len(), 0)
}

// TestCreatePlayoffs_NumberPrefix_ByteIdenticalNumbering is the playoffs
// analogue of create-pools_test.go's TestCreatePools_NumberPrefix_ByteIdenticalNumbering
// (bc-pnum D1): an explicit --number-prefix numbers byte-identically to how
// it always has, prefix plus one counter running straight through the
// participant order. Mutation: gating helper.AssignPlayerNumbers on a
// non-empty prefix would be a no-op here (the default is never empty), so
// this pins the wiring itself, not just the composition helper.
func TestCreatePlayoffs_NumberPrefix_ByteIdenticalNumbering(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)
	o := &playoffOptions{
		outputWriter: writer,
		outputPath:   "prefix.xlsx",
		determined:   true, // no shuffle: roster order must match expected numbering
		numberPrefix: "K",
	}
	entries := []string{
		"Alice,DojoA",
		"Bob,DojoB",
		"Carol,DojoC",
		"Dave,DojoD",
	}
	require.NoError(t, o.createPlayoffs(entries))
	require.NoError(t, writer.Flush())

	f, err := excelize.OpenReader(bytes.NewReader(b.Bytes()))
	require.NoError(t, err)
	defer func() { assert.NoError(t, f.Close()) }()

	rows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)
	var got []string
	for i, row := range rows {
		if i < 2 || len(row) < 4 { // rows 1-2 are the title/header block
			continue
		}
		got = append(got, row[3]) // column D: Player Number (non-sanitized layout)
	}
	assert.Equal(t, []string{"K1", "K2", "K3", "K4"}, got,
		"an explicit --number-prefix must number straight through the roster with no gap, duplicate or reordering")
}

// TestCreatePlayoffs_NumberPrefix_OverLongExplicit_Errors pins bc-pnum A10:
// an over-long explicit --number-prefix must be refused, not accepted
// verbatim.
func TestCreatePlayoffs_NumberPrefix_OverLongExplicit_Errors(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)
	o := &playoffOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		numberPrefix: "SENIORS1",
	}
	err := o.createPlayoffs([]string{"Alice,DojoA", "Bob,DojoB"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "SENIORS1")
}

// TestCreatePlayoffs_TitlePrefixDerivation pins bc-pnum A10/D2: with no
// explicit --number-prefix, the CLI derives one from --title-prefix through
// the shared resolveNumberPrefix. "Senior Men" has two words (initials "SM"),
// but with nothing else taken the initials loop returns the bare first
// initial "S" immediately -- the shortest non-taken candidate, not the full
// initials. Mutation: replacing o.titlePrefix with "" here must go red (the
// fallback "K" would print instead).
func TestCreatePlayoffs_TitlePrefixDerivation(t *testing.T) {
	var b bytes.Buffer
	writer := bufio.NewWriter(&b)
	o := &playoffOptions{
		outputWriter: writer,
		outputPath:   "titleprefix.xlsx",
		determined:   true,
		titlePrefix:  "Senior Men",
	}
	entries := []string{
		"Alice,DojoA",
		"Bob,DojoB",
		"Carol,DojoC",
		"Dave,DojoD",
	}
	require.NoError(t, o.createPlayoffs(entries))
	require.NoError(t, writer.Flush())

	f, err := excelize.OpenReader(bytes.NewReader(b.Bytes()))
	require.NoError(t, err)
	defer func() { assert.NoError(t, f.Close()) }()

	rows, err := f.GetRows(helper.SheetData)
	require.NoError(t, err)
	var got []string
	for i, row := range rows {
		if i < 2 || len(row) < 4 {
			continue
		}
		got = append(got, row[3])
	}
	assert.Equal(t, []string{"S1", "S2", "S3", "S4"}, got,
		"derivation from --title-prefix 'Senior Men' must give S1.., not SM1..")
}

func TestCreatePlayoffs_MissingSeed(t *testing.T) {

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	seedsPath := filepath.Join("..", "tests", "fixtures", "winners.csv")

	o := &playoffOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		seedsPath:    seedsPath,
	}

	// Jane Doe exists but John Smith doesn't - should fail with seed error
	entries := []string{
		"Jane Doe,Dojo1",
		"Alice,Dojo3",
		"Bob,Dojo4",
	}

	err := o.createPlayoffs(entries)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "seeded participant not found")
}

func TestCreatePlayoffs_InvalidSeedsFile(t *testing.T) {

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &playoffOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		seedsPath:    filepath.Join("..", "tests", "fixtures", "missing.csv"),
	}

	entries := []string{
		"Jane Doe,Dojo1",
		"John Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
	}

	err := o.createPlayoffs(entries)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse seeds file")
}

func TestCreatePlayoffs_DuplicateEntries(t *testing.T) {
	var b bytes.Buffer
	o := &playoffOptions{
		outputWriter: bufio.NewWriter(&b),
	}
	err := o.createPlayoffs([]string{"Alice", "Alice"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate participant entries")
}

func TestCreatePlayoffs_WithZekken(t *testing.T) {
	var b bytes.Buffer
	o := &playoffOptions{
		outputWriter:   bufio.NewWriter(&b),
		withZekkenName: true,
		courts:         2,
	}
	err := o.createPlayoffs([]string{"Alice,Ali,D1", "Bob,Bobby,D2"})
	assert.NoError(t, err)
}

// TestPlayoffOptionsRun_EmptyFile verifies that an empty input file returns
// a "no entries found" error.
func TestPlayoffOptionsRun_EmptyFile(t *testing.T) {
	tmpInput, err := os.CreateTemp("", "empty-input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	tmpInput.Close()

	tmpOutput, err := os.CreateTemp("", "output-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpOutput.Name())
	tmpOutput.Close()

	o := &playoffOptions{
		filePath:   tmpInput.Name(),
		outputPath: tmpOutput.Name(),
		courts:     2,
	}
	err = o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no entries")
}

// TestPlayoffOptionsRun_InvalidCourts verifies that an invalid court count
// (over the court cap) returns an error from ValidateCourts.
func TestPlayoffOptionsRun_InvalidCourts(t *testing.T) {
	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	_, err = tmpInput.WriteString("Alice,Dojo1\nBob,Dojo2\n")
	require.NoError(t, err)
	tmpInput.Close()

	tmpOutput, err := os.CreateTemp("", "output-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpOutput.Name())
	tmpOutput.Close()

	o := &playoffOptions{
		filePath:   tmpInput.Name(),
		outputPath: tmpOutput.Name(),
		courts:     27, // exceeds the court cap
	}
	err = o.run(nil, nil)
	assert.Error(t, err)
}

func TestPlayoffOptionsRun_InvalidOutputPath(t *testing.T) {
	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	_, err = tmpInput.WriteString("Alice,DojoA\nBob,DojoB\nCarol,DojoC\nDave,DojoD\n")
	require.NoError(t, err)
	tmpInput.Close()

	o := &playoffOptions{
		filePath:   tmpInput.Name(),
		outputPath: filepath.Join(t.TempDir(), "nonexistent", "output.xlsx"), // parent dir missing
		courts:     1,
	}
	err = o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open output file")
}

func TestPlayoffOptionsRun_FileNotFound(t *testing.T) {
	o := &playoffOptions{
		filePath:   "/nonexistent/input.csv",
		outputPath: filepath.Join(t.TempDir(), "output.xlsx"),
		courts:     1,
	}
	err := o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read entries from file")
}
