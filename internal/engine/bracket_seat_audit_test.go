package engine

// A bracket door that gives a knockout side another team takes the
// representative pick the old team's member held on it (seatBracketSide). A
// downstream match the write REOPENS names the pick on its reopen line; every
// other match the write re-seats -- scheduled or requeued, the bronze, a round
// further on that a retraction unwinds into, a slot a requalification repaint
// paints -- used to get a log line and nothing in its history. These tests pin
// that each such match gets exactly one `reseat` line naming the group, from
// each door, and that a reopened match keeps ONLY its reopen line.

import (
	"slices"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	auHawk   = "Hawk"
	auHawkID = "44444444-4444-4444-8444-444444444444"
)

// auLines is the whole history of one match.
func auLines(t *testing.T, store *state.Store, compID, matchID string) []state.MatchHistoryEntry {
	t.Helper()
	entries, err := store.LoadMatchHistory(compID, matchID)
	require.NoError(t, err)
	return entries
}

// auDoor is the entries of lines written through door.
func auDoor(lines []state.MatchHistoryEntry, door string) []state.MatchHistoryEntry {
	var out []state.MatchHistoryEntry
	for _, l := range lines {
		if l.Door == door {
			out = append(out, l)
		}
	}
	return out
}

// auRow is a representative bout row naming both teams and a pick for each.
func auRow(nameA, nameB, pickA, pickB string) []state.SubMatchResult {
	return []state.SubMatchResult{{
		Position: state.DaihyosenSubPosition, SideA: nameA, SideB: nameB,
		SideAMemberID: pickA, SideBMemberID: pickB,
		Decision: string(domain.DecisionDaihyosen),
	}}
}

// auStamped dates a match's picks mmT1 and the match itself mmT2, as rpSetup
// does, so a pick a re-seat takes is dated after the stamp it held.
func auStamped(m state.BracketMatch) state.BracketMatch {
	m.ModifiedAt = mmT2
	m.GroupStamps = map[string]int64{
		state.GroupRepPickA:                         mmT1,
		state.GroupRepPickB:                         mmT1,
		state.BoutGroup(state.DaihyosenSubPosition): mmT1,
		state.GroupPoints:                           mmT2,
	}
	return m
}

func auSaveFourTeams(t *testing.T, store *state.Store, compID string) {
	t.Helper()
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "team", TeamSize: 3, Status: state.CompStatusKnockout,
	}))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: wrTeamAID, Name: wrTeamA, Dojo: "DojoR"},
		{ID: wrTeamBID, Name: wrTeamB, Dojo: "DojoT"},
		{ID: wrTeamCID, Name: wrTeamC, Dojo: "DojoK"},
		{ID: auHawkID, Name: auHawk, Dojo: "DojoH"},
	}))
}

// auSemifinals stores a four-team knockout with a 3rd-place match: Ryu beat
// Tora (m-r1-0) and Kuma beat Hawk (m-r1-1), so Ryu v Kuma is the final and
// Tora v Hawk the bronze match. Both hold a representative bout with a pick on
// each side.
func auSemifinals(t *testing.T) (*Engine, *state.Store, string) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	const compID = "au-bronze"
	auSaveFourTeams(t, store, compID)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{ID: "m-r1-0", MatchNumber: 1, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
					Status: state.MatchStatusCompleted, Winner: wrTeamA, WinnerID: wrTeamAID,
					SubResults: []state.SubMatchResult{wrBout1("M")}},
				{ID: "m-r1-1", MatchNumber: 2, SideA: wrTeamC, SideAID: wrTeamCID, SideB: auHawk, SideBID: auHawkID,
					Status: state.MatchStatusCompleted, Winner: wrTeamC, WinnerID: wrTeamCID},
			},
			{auStamped(state.BracketMatch{ID: "m-final", MatchNumber: 4,
				SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamC, SideBID: wrTeamCID,
				Status: state.MatchStatusScheduled, SubResults: auRow(wrTeamA, wrTeamC, "pick-ryu", "pick-kuma")})},
		},
		ThirdPlaceMatch: func() *state.BracketMatch {
			m := auStamped(state.BracketMatch{ID: "m-bronze", MatchNumber: 3,
				SideA: wrTeamB, SideAID: wrTeamBID, SideB: auHawk, SideBID: auHawkID,
				Status: state.MatchStatusScheduled, SubResults: auRow(wrTeamB, auHawk, "pick-tora", "pick-hawk")})
			return &m
		}(),
	}))
	return eng, store, compID
}

