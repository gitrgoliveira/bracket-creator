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

// TestGenerateLeagueTiebreakMatches_RejectsBadIDs is the bc-pnum conversion of
// the former TestGenerateLeagueTiebreakMatches_RejectsBadNames: selection is
// now id-only (operator ruling bc-pnum), so "duplicate or unknown" is a
// property of tiedTeamIDs, not of names. An unknown or duplicate NAME can no
// longer reach this validation at all, since names are never looked at.
func TestGenerateLeagueTiebreakMatches_RejectsBadIDs(t *testing.T) {
	compID := "lt-badids"
	eng, store := setupTeamPoolComp(t, compID, true) // Alpha/Beta/Gamma all tied, complete
	ids := teamIDsByName(t, store, compID, []string{"Alpha", "Beta", "Gamma"})
	alphaID, betaID, gammaID := ids[0], ids[1], ids[2]

	t.Run("unknown team id", func(t *testing.T) {
		_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{alphaID, "id-does-not-exist"})
		require.Error(t, err, "an unknown team id must be rejected, not silently dropped")
	})
	t.Run("duplicate team id", func(t *testing.T) {
		_, err := eng.GenerateLeagueTiebreakMatches(compID, []string{alphaID, alphaID})
		require.Error(t, err, "a duplicate team id must be rejected")
	})
	t.Run("valid group succeeds", func(t *testing.T) {
		injected, err := eng.GenerateLeagueTiebreakMatches(compID, []string{alphaID, betaID, gammaID})
		require.NoError(t, err)
		assert.Len(t, injected, 3, "3-team round-robin → 3 tie-break bouts")
	})
}

// TestGenerateLeagueTiebreakMatches_AmbiguousNameDiagnosis pinned the former
// behaviour where a requested NAME matching TWO standings entries (a
// namesake collision) had to be diagnosed as "ambiguous" rather than "not
// found". DELETED (not converted): selection is now id-only (operator ruling
// bc-pnum), and per the ID-only selection comment in GenerateLeagueTiebreakMatches,
// an id names exactly one competitor by construction, so the ambiguous-name
// diagnosis this test pinned can no longer arise -- there is no name lookup
// left to be ambiguous. The exact same namesake fixture is preserved, tested
// via id selection instead, by TestGenerateLeagueTiebreakMatches_TeamIDsResolveNamesakeCollision
// below.

// TestGenerateLeagueTiebreakMatches_TeamIDsResolveNamesakeCollision is the
// bc-idfx finding 11 companion to the ambiguous-name test above: the EXACT
// same namesake-collision fixture (two "Team X" from different dojos) that
// name-based selection cannot disambiguate must succeed when the operator
// selects by participant id (tiedTeamIDs) instead.
func TestGenerateLeagueTiebreakMatches_TeamIDsResolveNamesakeCollision(t *testing.T) {
	compID := "lt-namesake-by-id"
	eng, store, _ := setupTestEngine(t)

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Namesake By ID Test",
		Format:   state.CompFormatLeague,
		Status:   state.CompStatusPools,
		Courts:   []string{"A"},
		Kind:     "team",
		TeamSize: 2,
	}))
	// Two DIFFERENT teams share the display name "Team X" (different dojos,
	// the documented checkNewTeamNameCollisions enforcement-hole shape).
	require.NoError(t, store.SavePools(compID, []helper.Pool{
		{PoolName: "Pool A", Players: []helper.Player{
			{ID: "id-team-x-dojo-a", Name: "Team X", Dojo: "Dojo A"},
			{ID: "id-team-x-dojo-b", Name: "Team X", Dojo: "Dojo B"},
		}},
	}))

	injected, err := eng.GenerateLeagueTiebreakMatches(compID, []string{"id-team-x-dojo-a", "id-team-x-dojo-b"})
	require.NoError(t, err, "selecting by id must resolve the exact same collision name selection cannot")
	require.Len(t, injected, 1, "a 2-team round-robin is exactly one DH bout")
	m := injected[0]
	assert.ElementsMatch(t, []string{"id-team-x-dojo-a", "id-team-x-dojo-b"}, []string{m.SideAID, m.SideBID},
		"the generated bout must be stamped with BOTH teams' real ids, not merged under the shared name")
}
