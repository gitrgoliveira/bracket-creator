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

// TestTallyKachinukiEliminations_Winner exercises the winner-based
// retirement branch: when SideB wins a bout, the SideA player is
// retired (b counter for winner's perspective, a for loser's). The
// fixture is deliberately ASYMMETRIC (2 SideA retirements vs 1 SideB)
// so a swap of the two returned counters cannot pass unnoticed.
func TestTallyKachinukiEliminations_Winner(t *testing.T) {
	m := &state.MatchResult{
		SideA: "RedTeam",
		SideB: "WhiteTeam",
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-Senpo", SideB: "W-Senpo", Winner: "W-Senpo", Decision: "fought"},
			{Position: 2, SideA: "R-Jiho", SideB: "W-Senpo", Winner: "R-Jiho", Decision: "fought"},
			{Position: 3, SideA: "R-Jiho", SideB: "W-Jiho", Winner: "W-Jiho", Decision: "fought"},
		},
	}
	a, b := tallyKachinukiEliminations(m)
	assert.Equal(t, 2, a, "SideA retired: R-Senpo (bout 1) and R-Jiho (bout 3) eliminated")
	assert.Equal(t, 1, b, "SideB retired: W-Senpo (bout 2) eliminated")
}

// TestTallyKachinukiEliminations_Hikiwake verifies that a hikiwake
// retires both players (one from each side).
func TestTallyKachinukiEliminations_Hikiwake(t *testing.T) {
	m := &state.MatchResult{
		SideA: "RedTeam",
		SideB: "WhiteTeam",
		SubResults: []state.SubMatchResult{
			{
				Position: 1,
				SideA:    "R-Senpo",
				SideB:    "W-Senpo",
				Decision: state.DecisionDraw,
			},
		},
	}
	a, b := tallyKachinukiEliminations(m)
	assert.Equal(t, 1, a, "hikiwake retires SideA player")
	assert.Equal(t, 1, b, "hikiwake retires SideB player")
}

// TestTallyKachinukiEliminations_CountsAFighterFieldedByNumber pins the
// switch from len(retiredX.Names) to retiredX.Count().
//
// The three fixtures above cannot pin it: every fighter in them carries a
// NAME and no member id, so the two expressions agree by construction and
// reverting the change leaves them green (verified -- the revert left the
// whole repo green, which is how this gap was found). The discriminating
// shape is the one bc-dnst introduced: a fighter picked by squad number and
// never named retires under a member id and an EMPTY name, so the Names set
// never hears about them and the exported Kachinuki Detail sheet under-counts
// that side's eliminations.
func TestTallyKachinukiEliminations_CountsAFighterFieldedByNumber(t *testing.T) {
	m := &state.MatchResult{
		SideA: "RedTeam",
		SideB: "WhiteTeam",
		SubResults: []state.SubMatchResult{
			// SideA is a blank squad slot fielded by number: id, no name.
			// The winner is named, so attribution takes the name tier and
			// retires SideA -- under its id alone.
			{Position: 1, SideA: "", SideAMemberID: "m-red-1", SideB: "W-Senpo", Winner: "W-Senpo", Decision: "fought"},
			// A second, NAMED SideA retirement, so the assertion below is a
			// count of two distinct fighters rather than of one: a naive
			// len(Names) would report 1 here, not 0, and an assertion of 1
			// could not tell the two implementations apart.
			{Position: 2, SideA: "R-Jiho", SideB: "W-Senpo", Winner: "W-Senpo", Decision: "fought"},
		},
	}
	a, b := tallyKachinukiEliminations(m)
	assert.Equal(t, 2, a, "both SideA fighters retired; the nameless one is counted by member id")
	assert.Equal(t, 0, b, "SideB's fighter won both bouts and stays on")
}

// TestTallyKachinukiEliminations_Empty verifies no panics/zero counts
// for a match with no sub-results.
func TestTallyKachinukiEliminations_Empty(t *testing.T) {
	m := &state.MatchResult{SideA: "A", SideB: "B"}
	a, b := tallyKachinukiEliminations(m)
	assert.Equal(t, 0, a)
	assert.Equal(t, 0, b)
}