// A scheduled downstream match a correction re-seats gets one `reseat` line
// naming the side's group, dated with the stamp the re-seat gave it.
func TestReseatAudit_ScheduledDownstreamGetsAReseatLine(t *testing.T) {
	eng, store, compID := rpSetup(t, false)
	require.Empty(t, auLines(t, store, compID, rpNextID), "no history before the write")

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", rpCorrect(wrTeamB), ForceOptions{})
		return err
	}))

	next := rpNext(t, store, compID)
	a, _ := next.RepPicks()
	require.Empty(t, a, "the pick went with the team")
	lines := auLines(t, store, compID, rpNextID)
	require.Len(t, lines, 1, "exactly one line on the re-seated match")
	assert.Equal(t, doorReseat, lines[0].Door)
	assert.Equal(t, []string{state.GroupRepPickA}, lines[0].Changed, "side A only: side B was not re-seated")
	assert.Equal(t, state.HistoryOutcomeApplied, lines[0].Outcomes[state.GroupRepPickA])
	assert.Equal(t, next.GroupStamp(state.GroupRepPickA), lines[0].Stamp, "dated with the stamp the re-seat gave the group")
	assert.Empty(t, lines[0].Held, "nothing was held")
	assert.Empty(t, lines[0].Reason)
}

// A played downstream match the same write reopens keeps its reopen line as the
// record of the pick, and gets no second line.
func TestReseatAudit_PlayedDownstreamHasOnlyItsReopenLine(t *testing.T) {
	eng, store, compID := rpSetup(t, true)

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", rpCorrect(wrTeamB), ForceOptions{Force: true})
		return err
	}))

	lines := auLines(t, store, compID, rpNextID)
	require.Len(t, lines, 1, "the reopen line, and nothing else")
	assert.Equal(t, doorDownstreamReopen, lines[0].Door)
	assert.Contains(t, lines[0].Changed, state.GroupRepPickA, "the reopen line names the pick that went")
	assert.Empty(t, auDoor(lines, doorReseat))
}

// The 3rd-place match is re-seated by the same write as the final: each gets
// its own line, for the side the semifinal feeds.
func TestReseatAudit_TheBronzeAndTheFinalEachGetALine(t *testing.T) {
	eng, store, compID := auSemifinals(t)

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", rpCorrect(wrTeamB), ForceOptions{})
		return err
	}))

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	for _, id := range []string{"m-final", "m-bronze"} {
		m := b.MatchByID(id)
		require.NotNil(t, m, id)
		a, c := m.RepPicks()
		assert.Empty(t, a, "%s: side A was given another team", id)
		assert.NotEmpty(t, c, "%s: side B was not", id)

		lines := auLines(t, store, compID, id)
		require.Len(t, lines, 1, "%s: one line", id)
		assert.Equal(t, doorReseat, lines[0].Door, id)
		assert.Equal(t, []string{state.GroupRepPickA}, lines[0].Changed, id)
		assert.Equal(t, m.GroupStamp(state.GroupRepPickA), lines[0].Stamp, id)
	}
	assert.Empty(t, auLines(t, store, compID, "m-r1-1"), "the other semifinal was not written")
}

