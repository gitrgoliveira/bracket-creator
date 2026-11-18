package export

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"slices"
	"sort"
	"strconv"
	"strings"

	excelize "github.com/xuri/excelize/v2"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// ErrSwissExportUnsupported is returned by BuildResultsWorkbook for
// Swiss-format competitions, which have no pool results or static bracket to
// archive as literal values.
//
// This is a plain alias of engine.ErrSwissExportUnsupported -- see that
// sentinel's doc comment for the message text and the full rationale, so
// this comment can't drift out of sync with what the error actually says.
// The sentinel must live in internal/engine, since internal/export imports
// internal/engine and the reverse direction would be an import cycle; the
// alias is what keeps errors.Is matching whichever export path -- this
// results-workbook builder or Engine.ExportCompetitionXlsx -- produced the
// error.
var ErrSwissExportUnsupported = engine.ErrSwissExportUnsupported

// ErrCompetitionNotFound is returned by BuildResultsWorkbook when the competition
// ID does not exist, so the handler can map it to HTTP 404 (matching every other
// competition endpoint) rather than an opaque 500.
var ErrCompetitionNotFound = errors.New("competition not found")

// BuildResultsWorkbook reads live tournament state and produces a results-
// populated XLSX workbook for the given competition. Pool scores, decision
// marks and elimination bracket results are written as values; the pool
// standings (W/L/T/PW/PL, the team tables, Rank and the Ranking names) stay
// the sheet's formulas over those scores, so the workbook can be checked
// against the app (see keepStandingsLive).
//
// This is a SEPARATE path from Engine.ExportCompetitionXlsx, the stored-draw
// export behind GET /api/competitions/:id/export and the PDF prints.
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
	// layout this builder produces. Block it explicitly, matching the stored-draw
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
	// the same way the stored-draw export does, otherwise the grid (and the
	// scores overlaid onto it) is empty.
	poolOrdinals := engine.AttachPoolMatches(pools, matchResults)

	standings, err := eng.CalculatePoolStandings(compID)
	if err != nil {
		return nil, fmt.Errorf("export: calculate standings: %w", err)
	}

	bracket, err := store.LoadBracket(compID)
	if err != nil {
		return nil, fmt.Errorf("export: load bracket: %w", err)
	}

	// The tournament, loaded ONCE and strictly (mp-yuy8 criterion 6), matching
	// the stored-draw export (Engine.ExportCompetitionXlsx): a corrupt
	// tournament.md now aborts this export instead of CompetitionCourts
	// silently degrading to the competition's own court list -- which prints
	// the WRONG shiaijo names on every sheet for exactly the legacy records
	// that need the venue's. A MISSING tournament.md is not an error --
	// LoadTournament returns (nil, nil) for that (state/tournament.go) -- so a
	// competition with no tournament record yet still exports.
	tourn, err := store.LoadTournament()
	if err != nil {
		return nil, fmt.Errorf("export: load tournament: %w", err)
	}

	// Index match results by ID for O(1) lookup.
	matchResultByID := make(map[string]state.MatchResult, len(matchResults))
	for _, mr := range matchResults {
		matchResultByID[mr.ID] = mr
	}

	// The shiaijo BY NAME, mirroring the stored-draw export: a competition
	// allocated C and D must not have its sheets titled A and B. The count is
	// read off the same list rather than derived a second time.
	courts := engine.CompetitionCourts(comp, tourn)
	numCourts := len(courts)
	// Where each pool is actually being fought, so the archived workbook bands a
	// pool under the shiaijo it was scored on.
	courtOfPool := engine.PoolCourtByName(matchResults)

	// EliminationDraw owns the leaf order -- pool winners, or the frozen
	// bracket's own leaves for a pure knockout competition -- and is shared
	// with the stored-draw export so the two exports of one competition
	// render the identical bracket, with numbering that matches the stored
	// bracket overlayBracketScores fills in (mp-ndfu).
	draw := engine.EliminationDraw(comp, pools, bracket, numCourts)

	// Kachinuki Detail sheet's bout log (GAP 6): bout-by-bout log for
	// kachinuki team competitions. Same opt-in semantics as the stored-draw
	// export (Engine.ExportCompetitionXlsx): the renderer is a
	// no-op for empty input, so fixed-format and individual comps are
	// unaffected. Without this, the admin "Download results" workbook
	// (which builds HERE, not via ExportCompetitionXlsx) had no bout log.
	kachinukiMatches, err := eng.KachinukiDetailMatches(compID)
	if err != nil {
		return nil, fmt.Errorf("export: collect kachinuki detail: %w", err)
	}

	// Representative Bouts sheet's bouts: the same opt-in as the stored-draw
	// export's, a fixed-order team competition's representative bouts, which
	// no other sheet of this workbook lists.
	representativeBouts, err := eng.RepresentativeBoutMatches(compID)
	if err != nil {
		return nil, fmt.Errorf("export: collect representative bouts: %w", err)
	}

	f, err := excel.NewFileFromScratch()
	if err != nil {
		return nil, fmt.Errorf("export: create workbook: %w", err)
	}
	defer func() {
		// The workbook's bytes are already taken by then, so a close failure
		// (a temp file excelize could not remove) is logged, not returned.
		if err := f.Close(); err != nil {
			log.Printf("export: %q: closing the results workbook failed: %v", compID, err)
		}
	}()

	// The shared sheet pipeline (mp-yuy8): Data, Pool Draw, Pool Matches,
	// knockout, Tree cleanup, Names to Print, Kachinuki Detail, Representative
	// Bouts -- identical
	// steps and order to Engine.ExportCompetitionXlsx. poolsByCourt is the one
	// artifact the overlays below need (PrintPoolMatches's pool-index grouping
	// per shiaijo band); everything else PrintPoolMatches/AddPoolDataToSheet
	// return is consumed entirely inside the shared pipeline. The pipeline
	// derives its own numbered-roster (namesToPrintPlayers) internally for a
	// knockout-only competition (bc-pnum A8, KnockoutNamesToPrint in
	// numbering.go): its Names-to-Print/Data writers use it for the sheets
	// that read straight off a numbered roster, but the elimination entrant
	// NAMES in the bracket sheets are a SEPARATE concern -- they still need
	// literal overlaying from the stored bracket further down (see the
	// len(pools) == 0 branch below), because this workbook is a results
	// snapshot and the pool-oriented renderer's formula references have
	// nowhere valid to point without a pool data sheet.
	// The second return value (the knockout-only numbered roster) is the
	// stored-draw export's own extra (its Tags sheet); this results
	// export has no such extra and discards it.
	poolsByCourt, _, err := eng.RenderCompetitionWorkbook(f, comp, pools, bracket, courts, courtOfPool, draw, kachinukiMatches, representativeBouts)
	if err != nil {
		return nil, fmt.Errorf("export: %w", err)
	}

	// ---- Results-only overlays: literal values over the skeleton the shared
	// pipeline just rendered. These run AFTER the whole pipeline rather than
	// interleaved with it (as they used to be, immediately after the Pool
	// Matches / Elimination Matches skeleton steps): nothing the shared
	// pipeline does past those two steps (deleting the Tree template, Names
	// to Print, Kachinuki Detail) touches either sheet again, so the order
	// shift changes nothing about what ends up in either cell. ----

	// Each team block's bout-row count, the same one the shared pipeline
	// printed the blocks with, so every row these overlays address lies
	// inside its own block.
	boutRows := comp.TeamBoutRows()

	// Pool Matches: the scores and decision marks are written as values; the
	// standings stay the sheet's own formulas over them (keepStandingsLive).
	if err := overlayPoolScores(f, pools, matchResultByID, poolOrdinals, boutRows, poolsByCourt, comp.Engi); err != nil {
		return nil, fmt.Errorf("export: overlay pool scores: %w", err)
	}
	if err := keepStandingsLive(f, pools, standings, matchResults, poolsByCourt); err != nil {
		return nil, fmt.Errorf("export: standings: %w", err)
	}

	// Elimination Matches: literal scores from the live bracket state.
	// Gated on `bracket != nil` alone (not also `draw != nil &&
	// comp.IsKnockoutEnabled()`, the condition RenderCompetitionWorkbook's own
	// knockout step gates on): both overlayBracketScores and
	// overlayKnockoutBracketNames work by SCANNING the sheet the shared
	// pipeline just produced for header cells ("Round N - Match N" / "3rd
	// Place"), so when the pipeline rendered nothing there (no derivable
	// draw, and no stored knockout content to disagree about it --
	// bracketHasKnockoutContent false, so the draw-mismatch case above
	// ERRORS OUT of RenderCompetitionWorkbook before this function ever
	// reaches here) they find nothing and are a no-op -- identical to
	// skipping the call outright. Keeping the broader gate rather than
	// nesting inside `draw != nil && IsKnockoutEnabled()` is therefore just
	// avoiding a redundant condition, not covering a reachable divergence.
	if bracket != nil {
		bracketByNum := buildBracketMatchIndex(bracket)
		thirdPlaceMatch := bracket.ThirdPlaceMatch
		if err := overlayBracketScores(f, bracketByNum, boutRows, comp.Engi, thirdPlaceMatch); err != nil {
			return nil, fmt.Errorf("export: overlay bracket scores: %w", err)
		}
		// A knockout-only competition's entrant cells are formulas into the data
		// sheet and the earlier matches' winner cells, which this results
		// snapshot never fills in; overwrite them with the stored bracket's names,
		// a side still to be decided as "M n".
		//
		// comp.EffectiveFormat(), not comp.Format directly: an unset Format ("")
		// is standalone knockout too (generation's default case), so it has the
		// identical no-pool-data-sheet shape and needs the identical overlay.
		if len(pools) == 0 && comp.EffectiveFormat() == state.CompFormatKnockout {
			if err := overlayKnockoutBracketNames(f, bracketByNum, engine.PrintedBracket(bracket), boutRows); err != nil {
				return nil, fmt.Errorf("export: overlay knockout names: %w", err)
			}
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, fmt.Errorf("export: write workbook: %w", err)
	}
	return buf.Bytes(), nil
}

