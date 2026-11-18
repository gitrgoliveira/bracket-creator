package export

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	bctest "github.com/gitrgoliveira/bracket-creator/internal/test"
)

func TestBuildResultsWorkbook_BracketScores(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	// Bracket with one completed match.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:          "B1",
					SideA:       "Alice",
					SideB:       "Charlie",
					Winner:      "Alice",
					Status:      state.MatchStatusCompleted,
					IpponsA:     []string{"M", "K"},
					IpponsB:     []string{"M"},
					Decision:    "fought",
					MatchNumber: 1,
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	sheets := f.GetSheetList()
	assert.Contains(t, sheets, helper.SheetEliminationMatches,
		"elimination matches sheet must exist when bracket has rounds")
}

// TestBuildResultsWorkbook_BracketHanteiScoreCell pins the bracket-overlay
// fix for a double-printed judges'-decision mark. A BracketMatch carries its
// scoreline as ippon arrays, and the live scoring path embeds the mark
// inline as a literal domain.HanteiMark entry in the winner's slice.
// writeScoreRowCells separately appends "Ht" again via SideMarksLR (which
// reads the mark off the MatchResult view), so rendering the array verbatim
// would render "MHt Ht" -- the mark twice. The pool path
// (TestBuildResultsWorkbook_ResultMarksInScoreCells) already filters through
// domain.IpponsScore first, rendering "M Ht"; the bracket overlay must match it
// exactly, not double the mark.
func TestBuildResultsWorkbook_BracketHanteiScoreCell(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setCompFormat(t, store, compID, state.CompFormatMixed)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	// Alice beats Charlie, decided by hantei: her ippon array carries the
	// mark inline, exactly as the live scoring path writes it.
	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:          "B1",
					SideA:       "Alice",
					SideB:       "Charlie",
					Winner:      "Alice",
					Status:      state.MatchStatusCompleted,
					IpponsA:     []string{"M", domain.HanteiMark},
					Decision:    "fought",
					MatchNumber: 1,
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	// Alice is SideA (Aka), so her cell is the RIGHT one.
	_, right := bracketVictoryCells(t, rows, "Round 1 - Match 1")
	assert.Equal(t, "M Ht", right,
		"the winner's bracket cell must render the score plus one Ht mark, mirroring the pool cell")
	assert.False(t, sheetContainsCell(rows, "MHt Ht"),
		"the judges'-decision mark must not be double-printed")
}

func TestBuildResultsWorkbook_BracketScoresWithWinner(t *testing.T) {
	// Verifies that a completed bracket match has score cells overlaid and the
	// winner name is written into the result cell below the match block.
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)
	setCompFormat(t, store, compID, state.CompFormatMixed)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:          "B1",
					SideA:       "Alice",
					SideB:       "Charlie",
					Winner:      "Alice",
					Status:      state.MatchStatusCompleted,
					IpponsA:     []string{"M", "K"},
					IpponsB:     []string{"M"},
					Decision:    "fought",
					MatchNumber: 1,
				},
				{
					ID:          "B2",
					SideA:       "Bob",
					SideB:       "Dave",
					Winner:      "Dave",
					Status:      state.MatchStatusCompleted,
					IpponsA:     []string{"M"},
					IpponsB:     []string{"M", "K"},
					Decision:    "fought",
					MatchNumber: 2,
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	foundMK := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "MK" {
				foundMK = true
				break
			}
		}
		if foundMK {
			break
		}
	}
	assert.True(t, foundMK, "elimination matches sheet must contain score 'MK'")
}

func TestBuildResultsWorkbook_BracketKiken(t *testing.T) {
	// Verifies that a kiken decision in the bracket produces a suffix in the
	// elimination matches sheet.
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)
	setCompFormat(t, store, compID, state.CompFormatMixed)

	pools := makePools()
	require.NoError(t, store.SavePools(compID, pools))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	bracket := &state.Bracket{
		Rounds: [][]state.BracketMatch{
			{
				{
					ID:          "B1",
					SideA:       "Alice",
					SideB:       "Charlie",
					Winner:      "Alice",
					Status:      state.MatchStatusCompleted,
					IpponsA:     []string{"M"},
					Decision:    "kiken-voluntary",
					MatchNumber: 1,
				},
			},
		},
	}
	require.NoError(t, store.SaveBracket(compID, bracket))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	foundKiken := false
	for _, row := range rows {
		for _, cell := range row {
			if cell == "Kiken" {
				foundKiken = true
				break
			}
		}
		if foundKiken {
			break
		}
	}
	assert.True(t, foundKiken, "elimination matches sheet must contain 'Kiken' suffix")
}

