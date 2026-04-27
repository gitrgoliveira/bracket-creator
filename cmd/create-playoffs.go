package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/excel"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/spf13/cobra"
)

type playoffOptions struct {
	teamMatches     int
	courts          int
	filePath        string
	outputPath      string
	seedsPath       string
	outputWriter    *bufio.Writer
	withZekkenName  bool
	singleTree      bool
	determined      bool
	mirror          bool
	titlePrefix     string
	numberPrefix    string
	SeedAssignments []domain.SeedAssignment
}

func newCreatePlayoffCmd() *cobra.Command {

	o := &playoffOptions{}

	cmd := &cobra.Command{
		Use:          "create-playoffs",
		Short:        "Creates playoff brackets only",
		SilenceUsage: true,
		// Args:         cobra.ExactArgs(1),
		RunE: o.run,
	}

	cmd.Flags().BoolVarP(&o.determined, "determined", "d", false, "Do not shuffle the names read from the input file (default false)")
	cmd.PersistentFlags().StringVarP(&o.filePath, "file", "f", "", "file with the list of players/teams")
	cmd.PersistentFlags().StringVarP(&o.outputPath, "output", "o", "", "output path for the excel file")
	cmd.PersistentFlags().StringVarP(&o.seedsPath, "seeds", "", "", "CSV file mapping exact participant names to their initial seed rank")
	cmd.Flags().BoolVarP(&o.withZekkenName, "with-zekken-name", "z", false, "Use the second column of the input CSV as the participant's display name on the zekken. Falls back to sanitized name if empty.")
	cmd.Flags().BoolVarP(&o.singleTree, "single-tree", "", false, "Create a single tree instead of dividing into multiple sheets (default false)")
	cmd.Flags().IntVarP(&o.teamMatches, "team-matches", "t", 0, "create team matches with x players per team (default 0)")
	cmd.Flags().IntVarP(&o.courts, "courts", "c", 2, "number of Shiaijo (courts) to distribute tree pages across (default 2)")
	cmd.Flags().BoolVarP(&o.mirror, "mirror", "", true, "Mirror match sides (White on left, Red on right) (default true)")
	cmd.Flags().StringVarP(&o.titlePrefix, "title-prefix", "", "", "title prefix for the tournament (default \"\")")
	cmd.Flags().StringVarP(&o.numberPrefix, "number-prefix", "n", "", "Assign consecutive numbers with this letter prefix (e.g. 'K' produces K1, K2, ...)")

	if err := cmd.MarkPersistentFlagRequired("file"); err != nil {
		fmt.Fprintf(os.Stderr, "Error marking file flag as required: %v\n", err)
	}
	if err := cmd.MarkPersistentFlagRequired("output"); err != nil {
		fmt.Fprintf(os.Stderr, "Error marking output flag as required: %v\n", err)
	}

	return cmd
}

func (o *playoffOptions) run(cmd *cobra.Command, args []string) error {
	fmt.Printf("Reading file: %s\n", o.filePath)

	entries, err := helper.ReadEntriesFromFile(o.filePath)
	if err != nil {
		return fmt.Errorf("failed to read entries from file: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("no entries found in file")
	}

	if err := helper.ValidateCourts(o.courts); err != nil {
		return err
	}

	outputFile, outputWriter, err := openOutputFile(o.outputPath)
	if err != nil {
		return err
	}
	defer func() {
		if err := outputFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing output file: %v\n", err)
		}
	}()
	o.outputWriter = outputWriter
	defer func() {
		if err := o.outputWriter.Flush(); err != nil {
			fmt.Fprintf(os.Stderr, "Error flushing output buffer: %v\n", err)
		}
	}()

	err = o.createPlayoffs(entries)
	if err != nil {
		return fmt.Errorf("failed to create playoffs: %w", err)
	}

	fmt.Println("Excel file created successfully:", o.outputPath)
	return nil
}

