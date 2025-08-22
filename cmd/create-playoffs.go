package cmd

import (
	"bufio"
	"fmt"
	"math/rand"
	"os"

	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/spf13/cobra"

	excelize "github.com/xuri/excelize/v2"
)

type playoffOptions struct {
	teamMatches  int
	filePath     string
	outputPath   string
	outputWriter *bufio.Writer
	sanitize     bool
	singleTree   bool
	determined   bool
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
	cmd.Flags().BoolVarP(&o.sanitize, "sanitize", "s", false, "sanitize names into first and last name and capitalize (default false)")
	cmd.Flags().BoolVarP(&o.singleTree, "single-tree", "", false, "Create a single tree instead of dividing into multiple sheets (default false)")
	cmd.Flags().IntVarP(&o.teamMatches, "team-matches", "t", 0, "create team matches with x players per team (default 0)")

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

	outputFile, err := os.OpenFile(o.outputPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("failed to open output file: %w", err)
	}
	defer func() {
		if err := outputFile.Close(); err != nil {
			fmt.Fprintf(os.Stderr, "Error closing output file: %v\n", err)
		}
	}()

	o.outputWriter = bufio.NewWriter(outputFile)
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

	entries = helper.RemoveDuplicates(entries)

	// Shuffle all entries
	if !o.determined {
		rand.Shuffle(len(entries), func(i, j int) {
			entries[i], entries[j] = entries[j], entries[i]
		})
	}

	players := helper.CreatePlayers(entries)

	// Openning the template Excel file.
	templateFile, err := helper.TemplateFile.Open("template.xlsx")
	if err != nil {
		fmt.Println(err)
		return err
	}

	f, err := excelize.OpenReader(templateFile)
	if err != nil {
		fmt.Println(err)
		return err
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	if o.sanitize {
		fmt.Println("Sanitizing names")
	}

	helper.AddPlayerDataToSheet(f, players, o.sanitize)
	// gather all player names
	var names []string
	if o.sanitize {
		for _, player := range players {
			names = append(names, player.DisplayName)
		}
	} else {
		for _, player := range players {
			names = append(names, player.Name)
		}
	}
	fmt.Printf("There will be %d finalists\n", len(names))

	maxPlayersPerTree := 16
	numPages := helper.RoundToPowerOf2(float64(len(names)), float64(maxPlayersPerTree))
	if numPages < 1 || o.singleTree {
		numPages = 1
	}
	fmt.Printf("Spread across %d tree pages\n", numPages)

	// Create balanced tree
	tree := helper.CreateBalancedTree(names, o.sanitize)

	// divide the tree depending on the number of pages
	subtrees := helper.SubdivideTree(tree, numPages)

	treeSheet, err := f.GetSheetIndex("Tree")
	if err != nil {
		fmt.Println("Could not find Tree sheet")
		fmt.Println(err)
		return err
	}

	// adding extra sheets
	for i := 0; i < len(subtrees); i++ {
		subtreeSheet := "Tree " + strconv.Itoa(i+1)
		fmt.Printf("Adding sheet %s\n", subtreeSheet)
		index, _ := f.NewSheet(subtreeSheet)
		err = f.CopySheet(treeSheet, index)
		if err != nil {
			fmt.Printf("Could not copy sheet %d\n", treeSheet)
			fmt.Println(err)
			return err
		}

		depth := helper.CalculateDepth(subtrees[i])
		fmt.Printf("With tree Depth: %d\n", depth)
		startRow := 1
		helper.PrintLeafNodes(subtrees[i], f, subtreeSheet, depth*2, startRow, depth, false)
		helper.PrintLeafNodes(subtrees[i], f, subtreeSheet, depth*2, startRow, depth, false)
	}
	if err := f.DeleteSheet("Tree"); err != nil {
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
	if err := f.DeleteSheet("Pool Draw"); err != nil {
		// Ignore sheet not exist error
		fmt.Println("Note: Pool Draw sheet might not exist:", err)
	}
	if err := f.DeleteSheet("Pool Matches"); err != nil {
		// Ignore sheet not exist error
		fmt.Println("Note: Pool Matches sheet might not exist:", err)
	}

	// hurray! they are all winners
	matchWinners = helper.ConvertPlayersToWinners(players, o.sanitize)
	helper.CreateNamesToPrint(f, players, o.sanitize)

	helper.PrintTeamEliminationMatches(f, matchWinners, eliminationMatchRounds, o.teamMatches)
	helper.FillEstimations(f, 0, 0, 0, int64(o.teamMatches), int64(len(names)-1))

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
