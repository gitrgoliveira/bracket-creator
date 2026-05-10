package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOpenOutputFile_Error(t *testing.T) {
	// Try to open a file in a non-existent directory
	f, w, err := openOutputFile("/non/existent/dir/output.xlsx")
	assert.Error(t, err)
	assert.Nil(t, f)
	assert.Nil(t, w)
}

func TestProcessEntries_Success(t *testing.T) {
	entries := []string{"John Doe,Dojo1", "Jane Smith,Dojo2"}
	players, err := processEntries(entries, true, false)
	assert.NoError(t, err)
	assert.Len(t, players, 2)
	assert.Equal(t, "John Doe", players[0].Name)
}

func TestProcessEntries_Shuffle(t *testing.T) {
	entries := []string{"1,D1", "2,D2", "3,D3", "4,D4", "5,D5", "6,D6", "7,D7", "8,D8", "9,D9", "10,D10"}
	// This might flakes if shuffle result matches original, but with 10 it's unlikely
	players, err := processEntries(entries, false, false)
	assert.NoError(t, err)
	assert.Len(t, players, 10)
}

func TestProcessEntries_DuplicateError(t *testing.T) {
	entries := []string{"John Doe,Dojo1", "John Doe,Dojo1"}
	players, err := processEntries(entries, true, false)
	assert.Error(t, err)
	assert.Nil(t, players)
	assert.Contains(t, err.Error(), "duplicate participant entries found")
}
