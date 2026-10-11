package engine

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// The self-run member judge (mobileapp.landedMembersRefusal) is only asked
// about a write whose payload INTRODUCES a member id (introducesJudgedMember),
// and that gate is equivalent to judging every write only while the ENGINE never
// lands a member id the judge would refuse that the payload did not introduce.
// This is a PIN, green by design: it holds the invariant from this side, so a
// change inside the write that fills ids from somewhere new fails here rather
// than silently slipping past a gate that does not look.
//
// The id fillers it exercises inside the write are preserveKachinukiMemberIDs
// (the stored row's id at the same position and name), ResolveMemberWinnerID /
// ReconcileWinnerMemberID (the winner's id, derived from the row's own landed
// side ids, which the judge accepts) and the merge's copies of the groups a
// write does not apply, state.CopyGroup and SetRepPick (the stored row's ids
// and picks). The load repair (state.resolveSubMemberIDs) runs once per
// competition per process, before the write under test, on rows that already
// carry their ids, and fills only a row's own team's members.
//
// What the fixture is built to reach:
//   - Both teams' starting lineups and team members are saved. The store mints
//     the members' ids, so no row carries one: an id taken from a lineup or from
//     the members by anything inside the write would be a FOREIGN id (neither
//     the payload's nor the stored match's), which the invariant refuses.
//     MaybeAdvanceKachinuki is NOT called: its lineup ids are added after the
//     transaction, in the handler, and this pins what the write itself lands.
//   - The match is stored at mmT0 and every write but one is stamped, so the
//     merge orders it. The "older" cases send a write stamped between mmT0 and
//     the stamp of the group it rewrites, with another group applying, so that
//     group is HELD and copied back from the stored match; each case states
//     which groups it expects held, and the match's history is asked.
//   - Kachinuki runs on a league (the pool branch) and on a knockout (the
//     bracket branch). The representative bout runs on the knockout only, the
//     one place production writes it (AddDaihyosen refuses a pool match).
//   - Nothing here sends startOnly, so keepQueuedScore (a start) is not pinned.
//
// The invariant, per landed row and judged field (either side's member id, and
// the winner's): the id is empty, or the payload's row at that position carried
// it, or the stored row at that position held it, or (the winner only) it is one
// of the landed row's own side ids.

// assertNoIntroducedMemberID fails for any landed member id at a judged field
// that neither the payload nor the stored match supplied.
func assertNoIntroducedMemberID(t *testing.T, stored, payload, landed []state.SubMatchResult) {
	t.Helper()
	supplied := func(got string, field func(state.SubMatchResult) string, row state.SubMatchResult) bool {
		return got == "" ||
			got == field(state.SubResultAt(payload, row.Position)) ||
			got == field(state.SubResultAt(stored, row.Position))
	}
	sideA := func(r state.SubMatchResult) string { return r.SideAMemberID }
	sideB := func(r state.SubMatchResult) string { return r.SideBMemberID }
	winner := func(r state.SubMatchResult) string { return r.WinnerMemberID }
	for _, row := range landed {
		assert.True(t, supplied(row.SideAMemberID, sideA, row),
			"position %d: side A member id %q was supplied by neither the payload nor the stored row", row.Position, row.SideAMemberID)
		assert.True(t, supplied(row.SideBMemberID, sideB, row),
			"position %d: side B member id %q was supplied by neither the payload nor the stored row", row.Position, row.SideBMemberID)
		ownFighter := row.WinnerMemberID == row.SideAMemberID || row.WinnerMemberID == row.SideBMemberID
		assert.True(t, supplied(row.WinnerMemberID, winner, row) || ownFighter,
			"position %d: winner member id %q was supplied by neither the payload, the stored row, nor the row's own landed fighters", row.Position, row.WinnerMemberID)
	}
}

// pinBetween is a stamp after the match was stored (mmT0) and before the stamps
// the seeding writes give the groups they change (mmT1): a write made then is
// older than those groups and newer than every other.
const pinBetween = mmT0 + 30_000

// pinIDs are the member ids a landed row must hold: its two fighters', and the
// winner's.
type pinIDs struct{ a, b, winner string }

// pinWriteSpec is one stamped score write: when it was made, the groups it names
// (nil: every group its rows carry, as an older client sends it), and its rows.
type pinWriteSpec struct {
	at      int64
	changed []string
	rows    []state.SubMatchResult
}

