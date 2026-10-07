package engine

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test/idstamp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"
	"gopkg.in/yaml.v3"
)

// TestLineupKey verifies the composite key is stable and uses the
// null-byte separator so a team named "A\x00B" can't collide with
// team "A" + player "B\x00anything".
func TestLineupKey(t *testing.T) {
	k1 := lineupKey("TeamA", "Alice")
	k2 := lineupKey("TeamA", "Bob")
	k3 := lineupKey("TeamB", "Alice")

	assert.NotEqual(t, k1, k2, "different players on same team must produce different keys")
	assert.NotEqual(t, k1, k3, "same player on different teams must produce different keys")
	assert.Equal(t, k1, lineupKey("TeamA", "Alice"), "key must be deterministic")
}

// TestBuildKachinukiDetail verifies the full conversion: sub-results
// become Bouts and top-level fields are copied.
func TestBuildKachinukiDetail(t *testing.T) {
	positions := map[string]string{
		lineupKey("RedTeam", "R-Senpo"):   "Senpo",
		lineupKey("WhiteTeam", "W-Senpo"): "Senpo",
	}

	m := &state.MatchResult{
		SideA:  "RedTeam",
		SideB:  "WhiteTeam",
		Winner: "RedTeam",
		Status: state.MatchStatusCompleted,
		SubResults: []state.SubMatchResult{
			{
				Position: 1,
				SideA:    "R-Senpo",
				SideB:    "W-Senpo",
				IpponsA:  []string{"M", "K"},
				IpponsB:  []string{},
				Winner:   "R-Senpo",
				Decision: "fought",
			},
		},
	}

	detail := buildKachinukiDetail(m, "Pool Match 1", positions, map[string]string{}, map[string][]domain.TeamMember{})

	assert.Equal(t, "Pool Match 1", detail.Label)
	assert.Equal(t, "RedTeam", detail.SideATeam)
	assert.Equal(t, "WhiteTeam", detail.SideBTeam)
	require.Len(t, detail.Bouts, 1)
	assert.Equal(t, 1, detail.Bouts[0].Position)
	assert.Equal(t, "R-Senpo", detail.Bouts[0].SideAName)
	assert.Equal(t, "Senpo", detail.Bouts[0].SideAPos)
	assert.Equal(t, "MK", detail.Bouts[0].ScoreA)
	assert.Equal(t, "", detail.Bouts[0].ScoreB)
	assert.Equal(t, "W-Senpo", detail.Bouts[0].SideBName)
	assert.Equal(t, "Senpo", detail.Bouts[0].SideBPos)
	// A fought bout with no encho and no hantei carries no middle mark and
	// no side mark.
	assert.Equal(t, "", detail.Bouts[0].Middle)
	assert.Equal(t, "", detail.Bouts[0].MarkA)
	assert.Equal(t, "", detail.Bouts[0].MarkB)
}

// TestBuildKachinukiDetail_FusenshoMarksTheWinnerBesideItsScore pins the
// engine half of the per-bout default-win rule (bc-kdsc change 8b): a
// per-bout fusensho (the exhaustion walkover's default win) sets MarkA on
// the present side alone -- domain.SideMarksAB attributes it by the bout's
// own SideA/SideB/Winner -- leaves MarkB empty, and leaves Middle untouched
// (a default win is not a middle-mark decision; the closed set stays vs).
func TestBuildKachinukiDetail_FusenshoMarksTheWinnerBesideItsScore(t *testing.T) {
	m := &state.MatchResult{
		SideA: "RedTeam",
		SideB: "WhiteTeam",
		SubResults: []state.SubMatchResult{
			{
				Position: 1,
				SideA:    "R-Senpo", SideB: "W-Senpo",
				IpponsA:  domain.DefaultWinIppons(false),
				Winner:   "R-Senpo",
				Decision: "fusensho",
			},
		},
	}

	detail := buildKachinukiDetail(m, "Pool Match 1", map[string]string{}, map[string]string{}, map[string][]domain.TeamMember{})

	require.Len(t, detail.Bouts, 1)
	assert.Equal(t, "Fus.", detail.Bouts[0].MarkA, "the present side's own mark names the default win")
	assert.Equal(t, "", detail.Bouts[0].MarkB, "fusensho marks only the winner, never the no-show's own cell")
	assert.Equal(t, "", detail.Bouts[0].Middle, "a default win is not a middle-mark decision")
	assert.Equal(t, "○○", detail.Bouts[0].ScoreA, "the FIK default-win maru, joined from the stored ippons")
}

// TestBuildKachinukiDetail_FusenshoEmptyIpponsGetsMaruFallback pins that a
// per-bout fusensho whose recorded ippons are EMPTY (legacy or pre-fill data,
// not the engine's own maru fill as in the test above) still gets the FIK
// default-win maru (domain.DefaultWinMaruAB) on the winner's score, as the
// main sheets print it (TestScoreCellsCarryOutstandingHansokuTriangle in
// internal/export, a matching literal fixture since export imports engine).
func TestBuildKachinukiDetail_FusenshoEmptyIpponsGetsMaruFallback(t *testing.T) {
	m := &state.MatchResult{
		SideA: "RedTeam",
		SideB: "WhiteTeam",
		SubResults: []state.SubMatchResult{
			{
				Position: 1,
				SideA:    "R-Senpo", SideB: "W-Senpo",
				Winner:   "R-Senpo",
				Decision: "fusensho",
				// IpponsA/IpponsB left nil on purpose: this is the empty-cell
				// gap, not the already-covered stored-maru case above.
			},
		},
	}

	detail := buildKachinukiDetail(m, "Pool Match 1", map[string]string{}, map[string]string{}, map[string][]domain.TeamMember{})

	require.Len(t, detail.Bouts, 1)
	assert.Equal(t, "○○", detail.Bouts[0].ScoreA, "an empty recorded score still gets the FIK maru fallback")
	assert.Equal(t, "", detail.Bouts[0].ScoreB)
	assert.Equal(t, "Fus.", detail.Bouts[0].MarkA)
	assert.Equal(t, "", detail.Bouts[0].MarkB)
}

// TestBuildKachinukiDetail_FixedOrderNamelessFusenshoAppliesMaruFallback
// pins that a bout row naming no fighter of its own (SideA/SideB empty,
// Winner the TEAM name) is attributed through the encounter's team names
// (domain.SubBoutAttributionForTeamRow), so it carries its mark and maru as
// the main sheets print the identical fixture in
// TestScoreCellsCarryOutstandingHansokuTriangle (internal/export).
func TestBuildKachinukiDetail_FixedOrderNamelessFusenshoAppliesMaruFallback(t *testing.T) {
	m := &state.MatchResult{
		SideA: "Tora A",
		SideB: "Kenshi B",
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "", SideB: "", Winner: "Tora A", Decision: "fusensho"},
		},
	}

	detail := buildKachinukiDetail(m, "Pool Match 1", map[string]string{}, map[string]string{}, map[string][]domain.TeamMember{})

	require.Len(t, detail.Bouts, 1)
	// Tora A is SideA, matching the builder_test.go fixture's own comment:
	// "The winner is SideA (Aka)".
	assert.Equal(t, "Fus.", detail.Bouts[0].MarkA, "the present side's own mark names the default win")
	assert.Equal(t, "", detail.Bouts[0].MarkB)
	assert.Equal(t, "○○", detail.Bouts[0].ScoreA, "an empty recorded score still gets the FIK maru fallback")
	assert.Equal(t, "", detail.Bouts[0].ScoreB)
}

// TestBuildKachinukiDetail_NoPositions verifies graceful handling when
// the position map is empty (positions render as empty strings).
func TestBuildKachinukiDetail_NoPositions(t *testing.T) {
	m := &state.MatchResult{
		SideA: "A",
		SideB: "B",
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "A1", SideB: "B1", Winner: "A1"},
		},
	}
	detail := buildKachinukiDetail(m, "label", map[string]string{}, map[string]string{}, map[string][]domain.TeamMember{})
	require.Len(t, detail.Bouts, 1)
	assert.Empty(t, detail.Bouts[0].SideAPos)
	assert.Empty(t, detail.Bouts[0].SideBPos)
}

// TestCollectKachinukiMatches_NilComp verifies that a nil competition
// or wrong TeamMatchType returns nil without error.
func TestCollectKachinukiMatches_NilComp(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	out, err := eng.collectKachinukiMatches("any-comp", nil)
	assert.NoError(t, err)
	assert.Nil(t, out)

	// Non-kachinuki type.
	comp := &state.Competition{TeamMatchType: state.TeamMatchTypeFixed, TeamSize: 5}
	out, err = eng.collectKachinukiMatches("any-comp", comp)
	assert.NoError(t, err)
	assert.Nil(t, out)
}

// saveKachinukiPool saves one pools.csv pool holding teams, returning each
// team's participant id: a pool section is listed from the grid, which
// resolves a stored row's sides by id.
func saveKachinukiPool(t *testing.T, store *state.Store, compID, poolName string, teams ...string) map[string]string {
	t.Helper()
	ids := make(map[string]string, len(teams))
	players := make([]helper.Player, len(teams))
	for i, team := range teams {
		ids[team] = bctest.StampPlayerID(team, "Dojo")
		players[i] = helper.Player{ID: ids[team], Name: team, Dojo: "Dojo"}
	}
	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	require.NoError(t, store.SavePools(compID, append(pools, helper.Pool{PoolName: poolName, Players: players})))
	return ids
}

