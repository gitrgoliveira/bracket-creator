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
// one-at-a-time operation. Concurrency is bounded by the process-wide mutex
// inside pdf.Generator (soffice serialises workbook conversion); no
// additional queue is needed here.
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

		// Stream a ZIP archive containing the produced PDFs directly into the
		// response. No intermediate ZIP file is written to disk.
		c.Header("Content-Type", "application/zip")
		c.Header("Content-Disposition", `attachment; filename="tournament-pdfs.zip"`)
		c.Status(http.StatusOK)

		zw := zip.NewWriter(c.Writer)
		for _, pdfPath := range produced {
			data, readErr := os.ReadFile(pdfPath) // #nosec G304 -- pdfPath is an internally-generated PDF in a temp dir.
			if readErr != nil {
				// Header already written — log and skip rather than leaving a
				// half-written ZIP. The client will detect a malformed archive.
				_ = zw.Close()
				return
			}
			entry, createErr := zw.Create(filepath.Base(pdfPath))
			if createErr != nil {
				_ = zw.Close()
				return
			}
			if _, writeErr := entry.Write(data); writeErr != nil {
				_ = zw.Close()
				return
			}
		}
		if closeErr := zw.Close(); closeErr != nil {
			// Archive already partially delivered; nothing more we can do.
			return
		}
	})
}
