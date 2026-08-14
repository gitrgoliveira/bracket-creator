package helper

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	excelize "github.com/xuri/excelize/v2"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// This file captures the Excel/CLI knockout PAGE behaviour into
// testdata/excel_pages.json (bc-draw). It is the sibling of
// draw_shapes_golden_test.go, which covers the ENGINE draw pipeline and stops
// at the tree. Nothing pinned the other path: what a tree PAGE actually prints
// had no characterization at all, so the two paths could (and did) diverge
// unobserved. They now share helper.BuildKnockoutDraw, so a shape change must
// appear in BOTH files.
//
// It used to pin DEFECTS on purpose, which is why the golden file's `_comment`
// block still names them: reading the two revisions side by side is how the
// rewrite is reviewed. What it records now is the shipped behaviour. Do NOT
// hand-edit a value.
//
// Everything recorded is READ BACK OUT OF A RENDERED WORKBOOK, not recomputed
// from the tree: the generator drives the real helper.RenderKnockoutPages (the
// single funnel behind cmd/create-pools.go, cmd/create-playoffs.go,
// internal/export/builder.go and internal/engine/export.go) into an in-memory
// excelize file and then scans each "Tree N" sheet. That is deliberate. A
// re-implementation of the render loop would track the refactor instead of
// measuring it, and page geometry is exactly what the refactor moves.
//
// Regeneration (same convention as draw_shapes.json):
//
//	UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestExcelPagesGolden
//
// Regeneration is deterministic: no shuffling, no map iteration and no clock
// reaches the output, so two consecutive runs produce a byte-identical file.

// The sweep required by bc-draw. It is a superset of the engine
// golden's (drawSweepPoolCounts starts at 2): pool count 1 is added because the
// degenerate cases are precisely where a tree PAGE ROOT can be a leaf, which is
// the one shape whose placement a page-scoped pass cannot reach.
var (
	excelPagesSweepPoolWinners = []int{1, 2, 3, 4}
	excelPagesSweepPoolCounts  = []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}
	excelPagesSweepCourts      = []int{1, 2, 4}
)

// excelPagesGolden is the whole golden file. Field order here is the field
// order in the JSON (encoding/json emits struct fields in declaration order and
// sorts map keys), which is what keeps the file stable and diff-friendly.
type excelPagesGolden struct {
	Comment []string                 `json:"_comment"`
	Sweep   excelPagesSweep          `json:"sweep"`
	Cases   map[string]excelPageCase `json:"cases"`
}

type excelPagesSweep struct {
	PoolWinners []int  `json:"poolWinners"`
	PoolCounts  []int  `json:"poolCounts"`
	Courts      []int  `json:"courts"`
	KeyFormat   string `json:"keyFormat"`
}

// excelPageCase is one (poolCount, poolWinners, courts) combination, rendered.
type excelPageCase struct {
	// Error is set (and every other field left zero) when the combination
	// cannot be rendered at all. Recorded rather than skipped so the sweep's
	// coverage is visible in the file itself.
	Error string `json:"error,omitempty"`

	NumEntrants int `json:"numEntrants"`

	// DrawCourts is the shiaijo count the draw actually used, i.e. the
	// requested count clamped by EffectiveDrawCourts. Every page count below is
	// a multiple of it.
	DrawCourts int `json:"drawCourts"`

	// PagesRequested is RenderKnockoutPages' returned page count
	// (DrawCourts x {1,2,4}). PagesRendered is how many "Tree N" sheets the
	// workbook actually carries. R8 makes them equal in every case; a case
	// where they diverge is a bug, not a recorded quirk.
	PagesRequested int `json:"pagesRequested"`
	PagesRendered  int `json:"pagesRendered"`

	Pages []excelPageView `json:"pages"`

	// TreeLeaves is the whole draw's leaf order, left to right: the tree
	// BuildEliminationMatchRounds and FillInMatches traverse, and therefore the
	// one the Elimination Matches sheet describes. Rendering no longer mutates
	// it (placement happens in BuildKnockoutDraw), so this is also the order the
	// pages were cut from - it is the field that shows a cross-page placement
	// move, which a per-page leaf list alone can hide.
	TreeLeaves []string `json:"treeLeaves"`
}

