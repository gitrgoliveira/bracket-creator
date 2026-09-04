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

// TestExportCompetitionXlsx_RejectsRoundsOnlyDrawMismatch pins mp-yuy8 task
// 1's widening of the ErrBracketDrawMismatch guard from hasBronze (bracket.
// ThirdPlaceMatch != nil) to bracketHasKnockoutContent, which also catches a
// stored bracket carrying real Rounds content -- no third-place bout
// involved. Before the widening this shape fell through step 4 silently and
// rendered an Elimination Matches sheet with NO knockout content at all,
// exactly the "silently-partial workbook" ErrBracketDrawMismatch exists to
// prevent for the bronze-only case.
//
// Reachable the same way the bronze-only mismatch is (see
// TestExportCompetitionXlsx_RejectsBronzeOnlyDrawMismatch above and
// TestExportPipeline_BronzeOnlyMismatchErrorsInBothBuilders,
// internal/export/pipeline_parity_test.go, for the fuller drift story):
// comp.ExtraQualifiers carries no `started` guard in PUT
// /api/competitions/:id, so an operator can flip a Mixed competition's
// ExtraQualifiers to a value buildPoolFedDraw marks "out of scope" for the
// CURRENT pool shape after the original (non-naginata, no bronze block)
// bracket was already built. This fixture hand-constructs the resulting
// shape directly, the same simplification the bronze-only fixture uses:
// empty pools so EliminationDraw's re-derivation comes back nil (poolDraw:
// no pools; playoffLeaves: not pure playoffs since Format is Mixed, so nil)
// while the bracket's Rounds are still on disk from the original draw.
func TestExportCompetitionXlsx_RejectsRoundsOnlyDrawMismatch(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "rounds-only-mismatch"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:     compID,
		Name:   "Rounds Only Mismatch Comp",
		Kind:   "individual",
		Format: state.CompFormatMixed,
		Courts: []string{"A"},
	}))
	require.NoError(t, store.SavePools(compID, []helper.Pool{}))
	require.NoError(t, store.SavePoolMatches(compID, nil))
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:     "m-r1-0",
					SideA:  "Alice",
					SideB:  "Bob",
					Status: state.MatchStatusScheduled,
				},
			},
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

// TestExportCompetitionXlsx_EmptyFormatRendersKnockout pins mp-yuy8's
// state.Competition.EffectiveFormat fix: a competition whose Format was
// never set is standalone playoffs (runDrawPipeline's generation switch has
// always built a real bracket via generatePlayoffs for "" in its `default:`
// case, identically to the literal "playoffs" value), so its export must
// render a proper knockout rather than the empty Elimination Matches sheet
// the workbook.go KNOWN GAP used to produce. Before EffectiveFormat existed,
// IsPlayoffEnabled() and isPurePlayoffs() both compared Format literally and
// answered false for "", so EliminationDraw's leaf source (playoffLeaves)
// was never reached and step 4 rendered nothing -- with no error, so the
// gap was silent.
func TestExportCompetitionXlsx_EmptyFormatRendersKnockout(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "empty-format-knockout"

	createTestCompetition(t, store, compID, "", 0)
	saveTestParticipants(t, store, compID, []string{"Alice", "Bob", "Charlie", "Dave"})
	require.NoError(t, eng.StartCompetition(compID))

	f := openExportedWorkbook(t, eng, compID)
	elim, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	assert.Equal(t, 3, countEliminationMatchBlocks(elim),
		"a 4-entrant standalone playoffs bracket must render 3 match blocks (F-1) even though Format was never set")
}

// TestExportCompetitionXlsx_EmptyFormatRejectsDrawMismatch is the empty-Format
// twin of TestExportCompetitionXlsx_RejectsBronzeOnlyDrawMismatch: the same
// bronze-only mismatch shape (a stored bracket carrying a third-place bout
// but no re-derivable Rounds content), constructed for a competition whose
// Format is "" rather than the literal "playoffs" value. It must be refused
// with ErrBracketDrawMismatch, not rendered as a silently-partial workbook.
//
// Before EffectiveFormat, this shape was NOT refused for an empty-Format
// competition: IsPlayoffEnabled() answered false for Format == "", so the
// guard's own `comp.IsPlayoffEnabled() && bracketHasKnockoutContent(bracket)`
// condition never fired, and step 4 silently rendered an empty Elimination
// Matches sheet instead of erroring -- the reachable half of the KNOWN GAP
// workbook.go used to document.
func TestExportCompetitionXlsx_EmptyFormatRejectsDrawMismatch(t *testing.T) {
	eng, store, _ := setupTestEngine(t)
	compID := "empty-format-mismatch"

	require.NoError(t, store.SaveCompetition(&state.Competition{
		ID:       compID,
		Name:     "Empty Format Mismatch Comp",
		Kind:     "individual",
		Format:   "",
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
