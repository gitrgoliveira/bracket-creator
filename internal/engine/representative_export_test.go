package engine

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// representativeTeams is a fixed-order team knockout's two teams with three
// named members each; the draw puts Red first, so its members are T1.1-T1.3
// and White's T2.1-T2.3.
type representativeTeams struct {
	redID, whiteID string
	red, white     []domain.TeamMember
}

// setupRepresentativeComp saves a fixed-order team knockout with two teams and
// their members, and returns the engine over it. Deliberately NOT
// setupKachinukiComp: this fixture's point is the non-kachinuki team type.
func setupRepresentativeComp(t *testing.T, compID string) (*Engine, *state.Store, representativeTeams) {
	t.Helper()
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		Format:        state.CompFormatKnockout,
		Status:        state.CompStatusKnockout,
		Kind:          "team",
		TeamSize:      3,
		TeamMatchType: state.TeamMatchTypeFixed,
		NumberPrefix:  "T",
	}))

	teams := representativeTeams{redID: helper.NewUUID4(), whiteID: helper.NewUUID4()}
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: teams.redID, Name: "RedTeam", Dojo: "DojoR"},
		{ID: teams.whiteID, Name: "WhiteTeam", Dojo: "DojoW"},
	}))
	add := func(teamID string, names ...string) []domain.TeamMember {
		out := make([]domain.TeamMember, 0, len(names))
		for _, n := range names {
			m, err := store.AddTeamMember(compID, teamID, n)
			require.NoError(t, err)
			out = append(out, m)
		}
		return out
	}
	teams.red = add(teams.redID, "Alice", "Aiko", "Akira")
	teams.white = add(teams.whiteID, "Bob", "Bea", "Ben")
	require.Equal(t, 3, teams.red[2].Index, "Akira is Red's third member")
	require.Equal(t, 2, teams.white[1].Index, "Bea is White's second member")
	return eng, store, teams
}

// representativeRow is the representative bout a hantei decided for Red: the
// row names the TEAMS (by rule), the picked members ride in the member ids.
func representativeRow(teams representativeTeams) state.SubMatchResult {
	return state.SubMatchResult{
		Position:      state.DaihyosenSubPosition,
		SideA:         "RedTeam",
		SideB:         "WhiteTeam",
		SideAMemberID: teams.red[2].ID,
		SideBMemberID: teams.white[1].ID,
		Winner:        "RedTeam",
		Decision:      string(domain.DecisionDaihyosen),
		IpponsA:       []string{domain.HanteiMark},
	}
}

// bracketMatchWith is one completed knockout semi-final between Red and White
// holding subs.
func bracketMatchWith(teams representativeTeams, subs ...state.SubMatchResult) state.BracketMatch {
	return state.BracketMatch{
		ID:           "SF1",
		MatchNumber:  1,
		DisplayRound: 1,
		SideA:        "RedTeam",
		SideAID:      teams.redID,
		SideB:        "WhiteTeam",
		SideBID:      teams.whiteID,
		Winner:       "RedTeam",
		Status:       state.MatchStatusCompleted,
		SubResults:   subs,
	}
}

func saveBracketOf(t *testing.T, store *state.Store, compID string, teams representativeTeams, matches ...state.BracketMatch) {
	t.Helper()
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		DrawOrder: []string{teams.redID, teams.whiteID},
		Rounds:    [][]state.BracketMatch{matches},
	}))
}

// numberedBout is a tied numbered bout, which the sheet must not list.
func numberedBout(teams representativeTeams) state.SubMatchResult {
	return state.SubMatchResult{
		Position: 1, SideA: "Alice", SideAMemberID: teams.red[0].ID,
		SideB: "Bob", SideBMemberID: teams.white[0].ID, Decision: "hikiwake",
	}
}

