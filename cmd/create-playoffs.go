package cmd

import (
	"bufio"
	"fmt"
	"log"
	"math/rand"
	"os"

	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/spf13/cobra"

	"github.com/xuri/excelize/v2"
)

type createPlayoffOptions struct {
	teamMatches int
	filePath    string
	outputPath  string
	sanatize    bool
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

	cmd.Flags().BoolVarP(&o.determined, "determined", "d", false, "Do not shuffle the names read from the input file")
	cmd.PersistentFlags().StringVarP(&o.filePath, "file", "f", "", "file with the list of players/teams")
	cmd.PersistentFlags().StringVarP(&o.outputPath, "output", "o", "", "output path for the excel file")
	cmd.Flags().BoolVarP(&o.sanatize, "sanatize", "s", false, "Sanatize names into first and last name and capitalize")
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
	tree := helper.CreateBalancedTree(names, o.sanatize)

	depth := helper.CalculateDepth(tree)
	fmt.Printf("Tree Depth: %d\n", depth)
	helper.PrintLeafNodes(tree, f, "Tree", depth*2, 4, depth, false)

	// gathers a list of all of the matches
	matches := helper.InOrderTraversal(tree)
	matchMapping := helper.FillInMatches(f, matches)
	eliminationMatchRounds := make([][]helper.EliminationMatch, depth-1)
	// Get all the rounds
	for i := depth; i > 1; i-- {
		rounds := helper.TraverseRounds(tree, 1, i-1, matchMapping)
		eliminationMatchRounds[depth-i] = rounds
		fmt.Printf("Elimination matches for round %d: %d\n", i-1, len(eliminationMatchRounds[depth-i]))
	}

	var matchWinners map[string]helper.MatchWinner
	f.DeleteSheet("Pool Draw")
	f.DeleteSheet("Pool Matches")
	// hurray! they are all winners
	matchWinners = helper.ConvertPlayersToWinners(players, o.sanatize)
	helper.CreateNamesToPrint(f, players, o.sanatize)

	helper.PrintTeamEliminationMatches(f, matchWinners, matchMapping, eliminationMatchRounds, o.teamMatches)

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
