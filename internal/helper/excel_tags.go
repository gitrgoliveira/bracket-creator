package helper

import (
	"fmt"

	excelize "github.com/xuri/excelize/v2"
)

// CreateTagsSheet adds a "Tags" sheet to f with two large competitor tags per
// A4 page (one tag per half-page, two copies per player). When publicURL is
// non-empty and a player has a Number, a QR code is embedded in the
// bottom-left corner of each tag linking to the public viewer pre-filtered to
// that competitor. numberPrefix is the competition's own number prefix
// (helper.AssignPlayerNumbers/NumberPools's prefix argument), the single
// source of truth this sheet's layout is driven from -- see splitNumberLines
// (numbers.go, bc-pnum review) for why re-deriving it by scanning a
// representative player's Number is wrong whenever the prefix itself carries
// a digit.
func CreateTagsSheet(f *excelize.File, pools []Pool, publicURL string, numberPrefix string) error {
	sheetName := SheetTags
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return fmt.Errorf("failed to create sheet %s: %w", sheetName, err)
	}

	// A4 portrait, 2 tags per page (each tag = one half-page row)
	pageSize := 9 // A4
	orientation := "portrait"
	handleExcelError("SetPageLayout", f.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
		Size:        &pageSize,
		Orientation: &orientation,
	}))

	// Narrow margins
	margin := 0.1
	handleExcelError("SetPageMargins", f.SetPageMargins(sheetName, &excelize.PageLayoutMarginsOptions{
		Bottom: &margin,
		Footer: &margin,
		Header: &margin,
		Left:   &margin,
		Right:  &margin,
		Top:    &margin,
	}))

	// Column width to fill A4 portrait content width (~205 mm). bc-pnum A9:
	// 110 units renders to ~691pt, wider than the ~580.9pt A4-portrait
	// printable width (narrow margins from above), so a 4-character tag
	// (e.g. "KOR19", now reachable since a prefix can be up to 3 characters
	// everywhere) was sheared at the page edge -- rendered LibreOffice PDFs
	// showed "KOR1(" indistinguishable from "KOR1", and the excess spilled
	// into blank overflow pages. 88 units fits the printable width.
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "A", "A", 88))

	// The sheet's number layout is decided from the competition's own
	// number prefix (bc-pnum operator ruling -- a prefix of more than one
	// CHARACTER prints as two stacked lines, "KO" over "20"; a one-character
	// prefix stays on one line, "K20"), not by re-scanning players for one
	// (bc-pnum review -- see splitNumberLines, numbers.go, for why
	// that guess breaks on a digit-bearing prefix like "KO2").
	stacked := stackedNumberPrefix(numberPrefix)
	style, err := tagNumberStyle(f, stacked)
	if err != nil {
		return fmt.Errorf("failed to create style: %w", err)
	}

	printObj := true

	row := 1
	for _, pool := range pools {
		for _, player := range pool.Players {
			// The tag IS the competitor's number: it has to match the
			// competition's number prefix, because that is what the desk and
			// the tag's own QR resolve against. There is deliberately no
			// pool-letter substitute for an unnumbered competitor -- a
			// second, prefix-less numbering scheme on the printed tag was
			// considered for this bead and rejected (bc-pnum).
			tag := player.Number

			// Stacked layout (sheet-wide decision above) writes this
			// PLAYER's own letters/digits split as two lines; the prefix
			// is the same for every player on the sheet, only the digits
			// differ. An unnumbered player's tag stays empty either way.
			// A tag that does NOT actually carry the sheet's prefix
			// (hand-edited/legacy data) falls back to single-line rather
			// than a fabricated cut -- splitNumberLines' own D1 rule.
			cellValue := tag
			if stacked && tag != "" {
				if letters, digits, tagStacked := splitNumberLines(tag, numberPrefix); tagStacked {
					cellValue = letters + "\n" + digits
				}
			}

			// Generate QR once per player; reuse PNG for both tag copies.
			// playerTagQRPNG returns nil,nil for empty inputs, so no guard needed.
			var qrPNG []byte
			qrPNG, err = playerTagQRPNG(publicURL, player.Number)
			if err != nil {
				return fmt.Errorf("QR for %s: %w", player.Number, err)
			}

			// Write the same tag twice (top half and bottom half of A4 = 2 per page).
			for range 2 {
				cell := fmt.Sprintf("A%d", row)
				if err := f.SetCellValue(sheetName, cell, cellValue); err != nil {
					return fmt.Errorf("failed to set cell value: %w", err)
				}
				if err := f.SetCellStyle(sheetName, cell, cell, style); err != nil {
					return fmt.Errorf("failed to set cell style: %w", err)
				}
				// excelize max row height is 409pt (~144mm, ~half A4 portrait)
				handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 409))

				if len(qrPNG) > 0 {
					// Bottom-left corner of the tag, in BOTH the single-line and
					// stacked layouts (bc-pnum operator ruling on stacked
					// prefixes). Single-line stays vertical-top now (not
					// centred), so the whole band below the glyphs is free;
					// stacked is two lines at 160pt, and the centred digits
					// line starts no further left than x≈130px, clear of a
					// 120px QR anchored at the column's left edge. A 120 px QR
					// (200 px × 0.6, about 3.2 cm on paper) at OffsetX 8,
					// OffsetY 415 sits inside that band clear of any glyph for
					// every prefix length; rendered with LibreOffice for K20,
					// KO20, KOR20 and KO120 (bc-pnum review).
					if err := f.AddPictureFromBytes(sheetName, cell, &excelize.Picture{
						Extension: ".png",
						File:      qrPNG,
						Format: &excelize.GraphicOptions{
							PrintObject: &printObj,
							OffsetX:     8,
							OffsetY:     415,
							ScaleX:      0.6,
							ScaleY:      0.6,
							Positioning: "oneCell",
						},
					}); err != nil {
						return fmt.Errorf("failed to add QR picture at %s: %w", cell, err)
					}
				}

				row++
			}

			// Page break after each pair of identical labels.
			handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", row)))
		}
	}

	// bc-pnum A9: without an explicit print area, LibreOffice's rendering of
	// this sheet's used range (which excelize's own column-width/row-height
	// bookkeeping can make wider than the actual A column) spilled 82 blank
	// overflow pages in the reproduction that surfaced this. row-1 is the
	// last row actually written (the loop above leaves row one past it).
	//
	// Guarded on row > 1 (bc-pnum [review]): with zero players (reachable --
	// an export of a mixed competition still in setup, before any pool has a
	// member) the loop above never runs and row stays at its initial 1, so
	// row-1 is 0 and SetPrintArea would define the invalid range
	// "$A$1:$A$0" (row 0 does not exist). No players means nothing was
	// written, so there is nothing to scope a print area to at all.
	if row > 1 {
		SetPrintArea(f, sheetName, 1, row-1)
	}

	f.SetActiveSheet(index)
	return nil
}