// TestBuildResultsWorkbook_KnockoutBracket covers the pure-knockout format, which
// has NO pools: the elimination skeleton must be seeded from the participants (as
// engine.generateKnockout does) so the stored bracket renders and its scores overlay.
// TestKnockoutLeavesFromBracket unit-tests the leaf-order reconstruction: each
// round-1 match contributes SideA then SideB in order, byes included as "".
func TestKnockoutLeavesFromBracket(t *testing.T) {
	t.Parallel()
	assert.Nil(t, engine.KnockoutLeavesFromBracket(nil))
	assert.Nil(t, engine.KnockoutLeavesFromBracket(&state.Bracket{}))

	br := &state.Bracket{Rounds: [][]state.BracketMatch{
		{
			{SideA: "Alice", SideB: "Dave"},
			{SideA: "Carol", SideB: ""}, // bye
		},
		{{SideA: "Winner of r1-m0", SideB: "Winner of r1-m1"}},
	}}
	assert.Equal(t, []string{"Alice", "Dave", "Carol", ""}, engine.KnockoutLeavesFromBracket(br))
}

// TestBuildResultsWorkbook_KnockoutNonPow2TopologyMatchesBracket is the regression
// test proving the export skeleton is derived from the FROZEN bracket, not
// recomputed from participants. For a non-power-of-two roster the engine pads the
// bracket to the next pow2 with byes (buildBracketFromDraw), so a 6-entry
// knockout is STORED as an 8-leaf, 7-node tree in which two nodes are structural
// byes and one is an empty-vs-empty phantom.
//
// The printed sheet must carry a block per REAL bout and no more. A 6-entrant
// knockout is 6-1 = 5 bouts, because every bout eliminates exactly one entrant;
// the stored bracket agrees, numbering exactly those 5 (MatchNumber > 0) and
// leaving the byes/phantoms Hidden at 0.
//
// This assertion used to read `bracketNodes` (7), which counted the stored byes
// and phantoms as printable blocks. That was the defect this file's own fixture
// had frozen (bc-cse): the export rebuilt the pow2 leaf array with
// helper.CreateBalancedTree, which gives every "" slot a node, so it drew and
// numbered a junction for each phantom pair and printed 7 blocks numbered 1-7
// against a bracket numbered 1-5. Blocks 6 and 7 named no match at all and
// blocks 1-5 named the WRONG ones, so overlayBracketScores -- which matches
// blocks to matches BY printed number -- wrote every result into the wrong
// block. The fix rebuilds through helper.BuildSlotTree, which collapses an
// all-empty half exactly as the pool-fed draw does.
//
// The count alone no longer distinguishes the frozen bracket from a recompute
// off participants.csv (both are 5 nodes), so the mp-ndfu guard the test exists
// for is carried by the two assertions below it instead: the printed NUMBERS
// must be the stored bracket's own, and a bracket carrying an entrant name that
// participants.csv does not have must still be what the pages print.
func TestBuildResultsWorkbook_KnockoutNonPow2TopologyMatchesBracket(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatKnockout
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	players := make([]domain.Player, 6) // non-power-of-two -> 2 byes when padded to 8
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%d", i+1), Dojo: "D"}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	for ri := range br.Rounds {
		for mi := range br.Rounds[ri] {
			m := &br.Rounds[ri][mi]
			if m.SideA != "" && m.SideB != "" {
				m.Winner, m.Status, m.Decision = m.SideA, state.MatchStatusCompleted, "fought"
				m.IpponsA = []string{"M", "K"}
			}
		}
	}
	// Rename one round-1 entrant IN THE BRACKET ONLY. participants.csv still says
	// "P1", so a skeleton recomputed from the participants could not know this
	// name; the tree page printing it is what proves the frozen bracket is the
	// source (mp-ndfu). Renaming rather than reordering keeps the draw's shape,
	// and therefore every other assertion here, untouched.
	frozenOnlyName := "Zulu-Only-In-The-Bracket"
	renamed := false
	for mi := range br.Rounds[0] {
		m := &br.Rounds[0][mi]
		if m.SideA == "P1" {
			m.SideA, renamed = frozenOnlyName, true
			break
		}
	}
	require.True(t, renamed, "fixture must find P1 in the stored first round")
	require.NoError(t, store.SaveBracket(compID, br))

	// The bracket's own numbering: exactly the real bouts, byes and phantoms at 0.
	bracketNumbers := []int{}
	for _, round := range br.Rounds {
		for _, m := range round {
			if m.MatchNumber > 0 {
				bracketNumbers = append(bracketNumbers, m.MatchNumber)
			}
		}
	}
	slices.Sort(bracketNumbers)
	// Pinned from first principles, not from what the code emits: N entrants are
	// eliminated down to one champion by exactly N-1 bouts.
	require.Equal(t, len(players)-1, len(bracketNumbers),
		"a %d-entrant knockout is %d bouts", len(players), len(players)-1)

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	printed := []int{}
	for _, row := range rows {
		for _, cell := range row {
			if n := parseRoundMatchLabel(cell); n > 0 {
				printed = append(printed, n)
			}
		}
	}
	slices.Sort(printed)
	assert.Equal(t, bracketNumbers, printed,
		"the printed match blocks must be exactly the stored bracket's numbered bouts: "+
			"a block numbering a bye, a phantom or a number the bracket never issued sends "+
			"overlayBracketScores' by-number lookup into the wrong block")

	treeRows, err := f.GetRows("Tree 1")
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(treeRows, frozenOnlyName),
		"the bracket page must print the STORED bracket's entrant, not one recomputed from participants.csv")
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
}