// A round further on that the retraction of a reopened match unwinds into is
// re-seated without being reopened: it gets a `reseat` line while the match
// reopened beside it keeps only its reopen line.
func TestReseatAudit_AMatchUnwoundBeyondTheReopenedOneGetsALine(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "au-chain"
	auSaveFourTeams(t, store, compID)
	played := auStamped(state.BracketMatch{ID: "m-r2-0", MatchNumber: 3,
		SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamC, SideBID: wrTeamCID,
		Status: state.MatchStatusCompleted, Winner: wrTeamA, WinnerID: wrTeamAID,
		SubResults: auRow(wrTeamA, wrTeamC, "pick-ryu", "pick-kuma")})
	final := auStamped(state.BracketMatch{ID: "m-r3-0", MatchNumber: 4,
		SideA: wrTeamA, SideAID: wrTeamAID, SideB: auHawk, SideBID: auHawkID,
		Status: state.MatchStatusScheduled, SubResults: auRow(wrTeamA, auHawk, "pick-ryu-final", "pick-hawk")})
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{ID: "m-r1-0", MatchNumber: 1, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
				Status: state.MatchStatusCompleted, Winner: wrTeamA, WinnerID: wrTeamAID,
				SubResults: []state.SubMatchResult{wrBout1("M")}},
			{ID: "m-r1-1", MatchNumber: 2, SideA: wrTeamC, SideAID: wrTeamCID,
				Status: state.MatchStatusCompleted, Winner: wrTeamC, WinnerID: wrTeamCID},
		},
		{played},
		{final},
	}}))

	var reopened []ReopenedMatch
	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", rpCorrect(wrTeamB),
			ForceOptions{Force: true, Reopened: &reopened})
		return err
	}))
	require.Equal(t, []string{"m-r2-0"}, reopenedIDs(reopened), "only the played match is reopened")

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	a, c := b.MatchByID("m-r3-0").RepPicks()
	require.Empty(t, a, "the slot went back to a placeholder, and the pick with it")
	require.Equal(t, "pick-hawk", c)

	finalLines := auLines(t, store, compID, "m-r3-0")
	require.Len(t, finalLines, 1)
	assert.Equal(t, doorReseat, finalLines[0].Door)
	assert.Equal(t, []string{state.GroupRepPickA}, finalLines[0].Changed)

	playedLines := auLines(t, store, compID, "m-r2-0")
	require.Len(t, playedLines, 1, "the reopened match keeps its reopen line alone")
	assert.Equal(t, doorDownstreamReopen, playedLines[0].Door)
	assert.Contains(t, playedLines[0].Changed, state.GroupRepPickA)
}

func TestReseatAudit_OverrideDoorRecordsTheLine(t *testing.T) {
	eng, store, compID := rpSetup(t, false)

	applied, err := eng.OverrideBracketWinner(compID, "m-r1-0", wrTeamB, 0)
	require.NoError(t, err)
	require.True(t, applied)

	next := rpNext(t, store, compID)
	lines := auLines(t, store, compID, rpNextID)
	require.Len(t, lines, 1)
	assert.Equal(t, doorReseat, lines[0].Door)
	assert.Equal(t, []string{state.GroupRepPickA}, lines[0].Changed)
	assert.Equal(t, next.GroupStamp(state.GroupRepPickA), lines[0].Stamp)
}

// The decision door reaches the bracket through the same primitive as a score,
// so a withdrawal that turns the winner re-seats the next round the same way.
func TestReseatAudit_DecisionDoorRecordsTheLine(t *testing.T) {
	eng, store, compID := rpSetup(t, false)

	_, _, err := eng.RecordDecision(compID, "m-r1-0", "kiken-voluntary", "aka", "illness", nil)
	require.NoError(t, err)

	next := rpNext(t, store, compID)
	require.Equal(t, wrTeamB, next.SideA, "Tora advanced in place of the team that withdrew")
	a, _ := next.RepPicks()
	require.Empty(t, a)
	lines := auLines(t, store, compID, rpNextID)
	require.Len(t, lines, 1)
	assert.Equal(t, doorReseat, lines[0].Door)
	assert.Equal(t, []string{state.GroupRepPickA}, lines[0].Changed)
}