// TestCollectKachinukiMatches_PoolMatchesWithBouts verifies that a pool
// match with sub-results lists exactly those bouts, a pool match with none
// gets 2*teamSize-1 empty rows for hand entry (operator decision 2026-09-27,
// bc-kdsc), and a tie-break or daihyosen row has a section only once it has
// bouts, titled by its operator label since it is not a match of the draw.
func TestCollectKachinukiMatches_PoolMatchesWithBouts(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-collect"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		Name:          "Kachinuki Collect",
		Format:        state.CompFormatMixed,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Status:        state.CompStatusPools,
	}))
	ids := saveKachinukiPool(t, store, compID, "Pool A", "RedTeam", "WhiteTeam", "AlphaTeam", "BetaTeam")

	matches := []state.MatchResult{
		{
			ID:      "Pool A-0",
			SideA:   "RedTeam",
			SideB:   "WhiteTeam",
			SideAID: ids["RedTeam"],
			SideBID: ids["WhiteTeam"],
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R1", SideB: "W1", Winner: "R1", Decision: "fought"},
			},
		},
		{
			// No sub-results: empty rows for hand entry.
			ID:      "Pool A-1",
			SideA:   "AlphaTeam",
			SideB:   "BetaTeam",
			SideAID: ids["AlphaTeam"],
			SideBID: ids["BetaTeam"],
		},
		// Supplementary rows: skipped while empty, listed once fought.
		{ID: "Pool A-TB-0", SideA: "AlphaTeam", SideB: "RedTeam"},
		{ID: "Pool A-DH-0", SideA: "AlphaTeam", SideB: "RedTeam"},
		{
			ID:    "Pool B-TB-0",
			SideA: "BetaTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "B1", SideB: "W1", Winner: "B1", Decision: "fought"},
			},
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	comp := &state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}
	out, err := eng.collectKachinukiMatches(compID, comp)
	require.NoError(t, err)
	require.Len(t, out, 3, "both draw matches and the fought tie-break, never an empty supplementary row")

	assert.Equal(t, "Pool Match 1", out[0].Label)
	assert.Equal(t, "RedTeam", out[0].SideATeam)
	assert.Len(t, out[0].Bouts, 1)
	assert.Zero(t, out[0].BlankBoutRows, "a match with bouts lists exactly those")

	assert.Equal(t, "Pool Match 2", out[1].Label)
	assert.Equal(t, "AlphaTeam", out[1].SideATeam)
	assert.Equal(t, "BetaTeam", out[1].SideBTeam)
	assert.Empty(t, out[1].Bouts)
	assert.Equal(t, 9, out[1].BlankBoutRows, "a match with no bouts gets 2*5-1 empty rows")

	assert.Equal(t, "Pool B tiebreaker", out[2].Label, "a supplementary row takes no draw number")
	assert.Len(t, out[2].Bouts, 1)
}

// TestKachinukiPositions_StartingLineup verifies that a saved team lineup is
// mapped to the (team, player) → position lookup of a match that has none of
// its own.
func TestKachinukiPositions_StartingLineup(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pos-map-comp"

	comp := &state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}
	require.NoError(t, store.SaveCompetition(comp))

	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "red-id",
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "R-Senpo",
			domain.PosJiho:    "R-Jiho",
			domain.PosChuken:  "R-Chuken",
			domain.PosFukusho: "R-Fukusho",
			domain.PosTaisho:  "R-Taisho",
		},
	}, 5))

	posMap := eng.lineupRuleOrNone("test", compID, true, nil, nil).positionsForMatch(
		&state.MatchResult{ID: "P1-0", SideA: "RedTeam", SideAID: "red-id", SideB: "WhiteTeam", SideBID: "white-id"})

	assert.Equal(t, "Senpo", posMap[lineupKey("RedTeam", "R-Senpo")])
	assert.Equal(t, "Jiho", posMap[lineupKey("RedTeam", "R-Jiho")])
	assert.Empty(t, posMap[lineupKey("WhiteTeam", "R-Senpo")], "a team's lineup labels only that team's fighters")
}

// TestKachinukiPositions_ParticipantIDKeyed verifies that lineups saved by the
// UI (TeamID = team participant id, a UUID) resolve position labels for a match
// side that carries the team's id, and that a side carrying only the team's
// display NAME has none: a team is its id. The lookup is keyed by the side's
// name, which buildKachinukiDetail is called with (m.SideA/m.SideB).
func TestKachinukiPositions_ParticipantIDKeyed(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pos-map-pid-keyed"

	comp := &state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}
	require.NoError(t, store.SaveCompetition(comp))

	redID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "DojoR"},
	}))

	// Round-scoped lineup keyed by the participant id (UI shape).
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: redID,
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "R-Senpo",
			domain.PosJiho:  "R-Jiho",
		},
	}, 5))
	// Match-scoped lineup keyed by the participant id (mp-825 UI shape).
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID:  redID,
		MatchID: "SF-1",
		Positions: map[domain.Position]string{
			domain.PosSenpo: "R-Sub",
		},
	}, 5))

	// Older data stored under the team's NAME: not the team's lineup.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID:    "RedTeam",
		Positions: map[domain.Position]string{domain.PosSenpo: "R-ByName"},
	}, 5))

	rule := eng.lineupRuleOrNone("test", compID, true, nil, nil)

	t.Run("by id", func(t *testing.T) {
		posMap := rule.positionsForMatch(&state.MatchResult{ID: "SF-1", SideA: "RedTeam", SideAID: redID})
		assert.Equal(t, "Senpo", resolveKachinukiPosition(posMap, "RedTeam", "R-Sub"), "the match's own lineup")
		assert.Empty(t, resolveKachinukiPosition(posMap, "RedTeam", "R-Jiho"), "the own lineup replaces the starting one")
	})

	t.Run("a side with only the team's name has no lineup", func(t *testing.T) {
		posMap := rule.positionsForMatch(&state.MatchResult{ID: "SF-1", SideA: "RedTeam"})
		assert.Empty(t, posMap, "the roster knows the name, and a lineup stored under it is not the team's either")
	})

	other := rule.positionsForMatch(&state.MatchResult{ID: "SF-2", SideA: "RedTeam", SideAID: redID})
	assert.Equal(t, "Jiho", resolveKachinukiPosition(other, "RedTeam", "R-Jiho"), "a match with no lineup of its own falls back to the starting lineup")
}

// TestKachinukiDetailMatches_UnreadableLineupsLabelNothingAndSayWhy pins that a
// lineups.yaml that cannot be read leaves the export's positions empty rather
// than failing it, and that the failure is logged, naming the file, instead of
// vanishing.
func TestKachinukiDetailMatches_UnreadableLineupsLabelNothingAndSayWhy(t *testing.T) {
	eng, store, comp := setupKachinukiComp(t, "kx-unreadable-lineups", 3, func(c *state.Competition) { c.Format = state.CompFormatMixed })
	require.NoError(t, os.WriteFile(
		filepath.Join(store.GetFolder(), "competitions", comp.ID, "lineups.yaml"),
		[]byte("lineups: [this is: not: valid yaml"), 0o600))
	require.NoError(t, store.SavePoolMatches(comp.ID, []state.MatchResult{{
		ID: "P1-0", SideA: "RedTeam", SideAID: "red-id", SideB: "WhiteTeam", SideBID: "white-id",
		SubResults: []state.SubMatchResult{{Position: 1, SideA: "R-1", SideB: "W-1", Winner: "R-1", Decision: "fought"}},
	}}))

	var out []helper.KachinukiMatchDetail
	var err error
	logged := captureLog(t, func() { out, err = eng.KachinukiDetailMatches(comp.ID) })

	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Bouts, 1)
	assert.Empty(t, out[0].Bouts[0].SideAPos)
	assert.Contains(t, logged, "lineups.yaml", "the failure is logged, naming the file")
}

// TestCollectKachinukiMatches_WithBracketStub verifies that a bracket match
// with no SubResults, even one a kachinuki-exhaustion decision closed, gets
// 2*teamSize-1 empty rows for hand entry rather than being skipped.
func TestCollectKachinukiMatches_WithBracketStub(t *testing.T) {
	compID := "kachinuki-bracket-stub"
	eng, store, comp := setupKachinukiComp(t, compID, 5)

	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:       "B1",
					SideA:    "RedTeam",
					SideB:    "WhiteTeam",
					Winner:   "RedTeam",
					Decision: string(domain.DecisionKachinukiExhaustion),
				},
			},
		},
	}))

	out, err := eng.collectKachinukiMatches(compID, comp)
	require.NoError(t, err)
	require.Len(t, out, 1, "a bracket match with no bouts still has a section")
	assert.Equal(t, "Bracket R1-M1", out[0].Label)
	assert.Equal(t, "RedTeam", out[0].SideATeam)
	assert.Equal(t, "WhiteTeam", out[0].SideBTeam)
	assert.Empty(t, out[0].Bouts)
	assert.Equal(t, 9, out[0].BlankBoutRows)
}

