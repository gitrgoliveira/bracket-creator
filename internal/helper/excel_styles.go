package helper

import (
	"fmt"
	"sync"

	excelize "github.com/xuri/excelize/v2"
)

type styleKey string

const (
	styleBorderTop            styleKey = "border_top"
	styleBorderBottom         styleKey = "border_bottom"
	styleBorderBottomLeft     styleKey = "border_bottom_left"
	styleBorderLeft           styleKey = "border_left"
	styleTreeHeader           styleKey = "tree_header"
	styleTreeTop              styleKey = "tree_top"
	styleTreeBody             styleKey = "tree_body"
	styleTreeBottom           styleKey = "tree_bottom"
	styleTreeText             styleKey = "tree_text"
	stylePoolHeader           styleKey = "pool_header"
	styleRedHeader            styleKey = "red_header"
	styleWhiteHeader          styleKey = "white_header"
	styleText                 styleKey = "text"
	styleNameID               styleKey = "name_id"
	styleNameIDPosition       styleKey = "name_id_position"
	styleNameIDPositionStack  styleKey = "name_id_position_stacked"
	styleTime                 styleKey = "time"
	styleDuration             styleKey = "duration"
	styleUnlockedText         styleKey = "unlocked_text"
	styleUnlockedBorderBottom styleKey = "unlocked_border_bottom"
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
	borderStyle := mustNewStyle(f, &excelize.Style{
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
	borderStyle := mustNewStyle(f, &excelize.Style{
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
	borderStyle := mustNewStyle(f, &excelize.Style{
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
		Font:      &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 12},
		Alignment: &excelize.Alignment{Horizontal: "left"},
	})
	return borderStyle
}

func GetBorderStyleLeft(f *excelize.File) int {
	return getCachedStyle(f, styleBorderLeft, buildBorderStyleLeft)
}

func buildBorderStyleLeft(f *excelize.File) int {
	borderStyle := mustNewStyle(f, &excelize.Style{
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
	borderStyle := mustNewStyle(f, &excelize.Style{
		Font: &excelize.Font{Family: "Calibri", Bold: true, Color: "000000", Size: 12},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"EFEFEF"}, Pattern: 1},
	})
	return borderStyle
}

func getTreeTopStyle(f *excelize.File) int {
	return getCachedStyle(f, styleTreeTop, buildTreeTopStyle)
}

func buildTreeTopStyle(f *excelize.File) int {
	borderStyle := mustNewStyle(f, &excelize.Style{
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
		Font: &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 12},
	})
	return borderStyle
}

func getTreeBodyStyle(f *excelize.File) int {
	return getCachedStyle(f, styleTreeBody, buildTreeBodyStyle)
}

func buildTreeBodyStyle(f *excelize.File) int {
	borderStyle := mustNewStyle(f, &excelize.Style{
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
		Font: &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 12},
	})
	return borderStyle
}

func getTreeBottomStyle(f *excelize.File) int {
	return getCachedStyle(f, styleTreeBottom, buildTreeBottomStyle)
}

func buildTreeBottomStyle(f *excelize.File) int {
	borderStyle := mustNewStyle(f, &excelize.Style{
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
		Font: &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 12},
	})
	return borderStyle
}

func getTreeTextStyle(f *excelize.File) int {
	return getCachedStyle(f, styleTreeText, buildTreeTextStyle)
}

// buildTreeTextStyle styles a tree leaf label: bold text sitting on the
// bracket's entrant underline. Deliberately NO fill: the label cell used to be
// a 3.5-unit bracket column where a grey fill was a barely-visible sliver, but
// with the wide label column (and the bye-leaf span in writeTreeValue) a fill
// renders as a prominent grey band across the page.
func buildTreeTextStyle(f *excelize.File) int {
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "right", Vertical: "center"},
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Color: "000000", Size: 12},
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
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center", Vertical: "center"},
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Color: "000000", Size: 12},
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
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Color: "FFFFFF", Size: 12},
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
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Color: "000000", Size: 12},
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
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 12},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
			{Type: "left", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
		},
	})
	return style
}

func getGreyTextStyle(f *excelize.File) int {
	return getCachedStyle(f, styleKey("grey_text"), buildGreyTextStyle)
}

func buildGreyTextStyle(f *excelize.File) int {
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 12},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
			{Type: "left", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
		},
		Fill: excelize.Fill{Type: "pattern", Color: []string{"EFEFEF"}, Pattern: 1},
	})
	return style
}

func getNameIDStyle(f *excelize.File) int {
	return getCachedStyle(f, styleNameID, buildNameIDStyle)
}

func buildNameIDStyle(f *excelize.File) int {
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
			// The name column has a fixed width (namesToPrintNameColWidth); a
			// long name shrinks into it rather than overflowing or wrapping.
			ShrinkToFit: true,
		},
		Font: &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 110},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 2},
			{Type: "bottom", Color: "000000", Style: 2},
			{Type: "left", Color: "000000", Style: 2},
			{Type: "right", Color: "000000", Style: 2},
		},
	})
	return style
}

func getNameIDPositionStyle(f *excelize.File) int {
	return getCachedStyle(f, styleNameIDPosition, buildNameIDPositionStyle)
}

