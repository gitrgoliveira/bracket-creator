package helper

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"unicode/utf8"
	"weak"

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
	styleCompetitionTag       styleKey = "competition_tag"
	styleTime                 styleKey = "time"
	styleDuration             styleKey = "duration"
	styleUnlockedText         styleKey = "unlocked_text"
	styleUnlockedBorderBottom styleKey = "unlocked_border_bottom"
)

// styleCacheByFile holds each workbook's style ids, keyed by a WEAK pointer so
// the cache never keeps a workbook alive: the long-running app styles one per
// export. The entry is dropped by a cleanup the runtime runs once the
// workbook is unreachable, so no caller has a release to forget.
var (
	styleCacheMu     sync.Mutex
	styleCacheByFile = make(map[weak.Pointer[excelize.File]]map[styleKey]int)
)

func getCachedStyle(f *excelize.File, key styleKey, builder func(*excelize.File) int) int {
	fileKey := weak.Make(f)
	styleCacheMu.Lock()
	defer styleCacheMu.Unlock()

	cacheForFile, ok := styleCacheByFile[fileKey]
	if !ok {
		cacheForFile = make(map[styleKey]int)
		styleCacheByFile[fileKey] = cacheForFile
		runtime.AddCleanup(f, dropStyleCache, fileKey)
	}

	if styleID, ok := cacheForFile[key]; ok {
		return styleID
	}

	styleID := builder(f)
	cacheForFile[key] = styleID
	return styleID
}

// dropStyleCache removes a collected workbook's style ids.
func dropStyleCache(fileKey weak.Pointer[excelize.File]) {
	styleCacheMu.Lock()
	defer styleCacheMu.Unlock()
	delete(styleCacheByFile, fileKey)
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

// buildNameIDStyle is the competitor name's style. The name column has a fixed
// width (namesToPrintNameColWidth); a long name shrinks into it rather than
// overflowing or wrapping.
func buildNameIDStyle(f *excelize.File) int {
	return buildNameBoxStyle(f, false, 110)
}

// buildNameBoxStyle is the one style of a name printed in a Names to Print box,
// a competitor's or the competition's tag: centred in a thick border at the
// given size, wrapping when wrap is set and shrinking to fit otherwise.
func buildNameBoxStyle(f *excelize.File, wrap bool, size float64) int {
	alignment := &excelize.Alignment{Horizontal: "center", Vertical: "center"}
	if wrap {
		alignment.WrapText = true
	} else {
		alignment.ShrinkToFit = true
	}
	return mustNewStyle(f, &excelize.Style{
		Alignment: alignment,
		Font:      &excelize.Font{Family: "Calibri", Bold: false, Color: "000000", Size: size},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 2},
			{Type: "bottom", Color: "000000", Style: 2},
			{Type: "left", Color: "000000", Style: 2},
			{Type: "right", Color: "000000", Style: 2},
		},
	})
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
	return buildNameIDPositionStyleFor(f, false, 0)
}

// buildNameIDPositionStyleFor holds the border/font/alignment block shared
// by buildNameIDPositionStyle and buildNameIDPositionStackedStyle, which
// differ only in ShrinkToFit-vs-WrapText and font size (bc-pnum review(d)).
// letterCount is unused when stacked is false (buildNameIDPositionStyle
// passes 0).
func buildNameIDPositionStyleFor(f *excelize.File, stacked bool, letterCount int) int {
	alignment := &excelize.Alignment{
		Horizontal: "center",
		Vertical:   "center",
	}
	fontSize := 100.0
	if stacked {
		alignment.WrapText = true
		fontSize = nameIDPositionStackedFontSize(letterCount)
	} else {
		alignment.ShrinkToFit = true
	}
	return mustNewStyle(f, &excelize.Style{
		Alignment: alignment,
		Font:      &excelize.Font{Family: "Calibri", Bold: true, Color: "000000", Size: fontSize},
		Border: []excelize.Border{
			{Type: "top", Color: "000000", Style: 2},
			{Type: "bottom", Color: "000000", Style: 2},
			{Type: "left", Color: "000000", Style: 2},
			{Type: "right", Color: "000000", Style: 2},
		},
	})
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
	return buildNameIDPositionStyleFor(f, true, letterCount)
}

// Competition-name tag sizing (competitionTagFit). A name of up to
// competitionTagOneLineMaxRunes characters prints on one line, shrunk to fit
// the merged A:B cell as a competitor's name is. A longer one wraps, at
// competitionTagFitConstant / sqrt(characters) points, rounded down: the area
// a wrapped name covers grows with its length times the square of its size,
// so this keeps it inside the 270pt row at any length.
//
// Both were measured with LibreOffice, which lays a cell out the way the
// Export PDFs print it (each row's optimal height for the wrapped name, set
// against the 270pt row), for every length from 25 to 300 characters and at
// 350, 400, 500, 800 and 1000, in ordinary text, the same text in capitals,
// words of W and M, and an unbroken run of W. Locally (LibreOffice 26.8) in
// Liberation Sans, DejaVu Sans and Carlito (Calibri's metrics, also in a
// column narrowed to the 150 units Excel's narrower width units amount to):
// at 370 the tallest is 248pt (DejaVu Sans, 27 W's at 71pt); at 380, 25 W's
// at 76pt came out taller than the row. Again from 46 characters up in the
// Debian image the app's PDF export runs in (LibreOffice 25.2): with Carlito
// installed the tallest is 204pt (76 W's at 42pt), and with Liberation alone,
// where Calibri falls back to Liberation Mono, 181pt. The one-line limit, 45
// characters, is the organiser's ruling: a name that fits a line keeps it. A
// 45-character name on one line, in ordinary text, words of W and M or an
// unbroken run of W, shrinks to print whole in all these fonts, and a
// 46-character one wraps at 54pt. From about 25 characters up, wrapping would
// print ordinary text about as large or larger (at 30 characters one line is
// 58 to 70pt against 67pt wrapped), so the limit trades some size for keeping
// the name on one line; it never costs fit.
const (
	competitionTagFontSize        = 110.0
	competitionTagOneLineMaxRunes = 45
	competitionTagFitConstant     = 370.0
)

// competitionTagFit is the one rule for how a competition-name tag prints:
// whether it wraps and at what font size. A one-line tag shrinks to fit from
// competitionTagFontSize, so its size is only where shrinking starts.
func competitionTagFit(tag string) (wrap bool, size float64) {
	n := utf8.RuneCountInString(tag)
	if n <= competitionTagOneLineMaxRunes {
		return false, competitionTagFontSize
	}
	return true, math.Floor(competitionTagFitConstant / math.Sqrt(float64(n)))
}

// getCompetitionTagStyle returns the style for a competition-name tag, cached
// per size, so every tag of one name shares one style.
func getCompetitionTagStyle(f *excelize.File, tag string) int {
	wrap, size := competitionTagFit(tag)
	key := styleCompetitionTag
	if wrap {
		key = styleKey(fmt.Sprintf("%s_wrap_%g", styleCompetitionTag, size))
	}
	return getCachedStyle(f, key, func(f *excelize.File) int {
		return buildNameBoxStyle(f, wrap, size)
	})
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
