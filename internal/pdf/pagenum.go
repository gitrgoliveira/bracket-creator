package pdf

import (
	"fmt"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

// pageNumberDesc is the pdfcpu watermark descriptor for the footer page number:
// bottom-centre, small grey text, opaque, not rotated, drawn on top of content.
// pdfcpu text watermarks carry no dynamic page-number token, so we build one
// watermark per page (each with its own "N / M" text) and apply them in a
// single pass via AddWatermarksMapFile.
const pageNumberDesc = "scale:1 abs, pos:bc, off:0 12, rot:0, op:1, fillc:#555555, points:9"

// StampPageNumbers writes a copy of inPath to outPath with a "N / M" footer
// stamped on every page. It returns an error if the PDF has no pages.
func StampPageNumbers(inPath, outPath string) error {
	total, err := PageCount(inPath)
	if err != nil {
		return err
	}
	if total == 0 {
		return fmt.Errorf("cannot stamp page numbers: %s has no pages", inPath)
	}

	m := make(map[int]*model.Watermark, total)
	for p := 1; p <= total; p++ {
		text := fmt.Sprintf("%d / %d", p, total)
		wm, err := api.TextWatermark(text, pageNumberDesc, true /*onTop*/, false /*update*/, types.POINTS)
		if err != nil {
			return fmt.Errorf("build page-number watermark for page %d: %w", p, err)
		}
		m[p] = wm
	}

	if err := api.AddWatermarksMapFile(inPath, outPath, m, conf()); err != nil {
		return fmt.Errorf("stamp page numbers on %s: %w", inPath, err)
	}
	return nil
}
