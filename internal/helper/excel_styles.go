package helper

import "github.com/xuri/excelize/v2"

func getBorderStyleTop(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "top",
				Color: "000000",
				Style: 2,
			},
		},
	})
	return borderStyle
}

func getBorderStyleBottom(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "bottom",
				Color: "000000",
				Style: 2,
			},
		},
	})
	return borderStyle
}

func GetBorderStyleBottomLeft(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "bottom",
				Color: "000000",
				Style: 2,
			},
			{
				Type:  "left",
				Color: "000000",
				Style: 2,
			},
		},
	})
	return borderStyle
}

func GetBorderStyleLeft(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "left",
				Color: "000000",
				Style: 2,
			},
		},
	})
	return borderStyle
}

func getTreeHeaderStyle(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "000000", Size: 12},
	})
	return borderStyle
}

func getTreeTopStyle(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "top",
				Color: "000000",
				Style: 2,
			},
			{
				Type:  "left",
				Color: "000000",
				Style: 2,
			},
			{
				Type:  "right",
				Color: "000000",
				Style: 2,
			},
		},
		Font: &excelize.Font{Bold: false, Color: "000000", Size: 12},
	})
	return borderStyle
}

func getTreeBodyStyle(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "left",
				Color: "000000",
				Style: 2,
			},
			{
				Type:  "right",
				Color: "000000",
				Style: 2,
			},
		},
		Font: &excelize.Font{Bold: false, Color: "000000", Size: 12},
	})
	return borderStyle
}

func getTreeBottomStyle(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "bottom",
				Color: "000000",
				Style: 2,
			},
			{
				Type:  "left",
				Color: "000000",
				Style: 2,
			},
			{
				Type:  "right",
				Color: "000000",
				Style: 2,
			},
		},
		Font: &excelize.Font{Bold: false, Color: "000000", Size: 12},
	})
	return borderStyle
}

func getPoolHeaderStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Bold: true, Color: "000000", Size: 12},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 2},
			{Type: "bottom", Color: "000000", Style: 2},
			{Type: "left", Color: "000000", Style: 2},
			{Type: "right", Color: "000000", Style: 2},
		},
	})
	return style
}
func getRedHeaderStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Bold: true, Color: "FFFFFF", Size: 12},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
			{Type: "left", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
		},
		Fill: excelize.Fill{
			Type:    "pattern",
			Color:   []string{"FF0000"},
			Pattern: 1,
		},
	})
	return style
}

func getWhiteHeaderStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Bold: true, Color: "000000", Size: 12},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
			{Type: "left", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
		},
		Fill: excelize.Fill{
			Type:  "solid",
			Color: []string{"FFFFFF"},
		},
	})
	return style
}

func getTextStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Bold: false, Color: "000000", Size: 12},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
			{Type: "left", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
		},
	})
	return style
}

func getNameIDStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Font: &excelize.Font{Bold: false, Color: "000000", Size: 30},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 2},
			{Type: "bottom", Color: "000000", Style: 2},
			{Type: "left", Color: "000000", Style: 2},
			{Type: "right", Color: "000000", Style: 2},
		},
	})
	return style
}