// TestBuildKachinukiDetail verifies the full conversion: sub-results
// become Bouts, eliminations are tallied, and top-level fields are
// copied.
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
	// Elimination tally: W-Senpo lost so b=1, R-Senpo won so a=0.
	assert.Equal(t, 0, detail.EliminationA)
	assert.Equal(t, 1, detail.EliminationB)
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

// TestCollectKachinukiMatches_PoolMatchesWithBouts verifies that a pool
// match with sub-results lists exactly those bouts, a pool match with none
// gets 2*teamSize-1 empty rows for hand entry (operator decision 2026-09-27,
// bc-kdsc), and a tie-break or daihyosen row has a section only once it has
// bouts, since it is not a match of the draw.
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

	matches := []state.MatchResult{
		{
			ID:    "P1-0",
			SideA: "RedTeam",
			SideB: "WhiteTeam",
			SubResults: []state.SubMatchResult{
				{Position: 1, SideA: "R1", SideB: "W1", Winner: "R1", Decision: "fought"},
			},
		},
		{
			// No sub-results: empty rows for hand entry.
			ID:    "P1-1",
			SideA: "AlphaTeam",
			SideB: "BetaTeam",
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

	assert.Equal(t, "Pool Match 5", out[2].Label, "the label keeps the row's place in the file")
	assert.Len(t, out[2].Bouts, 1)
}

// TestBuildKachinukiPositionMap_WithLineups verifies that saved team lineups
// are correctly mapped to the (team, player) → position lookup table.
func TestBuildKachinukiPositionMap_WithLineups(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "pos-map-comp"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}))

	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "RedTeam",
		Round:  0,
		Positions: map[domain.Position]string{
			domain.PosSenpo:   "R-Senpo",
			domain.PosJiho:    "R-Jiho",
			domain.PosChuken:  "R-Chuken",
			domain.PosFukusho: "R-Fukusho",
			domain.PosTaisho:  "R-Taisho",
		},
	}, 5))

	comp := &state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}
	posMap := eng.buildKachinukiPositionMap(compID, comp)

	assert.Equal(t, "Senpo", posMap[lineupKey("RedTeam", "R-Senpo")])
	assert.Equal(t, "Jiho", posMap[lineupKey("RedTeam", "R-Jiho")])
}

// TestBuildKachinukiPositionMap_ParticipantIDKeyed verifies that lineups
// saved by the UI (TeamID = team participant id, a UUID) still resolve
// position labels for match sides that carry the team display NAME.
// The map must be indexed under both keys so resolveKachinukiPosition,
// which is called with m.SideA/m.SideB (names), finds the label.
func TestBuildKachinukiPositionMap_ParticipantIDKeyed(t *testing.T) {
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

	posMap := eng.buildKachinukiPositionMap(compID, comp)

	// Name-keyed lookups: what resolveKachinukiPosition uses with m.SideA/B.
	assert.Equal(t, "Senpo", posMap[lineupKey("RedTeam", "R-Senpo")])
	assert.Equal(t, "Jiho", posMap[lineupKey("RedTeam", "R-Jiho")])
	assert.Equal(t, "Senpo", resolveKachinukiPosition(posMap, "SF-1", "RedTeam", "R-Sub"))
	// Raw participant-id keys stay available too (match on id OR name).
	assert.Equal(t, "Senpo", posMap[lineupKey(redID, "R-Senpo")])
}

