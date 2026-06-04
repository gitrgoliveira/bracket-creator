package pdf

import "strings"

// matchKind controls how a sheetSpec matches the actual sheet names a workbook
// produces. The current generator splits some logical sheets into suffixed
// physical sheets ("Names to Print A/B", "Tree 1/2"), so those must match by
// prefix; unambiguous singletons match exactly.
type matchKind int

const (
	matchExact  matchKind = iota // sheet name equals spec
	matchPrefix                  // sheet name begins with spec (e.g. "Tree 1")
)

// sheetSpec selects sheets within a workbook for a group.
type sheetSpec struct {
	name string
	kind matchKind
}

func exact(name string) sheetSpec  { return sheetSpec{name: name, kind: matchExact} }
func prefix(name string) sheetSpec { return sheetSpec{name: name, kind: matchPrefix} }

func (s sheetSpec) matches(sheet string) bool {
	switch s.kind {
	case matchPrefix:
		return strings.HasPrefix(sheet, s.name)
	default:
		return sheet == s.name
	}
}

// Group describes one output PDF: which sheets to pull from each source
// workbook, whether to prepend a per-tournament title page, which source files
// to skip, and whether to stamp page numbers on the merged result.
type Group struct {
	// Type is the CLI/web selector (e.g. "registration", "names").
	Type string
	// Output is the default output filename.
	Output string
	// Description is a human-readable label for progress output.
	Description string
	// Sheets are the sheet selectors, applied in order, for each workbook.
	Sheets []sheetSpec
	// InsertTitle prepends a per-tournament title page before each file's pages.
	InsertTitle bool
	// A3Landscape renders the title page in A3 landscape (for Names to Print).
	A3Landscape bool
	// SkipTeamWorkbooks excludes team-registration workbooks (no individual tags).
	SkipTeamWorkbooks bool
	// PageNumbers stamps "N / M" footers on the final merged PDF.
	PageNumbers bool
}

// resolveSheets returns the source sheet names (in workbook order) that this
// group wants, given the actual sheet names present in a workbook's PDF.
func (g Group) resolveSheets(present []string) []string {
	var out []string
	for _, spec := range g.Sheets {
		for _, sheet := range present {
			if spec.matches(sheet) {
				out = append(out, sheet)
			}
		}
	}
	return out
}

// Groups is the canonical set of output PDFs, mirroring the PDF_GROUPS config
// in square_prep/lc2026/xlsx_to_pdf.py. Sheet names use the Sheet* constants'
// values from internal/helper/constants.go. Names/Tags/Tree match by prefix
// because the generator splits them into suffixed physical sheets.
var Groups = []Group{
	{
		Type:        "registration",
		Output:      "print_registration.pdf",
		Description: "Registration (data sheets)",
		Sheets:      []sheetSpec{exact("data")},
	},
	{
		Type:        "names",
		Output:      "print_names_to_print.pdf",
		Description: "Names to Print",
		Sheets:      []sheetSpec{prefix("Names to Print")},
		InsertTitle: true,
		A3Landscape: true,
	},
	{
		Type:              "tags",
		Output:            "print_tags.pdf",
		Description:       "Tags",
		Sheets:            []sheetSpec{prefix("Tags")},
		InsertTitle:       true,
		SkipTeamWorkbooks: true,
	},
	{
		Type:        "pools-trees",
		Output:      "print_pools_and_trees.pdf",
		Description: "Pool Draw + Trees (participant booklet)",
		Sheets:      []sheetSpec{exact("Pool Draw"), prefix("Tree")},
		PageNumbers: true,
	},
	{
		Type:        "full-bracket",
		Output:      "print_full_bracket.pdf",
		Description: "Full bracket (pools, matches, trees)",
		Sheets: []sheetSpec{
			exact("Pool Draw"),
			exact("Pool Matches"),
			exact("Elimination Matches"),
			prefix("Tree"),
		},
		PageNumbers: true,
	},
}

// GroupByType returns the group with the given Type selector, or false.
func GroupByType(t string) (Group, bool) {
	for _, g := range Groups {
		if g.Type == t {
			return g, true
		}
	}
	return Group{}, false
}