// TestCollectKachinukiMatches_BracketWithSubResults verifies that a bracket
// match carrying real SubResults (from MaybeAdvanceKachinuki) produces a
// full detail entry with per-bout rows, not a stub.
func TestCollectKachinukiMatches_BracketWithSubResults(t *testing.T) {
	compID := "kachinuki-bracket-subs"
	eng, store, comp := setupKachinukiComp(t, compID, 5)

	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:     "SF1",
					SideA:  "RedTeam",
					SideB:  "WhiteTeam",
					Winner: "RedTeam",
					Status: state.MatchStatusCompleted,
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "R-Senpo", SideB: "W-Senpo", Winner: "R-Senpo", Decision: "fought"},
						{Position: 2, SideA: "R-Senpo", SideB: "W-Jiho", Winner: "W-Jiho", Decision: "fought"},
						{Position: 3, SideA: "R-Jiho", SideB: "W-Jiho", Winner: "R-Jiho", Decision: "fought"},
					},
				},
			},
		},
	}))

	out, err := eng.collectKachinukiMatches(compID, comp)
	require.NoError(t, err)
	require.Len(t, out, 1, "bracket match with 3 bouts should produce one detail entry")
	assert.Equal(t, "Bracket R1-M1", out[0].Label)
	assert.Equal(t, "RedTeam", out[0].SideATeam)
	assert.Equal(t, "WhiteTeam", out[0].SideBTeam)
	require.Len(t, out[0].Bouts, 3, "three bouts should be present")
	assert.Equal(t, 1, out[0].Bouts[0].Position)
	assert.Equal(t, "R-Senpo", out[0].Bouts[0].SideAName)
	assert.Equal(t, "W-Senpo", out[0].Bouts[0].SideBName)
	assert.Equal(t, 3, out[0].Bouts[2].Position)
}

// TestCollectKachinukiMatches_BronzeWithSubResults verifies that the
// ThirdPlaceMatch sibling of bracket.Rounds is collected when it carries
// real SubResults.
func TestCollectKachinukiMatches_BronzeWithSubResults(t *testing.T) {
	compID := "kachinuki-bronze-subs"
	eng, store, comp := setupKachinukiComp(t, compID, 5, func(c *state.Competition) { c.Naginata = true })

	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:     "m-bronze",
			SideA:  "RedTeam",
			SideB:  "BlueTeam",
			Winner: "BlueTeam",
			Status: state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R1", SideB: "B1", Winner: "B1", Decision: "fought"},
				{Position: 2, SideA: "R2", SideB: "B1", Winner: "B1", Decision: "fought"},
			},
		},
	}))

	out, err := eng.collectKachinukiMatches(compID, comp)
	require.NoError(t, err)
	require.Len(t, out, 1, "bronze match with 2 bouts should produce one detail entry")
	assert.Equal(t, helper.ThirdPlaceLabel, out[0].Label)
	require.Len(t, out[0].Bouts, 2)
	assert.Equal(t, "R1", out[0].Bouts[0].SideAName)
	assert.Equal(t, "B1", out[0].Bouts[0].SideBName)
	// A fought bout carries no middle or side mark.
	assert.Equal(t, "", out[0].Bouts[0].Middle)
	assert.Equal(t, "", out[0].Bouts[0].MarkA)
	assert.Equal(t, "", out[0].Bouts[0].MarkB)
}

// TestCollectKachinukiMatches_BronzeStub verifies the Naginata 3rd-place
// (bronze) match — a sibling of bracket.Rounds — is considered by the export
// at parity with Rounds matches: with no bouts it gets empty rows, and a
// bracket with no rounds to find the final in does not stop it.
func TestCollectKachinukiMatches_BronzeStub(t *testing.T) {
	compID := "kachinuki-bronze-stub"
	eng, store, comp := setupKachinukiComp(t, compID, 5, func(c *state.Competition) { c.Naginata = true })

	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:       "m-bronze",
			SideA:    "RedTeam",
			SideB:    "WhiteTeam",
			Winner:   "RedTeam",
			Decision: string(domain.DecisionKachinukiExhaustion),
		},
	}))

	out, err := eng.collectKachinukiMatches(compID, comp)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, helper.ThirdPlaceLabel, out[0].Label)
	assert.Equal(t, "RedTeam", out[0].SideATeam)
	assert.Equal(t, "WhiteTeam", out[0].SideBTeam)
	assert.Empty(t, out[0].Bouts)
	assert.Equal(t, 9, out[0].BlankBoutRows)
}

// TestCollectKachinukiMatches_UnfoughtDrawCoversEveryMatch pins the hand-entry
// rule on real draws (operator decision 2026-09-27, bc-kdsc): before any bout
// is recorded, every match of the draw has a section of 2*teamSize-1 empty
// rows -- each pool match, each numbered bracket match including the later
// rounds whose sides are not known yet, and the 3rd-place match -- while a
// bye has none. A side an earlier match decides is named the way the
// Elimination Matches sheet prints it, "M <n>"; a pool placeholder as it
// stands.
func TestCollectKachinukiMatches_UnfoughtDrawCoversEveryMatch(t *testing.T) {
	singleThird := false
	cases := []struct {
		name     string
		format   string
		teams    []string
		wantPool int  // pool-match sections, listed before the bracket's
		wantByes bool // the fixture must include a bye for the exclusion to be tested
	}{
		{name: "knockout of five, three byes", format: state.CompFormatKnockout,
			teams: []string{"Ryu", "Tora", "Kame", "Taka", "Kuma"}, wantByes: true},
		{name: "pools then knockout", format: state.CompFormatMixed,
			teams: []string{"Ryu", "Tora", "Kame", "Taka", "Kuma", "Hebi"}, wantPool: 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			eng, store, _ := setupTestEngine(t)
			compID := "kachinuki-blank-draw"
			createTestCompetition(t, store, compID, tc.format, 3, func(c *state.Competition) {
				c.Kind = "team"
				c.TeamSize = 3
				c.TeamMatchType = state.TeamMatchTypeKachinuki
				c.TwoThirdPlaces = &singleThird
			})
			saveTestParticipants(t, store, compID, tc.teams)
			require.NoError(t, eng.StartCompetition(compID))

			bracket, err := store.LoadBracket(compID)
			require.NoError(t, err)
			require.NotNil(t, bracket.ThirdPlaceMatch, "the fixture must draw a 3rd-place match")
			numbered, byes := 0, 0
			for _, round := range bracket.Rounds {
				for _, bm := range round {
					if bm.MatchNumber > 0 {
						numbered++
					} else {
						byes++
					}
				}
			}
			if tc.wantByes {
				require.Positive(t, byes, "the fixture must include a bye")
			}

			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			out, err := eng.collectKachinukiMatches(compID, comp)
			require.NoError(t, err)
			require.Len(t, out, tc.wantPool+numbered+1, "every pool match, every numbered bracket match and the 3rd-place match; no bye")

			var sides []string
			for _, d := range out {
				assert.Empty(t, d.Bouts, d.Label)
				assert.Equal(t, 5, d.BlankBoutRows, "%s: 2*3-1 empty rows", d.Label)
				for _, side := range []string{d.SideATeam, d.SideBTeam} {
					assert.NotEmpty(t, side, "%s: every side is named", d.Label)
					assert.NotContains(t, side, "Winner of", "%s: the stored placeholder is never printed", d.Label)
					sides = append(sides, side)
				}
			}
			for _, d := range out[:tc.wantPool] {
				assert.Contains(t, d.Label, "Pool Match ")
			}
			if tc.wantPool > 0 {
				first := out[tc.wantPool]
				assert.True(t, helper.IsPoolFinalistPlaceholder(first.SideATeam), first.SideATeam)
				assert.True(t, helper.IsPoolFinalistPlaceholder(first.SideBTeam), first.SideBTeam)
			} else {
				assert.Contains(t, sides, helper.MatchRefLabel(1), "the semi-final fed by the first-round bout names it")
			}

			// Each bracket section carries the title of its block on the
			// Elimination Matches sheet, so a side reading "M n" leads to the
			// section titled with match n.
			data, err := eng.ExportCompetitionXlsx(compID)
			require.NoError(t, err)
			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err)
			defer func() { require.NoError(t, f.Close()) }()
			rows, err := f.GetRows(helper.SheetEliminationMatches)
			require.NoError(t, err)
			var sheetTitles, sectionTitles []string
			for _, row := range rows {
				if len(row) > 0 && (strings.HasPrefix(row[0], "Round ") || row[0] == helper.ThirdPlaceLabel) {
					sheetTitles = append(sheetTitles, row[0])
				}
			}
			titled := map[int]bool{}
			for _, d := range out[tc.wantPool:] {
				sectionTitles = append(sectionTitles, d.Label)
				var round, number int
				if _, err := fmt.Sscanf(d.Label, helper.EliminationMatchTitleFormat, &round, &number); err == nil {
					titled[number] = true
				}
			}
			assert.Equal(t, sheetTitles, sectionTitles, "each bracket section is titled as its Elimination Matches block, in the order the sheet prints them")
			for _, side := range sides {
				var number int
				if _, err := fmt.Sscanf(side, "M %d", &number); err == nil {
					assert.True(t, titled[number], "side %q leads to a section titled with match %d", side, number)
				}
			}

			final, bronze := out[len(out)-2], out[len(out)-1]
			assert.Equal(t, helper.ThirdPlaceLabel, bronze.Label)
			require.GreaterOrEqual(t, len(sheetTitles), 2)
			assert.Equal(t, sheetTitles[len(sheetTitles)-2], final.Label, "the final carries the title of the last block before the 3rd place")
			assert.ElementsMatch(t,
				[]string{helper.MatchRefLabel(numbered - 2), helper.MatchRefLabel(numbered - 1)},
				[]string{final.SideATeam, final.SideBTeam},
				"the final's sides are the two semi-finals, by number")
			assert.Equal(t,
				[]string{final.SideATeam, final.SideBTeam},
				[]string{bronze.SideATeam, bronze.SideBTeam},
				"the 3rd-place sides are the same semi-finals, side for side")
		})
	}
}

