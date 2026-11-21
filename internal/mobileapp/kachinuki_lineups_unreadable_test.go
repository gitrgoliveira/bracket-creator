package mobileapp

import (
	"net/http"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// kachinukiLineupsUnreadSentence is the roster's warning when lineups.yaml
// cannot be read (operator decision L1). Spelled out here, not taken from the
// engine, so a change to what the operator reads is a change to this test.
const kachinukiLineupsUnreadSentence = "The lineups could not be read, so who fights next is not worked out. Add each bout's pairing by hand."

// When lineups.yaml cannot be read, a lineup may exist that was not read, so
// neither side fields anyone (L1): the roster read answers 200 with the lineups
// sentence, no side reads as having a lineup, nobody is queued and nobody reads
// as having no fighters left, and the live bout's fighters are still named as
// on. Record bout answers 200 and appends nothing.
func TestKachinukiRoster_LineupsUnreadableWarnsAndRecordBoutAppendsNothing(t *testing.T) {
	w := newNoLineupWorld(t, false)
	w.start()
	// Bout 1 as the seat leaves it, each team's member 1, with a point on it.
	w.pointOnBoutOne()
	w.requireBout(w.stored(), 1, 1, 1)
	logs := captureLog(t)
	w.corruptCompetitionFile("lineups.yaml")

	rec := w.send(http.MethodGet, w.path("/kachinuki-roster"), nil)
	require.Equal(t, http.StatusOK, rec.Code, "an unread file is no error: %s", rec.Body.String())
	assert.Contains(t, rec.Body.String(), `"remaining":[]`, "nobody is queued, and the list is present, never null")
	r := w.roster()
	assert.Equal(t, kachinukiLineupsUnreadSentence, r.Warning)
	for name, side := range map[string]noLineupRosterSide{"A": r.SideA, "B": r.SideB} {
		assert.False(t, side.LineupFound, "side %s fields nobody", name)
		assert.Empty(t, side.Remaining, "side %s queues nobody", name)
		assert.False(t, side.Exhausted, "side %s: never no fighters left under a warning", name)
	}
	require.NotNil(t, r.SideA.On, "the live bout's fighter is still on")
	assert.Equal(t, w.a[1], r.SideA.On.MemberID)
	require.NotNil(t, r.SideB.On, "the live bout's fighter is still on")
	assert.Equal(t, w.b[1], r.SideB.On.MemberID)
	assert.Contains(t, logs.String(), "ERROR")
	assert.Contains(t, logs.String(), "lineups.yaml", "the ERROR names the file")

	m := w.recordBout(true)
	require.Len(t, m.SubResults, 1, "no pairing is worked out, so nothing is appended")
	assert.Equal(t, []string{"M", "K"}, state.SubResultAt(m.SubResults, 1).IpponsA)
}
