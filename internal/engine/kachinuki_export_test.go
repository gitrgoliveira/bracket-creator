package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFormatPositionLabel exercises every named position and the
// numeric/empty fall-through paths.
func TestFormatPositionLabel(t *testing.T) {
	tests := []struct {
		pos  domain.Position
		want string
	}{
		{domain.PosSenpo, "Senpo"},
		{domain.PosJiho, "Jiho"},
		{domain.PosChuken, "Chuken"},
		{domain.PosFukusho, "Fukusho"},
		{domain.PosTaisho, "Taisho"},
		{"1", "1"},
		{"2", "2"},
		{"", ""},
		{"unknown", "Unknown"},
	}
	for _, tc := range tests {
		t.Run(string(tc.pos), func(t *testing.T) {
			assert.Equal(t, tc.want, formatPositionLabel(tc.pos))
		})
	}
}

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
// retired (b counter for winner's perspective, a for loser's).
func TestTallyKachinukiEliminations_Winner(t *testing.T) {
	m := &state.MatchResult{
		SideA: "RedTeam",
		SideB: "WhiteTeam",
		SubResults: []state.SubMatchResult{
			{Position: 1, SideA: "R-Senpo", SideB: "W-Senpo", Winner: "W-Senpo", Decision: "fought"},
			{Position: 2, SideA: "R-Jiho", SideB: "W-Senpo", Winner: "R-Jiho", Decision: "fought"},
		},
	}
	a, b := tallyKachinukiEliminations(m)
	assert.Equal(t, 1, a, "SideA retired: W-Senpo won bout 1 → R-Senpo eliminated")
	assert.Equal(t, 1, b, "SideB retired: R-Jiho won bout 2 → W-Senpo eliminated")
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

	detail := buildKachinukiDetail(m, "Pool Match 1", positions)

	assert.Equal(t, "Pool Match 1", detail.Label)
	assert.Equal(t, "RedTeam", detail.SideATeam)
	assert.Equal(t, "WhiteTeam", detail.SideBTeam)
	assert.Equal(t, "RedTeam", detail.Winner)
	require.Len(t, detail.Bouts, 1)
	assert.Equal(t, 1, detail.Bouts[0].Position)
	assert.Equal(t, "R-Senpo", detail.Bouts[0].SideAName)
	assert.Equal(t, "Senpo", detail.Bouts[0].SideAPos)
	assert.Equal(t, "MK", detail.Bouts[0].ScoreA)
	assert.Equal(t, "", detail.Bouts[0].ScoreB)
	assert.Equal(t, "W-Senpo", detail.Bouts[0].SideBName)
	assert.Equal(t, "Senpo", detail.Bouts[0].SideBPos)
	assert.Equal(t, "R-Senpo", detail.Bouts[0].Winner)
	// Elimination tally: W-Senpo lost so b=1, R-Senpo won so a=0.
	assert.Equal(t, 0, detail.EliminationA)
	assert.Equal(t, 1, detail.EliminationB)
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
	detail := buildKachinukiDetail(m, "label", map[string]string{})
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

// TestCollectKachinukiMatches_PoolMatchesWithBouts verifies that pool
// matches with sub-results are collected and returned.
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
			// No sub-results, should be skipped.
			ID:    "P1-1",
			SideA: "AlphaTeam",
			SideB: "BetaTeam",
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
	require.Len(t, out, 1, "only the match with sub-results should be collected")
	assert.Equal(t, "RedTeam", out[0].SideATeam)
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
// with no SubResults is skipped even if it has a kachinuki-exhaustion decision.
func TestCollectKachinukiMatches_WithBracketStub(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-bracket-stub"

	comp := &state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

	// Bracket match with kachinuki-exhaustion decision but no SubResults:
	// the export skips any bracket match where len(bm.SubResults) == 0.
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
	assert.Empty(t, out, "bracket match with no sub-results should be skipped")
}

// TestCollectKachinukiMatches_BracketWithSubResults verifies that a bracket
// match carrying real SubResults (from MaybeAdvanceKachinuki) produces a
// full detail entry with per-bout rows, not a stub.
func TestCollectKachinukiMatches_BracketWithSubResults(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-bracket-subs"

	comp := &state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

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
	assert.Equal(t, "RedTeam", out[0].Winner)
	require.Len(t, out[0].Bouts, 3, "three bouts should be present")
	assert.Equal(t, 1, out[0].Bouts[0].Position)
	assert.Equal(t, "R-Senpo", out[0].Bouts[0].SideAName)
	assert.Equal(t, "W-Senpo", out[0].Bouts[0].SideBName)
	assert.Equal(t, "R-Senpo", out[0].Bouts[0].Winner)
	assert.Equal(t, 3, out[0].Bouts[2].Position)
}

// TestCollectKachinukiMatches_BronzeWithSubResults verifies that the
// ThirdPlaceMatch sibling of bracket.Rounds is collected when it carries
// real SubResults.
func TestCollectKachinukiMatches_BronzeWithSubResults(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-bronze-subs"

	comp := &state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Naginata:      true,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

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
	assert.Equal(t, "3rd Place Match", out[0].Label)
	assert.Equal(t, "BlueTeam", out[0].Winner)
	require.Len(t, out[0].Bouts, 2)
	assert.Equal(t, "B1", out[0].Bouts[0].Winner)
}

// TestCollectKachinukiMatches_BronzeStub verifies the Naginata 3rd-place
// (bronze) match — a sibling of bracket.Rounds — is considered by the export
// at parity with Rounds matches (kachinuki-exhaustion stub, skipped when it
// has no bouts).
func TestCollectKachinukiMatches_BronzeStub(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "kachinuki-bronze-stub"

	comp := &state.Competition{
		ID:            compID,
		TeamMatchType: state.TeamMatchTypeKachinuki,
		TeamSize:      5,
		Naginata:      true,
	}
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{}))

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
	// Bronze stub has no bouts → skipped by the renderer guard, same as a
	// Rounds bracket stub.
	assert.Empty(t, out, "bronze stub with no bouts should be skipped")
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
