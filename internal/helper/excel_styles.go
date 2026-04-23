package helper

import (
	"sync"

	excelize "github.com/xuri/excelize/v2"
)

type styleKey string

const (
	styleBorderTop        styleKey = "border_top"
	styleBorderBottom     styleKey = "border_bottom"
	styleBorderBottomLeft styleKey = "border_bottom_left"
	styleBorderLeft       styleKey = "border_left"
	styleTreeHeader       styleKey = "tree_header"
	styleTreeTop          styleKey = "tree_top"
	styleTreeBody         styleKey = "tree_body"
	styleTreeBottom       styleKey = "tree_bottom"
	styleTreeText         styleKey = "tree_text"
	stylePoolHeader       styleKey = "pool_header"
	styleRedHeader        styleKey = "red_header"
	styleWhiteHeader      styleKey = "white_header"
	styleText             styleKey = "text"
	styleNameID           styleKey = "name_id"
	styleNameIDSide       styleKey = "name_id_side"
	styleTime             styleKey = "time"
)

var (
	styleCacheMu     sync.Mutex
	styleCacheByFile = make(map[*excelize.File]map[styleKey]int)
)

func getCachedStyle(f *excelize.File, key styleKey, builder func(*excelize.File) int) int {
	styleCacheMu.Lock()
	defer styleCacheMu.Unlock()

	cacheForFile, ok := styleCacheByFile[f]
	if !ok {
		cacheForFile = make(map[styleKey]int)
		styleCacheByFile[f] = cacheForFile
	}

	if styleID, ok := cacheForFile[key]; ok {
		return styleID
	}

	styleID := builder(f)
	cacheForFile[key] = styleID
	return styleID
}

func getBorderStyleTop(f *excelize.File) int {
	return getCachedStyle(f, styleBorderTop, buildBorderStyleTop)
}

func buildBorderStyleTop(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "top",
				Color: "000000",
				Style: 2,
			},
		}})
	return borderStyle
}

func getBorderStyleBottom(f *excelize.File) int {
	return getCachedStyle(f, styleBorderBottom, buildBorderStyleBottom)
}

func buildBorderStyleBottom(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "bottom",
				Color: "000000",
				Style: 2,
			},
		}})
	return borderStyle
}

func GetBorderStyleBottomLeft(f *excelize.File) int {
	return getCachedStyle(f, styleBorderBottomLeft, buildBorderStyleBottomLeft)
}

func buildBorderStyleBottomLeft(f *excelize.File) int {
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
		Font:      &excelize.Font{Bold: false, Color: "000000", Size: 12},
		Alignment: &excelize.Alignment{Horizontal: "left"},
	})
	return borderStyle
}

func GetBorderStyleLeft(f *excelize.File) int {
	return getCachedStyle(f, styleBorderLeft, buildBorderStyleLeft)
}

func buildBorderStyleLeft(f *excelize.File) int {
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
	return getCachedStyle(f, styleTreeHeader, buildTreeHeaderStyle)
}

func buildTreeHeaderStyle(f *excelize.File) int {
	borderStyle, _ := f.NewStyle(&excelize.Style{
		Font: &excelize.Font{Bold: true, Color: "000000", Size: 12},
	})
	return borderStyle
}

func getTreeTopStyle(f *excelize.File) int {
	return getCachedStyle(f, styleTreeTop, buildTreeTopStyle)
}

func buildTreeTopStyle(f *excelize.File) int {
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
	return getCachedStyle(f, styleTreeBody, buildTreeBodyStyle)
}

func buildTreeBodyStyle(f *excelize.File) int {
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
	return getCachedStyle(f, styleTreeBottom, buildTreeBottomStyle)
}

func buildTreeBottomStyle(f *excelize.File) int {
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

func getTreeTextStyle(f *excelize.File) int {
	return getCachedStyle(f, styleTreeText, buildTreeTextStyle)
}

func buildTreeTextStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		Font:      &excelize.Font{Bold: true, Color: "000000", Size: 12},
		Border: []excelize.Border{
			{Type: "bottom", Color: "000000", Style: 2},
		},
	})
	return style
}

func getPoolHeaderStyle(f *excelize.File) int {
	return getCachedStyle(f, stylePoolHeader, buildPoolHeaderStyle)
}

func buildPoolHeaderStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
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
	return getCachedStyle(f, styleRedHeader, buildRedHeaderStyle)
}

func buildRedHeaderStyle(f *excelize.File) int {
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
	return getCachedStyle(f, styleWhiteHeader, buildWhiteHeaderStyle)
}

func buildWhiteHeaderStyle(f *excelize.File) int {
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
			Type:    "pattern",
			Color:   []string{"FFFFFF"},
			Pattern: 1,
		},
	})
	return style
}

func getTextStyle(f *excelize.File) int {
	return getCachedStyle(f, styleText, buildTextStyle)
}

func buildTextStyle(f *excelize.File) int {
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
	return getCachedStyle(f, styleNameID, buildNameIDStyle)
}

func buildNameIDStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Font: &excelize.Font{Bold: false, Color: "000000", Size: 110},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 2},
			{Type: "bottom", Color: "000000", Style: 2},
			{Type: "left", Color: "000000", Style: 2},
			{Type: "right", Color: "000000", Style: 2},
		},
	})
	return style
}

func getNameIDSideStyle(f *excelize.File) int {
	return getCachedStyle(f, styleNameIDSide, buildNameIDSideStyle)
}

func buildNameIDSideStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
		},
		Font: &excelize.Font{Bold: false, Color: "000000", Size: 28},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 2},
			{Type: "bottom", Color: "000000", Style: 2},
			{Type: "left", Color: "000000", Style: 2},
			{Type: "right", Color: "000000", Style: 2},
		},
	})
	return style
}

func getTimeStyle(f *excelize.File) int {
	return getCachedStyle(f, styleTime, buildTimeStyle)
}

func buildTimeStyle(f *excelize.File) int {
	style, _ := f.NewStyle(&excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Bold: false, Color: "000000", Size: 12},
		NumFmt:    20, // h:mm
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
			{Type: "left", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
		},
	})
	return style
}
