package cmd

import (
	"bufio"
	"fmt"
	"io"
	"math"
	"math/rand"
	"os"
	"strconv"

	"github.com/gitrgoliveira/bracket-creator/internal/domain"
	"github.com/gitrgoliveira/bracket-creator/internal/helper"
	"github.com/spf13/cobra"

	excelize "github.com/xuri/excelize/v2"
)

type poolOptions struct {
	numPlayers      int
	maxPlayers      int
	poolWinners     int
	teamMatches     int
	filePath        string
	outputPath      string
	outputWriter    *bufio.Writer
	roundRobin      bool
	withZekkenName  bool
	singleTree      bool
	determined      bool
	SeedAssignments []domain.SeedAssignment
}

func newCreatePoolCmd() *cobra.Command {

	o := &poolOptions{}

	cmd := &cobra.Command{
		Use:          "create-pools",
		Short:        "creates Pool brackets",
		SilenceUsage: true,
		// Args:         cobra.ExactArgs(1),
		RunE: o.run,
	}

	cmd.PersistentFlags().BoolVarP(&o.determined, "determined", "d", false, "Do not shuffle the names read from the input file (default false)")
	cmd.PersistentFlags().StringVarP(&o.filePath, "file", "f", "", "file with the list of players/teams")
	cmd.PersistentFlags().StringVarP(&o.outputPath, "output", "o", "", "output path for the excel file")
	cmd.Flags().IntVarP(&o.numPlayers, "players", "p", 3, "minimum number of players/teams per pool")
	cmd.Flags().IntVarP(&o.maxPlayers, "max-players", "m", 0, "maximum number of players/teams per pool")
	cmd.Flags().IntVarP(&o.poolWinners, "pool-winners", "w", 2, "number of players/teams that can qualify from each pool")
	cmd.Flags().BoolVarP(&o.roundRobin, "round-robin", "r", false, "ensure all pools are round robin. Example, in a pool of 4, everyone would fight everyone (default false)")
	cmd.Flags().BoolVarP(&o.withZekkenName, "with-zekken-name", "z", false, "Use the second column of the input CSV as the participant's display name on the zekken. Falls back to sanitized name if empty.")
	cmd.Flags().BoolVarP(&o.singleTree, "single-tree", "", false, "Create a single tree instead of dividing into multiple sheets (default false)")
	cmd.Flags().IntVarP(&o.teamMatches, "team-matches", "t", 0, "create team matches with x players per team (default 0)")

	cmd.MarkFlagsMutuallyExclusive("players", "max-players")

	if err := cmd.MarkPersistentFlagRequired("file"); err != nil {
		fmt.Fprintf(os.Stderr, "Error marking file flag as required: %v\n", err)
	}
	if err := cmd.MarkPersistentFlagRequired("output"); err != nil {
		fmt.Fprintf(os.Stderr, "Error marking output flag as required: %v\n", err)
	}

	return cmd
}

func (o *poolOptions) run(cmd *cobra.Command, args []string) error {
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

	err = o.createPools(entries)
	if err != nil {
		return fmt.Errorf("failed to create pools: %w", err)
	}

	fmt.Println("Excel file created successfully:", o.outputPath)
	return nil
}

