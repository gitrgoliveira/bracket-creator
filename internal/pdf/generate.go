package pdf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// SourceWorkbook is one input XLSX with the metadata the pipeline needs.
type SourceWorkbook struct {
	// Path is the absolute path to the .xlsx file.
	Path string
	// Title is the human-readable tournament name used on title pages. When
	// empty, the filename stem is used.
	Title string
	// IsTeam marks a team-registration workbook; excluded from the Tags group.
	IsTeam bool
}

func (s SourceWorkbook) titleOrStem() string {
	if s.Title != "" {
		return s.Title
	}
	return stemWithoutExt(filepath.Base(s.Path))
}

// Generator drives the XLSX→PDF pipeline. Construct with NewGenerator.
type Generator struct {
	conv *Converter
}

// NewGenerator locates LibreOffice and returns a Generator, or
// ErrSofficeNotFound (wrapped) when soffice is unavailable.
func NewGenerator() (*Generator, error) {
	conv, err := NewConverter()
	if err != nil {
		return nil, err
	}
	return &Generator{conv: conv}, nil
}

// converted holds a workbook's full PDF and its sheet→page ranges, computed
// once and reused across every group.
type converted struct {
	src    SourceWorkbook
	pdf    string
	ranges []SheetRange
}

// sheetNamesPresent returns the sheet names in document order.
func (c converted) sheetNamesPresent() []string {
	out := make([]string, len(c.ranges))
	for i, r := range c.ranges {
		out[i] = r.Sheet
	}
	return out
}

// rangeFor returns the page range for a named sheet.
func (c converted) rangeFor(sheet string) (SheetRange, bool) {
	for _, r := range c.ranges {
		if r.Sheet == sheet {
			return r, true
		}
	}
	return SheetRange{}, false
}

// GenerateAll produces every group's PDF from the given workbooks into outDir,
// returning a map of group type → output path. Each workbook is converted to a
// full PDF exactly once and reused across groups. A group that yields no pages
// (e.g. tags when only team workbooks are present) is skipped and omitted from
// the result map. Output files are written atomically (temp file → rename).
func (g *Generator) GenerateAll(ctx context.Context, sources []SourceWorkbook, outDir string) (map[string]string, error) {
	return g.generate(ctx, Groups, sources, outDir)
}

// GenerateGroups produces only the named group types. Unknown types error.
func (g *Generator) GenerateGroups(ctx context.Context, types []string, sources []SourceWorkbook, outDir string) (map[string]string, error) {
	groups := make([]Group, 0, len(types))
	for _, t := range types {
		grp, ok := GroupByType(t)
		if !ok {
			return nil, fmt.Errorf("unknown PDF type %q", t)
		}
		groups = append(groups, grp)
	}
	return g.generate(ctx, groups, sources, outDir)
}

func (g *Generator) generate(ctx context.Context, groups []Group, sources []SourceWorkbook, outDir string) (map[string]string, error) {
	if len(sources) == 0 {
		return nil, fmt.Errorf("no source workbooks provided")
	}
	if err := os.MkdirAll(outDir, 0o750); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}

	// Scratch space for full PDFs, title pages, and per-file extracts.
	work, err := os.MkdirTemp("", "bracket-pdf-*")
	if err != nil {
		return nil, fmt.Errorf("create work dir: %w", err)
	}
	defer func() { _ = os.RemoveAll(work) }()

	// Convert each workbook once.
	conv := make([]converted, 0, len(sources))
	for _, src := range sources {
		full, err := g.conv.ConvertToPDF(ctx, src.Path, work)
		if err != nil {
			return nil, err
		}
		ranges, err := SheetRanges(full)
		if err != nil {
			return nil, err
		}
		conv = append(conv, converted{src: src, pdf: full, ranges: ranges})
	}

	out := make(map[string]string)
	for _, grp := range groups {
		path, ok, err := g.buildGroup(ctx, grp, conv, work, outDir)
		if err != nil {
			return nil, fmt.Errorf("group %q: %w", grp.Type, err)
		}
		if ok {
			out[grp.Type] = path
		}
	}
	return out, nil
}

// buildGroup assembles one group's PDF. The bool is false when the group
// produced no pages (and no file was written).
func (g *Generator) buildGroup(ctx context.Context, grp Group, conv []converted, work, outDir string) (string, bool, error) {
	var parts []string // per-file PDFs (title pages + extracts), in order
	extractSeq := 0

	for _, c := range conv {
		if grp.SkipTeamWorkbooks && c.src.IsTeam {
			continue
		}
		wanted := grp.resolveSheets(c.sheetNamesPresent())
		if len(wanted) == 0 {
			continue
		}

		var picks []SheetRange
		for _, sheet := range wanted {
			if r, ok := c.rangeFor(sheet); ok {
				picks = append(picks, r)
			}
		}
		if len(picks) == 0 {
			continue
		}

		extractSeq++

		if grp.InsertTitle {
			uid := fmt.Sprintf("%s_%d", grp.Type, extractSeq)
			titlePDF, err := g.conv.makeTitlePage(ctx, c.src.titleOrStem(), grp.A3Landscape, work, uid)
			if err != nil {
				return "", false, err
			}
			parts = append(parts, titlePDF)
		}

		extract := filepath.Join(work, fmt.Sprintf("%s_%d_extract.pdf", grp.Type, extractSeq))
		if err := ExtractPages(c.pdf, picks, extract); err != nil {
			return "", false, err
		}
		parts = append(parts, extract)
	}

	if len(parts) == 0 {
		return "", false, nil
	}

	merged := filepath.Join(work, grp.Type+"_merged.pdf")
	if err := MergePDFs(parts, merged); err != nil {
		return "", false, err
	}

	final := merged
	if grp.PageNumbers {
		stamped := filepath.Join(work, grp.Type+"_stamped.pdf")
		if err := StampPageNumbers(merged, stamped); err != nil {
			return "", false, err
		}
		final = stamped
	}

	// Atomic publish: write into outDir via a temp file then rename, so a
	// partially-written PDF is never observed at the destination path.
	outPath := filepath.Join(outDir, grp.Output)
	if err := publishAtomic(final, outPath); err != nil {
		return "", false, err
	}
	return outPath, true, nil
}

// publishAtomic copies src into the destination directory under a temp name and
// renames it onto dst. src and dst may be on different filesystems (work dir is
// in the OS temp area), so we copy rather than rename across the boundary; the
// final rename within dst's directory is atomic.
func publishAtomic(src, dst string) error {
	data, err := os.ReadFile(src) // #nosec G304 -- src is an internally-generated PDF in a temp work dir.
	if err != nil {
		return fmt.Errorf("read generated pdf: %w", err)
	}
	tmp := dst + ".tmp"
	// #nosec G703 G304 -- dst is the caller's output dir joined with a fixed group filename constant.
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write temp output: %w", err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("publish output: %w", err)
	}
	return nil
}