// pinPeople are the people each team is given, in lineup order. The store mints
// their member ids, which no row of the fixtures carries.
var pinPeople = []struct {
	teamID string
	names  []string
}{
	{wrTeamAID, []string{"Sato", "Ota", "Hara"}},
	{wrTeamBID, []string{"Tanaka", "Ito", "Mori"}},
}

// pinHome seeds a team match of the given type, running since mmT0 and holding
// the stored rows, on the pool branch (a league) or the bracket branch (a
// knockout), with both teams' starting lineups and team members saved.
func pinHome(t *testing.T, knockout bool, matchType state.TeamMatchType, stored []state.SubMatchResult) mmHome {
	t.Helper()
	eng, store, dir := setupTestEngine(t)
	h := mmHome{eng: eng, store: store, dir: dir}
	team := func(c *state.Competition) { c.Kind, c.TeamSize, c.TeamMatchType = "team", 3, matchType }
	if knockout {
		h.compID, h.matchID = "pin-ko", "m-r1-0"
		c := &state.Competition{ID: h.compID, Name: "pin", Status: state.CompStatusKnockout}
		team(c)
		require.NoError(t, store.SaveCompetition(c))
	} else {
		h.compID, h.matchID = "pin-pool", "Pool A-0"
		createTestCompetition(t, store, h.compID, "league", 3, func(c *state.Competition) { team(c); c.Status = state.CompStatusPools })
	}
	wrSaveTeams(t, store, h.compID)
	for _, p := range pinPeople {
		lineup := domain.TeamLineup{
			TeamID: p.teamID, Round: 0,
			Positions: map[domain.Position]string{}, MemberIDs: map[domain.Position]string{},
		}
		for i, name := range p.names {
			member, err := store.AddTeamMember(h.compID, p.teamID, name)
			require.NoError(t, err)
			pos := domain.PositionNumbered(i + 1)
			lineup.Positions[pos], lineup.MemberIDs[pos] = name, member.ID
		}
		require.NoError(t, store.SetTeamLineup(h.compID, lineup, 3))
	}
	if knockout {
		require.NoError(t, store.SaveBracket(h.compID, &state.Bracket{Rounds: [][]state.BracketMatch{{{
			ID: h.matchID, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
			Status: state.MatchStatusRunning, ModifiedAt: mmT0, MatchNumber: 1,
			SubResults: state.CloneSubResults(stored),
		}}}}))
		return h
	}
	require.NoError(t, store.SavePoolMatches(h.compID, []state.MatchResult{{
		ID: h.matchID, SideA: wrTeamA, SideAID: wrTeamAID, SideB: wrTeamB, SideBID: wrTeamBID,
		Status: state.MatchStatusRunning, ModifiedAt: mmT0, SubResults: state.CloneSubResults(stored),
	}}))
	return h
}

// pinWrite is the running score write a board makes. The write reshapes the rows
// it is handed in place, so it is given a copy: the spec keeps what was sent.
func pinWrite(h mmHome, s pinWriteSpec) *state.MatchResult {
	w := mmRunning(h, s.at, s.changed...)
	w.SubResults = state.CloneSubResults(s.rows)
	return w
}

// pinHeld lists, sorted, the groups the last write in the match's history was
// held on.
func pinHeld(t *testing.T, entries []state.MatchHistoryEntry) []string {
	t.Helper()
	require.NotEmpty(t, entries)
	var held []string
	for group, outcome := range entries[len(entries)-1].Outcomes {
		if outcome == state.HistoryOutcomeHeld {
			held = append(held, group)
		}
	}
	slices.Sort(held)
	return held
}

