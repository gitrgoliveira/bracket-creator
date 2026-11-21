package engine

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeLineupsUnreadable puts a directory where the competition's lineups.yaml
// should be, so every read of it fails (even as root), whatever the cache held.
func makeLineupsUnreadable(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	path := filepath.Join(store.GetFolder(), "competitions", compID, "lineups.yaml")
	require.NoError(t, os.RemoveAll(path))
	require.NoError(t, os.Mkdir(path, 0o755))
}

// lineups.yaml that cannot be read does not fail the roster read (operator
// decision L1, "Warn, as for the team-members file"): it answers 200 with both
// sides present, nobody remaining, nobody exhausted and the lineups warning,
// logs an ERROR naming the file, and still names each side's fighter on. Both
// teams have a saved lineup, so a lineup exists that the read could not see.
func TestKachinukiRoster_UnreadableLineupsAnswerBothSidesWithTheWarning(t *testing.T) {
	for _, bracket := range []bool{false, true} {
		t.Run(branchName(bracket), func(t *testing.T) {
			eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-lineups-unread", 3,
				func(c *state.Competition) { c.Format = state.CompFormatMixed })
			rt := seedRosterTeams(t, store, comp.ID, 3,
				[]string{"A1", "A2", "A3"}, []string{"B1", "B2", "B3"}, true, true)
			// Bout 1 is being fought: the running autosave's winner is no result.
			matchID := saveRosterMatch(t, store, comp.ID, rt,
				[]state.SubMatchResult{rosterBout(1, rt.a[0], rt.b[0], &rt.a[0], false)}, bracket)
			makeLineupsUnreadable(t, store, comp.ID)

			var got KachinukiRoster
			var err error
			logs := captureLog(t, func() { got, err = eng.KachinukiRoster(comp.ID, matchID) })
			require.NoError(t, err, "an unreadable lineups.yaml does not fail the read")
			assert.Equal(t, kachinukiLineupsUnreadWarning, got.Warning)
			assert.Contains(t, logs, "engine: ERROR: engine.KachinukiRoster compId="+comp.ID+": lineups.yaml load error:")
			for name, side := range map[string]KachinukiRosterSide{"sideA": got.SideA, "sideB": got.SideB} {
				assert.False(t, side.LineupFound, name+": a lineup may exist that was not read, so the side fields nobody")
				assert.NotNil(t, side.Remaining, name+": the wire carries [] rather than null")
				assert.Empty(t, side.Remaining, name+": nobody remaining is listed")
				assert.False(t, side.Exhausted, name+": never exhausted under a warning")
			}
			assert.Equal(t, rt.a[0].ID, onID(got.SideA), "the live bout row still names the fighters on")
			assert.Equal(t, rt.b[0].ID, onID(got.SideB))

			// nextPairing is the advance's own decision, whatever the warning:
			// the read says what Record bout then does.
			appended, post, err := eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			assert.Equal(t, got.NextPairing, appended, "the read said what Record bout does")
			require.NotNil(t, post, "Record bout marks the bout recorded")
			require.Len(t, post.BoutLog, 1, "and appends nothing")
			assert.True(t, post.BoutLog[0].Recorded)

			// Read again over the recorded win: the loser is off, the winner
			// stays on, and still nobody is exhausted and nothing is left.
			got, err = eng.KachinukiRoster(comp.ID, matchID)
			require.NoError(t, err)
			assert.Equal(t, kachinukiLineupsUnreadWarning, got.Warning)
			assert.Equal(t, rt.a[0].ID, onID(got.SideA), "the winner stays on")
			assert.Nil(t, got.SideB.On, "the recorded win took the loser off")
			assert.False(t, got.SideA.Exhausted)
			assert.False(t, got.SideB.Exhausted, "the empty queue says nothing about who the side has left")
			assert.Empty(t, got.SideB.Remaining)
			appended, post, err = eng.MaybeAdvanceKachinuki(comp.ID, matchID)
			require.NoError(t, err)
			assert.Equal(t, got.NextPairing, appended)
			assert.Nil(t, post, "a second tap writes nothing")
		})
	}
}

// Only one warning is answered at a time: with the lineups unread nothing is
// filled by number, so the team members are never read and their file, broken
// as well, is neither warned of nor logged.
func TestKachinukiRoster_UnreadableLineupsWarnOfTheLineupsAlone(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kachinuki-roster-both-unread", 3,
		func(c *state.Competition) { c.Format = state.CompFormatMixed })
	aID, bID := numberedTeams(t, store, comp.ID)
	a := teamMemberIDs(t, store, comp.ID, aID, 3)
	b := teamMemberIDs(t, store, comp.ID, bID, 3)
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "Pool A-0", SideA: "Ryu", SideAID: aID, SideB: "Tora", SideBID: bID, Status: state.MatchStatusRunning,
		SubResults: []state.SubMatchResult{numberBout(1, a[0], b[0], "Ryu", a[0])},
	}}))
	makeLineupsUnreadable(t, store, comp.ID)
	makeTeamMembersUnreadable(t, store, comp.ID)

	var got KachinukiRoster
	var err error
	logs := captureLog(t, func() { got, err = eng.KachinukiRoster(comp.ID, "Pool A-0") })
	require.NoError(t, err)
	assert.Equal(t, kachinukiLineupsUnreadWarning, got.Warning)
	assert.Contains(t, logs, "lineups.yaml load error:")
	assert.NotContains(t, logs, "team-members.yaml", "the members are not read over unread lineups")
	assert.Equal(t, a[0], onID(got.SideA), "a fighter picked by number is still on")
	assert.Equal(t, b[0], onID(got.SideB))
	assert.Empty(t, got.SideA.Remaining)
	assert.Empty(t, got.SideB.Remaining)
}
