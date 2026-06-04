package mobileapp

import (
	"archive/zip"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/pdf"
)

// validPrintTypes is the set of accepted :type values for POST /api/print/:type.
// "all" is a meta-selector that maps to every group.
var validPrintTypes = map[string]bool{
	"all":          true,
	"registration": true,
	"names":        true,
	"tags":         true,
	"pools-trees":  true,
	"full-bracket": true,
}

// RegisterPrintHandlers wires the admin-gated PDF export endpoint under r.
// Route: POST /api/print/:type
//
// The handler is synchronous — PDF generation via LibreOffice takes 30–60 s
// for a typical tournament. That is acceptable for an admin-initiated,
// one-at-a-time operation. Concurrency is bounded by the package-level
// sofficeMu mutex in internal/pdf, which serialises every soffice invocation
// (pdf.Converter.ConvertToPDF); no additional queue is needed here.
func RegisterPrintHandlers(r *gin.RouterGroup, eng *engine.Engine) {
	r.POST("/print/:type", func(c *gin.Context) {
		printType := c.Param("type")

		// Validate :type.
		if !validPrintTypes[printType] {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": fmt.Sprintf(
					"unknown print type %q; valid values: registration, names, tags, pools-trees, full-bracket, all",
					printType,
				),
			})
			return
		}

		// Acquire the PDF generator, detecting LibreOffice availability.
		gen, err := pdf.NewGenerator()
		if err != nil {
			if errors.Is(err, pdf.ErrSofficeNotFound) {
				c.JSON(http.StatusServiceUnavailable, gin.H{
					"error": "PDF generation requires LibreOffice. " +
						"Pull the bracket-creator-mobile-pdf image, or install LibreOffice locally " +
						"(brew install --cask libreoffice).",
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

		// Generate PDFs — either all groups or the single requested group.
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
				"error": "no PDF pages were produced; the tournament may have no started competitions or the requested type has no matching sheets",
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
		c.Header("Content-Disposition", `attachment; filename="tournament-pdfs.zip"`)
		c.Status(http.StatusOK)

		zw := zip.NewWriter(c.Writer)
		for _, pdfPath := range ordered {
			data, readErr := os.ReadFile(pdfPath) // #nosec G304 -- pdfPath is an internally-generated PDF in a temp dir.
			if readErr != nil {
				// The 200 status + headers are already committed, so we cannot
				// switch to an error response. Record the error on the context
				// for server logs and abort; the truncated ZIP signals failure
				// to the client.
				_ = c.Error(fmt.Errorf("read generated pdf %s: %w", pdfPath, readErr))
				_ = zw.Close()
				return
			}
			entry, createErr := zw.Create(filepath.Base(pdfPath))
			if createErr != nil {
				_ = c.Error(fmt.Errorf("create zip entry for %s: %w", pdfPath, createErr))
				_ = zw.Close()
				return
			}
			if _, writeErr := entry.Write(data); writeErr != nil {
				_ = c.Error(fmt.Errorf("write zip entry for %s: %w", pdfPath, writeErr))
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
