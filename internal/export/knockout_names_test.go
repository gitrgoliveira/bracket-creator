package export

import (
	"bytes"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// TestBuildResultsWorkbook_KnockoutUnresolvedSidesReadAsTheSheetNamesThem pins
// the knockout-only results export's entrant names: a side an earlier match
// still has to decide reads "M n", the name the Elimination Matches sheet's
// own formula gives it, and never the stored "Winner of r2-m1" placeholder;
// a decided side reads the competitor. A team bracket repeats the names on its
// summary row.
func TestBuildResultsWorkbook_KnockoutUnresolvedSidesReadAsTheSheetNamesThem(t *testing.T) {
	cases := []struct {
		name     string
		teamSize int
	}{
		{name: "individual", teamSize: 0},
		{name: "team match of 3", teamSize: 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir, store, eng, compID := testSetup(t)
			defer os.RemoveAll(dir)

			comp, err := store.LoadCompetition(compID)
			require.NoError(t, err)
			comp.Kind = "individual"
			if tc.teamSize > 0 {
				comp.Kind = "team"
			}
			comp.TeamSize = tc.teamSize
			comp.Format = state.CompFormatKnockout
			comp.Status = state.CompStatusSetup
			comp.StartTime = "09:00"
			require.NoError(t, store.SaveCompetition(comp))
			names := []string{"Ann", "Bea", "Cid", "Dan", "Eve", "Fay"}
			players := make([]domain.Player, len(names))
			for i, n := range names {
				players[i] = domain.Player{Name: n, Dojo: n + " Dojo"}
			}
			require.NoError(t, store.SaveParticipants(compID, players))
			require.NoError(t, eng.StartCompetition(compID))

			data, err := BuildResultsWorkbook(store, eng, compID)
			require.NoError(t, err)
			f, err := excelize.OpenReader(bytes.NewReader(data))
			require.NoError(t, err)
			defer f.Close()
			rows, err := f.GetRows(helper.SheetEliminationMatches)
			require.NoError(t, err)

			boutRows := comp.TeamBoutRows()
			var finalSides []string
			for r, row := range rows {
				if len(row) == 0 || parseRoundMatchLabel(row[0]) <= 0 {
					continue
				}
				entrantRows := []int{r + 2} // 0-based: header, White/Red, entrants
				if boutRows > 0 {
					entrantRows = append(entrantRows, r+4+boutRows)
				}
				for _, er := range entrantRows {
					for _, col := range []int{0, 6} {
						name := cellAt(rows, er, col)
						assert.NotContains(t, name, "Winner of", "%s, row %d: a side still to be decided reads as the sheet names it", row[0], er+1)
						isMatchRef := strings.HasPrefix(name, "M ")
						assert.Truef(t, isMatchRef || slices.Contains(names, name), "%s, row %d: %q is a competitor or an M n reference", row[0], er+1, name)
					}
				}
				finalSides = []string{cellAt(rows, r+2, 0), cellAt(rows, r+2, 6)}
			}
			// Six entrants: two first-round bouts (1, 2), two semi-finals (3, 4)
			// and the final (5), whose sides are the semi-finals.
			assert.ElementsMatch(t, []string{helper.MatchRefLabel(3), helper.MatchRefLabel(4)}, finalSides, "the final reads M 3 and M 4")
		})
	}
}
