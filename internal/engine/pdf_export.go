package engine

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/gitrgoliveira/bracket-creator/internal/pdf"
)

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
func (e *Engine) ExportTournamentWorkbooks(tmpDir string, compIDs ...string) ([]pdf.SourceWorkbook, error) {
	ids := compIDs
	if len(ids) == 0 {
		all, err := e.store.ListCompetitions()
		if err != nil {
			return nil, fmt.Errorf("list competitions: %w", err)
		}
		ids = all
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no competitions to export")
	}

	sources := make([]pdf.SourceWorkbook, 0, len(ids))
	for _, id := range ids {
		comp, err := e.store.LoadCompetition(id)
		if err != nil {
			return nil, fmt.Errorf("load competition %s: %w", id, err)
		}
		if comp == nil {
			return nil, notFoundErrorf("competition %s not found", id)
		}

		data, err := e.ExportCompetitionXlsx(id)
		if err != nil {
			return nil, fmt.Errorf("export competition %s: %w", id, err)
		}

		xlsxPath := filepath.Join(tmpDir, id+".xlsx")
		if err := os.WriteFile(xlsxPath, data, 0o600); err != nil {
			return nil, fmt.Errorf("write workbook %s: %w", xlsxPath, err)
		}

		title := comp.Name
		if title == "" {
			title = id
		}
		sources = append(sources, pdf.SourceWorkbook{
			Path:   xlsxPath,
			Title:  title,
			IsTeam: comp.TeamSize > 0 || comp.Kind == "team",
		})
	}
	return sources, nil
}
