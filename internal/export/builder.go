package export

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ErrSwissExportUnsupported is returned by BuildResultsWorkbook for Swiss-format
// competitions, which have no static bracket to render. Callers should surface a
// clear message and point operators at the live Swiss standings instead.
var ErrSwissExportUnsupported = errors.New("results export is not supported for Swiss competitions; use the live standings view")

// BuildResultsWorkbook reads live tournament state and produces a results-
// populated XLSX workbook for the given competition. Both pool results (scores
// + standings) and elimination bracket results are included as literal values,
// so the workbook is suitable for archiving after a live event.
//
// This is a SEPARATE path from Engine.ExportCompetitionXlsx (the blank-template
// export). That function and the existing GET /api/competitions/:id/export
// endpoint are not modified.
//
// Suggested download filename: "bracket-results-<compID>.xlsx".
func BuildResultsWorkbook(store *state.Store, eng *engine.Engine, compID string) ([]byte, error) {
	comp, err := store.LoadCompetition(compID)
	if err != nil {
		return nil, fmt.Errorf("export: load competition %s: %w", compID, err)
	}
	if comp == nil {
		return nil, fmt.Errorf("export: competition %s not found", compID)
	}

	// Swiss has no pools and no static bracket (results are per-round pairings and
	// a running standings table), so there is nothing to render into the pool/tree
	// layout this builder produces. Block it explicitly, matching the blank-template
	// export, rather than emitting an empty workbook. A dedicated Swiss sheet is
	// tracked as follow-up work.
	if comp.Format == state.CompFormatSwiss {
		return nil, ErrSwissExportUnsupported
	}

	pools, err := store.LoadPools(compID)
	if err != nil {
		return nil, fmt.Errorf("export: load pools: %w", err)
	}

	matchResults, err := store.LoadPoolMatches(compID)
	if err != nil {
		return nil, fmt.Errorf("export: load pool matches: %w", err)
	}

	// LoadPools restores only pool membership (pools.csv), not matches
	// (pool-matches.csv). PrintPoolMatches renders the per-match grid from
	// pool.Matches, so reconstruct it from the stored results before rendering,
	// otherwise the grid (and the scores overlaid onto it) is empty.
	attachPoolMatches(pools, matchResults)

	standings, err := eng.CalculatePoolStandings(compID)
	if err != nil {
		return nil, fmt.Errorf("export: calculate standings: %w", err)
	}

	bracket, err := store.LoadBracket(compID)
	if err != nil {
		return nil, fmt.Errorf("export: load bracket: %w", err)
	}

	// Index match results by ID for O(1) lookup.
	matchResultByID := make(map[string]state.MatchResult, len(matchResults))
	for _, mr := range matchResults {
		matchResultByID[mr.ID] = mr
	}

	f, err := excel.NewFileFromScratch()
	if err != nil {
		return nil, fmt.Errorf("export: create workbook: %w", err)
	}
	defer func() {
		_ = f.Close()
	}()

	// 1. Data sheet + coordinate maps. Helper formula references in other
	//    sheets that point here (player names, etc.) still resolve correctly.
	poolCoords, playerCoords := helper.AddPoolDataToSheet(f, pools, comp.WithZekkenName, comp.Name)

	// 2. Pool Draw sheet (formula refs to data sheet survive store round-trips).
	if err := helper.AddPoolsToSheet(f, pools, poolCoords, playerCoords); err != nil {
		return nil, fmt.Errorf("export: add pools to sheet: %w", err)
	}

	// 3. Pool Matches sheet: lay skeleton, then overlay literal scores and standings.
	//    W/L/T/RANK formula cells collapse to 0 after a store round-trip
	//    (documented at cmd/create_handler.go:25), so we overwrite them with
	//    literal values from the engine.
	numCourts := len(comp.Courts)
	if numCourts == 0 {
		numCourts = 1
	}
	matchWinners := helper.PrintPoolMatches(
		f, pools, comp.TeamSize, comp.EffectivePoolWinners(),
		numCourts, comp.Mirror, poolCoords, playerCoords,
	)
	if err := overlayPoolScores(f, pools, matchResultByID, comp.TeamSize, comp.Mirror, numCourts); err != nil {
		return nil, fmt.Errorf("export: overlay pool scores: %w", err)
	}
	if err := overlayPoolStandings(f, pools, standings, comp.TeamSize, numCourts); err != nil {
		return nil, fmt.Errorf("export: overlay standings: %w", err)
	}

	// 4. Elimination Matches + Tree sheets.
	//    Only when there are actually pool winners advancing to a bracket.
	// Elimination skeleton leaves: pool winners for pools-based formats, or seeded
	// participants for a pure playoffs bracket (no pools). The latter mirrors
	// engine.generatePlayoffs so the rendered tree matches the stored bracket that
	// overlayBracketScores fills in.
	finals := helper.GenerateFinals(pools, comp.EffectivePoolWinners())
	if len(finals) == 0 && len(pools) == 0 && comp.Format == state.CompFormatPlayoffs {
		finals = playoffFinalsFromParticipants(store, comp)
	}
	if len(finals) > 0 {
		tree := helper.CreateBalancedTree(finals)
		depth := helper.CalculateDepth(tree)

		// Build per-round match slices (same logic as cmd/create-pools.go).
		eliminationMatchRounds := make([][]*helper.Node, depth-1)
		for i := depth; i > 1; i-- {
			rounds := helper.TraverseRounds(tree, 1, i-1)
			eliminationMatchRounds[depth-i] = rounds
		}

		// Populate the Elimination Matches sheet skeleton so overlayBracketScores
		// has "Round N - Match N" headers to scan.
		helper.FillInMatches(f, eliminationMatchRounds)
		helper.PrintTeamEliminationMatches(f, matchWinners, eliminationMatchRounds, comp.TeamSize, numCourts, comp.Mirror)

		// Overlay literal scores from the live bracket state.
		if bracket != nil {
			bracketByID := buildBracketMatchIndex(bracket)
			if err := overlayBracketScores(f, bracketByID, comp.TeamSize, comp.Mirror); err != nil {
				return nil, fmt.Errorf("export: overlay bracket scores: %w", err)
			}
			// Playoffs have no pool data sheet, so the pool-oriented renderer emits
			// broken ''! references for the entrant name cells. Overwrite them with
			// the stored bracket's literal names (empty for unresolved slots) so the
			// sheet is a valid literal snapshot with no broken formulas.
			if len(pools) == 0 && comp.Format == state.CompFormatPlayoffs {
				if err := overlayPlayoffBracketNames(f, bracketByID, comp.TeamSize, comp.Mirror); err != nil {
					return nil, fmt.Errorf("export: overlay playoff names: %w", err)
				}
			}
		}

		// Tree sheet.
		numPages, _ := helper.TreePageLayout(len(finals), numCourts, false)
		subtrees := helper.SubdivideTree(tree, numPages)
		for i, subtree := range subtrees {
			sheetName := helper.SheetTree
			if i > 0 {
				sheetName = fmt.Sprintf("Tree %d", i+1)
				if _, err := f.NewSheet(sheetName); err != nil {
					return nil, fmt.Errorf("export: create tree sheet: %w", err)
				}
			}
			if i == 0 {
				d := helper.CalculateDepth(subtree)
				helper.PrintLeafNodes(subtree, f, sheetName, 2*d, 1, d, true, matchWinners)
				helper.SetTreeSheetTitle(f, sheetName, comp.Name)
			}
		}
	}

	// 5. Names to Print sheet (identical to blank-template export).
	helper.CreateNamesWithPoolToPrint(f, pools, comp.WithZekkenName, numCourts, playerCoords)

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("export: write workbook: %w", err)
	}
	return buf.Bytes(), nil
}

