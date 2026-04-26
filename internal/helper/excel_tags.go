package helper

import (
	"fmt"
	"strings"

	excelize "github.com/xuri/excelize/v2"
)

func CreateTagsSheet(f *excelize.File, pools []Pool) error {
	sheetName := "Tags"
	index, err := f.NewSheet(sheetName)
	if err != nil {
		return fmt.Errorf("failed to create sheet %s: %w", sheetName, err)
	}

	// A4 portrait, 2 labels per page (each label = half the page height)
	pageSize := 9 // A4
	orientation := "portrait"
	if err := f.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
		Size:        &pageSize,
		Orientation: &orientation,
	}); err != nil {
		fmt.Printf("Warning: failed to set page layout: %v\n", err)
	}

	// Narrow margins
	margin := 0.1
	if err := f.SetPageMargins(sheetName, &excelize.PageLayoutMarginsOptions{
		Bottom: &margin,
		Footer: &margin,
		Header: &margin,
		Left:   &margin,
		Right:  &margin,
		Top:    &margin,
	}); err != nil {
		fmt.Printf("Warning: failed to set page margins: %v\n", err)
	}

	// Column width to fill A4 portrait width
	if err := f.SetColWidth(sheetName, "A", "A", 90); err != nil {
		fmt.Printf("Warning: failed to set column width: %v\n", err)
	}

	style, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Font: &excelize.Font{Family: "Calibri",
			Bold: true,
			Size: 150,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create style: %w", err)
	}

	row := 1
	for _, pool := range pools {
		poolLetter := strings.TrimPrefix(pool.PoolName, "Pool ")

		for _, player := range pool.Players {
			tag := fmt.Sprintf("%s%d", poolLetter, player.PoolPosition)
			if player.Number != "" {
				tag = player.Number
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
				// Half of A4 portrait printable height (~146mm = ~390 points)
				if err := f.SetRowHeight(sheetName, row, 390); err != nil {
					fmt.Printf("Warning: failed to set row height: %v\n", err)
				}
				row++
			}

			// Page break after each pair of identical labels
			if err := f.InsertPageBreak(sheetName, fmt.Sprintf("A%d", row)); err != nil {
				fmt.Printf("Warning: failed to insert page break: %v\n", err)
			}
		}
	}

	f.SetActiveSheet(index)
	return nil
}