// An engi pair never holds a representative bout, so the picks here are built by
// hand: the door's wiring is what is pinned, the same as the others'.
func TestReseatAudit_EngiDoorRecordsTheLine(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	const compID = "au-engi"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: "Engi Knockout", Kind: "individual",
		Format: state.CompFormatKnockout, PoolSize: 3, PoolWinners: 2,
		Courts: []string{"A"}, StartTime: "09:00", Status: "setup", Engi: true,
	}))
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	last := len(b.Rounds) - 1
	sf0, sf1, finalID := b.Rounds[last-1][0].ID, b.Rounds[last-1][1].ID, b.Rounds[last][0].ID
	_, err = eng.recordEngiMatchResult(store, compID, sf0, 3, 2, "")
	require.NoError(t, err)
	_, err = eng.recordEngiMatchResult(store, compID, sf1, 3, 2, "")
	require.NoError(t, err)

	b, err = store.LoadBracket(compID)
	require.NoError(t, err)
	final := &b.Rounds[last][0]
	final.SubResults = auRow(final.SideA, final.SideB, "pick-a", "pick-b")
	require.NoError(t, store.SaveBracket(compID, b))
	require.Empty(t, auLines(t, store, compID, finalID))

	_, err = eng.recordEngiMatchResult(store, compID, sf0, 2, 3, "correction")
	require.NoError(t, err)

	got, err := store.LoadBracket(compID)
	require.NoError(t, err)
	a, c := got.Rounds[last][0].RepPicks()
	require.Empty(t, a, "the final's side A was given the other pair")
	require.Equal(t, "pick-b", c)
	lines := auLines(t, store, compID, finalID)
	require.Len(t, lines, 1)
	assert.Equal(t, doorReseat, lines[0].Door)
	assert.Equal(t, []string{state.GroupRepPickA}, lines[0].Changed)
}

// A reopen retracts the winner it had advanced: the slot goes back to a
// placeholder, which is a re-seat too.
func TestReseatAudit_ReopenDoorRecordsTheLine(t *testing.T) {
	eng, store, compID := rpSetup(t, false)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	first := b.MatchByID("m-r1-0")
	first.Decision, first.DecisionBy = string(domain.DecisionKikenVoluntary), "B"
	require.NoError(t, store.SaveBracket(compID, b))

	_, err = eng.ReopenMatch(compID, "m-r1-0", "wrong waza")
	require.NoError(t, err)

	next := rpNext(t, store, compID)
	a, _ := next.RepPicks()
	require.Empty(t, a)
	lines := auLines(t, store, compID, rpNextID)
	require.Len(t, lines, 1)
	assert.Equal(t, doorReseat, lines[0].Door)
	assert.Equal(t, []string{state.GroupRepPickA}, lines[0].Changed)
	assert.Equal(t, next.GroupStamp(state.GroupRepPickA), lines[0].Stamp)
}