// ---------- pool score overlay ----------

// overlayPoolScores writes literal score values into the Pool Matches sheet.
// The skeleton written by PrintPoolMatches uses formula references for player
// names - excelize's GetRows does NOT evaluate these formulas, so we cannot
// match by player names. Instead we use ordinal position.
//
// Individual pools render as a COMPACT block: one "White ... vs ... Red" header
// per pool, immediately followed by one row per round-robin match (in pool.Matches
// order). So the N-th header in a court column is the N-th pool assigned to that
// court, and match i sits at header row + 1 + i. SideB (White) is the left
// column and SideA (Red) the right (helper.WhiteLeft).
func overlayPoolScores(f *excelize.File, pools []helper.Pool, resultByID map[string]state.MatchResult, poolOrdinals map[string][]int, boutRows int, poolsByCourt [][]int, engi bool) error {
	if len(pools) == 0 {
		return nil
	}
	if boutRows != 0 {
		return overlayTeamPoolScores(f, pools, resultByID, poolOrdinals, boutRows, poolsByCourt)
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
			if row[startColIdx] != helper.MatchHeaderLeftLabel() {
				continue
			}

			// N-th header in court c == N-th pool assigned to that court.
			poolOrder := courtHdrIdx[c]
			courtHdrIdx[c]++
			if poolOrder >= len(poolsByCourt[c]) {
				continue
			}
			pool := pools[poolsByCourt[c][poolOrder]]

			// Column layout (1-based): startCol+1 = left victories
			// (White/SideB), startCol+3 = middle/vs, startCol+5 = right
			// victories (Red/SideA). writeScoreRowCells owns that placement
			// and derives the columns from courtStartCol.
			courtStartCol := 1 + c*helper.CourtsColumnsPerCourt

			ords := poolOrdinals[pool.PoolName]
			for i := range pool.Matches {
				// Grid row i maps back to its stored result via the ORIGINAL numeric
				// suffix recorded by engine.AttachPoolMatches (row i is NOT necessarily suffix
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
						domain.IpponsScore(mr.IpponsA), domain.IpponsScore(mr.IpponsB),
						mr.Decision, mr.Encho, mr.Attribution())
				}
				writeScoreRowCells(f, sheetName, courtStartCol, excelRow, scoreA, scoreB, mr, engi)
			}
		}
	}

	return nil
}

