package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/gitrgoliveira/bracket-creator/internal/engine"
	"github.com/gitrgoliveira/bracket-creator/internal/pdf"
	"github.com/gitrgoliveira/bracket-creator/internal/state"
	"github.com/spf13/cobra"
)

type printOptions struct {
	pdfType        string
	inputDir       string
	tournamentData string
	output         string
	outputDir      string
	teamFiles      []string
}

func newPrintCmd() *cobra.Command {
	o := &printOptions{}

	cmd := &cobra.Command{
		Use:   "print",
		Short: "Render bracket XLSX workbooks to print-ready grouped PDFs",
		Long: `Render the sheets of one or more bracket XLSX workbooks into grouped,
print-ready PDFs using LibreOffice.

Input modes (exactly one required):
  --input <dir>             directory containing pre-existing bracket XLSX files
  --tournament-data <dir>   live mobile-app tournament-data directory; workbooks
                            are generated on the fly from competition state

Types:
  registration   the "data" sheet from every workbook
  names          the "Names to Print" sheet(s), A3 landscape, with title pages
  tags           the "Tags" sheet(s), with title pages (team workbooks excluded)
  pools-trees    "Pool Draw" + "Tree" sheets (participant booklet), page-numbered
  full-bracket   Pool Draw + Pool/Elimination Matches + Trees, page-numbered
  all            produce all of the above into --output-dir

Examples:
  bracket-creator print --type=all --input=./xlsx/ --output-dir=./pdfs
  bracket-creator print --type=all --tournament-data=tournament-data/ --output-dir=./pdfs

LibreOffice (soffice) must be installed. If it is not found, this command
exits with installation instructions.`,
		SilenceUsage: true,
		RunE:         o.run,
	}

	cmd.Flags().StringVar(&o.pdfType, "type", "", "PDF type: registration|names|tags|pools-trees|full-bracket|all (required)")
	cmd.Flags().StringVar(&o.inputDir, "input", "", "directory containing the bracket XLSX files")
	cmd.Flags().StringVar(&o.tournamentData, "tournament-data", "", "live mobile-app tournament-data directory (mutually exclusive with --input)")
	cmd.Flags().StringVarP(&o.output, "output", "o", "", "output PDF path (single --type)")
	cmd.Flags().StringVar(&o.outputDir, "output-dir", "", "output directory (--type=all, or to use default filenames)")
	cmd.Flags().StringSliceVar(&o.teamFiles, "team-file", nil, "XLSX filename (basename) to treat as a team workbook; excluded from tags. Repeatable. Defaults to any filename containing 'team'.")

	if err := cmd.MarkFlagRequired("type"); err != nil {
		fmt.Fprintf(os.Stderr, "Error marking type flag as required: %v\n", err)
	}

	return cmd
}

func (o *printOptions) run(cmd *cobra.Command, args []string) error {
	// Validate mutually exclusive input modes.
	if o.inputDir == "" && o.tournamentData == "" {
		return fmt.Errorf("provide exactly one of --input <dir> or --tournament-data <dir>")
	}
	if o.inputDir != "" && o.tournamentData != "" {
		return fmt.Errorf("--input and --tournament-data are mutually exclusive; provide exactly one")
	}

	if o.pdfType != "all" {
		if _, ok := pdf.GroupByType(o.pdfType); !ok {
			return fmt.Errorf("unknown --type %q (want registration|names|tags|pools-trees|full-bracket|all)", o.pdfType)
		}
	}

	var sources []pdf.SourceWorkbook
	var sourcesLabel string // used in error messages
	var tempDir string

	if o.inputDir != "" {
		var err error
		sources, err = collectWorkbooks(o.inputDir, o.teamFiles)
		if err != nil {
			return err
		}
		sourcesLabel = o.inputDir
	} else {
		// --tournament-data mode: build store+engine, export workbooks to a temp dir.
		store, err := state.NewStore(o.tournamentData)
		if err != nil {
			if hint := diagnoseFolderError(o.tournamentData); hint != "" {
				return fmt.Errorf("failed to initialize state store at %q: %w\n%s", o.tournamentData, err, hint)
			}
			return fmt.Errorf("failed to initialize state store at %q: %w", o.tournamentData, err)
		}
		eng := engine.New(store)

		tempDir, err = os.MkdirTemp("", "bracket-creator-print-*")
		if err != nil {
			return fmt.Errorf("create temp dir: %w", err)
		}
		defer func() { _ = os.RemoveAll(tempDir) }()

		sources, err = eng.ExportTournamentWorkbooks(tempDir)
		if err != nil {
			return fmt.Errorf("export tournament workbooks: %w", err)
		}
		sourcesLabel = o.tournamentData
	}

	gen, err := pdf.NewGenerator()
	if err != nil {
		if errors.Is(err, pdf.ErrSofficeNotFound) {
			return fmt.Errorf("PDF generation requires LibreOffice.\n  Install it with: brew install --cask libreoffice\n  or set $LIBREOFFICE_PATH to the soffice binary.\n(%w)", err)
		}
		return err
	}

	return o.generatePDFs(cmd, gen, sources, sourcesLabel)
}