// Before the draw, a knockout-only competition's workbook prints the bracket
// skeleton seeded from the roster on the Elimination Matches sheet. Its
// Kachinuki Detail sheet lists the same matches, from the same skeleton, each
// with its empty bout rows for hand entry: there is no stored bracket yet for
// collectKachinukiMatches to read, and the sheet used to be left out.
func TestExportBeforeTheDrawListsTheSkeletonOnTheKachinukiDetail(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-before-the-draw"
	createTestCompetition(t, store, compID, state.CompFormatKnockout, 3, func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = 3
		c.TeamMatchType = state.TeamMatchTypeKachinuki
	})
	saveTestParticipants(t, store, compID, []string{"Ryu", "Tora", "Kame", "Taka", "Kuma", "Hebi"})

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()

	elim, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	var blocks []string
	for _, row := range elim {
		if len(row) > 0 && strings.HasPrefix(row[0], "Round ") {
			blocks = append(blocks, row[0])
		}
	}
	require.NotEmpty(t, blocks, "the skeleton is printed on the Elimination Matches sheet")

	idx, err := f.GetSheetIndex(helper.SheetKachinukiDetail)
	require.NoError(t, err)
	require.NotEqual(t, -1, idx, "the workbook has a Kachinuki Detail sheet")
	detail, err := f.GetRows(helper.SheetKachinukiDetail)
	require.NoError(t, err)
	var sections []string
	for r, row := range detail {
		if len(row) == 0 {
			continue
		}
		title, ok := strings.CutSuffix(row[0], " (Kachinuki)")
		if !ok {
			continue
		}
		sections = append(sections, title)
		bouts := 0
		for b := r + 3; b < len(detail) && len(detail[b]) > 0 && detail[b][0] == fmt.Sprint(bouts+1); b++ {
			bouts++
		}
		assert.Equal(t, 5, bouts, "%s: 2*3-1 empty bout rows", title)
	}
	assert.Equal(t, blocks, sections, "one section per printed block, titled as it and in its order")
}

// The skeleton stands in for the stored bracket only where there is no pool
// phase. A competition with pools keeps the sections its bout-log read lists,
// its pool matches included, even with a bracket that carries no rounds.
func TestExportKeepsAMixedCompetitionsPoolSectionsOnTheKachinukiDetail(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-mixed-no-rounds"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = 3
		c.TeamMatchType = state.TeamMatchTypeKachinuki
	})
	saveTestParticipants(t, store, compID, []string{"Ryu", "Tora", "Kame", "Taka", "Kuma", "Hebi"})
	require.NoError(t, eng.StartCompetition(compID))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{}))

	data, err := eng.ExportCompetitionXlsx(compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer func() { require.NoError(t, f.Close()) }()
	detail, err := f.GetRows(helper.SheetKachinukiDetail)
	require.NoError(t, err)
	pools := 0
	for _, row := range detail {
		if len(row) > 0 && strings.HasPrefix(row[0], "Pool Match ") && strings.HasSuffix(row[0], " (Kachinuki)") {
			pools++
		}
	}
	assert.Equal(t, 6, pools, "the six pool matches keep their sections")
}

// TestCollectKachinukiMatches_MoreBoutsThanTheBlockKeepsThemAll pins that an
// encounter that fielded reserves past 2*teamSize-1 bouts still lists every
// bout on the detail sheet; only its main-sheet block stops at that count.
func TestCollectKachinukiMatches_MoreBoutsThanTheBlockKeepsThemAll(t *testing.T) {
	compID := "kachinuki-reserves"
	eng, store, comp := setupKachinukiComp(t, compID, 2)

	subs := make([]state.SubMatchResult, 5)
	for i := range subs {
		subs[i] = state.SubMatchResult{Position: i + 1, SideA: "R" + string(rune('1'+i)), SideB: "W1", Winner: "W1", Decision: "fought"}
	}
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{ID: "Pool A-0", SideA: "RedTeam", SideB: "WhiteTeam", SubResults: subs},
	}))

	out, err := eng.collectKachinukiMatches(compID, comp)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Bouts, 5, "all five bouts, past the block's 2*2-1 = 3")
	assert.Equal(t, 5, out[0].Bouts[4].Position)
	assert.Zero(t, out[0].BlankBoutRows)
}

// TestResolveKachinukiPosition verifies the lookup is keyed by team AND player:
// the two teams of a match may field players with the same name.
func TestResolveKachinukiPosition(t *testing.T) {
	positions := map[string]string{lineupKey("TeamA", "alice"): "Senpo"}

	assert.Equal(t, "Senpo", resolveKachinukiPosition(positions, "TeamA", "alice"))
	assert.Equal(t, "", resolveKachinukiPosition(positions, "TeamA", "bob"), "unknown player resolves nothing")
	assert.Equal(t, "", resolveKachinukiPosition(positions, "TeamB", "alice"), "the same name on another team is another fighter")
}

// --- Engine.KachinukiDetailMatches (exported wrapper) ---

// TestKachinukiDetailMatches_NonKachinukiComp verifies that fixed-team and
// individual (non-team) competitions both yield an empty/nil result with no
// error, mirroring collectKachinukiMatches' nil-guard.
func TestKachinukiDetailMatches_NonKachinukiComp(t *testing.T) {
	t.Run("fixed team", func(t *testing.T) {
		// Deliberately NOT setupKachinukiComp: this fixture's whole point is
		// a non-kachinuki team type, and a helper named for kachinuki plus a
		// counteracting override would obscure that.
		eng, store, _ := setupTestEngine(t)
		compID := "fixed-team-comp"
		require.NoError(t, store.SaveCompetition(&state.Competition{
			ID:            compID,
			TeamMatchType: state.TeamMatchTypeFixed,
			TeamSize:      5,
		}))
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam", SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R1", SideB: "W1", Winner: "R1"},
			}},
		}))

		out, err := eng.KachinukiDetailMatches(compID)
		assert.NoError(t, err)
		assert.Empty(t, out)
	})

	t.Run("individual", func(t *testing.T) {
		eng, store, _ := setupTestEngine(t)
		compID := "individual-comp"
		createTestCompetition(t, store, compID, "mixed", 3)
		saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie"})

		out, err := eng.KachinukiDetailMatches(compID)
		assert.NoError(t, err)
		assert.Empty(t, out)
	})
}