func TestRepresentativeBoutMatches_ListsTheRepresentativeBout(t *testing.T) {
	compID := "representative-lists-the-bout"
	eng, store, teams := setupRepresentativeComp(t, compID)
	saveBracketOf(t, store, compID, teams, bracketMatchWith(teams, numberedBout(teams), representativeRow(teams)))

	out, err := eng.RepresentativeBoutMatches(compID)
	require.NoError(t, err)
	require.Len(t, out, 1, "one section for the one match holding a representative bout")

	section := out[0]
	assert.Equal(t, helper.EliminationMatchTitle(1, 1), section.Label, "titled as the Elimination Matches sheet prints the match")
	assert.Equal(t, "RedTeam", section.SideATeam)
	assert.Equal(t, "WhiteTeam", section.SideBTeam)
	require.Len(t, section.Bouts, 1, "the representative bout alone: the numbered bouts are on the main sheets")

	bout := section.Bouts[0]
	assert.Equal(t, state.DaihyosenSubPosition, bout.Position)
	assert.Equal(t, "Akira", bout.SideAName, "Red's representative, not the team")
	assert.Equal(t, "T1.3", bout.SideALabel)
	assert.Equal(t, "Bea", bout.SideBName)
	assert.Equal(t, "T2.2", bout.SideBLabel)
	assert.Empty(t, bout.SideAPos, "a representative holds no lineup position")
	assert.Empty(t, bout.SideBPos)
	assert.Equal(t, "(DH)", bout.Middle, "the centre is the closed-set daihyosen mark, domain.MiddleMark's")
	assert.Equal(t, "Ht", bout.MarkA, "the hantei mark rides beside the winner's side")
	assert.Empty(t, bout.MarkB)
	assert.Empty(t, bout.ScoreA, "the hantei mark is not a point")
	assert.Empty(t, bout.ScoreB)
	assert.Zero(t, section.BlankBoutRows)
}

// A side whose representative was not picked prints no fighter: the row stores
// the TEAM's name where a fighter's would be, and the viewer shows "-" there,
// so the export must not print the team as a person.
func TestRepresentativeBoutMatches_UnpickedSidePrintsNoName(t *testing.T) {
	cases := []struct {
		name  string
		sideB func(teams representativeTeams) string
	}{
		{"no pick", func(representativeTeams) string { return "" }},
		{"an id that resolves to no member", func(representativeTeams) string { return "a-removed-member" }},
		{"a member of the other team", func(teams representativeTeams) string { return teams.red[0].ID }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			compID := "representative-unpicked-" + strings.ReplaceAll(tc.name, " ", "-")
			eng, store, teams := setupRepresentativeComp(t, compID)
			row := representativeRow(teams)
			row.SideBMemberID = tc.sideB(teams)
			saveBracketOf(t, store, compID, teams, bracketMatchWith(teams, row))

			out, err := eng.RepresentativeBoutMatches(compID)
			require.NoError(t, err)
			require.Len(t, out, 1)
			bout := out[0].Bouts[0]
			assert.Empty(t, bout.SideBName, "White's side picked nobody that resolves")
			assert.Empty(t, bout.SideBLabel)
			assert.Equal(t, "Akira", bout.SideAName, "the other side's pick is unaffected")
			assert.Equal(t, "T1.3", bout.SideALabel)
		})
	}
}

