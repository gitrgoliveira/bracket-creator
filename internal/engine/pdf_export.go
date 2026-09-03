package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/gitrgoliveira/bracket-creator/internal/pdf"
)

// SkippedCompetition names a competition that ExportTournamentWorkbooks left
// out of a print/export batch, and why, so callers can warn the operator
// instead of silently shipping an incomplete booklet.
type SkippedCompetition struct {
	ID     string
	Name   string
	Reason string
}

// ExportTournamentWorkbooks renders every competition in the tournament to a
// bracket XLSX in tmpDir and returns the corresponding pdf.SourceWorkbook list,
// ready to feed pdf.Generator. This is the bridge from live mobile-app state to
// the PDF pipeline, shared by the CLI `print --tournament-data` mode and the
// mobile-app Export-PDFs endpoint.
//
// Each competition's display name becomes the title-page text; team
// competitions (TeamSize > 0 or Kind == "team") are flagged so the Tags group
// can exclude them. compIDs, when non-empty, restricts export to those
// competitions; otherwise all competitions are exported.
//
// A competition ExportCompetitionXlsx cannot render -- Swiss (no static
// bracket) or a stored bracket that no longer matches the competition's
// current settings (ErrBracketDrawMismatch) -- is SKIPPED rather than
// aborting the whole batch: one such competition in an otherwise printable
// tournament must not make the entire booklet unprintable. Every skipped
// competition is reported in the second return value so both callers
// (cmd/print.go, handlers_print.go) can warn the operator rather than
// silently omitting it.
func (e *Engine) ExportTournamentWorkbooks(tmpDir string, compIDs ...string) ([]pdf.SourceWorkbook, []SkippedCompetition, error) {
	ids := compIDs
	if len(ids) == 0 {
		all, err := e.store.ListCompetitions()
		if err != nil {
			return nil, nil, fmt.Errorf("list competitions: %w", err)
		}
		ids = all
	}
	if len(ids) == 0 {
		return nil, nil, fmt.Errorf("no competitions to export")
	}

	sources := make([]pdf.SourceWorkbook, 0, len(ids))
	var skipped []SkippedCompetition
	for _, id := range ids {
		comp, err := e.store.LoadCompetition(id)
		if err != nil {
			return nil, nil, fmt.Errorf("load competition %s: %w", id, err)
		}
		if comp == nil {
			return nil, nil, notFoundErrorf("competition %s not found", id)
		}

		title := comp.Name
		if title == "" {
			title = id
		}

		data, err := e.ExportCompetitionXlsx(id)
		if err != nil {
			// A competition this pipeline cannot render is SKIPPED, never
			// fatal: one such competition must not make an otherwise
			// printable tournament booklet unprintable. The skip is driven
			// off the sentinel(s) rather than re-testing comp.Format or
			// bracket state here, so ExportCompetitionXlsx stays the SINGLE
			// owner of what is exportable -- a format gaining support
			// (bc-swex) then changes one place, and cannot leave this loop
			// silently skipping a competition the exporter has learned to
			// render. Every skip is returned to the caller, which warns the
			// operator.
			if isUnexportable(err) {
				skipped = append(skipped, SkippedCompetition{
					ID:     id,
					Name:   title,
					Reason: err.Error(),
				})
				continue
			}
			return nil, nil, fmt.Errorf("export competition %s: %w", id, err)
		}

		xlsxPath := filepath.Join(tmpDir, id+".xlsx")
		if err := os.WriteFile(xlsxPath, data, 0o600); err != nil {
			return nil, nil, fmt.Errorf("write workbook %s: %w", xlsxPath, err)
		}

		sources = append(sources, pdf.SourceWorkbook{
			Path:   xlsxPath,
			Title:  title,
			IsTeam: comp.TeamSize > 0 || comp.Kind == "team",
		})
	}
	return sources, skipped, nil
}

// isUnexportable reports whether err is one of the sentinels
// ExportCompetitionXlsx returns for a competition it cannot render at all --
// as opposed to a real failure (I/O, corrupt state) that should abort the
// whole batch. Centralised here so a third such sentinel has exactly one
// place to be added, rather than a new errors.Is call at every skip site.
func isUnexportable(err error) bool {
	return errors.Is(err, ErrSwissExportUnsupported) || errors.Is(err, ErrBracketDrawMismatch)
}