// attachPoolMatches reconstructs each pool's Matches slice from the stored pool
// results. LoadPools restores only pool membership; the matches live in
// pool-matches.csv (loaded separately). PrintPoolMatches renders its per-match
// grid from pool.Matches, and the ordinal overlay in overlayPoolScores /
// overlayTeamPoolScores maps the N-th grid match to result ID "<Pool>-<N>", so
// matches are ordered by the numeric suffix of that ID to keep both in lockstep.
// Tiebreak/daihyosen results (non-numeric suffix, e.g. "Pool A-DH-0") are skipped.
func attachPoolMatches(pools []helper.Pool, matchResults []state.MatchResult) {
	for pi := range pools {
		p := &pools[pi]
		prefix := p.PoolName + "-"

		type idxRes struct {
			idx int
			mr  state.MatchResult
		}
		var mine []idxRes
		for _, mr := range matchResults {
			if !strings.HasPrefix(mr.ID, prefix) {
				continue
			}
			n, err := strconv.Atoi(mr.ID[len(prefix):])
			if err != nil {
				continue // tiebreak/daihyosen or malformed suffix
			}
			mine = append(mine, idxRes{n, mr})
		}
		sort.Slice(mine, func(i, j int) bool { return mine[i].idx < mine[j].idx })

		ptr := make(map[string]*helper.Player, len(p.Players))
		for i := range p.Players {
			ptr[p.Players[i].Name] = &p.Players[i]
		}

		p.Matches = make([]helper.Match, 0, len(mine))
		for _, ir := range mine {
			p.Matches = append(p.Matches, helper.Match{
				SideA: ptr[ir.mr.SideA],
				SideB: ptr[ir.mr.SideB],
			})
		}
	}
}

// playoffFinalsFromParticipants seeds the competition's participants exactly as
// engine.generatePlayoffs does (ApplySeeds → optional numbering → StandardSeeding),
// returning the seeded names to feed the elimination-tree skeleton. Used for pure
// playoffs competitions, which have no pools to derive finalists from. Returns nil
// when participants can't be loaded, in which case no elimination sheet is rendered.
func playoffFinalsFromParticipants(store *state.Store, comp *state.Competition) []string {
	players, err := store.LoadParticipants(comp.ID, comp.WithZekkenName)
	if err != nil || len(players) == 0 {
		return nil
	}
	if seeds, serr := store.LoadSeeds(comp.ID); serr == nil && len(seeds) > 0 {
		if aerr := helper.ApplySeeds(players, seeds); aerr != nil {
			// An unmatched seed name is non-fatal for a read-only export; the
			// bracket still renders, just unseeded. Mirror the file's warn pattern.
			fmt.Printf("export: warning: apply seeds for playoffs skeleton: %v\n", aerr)
		}
	}
	if comp.NumberPrefix != "" {
		helper.AssignPlayerNumbers(players, comp.NumberPrefix, 1)
	}
	seeded := helper.StandardSeeding(players)
	names := make([]string, len(seeded))
	for i, p := range seeded {
		names[i] = p.Name
	}
	return names
}

// ---------- pool score overlay ----------