// excelPageView is one rendered "Tree N" sheet.
type excelPageView struct {
	Page int `json:"page"`
	// CourtLabel is parsed back out of the page's real title formula
	// (SetTreeSheetTitle writes IF(data!$B$1="","Shiaijo X",...)), not
	// recomputed from SubtreeCourtIndex.
	CourtLabel string `json:"courtLabel"`
	// Leaves is every entrant label printed on the page, in RENDER ORDER.
	// PrintLeafNodes lays a left subtree above its right sibling, so scanning
	// the sheet top to bottom reproduces the tree's left-to-right leaf order,
	// and is also literally the order an operator reads the page in.
	Leaves []string `json:"leaves"`
}

// excelPageLeafLabel matches the entrant placeholders the draw emits
// ("Pool A-1st"). Scanning for them is what separates a leaf label from the
// junction match NUMBERS FillInMatches writes into the same odd columns.
var excelPageLeafLabel = regexp.MustCompile(`^Pool [A-Z]+-\d+(?:st|nd|rd|th)$`)

// excelPageTitleLabel pulls the shiaijo letter out of a rendered title formula.
var excelPageTitleLabel = regexp.MustCompile(`"Shiaijo ([A-Z])"`)

// excelPageSheetName matches the pages RenderTreePages creates.
var excelPageSheetName = regexp.MustCompile(`^Tree (\d+)$`)

// newRenderTargetFile builds the minimum workbook RenderKnockoutPages needs: a
// data sheet for AddPoolDataToSheet's roster and the styled-page source sheet
// RenderTreePages copies each tree page from. internal/excel owns the real
// template but imports this package, so a helper-internal test cannot use it;
// the sheets' STYLING is irrelevant here because only cell values are read back.
func newRenderTargetFile(t *testing.T) *excelize.File {
	t.Helper()
	f := excelize.NewFile()
	require.NoError(t, f.SetSheetName("Sheet1", SheetData))
	_, err := f.NewSheet(SheetTree)
	require.NoError(t, err)
	return f
}

// treePageSheets returns the rendered page sheets in page order.
func treePageSheets(f *excelize.File) []string {
	type page struct {
		num  int
		name string
	}
	pages := []page{}
	for _, name := range f.GetSheetList() {
		m := excelPageSheetName.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		num, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		pages = append(pages, page{num: num, name: name})
	}
	slices.SortFunc(pages, func(a, b page) int { return a.num - b.num })
	names := make([]string, len(pages))
	for i, p := range pages {
		names[i] = p.name
	}
	return names
}

// readTreePageLeaves scans a rendered tree page top to bottom, left to right and
// returns every entrant label on it. PrintLeafNodes gives each leaf its own row,
// so row order IS the bracket's top-to-bottom order.
func readTreePageLeaves(t *testing.T, f *excelize.File, sheet string) []string {
	t.Helper()
	rows, err := f.GetRows(sheet)
	require.NoError(t, err)
	leaves := []string{}
	for _, row := range rows {
		for _, cell := range row {
			if excelPageLeafLabel.MatchString(cell) {
				leaves = append(leaves, cell)
			}
		}
	}
	return leaves
}

// readTreePageCourtLabel returns the shiaijo letter the page is titled with.
func readTreePageCourtLabel(t *testing.T, f *excelize.File, sheet string) string {
	t.Helper()
	formula, err := f.GetCellFormula(sheet, "A1")
	require.NoError(t, err)
	m := excelPageTitleLabel.FindStringSubmatch(formula)
	if m == nil {
		return ""
	}
	return m[1]
}