// With no team-members file at all every pick is unresolvable.
func TestRepresentativeBoutMatches_NoMembersFilePrintsTheBoutLabelAlone(t *testing.T) {
	compID := "representative-no-members"
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Format: state.CompFormatKnockout, Status: state.CompStatusKnockout,
		Kind: "team", TeamSize: 3, TeamMatchType: state.TeamMatchTypeFixed, NumberPrefix: "T",
	}))
	teams := representativeTeams{redID: helper.NewUUID4(), whiteID: helper.NewUUID4()}
	teams.red = []domain.TeamMember{{ID: "m-red"}, {ID: "m-red-2"}, {ID: "m-red-3"}}
	teams.white = []domain.TeamMember{{ID: "m-white"}, {ID: "m-white-2"}}
	saveBracketOf(t, store, compID, teams, bracketMatchWith(teams, representativeRow(teams)))

	out, err := eng.RepresentativeBoutMatches(compID)
	require.NoError(t, err)
	require.Len(t, out, 1)
	bout := out[0].Bouts[0]
	assert.Empty(t, bout.SideAName)
	assert.Empty(t, bout.SideALabel)
	assert.Empty(t, bout.SideBName)
	assert.Empty(t, bout.SideBLabel)
	assert.Equal(t, "(DH)", bout.Middle, "the bout is still listed, with its centre")
}

func TestRepresentativeBoutMatches_NoneWithoutARepresentativeBout(t *testing.T) {
	t.Run("a match with numbered bouts only", func(t *testing.T) {
		compID := "representative-none-numbered"
		eng, store, teams := setupRepresentativeComp(t, compID)
		saveBracketOf(t, store, compID, teams, bracketMatchWith(teams, numberedBout(teams)))

		out, err := eng.RepresentativeBoutMatches(compID)
		require.NoError(t, err)
		assert.Empty(t, out)
	})
	t.Run("a draw with no matches fought", func(t *testing.T) {
		compID := "representative-none-undrawn"
		eng, _, _ := setupRepresentativeComp(t, compID)

		out, err := eng.RepresentativeBoutMatches(compID)
		require.NoError(t, err)
		assert.Empty(t, out)
	})
	t.Run("an individual competition", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "representative-none-individual"
		createTestCompetition(t, store, compID, "mixed", 3)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})

		out, err := eng.RepresentativeBoutMatches(compID)
		require.NoError(t, err)
		assert.Empty(t, out)
	})
}

// A kachinuki encounter has no representative bout (AddDaihyosen refuses it),
// so a stray position -1 row there is not listed; the Kachinuki Detail sheet
// owns that competition's bouts.
func TestRepresentativeBoutMatches_NoneForKachinuki(t *testing.T) {
	compID := "representative-none-kachinuki"
	eng, store, _ := setupKachinukiComp(t, compID, 3, func(c *state.Competition) {
		c.Format = state.CompFormatKnockout
		c.Status = state.CompStatusKnockout
		c.NumberPrefix = "T"
	})
	teams := representativeTeams{redID: helper.NewUUID4(), whiteID: helper.NewUUID4()}
	teams.red = []domain.TeamMember{{ID: "m1"}, {ID: "m2"}, {ID: "m3"}}
	teams.white = []domain.TeamMember{{ID: "w1"}, {ID: "w2"}}
	saveBracketOf(t, store, compID, teams, bracketMatchWith(teams, representativeRow(teams)))

	out, err := eng.RepresentativeBoutMatches(compID)
	require.NoError(t, err)
	assert.Empty(t, out)
}

func TestRepresentativeBoutMatches_UnknownOrInvalidCompetition(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	out, err := eng.RepresentativeBoutMatches("does-not-exist")
	assert.NoError(t, err, "an unknown competition lists nothing, as the Kachinuki Detail read does")
	assert.Nil(t, out)

	out, err = eng.RepresentativeBoutMatches("../evil")
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "invalid competition ID")
}