// TestBuildResultsWorkbook_RaggedKnockoutScoresLandInTheRightBlocks is the
// corruption test for bc-cse. overlayBracketScores and overlayKnockoutBracketNames
// both find a match's score block BY ITS PRINTED NUMBER, so the printed numbering
// is the only thing tying a result to a bout. If the Tree page numbers a
// different bout N than the bracket does, the delivered workbook is silently
// wrong: the block headed "Match N" carries some other pair's names and score
// while the tree junction numbered N sits over this pair.
//
// A ragged (non-power-of-two) roster is the case that breaks, because only then
// does the leaf array carry "" bye slots for a tree builder to mistake for
// entrants. At 5 entrants the export used to print 7 junctions for a 4-bout
// draw, and the tree's Match 1 (P001 v P002) collided with the bracket's Match 1
// (P004 v P005), so P004/P005's score was written into the block the tree points
// at for P001/P002.
//
// The check is made on the ARTIFACT, both sheets of the one workbook: every
// score block must name competitors that the tree's junction of the same number
// actually sits above, and must carry that bracket match's scores.
func TestBuildResultsWorkbook_RaggedKnockoutScoresLandInTheRightBlocks(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatKnockout
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))

	// 5 entrants: the smallest roster with byes at two different depths, so a
	// bye-blind rebuild mis-numbers rather than merely mis-counting.
	players := make([]domain.Player, 5)
	for i := range players {
		players[i] = domain.Player{Name: fmt.Sprintf("P%03d", i+1), Dojo: fmt.Sprintf("D%d", i+1)}
	}
	require.NoError(t, store.SaveParticipants(compID, players))
	require.NoError(t, eng.StartCompetition(compID))

	// Score every real bout THROUGH THE ENGINE, round by round, so each winner
	// propagates into the next round's slots and the later blocks carry resolved
	// names rather than "Winner of ..." placeholders. Each bout gets a DISTINCT
	// score, so a result written into the wrong block shows up as a wrong value
	// and not merely a wrong position.
	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, br)
	ippons := [][]string{{"M"}, {"K"}, {"D"}, {"T"}}
	for ri := range br.Rounds {
		br, err = store.LoadBracket(compID) // pick up the previous round's propagation
		require.NoError(t, err)
		for _, m := range br.Rounds[ri] {
			if m.MatchNumber <= 0 {
				continue
			}
			require.LessOrEqual(t, m.MatchNumber, len(ippons), "fixture needs a distinct score per bout")
			require.NoError(t, eng.RecordMatchResult(compID, m.ID, &state.MatchResult{
				SideA:    m.SideA,
				SideB:    m.SideB,
				Winner:   m.SideA,
				IpponsA:  ippons[m.MatchNumber-1],
				Decision: "fought",
				Status:   state.MatchStatusCompleted,
			}))
		}
	}

	br, err = store.LoadBracket(compID)
	require.NoError(t, err)
	byNumber := map[int]state.BracketMatch{}
	for _, round := range br.Rounds {
		for _, m := range round {
			if m.MatchNumber > 0 {
				require.Equalf(t, state.MatchStatusCompleted, m.Status,
					"fixture must score every bout; Match %d is %q", m.MatchNumber, m.Status)
				byNumber[m.MatchNumber] = m
			}
		}
	}
	require.Len(t, byNumber, len(players)-1,
		"a %d-entrant knockout is %d bouts", len(players), len(players)-1)

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	// (1) Read the rendered tree page back: junction number -> the entrants
	//     printed under it. One court, so the whole draw is on "Tree 1".
	treeRows, err := f.GetRows("Tree 1")
	require.NoError(t, err)
	type node struct{ lo, hi int }
	junctions := map[int]node{}
	type leaf struct {
		row   int
		label string
	}
	var leaves []leaf
	for r, row := range treeRows {
		for c, value := range row {
			col, rowNum := c+1, r+1
			if value == "" || col < helper.TreeLabelCol {
				continue
			}
			lo, hi, ok := treeNodeRowSpan(col, rowNum)
			if !ok {
				continue
			}
			if n, convErr := strconv.Atoi(value); convErr == nil {
				// A bare integer on a node position is a printed match number;
				// an entrant label never parses as one.
				_, dup := junctions[n]
				require.Falsef(t, dup, "match number %d printed twice on Tree 1", n)
				junctions[n] = node{lo: lo, hi: hi}
				continue
			}
			leaves = append(leaves, leaf{row: rowNum, label: value})
		}
	}
	entrantNames := make([]string, len(players))
	for i, p := range players {
		entrantNames[i] = p.Name
	}
	printedLabels := make([]string, 0, len(leaves))
	for _, l := range leaves {
		printedLabels = append(printedLabels, l.label)
	}
	require.ElementsMatch(t, entrantNames, printedLabels,
		"the scan must find exactly the roster, or the comparisons below are vacuous")

	printedNumbers := make([]int, 0, len(junctions))
	for n := range junctions {
		printedNumbers = append(printedNumbers, n)
	}
	slices.Sort(printedNumbers)
	bracketNumbers := make([]int, 0, len(byNumber))
	for n := range byNumber {
		bracketNumbers = append(bracketNumbers, n)
	}
	slices.Sort(bracketNumbers)
	require.Equal(t, bracketNumbers, printedNumbers,
		"the tree page must number exactly the bracket's bouts: an extra junction is a bye's "+
			"empty slots drawn as a bout, and it shifts every number after it")

	// (2) Read the score blocks back and check each against the bout the TREE
	//     says carries that number. Individual layout, one court: the header at
	//     row H puts entrant names in columns A and G of row H+2, with their
	//     scores in B and F (overlayKnockoutBracketNames / writeScoreRowCells):
	//     SideB (Shiro) on the left, SideA (Aka) on the right.
	elimRows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	checked := 0
	for rowIdx, row := range elimRows {
		for headerCol, cell := range row {
			num := parseRoundMatchLabel(cell)
			if num <= 0 {
				continue
			}
			bm, ok := byNumber[num]
			require.Truef(t, ok, "block %q names a match the bracket never numbered", cell)
			j, ok := junctions[num]
			require.Truef(t, ok, "block %q has no junction on the tree page", cell)

			scoreRow := rowIdx + 2 // header, White/Red labels, then names+scores
			nameB := cellAt(elimRows, scoreRow, headerCol)
			nameA := cellAt(elimRows, scoreRow, headerCol+6)
			scoreB := cellAt(elimRows, scoreRow, headerCol+1)
			scoreA := cellAt(elimRows, scoreRow, headerCol+5)

			// The block's competitors must be ones the tree's junction of this
			// number sits above. This is the assertion the defect broke: under
			// it, block 1 named P004/P005 while junction 1 spanned P001/P002.
			under := map[string]bool{}
			for _, l := range leaves {
				if l.row >= j.lo && l.row <= j.hi {
					under[l.label] = true
				}
			}
			for _, name := range []string{nameA, nameB} {
				assert.Truef(t, under[name], "%q is printed in the block for Match %d, but the tree's "+
					"junction %d sits above %v instead", name, num, num, slices.Sorted(maps.Keys(under)))
			}

			assert.Equal(t, bm.SideB, nameB, "Match %d left entrant", num)
			assert.Equal(t, bm.SideA, nameA, "Match %d right entrant", num)
			assert.Equal(t, domain.IpponsScore(bm.IpponsB), scoreB, "Match %d left score", num)
			assert.Equal(t, domain.IpponsScore(bm.IpponsA), scoreA, "Match %d right score", num)
			checked++
		}
	}
	assert.Equal(t, len(byNumber), checked, "every bout must have exactly one score block")
}