// buildExcelPageCase renders one combination through the real production funnel
// and reads the result back. Everything it records comes from exported helper
// functions driving an actual workbook, so the golden tracks the shipped
// renderer rather than a model of it.
//
// Pools come from the same synthetic roster the engine golden uses
// (drawGoldenRoster / drawGoldenPoolSize) so the two files describe the same
// competitions and can be read side by side.
func buildExcelPageCase(t *testing.T, numPools, poolWinners, courts int) excelPageCase {
	t.Helper()

	pools, err := CreatePools(drawGoldenRoster(numPools), drawGoldenPoolSize, true)
	if err != nil {
		return excelPageCase{Error: "CreatePools: " + err.Error()}
	}
	if len(pools) != numPools {
		return excelPageCase{Error: fmt.Sprintf("CreatePools produced %d pools, want %d", len(pools), numPools)}
	}

	f := newRenderTargetFile(t)
	defer func() { _ = f.Close() }()

	poolCoords, playerCoords := AddPoolDataToSheet(f, pools, false, "")

	draw := BuildKnockoutDraw(pools, poolWinners, courts)
	if draw == nil {
		return excelPageCase{Error: "BuildKnockoutDraw returned no draw"}
	}

	// The live Excel funnel, verbatim: every one of the four workbook
	// generators calls exactly this, with matchWinners nil here so the leaf
	// cells hold literal labels instead of CONCATENATE formulas (the draw is
	// independent of the pool-match cross-references).
	_, numPages, err := RenderKnockoutPages(f, draw, CourtLabels(draw.NumCourts()), false, pools, poolCoords, playerCoords, nil)
	if err != nil {
		return excelPageCase{Error: "RenderKnockoutPages: " + err.Error()}
	}

	c := excelPageCase{
		NumEntrants:    numPools * poolWinners,
		DrawCourts:     draw.NumCourts(),
		PagesRequested: numPages,
		Pages:          []excelPageView{},
		TreeLeaves:     TreeLeafLabels(draw.Root),
	}
	for i, sheet := range treePageSheets(f) {
		c.Pages = append(c.Pages, excelPageView{
			Page:       i + 1,
			CourtLabel: readTreePageCourtLabel(t, f, sheet),
			Leaves:     readTreePageLeaves(t, f, sheet),
		})
	}
	c.PagesRendered = len(c.Pages)

	return c
}

