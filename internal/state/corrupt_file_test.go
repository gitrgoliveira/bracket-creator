package state

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The storage is shaped for an organiser to read and repair, which only works
// if a wrong edit says WHAT is wrong and WHERE. These pin the located reporting
// for the loud class: damage that fails the load outright.

func TestOffsetToLineColumn(t *testing.T) {
	raw := []byte("one\ntwo\nthree")
	tests := []struct {
		name      string
		offset    int64
		line, col int
	}{
		{"start of file", 0, 1, 1},
		{"within the first line", 2, 1, 3},
		{"immediately after a newline", 4, 2, 1},
		{"on the third line", 10, 3, 3},
		{"past the end resolves to the last position, as a truncated document reports", 999, 3, 6},
		{"a negative offset reports no position rather than guessing", -1, 0, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			line, col := offsetToLineColumn(raw, tt.offset)
			assert.Equal(t, tt.line, line)
			assert.Equal(t, tt.col, col)
		})
	}
}

func TestLoadBracketReportsWhereTheFileIsBroken(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))

	// A hand edit that breaks the JSON on a known line.
	broken := "{\n  \"rounds\": [\n    [{\"id\": \"R1-1\"x}]\n  ]\n}\n"
	require.NoError(t, s.atomicWrite(s.compPath("c", "bracket.json"), []byte(broken), 0600))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	_, err = fresh.LoadBracket("c")
	require.Error(t, err, "a corrupt bracket must fail the load, not degrade")

	cf, ok := AsCorruptFile(err)
	require.True(t, ok, "and must arrive as a located CorruptFileError, got %T", err)
	assert.Equal(t, "bracket.json", cf.File)
	assert.Equal(t, 3, cf.Line, "the line the operator has to open")
	assert.Positive(t, cf.Column)
	assert.Contains(t, cf.Error(), "line 3")
	assert.NotEmpty(t, cf.Detail)
}

func TestLoadBracketLeavesTheBrokenFileAlone(t *testing.T) {
	// The whole reason a hand repair is the right advice: nothing rewrites the
	// file while it will not parse, so the operator's bytes are still there.
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))
	path := s.compPath("c", "bracket.json")
	broken := "{\n  \"rounds\": [[{\"id\": \"R1-1\"x}]]\n}\n"
	require.NoError(t, s.atomicWrite(path, []byte(broken), 0600))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	_, loadErr := fresh.LoadBracket("c")
	require.Error(t, loadErr)

	// Every bracket write path aborts on the parse before saving.
	found, err := fresh.UpdateBracketMatchByID("c", "R1-1", func(m *BracketMatch) { m.Winner = "anyone" })
	assert.False(t, found)
	require.Error(t, err)
	_, ok := AsCorruptFile(err)
	assert.True(t, ok, "the write refuses with the same located reason")

	after, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	assert.Equal(t, broken, string(after), "the operator's bytes are untouched")
}

func TestLoadPoolMatchesReportsWhereTheRowStructureBroke(t *testing.T) {
	// CSV-level damage is a different class from a malformed CELL: there is no
	// row left to degrade, so the load fails and must say where.
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))

	// An unclosed quoted field: the shape a half-finished hand edit leaves.
	broken := "PoolName,MatchIdx,SideA\nPool A,1,\"Kyoto\nPool A,2,Osaka,extra\n"
	require.NoError(t, s.atomicWrite(s.compPath("c", "pool-matches.csv"), []byte(broken), 0600))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	_, err = fresh.LoadPoolMatches("c")
	require.Error(t, err)
	cf, ok := AsCorruptFile(err)
	require.True(t, ok, "got %T", err)
	assert.Equal(t, "pool-matches.csv", cf.File)
	assert.Positive(t, cf.Line)
	assert.NotEmpty(t, cf.Detail)
}
