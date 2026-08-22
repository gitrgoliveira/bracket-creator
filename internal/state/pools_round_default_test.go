package state

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPoolMatches_LegacyCoreColumnsDefaultRound pins the documented
// Round: -1 "absent-column" default (parsePoolMatchesRecords,
// poolMatchColumns) against a row that is genuinely too short to reach the
// Round column at all: exactly poolMatchesLegacyCoreColumns (12) fields,
// the width of the original pre-append layout, well short of Round's index
// (15) in poolMatchColumns.
//
// The other legacy fixtures in this package (TestPoolMatchesReadsPreColumnFiles,
// TestPoolMatches_LegacyFileWithoutRepPlayers) both already carry an explicit
// Round cell (25 and 20 columns respectively, the latter literally writing
// "-1"), so neither exercises the reader ever having to fall back to the seed
// default. TestPoolMatches_LegacyFileWithoutIDs is 15 columns — short of
// Round too — but does not assert on Round at all. This test closes that
// gap: it is the one fixture in the suite that is BOTH short enough to skip
// the Round column AND asserts the value the reader is documented to
// produce when it does.
//
// result.Round = stored.Round on the next score write means a wrong default
// here does not just mis-report on load — it gets written back out on the
// very first match result recorded against this row.
func TestPoolMatches_LegacyCoreColumnsDefaultRound(t *testing.T) {
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))

	// Exactly the legacy core: PoolName,MatchIdx,SideA,SideB,Winner,IpponsA,
	// IpponsB,HansokuA,HansokuB,Decision,Status,Court — 12 columns, no header
	// row at all (the reader does not require one; it only skips a first
	// record whose first cell matches the current header's first column
	// name).
	legacy := "Pool A,0,Kyoto,Osaka,Kyoto,M,K,0,0,,completed,B\n"
	require.NoError(t, s.atomicWrite(s.compPath("c", "pool-matches.csv"), []byte(legacy), 0600))

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadPoolMatches("c")
	require.NoError(t, err)
	require.Len(t, loaded, 1)
	assert.Equal(t, "Kyoto", loaded[0].Winner, "the legacy-core columns still parse")
	assert.Equal(t, -1, loaded[0].Round,
		"a row too short to reach the Round column must load the documented "+
			"absent-column sentinel, not the zero value")
}