// tagNumberStyle builds the Tags sheet's number-cell style: stacked (a
// prefix of more than one letter, "KO" over "20") wraps the letters+digits
// value onto two lines at 160pt, which fits Excel's 409pt row-height cap and
// keeps up to three letters within the 88-unit column; single-line (a
// one-letter prefix, "K20") keeps the existing 250pt ShrinkToFit behaviour.
// Both are vertical-top rather than centred, so the whole band below the
// glyphs stays free for the QR (bc-pnum operator ruling).
func tagNumberStyle(f *excelize.File, stacked bool) (int, error) {
	if stacked {
		return f.NewStyle(&excelize.Style{
			Alignment: &excelize.Alignment{
				Horizontal: "center",
				Vertical:   "top",
				WrapText:   true,
			},
			Font: &excelize.Font{Family: "Calibri", Bold: true, Size: 160},
		})
	}
	return f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "top",
			// ShrinkToFit is the second half of the same fix: even at 88
			// units, a 5-character tag ("KOR19") at 250pt bold can still
			// overflow the column, and shrinking the font to fit rather than
			// clipping is what actually keeps every character of a long tag
			// visible and readable at the desk.
			ShrinkToFit: true,
		},
		Font: &excelize.Font{Family: "Calibri",
			Bold: true,
			Size: 250,
		},
	})
}
