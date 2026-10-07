package helper

import (
	"fmt"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

// TestPoolsDoNotOverlap pins that each pool on the Pool Matches sheet ends
// before the next one starts, whatever its size. A pool of four teams (six
// match blocks, each followed by a spacing row) printed past the height the
// sheet gave it, so the next pool's header overwrote its fourth ranking row.
// Every ranking row must survive, a blank row must separate the two pools, and
// each "Pool X-Nth" link the Tree and Elimination Matches sheets use must point
// at the ranking row it names.
func TestPoolsDoNotOverlap(t *testing.T) {
	cases := []struct {
		name        string
		teamMatches int
		poolSize    int
	}{
		{name: "individual pools of 4", teamMatches: 0, poolSize: 4},
		{name: "individual pools of 6", teamMatches: 0, poolSize: 6},
		{name: "team pools of 2", teamMatches: 3, poolSize: 2},
		{name: "team pools of 3", teamMatches: 3, poolSize: 3},
		{name: "team pools of 4", teamMatches: 3, poolSize: 4},
		{name: "team pools of 5", teamMatches: 5, poolSize: 5},
		{name: "kachinuki pools of 4", teamMatches: 9, poolSize: 4},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pools := make([]Pool, 2)
			poolCoords := map[string]cellCoord{}
			pCoords := map[string]playerCellCoord{}
			for pi := range pools {
				name := fmt.Sprintf("Pool %c", 'A'+pi)
				pools[pi].PoolName = name
				poolCoords[name] = cellCoord{sheetName: SheetPoolDraw, cell: fmt.Sprintf("A%d", pi+1)}
				for k := range tc.poolSize {
					p := Player{Name: fmt.Sprintf("%s entrant %d", name, k+1), Dojo: fmt.Sprintf("%s dojo %d", name, k+1)}
					pools[pi].Players = append(pools[pi].Players, p)
					pCoords[playerCoordKey(p)] = playerCellCoord{cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: fmt.Sprintf("B%d", len(pCoords)+1)}}
				}
			}
			CreatePoolRoundRobinMatches(pools)

			f := excelize.NewFile()
			defer func() { _ = f.Close() }()
			_, err := f.NewSheet(SheetPoolMatches)
			require.NoError(t, err)
			_, err = f.NewSheet(SheetPoolDraw)
			require.NoError(t, err)
			matchWinners, _ := PrintPoolMatches(f, pools, tc.teamMatches, tc.poolSize, CourtLabels(1), nil, poolCoords, pCoords, false)

			cell := func(col string, row int) string {
				v, err := f.GetCellValue(SheetPoolMatches, col+strconv.Itoa(row))
				require.NoError(t, err)
				return v
			}
			headerRow := func(pool string) int {
				want := sheetRef(poolCoords[pool].sheetName, poolCoords[pool].cell)
				for r := 1; r < 1000; r++ {
					formula, err := f.GetCellFormula(SheetPoolMatches, "A"+strconv.Itoa(r))
					require.NoError(t, err)
					if formula == want {
						return r
					}
				}
				t.Fatalf("no header row for %s", pool)
				return 0
			}
			rankingRow := func(from int) int {
				for r := from; r < from+1000; r++ {
					if cell("G", r) == "Ranking" {
						return r
					}
				}
				t.Fatalf("no Ranking header below row %d", from)
				return 0
			}

			headerA, headerB := headerRow("Pool A"), headerRow("Pool B")
			rankingA := rankingRow(headerA)
			for k := 1; k <= tc.poolSize; k++ {
				assert.Equal(t, strconv.Itoa(k)+".", cell("F", rankingA+k), "Pool A ranking row %d survives", k)
			}
			lastA := rankingA + tc.poolSize
			assert.Greater(t, headerB, lastA+1, "Pool B starts after Pool A's last ranking row (%d) and a blank row", lastA)
			// The layout the pager counts is the one printed: Pool A's rows,
			// then its gap, then Pool B.
			layout := layPoolRow(pools[:1], tc.teamMatches)
			assert.Equal(t, headerA+layout.height()-layout.gap-1, lastA, "Pool A's last printed row is where its layout ends")
			assert.Equal(t, headerA+layout.height(), headerB, "Pool B starts where Pool A's layout ends")

			for _, pool := range []string{"Pool A", "Pool B"} {
				for k := 1; k <= tc.poolSize; k++ {
					mw, ok := matchWinners[pool+"-"+GetOrdinal(k)]
					require.True(t, ok, "%s-%s is registered", pool, GetOrdinal(k))
					_, row, err := excelize.SplitCellName(mw.cell)
					require.NoError(t, err)
					assert.Equal(t, strconv.Itoa(k)+".", cell("F", row), "%s-%s links to its own ranking row", pool, GetOrdinal(k))
				}
			}
		})
	}
}

// TestPoolPagesCountTheRowsASplitPoolUses pins the Pool Matches paging after a
// pool taller than a page: the page count takes exactly the rows the pool
// uses, so the next pool shares the page with its results when it fits. With
// teams of six, a pool of three splits after its match blocks (a break after
// row 29) and a pool of two then fits on that second page; counting the pool's
// results at their old padded height put the count two rows ahead and forced
// a third page (a break after row 47) for nothing.
func TestPoolPagesCountTheRowsASplitPoolUses(t *testing.T) {
	pools := make([]Pool, 2)
	poolCoords := map[string]cellCoord{}
	pCoords := map[string]playerCellCoord{}
	for pi, size := range []int{3, 2} {
		name := fmt.Sprintf("Pool %c", 'A'+pi)
		pools[pi].PoolName = name
		poolCoords[name] = cellCoord{sheetName: SheetPoolDraw, cell: fmt.Sprintf("A%d", pi+1)}
		for k := range size {
			p := Player{Name: fmt.Sprintf("%s team %d", name, k+1), Dojo: fmt.Sprintf("%s dojo %d", name, k+1)}
			pools[pi].Players = append(pools[pi].Players, p)
			pCoords[playerCoordKey(p)] = playerCellCoord{cellCoord: cellCoord{sheetName: SheetPoolDraw, cell: fmt.Sprintf("B%d", len(pCoords)+1)}}
		}
	}
	CreatePoolRoundRobinMatches(pools)

	f := excelize.NewFile()
	defer func() { _ = f.Close() }()
	_, err := f.NewSheet(SheetPoolMatches)
	require.NoError(t, err)
	_, err = f.NewSheet(SheetPoolDraw)
	require.NoError(t, err)
	PrintPoolMatches(f, pools, 6, 2, CourtLabels(1), nil, poolCoords, pCoords, false)

	buf, err := f.WriteToBuffer()
	require.NoError(t, err)
	breaks, err := bctest.RowBreaks(buf.Bytes(), SheetPoolMatches)
	require.NoError(t, err)
	assert.Equal(t, []int{29}, breaks, "one break, inside the pool of three; the pool of two follows its results")
	rows, err := f.GetRows(SheetPoolMatches)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(rows)-breaks[len(breaks)-1], PoolMatchesRowsPerPage, "the second page holds both")
}