func TestBuildResultsWorkbook_KnockoutBracket(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatKnockout
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "D"}, {Name: "Bob", Dojo: "D"}, {Name: "Carol", Dojo: "D"}, {Name: "Dave", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	// Complete the generated bracket's real matches.
	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, br)
	for ri := range br.Rounds {
		for mi := range br.Rounds[ri] {
			m := &br.Rounds[ri][mi]
			if m.SideA != "" && m.SideB != "" {
				m.Winner = m.SideA
				m.Status = state.MatchStatusCompleted
				m.IpponsA = []string{"M", "K"}
				m.Decision = "fought"
			}
		}
	}
	require.NoError(t, store.SaveBracket(compID, br))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "MK"),
		"knockout (no pools) must render the bracket with its literal score 'MK'")
	assert.True(t, sheetContainsCell(rows, "Alice"),
		"knockout bracket must render literal entrant names (no pool data to reference)")
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
}

// TestBuildResultsWorkbook_KnockoutCellRefLikeNames is a regression test for
// mp-uagg: internal/helper.printSingleEliminationMatch used to decide leaf- vs
// match-feeder nodes by asking whether Node.LeafVal parsed as an Excel cell
// reference (excelize.SplitCellName). A no-pools knockout bracket renders raw
// participant names as leaves (engine.KnockoutLeavesFromBracket), so a competitor
// named like a cell coordinate ("P1" = column P row 1, "M3", "A4") was
// misclassified as a match-feeder, producing a broken CONCATENATE(...,”!)
// formula. The fix checks the structural Node.LeafNode flag instead.
func TestBuildResultsWorkbook_KnockoutCellRefLikeNames(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatKnockout
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "P1", Dojo: "D"}, {Name: "M3", Dojo: "D"}, {Name: "A4", Dojo: "D"}, {Name: "Z9", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, br)
	for ri := range br.Rounds {
		for mi := range br.Rounds[ri] {
			m := &br.Rounds[ri][mi]
			if m.SideA != "" && m.SideB != "" {
				m.Winner = m.SideA
				m.Status = state.MatchStatusCompleted
				m.IpponsA = []string{"M", "K"}
				m.Decision = "fought"
			}
		}
	}
	require.NoError(t, store.SaveBracket(compID, br))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)

	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "P1"),
		"cell-ref-like entrant name 'P1' must render literally, not as a match-feeder reference")
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
}

