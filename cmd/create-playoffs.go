package cmd

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"os"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/spf13/cobra"

	"github.com/xuri/excelize/v2"
)

type createPlayoffOptions struct {
	teamMatches int
	filePath    string
	outputPath  string
	sanatize    bool
	singleTree  bool
	determined  bool
}

func newCreatePlayoffCmd() *cobra.Command {

	o := &createPlayoffOptions{}

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
	cmd.Flags().BoolVarP(&o.sanatize, "sanatize", "s", false, "Sanatize names into first and last name and capitalize (default false)")
	cmd.Flags().BoolVarP(&o.singleTree, "single-tree", "", false, "Create a single tree instead of dividing into multiple sheets (default false)")
	cmd.Flags().IntVarP(&o.teamMatches, "team-matches", "t", 0, "create team matches with x players per team (default 0)")

	cmd.MarkFlagRequired("file")
	cmd.MarkFlagRequired("output")

	return cmd
}

func (o *createPlayoffOptions) run(cmd *cobra.Command, args []string) error {

	fmt.Fprintf(cmd.OutOrStdout(), "Reading file: %s\n", o.filePath)
	file, err := os.Open(o.filePath)
	if err != nil {
		log.Fatal(err)
	}
	defer file.Close()

	entries := make([]string, 0)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		entry := scanner.Text()
		entries = append(entries, entry)
	}

	entries = helper.RemoveDuplicates(entries)

	// Shuffle all entries
	if !o.determined {
		rand.Shuffle(len(entries), func(i, j int) {
			entries[i], entries[j] = entries[j], entries[i]
		})
	}

	players := helper.CreatePlayers(entries)

	// Openning the template Excel file.
	f, err := excelize.OpenFile("template.xlsx")
	if err != nil {
		fmt.Println(err)
		return nil
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	helper.AddPlayerDataToSheet(f, players, o.sanatize)
	// gather all player names
	var names []string
	if o.sanatize {
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
	tree := helper.CreateBalancedTree(names, o.sanatize)

	// divide the tree depending on the number of pages
	subtrees := helper.SubdivideTree(tree, numPages)

	treeSheet, err := f.GetSheetIndex("Tree")
	if err != nil {
		fmt.Println("Could not find Tree sheet")
		fmt.Println(err)
		return nil
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
			return nil
		}

		depth := helper.CalculateDepth(subtrees[i])
		fmt.Printf("With tree Depth: %d\n", depth)
		helper.PrintLeafNodes(subtrees[i], f, subtreeSheet, depth*2, 0, depth, false)
		helper.PrintLeafNodes(subtrees[i], f, subtreeSheet, depth*2, 0, depth, false)
	}
	f.DeleteSheet("Tree")
	if err != nil {
		fmt.Println("Could not find Tree sheet")
		fmt.Println(err)
		return nil
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
	f.DeleteSheet("Pool Draw")
	f.DeleteSheet("Pool Matches")
	// hurray! they are all winners
	matchWinners = helper.ConvertPlayersToWinners(players, o.sanatize)
	helper.CreateNamesToPrint(f, players, o.sanatize)

	helper.PrintTeamEliminationMatches(f, matchWinners, eliminationMatchRounds, o.teamMatches)
	helper.FillEstimations(f, 0, 0, 0, o.teamMatches, len(names)-1)

	// Save the spreadsheet file
	if err := f.SaveAs(o.outputPath); err != nil {
		fmt.Println("Error saving Excel file:", err)
		return err
	}

	fmt.Println("Excel file created successfully:", o.outputPath)
	return nil
}

func init() {
	rootCmd.AddCommand(newCreatePlayoffCmd())
}
