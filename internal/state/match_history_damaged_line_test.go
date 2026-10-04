package state

import (
	"bytes"
	"log"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A damaged history line (a crash mid-append, a hand edit) is skipped AND
// logged, naming the match: the entries either side of it are still read, and
// the operator looking for a held change that is not in the History view has
// a line in the server log to find, rather than nothing.
func TestLoadMatchHistorySkipsAndLogsADamagedLine(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	require.NoError(t, store.SaveCompetition(&Competition{ID: "hist", Name: "Hist"}))
	require.NoError(t, store.AppendMatchHistory("hist", MatchHistoryEntry{MatchID: "Pool A-0", Door: "score", Stamp: 1}))
	f, err := os.OpenFile(store.matchHistoryPath("hist", "Pool A-0"), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	_, err = f.WriteString(`{"matchId":"Pool A-0","door":"sc` + "\n")
	require.NoError(t, err)
	require.NoError(t, f.Close())
	require.NoError(t, store.AppendMatchHistory("hist", MatchHistoryEntry{MatchID: "Pool A-0", Door: "score", Stamp: 2}))

	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(prev)
	entries, err := store.LoadMatchHistory("hist", "Pool A-0")
	require.NoError(t, err)
	require.Len(t, entries, 2, "the lines either side of the damaged one are read")
	assert.Equal(t, int64(1), entries[0].Stamp)
	assert.Equal(t, int64(2), entries[1].Stamp)
	assert.Contains(t, logged.String(), "match history hist/Pool A-0: a line could not be read and is skipped")
}

// A line past the scanner's 8 MiB buffer cap is a scanner error
// (bufio.ErrTooLong), not an unparsable line: unlike the damaged line above,
// which is skipped and logged while the entries either side of it are both
// read, the scanner cannot resume past this one, so every entry after it is
// silently missing from the read unless this is logged too. This documents
// that the read returns only the entry BEFORE the oversized line; the
// entry recorded after it is not recovered.
func TestLoadMatchHistoryLogsAScannerErrorAndStopsReading(t *testing.T) {
	store, cleanup := newTestStore(t)
	defer cleanup()
	require.NoError(t, store.SaveCompetition(&Competition{ID: "hist", Name: "Hist"}))
	require.NoError(t, store.AppendMatchHistory("hist", MatchHistoryEntry{MatchID: "Pool A-0", Door: "score", Stamp: 1}))

	f, err := os.OpenFile(store.matchHistoryPath("hist", "Pool A-0"), os.O_APPEND|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	oversized := append([]byte(`{"matchId":"Pool A-0","door":"score","session":"`), bytes.Repeat([]byte("x"), 8*1024*1024+1)...)
	oversized = append(oversized, '"', '}', '\n')
	_, err = f.Write(oversized)
	require.NoError(t, err)
	require.NoError(t, f.Close())

	// A good entry written after the oversized line, to document that the
	// scanner cannot continue past it: this third entry is NOT recovered by
	// the read below, even though it is well-formed on its own.
	require.NoError(t, store.AppendMatchHistory("hist", MatchHistoryEntry{MatchID: "Pool A-0", Door: "score", Stamp: 3}))

	var logged bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(prev)
	entries, err := store.LoadMatchHistory("hist", "Pool A-0")
	require.NoError(t, err)
	require.Len(t, entries, 1, "the scanner stops at the oversized line; the well-formed entry written after it is not read")
	assert.Equal(t, int64(1), entries[0].Stamp)
	assert.Contains(t, logged.String(), "match history hist/Pool A-0")
	assert.Contains(t, logged.String(), "the entries after it were not read")
}
