package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLeagueTiebreakCandidates_EmptyUntilRegularComplete pins the Copilot fix:
// candidates must be empty while any regular league match is still pending,
// otherwise provisional mid-league standings (everyone tied at 0 points) would
// surface a spurious "everyone tied" group and pop the banner prematurely.
func TestLeagueTiebreakCandidates_EmptyUntilRegularComplete(t *testing.T) {
	compID := "lt-incomplete"
	eng, store, _ := setupTestEngine(t)
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID: compID, Name: compID, Kind: "team", Format: state.CompFormatLeague,
		TeamSize: 2, PoolSize: 3, RoundRobin: true, Courts: []string{"A"}, Status: state.CompStatusPools,
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{{Name: "Alpha", Dojo: "Dojo Alpha"}, {Name: "Beta", Dojo: "Dojo Beta"}, {Name: "Gamma", Dojo: "Dojo Gamma"}}},
	}))
	// Three round-robin matches, only the FIRST completed (as a draw); the other
	// two still scheduled.
	draw := "hikiwake"
	matches := []state.MatchResult{
		{ID: "Pool A-0", SideA: "Alpha", SideB: "Beta", Status: state.MatchStatusCompleted, Decision: draw,
			SubResults: []state.SubMatchResult{{Position: 1, SideA: "Alpha", SideB: "Beta", Decision: draw}}},
		{ID: "Pool A-1", SideA: "Alpha", SideB: "Gamma", Status: state.MatchStatusScheduled},
		{ID: "Pool A-2", SideA: "Beta", SideB: "Gamma", Status: state.MatchStatusScheduled},
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))

	cands, err := eng.LeagueTiebreakCandidates(compID)
	require.NoError(t, err)
	assert.Empty(t, cands, "no candidates while regular matches are still pending")

	// Complete the remaining matches as draws → everyone tied → candidates appear.
	for i := range matches {
		matches[i].Status = state.MatchStatusCompleted
		matches[i].Decision = draw
		matches[i].SubResults = []state.SubMatchResult{{Position: 1, SideA: matches[i].SideA, SideB: matches[i].SideB, Decision: draw}}
	}
	require.NoError(t, store.SavePoolMatches(compID, matches))
	cands, err = eng.LeagueTiebreakCandidates(compID)
	require.NoError(t, err)
	assert.NotEmpty(t, cands, "candidates appear once every regular match is complete")
}

// TestGenerateLeagueTiebreakMatches_RejectsBadNames pins the Copilot fix: the
// engine method must reject duplicate or unknown team names rather than silently
// building a partial group.
func TestGenerateLeagueTiebreakMatches_RejectsBadNames(t *testing.T) {
	compID := "lt-badnames"
	eng, _ := setupTeamPoolComp(t, compID, true) // Alpha/Beta/Gamma all tied, complete

	t.Run("unknown team name", func(t *testing.T) {
		_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{"Alpha", "Zeta"})
		require.Error(t, err, "an unknown team must be rejected, not silently dropped")
	})
	t.Run("duplicate team name", func(t *testing.T) {
		_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{"Alpha", "Alpha"})
		require.Error(t, err, "a duplicate team must be rejected")
	})
	t.Run("valid group succeeds", func(t *testing.T) {
		injected, err := eng.GenerateLeagueTiebreakMatches(compID, []string{"Alpha", "Beta", "Gamma"})
		require.NoError(t, err)
		assert.Len(t, injected, 3, "3-team round-robin → 3 tie-break bouts")
	})
}

// TestGenerateLeagueTiebreakMatches_AmbiguousNameDiagnosis is the regression
// guard for the finding that a requested name matching TWO standings entries
// (a namesake collision -- team names must be unique by rule, but
// checkNewTeamNameCollisions has documented enforcement holes) was
// misdiagnosed as "not found": len(tiedGroup) != len(nameSet) is also true
// when a name matches MORE than once, not just zero times, and the old
// unconditional message sent the operator hunting for a team that was
// actually on the sheet, twice. The fix must name the ambiguous team and
// must NOT claim it was "not found".
func TestGenerateLeagueTiebreakMatches_AmbiguousNameDiagnosis(t *testing.T) {
	compID := "lt-ambiguous-name"
	eng, store, _ := setupTestEngine(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Ambiguous Name Test",
		Format:   state.CompFormatLeague,
		Status:   state.CompStatusPools,
		Courts:   []string{"A"},
		Kind:     "team",
		TeamSize: 2,
	}))
	// Two DIFFERENT teams share the display name "Team X" (different dojos,
	// the documented enforcement-hole shape); "Team Y" is unambiguous.
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{Name: "Team X", Dojo: "Dojo A"}, {Name: "Team X", Dojo: "Dojo B"}, {Name: "Team Y", Dojo: "Dojo C"},
		}},
	}))
	// No pool matches needed: CalculatePoolStandings returns one entry per
	// pool player even with zero matches played, and GenerateLeagueTiebreakMatches
	// does not re-validate consequentiality (the handler is the gate).

	_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{"Team X", "Team Y"})
	require.Error(t, err, "an ambiguous team name must be rejected")
	assert.NotContains(t, err.Error(), "not found",
		"a name that matched twice was FOUND, just ambiguously -- must not be reported as missing")
	assert.Contains(t, err.Error(), "Team X",
		"the error must name the ambiguous team so the operator isn't left guessing")
}