func (o *poolOptions) createPools(entries []string) error {
	var isMax bool
	var activePoolSize int
	if o.maxPlayers > 0 {
		isMax = true
		activePoolSize = o.maxPlayers
	} else {
		isMax = false
		activePoolSize = o.numPlayers
	}

	// validation
	if len(entries) < o.poolWinners {
		return fmt.Errorf("number of entries must be higher than number of winners per pool")
	}
	if !isMax && len(entries) < activePoolSize {
		return fmt.Errorf("number of entries must be greater than requested players in pool")
	}

	if activePoolSize < 2 {
		return fmt.Errorf("number of players per pool must be greater than 1")
	}
	if o.poolWinners >= activePoolSize {
		return fmt.Errorf("number of pool winners must be less than number of players per pool")
	}

	entries = helper.RemoveDuplicates(entries)

	// Shuffle all entries
	if !o.determined {
		rand.Shuffle(len(entries), func(i, j int) {
			entries[i], entries[j] = entries[j], entries[i]
		})
	}

	players, err := helper.CreatePlayers(entries, o.withZekkenName)
	if err != nil {
		return err
	}

	if len(o.SeedAssignments) > 0 {
		err := helper.ApplySeeds(players, o.SeedAssignments)
		if err != nil {
			return fmt.Errorf("failed to apply seeds: %w", err)
		}
	}

	// Reorder players to ensure seeded participants are distributed effectively across pools
	players = helper.StandardSeeding(players)

	pools := helper.CreatePools(players, activePoolSize, isMax)

	// Opening the template Excel file.
	var templateFile io.ReadCloser
	templateFile, err = helper.TemplateFile.Open("template.xlsx")
	if err != nil {
		fmt.Println("Warning: template.xlsx not found in embedded FS, trying local disk:", err)
		templateFile, err = os.Open("template.xlsx")
		if err != nil {
			return fmt.Errorf("could not find template.xlsx: %w", err)
		}
	}

	f, err := excelize.OpenReader(templateFile)
	if err != nil {
		return fmt.Errorf("failed to open template Excel file: %w", err)
	}
	defer func() {
		if err := f.Close(); err != nil {
			fmt.Println(err)
		}
	}()

	if o.withZekkenName {
		fmt.Println("Using Zekken names")
	}

	helper.AddPoolDataToSheet(f, pools, o.withZekkenName)

	if err := helper.AddPoolsToSheet(f, pools); err != nil {
		fmt.Fprintf(os.Stderr, "Error adding pools to sheet: %v\n", err)
	}
	finals := helper.GenerateFinals(pools, o.poolWinners)

	fmt.Printf("There will be %d finalists\n", len(finals))

	maxPlayersPerTree := 16
	numPages, err := helper.RoundToPowerOf2(float64(len(finals)), float64(maxPlayersPerTree))
	if err != nil {
		return err
	}
	if numPages < 1 || o.singleTree {
		numPages = 1
	}
	fmt.Printf("Spread across %d tree pages\n", numPages)

	// Create balanced tree
	tree := helper.CreateBalancedTree(finals)

	// divide the tree depending on the number of pages
	subtrees := helper.SubdivideTree(tree, numPages)

	// Create pool matches and get winners BEFORE creating tree sheets
	if o.roundRobin {
		helper.CreatePoolRoundRobinMatches(pools)
	} else {
		helper.CreatePoolMatches(pools)
	}
	matchWinners := helper.PrintPoolMatches(f, pools, o.teamMatches, o.poolWinners)

	treeSheet, err := f.GetSheetIndex("Tree")
	if err != nil {
		return fmt.Errorf("could not find Tree sheet: %w", err)
	}
	numPools := int(math.Ceil(float64(len(pools)) / float64(len(subtrees))))
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
		startRow := 1
		helper.PrintLeafNodes(subtrees[i], f, subtreeSheet, depth*2, startRow, depth, true, matchWinners)
		helper.PrintLeafNodes(subtrees[i], f, subtreeSheet, depth*2, startRow, depth, true, matchWinners)

		lastPos := (i + 1) * numPools
		if lastPos > len(pools) {
			lastPos = len(pools)
		}

		helper.AddPoolsToTree(f, subtreeSheet, pools[i*numPools:lastPos])
	}
	if err := f.DeleteSheet("Tree"); err != nil {
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

	helper.CreateNamesWithPoolToPrint(f, pools, o.withZekkenName)

	if err := helper.CreateTagsSheet(f, pools); err != nil {
		fmt.Fprintf(os.Stderr, "Error creating tags sheet: %v\n", err)
	}

	helper.PrintTeamEliminationMatches(f, matchWinners, eliminationMatchRounds, o.teamMatches)
	helper.FillEstimations(f, int64(len(pools)), int64(len(pools[0].Matches)), 0, int64(o.teamMatches), int64(len(finals)-1))

	// Save the spreadsheet file
	err = f.Write(o.outputWriter)
	if err != nil {
		return fmt.Errorf("error writing to buffer: %w", err)
	}

	return nil
}

func init() {
	rootCmd.AddCommand(newCreatePoolCmd())
}