// overlayPoolScores writes literal score values into the Pool Matches sheet.
// The skeleton written by PrintPoolMatches uses formula references for player
// names - excelize's GetRows does NOT evaluate these formulas, so we cannot
// match by player names. Instead we use ordinal position.
//
// Individual pools render as a COMPACT block: one "Red ... vs ... White" header
// per pool, immediately followed by one row per round-robin match (in pool.Matches
// order). So the N-th header in a court column is the N-th pool assigned to that
// court, and match i sits at header row + 1 + i. By default SideA (Red) is the
// left column and SideB (White) the right; mirror swaps the two score columns.
func overlayPoolScores(f *excelize.File, pools []helper.Pool, resultByID map[string]state.MatchResult, teamSize int, mirror bool, numCourts int) error {
	if len(pools) == 0 {
		return nil
	}
	if teamSize != 0 {
		return overlayTeamPoolScores(f, pools, resultByID, mirror, numCourts)
	}

	sheetName := helper.SheetPoolMatches

	courtAssignments, _ := helper.AssignPoolsToCourts(len(pools), numCourts)
	poolsByCourt := make([][]int, numCourts)
	for i, c := range courtAssignments {
		poolsByCourt[c] = append(poolsByCourt[c], i)
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayPoolScores: get rows: %w", err)
	}

	courtHdrIdx := make([]int, numCourts)

	for rowIdx, row := range rows {
		for c := 0; c < numCourts; c++ {
			startColIdx := c * helper.CourtsColumnsPerCourt // 0-based
			if startColIdx >= len(row) {
				continue
			}
			if row[startColIdx] != "Red" && row[startColIdx] != "White" {
				continue
			}

			// N-th header in court c == N-th pool assigned to that court.
			poolOrder := courtHdrIdx[c]
			courtHdrIdx[c]++
			if poolOrder >= len(poolsByCourt[c]) {
				continue
			}
			pool := pools[poolsByCourt[c][poolOrder]]

			// Column layout (1-based): startCol+1 = left victories (White/SideB),
			// startCol+3 = middle/vs, startCol+5 = right victories (Red/SideA).
			courtStartCol := 1 + c*helper.CourtsColumnsPerCourt
			lVCol := colNum(courtStartCol + 1)
			middleCol := colNum(courtStartCol + 3)
			rVCol := colNum(courtStartCol + 5)

			for i := range pool.Matches {
				matchID := fmt.Sprintf("%s-%d", pool.PoolName, i)
				mr, found := resultByID[matchID]
				if !found || mr.Status != state.MatchStatusCompleted {
					continue
				}
				// Header is at excel row rowIdx+1; match i sits at header + 1 + i.
				excelRow := rowIdx + 2 + i

				leftIppons := mr.IpponsA
				rightIppons := mr.IpponsB
				if mirror {
					leftIppons, rightIppons = mr.IpponsB, mr.IpponsA
				}

				hantei := mr.DecidedByHantei != nil && *mr.DecidedByHantei
				sfx := DecisionSuffix(mr.Decision, mr.Encho, hantei)

				setCellStr(f, sheetName, lVCol, excelRow, IpponsScore(leftIppons))
				setCellStr(f, sheetName, rVCol, excelRow, IpponsScore(rightIppons))
				if mr.Decision == state.DecisionDraw {
					setCellStr(f, sheetName, middleCol, excelRow, "X")
				}
				if sfx != "" {
					setCellStr(f, sheetName, middleCol, excelRow, sfx)
				}
			}
		}
	}

	return nil
}

// overlayTeamPoolScores writes literal sub-match ippon letters + the team IV/PW
// summary onto the team pool-match layout produced by PrintPoolMatches when
// teamMatches > 0. The layout per encounter (see printSinglePool team branch):
//
//	Red header row      (scanned: start col == "Red")
//	team names / summary row  = Red row + 1  (holds IV/PW summary: lV/lP left, rV/rP right)
//	sub-match rows      = Red row + 2 .. Red row + 1 + teamSize (ordinals 1..teamSize)
//
// It uses the same ordinal-position matching as the individual path: the N-th
// "Red" header in a court's column band corresponds to the N-th match across
// that court's pools, in pool order.
func overlayTeamPoolScores(f *excelize.File, pools []helper.Pool, resultByID map[string]state.MatchResult, mirror bool, numCourts int) error {
	sheetName := helper.SheetPoolMatches

	courtMatches := buildCourtMatchJobs(pools, numCourts)

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayTeamPoolScores: get rows: %w", err)
	}

	courtMatchIdx := make([]int, numCourts)

	for rowIdx, row := range rows {
		for c := 0; c < numCourts; c++ {
			startColIdx := c * helper.CourtsColumnsPerCourt // 0-based
			if startColIdx >= len(row) {
				continue
			}
			if row[startColIdx] != "Red" && row[startColIdx] != "White" {
				continue
			}

			mJobIdx := courtMatchIdx[c]
			if mJobIdx >= len(courtMatches[c]) {
				continue
			}
			job := courtMatches[c][mJobIdx]
			courtMatchIdx[c]++

			pool := pools[job.poolIdx]
			matchID := fmt.Sprintf("%s-%d", pool.PoolName, job.matchIdx)
			mr, found := resultByID[matchID]
			if !found || mr.Status != state.MatchStatusCompleted {
				continue
			}

			courtStartCol := 1 + c*helper.CourtsColumnsPerCourt
			// summary row = Red header row + 1 (1-based excel row).
			summaryExcelRow := rowIdx + 2
			writeTeamSummaryCells(f, sheetName, courtStartCol, summaryExcelRow, mr, mirror)

			// Sub-match rows start two rows below the Red header (1-based).
			subStartExcelRow := rowIdx + 3
			writeTeamSubMatchScores(f, sheetName, courtStartCol, subStartExcelRow, mr.SubResults, mirror)
		}
	}

	return nil
}