// TestKachinukiDetailMatches_PoolMatchWithSubResults verifies the exported
// wrapper end-to-end: a kachinuki competition with a scored pool match
// produces a detail entry with the correct Label ("Pool Match 1"), joined
// ippon scores, winner, decision, and elimination tallies.
func TestKachinukiDetailMatches_PoolMatchWithSubResults(t *testing.T) {
	compID := "kachinuki-detail-pool"
	eng, store, _ := setupKachinukiComp(t, compID, 5)
	ids := saveKachinukiPool(t, store, compID, "Pool A", "RedTeam", "WhiteTeam")

	matches := []state.MatchResult{
		{
			ID:      "Pool A-0",
			SideA:   "RedTeam",
			SideB:   "WhiteTeam",
			SideAID: ids["RedTeam"],
			SideBID: ids["WhiteTeam"],
			Winner:  "RedTeam",
			Status:  state.MatchStatusCompleted,
			SubResults: []state.SubMatchResult{
				{
					Position: 1,
					SideA:    "R-Senpo",
					SideB:    "W-Senpo",
					IpponsA:  []string{"M", "K"},
					IpponsB:  []string{"D"},
					Winner:   "R-Senpo",
					Decision: "fought",
				},
				{
					Position: 2,
					SideA:    "R-Senpo",
					SideB:    "W-Jiho",
					IpponsA:  []string{},
					IpponsB:  []string{},
					Winner:   "",
					Decision: state.DecisionDraw,
				},
			},
			Decision: "fought",
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	out, err := eng.KachinukiDetailMatches(compID)
	require.NoError(t, err)
	require.Len(t, out, 1)

	detail := out[0]
	assert.Equal(t, "Pool Match 1", detail.Label)
	assert.Equal(t, "RedTeam", detail.SideATeam)
	assert.Equal(t, "WhiteTeam", detail.SideBTeam)

	require.Len(t, detail.Bouts, 2)
	assert.Equal(t, 1, detail.Bouts[0].Position)
	assert.Equal(t, "R-Senpo", detail.Bouts[0].SideAName)
	assert.Equal(t, "MK", detail.Bouts[0].ScoreA, "IpponsA must be joined into one string")
	assert.Equal(t, "W-Senpo", detail.Bouts[0].SideBName)
	assert.Equal(t, "D", detail.Bouts[0].ScoreB, "IpponsB must be joined into one string")
	// Bout 1 is fought, no encho, no hantei: no middle or side mark.
	assert.Equal(t, "", detail.Bouts[0].Middle)
	assert.Equal(t, "", detail.Bouts[0].MarkA)
	assert.Equal(t, "", detail.Bouts[0].MarkB)
	// Bout 2 is the fixture's hikiwake: centre X, still no side mark.
	assert.Equal(t, "X", detail.Bouts[1].Middle)
	assert.Equal(t, "", detail.Bouts[1].MarkA)
	assert.Equal(t, "", detail.Bouts[1].MarkB)
}

// TestKachinukiDetailMatches_MatchWithoutSubResultsGetsBlankRows verifies,
// through the exported wrapper the results export uses, that a pool match
// carrying no SubResults gets 2*teamSize-1 empty rows for hand entry while a
// fought one lists its bouts only, each labelled by its place in the grid.
func TestKachinukiDetailMatches_MatchWithoutSubResultsGetsBlankRows(t *testing.T) {
	compID := "kachinuki-detail-blank"
	eng, store, _ := setupKachinukiComp(t, compID, 5)
	ids := saveKachinukiPool(t, store, compID, "Pool A", "RedTeam", "WhiteTeam", "AlphaTeam", "BetaTeam")

	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "RedTeam", SideB: "WhiteTeam", SideAID: ids["RedTeam"], SideBID: ids["WhiteTeam"]}, // no SubResults
		{
			ID:      "Pool A-1",
			SideA:   "AlphaTeam",
			SideB:   "BetaTeam",
			SideAID: ids["AlphaTeam"],
			SideBID: ids["BetaTeam"],
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "A1", SideB: "B1", Winner: "A1", Decision: "fought"},
			},
		},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	out, err := eng.KachinukiDetailMatches(compID)
	require.NoError(t, err)
	require.Len(t, out, 2, "every match of the draw has a section")
	assert.Equal(t, "Pool Match 1", out[0].Label)
	assert.Equal(t, "RedTeam", out[0].SideATeam)
	assert.Empty(t, out[0].Bouts)
	assert.Equal(t, 9, out[0].BlankBoutRows)
	assert.Equal(t, "Pool Match 2", out[1].Label)
	assert.Equal(t, "AlphaTeam", out[1].SideATeam)
	assert.Len(t, out[1].Bouts, 1)
	assert.Zero(t, out[1].BlankBoutRows)
}

// TestKachinukiDetailMatches_UnknownCompetition_ValidIDFormat documents the
// current behavior for a syntactically valid but nonexistent competition ID:
// LoadCompetition returns (nil, nil) for a missing config.md, so
// collectKachinukiMatches' nil-comp guard applies and the method returns an
// empty result with no error (it does NOT synthesize a NotFoundError).
func TestKachinukiDetailMatches_UnknownCompetition_ValidIDFormat(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	out, err := eng.KachinukiDetailMatches("does-not-exist")
	assert.NoError(t, err)
	assert.Nil(t, out)
}

// TestKachinukiDetailMatches_InvalidCompetitionID verifies the error path:
// an invalid (path-traversal-shaped) competition ID is rejected by
// state.ValidateCompetitionID inside LoadCompetition and the error
// propagates up through KachinukiDetailMatches.
func TestKachinukiDetailMatches_InvalidCompetitionID(t *testing.T) {
	eng, _, _ := setupTestEngine(t)

	out, err := eng.KachinukiDetailMatches("../evil")
	require.Error(t, err)
	assert.Nil(t, out)
	assert.Contains(t, err.Error(), "invalid competition ID")
}

// TestKachinukiPositions_MatchScopedLineupReachesLaterMatchesOnly verifies that
// a lineup entered for a match relabels that match and the team's later ones,
// and never an earlier one.
func TestKachinukiPositions_MatchScopedLineupReachesLaterMatchesOnly(t *testing.T) {
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	const compID = "kx-match"
	comp := &state.Competition{ID: compID, TeamSize: 5}
	require.NoError(t, store.SaveCompetition(comp))

	const teamID = "team-a-id"
	// Starting lineup.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: teamID,
		Positions: map[domain.Position]string{
			domain.PosSenpo: "alice", domain.PosJiho: "b", domain.PosChuken: "c",
			domain.PosFukusho: "d", domain.PosTaisho: "e",
		},
	}, 5))
	// Lineup entered for the team's second match puts alice at Taisho.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID:  teamID,
		MatchID: "Pool A-1",
		Positions: map[domain.Position]string{
			domain.PosSenpo: "e", domain.PosJiho: "b", domain.PosChuken: "c",
			domain.PosFukusho: "d", domain.PosTaisho: "alice",
		},
	}, 5))

	poolMatches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "TeamA", SideAID: teamID, SideB: "TeamB", SideBID: "team-b-id"},
		{ID: "Pool A-1", SideA: "TeamA", SideAID: teamID, SideB: "TeamC", SideBID: "team-c-id"},
		{ID: "Pool A-2", SideA: "TeamA", SideAID: teamID, SideB: "TeamD", SideBID: "team-d-id"},
	}
	rule := New(store).lineupRuleOrNone("test", compID, true, poolMatches, nil)
	aliceAt := func(i int) string {
		return resolveKachinukiPosition(rule.positionsForMatch(&poolMatches[i]), "TeamA", "alice")
	}

	assert.Equal(t, "Senpo", aliceAt(0), "before the lineup was entered: the starting lineup")
	assert.Equal(t, "Taisho", aliceAt(1), "the match it was entered for")
	assert.Equal(t, "Taisho", aliceAt(2), "carried to the team's next match")
}

// --- Squad member labels (bc-pnum: "make a team member's label
// available to the public surfaces") ---

// TestResolveKachinukiMemberLabel exercises resolveKachinukiMemberLabel's
// pure lookup: the happy path (including a BLANK-named member, which
// still resolves through its index, since a squad slot's label comes
// from Index alone, never Name), and every miss shape (unknown member,
// unknown team, no number yet, no team/member id recorded on the row).
func TestResolveKachinukiMemberLabel(t *testing.T) {
	squads := map[string][]domain.TeamMember{
		"team-red": {
			{ID: "m1", Index: 1, Name: "Alice"},
			{ID: "m2", Index: 2, Name: ""}, // unfilled seeded slot
		},
	}
	teamNumbers := map[string]string{"team-red": "T10"}

	assert.Equal(t, "T10.1", resolveKachinukiMemberLabel(teamNumbers, squads, "team-red", "m1"))
	assert.Equal(t, "T10.2", resolveKachinukiMemberLabel(teamNumbers, squads, "team-red", "m2"),
		"a blank-named member still gets a label: the label comes from its index, not its name")
	assert.Equal(t, "", resolveKachinukiMemberLabel(teamNumbers, squads, "team-red", "no-such-member"))
	assert.Equal(t, "", resolveKachinukiMemberLabel(teamNumbers, squads, "no-such-team", "m1"))
	assert.Equal(t, "", resolveKachinukiMemberLabel(map[string]string{}, squads, "team-red", "m1"),
		"team has no assigned number yet")
	assert.Equal(t, "", resolveKachinukiMemberLabel(teamNumbers, squads, "", "m1"),
		"no team id recorded on the bout row")
	assert.Equal(t, "", resolveKachinukiMemberLabel(teamNumbers, squads, "team-red", ""),
		"no member id recorded on the bout row")
}

// TestResolveKachinukiDisplayName exercises resolveKachinukiDisplayName,
// the Go twin of web-mobile/js/lineup_resolver.jsx's
// resolveBoutSideDisplayName (bc-dnst): a rename reaches a bout already
// fought on every surface including this Excel export, resolved by member
// id against the team's CURRENT squad, while the stored bout text itself
// stays frozen.
func TestResolveKachinukiDisplayName(t *testing.T) {
	squads := map[string][]domain.TeamMember{
		"team-red": {
			{ID: "m1", Index: 1, Name: "New Spelling"},
			{ID: "m2", Index: 2, Name: ""}, // unfilled seeded slot
		},
	}

	assert.Equal(t, "New Spelling", resolveKachinukiDisplayName(squads, "team-red", "m1", "Old Spelling"),
		"member id resolves to a renamed member: the current name wins over the frozen stored text")
	assert.Equal(t, "Old Spelling", resolveKachinukiDisplayName(squads, "team-red", "m2", "Old Spelling"),
		"member id resolves to a still-blank member: nothing newer to show, stored text stands")
	assert.Equal(t, "Old Spelling", resolveKachinukiDisplayName(squads, "team-red", "no-such-member", "Old Spelling"),
		"unknown member id: stored text stands")
	assert.Equal(t, "Old Spelling", resolveKachinukiDisplayName(squads, "no-such-team", "m1", "Old Spelling"),
		"unknown team id: stored text stands")
	assert.Equal(t, "Old Spelling", resolveKachinukiDisplayName(squads, "team-red", "", "Old Spelling"),
		"no member id recorded on the row: stored text stands")
	assert.Equal(t, "Old Spelling", resolveKachinukiDisplayName(squads, "", "m1", "Old Spelling"),
		"no team id recorded on the row: stored text stands")
}

