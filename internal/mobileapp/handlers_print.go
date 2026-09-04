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

// printTypeList is the human-readable list of valid: type values for the
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
// Valid: type values are the Type fields of pdf.Groups (e.g. "registration",
// "names", "tags", "pools-trees", "full-bracket") plus the meta-selector "all".
// The set is derived from pdf.Groups at call time so it never drifts.
//
// The handler is synchronous, PDF generation via LibreOffice takes 30–60 s
// for a typical tournament. That is acceptable for an admin-initiated,
// one-at-a-time operation. Concurrency is bounded by the package-level
// sofficeMu mutex in internal/pdf, which serialises every soffice invocation
// (pdf.Converter.ConvertToPDF); no additional queue is needed here.
func RegisterPrintHandlers(r *gin.RouterGroup, eng *engine.Engine) {
	r.POST("/print/:type", func(c *gin.Context) {
		printType := c.Param("type")

		// Validate: type against the canonical pdf.Groups list (plus "all").
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
			internalError(c, err, "initialise PDF generator")
			return
		}

		// Create a temporary working directory for XLSX exports and PDF output.
		workDir, err := os.MkdirTemp("", "bracket-print-*")
		if err != nil {
			internalError(c, err, "create work dir")
			return
		}
		defer func() { _ = os.RemoveAll(workDir) }()

		// Export all competitions to XLSX workbooks. A Swiss competition (no
		// static bracket; Swiss export is not yet implemented -- mp-4n9n) or
		// one whose stored bracket no longer matches its current settings
		// (engine.ErrBracketDrawMismatch) is skipped rather than aborting the
		// whole booklet; skipped is reported to the operator below via both a
		// response header and a text entry inside the ZIP, since a streamed
		// application/zip response has no JSON body to carry a warning in.
		sources, skipped, err := eng.ExportTournamentWorkbooks(workDir)
		if err != nil {
			internalError(c, err, "export workbooks")
			return
		}

		// Every competition was skipped (e.g. an all-Swiss tournament): there
		// is nothing left to hand to the PDF generator, which would otherwise
		// fail with "no source workbooks provided" and surface as a
		// misleading 500. len(sources)==0 means every competition that DID
		// exist was skipped -- see ExportTournamentWorkbooks' documented
		// contract, which owns that invariant -- so report why, per
		// competition, as a 422 instead of falling through to generation.
		if len(sources) == 0 {
			c.JSON(http.StatusUnprocessableEntity, gin.H{
				"error":   "no competitions could be exported; every competition was skipped",
				"skipped": skippedCompetitionsDetail(skipped),
			})
			return
		}

		// Generate PDFs, either all groups or the single requested group.
		var produced map[string]string
		if printType == "all" {
			produced, err = gen.GenerateAll(c.Request.Context(), sources, workDir)
		} else {
			produced, err = gen.GenerateGroups(c.Request.Context(), []string{printType}, sources, workDir)
		}
		if err != nil {
			internalError(c, err, "generate PDFs")
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
		if len(skipped) > 0 {
			// Header form for any programmatic caller that inspects the
			// response before unzipping. The ZIP entry written below is what
			// an operator actually sees when they open the archive, so it
			// carries the same information again in a form that survives
			// download/re-upload/attach, where headers do not.
			c.Header("X-Skipped-Competitions", skippedCompetitionsHeaderValue(skipped))
		}
		c.Status(http.StatusOK)

		zw := zip.NewWriter(c.Writer)
		if len(skipped) > 0 {
			if err := writeSkippedCompetitionsEntry(zw, skipped); err != nil {
				_ = c.Error(err)
				_ = zw.Close()
				return
			}
		}
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

// skippedCompetitionsDetail renders the skipped list for the 422 JSON error
// body returned when every competition was skipped. Unlike the
// X-Skipped-Competitions header (see skippedCompetitionsHeaderValue), a JSON
// response body has no ASCII constraint, so the full UTF-8 competition name
// is safe to include here directly.
func skippedCompetitionsDetail(skipped []engine.SkippedCompetition) []gin.H {
	details := make([]gin.H, 0, len(skipped))
	for _, s := range skipped {
		details = append(details, gin.H{
			"id":     s.ID,
			"name":   s.Name,
			"reason": s.Reason,
		})
	}
	return details
}

// skippedCompetitionsHeaderValue renders the skipped-competition list as a
// single HTTP header value, entries joined by " | " (header values cannot
// carry newlines). The delimiter is deliberately NOT "; ": our own sentinel
// reason strings (engine.ErrSwissExportUnsupported,
// engine.ErrBracketDrawMismatch) contain semicolons, so joining on "; "
// would let a single reason parse back as two entries and silently drop
// the tail on the JS side. No reason string may contain "|" either --
// TestSentinelReasonsContainNoDelimiterPipe pins that.
//
// It carries the competition ID, never the Name. Kendo competition names
// routinely contain kanji or accented characters, and Go does not reject
// them here -- httpguts.ValidHeaderFieldValue permits bytes >= 0x80, so a
// non-ASCII name would pass validation -- but HTTP clients decode header
// values as latin-1, so UTF-8 bytes would arrive as mojibake rather than an
// error. Competition IDs are guaranteed ASCII (state.validIDPattern,
// enforced by state.ValidateCompetitionID), so they round-trip safely
// through a header. The full UTF-8 name has no such constraint and is
// preserved instead in the SKIPPED-COMPETITIONS.txt ZIP entry (see
// writeSkippedCompetitionsEntry).
func skippedCompetitionsHeaderValue(skipped []engine.SkippedCompetition) string {
	parts := make([]string, 0, len(skipped))
	for _, s := range skipped {
		parts = append(parts, fmt.Sprintf("%s: %s", s.ID, s.Reason))
	}
	return strings.Join(parts, " | ")
}

// writeSkippedCompetitionsEntry adds a SKIPPED-COMPETITIONS.txt entry to the
// ZIP naming every competition ExportTournamentWorkbooks left out of the
// booklet and why. This is the entry an operator actually reads: the ZIP is
// the only payload a streamed application/zip response carries, so a header
// alone would be invisible to anyone who just downloads and opens the file.
//
// No generic trailing explanation is appended: each entry's Reason IS the
// sentinel's own message (engine.ErrSwissExportUnsupported or
// engine.ErrBracketDrawMismatch), and both are already written to be
// operator-actionable on their own -- a shared paragraph describing only one
// of the two possible causes would mislabel the other whenever both kinds of
// skip land in the same booklet.
func writeSkippedCompetitionsEntry(zw *zip.Writer, skipped []engine.SkippedCompetition) error {
	var body strings.Builder
	body.WriteString("The following competitions were NOT included in this export:\n\n")
	for _, s := range skipped {
		fmt.Fprintf(&body, "- %s (%s): %s\n", s.Name, s.ID, s.Reason)
	}

	entry, err := zw.Create("SKIPPED-COMPETITIONS.txt")
	if err != nil {
		return fmt.Errorf("create zip entry for SKIPPED-COMPETITIONS.txt: %w", err)
	}
	if _, err := io.WriteString(entry, body.String()); err != nil {
		return fmt.Errorf("write zip entry for SKIPPED-COMPETITIONS.txt: %w", err)
	}
	return nil
}