// buildCourtMatchJobs returns, per court, the ordered list of (poolIdx, matchIdx)
// jobs in the row order PrintPoolMatches lays them out (pool 0 matches, then pool
// 1 matches, ...). Shared by the individual and team pool-score overlays.
func buildCourtMatchJobs(pools []helper.Pool, numCourts int) [][]matchJob {
	courtAssignments, _ := helper.AssignPoolsToCourts(len(pools), numCourts)
	poolsByCourt := make([][]int, numCourts)
	for i, c := range courtAssignments {
		poolsByCourt[c] = append(poolsByCourt[c], i)
	}
	courtMatches := make([][]matchJob, numCourts)
	for c := 0; c < numCourts; c++ {
		for _, pi := range poolsByCourt[c] {
			for mi := range pools[pi].Matches {
				courtMatches[c] = append(courtMatches[c], matchJob{pi, mi})
			}
		}
	}
	return courtMatches
}

// writeTeamSummaryCells writes the literal team IV/PW summary onto a team match's
// summary row. Layout mirrors printSingleEliminationMatch's IV/PW labels:
//
//	lVCol (startCol+1) = left IV,  lPCol (startCol+2) = left PW
//	rVCol (startCol+5) = right IV, rPCol (startCol+4) = right PW
//
// SideA is Red (left by default), SideB is Shiro (right); mirror swaps sides.
// The middle "vs" cell carries the encounter's decision suffix (DH etc.) or the
// hikiwake "X" marker when the team encounter is a draw.
func writeTeamSummaryCells(f *excelize.File, sheetName string, courtStartCol, excelRow int, mr state.MatchResult, mirror bool) {
	lVCol := colNum(courtStartCol + 1)
	lPCol := colNum(courtStartCol + 2)
	middleCol := colNum(courtStartCol + 3)
	rPCol := colNum(courtStartCol + 4)
	rVCol := colNum(courtStartCol + 5)

	line := state.TeamResultFrom(mr.SubResults, mr.SideA, mr.SideB)
	if line != nil {
		// SideA = Aka, SideB = Shiro. Left is Aka unless mirror.
		leftIV, leftPW := line.AkaIV, line.AkaPW
		rightIV, rightPW := line.ShiroIV, line.ShiroPW
		if mirror {
			leftIV, leftPW, rightIV, rightPW = rightIV, rightPW, leftIV, leftPW
		}
		setIntCellDirect(f, sheetName, lVCol, excelRow, leftIV)
		setIntCellDirect(f, sheetName, lPCol, excelRow, leftPW)
		setIntCellDirect(f, sheetName, rVCol, excelRow, rightIV)
		setIntCellDirect(f, sheetName, rPCol, excelRow, rightPW)
	}

	hantei := mr.DecidedByHantei != nil && *mr.DecidedByHantei
	sfx := DecisionSuffix(mr.Decision, mr.Encho, hantei)
	if mr.Decision == state.DecisionDraw {
		setCellStr(f, sheetName, middleCol, excelRow, "X")
	}
	if sfx != "" {
		setCellStr(f, sheetName, middleCol, excelRow, sfx)
	}
}

// writeTeamSubMatchScores writes each sub-bout's ippon letters onto the team
// sub-match rows. Left ippons -> lVCol (startCol+1), right -> rVCol (startCol+5),
// middle "vs" -> tie marker / suffix. subResults are keyed by Position (1-based);
// the daihyosen placeholder (Position < 0) is skipped so its blank row stays clean.
func writeTeamSubMatchScores(f *excelize.File, sheetName string, courtStartCol, subStartExcelRow int, subResults []state.SubMatchResult, mirror bool) {
	lVCol := colNum(courtStartCol + 1)
	middleCol := colNum(courtStartCol + 3)
	rVCol := colNum(courtStartCol + 5)

	for _, sub := range subResults {
		if sub.Position <= 0 {
			continue // skip daihyosen placeholder / unpositioned rows
		}
		// Sub-match row for Position P is the P-th sub row (1-based Position).
		excelRow := subStartExcelRow + (sub.Position - 1)

		leftIppons, rightIppons := sub.IpponsA, sub.IpponsB
		if mirror {
			leftIppons, rightIppons = sub.IpponsB, sub.IpponsA
		}
		lScore := IpponsScore(leftIppons)
		rScore := IpponsScore(rightIppons)
		if lScore != "" {
			setCellStr(f, sheetName, lVCol, excelRow, lScore)
		}
		if rScore != "" {
			setCellStr(f, sheetName, rVCol, excelRow, rScore)
		}

		hantei := sub.DecidedByHantei
		sfx := DecisionSuffix(sub.Decision, sub.Encho, hantei)
		if sub.Decision == state.DecisionDraw {
			setCellStr(f, sheetName, middleCol, excelRow, "X")
		}
		if sfx != "" {
			setCellStr(f, sheetName, middleCol, excelRow, sfx)
		}
	}
}

// ---------- standings overlay ----------

