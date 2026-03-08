package cmd

import (
	"bufio"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCreatePlayoffs_WithSeeds(t *testing.T) {
	err := os.Chdir("..")
	assert.NoError(t, err)
	defer os.Chdir("cmd")

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	// Note: requires running from cmd/ or correctly resolving paths
	seedsPath := filepath.Join("tests", "fixtures", "winners.csv")

	o := &playoffOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		seedsPath:    seedsPath,
		sanitize:     false,
	}

	entries := []string{
		"Jane Doe,Dojo1",
		"John Smith,Dojo2",
		"Alice,Dojo3",
		"Bob,Dojo4",
	}

	// Create playoffs
	err = o.createPlayoffs(entries)

	// Ensure no error because seeds path is valid and names match
	assert.NoError(t, err)

	err = writer.Flush()
	assert.NoError(t, err)

	// Buffer should contain excel data
	assert.Greater(t, b.Len(), 0)
}

func TestCreatePlayoffs_MissingSeed(t *testing.T) {
	err := os.Chdir("..")
	assert.NoError(t, err)
	defer os.Chdir("cmd")

	var b bytes.Buffer
	writer := bufio.NewWriter(&b)

	seedsPath := filepath.Join("tests", "fixtures", "winners.csv")

	o := &playoffOptions{
		outputWriter: writer,
		outputPath:   "dummy.xlsx",
		seedsPath:    seedsPath,
	}

	// Jane Doe exists but John Smith doesn't - should warn but not fail
	entries := []string{
		"Jane Doe,Dojo1",
		"Alice,Dojo3",
		"Bob,Dojo4",
	}

	err = o.createPlayoffs(entries)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "seeded participant not found")
}