// TestBuildKachinukiDetail_NamelessFighterPickedByNumberStillPrints is the
// bc-kdsc fold-in f repro: a fighter picked by squad number and never named
// (bc-dnst) has a bout row whose own SideA stays "" -- so
// resolveKachinukiDisplayName has nothing newer than that empty stored text
// to show -- while resolveKachinukiMemberLabel still resolves a real label
// from the squad slot's index alone, independent of its (blank) name. This
// drives the REAL export path end to end (buildKachinukiDetail ->
// helper.WriteKachinukiDetailSheet) and reads the rendered cell, because the
// bug (or its absence) lives in formatKachinukiPlayer's response to a
// label-but-no-name bout, which only the sheet renderer can confirm.
func TestBuildKachinukiDetail_NamelessFighterPickedByNumberStillPrints(t *testing.T) {
	squads := map[string][]domain.TeamMember{
		"RedTeam": {
			{ID: "m-red-3", Index: 3, Name: ""}, // picked by number, never named
		},
	}
	teamNumbers := map[string]string{"RedTeam": "T10"}
	positions := map[string]string{
		lineupKey("RedTeam", memberKey("m-red-3")): "Chuken",
	}

	m := &state.MatchResult{
		SideA:   "RedTeam",
		SideB:   "WhiteTeam",
		SideAID: "RedTeam",
		Status:  state.MatchStatusCompleted,
		SubResults: []state.SubMatchResult{
			{
				Position:      1,
				SideA:         "", // never named: picked by squad number alone
				SideAMemberID: "m-red-3",
				SideB:         "W-Senpo",
				IpponsA:       []string{"M"},
				Decision:      "fought",
			},
		},
	}

	detail := buildKachinukiDetail(m, "Pool Match 1", positions, teamNumbers, squads)
	require.Len(t, detail.Bouts, 1)
	// The data buildKachinukiDetail hands the renderer: a real label, no name.
	assert.Equal(t, "", detail.Bouts[0].SideAName, "the stored name stays empty: nothing newer to show")
	assert.Equal(t, "T10.3", detail.Bouts[0].SideALabel, "the label still resolves from the squad slot's index")
	assert.Equal(t, "Chuken", detail.Bouts[0].SideAPos)

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	require.NoError(t, helper.WriteKachinukiDetailSheet(f, []helper.KachinukiMatchDetail{detail}))

	// Side A (Aka) sits in the RIGHT name column (G) per helper.WhiteLeft, on
	// the first bout row under the title, colour and team rows.
	cell, err := f.GetCellValue(helper.SheetKachinukiDetail, "G4")
	require.NoError(t, err)
	assert.NotEmptyf(t, cell, "a fighter picked by number and never named must not print a blank cell (got %q)", cell)
	assert.Contains(t, cell, "T10.3", "the printed cell should at least show the known label")
}

// TestBuildKachinukiDetail_DisplayNameFollowsRename verifies the export
// wires resolveKachinukiDisplayName into every bout's SideAName/SideBName:
// a bout whose stored side text predates a rename exports the CURRENT
// squad name, while a side carrying no member id keeps its stored text
// exactly as before this feature (bc-dnst).
func TestBuildKachinukiDetail_DisplayNameFollowsRename(t *testing.T) {
	squads := map[string][]domain.TeamMember{
		"RedTeam": {
			{ID: "m-red-1", Index: 1, Name: "Renamed Senpo"},
		},
	}
	m := &state.MatchResult{
		SideA:   "RedTeam",
		SideB:   "WhiteTeam",
		SideAID: "RedTeam",
		Winner:  "RedTeam",
		Status:  state.MatchStatusCompleted,
		SubResults: []state.SubMatchResult{
			{
				Position:      1,
				SideA:         "Old Senpo Spelling",
				SideAMemberID: "m-red-1",
				SideB:         "W-Senpo", // no member id: stays exactly as stored
				IpponsA:       []string{"M", "K"},
				IpponsB:       []string{},
				Winner:        "Old Senpo Spelling",
				Decision:      "fought",
			},
		},
	}

	detail := buildKachinukiDetail(m, "Pool Match 1", map[string]string{}, map[string]string{}, squads)

	require.Len(t, detail.Bouts, 1)
	assert.Equal(t, "Renamed Senpo", detail.Bouts[0].SideAName,
		"member id resolves against squads[SideAID]: the export shows the CURRENT name")
	assert.Equal(t, "W-Senpo", detail.Bouts[0].SideBName,
		"no member id on this side: the stored text is exported unchanged")
}

// TestBuildKachinukiTeamNumbers_NilOrNoPrefix verifies the guard clause:
// a nil competition or one with no number prefix assigned yet returns an
// empty map without attempting any read.
func TestBuildKachinukiTeamNumbers_NilOrNoPrefix(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	assert.Empty(t, eng.buildKachinukiTeamNumbers("any-comp", nil))
	assert.Empty(t, eng.buildKachinukiTeamNumbers("any-comp", &state.Competition{ID: "any-comp"}))
}

// TestBuildKachinukiTeamNumbers_DrawInPools verifies the pooled-format
// branch reads pools.csv directly (RenumberCompetitors' own persisted
// Number column), keyed by participant id.
func TestBuildKachinukiTeamNumbers_DrawInPools(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "team-numbers-pools"
	redID := helper.NewUUID4()
	whiteID := helper.NewUUID4()

	comp := &state.Competition{
		ID:            compID,
		Format:        state.CompFormatMixed,
		Status:        state.CompStatusPools,
		NumberPrefix:  "T",
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []domain.Player{
			{ID: redID, Name: "RedTeam", Number: "T1"},
			{ID: whiteID, Name: "WhiteTeam", Number: "T2"},
		}},
	}))

	numbers := eng.buildKachinukiTeamNumbers(compID, comp)
	assert.Equal(t, "T1", numbers[redID])
	assert.Equal(t, "T2", numbers[whiteID])
}

// TestBuildKachinukiTeamNumbers_DrawInBracket verifies the knockout-only
// branch composes numbers from the bracket's DrawOrder (bc-pnum ruling 2,
// NumberKnockoutParticipants), matching the viewer/display merge rather
// than reading a persisted column that does not exist for this format.
func TestBuildKachinukiTeamNumbers_DrawInBracket(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "team-numbers-bracket"
	redID := helper.NewUUID4()
	whiteID := helper.NewUUID4()

	comp := &state.Competition{
		ID:            compID,
		Format:        state.CompFormatKnockout,
		Status:        state.CompStatusKnockout,
		NumberPrefix:  "T",
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "DojoR"},
		{ID: whiteID, Name: "WhiteTeam", Dojo: "DojoW"},
	}))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{DrawOrder: []string{redID, whiteID}}))

	numbers := eng.buildKachinukiTeamNumbers(compID, comp)
	assert.Equal(t, "T1", numbers[redID], "RedTeam is DrawOrder[0]")
	assert.Equal(t, "T2", numbers[whiteID], "WhiteTeam is DrawOrder[1]")
}

// TestKachinukiDetailMatches_SquadLabel_PoolMatch is the end-to-end path
// for a pooled-format kachinuki competition: a pool match bout that
// carries a resolvable team id (SideAID) and squad member id
// (SubMatchResult.SideAMemberID) gets its SideALabel composed from the
// team's pools.csv number and the squad member's stable index. Side B
// carries no member id (a legacy/unresolved bout row) and stays blank,
// matching resolveKachinukiMemberLabel's own miss case.
func TestKachinukiDetailMatches_SquadLabel_PoolMatch(t *testing.T) {
	compID := "kachinuki-squad-label-pool"
	eng, store, _ := setupKachinukiComp(t, compID, 5, func(c *state.Competition) {
		c.Format = state.CompFormatMixed
		c.Status = state.CompStatusPools
		c.NumberPrefix = "T"
	})

	redID := helper.NewUUID4()
	whiteID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "DojoR"},
		{ID: whiteID, Name: "WhiteTeam", Dojo: "DojoW"},
	}))

	member, err := store.AddTeamMember(compID, redID, "Alice")
	require.NoError(t, err)

	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []domain.Player{
			{ID: redID, Name: "RedTeam", Number: "T1"},
			{ID: whiteID, Name: "WhiteTeam", Number: "T2"},
		}},
	}))

	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
		{
			ID:      "P1-0",
			SideA:   "RedTeam",
			SideAID: redID,
			SideB:   "WhiteTeam",
			SideBID: whiteID,
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "Alice", SideAMemberID: member.ID, SideB: "W-Senpo", Winner: "Alice", Decision: "fought"},
			},
		},
	}))

	out, err := eng.KachinukiDetailMatches(compID)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Bouts, 1)
	assert.Equal(t, "T1.1", out[0].Bouts[0].SideALabel, "RedTeam=T1, Alice is squad member index 1")
	assert.Equal(t, "", out[0].Bouts[0].SideBLabel, "no member id recorded for Side B")
}