// TestBuildResultsWorkbook_SetupKnockoutHasNoBracket: a knockout-only
// competition still in setup has no draw, so its results workbook prints no
// bracket and no competitor's name in one (the pre-draw roster fallback that
// seeded the registered names into a bracket is gone), and it is a workbook,
// never an error the results door would answer 500.
func TestBuildResultsWorkbook_SetupKnockoutHasNoBracket(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatKnockout
	comp.Status = state.CompStatusSetup
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "D1"}, {Name: "Bob", Dojo: "D2"}, {Name: "Carol", Dojo: "D3"}, {Name: "Dave", Dojo: "D4"},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	for _, sheet := range f.GetSheetList() {
		assert.Falsef(t, strings.HasPrefix(sheet, "Tree "), "no draw, so no tree page, got %q", sheet)
		rows, err := f.GetRows(sheet)
		require.NoError(t, err)
		for _, name := range []string{"Alice", "Bob", "Carol", "Dave"} {
			if sheet == helper.SheetData {
				continue
			}
			assert.Falsef(t, sheetContainsCell(rows, name), "sheet %q names %s before the draw", sheet, name)
		}
	}
	elim, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	for _, row := range elim {
		for _, cell := range row {
			assert.NotContainsf(t, cell, "Round ", "no elimination match block before the draw")
		}
	}
}

// TestBuildResultsWorkbook_KnockoutPartialBracket scores only the first bracket
// round (e.g. semifinals) and leaves later rounds unplayed. The export must render
// the played scores and leave the rest blank, without error.
func TestBuildResultsWorkbook_KnockoutPartialBracket(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	comp, err := store.LoadCompetition(compID)
	require.NoError(t, err)
	comp.Kind = "individual"
	comp.Format = state.CompFormatKnockout
	comp.Status = "setup"
	require.NoError(t, store.SaveCompetition(comp))
	require.NoError(t, store.SaveParticipants(compID, []domain.Player{
		{Name: "Alice", Dojo: "D"}, {Name: "Bob", Dojo: "D"}, {Name: "Carol", Dojo: "D"}, {Name: "Dave", Dojo: "D"},
	}))
	require.NoError(t, eng.StartCompetition(compID))

	br, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotNil(t, br)
	require.GreaterOrEqual(t, len(br.Rounds), 2, "4 entrants → semifinal + final")

	// Score ONLY the first round (semifinals); leave the final unplayed.
	for mi := range br.Rounds[0] {
		m := &br.Rounds[0][mi]
		if m.SideA != "" && m.SideB != "" {
			m.Winner = m.SideA
			m.Status = state.MatchStatusCompleted
			m.IpponsA = []string{"M", "K"}
			m.Decision = "fought"
		}
	}
	require.NoError(t, store.SaveBracket(compID, br))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	assert.True(t, sheetContainsCell(rows, "MK"),
		"the played semifinal score must render even with the final unplayed")
	assertNoBrokenFormulas(t, f, helper.SheetEliminationMatches)
}