// generatePDFs is the shared tail: given a generator and sources, run the
// appropriate GenerateAll or GenerateGroups call and report outputs.
func (o *printOptions) generatePDFs(cmd *cobra.Command, gen *pdf.Generator, sources []pdf.SourceWorkbook, sourcesLabel string) error {
	ctx := context.Background()

	if o.pdfType == "all" {
		outDir := o.outputDir
		if outDir == "" {
			return fmt.Errorf("--type=all requires --output-dir")
		}
		out, err := gen.GenerateAll(ctx, sources, outDir)
		if err != nil {
			return err
		}
		reportOutputs(cmd, out)
		return nil
	}

	// Single type. Produce into a scratch dir under the destination, then move
	// to the requested --output (or default filename in --output-dir).
	if o.output == "" && o.outputDir == "" {
		return fmt.Errorf("provide --output <file> or --output-dir <dir>")
	}
	destDir := o.outputDir
	if destDir == "" {
		destDir = filepath.Dir(o.output)
	}
	out, err := gen.GenerateGroups(ctx, []string{o.pdfType}, sources, destDir)
	if err != nil {
		return err
	}
	produced, ok := out[o.pdfType]
	if !ok {
		return fmt.Errorf("no pages produced for --type=%s from %s (no matching sheets)", o.pdfType, sourcesLabel)
	}
	if o.output != "" && o.output != produced {
		if err := os.Rename(produced, o.output); err != nil {
			return fmt.Errorf("move output to %s: %w", o.output, err)
		}
		produced = o.output
	}
	reportOutputs(cmd, map[string]string{o.pdfType: produced})
	return nil
}

// collectWorkbooks enumerates *.xlsx in dir (sorted), marking team workbooks
// either by explicit --team-file basenames or, by default, any filename whose
// basename contains "team" (case-insensitive). Temporary Excel lock files
// (~$...) are ignored.
func collectWorkbooks(dir string, teamFiles []string) ([]pdf.SourceWorkbook, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read input dir %s: %w", dir, err)
	}
	teamSet := make(map[string]bool, len(teamFiles))
	for _, t := range teamFiles {
		teamSet[t] = true
	}

	var sources []pdf.SourceWorkbook
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasPrefix(name, "~$") || !strings.EqualFold(filepath.Ext(name), ".xlsx") {
			continue
		}
		abs, err := filepath.Abs(filepath.Join(dir, name))
		if err != nil {
			return nil, err
		}
		isTeam := teamSet[name] || strings.Contains(strings.ToLower(name), "team")
		sources = append(sources, pdf.SourceWorkbook{Path: abs, IsTeam: isTeam})
	}
	if len(sources) == 0 {
		return nil, fmt.Errorf("no .xlsx files found in %s", dir)
	}
	sort.Slice(sources, func(i, j int) bool { return sources[i].Path < sources[j].Path })
	return sources, nil
}

func reportOutputs(cmd *cobra.Command, out map[string]string) {
	types := make([]string, 0, len(out))
	for t := range out {
		types = append(types, t)
	}
	sort.Strings(types)
	for _, t := range types {
		_, _ = fmt.Fprintf(cmd.OutOrStdout(), "wrote %s → %s\n", t, out[t])
	}
}

func init() {
	rootCmd.AddCommand(newPrintCmd())
}
