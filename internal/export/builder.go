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

// ErrCompetitionNotFound is returned by BuildResultsWorkbook when the competition
// ID does not exist, so the handler can map it to HTTP 404 (matching every other
// competition endpoint) rather than an opaque 500.
var ErrCompetitionNotFound = errors.New("competition not found")

// BuildResultsWorkbook reads live tournament state and produces a results-
// populated XLSX workbook for the given competition. Both pool results (scores
// + standings) and elimination bracket results are included as literal values,
// so the workbook is suitable for archiving after a live event.
//
// This is a SEPARATE path from Engine.ExportCompetitionXlsx (the blank-template
// export). That function and the existing GET /api/competitions/:id/export
// endpoint are not modified.
//
// Download filename served by the handler: "results-<compID>.xlsx".
func BuildResultsWorkbook(store *state.Store, eng *engine.Engine, compID string) ([]byte, error) {
	comp, err := store.LoadCompetition(compID)
	if err != nil {
		return nil, fmt.Errorf("export: load competition %s: %w", compID, err)
	}
	if comp == nil {
		return nil, fmt.Errorf("export: competition %s: %w", compID, ErrCompetitionNotFound)
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
	poolOrdinals := attachPoolMatches(pools, matchResults)

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
	poolCoords, playerCoords := helper.AddPoolDataToSheet(f, pools, comp.EffectiveWithZekkenName(), comp.Name)

	// 2. Pool Draw sheet (formula refs to data sheet survive store round-trips).
	if err := helper.AddPoolsToSheet(f, pools, poolCoords, playerCoords); err != nil {
		return nil, fmt.Errorf("export: add pools to sheet: %w", err)
	}

	// 3. Pool Matches sheet: lay skeleton, then overlay literal scores and standings.
	//    W/L/T/RANK formula cells collapse to 0 after a store round-trip
	//    (documented at cmd/create_handler.go:25), so we overwrite them with
	//    literal values from the engine.
	// The shiaijo BY NAME, mirroring the blank-template export: a competition
	// allocated C and D must not have its sheets titled A and B. The count is
	// read off the same list rather than derived a second time.
	courts := engine.CompetitionCourts(store, comp)
	numCourts := len(courts)
	// Where each pool is actually being fought, so the archived workbook bands a
	// pool under the shiaijo it was scored on.
	courtOfPool := engine.PoolCourtByName(matchResults)
	// numCourts is the operator's ALLOCATION; the pool-banded sheet clamps it
	// itself to the count the pool phase actually runs on. The grouping the
	// skeleton LAID OUT comes back with the winners and is what both overlays
	// write against -- taken from the skeleton rather than recomputed here, so
	// "computed ONCE and handed to every overlay" is enforced by the call shape
	// instead of by two calls happening to be given the same arguments.
	// MatchWinnerRanksNeeded (not EffectivePoolWinners() directly): mirrors
	// the blank-template export (internal/engine/export.go) so the two
	// exports of one competition register the SAME matchWinners ranks --
	// under bc-qual larger-pools, an oversized pool's crossed 2nd needs a
	// matchWinners["<pool>-2nd"] entry too, or its cell renders as inert
	// literal text (or a broken CONCATENATE formula) instead of a live link.
	matchWinners, poolsByCourt := helper.PrintPoolMatches(
		f, pools, comp.TeamSize, comp.MatchWinnerRanksNeeded(),
		courts, courtOfPool, comp.Mirror, poolCoords, playerCoords, comp.Engi,
	)
	if err := overlayPoolScores(f, pools, matchResultByID, poolOrdinals, comp.TeamSize, comp.Mirror, poolsByCourt, comp.Engi); err != nil {
		return nil, fmt.Errorf("export: overlay pool scores: %w", err)
	}
	if err := overlayPoolStandings(f, pools, standings, comp.TeamSize, poolsByCourt, comp.Engi); err != nil {
		return nil, fmt.Errorf("export: overlay standings: %w", err)
	}

	// 4. Elimination Matches + Tree sheets. Only for formats with a knockout
	//    phase: the IsPlayoffEnabled gate below drops the phantom bracket a
	//    league's placeholder finals would otherwise imply. EliminationDraw owns
	//    the leaf order -- pool winners, or the frozen bracket's own leaves for a
	//    pure playoffs competition -- and is shared with the blank-template export
	//    so the two exports of one competition render the identical bracket, with
	//    numbering that matches the stored bracket overlayBracketScores fills in
	//    (mp-ndfu).
	draw := engine.EliminationDraw(store, comp, pools, bracket, numCourts)
	if draw != nil && comp.IsPlayoffEnabled() {
		// Tree sheets FIRST, then the Elimination Matches skeleton, in the one
		// mandatory order RenderKnockoutPages enforces (also behind the CLI and
		// the blank-template export). The skeleton's "Round N - Match N" headers
		// are what overlayBracketScores below scans. Bronze gates on the stored
		// bracket's ThirdPlaceMatch: the bracket is authoritative here, unlike
		// the CLI's flag-derived NeedsBronzeBlock.
		// Band each bout by the shiaijo it is CURRENTLY on, read off the stored
		// bracket the overlay below fills in, so the archived workbook records
		// where each bout was actually fought rather than where the draw first
		// put it. ONE plan for the tree pages and the elimination sheet: they
		// describe the same bouts, so resolving their shiaijo separately is how
		// a wall chart headed "Shiaijo D" ends up filed with score sheets banded
		// "Shiaijo A".
		plan := engine.LiveCourtPlan(draw, courts, bracket)
		eliminationMatchRounds, _, err := helper.RenderKnockoutPages(f, plan, false, pools, poolCoords, playerCoords, matchWinners)
		if err != nil {
			return nil, fmt.Errorf("export: %w", err)
		}
		helper.PrintEliminationWithBronze(f, matchWinners, eliminationMatchRounds, comp.TeamSize,
			plan, comp.Mirror, comp.Engi,
			bracket != nil && bracket.ThirdPlaceMatch != nil)

		// Overlay literal scores from the live bracket state.
		if bracket != nil {
			bracketByNum := buildBracketMatchIndex(bracket)
			thirdPlaceMatch := bracket.ThirdPlaceMatch
			if err := overlayBracketScores(f, bracketByNum, comp.TeamSize, comp.Mirror, comp.Engi, thirdPlaceMatch); err != nil {
				return nil, fmt.Errorf("export: overlay bracket scores: %w", err)
			}
			// Playoffs have no pool data sheet, so the pool-oriented renderer emits
			// broken ''! references for the entrant name cells. Overwrite them with
			// the stored bracket's literal names (empty for unresolved slots) so the
			// sheet is a valid literal snapshot with no broken formulas.
			if len(pools) == 0 && comp.Format == state.CompFormatPlayoffs {
				if err := overlayPlayoffBracketNames(f, bracketByNum, comp.TeamSize, comp.Mirror); err != nil {
					return nil, fmt.Errorf("export: overlay playoff names: %w", err)
				}
			}
		}
	}
	// The bare "Tree" sheet is a styled scaffold that every page is copied from,
	// never output itself. Delete it whether it was consumed above or left unused
	// by a format with no knockout phase, so no blank tree page reaches the
	// workbook or the printed booklet.
	if derr := f.DeleteSheet(helper.SheetTree); derr != nil {
		return nil, fmt.Errorf("export: delete tree template sheet: %w", derr)
	}

	// 5. Names to Print sheet (identical to blank-template export). Clamps the
	//    allocation to the pool phase's own shiaijo count internally, as step 3
	//    does.
	helper.CreateNamesWithPoolToPrint(f, pools, comp.EffectiveWithZekkenName(), courts, courtOfPool, playerCoords)

	// 6. Kachinuki Detail sheet: bout-by-bout log for kachinuki team
	//    competitions (GAP 6). Same opt-in semantics as the blank-template
	//    export (Engine.ExportCompetitionXlsx step 7): the renderer is a
	//    no-op for empty input, so fixed-format and individual comps are
	//    unaffected. Without this, the admin "Download results" workbook
	//    (which builds HERE, not via ExportCompetitionXlsx) had no bout log.
	kachinukiMatches, err := eng.KachinukiDetailMatches(compID)
	if err != nil {
		return nil, fmt.Errorf("export: collect kachinuki detail: %w", err)
	}
	if err := helper.WriteKachinukiDetailSheet(f, kachinukiMatches); err != nil {
		return nil, fmt.Errorf("export: write kachinuki detail sheet: %w", err)
	}

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
// overlayTeamPoolScores maps the N-th grid row back to its result.
//
// Because an unresolvable match is SKIPPED (see below), pool.Matches can be
// non-contiguous relative to the stored "<Pool>-<suffix>" IDs, so this returns
// poolOrdinals: poolName -> the original numeric suffix of each KEPT match, in
// grid order. The overlays use poolOrdinals[pool][i] to rebuild the result ID for
// grid row i, rather than assuming row i == suffix i. Tiebreak/daihyosen results
// (non-numeric suffix, e.g. "Pool A-DH-0") are skipped.
//
// Each side is resolved to its pool Player by the authoritative SideAID/SideBID
// UUID first, which disambiguates same-name-different-dojo participants. Legacy
// results written before side UUIDs existed fall back to name matching (last
// write wins in the name map), so exact-duplicate names in such old data can
// still be conflated; current data always carries the UUIDs.
func attachPoolMatches(pools []helper.Pool, matchResults []state.MatchResult) map[string][]int {
	poolOrdinals := make(map[string][]int, len(pools))
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

		byID := make(map[string]*helper.Player, len(p.Players))
		byName := make(map[string]*helper.Player, len(p.Players))
		for i := range p.Players {
			pl := &p.Players[i]
			if pl.ID != "" {
				byID[pl.ID] = pl
			}
			byName[pl.Name] = pl
		}
		// Prefer the authoritative side UUID (SideAID/SideBID from pool-matches.csv)
		// and fall back to the display name. Names are not unique within a
		// competition (same name, different dojo is allowed), so a name-only lookup
		// could attach the wrong Player and mislabel the grid; the UUID disambiguates.
		resolve := func(id, name string) *helper.Player {
			if id != "" {
				if pl, ok := byID[id]; ok {
					return pl
				}
			}
			return byName[name]
		}

		p.Matches = make([]helper.Match, 0, len(mine))
		ords := make([]int, 0, len(mine))
		for _, ir := range mine {
			sideA := resolve(ir.mr.SideAID, ir.mr.SideA)
			sideB := resolve(ir.mr.SideBID, ir.mr.SideB)
			// A side that resolves to no pool member (e.g. a participant removed
			// after the match was recorded, or partially-written state) would be a
			// nil *Player, which PrintPoolMatches dereferences unconditionally and
			// panics on. Skip the unresolvable match: the skeleton row is simply left
			// without an overlaid score, consistent with the frozen-snapshot semantics.
			// The skip is why we track the original ordinal separately below.
			if sideA == nil || sideB == nil {
				continue
			}
			p.Matches = append(p.Matches, helper.Match{SideA: sideA, SideB: sideB})
			ords = append(ords, ir.idx)
		}
		poolOrdinals[p.PoolName] = ords
	}
	return poolOrdinals
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
func overlayPoolScores(f *excelize.File, pools []helper.Pool, resultByID map[string]state.MatchResult, poolOrdinals map[string][]int, teamSize int, mirror bool, poolsByCourt [][]int, engi bool) error {
	if len(pools) == 0 {
		return nil
	}
	if teamSize != 0 {
		return overlayTeamPoolScores(f, pools, resultByID, poolOrdinals, teamSize, mirror, poolsByCourt)
	}

	sheetName := helper.SheetPoolMatches

	// helper.PoolsByCourt owns the pool clamp, so the grouping's length is the
	// number of bands the skeleton actually printed.
	numCourts := len(poolsByCourt)

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

			// Column layout (1-based) for the default mirror=false template:
			// startCol+1 = left victories (Red/SideA), startCol+3 = middle/vs,
			// startCol+5 = right victories (White/SideB). With mirror=true the
			// sides swap physically (White left, Red right); writeScoreRowCells
			// owns that display swap and derives the columns from courtStartCol.
			courtStartCol := 1 + c*helper.CourtsColumnsPerCourt

			ords := poolOrdinals[pool.PoolName]
			for i := range pool.Matches {
				// Grid row i maps back to its stored result via the ORIGINAL numeric
				// suffix recorded by attachPoolMatches (row i is NOT necessarily suffix
				// i once an unresolvable match has been skipped). A missing result is
				// simply left blank.
				if i >= len(ords) {
					break
				}
				matchID := fmt.Sprintf("%s-%d", pool.PoolName, ords[i])
				mr, found := resultByID[matchID]
				if !found || mr.Status != state.MatchStatusCompleted {
					continue
				}
				// Header is at excel row rowIdx+1; match i sits at header + 1 + i.
				excelRow := rowIdx + 2 + i

				var scoreA, scoreB string
				if engi {
					scoreA, scoreB = FlagsScorePair(mr.FlagsA, mr.FlagsB)
				} else {
					// The maru fallback is a property of ippon-score
					// derivation, so it rides the else branch and never
					// touches engi flag counts.
					scoreA, scoreB = DefaultWinMaruAB(
						IpponsScore(mr.IpponsA), IpponsScore(mr.IpponsB),
						mr.Decision, mr.Encho, mr.Winner, mr.SideA, mr.SideB)
				}
				writeScoreRowCells(f, sheetName, courtStartCol, excelRow, scoreA, scoreB, mr, mirror)
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
func overlayTeamPoolScores(f *excelize.File, pools []helper.Pool, resultByID map[string]state.MatchResult, poolOrdinals map[string][]int, teamSize int, mirror bool, poolsByCourt [][]int) error {
	sheetName := helper.SheetPoolMatches

	courtMatches := buildCourtMatchJobs(pools, poolsByCourt, poolOrdinals)
	// buildCourtMatchJobs inherits the pool clamp from helper.PoolsByCourt, so
	// its length is the band count the skeleton actually printed.
	numCourts := len(courtMatches)

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
			// job.ordinal is the ORIGINAL numeric suffix of this match's stored result
			// ID (buildCourtMatchJobs threads it through poolOrdinals), so a skipped
			// unresolvable match doesn't shift the lookup for the matches after it.
			matchID := fmt.Sprintf("%s-%d", pool.PoolName, job.ordinal)
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
			writeTeamSubMatchScores(f, sheetName, courtStartCol, subStartExcelRow, mr.SubResults, teamSize, mirror)
		}
	}

	return nil
}

// buildCourtMatchJobs returns, per court, the ordered list of match jobs in the
// row order PrintPoolMatches lays them out (pool 0 matches, then pool 1 matches,
// ...). Each job carries the pool index and the match's ORIGINAL numeric suffix
// (from poolOrdinals), so the team overlay rebuilds the correct result ID even
// when an unresolvable match was skipped. Shared by the individual and team
// pool-score overlays.
func buildCourtMatchJobs(pools []helper.Pool, poolsByCourt [][]int, poolOrdinals map[string][]int) [][]matchJob {
	// Sized from the clamped grouping, never from the requested numCourts.
	courtMatches := make([][]matchJob, len(poolsByCourt))
	for c := range poolsByCourt {
		for _, pi := range poolsByCourt[c] {
			ords := poolOrdinals[pools[pi].PoolName]
			for mi := range pools[pi].Matches {
				if mi >= len(ords) {
					break
				}
				courtMatches[c] = append(courtMatches[c], matchJob{poolIdx: pi, ordinal: ords[mi]})
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
// The middle "vs" cell carries only the encounter's single middle mark
// (X for a drawn encounter, (DH) when it went to a representative bout);
// encounter-level result marks (Kiken/Fus./Ht) ride in the competitor's IV
// cell, next to their victory count.
func writeTeamSummaryCells(f *excelize.File, sheetName string, courtStartCol, excelRow int, mr state.MatchResult, mirror bool) {
	lVCol := colNum(courtStartCol + 1)
	lPCol := colNum(courtStartCol + 2)
	rPCol := colNum(courtStartCol + 4)
	rVCol := colNum(courtStartCol + 5)

	lMark, rMark := SideMarksLR(mr.Decision, hanteiOf(mr), mr.Winner, mr.SideA, mr.SideB, mirror)

	line := state.TeamResultFrom(mr.SubResults, mr.SideA, mr.SideB)
	if line != nil {
		// SideA = Aka, SideB = Shiro. Left is Aka unless mirror.
		leftIV, leftPW := line.AkaIV, line.AkaPW
		rightIV, rightPW := line.ShiroIV, line.ShiroPW
		if mirror {
			leftIV, leftPW, rightIV, rightPW = rightIV, rightPW, leftIV, leftPW
		}
		setIVCellWithMark(f, sheetName, lVCol, excelRow, leftIV, lMark)
		setIntCellDirect(f, sheetName, lPCol, excelRow, leftPW)
		setIVCellWithMark(f, sheetName, rVCol, excelRow, rightIV, rMark)
		setIntCellDirect(f, sheetName, rPCol, excelRow, rightPW)
	} else {
		// No summary line (e.g. a forfeit before any bout was fought):
		// the result marks still need a home in the competitor's cell.
		if lMark != "" {
			setCellStr(f, sheetName, lVCol, excelRow, lMark)
		}
		if rMark != "" {
			setCellStr(f, sheetName, rVCol, excelRow, rMark)
		}
	}

	writeMiddleMarkCell(f, sheetName, courtStartCol, excelRow, mr.Decision, mr.Encho)
}

// writeScoreRowCells writes an individual match's score row: each side's
// score joined with its result mark (Kiken/Fus./Ht ride in the competitor's
// cell), and the single middle mark. Takes SIDE-ordered scores (A = Aka,
// B = Shiro) and owns the display swap itself — like writeTeamSubMatchScores —
// so no caller re-enforces the mirror rule. Shared by the pool and bracket
// overlays so the cell contract lives in one place.
func writeScoreRowCells(f *excelize.File, sheetName string, courtStartCol, excelRow int, scoreA, scoreB string, mr state.MatchResult, mirror bool) {
	leftScore, rightScore := scoreA, scoreB
	if mirror {
		leftScore, rightScore = scoreB, scoreA
	}
	lMark, rMark := SideMarksLR(mr.Decision, hanteiOf(mr), mr.Winner, mr.SideA, mr.SideB, mirror)
	setCellStr(f, sheetName, colNum(courtStartCol+1), excelRow, joinSp(leftScore, lMark))
	setCellStr(f, sheetName, colNum(courtStartCol+5), excelRow, joinSp(rightScore, rMark))
	writeMiddleMarkCell(f, sheetName, courtStartCol, excelRow, mr.Decision, mr.Encho)
}

// writeMiddleMarkCell writes the single middle mark (X / (E) / (DH)),
// leaving the cell untouched when no mark applies so the template's own
// "vs" survives. The one place the middle-cell contract lives.
func writeMiddleMarkCell(f *excelize.File, sheetName string, courtStartCol, excelRow int, decision string, encho *state.EnchoMetadata) {
	if mid := MiddleMark(decision, encho); mid != "" {
		setCellStr(f, sheetName, colNum(courtStartCol+3), excelRow, mid)
	}
}

// hanteiOf reports whether the judges'-decision mark stands on the match:
// the mark is an entry in the winner's ippon slice (the mark IS the record).
func hanteiOf(mr state.MatchResult) bool {
	return mr.HanteiDecided()
}

// bracketMatchResultView adapts a BracketMatch to the MatchResult shape the
// shared row writers consume (they read only the result fields).
func bracketMatchResultView(bm *state.BracketMatch) state.MatchResult {
	return state.MatchResult{
		SideA:      bm.SideA,
		SideB:      bm.SideB,
		Winner:     bm.Winner,
		Decision:   bm.Decision,
		Encho:      bm.Encho,
		SubResults: bm.SubResults,
		// The scoreline (and with it the judges'-decision mark, an ippon
		// entry) is a direct field read: BracketMatch persists ippon arrays
		// natively, the same shape as MatchResult.
		IpponsA:  bm.IpponsA,
		IpponsB:  bm.IpponsB,
		HansokuA: bm.HansokuA,
		HansokuB: bm.HansokuB,
	}
}

// setIVCellWithMark writes a team IV count, appending a result mark
// ("2 Kiken") when one applies to that side; a markless cell stays numeric.
func setIVCellWithMark(f *excelize.File, sheetName, col string, row, iv int, mark string) {
	if mark == "" {
		setIntCellDirect(f, sheetName, col, row, iv)
		return
	}
	setCellStr(f, sheetName, col, row, joinSp(fmt.Sprintf("%d", iv), mark))
}

// writeTeamSubMatchScores writes each sub-bout's ippon letters onto the team
// sub-match rows. Left ippons -> lVCol (startCol+1), right -> rVCol (startCol+5),
// middle "vs" -> tie marker / suffix. subResults are keyed by Position (1-based);
// the daihyosen placeholder (Position < 0) is skipped so its blank row stays clean.
// teamSize bounds the number of sub-match rows the grid actually has; a Position
// outside [1, teamSize] (corrupted state) is skipped rather than writing into the
// next encounter's cells. Shared by the pool sheet and overlayTeamBracketScores.
func writeTeamSubMatchScores(f *excelize.File, sheetName string, courtStartCol, subStartExcelRow int, subResults []state.SubMatchResult, teamSize int, mirror bool) {
	lVCol := colNum(courtStartCol + 1)
	rVCol := colNum(courtStartCol + 5)

	for _, sub := range subResults {
		if sub.Position <= 0 || sub.Position > teamSize {
			continue // skip daihyosen placeholder / unpositioned / out-of-range rows
		}
		// Sub-match row for Position P is the P-th sub row (1-based Position).
		excelRow := subStartExcelRow + (sub.Position - 1)

		scoreA, scoreB := DefaultWinMaruAB(
			IpponsScore(sub.IpponsA), IpponsScore(sub.IpponsB),
			sub.Decision, sub.Encho, sub.Winner, sub.SideA, sub.SideB)
		leftScore, rightScore := scoreA, scoreB
		if mirror {
			leftScore, rightScore = scoreB, scoreA
		}
		lMark, rMark := SideMarksLR(sub.Decision, sub.HanteiDecided(), sub.Winner, sub.SideA, sub.SideB, mirror)
		if lScore := joinSp(leftScore, lMark); lScore != "" {
			setCellStr(f, sheetName, lVCol, excelRow, lScore)
		}
		if rScore := joinSp(rightScore, rMark); rScore != "" {
			setCellStr(f, sheetName, rVCol, excelRow, rScore)
		}

		writeMiddleMarkCell(f, sheetName, courtStartCol, excelRow, sub.Decision, sub.Encho)
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
func overlayPoolStandings(f *excelize.File, pools []helper.Pool, standings map[string][]state.PlayerStanding, teamSize int, poolsByCourt [][]int, engi bool) error {
	if len(pools) == 0 {
		return nil
	}
	if teamSize != 0 {
		return overlayTeamPoolStandings(f, pools, standings, poolsByCourt)
	}

	sheetName := helper.SheetPoolMatches

	// helper.PoolsByCourt owns the pool clamp, so the grouping's length is the
	// number of bands the skeleton actually printed.
	numCourts := len(poolsByCourt)

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
			// Scope the header map to THIS court's 8-column band. Pool Matches
			// repeats the W/L/T/PW/PL/Rank headers once per court, and a whole-row
			// map keeps only the first occurrence, so on a multi-court sheet every
			// court past the first would otherwise write its standings into court 0's
			// columns.
			colMap := buildCourtColumnMap(row, startColIdx)

			// Write standings literals for each player (in pool draw order).
			for i, player := range pool.Players {
				dataRowIdx := rowIdx + 1 + i
				if dataRowIdx >= len(rows) {
					break
				}
				ps, ok := byName[standingKey(player)]
				if !ok {
					continue
				}
				excelRow := dataRowIdx + 1
				// teamSize == 0 is guaranteed here (we returned early above for team competitions).
				setIntCell(f, sheetName, excelRow, colMap, "W", ps.Wins)
				if engi {
					// Engi standings: W / Flags / Rank only (no L, T, PW, PL).
					// Losses are not recorded in engi; skipping "L" here keeps
					// that cell blank (matching the formula sheet which also
					// omits the L column for engi).
					setIntCell(f, sheetName, excelRow, colMap, helper.ColHeaderFlags, ps.Flags)
				} else {
					setIntCell(f, sheetName, excelRow, colMap, "L", ps.Losses)
					setIntCell(f, sheetName, excelRow, colMap, "T", ps.Draws)
					setIntCell(f, sheetName, excelRow, colMap, "PW", ps.IpponsGiven)
					setIntCell(f, sheetName, excelRow, colMap, "PL", ps.IpponsTaken)
				}
				setIntCell(f, sheetName, excelRow, colMap, "Rank", ps.Rank)
			}
		}
	}

	// Overlay Ranking sections.
	return overlayRankingSections(f, sheetName, rows, pools, standings, numCourts, poolsByCourt)
}

// overlayRankingSections replaces the IFERROR/INDEX/MATCH formula cells in the
// "Ranking" sections with literal player names from the engine-ordered standings.
func overlayRankingSections(f *excelize.File, sheetName string, rows [][]string, pools []helper.Pool, standings map[string][]state.PlayerStanding, numCourts int, poolsByCourt [][]int) error {

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
func overlayTeamPoolStandings(f *excelize.File, pools []helper.Pool, standings map[string][]state.PlayerStanding, poolsByCourt [][]int) error {
	sheetName := helper.SheetPoolMatches

	// helper.PoolsByCourt owns the pool clamp, so the grouping's length is the
	// number of bands the skeleton actually printed.
	numCourts := len(poolsByCourt)

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
			// Table 2 reuses startCol+1..+3 for IV/IL/IT (same physical columns as
			// W/L/T, different meaning), then +4/+5 for PW/PL.
			ivCol, ilCol, itCol := wCol, lCol, tCol
			pwCol := colNum(courtStartCol + 4)
			plCol := colNum(courtStartCol + 5)

			nPlayers := len(pool.Players)
			for i, player := range pool.Players {
				ps, ok := byName[standingKey(player)]
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

	return overlayRankingSections(f, sheetName, rows, pools, standings, numCourts, poolsByCourt)
}

// ---------- bracket score overlay ----------

// overlayBracketScores writes literal score values into the Elimination Matches
// sheet by scanning for "Round N - Match N" header cells and, when present, a
// "3rd Place" header cell. For each completed match found, the score cells in
// the row two rows below the header are overwritten with literal values.
// thirdPlaceMatch is the bracket's bronze match (nil when absent/not naginata).
func overlayBracketScores(f *excelize.File, bracketByNum map[int]state.BracketMatch, teamSize int, mirror bool, engi bool, thirdPlaceMatch *state.BracketMatch) error {
	if teamSize != 0 {
		// Engi is individual-only; the team overlay renders ippon strings and
		// would silently drop flag scores. Fail loudly if the invariant breaks.
		if engi {
			return fmt.Errorf("overlayBracketScores: engi is individual-only (teamSize=%d)", teamSize)
		}
		return overlayTeamBracketScores(f, bracketByNum, teamSize, mirror, thirdPlaceMatch)
	}
	sheetName := helper.SheetEliminationMatches

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayBracketScores: get rows: %w", err)
	}

	// Courts are laid out side-by-side (one 8-column band each), so a single row
	// can carry a "Round N - Match N" header at EACH court's start column. Process
	// every header in the row, not just the first. Also handle the "3rd Place"
	// bronze header when thirdPlaceMatch is present.
	for rowIdx, row := range rows {
		for headerCol, cell := range row {
			bm, ok := resolveBracketMatch(cell, bracketByNum, thirdPlaceMatch)
			if !ok {
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

			// For the 3rd Place block write entrant names unconditionally so they
			// appear even when the bronze match is not yet played. Scores, the
			// middle cell, and the winner marker remain gated on
			// MatchStatusCompleted below.
			if cell == helper.ThirdPlaceLabel {
				if !writeThirdPlaceEntrants(f, sheetName, bm, courtStartCol, excelRow, mirror) {
					continue
				}
			}

			// For engi, the bracket stores flag counts in FlagsA/FlagsB;
			// ScoreA/ScoreB hold ippon letters that do not apply. Render the
			// flag count via FlagsScorePair instead, matching
			// overlayPoolScores.
			mrView := bracketMatchResultView(&bm)
			var scoreA, scoreB string
			if engi {
				scoreA, scoreB = FlagsScorePair(bm.FlagsA, bm.FlagsB)
			} else {
				// bm.IpponsA/IpponsB (via mrView) carry the raw ippon entries,
				// including the judges'-decision mark as a literal "Ht" entry
				// (domain.HanteiMark). writeScoreRowCells below ALSO appends
				// that mark via SideMarksLR (reading it off mrView), so
				// rendering the array verbatim would double-print it:
				// "MHt Ht". Render through IpponsScore instead, which filters
				// non-scoring entries (the mark, the bye placeholder) exactly
				// as the pool path (overlayPoolScores) already does — the
				// mark then rides ONLY through the appended SideMarksLR
				// suffix, matching the pool cell's "M Ht".
				scoreA, scoreB = DefaultWinMaruAB(
					IpponsScore(mrView.IpponsA), IpponsScore(mrView.IpponsB),
					bm.Decision, bm.Encho, bm.Winner, bm.SideA, bm.SideB)
			}

			writeScoreRowCells(f, sheetName, courtStartCol, excelRow, scoreA, scoreB, mrView, mirror)

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
func overlayTeamBracketScores(f *excelize.File, bracketByNum map[int]state.BracketMatch, teamSize int, mirror bool, thirdPlaceMatch *state.BracketMatch) error {
	sheetName := helper.SheetEliminationMatches

	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayTeamBracketScores: get rows: %w", err)
	}

	// Courts are laid out side-by-side (one 8-column band each), so a single row
	// can carry a "Round N - Match N" header at EACH court's start column. Process
	// every header in the row, not just the first. Also handle the "3rd Place"
	// bronze header when thirdPlaceMatch is present.
	for rowIdx, row := range rows {
		for headerCol, cell := range row {
			bm, ok := resolveBracketMatch(cell, bracketByNum, thirdPlaceMatch)
			if !ok {
				continue
			}

			courtStartCol := headerCol + 1 // 1-based
			headerExcelRow := rowIdx + 1   // H (1-based)

			// For the 3rd Place block write entrant names unconditionally so they
			// appear even when the bronze match is not yet played. Sub-match rows,
			// IV/PW summary, and the winner marker remain gated on
			// MatchStatusCompleted below.
			if cell == helper.ThirdPlaceLabel {
				if !writeThirdPlaceEntrants(f, sheetName, bm, courtStartCol, headerExcelRow+2, mirror) {
					continue
				}
			}

			// Sub-match ippon letters: Position p sits at H+2+p, i.e. the sub
			// rows start at H+3. Same writer as the pool sheet.
			writeTeamSubMatchScores(f, sheetName, courtStartCol, headerExcelRow+3, bm.SubResults, teamSize, mirror)

			// IV/PW summary row = H + 5 + teamSize. Route through the shared
			// pool-sheet writer so the IV-mark contract (and the forfeit
			// fallback when no summary line exists) lives in one place.
			summaryExcelRow := headerExcelRow + 5 + teamSize
			writeTeamSummaryCells(f, sheetName, courtStartCol, summaryExcelRow, bracketMatchResultView(&bm), mirror)

			// Winner marker: the "1." row is 3 rows below the summary row; reuse the
			// individual writer, which scans forward for the "1." ordinal.
			if bm.Winner != "" {
				writeWinnerCell(f, sheetName, rows, summaryExcelRow-1, headerCol, bm.Winner)
			}
		}
	}
	return nil
}

// writeThirdPlaceEntrants writes the bronze-match entrant names into the name
// cells (court start column and start+6) on entrantRow. A side is written
// whenever the app knows it (non-empty), even before the bronze bout is
// played, because the app's semifinal loser is authoritative (it accounts for
// kiken/decision outcomes the sheet formulas cannot derive); this matches the
// literal-name snapshot semantics overlayPlayoffBracketNames applies to every
// other match. An EMPTY side is skipped so the cell keeps the self-populating
// CONCATENATE formula written by PrintThirdPlaceBlock (pinned by the
// no-scoring-yet formulas test in builder_test.go). The return value reports
// whether the match is completed so the caller can skip the score overlay for
// an unplayed match.
func writeThirdPlaceEntrants(f *excelize.File, sheetName string, bm state.BracketMatch, courtStartCol, entrantRow int, mirror bool) bool {
	leftNameCol := colNum(courtStartCol)
	rightNameCol := colNum(courtStartCol + 6)
	sideA, sideB := bm.SideA, bm.SideB
	if mirror {
		sideA, sideB = sideB, sideA
	}
	if sideA != "" {
		setCellStr(f, sheetName, leftNameCol, entrantRow, sideA)
	}
	if sideB != "" {
		setCellStr(f, sheetName, rightNameCol, entrantRow, sideB)
	}
	return bm.Status == state.MatchStatusCompleted
}

// overlayPlayoffBracketNames overwrites the elimination entrant name cells with
// the stored bracket's literal SideA/SideB. Playoffs have no pool data sheet, so
// the pool-oriented renderer points those cells at an empty pool-winner cell,
// producing a broken ”! formula. Writing the literal names (or "" for an
// unresolved slot, which clears the broken formula) yields a valid snapshot.
//
// Name cells sit at the court's start column (left) and start+6 (right) on the
// entrant row (header + 2). Team brackets repeat the entrant name formulas on the
// summary row (header + 4 + teamSize, just above the "Victories / Points" row), so
// those are overwritten too.
func overlayPlayoffBracketNames(f *excelize.File, bracketByNum map[int]state.BracketMatch, teamSize int, mirror bool) error {
	sheetName := helper.SheetEliminationMatches
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayPlayoffBracketNames: get rows: %w", err)
	}

	for rowIdx, row := range rows {
		for headerCol, cell := range row {
			matchNum := parseRoundMatchLabel(cell)
			if matchNum <= 0 {
				continue
			}
			bm, ok := bracketByNum[matchNum]
			if !ok {
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
				// The repeated entrant-name formulas sit at header + 4 + teamSize
				// (rowIdx+5+teamSize), one row ABOVE the "Victories / Points" text
				// row. printSingleEliminationMatch: header + Red/White + entrant (H+2),
				// teamSize sub-match rows (H+3..H+2+teamSize), then matchRow += 2 lands
				// the summary name row at H+4+teamSize.
				summaryRow := rowIdx + 5 + teamSize
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

// matchJob identifies one pool match by its pool index and its ORIGINAL numeric
// suffix (the N in result ID "<Pool>-<N>"), in the row order PrintPoolMatches lays
// matches out. The suffix, not the grid position, is used to look up the result so
// a skipped unresolvable match doesn't shift subsequent lookups.
type matchJob struct {
	poolIdx int
	ordinal int
}

// buildCourtColumnMap returns header label -> 0-based ABSOLUTE column index,
// scanning only the given court's 8-column band [startColIdx, startColIdx+8).
// Pool Matches repeats the W/L/T/PW/PL/Rank headers once per court, so a
// whole-row map (which keeps the first occurrence) collapses every court onto
// court 0's columns; scoping to the band keeps each court's standings correct.
func buildCourtColumnMap(row []string, startColIdx int) map[string]int {
	m := make(map[string]int, helper.CourtsColumnsPerCourt)
	end := startColIdx + helper.CourtsColumnsPerCourt
	for i := startColIdx; i < end && i < len(row); i++ {
		cell := row[i]
		if cell == "" {
			continue
		}
		if _, exists := m[cell]; !exists {
			m[cell] = i
		}
	}
	return m
}

// standingMap keys standings by participant ID (falling back to name for legacy
// state without UUIDs) so two same-name competitors in one pool don't collapse
// onto a single entry. Look up with standingKey(player).
func standingMap(standings []state.PlayerStanding) map[string]state.PlayerStanding {
	m := make(map[string]state.PlayerStanding, len(standings))
	for _, ps := range standings {
		m[standingKey(ps.Player)] = ps
	}
	return m
}

// standingKey returns the lookup key for standingMap: the player's UUID when
// present, else the display name (legacy data). Mirrors the ID-first, name-
// fallback resolution used by attachPoolMatches.
func standingKey(p helper.Player) string {
	if p.ID != "" {
		return p.ID
	}
	return p.Name
}

// buildBracketMatchIndex maps MatchNumber -> match for O(1) lookup by the printed
// "Round N - Match N" number (the only way the overlays query it). Byes and other
// unnumbered matches (MatchNumber 0) are skipped, both because overlays never look
// up 0 and to avoid collapsing several of them onto a single key.
func buildBracketMatchIndex(bracket *state.Bracket) map[int]state.BracketMatch {
	idx := make(map[int]state.BracketMatch)
	add := func(bm state.BracketMatch) {
		if bm.MatchNumber > 0 {
			idx[bm.MatchNumber] = bm
		}
	}
	for _, round := range bracket.Rounds {
		for _, bm := range round {
			add(bm)
		}
	}
	if bracket.ThirdPlaceMatch != nil {
		add(*bracket.ThirdPlaceMatch)
	}
	return idx
}

// resolveBracketMatch resolves a cell label to the BracketMatch it describes.
// Regular rounds use the bracketByNum index (via parseRoundMatchLabel) and are
// returned only when completed. The bronze block uses the thirdPlaceMatch
// pointer directly (ThirdPlaceMatch.MatchNumber is 0 and is not indexed) and is
// returned whenever present, regardless of status, so the overlay can still
// write its self-populating entrant formulas before the bout is played.
// Returns (zero, false) when the cell matches no such match.
func resolveBracketMatch(cell string, bracketByNum map[int]state.BracketMatch, thirdPlaceMatch *state.BracketMatch) (state.BracketMatch, bool) {
	matchNum := parseRoundMatchLabel(cell)
	if matchNum > 0 {
		bm, ok := bracketByNum[matchNum]
		if !ok || bm.Status != state.MatchStatusCompleted {
			return state.BracketMatch{}, false
		}
		return bm, true
	}
	if cell == helper.ThirdPlaceLabel && thirdPlaceMatch != nil {
		return *thirdPlaceMatch, true
	}
	return state.BracketMatch{}, false
}

// parseRoundMatchLabel parses "Round R - Match M" and returns the match number M
// (the round is not needed by any overlay: matches are looked up by their global
// match number). Returns 0 when the string does not match that pattern.
func parseRoundMatchLabel(s string) int {
	if !strings.Contains(s, "Round") || !strings.Contains(s, "Match") {
		return 0
	}
	var round, match int
	if _, err := fmt.Sscanf(s, "Round %d - Match %d", &round, &match); err != nil {
		return 0
	}
	return match
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