// A bracket row's marks are attributed by the row's own side ids, not by the
// aka-first name convention. The subject is a pairing both of whose sides show
// the SAME display name, which is legal (two competitors from different dojos),
// and which no name comparison can separate: "winner == SideA" and
// "winner == SideB" are both true, so the name tier answers side A every time.
//
// The control is the same encounter with distinct names and the winner on the
// same side. Asserting the two land in the SAME physical cells is what makes
// this a property rather than a transcription of today's output: the sheet's
// left/right mapping is whatever it is, and the same-name row must follow it
// exactly as its distinguishable twin does.
func TestBuildResultsWorkbook_BracketMarksFollowTheSideIDs(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setCompFormat(t, store, compID, state.CompFormatMixed)
	require.NoError(t, store.SavePools(compID, makePools()))
	require.NoError(t, store.SavePoolMatches(compID, nil))

	// Side B wins by the opponent's withdrawal in both matches.
	require.NoError(t, store.SaveBracket(compID, &state.Bracket{
		Rounds: [][]state.BracketMatch{{
			{
				ID: "B1", MatchNumber: 1, Status: state.MatchStatusCompleted,
				SideA: "Alice", SideAID: "id-alice",
				SideB: "Charlie", SideBID: "id-charlie",
				Winner: "Charlie", WinnerID: "id-charlie",
				Decision: string(domain.DecisionKikenVoluntary),
			},
			{
				ID: "B2", MatchNumber: 2, Status: state.MatchStatusCompleted,
				SideA: "Yamada", SideAID: "id-yamada-tokyo",
				SideB: "Yamada", SideBID: "id-yamada-osaka",
				Winner: "Yamada", WinnerID: "id-yamada-osaka",
				Decision: string(domain.DecisionKikenVoluntary),
			},
		}},
	}))

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()
	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)

	ctlLeft, ctlRight := bracketVictoryCells(t, rows, "Round 1 - Match 1")
	subLeft, subRight := bracketVictoryCells(t, rows, "Round 1 - Match 2")

	require.NotEqual(t, ctlLeft, ctlRight, "the control must distinguish the two sides at all")
	assert.Equal(t, ctlLeft, subLeft,
		"the same-name pairing's LOSING cell must sit where its distinguishable twin's does")
	assert.Equal(t, ctlRight, subRight,
		"the same-name pairing's WINNING cell must sit where its distinguishable twin's does")
}

