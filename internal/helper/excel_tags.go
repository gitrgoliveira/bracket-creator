package helper

import (
	"fmt"
	"net/url"

	qrcode "github.com/skip2/go-qrcode"
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
	return CreateTagsSheetWithReveal(f, pools, publicURL, numberPrefix, nil)
}

// TagReveal gives a competitor's tag the competition's per-person reveal
// link in place of the watchlist link. It is for a competition whose public
// pages show numbers and whose option has per-person links
// (state.PublicNamesPerson); every other competition passes nil, so no
// secret is ever printed for it.
type TagReveal struct {
	// CompetitionID is the competition the link opens (the /reveal/ path).
	CompetitionID string
	// KeyFor is the competitor's own reveal key by participant id, or ""
	// for a competitor that has none.
	KeyFor func(participantID string) string
}

// CreateTagsSheetWithReveal is CreateTagsSheet with the tag QR of a
// competitor who has a reveal key opening their own reveal link
// (<publicURL>/reveal/<competition>#<key>) instead of the watchlist link,
// so scanning the printed tag shows that one competitor's name. A nil reveal
// or a competitor with no key keeps the watchlist link, which carries a
// number only. The key travels in the fragment, so it never reaches a server
// log.
func CreateTagsSheetWithReveal(f *excelize.File, pools []Pool, publicURL string, numberPrefix string, reveal *TagReveal) error {
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
			// A competitor with a reveal key gets their own reveal link
			// (a longer URL, so a denser code printed at the same size:
			// medium error correction and a larger source image).
			var qrPNG []byte
			qrScale := 0.6
			if key := reveal.keyFor(player.ID); key != "" && publicURL != "" && player.Number != "" {
				qrPNG, err = revealTagQRPNG(publicURL, reveal.CompetitionID, key)
				qrScale = revealQRScale
			} else {
				qrPNG, err = playerTagQRPNG(publicURL, player.Number)
			}
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
							ScaleX:      qrScale,
							ScaleY:      qrScale,
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
	// Guarded on row > 1 (bc-pnum): with zero players (reachable --
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

// keyFor is the competitor's reveal key; a nil reveal, an unset KeyFor or a
// competitor with no participant id has none.
func (r *TagReveal) keyFor(participantID string) string {
	if r == nil || r.KeyFor == nil || participantID == "" {
		return ""
	}
	return r.KeyFor(participantID)
}

// revealQRPx is the source size of a reveal link's QR image. Its link is
// longer than the watchlist link, so the code has more modules; a larger
// image scaled down by revealQRScale prints at the same 120 px (about 3.2
// cm) as the watchlist code, keeping the modules whole pixels.
const (
	revealQRPx    = 400
	revealQRScale = 0.3
)

// revealTagURL builds the link a competitor's tag QR opens:
// <publicURL>/reveal/<competition>#<key>, composed through playerTagURL so
// the public URL is judged by the same rule (http or https, a host, no
// credentials, any path kept). "" when the public URL is not usable.
func revealTagURL(publicURL, competitionID, key string) string {
	base, err := url.Parse(playerTagURL(publicURL, "x"))
	if err != nil || base.Host == "" {
		return ""
	}
	base.Path += "reveal/" + competitionID
	base.RawPath = ""
	base.RawQuery = ""
	base.Fragment = key
	return base.String()
}

// revealTagQRPNG is the PNG QR code of a competitor's reveal link; nil, nil
// when any input is empty, an error when the public URL is not usable.
func revealTagQRPNG(publicURL, competitionID, key string) ([]byte, error) {
	if publicURL == "" || competitionID == "" || key == "" {
		return nil, nil
	}
	link := revealTagURL(publicURL, competitionID, key)
	if link == "" {
		return nil, fmt.Errorf("cannot build QR URL from %q", publicURL)
	}
	png, err := qrcode.Encode(link, qrcode.Medium, revealQRPx)
	if err != nil {
		return nil, fmt.Errorf("qr encode reveal link: %w", err)
	}
	return png, nil
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
			Font: &excelize.Font{Family: WorkbookFont, Bold: true, Size: 160},
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
		Font: &excelize.Font{Family: WorkbookFont,
			Bold: true,
			Size: 250,
		},
	})
}