// overlayPoolStandings overwrites formula-driven standings cells (W/L/T/PW/PL/Rank
// and the ranking section) with literal values from the engine. Formulas in these
// cells reference relative pointers that a store round-trip severs (per
// cmd/create_handler.go:25), so we replace them with Go-computed literals.
//
// Strategy: the N-th "Results" header row in each court column corresponds to the
// N-th pool assigned to that court. We match by ordinal position, not by
// resolved formula values (which are not evaluated by excelize's GetRows).
func overlayPoolStandings(f *excelize.File, pools []helper.Pool, standings map[string][]state.PlayerStanding, teamSize int, numCourts int) error {
	if len(pools) == 0 {
		return nil
	}
	if teamSize != 0 {
		return overlayTeamPoolStandings(f, pools, standings, numCourts)
	}

	sheetName := helper.SheetPoolMatches

	courtAssignments, _ := helper.AssignPoolsToCourts(len(pools), numCourts)
	poolsByCourt := make([][]int, numCourts)
	for i, c := range courtAssignments {
		poolsByCourt[c] = append(poolsByCourt[c], i)
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayPoolStandings: get rows: %w", err)
	}

	// Track how many "Results" headers we've seen per court column.
	courtResultsIdx := make([]int, numCourts)

	for rowIdx, row := range rows {
		for c := 0; c < numCourts; c++ {
			startColIdx := c * helper.CourtsColumnsPerCourt // 0-based
			if startColIdx >= len(row) {
				continue
			}
			if row[startColIdx] != "Results" && row[startColIdx] != "Team Results" {
				continue
			}

			poolOrderInCourt := courtResultsIdx[c]
			courtResultsIdx[c]++

			if poolOrderInCourt >= len(poolsByCourt[c]) {
				continue
			}
			poolIdx := poolsByCourt[c][poolOrderInCourt]
			pool := pools[poolIdx]

			poolStandings, ok := standings[pool.PoolName]
			if !ok {
				continue
			}
			byName := standingMap(poolStandings)
			colMap := buildColumnMap(row)

			// Write standings literals for each player (in pool draw order).
			for i, player := range pool.Players {
				dataRowIdx := rowIdx + 1 + i
				if dataRowIdx >= len(rows) {
					break
				}
				ps, ok := byName[player.Name]
				if !ok {
					continue
				}
				excelRow := dataRowIdx + 1
				// teamSize == 0 is guaranteed here (we returned early above for team competitions).
				setIntCell(f, sheetName, excelRow, colMap, "W", ps.Wins)
				setIntCell(f, sheetName, excelRow, colMap, "L", ps.Losses)
				setIntCell(f, sheetName, excelRow, colMap, "T", ps.Draws)
				setIntCell(f, sheetName, excelRow, colMap, "PW", ps.IpponsGiven)
				setIntCell(f, sheetName, excelRow, colMap, "PL", ps.IpponsTaken)
				setIntCell(f, sheetName, excelRow, colMap, "Rank", ps.Rank)
			}
		}
	}

	// Overlay Ranking sections.
	return overlayRankingSections(f, sheetName, rows, pools, standings, numCourts)
}

// overlayRankingSections replaces the IFERROR/INDEX/MATCH formula cells in the
// "Ranking" sections with literal player names from the engine-ordered standings.
func overlayRankingSections(f *excelize.File, sheetName string, rows [][]string, pools []helper.Pool, standings map[string][]state.PlayerStanding, numCourts int) error {
	courtAssignments, _ := helper.AssignPoolsToCourts(len(pools), numCourts)
	poolsByCourt := make([][]int, numCourts)
	for i, c := range courtAssignments {
		poolsByCourt[c] = append(poolsByCourt[c], i)
	}

	courtRankIdx := make([]int, numCourts)

	for rowIdx, row := range rows {
		for c := 0; c < numCourts; c++ {
			// "Ranking" label appears in resNameColName = startCol+6 (column G for court 0).
			// startCol = 1 + c*CourtsColumnsPerCourt, so resNameColName 0-based idx = c*8+6.
			rankingColIdx := c*helper.CourtsColumnsPerCourt + 6 // 0-based
			if rankingColIdx >= len(row) {
				continue
			}
			if row[rankingColIdx] != "Ranking" {
				continue
			}

			poolOrderInCourt := courtRankIdx[c]
			courtRankIdx[c]++

			if poolOrderInCourt >= len(poolsByCourt[c]) {
				continue
			}
			poolIdx := poolsByCourt[c][poolOrderInCourt]
			pool := pools[poolIdx]
			poolStandings, ok := standings[pool.PoolName]
			if !ok {
				continue
			}

			sorted := make([]state.PlayerStanding, len(poolStandings))
			copy(sorted, poolStandings)
			sort.Slice(sorted, func(i, j int) bool {
				return sorted[i].Rank < sorted[j].Rank
			})

			// Player name cells are also in resNameColName = startCol+6 = 1-based col c*8+7.
			nameColIdx := c*helper.CourtsColumnsPerCourt + 7 // 1-based col number
			nameCol := colNum(nameColIdx)
			for rankOrd, ps := range sorted {
				dataRowIdx := rowIdx + 1 + rankOrd
				if dataRowIdx >= len(rows) {
					break
				}
				excelRow := dataRowIdx + 1
				cellRef := fmt.Sprintf("%s%d", nameCol, excelRow)
				if err := f.SetCellValue(sheetName, cellRef, ps.Player.Name); err != nil {
					return fmt.Errorf("overlayRankingSections: %w", err)
				}
			}
		}
	}
	return nil
}