func buildExcelPagesGolden(t *testing.T) excelPagesGolden {
	t.Helper()
	g := excelPagesGolden{
		Comment: []string{
			"bc-draw characterization golden for the RENDERED knockout pages.",
			"Generated by internal/helper/excel_pages_golden_test.go; regenerate:",
			"",
			"    UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestExcelPagesGolden",
			"",
			"It is the Excel/CLI half of the draw's diff instrument;",
			"testdata/draw_shapes.json is the engine half. Both must show the same",
			"change for the same reason, because both paths now build the draw with",
			"the SAME helper.BuildKnockoutDraw. Do not hand-edit a value.",
			"",
			"Each case renders one (pool count, qualifiers per pool, shiaijo count)",
			"combination through the real funnel - BuildKnockoutDraw ->",
			"RenderKnockoutPages (KnockoutPagesPerCourt -> SubdivideRegions ->",
			"RenderTreePages -> PrintLeafNodes -> FillInMatches) - and READS THE",
			"WORKBOOK BACK: every leaf recorded here was scanned out of a rendered",
			"'Tree N' sheet and every court label was parsed out of that sheet's",
			"real title formula. Pools come from helper.CreatePools over the same",
			"synthetic roster the engine golden uses.",
			"",
			"IT USED TO PIN THREE DEFECTS, all now closed:",
			"",
			"1. pagesRequested vs pagesRendered - the page count was a power of two",
			"   unrelated to the tree, and the splitter, out of levels, appended the",
			"   WHOLE TREE as a trailing page that reprinted the entire bracket",
			"   under another shiaijo's title. The count is now shiaijo x {1,2,4}",
			"   and the two always agree.",
			"",
			"2. courtLabel vs leaves - a page titled 'Shiaijo X' printed whatever",
			"   the positional split handed it, so the title routinely named one",
			"   shiaijo while the bracket showed another's competitors. A page is",
			"   now a genuine subtree of exactly one court's region (R3/R8): it",
			"   holds that court's home winners plus the runners-up that crossed in",
			"   from its partner court, and nothing else.",
			"",
			"3. Degenerate pages - a page ROOT could be a single leaf when the",
			"   requested page count exceeded the tree's depth, printing one name",
			"   and no bracket while the match that name was due to play appeared on",
			"   no page. Pages are cut from regions, so a page is only a lone leaf",
			"   when that shiaijo genuinely has one qualifier (the reference draw's",
			"   one-pool court does exactly that).",
			"",
			"Cases that cannot be rendered record `error` and nothing else; none do,",
			"and a case gaining an error is itself a reportable change.",
		},
		Sweep: excelPagesSweep{
			PoolWinners: excelPagesSweepPoolWinners,
			PoolCounts:  excelPagesSweepPoolCounts,
			Courts:      excelPagesSweepCourts,
			KeyFormat:   "P<poolCount, 2 digits>-W<qualifiers per pool>-C<courts>",
		},
		Cases: map[string]excelPageCase{},
	}
	for _, numPools := range excelPagesSweepPoolCounts {
		for _, poolWinners := range excelPagesSweepPoolWinners {
			for _, courts := range excelPagesSweepCourts {
				g.Cases[drawShapeKey(numPools, poolWinners, courts)] = buildExcelPageCase(t, numPools, poolWinners, courts)
			}
		}
	}
	return g
}

func excelPagesGoldenPath() string {
	return filepath.Join("testdata", "excel_pages.json")
}

func encodeExcelPagesGolden(t *testing.T, g excelPagesGolden) []byte {
	t.Helper()
	// MarshalIndent sorts map keys and emits struct fields in declaration
	// order, so the encoding is stable run to run and one moved leaf shows up
	// as a one-line diff.
	encoded, err := json.MarshalIndent(g, "", "  ")
	require.NoError(t, err)
	return append(encoded, '\n')
}

// TestExcelPagesGolden is the characterization gate for the rendered knockout
// pages. It fails naming the exact cases whose pages moved.
func TestExcelPagesGolden(t *testing.T) {
	got := buildExcelPagesGolden(t)
	encoded := encodeExcelPagesGolden(t, got)
	path := excelPagesGoldenPath()

	if os.Getenv("UPDATE_GOLDEN") != "" {
		require.NoError(t, os.MkdirAll("testdata", 0o750))
		require.NoError(t, os.WriteFile(path, encoded, 0o600))
		t.Logf("regenerated %s with %d cases", path, len(got.Cases))
		return
	}

	raw, err := os.ReadFile(path) // #nosec G304 -- fixed testdata path
	require.NoError(t, err, "golden file missing; regenerate with: UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestExcelPagesGolden")
	if string(raw) == string(encoded) {
		return
	}

	var want excelPagesGolden
	require.NoError(t, json.Unmarshal(raw, &want), "golden file is not valid JSON")

	keys := map[string]bool{}
	for k := range want.Cases {
		keys[k] = true
	}
	for k := range got.Cases {
		keys[k] = true
	}
	ordered := make([]string, 0, len(keys))
	for k := range keys {
		ordered = append(ordered, k)
	}
	slices.Sort(ordered)

	var changed []string
	for _, k := range ordered {
		w, inWant := want.Cases[k]
		g, inGot := got.Cases[k]
		if inWant && inGot && assert.ObjectsAreEqual(w, g) {
			continue
		}
		changed = append(changed, k)
		// Print a readable field-level diff for the first few, then stop: a
		// full-struct dump of every case is unreadable.
		if len(changed) <= 5 {
			switch {
			case !inWant:
				t.Errorf("case %s is NEW (not in the golden file)", k)
			case !inGot:
				t.Errorf("case %s DISAPPEARED (present in the golden file, no longer generated)", k)
			default:
				assert.Equal(t, w, g, "rendered pages changed for case %s", k)
			}
		}
	}

	if len(changed) == 0 {
		// Byte difference with no case difference: formatting or the header
		// block moved. Still a change the golden must record.
		t.Errorf("golden %s differs byte-for-byte but every case matches (header/formatting change). Regenerate with: UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestExcelPagesGolden", path)
		return
	}

	t.Errorf("THE RENDERED PAGES CHANGED: %d of %d cases differ from %s: %v\n"+
		"If this is intentional, regenerate with: UPDATE_GOLDEN=1 go test ./internal/helper/ -run TestExcelPagesGolden\n"+
		"and review the resulting diff - it IS the behaviour change.",
		len(changed), len(ordered), path, changed)
}