// A pool correction that moves a qualifier repaints the knockout slots it
// filled; a repainted match nobody has played gets a `reseat` line.
func TestReseatAudit_RequalificationRepaintRecordsTheLine(t *testing.T) {
	f := newRQFixture(t, "au-rq", 2, [][]string{{"A1", "A2"}, {"B1", "B2"}})
	f.scorePool("Pool A-0", "A1")
	f.scorePool("Pool B-0", "B1")
	f.resolve()
	first, firstSide := f.slot("Pool A-1st")
	second, secondSide := f.slot("Pool A-2nd")
	require.NotEqual(t, first.ID, second.ID, "the two Pool A places sit in different matches")

	group := func(side string) string {
		if side == "A" {
			return state.GroupRepPickA
		}
		return state.GroupRepPickB
	}
	b := f.bracket()
	for _, p := range []struct{ id, side, pick string }{
		{first.ID, firstSide, "pick-a1"},
		{second.ID, secondSide, "pick-a2"},
	} {
		row := state.SubMatchResult{Position: state.DaihyosenSubPosition}
		if p.side == "A" {
			row.SideAMemberID = p.pick
		} else {
			row.SideBMemberID = p.pick
		}
		findBracketMatchInBracket(b, p.id).SubResults = []state.SubMatchResult{row}
	}
	require.NoError(t, f.store.SaveBracket(f.compID, b))

	// A2 now wins the pool: nothing is played in the knockout, so no
	// confirmation is asked and both places are repainted.
	require.NoError(t, f.write("Pool A-0", f.poolResult("Pool A-0", "A2")))

	after := f.bracket()
	for _, p := range []struct{ id, side string }{{first.ID, firstSide}, {second.ID, secondSide}} {
		m := findBracketMatchInBracket(after, p.id)
		a, c := m.RepPicks()
		assert.Empty(t, a+c, "%s: the repaint took the pick of the team that left the slot", p.id)
		lines := auLines(t, f.store, f.compID, p.id)
		reseats := auDoor(lines, doorReseat)
		require.Len(t, reseats, 1, "%s: one reseat line", p.id)
		assert.Equal(t, []string{group(p.side)}, reseats[0].Changed, p.id)
		assert.Equal(t, m.GroupStamp(group(p.side)), reseats[0].Stamp, p.id)
	}
}

// A correction that stores the winner already recorded re-seats nothing.
func TestReseatAudit_SameWinnerLeavesNoLine(t *testing.T) {
	eng, store, compID := rpSetup(t, false)

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		_, err := eng.RecordMatchResultWithIneligibilityTx(tx, compID, "m-r1-0", rpCorrect(wrTeamA), ForceOptions{})
		return err
	}))

	assert.Empty(t, auLines(t, store, compID, rpNextID), "nothing was re-seated, so nothing is recorded")
}

// The K3 rollback replays the prior result through the same bracket callback
// under matchWriteRestore. That re-seats the next round's side again, but a
// restore is not a door: it records no line.
func TestReseatAudit_ARestoreIsNotADoor(t *testing.T) {
	eng, store, compID := rpSetup(t, false)
	b, err := store.LoadBracket(compID)
	require.NoError(t, err)
	// The stored state a rejected write left: Tora advanced and holds a pick.
	prior := bracketMatchAsResult(b.MatchByID("m-r1-0"))
	first := b.MatchByID("m-r1-0")
	first.Winner, first.WinnerID = wrTeamB, wrTeamBID
	next := b.MatchByID(rpNextID)
	next.SideA, next.SideAID = wrTeamB, wrTeamBID
	next.SubResults[0].SideA, next.SubResults[0].SideAMemberID = wrTeamB, "pick-tora"
	require.NoError(t, store.SaveBracket(compID, b))

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		eng.rollbackMatchResultTx(tx, compID, "m-r1-0", prior)
		return nil
	}))

	got := rpNext(t, store, compID)
	assert.Equal(t, wrTeamA, got.SideA, "the restore put the prior winner back")
	a, _ := got.RepPicks()
	assert.Empty(t, a, "and the pick of the team that left went with the re-seat")
	assert.Empty(t, auLines(t, store, compID, rpNextID), "but a restore is not a door and records no line")
}

