package mobileapp

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/pdf"
)

// printTypeList is the human-readable list of valid :type values for the
// 400 error message. It is derived from pdf.Groups (plus "all") at init
// time so it stays in sync automatically if groups are added or renamed.
var printTypeList = func() string {
	names := make([]string, 0, len(pdf.Groups)+1)
	for _, g := range pdf.Groups {
		names = append(names, g.Type)
	}
	names = append(names, "all")
	return strings.Join(names, ", ")
}()

// RegisterPrintHandlers wires the admin-gated PDF export endpoint under r.
// Route: POST /api/print/:type
//
// Valid :type values are the Type fields of pdf.Groups (e.g. "registration",
// "names", "tags", "pools-trees", "full-bracket") plus the meta-selector "all".
// The set is derived from pdf.Groups at call time so it never drifts.
//
// The handler is synchronous ,  PDF generation via LibreOffice takes 30–60 s
// for a typical tournament. That is acceptable for an admin-initiated,
// one-at-a-time operation. Concurrency is bounded by the package-level
// sofficeMu mutex in internal/pdf, which serialises every soffice invocation
// (pdf.Converter.ConvertToPDF); no additional queue is needed here.
func RegisterPrintHandlers(r *gin.RouterGroup, eng *engine.Engine) {
	r.POST("/print/:type", func(c *gin.Context) {
		printType := c.Param("type")

		// Validate :type against the canonical pdf.Groups list (plus "all").
		// Deriving the check from pdf.Groups avoids the list drifting if a
		// group is ever added, removed, or renamed. The error message uses
		// printTypeList (also derived from pdf.Groups) for the same reason.
		if printType != "all" {
			if _, ok := pdf.GroupByType(printType); !ok {
				c.JSON(http.StatusBadRequest, gin.H{
					"error": fmt.Sprintf(
						"unknown print type %q; valid values: %s",
						printType, printTypeList,
					),
				})
				return
			}
		}

		// Acquire the PDF generator, detecting LibreOffice availability.
		gen, err := pdf.NewGenerator()
		if err != nil {
			if errors.Is(err, pdf.ErrSofficeNotFound) {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": "PDF generation requires LibreOffice. " +
						"Pull the bracket-creator-mobile-pdf image, or install LibreOffice locally and " +
						"ensure 'soffice' is on PATH (or set $LIBREOFFICE_PATH).",
				})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("initialise PDF generator: %s", err.Error())})
			return
		}

		// Create a temporary working directory for XLSX exports and PDF output.
		workDir, err := os.MkdirTemp("", "bracket-print-*")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("create work dir: %s", err.Error())})
			return
		}
		defer func() { _ = os.RemoveAll(workDir) }()

		// Export all competitions to XLSX workbooks.
		sources, err := eng.ExportTournamentWorkbooks(workDir)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("export workbooks: %s", err.Error())})
			return
		}

		// Generate PDFs ,  either all groups or the single requested group.
		var produced map[string]string
		if printType == "all" {
			produced, err = gen.GenerateAll(c.Request.Context(), sources, workDir)
		} else {
			produced, err = gen.GenerateGroups(c.Request.Context(), []string{printType}, sources, workDir)
		}
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("generate PDFs: %s", err.Error())})
			return
		}

		if len(produced) == 0 {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error": "no PDF pages were produced; the tournament may have no competitions started, or the requested type has no matching sheets",
			})
			return
		}

		// Emit ZIP entries in a stable order (pdf.Groups order) so the archive
		// is deterministic across runs; `produced` is a map and would otherwise
		// iterate in random order.
		ordered := make([]string, 0, len(produced))
		for _, g := range pdf.Groups {
			if p, ok := produced[g.Type]; ok {
				ordered = append(ordered, p)
			}
		}

		// Stream a ZIP archive containing the produced PDFs directly into the
		// response. No intermediate ZIP file is written to disk.
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="tournament-pdfs-%s.zip"`, printType))
		c.Status(http.StatusOK)

		zw := zip.NewWriter(c.Writer)
		for _, pdfPath := range ordered {
			if err := streamPDFIntoZip(zw, pdfPath); err != nil {
				// The 200 status + headers are already committed, so we cannot
				// switch to an error response. Record the error on the context
				// for server logs and abort; the truncated ZIP signals failure
				// to the client.
				_ = c.Error(err)
				_ = zw.Close()
				return
			}
		}
		if closeErr := zw.Close(); closeErr != nil {
			// Archive already partially delivered; record for server logs.
			_ = c.Error(fmt.Errorf("close zip writer: %w", closeErr))
			return
		}
	})
}

// streamPDFIntoZip adds one PDF to the ZIP, copying it with io.Copy so a large
// PDF is never fully buffered in memory.
func streamPDFIntoZip(zw *zip.Writer, pdfPath string) error {
	f, err := os.Open(pdfPath) // #nosec G304 -- pdfPath is an internally-generated PDF in a temp dir.
	if err != nil {
		return fmt.Errorf("open generated pdf %s: %w", pdfPath, err)
	}
	defer func() { _ = f.Close() }()

	entry, err := zw.Create(filepath.Base(pdfPath))
	if err != nil {
		return fmt.Errorf("create zip entry for %s: %w", pdfPath, err)
	}
	if _, err := io.Copy(entry, f); err != nil {
		return fmt.Errorf("write zip entry for %s: %w", pdfPath, err)
	}
	return nil
}