func TestEngineLandsNoMemberIDThePayloadDidNotIntroduce(t *testing.T) {
	// Sato (team Ryu) fought Tanaka and won; Sato now faces Ito.
	kachinukiRows := func() []state.SubMatchResult {
		return []state.SubMatchResult{
			{Position: 1, SideA: "Sato", SideAMemberID: "k-sato", SideB: "Tanaka", SideBMemberID: "o-tanaka",
				IpponsA: []string{"M"}, Winner: "Sato", WinnerMemberID: "k-sato", Decision: "fought"},
			{Position: 2, SideA: "Sato", SideAMemberID: "k-sato", SideB: "Ito", SideBMemberID: "o-ito"},
		}
	}
	// ito has Ito win bout 2 against Sato.
	ito := func(rows []state.SubMatchResult) []state.SubMatchResult {
		rows[1].IpponsB = []string{"K"}
		rows[1].Winner, rows[1].WinnerMemberID, rows[1].Decision = "Ito", "o-ito", "fought"
		return rows
	}
	// satoTwice gives Sato a second point in bout 1.
	satoTwice := func(rows []state.SubMatchResult) []state.SubMatchResult {
		rows[0].IpponsA = []string{"M", "K"}
		return rows
	}
	fixedRows := func() []state.SubMatchResult {
		return []state.SubMatchResult{
			{Position: 1, SideA: "Sato", SideAMemberID: "k1", SideB: "Tanaka", SideBMemberID: "o1",
				IpponsA: []string{"M"}, Winner: "Sato", WinnerMemberID: "k1", Decision: "fought"},
		}
	}
	// repBout is the representative-bout row, with the picks and the point the
	// board knows of (none: ""). A point is Ryu's, so Ryu is its winner.
	repBout := func(sideAID, sideBID, winnerID string, point bool) state.SubMatchResult {
		if !point {
			return repRow(sideAID, sideBID, nil, nil)
		}
		row := repRow(sideAID, sideBID, []string{"M"}, nil)
		row.Winner, row.WinnerMemberID = wrTeamA, winnerID
		return row
	}
	// stripIDs is a write that names its fighters and sends no member id.
	stripIDs := func(rows []state.SubMatchResult) []state.SubMatchResult {
		out := make([]state.SubMatchResult, len(rows))
		for i, r := range rows {
			r.SideAMemberID, r.SideBMemberID, r.WinnerMemberID = "", "", ""
			out[i] = r
		}
		return out
	}
	// repPicks is the write that seeds both representatives' picks at mmT1 (each
	// side's pick is its own group), beside the stored numbered bout.
	repPicks := pinWriteSpec{
		at:      mmT1,
		changed: []string{repBoutGroup, repPickAName, repPickBName},
		rows:    append(fixedRows(), repBout("k-rep", "o-rep", "", false)),
	}

	tests := []struct {
		name      string
		matchType state.TeamMatchType
		// bracketOnly keeps a case off the pool branch: the representative bout
		// is a knockout shape.
		bracketOnly bool
		stored      []state.SubMatchResult
		// setup are the writes made before the one under test.
		setup []pinWriteSpec
		write pinWriteSpec
		// held are the groups the write under test must be held on, per the
		// match's history: without them the held-group copies the "older"
		// cases are built for would not be known to have run.
		held []string
		// inherited are the ids the landed rows must hold, by position, so the
		// pin is not vacuously true of a write that dropped every id: the
		// carry-over mechanisms it guards are really exercised.
		inherited map[int]pinIDs
	}{
		{
			name:      "kachinuki: a write echoing the stored member ids",
			matchType: state.TeamMatchTypeKachinuki,
			stored:    kachinukiRows(),
			write:     pinWriteSpec{at: mmT1, changed: []string{"bout:2"}, rows: ito(kachinukiRows())},
			inherited: map[int]pinIDs{1: {"k-sato", "o-tanaka", "k-sato"}, 2: {"k-sato", "o-ito", "o-ito"}},
		},
		{
			name:      "kachinuki: a write whose rows carry names and no member ids, over a position the stored match lacks",
			matchType: state.TeamMatchTypeKachinuki,
			stored:    kachinukiRows(),
			write: pinWriteSpec{at: mmT1, rows: append(stripIDs(ito(kachinukiRows())),
				state.SubMatchResult{Position: 3, SideA: "Ota", SideB: "Ito"})},
			inherited: map[int]pinIDs{1: {"k-sato", "o-tanaka", "k-sato"}, 2: {"k-sato", "o-ito", "o-ito"}, 3: {}},
		},
		{
			name:      "kachinuki: the same write from a client that stamps nothing",
			matchType: state.TeamMatchTypeKachinuki,
			stored:    kachinukiRows(),
			write: pinWriteSpec{rows: append(stripIDs(ito(kachinukiRows())),
				state.SubMatchResult{Position: 3, SideA: "Ota", SideB: "Ito"})},
			inherited: map[int]pinIDs{1: {"k-sato", "o-tanaka", "k-sato"}, 2: {"k-sato", "o-ito", "o-ito"}, 3: {}},
		},
		{
			name:      "kachinuki: a write older than bout 1's last change, whose rows carry no member ids",
			matchType: state.TeamMatchTypeKachinuki,
			stored:    kachinukiRows(),
			setup:     []pinWriteSpec{{at: mmT1, changed: []string{"bout:1"}, rows: satoTwice(kachinukiRows())}},
			write:     pinWriteSpec{at: pinBetween, rows: stripIDs(ito(kachinukiRows()))},
			held:      []string{"bout:1"},
			inherited: map[int]pinIDs{1: {"k-sato", "o-tanaka", "k-sato"}, 2: {"k-sato", "o-ito", "o-ito"}},
		},
		{
			name:        "fixed order with a representative bout: a write echoing the stored member ids",
			matchType:   state.TeamMatchTypeFixed,
			bracketOnly: true,
			stored:      fixedRows(),
			setup:       []pinWriteSpec{repPicks},
			write: pinWriteSpec{at: mmT2,
				rows: append(satoTwice(fixedRows()), repBout("k-rep", "o-rep", "k-rep", true))},
			inherited: map[int]pinIDs{1: {"k1", "o1", "k1"}, state.DaihyosenSubPosition: {"k-rep", "o-rep", "k-rep"}},
		},
		{
			name:        "fixed order with a representative bout: a point on it from a board that holds no picks",
			matchType:   state.TeamMatchTypeFixed,
			bracketOnly: true,
			stored:      fixedRows(),
			setup:       []pinWriteSpec{repPicks},
			write: pinWriteSpec{at: mmT2, changed: []string{repBoutGroup},
				rows: append(stripIDs(fixedRows()), repBout("", "", "", true))},
			inherited: map[int]pinIDs{1: {"k1", "o1", "k1"}, state.DaihyosenSubPosition: {"k-rep", "o-rep", "k-rep"}},
		},
		{
			name:        "fixed order with a representative bout: a write older than the picks, whose representative row carries none",
			matchType:   state.TeamMatchTypeFixed,
			bracketOnly: true,
			stored:      fixedRows(),
			setup:       []pinWriteSpec{repPicks},
			write: pinWriteSpec{at: pinBetween,
				rows: append(satoTwice(fixedRows()), repBout("", "", "", true))},
			held:      []string{repBoutGroup, repPickAName, repPickBName},
			inherited: map[int]pinIDs{1: {"k1", "o1", "k1"}, state.DaihyosenSubPosition: {"k-rep", "o-rep", ""}},
		},
		{
			name:        "fixed order with a representative bout: a write from a sheet that does not list the row",
			matchType:   state.TeamMatchTypeFixed,
			bracketOnly: true,
			stored:      fixedRows(),
			setup:       []pinWriteSpec{repPicks},
			write:       pinWriteSpec{at: mmT2, changed: []string{"bout:1"}, rows: satoTwice(fixedRows())},
			inherited:   map[int]pinIDs{1: {"k1", "o1", "k1"}, state.DaihyosenSubPosition: {"k-rep", "o-rep", ""}},
		},
	}
	branches := []struct {
		name     string
		knockout bool
	}{{"pool", false}, {"knockout", true}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, b := range branches {
				if tt.bracketOnly && !b.knockout {
					continue
				}
				t.Run(b.name, func(t *testing.T) {
					h := pinHome(t, b.knockout, tt.matchType, tt.stored)
					for _, s := range tt.setup {
						require.NoError(t, h.write(pinWrite(h, s)))
					}
					// The match as it stands before the write under test: after the
					// setup writes it is no longer what the fixture stored.
					before := h.load(t)
					require.NotEmpty(t, before.SubResults)

					require.NoError(t, h.write(pinWrite(h, tt.write)),
						"the write applies in part: a write held whole answers superseded")

					landed := h.load(t)
					require.NotEmpty(t, landed.SubResults)
					assertNoIntroducedMemberID(t, before.SubResults, tt.write.rows, landed.SubResults)
					assert.ElementsMatch(t, tt.held, pinHeld(t, h.history(t)),
						"the groups the merge held, per the match's history")

					for position, want := range tt.inherited {
						row := state.SubResultAt(landed.SubResults, position)
						assert.Equal(t, want, pinIDs{row.SideAMemberID, row.SideBMemberID, row.WinnerMemberID},
							"position %d: the ids the write was meant to carry over are still there", position)
					}
				})
			}
		})
	}
}