// TestBuildResultsWorkbook_WhiteOnTheLeftRedOnTheRight pins the FIK scoreboard
// layout on the tournament app's export: White (Shiro) in the LEFT column and
// Red (Aka) in the RIGHT, on the pool sheet and the elimination sheet alike,
// with no setting that can turn it around. A pool match's SideA and a knockout
// match's upper-bracket side are Aka, so every LEFT cell holds SideB.
//
// The competition is testSetup's, untouched, so it carries no orientation
// setting at all. A legacy config.md that still holds a `mirror: false` key
// from before that per-competition switch was removed loads the same way:
// unknown YAML keys are ignored, as internal/state/testdata/legacy_playoffs_config.md
// (a fixture kept for exactly this) demonstrates.
func TestBuildResultsWorkbook_WhiteOnTheLeftRedOnTheRight(t *testing.T) {
	t.Parallel()

	t.Run("pool", func(t *testing.T) {
		t.Parallel()
		dir, store, eng, compID := testSetup(t)
		defer os.RemoveAll(dir)

		require.NoError(t, store.SavePools(compID, makePools()))
		// Alice (SideA, Aka) beats Bob (SideB, Shiro) MK to D. Both sides
		// score, so a blank cell cannot pass for either one.
		require.NoError(t, store.SavePoolMatches(compID, []state.MatchResult{{
			ID: "Pool A-0", SideA: "Alice", SideAID: "Alice", SideB: "Bob", SideBID: "Bob",
			IpponsA: []string{"M", "K"}, IpponsB: []string{"D"},
			Decision: "fought", Status: state.MatchStatusCompleted,
			Winner: "Alice", WinnerID: "Alice",
		}}))

		data, err := BuildResultsWorkbook(store, eng, compID)
		require.NoError(t, err)
		f, err := excelize.OpenReader(bytes.NewReader(data))
		require.NoError(t, err)
		defer f.Close()
		rows, err := f.GetRows(helper.SheetPoolMatches)
		require.NoError(t, err)

		// One court band, Pool A first: its header is the first row whose
		// column A carries a side label, and match 0 is the row below it.
		hdr := slices.IndexFunc(rows, func(row []string) bool {
			return len(row) > 0 && (row[0] == "White" || row[0] == "Red")
		})
		require.GreaterOrEqual(t, hdr, 0, "the pool sheet must carry a side-label header")
		assert.Equal(t, "White", cellAt(rows, hdr, 0), "the header's LEFT label")
		assert.Equal(t, "Red", cellAt(rows, hdr, 6), "the header's RIGHT label")
		assert.Equal(t, "D", cellAt(rows, hdr+1, 1), "the LEFT score cell is Shiro's (SideB)")
		assert.Equal(t, "MK", cellAt(rows, hdr+1, 5), "the RIGHT score cell is Aka's (SideA)")

		matchRow := hdr + 2 // 1-based Excel row of match 0
		assert.Equal(t, "Bob", resolvedCellText(t, f, helper.SheetPoolMatches, fmt.Sprintf("A%d", matchRow)),
			"the LEFT entrant is SideB (Shiro)")
		assert.Equal(t, "Alice", resolvedCellText(t, f, helper.SheetPoolMatches, fmt.Sprintf("G%d", matchRow)),
			"the RIGHT entrant is SideA (Aka)")
	})

	t.Run("knockout", func(t *testing.T) {
		t.Parallel()
		dir, store, eng, compID := testSetup(t)
		defer os.RemoveAll(dir)

		comp, err := store.LoadCompetition(compID)
		require.NoError(t, err)
		comp.Kind = "individual"
		comp.Format = state.CompFormatKnockout
		comp.Status = "setup"
		require.NoError(t, store.SaveCompetition(comp))
		require.NoError(t, store.SaveParticipants(compID, []domain.Player{
			{Name: "Alice", Dojo: "D1"}, {Name: "Bob", Dojo: "D2"},
			{Name: "Carol", Dojo: "D3"}, {Name: "Dave", Dojo: "D4"},
		}))
		require.NoError(t, eng.StartCompetition(compID))

		// Every bout whose two sides are resolved competitors (an id is
		// present exactly then; a "Winner of ..." feeder carries none): the
		// upper-bracket side (SideA, Aka) wins MK to D.
		br, err := store.LoadBracket(compID)
		require.NoError(t, err)
		require.NotNil(t, br)
		byNumber := map[int]state.BracketMatch{}
		for ri := range br.Rounds {
			for mi := range br.Rounds[ri] {
				m := &br.Rounds[ri][mi]
				if m.MatchNumber <= 0 || m.SideAID == "" || m.SideBID == "" {
					continue
				}
				m.Winner, m.WinnerID = m.SideA, m.SideAID
				m.IpponsA, m.IpponsB = []string{"M", "K"}, []string{"D"}
				m.Decision = "fought"
				m.Status = state.MatchStatusCompleted
				byNumber[m.MatchNumber] = *m
			}
		}
		require.NotEmpty(t, byNumber, "the fixture must score at least one bout")
		require.NoError(t, store.SaveBracket(compID, br))

		data, err := BuildResultsWorkbook(store, eng, compID)
		require.NoError(t, err)
		f, err := excelize.OpenReader(bytes.NewReader(data))
		require.NoError(t, err)
		defer f.Close()
		rows, err := f.GetRows(helper.SheetEliminationMatches)
		require.NoError(t, err)
		// Blocks are found by their printed number: round numbers count down
		// toward the final, so a fixed "Round 1" label would name the wrong bout.
		checked := 0
		for rowIdx, row := range rows {
			for headerCol, cell := range row {
				num := parseRoundMatchLabel(cell)
				bm, ok := byNumber[num]
				if num <= 0 || !ok {
					continue
				}
				assert.Equalf(t, "White", cellAt(rows, rowIdx+1, headerCol), "Match %d header's LEFT label", num)
				assert.Equalf(t, "Red", cellAt(rows, rowIdx+1, headerCol+6), "Match %d header's RIGHT label", num)
				assert.Equalf(t, bm.SideB, cellAt(rows, rowIdx+2, headerCol), "Match %d LEFT entrant is the lower-bracket side (SideB)", num)
				assert.Equalf(t, bm.SideA, cellAt(rows, rowIdx+2, headerCol+6), "Match %d RIGHT entrant is the upper-bracket side (SideA)", num)
				assert.Equalf(t, "D", cellAt(rows, rowIdx+2, headerCol+1), "Match %d LEFT score cell is Shiro's (SideB)", num)
				assert.Equalf(t, "MK", cellAt(rows, rowIdx+2, headerCol+5), "Match %d RIGHT score cell is Aka's (SideA)", num)
				checked++
			}
		}
		assert.Equal(t, len(byNumber), checked, "every scored bout must have exactly one block")
	})
}

