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

	// Set page layout to A5
	pageSize := int(11) // A5: 11
	if err := f.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
		Size: &pageSize,
	}); err != nil {
		fmt.Printf("Warning: failed to set page layout: %v\n", err)
	}

	// Set Page Margins to be narrow
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

	// Center on page
	// Note: CenterHorizontally/Vertically are not directly available in PageLayoutOptions in this version of excelize.
	// We will skip explicit centering for now or use default behavior.
	/*
		boolTrue := true
		if err := f.SetPageLayout(sheetName, &excelize.PageLayoutOptions{
			CenterHorizontally: &boolTrue,
			CenterVertically:   &boolTrue,
		}); err != nil {
			fmt.Printf("Warning: failed to set page centering: %v\n", err)
		}
	*/

	style, err := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Font: &excelize.Font{
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
			cell := fmt.Sprintf("A%d", row)
			tag := fmt.Sprintf("%s%d", poolLetter, player.PoolPosition)

			if err := f.SetCellValue(sheetName, cell, tag); err != nil {
				return fmt.Errorf("failed to set cell value: %w", err)
			}

			if err := f.SetCellStyle(sheetName, cell, cell, style); err != nil {
				return fmt.Errorf("failed to set cell style: %w", err)
			}

			// Set a large row height to try and cover the page
			// A5 height is 210mm (landscape) or 148mm (portrait)? Default is usually portrait.
			// 148mm is approx 420 points.
			// Excel max row height is 409 points.
			if err := f.SetRowHeight(sheetName, row, 400); err != nil {
				fmt.Printf("Warning: failed to set row height: %v\n", err)
			}

			// Insert page break after this row
			if err := f.InsertPageBreak(sheetName, cell); err != nil {
				fmt.Printf("Warning: failed to insert page break: %v\n", err)
			}

			row++
		}
	}

	f.SetActiveSheet(index)
	return nil
}
