package helper

import (
	"fmt"

	excelize "github.com/xuri/excelize/v2"
)

// CreateTagsSheet adds a "Tags" sheet to f with two large competitor tags per
// A4 page (one tag per half-page, two copies per player). When publicURL is
// non-empty and a player has a Number, a QR code is embedded in the
// bottom-left corner of each tag linking to the public viewer pre-filtered to
// that competitor.
func CreateTagsSheet(f *excelize.File, pools []Pool, publicURL string) error {
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

	style, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
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
				if err := f.SetCellValue(sheetName, cell, tag); err != nil {
					return fmt.Errorf("failed to set cell value: %w", err)
				}
				if err := f.SetCellStyle(sheetName, cell, cell, style); err != nil {
					return fmt.Errorf("failed to set cell style: %w", err)
				}
				// excelize max row height is 409pt (~144mm, ~half A4 portrait)
				handleExcelError("SetRowHeight", f.SetRowHeight(sheetName, row, 409))

				if len(qrPNG) > 0 {
					// Bottom-left corner of the tag, BELOW the number (OffsetX/Y in px at
					// 96 DPI). The QR used to sit left of the number at its vertical
					// centre, in white space that only a short number left free: the
					// number now shrinks to fit the 88-unit column, so a four- or
					// five-character number ("KO20", "KOR20") fills the width and the
					// code landed on top of the first letter. The vertical band is free
					// instead: the row is 409 pt ≈ 545 px and the shrunk glyphs occupy
					// roughly the middle 280 px, leaving ≈130 px below them. A 90 px QR
					// (200 px × 0.45, about 2.4 cm on paper) at OffsetY 440 sits inside
					// that band clear of any glyph for every prefix length; rendered
					// with LibreOffice for K20, KO20 and KOR20 (bc-pnum review).
					if err := f.AddPictureFromBytes(sheetName, cell, &excelize.Picture{
						Extension: ".png",
						File:      qrPNG,
						Format: &excelize.GraphicOptions{
							PrintObject: &printObj,
							OffsetX:     12,
							OffsetY:     440,
							ScaleX:      0.45,
							ScaleY:      0.45,
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