// TestBuildKachinukiPositionMap_NilComp verifies the nil guard.
func TestBuildKachinukiPositionMap_NilComp(t *testing.T) {
	eng, _, _ := setupTestEngine(t)
	posMap := eng.buildKachinukiPositionMap("any-comp", nil)
	assert.Empty(t, posMap)
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

// TestCollectKachinukiMatches_BlankTemplateCoversTheDraw pins the hand-entry
// rule on real draws (operator decision 2026-09-27, bc-kdsc): before any bout
// is recorded, every match of the draw has a section of 2*teamSize-1 empty
// rows -- each pool match, each numbered bracket match including the later
// rounds whose sides are not known yet, and the 3rd-place match -- while a
// bye has none. A side an earlier match decides is named the way the
// Elimination Matches sheet prints it, "M <n>"; a pool placeholder as it
// stands.
func TestCollectKachinukiMatches_BlankTemplateCoversTheDraw(t *testing.T) {
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
			assert.ElementsMatch(t, sheetTitles, sectionTitles, "each bracket section is titled as its Elimination Matches block")
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

// TestResolveKachinukiPosition_PrefersMatchScoped verifies the mp-825
// selection: a match-scoped lineup wins over the round-scoped fallback,
// and the fallback applies when no match-scoped entry exists.
func TestResolveKachinukiPosition_PrefersMatchScoped(t *testing.T) {
	positions := map[string]string{
		lineupKey("TeamA", "alice"):                 "Senpo",  // round-scoped fallback
		matchLineupKey("PoolA-1", "TeamA", "alice"): "Taisho", // match-scoped override for PoolA-1
	}

	// Match PoolA-1 has a match-scoped override → Taisho.
	assert.Equal(t, "Taisho", resolveKachinukiPosition(positions, "PoolA-1", "TeamA", "alice"))
	// Match PoolA-2 has no match-scoped entry → round-scoped fallback.
	assert.Equal(t, "Senpo", resolveKachinukiPosition(positions, "PoolA-2", "TeamA", "alice"))
	// Empty matchID → fallback only.
	assert.Equal(t, "Senpo", resolveKachinukiPosition(positions, "", "TeamA", "alice"))
	// Unknown player → empty.
	assert.Equal(t, "", resolveKachinukiPosition(positions, "PoolA-1", "TeamA", "bob"))
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

	matches := []state.MatchResult{
		{
			ID:     "P1-0",
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

	// Bout 1: R-Senpo (SideA) wins, so W-Senpo (SideB) retires.
	// Bout 2 is a hikiwake, which retires one player from EACH side:
	// R-Senpo (SideA) and W-Jiho (SideB). Distinct retired names per side:
	// SideA={R-Senpo} (1), SideB={W-Senpo, W-Jiho} (2).
	assert.Equal(t, 1, detail.EliminationA, "R-Senpo retires via the bout-2 hikiwake")
	assert.Equal(t, 2, detail.EliminationB, "W-Senpo (bout1 loser) and W-Jiho (bout2 hikiwake) both retire")
}

// TestKachinukiDetailMatches_MatchWithoutSubResultsGetsBlankRows verifies,
// through the exported wrapper the results export uses, that a pool match
// carrying no SubResults gets 2*teamSize-1 empty rows for hand entry while a
// fought one lists its bouts only, each labelled by its place in the file.
func TestKachinukiDetailMatches_MatchWithoutSubResultsGetsBlankRows(t *testing.T) {
	compID := "kachinuki-detail-blank"
	eng, store, _ := setupKachinukiComp(t, compID, 5)

	matches := []state.MatchResult{
		{ID: "P1-0", SideA: "RedTeam", SideB: "WhiteTeam"}, // no SubResults
		{
			ID:    "P1-1",
			SideA: "AlphaTeam",
			SideB: "BetaTeam",
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

// TestBuildKachinukiPositionMap_MatchScoped verifies the loader splits
// match-scoped and round-scoped lineups into their respective key
// namespaces.
func TestBuildKachinukiPositionMap_MatchScoped(t *testing.T) {
	dir := t.TempDir()
	store, err := state.NewStore(dir)
	require.NoError(t, err)
	const compID = "kx-match"
	comp := &state.Competition{ID: compID, TeamSize: 5}
	require.NoError(t, store.SaveCompetition(comp))

	// Round-scoped lineup.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID: "TeamA",
		Positions: map[domain.Position]string{
			domain.PosSenpo: "alice", domain.PosJiho: "b", domain.PosChuken: "c",
			domain.PosFukusho: "d", domain.PosTaisho: "e",
		},
	}, 5))
	// Match-scoped lineup for PoolA-1 puts alice at Taisho.
	require.NoError(t, store.SetTeamLineup(compID, domain.TeamLineup{
		TeamID:  "TeamA",
		MatchID: "PoolA-1",
		Positions: map[domain.Position]string{
			domain.PosSenpo: "e", domain.PosJiho: "b", domain.PosChuken: "c",
			domain.PosFukusho: "d", domain.PosTaisho: "alice",
		},
	}, 5))

	e := New(store)
	m := e.buildKachinukiPositionMap(compID, comp)

	assert.Equal(t, "Senpo", resolveKachinukiPosition(m, "PoolA-2", "TeamA", "alice"),
		"no match-scoped entry for PoolA-2 → round fallback Senpo")
	assert.Equal(t, "Taisho", resolveKachinukiPosition(m, "PoolA-1", "TeamA", "alice"),
		"match-scoped PoolA-1 overrides alice to Taisho")
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

	// Side A (Aka) sits in the RIGHT column (F) per helper.WhiteLeft.
	cell, err := f.GetCellValue(helper.SheetKachinukiDetail, "F4")
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

// TestBuildKachinukiPositionMap_NamelessFighterResolvesByMemberID pins the
// bc-dnst rule that a fighter fielded by squad number and not yet named (an
// id, an empty name) still gets a position label on the export: the map is
// indexed by member id as well as by name, and a bout side resolves by its
// id first, falling back to its name for legacy rows that carry no id.
func TestBuildKachinukiPositionMap_NamelessFighterResolvesByMemberID(t *testing.T) {
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

	posMap := eng.buildKachinukiPositionMap(compID, comp)

	assert.Equal(t, "Jiho", resolveKachinukiBoutPosition(posMap, "SF-2", "RedTeam", "mem-jiho", ""), "a nameless fighter resolves by id")
	assert.Equal(t, "Senpo", resolveKachinukiBoutPosition(posMap, "SF-2", "RedTeam", "mem-senpo", "R-Senpo"), "id wins for a named fighter too")
	assert.Equal(t, "Senpo", resolveKachinukiBoutPosition(posMap, "SF-2", "RedTeam", "", "R-Senpo"), "a row with no id still resolves by name")
	assert.Equal(t, "", resolveKachinukiBoutPosition(posMap, "SF-2", "RedTeam", "", ""), "no id and no name resolves nothing")
}

// A lineup can still hold ONE member id at TWO positions: the duplicate guard
// is new, and rows written before it are live data repaired by hand. Both
// iterations compute the same map key here, so a plain range over MemberIDs
// let Go's randomised map order decide which position label survived, and the
// same competition exported "Senpo" on one run and "Chuken" on the next from
// byte-identical state. Which of the two wins is arbitrary; that it is the
// SAME one every time is not.
func TestBuildKachinukiPositionMap_DuplicateMemberIDLabelsDeterministically(t *testing.T) {
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
		TeamID: "RedTeam", Round: 0,
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

	comp := &state.Competition{ID: compID, TeamMatchType: state.TeamMatchTypeKachinuki, TeamSize: 5}

	// Repeated because the defect was map-order-dependent: one run could agree
	// with the fix by luck, so the pin is that many runs agree with EACH OTHER.
	first := eng.buildKachinukiPositionMap(compID, comp)[lineupKey("RedTeam", memberKey("m-dup"))]
	require.NotEmpty(t, first, "the duplicated member must still resolve to a position")
	for i := 0; i < 24; i++ {
		got := eng.buildKachinukiPositionMap(compID, comp)[lineupKey("RedTeam", memberKey("m-dup"))]
		require.Equal(t, first, got, "the surviving label must not depend on map iteration order")
	}
	assert.Equal(t, "Chuken", first, "sorted key order keeps the first, which is chuken before senpo")
}