// overlayTeamPoolStandings overlays literal team-standings values onto the two
// stacked tables printPoolResultsTable renders for teamMatches>0:
//
//	Table 1 "Team Results": W/L/T at startCol+1/+2/+3, Rank at startCol+6.
//	Table 2 (header = Table 1 header + len(players) + 2): IV/IL/IT/PW/PL at
//	startCol+1..+5.
//
// Matching is by ordinal position (N-th "Team Results" header in a court column
// == N-th pool assigned to that court), mirroring overlayPoolStandings, because
// excelize does not evaluate the name formulas. Player order is identical in
// both tables (both iterate pool.Players), so index i maps to the same team.
func overlayTeamPoolStandings(f *excelize.File, pools []helper.Pool, standings map[string][]state.PlayerStanding, numCourts int) error {
	sheetName := helper.SheetPoolMatches

	courtAssignments, _ := helper.AssignPoolsToCourts(len(pools), numCourts)
	poolsByCourt := make([][]int, numCourts)
	for i, c := range courtAssignments {
		poolsByCourt[c] = append(poolsByCourt[c], i)
	}

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayTeamPoolStandings: get rows: %w", err)
	}

	courtResultsIdx := make([]int, numCourts)

	for rowIdx, row := range rows {
		for c := 0; c < numCourts; c++ {
			startColIdx := c * helper.CourtsColumnsPerCourt // 0-based
			if startColIdx >= len(row) {
				continue
			}
			if row[startColIdx] != "Team Results" {
				continue
			}

			poolOrderInCourt := courtResultsIdx[c]
			courtResultsIdx[c]++
			if poolOrderInCourt >= len(poolsByCourt[c]) {
				continue
			}
			pool := pools[poolsByCourt[c][poolOrderInCourt]]
			poolStandings, ok := standings[pool.PoolName]
			if !ok {
				continue
			}
			byName := standingMap(poolStandings)

			courtStartCol := 1 + c*helper.CourtsColumnsPerCourt // 1-based
			wCol := colNum(courtStartCol + 1)
			lCol := colNum(courtStartCol + 2)
			tCol := colNum(courtStartCol + 3)
			rankCol := colNum(courtStartCol + 6)
			// Table 2 columns share startCol+1..+5 with different meanings.
			ivCol := colNum(courtStartCol + 1)
			ilCol := colNum(courtStartCol + 2)
			itCol := colNum(courtStartCol + 3)
			pwCol := colNum(courtStartCol + 4)
			plCol := colNum(courtStartCol + 5)

			nPlayers := len(pool.Players)
			for i, player := range pool.Players {
				ps, ok := byName[player.Name]
				if !ok {
					continue
				}
				// Table 1 header is excel row rowIdx+1; player i at rowIdx+2+i.
				t1Row := rowIdx + 2 + i
				setIntCellDirect(f, sheetName, wCol, t1Row, ps.Wins)
				setIntCellDirect(f, sheetName, lCol, t1Row, ps.Losses)
				setIntCellDirect(f, sheetName, tCol, t1Row, ps.Draws)
				setIntCellDirect(f, sheetName, rankCol, t1Row, ps.Rank)

				// Table 2 header = Table 1 header + nPlayers + 2 (excel rows);
				// player i at (rowIdx+1) + nPlayers + 2 + 1 + i.
				t2Row := rowIdx + nPlayers + 4 + i
				setIntCellDirect(f, sheetName, ivCol, t2Row, ps.IndividualWins)
				setIntCellDirect(f, sheetName, ilCol, t2Row, ps.IndividualLosses)
				setIntCellDirect(f, sheetName, itCol, t2Row, ps.IndividualDraws)
				setIntCellDirect(f, sheetName, pwCol, t2Row, ps.PointsWon)
				setIntCellDirect(f, sheetName, plCol, t2Row, ps.PointsLost)
			}
		}
	}

	return overlayRankingSections(f, sheetName, rows, pools, standings, numCourts)
}

// ---------- bracket score overlay ----------

// overlayBracketScores writes literal score values into the Elimination Matches
// sheet by scanning for "Round N - Match N" header cells. For each completed
// match found, the score cells in the row two rows below are overwritten with
// literal values (ScoreA/ScoreB from the bracket JSON survive the round-trip).
func overlayBracketScores(f *excelize.File, bracketByID map[string]state.BracketMatch, teamSize int, mirror bool) error {
	if teamSize != 0 {
		return overlayTeamBracketScores(f, bracketByID, teamSize, mirror)
	}
	sheetName := helper.SheetEliminationMatches

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayBracketScores: get rows: %w", err)
	}

	// Courts are laid out side-by-side (one 8-column band each), so a single row
	// can carry a "Round N - Match N" header at EACH court's start column. Process
	// every header in the row, not just the first.
	for rowIdx, row := range rows {
		for headerCol, cell := range row {
			_, matchNum := parseRoundMatchLabel(cell)
			if matchNum <= 0 {
				continue
			}

			bm := findBracketMatchByNumber(bracketByID, matchNum)
			if bm == nil || bm.Status != state.MatchStatusCompleted {
				continue
			}

			// Score row is 2 rows below the header:
			//   header+1 = Red/White label row
			//   header+2 = player/score row
			scoreRowIdx := rowIdx + 2
			if scoreRowIdx >= len(rows) {
				continue
			}

			excelRow := scoreRowIdx + 1 // 1-based

			// headerCol is 0-based. The court start col (1-based) = headerCol+1.
			courtStartCol := headerCol + 1
			lVCol := colNum(courtStartCol + 1)
			middleCol := colNum(courtStartCol + 3)
			rVCol := colNum(courtStartCol + 5)

			leftScore := bm.ScoreA
			rightScore := bm.ScoreB
			if mirror {
				leftScore, rightScore = rightScore, leftScore
			}

			sfx := DecisionSuffix(bm.Decision, bm.Encho, bm.DecidedByHantei)

			setCellStr(f, sheetName, lVCol, excelRow, leftScore)
			setCellStr(f, sheetName, rVCol, excelRow, rightScore)

			if bm.Decision == state.DecisionDraw {
				setCellStr(f, sheetName, middleCol, excelRow, "X")
			}
			if sfx != "" {
				setCellStr(f, sheetName, middleCol, excelRow, sfx)
			}

			if bm.Winner != "" {
				writeWinnerCell(f, sheetName, rows, scoreRowIdx, headerCol, bm.Winner)
			}
		}
	}
	return nil
}

