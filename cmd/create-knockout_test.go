package cmd

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestKnockoutOptionsRun_Success(t *testing.T) {
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

	o := &knockoutOptions{
		filePath:   tmpInput.Name(),
		outputPath: tmpOutput.Name(),
		determined: true,
		courts:     2,
	}

	err = o.run(nil, nil)
	assert.NoError(t, err)
}

func TestKnockoutOptionsRun_WithSeeds(t *testing.T) {
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

	o := &knockoutOptions{
		filePath:   tmpInput.Name(),
		outputPath: tmpOutput.Name(),
		seedsPath:  tmpSeeds.Name(),
		determined: true,
		courts:     2,
	}

	err = o.run(nil, nil)
	assert.NoError(t, err)
}

func TestCreateKnockout_WithSeeds(t *testing.T) {

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	// Path relative to cmd/ directory
	seedsPath := filepath.Join("..", "tests", "fixtures", "winners.csv")

	o := &knockoutOptions{
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

	// Create knockout
	err := o.createKnockout(entries)

	// Ensure no error because seeds path is valid and names match
	assert.NoError(t, err)

	err = writer.Flush()
	assert.NoError(t, err)

	// Buffer should contain excel data
	assert.Greater(t, b.Len(), 0)
}

func TestCreateKnockout_MissingSeed(t *testing.T) {

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	seedsPath := filepath.Join("..", "tests", "fixtures", "winners.csv")

	o := &knockoutOptions{
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

	err := o.createKnockout(entries)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "seeded participant not found")
}

func TestCreateKnockout_InvalidSeedsFile(t *testing.T) {

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	o := &knockoutOptions{
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

	err := o.createKnockout(entries)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse seeds file")
}

func TestCreateKnockout_DuplicateEntries(t *testing.T) {
	var b bytes.Buffer
	o := &knockoutOptions{
		outputWriter: bufio.NewWriter(&b),
	}
	err := o.createKnockout([]string{"Alice", "Alice"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate participant entries")
}

func TestCreateKnockout_WithZekken(t *testing.T) {
	var b bytes.Buffer
	o := &knockoutOptions{
		outputWriter:   bufio.NewWriter(&b),
		withZekkenName: true,
		courts:         2,
	}
	err := o.createKnockout([]string{"Alice,Ali,D1", "Bob,Bobby,D2"})
	assert.NoError(t, err)
}

// TestKnockoutOptionsRun_EmptyFile verifies that an empty input file returns
// a "no entries found" error.
func TestKnockoutOptionsRun_EmptyFile(t *testing.T) {
	tmpInput, err := os.CreateTemp("", "empty-input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	tmpInput.Close()

	tmpOutput, err := os.CreateTemp("", "output-*.xlsx")
	require.NoError(t, err)
	defer os.Remove(tmpOutput.Name())
	tmpOutput.Close()

	o := &knockoutOptions{
		filePath:   tmpInput.Name(),
		outputPath: tmpOutput.Name(),
		courts:     2,
	}
	err = o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no entries")
}

// TestKnockoutOptionsRun_InvalidCourts verifies that an invalid court count
// (over the court cap) returns an error from ValidateCourts.
func TestKnockoutOptionsRun_InvalidCourts(t *testing.T) {
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

	o := &knockoutOptions{
		filePath:   tmpInput.Name(),
		outputPath: tmpOutput.Name(),
		courts:     27, // exceeds the court cap
	}
	err = o.run(nil, nil)
	assert.Error(t, err)
}

func TestKnockoutOptionsRun_InvalidOutputPath(t *testing.T) {
	tmpInput, err := os.CreateTemp("", "input-*.csv")
	require.NoError(t, err)
	defer os.Remove(tmpInput.Name())
	_, err = tmpInput.WriteString("Alice,DojoA\nBob,DojoB\nCarol,DojoC\nDave,DojoD\n")
	require.NoError(t, err)
	tmpInput.Close()

	o := &knockoutOptions{
		filePath:   tmpInput.Name(),
		outputPath: filepath.Join(t.TempDir(), "nonexistent", "output.xlsx"), // parent dir missing
		courts:     1,
	}
	err = o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open output file")
}

func TestKnockoutOptionsRun_FileNotFound(t *testing.T) {
	o := &knockoutOptions{
		filePath:   "/nonexistent/input.csv",
		outputPath: filepath.Join(t.TempDir(), "output.xlsx"),
		courts:     1,
	}
	err := o.run(nil, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read entries from file")
}
