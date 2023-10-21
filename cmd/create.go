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

type createOptions struct {
	numPlayers int
	filePath   string
	outputPath string
	roundRobin bool
	sanatize   bool
}

func newCreateCmd() *cobra.Command {

	o := &createOptions{}

	cmd := &cobra.Command{
		Use:          "create",
		Short:        "subcommand to create brackets",
		SilenceUsage: true,
		// Args:         cobra.ExactArgs(1),
		RunE: o.run,
	}

	cmd.Flags().IntVarP(&o.numPlayers, "players", "p", 3, "minimum number of players or teams per pool")
	cmd.Flags().BoolVarP(&o.roundRobin, "roundRobin", "r", false, "ensure all pools are round robin. Example, in a pool of 4, everyone would fight everyone")
	cmd.Flags().StringVarP(&o.filePath, "file", "f", "", "file with the list of players/teams")
	cmd.Flags().BoolVarP(&o.sanatize, "sanatize", "s", false, "Sanatize names into first and last name and capitalize")
	cmd.Flags().StringVarP(&o.outputPath, "output", "o", "", "output path for the excel file")

	return cmd
}

func (o *createOptions) run(cmd *cobra.Command, args []string) error {
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
	rand.Shuffle(len(entries), func(i, j int) {
		entries[i], entries[j] = entries[j], entries[i]
	})

	players := helper.CreatePlayers(entries)
	pools := helper.CreatePools(players, o.numPlayers)

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

	helper.AddDataToSheet(f, pools, o.sanatize)
	helper.AddPoolsToSheet(f, pools)

	finals := helper.GenerateFinals(pools)
	tree := helper.CreateBalancedTree(finals)
	// helper.calc
	depth := helper.CalculateDepth(tree)
	fmt.Printf("Tree Depth: %d\n", depth)
	helper.PrintLeafNodes(tree, f, "Tree", depth*2, 4, depth)
	helper.AddPoolsToTree(f, "Tree", pools)

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

	if o.roundRobin {
		helper.CreatePoolRoundRobinMatches(pools)
	} else {
		helper.CreatePoolMatches(pools)
	}

	poolMatchWinners := helper.PrintPoolMatches(f, pools)
	helper.PrintEliminationMatches(f, poolMatchWinners, matchMapping, eliminationMatchRounds)

	helper.CreateNamesToPrint(f, pools, o.sanatize)

	// Save the spreadsheet file
	if err := f.SaveAs(o.outputPath); err != nil {
		fmt.Println("Error saving Excel file:", err)
		return nil
	}

	fmt.Println("Excel file created successfully:", o.outputPath)
	return nil
}

func (o *createOptions) createGroups(entries []string) ([]map[int]string, error) {

	groups := make([]map[int]string, 0)
	currentGroup := make(map[int]string)

	// Calculate the number of full groups and the remaining entries
	fullGroups := len(entries) / o.numPlayers
	remainingEntries := len(entries) % o.numPlayers

	// Assign entries to full groups
	for i := 0; i < fullGroups; i++ {
		for j := 1; j <= o.numPlayers; j++ {
			index := i*o.numPlayers + j - 1
			currentGroup[j] = entries[index]
		}
		groups = append(groups, currentGroup)
		currentGroup = make(map[int]string)
	}
	// Distribute remaining entries among previous groups
	if remainingEntries > 0 {
		fmt.Printf("Some groups will have more than: %d\n", o.numPlayers)
	}

	for i := 0; i < remainingEntries; i++ {
		index := fullGroups*o.numPlayers + i
		groupIndex := i % fullGroups
		groups[groupIndex][o.numPlayers+i+1] = entries[index]
	}

	return groups, nil
}

func init() {
	rootCmd.AddCommand(newCreateCmd())
}