// overlayTeamBracketScores writes literal team-encounter results onto the team
// elimination layout produced by PrintTeamEliminationMatches. Relative to a
// "Round N - Match N" header at (1-based) row H:
//
//	sub-match row for Position p (1..teamSize) = H + 2 + p   (ippon letters)
//	IV/PW summary ("Victories / Points") row   = H + 5 + teamSize
//	"1." winner-marker row                      = H + 8 + teamSize
//
// IV/PW cell columns on the summary row mirror the pool summary: left IV=startCol+1,
// left PW=startCol+2, right IV=startCol+5, right PW=startCol+4. The summary IV/PW
// cells and per-player W/L/T standings are formula-driven (they tally the sub-match
// rows) and collapse after a store round-trip, so we overwrite them with literals.
func overlayTeamBracketScores(f *excelize.File, bracketByID map[string]state.BracketMatch, teamSize int, mirror bool) error {
	sheetName := helper.SheetEliminationMatches

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayTeamBracketScores: get rows: %w", err)
	}

	// Courts are laid out side-by-side (one 8-column band each), so a single row
	// can carry a "Round N - Match N" header at EACH court's start column. Process
	// every header in the row, not just the first.
	for rowIdx, row := range rows {
		for headerCol, cell := range row {
			_, matchNum := parseRoundMatchLabel(cell)
			if matchNum <= 0 {
				continue
			}

			bm := findBracketMatchByNumber(bracketByID, matchNum)
			if bm == nil || bm.Status != state.MatchStatusCompleted {
				continue
			}

			courtStartCol := headerCol + 1 // 1-based
			lVCol := colNum(courtStartCol + 1)
			lPCol := colNum(courtStartCol + 2)
			middleCol := colNum(courtStartCol + 3)
			rPCol := colNum(courtStartCol + 4)
			rVCol := colNum(courtStartCol + 5)

			headerExcelRow := rowIdx + 1 // H (1-based)

			// Sub-match ippon letters: Position p sits at H+2+p.
			for _, sub := range bm.SubResults {
				if sub.Position <= 0 || sub.Position > teamSize {
					continue // daihyosen placeholder / out-of-range
				}
				excelRow := headerExcelRow + 2 + sub.Position
				leftIppons, rightIppons := sub.IpponsA, sub.IpponsB
				if mirror {
					leftIppons, rightIppons = sub.IpponsB, sub.IpponsA
				}
				if s := IpponsScore(leftIppons); s != "" {
					setCellStr(f, sheetName, lVCol, excelRow, s)
				}
				if s := IpponsScore(rightIppons); s != "" {
					setCellStr(f, sheetName, rVCol, excelRow, s)
				}
				subSfx := DecisionSuffix(sub.Decision, sub.Encho, sub.DecidedByHantei)
				if sub.Decision == state.DecisionDraw {
					setCellStr(f, sheetName, middleCol, excelRow, "X")
				}
				if subSfx != "" {
					setCellStr(f, sheetName, middleCol, excelRow, subSfx)
				}
			}

			// IV/PW summary row = H + 5 + teamSize.
			summaryExcelRow := headerExcelRow + 5 + teamSize
			if line := state.TeamResultFrom(bm.SubResults, bm.SideA, bm.SideB); line != nil {
				leftIV, leftPW := line.AkaIV, line.AkaPW
				rightIV, rightPW := line.ShiroIV, line.ShiroPW
				if mirror {
					leftIV, leftPW, rightIV, rightPW = rightIV, rightPW, leftIV, leftPW
				}
				setIntCellDirect(f, sheetName, lVCol, summaryExcelRow, leftIV)
				setIntCellDirect(f, sheetName, lPCol, summaryExcelRow, leftPW)
				setIntCellDirect(f, sheetName, rVCol, summaryExcelRow, rightIV)
				setIntCellDirect(f, sheetName, rPCol, summaryExcelRow, rightPW)
			}

			sfx := DecisionSuffix(bm.Decision, bm.Encho, bm.DecidedByHantei)
			if bm.Decision == state.DecisionDraw {
				setCellStr(f, sheetName, middleCol, summaryExcelRow, "X")
			}
			if sfx != "" {
				setCellStr(f, sheetName, middleCol, summaryExcelRow, sfx)
			}

			// Winner marker: the "1." row is 3 rows below the summary row; reuse the
			// individual writer, which scans forward for the "1." ordinal.
			if bm.Winner != "" {
				writeWinnerCell(f, sheetName, rows, summaryExcelRow-1, headerCol, bm.Winner)
			}
		}
	}
	return nil
}

