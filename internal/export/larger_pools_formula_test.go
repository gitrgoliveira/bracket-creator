package export

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestBuildResultsWorkbook_LargerPools_CrossedQualifierHasLiveFormula mirrors
// internal/engine's TestExportCompetitionXlsx_LargerPools_CrossedQualifierHasLiveFormula
// for the SECOND Excel export path (the results workbook this package
// builds, as opposed to Engine.ExportCompetitionXlsx's blank-template one).
//
// bc-qual LP-3c review finding: helper.PrintPoolMatches only registers a
// matchWinners["<pool>-<ordinal>"] Excel cell-reference entry for ranks
// 1..numWinners, and this builder used to pass comp.EffectivePoolWinners()
// as that bound -- 1 for a larger-pools competition (which requires
// PoolWinners==1). An oversized pool's crossed qualifier is always rank 2,
// which exceeded that bound and had no matchWinners entry: the Tree sheet
// fell back to inert literal text, and the Elimination Matches sheet emitted
// a CONCATENATE formula whose second argument referenced an EMPTY sheet name
// and an empty cell (a broken formula Excel cannot evaluate).
// state.Competition.MatchWinnerRanksNeeded (mirrored by
// internal/engine/export.go and cmd/create-pools.go) fixes this: both
// exports of one competition must agree (the mp-ndfu invariant
// EliminationDraw's own doc comment states), so both had to be fixed
// together.
func TestBuildResultsWorkbook_LargerPools_CrossedQualifierHasLiveFormula(t *testing.T) {
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	courts := []string{"A", "B", "C", "D"}
	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatMixed
	comp.Status = "setup"
	comp.PoolSize = 3
	comp.PoolSizeMode = "min"
	comp.PoolWinners = 1
	comp.ExtraQualifiers = state.ExtraQualifiersLargerPools
	comp.Courts = courts
	require.NoError(t, store.SaveCompetition(comp))

	var players []domain.Player
	for i := 0; i < 97; i++ {
		players = append(players, domain.Player{
			Name: fmt.Sprintf("Player%03d", i),
			Dojo: fmt.Sprintf("Dojo%03d", i),
		})
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	foundCrossedFormula := false
	for _, sheet := range f.GetSheetList() {
		if !strings.HasPrefix(sheet, "Tree") && sheet != "Elimination Matches" {
			continue
		}
		rows, err := f.GetRows(sheet)
		require.NoError(t, err)
		for r := range rows {
			for c := 0; c < 40; c++ {
				addr, _ := excelize.CoordinatesToCellName(c+1, r+1)
				formula, ferr := f.GetCellFormula(sheet, addr)
				require.NoError(t, ferr)
				if formula == "" {
					continue
				}
				require.NotContainsf(t, formula, "''!", "broken CONCATENATE formula (empty sheet/cell ref) at %s!%s: %s", sheet, addr, formula)
				if strings.Contains(formula, "-2nd") && strings.Contains(formula, "'Pool Matches'!") {
					foundCrossedFormula = true
				}
			}
		}
	}
	assert.True(t, foundCrossedFormula, "expected a live CONCATENATE(\"Pool <X>-2nd \",'Pool Matches'!<cell>) formula for the oversized pool's crossed qualifier")
}
