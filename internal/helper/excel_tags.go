package helper

import (
	"fmt"
	"strings"

	excelize "github.com/xuri/excelize/v2"
)

// CreateTagsSheet adds a "Tags" sheet to f with one large competitor tag per
// row (two per A4 page). When publicURL is non-empty and a player has a
// Number, a QR code is embedded in the top-left corner of each tag linking to
// the public viewer pre-filtered to that competitor.
func CreateTagsSheet(f *excelize.File, pools []Pool, publicURL string) error {
	sheetName := SheetTags
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return fmt.Errorf("failed to create sheet %s: %w", sheetName, err)
	}

	// A4 portrait, 2 labels per page (each label = half the page height)
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

	// Column width to fill A4 portrait width
	handleExcelError("SetColWidth", f.SetColWidth(sheetName, "A", "A", 90))

	style, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Font: &excelize.Font{Family: "Calibri",
			Bold: true,
			Size: 200,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create style: %w", err)
	}

	printObj := true

	row := 1
	for _, pool := range pools {
		poolLetter := strings.TrimPrefix(pool.PoolName, "Pool ")

		for _, player := range pool.Players {
			tag := fmt.Sprintf("%s%d", poolLetter, player.PoolPosition)
			if player.Number != "" {
				tag = player.Number
			}

			// Generate QR once per player; reuse PNG for both tag copies.
			var qrPNG []byte
			if player.Number != "" {
				qrPNG, _ = playerTagQRPNG(publicURL, player.Number)
			}

			// Write the same tag twice (top half and bottom half of A4)
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
					// Bottom-left corner (OffsetX/Y in px at 96 DPI).
					// Tags are worn on the competitor's leg and read from far away —
					// the large centred number is primary. The QR is secondary: it
					// lets the competitor scan their own public profile with a phone.
					// At 409 pt row height (≈545 px), the 200 pt number occupies
					// roughly the middle 400 px. A 60 px QR (200 px × ScaleX 0.3) at
					// OffsetY 480 (= 545 − 60 − 5) sits in the bottom-left corner,
					// well clear of the number and unambiguously secondary.
					if err := f.AddPictureFromBytes(sheetName, cell, &excelize.Picture{
						Extension: ".png",
						File:      qrPNG,
						Format: &excelize.GraphicOptions{
							PrintObject: &printObj,
							OffsetX:     5,
							OffsetY:     480,
							ScaleX:      0.3,
							ScaleY:      0.3,
							Positioning: "oneCell",
						},
					}); err != nil {
						return fmt.Errorf("failed to add QR picture at %s: %w", cell, err)
					}
				}

				row++
			}

			// Page break after each pair of identical labels
			handleExcelError("InsertPageBreak", f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", row)))
		}
	}

	f.SetActiveSheet(index)
	return nil
}