// Pool rows come first (a row the Pool Matches grid has no block for is titled
// by its operator label), then the bracket in match-number order, not storage
// order, then the 3rd-place match: the Kachinuki Detail sheet's own order.
func TestRepresentativeBoutMatches_ListedInTheKachinukiDetailOrder(t *testing.T) {
	compID := "representative-order"
	eng, store, teams := setupRepresentativeComp(t, compID)
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)

	poolRow := state.MatchResult{
		ID: "Pool A-0", SideA: "RedTeam", SideAID: teams.redID, SideB: "WhiteTeam", SideBID: teams.whiteID,
		Status: state.MatchStatusCompleted, SubResults: []state.SubMatchResult{representativeRow(teams)},
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{poolRow}))

	second := bracketMatchWith(teams, representativeRow(teams))
	second.ID, second.MatchNumber = "SF2", 2
	first := bracketMatchWith(teams, representativeRow(teams))
	third := bracketMatchWith(teams, representativeRow(teams))
	third.ID = "BR"
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		DrawOrder:       []string{teams.redID, teams.whiteID},
		Rounds:          [][]state.BracketMatch{{second, first}},
		ThirdPlaceMatch: &third,
	}))

	out, err := eng.RepresentativeBoutMatches(compID)
	require.NoError(t, err)
	labels := make([]string, len(out))
	for i, s := range out {
		labels[i] = s.Label
	}
	assert.Equal(t, []string{
		OperatorMatchLabel(comp, nil, "Pool A-0"),
		helper.EliminationMatchTitle(1, 1),
		helper.EliminationMatchTitle(1, 2),
		helper.ThirdPlaceLabel,
	}, labels)
}

// The stored-draw export carries the sheet too (the results workbook's twin is
// in internal/export): a real drawn team knockout, one semi-final decided by a
// representative bout.
func TestExportCompetitionXlsx_RepresentativeBoutsSheet(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "export-representative-bouts"
	createTestCompetition(t, store, compID, state.CompFormatKnockout, 0, func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = 3
		c.TeamMatchType = state.TeamMatchTypeFixed
		c.NumberPrefix = "T"
	})
	saveTestParticipants(t, store, compID, []string{"Ryu", "Tora", "Kame", "Taka"})
	require.NoError(t, eng.StartCompetition(compID))

	t.Run("none before a representative bout exists", func(t *testing.T) {
		f := openExportedWorkbook(t, eng, compID)
		assert.NotContains(t, f.GetSheetList(), helper.SheetRepresentativeBouts)
	})

	bracket, err := store.LoadBracket(compID)
	require.NoError(t, err)
	semi := bracket.Rounds[0][0]
	require.NotEmpty(t, semi.SideAID)
	require.NotEmpty(t, semi.SideBID)
	a, err := store.AddTeamMember(compID, semi.SideAID, "Hiro")
	require.NoError(t, err)
	b, err := store.AddTeamMember(compID, semi.SideBID, "Sayaka")
	require.NoError(t, err)
	found, err := store.UpdateBracketMatchByID(compID, semi.ID, func(bm *state.BracketMatch) {
		bm.Status = state.MatchStatusCompleted
		bm.Winner = bm.SideA
		bm.SubResults = []state.SubMatchResult{{
			Position: state.DaihyosenSubPosition, SideA: bm.SideA, SideB: bm.SideB,
			SideAMemberID: a.ID, SideBMemberID: b.ID, Winner: bm.SideA,
			Decision: string(domain.DecisionDaihyosen), IpponsA: []string{domain.HanteiMark},
		}}
	})
	require.NoError(t, err)
	require.True(t, found)

	f := openExportedWorkbook(t, eng, compID)
	require.Contains(t, f.GetSheetList(), helper.SheetRepresentativeBouts)
	assert.NotContains(t, f.GetSheetList(), helper.SheetKachinukiDetail, "a fixed-order competition has no Kachinuki Detail sheet")

	rows, err := f.GetRows(helper.SheetRepresentativeBouts)
	require.NoError(t, err)
	var flat []string
	for _, row := range rows {
		flat = append(flat, strings.Join(row, "|"))
	}
	text := strings.Join(flat, "\n")
	assert.Contains(t, text, "(Representative bout)")
	assert.Contains(t, text, "DH ", "the bout's number cell reads DH")
	assert.Contains(t, text, "Hiro")
	assert.Contains(t, text, "Sayaka")
	assert.Contains(t, text, "(DH)")
	assert.Contains(t, text, "Ht")
}