// TestKachinukiDetailMatches_SquadLabel_BracketMatch is the end-to-end
// path for a knockout-only (knockout) kachinuki competition: the team
// number comes from the bracket's DrawOrder rather than pools.csv, and
// bracketMatchToTeamResult's SideAID/SideBID (bc-brid) carry the bout's
// team identity through the read-only projection collectKachinukiMatches
// uses for a bracket-origin match.
func TestKachinukiDetailMatches_SquadLabel_BracketMatch(t *testing.T) {
	compID := "kachinuki-squad-label-bracket"
	eng, store, _ := setupKachinukiComp(t, compID, 5, func(c *state.Competition) {
		c.Format = state.CompFormatKnockout
		c.Status = state.CompStatusKnockout
		c.NumberPrefix = "T"
	})

	redID := helper.NewUUID4()
	whiteID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{ID: redID, Name: "RedTeam", Dojo: "DojoR"},
		{ID: whiteID, Name: "WhiteTeam", Dojo: "DojoW"},
	}))

	member, err := store.AddTeamMember(compID, redID, "Alice")
	require.NoError(t, err)

	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		DrawOrder: []string{redID, whiteID},
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:      "SF1",
					SideA:   "RedTeam",
					SideAID: redID,
					SideB:   "WhiteTeam",
					SideBID: whiteID,
					Winner:  "RedTeam",
					Status:  state.MatchStatusCompleted,
					SubResults: []state.SubMatchResult{
						{Position: 1, SideA: "Alice", SideAMemberID: member.ID, SideB: "W-Senpo", Winner: "Alice", Decision: "fought"},
					},
				},
			},
		},
	}))

	out, err := eng.KachinukiDetailMatches(compID)
	require.NoError(t, err)
	require.Len(t, out, 1)
	require.Len(t, out[0].Bouts, 1)
	assert.Equal(t, "T1.1", out[0].Bouts[0].SideALabel, "RedTeam is DrawOrder[0] -> T1, Alice is squad member index 1")
}

// TestKachinukiPositions_NamelessFighterResolvesByMemberID pins the
// bc-dnst rule that a fighter fielded by squad number and not yet named (an
// id, an empty name) still gets a position label on the export: the lookup is
// indexed by member id as well as by name, and a bout side resolves by its
// id first, falling back to its name for legacy rows that carry no id.
func TestKachinukiPositions_NamelessFighterResolvesByMemberID(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pos-map-nameless"
	comp := &state.Competition{ID: compID, TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 5}
	require.NoError(t, store.SaveCompetition(comp))
	redID := helper.NewUUID4()
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{{ID: redID, Name: "RedTeam", Dojo: "DojoR"}}))
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID:    redID,
		MatchID:   "SF-2",
		Positions: map[domain.Position]string{domain.PosSenpo: "R-Senpo", domain.PosJiho: ""},
		MemberIDs: map[domain.Position]string{domain.PosSenpo: "mem-senpo", domain.PosJiho: "mem-jiho"},
	}, 5))

	posMap := eng.lineupRuleOrNone("test", compID, true, nil, nil).positionsForMatch(&state.MatchResult{ID: "SF-2", SideA: "RedTeam", SideAID: redID})

	assert.Equal(t, "Jiho", resolveKachinukiBoutPosition(posMap, "RedTeam", "mem-jiho", ""), "a nameless fighter resolves by id")
	assert.Equal(t, "Senpo", resolveKachinukiBoutPosition(posMap, "RedTeam", "mem-senpo", "R-Senpo"), "id wins for a named fighter too")
	assert.Equal(t, "Senpo", resolveKachinukiBoutPosition(posMap, "RedTeam", "", "R-Senpo"), "a row with no id still resolves by name")
	assert.Equal(t, "", resolveKachinukiBoutPosition(posMap, "RedTeam", "", ""), "no id and no name resolves nothing")
}

// A lineup can still hold ONE member id at TWO positions: the duplicate guard
// is new, and rows written before it are live data repaired by hand. Both
// iterations compute the same map key here, so a plain range over MemberIDs
// let Go's randomised map order decide which position label survived, and the
// same competition exported "Senpo" on one run and "Chuken" on the next from
// byte-identical state. Which of the two wins is arbitrary; that it is the
// SAME one every time is not.
func TestKachinukiPositions_DuplicateMemberIDLabelsDeterministically(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pos-map-dup"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 5,
	}))
	// Written STRAIGHT TO DISK: SetTeamLineup refuses this shape today, which
	// is exactly why the export still has to cope with it. LoadTeamLineups does
	// not run the legacy repair, so a row like this reaches the export as-is.
	type lineupFileShape struct {
		Lineups []domain.TeamLineup `yaml:"lineups"`
	}
	body, mErr := yaml.Marshal(&lineupFileShape{Lineups: []domain.TeamLineup{{
		TeamID: "red-id", Round: 0,
		Positions: map[domain.Position]string{
			domain.PosSenpo:  "R-Senpo",
			domain.PosChuken: "R-Chuken",
		},
		MemberIDs: map[domain.Position]string{
			domain.PosSenpo:  "m-dup",
			domain.PosChuken: "m-dup",
		},
	}}})
	require.NoError(t, mErr)
	require.NoError(t, os.WriteFile(
		filepath.Join(store.GetFolder(), "competitions", compID, "lineups.yaml"), body, 0o600))

	match := &state.MatchResult{ID: "P1-0", SideA: "RedTeam", SideAID: "red-id", SideB: "WhiteTeam", SideBID: "white-id"}
	labelOf := func() string {
		return eng.lineupRuleOrNone("test", compID, true, nil, nil).positionsForMatch(match)[lineupKey("RedTeam", memberKey("m-dup"))]
	}

	// Repeated because the defect was map-order-dependent: one run could agree
	// with the fix by luck, so the pin is that many runs agree with EACH OTHER.
	first := labelOf()
	require.NotEmpty(t, first, "the duplicated member must still resolve to a position")
	for i := 0; i < 24; i++ {
		require.Equal(t, first, labelOf(), "the surviving label must not depend on map iteration order")
	}
	assert.Equal(t, "Senpo", first, "the order a lineup is fielded in keeps the first, which is senpo before chuken: the roster fields this fighter at senpo")
}

// The same defect for NAMES: a lineup can hold one name at two positions (two
// teammates sharing a display name, or a hand-edited file), both of which
// compute the same map key, and a plain range over Positions let Go's
// randomised map order decide which label survived, so the same lineup exported
// "Senpo" on one run and "Chuken" on the next.
func TestIndexLineupPositions_DuplicateNameLabelsDeterministically(t *testing.T) {
	lineup := domain.TeamLineup{
		TeamID: "red-id",
		Positions: map[domain.Position]string{
			domain.PosSenpo:  "R-Twice",
			domain.PosChuken: "R-Twice",
			domain.PosTaisho: "R-Once",
		},
	}
	labelOf := func(fighter string) string {
		out := map[string]string{}
		indexLineupPositions(out, "RedTeam", lineup)
		return out[lineupKey("RedTeam", fighter)]
	}

	first := labelOf("R-Twice")
	require.NotEmpty(t, first, "the repeated name must still resolve to a position")
	for i := 0; i < 24; i++ {
		require.Equal(t, first, labelOf("R-Twice"), "the surviving label must not depend on map iteration order")
	}
	assert.Equal(t, "Senpo", first, "the order a lineup is fielded in keeps the first, which is senpo before chuken")
	assert.Equal(t, "Taisho", labelOf("R-Once"), "a name held once keeps its position")
}

// A team of ten or more fields numbered positions, and a fighter held at "2" and
// at "10" is at 2: the roster walks numbered positions by their number
// (TeamLineup.OrderedMembers), and so does the export. A text order put "10" first.
func TestIndexLineupPositions_NumberedPositionsAreOrderedByNumber(t *testing.T) {
	lineup := domain.TeamLineup{
		TeamID: "red-id",
		Positions: map[domain.Position]string{
			domain.PositionNumbered(10): "R-Twice",
			domain.PositionNumbered(2):  "R-Twice",
			domain.PositionNumbered(11): "R-Once",
		},
	}
	out := map[string]string{}

	indexLineupPositions(out, "RedTeam", lineup)

	assert.Equal(t, "2", out[lineupKey("RedTeam", "R-Twice")])
	assert.Equal(t, "11", out[lineupKey("RedTeam", "R-Once")])
}

// A pool section is numbered as BlankKachinukiSections numbers the same draw:
// pool by pool in the Pool Matches grid's order, not in the file's order,
// and a supplementary row takes no number.
func TestCollectKachinukiMatches_PoolSectionsNumberedAsTheBlankTemplate(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-pool-numbering"
	createTestCompetition(t, store, compID, state.CompFormatMixed, 3, func(c *state.Competition) {
		c.Kind = "team"
		c.TeamSize = 3
		c.TeamMatchType = state.TeamMatchTypeKachinuki
		c.Courts = []string{"A", "B"}
	})
	saveTestParticipants(t, store, compID, []string{"Ryu", "Tora", "Kame", "Taka", "Kuma", "Hebi"})
	require.NoError(t, eng.StartCompetition(compID))

	stored, err := store.LoadPoolMatches(compID)
	require.NoError(t, err)
	require.NotEmpty(t, stored)
	tb := state.MatchResult{ID: stored[0].ID[:strings.LastIndexByte(stored[0].ID, '-')] + "-TB-0", SideA: stored[0].SideA, SideB: stored[0].SideB}
	stored = append([]state.MatchResult{stored[len(stored)-1], tb}, stored[:len(stored)-1]...)
	require.NoError(t, store.SavePoolMatches(compID, stored))

	pools, err := store.LoadPools(compID)
	require.NoError(t, err)
	AttachPoolMatches(pools, stored)
	want := helper.BlankKachinukiSections(pools, nil, false, 5)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	out, err := eng.collectKachinukiMatches(compID, comp)
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(out), len(want))
	for i, w := range want {
		assert.Equal(t, [3]string{w.Label, w.SideATeam, w.SideBTeam}, [3]string{out[i].Label, out[i].SideATeam, out[i].SideBTeam}, "section %d", i)
	}
}

