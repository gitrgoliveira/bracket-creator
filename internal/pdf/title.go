package pdf

import (
	"context"
	"fmt"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// titleSafeName turns an arbitrary title into a filesystem-safe stem.
var titleSafeName = regexp.MustCompile(`[^\w]+`)

// titlePageHTML renders the A4-portrait (or A3-landscape) title-page HTML with
// the given title centred, 36pt bold — mirroring make_title_page_pdf in the
// Python reference. The title is HTML-escaped to stay safe with arbitrary
// tournament names.
func titlePageHTML(title string, a3Landscape bool) string {
	width := "210mm"
	if a3Landscape {
		width = "420mm"
	}
	const height = "297mm"
	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<style>
  @page { size: %[1]s %[2]s; margin: 0; }
  body {
    margin: 0; padding: 0;
    width: %[1]s; height: %[2]s;
    display: flex; justify-content: center; align-items: center;
    font-family: Arial, sans-serif; font-size: 36pt; font-weight: bold;
    text-align: center;
  }
  p { margin: 0; }
</style>
</head>
<body><p>%[3]s</p></body>
</html>`, width, height, html.EscapeString(title))
}

// makeTitlePage writes the title HTML to tmpDir, renders it to PDF via soffice,
// and returns the PDF path. uniqueID disambiguates the output filename so that
// repeated titles (or the same workbook used across groups) never overwrite an
// earlier title PDF before it is merged.
func (c *Converter) makeTitlePage(ctx context.Context, title string, a3Landscape bool, tmpDir, uniqueID string) (string, error) {
	safe := titleSafeName.ReplaceAllString(title, "_")
	safe = strings.Trim(safe, "_")
	if safe == "" {
		safe = "title"
	}
	if a3Landscape {
		safe += "_a3_landscape"
	}
	safe = uniqueID + "_" + safe

	htmlPath := filepath.Join(tmpDir, "title_"+safe+".html")
	if err := os.WriteFile(htmlPath, []byte(titlePageHTML(title, a3Landscape)), 0o600); err != nil {
		return "", fmt.Errorf("write title html: %w", err)
	}

	pdfPath, err := c.ConvertToPDF(ctx, htmlPath, tmpDir)
	if err != nil {
		return "", fmt.Errorf("render title page %q: %w", title, err)
	}
	return pdfPath, nil
}
