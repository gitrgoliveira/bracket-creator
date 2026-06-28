package pdf

import (
	"fmt"
	"os"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// conf returns a fresh default pdfcpu configuration. Some pdfcpu api entry
// points (e.g. PageCount) dereference the configuration and panic on nil, so
// we always pass an explicit one rather than relying on nil-defaulting.
func conf() *model.Configuration {
	return model.NewDefaultConfiguration()
}

// SheetRange is a sheet's inclusive 1-indexed page span within a full PDF.
type SheetRange struct {
	Sheet    string
	PageFrom int // 1-indexed, inclusive
	PageThru int // 1-indexed, inclusive
}

// SheetRanges reads the per-sheet bookmarks LibreOffice embeds in a converted
// PDF and returns one SheetRange per sheet, in document order.
//
// pdfcpu leaves PageThru==0 on the final bookmark because it has no successor
// to bound it; we patch any bookmark whose PageThru < PageFrom to end at the
// last page of the document. Without this the final sheet's pages are dropped.
func SheetRanges(pdfPath string) ([]SheetRange, error) {
	f, err := os.Open(pdfPath) // #nosec G304 -- pdfPath is an internally-generated soffice output path, not user input.
	if err != nil {
		return nil, fmt.Errorf("open pdf: %w", err)
	}
	defer func() { _ = f.Close() }()

	bms, err := api.Bookmarks(f, conf())
	if err != nil {
		return nil, fmt.Errorf("read bookmarks from %s: %w", pdfPath, err)
	}

	total, err := PageCount(pdfPath)
	if err != nil {
		return nil, err
	}

	ranges := make([]SheetRange, 0, len(bms))
	for _, b := range bms {
		thru := b.PageThru
		if thru < b.PageFrom {
			// Final (unbounded) bookmark — extend to end of document.
			thru = total
		}
		ranges = append(ranges, SheetRange{
			Sheet:    b.Title,
			PageFrom: b.PageFrom,
			PageThru: thru,
		})
	}
	return ranges, nil
}

// PageCount returns the number of pages in a PDF.
func PageCount(pdfPath string) (int, error) {
	f, err := os.Open(pdfPath) // #nosec G304 -- pdfPath is an internally-generated PDF path, not user input.
	if err != nil {
		return 0, fmt.Errorf("open pdf: %w", err)
	}
	defer func() { _ = f.Close() }()

	n, err := api.PageCount(f, conf())
	if err != nil {
		return 0, fmt.Errorf("page count of %s: %w", pdfPath, err)
	}
	return n, nil
}

// ExtractPages writes a new PDF at outPath containing only the given inclusive
// 1-indexed page ranges from srcPath, in the order supplied.
func ExtractPages(srcPath string, ranges []SheetRange, outPath string) error {
	if len(ranges) == 0 {
		return fmt.Errorf("no page ranges to extract from %s", srcPath)
	}
	pages := make([]string, 0, len(ranges))
	for _, r := range ranges {
		pages = append(pages, fmt.Sprintf("%d-%d", r.PageFrom, r.PageThru))
	}
	// api.CollectFile keeps the listed pages (in order) and writes a new file.
	if err := api.CollectFile(srcPath, outPath, pages, conf()); err != nil {
		return fmt.Errorf("extract pages %s from %s: %w", strings.Join(pages, ","), srcPath, err)
	}
	return nil
}

// MergePDFs concatenates the given PDFs (in order) into a single outPath.
func MergePDFs(inPaths []string, outPath string) error {
	if len(inPaths) == 0 {
		return fmt.Errorf("no PDFs to merge")
	}
	// dividerPage=false: no blank separator page between merged files.
	if err := api.MergeCreateFile(inPaths, outPath, false, conf()); err != nil {
		return fmt.Errorf("merge %d pdf(s) into %s: %w", len(inPaths), outPath, err)
	}
	return nil
}