// overlayTeamPoolScores writes literal sub-match ippon letters + the team IV/PW
// summary onto the team pool-match layout produced by PrintPoolMatches when
// teamMatches > 0. The layout per encounter (see printSinglePool team branch):
//
//	side-label header row  (scanned: start col == "White")
//	team names / summary row  = header row + 1  (holds IV/PW summary: lV/lP left, rV/rP right)
//	sub-match rows      = header row + 2 .. header row + 1 + boutRows (ordinals 1..boutRows)
//
// It uses the same ordinal-position matching as the individual path: the N-th
// side-label header in a court's column band corresponds to the N-th match across
// that court's pools, in pool order.
func overlayTeamPoolScores(f *excelize.File, pools []helper.Pool, resultByID map[string]state.MatchResult, poolOrdinals map[string][]int, boutRows int, poolsByCourt [][]int) error {
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
			if row[startColIdx] != helper.MatchHeaderLeftLabel() {
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
			// summary row = side-label header row + 1 (1-based excel row).
			summaryExcelRow := rowIdx + 2
			writeTeamSummaryCells(f, sheetName, courtStartCol, summaryExcelRow, mr)

			// Sub-match rows start two rows below the side-label header (1-based).
			// credit: same call writeTeamSummaryCells makes internally, so
			// the summary row and the bout rows below agree on which side a
			// default-win ruling credits.
			subStartExcelRow := rowIdx + 3
			credit := state.DefaultWinCreditSide(mr.Status, mr.Decision, mr.DecisionBy, mr.Attribution())
			writeTeamSubMatchScores(f, sheetName, courtStartCol, subStartExcelRow, mr.SubResults, boutRows, mr.SideA, mr.SideB, credit)
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
// SideA is Aka (Red, right), SideB is Shiro (White, left).
// The middle "vs" cell carries only the encounter's single middle mark
// (X for a drawn encounter, (DH) when it went to a representative bout);
// encounter-level result marks (Kiken/Fus./Ht) ride in the competitor's IV
// cell, next to their victory count.
func writeTeamSummaryCells(f *excelize.File, sheetName string, courtStartCol, excelRow int, mr state.MatchResult) {
	lVCol := colNum(courtStartCol + 1)
	lPCol := colNum(courtStartCol + 2)
	rPCol := colNum(courtStartCol + 4)
	rVCol := colNum(courtStartCol + 5)

	att := mr.Attribution()
	lMark, rMark := SideMarksLR(mr.Decision, mr.HanteiDecided(), att)

	// credit is state.DefaultWinCreditSide's answer for THIS match: which
	// side (if any) a default-win ruling (kiken/fusenpai/fusensho) closing
	// it credits every unfought numbered bout to. TeamResultFrom routes it
	// through SubBoutEffectiveResult per bout, so a match decided before
	// every bout was scored still shows the default-win maru for the
	// credited side's IV/PW, the same way a played bout with its own
	// per-bout fusensho decision already does.
	credit := state.DefaultWinCreditSide(mr.Status, mr.Decision, mr.DecisionBy, att)
	line := state.TeamResultFrom(mr.SubResults, mr.SideA, mr.SideB, credit)
	if line != nil {
		// SideA = Aka, SideB = Shiro: Shiro's figures on the left, Aka's on
		// the right, through helper.WhiteLeft like every other side-ordered
		// pair this export writes.
		leftIV, rightIV := helper.WhiteLeft(line.AkaIV, line.ShiroIV)
		leftPW, rightPW := helper.WhiteLeft(line.AkaPW, line.ShiroPW)
		setIVCellWithMark(f, sheetName, lVCol, excelRow, leftIV, lMark)
		setIntCellDirect(f, sheetName, lPCol, excelRow, leftPW)
		setIVCellWithMark(f, sheetName, rVCol, excelRow, rightIV, rMark)
		setIntCellDirect(f, sheetName, rPCol, excelRow, rightPW)
	} else {
		// No summary line: mr.SubResults is genuinely empty (0 rows). Since
		// a completed default-win team match is now padded with one row per
		// numbered position (state.PadDefaultWinBoutPositions, applied both
		// at write time and by the legacy-load repair), this is no longer
		// the ordinary "forfeit before any bout was fought" case it used to
		// be -- it is now the residual fallback for what padding does not
		// reach: a bye, a daihyosen/tiebreaker row, or a match this view's
		// SubResults were never threaded through at all. The result marks
		// still need a home in the competitor's cell.
		if lMark != "" {
			setIVCellWithMark(f, sheetName, lVCol, excelRow, 0, lMark)
		}
		if rMark != "" {
			setIVCellWithMark(f, sheetName, rVCol, excelRow, 0, rMark)
		}
	}

	writeMiddleMarkCell(f, sheetName, courtStartCol, excelRow, mr.Decision, mr.Encho)
}

// writeScoreRowCells writes an individual match's score row: each side's
// score joined with its result mark (Kiken/Fus./Ht ride in the competitor's
// cell), and the single middle mark. Takes SIDE-ordered scores (A = Aka,
// B = Shiro) and places them White-left itself (helper.WhiteLeft), like
// writeTeamSubMatchScores, so no caller orders the columns. Shared by the
// pool and bracket overlays so the cell contract lives in one place. engi says
// the scores are flag counts, which are written as numbers (setScoreCell).
func writeScoreRowCells(f *excelize.File, sheetName string, courtStartCol, excelRow int, scoreA, scoreB string, mr state.MatchResult, engi bool) {
	leftScore, rightScore := helper.WhiteLeft(scoreA, scoreB)
	lFoul, rFoul := helper.WhiteLeft(HansokuMark(mr.HansokuA), HansokuMark(mr.HansokuB))
	lMark, rMark := SideMarksLR(mr.Decision, mr.HanteiDecided(), mr.Attribution())
	// The outstanding-hansoku ▲ rides the cell's OUTER edge — nearest that
	// side's name column — per FIK Table 2 (White's ▲ far left, Red's far
	// right) and matching the scoreboard's placement between name and slots.
	setScoreCell(f, sheetName, colNum(courtStartCol+1), excelRow, lFoul, leftScore, lMark, engi)
	setScoreCell(f, sheetName, colNum(courtStartCol+5), excelRow, "", rightScore, domain.JoinNonEmpty(rMark, rFoul), engi)
	writeMiddleMarkCell(f, sheetName, courtStartCol, excelRow, mr.Decision, mr.Encho)
}

// setScoreCell writes one side's score cell: before, score and after joined by
// spaces, before being the hansoku mark that leads the cell and after the marks
// that trail it. An engi flag count is written as a NUMBER, as an operator
// types it into the template, so the engi standings formulas, which count
// flags through ISNUMBER and N, read it; written as text it counted as no bout
// fought. Marks beside a count ride in the cell's number format, so the cell
// still shows "3 Kiken" while holding the number 3. Everything else, an ippon
// score being letters, is text.
func setScoreCell(f *excelize.File, sheet, col string, row int, before, score, after string, engi bool) {
	n, err := strconv.Atoi(score)
	if !engi || err != nil || n < 0 {
		setCellStr(f, sheet, col, row, domain.JoinNonEmpty(before, domain.JoinNonEmpty(score, after)))
		return
	}
	setMarkedCount(f, sheet, col, row, before, n, after)
}

// setMarkedCount writes a count as a NUMBER with its marks riding in the cell's
// number format: the cell shows "3 Kiken" while holding 3, so a formula reading
// it (an engi flag count, a team IV) still sees a number. before leads the
// shown value and after trails it.
func setMarkedCount(f *excelize.File, sheet, col string, row int, before string, n int, after string) {
	setIntCellDirect(f, sheet, col, row, n)
	if before == "" && after == "" {
		return
	}
	cell := fmt.Sprintf("%s%d", col, row)
	format := "0"
	if before != "" {
		format = `"` + before + ` "` + format
	}
	if after != "" {
		format += `" ` + after + `"`
	}
	if err := withNumberFormat(f, sheet, cell, format); err != nil {
		fmt.Printf("export: warning: number format on %s: %v\n", cell, err)
	}
}

// withNumberFormat gives a cell a custom number format, keeping the rest of its
// style.
func withNumberFormat(f *excelize.File, sheet, cell, format string) error {
	id, err := f.GetCellStyle(sheet, cell)
	if err != nil {
		return err
	}
	style, err := f.GetStyle(id)
	if err != nil {
		return err
	}
	style.NumFmt = 0
	style.CustomNumFmt = &format
	id, err = f.NewStyle(style)
	if err != nil {
		return err
	}
	return f.SetCellStyle(sheet, cell, cell, id)
}

// writeMiddleMarkCell writes the single middle mark (X / (E) / (DH)),
// leaving the cell untouched when no mark applies so the template's own
// "vs" survives. The one place the middle-cell contract lives.
func writeMiddleMarkCell(f *excelize.File, sheetName string, courtStartCol, excelRow int, decision string, encho *state.EnchoMetadata) {
	if mid := MiddleMark(decision, encho); mid != "" {
		setCellStr(f, sheetName, colNum(courtStartCol+3), excelRow, mid)
	}
}

// bracketMatchResultView adapts a BracketMatch to the MatchResult shape the
// shared row writers consume (they read only the result fields).
func bracketMatchResultView(bm *state.BracketMatch) state.MatchResult {
	return state.MatchResult{
		SideA:  bm.SideA,
		SideB:  bm.SideB,
		Winner: bm.Winner,
		// The side and winner ids travel with the names they belong to
		// (bc-brid). Every mark this view feeds is attributed through
		// domain.AttributeWinnerSide, which prefers them, so dropping them
		// here would have made the export the one surface that still decided
		// a same-name bracket pairing by the aka-first name convention while
		// the app decided it by id. A row that carries no id (a bye, an
		// unresolved feeder, an unrepaired legacy row) passes "" and takes
		// the name path exactly as before.
		SideAID:  bm.SideAID,
		SideBID:  bm.SideBID,
		WinnerID: bm.WinnerID,
		// Status and DecisionBy travel too (bc-tmfn follow-up):
		// state.DefaultWinCreditSide (called from writeTeamSummaryCells)
		// needs both to decide whether a default-win ruling credits an
		// unfought numbered bout, and a bracket match's own Status/
		// DecisionBy are what that decision rests on -- omitting them left
		// Status at its zero value ("", never MatchStatusCompleted), so a
		// completed bracket kiken/fusenpai/fusensho could never be credited
		// through this projection.
		Status:     bm.Status,
		DecisionBy: bm.DecisionBy,
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

// setIVCellWithMark writes a team IV count, with a result mark ("2 Kiken")
// when one applies to that side. The count stays a NUMBER, the mark riding in
// the cell's number format (setMarkedCount), because the pool standings
// formulas add the cell and compare it with the other side's: as text it made
// IV/IL and Rank #VALUE! and turned W/L around.
func setIVCellWithMark(f *excelize.File, sheetName, col string, row, iv int, mark string) {
	setMarkedCount(f, sheetName, col, row, "", iv, mark)
}

// writeTeamSubMatchScores writes each sub-bout's ippon letters onto the team
// sub-match rows. Shiro's (SideB) ippons -> lVCol (startCol+1), Aka's
// (SideA) -> rVCol (startCol+5), middle "vs" -> tie marker / suffix.
// subResults are keyed by Position (1-based); the daihyosen placeholder
// (Position < 0) is skipped so its blank row stays clean.
// boutRows is the number of bout rows the block actually has
// (state.Competition.TeamBoutRows); a Position outside [1, boutRows] is skipped
// rather than writing into the next encounter's cells: corrupted state, or a
// kachinuki encounter that fielded reserves past its block, whose later bouts
// only the Kachinuki Detail sheet lists. Shared by the pool sheet and
// overlayTeamBracketScores.
// matchSideA/matchSideB are the ENCOUNTER's team names. A fixed-order bout
// settles at the match level, so its row records no per-fighter identity
// (bc-dnst) and its Winner names the TEAM: without these, SubBoutAttribution
// sees two empty side names, AttributeWinnerSide returns MatchSideNone, and the
// row silently loses both its default-win maru and its Fus./Kiken mark while
// this same sheet's IV/PW summary still counts the bout. state.SubBoutWinnerSide
// and the JS twin subWinnerSides both carry the same match-level arms; this is
// the third reader of that rule and it was the one without them.
// credit is state.DefaultWinCreditSide's answer for the whole match (the
// caller computes it once, shared with writeTeamSummaryCells so the summary
// row and this sheet's bout rows never disagree about which side a
// default-win ruling credits): a numbered bout with no result of its own is
// routed through state.SubBoutEffectiveResult, which shows the FIK
// default-win maru for that side without a per-row Kiken/Fus. mark -- the
// mark names the ONE competitor who withdrew and already rides the summary
// row's IV cell. domain.MatchSideNone reproduces the pre-credit behaviour
// exactly (an unfought bout's cells stay blank).
func writeTeamSubMatchScores(f *excelize.File, sheetName string, courtStartCol, subStartExcelRow int, subResults []state.SubMatchResult, boutRows int, matchSideA, matchSideB string, credit domain.MatchSide) {
	lVCol := colNum(courtStartCol + 1)
	rVCol := colNum(courtStartCol + 5)

	for _, sub := range subResults {
		if sub.Position <= 0 || sub.Position > boutRows {
			continue // skip daihyosen placeholder / unpositioned / out-of-range rows
		}
		// state.SubBoutEffectiveResult substitutes the FIK default-win maru
		// (Winner set to the credited side's team name, IpponsA/IpponsB the
		// maru) for a bout with no result of its own on a match a
		// default-win ruling closed; unchanged otherwise (a fought bout, or
		// credit == domain.MatchSideNone). Decision is deliberately left
		// untouched by that substitution, so this row draws no Kiken/Fus.
		// mark -- only the match-level summary row names the withdrawer.
		sub = state.SubBoutEffectiveResult(sub, credit, matchSideA, matchSideB)
		// Sub-match row for Position P is the P-th sub row (1-based Position).
		excelRow := subStartExcelRow + (sub.Position - 1)

		// ONE attribution per bout row, shared by the maru fallback and the
		// Kiken/Fus. mark because they name the same winner. The row's member
		// ids decide, a same-name pair no id can settle gets no mark (CLAUDE.md's
		// accepted no-mark class (i)), and a row naming no fighter falls back to
		// the encounter's team names: domain.SubBoutAttributionForTeamRow owns
		// both rules, shared with the Kachinuki Detail sheet.
		att := domain.SubBoutAttributionForTeamRow(sub.Attribution(), matchSideA, matchSideB)
		scoreA, scoreB := DefaultWinMaruAB(
			domain.IpponsScore(sub.IpponsA), domain.IpponsScore(sub.IpponsB),
			sub.Decision, sub.Encho, att)
		leftScore, rightScore := helper.WhiteLeft(scoreA, scoreB)
		lFoul, rFoul := helper.WhiteLeft(HansokuMark(sub.HansokuA), HansokuMark(sub.HansokuB))
		lMark, rMark := SideMarksLR(sub.Decision, sub.HanteiDecided(), att)
		// Outstanding-hansoku ▲ on the cell's outer edge, as in
		// writeScoreRowCells (FIK Table 2; scoreboard parity).
		if lScore := domain.JoinNonEmpty(lFoul, domain.JoinNonEmpty(leftScore, lMark)); lScore != "" {
			setCellStr(f, sheetName, lVCol, excelRow, lScore)
		}
		if rScore := domain.JoinNonEmpty(domain.JoinNonEmpty(rightScore, rMark), rFoul); rScore != "" {
			setCellStr(f, sheetName, rVCol, excelRow, rScore)
		}

		writeMiddleMarkCell(f, sheetName, courtStartCol, excelRow, sub.Decision, sub.Encho)
	}
}

// ---------- live standings ----------

// keepStandingsLive leaves the pool standings as the formulas the shared
// pipeline printed (W/L/T/PW/PL or the engi Flags, the team tables' W/L/T and
// IV/IL/IT/PW/PL, Rank and the Ranking names), counted over the scores and
// decision marks overlayPoolScores wrote, so the operator can check the sheet
// against the app (operator decision E4). Two things are added over them:
//
//   - every standings cell whose formula gives a number carries that number as
//     its cached result, so a reader that computes no formula shows the figure
//     Excel shows on opening. The number is what excelize's evaluator gives for
//     the formula, never the app's figure, so a pool decided by kiken, fusenpai
//     or hantei shows what the formulas count there (the accepted trade-off).
//     A name has no cached result: excelize can store one only for a number.
//   - in a pool where the sheet's Rank would differ from the app's, Rank and
//     the Ranking names are written as the app's values, with a note beside
//     the Ranking saying why (operator decision Q2 (b)). The formulas cannot
//     see a chusen, a tie-break or representative bout, or the app's order of
//     competitors level on every count. The knockout's entrant formulas read
//     the Ranking cells, so they follow the app's order there.
//
// Every formula is evaluated before anything is written, because excelize
// drops its calculation cache on every write.
func keepStandingsLive(f *excelize.File, pools []helper.Pool, standings map[string][]state.PlayerStanding, matchResults []state.MatchResult, poolsByCourt [][]int) error {
	if len(pools) == 0 {
		return nil
	}
	sheet := helper.SheetPoolMatches
	rows, err := f.GetRows(sheet)
	if err != nil {
		return fmt.Errorf("keepStandingsLive: get rows: %w", err)
	}
	areas := locatePoolStandings(rows, pools, poolsByCourt)

	evaluated := make([][]formulaCell, len(areas))
	for i, a := range areas {
		cells, err := evaluateFormulaCells(f, sheet, a.formulaRefs())
		if err != nil {
			return err
		}
		evaluated[i] = cells
	}

	var noteStyle int
	haveNoteStyle := false
	for i, a := range areas {
		app, ok := appRanks(a.pool, standings[a.pool.PoolName])
		appOrder := ok && !sameRanks(evaluated[i], a.rankRefs(), app)

		appOwned := make(map[string]bool)
		if appOrder {
			for _, ref := range a.rankRefs() {
				appOwned[ref] = true
			}
			for _, ref := range a.rankingRefs() {
				appOwned[ref] = true
			}
		}
		for _, c := range evaluated[i] {
			if appOwned[c.ref] {
				continue
			}
			if err := cacheFormulaResult(f, sheet, c); err != nil {
				return err
			}
		}
		if !appOrder {
			continue
		}
		if !haveNoteStyle {
			haveNoteStyle = true
			if noteStyle, err = f.NewStyle(&excelize.Style{
				Alignment: &excelize.Alignment{WrapText: true, Vertical: "top"},
				Font:      &excelize.Font{Italic: true, Size: 9},
			}); err != nil {
				return fmt.Errorf("keepStandingsLive: note style: %w", err)
			}
		}
		if err := writeAppRanking(f, sheet, a, standings[a.pool.PoolName], app, evaluated[i]); err != nil {
			return err
		}
		reasons := rankingDifferenceReasons(standings[a.pool.PoolName], poolMatchResults(a.pool, matchResults))
		if err := writeRankingNote(f, sheet, a, rankingNote(reasons), noteStyle); err != nil {
			return err
		}
	}
	return nil
}

// poolStandingsArea is where one pool's standings sit on the Pool Matches
// sheet. The N-th "Results" (or "Team Results") header and the N-th "Ranking"
// label in a court band belong to the N-th pool assigned to that band
// (helper.PoolsByCourt), so a pool is found by its ordinal, never by a name a
// formula has not resolved.
type poolStandingsArea struct {
	pool helper.Pool
	// startCol is the 1-based first column of the pool's court band.
	startCol int
	// tableRows are the 1-based rows of the results table(s) holding a
	// competitor: one per competitor, and a team pool's IV/IL/IT/PW/PL table
	// below its W/L/T one.
	tableRows []int
	// rankRows are the 1-based rows of each competitor's Rank cell, in
	// pool.Players order.
	rankRows []int
	// rankingRow is the 1-based row of the "Ranking" label, 0 when none.
	rankingRow int
}

// standingsColumns is how many columns of a court band the results table
// spans: the name, the counts and Rank (startCol .. startCol+6).
const standingsColumns = 7

func (a *poolStandingsArea) rankCol() string    { return colNum(a.startCol + 6) }
func (a *poolStandingsArea) rankingCol() string { return colNum(a.startCol + 6) }

func (a *poolStandingsArea) rankRefs() []string {
	refs := make([]string, 0, len(a.rankRows))
	for _, r := range a.rankRows {
		refs = append(refs, fmt.Sprintf("%s%d", a.rankCol(), r))
	}
	return refs
}

// nameRefs are the results table's name cells, in pool.Players order: the
// first column of each competitor's row in the W/L/T table.
func (a *poolStandingsArea) nameRefs() []string {
	refs := make([]string, 0, len(a.pool.Players))
	for _, r := range a.tableRows[:len(a.pool.Players)] {
		refs = append(refs, fmt.Sprintf("%s%d", colNum(a.startCol), r))
	}
	return refs
}

func (a *poolStandingsArea) rankingRefs() []string {
	if a.rankingRow == 0 {
		return nil
	}
	refs := make([]string, 0, len(a.pool.Players))
	for i := range a.pool.Players {
		refs = append(refs, fmt.Sprintf("%s%d", a.rankingCol(), a.rankingRow+1+i))
	}
	return refs
}

func (a *poolStandingsArea) formulaRefs() []string {
	var refs []string
	for _, r := range a.tableRows {
		for c := a.startCol; c < a.startCol+standingsColumns; c++ {
			refs = append(refs, fmt.Sprintf("%s%d", colNum(c), r))
		}
	}
	return append(refs, a.rankingRefs()...)
}

// locatePoolStandings finds each pool's standings area, in sheet order.
func locatePoolStandings(rows [][]string, pools []helper.Pool, poolsByCourt [][]int) []*poolStandingsArea {
	numCourts := len(poolsByCourt)
	byPool := make(map[int]*poolStandingsArea)
	var order []*poolStandingsArea
	areaOf := func(c, ordinal int) *poolStandingsArea {
		if ordinal >= len(poolsByCourt[c]) {
			return nil
		}
		pi := poolsByCourt[c][ordinal]
		a, ok := byPool[pi]
		if !ok {
			a = &poolStandingsArea{pool: pools[pi], startCol: 1 + c*helper.CourtsColumnsPerCourt}
			byPool[pi] = a
			order = append(order, a)
		}
		return a
	}
	resultsSeen := make([]int, numCourts)
	rankingSeen := make([]int, numCourts)
	for rowIdx, row := range rows {
		for c := 0; c < numCourts; c++ {
			start := c * helper.CourtsColumnsPerCourt // 0-based
			if start < len(row) && (row[start] == "Results" || row[start] == "Team Results") {
				a := areaOf(c, resultsSeen[c])
				resultsSeen[c]++
				if a != nil {
					header := rowIdx + 1
					n := len(a.pool.Players)
					for i := 0; i < n; i++ {
						a.rankRows = append(a.rankRows, header+1+i)
						a.tableRows = append(a.tableRows, header+1+i)
					}
					if row[start] == "Team Results" {
						// The IV/IL/IT/PW/PL table sits under the W/L/T one, after
						// a blank row and its own header.
						for i := 0; i < n; i++ {
							a.tableRows = append(a.tableRows, header+n+3+i)
						}
					}
				}
			}
			if label := start + 6; label < len(row) && row[label] == "Ranking" {
				a := areaOf(c, rankingSeen[c])
				rankingSeen[c]++
				if a != nil {
					a.rankingRow = rowIdx + 1
				}
			}
		}
	}
	return order
}

// formulaCell is one formula cell and what excelize's evaluator gives for it.
type formulaCell struct {
	ref     string
	formula string
	value   string
	// evaluated is false when the evaluator could not work the formula out.
	evaluated bool
}

func evaluateFormulaCells(f *excelize.File, sheet string, refs []string) ([]formulaCell, error) {
	cells := make([]formulaCell, 0, len(refs))
	for _, ref := range refs {
		formula, err := f.GetCellFormula(sheet, ref)
		if err != nil {
			return nil, fmt.Errorf("keepStandingsLive: formula %s: %w", ref, err)
		}
		if formula == "" {
			continue
		}
		value, err := f.CalcCellValue(sheet, ref, excelize.Options{RawCellValue: true})
		cells = append(cells, formulaCell{ref: ref, formula: formula, value: value, evaluated: err == nil})
	}
	return cells, nil
}

// cacheFormulaResult stores a number the formula gives as the cell's cached
// result and keeps the formula. excelize keeps a cell's value when a formula
// is set on it (and drops the formula when a value is set), so the value goes
// first.
func cacheFormulaResult(f *excelize.File, sheet string, c formulaCell) error {
	if !c.evaluated {
		return nil
	}
	if _, err := strconv.ParseFloat(c.value, 64); err != nil {
		return nil
	}
	if err := f.SetCellDefault(sheet, c.ref, c.value); err != nil {
		return fmt.Errorf("keepStandingsLive: cache %s: %w", c.ref, err)
	}
	if err := f.SetCellFormula(sheet, c.ref, c.formula); err != nil {
		return fmt.Errorf("keepStandingsLive: formula %s: %w", c.ref, err)
	}
	return nil
}

// appRanks is the app's rank of each competitor in pool.Players order; ok is
// false when a competitor has no standing, so there is no app order to write.
func appRanks(pool helper.Pool, poolStandings []state.PlayerStanding) ([]int, bool) {
	byID := standingMap(poolStandings)
	ranks := make([]int, len(pool.Players))
	for i, p := range pool.Players {
		ps, ok := byID[p.ID]
		if !ok {
			return nil, false
		}
		ranks[i] = ps.Rank
	}
	return ranks, len(ranks) > 0
}

// sameRanks reports whether the sheet's Rank formulas give the app's ranks.
// A Rank the evaluator cannot work out is not the app's.
func sameRanks(cells []formulaCell, rankRefs []string, app []int) bool {
	byRef := make(map[string]formulaCell, len(cells))
	for _, c := range cells {
		byRef[c.ref] = c
	}
	for i, ref := range rankRefs {
		c, ok := byRef[ref]
		if !ok || !c.evaluated {
			return false
		}
		v, err := strconv.ParseFloat(c.value, 64)
		if err != nil || int(v) != app[i] || v != float64(int(v)) {
			return false
		}
	}
	return true
}

// writeAppRanking writes the app's Rank of each competitor and the Ranking
// names in the app's order over the formulas. A Ranking name reads as the
// results table prints it (a numbered draw's "A1 Alice"), so each competitor's
// text is the one their results-table name cell gives; cells holds those
// evaluated cells.
func writeAppRanking(f *excelize.File, sheet string, a *poolStandingsArea, poolStandings []state.PlayerStanding, app []int, cells []formulaCell) error {
	for i, ref := range a.rankRefs() {
		if err := f.SetCellInt(sheet, ref, int64(app[i])); err != nil {
			return fmt.Errorf("keepStandingsLive: rank %s: %w", ref, err)
		}
	}
	printed := make(map[string]string, len(cells))
	for _, c := range cells {
		printed[c.ref] = c.value
	}
	printedName := make(map[string]string, len(a.pool.Players))
	for i, ref := range a.nameRefs() {
		if text, ok := printed[ref]; ok {
			printedName[a.pool.Players[i].ID] = text
		}
	}
	sorted := make([]state.PlayerStanding, len(poolStandings))
	copy(sorted, poolStandings)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Rank < sorted[j].Rank })
	for i, ref := range a.rankingRefs() {
		if i >= len(sorted) {
			break
		}
		name, ok := printedName[sorted[i].Player.ID]
		if !ok {
			name = sorted[i].Player.Name
		}
		if err := f.SetCellValue(sheet, ref, name); err != nil {
			return fmt.Errorf("keepStandingsLive: ranking %s: %w", ref, err)
		}
	}
	return nil
}

// poolMatchResults is the stored results of one pool: its regular matches and
// its tie-break and representative bouts.
func poolMatchResults(pool helper.Pool, matchResults []state.MatchResult) []state.MatchResult {
	prefix := pool.PoolName + "-"
	var mine []state.MatchResult
	for _, mr := range matchResults {
		if strings.HasPrefix(mr.ID, prefix) {
			mine = append(mine, mr)
		}
	}
	return mine
}

// rankingDifferenceReasons names what in a pool the app's order rests on that
// the formulas cannot see. It only words the note: whether the app's order is
// written is decided by comparing the two ranks (sameRanks).
func rankingDifferenceReasons(poolStandings []state.PlayerStanding, poolMatches []state.MatchResult) []string {
	var reasons []string
	if slices.ContainsFunc(poolStandings, func(ps state.PlayerStanding) bool { return ps.IsOverridden }) {
		reasons = append(reasons, "a chusen settled a tie")
	}
	var tieBreak, representative bool
	var decided []string
	for i := range poolMatches {
		m := &poolMatches[i]
		if m.Status != state.MatchStatusCompleted {
			continue
		}
		switch {
		case state.IsTiebreakerMatchID(m.ID):
			tieBreak = true
		case state.IsPoolDaihyosenMatchID(m.ID):
			representative = true
		}
		word := domain.DecisionWord(m.Decision)
		if word == "" && m.HanteiDecided() {
			word = "hantei"
		}
		if word != "" && !slices.Contains(decided, word) {
			decided = append(decided, word)
		}
	}
	if tieBreak {
		reasons = append(reasons, "a tie-break bout was fought")
	}
	if representative {
		reasons = append(reasons, "a representative bout was fought")
	}
	if n := len(decided); n > 0 {
		words := decided[n-1]
		if n > 1 {
			words = strings.Join(decided[:n-1], ", ") + " or " + words
		}
		reasons = append(reasons, "a match was decided by "+words)
	}
	seen := make(map[int]bool, len(poolStandings))
	for _, ps := range poolStandings {
		if seen[ps.Rank] {
			reasons = append(reasons, "competitors share a place")
			break
		}
		seen[ps.Rank] = true
	}
	return reasons
}

// rankingNote is the note written beside a pool's Ranking when Rank and
// Ranking show the app's order instead of the formulas' (operator decision
// Q2 (b): "write a note on why they differ").
func rankingNote(reasons []string) string {
	if len(reasons) == 0 {
		reasons = []string{"competitors level on every count are listed in the app's order"}
	}
	return "Rank and Ranking show the app's order, not the formulas': " + strings.Join(reasons, "; ") + "."
}

// writeRankingNote writes the note in the empty columns left of the Ranking,
// merged down the Ranking's rows so it wraps there.
func writeRankingNote(f *excelize.File, sheet string, a *poolStandingsArea, note string, style int) error {
	if a.rankingRow == 0 {
		return nil
	}
	top := fmt.Sprintf("%s%d", colNum(a.startCol), a.rankingRow)
	bottom := fmt.Sprintf("%s%d", colNum(a.startCol+4), a.rankingRow+len(a.pool.Players))
	if err := f.MergeCell(sheet, top, bottom); err != nil {
		return fmt.Errorf("keepStandingsLive: note %s: %w", top, err)
	}
	if err := f.SetCellValue(sheet, top, note); err != nil {
		return fmt.Errorf("keepStandingsLive: note %s: %w", top, err)
	}
	if err := f.SetCellStyle(sheet, top, bottom, style); err != nil {
		return fmt.Errorf("keepStandingsLive: note style %s: %w", top, err)
	}
	return nil
}

// ---------- bracket score overlay ----------

// overlayBracketScores writes literal score values into the Elimination Matches
// sheet by scanning for "Round N - Match N" header cells and, when present, a
// "3rd Place" header cell. For each completed match found, the score cells in
// the row two rows below the header are overwritten with literal values.
// thirdPlaceMatch is the bracket's bronze match (nil when this competition
// does not require a single 3rd place -- see
// state.Competition.RequiresSingleThirdPlace, bc-3rdp).
func overlayBracketScores(f *excelize.File, bracketByNum map[int]state.BracketMatch, boutRows int, engi bool, thirdPlaceMatch *state.BracketMatch) error {
	if boutRows != 0 {
		// Engi is individual-only; the team overlay renders ippon strings and
		// would silently drop flag scores. Fail loudly if the invariant breaks.
		if engi {
			return fmt.Errorf("overlayBracketScores: engi is individual-only (boutRows=%d)", boutRows)
		}
		return overlayTeamBracketScores(f, bracketByNum, boutRows, thirdPlaceMatch)
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
			//   header+1 = White/Red label row
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
				if !writeThirdPlaceEntrants(f, sheetName, bm, courtStartCol, excelRow) {
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
				// "MHt Ht". Render through domain.IpponsScore instead, which filters
				// non-scoring entries (the mark, the bye placeholder) exactly
				// as the pool path (overlayPoolScores) already does — the
				// mark then rides ONLY through the appended SideMarksLR
				// suffix, matching the pool cell's "M Ht".
				scoreA, scoreB = DefaultWinMaruAB(
					domain.IpponsScore(mrView.IpponsA), domain.IpponsScore(mrView.IpponsB),
					bm.Decision, bm.Encho, mrView.Attribution())
			}

			writeScoreRowCells(f, sheetName, courtStartCol, excelRow, scoreA, scoreB, mrView, engi)

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
//	sub-match row for Position p (1..boutRows) = H + 2 + p   (ippon letters)
//	IV/PW summary ("Victories / Points") row   = H + 5 + boutRows
//	"1." winner-marker row                      = H + 8 + boutRows
//
// IV/PW cell columns on the summary row mirror the pool summary: left IV=startCol+1,
// left PW=startCol+2, right IV=startCol+5, right PW=startCol+4. The summary IV/PW
// cells and per-player W/L/T standings are formula-driven (they tally the sub-match
// rows) and collapse after a store round-trip, so we overwrite them with literals.
func overlayTeamBracketScores(f *excelize.File, bracketByNum map[int]state.BracketMatch, boutRows int, thirdPlaceMatch *state.BracketMatch) error {
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
				if !writeThirdPlaceEntrants(f, sheetName, bm, courtStartCol, headerExcelRow+2) {
					continue
				}
			}

			// Sub-match ippon letters: Position p sits at H+2+p, i.e. the sub
			// rows start at H+3. Same writer as the pool sheet. credit is
			// computed straight off bm (not the bracketMatchResultView below,
			// which exists for writeTeamSummaryCells' state.MatchResult
			// param) but is the SAME answer: both read Status/Decision/
			// DecisionBy/Attribution off this one bm.
			credit := state.DefaultWinCreditSide(bm.Status, bm.Decision, bm.DecisionBy, bm.Attribution())
			writeTeamSubMatchScores(f, sheetName, courtStartCol, headerExcelRow+3, bm.SubResults, boutRows, bm.SideA, bm.SideB, credit)

			// IV/PW summary row = H + 5 + boutRows. Route through the shared
			// pool-sheet writer so the IV-mark contract (and the forfeit
			// fallback when no summary line exists) lives in one place.
			summaryExcelRow := headerExcelRow + 5 + boutRows
			writeTeamSummaryCells(f, sheetName, courtStartCol, summaryExcelRow, bracketMatchResultView(&bm))

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
// literal-name snapshot semantics overlayKnockoutBracketNames applies to every
// other match. An EMPTY side is skipped so the cell keeps the self-populating
// CONCATENATE formula written by PrintThirdPlaceBlock (pinned by the
// no-scoring-yet formulas test in builder_naginata_test.go). The return value reports
// whether the match is completed so the caller can skip the score overlay for
// an unplayed match.
func writeThirdPlaceEntrants(f *excelize.File, sheetName string, bm state.BracketMatch, courtStartCol, entrantRow int) bool {
	leftNameCol := colNum(courtStartCol)
	rightNameCol := colNum(courtStartCol + 6)
	leftName, rightName := helper.WhiteLeft(bm.SideA, bm.SideB)
	if leftName != "" {
		setCellStr(f, sheetName, leftNameCol, entrantRow, leftName)
	}
	if rightName != "" {
		setCellStr(f, sheetName, rightNameCol, entrantRow, rightName)
	}
	return bm.Status == state.MatchStatusCompleted
}

// overlayKnockoutBracketNames overwrites a knockout-only competition's
// elimination entrant name cells with literal names from the stored bracket,
// named as engine.PrintedBracket names them: a competitor as stored, a side an
// earlier match decides as "M n" (what the cell's own formula prints before a
// result is entered), and a bye's empty side as a blank cell.
//
// Name cells sit at the court's start column (left, SideB) and start+6 (right,
// SideA) on the entrant row (header + 2), placed by helper.WhiteLeft. Team brackets repeat the entrant name formulas on the
// summary row (header + 4 + boutRows, just above the "Victories / Points" row), so
// those are overwritten too.
func overlayKnockoutBracketNames(f *excelize.File, bracketByNum map[int]state.BracketMatch, printed map[string]engine.PrintedBracketMatch, boutRows int) error {
	sheetName := helper.SheetEliminationMatches
	rows, err := f.GetRows(sheetName)
	if err != nil {
		return fmt.Errorf("overlayKnockoutBracketNames: get rows: %w", err)
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

			sides := printed[bm.ID]
			leftName, rightName := helper.WhiteLeft(sides.SideA, sides.SideB)
			leftCol := colNum(headerCol + 1)  // court start column
			rightCol := colNum(headerCol + 7) // start + 6 (endColName)

			entrantRow := rowIdx + 3 // header (rowIdx+1) + 2
			setCellStr(f, sheetName, leftCol, entrantRow, leftName)
			setCellStr(f, sheetName, rightCol, entrantRow, rightName)

			if boutRows > 0 {
				// The repeated entrant-name formulas sit at header + 4 + boutRows
				// (rowIdx+5+boutRows), one row ABOVE the "Victories / Points" text
				// row. printSingleEliminationMatch: header + White/Red + entrant (H+2),
				// boutRows sub-match rows (H+3..H+2+boutRows), then matchRow += 2 lands
				// the summary name row at H+4+boutRows.
				summaryRow := rowIdx + 5 + boutRows
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

// setIntCellDirect writes an int to a cell addressed by an explicit column
// letter (as returned by colNum) and 1-based row. Used by the team overlays.
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

// standingMap keys standings by participant ID ONLY (operator ruling
// bc-pnum: a state.PlayerStanding carries an id field, so it is resolved by
// id only) so two same-name competitors in one pool don't collapse onto a
// single entry. A row is keyed by its participant id; a row without one
// resolves to nothing, so an id-less standing is never inserted, matching
// engine.AttachPoolMatches' own id-only resolution. Look up with player.ID.
func standingMap(standings []state.PlayerStanding) map[string]state.PlayerStanding {
	m := make(map[string]state.PlayerStanding, len(standings))
	for _, ps := range standings {
		if ps.Player.ID == "" {
			continue
		}
		m[ps.Player.ID] = ps
	}
	return m
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
	if _, err := fmt.Sscanf(s, helper.EliminationMatchTitleFormat, &round, &match); err != nil {
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