// buildNameIDPositionStyle creates a large, bold style for the position-number
// row in "Names to Print" column A, for a one-letter (or empty) number
// prefix that prints on a SINGLE line (bc-pnum operator ruling: "K20" stays
// one line, "KO20" stacks -- see getNameIDPositionStackedStyle below). The
// font size matches the Tags sheet so the number is clearly visible when
// printed.
//
// ShrinkToFit (bc-pnum A9): a 4- or 5-character number (a one-letter prefix
// plus a multi-digit counter, e.g. "K1234") rasterised byte-identically to
// shorter numbers in the same column ("K1230".."K1234" were indistinguishable
// in the reproduction) because the cell clipped rather than shrinking the
// glyphs to fit. No width/font change is needed here (unlike the Tags
// sheet): this column's width already comes from setupNamesToPrintLayout, so
// this flag alone is the fix.
func buildNameIDPositionStyle(f *excelize.File) int {
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal:  "center",
			Vertical:    "center",
			ShrinkToFit: true,
		},
		Font: &excelize.Font{Family: "Calibri", Bold: true, Color: "000000", Size: 100},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 2},
			{Type: "bottom", Color: "000000", Style: 2},
			{Type: "left", Color: "000000", Style: 2},
			{Type: "right", Color: "000000", Style: 2},
		},
	})
	return style
}

// getNameIDPositionStackedStyle returns the stacked position style sized for
// letterCount letters (a rune count -- see printNameEntries in excel.go).
// Cached per size: two prefixes of the same letter count share one style
// object, but a two-letter and a three-letter prefix need DIFFERENT font
// sizes (see buildNameIDPositionStackedStyle) and so cannot share a cache
// entry keyed only on styleNameIDPositionStack (bc-pnum review).
func getNameIDPositionStackedStyle(f *excelize.File, letterCount int) int {
	key := styleKey(fmt.Sprintf("%s_%d", styleNameIDPositionStack, letterCount))
	return getCachedStyle(f, key, func(f *excelize.File) int {
		return buildNameIDPositionStackedStyle(f, letterCount)
	})
}

// nameIDPositionStackedFontSize returns the stacked position style's font
// size for a prefix of letterCount letters. Two letters fit the 40-unit
// column at 100pt ("WW1234" -> "WW"/"1234" renders clean). Rendered with
// LibreOffice, a WIDE three-letter prefix ("WOM", "MMM", "WWW") at 100pt
// does not: the letters line itself is too wide for the column and wraps
// onto a THIRD line, overflowing the 270pt row cap ("WOM"/"119" rendered as
// "WO"/"M"/"119") -- KOR's narrower glyphs happened to fit at 100pt, which
// is why that fixture alone gave false confidence (bc-pnum review). 80pt is
// the measured fix for three letters (rendered clean for KOR, WOM and WWW);
// 90pt still wraps. MaxNumberPrefixLen caps a prefix at 3 characters, so
// letterCount is never reachable above 3 in practice, but any count of 3 or
// more takes the smaller size rather than assuming the cap holds.
func nameIDPositionStackedFontSize(letterCount int) float64 {
	if letterCount >= 3 {
		return 80
	}
	return 100
}

// buildNameIDPositionStackedStyle is the wrap-text counterpart of
// buildNameIDPositionStyle above, used when the sheet's number prefix is
// more than one letter (bc-pnum operator ruling): the position cell then
// holds a two-line LEFT/MID formula (letters over digits, see
// printNameEntries in excel.go) instead of a plain cross-sheet reference, so
// it needs WrapText rather than ShrinkToFit. The row height (270pt) is set
// per-row in printNameEntries (excel.go), not by setupNamesToPrintLayout,
// which sets the page layout and column widths; see nameIDPositionStackedFontSize above
// for why the font size depends on letterCount.
func buildNameIDPositionStackedStyle(f *excelize.File, letterCount int) int {
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{
			Horizontal: "center",
			Vertical:   "center",
			WrapText:   true,
		},
		Font: &excelize.Font{Family: "Calibri", Bold: true, Color: "000000", Size: nameIDPositionStackedFontSize(letterCount)},
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
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 12},
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

func getDurationStyle(f *excelize.File) int {
	return getCachedStyle(f, styleDuration, buildDurationStyle)
}

func buildDurationStyle(f *excelize.File) int {
	customFmt := "[h]:mm:ss"
	style := mustNewStyle(f, &excelize.Style{
		Alignment:    &excelize.Alignment{Horizontal: "center"},
		Font:         &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 12},
		CustomNumFmt: &customFmt,
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
			{Type: "left", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
		},
	})
	return style
}

func getUnlockedTextStyle(f *excelize.File) int {
	return getCachedStyle(f, styleUnlockedText, buildUnlockedTextStyle)
}

func buildUnlockedTextStyle(f *excelize.File) int {
	style := mustNewStyle(f, &excelize.Style{
		Alignment: &excelize.Alignment{Horizontal: "center"},
		Font:      &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: 12},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 1},
			{Type: "bottom", Color: "000000", Style: 1},
			{Type: "left", Color: "000000", Style: 1},
			{Type: "right", Color: "000000", Style: 1},
		},
		Protection: &excelize.Protection{Locked: false},
	})
	return style
}

func getUnlockedBorderStyleBottom(f *excelize.File) int {
	return getCachedStyle(f, styleUnlockedBorderBottom, buildUnlockedBorderStyleBottom)
}

func buildUnlockedBorderStyleBottom(f *excelize.File) int {
	style := mustNewStyle(f, &excelize.Style{
		Border: []excelize.Border{
			{
				Type:  "bottom",
				Color: "000000",
				Style: 2,
			},
		},
		Protection: &excelize.Protection{Locked: false},
	})
	return style
}

// mustNewStyle creates an Excel style and returns its ID.  It panics when
// style creation fails, which only happens when the Style definition itself
// is malformed, a programming error, not a runtime condition.
func mustNewStyle(f *excelize.File, style *excelize.Style) int {
	id, err := f.NewStyle(style)
	if err != nil {
		panic(fmt.Sprintf("failed to create Excel style: %v", err))
	}
	return id
}
