package state

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A team encounter's five bouts live in ONE cell of pool-matches.csv, and the
// file is the one the tool invites an organiser to hand-edit. These guards
// cover what happens when that edit is wrong: the cell must degrade to an empty
// encounter (the documented contract), must SAY SO, and must not be destroyed
// by the whole-file rewrite that every subsequent match write performs.

// corruptCompetition writes a two-match competition where the first match holds
// a team encounter and returns the on-disk path of pool-matches.csv.
func corruptCompetition(t *testing.T) (*Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := NewStore(dir)
	require.NoError(t, err)
	require.NoError(t, s.SaveCompetition(&Competition{ID: "c", Name: "C"}))

	team := MatchResult{ID: "Pool A-1", SideA: "Kenshikan", SideB: "Sanshukan", Winner: "Kenshikan", SubResults: []SubMatchResult{
		{Position: 1, SideA: "Tanaka", SideB: "Suzuki", IpponsA: []string{"M"}, Winner: "Tanaka"},
		{Position: 2, SideA: "Ito", SideB: "Sato", IpponsA: []string{"K", "M"}, Winner: "Ito"},
	}}
	other := MatchResult{ID: "Pool A-2", SideA: "Kenshikan", SideB: "Meirinkan"}
	require.NoError(t, s.SavePoolMatches("c", []MatchResult{team, other}))
	return s, dir, s.compPath("c", "pool-matches.csv")
}

// mangleCell rewrites the sub-bout JSON so it no longer parses while leaving the
// CSV quoting untouched, which is what a hand edit inside the cell looks like.
func mangleCell(t *testing.T, path, from, to string) {
	t.Helper()
	raw, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	mangled := strings.Replace(string(raw), from, to, 1)
	require.NotEqual(t, string(raw), mangled, "the mangle pattern must actually match")
	require.NoError(t, os.WriteFile(path, []byte(mangled), 0600))
}

// TestCorruptSubResultsSurvivesAWriteToAnotherMatch is the regression guard for
// bc-subj. Scoring an UNRELATED match rewrites the whole file, so before the
// fix the organiser's malformed cell came back as empty and the encounter's
// bouts were gone for good -- destroying the very bytes the documented remedy
// ("fix the cell, reload") depends on, seconds into a live tournament.
func TestCorruptSubResultsSurvivesAWriteToAnotherMatch(t *testing.T) {
	_, dir, path := corruptCompetition(t)
	mangleCell(t, path, `""position"":2`, `""position"":2x`)

	// Cold reload, as after a restart.
	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadPoolMatches("c")
	require.NoError(t, err, "a corrupt cell must never fail the load")
	require.Len(t, loaded, 2)
	assert.Empty(t, loaded[0].SubResults, "the encounter degrades to empty")
	assert.True(t, loaded[0].SubResultsUnreadable, "and says so")

	// Score a DIFFERENT match. This rewrites every row in the file.
	found, err := fresh.UpdatePoolMatchByID("c", "Pool A-2", func(m *MatchResult) {
		m.Status = MatchStatusCompleted
	})
	require.NoError(t, err)
	require.True(t, found)

	after, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(after), "Tanaka",
		"the malformed cell must survive a write to another match, or the organiser "+
			"has nothing left to repair")
	assert.Contains(t, string(after), `""position"":2x`,
		"and must survive VERBATIM, not partially rewritten")

	// It still reads back as an unreadable, empty encounter.
	again, err := NewStore(dir)
	require.NoError(t, err)
	reloaded, err := again.LoadPoolMatches("c")
	require.NoError(t, err)
	assert.Empty(t, reloaded[0].SubResults)
	assert.True(t, reloaded[0].SubResultsUnreadable, "the warning outlives the write too")
}

// TestCorruptSubResultsTypeErrorDoesNotNormaliseDamageOntoDisk covers the other
// half of "malformed". encoding/json rejects a SYNTAX error before decoding
// anything, but a TYPE error (valid JSON, wrong value type, e.g. quoting a
// number) decodes as far as it got and returns the error at the end, leaving a
// partially built slice whose failed entries are silently zeroed. Loading that
// slice would make the documented "loads as an empty encounter" contract false,
// and re-marshalling it would replace the organiser's cell with a
// valid-LOOKING one holding nameless bouts.
func TestCorruptSubResultsTypeErrorDoesNotNormaliseDamageOntoDisk(t *testing.T) {
	_, dir, path := corruptCompetition(t)
	mangleCell(t, path, `""position"":2,`, `""position"":""two"",`)

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	loaded, err := fresh.LoadPoolMatches("c")
	require.NoError(t, err)
	require.Len(t, loaded, 2)
	assert.Empty(t, loaded[0].SubResults,
		"a type error must degrade to EMPTY, not to a half-decoded encounter")
	assert.True(t, loaded[0].SubResultsUnreadable)

	found, err := fresh.UpdatePoolMatchByID("c", "Pool A-2", func(m *MatchResult) {
		m.Status = MatchStatusCompleted
	})
	require.NoError(t, err)
	require.True(t, found)

	after, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	assert.Contains(t, string(after), `""position"":""two""`,
		"the operator's bytes stay as they are")
	assert.Contains(t, string(after), "Tanaka",
		"including the bout that DID decode, which a re-marshal would have kept "+
			"while silently dropping its neighbour")
}

// TestRepairingTheCellClearsTheWarning: re-entering the encounter through a
// normal write is the repair path, and it is the only thing that takes the
// retained bytes and the warning down.
func TestRepairingTheCellClearsTheWarning(t *testing.T) {
	_, dir, path := corruptCompetition(t)
	mangleCell(t, path, `""position"":2`, `""position"":2x`)

	fresh, err := NewStore(dir)
	require.NoError(t, err)
	found, err := fresh.UpdatePoolMatchByID("c", "Pool A-1", func(m *MatchResult) {
		require.True(t, m.SubResultsUnreadable, "the mutate sees the flagged match")
		m.SubResults = []SubMatchResult{
			{Position: 1, SideA: "Tanaka", SideB: "Suzuki", IpponsA: []string{"M"}, Winner: "Tanaka"},
		}
		m.SubResultsRaw = ""
		m.SubResultsUnreadable = false
	})
	require.NoError(t, err)
	require.True(t, found)

	after, err := os.ReadFile(path) // #nosec G304
	require.NoError(t, err)
	assert.NotContains(t, string(after), "2x", "the malformed bytes are gone once repaired")

	again, err := NewStore(dir)
	require.NoError(t, err)
	reloaded, err := again.LoadPoolMatches("c")
	require.NoError(t, err)
	assert.Len(t, reloaded[0].SubResults, 1)
	assert.False(t, reloaded[0].SubResultsUnreadable, "and the warning is answered")
}
