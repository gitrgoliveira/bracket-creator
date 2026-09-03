package engine

import (
	"testing"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestExportCompetitionXlsx_RejectsBronzeOnlyDrawMismatch pins mp-yuy8 PHASE
// 3's correction: a stored bracket that already carries a third-place bout,
// but whose knockout draw cannot be re-derived from the competition's
// CURRENT settings, must be refused with ErrBracketDrawMismatch rather than
// rendering an Elimination Matches sheet that carries only the lone
// 3rd-place block and no other knockout content -- a silently-partial
// workbook with no way for the operator to tell it is partial.
//
// Reachable through a real write path: comp.ExtraQualifiers carries no
// `started` guard in PUT /api/competitions/:id
// (internal/mobileapp/handlers_competition.go), unlike its
// Naginata/Engi/Format/Kind/TeamMatchType siblings, so an operator can flip
// it after the bracket -- bronze block included -- was already built, and
// EliminationDraw's re-derivation then comes back nil for the CURRENT pool
// shape while bracket.ThirdPlaceMatch is still on disk from the original
// draw. This fixture hand-constructs that resulting shape directly (see
// internal/export/pipeline_parity_test.go's twin test for the fuller
// derivation story, mirrored here so the engine package pins its own
// sentinel independent of internal/export).
func TestExportCompetitionXlsx_RejectsBronzeOnlyDrawMismatch(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "bronze-mismatch"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Bronze Mismatch Comp",
		Kind:     "individual",
		Format:   state.CompFormatPlayoffs,
		Naginata: true,
		Courts:   []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:     "m-bronze",
			Status: state.MatchStatusScheduled,
		},
	}))

	data, err := eng.ExportCompetitionXlsx(compID)
	require.Error(t, err)
	assert.Nil(t, data)
	assert.ErrorIs(t, err, ErrBracketDrawMismatch)
}

// TestExportTournamentWorkbooks_SkipsBracketDrawMismatchAndReportsIt mirrors
// TestExportTournamentWorkbooks_SkipsSwissAndReportsIt: one competition in
// this state must not abort the whole print booklet for every OTHER
// competition in the tournament. It is skipped and reported back to the
// caller (via the second return value), routed through the same
// isUnexportable predicate as the Swiss sentinel.
func TestExportTournamentWorkbooks_SkipsBracketDrawMismatchAndReportsIt(t *testing.T) {
	eng, store, _ := setupTestEngine(t)

	mismatchID := "bronze-mismatch-comp"
	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       mismatchID,
		Name:     "Bronze Mismatch Comp",
		Kind:     "individual",
		Format:   state.CompFormatPlayoffs,
		Naginata: true,
		Courts:   []string{"A"},
	}))
	require.NoError(t, store.SavePools(mismatchID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(mismatchID, nil))
	require.NoError(t, store.SaveBracket(mismatchID, &state.Bracket{
		Rounds: [][]state.BracketMatch{},
		ThirdPlaceMatch: &state.BracketMatch{
			ID:     "m-bronze",
			Status: state.MatchStatusScheduled,
		},
	}))

	createTestCompetition(t, store, "league-comp", state.CompFormatLeague, 3, func(c *state.Competition) {
		c.Name = "League Comp"
	})
	saveTestParticipants(t, store, "league-comp", []string{"Eve", "Frank", "Grace"})
	require.NoError(t, eng.StartCompetition("league-comp"))

	tmpDir := t.TempDir()
	sources, skipped, err := eng.ExportTournamentWorkbooks(tmpDir, mismatchID, "league-comp")
	require.NoError(t, err, "one bracket-draw-mismatch competition must not abort the whole batch")

	require.Len(t, sources, 1, "the renderable competition must still be exported")
	assert.Equal(t, "League Comp", sources[0].Title)

	require.Len(t, skipped, 1, "the mismatched competition must be reported as skipped, not silently dropped")
	assert.Equal(t, mismatchID, skipped[0].ID)
	assert.Equal(t, "Bronze Mismatch Comp", skipped[0].Name)
	assert.NotEmpty(t, skipped[0].Reason)
}