// overlayPlayoffBracketNames overwrites the elimination entrant name cells with
// the stored bracket's literal SideA/SideB. Playoffs have no pool data sheet, so
// the pool-oriented renderer points those cells at an empty pool-winner cell,
// producing a broken ”! formula. Writing the literal names (or "" for an
// unresolved slot, which clears the broken formula) yields a valid snapshot.
//
// Name cells sit at the court's start column (left) and start+6 (right) on the
// entrant row (header + 2). Team brackets repeat the names on the IV/PW summary
// row (header + 5 + teamSize), so those are overwritten too.
func overlayPlayoffBracketNames(f *excelize.File, bracketByID map[string]state.BracketMatch, teamSize int, mirror bool) error {
	sheetName := helper.SheetEliminationMatches
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayPlayoffBracketNames: get rows: %w", err)
	}

	for rowIdx, row := range rows {
		for headerCol, cell := range row {
			_, matchNum := parseRoundMatchLabel(cell)
			if matchNum <= 0 {
				continue
			}
			bm := findBracketMatchByNumber(bracketByID, matchNum)
			if bm == nil {
				continue
			}

			leftName, rightName := bm.SideA, bm.SideB
			if mirror {
				leftName, rightName = rightName, leftName
			}
			leftCol := colNum(headerCol + 1)  // court start column
			rightCol := colNum(headerCol + 7) // start + 6 (endColName)

			entrantRow := rowIdx + 3 // header (rowIdx+1) + 2
			setCellStr(f, sheetName, leftCol, entrantRow, leftName)
			setCellStr(f, sheetName, rightCol, entrantRow, rightName)

			if teamSize > 0 {
				summaryRow := rowIdx + 6 + teamSize // header + 5 + teamSize
				setCellStr(f, sheetName, leftCol, summaryRow, leftName)
				setCellStr(f, sheetName, rightCol, summaryRow, rightName)
			}
		}
	}
	return nil
}

// ---------- helper utilities ----------

// colNum converts a 1-based column index to an Excel column letter (e.g. 1 -> "A").
func colNum(col int) string {
	name, err := excelize.ColumnNumberToName(col)
	if err != nil {
		return fmt.Sprintf("?%d", col)
	}
	return name
}

func setCellStr(f *excelize.File, sheet, col string, row int, value string) {
	if err := f.SetCellValue(sheet, fmt.Sprintf("%s%d", col, row), value); err != nil {
		fmt.Printf("export: warning: set cell %s%d: %v\n", col, row, err)
	}
}

func setIntCell(f *excelize.File, sheet string, row int, colMap map[string]int, key string, value int) {
	colIdx, ok := colMap[key]
	if !ok {
		return
	}
	col := colNum(colIdx + 1) // colMap stores 0-based indices; colNum wants 1-based
	cell := fmt.Sprintf("%s%d", col, row)
	if err := f.SetCellInt(sheet, cell, int64(value)); err != nil {
		fmt.Printf("export: warning: set int cell %s: %v\n", cell, err)
	}
}

// setIntCellDirect writes an int to a cell addressed by an explicit column
// letter (as returned by colNum) and 1-based row. Used by the team overlays,
// which compute column letters directly rather than via a header colMap.
func setIntCellDirect(f *excelize.File, sheet, col string, row, value int) {
	cell := fmt.Sprintf("%s%d", col, row)
	if err := f.SetCellInt(sheet, cell, int64(value)); err != nil {
		fmt.Printf("export: warning: set int cell %s: %v\n", cell, err)
	}
}

// matchJob identifies one pool match by its pool index and match index within
// that pool, in the row order PrintPoolMatches lays matches out.
type matchJob struct {
	poolIdx  int
	matchIdx int
}

// buildColumnMap returns header label -> 0-based column index for all non-empty cells.
func buildColumnMap(headerRow []string) map[string]int {
	m := make(map[string]int, len(headerRow))
	for i, cell := range headerRow {
		if cell != "" {
			if _, exists := m[cell]; !exists {
				m[cell] = i
			}
		}
	}
	return m
}

func standingMap(standings []state.PlayerStanding) map[string]state.PlayerStanding {
	m := make(map[string]state.PlayerStanding, len(standings))
	for _, ps := range standings {
		m[ps.Player.Name] = ps
	}
	return m
}

// buildBracketMatchIndex indexes all bracket matches by ID.
func buildBracketMatchIndex(bracket *state.Bracket) map[string]state.BracketMatch {
	idx := make(map[string]state.BracketMatch)
	for _, round := range bracket.Rounds {
		for _, bm := range round {
			idx[bm.ID] = bm
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		idx[bracket.ThirdPlaceMatch.ID] = *bracket.ThirdPlaceMatch
	}
	return idx
}

func findBracketMatchByNumber(idx map[string]state.BracketMatch, matchNum int) *state.BracketMatch {
	for _, bm := range idx {
		if bm.MatchNumber == matchNum {
			bm2 := bm
			return &bm2
		}
	}
	return nil
}

// parseRoundMatchLabel parses "Round R - Match M" and returns (R, M).
// Returns (0, 0) when the string does not match that pattern.
func parseRoundMatchLabel(s string) (round, match int) {
	if !strings.Contains(s, "Round") || !strings.Contains(s, "Match") {
		return 0, 0
	}
	_, err := fmt.Sscanf(s, "Round %d - Match %d", &round, &match)
	if err != nil {
		return 0, 0
	}
	return round, match
}

// writeWinnerCell scans nearby rows for a "1." label and writes the winner
// name into the adjacent result cell.
func writeWinnerCell(f *excelize.File, sheetName string, rows [][]string, scoreRowIdx, headerCol int, winner string) {
	// The ordinal "1." label is in resLabelColName = startCol+5 = headerCol+5 (0-based).
	// The winner name cell is in resNameColName = startCol+6 = headerCol+6 (0-based)
	// = headerCol+7 when passed to colNum (which expects 1-based).
	ordinalColIdx := headerCol + 5 // 0-based
	nameColIdx := headerCol + 7    // 1-based for colNum
	for offset := 1; offset <= 10; offset++ {
		checkIdx := scoreRowIdx + offset
		if checkIdx >= len(rows) {
			break
		}
		row := rows[checkIdx]
		if ordinalColIdx < len(row) && row[ordinalColIdx] == "1." {
			excelRow := checkIdx + 1
			nameCol := colNum(nameColIdx)
			setCellStr(f, sheetName, nameCol, excelRow, winner)
			return
		}
	}
}
