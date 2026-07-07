package mobileapp

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/export"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
)

// RegisterExportResultsHandlers wires the admin-gated results-export endpoint.
//
// Route: GET /api/competitions/:id/export-results
//
// This streams a RESULTS-populated workbook (played scores, standings, winners,
// and decision suffixes written as literal values) built by internal/export.
// It is deliberately DISTINCT from the sibling GET .../export, which renders a
// blank formula template that feeds the PDF pipeline; registering the same path
// twice would panic Gin at startup. Read-only: it loads state and writes
// nothing, so it is safe alongside the stateful admin APIs.
func RegisterExportResultsHandlers(r *gin.RouterGroup, store *state.Store, eng *engine.Engine) {
	r.GET("/competitions/:id/export-results", func(c *gin.Context) {
		id, ok := requireValidCompID(c)
		if !ok {
			return
		}

		data, err := export.BuildResultsWorkbook(store, eng, id)
		if err != nil {
			// Swiss has no static bracket to export; surface a clear 422 rather
			// than a generic 500 so the UI can explain it.
			if errors.Is(err, export.ErrSwissExportUnsupported) {
				c.JSON(http.StatusUnprocessableEntity, gin.H{"error": err.Error()})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		filename := fmt.Sprintf("results-%s.xlsx", id)
		c.Header("Content-Description", "File Transfer")
		c.Header("Content-Disposition", "attachment; filename="+filename)
		c.Data(http.StatusOK, "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", data)
	})
}