// The recorder skips the groups a reopen line already names and only those: a
// reopened match whose reopen line does not name a group that went still gets
// the reseat line for it.
func TestReseatAudit_RecorderSkipsOnlyWhatAReopenLineNames(t *testing.T) {
	eng, store, compID := rpSetup(t, false)
	clears := []repPickClear{{MatchID: rpNextID, Cleared: []clearedPick{
		{Group: state.GroupRepPickA, Stamp: 5},
		{Group: state.GroupRepPickB, Stamp: 9},
	}}}

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		eng.recordRepPickClears(tx, compID, clears, []ReopenedMatch{{ID: rpNextID, RepPicksCleared: []string{state.GroupRepPickA}}})
		return nil
	}))
	lines := auLines(t, store, compID, rpNextID)
	require.Len(t, lines, 1)
	assert.Equal(t, []string{state.GroupRepPickB}, lines[0].Changed, "side A is the reopen line's, side B is not")
	assert.Equal(t, int64(9), lines[0].Stamp, "dated with the group the line names")

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		eng.recordRepPickClears(tx, compID, clears, []ReopenedMatch{{ID: rpNextID, RepPicksCleared: []string{state.GroupRepPickA, state.GroupRepPickB}}})
		return nil
	}))
	assert.Len(t, auLines(t, store, compID, rpNextID), 1, "every group named by the reopen line: no further line")

	require.NoError(t, inTx(t, store, compID, func(tx state.StoreTx) error {
		eng.recordRepPickClears(tx, compID, clears, []ReopenedMatch{{ID: "another-match", RepPicksCleared: []string{state.GroupRepPickA, state.GroupRepPickB}}})
		return nil
	}))
	both := auLines(t, store, compID, rpNextID)
	require.Len(t, both, 2, "a reopen line on another match names nothing here")
	assert.ElementsMatch(t, []string{state.GroupRepPickA, state.GroupRepPickB}, both[1].Changed)
	assert.Equal(t, int64(9), both[1].Stamp, "a match that lost both is dated with the later of the two")
}

// The snapshot leaves out the match the door itself writes and any match with
// no pick, and the diff lists only a pick that is gone.
func TestReseatAudit_SnapshotAndDiff(t *testing.T) {
	picked := func(id, a, c string) state.BracketMatch {
		return state.BracketMatch{ID: id, SubResults: auRow("A", "B", a, c)}
	}
	build := func() *state.Bracket {
		return &state.Bracket{
			Rounds: [][]state.BracketMatch{
				{picked("own", "own-a", "own-b"), picked("one", "p1", "p2")},
				{picked("two", "p3", ""), {ID: "bare"}},
			},
			ThirdPlaceMatch: func() *state.BracketMatch { m := picked("bronze", "", "p4"); return &m }(),
		}
	}
	b := build()
	snap := repPickSnapshot(b, "own")
	assert.Equal(t, map[string][2]string{
		"one": {"p1", "p2"}, "two": {"p3", ""}, "bronze": {"", "p4"},
	}, snap, "the written match and a match with no pick are not in it")
	assert.Contains(t, repPickSnapshot(b, ""), "own")
	assert.Empty(t, repPickSnapshot(nil, ""))

	assert.Empty(t, repPickClears(snap, b), "nothing changed")
	assert.Empty(t, repPickClears(nil, b), "no snapshot, no diff")

	b.Rounds[0][1].SetRepPick(domain.MatchSideB, "")
	b.Rounds[1][0].SubResults = nil // the whole row went
	b.ThirdPlaceMatch.SetRepPick(domain.MatchSideB, "p9")
	b.Rounds[0][0].SetRepPick(domain.MatchSideA, "")
	got := repPickClears(snap, b)
	require.Len(t, got, 2, "a pick replaced by another is not a clear; the written match is not diffed")
	assert.Equal(t, "one", got[0].MatchID)
	assert.Equal(t, []string{state.GroupRepPickB}, groupsOf(got[0]))
	assert.Equal(t, "two", got[1].MatchID)
	assert.Equal(t, []string{state.GroupRepPickA}, groupsOf(got[1]))
}

func groupsOf(c repPickClear) []string {
	var out []string
	for _, p := range c.Cleared {
		out = append(out, p.Group)
	}
	slices.Sort(out)
	return out
}