// A match decided without a bout has nothing left to enter, so it gets no
// blank rows; one not played yet still does.
func TestCollectKachinukiMatches_DecidedWithoutBoutsGetsNoBlankRows(t *testing.T) {
	compID := "kachinuki-decided-no-bouts"
	eng, store, comp := setupKachinukiComp(t, compID, 5)
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{{
		{ID: "r1-m1", MatchNumber: 1, SideA: "AlphaTeam", SideB: "BetaTeam", Status: state.MatchStatusCompleted, Decision: "kiken-voluntary", Winner: "AlphaTeam"},
		{ID: "r1-m2", MatchNumber: 2, SideA: "GammaTeam", SideB: "DeltaTeam"},
	}}}))

	out, err := eng.collectKachinukiMatches(compID, comp)
	require.NoError(t, err)
	rows := map[string]int{}
	for _, d := range out {
		rows[d.SideATeam] = d.BlankBoutRows
	}
	require.Contains(t, rows, "AlphaTeam")
	require.Contains(t, rows, "GammaTeam")
	assert.Zero(t, rows["AlphaTeam"], "a match decided by a withdrawal")
	assert.Equal(t, 9, rows["GammaTeam"], "a match not played yet keeps 2*5-1 rows")
}

// kachinukiLineupFor builds a lineup holding the named fighters at the first
// positions, round-scoped when matchID is empty.
func kachinukiLineupFor(team, matchID string, round int, fighters map[domain.Position]string) domain.TeamLineup {
	return domain.TeamLineup{TeamID: team, MatchID: matchID, Round: round, Positions: fighters}
}

// TestKachinukiDetail_PositionsFollowTheLineupInForce pins that a bout row's
// position label is read from the lineup in force for its team at ITS match
// (operator ruling 2026-10-05), never from whichever saved lineup happens to
// hold the fighter's name.
func TestKachinukiDetail_PositionsFollowTheLineupInForce(t *testing.T) {
	positionOf := func(d helper.KachinukiMatchDetail, name string) string {
		for _, b := range d.Bouts {
			if b.SideAName == name {
				return b.SideAPos
			}
		}
		t.Fatalf("no bout with %s on side A in %q", name, d.Label)
		return ""
	}

	// The teams as the store holds them: a participant id and the team name.
	saveTeams := func(store *state.Store, compID string, teams ...string) map[string]string {
		ids := map[string]string{}
		roster := make([]domain.Player, len(teams))
		for i, team := range teams {
			ids[team] = bctest.StampPlayerID(team, "Dojo")
			roster[i] = domain.Player{ID: ids[team], Name: team, Dojo: "Dojo"}
		}
		require.NoError(t, store.SaveParticipants(compID, roster))
		return ids
	}

	t.Run("a lineup entered for a team's first match is carried to its second", func(t *testing.T) {
		compID := "kx-pos-carried"
		eng, store, _ := setupKachinukiComp(t, compID, 5, func(c *state.Competition) { c.Format = state.CompFormatMixed })
		ids := saveTeams(store, compID, "RedTeam", "WhiteTeam", "BlueTeam")

		require.NoError(t, store.SetTeamLineup(compID, kachinukiLineupFor(ids["RedTeam"], "", 0, map[domain.Position]string{
			domain.PosSenpo: "R-Senpo", domain.PosJiho: "R-Jiho",
		}), 5))
		require.NoError(t, store.SetTeamLineup(compID, kachinukiLineupFor(ids["RedTeam"], "Pool A-0", 0, map[domain.Position]string{
			domain.PosSenpo: "R-Jiho", domain.PosJiho: "R-Senpo",
		}), 5))
		bout := func(red, other string) []state.SubMatchResult {
			return []state.SubMatchResult{{Position: 1, SideA: red, SideB: other, Winner: red, Decision: "fought"}}
		}
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{
			{ID: "Pool A-0", SideA: "RedTeam", SideB: "WhiteTeam", SideAID: ids["RedTeam"], SideBID: ids["WhiteTeam"], SubResults: bout("R-Senpo", "W-1")},
			{ID: "Pool A-1", SideA: "RedTeam", SideB: "BlueTeam", SideAID: ids["RedTeam"], SideBID: ids["BlueTeam"], SubResults: bout("R-Senpo", "B-1")},
		}))

		out, err := eng.KachinukiDetailMatches(compID)
		require.NoError(t, err)
		require.Len(t, out, 2)
		assert.Equal(t, "Jiho", positionOf(out[0], "R-Senpo"), "the match's own lineup")
		assert.Equal(t, "Jiho", positionOf(out[1], "R-Senpo"),
			"the second match carries the lineup entered for the first, not the starting lineup")
	})

	// A round 1 lineup (round >= 1, which releases up to v2.1.1 let the Lineups
	// page save) is given to the matches it applied to by the state layer when
	// its team is seated; one written straight to the store afterwards is not
	// read.
	round1Bracket := func(t *testing.T, store *state.Store, compID string, ids map[string]string) {
		t.Helper()
		bouts := func(fighters ...string) []state.SubMatchResult {
			out := make([]state.SubMatchResult, len(fighters))
			for i, f := range fighters {
				out[i] = state.SubMatchResult{Position: i + 1, SideA: f, SideB: "X", Winner: f, Decision: "fought"}
			}
			return out
		}
		require.NoError(t, store.SaveBracket(compID, &state.Bracket{Rounds: [][]state.BracketMatch{
			{
				{ID: "r0-m0", SideA: "RedTeam", SideB: "WhiteTeam", SideAID: ids["RedTeam"], SideBID: ids["WhiteTeam"], SubResults: bouts("R-A", "R-New")},
				{ID: "r0-m1", SideA: "BlueTeam", SideB: "Other", SideAID: ids["BlueTeam"], SubResults: bouts("B-1")},
			},
			{
				{ID: "r1-m0", SideA: "RedTeam", SideB: "BlueTeam", SideAID: ids["RedTeam"], SideBID: ids["BlueTeam"], SubResults: bouts("R-A", "R-New")},
			},
		}}))
	}
	startAndRound1 := map[domain.Position]string{domain.PosSenpo: "R-B", domain.PosJiho: "R-A", domain.PosChuken: "R-New"}

	t.Run("a Lineups-page entry for round 1 is not read: the round 1 match carries the lineup before it", func(t *testing.T) {
		compID := "kx-pos-round-ignored"
		eng, store, _ := setupKachinukiComp(t, compID, 5, func(c *state.Competition) { c.Format = state.CompFormatKnockout })
		ids := saveTeams(store, compID, "RedTeam", "WhiteTeam", "BlueTeam")
		require.NoError(t, store.SetTeamLineup(compID, kachinukiLineupFor(ids["RedTeam"], "", 0, map[domain.Position]string{
			domain.PosSenpo: "R-A", domain.PosJiho: "R-B",
		}), 5))
		require.NoError(t, store.SetTeamLineup(compID, kachinukiLineupFor(ids["RedTeam"], "", 1, startAndRound1), 5))
		round1Bracket(t, store, compID, ids)

		out, err := eng.KachinukiDetailMatches(compID)
		require.NoError(t, err)
		require.Len(t, out, 3)
		assert.Equal(t, "Senpo", positionOf(out[0], "R-A"), "round 0: the starting lineup")
		assert.Equal(t, "", positionOf(out[0], "R-New"), "a fighter only the round 1 entry holds has no position")
		assert.Equal(t, "Senpo", positionOf(out[2], "R-A"), "round 1 carries the starting lineup: the round 1 entry is not read")
		assert.Equal(t, "", positionOf(out[2], "R-New"))
	})

	t.Run("a lineup entered for a round 1 match relabels from that match", func(t *testing.T) {
		compID := "kx-pos-round-match"
		eng, store, _ := setupKachinukiComp(t, compID, 5, func(c *state.Competition) { c.Format = state.CompFormatKnockout })
		ids := saveTeams(store, compID, "RedTeam", "WhiteTeam", "BlueTeam")
		require.NoError(t, store.SetTeamLineup(compID, kachinukiLineupFor(ids["RedTeam"], "", 0, map[domain.Position]string{
			domain.PosSenpo: "R-A", domain.PosJiho: "R-B",
		}), 5))
		require.NoError(t, store.SetTeamLineup(compID, kachinukiLineupFor(ids["RedTeam"], "r1-m0", 0, startAndRound1), 5))
		round1Bracket(t, store, compID, ids)

		out, err := eng.KachinukiDetailMatches(compID)
		require.NoError(t, err)
		require.Len(t, out, 3)
		assert.Equal(t, "Senpo", positionOf(out[0], "R-A"), "round 0: the starting lineup")
		assert.Equal(t, "", positionOf(out[0], "R-New"), "a fighter only the round 1 match's lineup holds has no position before it")
		assert.Equal(t, "Jiho", positionOf(out[2], "R-A"), "round 1 starts with its own entry")
		assert.Equal(t, "Chuken", positionOf(out[2], "R-New"))
	})
}
