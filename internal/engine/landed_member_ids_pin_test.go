package engine

import (
	"fmt"
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
// and picks). On a kachinuki match the write also seats bout 1 from the lineup
// the match fields (afterMatchWrite through seatOpenerAfterWrite: its own
// lineup, its empty places filled by member number, and the stayer of bout 1's
// streak renamed with it), which
// lands the side's own team's members. The load repair
// (state.resolveSubMemberIDs) runs once per competition per process, before the
// write under test, on rows that already carry their ids, and fills only a
// row's own team's members.
//
// What the fixture is built to reach:
//   - Both teams' lineups for the match (as the start pins them) and team
//     members are saved, every place named, so the seat changes nothing. The
//     store mints the members' ids, so no row carries one: an id taken from a
//     lineup or from the members by anything inside the write would be a FOREIGN
//     id (neither the payload's nor the stored match's), which the invariant
//     refuses unless it is the side's own team's member on a kachinuki match.
//     One kachinuki case stores no lineup at all, so the seat really lands the
//     seeded, nameless member 1 of each team.
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
// of the landed row's own side ids, or (a kachinuki side only, at any position)
// the side's own team holds it. That last arm is the server's own seating from
// the fielded lineup (bout 1's seat and its streak rename, kachinuki_opener.go,
// and the advance's appended pairing), whose ids are always the side's own
// team's members: exactly what the judge accepts (rowMembersOutsideTeams
// refuses only an id the side's team does not hold). It is position
// independent, because the seat renames along the streak and Record bout
// appends team-owned ids at later positions.

// pinTeams are the landed match's side team ids and the competition's team
// members, for the kachinuki arm of the invariant. A nil *pinTeams adds no arm.
type pinTeams struct {
	aID, bID string
	members  map[string][]domain.TeamMember
}

// holds answers whether the team with participant id teamID holds a member
// with id memberID.
func (p *pinTeams) holds(teamID, memberID string) bool {
	if p == nil || teamID == "" || memberID == "" {
		return false
	}
	return slices.ContainsFunc(p.members[teamID], func(m domain.TeamMember) bool { return m.ID == memberID })
}

// introducedMemberIDs lists, one line each, every landed member id at a judged
// field that the invariant does not account for.
func introducedMemberIDs(stored, payload, landed []state.SubMatchResult, teams *pinTeams) []string {
	supplied := func(got string, field func(state.SubMatchResult) string, row state.SubMatchResult) bool {
		return got == "" ||
			got == field(state.SubResultAt(payload, row.Position)) ||
			got == field(state.SubResultAt(stored, row.Position))
	}
	sideA := func(r state.SubMatchResult) string { return r.SideAMemberID }
	sideB := func(r state.SubMatchResult) string { return r.SideBMemberID }
	winner := func(r state.SubMatchResult) string { return r.WinnerMemberID }
	var out []string
	for _, row := range landed {
		if !supplied(row.SideAMemberID, sideA, row) && (teams == nil || !teams.holds(teams.aID, row.SideAMemberID)) {
			out = append(out, fmt.Sprintf("position %d: side A member id %q was supplied by neither the payload, the stored row, nor side A's own team", row.Position, row.SideAMemberID))
		}
		if !supplied(row.SideBMemberID, sideB, row) && (teams == nil || !teams.holds(teams.bID, row.SideBMemberID)) {
			out = append(out, fmt.Sprintf("position %d: side B member id %q was supplied by neither the payload, the stored row, nor side B's own team", row.Position, row.SideBMemberID))
		}
		ownFighter := row.WinnerMemberID == row.SideAMemberID || row.WinnerMemberID == row.SideBMemberID
		if !supplied(row.WinnerMemberID, winner, row) && !ownFighter {
			out = append(out, fmt.Sprintf("position %d: winner member id %q was supplied by neither the payload, the stored row, nor the row's own landed fighters", row.Position, row.WinnerMemberID))
		}
	}
	return out
}

// assertNoIntroducedMemberID fails for any landed member id at a judged field
// that the invariant does not account for.
func assertNoIntroducedMemberID(t *testing.T, stored, payload, landed []state.SubMatchResult, teams *pinTeams) {
	t.Helper()
	for _, line := range introducedMemberIDs(stored, payload, landed, teams) {
		t.Error(line)
	}
}

// The kachinuki arm accepts only the side's OWN team's members, at any
// position: another team's member, or an id no team holds, is still refused,
// on bout 1 and on a later bout alike.
func TestPinInvariant_AKachinukiSideTakesOnlyItsOwnTeamsMembers(t *testing.T) {
	teams := &pinTeams{aID: wrTeamAID, bID: wrTeamBID, members: map[string][]domain.TeamMember{
		wrTeamAID: {{ID: "a-1", Index: 1}, {ID: "a-2", Index: 2}},
		wrTeamBID: {{ID: "b-1", Index: 1}},
	}}
	rows := func(pos int, a, b string) []state.SubMatchResult {
		return []state.SubMatchResult{{Position: pos, SideAMemberID: a, SideBMemberID: b}}
	}
	for _, pos := range []int{1, 3} {
		assert.Empty(t, introducedMemberIDs(nil, nil, rows(pos, "a-2", "b-1"), teams),
			"position %d: each side's own team's members pass", pos)
		assert.Len(t, introducedMemberIDs(nil, nil, rows(pos, "b-1", ""), teams), 1,
			"position %d: the other team's member on side A is refused", pos)
		assert.Len(t, introducedMemberIDs(nil, nil, rows(pos, "", "a-1"), teams), 1,
			"position %d: the other team's member on side B is refused", pos)
		assert.Len(t, introducedMemberIDs(nil, nil, rows(pos, "k-sato", "o-tanaka"), teams), 2,
			"position %d: ids no team holds are refused", pos)
		assert.Len(t, introducedMemberIDs(nil, nil, rows(pos, "a-1", "b-1"), nil), 2,
			"position %d: with no arm (fixed order) a team's own member the payload did not bring is refused", pos)
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

// pinOpenerIDs are the member ids of each team's first fighter, the ones the
// kachinuki fixture's bout 1 holds.
var pinOpenerIDs = map[string]string{wrTeamAID: "k-sato", wrTeamBID: "o-tanaka"}

// pinHome seeds a team match of the given type, running since mmT0 and holding
// the stored rows, on the pool branch (a league) or the bracket branch (a
// knockout), with both teams' lineups for the match and team members saved, or,
// with noLineup, no lineup at all and only the members the store seeds.
func pinHome(t *testing.T, knockout bool, matchType state.TeamMatchType, stored []state.SubMatchResult, noLineup bool) mmHome {
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
		if noLineup {
			break
		}
		// The lineup is the match's OWN, as the start pins it (pinStartLineups):
		// a started match with nothing of its own fields an empty lineup filled by
		// member number (kachinukiFielded, K1 (c)), never a carried starting
		// lineup, and production never stores a running match without its pin.
		lineup := domain.TeamLineup{
			TeamID: p.teamID, MatchID: h.matchID,
			Positions: map[domain.Position]string{}, MemberIDs: map[domain.Position]string{},
		}
		for i, name := range p.names {
			pos := domain.PositionNumbered(i + 1)
			if i == 0 {
				// A kachinuki encounter's first fighters are the lineup's
				// (operator decision K1 (c), kachinuki_opener.go), seated by
				// every write: the lineup names the fighters the stored rows
				// hold, so the seat lands no id of its own and the invariant
				// is about what the write itself introduces.
				lineup.Positions[pos], lineup.MemberIDs[pos] = name, pinOpenerIDs[p.teamID]
				continue
			}
			member, err := store.AddTeamMember(h.compID, p.teamID, name)
			require.NoError(t, err)
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
		// noLineup stores no lineup at all, so the seat fields each team's
		// members by number (kachinukiFielded); seated then gives the ids the
		// landed rows must hold from each team's member 1 (by team id), since the
		// store mints them.
		noLineup bool
		seated   func(first map[string]string) map[int]pinIDs
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
			// The headline case of number order (bc-kno1): no lineup anywhere and
			// the seeded, nameless members. The seat lands each team's member 1 on
			// bout 1 and carries team A's onto bout 2, where Sato stayed on, so
			// the write lands ids neither the payload nor the stored rows held:
			// the server's own seating, always the side's own team's members.
			name:      "kachinuki: no lineup, the seat fields each team's member 1 and renames the streak",
			matchType: state.TeamMatchTypeKachinuki,
			noLineup:  true,
			stored:    stripIDs(kachinukiRows()),
			write:     pinWriteSpec{at: mmT1, changed: []string{"bout:2"}, rows: stripIDs(ito(kachinukiRows()))},
			seated: func(first map[string]string) map[int]pinIDs {
				a, b := first[wrTeamAID], first[wrTeamBID]
				return map[int]pinIDs{1: {a, b, a}, 2: {a, "", ""}}
			},
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
					h := pinHome(t, b.knockout, tt.matchType, tt.stored, tt.noLineup)
					for _, s := range tt.setup {
						require.NoError(t, h.write(pinWrite(h, s)))
					}
					// The match as it stands before the write under test: after the
					// setup writes it is no longer what the fixture stored.
					before := h.load(t)
					require.NotEmpty(t, before.SubResults)
					inherited := tt.inherited
					if tt.seated != nil {
						// The load above seeded the team members (a roster save
						// seeds them on the next load); each team's member 1 is
						// nameless, so the case is about a nameless fighter.
						first := map[string]string{}
						squads, err := h.store.LoadSquads(h.compID)
						require.NoError(t, err)
						for _, teamID := range []string{wrTeamAID, wrTeamBID} {
							for _, m := range squads[teamID] {
								if m.Index == 1 {
									first[teamID] = m.ID
									require.Empty(t, m.Name, "team %s: member 1 is seeded nameless", teamID)
								}
							}
							require.NotEmpty(t, first[teamID], "team %s holds a member 1 with an id", teamID)
						}
						inherited = tt.seated(first)
					}

					require.NoError(t, h.write(pinWrite(h, tt.write)),
						"the write applies in part: a write held whole answers superseded")

					landed := h.load(t)
					require.NotEmpty(t, landed.SubResults)
					var teams *pinTeams
					if tt.matchType == state.TeamMatchTypeKachinuki {
						members, err := h.store.LoadSquads(h.compID)
						require.NoError(t, err)
						teams = &pinTeams{aID: landed.SideAID, bID: landed.SideBID, members: members}
						require.NotEmpty(t, teams.aID)
						require.NotEmpty(t, teams.bID)
					}
					assertNoIntroducedMemberID(t, before.SubResults, tt.write.rows, landed.SubResults, teams)
					assert.ElementsMatch(t, tt.held, pinHeld(t, h.history(t)),
						"the groups the merge held, per the match's history")

					for position, want := range inherited {
						row := state.SubResultAt(landed.SubResults, position)
						assert.Equal(t, want, pinIDs{row.SideAMemberID, row.SideBMemberID, row.WinnerMemberID},
							"position %d: the ids the write was meant to carry over are still there", position)
					}
				})
			}
		})
	}
}