// TestBuildResultsWorkbook_BronzeEntrantsWhiteOnTheLeft pins
// writeThirdPlaceEntrants' side placement once the bronze match's entrants
// are RESOLVED (SideA/SideB stamped onto ThirdPlaceMatch from the semifinal
// losers, the same "resolved entrants" shape assertBronzeEntrantsPopulated
// and TestBuildResultsWorkbook_NaginataThirdPlaceNamesBeforeBronze already
// exercise): Shiro (SideB) must land in the LEFT entrant cell and Aka
// (SideA) in the RIGHT one, matching every other entrant pair on this sheet
// (helper.WhiteLeft). Those existing bronze tests only ever check that BOTH
// cells are non-empty or exercise the unplayed CONCATENATE-formula path;
// neither pins WHICH side lands where, so a swapped pair there would pass
// them both.
func TestBuildResultsWorkbook_BronzeEntrantsWhiteOnTheLeft(t *testing.T) {
	t.Parallel()
	dir, store, eng, compID := testSetup(t)
	defer os.RemoveAll(dir)

	setNaginataKnockout(t, store, compID, false)
	bracket := startNaginataWith4Players(t, store, eng, compID, false)

	sfIdx := len(bracket.Rounds) - 2
	sf := bracket.Rounds[sfIdx]
	require.Len(t, sf, 2, "expected 2 semifinals for 4 players")

	// Score both SFs so the engine resolves ThirdPlaceMatch.SideA/SideB to
	// the two losers' names.
	require.NoError(t, eng.RecordMatchResult(compID, sf[0].ID, &state.MatchResult{
		Winner:  sf[0].SideA,
		IpponsA: []string{"M"},
		Status:  state.MatchStatusCompleted,
	}))
	require.NoError(t, eng.RecordMatchResult(compID, sf[1].ID, &state.MatchResult{
		Winner:  sf[1].SideB,
		IpponsB: []string{"K"},
		Status:  state.MatchStatusCompleted,
	}))

	b2, err := store.LoadBracket(compID)
	require.NoError(t, err)
	require.NotEmpty(t, b2.ThirdPlaceMatch.SideA, "engine must populate ThirdPlaceMatch.SideA after SFs")
	require.NotEmpty(t, b2.ThirdPlaceMatch.SideB, "engine must populate ThirdPlaceMatch.SideB after SFs")
	aka, shiro := b2.ThirdPlaceMatch.SideA, b2.ThirdPlaceMatch.SideB

	data, err := BuildResultsWorkbook(store, eng, compID)
	require.NoError(t, err)
	f, err := excelize.OpenReader(bytes.NewReader(data))
	require.NoError(t, err)
	defer f.Close()

	rows, err := f.GetRows(helper.SheetEliminationMatches)
	require.NoError(t, err)
	// FindCellRow is 0-based; the entrant row sits 2 rows below the header
	// (header, side-label row, entrant row), so the 1-based Excel row is
	// headerIdx + 1 (to 1-based) + 2 (down to the entrant row).
	entrantExcelRow := bctest.FindCellRow(rows, helper.ThirdPlaceLabel) + 3

	assert.Equal(t, shiro, resolvedCellText(t, f, helper.SheetEliminationMatches, fmt.Sprintf("A%d", entrantExcelRow)),
		"the bronze LEFT entrant is SideB (Shiro)")
	assert.Equal(t, aka, resolvedCellText(t, f, helper.SheetEliminationMatches, fmt.Sprintf("G%d", entrantExcelRow)),
		"the bronze RIGHT entrant is SideA (Aka)")
}
