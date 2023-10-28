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
	numPlayers  int
	teamMatches int
	filePath    string
	outputPath  string
	roundRobin  bool
	sanatize    bool
	determined  bool
	noPools     bool
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

	cmd.Flags().BoolVarP(&o.determined, "determined", "d", false, "Do not shuffle the names read from the input file")
	cmd.Flags().StringVarP(&o.filePath, "file", "f", "", "file with the list of players/teams")
	cmd.Flags().BoolVarP(&o.noPools, "no-pools", "", false, "Do not create pools and have only straight knockouts.")
	cmd.Flags().StringVarP(&o.outputPath, "output", "o", "", "output path for the excel file")
	cmd.Flags().IntVarP(&o.numPlayers, "players", "p", 3, "minimum number of players/teams per pool")
	cmd.Flags().BoolVarP(&o.roundRobin, "round-robin", "r", false, "ensure all pools are round robin. Example, in a pool of 4, everyone would fight everyone")
	cmd.Flags().BoolVarP(&o.sanatize, "sanatize", "s", false, "Sanatize names into first and last name and capitalize")
	cmd.Flags().IntVarP(&o.teamMatches, "team-matches", "t", 0, "create team matches with x players per team (default 0)")

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
	if !o.determined {
		rand.Shuffle(len(entries), func(i, j int) {
			entries[i], entries[j] = entries[j], entries[i]
		})
	}

	players := helper.CreatePlayers(entries)
	var pools []helper.Pool

	if !o.noPools {
		pools = helper.CreatePools(players, o.numPlayers)
	}

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

	if o.noPools {
		helper.AddPlayerDataToSheet(f, players, o.sanatize)
	} else {
		helper.AddPoolDataToSheet(f, pools, o.sanatize)
	}
	var tree *helper.Node

	if !o.noPools {
		helper.AddPoolsToSheet(f, pools)
		finals := helper.GenerateFinals(pools)
		tree = helper.CreateBalancedTree(finals, false)
	} else {
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
		tree = helper.CreateBalancedTree(names, o.sanatize)
	}

	// helper.calc
	depth := helper.CalculateDepth(tree)
	fmt.Printf("Tree Depth: %d\n", depth)
	helper.PrintLeafNodes(tree, f, "Tree", depth*2, 4, depth)

	if !o.noPools {
		helper.AddPoolsToTree(f, "Tree", pools)
	}

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

	if !o.noPools {
		if o.roundRobin {
			helper.CreatePoolRoundRobinMatches(pools)
		} else {
			helper.CreatePoolMatches(pools)
		}
	}

	var matchWinners map[string]helper.MatchWinner
	if o.noPools {
		f.DeleteSheet("Pool Draw")
		f.DeleteSheet("Pool Matches")
		// hurray! they are all winners
		matchWinners = helper.ConvertPlayersToWinners(players, o.sanatize)
		helper.CreateNamesToPrint(f, players, o.sanatize)

	} else {
		matchWinners = helper.PrintPoolMatches(f, pools, o.teamMatches)
		helper.CreateNamesWithPoolToPrint(f, pools, o.sanatize)
	}

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
	rootCmd.AddCommand(newCreateCmd())
}