func (o *playoffOptions) createPlayoffs(entries []string) error {
	players, err := processEntries(entries, o.determined, o.withZekkenName)
	if err != nil {
		return err
	}

	if o.seedsPath != "" {
		fmt.Printf("Parsing seeds file: %s\n", o.seedsPath)
		assignments, err := helper.ParseSeedsFile(o.seedsPath)
		if err != nil {
			return fmt.Errorf("failed to parse seeds file: %w", err)
		}
		o.SeedAssignments = append(o.SeedAssignments, assignments...)
	}

	if len(o.SeedAssignments) > 0 {
		err := helper.ApplySeeds(players, o.SeedAssignments)
		if err != nil {
			return fmt.Errorf("failed to apply seeds: %w", err)
		}
	}

	f, err := excel.NewFileFromScratch()
	if err != nil {
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	if o.withZekkenName {
		fmt.Println("Using Zekken names")
	}

	if o.numberPrefix != "" {
		assignPlayerNumbers(players, o.numberPrefix, 1)
	}

	helper.AddPlayerDataToSheet(f, players, o.withZekkenName, o.titlePrefix)

	// Reorder players based on seeds for standard bracket distribution
	players = helper.StandardSeeding(players)

	// gather all player names
	var names []string
	if o.withZekkenName {
		for _, player := range players {
			names = append(names, player.DisplayName)
		}
	} else {
		for _, player := range players {
			names = append(names, player.Name)
		}
	}
	fmt.Printf("There will be %d finalists\n", len(names))

	maxPlayersPerTree := helper.MaxPlayersPerTree
	numPages, err := helper.RoundToPowerOf2(float64(len(names)), float64(maxPlayersPerTree))
	if err != nil {
		return err
	}
	if numPages < 1 || o.singleTree {
		numPages = 1
	}
	// Apply default for courts if unset
	if o.courts < 1 {
		o.courts = 2
	}
	// Clamp courts to actual number of tree pages
	if o.courts > numPages {
		o.courts = numPages
	}
	// Ensure enough tree pages for the number of courts
	if courtPages := helper.NextPow2(o.courts); courtPages > numPages {
		numPages = courtPages
	}
	fmt.Printf("Spread across %d tree pages\n", numPages)

	// Create balanced tree
	tree := helper.CreateBalancedTree(names)

	// divide the tree depending on the number of pages
	subtrees := helper.SubdivideTree(tree, numPages)

	treeSheet, err := f.GetSheetIndex(helper.SheetTree)
	if err != nil {
		return fmt.Errorf("could not find Tree sheet: %w", err)
	}

	// adding extra sheets
	for i := 0; i < len(subtrees); i++ {
		subtreeSheet := "Tree " + strconv.Itoa(i+1)
		fmt.Printf("Adding sheet %s\n", subtreeSheet)
		index, err := f.NewSheet(subtreeSheet)
		if err != nil {
			return fmt.Errorf("failed to create sheet %s: %w", subtreeSheet, err)
		}
		err = f.CopySheet(treeSheet, index)
		if err != nil {
			return fmt.Errorf("failed to copy sheet %d to %s: %w", treeSheet, subtreeSheet, err)
		}

		depth := helper.CalculateDepth(subtrees[i])
		fmt.Printf("With tree Depth: %d\n", depth)
		startRow := helper.TreeTitleRows + 1
		// Group consecutive tree sheets under the same Shiaijo label
		pagesPerCourt := len(subtrees) / o.courts
		if pagesPerCourt > 0 {
			courtIndex := i / pagesPerCourt
			if courtIndex >= o.courts {
				courtIndex = o.courts - 1
			}
			courtLabel := string("ABCDEFGHIJKLMNOPQRSTUVWXYZ"[courtIndex])
			helper.SetTreeSheetTitle(f, subtreeSheet, "Shiaijo "+courtLabel)
		}

		helper.PrintLeafNodes(subtrees[i], f, subtreeSheet, depth*2, startRow, depth, false, nil)
	}
	if err := f.DeleteSheet(helper.SheetTree); err != nil {
		// Ignore sheet not exist error
		fmt.Println("Note: Tree sheet might not exist:", err)
	}

	depth := helper.CalculateDepth(tree)
	eliminationMatchRounds := make([][]*helper.Node, depth-1)
	// Get all the rounds
	for i := depth; i > 1; i-- {
		rounds := helper.TraverseRounds(tree, 1, i-1)
		eliminationMatchRounds[depth-i] = rounds
		fmt.Printf("Elimination matches for round %d: %d\n", i-1, len(eliminationMatchRounds[depth-i]))
	}

	helper.FillInMatches(f, eliminationMatchRounds)

	var matchWinners map[string]helper.MatchWinner
	if err := f.DeleteSheet(helper.SheetPoolDraw); err != nil {
		// Ignore sheet not exist error
		fmt.Println("Note: Pool Draw sheet might not exist:", err)
	}
	if err := f.DeleteSheet(helper.SheetPoolMatches); err != nil {
		// Ignore sheet not exist error
		fmt.Println("Note: Pool Matches sheet might not exist:", err)
	}

	// Convert all players for match-winner processing
	matchWinners = helper.ConvertPlayersToWinners(players, o.withZekkenName)
	helper.CreateNamesToPrint(f, players, o.withZekkenName)

	helper.PrintTeamEliminationMatches(f, matchWinners, eliminationMatchRounds, o.teamMatches, o.courts, o.mirror)
	helper.FillEstimations(f, 0, 0, int64(o.teamMatches), int64(len(names)-1), o.courts)

	// Save the spreadsheet file
	err = f.Write(o.outputWriter)
	if err != nil {
		return fmt.Errorf("error writing to buffer: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(newCreatePlayoffCmd())
}