// TestExcelPagesGoldenIsDeterministic proves the generator has no shuffle, map
// iteration or clock in it: two renders in the same process must encode
// identically. (The regeneration-twice check is a manual step; this makes the
// property a permanent gate.)
func TestExcelPagesGoldenIsDeterministic(t *testing.T) {
	first := encodeExcelPagesGolden(t, buildExcelPagesGolden(t))
	second := encodeExcelPagesGolden(t, buildExcelPagesGolden(t))
	assert.Equal(t, string(first), string(second), "rendered page capture is not deterministic")
}

// TestExcelPagesGoldenReadbackIsComplete validates the INSTRUMENT itself: the
// cell scan must find every entrant the tree holds and nothing else. Without
// this, a read-back that silently missed a leaf (or picked up a match number as
// if it were one) would make the golden agree with itself while measuring the
// wrong thing, and the whole zero-diff proof would be worthless.
func TestExcelPagesGoldenReadbackIsComplete(t *testing.T) {
	for _, numPools := range excelPagesSweepPoolCounts {
		for _, poolWinners := range excelPagesSweepPoolWinners {
			for _, courts := range excelPagesSweepCourts {
				name := fmt.Sprintf("%d_pools_%d_winners_%d_courts", numPools, poolWinners, courts)
				t.Run(name, func(t *testing.T) {
					c := buildExcelPageCase(t, numPools, poolWinners, courts)
					require.Empty(t, c.Error)

					onPages := []string{}
					for _, p := range c.Pages {
						onPages = append(onPages, p.Leaves...)
					}
					slices.Sort(onPages)
					onPages = slices.Compact(onPages)

					inTree := slices.Clone(c.TreeLeaves)
					slices.Sort(inTree)
					inTree = slices.Compact(inTree)

					assert.Equal(t, inTree, onPages,
						"the set of labels scanned off the rendered pages must equal the tree's leaf set")
					assert.Len(t, inTree, c.NumEntrants,
						"every entrant must be distinct and present")
					assert.NotEmpty(t, c.Pages)
					for _, p := range c.Pages {
						assert.NotEmpty(t, p.CourtLabel, "page %d must carry a parseable shiaijo title", p.Page)
					}
				})
			}
		}
	}
}

// TestExcelPageLeafLabelPattern pins the scanner's discrimination directly: it
// must accept the entrant placeholders the draw emits and reject the
// junction match numbers FillInMatches writes into the same columns.
func TestExcelPageLeafLabelPattern(t *testing.T) {
	for _, v := range []string{"Pool A-1st", "Pool B-2nd", "Pool C-3rd", "Pool D-4th", "Pool AA-1st"} {
		assert.Truef(t, excelPageLeafLabel.MatchString(v), "%q is an entrant label", v)
	}
	for _, v := range []string{"", "1", "12", "Pool A", "data!B3", strings.Repeat("x", 8)} {
		assert.Falsef(t, excelPageLeafLabel.MatchString(v), "%q is not an entrant label", v)
	}
}
